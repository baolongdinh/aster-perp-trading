package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// PatternAccuracy tracks prediction accuracy per regime (T081a)
type PatternAccuracy struct {
	Regime             RegimeType `json:"regime"`
	TotalPredictions   int        `json:"total_predictions"`
	CorrectPredictions int        `json:"correct_predictions"`
	Accuracy           float64    `json:"accuracy"` // 0-100%
	LastUpdated        time.Time  `json:"last_updated"`
}

// PatternPrediction records a prediction for later validation
type PatternPrediction struct {
	Timestamp    time.Time
	Regime       RegimeType
	PredictedPnL float64 // Predicted outcome
	ActualPnL    float64 // Filled in later when trade completes
	PatternIDs   []string
	Validated    bool
}

// PairPatterns holds patterns for a specific trading pair
type PairPatterns struct {
	Pair              string                          `json:"pair"`
	Patterns          []HistoricalPattern             `json:"patterns"`
	AccuracyMap       map[RegimeType]*PatternAccuracy `json:"accuracy_map"`
	PredictionHistory []PatternPrediction             `json:"prediction_history"`
	IsActive          bool                            `json:"is_active"`
	mu                sync.RWMutex
}

// PatternStore manages historical patterns for multiple pairs (BTC, ETH, SOL)
type PatternStore struct {
	config     *PatternsConfig
	pairStores map[string]*PairPatterns // Key: "BTCUSD1", "ETHUSD1", "SOLUSD1"
	mu         sync.RWMutex
}

// NewPatternStore creates a new pattern store for multiple pairs
func NewPatternStore(config *PatternsConfig) (*PatternStore, error) {
	ps := &PatternStore{
		config:     config,
		pairStores: make(map[string]*PairPatterns),
	}
	return ps, nil
}

// getOrCreatePairStore gets or creates a pair-specific store
func (ps *PatternStore) getOrCreatePairStore(pair string) *PairPatterns {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Normalize pair name (remove / and uppercase)
	key := normalizePair(pair)

	if store, ok := ps.pairStores[key]; ok {
		return store
	}

	// Create new pair store
	store := &PairPatterns{
		Pair:              key,
		Patterns:          make([]HistoricalPattern, 0),
		AccuracyMap:       make(map[RegimeType]*PatternAccuracy),
		PredictionHistory: make([]PatternPrediction, 0),
		IsActive:          false,
	}

	// Initialize accuracy tracking for all regimes
	for _, regime := range []RegimeType{RegimeSideways, RegimeTrending, RegimeVolatile, RegimeRecovery} {
		store.AccuracyMap[regime] = &PatternAccuracy{
			Regime:      regime,
			LastUpdated: time.Now(),
		}
	}

	ps.pairStores[key] = store

	// Try to load existing patterns for this pair
	_ = ps.loadPairPatterns(key, store)

	return store
}

// normalizePair normalizes pair name for storage key
func normalizePair(pair string) string {
	// Remove / and -, uppercase
	pair = strings.ToUpper(pair)
	pair = strings.ReplaceAll(pair, "/", "")
	pair = strings.ReplaceAll(pair, "-", "")
	return pair
}

// loadPairPatterns loads patterns for a specific pair from disk
func (ps *PatternStore) loadPairPatterns(key string, store *PairPatterns) error {
	patternFile := ps.getPairPatternFilePath(key)
	data, err := os.ReadFile(patternFile)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, store); err != nil {
		return fmt.Errorf("failed to unmarshal patterns for %s: %w", key, err)
	}

	// Reinitialize accuracy map if missing
	if store.AccuracyMap == nil {
		store.AccuracyMap = make(map[RegimeType]*PatternAccuracy)
		for _, regime := range []RegimeType{RegimeSideways, RegimeTrending, RegimeVolatile, RegimeRecovery} {
			store.AccuracyMap[regime] = &PatternAccuracy{
				Regime:      regime,
				LastUpdated: time.Now(),
			}
		}
	}

	store.updateActivationStatus(ps.config.MinTradesToActivate)
	return nil
}

// SavePair saves patterns for a specific pair to disk
func (ps *PatternStore) SavePair(pair string) error {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	key := normalizePair(pair)
	store, ok := ps.pairStores[key]
	if !ok {
		return fmt.Errorf("no patterns for pair %s", pair)
	}

	return ps.savePairStore(key, store)
}

// savePairStore saves a pair store to disk
func (ps *PatternStore) savePairStore(key string, store *PairPatterns) error {
	store.mu.RLock()
	defer store.mu.RUnlock()

	patternFile := ps.getPairPatternFilePath(key)

	// Ensure directory exists
	dir := filepath.Dir(patternFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create pattern directory: %w", err)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal patterns: %w", err)
	}

	if err := os.WriteFile(patternFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write pattern file: %w", err)
	}

	return nil
}

// getPairPatternFilePath returns file path for a pair's patterns
func (ps *PatternStore) getPairPatternFilePath(key string) string {
	return filepath.Join(ps.config.StoragePath, fmt.Sprintf("%s_patterns.json", strings.ToLower(key)))
}

