package risk

import (
	"fmt"
	"math"
)

// AdaptiveSizingManager manages position sizing based on market regime
type AdaptiveSizingManager struct {
	baseSizing     *SizingManager
	regimeCalculator *RegimeRiskCalculator
}

// NewAdaptiveSizingManager creates a new adaptive sizing manager
func NewAdaptiveSizingManager(
	baseSizing *SizingManager,
	regimeCalculator *RegimeRiskCalculator,
) *AdaptiveSizingManager {
	return &AdaptiveSizingManager{
		baseSizing:      baseSizing,
		regimeCalculator: regimeCalculator,
	}
}

// CalculateAdaptivePositionSize calculates position size for specific regime
func (a *AdaptiveSizingManager) CalculateAdaptivePositionSize(
	symbol string,
	entryPrice float64,
	stopLossPrice float64,
	atr float64,
	accountBalance float64,
	regime string,
	maxPositionUSDT float64,
) (float64, error) {
	// Calculate regime-specific risk metrics
	riskMetrics, err := a.regimeCalculator.CalculateRegimeRisk(
		entryPrice,
		stopLossPrice,
		atr,
		regime,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate regime risk: %w", err)
	}
	
	// Calculate base position size from account balance
	baseSize := accountBalance * riskMetrics.AdjustedRisk
	
	// Apply regime adjustment
	regimeSize := baseSize * riskMetrics.PositionSize
	
	// Apply maximum position limit
	if regimeSize > maxPositionUSDT {
		regimeSize = maxPositionUSDT
	}
	
	// Calculate actual position size in units
	positionUnits := regimeSize / entryPrice
	
	// Round to appropriate precision
	positionUnits = math.Round(positionUnits*1000000) / 1000000
	
	return positionUnits, nil
}

// CalculateRegimeAdjustedSize calculates size adjusted for regime volatility
func (a *AdaptiveSizingManager) CalculateRegimeAdjustedSize(
	baseSize float64,
	atr float64,
	regime string,
) float64 {
	if baseSize <= 0 {
		return 0
	}
	
	// Calculate volatility adjustment
	volatilityAdjustment := a.getVolatilityAdjustment(atr, regime)
	
	// Apply adjustment
	adjustedSize := baseSize * volatilityAdjustment
	
	return adjustedSize
}

// getVolatilityAdjustment returns volatility adjustment for regime
func (a *AdaptiveSizingManager) getVolatilityAdjustment(atr float64, regime string) float64 {
	baseAdjustment := 1.0
	
	// ATR-based adjustment
	atrAdjustment := 1.0
	if atr > 0 {
		// Normalize ATR (assuming price around 1.0)
		atrRatio := atr
		if atrRatio > 0.05 {
			atrAdjustment = 0.7 // High volatility
		} else if atrRatio > 0.02 {
			atrAdjustment = 0.85 // Medium volatility
		} else {
			atrAdjustment = 1.0 // Low volatility
		}
	}
	
	// Regime-based adjustment
	regimeAdjustment := 1.0
	switch regime {
	case "trending":
		regimeAdjustment = 0.8 // Conservative in trends
	case "ranging":
		regimeAdjustment = 1.2 // Optimal in ranging
	case "volatile":
		regimeAdjustment = 0.7 // Conservative in volatility
	}
	
	// Combined adjustment
	return baseAdjustment * atrAdjustment * regimeAdjustment
}

// ValidatePositionSize validates if calculated position size is acceptable
func (a *AdaptiveSizingManager) ValidatePositionSize(
	size float64,
	entryPrice float64,
	accountBalance float64,
	maxRiskPercent float64,
) error {
	if size <= 0 {
		return fmt.Errorf("position size must be positive")
	}
	
	// Calculate notional value
	notional := size * entryPrice
	
	// Check account balance constraint
	maxNotional := accountBalance * maxRiskPercent / 100.0
	if notional > maxNotional {
		return fmt.Errorf("position size %.6f exceeds risk limit (notional: %.2f, limit: %.2f)",
			size, notional, maxNotional)
	}
	
	return nil
}

// SizingManager is a placeholder for the base sizing manager type
type SizingManager struct{}
