package adaptive_grid

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// TrendState represents the detected trend state
type TrendState int

const (
	TrendStateNeutral TrendState = iota
	TrendStateUp
	TrendStateDown
	TrendStateStrongUp
	TrendStateStrongDown
)

func (t TrendState) String() string {
	switch t {
	case TrendStateStrongUp:
		return "STRONG_UP"
	case TrendStateUp:
		return "UP"
	case TrendStateDown:
		return "DOWN"
	case TrendStateStrongDown:
		return "STRONG_DOWN"
	default:
		return "NEUTRAL"
	}
}

// TrendDetector detects market trends using RSI and other indicators
type TrendDetector struct {
	rsiCalc        *RSICalculator
	prices         []float64
	volumes        []float64
	trendState     TrendState
	trendScore     int
	trendStartTime time.Time
	rsiThresholds  *RSIThresholds
	updateInterval time.Duration
	lastUpdate     time.Time
	logger         *zap.Logger
	mu             sync.RWMutex
}

// RSIThresholds defines RSI levels for trend detection
type RSIThresholds struct {
	StrongOverbought float64 `yaml:"strong_overbought"` // 70
	MildOverbought   float64 `yaml:"mild_overbought"`   // 60
	NeutralHigh      float64 `yaml:"neutral_high"`      // 60
	NeutralLow       float64 `yaml:"neutral_low"`       // 40
	MildOversold     float64 `yaml:"mild_oversold"`     // 40
	StrongOversold   float64 `yaml:"strong_oversold"`   // 30
}

// TrendDetectionConfig holds configuration
type TrendDetectionConfig struct {
	RSIPeriod       int           `yaml:"rsi_period"`       // 14
	UpdateInterval  time.Duration `yaml:"update_interval"`  // 5 minutes
	PersistenceTime time.Duration `yaml:"persistence_time"` // 15 minutes
	Thresholds      RSIThresholds `yaml:"thresholds"`
}

// DefaultTrendDetectionConfig returns default configuration
func DefaultTrendDetectionConfig() *TrendDetectionConfig {
	return &TrendDetectionConfig{
		RSIPeriod:       14,
		UpdateInterval:  5 * time.Minute,
		PersistenceTime: 15 * time.Minute,
		Thresholds: RSIThresholds{
			StrongOverbought: 70,
			MildOverbought:   60,
			NeutralHigh:      60,
			NeutralLow:       40,
			MildOversold:     40,
			StrongOversold:   30,
		},
	}
}

// NewTrendDetector creates new trend detector
func NewTrendDetector(config *TrendDetectionConfig, logger *zap.Logger) *TrendDetector {
	if config == nil {
		config = DefaultTrendDetectionConfig()
	}

	return &TrendDetector{
		rsiCalc:        NewRSICalculator(config.RSIPeriod),
		prices:         make([]float64, 0, 100),
		volumes:        make([]float64, 0, 100),
		trendState:     TrendStateNeutral,
		rsiThresholds:  &config.Thresholds,
		updateInterval: config.UpdateInterval,
		lastUpdate:     time.Now(),
		logger:         logger,
	}
}

// UpdatePrice adds new price data and updates trend detection
func (td *TrendDetector) UpdatePrice(price, volume float64) {
	td.mu.Lock()
	defer td.mu.Unlock()

	td.prices = append(td.prices, price)
	td.volumes = append(td.volumes, volume)

	// Keep only last 100 points
	if len(td.prices) > 100 {
		td.prices = td.prices[1:]
		td.volumes = td.volumes[1:]
	}

	// Update RSI
	td.rsiCalc.AddPrice(price)

	// Check if time to update trend (every 5 minutes)
	if time.Since(td.lastUpdate) >= td.updateInterval {
		td.detectTrend()
		td.lastUpdate = time.Now()
	}
}