// AddPattern adds a new pattern for a specific pair after a trade completes
func (ps *PatternStore) AddPattern(pair string, snapshot RegimeSnapshot, params GridParams, outcome TradeOutcome) {
	store := ps.getOrCreatePairStore(pair)
	store.mu.Lock()
	defer store.mu.Unlock()

	// Create context vector from factors
	contextVector := ps.createContextVector(snapshot.Indicators)

	pattern := HistoricalPattern{
		ID:              uuid.New().String(),
		CreatedAt:       time.Now(),
		ContextVector:   contextVector,
		Regime:          snapshot.Regime,
		GridParams:      params,
		OutcomePnL:      outcome.PnL,
		OutcomeDuration: int(outcome.Duration.Minutes()),
		MaxDrawdown:     outcome.MaxDrawdown,
	}

	store.Patterns = append(store.Patterns, pattern)

	// Purge old patterns if needed
	store.purgeOldPatterns(ps.config.DecayHalfLifeDays * 2)
	store.updateActivationStatus(ps.config.MinTradesToActivate)

	// Save asynchronously
	go ps.SavePair(pair)
}

// FindMatches finds similar historical patterns for a specific pair
func (ps *PatternStore) FindMatches(pair string, currentIndicators IndicatorSnapshot, limit int) []PatternMatch {
	store := ps.getOrCreatePairStore(pair)
	store.mu.RLock()
	defer store.mu.RUnlock()

	if !store.IsActive {
		return nil
	}

	currentVector := ps.createContextVector(currentIndicators)

	// Calculate similarity for all patterns
	type scoredPattern struct {
		pattern    HistoricalPattern
		similarity float64
		weight     float64
	}

	scored := make([]scoredPattern, 0, len(store.Patterns))
	for _, p := range store.Patterns {
		similarity := ps.calculateSimilarity(currentVector, p.ContextVector)
		weight := ps.calculateDecayWeight(p.CreatedAt, ps.config.DecayHalfLifeDays)

		scored = append(scored, scoredPattern{
			pattern:    p,
			similarity: similarity,
			weight:     weight,
		})
	}

	// Sort by weighted similarity
	sort.Slice(scored, func(i, j int) bool {
		scoreI := scored[i].similarity * scored[i].weight
		scoreJ := scored[j].similarity * scored[j].weight
		return scoreI > scoreJ
	})

	// Return top matches above threshold
	matches := make([]PatternMatch, 0, limit)
	for i, sp := range scored {
		if i >= limit {
			break
		}
		if sp.similarity < ps.config.SimilarityThreshold {
			continue
		}

		matches = append(matches, PatternMatch{
			PatternID:       sp.pattern.ID,
			SimilarityScore: sp.similarity,
			HistoricalPnL:   sp.pattern.OutcomePnL,
			Weight:          sp.weight,
		})
	}

	return matches
}

// CalculatePatternImpact computes the score adjustment from patterns for a specific pair
func (ps *PatternStore) CalculatePatternImpact(pair string, matches []PatternMatch) float64 {
	store := ps.getOrCreatePairStore(pair)
	store.mu.RLock()
	defer store.mu.RUnlock()

	if len(matches) == 0 {
		return 0
	}

	// Calculate weighted average PnL
	var totalWeight, weightedPnL float64
	for _, m := range matches {
		weightedPnL += m.HistoricalPnL * m.Weight * m.SimilarityScore
		totalWeight += m.Weight * m.SimilarityScore
	}

	if totalWeight == 0 {
		return 0
	}

	avgPnL := weightedPnL / totalWeight

	// Convert to score impact (±5 points max)
	impact := avgPnL * 10

	// Clamp to max impact
	if impact > ps.config.MaxImpactPoints {
		impact = ps.config.MaxImpactPoints
	} else if impact < -ps.config.MaxImpactPoints {
		impact = -ps.config.MaxImpactPoints
	}

	return impact
}

// IsActive returns whether pattern matching is active for a specific pair
func (ps *PatternStore) IsActive(pair string) bool {
	store := ps.getOrCreatePairStore(pair)
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.IsActive && store.hasMinimumAccuracy()
}

// GetPatternCount returns the number of stored patterns for a specific pair
func (ps *PatternStore) GetPatternCount(pair string) int {
	store := ps.getOrCreatePairStore(pair)
	store.mu.RLock()
	defer store.mu.RUnlock()
	return len(store.Patterns)
}

// updateActivationStatus updates whether patterns are active based on count
func (pp *PairPatterns) updateActivationStatus(minTrades int) {
	pp.IsActive = len(pp.Patterns) >= minTrades
}

// hasMinimumAccuracy checks if accuracy >= 60% for any regime (T081b)
func (pp *PairPatterns) hasMinimumAccuracy() bool {
	for _, acc := range pp.AccuracyMap {
		if acc.Accuracy >= 60.0 {
			return true
		}
	}
	return false
}

