package maker

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// LeverageAdapter provides dynamic leverage adjustment based on market conditions
type LeverageAdapter struct {
	baseMaxLeverage  int
	currentLeverages map[string]int // Symbol -> current leverage
	volatilityWindow int            // Window for volatility calculation
	trendWindow      int            // Window for trend detection
	mu               sync.RWMutex
	logger           *zap.Logger

	// Historical data for calculations
	priceHistory   map[string][]PricePoint
	lastUpdateTime map[string]time.Time
}

// PricePoint stores timestamp and price for volatility/trend calculations
type PricePoint struct {
	Timestamp time.Time
	Price     float64
}

// NewLeverageAdapter creates a new leverage adapter
func NewLeverageAdapter(baseMaxLeverage int, logger *zap.Logger) *LeverageAdapter {
	return &LeverageAdapter{
		baseMaxLeverage:  baseMaxLeverage,
		currentLeverages: make(map[string]int),
		priceHistory:     make(map[string][]PricePoint),
		lastUpdateTime:   make(map[string]time.Time),
		volatilityWindow: 30, // 30 price points for volatility
		trendWindow:      10, // 10 price points for trend
		logger:           logger,
	}
}

// UpdatePrice records a new price point for volatility/trend analysis
func (la *LeverageAdapter) UpdatePrice(symbol string, price float64) {
	la.mu.Lock()
	defer la.mu.Unlock()

	now := time.Now()

	// Initialize history if needed
	if la.priceHistory[symbol] == nil {
		la.priceHistory[symbol] = make([]PricePoint, 0, 100)
	}

	// Add new price point
	la.priceHistory[symbol] = append(la.priceHistory[symbol], PricePoint{
		Timestamp: now,
		Price:     price,
	})

	// Keep only recent history (last 100 points)
	if len(la.priceHistory[symbol]) > 100 {
		la.priceHistory[symbol] = la.priceHistory[symbol][1:]
	}

	la.lastUpdateTime[symbol] = now

	// Recalculate leverage for this symbol
	la.recalculateLeverage(symbol)
}

// CalculateAdaptiveLeverage computes optimal leverage based on current market conditions
func (la *LeverageAdapter) CalculateAdaptiveLeverage(symbol string) int {
	la.mu.RLock()
	defer la.mu.RUnlock()

	// Return cached leverage if available
	if leverage, exists := la.currentLeverages[symbol]; exists {
		return leverage
	}

	// Default to base leverage if no data
	return la.baseMaxLeverage
}

// GetVolatility calculates 24h realized volatility for a symbol
func (la *LeverageAdapter) GetVolatility(symbol string) float64 {
	la.mu.RLock()
	defer la.mu.RUnlock()

	history := la.priceHistory[symbol]
	if len(history) < la.volatilityWindow {
		return 0
	}

	// Get recent price points
	recent := history[len(history)-la.volatilityWindow:]
	if len(recent) < 2 {
		return 0
	}

	// Calculate log returns
	var logReturns []float64
	for i := 1; i < len(recent); i++ {
		if recent[i-1].Price > 0 {
			logReturn := math.Log(recent[i].Price / recent[i-1].Price)
			logReturns = append(logReturns, logReturn)
		}
	}

	if len(logReturns) == 0 {
		return 0
	}

	// Calculate mean and standard deviation
	var sum, sumSquares float64
	for _, ret := range logReturns {
		sum += ret
		sumSquares += ret * ret
	}

	mean := sum / float64(len(logReturns))
	variance := (sumSquares / float64(len(logReturns))) - (mean * mean)

	if variance < 0 {
		variance = 0
	}

	// Annualize volatility (assuming 1-second intervals, adjust as needed)
	volatility := math.Sqrt(variance) * math.Sqrt(86400) // Daily to annual
	return volatility * 100                              // Convert to percentage
}

