package maker

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MarketAnalyzer provides deep market analysis for adaptive configuration
type MarketAnalyzer struct {
	logger     *zap.Logger
	httpClient FuturesClientInterface
	wsClient   WebSocketClientInterface

	// Historical data storage
	priceHistory    map[string][]PricePoint
	spreadHistory   map[string][]float64
	volumeHistory   map[string][]float64
	volatilityCache map[string]float64
	mu              sync.RWMutex

	// Analysis parameters
	historyLength    int
	updateInterval   time.Duration
	volatilityWindow time.Duration
}

// PricePoint represents a price data point with timestamp
type PricePoint struct {
	Price     float64   `json:"price"`
	Timestamp time.Time `json:"timestamp"`
	Volume    float64   `json:"volume"`
}

// MarketMetrics contains comprehensive market analysis
type MarketMetrics struct {
	Symbol string `json:"symbol"`

	// Current market state
	CurrentPrice  float64 `json:"current_price"`
	CurrentSpread float64 `json:"current_spread"`
	CurrentVolume float64 `json:"current_volume"`

	// Volatility metrics
	Volatility1m  float64 `json:"volatility_1m"`
	Volatility5m  float64 `json:"volatility_5m"`
	Volatility15m float64 `json:"volatility_15m"`
	Volatility1h  float64 `json:"volatility_1h"`
	Volatility24h float64 `json:"volatility_24h"`
	AvgVolatility float64 `json:"avg_volatility"`

	// Trend metrics
	PriceTrend1h  float64 `json:"price_trend_1h"`  // Price change % in 1h
	PriceTrend6h  float64 `json:"price_trend_6h"`  // Price change % in 6h
	PriceTrend24h float64 `json:"price_trend_24h"` // Price change % in 24h
	MomentumScore float64 `json:"momentum_score"`  // Combined momentum indicator

	// Liquidity metrics
	LiquidityScore     float64 `json:"liquidity_score"`
	OrderBookImbalance float64 `json:"order_book_imbalance"`
	MarketDepth        int     `json:"market_depth"`
	SpreadStability    float64 `json:"spread_stability"`

	// Volume metrics
	VolumeProfile         *VolumeProfile `json:"volume_profile"`
	VolumeTrend           float64        `json:"volume_trend"`
	VolatilityVolumeRatio float64        `json:"volatility_volume_ratio"`

	// Risk metrics
	RiskScore       float64 `json:"risk_score"`
	MarketRegime    string  `json:"market_regime"` // "trending", "ranging", "volatile"
	ConfidenceLevel float64 `json:"confidence_level"`

	LastUpdated time.Time `json:"last_updated"`
}

// VolumeProfile analyzes volume distribution across price levels
type VolumeProfile struct {
	POCs           []float64 `json:"pocs"` // Point of Control levels
	HighVolumeNode float64   `json:"high_volume_node"`
	LowVolumeNode  float64   `json:"low_volume_node"`
	ValueArea      struct {
		High  float64 `json:"high"`
		Low   float64 `json:"low"`
		Range float64 `json:"range"`
	} `json:"value_area"`
}

func NewMarketAnalyzer(
	httpClient FuturesClientInterface,
	wsClient WebSocketClientInterface,
	logger *zap.Logger,
) *MarketAnalyzer {
	return &MarketAnalyzer{
		logger:           logger,
		httpClient:       httpClient,
		wsClient:         wsClient,
		priceHistory:     make(map[string][]PricePoint),
		spreadHistory:    make(map[string][]float64),
		volumeHistory:    make(map[string][]float64),
		volatilityCache:  make(map[string]float64),
		historyLength:    1000, // Keep last 1000 price points
		updateInterval:   30 * time.Second,
		volatilityWindow: 24 * time.Hour,
	}
}

// Start begins market analysis
func (m *MarketAnalyzer) Start(ctx context.Context) error {
	m.logger.Info("Starting Market Analyzer")

	// Initialize data collection
	go m.dataCollectionLoop(ctx)

	// Start analysis calculations
	go m.analysisLoop(ctx)

	return nil
}

