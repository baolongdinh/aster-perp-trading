package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"aster-bot/internal/config"
	"aster-bot/internal/farming"

	"go.uber.org/zap"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	dryRun := flag.Bool("dry-run", false, "Run in dry-run mode (no real orders)")
	port := flag.Int("port", 8081, "API server port (different from main bot)")
	flag.Parse()

	// Initialize production logger (no debug spam)
	logger, err := zap.NewProduction()
	if err != nil {
		panic("failed to initialize logger")
	}
	defer logger.Sync()

	logger.Info("🚀 Starting Aster Volume Farming Bot (Production Mode)")

	// Load main configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Load volume farming configuration
	volumeFarmConfigPath := "config/volume-farm-config.yaml"
	volumeCfg, err := config.LoadVolumeFarming(volumeFarmConfigPath)
	if err != nil {
		logger.Warn("Failed to load volume farming config, using defaults", zap.Error(err))
		// Don't create default, let the engine use its defaults
		volumeCfg = nil
	} else {
		logger.Info("Loaded volume farming config", zap.String("path", volumeFarmConfigPath))
	}

	// Set volume farming config in main config
	cfg.VolumeFarming = volumeCfg

	// Merge volume farming risk config into main risk config if available
	if volumeCfg != nil {
		logger.Info("Merging volume farming risk config",
			zap.Float64("max_position_usdt", volumeCfg.Risk.MaxPositionUSDTPerSymbol),
			zap.Float64("daily_loss_limit", volumeCfg.Risk.DailyLossLimitUSDT),
			zap.Int("max_open_positions", volumeCfg.Risk.MaxOpenPositions),
		)
		// Only override if volume farming config has valid values
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

	// Load optimization configs (dynamic_grid, inventory_skew, cluster_stoploss, etc.)
	optimizationConfigPath := "config"
	optCfg, err := config.LoadOptimizationConfig(optimizationConfigPath)
	if err != nil {
		logger.Warn("Failed to load optimization configs, using defaults", zap.Error(err))
	} else {
		logger.Info("Loaded optimization configs", zap.String("path", optimizationConfigPath))
		cfg.Optimization = optCfg
	}

	// Override dry-run if flag is set
	if *dryRun {
		cfg.Bot.DryRun = true
		logger.Info("🧪 Running in DRY-RUN mode")
	}

	// Override API port for volume farming
	cfg.API.Port = *port

	// Initialize farming engine
	engine, err := farming.NewVolumeFarmEngine(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize volume farming engine", zap.Error(err))
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start farming engine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Volume Farm Engine goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		logger.Info("🔄 Starting Volume Farming Engine")
		if err := engine.Start(ctx); err != nil {
			logger.Error("Volume farming engine error", zap.Error(err))
			cancel()
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("✅ Volume Farming Bot started successfully")
	logger.Info("Press Ctrl+C to stop")

	// Block until signal received
	<-sigChan

	logger.Info("🛑 Shutting down Volume Farming Bot...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop farming engine
	if err := engine.Stop(shutdownCtx); err != nil {
		logger.Error("Error stopping farming engine", zap.Error(err))
	}

	logger.Info("✅ Volume Farming Bot stopped gracefully")
}
