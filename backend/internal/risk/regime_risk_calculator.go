package risk

import (
	"fmt"
	"math"
)

// RegimeRiskCalculator calculates risk parameters based on market regime
type RegimeRiskCalculator struct{}

// NewRegimeRiskCalculator creates a new regime risk calculator
func NewRegimeRiskCalculator() *RegimeRiskCalculator {
	return &RegimeRiskCalculator{}
}

// CalculateRegimeRisk calculates risk metrics for specific regime
func (r *RegimeRiskCalculator) CalculateRegimeRisk(
	entryPrice float64,
	stopLossPrice float64,
	atr float64,
	regime string,
) (*RegimeRiskMetrics, error) {
	// Validate inputs
	if entryPrice <= 0 {
		return nil, fmt.Errorf("entry price must be positive")
	}
	if stopLossPrice <= 0 {
		return nil, fmt.Errorf("stop loss price must be positive")
	}
	if atr < 0 {
		return nil, fmt.Errorf("ATR cannot be negative")
	}
	
	// Calculate base risk
	baseRisk := math.Abs(entryPrice - stopLossPrice) / entryPrice
	
	// Calculate regime multiplier
	regimeMultiplier := r.getRegimeMultiplier(regime)
	
	// Calculate adjusted risk
	adjustedRisk := baseRisk * regimeMultiplier
	
	// Calculate position size based on regime
	positionSize := r.calculateRegimePositionSize(adjustedRisk, atr, regime)
	
	return &RegimeRiskMetrics{
		BaseRisk:        baseRisk,
		AdjustedRisk:    adjustedRisk,
		RegimeMultiplier: regimeMultiplier,
		PositionSize:    positionSize,
		ATRRatio:       atr / entryPrice,
	}, nil
}

// getRegimeMultiplier returns risk multiplier for regime
func (r *RegimeRiskCalculator) getRegimeMultiplier(regime string) float64 {
	switch regime {
	case "trending":
		return 0.8 // Reduce risk in trending markets
	case "ranging":
		return 1.0 // Normal risk in ranging markets
	case "volatile":
		return 1.2 // Increase risk tolerance in volatile markets
	default:
		return 1.0 // Default multiplier
	}
}

// calculateRegimePositionSize calculates position size for regime
func (r *RegimeRiskCalculator) calculateRegimePositionSize(
	risk float64,
	atr float64,
	regime string,
) float64 {
	if risk <= 0 {
		return 0
	}
	
	// Base position size calculation
	baseSize := 1.0 / risk
	
	// Apply regime adjustment
	regimeAdjustment := r.getRegimePositionAdjustment(regime)
	
	return baseSize * regimeAdjustment
}

// getRegimePositionAdjustment returns position size adjustment for regime
func (r *RegimeRiskCalculator) getRegimePositionAdjustment(regime string) float64 {
	switch regime {
	case "trending":
		return 0.5 // Conservative sizing in trends
	case "ranging":
		return 1.2 // Optimal sizing for volume
	case "volatile":
		return 0.7 // Balanced sizing in volatility
	default:
		return 1.0 // Default adjustment
	}
}

// RegimeRiskMetrics contains calculated risk metrics for regime
type RegimeRiskMetrics struct {
	BaseRisk         float64
	AdjustedRisk     float64
	RegimeMultiplier float64
	PositionSize     float64
	ATRRatio        float64
}