// AnalyzeMarket returns comprehensive market metrics for a symbol
func (m *MarketAnalyzer) AnalyzeMarket(symbol string) (*MarketMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get current market data
	bestBid, bestAsk, volume24h, err := m.wsClient.GetTickerData(symbol)
	if err != nil {
		return nil, err
	}

	currentPrice := (bestBid + bestAsk) / 2
	currentSpread := bestAsk - bestBid

	// Calculate volatility metrics
	volatilityMetrics := m.calculateVolatilityMetrics(symbol)

	// Calculate trend metrics
	trendMetrics := m.calculateTrendMetrics(symbol, currentPrice)

	// Calculate liquidity metrics
	liquidityMetrics := m.calculateLiquidityMetrics(symbol, currentSpread, volume24h)

	// Calculate volume metrics
	volumeMetrics := m.calculateVolumeMetrics(symbol, volume24h)

	// Calculate risk metrics
	riskMetrics := m.calculateRiskMetrics(symbol, volatilityMetrics, liquidityMetrics, trendMetrics)

	// Determine market regime
	marketRegime := m.determineMarketRegime(trendMetrics, volatilityMetrics)

	// Calculate confidence level
	confidence := m.calculateConfidenceLevel(symbol, volatilityMetrics, liquidityMetrics)

	// Create volume profile for VolumeProfile field
	profile := &VolumeProfile{
		POCs:           []float64{volume24h * 0.8}, // Simplified
		HighVolumeNode: volume24h,
		LowVolumeNode:  volume24h * 0.2,
	}
	profile.ValueArea.High = volume24h * 1.2
	profile.ValueArea.Low = volume24h * 0.8
	profile.ValueArea.Range = profile.ValueArea.High - profile.ValueArea.Low

	metrics := &MarketMetrics{
		Symbol:                symbol,
		CurrentPrice:          currentPrice,
		CurrentSpread:         currentSpread,
		CurrentVolume:         volume24h,
		Volatility1m:          volatilityMetrics["1m"],
		Volatility5m:          volatilityMetrics["5m"],
		Volatility15m:         volatilityMetrics["15m"],
		Volatility1h:          volatilityMetrics["1h"],
		Volatility24h:         volatilityMetrics["24h"],
		AvgVolatility:         volatilityMetrics["avg"],
		PriceTrend1h:          trendMetrics["1h"],
		PriceTrend6h:          trendMetrics["6h"],
		PriceTrend24h:         trendMetrics["24h"],
		MomentumScore:         trendMetrics["momentum"],
		LiquidityScore:        liquidityMetrics["score"],
		OrderBookImbalance:    liquidityMetrics["imbalance"],
		MarketDepth:           int(liquidityMetrics["depth"]),
		SpreadStability:       liquidityMetrics["stability"],
		VolumeProfile:         profile,
		VolumeTrend:           volumeMetrics["trend"],
		VolatilityVolumeRatio: volumeMetrics["vol_ratio"],
		RiskScore:             riskMetrics["score"],
		MarketRegime:          marketRegime,
		ConfidenceLevel:       confidence,
		LastUpdated:           time.Now(),
	}

	return metrics, nil
}

// dataCollectionLoop continuously collects market data
func (m *MarketAnalyzer) dataCollectionLoop(ctx context.Context) {
	ticker := time.NewTicker(m.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.collectMarketData(ctx)
		}
	}
}

// analysisLoop performs periodic market analysis
func (m *MarketAnalyzer) analysisLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute) // Analyze every minute
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.performAnalysis(ctx)
		}
	}
}

// collectMarketData gathers current market data
func (m *MarketAnalyzer) collectMarketData(ctx context.Context) {
	// This would integrate with exchange APIs to collect:
	// - Ticker data
	// - Order book snapshots
	// - Recent trades
	// - Volume data

	// For now, simulate with WebSocket data
	// TODO: Implement actual exchange API calls
}

// performAnalysis runs analysis on all tracked symbols
func (m *MarketAnalyzer) performAnalysis(ctx context.Context) {
	m.mu.RLock()
	symbols := make([]string, 0, len(m.priceHistory))
	for symbol := range m.priceHistory {
		symbols = append(symbols, symbol)
	}
	m.mu.RUnlock()

	for _, symbol := range symbols {
		metrics, err := m.AnalyzeMarket(symbol)
		if err != nil {
			m.logger.Error("Failed to analyze market",
				zap.String("symbol", symbol),
				zap.Error(err))
			continue
		}

		m.logger.Debug("Market analysis completed",
			zap.String("symbol", symbol),
			zap.Float64("volatility_24h", metrics.Volatility24h),
			zap.Float64("liquidity_score", metrics.LiquidityScore),
			zap.Float64("risk_score", metrics.RiskScore),
			zap.String("market_regime", metrics.MarketRegime))
	}
}

