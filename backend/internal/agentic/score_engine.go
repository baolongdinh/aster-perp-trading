package agentic

import (
	"math"
	"sync"
	"time"

	"aster-bot/internal/config"
	"go.uber.org/zap"
)

// ScoreCalculationEngine calculates scores for different trading modes
// This is the core decision-making component for the adaptive state system
type ScoreCalculationEngine struct {
	config   *config.ScoreEngineConfig
	logger   *zap.Logger
	mu       sync.RWMutex
	
	// Historical performance tracking
	performanceHistory map[string]map[TradingMode]*ModePerformance
	
	// Adaptive thresholds that adjust based on market conditions
	adaptiveThresholds map[string]*AdaptiveThresholds
}

// ModePerformance tracks how well a mode has performed
type ModePerformance struct {
	Mode            TradingMode
	TradesCount     int
	WinCount        int
	LossCount       int
	TotalPnL        float64
	AvgPnL          float64
	SuccessRate     float64
	LastUpdated     time.Time
	RecentTrades    []float64 // Last 10 PnL values
}

// ScoreInputs contains all inputs needed for score calculation
type ScoreInputs struct {
	Symbol            string
	RegimeSnapshot    RegimeSnapshot
	MeanReversionSignals float64 // e.g., BB Bounce, RSI Divergence
	FVGSignal         float64   // Fair Value Gap signal
	LiquiditySignal   float64   // Liquidity sweep signal
	BreakoutSignal    float64   // Range breakout signal
	MomentumSignal    float64   // ROC + velocity signal
	VolumeConfirm     float64   // Volume spike confirmation
	Volatility        float64   // Current ATR/volatility
	HistoricalWeight  float64   // Based on past performance
}

// NewScoreCalculationEngine creates a new score calculation engine
func NewScoreCalculationEngine(cfg *config.ScoreEngineConfig, logger *zap.Logger) *ScoreCalculationEngine {
	if cfg == nil {
		cfg = &config.ScoreEngineConfig{
			GridThreshold:     0.6,
			TrendThreshold:    0.75,
			HysteresisBuffer:  0.1,
			RegimeWeight:      0.4,
			SignalWeight:      0.6,
		}
	}
	
	return &ScoreCalculationEngine{
		config:             cfg,
		logger:             logger.With(zap.String("component", "score_engine")),
		performanceHistory: make(map[string]map[TradingMode]*ModePerformance),
		adaptiveThresholds: make(map[string]*AdaptiveThresholds),
	}
}

// CalculateGridScore calculates the GRID mode score (0-1)
// Formula: sideways_regime * 0.4 + mean_reversion_signals * 0.6
func (se *ScoreCalculationEngine) CalculateGridScore(inputs *ScoreInputs) *TradingModeScore {
	se.mu.RLock()
	defer se.mu.RUnlock()
	
	// 1. Regime component (40% weight)
	var regimeComponent float64
	if inputs.RegimeSnapshot.Regime == RegimeSideways {
		regimeComponent = inputs.RegimeSnapshot.Confidence
	} else if inputs.RegimeSnapshot.Regime == RegimeRecovery {
		regimeComponent = inputs.RegimeSnapshot.Confidence * 0.5 // Half weight for recovery
	} else {
		regimeComponent = 0.1 // Small baseline for non-sideways
	}
	
	// 2. Signal component (60% weight)
	// Weight the mean reversion signals
	bbWeight := 0.3
	fvgWeight := 0.4
	liquidityWeight := 0.3
	
	signalComponent := inputs.MeanReversionSignals*bbWeight +
		inputs.FVGSignal*fvgWeight +
		inputs.LiquiditySignal*liquidityWeight
	
	// 3. Apply volatility adjustment
	// Higher volatility = slightly lower score (risk adjustment)
	volatilityFactor := se.calculateVolatilityFactor(inputs.Volatility, inputs.Symbol)
	
	// 4. Apply historical performance weight
	historicalWeight := se.getHistoricalWeight(inputs.Symbol, TradingModeGrid)
	
	// Calculate final score
	rawScore := (regimeComponent*se.config.RegimeWeight + signalComponent*se.config.SignalWeight)
	finalScore := rawScore * volatilityFactor * historicalWeight
	
	// Normalize to 0-1
	finalScore = math.Max(0, math.Min(1, finalScore))
	
	components := map[string]float64{
		"regime":      regimeComponent,
		"signals":     signalComponent,
		"volatility":  volatilityFactor,
		"historical":  historicalWeight,
		"raw":         rawScore,
	}
	
	se.logger.Debug("Calculated GRID score",
		zap.String("symbol", inputs.Symbol),
		zap.Float64("score", finalScore),
		zap.Float64("regime_component", regimeComponent),
		zap.Float64("signal_component", signalComponent),
	)
	
	return &TradingModeScore{
		Mode:       TradingModeGrid,
		Score:      finalScore,
		Threshold:  se.config.GridThreshold,
		Components: components,
		Timestamp:  time.Now(),
		IsActive:   false,
	}
}