// detectTrend determines current trend state
func (td *TrendDetector) detectTrend() {
	if !td.rsiCalc.IsReady() {
		return
	}

	rsi := td.rsiCalc.GetRSI()
	newState := TrendStateNeutral

	switch {
	case rsi > td.rsiThresholds.StrongOverbought:
		newState = TrendStateStrongUp
	case rsi > td.rsiThresholds.MildOverbought:
		newState = TrendStateUp
	case rsi < td.rsiThresholds.StrongOversold:
		newState = TrendStateStrongDown
	case rsi < td.rsiThresholds.MildOversold:
		newState = TrendStateDown
	default:
		newState = TrendStateNeutral
	}

	// Calculate trend score
	td.trendScore = td.calculateTrendScore()

	// Log state change
	if newState != td.trendState {
		duration := ""
		if !td.trendStartTime.IsZero() {
			duration = time.Since(td.trendStartTime).String()
		}

		td.logger.Info("Trend state changed",
			zap.String("from", td.trendState.String()),
			zap.String("to", newState.String()),
			zap.Float64("rsi", rsi),
			zap.Int("trend_score", td.trendScore),
			zap.String("previous_duration", duration))

		td.trendState = newState
		td.trendStartTime = time.Now()
	}
}

// calculateTrendScore calculates trend strength (0-10)
func (td *TrendDetector) calculateTrendScore() int {
	score := 0
	rsi := td.rsiCalc.GetRSI()
	prices := td.prices

	// RSI contribution (0-4 points)
	switch {
	case rsi > 75 || rsi < 25:
		score += 4
	case rsi > 70 || rsi < 30:
		score += 3
	case rsi > 65 || rsi < 35:
		score += 2
	case rsi > 60 || rsi < 40:
		score += 1
	}

	// Price vs EMA contribution (0-2 points)
	if len(prices) >= 20 {
		ema := calculateEMA(prices, 20)
		currentPrice := prices[len(prices)-1]

		if currentPrice > ema*1.02 || currentPrice < ema*0.98 {
			score += 2
		} else if currentPrice > ema*1.01 || currentPrice < ema*0.99 {
			score += 1
		}
	}

	// Volume contribution (0-2 points)
	if len(td.volumes) >= 20 {
		avgVolume := calculateAverage(td.volumes[len(td.volumes)-20:])
		currentVolume := td.volumes[len(td.volumes)-1]

		if currentVolume > avgVolume*3 {
			score += 2
		} else if currentVolume > avgVolume*2 {
			score += 1
		}
	}

	// Higher highs / Lower lows (0-2 points)
	if len(prices) >= 10 {
		higherHighs, lowerLows := td.checkHigherHighsLowerLows(prices[len(prices)-10:])
		if higherHighs || lowerLows {
			score += 2
		}
	}

	return score
}

// checkHigherHighsLowerLows checks for successive higher highs or lower lows
func (td *TrendDetector) checkHigherHighsLowerLows(prices []float64) (higherHighs, lowerLows bool) {
	if len(prices) < 5 {
		return false, false
	}

	hhCount := 0
	llCount := 0

	for i := 2; i < len(prices); i++ {
		// Check for higher high
		if prices[i] > prices[i-1] && prices[i-1] > prices[i-2] {
			hhCount++
		}
		// Check for lower low
		if prices[i] < prices[i-1] && prices[i-1] < prices[i-2] {
			llCount++
		}
	}

	return hhCount >= 3, llCount >= 3
}

// GetTrendState returns current trend state
func (td *TrendDetector) GetTrendState() TrendState {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.trendState
}

// GetRSI returns current RSI value
func (td *TrendDetector) GetRSI() float64 {
	return td.rsiCalc.GetRSI()
}

// GetTrendScore returns current trend score
func (td *TrendDetector) GetTrendScore() int {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.trendScore
}

