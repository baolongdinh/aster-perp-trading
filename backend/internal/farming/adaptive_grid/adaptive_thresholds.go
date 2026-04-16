package adaptive_grid

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// AdaptiveThresholdManager manages adaptive thresholds that adjust based on
// symbol, regime, performance, time, and funding.
type AdaptiveThresholdManager struct {
	logger *zap.Logger
	config *AdaptiveThresholdConfig

	// Symbol-specific thresholds
	symbolThresholds map[string]*SymbolThresholds
	mu               sync.RWMutex

	// Performance history for learning
	performanceHistory map[string]*PerformanceHistory
}

// AdaptiveThresholdConfig holds configuration for adaptive thresholds
type AdaptiveThresholdConfig struct {
	// Base thresholds
	BasePositionThreshold float64 `yaml:"base_position_threshold"`
	BaseVolatilityThreshold float64 `yaml:"base_volatility_threshold"`
	BaseRiskThreshold float64 `yaml:"base_risk_threshold"`
	BaseTrendThreshold float64 `yaml:"base_trend_threshold"`

	// Adaptation parameters
	AdaptationRate float64 `yaml:"adaptation_rate"` // How quickly thresholds adapt (0-1)
	MinThreshold   float64 `yaml:"min_threshold"`    // Minimum threshold value
	MaxThreshold   float64 `yaml:"max_threshold"`    // Maximum threshold value

	// Learning parameters
	EnableLearning bool    `yaml:"enable_learning"`
	LearningRate   float64 `yaml:"learning_rate"`
}

// SymbolThresholds holds symbol-specific threshold adjustments
type SymbolThresholds struct {
	Symbol string
	// Multipliers for base thresholds (1.0 = no adjustment)
	PositionMultiplier float64
	VolatilityMultiplier float64
	RiskMultiplier float64
	TrendMultiplier float64

	// Last update timestamp
	LastUpdated time.Time
}

// PerformanceHistory tracks performance for threshold learning
type PerformanceHistory struct {
	Symbol string
	WinRate float64
	AvgWin float64
	AvgLoss float64
	TotalTrades int
	LastUpdated time.Time
}

// NewAdaptiveThresholdManager creates a new AdaptiveThresholdManager
func NewAdaptiveThresholdManager(logger *zap.Logger, config *AdaptiveThresholdConfig) *AdaptiveThresholdManager {
	return &AdaptiveThresholdManager{
		logger: logger,
		config: config,
		symbolThresholds: make(map[string]*SymbolThresholds),
		performanceHistory: make(map[string]*PerformanceHistory),
	}
}

// GetThreshold returns the adaptive threshold for a given symbol and dimension
func (atm *AdaptiveThresholdManager) GetThreshold(symbol, dimension string) float64 {
	atm.mu.RLock()
	defer atm.mu.RUnlock()

	// Get symbol-specific thresholds if available
	symbolThresholds, exists := atm.symbolThresholds[symbol]
	if !exists {
		symbolThresholds = &SymbolThresholds{
			Symbol: symbol,
			PositionMultiplier: 1.0,
			VolatilityMultiplier: 1.0,
			RiskMultiplier: 1.0,
			TrendMultiplier: 1.0,
			LastUpdated: time.Now(),
		}
		atm.symbolThresholds[symbol] = symbolThresholds
	}

	// Get base threshold for dimension
	var baseThreshold float64
	var multiplier float64

	switch dimension {
	case "position":
		baseThreshold = atm.config.BasePositionThreshold
		multiplier = symbolThresholds.PositionMultiplier
	case "volatility":
		baseThreshold = atm.config.BaseVolatilityThreshold
		multiplier = symbolThresholds.VolatilityMultiplier
	case "risk":
		baseThreshold = atm.config.BaseRiskThreshold
		multiplier = symbolThresholds.RiskMultiplier
	case "trend":
		baseThreshold = atm.config.BaseTrendThreshold
		multiplier = symbolThresholds.TrendMultiplier
	default:
		baseThreshold = 0.5 // Default
		multiplier = 1.0
	}

	// Apply multiplier
	threshold := baseThreshold * multiplier

	// Clamp to min/max
	if threshold < atm.config.MinThreshold {
		threshold = atm.config.MinThreshold
	}
	if threshold > atm.config.MaxThreshold {
		threshold = atm.config.MaxThreshold
	}

	return threshold
}

