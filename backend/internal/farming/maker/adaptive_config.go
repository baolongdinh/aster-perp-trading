package maker

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AdaptiveConfigManager manages dynamic symbol configuration based on market data
type AdaptiveConfigManager struct {
	logger     *zap.Logger
	httpClient FuturesClientInterface
	wsClient   WebSocketClientInterface

	// Cache for market data and calculated configs
	marketDataCache map[string]*MarketData
	configCache     map[string]*AdaptiveConfig
	mu              sync.RWMutex

	// Update intervals
	marketDataUpdateInterval time.Duration
	configReevalInterval     time.Duration
}

// MarketData contains real-time market information from exchange
type MarketData struct {
	Symbol         string    `json:"symbol"`
	BestBid        float64   `json:"best_bid"`
	BestAsk        float64   `json:"best_ask"`
	SpreadBps      float64   `json:"spread_bps"`
	Volume24h      float64   `json:"volume_24h"`
	PriceChange24h float64   `json:"price_change_24h"`
	Volatility24h  float64   `json:"volatility_24h"`
	OrderBookDepth int       `json:"order_book_depth"`
	LiquidityScore float64   `json:"liquidity_score"`
	LastUpdate     time.Time `json:"last_update"`

	// Historical data for trend analysis
	PriceHistory  []float64 `json:"price_history"`
	SpreadHistory []float64 `json:"spread_history"`
	VolumeHistory []float64 `json:"volume_history"`
}

// AdaptiveConfig contains dynamically calculated optimal parameters
type AdaptiveConfig struct {
	Symbol                string  `json:"symbol"`
	DefaultSpreadBps      float64 `json:"default_spread_bps"`
	MinSpreadBps          float64 `json:"min_spread_bps"`
	MaxSpreadBps          float64 `json:"max_spread_bps"`
	MicroGridSpacingBps   float64 `json:"micro_grid_spacing_bps"`
	MicroGridLevels       int     `json:"micro_grid_levels"`
	BaseNotionalUSD       float64 `json:"base_notional_usd"`
	MaxPositionUSDT       float64 `json:"max_position_usdt"`
	PositionBiasThreshold float64 `json:"position_bias_threshold"`
	MomentumThresholdPct  float64 `json:"momentum_threshold_pct"`
	ToxicFlowThreshold    float64 `json:"toxic_flow_threshold"`

	// Risk metrics
	RiskScore          float64   `json:"risk_score"`
	VolatilityCategory string    `json:"volatility_category"` // "low", "medium", "high", "extreme"
	LiquidityCategory  string    `json:"liquidity_category"`  // "low", "medium", "high"
	OptimizationScore  float64   `json:"optimization_score"`
	LastCalculated     time.Time `json:"last_calculated"`
}

// SymbolProfile defines characteristics for different symbol categories
type SymbolProfile struct {
	Category             string  `json:"category"`
	BaseSpreadMultiplier float64 `json:"base_spread_multiplier"`
	VolatilityMultiplier float64 `json:"volatility_multiplier"`
	LiquidityMultiplier  float64 `json:"liquidity_multiplier"`
	RiskAdjustment       float64 `json:"risk_adjustment"`
}

func NewAdaptiveConfigManager(
	httpClient FuturesClientInterface,
	wsClient WebSocketClientInterface,
	logger *zap.Logger,
) *AdaptiveConfigManager {
	return &AdaptiveConfigManager{
		httpClient:               httpClient,
		wsClient:                 wsClient,
		logger:                   logger,
		marketDataCache:          make(map[string]*MarketData),
		configCache:              make(map[string]*AdaptiveConfig),
		marketDataUpdateInterval: 30 * time.Second,
		configReevalInterval:     5 * time.Minute,
	}
}

// Start begins the adaptive configuration management
func (a *AdaptiveConfigManager) Start(ctx context.Context) error {
	a.logger.Info("Starting Adaptive Configuration Manager")

	// Start market data collection
	go a.marketDataCollectionLoop(ctx)

	// Start configuration optimization loop
	go a.configOptimizationLoop(ctx)

	return nil
}

