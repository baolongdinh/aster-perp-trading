package agentic

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/config"
	"aster-bot/internal/realtime"

	"go.uber.org/zap"
)

// RegimeDetector detects market regime for a single symbol
type RegimeDetector struct {
	symbol     string
	config     config.RegimeDetectionConfig
	calculator *IndicatorCalculator
	marketData realtime.MarketStateProvider
	logger     *zap.Logger

	mu           sync.RWMutex
	candleBuffer []Candle
	lastRegime   RegimeSnapshot
	lastUpdate   time.Time
	atrHistory   []float64 // For detecting ATR spikes
}

// NewRegimeDetector creates a new regime detector for a symbol
func NewRegimeDetector(
	symbol string,
	cfg config.RegimeDetectionConfig,
	marketData realtime.MarketStateProvider,
	logger *zap.Logger,
) *RegimeDetector {
	return &RegimeDetector{
		symbol:       symbol,
		config:       cfg,
		calculator:   NewIndicatorCalculator(cfg.ADXPeriod, cfg.BBPeriod, cfg.ATRPeriod),
		marketData:   marketData,
		logger:       logger.With(zap.String("component", "regime_detector"), zap.String("symbol", symbol)),
		candleBuffer: make([]Candle, 0, 100),
		atrHistory:   make([]float64, 0, 20),
		lastRegime: RegimeSnapshot{
			Regime: RegimeSideways, // Default to sideways
		},
	}
}

// Detect performs regime detection for the symbol
func (rd *RegimeDetector) Detect(ctx context.Context) (RegimeSnapshot, error) {
	// Fetch latest candles
	candles, err := rd.fetchCandles(ctx)
	if err != nil {
		return RegimeSnapshot{}, fmt.Errorf("failed to fetch candles for %s: %w", rd.symbol, err)
	}

	// Update candle buffer
	rd.mu.Lock()
	rd.updateCandleBuffer(candles)
	buffer := make([]Candle, len(rd.candleBuffer))
	copy(buffer, rd.candleBuffer)
	rd.mu.Unlock()

	// Not enough data
	if len(buffer) < rd.config.ADXPeriod {
		return RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.5,
			Timestamp:  time.Now(),
		}, nil
	}

	// Calculate indicators
	values := rd.calculator.CalculateAll(buffer)

	// Detect regime
	regime, confidence := rd.classifyRegime(values)

	snapshot := RegimeSnapshot{
		Regime:     regime,
		ADX:        values.ADX,
		ATR14:      values.ATR14,
		BBWidth:    values.BBWidth,
		Volume24h:  values.Volume24h,
		Timestamp:  time.Now(),
		Confidence: confidence,
	}

	// Update state
	rd.mu.Lock()
	rd.lastRegime = snapshot
	rd.lastUpdate = time.Now()

	// Track ATR for spike detection
	rd.atrHistory = append(rd.atrHistory, values.ATR14)
	if len(rd.atrHistory) > 20 {
		rd.atrHistory = rd.atrHistory[1:]
	}
	rd.mu.Unlock()

	rd.logger.Debug("Regime detected",
		zap.String("regime", string(regime)),
		zap.Float64("confidence", confidence),
		zap.Float64("adx", values.ADX),
		zap.Float64("atr", values.ATR14),
	)

	return snapshot, nil
}

// fetchCandles fetches candle data from the exchange
func (rd *RegimeDetector) fetchCandles(ctx context.Context) ([]Candle, error) {
	if rd.marketData == nil {
		return nil, fmt.Errorf("market state provider is nil")
	}

	// Convert interval string to client interval
	interval := rd.config.CandleInterval
	if interval == "" {
		interval = "5m"
	}

	// Calculate limit (need at least 2x period for accurate ADX)
	limit := rd.config.ADXPeriod * 2
	if limit < 50 {
		limit = 50
	}

	if err := rd.marketData.EnsureKlineSubscription(rd.symbol, interval); err != nil {
		rd.logger.Debug("Failed to ensure kline subscription",
			zap.Error(err),
			zap.String("interval", interval),
		)
	}

	exchangeCandles, err := rd.marketData.GetKlines(ctx, rd.symbol, interval, limit)
	if err != nil {
		return nil, err
	}

	// Convert to our Candle type
	candles := make([]Candle, len(exchangeCandles))
	for i, c := range exchangeCandles {
		candles[i] = Candle{
			Symbol:    rd.symbol,
			Open:      c.Open,
			High:      c.High,
			Low:       c.Low,
			Close:     c.Close,
			Volume:    c.Volume,
			Timestamp: time.Unix(c.EndTime/1000, 0),
		}
	}

	return candles, nil
}

