package adaptive_grid

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// PerformanceRecord tracks performance for a condition/strategy combination
type PerformanceRecord struct {
	Condition string
	Strategy  string
	Profit    float64
	Risk      float64
	Volume    float64
	Drawdown  float64
	Timestamp time.Time
}

// ThresholdHistory tracks historical threshold values
type ThresholdHistory struct {
	Symbol    string
	Dimension string
	Value     float64
	Timestamp time.Time
	Source    string // "manual", "learned", "default"
}

// LearningParameters defines learning engine parameters
type LearningParameters struct {
	LearningRate      float64 // Rate of adaptation (0-1)
	PerformanceWindow int     // Number of records to consider
	MinSamples        int     // Minimum samples before adaptation
	ABTestEnabled     bool    // Enable A/B testing
	ABTestDuration    time.Duration
	ExplorationRate   float64 // Rate of random exploration (0-1)

	// Seed Parameters (Cold Start)
	SeedThresholds map[string]float64 // Initial safe thresholds
	SeedRangePct   float64            // Max deviation from seed (e.g., 0.2 = ±20%)
	EnableSeed     bool               // Use seed parameters on initialization
}

// LearningEngine tracks performance and adapts thresholds/parameters over time
type LearningEngine struct {
	logger *zap.Logger

	// Performance database
	performanceDB map[string][]PerformanceRecord // key: condition_strategy
	performanceMu sync.RWMutex

	// Threshold history
	thresholdHistory map[string][]ThresholdHistory // key: symbol_dimension
	thresholdMu      sync.RWMutex

	// Optimal thresholds per symbol
	optimalThresholds map[string]map[string]float64 // symbol -> dimension -> value
	optimalMu         sync.RWMutex

	// Learning parameters
	params LearningParameters

	// A/B testing state
	abTestActive map[string]bool // symbol -> is in A/B test
	abTestMu     sync.RWMutex
}

// NewLearningEngine creates a new learning engine
func NewLearningEngine(logger *zap.Logger) *LearningEngine {
	// Default safe seed thresholds
	seedThresholds := map[string]float64{
		"position_threshold":   0.8,  // 80% position threshold
		"volatility_threshold": 0.7,  // 70% volatility threshold
		"risk_threshold":       0.6,  // 60% risk threshold
		"drawdown_threshold":   0.15, // 15% drawdown threshold
	}

	return &LearningEngine{
		logger:            logger,
		performanceDB:     make(map[string][]PerformanceRecord),
		thresholdHistory:  make(map[string][]ThresholdHistory),
		optimalThresholds: make(map[string]map[string]float64),
		params: LearningParameters{
			LearningRate:      0.1,   // 10% adaptation rate
			PerformanceWindow: 100,   // Last 100 records
			MinSamples:        10,    // Minimum 10 samples
			ABTestEnabled:     false, // Disabled by default
			ABTestDuration:    time.Hour,
			ExplorationRate:   0.05, // 5% exploration
			SeedThresholds:    seedThresholds,
			SeedRangePct:      0.2,  // Allow ±20% deviation from seed
			EnableSeed:        true, // Use seed parameters on cold start
		},
		abTestActive: make(map[string]bool),
	}
}

// RecordPerformance records performance for a condition/strategy
func (l *LearningEngine) RecordPerformance(condition, strategy string, profit, risk, volume, drawdown float64) {
	l.performanceMu.Lock()
	defer l.performanceMu.Unlock()

	key := condition + "_" + strategy
	record := PerformanceRecord{
		Condition: condition,
		Strategy:  strategy,
		Profit:    profit,
		Risk:      risk,
		Volume:    volume,
		Drawdown:  drawdown,
		Timestamp: time.Now(),
	}

	l.performanceDB[key] = append(l.performanceDB[key], record)

	// Keep only the most recent records
	if len(l.performanceDB[key]) > l.params.PerformanceWindow {
		l.performanceDB[key] = l.performanceDB[key][len(l.performanceDB[key])-l.params.PerformanceWindow:]
	}

	l.logger.Debug("Performance recorded",
		zap.String("condition", condition),
		zap.String("strategy", strategy),
		zap.Float64("profit", profit),
		zap.Float64("risk", risk),
		zap.Int("total_records", len(l.performanceDB[key])))
}

// GetPerformance retrieves performance data for a condition/strategy
func (l *LearningEngine) GetPerformance(condition, strategy string) (avgProfit, avgRisk, avgVolume, avgDrawdown float64, count int) {
	l.performanceMu.RLock()
	defer l.performanceMu.RUnlock()

	key := condition + "_" + strategy
	records, exists := l.performanceDB[key]
	if !exists || len(records) == 0 {
		return 0, 0, 0, 0, 0
	}

	var totalProfit, totalRisk, totalVolume, totalDrawdown float64
	for _, record := range records {
		totalProfit += record.Profit
		totalRisk += record.Risk
		totalVolume += record.Volume
		totalDrawdown += record.Drawdown
	}

	count = len(records)
	avgProfit = totalProfit / float64(count)
	avgRisk = totalRisk / float64(count)
	avgVolume = totalVolume / float64(count)
	avgDrawdown = totalDrawdown / float64(count)

	return avgProfit, avgRisk, avgVolume, avgDrawdown, count
}