// CalculateTrendScore calculates the TREND mode score (0-1)
// Formula: trending_regime * 0.3 + (breakout + momentum) * 0.35 + volume * 0.35
func (se *ScoreCalculationEngine) CalculateTrendScore(inputs *ScoreInputs) *TradingModeScore {
	se.mu.RLock()
	defer se.mu.RUnlock()
	
	// 1. Regime component (30% weight)
	var regimeComponent float64
	if inputs.RegimeSnapshot.Regime == RegimeTrending {
		regimeComponent = inputs.RegimeSnapshot.Confidence
	} else if inputs.RegimeSnapshot.Regime == RegimeVolatile {
		// Volatile can have trends, but lower confidence
		regimeComponent = inputs.RegimeSnapshot.Confidence * 0.3
	} else {
		regimeComponent = 0.05 // Very small baseline
	}
	
	// 2. Signal component (70% weight, split between breakout/momentum/volume)
	// Breakout gets 35%, Momentum gets 35%
	breakoutWeight := 0.35
	momentumWeight := 0.35
	
	signalComponent := inputs.BreakoutSignal*breakoutWeight + inputs.MomentumSignal*momentumWeight
	
	// 3. Volume confirmation (acts as multiplier)
	volumeMultiplier := 0.7 + (inputs.VolumeConfirm * 0.6) // 0.7 to 1.3x
	
	// 4. Apply historical performance weight
	historicalWeight := se.getHistoricalWeight(inputs.Symbol, TradingModeTrending)
	
	// Calculate final score
	rawScore := regimeComponent*0.3 + signalComponent*0.7
	finalScore := rawScore * volumeMultiplier * historicalWeight
	
	// Normalize to 0-1
	finalScore = math.Max(0, math.Min(1, finalScore))
	
	components := map[string]float64{
		"regime":     regimeComponent,
		"breakout":   inputs.BreakoutSignal,
		"momentum":   inputs.MomentumSignal,
		"volume_mult": volumeMultiplier,
		"historical": historicalWeight,
		"raw":        rawScore,
	}
	
	se.logger.Debug("Calculated TREND score",
		zap.String("symbol", inputs.Symbol),
		zap.Float64("score", finalScore),
		zap.Float64("breakout", inputs.BreakoutSignal),
		zap.Float64("momentum", inputs.MomentumSignal),
	)
	
	return &TradingModeScore{
		Mode:       TradingModeTrending,
		Score:      finalScore,
		Threshold:  se.config.TrendThreshold,
		Components: components,
		Timestamp:  time.Now(),
		IsActive:   false,
	}
}

// CalculateHybridTrendScore calculates hybrid score using breakout + momentum
// Formula: max(breakout, momentum) * 0.6 + min(breakout, momentum) * 0.4
// Plus agreement bonus when signals align
func (se *ScoreCalculationEngine) CalculateHybridTrendScore(
	breakoutSignal, momentumSignal float64,
) float64 {
	maxSignal := math.Max(breakoutSignal, momentumSignal)
	minSignal := math.Min(breakoutSignal, momentumSignal)
	
	// Base hybrid score
	hybrid := maxSignal*0.6 + minSignal*0.4
	
	// Agreement bonus: when signals are close (agreeing), add bonus
	difference := math.Abs(breakoutSignal - momentumSignal)
	if difference < 0.2 {
		// Strong agreement - add up to 15% bonus
		agreementBonus := (0.2 - difference) * 0.75 // Max 0.15
		hybrid *= (1 + agreementBonus)
	}
	
	return math.Max(0, math.Min(1, hybrid))
}

