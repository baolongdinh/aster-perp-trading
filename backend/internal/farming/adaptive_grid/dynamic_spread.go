package adaptive_grid

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// VolatilityLevel represents the current volatility regime
type VolatilityLevel int

const (
	VolatilityLow VolatilityLevel = iota
	VolatilityNormal
	VolatilityHigh
	VolatilityExtreme
)

func (v VolatilityLevel) String() string {
	switch v {
	case VolatilityLow:
		return "LOW"
	case VolatilityNormal:
		return "NORMAL"
	case VolatilityHigh:
		return "HIGH"
	case VolatilityExtreme:
		return "EXTREME"
	default:
		return "UNKNOWN"
	}
}

// DynamicSpreadCalculator calculates adaptive grid spread based on ATR
type DynamicSpreadCalculator struct {
	atrCalc           *ATRCalculator
	baseSpreadPct     float64
	lowThreshold      float64
	normalThreshold   float64
	highThreshold     float64
	multipliers       map[VolatilityLevel]float64
	currentLevel      VolatilityLevel
	currentMultiplier float64
	lastUpdate        time.Time
	logger            *zap.Logger
	mu                sync.RWMutex
}

// DynamicSpreadConfig holds configuration for dynamic spread
type DynamicSpreadConfig struct {
	BaseSpreadPct     float64 `yaml:"base_spread_pct"`    // Default 0.5%
	LowThreshold      float64 `yaml:"low_threshold"`      // ATR < 0.3%
	NormalThreshold   float64 `yaml:"normal_threshold"`   // ATR 0.3-0.8%
	HighThreshold     float64 `yaml:"high_threshold"`     // ATR 0.8-1.5%
	LowMultiplier     float64 `yaml:"low_multiplier"`     // 0.6x
	NormalMultiplier  float64 `yaml:"normal_multiplier"`  // 1.0x
	HighMultiplier    float64 `yaml:"high_multiplier"`    // 1.8x
	ExtremeMultiplier float64 `yaml:"extreme_multiplier"` // 2.5x
	ATRPeriod         int     `yaml:"atr_period"`         // 14
}

// DefaultDynamicSpreadConfig returns default configuration for volume farming
func DefaultDynamicSpreadConfig() *DynamicSpreadConfig {
	return &DynamicSpreadConfig{
		BaseSpreadPct:     0.01, // 0.01% for tight grid (volume farming)
		LowThreshold:      0.3,  // ATR < 0.3% = low vol
		NormalThreshold:   0.8,  // ATR 0.3-0.8% = normal
		HighThreshold:     1.5,  // ATR 0.8-1.5% = high
		LowMultiplier:     0.8,  // Low vol: 0.008% spread (tighter)
		NormalMultiplier:  1.0,  // Normal: 0.01% spread (base)
		HighMultiplier:    1.5,  // High vol: 0.015% spread (wider)
		ExtremeMultiplier: 2.0,  // Extreme: 0.02% spread (max)
		ATRPeriod:         7,    // Faster ATR for quick response
	}
}

// NewDynamicSpreadCalculator creates a new dynamic spread calculator
func NewDynamicSpreadCalculator(config *DynamicSpreadConfig, logger *zap.Logger) *DynamicSpreadCalculator {
	if config == nil {
		config = DefaultDynamicSpreadConfig()
	}

	return &DynamicSpreadCalculator{
		atrCalc:         NewATRCalculator(config.ATRPeriod),
		baseSpreadPct:   config.BaseSpreadPct,
		lowThreshold:    config.LowThreshold,
		normalThreshold: config.NormalThreshold,
		highThreshold:   config.HighThreshold,
		multipliers: map[VolatilityLevel]float64{
			VolatilityLow:     config.LowMultiplier,
			VolatilityNormal:  config.NormalMultiplier,
			VolatilityHigh:    config.HighMultiplier,
			VolatilityExtreme: config.ExtremeMultiplier,
		},
		currentLevel:      VolatilityNormal,
		currentMultiplier: config.NormalMultiplier,
		lastUpdate:        time.Now(),
		logger:            logger,
	}
}

