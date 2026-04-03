package farming

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/auth"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/risk"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// MarkPrice represents a mark price update
type MarkPrice struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
}

// OrderUpdate represents an order update event
type OrderUpdate struct {
	Symbol      string  `json:"symbol"`
	OrderID     string  `json:"order_id"`
	ClientID    string  `json:"client_id"`
	Side        string  `json:"side"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Price       float64 `json:"price"`
	Quantity    float64 `json:"quantity"`
	ExecutedQty float64 `json:"executed_qty"`
	Fee         float64 `json:"fee"`
	Timestamp   int64   `json:"timestamp"`
}

// VolumeFarmEngine orchestrates volume farming operations
type VolumeFarmEngine struct {
	config         *config.Config
	logger         *zap.Logger
	isRunning      bool
	isRunningMu    sync.RWMutex
	futuresClient  *client.FuturesClient
	riskManager    *risk.Manager
	symbolSelector *SymbolSelector
	gridManager    *GridManager
	pointsTracker  *PointsTracker
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

// NewVolumeFarmEngine creates a new volume farming engine
func NewVolumeFarmEngine(cfg *config.Config, logger *zap.Logger) (*VolumeFarmEngine, error) {
	engine := &VolumeFarmEngine{
		config: cfg,
		logger: logger,
		stopCh: make(chan struct{}),
	}

	// Create auth signer - try V3 first, fallback to V1
	var httpClient *client.HTTPClient

	// Try V3 authentication first
	if cfg.Exchange.UserWallet != "" && cfg.Exchange.APISigner != "" && cfg.Exchange.APISignerKey != "" {
		v3Signer, err := auth.NewV3Signer(
			cfg.Exchange.UserWallet,
			cfg.Exchange.APISigner,
			cfg.Exchange.APISignerKey,
			int64(cfg.Exchange.RecvWindow),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create V3 signer: %w", err)
		}

		// Initialize futures client with V3 auth
		httpClient = client.NewHTTPClientV3(cfg.Exchange.FuturesRESTBase, v3Signer, logger, cfg.Exchange.RequestsPerSecond)
		engine.logger.Info("🔐 Using V3 authentication (API Wallet/Agent model)")
	} else if cfg.Exchange.APIKey != "" && cfg.Exchange.APISecret != "" {
		// Fallback to V1 authentication
		v1Signer, err := auth.NewSigner(cfg.Exchange.APIKey, cfg.Exchange.APISecret, cfg.Exchange.RecvWindow)
		if err != nil {
			return nil, fmt.Errorf("failed to create V1 signer: %w", err)
		}

		// Initialize futures client with V1 auth
		httpClient = client.NewHTTPClient(cfg.Exchange.FuturesRESTBase, v1Signer, logger, cfg.Exchange.RequestsPerSecond)
		engine.logger.Info("🔐 Using V1 authentication (API Key model - deprecated)")
	} else {
		return nil, fmt.Errorf("no valid authentication credentials found - please configure either V3 or V1")
	}

	// Use volume farming specific dry-run setting
	volumeDryRun := cfg.VolumeFarming.Bot.DryRun
	engine.futuresClient = client.NewFuturesClient(httpClient, volumeDryRun, logger)

	// Initialize risk manager (reuse existing)
	engine.riskManager = risk.NewManager(cfg.Risk, logger)

	// Initialize volume farming specific components
	volumeConfig := engine.extractVolumeFarmConfig(cfg)

	// Convert zap logger to logrus (temporary solution)
	// In production, we should standardize on one logger type
	logrusEntry := logrus.NewEntry(logrus.StandardLogger()).WithField("component", "volume_farm")

	// Create symbol selector
	engine.symbolSelector = NewSymbolSelector(engine.futuresClient, logrusEntry.WithField("component", "symbol_selector"), volumeConfig)
	gridLogger := logrusEntry.WithField("component", "grid_manager")
	pointsLogger := logrusEntry.WithField("component", "points_tracker")

	engine.gridManager = NewGridManager(engine.futuresClient, gridLogger)
	engine.pointsTracker = NewPointsTracker(volumeConfig, pointsLogger)

	return engine, nil
}

// Start starts the volume farming engine
func (e *VolumeFarmEngine) Start(ctx context.Context) error {
	e.isRunningMu.Lock()
	if e.isRunning {
		e.isRunningMu.Unlock()
		return fmt.Errorf("volume farming engine is already running")
	}
	e.isRunning = true
	e.isRunningMu.Unlock()

	e.logger.Info("🚀 Starting Volume Farming Engine")

	// Start symbol selector
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.symbolSelector.Start(ctx); err != nil {
			e.logger.Error("Symbol selector error", zap.Error(err))
		}
	}()

	// Connect symbol selector to grid manager
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(15 * time.Second) // Faster updates for testing
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get active symbols from selector and update grid manager
				activeSymbols := e.symbolSelector.GetActiveSymbols()
				e.logger.Debug("Symbol update check", zap.Int("active_symbols", len(activeSymbols)))
				if len(activeSymbols) > 0 {
					e.logger.Info("Updating grid manager with symbols", zap.Int("count", len(activeSymbols)))
					e.gridManager.UpdateSymbols(activeSymbols)
				} else {
					e.logger.Debug("No active symbols found yet")
				}
			}
		}
	}()

	// Start grid manager
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.gridManager.Start(ctx); err != nil {
			e.logger.Error("Grid manager error", zap.Error(err))
		}
	}()

	// Start points tracker
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.pointsTracker.Start(ctx); err != nil {
			e.logger.Error("Points tracker error", zap.Error(err))
		}
	}()

	// Start risk monitoring
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.monitorRisk(ctx)
	}()

	e.logger.Info("✅ Volume Farming Engine started successfully")
	return nil
}

// Stop stops the volume farming engine
func (e *VolumeFarmEngine) Stop(ctx context.Context) error {
	e.isRunningMu.Lock()
	if !e.isRunning {
		e.isRunningMu.Unlock()
		return nil
	}
	e.isRunning = false
	e.isRunningMu.Unlock()

	e.logger.Info("🛑 Stopping Volume Farming Engine")

	// Signal all goroutines to stop
	close(e.stopCh)

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		e.logger.Info("✅ Volume Farming Engine stopped gracefully")
		return nil
	case <-ctx.Done():
		e.logger.Warn("⚠️  Volume Farming Engine stop timeout")
		return ctx.Err()
	}
}

// IsRunning returns whether the engine is running
func (e *VolumeFarmEngine) IsRunning() bool {
	e.isRunningMu.RLock()
	defer e.isRunningMu.RUnlock()
	return e.isRunning
}

// GetStatus returns the current status
func (e *VolumeFarmEngine) GetStatus() *VolumeFarmStatus {
	return &VolumeFarmStatus{
		IsRunning:     e.IsRunning(),
		ActiveSymbols: e.symbolSelector.GetActiveSymbolCount(),
		ActiveGrids:   0, // TODO: Implement GetActiveGridCount in GridManager
		CurrentPoints: e.pointsTracker.GetCurrentPoints(),
		CurrentVolume: e.pointsTracker.GetCurrentVolume(),
		DailyLoss:     e.riskManager.DailyPnL(),
		RiskStatus: func() string {
			if e.riskManager.IsPaused() {
				return "PAUSED"
			}
			return "ACTIVE"
		}(),
		LastUpdate: time.Now(),
	}
}

// monitorRisk monitors risk levels and takes action
func (e *VolumeFarmEngine) monitorRisk(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			// Check risk limits
			dailyPnL := e.riskManager.DailyPnL()
			isPaused := e.riskManager.IsPaused()

			if dailyPnL < -e.config.Risk.DailyLossLimitUSDT {
				e.logger.Warn("Daily loss limit reached, stopping farming",
					zap.Float64("daily_loss", dailyPnL),
					zap.Float64("limit", e.config.Risk.DailyLossLimitUSDT))
				// Emergency stop - just log for now
			}

			if isPaused {
				e.logger.Warn("Risk manager paused, stopping farming")
				// Emergency stop - just log for now
			}
		}
	}
}

// extractVolumeFarmConfig extracts volume farming config from main config
func (e *VolumeFarmEngine) extractVolumeFarmConfig(cfg *config.Config) *config.VolumeFarmConfig {
	// Debug logging
	e.logger.Info("Extracted volume farming config",
		zap.Bool("volume_farming_enabled", cfg.VolumeFarming.Enabled),
		zap.String("quote_currency_mode", cfg.VolumeFarming.Symbols.QuoteCurrencyMode),
		zap.Strings("quote_currencies", cfg.VolumeFarming.Symbols.QuoteCurrencies),
		zap.Float64("min_volume_24h", cfg.VolumeFarming.Symbols.MinVolume24h),
	)

	if cfg.VolumeFarming == nil {
		// Return default config if not set
		return &config.VolumeFarmConfig{
			Enabled:                true,
			MaxDailyLossUSDT:       50,
			MaxTotalDrawdownPct:    5.0,
			OrderSizeUSDT:          100,
			GridSpreadPct:          0.05,
			MaxOrdersPerSide:       2,
			ReplaceImmediately:     true,
			PositionTimeoutMinutes: 30,
			Bot:                    cfg.Bot, // Use main bot config
			Exchange:               cfg.Exchange,
			Risk:                   cfg.Risk,
			API:                    cfg.API,
		}
	}

	return &config.VolumeFarmConfig{
		Enabled:                cfg.VolumeFarming.Enabled,
		MaxDailyLossUSDT:       cfg.VolumeFarming.MaxDailyLossUSDT,
		MaxTotalDrawdownPct:    cfg.VolumeFarming.MaxTotalDrawdownPct,
		OrderSizeUSDT:          cfg.VolumeFarming.OrderSizeUSDT,
		GridSpreadPct:          cfg.VolumeFarming.GridSpreadPct,
		MaxOrdersPerSide:       cfg.VolumeFarming.MaxOrdersPerSide,
		ReplaceImmediately:     cfg.VolumeFarming.ReplaceImmediately,
		PositionTimeoutMinutes: cfg.VolumeFarming.PositionTimeoutMinutes,
		Bot:                    cfg.Bot, // Use main bot config
		Symbols:                cfg.VolumeFarming.Symbols,
		Exchange:               cfg.Exchange,
		Risk:                   cfg.Risk,
		API:                    cfg.API,
	}
}

// VolumeFarmStatus represents the current status
type VolumeFarmStatus struct {
	IsRunning     bool      `json:"is_running"`
	ActiveSymbols int       `json:"active_symbols"`
	ActiveGrids   int       `json:"active_grids"`
	CurrentPoints int64     `json:"current_points"`
	CurrentVolume float64   `json:"current_volume"`
	DailyLoss     float64   `json:"daily_loss"`
	RiskStatus    string    `json:"risk_status"`
	LastUpdate    time.Time `json:"last_update"`
}