// CalculateAllScores calculates scores for all trading modes
func (se *ScoreCalculationEngine) CalculateAllScores(inputs *ScoreInputs) map[TradingMode]*TradingModeScore {
	scores := make(map[TradingMode]*TradingModeScore)
	
	// Calculate GRID score
	scores[TradingModeGrid] = se.CalculateGridScore(inputs)
	
	// Calculate TREND score
	scores[TradingModeTrending] = se.CalculateTrendScore(inputs)
	
	// For ACCUMULATION: based on compression detection
	accumulationScore := se.calculateAccumulationScore(inputs)
	scores[TradingModeAccumulation] = accumulationScore
	
	se.logger.Info("All scores calculated",
		zap.String("symbol", inputs.Symbol),
		zap.Float64("grid_score", scores[TradingModeGrid].Score),
		zap.Float64("trend_score", scores[TradingModeTrending].Score),
		zap.Float64("accumulation_score", accumulationScore.Score),
	)
	
	return scores
}

// calculateAccumulationScore detects pre-breakout accumulation patterns
func (se *ScoreCalculationEngine) calculateAccumulationScore(inputs *ScoreInputs) *TradingModeScore {
	// Compression detection: low BB width + low ATR
	bbWidth := inputs.RegimeSnapshot.BBWidth
	atr := inputs.RegimeSnapshot.ATR14
	
	// Compression score: lower is more compressed
	compressionScore := 0.0
	if bbWidth < 0.02 && atr < 0.003 {
		compressionScore = 1.0 // Strong compression
	} else if bbWidth < 0.03 {
		compressionScore = 0.7 // Moderate compression
	} else if bbWidth < 0.05 {
		compressionScore = 0.4 // Weak compression
	}
	
	// Volume profile: increasing volume during compression = accumulation
	volumeScore := inputs.VolumeConfirm
	
	// Combine: compression is primary, volume confirms
	finalScore := compressionScore*0.7 + volumeScore*0.3
	
	return &TradingModeScore{
		Mode:       TradingModeAccumulation,
		Score:      finalScore,
		Threshold:  0.6, // Lower threshold for accumulation
		Components: map[string]float64{
			"compression": compressionScore,
			"volume":      volumeScore,
		},
		Timestamp: time.Now(),
		IsActive:  false,
	}
}

// calculateVolatilityFactor adjusts score based on volatility
func (se *ScoreCalculationEngine) calculateVolatilityFactor(volatility float64, symbol string) float64 {
	// Get average volatility for this symbol
	avgVolatility := se.getAverageVolatility(symbol)
	
	if avgVolatility == 0 {
		return 1.0 // No history, neutral
	}
	
	// Ratio of current to average
	ratio := volatility / avgVolatility
	
	// Adjust: very high volatility reduces score (risk), very low increases slightly
	if ratio > 2.0 {
		return 0.8 // High volatility = 20% reduction
	} else if ratio > 1.5 {
		return 0.9 // Moderately high = 10% reduction
	} else if ratio < 0.5 {
		return 1.05 // Very low volatility = 5% bonus
	}
	
	return 1.0 // Normal range = neutral
}

// getHistoricalWeight returns performance-based weight for a mode
func (se *ScoreCalculationEngine) getHistoricalWeight(symbol string, mode TradingMode) float64 {
	se.mu.RLock()
	defer se.mu.RUnlock()
	
	if perfs, ok := se.performanceHistory[symbol]; ok {
		if perf, ok := perfs[mode]; ok {
			// Weight based on success rate: 0.8 to 1.2 range
			// 50% success = 1.0 weight
			// 80% success = 1.2 weight (max)
			// 20% success = 0.8 weight (min)
			weight := 0.8 + (perf.SuccessRate * 0.5)
			return math.Max(0.8, math.Min(1.2, weight))
		}
	}
	
	return 1.0 // Default neutral weight
}

