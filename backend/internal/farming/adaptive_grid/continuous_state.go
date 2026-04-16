package adaptive_grid

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// ContinuousState represents a continuous multi-dimensional state
// instead of discrete states. This enables finer-grained decision making.
type ContinuousState struct {
	mu sync.RWMutex // CRITICAL: Protects all fields from race conditions

	// 5 dimensions normalized to appropriate ranges
	PositionSize float64 // 0-1: Position notional vs max
	Volatility   float64 // 0-1: Combined ATR and BB width
	Risk         float64 // 0-1: PnL and drawdown based
	Trend        float64 // 0-1: ADX based trend strength
	Skew         float64 // -1 to 1: Inventory skew (negative = short bias, positive = long bias)

	// Smoothing parameters
	smoothedPositionSize float64
	smoothedVolatility   float64
	smoothedRisk         float64
	smoothedTrend        float64
	smoothedSkew         float64

	// Timestamp for tracking state changes
	Timestamp time.Time
}

// ContinuousStateConfig holds configuration for continuous state tracking
type ContinuousStateConfig struct {
	SmoothingAlpha float64 `yaml:"smoothing_alpha"` // EMA smoothing factor (0-1, lower = more smoothing)
}

// NewContinuousState creates a new ContinuousState with default configuration
func NewContinuousState() *ContinuousState {
	return &ContinuousState{
		smoothedPositionSize: 0,
		smoothedVolatility:   0,
		smoothedRisk:         0.5,
		smoothedTrend:        0.5,
		smoothedSkew:         0,
		Timestamp:            time.Now(),
	}
}

// CalculatePositionSize calculates position size dimension (0-1)
func (cs *ContinuousState) CalculatePositionSize(positionNotional, maxPosition float64) float64 {
	if maxPosition <= 0 {
		return 0
	}
	ratio := positionNotional / maxPosition
	if ratio > 1 {
		return 1
	}
	return ratio
}

// CalculateVolatility calculates volatility dimension (0-1)
// Combines ATR (60%) and BB width (40%)
func (cs *ContinuousState) CalculateVolatility(atrPct, bbWidthPct float64) float64 {
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

// CalculateRisk calculates risk dimension (0-1) based on PnL and drawdown
func (cs *ContinuousState) CalculateRisk(pnl, drawdown float64) float64 {
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

// CalculateTrend calculates trend strength dimension (0-1) based on ADX
func (cs *ContinuousState) CalculateTrend(adx float64) float64 {
	// Normalize ADX: 0 -> 0, 60 -> 1
	score := adx / 60.0
	if score > 1 {
		score = 1
	}
	return score
}

// CalculateSkew calculates inventory skew dimension (-1 to 1)
// Skew ranges from -1 (short bias) to 1 (long bias)
func (cs *ContinuousState) CalculateSkew(inventory float64) float64 {
	// Normalize inventory to -1 to 1 range
	// Assuming max inventory is 10 (adjust based on your setup)
	maxInventory := 10.0
	skew := inventory / maxInventory
	if skew > 1 {
		skew = 1
	}
	if skew < -1 {
		skew = -1
	}
	return skew
}

// UpdateContinuousState updates the continuous state with new values and applies smoothing
func (cs *ContinuousState) UpdateContinuousState(
	positionNotional, maxPosition float64,
	atrPct, bbWidthPct float64,
	pnl, drawdown float64,
	adx float64,
	inventory float64,
	config *ContinuousStateConfig,
	logger *zap.Logger,
) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Calculate raw values
	cs.PositionSize = cs.CalculatePositionSize(positionNotional, maxPosition)
	cs.Volatility = cs.CalculateVolatility(atrPct, bbWidthPct)
	cs.Risk = cs.CalculateRisk(pnl, drawdown)
	cs.Trend = cs.CalculateTrend(adx)
	cs.Skew = cs.CalculateSkew(inventory)

	// Apply EMA smoothing
	alpha := config.SmoothingAlpha
	if alpha <= 0 || alpha > 1 {
		alpha = 0.3 // Default smoothing
	}

	cs.smoothedPositionSize = applyEMA(cs.smoothedPositionSize, cs.PositionSize, alpha)
	cs.smoothedVolatility = applyEMA(cs.smoothedVolatility, cs.Volatility, alpha)
	cs.smoothedRisk = applyEMA(cs.smoothedRisk, cs.Risk, alpha)
	cs.smoothedTrend = applyEMA(cs.smoothedTrend, cs.Trend, alpha)
	cs.smoothedSkew = applyEMA(cs.smoothedSkew, cs.Skew, alpha)

	cs.Timestamp = time.Now()

	logger.Debug("Continuous state updated",
		zap.Float64("position_size", cs.PositionSize),
		zap.Float64("volatility", cs.Volatility),
		zap.Float64("risk", cs.Risk),
		zap.Float64("trend", cs.Trend),
		zap.Float64("skew", cs.Skew),
		zap.Float64("smoothed_position_size", cs.smoothedPositionSize),
		zap.Float64("smoothed_volatility", cs.smoothedVolatility),
		zap.Float64("smoothed_risk", cs.smoothedRisk),
		zap.Float64("smoothed_trend", cs.smoothedTrend),
		zap.Float64("smoothed_skew", cs.smoothedSkew))
}

// applyEMA applies exponential moving average smoothing
func applyEMA(previous, current, alpha float64) float64 {
	if previous == 0 {
		return current
	}
	return (alpha * current) + ((1 - alpha) * previous)
}

// GetSmoothedState returns the smoothed state values
func (cs *ContinuousState) GetSmoothedState() (positionSize, volatility, risk, trend, skew float64) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.smoothedPositionSize, cs.smoothedVolatility, cs.smoothedRisk, cs.smoothedTrend, cs.smoothedSkew
}

// GetRawState returns the raw (unsmoothed) state values
func (cs *ContinuousState) GetRawState() (positionSize, volatility, risk, trend, skew float64) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.PositionSize, cs.Volatility, cs.Risk, cs.Trend, cs.Skew
}

// GetStateVector returns a 5-dimensional state vector for ML/optimization
func (cs *ContinuousState) GetStateVector(smoothed bool) []float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if smoothed {
		return []float64{
			cs.smoothedPositionSize,
			cs.smoothedVolatility,
			cs.smoothedRisk,
			cs.smoothedTrend,
			cs.smoothedSkew,
		}
	}
	return []float64{
		cs.PositionSize,
		cs.Volatility,
		cs.Risk,
		cs.Trend,
		cs.Skew,
	}
}