// CalculateSymbolThreshold calculates symbol-specific threshold adjustment
func (atm *AdaptiveThresholdManager) CalculateSymbolThreshold(symbol, dimension string, baseThreshold float64) float64 {
	atm.mu.RLock()
	symbolThresholds, exists := atm.symbolThresholds[symbol]
	atm.mu.RUnlock()

	if !exists {
		return baseThreshold
	}

	var multiplier float64
	switch dimension {
	case "position":
		multiplier = symbolThresholds.PositionMultiplier
	case "volatility":
		multiplier = symbolThresholds.VolatilityMultiplier
	case "risk":
		multiplier = symbolThresholds.RiskMultiplier
	case "trend":
		multiplier = symbolThresholds.TrendMultiplier
	default:
		multiplier = 1.0
	}

	threshold := baseThreshold * multiplier

	// Clamp
	if threshold < atm.config.MinThreshold {
		threshold = atm.config.MinThreshold
	}
	if threshold > atm.config.MaxThreshold {
		threshold = atm.config.MaxThreshold
	}

	return threshold
}

// CalculateRegimeThreshold calculates regime-based threshold adjustment
// This is a placeholder for future implementation
func (atm *AdaptiveThresholdManager) CalculateRegimeThreshold(regime, dimension string, baseThreshold float64) float64 {
	// For now, just return base threshold
	// In future, this would adjust based on regime (ranging, trending, volatile)
	return baseThreshold
}

// CalculatePerformanceThreshold calculates performance-based threshold adjustment
func (atm *AdaptiveThresholdManager) CalculatePerformanceThreshold(symbol, dimension string, baseThreshold float64) float64 {
	if !atm.config.EnableLearning {
		return baseThreshold
	}

	atm.mu.RLock()
	perf, exists := atm.performanceHistory[symbol]
	atm.mu.RUnlock()

	if !exists || perf.TotalTrades < 10 {
		return baseThreshold // Not enough data
	}

	// Adjust threshold based on win rate
	// Higher win rate = can use tighter thresholds
	// Lower win rate = need looser thresholds
	winRateMultiplier := 1.0 + (perf.WinRate - 0.5) * 0.5 // 0.5 -> 1.0, 1.0 -> 1.25

	threshold := baseThreshold * winRateMultiplier

	// Clamp
	if threshold < atm.config.MinThreshold {
		threshold = atm.config.MinThreshold
	}
	if threshold > atm.config.MaxThreshold {
		threshold = atm.config.MaxThreshold
	}

	return threshold
}

// CalculateTimeThreshold calculates time-based threshold adjustment
// This is a placeholder for future implementation
func (atm *AdaptiveThresholdManager) CalculateTimeThreshold(t time.Time, dimension string, baseThreshold float64) float64 {
	// For now, just return base threshold
	// In future, this could adjust based on time of day, session, etc.
	return baseThreshold
}

// CalculateFundingThreshold calculates funding-based threshold adjustment
// This is a placeholder for future implementation
func (atm *AdaptiveThresholdManager) CalculateFundingThreshold(fundingRate, dimension string, baseThreshold float64) float64 {
	// For now, just return base threshold
	// In future, this could adjust based on funding rate
	return baseThreshold
}

// UpdateThreshold updates a threshold based on recent performance
func (atm *AdaptiveThresholdManager) UpdateThreshold(symbol, dimension string, value, performance float64) {
	atm.mu.Lock()
	defer atm.mu.Unlock()

	// Get or create symbol thresholds
	symbolThresholds, exists := atm.symbolThresholds[symbol]
	if !exists {
		symbolThresholds = &SymbolThresholds{
			Symbol: symbol,
			PositionMultiplier: 1.0,
			VolatilityMultiplier: 1.0,
			RiskMultiplier: 1.0,
			TrendMultiplier: 1.0,
			LastUpdated: time.Now(),
		}
		atm.symbolThresholds[symbol] = symbolThresholds
	}

	// Adapt threshold based on performance
	// Good performance (positive) = tighten thresholds (lower multiplier)
	// Bad performance (negative) = loosen thresholds (higher multiplier)
	adjustment := 1.0 - (performance * atm.config.AdaptationRate)

	// Clamp adjustment
	if adjustment < 0.5 {
		adjustment = 0.5
	}
	if adjustment > 2.0 {
		adjustment = 2.0
	}

	// Apply to appropriate multiplier
	switch dimension {
	case "position":
		symbolThresholds.PositionMultiplier = adjustment
	case "volatility":
		symbolThresholds.VolatilityMultiplier = adjustment
	case "risk":
		symbolThresholds.RiskMultiplier = adjustment
	case "trend":
		symbolThresholds.TrendMultiplier = adjustment
	}

	symbolThresholds.LastUpdated = time.Now()

	atm.logger.Debug("Threshold updated",
		zap.String("symbol", symbol),
		zap.String("dimension", dimension),
		zap.Float64("value", value),
		zap.Float64("performance", performance),
		zap.Float64("adjustment", adjustment))
}

