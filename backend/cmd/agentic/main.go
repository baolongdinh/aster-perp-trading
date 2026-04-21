package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"aster-bot/internal/agentic"
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

	// // Override dry-run if flag is set
	// if *dryRun {
	// 	cfg.Bot.DryRun = true
	// 	logger.Info("🧪 Running in DRY-RUN mode")
	// }

	// Override API port
	cfg.API.Port = *port

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Initialize Volume Farm Engine (Execution Layer)
	logger.Info("🔧 Initializing Volume Farm Engine...")
	vfEngine, err := farming.NewVolumeFarmEngine(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize Volume Farm Engine", zap.Error(err))
	}

	// Start Volume Farm Engine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Volume Farm Engine goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		logger.Info("▶️ Starting Volume Farm Engine")
		if err := vfEngine.Start(ctx); err != nil {
			logger.Error("Volume Farm Engine error", zap.Error(err))
			cancel()
		}
		logger.Info("✅ Volume Farm Engine started successfully")
	}()

	// Initialize and start Agentic Engine (Decision Layer) if enabled
	var agenticEngine *agentic.AgenticEngine
	if !*vfOnly && cfg.Agentic != nil && cfg.Agentic.Enabled {
		if err := vfEngine.WaitUntilReady(ctx, 45*time.Second); err != nil {
			logger.Fatal("Volume Farm runtime was not ready for Agentic startup", zap.Error(err))
		}
		logger.Info("🧠 Initializing Agentic Engine...")

		agenticEngine, err = agentic.NewAgenticEngine(
			cfg.Agentic,
			vfEngine.GetMarketStateProvider(),
			vfEngine.GetRuntimeSnapshotProvider(),
			vfEngine,
			logger,
		)
		if err != nil {
			logger.Fatal("Failed to initialize Agentic Engine", zap.Error(err))
		}

		// NEW: Hybrid Integration - Wire VF Event Handler to Agentic Event Bus
		// This enables event-driven state transitions between Agentic (decision) and VF (execution)
		logger.Info("🔗 Setting up hybrid integration (Agentic ↔ VF)...")
		vfEventHandler := farming.NewAgenticEventHandler(vfEngine, logger)

		// Subscribe VF handler to Agentic's event bus
		if bus := agenticEngine.GetStateEventBus(); bus != nil {
			bus.GetPublisher().Subscribe(vfEventHandler)
			vfEngine.SetStateEventBus(bus)
			logger.Info("✅ Hybrid integration active - Agentic decisions → VF execution")
		} else {
			logger.Warn("⚠️ StateEventBus not available - hybrid integration disabled")
		}

		// Start Agentic Engine
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Agentic Engine goroutine panic recovered",
						zap.Any("panic", r),
						zap.String("stack", string(debug.Stack())))
				}
			}()
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

	// Merge adaptive state machine configs
	if volumeCfg.Risk.MarketConditionEvaluator != nil {
		cfg.Risk.MarketConditionEvaluator = volumeCfg.Risk.MarketConditionEvaluator
	}
	if volumeCfg.Risk.OverSize != nil {
		cfg.Risk.OverSize = volumeCfg.Risk.OverSize
	}
	if volumeCfg.Risk.DefensiveState != nil {
		cfg.Risk.DefensiveState = volumeCfg.Risk.DefensiveState
	}
	if volumeCfg.Risk.RecoveryState != nil {
		cfg.Risk.RecoveryState = volumeCfg.Risk.RecoveryState
	}
	if volumeCfg.Risk.PnLRiskControl != nil {
		cfg.Risk.PnLRiskControl = volumeCfg.Risk.PnLRiskControl
	}
}
