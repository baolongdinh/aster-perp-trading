package adaptive_grid

import (
	"math"
	"time"

	"go.uber.org/zap"
)

// AdaptiveGridGeometry calculates adaptive grid parameters based on market conditions
type AdaptiveGridGeometry struct {
	logger *zap.Logger

	// Volatility band thresholds
	lowATRThreshold    float64 // ATR < 0.3%
	normalATRThreshold float64 // ATR 0.3-0.8%
	highATRThreshold   float64 // ATR 0.8-1.5%

	// Depth thresholds
	shallowDepthThreshold float64 // Depth < 50% of range
	deepDepthThreshold    float64 // Depth > 80% of range

	// Skew sensitivity
	skewSensitivity float64 // How much asymmetry to apply based on skew (0-1)

	// Base parameters
	baseSpread     float64 // Base spread percentage
	baseOrderCount int     // Base number of orders per side
	baseSpacing    float64 // Base spacing percentage
}

// NewAdaptiveGridGeometry creates a new adaptive grid geometry calculator
func NewAdaptiveGridGeometry(logger *zap.Logger) *AdaptiveGridGeometry {
	return &AdaptiveGridGeometry{
		logger:                logger,
		lowATRThreshold:       0.003,  // 0.3%
		normalATRThreshold:    0.008,  // 0.8%
		highATRThreshold:      0.015,  // 1.5%
		shallowDepthThreshold: 0.5,    // 50%
		deepDepthThreshold:    0.8,    // 80%
		skewSensitivity:       0.5,    // Medium sensitivity
		baseSpread:            0.0015, // 0.15%
		baseOrderCount:        10,
		baseSpacing:           0.002, // 0.2%
	}
}

// ClassifyVolatility classifies volatility into bands based on ATR
func (g *AdaptiveGridGeometry) ClassifyVolatility(atr float64) VolatilityLevel {
	if atr < g.lowATRThreshold {
		return VolatilityLow
	} else if atr < g.normalATRThreshold {
		return VolatilityNormal
	} else if atr < g.highATRThreshold {
		return VolatilityHigh
	}
	return VolatilityExtreme
}

// CalculateSpread calculates adaptive spread based on volatility, skew, funding, and time
func (g *AdaptiveGridGeometry) CalculateSpread(volatility, skew, funding float64, currentTime time.Time) float64 {
	// Classify volatility
	band := g.ClassifyVolatility(volatility)

	// Base spread based on volatility band
	spread := g.baseSpread
	switch band {
	case VolatilityLow:
		spread *= 0.5 // Tighter spread in low volatility
	case VolatilityNormal:
		spread *= 1.0 // Normal spread
	case VolatilityHigh:
		spread *= 1.5 // Wider spread in high volatility
	case VolatilityExtreme:
		spread *= 2.0 // Widest spread in extreme volatility
	}

	// Adjust for skew (asymmetry)
	if skew > 0 {
		// Positive skew (buy pressure) -> widen buy spread, tighten sell spread
		spread *= (1.0 + skew*g.skewSensitivity*0.2)
	} else if skew < 0 {
		// Negative skew (sell pressure) -> widen sell spread, tighten buy spread
		spread *= (1.0 + math.Abs(skew)*g.skewSensitivity*0.2)
	}

	// Adjust for funding rate
	if math.Abs(funding) > 0.0001 { // 0.01% funding rate
		spread *= (1.0 + math.Abs(funding)*10.0)
	}

	// Time-based adjustment (wider spread during volatile hours)
	hour := currentTime.Hour()
	if hour >= 8 && hour <= 16 {
		// Trading hours - normal spread
	} else {
		// Off-hours - slightly wider spread
		spread *= 1.1
	}

	g.logger.Debug("Calculated adaptive spread",
		zap.Float64("volatility", volatility),
		zap.String("volatility_band", band.String()),
		zap.Float64("skew", skew),
		zap.Float64("funding", funding),
		zap.Float64("spread", spread))

	return spread
}

// CalculateOrderCount calculates adaptive order count based on depth, risk, and regime
func (g *AdaptiveGridGeometry) CalculateOrderCount(depth, risk float64, regime string) int {
	// Base order count
	orderCount := g.baseOrderCount

	// Adjust for depth
	if depth < g.shallowDepthThreshold {
		// Shallow depth - fewer orders
		orderCount = int(float64(orderCount) * 0.5)
	} else if depth > g.deepDepthThreshold {
		// Deep depth - more orders
		orderCount = int(float64(orderCount) * 1.5)
	}

	// Adjust for risk
	if risk > 0.8 {
		// High risk - fewer orders
		orderCount = int(float64(orderCount) * 0.7)
	} else if risk < 0.3 {
		// Low risk - more orders
		orderCount = int(float64(orderCount) * 1.2)
	}

	// Adjust for regime
	switch regime {
	case "RANGING":
		// Ranging - more orders for grid
		orderCount = int(float64(orderCount) * 1.2)
	case "TRENDING":
		// Trending - fewer orders, focus on trend direction
		orderCount = int(float64(orderCount) * 0.8)
	case "VOLATILE":
		// Volatile - fewer orders to reduce exposure
		orderCount = int(float64(orderCount) * 0.6)
	}

	// Clamp to reasonable bounds
	if orderCount < 2 {
		orderCount = 2
	}
	if orderCount > 50 {
		orderCount = 50
	}

	g.logger.Debug("Calculated adaptive order count",
		zap.Float64("depth", depth),
		zap.Float64("risk", risk),
		zap.String("regime", regime),
		zap.Int("order_count", orderCount))

	return orderCount
}