// GetOptimalConfig returns the current optimal configuration for a symbol
func (a *AdaptiveConfigManager) GetOptimalConfig(symbol string) (*AdaptiveConfig, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if config, exists := a.configCache[symbol]; exists {
		// Check if config is still valid (not too old)
		if time.Since(config.LastCalculated) < a.configReevalInterval {
			return config, nil
		}
	}

	// Generate new config if not exists or expired
	return a.calculateOptimalConfig(symbol)
}

// marketDataCollectionLoop continuously updates market data
func (a *AdaptiveConfigManager) marketDataCollectionLoop(ctx context.Context) {
	ticker := time.NewTicker(a.marketDataUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.updateMarketData(ctx)
		}
	}
}

// configOptimizationLoop periodically recalculates optimal configurations
func (a *AdaptiveConfigManager) configOptimizationLoop(ctx context.Context) {
	ticker := time.NewTicker(a.configReevalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.recalculateAllConfigs(ctx)
		}
	}
}

// updateMarketData fetches latest market data from exchange
func (a *AdaptiveConfigManager) updateMarketData(ctx context.Context) {
	// Get all active symbols from config cache
	a.mu.RLock()
	symbols := make([]string, 0, len(a.configCache))
	for symbol := range a.configCache {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range symbols {
		marketData, err := a.fetchMarketData(ctx, symbol)
		if err != nil {
			a.logger.Error("Failed to fetch market data",
				zap.String("symbol", symbol),
				zap.Error(err))
			continue
		}

		a.mu.Lock()
		a.marketDataCache[symbol] = marketData
		a.mu.Unlock()
	}
}

// fetchMarketData gets comprehensive market data from exchange
func (a *AdaptiveConfigManager) fetchMarketData(ctx context.Context, symbol string) (*MarketData, error) {
	// Check if wsClient is available
	if a.wsClient == nil {
		return nil, fmt.Errorf("wsClient not initialized for symbol %s", symbol)
	}

	// Get current ticker data
	bestBid, bestAsk, volume24h, err := a.wsClient.GetTickerData(symbol)
	if err != nil {
		return nil, err
	}

	// Calculate spread
	spread := bestAsk - bestBid
	spreadBps := (spread / bestBid) * 10000

	// Get 24h price statistics for volatility calculation
	priceChange24h, volatility24h, err := a.getPriceStatistics(ctx, symbol)
	if err != nil {
		// Use fallback calculation
		priceChange24h = 0
		volatility24h = 0.02 // 2% default
	}

	// Get order book depth for liquidity assessment
	orderBookDepth, err := a.getOrderBookDepth(ctx, symbol)
	if err != nil {
		orderBookDepth = 100 // default
	}

	// Calculate liquidity score (0-1, higher is better)
	liquidityScore := a.calculateLiquidityScore(volume24h, orderBookDepth, spreadBps)

	return &MarketData{
		Symbol:         symbol,
		BestBid:        bestBid,
		BestAsk:        bestAsk,
		SpreadBps:      spreadBps,
		Volume24h:      volume24h,
		PriceChange24h: priceChange24h,
		Volatility24h:  volatility24h,
		OrderBookDepth: orderBookDepth,
		LiquidityScore: liquidityScore,
		LastUpdate:     time.Now(),
	}, nil
}

// calculateOptimalConfig determines optimal parameters based on market data
func (a *AdaptiveConfigManager) calculateOptimalConfig(symbol string) (*AdaptiveConfig, error) {
	a.mu.RLock()
	marketData, exists := a.marketDataCache[symbol]
	a.mu.RUnlock()

	if !exists {
		// Use default config if no market data available
		return a.getDefaultConfig(symbol), nil
	}

	// Determine symbol characteristics
	volatilityCat := a.categorizeVolatility(marketData.Volatility24h)
	liquidityCat := a.categorizeLiquidity(marketData.LiquidityScore)

	// Get symbol profile
	profile := a.getSymbolProfile(symbol, volatilityCat, liquidityCat)

	// Calculate optimal parameters
	config := &AdaptiveConfig{
		Symbol: symbol,

		// Spread calculation based on market conditions
		DefaultSpreadBps: a.calculateOptimalSpread(marketData, profile),
		MinSpreadBps:     math.Max(1, marketData.SpreadBps*0.5),
		MaxSpreadBps:     math.Min(50, marketData.SpreadBps*3),

		// Grid parameters based on volatility and liquidity
		MicroGridSpacingBps: a.calculateOptimalGridSpacing(marketData, profile),
		MicroGridLevels:     a.calculateOptimalGridLevels(marketData, profile),

		// Position sizing based on volatility and risk
		BaseNotionalUSD: a.calculateOptimalBaseNotional(marketData, profile),
		MaxPositionUSDT: a.calculateOptimalMaxPosition(marketData, profile),

		// Risk thresholds based on market characteristics
		PositionBiasThreshold: a.calculateOptimalBiasThreshold(marketData, profile),
		MomentumThresholdPct:  a.calculateOptimalMomentumThreshold(marketData, profile),
		ToxicFlowThreshold:    a.calculateOptimalToxicFlowThreshold(marketData, profile),

		// Metadata
		RiskScore:          a.calculateRiskScore(marketData),
		VolatilityCategory: volatilityCat,
		LiquidityCategory:  liquidityCat,
		OptimizationScore:  a.calculateOptimizationScore(marketData, profile),
		LastCalculated:     time.Now(),
	}

	// Cache the config
	a.mu.Lock()
	a.configCache[symbol] = config
	a.mu.Unlock()

	a.logger.Info("Calculated optimal configuration",
		zap.String("symbol", symbol),
		zap.Float64("spread_bps", config.DefaultSpreadBps),
		zap.Float64("grid_spacing_bps", config.MicroGridSpacingBps),
		zap.Int("grid_levels", config.MicroGridLevels),
		zap.String("volatility_cat", volatilityCat),
		zap.String("liquidity_cat", liquidityCat))

	return config, nil
}

// Helper methods for optimal parameter calculation
func (a *AdaptiveConfigManager) calculateOptimalSpread(marketData *MarketData, profile *SymbolProfile) float64 {
	// Base spread from market conditions
	baseSpread := marketData.SpreadBps * profile.BaseSpreadMultiplier

	// Adjust for volatility
	if marketData.Volatility24h > 0.05 { // High volatility
		baseSpread *= 1.5
	} else if marketData.Volatility24h > 0.03 { // Medium volatility
		baseSpread *= 1.2
	}

	// Adjust for liquidity
	if marketData.LiquidityScore < 0.3 { // Low liquidity
		baseSpread *= 2.0
	} else if marketData.LiquidityScore < 0.7 { // Medium liquidity
		baseSpread *= 1.3
	}

	// Ensure profitable minimum
	return math.Max(1, math.Min(50, baseSpread))
}

func (a *AdaptiveConfigManager) calculateOptimalGridSpacing(marketData *MarketData, profile *SymbolProfile) float64 {
	// Tighter spacing for high liquidity, wider for high volatility
	baseSpacing := 0.1 // Default ultra-tight

	if marketData.LiquidityScore > 0.8 {
		baseSpacing = 0.05 // Ultra-tight for high liquidity
	} else if marketData.LiquidityScore < 0.4 {
		baseSpacing = 0.2 // Wider for low liquidity
	}

	// Adjust for volatility
	if marketData.Volatility24h > 0.05 {
		baseSpacing *= 2.0 // Wider spacing for high volatility
	}

	return math.Max(0.01, math.Min(1.0, baseSpacing))
}

func (a *AdaptiveConfigManager) calculateOptimalGridLevels(marketData *MarketData, profile *SymbolProfile) int {
	// More levels for high liquidity, fewer for high volatility
	baseLevels := 50

	if marketData.LiquidityScore > 0.8 {
		baseLevels = 75 // More levels for high liquidity
	} else if marketData.LiquidityScore < 0.4 {
		baseLevels = 25 // Fewer levels for low liquidity
	}

	// Adjust for volatility
	if marketData.Volatility24h > 0.05 {
		baseLevels = int(float64(baseLevels) * 0.7) // Fewer levels for high volatility
	}

	return int(math.Max(10, math.Min(100, float64(baseLevels))))
}

func (a *AdaptiveConfigManager) calculateOptimalBaseNotional(marketData *MarketData, profile *SymbolProfile) float64 {
	// Base notional adjusted for volatility and risk
	baseNotional := 100.0

	// Adjust for volatility
	if marketData.Volatility24h > 0.05 {
		baseNotional *= 0.5 // Reduce size for high volatility
	} else if marketData.Volatility24h > 0.03 {
		baseNotional *= 0.7
	}

	// Adjust for liquidity
	if marketData.LiquidityScore > 0.8 {
		baseNotional *= 1.5 // Increase size for high liquidity
	} else if marketData.LiquidityScore < 0.4 {
		baseNotional *= 0.6 // Reduce size for low liquidity
	}

	return math.Max(10, math.Min(1000, baseNotional))
}

func (a *AdaptiveConfigManager) calculateOptimalMaxPosition(marketData *MarketData, profile *SymbolProfile) float64 {
	// Max position based on volatility and liquidity
	basePosition := 3000.0

	if marketData.Volatility24h > 0.05 {
		basePosition *= 0.5 // Reduce for high volatility
	}

	if marketData.LiquidityScore > 0.8 {
		basePosition *= 1.5 // Increase for high liquidity
	} else if marketData.LiquidityScore < 0.4 {
		basePosition *= 0.5 // Reduce for low liquidity
	}

	return math.Max(500, math.Min(10000, basePosition))
}

// Additional calculation methods...
func (a *AdaptiveConfigManager) calculateOptimalBiasThreshold(marketData *MarketData, profile *SymbolProfile) float64 {
	// Lower threshold for high volatility symbols
	if marketData.Volatility24h > 0.05 {
		return 0.2
	} else if marketData.Volatility24h > 0.03 {
		return 0.25
	}
	return 0.3
}

func (a *AdaptiveConfigManager) calculateOptimalMomentumThreshold(marketData *MarketData, profile *SymbolProfile) float64 {
	// Higher threshold for high volatility symbols
	if marketData.Volatility24h > 0.05 {
		return 0.04 // 4%
	} else if marketData.Volatility24h > 0.03 {
		return 0.03 // 3%
	}
	return 0.025 // 2.5%
}

func (a *AdaptiveConfigManager) calculateOptimalToxicFlowThreshold(marketData *MarketData, profile *SymbolProfile) float64 {
	// Lower tolerance for low liquidity symbols
	if marketData.LiquidityScore < 0.4 {
		return 0.5
	} else if marketData.LiquidityScore < 0.7 {
		return 0.6
	}
	return 0.65
}

// Categorization methods
func (a *AdaptiveConfigManager) categorizeVolatility(volatility float64) string {
	if volatility > 0.08 {
		return "extreme"
	} else if volatility > 0.05 {
		return "high"
	} else if volatility > 0.02 {
		return "medium"
	}
	return "low"
}

func (a *AdaptiveConfigManager) categorizeLiquidity(score float64) string {
	if score > 0.8 {
		return "high"
	} else if score > 0.5 {
		return "medium"
	}
	return "low"
}

// Profile and scoring methods
func (a *AdaptiveConfigManager) getSymbolProfile(symbol, volatilityCat, liquidityCat string) *SymbolProfile {
	// Create dynamic profile based on symbol characteristics and market conditions
	profile := &SymbolProfile{
		Category:             "dynamic",
		BaseSpreadMultiplier: 1.0,
		VolatilityMultiplier: 1.0,
		LiquidityMultiplier:  1.0,
		RiskAdjustment:       1.0,
	}

	// Safe prefix extraction - protect against short symbol names
	prefix := ""
	if len(symbol) >= 3 {
		prefix = symbol[:3]
	}

	// Adjust multipliers based on symbol type (handles both btcusd1 and btcusdt formats)
	switch prefix {
	case "btc":
		profile.BaseSpreadMultiplier = 1.2
		profile.VolatilityMultiplier = 0.8
		profile.LiquidityMultiplier = 1.3
	case "eth":
		profile.BaseSpreadMultiplier = 0.8
		profile.VolatilityMultiplier = 1.2
		profile.LiquidityMultiplier = 1.4
	case "sol":
		profile.BaseSpreadMultiplier = 1.8
		profile.VolatilityMultiplier = 1.5
		profile.LiquidityMultiplier = 0.6
	case "ada":
		profile.BaseSpreadMultiplier = 1.4
		profile.VolatilityMultiplier = 1.1
		profile.LiquidityMultiplier = 0.8
	case "bnb":
		profile.BaseSpreadMultiplier = 1.5
		profile.VolatilityMultiplier = 1.3
		profile.LiquidityMultiplier = 0.7
	case "xrp":
		profile.BaseSpreadMultiplier = 2.0
		profile.VolatilityMultiplier = 2.0
		profile.LiquidityMultiplier = 0.5
	case "dot":
		profile.BaseSpreadMultiplier = 1.6
		profile.VolatilityMultiplier = 1.4
		profile.LiquidityMultiplier = 0.6
	case "avax":
		profile.BaseSpreadMultiplier = 1.7
		profile.VolatilityMultiplier = 1.6
		profile.LiquidityMultiplier = 0.5
	case "matic":
		profile.BaseSpreadMultiplier = 1.5
		profile.VolatilityMultiplier = 1.4
		profile.LiquidityMultiplier = 0.6
	case "link":
		profile.BaseSpreadMultiplier = 1.3
		profile.VolatilityMultiplier = 1.2
		profile.LiquidityMultiplier = 0.7
	case "ltc":
		profile.BaseSpreadMultiplier = 1.1
		profile.VolatilityMultiplier = 1.0
		profile.LiquidityMultiplier = 0.9
	case "uni":
		profile.BaseSpreadMultiplier = 1.4
		profile.VolatilityMultiplier = 1.3
		profile.LiquidityMultiplier = 0.6
	}

	return profile
}

func (a *AdaptiveConfigManager) calculateLiquidityScore(volume24h float64, orderBookDepth int, spreadBps float64) float64 {
	// Normalize volume (log scale for better distribution)
	volumeScore := math.Min(1.0, math.Log(volume24h+1)/10)

	// Normalize order book depth
	depthScore := math.Min(1.0, float64(orderBookDepth)/1000)

	// Inverse spread score (lower spread = higher score) - protect division by zero
	effectiveSpreadBps := spreadBps
	if effectiveSpreadBps <= 0 {
		effectiveSpreadBps = 0.1 // Minimum to avoid division by zero
	}
	spreadScore := math.Max(0.1, 1.0/(effectiveSpreadBps/10))

	// Weighted combination
	return (volumeScore*0.4 + depthScore*0.3 + spreadScore*0.3)
}

func (a *AdaptiveConfigManager) calculateRiskScore(marketData *MarketData) float64 {
	// Risk score based on volatility, spread, and liquidity
	volatilityRisk := math.Min(1.0, marketData.Volatility24h*10) // Normalize to 0-1
	spreadRisk := math.Min(1.0, marketData.SpreadBps/20)         // Normalize to 0-1
	liquidityRisk := 1.0 - marketData.LiquidityScore             // Invert liquidity

	return (volatilityRisk*0.5 + spreadRisk*0.3 + liquidityRisk*0.2)
}

func (a *AdaptiveConfigManager) calculateOptimizationScore(marketData *MarketData, profile *SymbolProfile) float64 {
	// Higher score = better optimization opportunity
	liquidityBonus := marketData.LiquidityScore * 0.4
	volatilityPenalty := math.Min(0.3, marketData.Volatility24h*3)
	spreadPenalty := math.Min(0.3, marketData.SpreadBps/50)

	return math.Max(0, liquidityBonus-volatilityPenalty-spreadPenalty)
}

// Placeholder methods for exchange API integration
func (a *AdaptiveConfigManager) getPriceStatistics(ctx context.Context, symbol string) (float64, float64, error) {
	// Get 24h ticker data from exchange
	tickers, err := a.httpClient.Get24hrTicker(ctx)
	if err != nil {
		a.logger.Warn("Failed to get 24hr ticker, using defaults",
			zap.String("symbol", symbol),
			zap.Error(err))
		return 0.02, 0.025, nil
	}

	// Find the matching symbol (case-insensitive)
	upperSymbol := strings.ToUpper(symbol)
	for _, ticker := range tickers {
		if strings.ToUpper(ticker.Symbol) == upperSymbol {
			// Calculate volatility as (High - Low) / Open
			var volatility float64
			if ticker.OpenPrice > 0 {
				volatility = (ticker.HighPrice - ticker.LowPrice) / ticker.OpenPrice
			} else {
				volatility = 0.02 // Default
			}

			// Price change as percentage
			priceChangePercent := ticker.PriceChangePercent / 100.0

			a.logger.Debug("Got price statistics",
				zap.String("symbol", symbol),
				zap.Float64("volatility", volatility),
				zap.Float64("price_change_pct", priceChangePercent))

			return priceChangePercent, volatility, nil
		}
	}

	// Symbol not found, use defaults
	a.logger.Warn("Symbol not found in 24hr ticker response, using defaults",
		zap.String("symbol", symbol))
	return 0.02, 0.025, nil
}

func (a *AdaptiveConfigManager) getOrderBookDepth(ctx context.Context, symbol string) (int, error) {
	// Try to get order book depth from HTTP client if available
	// For now, estimate from ticker data
	a.mu.RLock()
	marketData, exists := a.marketDataCache[symbol]
	a.mu.RUnlock()

	if exists && marketData.Volume24h > 0 {
		// Estimate depth based on volume (higher volume = deeper market)
		// This is a rough approximation
		estimatedDepth := int(math.Min(1000, math.Max(50, marketData.Volume24h/1000)))
		return estimatedDepth, nil
	}

	// Default depth if no data
	return 200, nil
}

func (a *AdaptiveConfigManager) getDefaultConfig(symbol string) *AdaptiveConfig {
	return &AdaptiveConfig{
		Symbol:                symbol,
		DefaultSpreadBps:      5,
		MinSpreadBps:          1,
		MaxSpreadBps:          20,
		MicroGridSpacingBps:   0.1,
		MicroGridLevels:       50,
		BaseNotionalUSD:       100,
		MaxPositionUSDT:       3000,
		PositionBiasThreshold: 0.3,
		MomentumThresholdPct:  0.03,
		ToxicFlowThreshold:    0.6,
		LastCalculated:        time.Now(),
	}
}

func (a *AdaptiveConfigManager) recalculateAllConfigs(ctx context.Context) {
	a.mu.RLock()
	symbols := make([]string, 0, len(a.configCache))
	for symbol := range a.configCache {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range symbols {
		_, err := a.calculateOptimalConfig(symbol)
		if err != nil {
			a.logger.Error("Failed to recalculate config",
				zap.String("symbol", symbol),
				zap.Error(err))
		}
	}
}