// calculateVolatilityMetrics computes volatility across multiple timeframes
func (m *MarketAnalyzer) calculateVolatilityMetrics(symbol string) map[string]float64 {
	m.mu.RLock()
	prices := m.priceHistory[symbol]
	m.mu.RUnlock()

	if len(prices) < 2 {
		return map[string]float64{
			"1m": 0.01, "5m": 0.01, "15m": 0.01,
			"1h": 0.01, "24h": 0.01, "avg": 0.01,
		}
	}

	now := time.Now()
	metrics := make(map[string]float64)

	// Calculate volatility for different timeframes
	timeframes := []struct {
		name     string
		duration time.Duration
	}{
		{"1m", 1 * time.Minute},
		{"5m", 5 * time.Minute},
		{"15m", 15 * time.Minute},
		{"1h", 1 * time.Hour},
		{"24h", 24 * time.Hour},
	}

	var totalVolatility float64
	count := 0

	for _, tf := range timeframes {
		volatility := m.calculateVolatilityInTimeframe(prices, now, tf.duration)
		metrics[tf.name] = volatility
		totalVolatility += volatility
		count++
	}

	metrics["avg"] = totalVolatility / float64(count)

	return metrics
}

// calculateVolatilityInTimeframe calculates volatility for specific timeframe
func (m *MarketAnalyzer) calculateVolatilityInTimeframe(prices []PricePoint, now time.Time, timeframe time.Duration) float64 {
	cutoff := now.Add(-timeframe)
	var relevantPrices []float64

	for _, point := range prices {
		if point.Timestamp.After(cutoff) {
			relevantPrices = append(relevantPrices, point.Price)
		}
	}

	if len(relevantPrices) < 2 {
		return 0.01 // Default low volatility
	}

	// Calculate standard deviation
	mean := m.calculateMean(relevantPrices)
	variance := m.calculateVariance(relevantPrices, mean)
	return math.Sqrt(variance) / mean // Coefficient of variation
}

// calculateTrendMetrics computes price trends and momentum
func (m *MarketAnalyzer) calculateTrendMetrics(symbol string, currentPrice float64) map[string]float64 {
	m.mu.RLock()
	prices := m.priceHistory[symbol]
	m.mu.RUnlock()

	if len(prices) < 2 {
		return map[string]float64{
			"1h": 0, "6h": 0, "24h": 0, "momentum": 0,
		}
	}

	now := time.Now()
	metrics := make(map[string]float64)

	// Calculate price changes for different timeframes
	timeframes := []struct {
		name     string
		duration time.Duration
	}{
		{"1h", 1 * time.Hour},
		{"6h", 6 * time.Hour},
		{"24h", 24 * time.Hour},
	}

	var momentumSum float64
	for _, tf := range timeframes {
		priceChange := m.calculatePriceChange(prices, now, tf.duration, currentPrice)
		metrics[tf.name] = priceChange
		momentumSum += math.Abs(priceChange)
	}

	// Momentum score combines price changes with volume
	volumeTrend := m.calculateVolumeTrend(symbol)
	metrics["momentum"] = momentumSum/3 + volumeTrend*0.1

	return metrics
}

// calculateLiquidityMetrics assesses market liquidity
func (m *MarketAnalyzer) calculateLiquidityMetrics(symbol string, currentSpread, volume24h float64) map[string]float64 {
	// Get order book data (placeholder - would use exchange API)
	orderBookDepth := 200 // TODO: Get from exchange

	// Calculate liquidity score (0-1, higher is better)
	spreadScore := math.Max(0.1, 1.0/(currentSpread/10)) // Inverse spread
	volumeScore := math.Min(1.0, math.Log(volume24h+1)/10)
	depthScore := math.Min(1.0, float64(orderBookDepth)/1000)

	liquidityScore := (spreadScore*0.4 + volumeScore*0.4 + depthScore*0.2)

	// Calculate spread stability
	m.mu.RLock()
	spreads := m.spreadHistory[symbol]
	m.mu.RUnlock()

	spreadStability := 0.5 // Default
	if len(spreads) > 10 {
		spreadStability = 1.0 - (m.calculateStandardDeviation(spreads) / m.calculateMean(spreads))
	}

	return map[string]float64{
		"score":     liquidityScore,
		"imbalance": 0.1, // TODO: Calculate from order book
		"depth":     float64(orderBookDepth),
		"stability": spreadStability,
	}
}