// updateCandleBuffer updates the internal candle buffer
func (rd *RegimeDetector) updateCandleBuffer(newCandles []Candle) {
	if len(newCandles) == 0 {
		return
	}

	// Create a map of existing timestamps for deduplication
	existing := make(map[int64]int) // timestamp -> index
	for i, c := range rd.candleBuffer {
		existing[c.Timestamp.Unix()] = i
	}

	// Add or update candles
	for _, newCandle := range newCandles {
		ts := newCandle.Timestamp.Unix()
		if idx, ok := existing[ts]; ok {
			// Update existing
			rd.candleBuffer[idx] = newCandle
		} else {
			// Add new
			rd.candleBuffer = append(rd.candleBuffer, newCandle)
		}
	}

	// Sort by timestamp
	for i := 0; i < len(rd.candleBuffer); i++ {
		for j := i + 1; j < len(rd.candleBuffer); j++ {
			if rd.candleBuffer[j].Timestamp.Before(rd.candleBuffer[i].Timestamp) {
				rd.candleBuffer[i], rd.candleBuffer[j] = rd.candleBuffer[j], rd.candleBuffer[i]
			}
		}
	}

	// Keep only necessary history (max 100 candles)
	maxSize := 100
	if len(rd.candleBuffer) > maxSize {
		rd.candleBuffer = rd.candleBuffer[len(rd.candleBuffer)-maxSize:]
	}
}

// classifyRegime determines the market regime based on indicator values
func (rd *RegimeDetector) classifyRegime(values *IndicatorValues) (RegimeType, float64) {
	thresholds := rd.config.Thresholds

	// Check for volatile regime first (ATR spike)
	if rd.isATRSpike(values.ATR14) {
		confidence := rd.calculateConfidence(values, RegimeVolatile)
		return RegimeVolatile, confidence
	}

	// Check for trending regime (high ADX)
	if values.ADX > thresholds.TrendingADXMin {
		confidence := rd.calculateConfidence(values, RegimeTrending)
		return RegimeTrending, confidence
	}

	// Check for sideways regime (low ADX)
	if values.ADX < thresholds.SidewaysADXMax {
		confidence := rd.calculateConfidence(values, RegimeSideways)
		return RegimeSideways, confidence
	}

	// Default to recovery (transition period)
	confidence := 0.5
	return RegimeRecovery, confidence
}

// isATRSpike checks if current ATR is significantly higher than average
func (rd *RegimeDetector) isATRSpike(currentATR float64) bool {
	if len(rd.atrHistory) < 5 {
		return false
	}

	// Calculate average ATR (excluding the last value)
	sum := 0.0
	for i := 0; i < len(rd.atrHistory)-1; i++ {
		sum += rd.atrHistory[i]
	}
	avgATR := sum / float64(len(rd.atrHistory)-1)

	if avgATR == 0 {
		return false
	}

	// Check if current ATR is N times higher than average
	threshold := rd.config.Thresholds.VolatileATRSpike
	return currentATR > avgATR*threshold
}

// calculateConfidence calculates how confident we are in the regime classification
func (rd *RegimeDetector) calculateConfidence(values *IndicatorValues, regime RegimeType) float64 {
	switch regime {
	case RegimeTrending:
		// Higher ADX = higher confidence in trending
		// ADX 25 = 0.5, ADX 50 = 1.0
		return min(1.0, 0.5+(values.ADX-25)/50)

	case RegimeSideways:
		// Lower ADX = higher confidence in sideways
		// ADX 0 = 1.0, ADX 25 = 0.5
		return max(0.5, 1.0-values.ADX/50)

	case RegimeVolatile:
		// Higher ATR spike = higher confidence
		// Use BB width as confirmation
		if values.BBWidth > 0.02 {
			return 0.8
		}
		return 0.6

	case RegimeRecovery:
		return 0.5

	default:
		return 0.5
	}
}

// GetLastRegime returns the last detected regime
func (rd *RegimeDetector) GetLastRegime() RegimeSnapshot {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return rd.lastRegime
}

// GetLastUpdate returns the time of last update
func (rd *RegimeDetector) GetLastUpdate() time.Time {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return rd.lastUpdate
}

// IsStale checks if the regime data is stale
func (rd *RegimeDetector) IsStale(maxAge time.Duration) bool {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return time.Since(rd.lastUpdate) > maxAge
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