// CalculateSpacing calculates adaptive spacing based on volatility and trend
func (g *AdaptiveGridGeometry) CalculateSpacing(volatility, trend float64) float64 {
	// Base spacing
	spacing := g.baseSpacing

	// Adjust for volatility
	band := g.ClassifyVolatility(volatility)
	switch band {
	case VolatilityLow:
		spacing *= 0.5 // Tighter spacing in low volatility
	case VolatilityNormal:
		spacing *= 1.0 // Normal spacing
	case VolatilityHigh:
		spacing *= 1.5 // Wider spacing in high volatility
	case VolatilityExtreme:
		spacing *= 2.0 // Widest spacing in extreme volatility
	}

	// Adjust for trend strength
	if math.Abs(trend) > 0.7 {
		// Strong trend - wider spacing
		spacing *= 1.3
	} else if math.Abs(trend) < 0.3 {
		// Weak trend - tighter spacing
		spacing *= 0.8
	}

	g.logger.Debug("Calculated adaptive spacing",
		zap.Float64("volatility", volatility),
		zap.String("volatility_band", band.String()),
		zap.Float64("trend", trend),
		zap.Float64("spacing", spacing))

	return spacing
}

// CalculateAsymmetry calculates asymmetric parameters based on skew
// Returns (buySpread, sellSpread, buyCount, sellCount)
func (g *AdaptiveGridGeometry) CalculateAsymmetry(skew float64, baseSpread float64, baseOrderCount int) (float64, float64, int, int) {
	// No asymmetry if skew is neutral
	if math.Abs(skew) < 0.1 {
		return baseSpread, baseSpread, baseOrderCount, baseOrderCount
	}

	// Calculate asymmetry factor
	asymmetryFactor := math.Abs(skew) * g.skewSensitivity

	if skew > 0 {
		// Positive skew (buy pressure) -> widen buy spread, tighten sell spread, more sell orders
		buySpread := baseSpread * (1.0 + asymmetryFactor*0.3)
		sellSpread := baseSpread * (1.0 - asymmetryFactor*0.2)
		buyCount := int(float64(baseOrderCount) * (1.0 - asymmetryFactor*0.2))
		sellCount := int(float64(baseOrderCount) * (1.0 + asymmetryFactor*0.3))

		// Clamp to reasonable bounds
		if buyCount < 1 {
			buyCount = 1
		}
		if sellCount < 1 {
			sellCount = 1
		}

		g.logger.Debug("Calculated asymmetry (positive skew)",
			zap.Float64("skew", skew),
			zap.Float64("buy_spread", buySpread),
			zap.Float64("sell_spread", sellSpread),
			zap.Int("buy_count", buyCount),
			zap.Int("sell_count", sellCount))

		return buySpread, sellSpread, buyCount, sellCount
	} else {
		// Negative skew (sell pressure) -> widen sell spread, tighten buy spread, more buy orders
		buySpread := baseSpread * (1.0 - asymmetryFactor*0.2)
		sellSpread := baseSpread * (1.0 + asymmetryFactor*0.3)
		buyCount := int(float64(baseOrderCount) * (1.0 + asymmetryFactor*0.3))
		sellCount := int(float64(baseOrderCount) * (1.0 - asymmetryFactor*0.2))

		// Clamp to reasonable bounds
		if buyCount < 1 {
			buyCount = 1
		}
		if sellCount < 1 {
			sellCount = 1
		}

		g.logger.Debug("Calculated asymmetry (negative skew)",
			zap.Float64("skew", skew),
			zap.Float64("buy_spread", buySpread),
			zap.Float64("sell_spread", sellSpread),
			zap.Int("buy_count", buyCount),
			zap.Int("sell_count", sellCount))

		return buySpread, sellSpread, buyCount, sellCount
	}
}

// CalculateFullGeometry calculates all geometry parameters at once
func (g *AdaptiveGridGeometry) CalculateFullGeometry(
	atr, skew, funding, depth, risk, trend float64,
	regime string,
	currentTime time.Time,
) (spread, spacing float64, orderCount int, buySpread, sellSpread float64, buyCount, sellCount int) {
	// Calculate spread
	spread = g.CalculateSpread(atr, skew, funding, currentTime)

	// Calculate spacing
	spacing = g.CalculateSpacing(atr, trend)

	// Calculate order count
	orderCount = g.CalculateOrderCount(depth, risk, regime)

	// Calculate asymmetry
	buySpread, sellSpread, buyCount, sellCount = g.CalculateAsymmetry(skew, spread, orderCount)

	g.logger.Info("Calculated full adaptive geometry",
		zap.Float64("atr", atr),
		zap.Float64("spread", spread),
		zap.Float64("spacing", spacing),
		zap.Int("order_count", orderCount),
		zap.Float64("buy_spread", buySpread),
		zap.Float64("sell_spread", sellSpread),
		zap.Int("buy_count", buyCount),
		zap.Int("sell_count", sellCount))

	return spread, spacing, orderCount, buySpread, sellSpread, buyCount, sellCount
}
