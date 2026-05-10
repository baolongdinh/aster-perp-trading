package main

import (
	"context"
	"flag"
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
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	dryRun := flag.Bool("dry-run", false, "Run in dry-run mode (no real orders)")
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

	flag.Parse()

	logger, err := zap.NewDevelopment()
	if err != nil {
		panic("failed to initialize logger")
	}
	defer logger.Sync()

	logger.Info("🚀 Starting Volume Farm Micro Profit Engine (Maker Strategy Only)")

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

	// Use bookTicker stream for real-time best bid/ask
	wsURL := "wss://fstream.asterdex.com/ws/ethusd1@bookTicker"

	wsClient := client.NewWebSocketClient(wsURL, logger)
	if err := wsClient.Connect(context.Background()); err != nil {
		logger.Fatal("Failed to connect WebSocket", zap.Error(err))
	}
	logger.Info("WebSocket connected", zap.String("url", wsURL))

	// Wait for initial ticker data to populate cache
	time.Sleep(2 * time.Second)

	makerConfig := maker.DefaultConfig()
	// Only trade ETHUSD1 for simplicity
	makerConfig.Symbols = []string{"ethusd1"}
	makerConfig.DefaultSpreadBps = *spreadBps
	makerConfig.MaxLeverage = *maxLeverage

	// Apply new config flags
	makerConfig.UseDynamicSizing = *useDynamicSizing
	makerConfig.BaseNotionalUSD = *baseNotionalUSD
	makerConfig.MicroProfitMode = *microProfitMode
	makerConfig.MicroGridSpacingBps = *microGridSpacingBps
	makerConfig.MicroGridLevels = *microGridLevels
	makerConfig.ToxicFlowDetection = *toxicFlowDetection
	makerConfig.PositionBiasThreshold = *positionBiasThreshold
	makerConfig.MomentumDetection = *momentumDetection
	makerConfig.MomentumThresholdPct = *momentumThreshold

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
			cancel()
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("✅ Volume Farm Micro Profit Engine started successfully")
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

func parseSymbols(s string) []string {
	if s == "" {
		return []string{"btcusd1", "ethusd1"}
	}
	var symbols []string
	for _, sym := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(sym)
		if trimmed != "" {
			symbols = append(symbols, trimmed)
		}
	}
	if len(symbols) == 0 {
		return []string{"btcusd1", "ethusd1"}
	}
	return symbols
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