// calculateVolumeMetrics analyzes volume patterns
func (m *MarketAnalyzer) calculateVolumeMetrics(symbol string, currentVolume float64) map[string]float64 {
	m.mu.RLock()
	volumes := m.volumeHistory[symbol]
	m.mu.RUnlock()

	// Volume trend
	volumeTrend := 0.0
	if len(volumes) > 10 {
		recent := volumes[len(volumes)-10:]
		older := volumes[:len(volumes)-10]
		recentAvg := m.calculateMean(recent)
		olderAvg := m.calculateMean(older)
		volumeTrend = (recentAvg - olderAvg) / olderAvg
	}

	// Volatility-volume ratio
	volatility := m.volatilityCache[symbol]
	volVolumeRatio := 0.0
	if volatility > 0 {
		volVolumeRatio = currentVolume / volatility
	}

	// Create simple volume profile
	profile := &VolumeProfile{
		POCs:           []float64{currentVolume * 0.8}, // Simplified
		HighVolumeNode: currentVolume,
		LowVolumeNode:  currentVolume * 0.2,
	}
	profile.ValueArea.High = currentVolume * 1.2
	profile.ValueArea.Low = currentVolume * 0.8
	profile.ValueArea.Range = profile.ValueArea.High - profile.ValueArea.Low

	return map[string]float64{
		"profile":   0, // Placeholder for profile score
		"trend":     volumeTrend,
		"vol_ratio": volVolumeRatio,
	}
}

// calculateRiskMetrics computes overall market risk
func (m *MarketAnalyzer) calculateRiskMetrics(symbol string, volatility, liquidity, trend map[string]float64) map[string]float64 {
	// Risk components
	volatilityRisk := volatility["24h"] * 10     // Normalize to 0-1 scale
	liquidityRisk := 1.0 - liquidity["score"]    // Invert liquidity score
	trendRisk := math.Abs(trend["momentum"]) * 5 // Momentum risk

	// Combined risk score
	riskScore := (volatilityRisk*0.5 + liquidityRisk*0.3 + trendRisk*0.2)
	riskScore = math.Max(0, math.Min(1, riskScore))

	return map[string]float64{
		"score": riskScore,
	}
}

// determineMarketRegime classifies current market state
func (m *MarketAnalyzer) determineMarketRegime(trend, volatility map[string]float64) string {
	vol24h := volatility["24h"]
	momentum := trend["momentum"]

	if vol24h > 0.08 {
		return "volatile"
	} else if math.Abs(momentum) > 0.05 {
		return "trending"
	}
	return "ranging"
}

// calculateConfidenceLevel assesses analysis reliability
func (m *MarketAnalyzer) calculateConfidenceLevel(symbol string, volatility, liquidity map[string]float64) float64 {
	// Higher confidence for stable, liquid markets
	stabilityBonus := (1.0 - volatility["avg"]) * 0.5
	liquidityBonus := liquidity["score"] * 0.5

	return math.Max(0, math.Min(1, stabilityBonus+liquidityBonus))
}

// Helper methods
func (m *MarketAnalyzer) calculatePriceChange(prices []PricePoint, now time.Time, timeframe time.Duration, currentPrice float64) float64 {
	cutoff := now.Add(-timeframe)

	for i := len(prices) - 1; i >= 0; i-- {
		if prices[i].Timestamp.Before(cutoff) {
			if i > 0 {
				oldPrice := prices[i].Price
				return (currentPrice - oldPrice) / oldPrice
			}
			break
		}
	}

	return 0.0
}

func (m *MarketAnalyzer) calculateVolumeTrend(symbol string) float64 {
	m.mu.RLock()
	volumes := m.volumeHistory[symbol]
	m.mu.RUnlock()

	if len(volumes) < 20 {
		return 0.0
	}

	recent := volumes[len(volumes)-10:]
	older := volumes[len(volumes)-20 : len(volumes)-10]

	recentAvg := m.calculateMean(recent)
	olderAvg := m.calculateMean(older)

	return (recentAvg - olderAvg) / olderAvg
}

func (m *MarketAnalyzer) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (m *MarketAnalyzer) calculateVariance(values []float64, mean float64) float64 {
	if len(values) == 0 {
		return 0.0
	}

	sum := 0.0
	for _, v := range values {
		diff := v - mean
		sum += diff * diff
	}
	return sum / float64(len(values))
}

func (m *MarketAnalyzer) calculateStandardDeviation(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}

	mean := m.calculateMean(values)
	variance := m.calculateVariance(values, mean)
	return math.Sqrt(variance)
}
