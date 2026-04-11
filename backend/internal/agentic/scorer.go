package agentic

import (
	"math"

	"aster-bot/internal/config"
)

// OpportunityScorer calculates opportunity scores for symbols
type OpportunityScorer struct {
	config config.ScoringConfig
}

// NewOpportunityScorer creates a new scorer with the given configuration
func NewOpportunityScorer(cfg config.ScoringConfig) *OpportunityScorer {
	return &OpportunityScorer{config: cfg}
}

// CalculateScore computes the overall opportunity score for a symbol
func (os *OpportunityScorer) CalculateScore(
	regime RegimeSnapshot,
	values *IndicatorValues,
) float64 {
	weights := os.config.Weights

	// Calculate individual factor scores (0-100)
	trendScore := os.scoreTrend(values, regime)
	volScore := os.scoreVolatility(values, regime)
	volumeScore := os.scoreVolume(values)
	structureScore := os.scoreStructure(values, regime)

	// Weighted final score
	finalScore :=
		trendScore*weights.Trend +
			volScore*weights.Volatility +
			volumeScore*weights.Volume +
			structureScore*weights.Structure

	// Apply regime-based adjustments
	finalScore = os.applyRegimeAdjustments(finalScore, regime.Regime)

	// Clamp to 0-100
	return clamp(finalScore, 0, 100)
}

// CalculateRecommendation determines the recommendation based on score
func (os *OpportunityScorer) CalculateRecommendation(score float64) Recommendation {
	thresholds := os.config.Thresholds

	switch {
	case score >= thresholds.HighScore:
		return RecHigh
	case score >= thresholds.MediumScore:
		return RecMedium
	case score >= thresholds.LowScore:
		return RecLow
	default:
		return RecSkip
	}
}

// scoreTrend calculates trend score based on ADX and price action
func (os *OpportunityScorer) scoreTrend(values *IndicatorValues, regime RegimeSnapshot) float64 {
	// For grid trading, moderate trend is best
	// ADX 15-30 = good range (moderate trend, manageable risk)
	// ADX < 10 = too weak (poor fills)
	// ADX > 40 = too strong (high risk of directional moves)

	adx := values.ADX

	switch {
	case adx >= 15 && adx <= 30:
		// Sweet spot for grid trading
		return 85.0
	case adx > 30 && adx <= 40:
		// Moderate trend, slightly higher risk
		return 70.0
	case adx > 10 && adx < 15:
		// Weak trend, may have poor fills
		return 60.0
	case adx > 40:
		// Strong trend, high risk
		return 40.0
	default:
		// Very weak trend
		return 30.0
	}
}

// scoreVolatility calculates volatility score
func (os *OpportunityScorer) scoreVolatility(values *IndicatorValues, regime RegimeSnapshot) float64 {
	// ATR-based scoring
	// For grid trading, moderate volatility is best
	// Too low = few fills
	// Too high = high risk

	atr := values.ATR14
	bbWidth := values.BBWidth

	// Normalize ATR (as percentage of price)
	// ATR 0.3-0.8% = good range
	// ATR < 0.3% = too low
	// ATR > 1.5% = too high

	var atrScore float64
	switch {
	case atr >= 0.003 && atr <= 0.008:
		// Good volatility range
		atrScore = 85.0
	case atr > 0.008 && atr <= 0.015:
		// Higher volatility, manageable
		atrScore = 70.0
	case atr > 0.001 && atr < 0.003:
		// Low volatility, fewer fills
		atrScore = 60.0
	case atr > 0.015:
		// Very high volatility, risky
		atrScore = 30.0
	default:
		// Extremely low
		atrScore = 40.0
	}

	// Factor in BB width for additional context
	bbScore := 50.0
	if bbWidth > 0.01 && bbWidth < 0.05 {
		bbScore = 80.0
	} else if bbWidth >= 0.05 {
		bbScore = 40.0
	}

	// Weighted combination (70% ATR, 30% BB)
	return atrScore*0.7 + bbScore*0.3
}

// scoreVolume calculates volume score
func (os *OpportunityScorer) scoreVolume(values *IndicatorValues) float64 {
	// Volume 24h in USD
	volume24h := values.Volume24h

	// Score based on absolute volume
	// > 100M = excellent (100)
	// 50M-100M = very good (90)
	// 10M-50M = good (80)
	// 1M-10M = moderate (70)
	// < 1M = poor (50)

	switch {
	case volume24h >= 100_000_000:
		return 100.0
	case volume24h >= 50_000_000:
		return 90.0
	case volume24h >= 10_000_000:
		return 80.0
	case volume24h >= 1_000_000:
		return 70.0
	default:
		return 50.0
	}
}

// scoreStructure calculates market structure score
func (os *OpportunityScorer) scoreStructure(values *IndicatorValues, regime RegimeSnapshot) float64 {
	// Price change context
	priceChange := values.PriceChange

	// Smaller price changes are better for grid stability
	// |change| < 1% = excellent
	// |change| 1-3% = good
	// |change| 3-5% = moderate
	// |change| > 5% = poor

	absChange := math.Abs(priceChange)

	var changeScore float64
	switch {
	case absChange < 1.0:
		changeScore = 90.0
	case absChange < 3.0:
		changeScore = 80.0
	case absChange < 5.0:
		changeScore = 65.0
	default:
		changeScore = 40.0
	}

	// Bonus for sideways regime (best for grid)
	regimeBonus := 0.0
	if regime.Regime == RegimeSideways {
		regimeBonus = 10.0
	}

	return min(100.0, changeScore+regimeBonus)
}

// applyRegimeAdjustments applies regime-based score adjustments
func (os *OpportunityScorer) applyRegimeAdjustments(score float64, regime RegimeType) float64 {
	// Regime multipliers
	switch regime {
	case RegimeSideways:
		// Best for grid - bonus
		return score * 1.1
	case RegimeTrending:
		// Moderate - slight penalty
		return score * 0.95
	case RegimeVolatile:
		// Risky - penalty
		return score * 0.75
	case RegimeRecovery:
		// Uncertain - moderate penalty
		return score * 0.90
	default:
		return score
	}
}

// GetFactorBreakdown returns detailed score breakdown by factor
func (os *OpportunityScorer) GetFactorBreakdown(
	regime RegimeSnapshot,
	values *IndicatorValues,
) map[string]float64 {
	return map[string]float64{
		"trend":        os.scoreTrend(values, regime),
		"volatility":   os.scoreVolatility(values, regime),
		"volume":       os.scoreVolume(values),
		"structure":    os.scoreStructure(values, regime),
		"regime_bonus": os.applyRegimeAdjustments(100, regime.Regime) - 100,
	}
}

func clamp(v, minVal, maxVal float64) float64 {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}
