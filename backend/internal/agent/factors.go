package agent

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// FactorEngine calculates the multi-factor deployment score
type FactorEngine struct {
	config      *FactorsConfig
	calculator  *IndicatorCalculator
	lastFactors []DecisionFactor
	lastScore   float64
	lastUpdate  time.Time
	cacheTTL    time.Duration // T044a: Cache time-to-live
}

// NewFactorEngine creates a new factor engine
func NewFactorEngine(config *FactorsConfig) *FactorEngine {
	return &FactorEngine{
		config:     config,
		calculator: NewIndicatorCalculator(),
		cacheTTL:   5 * time.Second, // T044a: 5s cache TTL
	}
}

// CalculateScore computes the deployment score from all factors
func (fe *FactorEngine) CalculateScore(values *IndicatorValues, regime RegimeType) (float64, []DecisionFactor) {
	// T044a: Check cache - if last update within TTL, return cached result
	if time.Since(fe.lastUpdate) < fe.cacheTTL && fe.lastFactors != nil {
		return fe.lastScore, fe.lastFactors
	}

	factors := make([]DecisionFactor, 0, 4)

	// Calculate individual factors
	trendFactor := fe.calculateTrendFactor(values)
	volFactor := fe.calculateVolatilityFactor(values)
	volumeFactor := fe.calculateVolumeFactor(values)
	structureFactor := fe.calculateStructureFactor(values)

	// Get weights from config
	weights := fe.config.Weights

	// Calculate contributions
	trendContrib := trendFactor.NormalizedScore * weights.Trend
	volContrib := volFactor.NormalizedScore * weights.Volatility
	volumeContrib := volumeFactor.NormalizedScore * weights.Volume
	structureContrib := structureFactor.NormalizedScore * weights.Structure

	// Apply regime multiplier
	regimeMult := fe.getRegimeMultiplier(regime)

	// Calculate final score
	score := (trendContrib + volContrib + volumeContrib + structureContrib) * regimeMult

	// Normalize to 0-100
	score = math.Max(0, math.Min(100, score))

	// Set contributions
	trendFactor.Contribution = trendContrib * regimeMult
	volFactor.Contribution = volContrib * regimeMult
	volumeFactor.Contribution = volumeContrib * regimeMult
	structureFactor.Contribution = structureContrib * regimeMult

	factors = append(factors, trendFactor, volFactor, volumeFactor, structureFactor)

	fe.lastFactors = factors
	fe.lastScore = score
	fe.lastUpdate = time.Now()

	return score, factors
}

// calculateTrendFactor calculates the trend component
func (fe *FactorEngine) calculateTrendFactor(values *IndicatorValues) DecisionFactor {
	score := 50.0 // Neutral base

	// ADX contribution (0-50 points)
	// ADX > 25 indicates strong trend (bad for grid), < 25 indicates weak trend (good for grid)
	if values.ADX < 15 {
		score += 25 // Excellent for grid (no trend)
	} else if values.ADX < 25 {
		score += 15 // Good for grid
	} else if values.ADX < 40 {
		score += 5 // Okay for grid
	} // ADX > 40 is bad, score stays at base

	// EMA alignment check
	if !values.IsBullish && !values.IsBearish {
		score += 25 // No clear trend direction (good for grid)
	} else {
		score -= 15 // Clear trend (bad for grid)
	}

	score = math.Max(0, math.Min(100, score))

	return DecisionFactor{
		ID:              uuid.New(),
		Name:            FactorTrend,
		CurrentValue:    values.ADX,
		NormalizedScore: score,
		Weight:          fe.config.Weights.Trend,
		CalculatedAt:    time.Now(),
	}
}

// calculateVolatilityFactor calculates the volatility component
func (fe *FactorEngine) calculateVolatilityFactor(values *IndicatorValues) DecisionFactor {
	score := 50.0 // Neutral base

	// ATR-based scoring
	// Low ATR is good for grid trading
	// Normalized ATR (ATR as % of price)
	normalizedATR := values.ATR14 / 100 // Assuming price around 100 for normalization

	if normalizedATR < 0.005 { // < 0.5% volatility
		score += 30 // Excellent
	} else if normalizedATR < 0.01 { // < 1% volatility
		score += 20 // Good
	} else if normalizedATR < 0.02 { // < 2% volatility
		score += 10 // Okay
	} else if normalizedATR > 0.05 { // > 5% volatility
		score -= 30 // Bad
	}

	// BB Width contribution
	if values.BBWidth < 3.0 {
		score += 20 // Narrow bands (good for grid)
	} else if values.BBWidth > 8.0 {
		score -= 20 // Wide bands (bad for grid)
	}

	score = math.Max(0, math.Min(100, score))

	return DecisionFactor{
		ID:              uuid.New(),
		Name:            FactorVolatility,
		CurrentValue:    values.ATR14,
		NormalizedScore: score,
		Weight:          fe.config.Weights.Volatility,
		CalculatedAt:    time.Now(),
	}
}

