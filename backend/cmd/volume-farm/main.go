package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
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