// AdaptThresholdsBasedOnPerformance adapts all thresholds based on recent performance
func (atm *AdaptiveThresholdManager) AdaptThresholdsBasedOnPerformance() {
	if !atm.config.EnableLearning {
		return
	}

	atm.mu.Lock()
	defer atm.mu.Unlock()

	for symbol, perf := range atm.performanceHistory {
		if perf.TotalTrades < 10 {
			continue // Not enough data
		}

		// Calculate performance score (-1 to 1)
		// Win rate above 0.5 is positive, below is negative
		performanceScore := (perf.WinRate - 0.5) * 2

		// Get or create symbol thresholds
		symbolThresholds, exists := atm.symbolThresholds[symbol]
		if !exists {
			symbolThresholds = &SymbolThresholds{
				Symbol: symbol,
				PositionMultiplier: 1.0,
				VolatilityMultiplier: 1.0,
				RiskMultiplier: 1.0,
				TrendMultiplier: 1.0,
				LastUpdated: time.Now(),
			}
			atm.symbolThresholds[symbol] = symbolThresholds
		}

		// Apply adaptation
		adjustment := 1.0 - (performanceScore * atm.config.LearningRate)

		// Clamp
		if adjustment < 0.5 {
			adjustment = 0.5
		}
		if adjustment > 2.0 {
			adjustment = 2.0
		}

		// Apply to all multipliers
		symbolThresholds.PositionMultiplier = adjustment
		symbolThresholds.VolatilityMultiplier = adjustment
		symbolThresholds.RiskMultiplier = adjustment
		symbolThresholds.TrendMultiplier = adjustment
		symbolThresholds.LastUpdated = time.Now()

		atm.logger.Debug("Thresholds adapted based on performance",
			zap.String("symbol", symbol),
			zap.Float64("win_rate", perf.WinRate),
			zap.Float64("performance_score", performanceScore),
			zap.Float64("adjustment", adjustment))
	}
}

// RecordPerformance records performance data for a symbol
func (atm *AdaptiveThresholdManager) RecordPerformance(symbol string, winRate, avgWin, avgLoss float64, totalTrades int) {
	atm.mu.Lock()
	defer atm.mu.Unlock()

	atm.performanceHistory[symbol] = &PerformanceHistory{
		Symbol: symbol,
		WinRate: winRate,
		AvgWin: avgWin,
		AvgLoss: avgLoss,
		TotalTrades: totalTrades,
		LastUpdated: time.Now(),
	}
}

// GetOptimalThresholds returns the optimal thresholds for a symbol
func (atm *AdaptiveThresholdManager) GetOptimalThresholds(symbol string) map[string]float64 {
	return map[string]float64{
		"position": atm.GetThreshold(symbol, "position"),
		"volatility": atm.GetThreshold(symbol, "volatility"),
		"risk": atm.GetThreshold(symbol, "risk"),
		"trend": atm.GetThreshold(symbol, "trend"),
	}
}

// UpdateOptimalThresholds updates optimal thresholds for a symbol
func (atm *AdaptiveThresholdManager) UpdateOptimalThresholds(symbol string, thresholds map[string]float64) {
	atm.mu.Lock()
	defer atm.mu.Unlock()

	// Get or create symbol thresholds
	symbolThresholds, exists := atm.symbolThresholds[symbol]
	if !exists {
		symbolThresholds = &SymbolThresholds{
			Symbol: symbol,
			PositionMultiplier: 1.0,
			VolatilityMultiplier: 1.0,
			RiskMultiplier: 1.0,
			TrendMultiplier: 1.0,
			LastUpdated: time.Now(),
		}
		atm.symbolThresholds[symbol] = symbolThresholds
	}

	// Update multipliers based on provided thresholds
	// Calculate multiplier as threshold / base_threshold
	if position, ok := thresholds["position"]; ok && atm.config.BasePositionThreshold > 0 {
		symbolThresholds.PositionMultiplier = position / atm.config.BasePositionThreshold
	}
	if volatility, ok := thresholds["volatility"]; ok && atm.config.BaseVolatilityThreshold > 0 {
		symbolThresholds.VolatilityMultiplier = volatility / atm.config.BaseVolatilityThreshold
	}
	if risk, ok := thresholds["risk"]; ok && atm.config.BaseRiskThreshold > 0 {
		symbolThresholds.RiskMultiplier = risk / atm.config.BaseRiskThreshold
	}
	if trend, ok := thresholds["trend"]; ok && atm.config.BaseTrendThreshold > 0 {
		symbolThresholds.TrendMultiplier = trend / atm.config.BaseTrendThreshold
	}

	symbolThresholds.LastUpdated = time.Now()

	atm.logger.Debug("Optimal thresholds updated",
		zap.String("symbol", symbol),
		zap.Any("thresholds", thresholds))
}