// UpdateATR updates ATR with new price data
func (d *DynamicSpreadCalculator) UpdateATR(high, low, close float64) {
	d.atrCalc.AddPrice(high, low, close)
	d.recalculate()
}

// recalculate determines current volatility level and spread
func (d *DynamicSpreadCalculator) recalculate() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.atrCalc.IsReady() {
		return
	}

	// Get current price from closes
	atr := d.atrCalc.GetATR()
	currentPrice := d.getCurrentPrice()

	if currentPrice == 0 {
		return
	}

	atrPercent := (atr / currentPrice) * 100

	// Determine volatility level
	var newLevel VolatilityLevel
	switch {
	case atrPercent < d.lowThreshold:
		newLevel = VolatilityLow
	case atrPercent < d.normalThreshold:
		newLevel = VolatilityNormal
	case atrPercent < d.highThreshold:
		newLevel = VolatilityHigh
	default:
		newLevel = VolatilityExtreme
	}

	// Log level change
	if newLevel != d.currentLevel {
		d.logger.Info("Volatility level changed",
			zap.String("from", d.currentLevel.String()),
			zap.String("to", newLevel.String()),
			zap.Float64("atr_pct", atrPercent))
		d.currentLevel = newLevel
	}

	d.currentMultiplier = d.multipliers[newLevel]
	d.lastUpdate = time.Now()
}

// getCurrentPrice returns last known price
func (d *DynamicSpreadCalculator) getCurrentPrice() float64 {
	// Use ATR calculator's closes
	atr := d.atrCalc
	atr.mu.RLock()
	defer atr.mu.RUnlock()

	if len(atr.closes) > 0 {
		return atr.closes[len(atr.closes)-1]
	}
	return 0
}

// GetDynamicSpread returns the calculated dynamic spread percentage
func (d *DynamicSpreadCalculator) GetDynamicSpread() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.baseSpreadPct * d.currentMultiplier
}

// GetATRPercent returns current ATR as percentage
func (d *DynamicSpreadCalculator) GetATRPercent() float64 {
	currentPrice := d.getCurrentPrice()
	if currentPrice == 0 {
		return 0
	}
	return d.atrCalc.GetATRPercent(currentPrice)
}

// GetVolatilityLevel returns current volatility level
func (d *DynamicSpreadCalculator) GetVolatilityLevel() VolatilityLevel {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentLevel
}

// GetMultiplier returns current spread multiplier
func (d *DynamicSpreadCalculator) GetMultiplier() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentMultiplier
}

// CalculateGridLevels returns recommended number of grid levels
func (d *DynamicSpreadCalculator) CalculateGridLevels(baseLevels int) int {
	d.mu.RLock()
	multiplier := d.currentMultiplier
	d.mu.RUnlock()

	var adjustedLevels float64

	switch {
	case multiplier > 2.0:
		// Extreme volatility - reduce levels by 30%
		adjustedLevels = float64(baseLevels) * 0.7
	case multiplier < 0.7:
		// Low volatility - increase levels by 20%
		adjustedLevels = float64(baseLevels) * 1.2
	default:
		adjustedLevels = float64(baseLevels)
	}

	// Enforce bounds
	minLevels := 3
	maxLevels := 10

	levels := int(adjustedLevels)
	if levels < minLevels {
		levels = minLevels
	}
	if levels > maxLevels {
		levels = maxLevels
	}

	return levels
}

// GetStatus returns full status for monitoring
func (d *DynamicSpreadCalculator) GetStatus() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]interface{}{
		"base_spread_pct":    d.baseSpreadPct,
		"current_multiplier": d.currentMultiplier,
		"dynamic_spread_pct": d.baseSpreadPct * d.currentMultiplier,
		"volatility_level":   d.currentLevel.String(),
		"atr_percent":        d.GetATRPercent(),
		"last_update":        d.lastUpdate,
		"atr_ready":          d.atrCalc.IsReady(),
	}
}

// Reset clears all data
func (d *DynamicSpreadCalculator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.atrCalc.Reset()
	d.currentLevel = VolatilityNormal
	d.currentMultiplier = d.multipliers[VolatilityNormal]
	d.lastUpdate = time.Now()
}