// getAverageVolatility returns average ATR for symbol
func (se *ScoreCalculationEngine) getAverageVolatility(symbol string) float64 {
	// This would be populated from historical data
	// For now, return 0 (neutral)
	return 0
}

// UpdatePerformance updates performance tracking for a mode
func (se *ScoreCalculationEngine) UpdatePerformance(
	symbol string,
	mode TradingMode,
	pnl float64,
) {
	se.mu.Lock()
	defer se.mu.Unlock()
	
	if _, ok := se.performanceHistory[symbol]; !ok {
		se.performanceHistory[symbol] = make(map[TradingMode]*ModePerformance)
	}
	
	perf, ok := se.performanceHistory[symbol][mode]
	if !ok {
		perf = &ModePerformance{
			Mode:         mode,
			RecentTrades: make([]float64, 0, 10),
		}
		se.performanceHistory[symbol][mode] = perf
	}
	
	// Update stats
	perf.TradesCount++
	perf.TotalPnL += pnl
	if pnl > 0 {
		perf.WinCount++
	} else {
		perf.LossCount++
	}
	
	// Update recent trades window
	perf.RecentTrades = append(perf.RecentTrades, pnl)
	if len(perf.RecentTrades) > 10 {
		perf.RecentTrades = perf.RecentTrades[1:]
	}
	
	// Recalculate averages
	perf.AvgPnL = perf.TotalPnL / float64(perf.TradesCount)
	perf.SuccessRate = float64(perf.WinCount) / float64(perf.TradesCount)
	perf.LastUpdated = time.Now()
}

// GetBestMode returns the trading mode with highest score above threshold
func (se *ScoreCalculationEngine) GetBestMode(
	scores map[TradingMode]*TradingModeScore,
	currentMode TradingMode,
) (TradingMode, float64, bool) {
	var bestMode TradingMode
	var bestScore float64
	
	for mode, score := range scores {
		// Apply hysteresis: need higher score to switch from current mode
		threshold := score.Threshold
		if mode != currentMode {
			threshold += se.config.HysteresisBuffer
		}
		
		if score.Score >= threshold && score.Score > bestScore {
			bestMode = mode
			bestScore = score.Score
		}
	}
	
	found := bestScore > 0
	return bestMode, bestScore, found
}

// GetAdaptiveThresholds returns adaptive thresholds for a symbol
func (se *ScoreCalculationEngine) GetAdaptiveThresholds(symbol string) *AdaptiveThresholds {
	se.mu.RLock()
	defer se.mu.RUnlock()
	
	if thresholds, ok := se.adaptiveThresholds[symbol]; ok {
		return thresholds
	}
	
	// Return default thresholds
	return &AdaptiveThresholds{
		GridThreshold:    se.config.GridThreshold,
		TrendThreshold:   se.config.TrendThreshold,
		HysteresisBuffer: se.config.HysteresisBuffer,
		VolatilityFactor: 1.0,
	}
}

// UpdateAdaptiveThresholds updates thresholds based on market conditions
func (se *ScoreCalculationEngine) UpdateAdaptiveThresholds(
	symbol string,
	volatility float64,
	recentFlipFlops int,
) {
	se.mu.Lock()
	defer se.mu.Unlock()
	
	if _, ok := se.adaptiveThresholds[symbol]; !ok {
		se.adaptiveThresholds[symbol] = &AdaptiveThresholds{
			GridThreshold:    se.config.GridThreshold,
			TrendThreshold:   se.config.TrendThreshold,
			HysteresisBuffer: se.config.HysteresisBuffer,
			VolatilityFactor: 1.0,
		}
	}
	
	thresholds := se.adaptiveThresholds[symbol]
	
	// Increase hysteresis if too many flip-flops
	if recentFlipFlops > 3 {
		thresholds.HysteresisBuffer = math.Min(0.2, se.config.HysteresisBuffer*1.5)
	} else {
		thresholds.HysteresisBuffer = se.config.HysteresisBuffer
	}
	
	// Adjust by volatility
	thresholds.VolatilityFactor = se.calculateVolatilityFactor(volatility, symbol)
}
