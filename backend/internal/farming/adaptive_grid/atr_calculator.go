package adaptive_grid

import (
	"math"
	"sync"
)

// ATRCalculator calculates Average True Range for volatility measurement
type ATRCalculator struct {
	period  int
	highs   []float64
	lows    []float64
	closes  []float64
	lastATR float64
	mu      sync.RWMutex
}

// NewATRCalculator creates a new ATR calculator with specified period
func NewATRCalculator(period int) *ATRCalculator {
	return &ATRCalculator{
		period: period,
		highs:  make([]float64, 0, period+1),
		lows:   make([]float64, 0, period+1),
		closes: make([]float64, 0, period+1),
	}
}

// AddPrice adds a new price data point
func (a *ATRCalculator) AddPrice(high, low, close float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.highs = append(a.highs, high)
	a.lows = append(a.lows, low)
	a.closes = append(a.closes, close)

	// Keep only needed history
	if len(a.highs) > a.period+1 {
		a.highs = a.highs[1:]
		a.lows = a.lows[1:]
		a.closes = a.closes[1:]
	}

	// Recalculate ATR
	a.calculateATR()
}

// calculateATR computes the Average True Range
func (a *ATRCalculator) calculateATR() {
	if len(a.highs) < 2 {
		a.lastATR = 0
		return
	}

	var trSum float64
	count := 0

	// Start from index 1 (need previous close)
	startIdx := 0
	if len(a.highs) > a.period {
		startIdx = len(a.highs) - a.period
	}

	for i := startIdx + 1; i < len(a.highs); i++ {
		// True Range = max(high-low, |high-close_prev|, |low-close_prev|)
		highLow := a.highs[i] - a.lows[i]
		highClose := math.Abs(a.highs[i] - a.closes[i-1])
		lowClose := math.Abs(a.lows[i] - a.closes[i-1])

		tr := highLow
		if highClose > tr {
			tr = highClose
		}
		if lowClose > tr {
			tr = lowClose
		}
		trSum += tr
		count++
	}

	if count > 0 {
		a.lastATR = trSum / float64(count)
	}
}

// GetATR returns the current ATR value
func (a *ATRCalculator) GetATR() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastATR
}

// GetATRPercent returns ATR as percentage of current price
func (a *ATRCalculator) GetATRPercent(currentPrice float64) float64 {
	if currentPrice == 0 {
		return 0
	}
	atr := a.GetATR()
	return (atr / currentPrice) * 100
}

// GetATRPct is an alias for GetATRPercent for backward compatibility
func (a *ATRCalculator) GetATRPct(currentPrice float64) float64 {
	return a.GetATRPercent(currentPrice)
}

// IsReady returns true if calculator has enough data
func (a *ATRCalculator) IsReady() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.highs) >= a.period
}

// Reset clears all data
func (a *ATRCalculator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.highs = a.highs[:0]
	a.lows = a.lows[:0]
	a.closes = a.closes[:0]
	a.lastATR = 0
}
