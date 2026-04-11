package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aster-bot/internal/agentic"
	"aster-bot/internal/auth"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming"

	"go.uber.org/zap"
)

var (
	configPath = flag.String("config", "config/agentic-vf-config.yaml", "Path to unified agentic-vf configuration file")
	dryRun     = flag.Bool("dry-run", false, "Run in dry-run mode (no real orders)")
	port       = flag.Int("port", 8081, "API server port")
	vfOnly     = flag.Bool("vf-only", false, "Run only Volume Farm (disable Agentic layer)")
)

func main() {
	flag.Parse()

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}
	defer logger.Sync()

	logger.Info("🚀 Starting Agentic + Volume Farm Trading Bot")

	// Load main configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Load volume farming configuration
	volumeFarmConfigPath := "config/volume-farm-config.yaml"
	if _, err := os.Stat(*configPath); err == nil {
		// Use unified config if it exists
		volumeFarmConfigPath = *configPath
	}

	volumeCfg, err := config.LoadVolumeFarming(volumeFarmConfigPath)
	if err != nil {
		logger.Warn("Failed to load volume farming config, using defaults", zap.Error(err))
		volumeCfg = nil
	} else {
		logger.Info("Loaded volume farming config", zap.String("path", volumeFarmConfigPath))
	}

	// Set volume farming config in main config
	cfg.VolumeFarming = volumeCfg

	// Merge volume farming risk config into main risk config
	if volumeCfg != nil {
		mergeRiskConfig(cfg, volumeCfg)
	}

	// Override dry-run if flag is set
	if *dryRun {
		cfg.Bot.DryRun = true
		logger.Info("🧪 Running in DRY-RUN mode")
	}

	// Override API port
	cfg.API.Port = *port

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create HTTP client for authentication
	httpClient := createHTTPClient(cfg, logger)

	// Initialize Volume Farm Engine (Execution Layer)
	logger.Info("🔧 Initializing Volume Farm Engine...")
	vfEngine, err := farming.NewVolumeFarmEngine(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize Volume Farm Engine", zap.Error(err))
	}

	// Start Volume Farm Engine
	go func() {
		logger.Info("▶️ Starting Volume Farm Engine")
		if err := vfEngine.Start(ctx); err != nil {
			logger.Error("Volume Farm Engine error", zap.Error(err))
			cancel()
		}
	}()

	// Initialize and start Agentic Engine (Decision Layer) if enabled
	var agenticEngine *agentic.AgenticEngine
	if !*vfOnly && cfg.Agentic != nil && cfg.Agentic.Enabled {
		logger.Info("🧠 Initializing Agentic Engine...")

		agenticEngine, err = agentic.NewAgenticEngine(
			cfg.Agentic,
			httpClient,
			vfEngine,
			logger,
		)
		if err != nil {
			logger.Fatal("Failed to initialize Agentic Engine", zap.Error(err))
		}

		// Start Agentic Engine
		go func() {
			logger.Info("▶️ Starting Agentic Engine")
			if err := agenticEngine.Start(ctx); err != nil {
				logger.Error("Agentic Engine error", zap.Error(err))
			}
		}()
	} else {
		logger.Info("⏭️ Skipping Agentic Engine (running VF only)")
	}

	logger.Info("✅ Bot started successfully")
	logger.Info("Press Ctrl+C to stop")

	// Wait for shutdown signal
	<-sigCh
	logger.Info("🛑 Shutdown signal received, stopping...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop Agentic Engine first
	if agenticEngine != nil {
		logger.Info("Stopping Agentic Engine...")
		if err := agenticEngine.Stop(); err != nil {
			logger.Error("Error stopping Agentic Engine", zap.Error(err))
		}
	}

	// Stop Volume Farm Engine
	logger.Info("Stopping Volume Farm Engine...")
	if err := vfEngine.Stop(shutdownCtx); err != nil {
		logger.Error("Error stopping Volume Farm Engine", zap.Error(err))
	}

	// Cancel main context
	cancel()

	logger.Info("✅ Bot stopped gracefully")
}

// createHTTPClient creates an HTTP client based on configuration
func createHTTPClient(cfg *config.Config, logger *zap.Logger) *client.HTTPClient {
	// V3 Authentication
	if cfg.Exchange.UserWallet != "" && cfg.Exchange.APISigner != "" && cfg.Exchange.APISignerKey != "" {
		v3Signer, err := auth.NewV3Signer(
			cfg.Exchange.UserWallet,
			cfg.Exchange.APISigner,
			cfg.Exchange.APISignerKey,
			int64(cfg.Exchange.RecvWindow),
		)
		if err != nil {
			logger.Fatal("Failed to create V3 signer", zap.Error(err))
		}
		return client.NewHTTPClientV3(cfg.Exchange.FuturesRESTBase, v3Signer, logger, cfg.Exchange.RequestsPerSecond)
	}

	// V1 Authentication (deprecated)
	if cfg.Exchange.APIKey != "" {
		v1Signer, err := auth.NewSigner(cfg.Exchange.APIKey, cfg.Exchange.APISecret, cfg.Exchange.RecvWindow)
		if err != nil {
			logger.Fatal("Failed to create V1 signer", zap.Error(err))
		}
		return client.NewHTTPClient(cfg.Exchange.FuturesRESTBase, v1Signer, logger, cfg.Exchange.RequestsPerSecond)
	}

	logger.Fatal("No valid authentication credentials found")
	return nil
}

// mergeRiskConfig merges volume farming risk config into main config
func mergeRiskConfig(cfg *config.Config, volumeCfg *config.VolumeFarmConfig) {
	if volumeCfg.Risk.MaxPositionUSDTPerSymbol > 0 {
		cfg.Risk.MaxPositionUSDTPerSymbol = volumeCfg.Risk.MaxPositionUSDTPerSymbol
	}
	if volumeCfg.Risk.MaxTotalPositionsUSDT > 0 {
		cfg.Risk.MaxTotalPositionsUSDT = volumeCfg.Risk.MaxTotalPositionsUSDT
	}
	if volumeCfg.Risk.DailyLossLimitUSDT > 0 {
		cfg.Risk.DailyLossLimitUSDT = volumeCfg.Risk.DailyLossLimitUSDT
	}
	if volumeCfg.Risk.DailyDrawdownPct > 0 {
		cfg.Risk.DailyDrawdownPct = volumeCfg.Risk.DailyDrawdownPct
	}
	if volumeCfg.Risk.MaxOpenPositions > 0 {
		cfg.Risk.MaxOpenPositions = volumeCfg.Risk.MaxOpenPositions
	}
	if volumeCfg.Risk.MaxGlobalPendingLimitOrders > 0 {
		cfg.Risk.MaxGlobalPendingLimitOrders = volumeCfg.Risk.MaxGlobalPendingLimitOrders
	}
	if volumeCfg.Risk.MaxPendingPerSide > 0 {
		cfg.Risk.MaxPendingPerSide = volumeCfg.Risk.MaxPendingPerSide
	}
	if volumeCfg.Risk.PerTradeStopLossPct > 0 {
		cfg.Risk.PerTradeStopLossPct = volumeCfg.Risk.PerTradeStopLossPct
	}
	if volumeCfg.Risk.PerTradeTakeProfitPct > 0 {
		cfg.Risk.PerTradeTakeProfitPct = volumeCfg.Risk.PerTradeTakeProfitPct
	}
}
