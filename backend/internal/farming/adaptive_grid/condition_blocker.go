package adaptive_grid

import (
	"math"

	"go.uber.org/zap"
)

// ConditionBlocker calculates blocking factor based on multi-dimensional conditions
// instead of binary state blocking. Returns a value between 0 (full block) and 1 (no block).
type ConditionBlocker struct {
	logger *zap.Logger
	config *ConditionBlockerConfig
}

// ConditionBlockerConfig holds configuration for condition-based blocking
type ConditionBlockerConfig struct {
	// Score weights for calculating blocking factor
	PositionSizeWeight float64 `yaml:"position_size_weight"` // Weight for position size score (0-1)
	VolatilityWeight    float64 `yaml:"volatility_weight"`    // Weight for volatility score (0-1)
	RiskWeight          float64 `yaml:"risk_weight"`          // Weight for risk score (0-1)
	TrendWeight         float64 `yaml:"trend_weight"`         // Weight for trend score (0-1)
	SkewWeight          float64 `yaml:"skew_weight"`          // Weight for skew score (0-1)

	// Blocking thresholds
	BlockingThreshold float64 `yaml:"blocking_threshold"` // Threshold to trigger blocking (0-1)
	MicroModeMin      float64 `yaml:"micro_mode_min"`      // Minimum blocking factor for MICRO mode (0-1)
}

// NewConditionBlocker creates a new ConditionBlocker with default configuration
func NewConditionBlocker(logger *zap.Logger) *ConditionBlocker {
	return &ConditionBlocker{
		logger: logger,
		config: &ConditionBlockerConfig{
			PositionSizeWeight: 0.3,  // 30% weight for position size
			VolatilityWeight:    0.2,  // 20% weight for volatility
			RiskWeight:          0.25, // 25% weight for risk
			TrendWeight:         0.1,  // 10% weight for trend
			SkewWeight:          0.15, // 15% weight for skew
			BlockingThreshold:   0.7,  // Block if combined score > 0.7
			MicroModeMin:        0.1,  // MICRO mode if blocking factor > 0.1
		},
	}
}

// SetConfig sets the configuration for the ConditionBlocker
func (cb *ConditionBlocker) SetConfig(config *ConditionBlockerConfig) {
	cb.config = config
}

// GetConfig returns the current configuration
func (cb *ConditionBlocker) GetConfig() *ConditionBlockerConfig {
	return cb.config
}

// NormalizePositionSize normalizes position size to 0-1 range
func (cb *ConditionBlocker) NormalizePositionSize(positionNotional, maxPosition float64) float64 {
	if maxPosition <= 0 {
		return 0
	}
	ratio := positionNotional / maxPosition
	if ratio > 1 {
		return 1
	}
	return ratio
}

// NormalizeVolatility normalizes volatility score to 0-1 range
// Combines ATR and BB width (60% ATR + 40% BB width)
func (cb *ConditionBlocker) NormalizeVolatility(atrPct, bbWidthPct float64) float64 {
	// Normalize ATR: 0% -> 0, 2% -> 1
	atrScore := atrPct / 0.02
	if atrScore > 1 {
		atrScore = 1
	}

	// Normalize BB width: 0% -> 0, 5% -> 1
	bbScore := bbWidthPct / 0.05
	if bbScore > 1 {
		bbScore = 1
	}

	// Weighted average: 60% ATR + 40% BB width
	return (atrScore * 0.6) + (bbScore * 0.4)
}

// NormalizeRisk normalizes risk score to 0-1 range based on PnL and drawdown
func (cb *ConditionBlocker) NormalizeRisk(pnl, drawdown float64) float64 {
	// Map PnL to risk: -$10 -> 1.0, $0 -> 0.5, $10 -> 0.0
	pnlScore := 0.5 - (pnl / 20.0)
	if pnlScore < 0 {
		pnlScore = 0
	}
	if pnlScore > 1 {
		pnlScore = 1
	}

	// Map drawdown to risk: 0% -> 0, 20% -> 1
	drawdownScore := drawdown / 0.2
	if drawdownScore > 1 {
		drawdownScore = 1
	}

	// Weighted average: 70% PnL + 30% drawdown
	return (pnlScore * 0.7) + (drawdownScore * 0.3)
}

// NormalizeTrend normalizes trend strength to 0-1 range based on ADX
func (cb *ConditionBlocker) NormalizeTrend(adx float64) float64 {
	// Normalize ADX: 0 -> 0, 60 -> 1
	score := adx / 60.0
	if score > 1 {
		score = 1
	}
	return score
}

// NormalizeSkew normalizes inventory skew to 0-1 range
// Skew ranges from -1 (short bias) to 1 (long bias)
// Returns absolute skew magnitude (0 = balanced, 1 = extreme imbalance)
func (cb *ConditionBlocker) NormalizeSkew(skew float64) float64 {
	// Return absolute value of skew
	absSkew := math.Abs(skew)
	if absSkew > 1 {
		absSkew = 1
	}
	return absSkew
}

// CalculateBlockingFactor calculates the blocking factor based on 5 input scores
// Returns value between 0 (full block) and 1 (no block)
func (cb *ConditionBlocker) CalculateBlockingFactor(
	positionSizeScore float64,
	volatilityScore float64,
	riskScore float64,
	trendScore float64,
	skewScore float64,
) float64 {
	// Calculate weighted sum of all scores
	weightedSum := (positionSizeScore * cb.config.PositionSizeWeight) +
		(volatilityScore * cb.config.VolatilityWeight) +
		(riskScore * cb.config.RiskWeight) +
		(trendScore * cb.config.TrendWeight) +
		(skewScore * cb.config.SkewWeight)

	// Invert: higher score = more blocking = lower blocking factor
	// blockingFactor = 1 - weightedSum
	blockingFactor := 1 - weightedSum

	// Clamp to [0, 1] range
	if blockingFactor < 0 {
		blockingFactor = 0
	}
	if blockingFactor > 1 {
		blockingFactor = 1
	}

	cb.logger.Debug("Blocking factor calculated",
		zap.Float64("position_size_score", positionSizeScore),
		zap.Float64("volatility_score", volatilityScore),
		zap.Float64("risk_score", riskScore),
		zap.Float64("trend_score", trendScore),
		zap.Float64("skew_score", skewScore),
		zap.Float64("weighted_sum", weightedSum),
		zap.Float64("blocking_factor", blockingFactor),
		zap.Float64("blocking_threshold", cb.config.BlockingThreshold))

	return blockingFactor
}

// ShouldBlock returns true if conditions warrant blocking
func (cb *ConditionBlocker) ShouldBlock(blockingFactor float64) bool {
	return blockingFactor < cb.config.BlockingThreshold
}

// IsMicroMode returns true if MICRO mode should be used (partial trading allowed)
func (cb *ConditionBlocker) IsMicroMode(blockingFactor float64) bool {
	return blockingFactor > cb.config.MicroModeMin && blockingFactor < cb.config.BlockingThreshold
}

// GetSizeMultiplier returns the size multiplier based on blocking factor
// MICRO mode: 0.1x, FULL mode: blockingFactor
func (cb *ConditionBlocker) GetSizeMultiplier(blockingFactor float64) float64 {
	if cb.IsMicroMode(blockingFactor) {
		return 0.1 // MICRO mode: 10% size
	}
	if cb.ShouldBlock(blockingFactor) {
		return 0 // Full block
	}
	return blockingFactor // Use blocking factor directly
}
