package market_regime

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RegimeDetector analyzes price history to determine market conditions
type RegimeDetector struct {
	config        *RegimeDetectorConfig
	priceHistory  map[string][]PricePoint // Symbol -> historical prices with highs/lows
	currentRegime map[string]MarketRegime // Symbol -> current regime
	confidence    map[string]float64      // Symbol -> detection confidence
	lastUpdate    map[string]time.Time    // Symbol -> last detection time
	atrValues     map[string][]float64    // Symbol -> ATR history
	mu            sync.RWMutex            // Thread safety
	logger        *zap.Logger             // Logger for regime transitions
}

// NewRegimeDetector creates a new regime detector instance
func NewRegimeDetector(logger *zap.Logger, cfg *RegimeDetectorConfig) *RegimeDetector {
	if cfg == nil {
		cfg = DefaultRegimeDetectorConfig()
	}

	return &RegimeDetector{
		config:        cfg,
		priceHistory:  make(map[string][]PricePoint),
		currentRegime: make(map[string]MarketRegime),
		confidence:    make(map[string]float64),
		lastUpdate:    make(map[string]time.Time),
		atrValues:     make(map[string][]float64),
		mu:            sync.RWMutex{},
		logger:        logger,
	}
}

// DetectRegime analyzes current market conditions for a symbol
func (d *RegimeDetector) DetectRegime(symbol string, currentPrice float64) MarketRegime {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Add current price to history (use same value for high/low/close for tick data)
	prices := d.priceHistory[symbol]
	prices = append(prices, PricePoint{
		High:      currentPrice,
		Low:       currentPrice,
		Close:     currentPrice,
		Timestamp: time.Now(),
	})
	if len(prices) > 100 {
		prices = prices[len(prices)-100:]
	}
	d.priceHistory[symbol] = prices

	// Only update if enough time has passed
	if last, exists := d.lastUpdate[symbol]; exists {
		if time.Since(last) < time.Duration(d.config.UpdateIntervalSec)*time.Second {
			// Return existing regime if not time to update
			if regime, ok := d.currentRegime[symbol]; ok {
				return regime
			}
			return RegimeUnknown
		}
	}

	return d.detectAndUpdateRegime(symbol)
}

// detectAndUpdateRegime implements hybrid ATR + momentum detection
func (d *RegimeDetector) detectAndUpdateRegime(symbol string) MarketRegime {
	prices := d.priceHistory[symbol]
	if len(prices) < d.config.MomentumLong+5 {
		// Not enough data
		d.currentRegime[symbol] = RegimeUnknown
		return RegimeUnknown
	}

	// Calculate ATR (Average True Range)
	atr := d.calculateATR(prices, d.config.ATRPeriod)
	d.atrValues[symbol] = append(d.atrValues[symbol], atr)
	if len(d.atrValues[symbol]) > 50 {
		d.atrValues[symbol] = d.atrValues[symbol][1:]
	}

	// Get average ATR as percentage of current price
	currentPrice := prices[len(prices)-1].Close
	atrPct := 0.0
	if currentPrice > 0 {
		atrPct = (atr / currentPrice) * 100
	}

	// Calculate momentum
	momentum := d.calculateMomentum(prices, d.config.MomentumShort, d.config.MomentumLong)

	// Calculate trend strength
	trendStrength := d.calculateTrendStrength(prices, d.config.MomentumLong)

	// Detect regime using hybrid method
	newRegime := d.classifyRegime(atrPct, momentum, trendStrength)

	// Update confidence based on consistency
	confidence := d.calculateConfidence(symbol, newRegime)
	d.confidence[symbol] = confidence
	d.lastUpdate[symbol] = time.Now()

	// Check if regime changed
	oldRegime, exists := d.currentRegime[symbol]
	if !exists || oldRegime != newRegime {
		d.currentRegime[symbol] = newRegime

		if d.logger != nil {
			d.logger.Info("Regime transition detected",
				zap.String("symbol", symbol),
				zap.String("from", string(oldRegime)),
				zap.String("to", string(newRegime)),
				zap.Float64("atr_pct", atrPct),
				zap.Float64("momentum", momentum),
				zap.Float64("trend_strength", trendStrength),
				zap.Float64("confidence", confidence),
				zap.Time("timestamp", d.lastUpdate[symbol]))
		}
	}

	return newRegime
}

// GetCurrentRegime returns the current regime for a symbol
func (d *RegimeDetector) GetCurrentRegime(symbol string) MarketRegime {
	d.mu.RLock()
	defer d.mu.RUnlock()

	regime, exists := d.currentRegime[symbol]
	if !exists {
		regime = RegimeUnknown
	}

	return regime
}

// RegimeDetectorConfig holds configuration for regime detection
type RegimeDetectorConfig struct {
	Method            string  // "hybrid", "atr_only", "momentum_only"
	ATRPeriod         int     // Period for ATR calculation
	MomentumShort     int     // Short period for momentum
	MomentumLong      int     // Long period for momentum
	UpdateIntervalSec int     // Seconds between updates
	ATRThresholdLow   float64 // ATR threshold for low volatility
	ATRThresholdHigh  float64 // ATR threshold for high volatility
	TrendThreshold    float64 // Threshold to detect trending
}

// DefaultRegimeDetectorConfig returns default config matching adaptive_config.yaml
func DefaultRegimeDetectorConfig() *RegimeDetectorConfig {
	return &RegimeDetectorConfig{
		Method:            "hybrid",
		ATRPeriod:         7,
		MomentumShort:     10,
		MomentumLong:      20,
		UpdateIntervalSec: 60,
		ATRThresholdLow:   0.3,
		ATRThresholdHigh:  0.8,
		TrendThreshold:    0.02,
	}
}