// ShouldPauseCounterTrend returns true if counter-trend orders should be paused
func (td *TrendDetector) ShouldPauseCounterTrend(side string) bool {
	td.mu.RLock()
	defer td.mu.RUnlock()

	trendState := td.trendState
	trendScore := td.trendScore

	// Strong trend (score >= 4) - pause counter-trend
	if trendScore >= 4 {
		switch trendState {
		case TrendStateStrongUp, TrendStateUp:
			return side == "SHORT" // Pause shorts in uptrend
		case TrendStateStrongDown, TrendStateDown:
			return side == "LONG" // Pause longs in downtrend
		}
	}

	// Mild trend (score 2-3) - reduce counter-trend (handled by size reduction)
	return false
}

// GetTrendAdjustedSize returns size adjusted for trend
func (td *TrendDetector) GetTrendAdjustedSize(baseSize float64, side string) float64 {
	td.mu.RLock()
	trendState := td.trendState
	trendScore := td.trendScore
	td.mu.RUnlock()

	// Determine if this is counter-trend
	isCounterTrend := false
	switch trendState {
	case TrendStateStrongUp, TrendStateUp:
		isCounterTrend = side == "SHORT"
	case TrendStateStrongDown, TrendStateDown:
		isCounterTrend = side == "LONG"
	}

	if !isCounterTrend {
		return baseSize // Pro-trend, no reduction
	}

	// Reduce size for counter-trend
	var reduction float64
	switch trendScore {
	case 2, 3:
		reduction = 0.30 // 30% reduction
	case 4, 5:
		reduction = 0.50 // 50% reduction
	case 6, 7, 8, 9, 10:
		reduction = 0.70 // 70% reduction
	default:
		return baseSize
	}

	return baseSize * (1 - reduction)
}

// IsTrendExhausted checks if trend might be ending (RSI divergence)
func (td *TrendDetector) IsTrendExhausted() bool {
	td.mu.RLock()
	defer td.mu.RUnlock()

	if len(td.prices) < 20 {
		return false
	}

	// Check for RSI divergence
	recentPrices := td.prices[len(td.prices)-20:]

	// This is simplified - real RSI divergence needs proper calculation
	// For now, check if RSI is trending opposite to price
	priceTrend := recentPrices[len(recentPrices)-1] - recentPrices[0]
	rsi := td.rsiCalc.GetRSI()

	// If price is higher but RSI is lower (or vice versa), divergence detected
	if td.trendState == TrendStateStrongUp && priceTrend > 0 && rsi < 65 {
		td.logger.Info("Potential trend exhaustion detected - bullish divergence")
		return true
	}
	if td.trendState == TrendStateStrongDown && priceTrend < 0 && rsi > 35 {
		td.logger.Info("Potential trend exhaustion detected - bearish divergence")
		return true
	}

	return false
}

// GetStatus returns full status
func (td *TrendDetector) GetStatus() map[string]interface{} {
	td.mu.RLock()
	defer td.mu.RUnlock()

	duration := ""
	if !td.trendStartTime.IsZero() {
		duration = time.Since(td.trendStartTime).String()
	}

	return map[string]interface{}{
		"trend_state": td.trendState.String(),
		"trend_score": td.trendScore,
		"rsi":         td.rsiCalc.GetRSI(),
		"duration":    duration,
		"last_update": td.lastUpdate,
		"rsi_ready":   td.rsiCalc.IsReady(),
	}
}

// calculateEMA calculates exponential moving average
func calculateEMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return prices[len(prices)-1]
	}

	multiplier := 2.0 / float64(period+1)
	ema := prices[0]

	for i := 1; i < len(prices); i++ {
		ema = prices[i]*multiplier + ema*(1-multiplier)
	}

	return ema
}

// calculateAverage calculates simple average
func calculateAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// Reset clears all data
func (td *TrendDetector) Reset() {
	td.mu.Lock()
	defer td.mu.Unlock()

	td.rsiCalc.Reset()
	td.prices = td.prices[:0]
	td.volumes = td.volumes[:0]
	td.trendState = TrendStateNeutral
	td.trendScore = 0
	td.trendStartTime = time.Time{}
	td.lastUpdate = time.Now()
}