// AdaptThreshold adapts a threshold based on recent performance
func (l *LearningEngine) AdaptThreshold(symbol, dimension string, recentPerformance float64) float64 {
	l.thresholdMu.Lock()
	defer l.thresholdMu.Unlock()

	// Get current optimal threshold
	if l.optimalThresholds[symbol] == nil {
		l.optimalThresholds[symbol] = make(map[string]float64)
	}
	currentValue, exists := l.optimalThresholds[symbol][dimension]
	if !exists {
		// COLD START: Use seed parameters if available
		if l.params.EnableSeed && l.params.SeedThresholds != nil {
			if seedValue, seedExists := l.params.SeedThresholds[dimension]; seedExists {
				currentValue = seedValue
				l.optimalThresholds[symbol][dimension] = currentValue
				l.logger.Info("Cold start - using seed threshold",
					zap.String("symbol", symbol),
					zap.String("dimension", dimension),
					zap.Float64("seed_value", seedValue))
				return currentValue
			}
		}
		// No seed available, use recent performance as starting point
		currentValue = recentPerformance
		l.optimalThresholds[symbol][dimension] = currentValue
		return currentValue
	}

	// Adapt threshold based on learning rate
	newValue := currentValue + l.params.LearningRate*(recentPerformance-currentValue)

	// SEED RANGE LIMIT: Ensure value stays within ±SeedRangePct of seed
	if l.params.EnableSeed && l.params.SeedThresholds != nil {
		if seedValue, seedExists := l.params.SeedThresholds[dimension]; seedExists {
			minValue := seedValue * (1.0 - l.params.SeedRangePct)
			maxValue := seedValue * (1.0 + l.params.SeedRangePct)

			if newValue < minValue {
				newValue = minValue
				l.logger.Warn("Adapted value below seed range, clamping",
					zap.String("dimension", dimension),
					zap.Float64("new_value", newValue),
					zap.Float64("min_allowed", minValue))
			} else if newValue > maxValue {
				newValue = maxValue
				l.logger.Warn("Adapted value above seed range, clamping",
					zap.String("dimension", dimension),
					zap.Float64("new_value", newValue),
					zap.Float64("max_allowed", maxValue))
			}
		}
	}

	// Record threshold change
	history := ThresholdHistory{
		Symbol:    symbol,
		Dimension: dimension,
		Value:     newValue,
		Timestamp: time.Now(),
		Source:    "learned",
	}

	key := symbol + "_" + dimension
	l.thresholdHistory[key] = append(l.thresholdHistory[key], history)

	// Update optimal threshold
	l.optimalThresholds[symbol][dimension] = newValue

	l.logger.Info("Threshold adapted",
		zap.String("symbol", symbol),
		zap.String("dimension", dimension),
		zap.Float64("old_value", currentValue),
		zap.Float64("new_value", newValue),
		zap.Float64("learning_rate", l.params.LearningRate))

	return newValue
}

// GetOptimalThresholds returns optimal thresholds for a symbol
func (l *LearningEngine) GetOptimalThresholds(symbol string) map[string]float64 {
	l.optimalMu.RLock()
	defer l.optimalMu.RUnlock()

	if thresholds, exists := l.optimalThresholds[symbol]; exists {
		// Return a copy
		result := make(map[string]float64)
		for k, v := range thresholds {
			result[k] = v
		}
		return result
	}

	return make(map[string]float64)
}

// UpdateOptimalThresholds updates optimal thresholds for a symbol
func (l *LearningEngine) UpdateOptimalThresholds(symbol string, thresholds map[string]float64) {
	l.optimalMu.Lock()
	defer l.optimalMu.Unlock()

	if l.optimalThresholds[symbol] == nil {
		l.optimalThresholds[symbol] = make(map[string]float64)
	}

	for dimension, value := range thresholds {
		l.optimalThresholds[symbol][dimension] = value

		// Record threshold update
		history := ThresholdHistory{
			Symbol:    symbol,
			Dimension: dimension,
			Value:     value,
			Timestamp: time.Now(),
			Source:    "manual",
		}

		key := symbol + "_" + dimension
		l.thresholdHistory[key] = append(l.thresholdHistory[key], history)
	}

	l.logger.Info("Optimal thresholds updated",
		zap.String("symbol", symbol),
		zap.Int("threshold_count", len(thresholds)))
}

// StartABTest starts an A/B test for a symbol
func (l *LearningEngine) StartABTest(symbol string) {
	if !l.params.ABTestEnabled {
		return
	}

	l.abTestMu.Lock()
	defer l.abTestMu.Unlock()

	l.abTestActive[symbol] = true
	l.logger.Info("A/B test started", zap.String("symbol", symbol))
}

// StopABTest stops an A/B test for a symbol
func (l *LearningEngine) StopABTest(symbol string) {
	l.abTestMu.Lock()
	defer l.abTestMu.Unlock()

	l.abTestActive[symbol] = false
	l.logger.Info("A/B test stopped", zap.String("symbol", symbol))
}

// IsABTestActive checks if A/B test is active for a symbol
func (l *LearningEngine) IsABTestActive(symbol string) bool {
	l.abTestMu.RLock()
	defer l.abTestMu.RUnlock()

	return l.abTestActive[symbol]
}

// SetLearningParameters sets the learning parameters
func (l *LearningEngine) SetLearningParameters(params LearningParameters) {
	l.params = params
	l.logger.Info("Learning parameters updated",
		zap.Float64("learning_rate", params.LearningRate),
		zap.Int("performance_window", params.PerformanceWindow),
		zap.Int("min_samples", params.MinSamples),
		zap.Bool("ab_test_enabled", params.ABTestEnabled),
		zap.Duration("ab_test_duration", params.ABTestDuration),
		zap.Float64("exploration_rate", params.ExplorationRate))
}

// GetLearningParameters returns the current learning parameters
func (l *LearningEngine) GetLearningParameters() LearningParameters {
	return l.params
}