// GetTrendStrength calculates trend strength (-1 to 1) for a symbol
func (la *LeverageAdapter) GetTrendStrength(symbol string) float64 {
	la.mu.RLock()
	defer la.mu.RUnlock()

	history := la.priceHistory[symbol]
	if len(history) < la.trendWindow {
		return 0
	}

	// Get recent price points for trend calculation
	recent := history[len(history)-la.trendWindow:]
	if len(recent) < 2 {
		return 0
	}

	// Simple linear regression for trend
	n := float64(len(recent))
	var sumX, sumY, sumXY, sumX2 float64

	for i, point := range recent {
		x := float64(i)
		y := point.Price

		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// Calculate slope (trend)
	denominator := n*sumX2 - sumX*sumX
	if math.Abs(denominator) < 1e-10 {
		return 0
	}

	slope := (n*sumXY - sumX*sumY) / denominator

	// Normalize slope by price to get percentage trend
	avgPrice := sumY / n
	trendStrength := (slope / avgPrice) * 100 // Convert to percentage

	// Clamp to [-1, 1] range
	return math.Max(-1, math.Min(1, trendStrength))
}

// recalculateLeverage updates leverage based on current market conditions
func (la *LeverageAdapter) recalculateLeverage(symbol string) {
	history := la.priceHistory[symbol]
	if len(history) < la.volatilityWindow {
		la.currentLeverages[symbol] = la.baseMaxLeverage
		return
	}

	// Get market conditions
	volatility := la.GetVolatility(symbol)
	trendStrength := la.GetTrendStrength(symbol)

	// Start with base leverage
	leverage := float64(la.baseMaxLeverage)

	// ============================================================
	// VOLATILITY-BASED LEVERAGE ADJUSTMENT
	// ============================================================
	if volatility > 5.0 { // Very high volatility
		leverage *= 0.25 // → 37.5x (from 150x)
	} else if volatility > 3.0 { // High volatility
		leverage *= 0.5 // → 75x
	} else if volatility > 1.0 { // Normal volatility
		leverage *= 1.0 // → 150x (unchanged)
	} else { // Low volatility
		leverage *= 1.0 // → 150x (unchanged)
	}

	// ============================================================
	// TREND-BASED LEVERAGE ADJUSTMENT (reduce leverage in trending markets)
	// ============================================================
	trendAbs := math.Abs(trendStrength)
	if trendAbs > 0.7 { // Strong trend
		leverage *= 0.5 // Reduce by 50%
	} else if trendAbs > 0.4 { // Moderate trend
		leverage *= 0.75 // Reduce by 25%
	} else if trendAbs > 0.2 { // Weak trend
		leverage *= 0.9 // Reduce by 10%
	}
	// No trend adjustment for trendAbs <= 0.2

	// ============================================================
	// SAFETY CONSTRAINTS
	// ============================================================
	// Minimum leverage for safety
	minLeverage := float64(la.baseMaxLeverage) * 0.1 // 10% of base
	leverage = math.Max(minLeverage, leverage)

	// Maximum leverage (never exceed base)
	leverage = math.Min(float64(la.baseMaxLeverage), leverage)

	finalLeverage := int(leverage)
	la.currentLeverages[symbol] = finalLeverage

	la.logger.Debug("🎚️ Adaptive Leverage Updated",
		zap.String("symbol", symbol),
		zap.Float64("volatility_pct", volatility),
		zap.Float64("trend_strength", trendStrength),
		zap.Int("old_leverage", la.baseMaxLeverage),
		zap.Int("new_leverage", finalLeverage),
		zap.Float64("leverage_multiplier", float64(finalLeverage)/float64(la.baseMaxLeverage)))
}

// GetLeverageMultiplier returns the current leverage multiplier for a symbol
func (la *LeverageAdapter) GetLeverageMultiplier(symbol string) float64 {
	la.mu.RLock()
	defer la.mu.RUnlock()

	current := la.currentLeverages[symbol]
	if current == 0 {
		current = la.baseMaxLeverage
	}

	return float64(current) / float64(la.baseMaxLeverage)
}

// GetMetrics returns leverage adapter metrics for monitoring
func (la *LeverageAdapter) GetMetrics(symbol string) map[string]interface{} {
	la.mu.RLock()
	defer la.mu.RUnlock()

	return map[string]interface{}{
		"base_leverage":       la.baseMaxLeverage,
		"current_leverage":    la.currentLeverages[symbol],
		"leverage_multiplier": la.GetLeverageMultiplier(symbol),
		"volatility_pct":      la.GetVolatility(symbol),
		"trend_strength":      la.GetTrendStrength(symbol),
		"price_history_count": len(la.priceHistory[symbol]),
		"last_update":         la.lastUpdateTime[symbol],
	}
}

// Reset clears all historical data for a symbol
func (la *LeverageAdapter) Reset(symbol string) {
	la.mu.Lock()
	defer la.mu.Unlock()

	delete(la.priceHistory, symbol)
	delete(la.currentLeverages, symbol)
	delete(la.lastUpdateTime, symbol)

	la.logger.Info("LeverageAdapter reset for symbol", zap.String("symbol", symbol))
}

// ResetAll clears all historical data
func (la *LeverageAdapter) ResetAll() {
	la.mu.Lock()
	defer la.mu.Unlock()

	la.priceHistory = make(map[string][]PricePoint)
	la.currentLeverages = make(map[string]int)
	la.lastUpdateTime = make(map[string]time.Time)

	la.logger.Info("LeverageAdapter reset for all symbols")
}