// PricePoint represents a price candle for ATR calculation
type PricePoint struct {
	High      float64
	Low       float64
	Close     float64
	Timestamp time.Time
}

// calculateATR calculates Average True Range
func (d *RegimeDetector) calculateATR(prices []PricePoint, period int) float64 {
	if len(prices) < 2 {
		return 0
	}

	var trSum float64
	count := 0

	start := len(prices) - period
	if start < 1 {
		start = 1
	}

	for i := start; i < len(prices); i++ {
		current := prices[i]
		previous := prices[i-1]

		// True Range = max(high-low, |high-previous_close|, |low-previous_close|)
		range1 := current.High - current.Low
		range2 := math.Abs(current.High - previous.Close)
		range3 := math.Abs(current.Low - previous.Close)

		tr := math.Max(range1, math.Max(range2, range3))
		trSum += tr
		count++
	}

	if count == 0 {
		return 0
	}

	return trSum / float64(count)
}

// calculateMomentum calculates momentum as difference between short and long MA
func (d *RegimeDetector) calculateMomentum(prices []PricePoint, shortPeriod, longPeriod int) float64 {
	if len(prices) < longPeriod {
		return 0
	}

	// Calculate short MA
	shortMA := d.calculateMA(prices, shortPeriod)
	longMA := d.calculateMA(prices, longPeriod)

	if longMA == 0 {
		return 0
	}

	// Return momentum as percentage
	return (shortMA - longMA) / longMA
}

// calculateMA calculates simple moving average of closes
func (d *RegimeDetector) calculateMA(prices []PricePoint, period int) float64 {
	if len(prices) < period {
		return 0
	}

	sum := 0.0
	for i := len(prices) - period; i < len(prices); i++ {
		sum += prices[i].Close
	}

	return sum / float64(period)
}

// calculateTrendStrength using ADX-like calculation
func (d *RegimeDetector) calculateTrendStrength(prices []PricePoint, period int) float64 {
	if len(prices) < period+1 {
		return 0
	}

	plusDM := 0.0
	minusDM := 0.0
	count := 0

	start := len(prices) - period
	if start < 1 {
		start = 1
	}

	for i := start; i < len(prices); i++ {
		current := prices[i]
		previous := prices[i-1]

		upMove := current.High - previous.High
		downMove := previous.Low - current.Low

		if upMove > downMove && upMove > 0 {
			plusDM += upMove
		} else if downMove > upMove && downMove > 0 {
			minusDM += downMove
		}
		count++
	}

	if count == 0 {
		return 0
	}

	plusDI := plusDM / float64(count)
	minusDI := minusDM / float64(count)

	diff := math.Abs(plusDI - minusDI)
	sum := plusDI + minusDI

	if sum == 0 {
		return 0
	}

	// Return trend strength 0-1
	return diff / sum
}

// classifyRegime determines the market regime based on ATR and momentum
func (d *RegimeDetector) classifyRegime(atrPct, momentum, trendStrength float64) MarketRegime {
	// High volatility first - check ATR
	if atrPct > d.config.ATRThresholdHigh {
		return RegimeVolatile
	}

	// Check for trending (strong momentum + trend strength)
	if math.Abs(momentum) > d.config.TrendThreshold && trendStrength > 0.5 {
		return RegimeTrending
	}

	// Low ATR and no strong trend = ranging/sideways
	if atrPct < d.config.ATRThresholdLow && math.Abs(momentum) < d.config.TrendThreshold/2 {
		return RegimeRanging
	}

	// Default to ranging for medium cases
	return RegimeRanging
}

// calculateConfidence calculates confidence level based on consistency
func (d *RegimeDetector) calculateConfidence(symbol string, newRegime MarketRegime) float64 {
	atrHistory := d.atrValues[symbol]
	if len(atrHistory) < 5 {
		return 0.5
	}

	// Calculate ATR variance (lower = higher confidence)
	mean := 0.0
	for _, atr := range atrHistory {
		mean += atr
	}
	mean /= float64(len(atrHistory))

	if mean == 0 {
		return 0.5
	}

	variance := 0.0
	for _, atr := range atrHistory {
		diff := atr - mean
		variance += diff * diff
	}
	variance /= float64(len(atrHistory))
	stdDev := math.Sqrt(variance)

	// Normalize to confidence 0-1
	confidence := 1.0 - math.Min(stdDev/mean, 1.0)
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	return confidence
}

// GetConfidence returns the confidence level for current regime
func (d *RegimeDetector) GetConfidence(symbol string) float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	confidence, exists := d.confidence[symbol]
	if !exists {
		return 0
	}

	return confidence
}

// GetRegimeInfo returns full regime information
func (d *RegimeDetector) GetRegimeInfo(symbol string) map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]interface{}{
		"regime":       string(d.currentRegime[symbol]),
		"confidence":   d.confidence[symbol],
		"last_update":  d.lastUpdate[symbol],
		"price_points": len(d.priceHistory[symbol]),
	}
}

// Reset clears all data for a symbol
func (d *RegimeDetector) Reset(symbol string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.priceHistory, symbol)
	delete(d.currentRegime, symbol)
	delete(d.confidence, symbol)
	delete(d.lastUpdate, symbol)
	delete(d.atrValues, symbol)
}
