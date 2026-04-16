package adaptive_grid

import (
	"math"
	"sync"

	"go.uber.org/zap"
)

// FluidFlowEngine implements continuous flow behavior instead of discrete state transitions
// This makes the bot "soft like water" - adapting continuously rather than jumping between states
type FluidFlowEngine struct {
	logger *zap.Logger

	// Flow intensity (0-1): How aggressively to trade
	flowIntensity map[string]float64 // symbol -> intensity

	// Flow direction (-1 to 1): Bias toward long (-1) or short (1)
	flowDirection map[string]float64 // symbol -> direction

	// Flow velocity: Rate of change of flow parameters
	flowVelocity map[string]float64 // symbol -> velocity

	// Market condition weights for flow calculation
	weights struct {
		Volatility float64
		Trend      float64
		Risk       float64
		Skew       float64
		Liquidity  float64
	}

	mu sync.RWMutex
}

// NewFluidFlowEngine creates a new fluid flow engine
func NewFluidFlowEngine(logger *zap.Logger) *FluidFlowEngine {
	return &FluidFlowEngine{
		logger:        logger,
		flowIntensity: make(map[string]float64),
		flowDirection: make(map[string]float64),
		flowVelocity:  make(map[string]float64),
		weights: struct {
			Volatility float64
			Trend      float64
			Risk       float64
			Skew       float64
			Liquidity  float64
		}{
			Volatility: 0.25,
			Trend:      0.20,
			Risk:       0.30,
			Skew:       0.15,
			Liquidity:  0.10,
		},
	}
}

// FlowParameters represents continuous flow parameters
type FlowParameters struct {
	Intensity        float64 // 0-1: Trading aggressiveness
	Direction        float64 // -1 to 1: Long/Short bias
	Velocity         float64 // Rate of change
	SizeMultiplier   float64 // 0-1: Position size multiplier
	SpreadMultiplier float64 // 0.5-2.0: Spread multiplier
}

// CalculateFlow calculates continuous flow parameters based on market conditions
func (f *FluidFlowEngine) CalculateFlow(
	symbol string,
	positionSize, volatility, risk, trend, skew, liquidity float64,
) FlowParameters {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Calculate flow intensity (0-1)
	// Higher intensity = more aggressive trading
	intensity := f.calculateIntensity(positionSize, volatility, risk, trend, skew, liquidity)

	// Calculate flow direction (-1 to 1)
	// Negative = long bias, Positive = short bias
	direction := f.calculateDirection(skew, trend, risk)

	// Calculate flow velocity (rate of change)
	velocity := f.calculateVelocity(intensity, f.flowIntensity[symbol])

	// Update cached values
	f.flowIntensity[symbol] = intensity
	f.flowDirection[symbol] = direction
	f.flowVelocity[symbol] = velocity

	// Calculate derived parameters
	sizeMultiplier := f.calculateSizeMultiplier(intensity, risk)
	spreadMultiplier := f.calculateSpreadMultiplier(intensity, volatility)

	return FlowParameters{
		Intensity:        intensity,
		Direction:        direction,
		Velocity:         velocity,
		SizeMultiplier:   sizeMultiplier,
		SpreadMultiplier: spreadMultiplier,
	}
}

// calculateIntensity determines trading aggressiveness
func (f *FluidFlowEngine) calculateIntensity(positionSize, volatility, risk, trend, skew, liquidity float64) float64 {
	// Base intensity from position size (lower position = higher intensity to build)
	baseIntensity := 1.0 - positionSize

	// Adjust for volatility (higher volatility = lower intensity to be cautious)
	volatilityFactor := 1.0 - (volatility * f.weights.Volatility)

	// Adjust for risk (higher risk = lower intensity)
	riskFactor := 1.0 - (risk * f.weights.Risk)

	// Adjust for trend (stronger trend = higher intensity to follow)
	trendFactor := trend * f.weights.Trend

	// Adjust for skew (extreme skew = lower intensity to avoid imbalance)
	skewFactor := 1.0 - (math.Abs(skew) * f.weights.Skew)

	// Adjust for liquidity (higher liquidity = higher intensity)
	liquidityFactor := liquidity * f.weights.Liquidity

	// Combine factors
	intensity := baseIntensity * volatilityFactor * riskFactor
	intensity += trendFactor
	intensity *= skewFactor
	intensity += liquidityFactor

	// Clamp to [0, 1]
	intensity = math.Max(0, math.Min(1, intensity))

	return intensity
}

// calculateDirection determines long/short bias
func (f *FluidFlowEngine) calculateDirection(skew, trend, risk float64) float64 {
	// Base direction from skew (negative = long bias, positive = short bias)
	direction := -skew * 0.6 // 60% weight on skew

	// Adjust for trend (stronger trend = follow trend)
	direction += trend * 0.3 // 30% weight on trend

	// Adjust for risk (higher risk = reduce directional bias)
	direction *= (1.0 - risk*0.1) // 10% risk adjustment

	// Clamp to [-1, 1]
	direction = math.Max(-1, math.Min(1, direction))

	return direction
}

