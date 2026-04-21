package agentic

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SignalAggregator collects and aggregates trading signals from multiple sources
// This provides real signal values for state handlers to make decisions
type SignalAggregator struct {
	mu     sync.RWMutex
	logger *zap.Logger

	// Price history for calculations
	priceHistory map[string][]Candle // Symbol -> recent candles
	maxHistory   int

	// Calculated signals (0-1 scale)
	signals map[string]*SymbolSignals
}

// SymbolSignals holds all signals for a symbol
type SymbolSignals struct {
	Symbol               string
	Timestamp            time.Time
	MeanReversionSignals float64 // BB Bounce, RSI Divergence
	FVGSignal            float64 // Fair Value Gap
	LiquiditySignal      float64 // Liquidity sweep
	BreakoutSignal       float64 // Range breakout
	MomentumSignal       float64 // ROC + velocity
	VolumeConfirm        float64 // Volume spike
}

// NewSignalAggregator creates a new signal aggregator
func NewSignalAggregator(logger *zap.Logger) *SignalAggregator {
	return &SignalAggregator{
		logger:       logger.With(zap.String("component", "signal_aggregator")),
		priceHistory: make(map[string][]Candle),
		maxHistory:   100,
		signals:      make(map[string]*SymbolSignals),
	}
}

// UpdatePrice adds a new price candle and recalculates signals
func (sa *SignalAggregator) UpdatePrice(candle Candle) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	symbol := candle.Symbol
	history := sa.priceHistory[symbol]
	history = append(history, candle)
	if len(history) > sa.maxHistory {
		history = history[len(history)-sa.maxHistory:]
	}
	sa.priceHistory[symbol] = history

	// Recalculate signals if we have enough data
	if len(history) >= 20 {
		signals := sa.calculateSignals(symbol, history)
		sa.signals[symbol] = signals
	}
}

// SeedHistoricalData preloads historical candles for signal calculation
// This should be called on startup with fetched kline history
func (sa *SignalAggregator) SeedHistoricalData(symbol string, candles []Candle) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	if len(candles) == 0 {
		return
	}

	// Store candles (limited to maxHistory)
	if len(candles) > sa.maxHistory {
		candles = candles[len(candles)-sa.maxHistory:]
	}
	sa.priceHistory[symbol] = candles

	// Calculate signals immediately
	signals := sa.calculateSignals(symbol, candles)
	sa.signals[symbol] = signals

	sa.logger.Info("Historical data seeded for signal calculation",
		zap.String("symbol", symbol),
		zap.Int("candles_loaded", len(candles)),
		zap.Float64("mean_reversion", signals.MeanReversionSignals),
		zap.Float64("fvg", signals.FVGSignal),
		zap.Float64("momentum", signals.MomentumSignal),
	)
}

// GetSignals returns current signals for a symbol
func (sa *SignalAggregator) GetSignals(symbol string) *SymbolSignals {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if signals, ok := sa.signals[symbol]; ok {
		sa.logger.Debug("Signals retrieved from map",
			zap.String("symbol", symbol),
			zap.Float64("mean_reversion", signals.MeanReversionSignals),
			zap.Float64("fvg", signals.FVGSignal),
		)
		return signals
	}

	// Log when signals not found (for debugging)
	sa.logger.Debug("No signals found for symbol, returning empty",
		zap.String("symbol", symbol),
		zap.Int("available_symbols", len(sa.signals)),
	)
	return &SymbolSignals{
		Symbol:    symbol,
		Timestamp: time.Now(),
	}
}

// calculateSignals calculates all signals from price history
func (sa *SignalAggregator) calculateSignals(symbol string, candles []Candle) *SymbolSignals {
	prices := extractCloses(candles)
	highs := extractHighs(candles)
	lows := extractLows(candles)
	volumes := extractVolumes(candles)

	currentPrice := prices[len(prices)-1]

	return &SymbolSignals{
		Symbol:               symbol,
		Timestamp:            time.Now(),
		MeanReversionSignals: sa.calculateMeanReversion(prices, highs, lows, currentPrice),
		FVGSignal:            sa.calculateFVG(candles),
		LiquiditySignal:      sa.calculateLiquidity(highs, lows, currentPrice),
		BreakoutSignal:       sa.calculateBreakout(prices, highs, lows),
		MomentumSignal:       sa.calculateMomentum(prices),
		VolumeConfirm:        sa.calculateVolumeConfirm(volumes),
	}
}

// calculateMeanReversion detects mean reversion signals (BB bounce, etc.)
func (sa *SignalAggregator) calculateMeanReversion(prices, highs, lows []float64, currentPrice float64) float64 {
	if len(prices) < 20 {
		return 0.5 // Neutral if insufficient data
	}

	// Calculate Bollinger Bands
	sma := calculateSMA(prices, 20)
	stdDev := calculateStdDev(prices, 20, sma)
	upperBand := sma + 2*stdDev
	lowerBand := sma - 2*stdDev

	// Distance from bands (0 = at middle, 1 = at/touching band)
	bbWidth := upperBand - lowerBand
	if bbWidth == 0 {
		return 0.5
	}

	// Distance from middle band normalized
	distanceFromMiddle := math.Abs(currentPrice-sma) / (bbWidth / 2)
	distanceFromMiddle = math.Min(1.0, distanceFromMiddle) // Cap at 1

	// Strong mean reversion signal when price near band edges
	// This indicates potential bounce
	if currentPrice > upperBand*0.99 || currentPrice < lowerBand*1.01 {
		return 0.8 // Strong mean reversion opportunity
	}

	// Moderate signal when price moves away from middle
	return 0.5 + distanceFromMiddle*0.3
}

