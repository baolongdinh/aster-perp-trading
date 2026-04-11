package agentic

import (
	"aster-bot/internal/config"
)

// PositionSizer calculates dynamic position sizes based on scores and regime
type PositionSizer struct {
	config config.PositionSizingConfig
}

// NewPositionSizer creates a new position sizer
func NewPositionSizer(cfg config.PositionSizingConfig) *PositionSizer {
	return &PositionSizer{config: cfg}
}

// CalculateSizeMultiplier calculates the size multiplier based on score and regime
func (ps *PositionSizer) CalculateSizeMultiplier(score SymbolScore) float64 {
	// Get score-based multiplier
	scoreMult := ps.getScoreMultiplier(score.Recommendation)

	// Get regime-based multiplier
	regimeMult := ps.getRegimeMultiplier(score.Regime)

	// Combined multiplier
	return scoreMult * regimeMult
}

// getScoreMultiplier returns the multiplier based on recommendation
func (ps *PositionSizer) getScoreMultiplier(rec Recommendation) float64 {
	if ps.config.ScoreMultipliers == nil {
		// Default multipliers
		switch rec {
		case RecHigh:
			return 1.0
		case RecMedium:
			return 0.6
		case RecLow:
			return 0.3
		case RecSkip:
			return 0.0
		default:
			return 0.5
		}
	}

	// Use config multipliers
	switch rec {
	case RecHigh:
		if v, ok := ps.config.ScoreMultipliers["HIGH"]; ok {
			return v
		}
		return 1.0
	case RecMedium:
		if v, ok := ps.config.ScoreMultipliers["MEDIUM"]; ok {
			return v
		}
		return 0.6
	case RecLow:
		if v, ok := ps.config.ScoreMultipliers["LOW"]; ok {
			return v
		}
		return 0.3
	case RecSkip:
		if v, ok := ps.config.ScoreMultipliers["SKIP"]; ok {
			return v
		}
		return 0.0
	default:
		return 0.5
	}
}

// getRegimeMultiplier returns the multiplier based on regime
func (ps *PositionSizer) getRegimeMultiplier(regime RegimeType) float64 {
	if ps.config.RegimeMultipliers == nil {
		// Default multipliers
		switch regime {
		case RegimeSideways:
			return 1.0
		case RegimeTrending:
			return 0.7
		case RegimeVolatile:
			return 0.5
		case RegimeRecovery:
			return 0.8
		default:
			return 1.0
		}
	}

	// Use config multipliers
	switch regime {
	case RegimeSideways:
		if v, ok := ps.config.RegimeMultipliers["SIDEWAYS"]; ok {
			return v
		}
		return 1.0
	case RegimeTrending:
		if v, ok := ps.config.RegimeMultipliers["TRENDING"]; ok {
			return v
		}
		return 0.7
	case RegimeVolatile:
		if v, ok := ps.config.RegimeMultipliers["VOLATILE"]; ok {
			return v
		}
		return 0.5
	case RegimeRecovery:
		if v, ok := ps.config.RegimeMultipliers["RECOVERY"]; ok {
			return v
		}
		return 0.8
	default:
		return 1.0
	}
}

// ApplyToOrderSize applies the multiplier to a base order size
func (ps *PositionSizer) ApplyToOrderSize(baseSize float64, score SymbolScore) float64 {
	multiplier := ps.CalculateSizeMultiplier(score)
	return baseSize * multiplier
}

// GetSizingInfo returns detailed sizing information for logging
func (ps *PositionSizer) GetSizingInfo(score SymbolScore) map[string]float64 {
	return map[string]float64{
		"score_multiplier":  ps.getScoreMultiplier(score.Recommendation),
		"regime_multiplier": ps.getRegimeMultiplier(score.Regime),
		"total_multiplier":  ps.CalculateSizeMultiplier(score),
	}
}

// ShouldTrade determines if trading should be enabled based on score
func (ps *PositionSizer) ShouldTrade(score SymbolScore) bool {
	// Only trade HIGH and MEDIUM recommendations
	return score.Recommendation == RecHigh || score.Recommendation == RecMedium
}

// GetRecommendedGridSpread returns the recommended grid spread adjustment
func (ps *PositionSizer) GetRecommendedGridSpread(baseSpread float64, score SymbolScore) float64 {
	// Adjust grid spread based on volatility (ATR)
	atr := score.RawATR14

	// ATR-based spread adjustment
	// Low ATR (<0.3%): tighter spread for more fills
	// High ATR (>1.0%): wider spread for safety
	switch {
	case atr < 0.003:
		return baseSpread * 0.8
	case atr > 0.01:
		return baseSpread * 1.5
	case atr > 0.005:
		return baseSpread * 1.2
	default:
		return baseSpread
	}
}
