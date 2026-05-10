package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"aster-bot/internal/auth"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming/maker"

	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "config/volume-farm-config.yaml", "Path to configuration file")
	dryRun := flag.Bool("dry-run", false, "Run in dry-run mode (no real orders)")

	// Symbol selection flags
	symbol := flag.String("symbol", "", "Single symbol to trade (e.g., ethusd1)")
	symbols := flag.String("symbols", "", "Multiple symbols to trade (comma-separated, e.g., ethusd1,btcusd1)")

	spreadBps := flag.Float64("spread", 5, "Spread in basis points (default: 5)")
	maxLeverage := flag.Int("leverage", 50, "Max leverage (default: 50)")

	// New config flags for micro profit optimization
	useDynamicSizing := flag.Bool("dynamic-sizing", true, "Use dynamic balance-based sizing")
	baseNotionalUSD := flag.Float64("base-notional", 100, "Base notional USD for dynamic sizing")
	microProfitMode := flag.Bool("micro-profit", true, "Use micro profit ultra-tight grid")
	microGridSpacingBps := flag.Float64("micro-spacing", 0.1, "Micro grid spacing in bps (default: 0.1)")
	microGridLevels := flag.Int("micro-levels", 50, "Micro grid levels per side (default: 50)")
	toxicFlowDetection := flag.Bool("toxic-flow", true, "Enable toxic flow detection")
	positionBiasThreshold := flag.Float64("bias-threshold", 0.3, "Position bias threshold (default: 0.3)")

	// Momentum protection flags
	momentumDetection := flag.Bool("momentum", true, "Enable momentum detection")
	momentumThreshold := flag.Float64("momentum-threshold", 0.03, "Momentum threshold (default: 3%)")

	// Adaptive configuration flags
	enableAdaptive := flag.Bool("adaptive", true, "Enable adaptive configuration based on market data")
	autoOptimize := flag.Bool("auto-optimize", true, "Enable automatic parameter optimization")

	flag.Parse()

	logger, err := zap.NewDevelopment()
	if err != nil {
		panic("failed to initialize logger")
	}
	defer logger.Sync()

	logger.Info("🚀 Starting Volume Farm Micro Profit Engine (Maker Strategy Only)")

	// Debug: log raw flag values
	logger.Info("DEBUG raw flags",
		zap.String("symbol_flag", *symbol),
		zap.String("symbols_flag", *symbols),
		zap.String("config_path", *configPath))

	// Parse symbols
	tradingSymbols := parseTradingSymbols(*symbol, *symbols)
	if len(tradingSymbols) == 0 {
		tradingSymbols = []TradingSymbol{{Original: "ethusd1", Normalized: "ethusd1"}} // Default
	}

	logger.Info("Trading symbols configured",
		zap.String("symbol_original", tradingSymbols[0].Original),
		zap.String("symbol_normalized", tradingSymbols[0].Normalized),
		zap.Bool("adaptive_mode", *enableAdaptive),
		zap.Bool("auto_optimize", *autoOptimize))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	if *dryRun {
		cfg.Bot.DryRun = true
		logger.Info("🧪 Running in DRY-RUN mode")
	}

	restURL := cfg.Exchange.FuturesRESTBase
	if restURL == "" {
		restURL = "https://fapi.asterdex.com"
	}

	// Create HTTP client with proper authentication (V3 or V1)
	var httpClient *client.HTTPClient
	if cfg.Exchange.UserWallet != "" && cfg.Exchange.APISigner != "" && cfg.Exchange.APISignerKey != "" {
		// V3 Authentication (API Wallet/Agent model)
		v3Signer, err := auth.NewV3Signer(
			cfg.Exchange.UserWallet,
			cfg.Exchange.APISigner,
			cfg.Exchange.APISignerKey,
			int64(cfg.Exchange.RecvWindow),
		)
		if err != nil {
			logger.Fatal("Failed to create V3 signer", zap.Error(err))
		}
		httpClient = client.NewHTTPClientV3(restURL, v3Signer, logger, cfg.Exchange.RequestsPerSecond)
		logger.Info("Using V3 authentication (API Wallet/Agent model)")
	} else if cfg.Exchange.APIKey != "" && cfg.Exchange.APISecret != "" {
		// V1 Authentication (deprecated)
		v1Signer, err := auth.NewSigner(cfg.Exchange.APIKey, cfg.Exchange.APISecret, cfg.Exchange.RecvWindow)
		if err != nil {
			logger.Fatal("Failed to create V1 signer", zap.Error(err))
		}
		httpClient = client.NewHTTPClient(restURL, v1Signer, logger, cfg.Exchange.RequestsPerSecond)
		logger.Info("Using V1 authentication (API Key model - deprecated)")
	} else {
		logger.Fatal("No valid authentication credentials found - please configure either V3 or V1")
	}

	futuresClient := client.NewFuturesClient(httpClient, cfg.Bot.DryRun, logger, cfg.Exchange.RequestsPerSecond)
	logger.Info("Futures client initialized", zap.String("url", restURL))

	// Load exchange info to get symbol-specific constraints (max leverage, precision)
	precisionMgr := client.NewPrecisionManager()
	tempCtx, tempCancel := context.WithTimeout(context.Background(), 10*time.Second)
	exchangeInfo, err := client.NewMarketClient(httpClient).ExchangeInfo(tempCtx)
	tempCancel()
	if err != nil {
		logger.Warn("Failed to load exchange info, using defaults", zap.Error(err))
	} else if err := precisionMgr.UpdateFromExchangeInfo(exchangeInfo); err != nil {
		logger.Warn("Failed to parse exchange info, using defaults", zap.Error(err))
	} else {
		logger.Info("Exchange info loaded successfully")
	}

	// Get max leverage for the symbol
	maxLeverageFromExchange := precisionMgr.GetMaxLeverage(strings.ToUpper(tradingSymbols[0].Original))
	if maxLeverageFromExchange > 0 {
		logger.Info("Symbol leverage info",
			zap.String("symbol", tradingSymbols[0].Original),
			zap.Float64("max_leverage", maxLeverageFromExchange))
		// Override CLI leverage if exchange allows lower
		if *maxLeverage > int(maxLeverageFromExchange) {
			logger.Warn("CLI leverage too high for symbol, capping to exchange max",
				zap.Int("requested", *maxLeverage),
				zap.Float64("max_allowed", maxLeverageFromExchange))
			*maxLeverage = int(maxLeverageFromExchange)
		}
	}

	// Get min notional from exchange info for adaptive config
	symbolInfo := precisionMgr.GetSymbolInfo(strings.ToUpper(tradingSymbols[0].Original))
	if symbolInfo.MinNotional > 0 {
		logger.Info("Symbol exchange info",
			zap.String("symbol", tradingSymbols[0].Original),
			zap.Float64("min_notional", symbolInfo.MinNotional),
			zap.Float64("min_qty", symbolInfo.MinQty),
			zap.Float64("max_qty", symbolInfo.MaxQty),
			zap.Float64("tick_size", symbolInfo.TickSize),
			zap.Float64("step_size", symbolInfo.StepSize))
		// Override config min notional if exchange requires higher
		if *baseNotionalUSD < symbolInfo.MinNotional {
			logger.Warn("Base notional too low for symbol, increasing to meet exchange requirement",
				zap.Float64("requested", *baseNotionalUSD),
				zap.Float64("min_required", symbolInfo.MinNotional))
			*baseNotionalUSD = symbolInfo.MinNotional
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *enableAdaptive {
		// Adaptive mode with dynamic configuration
		startAdaptiveMode(ctx, tradingSymbols[0].Original, tradingSymbols[0].Normalized, cfg, futuresClient, *dryRun, logger,
			*spreadBps, *maxLeverage, *useDynamicSizing, *baseNotionalUSD,
			*microProfitMode, *microGridSpacingBps, *microGridLevels,
			*toxicFlowDetection, *positionBiasThreshold, *momentumDetection, *momentumThreshold)
	} else {
		// Legacy mode with static configuration
		startLegacyMode(ctx, tradingSymbols[0].Original, cfg, futuresClient, *dryRun, logger,
			*spreadBps, *maxLeverage, *useDynamicSizing, *baseNotionalUSD,
			*microProfitMode, *microGridSpacingBps, *microGridLevels,
			*toxicFlowDetection, *positionBiasThreshold, *momentumDetection, *momentumThreshold)
	}
}

// TradingSymbol holds both original and normalized symbol
type TradingSymbol struct {
	Original   string // Full symbol for WebSocket URL: "agtusdt"
	Normalized string // Short symbol for internal logic: "agt"
}

func parseTradingSymbols(singleSymbol, multipleSymbols string) []TradingSymbol {
	if singleSymbol != "" {
		original := strings.ToLower(strings.TrimSpace(singleSymbol))
		normalized := normalizeSymbol(singleSymbol)
		return []TradingSymbol{{Original: original, Normalized: normalized}}
	}

	if multipleSymbols != "" {
		var symbols []TradingSymbol
		for _, sym := range strings.Split(multipleSymbols, ",") {
			original := strings.ToLower(strings.TrimSpace(sym))
			normalized := normalizeSymbol(sym)
			if normalized != "" {
				symbols = append(symbols, TradingSymbol{Original: original, Normalized: normalized})
			}
		}
		return symbols
	}

	return []TradingSymbol{} // Will default to ethusd1 in main
}

// normalizeSymbol converts user input to internal symbol format
// Keeps full symbol for exchange compatibility, only extracts base for profile matching
// Examples:
//   - "BTCUSDT" -> "btcusdt" (keep full for exchange API)
//   - "btcusd1" -> "btcusd1"
//   - "ETH/USDT" -> "ethusdt"
//   - "BtcUsDt" -> "btcusdt"
//   - "agtusdt" -> "agtusdt"
func normalizeSymbol(symbol string) string {
	s := strings.ToLower(strings.TrimSpace(symbol))

	// Handle /USDT format - replace with nothing to get base
	s = strings.ReplaceAll(s, "/usdt", "")
	s = strings.ReplaceAll(s, "/usd", "")
	s = strings.ReplaceAll(s, "/busd", "")

	// If it looks like a full symbol (has usdt/usd/busd suffix), add it back
	// This ensures we keep "btcusdt" not just "btc"
	original := strings.ToLower(strings.TrimSpace(symbol))
	if strings.HasSuffix(original, "usdt") || strings.HasSuffix(original, "usd") || strings.HasSuffix(original, "busd") {
		// Already has suffix, return as-is (lowercase)
		return original
	}

	// If result is empty or too short, return original lowercase
	if len(s) < 2 {
		return strings.ToLower(strings.TrimSpace(symbol))
	}

	return s
}

// startAdaptiveMode starts single symbol with adaptive configuration
// originalSymbol: full symbol for WebSocket URL (e.g., "agtusdt")
// normalizedSymbol: short symbol for internal logic (e.g., "agt")
func startAdaptiveMode(ctx context.Context, originalSymbol, normalizedSymbol string, cfg *config.Config,
	futuresClient *client.FuturesClient, dryRun bool, logger *zap.Logger,
	spreadBps float64, maxLeverage int, useDynamicSizing bool, baseNotionalUSD float64,
	microProfitMode bool, microGridSpacingBps float64, microGridLevels int,
	toxicFlowDetection bool, positionBiasThreshold float64, momentumDetection bool, momentumThreshold float64) {

	logger.Info("🔄 Starting Adaptive Mode",
		zap.String("original_symbol", originalSymbol),
		zap.String("normalized_symbol", normalizedSymbol))

	// Use original symbol for WebSocket URL (full format like "agtusdt")
	wsURL := fmt.Sprintf("wss://fstream.asterdex.com/ws/%s@bookTicker", strings.ToLower(originalSymbol))

	wsClient := client.NewWebSocketClient(wsURL, logger)
	if err := wsClient.Connect(ctx); err != nil {
		logger.Fatal("Failed to connect WebSocket", zap.Error(err))
	}
	logger.Info("WebSocket connected", zap.String("url", wsURL))

	// Wait for initial ticker data
	time.Sleep(2 * time.Second)

	// Initialize AdaptiveConfigManager with real clients
	adaptiveConfigMgr := maker.NewAdaptiveConfigManager(futuresClient, wsClient, logger)
	if err := adaptiveConfigMgr.Start(ctx); err != nil {
		logger.Error("Failed to start adaptive config manager", zap.Error(err))
	}

	// Get optimal configuration for this symbol (use normalized for internal logic)
	adaptiveConfig, err := adaptiveConfigMgr.GetOptimalConfig(normalizedSymbol)
	if err != nil {
		logger.Error("Failed to get optimal config, using defaults", zap.Error(err))
		adaptiveConfig = &maker.AdaptiveConfig{
			DefaultSpreadBps:      spreadBps,
			MicroGridSpacingBps:   microGridSpacingBps,
			MicroGridLevels:       microGridLevels,
			BaseNotionalUSD:       baseNotionalUSD,
			MaxPositionUSDT:       500,
			PositionBiasThreshold: positionBiasThreshold,
			MomentumThresholdPct:  momentumThreshold,
			ToxicFlowThreshold:    0.6,
		}
	}

	logger.Info("📊 Adaptive Configuration Loaded",
		zap.String("symbol", normalizedSymbol),
		zap.Float64("optimal_spread_bps", adaptiveConfig.DefaultSpreadBps),
		zap.Float64("optimal_grid_spacing_bps", adaptiveConfig.MicroGridSpacingBps),
		zap.Int("optimal_grid_levels", adaptiveConfig.MicroGridLevels),
		zap.Float64("base_notional_usd", adaptiveConfig.BaseNotionalUSD),
		zap.Float64("max_position_usdt", adaptiveConfig.MaxPositionUSDT),
		zap.String("volatility_category", adaptiveConfig.VolatilityCategory),
		zap.String("liquidity_category", adaptiveConfig.LiquidityCategory),
		zap.Float64("risk_score", adaptiveConfig.RiskScore))

	makerConfig := maker.DefaultConfig()
	makerConfig.Symbols = []string{normalizedSymbol}
	makerConfig.DefaultSpreadBps = adaptiveConfig.DefaultSpreadBps
	makerConfig.MaxLeverage = maxLeverage

	// Apply adaptive configuration
	makerConfig.UseDynamicSizing = useDynamicSizing
	makerConfig.BaseNotionalUSD = adaptiveConfig.BaseNotionalUSD
	makerConfig.MicroProfitMode = microProfitMode
	makerConfig.MicroGridSpacingBps = adaptiveConfig.MicroGridSpacingBps
	makerConfig.MicroGridLevels = adaptiveConfig.MicroGridLevels
	makerConfig.ToxicFlowDetection = toxicFlowDetection
	makerConfig.PositionBiasThreshold = adaptiveConfig.PositionBiasThreshold
	makerConfig.MomentumDetection = momentumDetection
	makerConfig.MomentumThresholdPct = adaptiveConfig.MomentumThresholdPct

	logger.Info("Maker Strategy Config (Adaptive)",
		zap.Strings("symbols", makerConfig.Symbols),
		zap.Float64("spread_bps", makerConfig.DefaultSpreadBps),
		zap.Int("max_leverage", makerConfig.MaxLeverage),
		zap.Bool("dynamic_sizing", makerConfig.UseDynamicSizing),
		zap.Bool("micro_profit_mode", makerConfig.MicroProfitMode),
		zap.Float64("micro_grid_spacing_bps", makerConfig.MicroGridSpacingBps),
		zap.Int("micro_grid_levels", makerConfig.MicroGridLevels),
		zap.Bool("toxic_flow_detection", makerConfig.ToxicFlowDetection),
		zap.Float64("position_bias_threshold", makerConfig.PositionBiasThreshold),
		zap.Bool("momentum_detection", makerConfig.MomentumDetection),
		zap.Float64("momentum_threshold_pct", makerConfig.MomentumThresholdPct))

	wrappedClient := &wsClientWrapper{wsClient, logger}
	makerStrategy := maker.NewMakerStrategy(futuresClient, wrappedClient, makerConfig, logger)

	// Final verification - log the exact config being used
	logger.Info("🎯 FINAL CONFIG VERIFICATION",
		zap.String("symbol_for_websocket", originalSymbol),
		zap.String("symbol_for_maker_config", normalizedSymbol),
		zap.Strings("maker_config_symbols", makerConfig.Symbols),
		zap.Float64("spread_bps", makerConfig.DefaultSpreadBps),
		zap.Int("grid_levels", makerConfig.MicroGridLevels))

	// Start strategy in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Maker Strategy goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		logger.Info("🔄 Starting Maker Strategy")
		if err := makerStrategy.Start(ctx); err != nil {
			logger.Error("Maker Strategy error", zap.Error(err))
		}
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("✅ Adaptive Volume Farm Maker started successfully")
	logger.Info("Press Ctrl+C to stop")

	<-sigChan

	logger.Info("🛑 Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := makerStrategy.Stop(shutdownCtx); err != nil {
		logger.Error("Error stopping Maker Strategy", zap.Error(err))
	}

	logger.Info("✅ Stopped gracefully")
}

// startLegacyMode starts single symbol with static configuration (original behavior)
func startLegacyMode(ctx context.Context, symbol string, cfg *config.Config,
	futuresClient *client.FuturesClient, dryRun bool, logger *zap.Logger,
	spreadBps float64, maxLeverage int, useDynamicSizing bool, baseNotionalUSD float64,
	microProfitMode bool, microGridSpacingBps float64, microGridLevels int,
	toxicFlowDetection bool, positionBiasThreshold float64, momentumDetection bool, momentumThreshold float64) {

	logger.Info("🔄 Starting Legacy Mode", zap.String("symbol", symbol))

	// Use symbol-specific WebSocket URL
	wsURL := fmt.Sprintf("wss://fstream.asterdex.com/ws/%s@bookTicker", strings.ToLower(symbol))

	wsClient := client.NewWebSocketClient(wsURL, logger)
	if err := wsClient.Connect(ctx); err != nil {
		logger.Fatal("Failed to connect WebSocket", zap.Error(err))
	}
	logger.Info("WebSocket connected", zap.String("url", wsURL))

	// Wait for initial ticker data
	time.Sleep(2 * time.Second)

	makerConfig := maker.DefaultConfig()
	makerConfig.Symbols = []string{symbol}
	makerConfig.DefaultSpreadBps = spreadBps
	makerConfig.MaxLeverage = maxLeverage

	// Apply configuration flags
	makerConfig.UseDynamicSizing = useDynamicSizing
	makerConfig.BaseNotionalUSD = baseNotionalUSD
	makerConfig.MicroProfitMode = microProfitMode
	makerConfig.MicroGridSpacingBps = microGridSpacingBps
	makerConfig.MicroGridLevels = microGridLevels
	makerConfig.ToxicFlowDetection = toxicFlowDetection
	makerConfig.PositionBiasThreshold = positionBiasThreshold
	makerConfig.MomentumDetection = momentumDetection
	makerConfig.MomentumThresholdPct = momentumThreshold

	logger.Info("Maker Strategy Config",
		zap.Strings("symbols", makerConfig.Symbols),
		zap.Float64("spread_bps", makerConfig.DefaultSpreadBps),
		zap.Int("max_leverage", makerConfig.MaxLeverage),
		zap.Bool("dynamic_sizing", makerConfig.UseDynamicSizing),
		zap.Bool("micro_profit_mode", makerConfig.MicroProfitMode),
		zap.Float64("micro_grid_spacing_bps", makerConfig.MicroGridSpacingBps),
		zap.Int("micro_grid_levels", makerConfig.MicroGridLevels),
		zap.Bool("toxic_flow_detection", makerConfig.ToxicFlowDetection),
		zap.Float64("position_bias_threshold", makerConfig.PositionBiasThreshold),
		zap.Bool("momentum_detection", makerConfig.MomentumDetection),
		zap.Float64("momentum_threshold_pct", makerConfig.MomentumThresholdPct))

	wrappedClient := &wsClientWrapper{wsClient, logger}
	makerStrategy := maker.NewMakerStrategy(futuresClient, wrappedClient, makerConfig, logger)

	// Start strategy in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Maker Strategy goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		logger.Info("🔄 Starting Maker Strategy")
		if err := makerStrategy.Start(ctx); err != nil {
			logger.Error("Maker Strategy error", zap.Error(err))
		}
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("✅ Legacy Volume Farm Maker started successfully")
	logger.Info("Press Ctrl+C to stop")

	<-sigChan

	logger.Info("🛑 Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := makerStrategy.Stop(shutdownCtx); err != nil {
		logger.Error("Error stopping Maker Strategy", zap.Error(err))
	}

	logger.Info("✅ Stopped gracefully")
}

type wsClientWrapper struct {
	wsClient *client.WebSocketClient
	logger   *zap.Logger
}

func (w *wsClientWrapper) SubscribeToTicker(symbols []string) error {
	// Skip subscription - using aggregate stream !ticker@arr which sends all data automatically
	w.logger.Info("Using aggregate ticker stream - no subscription needed", zap.Strings("symbols", symbols))
	return nil
}

func (w *wsClientWrapper) GetTickerChannel() <-chan map[string]interface{} {
	return w.wsClient.GetTickerChannel()
}

func (w *wsClientWrapper) GetTickerData(symbol string) (bestBid, bestAsk, volume24h float64, err error) {
	return w.wsClient.GetTickerData(symbol)
}

func (w *wsClientWrapper) IsRunning() bool {
	return w.wsClient.IsRunning()
}

func (w *wsClientWrapper) GetCachedPositions() map[string]client.Position {
	return w.wsClient.GetCachedPositions()
}

func (w *wsClientWrapper) GetCachedBalance() client.Balance {
	return w.wsClient.GetCachedBalance()
}
