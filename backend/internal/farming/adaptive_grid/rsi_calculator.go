package adaptive_grid

import (
	"sync"
)

// RSICalculator calculates Relative Strength Index for trend detection
type RSICalculator struct {
	period  int
	prices  []float64
	gains   []float64
	losses  []float64
	lastRSI float64
	mu      sync.RWMutex
}

// NewRSICalculator creates a new RSI calculator with specified period
func NewRSICalculator(period int) *RSICalculator {
	return &RSICalculator{
		period: period,
		prices: make([]float64, 0, period+1),
		gains:  make([]float64, 0, period),
		losses: make([]float64, 0, period),
	}
}

// AddPrice adds a new price data point
func (r *RSICalculator) AddPrice(price float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Need at least 2 prices to calculate change
	if len(r.prices) > 0 {
		change := price - r.prices[len(r.prices)-1]
		if change > 0 {
			r.gains = append(r.gains, change)
			r.losses = append(r.losses, 0)
		} else {
			r.gains = append(r.gains, 0)
			r.losses = append(r.losses, -change)
		}

		// Keep only needed history
		if len(r.gains) > r.period {
			r.gains = r.gains[1:]
			r.losses = r.losses[1:]
		}
	}

	r.prices = append(r.prices, price)
	if len(r.prices) > r.period+1 {
		r.prices = r.prices[1:]
	}

	// Recalculate RSI
	r.calculateRSI()
}

// calculateRSI computes the Relative Strength Index
func (r *RSICalculator) calculateRSI() {
	if len(r.gains) < r.period {
		r.lastRSI = 50 // Neutral when not enough data
		return
	}

	// Calculate average gain and loss
	var avgGain, avgLoss float64
	for i := 0; i < r.period; i++ {
		avgGain += r.gains[len(r.gains)-1-i]
		avgLoss += r.losses[len(r.losses)-1-i]
	}
	avgGain /= float64(r.period)
	avgLoss /= float64(r.period)

	if avgLoss == 0 {
		r.lastRSI = 100
		return
	}

	rs := avgGain / avgLoss
	r.lastRSI = 100 - (100 / (1 + rs))
}

// GetRSI returns the current RSI value
func (r *RSICalculator) GetRSI() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastRSI
}

// IsOverbought returns true if RSI > threshold (default 70)
func (r *RSICalculator) IsOverbought(threshold float64) bool {
	return r.GetRSI() > threshold
}

// IsOversold returns true if RSI < threshold (default 30)
func (r *RSICalculator) IsOversold(threshold float64) bool {
	return r.GetRSI() < threshold
}

// GetTrendDirection returns trend based on RSI
func (r *RSICalculator) GetTrendDirection() TrendDirection {
	rsi := r.GetRSI()
	switch {
	case rsi > 70:
		return TrendStrongUp
	case rsi > 60:
		return TrendUp
	case rsi < 30:
		return TrendStrongDown
	case rsi < 40:
		return TrendDown
	default:
		return TrendNeutral
	}
}

// IsReady returns true if calculator has enough data
func (r *RSICalculator) IsReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.gains) >= r.period
}

// Reset clears all data
func (r *RSICalculator) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prices = r.prices[:0]
	r.gains = r.gains[:0]
	r.losses = r.losses[:0]
	r.lastRSI = 50
}

// TrendDirection represents the trend state
type TrendDirection int

const (
	TrendNeutral TrendDirection = iota
	TrendUp
	TrendDown
	TrendStrongUp
	TrendStrongDown
)

// String returns string representation of trend
func (t TrendDirection) String() string {
	switch t {
	case TrendStrongUp:
		return "STRONG_UP"
	case TrendUp:
		return "UP"
	case TrendDown:
		return "DOWN"
	case TrendStrongDown:
		return "STRONG_DOWN"
	default:
		return "NEUTRAL"
	}
}