// purgeOldPatterns removes patterns older than the decay threshold
func (pp *PairPatterns) purgeOldPatterns(days int) {
	cutoff := time.Now().AddDate(0, 0, -days)

	filtered := make([]HistoricalPattern, 0, len(pp.Patterns))
	for _, p := range pp.Patterns {
		if p.CreatedAt.After(cutoff) {
			filtered = append(filtered, p)
		}
	}

	pp.Patterns = filtered
}

// RecordPrediction records a new prediction for accuracy tracking (T081a)
func (ps *PatternStore) RecordPrediction(pair string, regime RegimeType, predictedPnL float64, patternIDs []string) {
	store := ps.getOrCreatePairStore(pair)
	store.mu.Lock()
	defer store.mu.Unlock()

	prediction := PatternPrediction{
		Timestamp:    time.Now(),
		Regime:       regime,
		PredictedPnL: predictedPnL,
		PatternIDs:   patternIDs,
		Validated:    false,
	}

	store.PredictionHistory = append(store.PredictionHistory, prediction)

	// Keep only last 100 predictions
	if len(store.PredictionHistory) > 100 {
		store.PredictionHistory = store.PredictionHistory[len(store.PredictionHistory)-100:]
	}

	// Update total predictions count for regime
	if acc, ok := store.AccuracyMap[regime]; ok {
		acc.TotalPredictions++
		acc.LastUpdated = time.Now()
	}
}

// ValidatePrediction validates a prediction with actual outcome (T081a)
func (ps *PatternStore) ValidatePrediction(pair string, regime RegimeType, actualPnL float64) {
	store := ps.getOrCreatePairStore(pair)
	store.mu.Lock()
	defer store.mu.Unlock()

	// Find most recent unvalidated prediction for this regime
	for i := len(store.PredictionHistory) - 1; i >= 0; i-- {
		pred := &store.PredictionHistory[i]
		if pred.Regime == regime && !pred.Validated {
			pred.ActualPnL = actualPnL
			pred.Validated = true

			// Update accuracy
			ps.updateAccuracy(store, regime, pred.PredictedPnL, actualPnL)
			break
		}
	}
}

// updateAccuracy updates accuracy for a regime (T081a)
func (ps *PatternStore) updateAccuracy(store *PairPatterns, regime RegimeType, predicted, actual float64) {
	acc, ok := store.AccuracyMap[regime]
	if !ok {
		return
	}

	// Consider correct if same sign (both profit or both loss)
	if (predicted > 0 && actual > 0) || (predicted < 0 && actual < 0) {
		acc.CorrectPredictions++
	}

	// Recalculate accuracy
	if acc.TotalPredictions > 0 {
		acc.Accuracy = float64(acc.CorrectPredictions) / float64(acc.TotalPredictions) * 100
	}
	acc.LastUpdated = time.Now()
}

// GetAccuracy returns accuracy for a specific pair and regime (T081a)
func (ps *PatternStore) GetAccuracy(pair string, regime RegimeType) float64 {
	store := ps.getOrCreatePairStore(pair)
	store.mu.RLock()
	defer store.mu.RUnlock()

	if acc, ok := store.AccuracyMap[regime]; ok {
		return acc.Accuracy
	}
	return 0
}

// Helper functions

func (ps *PatternStore) createContextVector(indicators IndicatorSnapshot) []float64 {
	adxNorm := math.Min(indicators.ADX/100.0, 1.0)
	bbWidthNorm := math.Min(indicators.BBWidth/10.0, 1.0)
	atrNorm := math.Min(indicators.ATR14/100.0, 1.0)

	volRatio := 0.5
	if indicators.VolumeMA20 > 0 {
		volRatio = indicators.CurrentVolume / indicators.VolumeMA20
		volRatio = math.Min(volRatio/3.0, 1.0)
	}

	return []float64{adxNorm, bbWidthNorm, atrNorm, volRatio}
}

func (ps *PatternStore) calculateSimilarity(v1, v2 []float64) float64 {
	if len(v1) != len(v2) {
		return 0
	}

	var dotProduct, norm1, norm2 float64
	for i := 0; i < len(v1); i++ {
		dotProduct += v1[i] * v2[i]
		norm1 += v1[i] * v1[i]
		norm2 += v2[i] * v2[i]
	}

	if norm1 == 0 || norm2 == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2))
}

func (ps *PatternStore) calculateDecayWeight(createdAt time.Time, halfLifeDays int) float64 {
	daysSince := time.Since(createdAt).Hours() / 24.0
	halfLife := float64(halfLifeDays)
	return math.Exp(-daysSince / halfLife)
}

// GetActivePairs returns list of pairs with active patterns
func (ps *PatternStore) GetActivePairs() []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	var active []string
	for key, store := range ps.pairStores {
		if store.IsActive && store.hasMinimumAccuracy() {
			active = append(active, key)
		}
	}
	return active
}