// calculateVelocity determines rate of change
func (f *FluidFlowEngine) calculateVelocity(currentIntensity, previousIntensity float64) float64 {
	if previousIntensity == 0 {
		// First calculation, assume moderate velocity
		return 0.1
	}

	// Velocity is the rate of change
	velocity := currentIntensity - previousIntensity

	// Clamp to [-0.5, 0.5] to prevent extreme changes
	velocity = math.Max(-0.5, math.Min(0.5, velocity))

	return velocity
}

// calculateSizeMultiplier determines position size multiplier based on flow
func (f *FluidFlowEngine) calculateSizeMultiplier(intensity, risk float64) float64 {
	// Base multiplier from intensity
	multiplier := intensity

	// Risk adjustment (higher risk = lower multiplier)
	multiplier *= (1.0 - risk*0.3)

	// Clamp to [0.1, 1.0]
	multiplier = math.Max(0.1, math.Min(1.0, multiplier))

	return multiplier
}

// calculateSpreadMultiplier determines spread multiplier based on flow
func (f *FluidFlowEngine) calculateSpreadMultiplier(intensity, volatility float64) float64 {
	// Higher intensity = tighter spread to capture more fills
	// Higher volatility = wider spread to account for price movement

	baseMultiplier := 2.0 - intensity // High intensity = low multiplier (tight spread)

	// Volatility adjustment
	volatilityAdjustment := volatility * 1.5
	multiplier := baseMultiplier + volatilityAdjustment

	// Clamp to [0.5, 2.0]
	multiplier = math.Max(0.5, math.Min(2.0, multiplier))

	return multiplier
}

// GetFlowParameters returns current flow parameters for a symbol
func (f *FluidFlowEngine) GetFlowParameters(symbol string) FlowParameters {
	f.mu.RLock()
	defer f.mu.RUnlock()

	intensity := f.flowIntensity[symbol]
	direction := f.flowDirection[symbol]
	velocity := f.flowVelocity[symbol]

	// Calculate derived parameters
	sizeMultiplier := f.calculateSizeMultiplier(intensity, 0.5)     // Use default risk
	spreadMultiplier := f.calculateSpreadMultiplier(intensity, 0.5) // Use default volatility

	return FlowParameters{
		Intensity:        intensity,
		Direction:        direction,
		Velocity:         velocity,
		SizeMultiplier:   sizeMultiplier,
		SpreadMultiplier: spreadMultiplier,
	}
}

// ShouldPauseTrading determines if trading should be paused based on flow
func (f *FluidFlowEngine) ShouldPauseTrading(symbol string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	intensity := f.flowIntensity[symbol]

	// Pause if intensity is very low (market conditions too poor)
	return intensity < 0.1
}

// ShouldAggressiveMode determines if aggressive trading is appropriate
func (f *FluidFlowEngine) ShouldAggressiveMode(symbol string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	intensity := f.flowIntensity[symbol]

	// Aggressive mode if intensity is high
	return intensity > 0.8
}

// ShouldDefensiveMode determines if defensive trading is appropriate
func (f *FluidFlowEngine) ShouldDefensiveMode(symbol string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	intensity := f.flowIntensity[symbol]

	// Defensive mode if intensity is low but not paused
	return intensity >= 0.1 && intensity < 0.4
}

// UpdateWeights updates the weights for flow calculation
func (f *FluidFlowEngine) UpdateWeights(volatility, trend, risk, skew, liquidity float64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	total := volatility + trend + risk + skew + liquidity
	if total == 0 {
		return
	}

	f.weights.Volatility = volatility / total
	f.weights.Trend = trend / total
	f.weights.Risk = risk / total
	f.weights.Skew = skew / total
	f.weights.Liquidity = liquidity / total

	f.logger.Info("Flow weights updated",
		zap.Float64("volatility", f.weights.Volatility),
		zap.Float64("trend", f.weights.Trend),
		zap.Float64("risk", f.weights.Risk),
		zap.Float64("skew", f.weights.Skew),
		zap.Float64("liquidity", f.weights.Liquidity))
}

// GetFlowState returns the current flow state for logging/monitoring
func (f *FluidFlowEngine) GetFlowState(symbol string) map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return map[string]interface{}{
		"intensity": f.flowIntensity[symbol],
		"direction": f.flowDirection[symbol],
		"velocity":  f.flowVelocity[symbol],
		"weights": map[string]float64{
			"volatility": f.weights.Volatility,
			"trend":      f.weights.Trend,
			"risk":       f.weights.Risk,
			"skew":       f.weights.Skew,
			"liquidity":  f.weights.Liquidity,
		},
	}
}