// calculateVolumeFactor calculates the volume component
func (fe *FactorEngine) calculateVolumeFactor(values *IndicatorValues) DecisionFactor {
	score := 50.0 // Neutral base

	if values.VolumeMA20 == 0 {
		return DecisionFactor{
			ID:              uuid.New(),
			Name:            FactorVolume,
			CurrentValue:    values.CurrentVolume,
			NormalizedScore: 50,
			Weight:          fe.config.Weights.Volume,
			CalculatedAt:    time.Now(),
		}
	}

	// Volume ratio
	volRatio := values.CurrentVolume / values.VolumeMA20

	if volRatio > 2.0 {
		// High volume spike - could indicate breakout (bad for grid)
		score -= 20
	} else if volRatio > 1.5 {
		// Elevated volume
		score -= 10
	} else if volRatio < 0.5 {
		// Low volume (good for grid - no institutional interest)
		score += 20
	} else if volRatio < 0.8 {
		// Below average volume
		score += 10
	}

	score = math.Max(0, math.Min(100, score))

	return DecisionFactor{
		ID:              uuid.New(),
		Name:            FactorVolume,
		CurrentValue:    values.CurrentVolume,
		NormalizedScore: score,
		Weight:          fe.config.Weights.Volume,
		CalculatedAt:    time.Now(),
	}
}

// calculateStructureFactor calculates the market structure component
func (fe *FactorEngine) calculateStructureFactor(values *IndicatorValues) DecisionFactor {
	score := 50.0 // Neutral base

	// Check if price is near EMAs (consolidation zone is good for grid)
	// Price near EMA21 indicates range-bound market
	ema21Diff := math.Abs(values.EMA9-values.EMA21) / values.EMA21 * 100

	if ema21Diff < 0.5 {
		score += 25 // Price very close to EMA21 (consolidation)
	} else if ema21Diff < 1.0 {
		score += 15
	} else if ema21Diff > 3.0 {
		score -= 15 // Price far from EMA (trending)
	}

	// Check for ranging vs trending using EMA spread
	emaSpread := (values.EMA50 - values.EMA200) / values.EMA200 * 100

	if math.Abs(emaSpread) < 5 {
		score += 15 // EMAs close together (ranging)
	} else if math.Abs(emaSpread) > 15 {
		score -= 15 // EMAs far apart (trending)
	}

	score = math.Max(0, math.Min(100, score))

	return DecisionFactor{
		ID:              uuid.New(),
		Name:            FactorStructure,
		CurrentValue:    ema21Diff,
		NormalizedScore: score,
		Weight:          fe.config.Weights.Structure,
		CalculatedAt:    time.Now(),
	}
}

// getRegimeMultiplier applies regime-based adjustments
func (fe *FactorEngine) getRegimeMultiplier(regime RegimeType) float64 {
	switch regime {
	case RegimeSideways:
		return 1.0 // Full score for sideways
	case RegimeRecovery:
		return 0.8 // Slightly reduced during recovery
	case RegimeTrending:
		return 0.3 // Significantly reduced for trending
	case RegimeVolatile:
		return 0.1 // Heavily reduced for volatile
	default:
		return 0.5 // Unknown regime - be cautious
	}
}

// GetLastFactors returns the last calculated factors
func (fe *FactorEngine) GetLastFactors() []DecisionFactor {
	return fe.lastFactors
}

// GetLastScore returns the last calculated score
func (fe *FactorEngine) GetLastScore() float64 {
	return fe.lastScore
}

// PositionSizer calculates position size based on score and volatility
type PositionSizer struct {
	config *PositionSizingConfig
}

// NewPositionSizer creates a new position sizer
func NewPositionSizer(config *PositionSizingConfig) *PositionSizer {
	return &PositionSizer{config: config}
}

// CalculateSize computes the final position size
func (ps *PositionSizer) CalculateSize(baseSize, score float64, values *IndicatorValues) float64 {
	// Score multiplier
	scoreMult := ps.getScoreMultiplier(score)

	// Volatility multiplier
	volMult := ps.getVolatilityMultiplier(values)

	// Calculate final size
	finalSize := baseSize * scoreMult * volMult

	return finalSize
}

// getScoreMultiplier returns multiplier based on deployment score
func (ps *PositionSizer) getScoreMultiplier(score float64) float64 {
	scoring := ps.config.ScoreMultipliers

	if score >= 75 {
		return scoring.Full
	} else if score >= 60 {
		return scoring.Reduced
	}
	return scoring.None
}

// getVolatilityMultiplier returns multiplier based on volatility
func (ps *PositionSizer) getVolatilityMultiplier(values *IndicatorValues) float64 {
	// Calculate normalized ATR
	normalizedATR := 0.0
	if values.EMA21 > 0 {
		normalizedATR = values.ATR14 / values.EMA21 * 100
	}

	if normalizedATR < 0.5 {
		return ps.config.VolatilityMultipliers.Normal
	} else if normalizedATR < 2.0 {
		return ps.config.VolatilityMultipliers.High
	}
	return ps.config.VolatilityMultipliers.Extreme
}

// CalculateGridSpacing computes grid spacing based on volatility
func (ps *PositionSizer) CalculateGridSpacing(values *IndicatorValues) float64 {
	normalizedATR := 0.0
	if values.EMA21 > 0 {
		normalizedATR = values.ATR14 / values.EMA21 * 100
	}

	if normalizedATR < 0.5 {
		return ps.config.GridSpacing.LowVol
	} else if normalizedATR < 2.0 {
		return ps.config.GridSpacing.NormalVol
	}
	return ps.config.GridSpacing.HighVol
}