// calculateFVG detects Fair Value Gap patterns
func (sa *SignalAggregator) calculateFVG(candles []Candle) float64 {
	if len(candles) < 3 {
		return 0.5
	}

	// Check last 3 candles for FVG pattern
	c1 := candles[len(candles)-3]
	c3 := candles[len(candles)-1]

	// Bullish FVG: c1 high < c3 low (gap up)
	bullishFVG := c1.High < c3.Low
	// Bearish FVG: c1 low > c3 high (gap down)
	bearishFVG := c1.Low > c3.High

	if bullishFVG || bearishFVG {
		// Check if price is returning to fill the gap
		currentPrice := c3.Close
		if bullishFVG && currentPrice < c3.Low && currentPrice > c1.High {
			return 0.9 // Strong FVG fill signal
		}
		if bearishFVG && currentPrice > c3.High && currentPrice < c1.Low {
			return 0.9 // Strong FVG fill signal
		}
		return 0.7 // FVG detected but not in fill zone
	}

	return 0.5
}

// calculateLiquidity detects liquidity sweep patterns
func (sa *SignalAggregator) calculateLiquidity(highs, lows []float64, currentPrice float64) float64 {
	if len(highs) < 10 || len(lows) < 10 {
		return 0.5
	}

	// Find recent swing highs and lows
	recentHighs := highs[len(highs)-10:]
	recentLows := lows[len(lows)-10:]

	swingHigh := sliceMax(recentHighs)
	swingLow := sliceMin(recentLows)

	// Check for liquidity sweep (price briefly beyond swing points then reversing)
	sweepRange := swingHigh - swingLow
	if sweepRange == 0 {
		return 0.5
	}

	// Distance from swing points
	distFromHigh := (swingHigh - currentPrice) / sweepRange
	distFromLow := (currentPrice - swingLow) / sweepRange

	// Strong signal when price is at liquidity level
	if distFromHigh < 0.02 || distFromLow < 0.02 {
		return 0.85 // At liquidity level
	}

	// Moderate signal near liquidity
	if distFromHigh < 0.1 || distFromLow < 0.1 {
		return 0.7
	}

	return 0.5
}

// calculateBreakout detects range breakout patterns
func (sa *SignalAggregator) calculateBreakout(prices, highs, lows []float64) float64 {
	if len(prices) < 20 {
		return 0.5
	}

	// Calculate recent range
	recentHighs := highs[len(highs)-20:]
	recentLows := lows[len(lows)-20:]

	rangeHigh := sliceMax(recentHighs)
	rangeLow := sliceMin(recentLows)
	rangeWidth := rangeHigh - rangeLow

	if rangeWidth == 0 {
		return 0.5
	}

	currentPrice := prices[len(prices)-1]

	// Distance from range boundaries
	distFromHigh := (rangeHigh - currentPrice) / rangeWidth
	distFromLow := (currentPrice - rangeLow) / rangeWidth

	// Breakout signal when price breaks range
	if distFromHigh < 0 {
		return 0.9 // Breakout above
	}
	if distFromLow < 0 {
		return 0.9 // Breakout below
	}

	// Near breakout levels
	if distFromHigh < 0.05 || distFromLow < 0.05 {
		return 0.75
	}

	return 0.5
}

// calculateMomentum calculates momentum strength
func (sa *SignalAggregator) calculateMomentum(prices []float64) float64 {
	if len(prices) < 10 {
		return 0.5
	}

	// Short-term vs long-term momentum
	shortMA := calculateSMA(prices, 5)
	longMA := calculateSMA(prices, 20)

	if longMA == 0 {
		return 0.5
	}

	momentum := (shortMA - longMA) / longMA

	// Normalize to 0-1 range (typical momentum range -0.1 to +0.1)
	normalized := 0.5 + momentum*5
	normalized = math.Max(0, math.Min(1, normalized))

	return normalized
}

// calculateVolumeConfirm detects volume confirmation
func (sa *SignalAggregator) calculateVolumeConfirm(volumes []float64) float64 {
	if len(volumes) < 10 {
		return 0.5
	}

	// Current volume vs average
	currentVol := volumes[len(volumes)-1]
	avgVol := calculateSMA(volumes, 10)

	if avgVol == 0 {
		return 0.5
	}

	volRatio := currentVol / avgVol

	// Strong confirmation when volume is 2x average
	if volRatio > 2.0 {
		return 0.9
	}
	if volRatio > 1.5 {
		return 0.75
	}
	if volRatio > 1.0 {
		return 0.6
	}

	return 0.5
}

// Helper functions
func extractCloses(candles []Candle) []float64 {
	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}
	return closes
}

func extractHighs(candles []Candle) []float64 {
	highs := make([]float64, len(candles))
	for i, c := range candles {
		highs[i] = c.High
	}
	return highs
}

func extractLows(candles []Candle) []float64 {
	lows := make([]float64, len(candles))
	for i, c := range candles {
		lows[i] = c.Low
	}
	return lows
}

func extractVolumes(candles []Candle) []float64 {
	volumes := make([]float64, len(candles))
	for i, c := range candles {
		volumes[i] = c.Volume
	}
	return volumes
}

func calculateSMA(values []float64, period int) float64 {
	if len(values) == 0 || period <= 0 {
		return 0
	}
	start := len(values) - period
	if start < 0 {
		start = 0
	}
	sum := 0.0
	for i := start; i < len(values); i++ {
		sum += values[i]
	}
	return sum / float64(len(values)-start)
}

func calculateStdDev(values []float64, period int, mean float64) float64 {
	if len(values) < period {
		return 0
	}
	start := len(values) - period
	sum := 0.0
	for i := start; i < len(values); i++ {
		diff := values[i] - mean
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(period))
}

func sliceMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func sliceMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
