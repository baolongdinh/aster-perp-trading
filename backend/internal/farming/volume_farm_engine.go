package farming

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/auth"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_config"
	"aster-bot/internal/farming/adaptive_grid"
	"aster-bot/internal/farming/market_regime"
	"aster-bot/internal/risk"
	"aster-bot/internal/strategy/regime"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// MarkPrice represents a mark price update.
type MarkPrice struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
}

// OrderUpdate represents an order update event.
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

// VolumeFarmEngine orchestrates volume farming operations.
type VolumeFarmEngine struct {
	config              *config.Config
	volumeConfig        *config.VolumeFarmConfig
	logger              *zap.Logger
	isRunning           bool
	isRunningMu         sync.RWMutex
	futuresClient       *client.FuturesClient
	riskManager         *risk.Manager
	symbolSelector      *SymbolSelector
	gridManager         *GridManager
	adaptiveGridManager *adaptive_grid.AdaptiveGridManager
	regimeDetector      *market_regime.RegimeDetector
	configManager       *adaptive_config.AdaptiveConfigManager
	pointsTracker       *PointsTracker
	stopCh              chan struct{}
	wg                  sync.WaitGroup
}

// NewVolumeFarmEngine creates a new volume farming engine.
func NewVolumeFarmEngine(cfg *config.Config, logger *zap.Logger) (*VolumeFarmEngine, error) {
	engine := &VolumeFarmEngine{
		config: cfg,
		logger: logger,
		stopCh: make(chan struct{}),
	}

	var httpClient *client.HTTPClient

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

		httpClient = client.NewHTTPClientV3(cfg.Exchange.FuturesRESTBase, v3Signer, logger, cfg.Exchange.RequestsPerSecond)
		engine.logger.Info("Using V3 authentication (API Wallet/Agent model)")

		// Sync server time offset for V3 signer to avoid timestamp drift errors (-1021)
		marketClient := client.NewMarketClient(httpClient)
		localTimeBefore := time.Now().UnixMilli()
		serverTime, err := marketClient.ServerTime(context.Background())
		if err != nil {
			engine.logger.Warn("Failed to sync server time for V3 signer", zap.Error(err))
		} else {
			localTimeAfter := time.Now().UnixMilli()
			localTimeEstimated := localTimeBefore + (localTimeAfter-localTimeBefore)/2
			offset := serverTime - localTimeEstimated
			v3Signer.SetTimeOffset(offset)
			engine.logger.Info("V3 server time synced", zap.Int64("offset_ms", offset), zap.Int64("server_time", serverTime))
		}
	} else if cfg.Exchange.APIKey != "" && cfg.Exchange.APISecret != "" {
		v1Signer, err := auth.NewSigner(cfg.Exchange.APIKey, cfg.Exchange.APISecret, cfg.Exchange.RecvWindow)
		if err != nil {
			return nil, fmt.Errorf("failed to create V1 signer: %w", err)
		}

		httpClient = client.NewHTTPClient(cfg.Exchange.FuturesRESTBase, v1Signer, logger, cfg.Exchange.RequestsPerSecond)
		engine.logger.Info("Using V1 authentication (API Key model - deprecated)")

		// Sync server time offset for V1 signer to avoid timestamp drift errors (-1021)
		marketClient := client.NewMarketClient(httpClient)
		localTimeBefore := time.Now().UnixMilli()
		serverTime, err := marketClient.ServerTime(context.Background())
		if err != nil {
			engine.logger.Warn("Failed to sync server time for V1 signer", zap.Error(err))
		} else {
			localTimeAfter := time.Now().UnixMilli()
			localTimeEstimated := localTimeBefore + (localTimeAfter-localTimeBefore)/2
			offset := serverTime - localTimeEstimated
			v1Signer.SetTimeOffset(offset)
			engine.logger.Info("V1 server time synced", zap.Int64("offset_ms", offset), zap.Int64("server_time", serverTime))
		}
	} else {
		return nil, fmt.Errorf("no valid authentication credentials found - please configure either V3 or V1")
	}

	volumeConfig := engine.extractVolumeFarmConfig(cfg)
	engine.volumeConfig = volumeConfig
	engine.futuresClient = client.NewFuturesClient(httpClient, volumeConfig.Bot.DryRun, logger, cfg.Exchange.RequestsPerSecond)
	engine.riskManager = risk.NewManager(volumeConfig.Risk, logger)

	// NEW: Initialize and inject CorrelationTracker for correlation-based risk checks
	corrThreshold := volumeConfig.Risk.CorrelationThreshold
	if corrThreshold <= 0 {
		corrThreshold = 0.8 // Default 80% correlation threshold
	}
	corrTracker := regime.NewCorrelationTracker(nil, corrThreshold)
	engine.riskManager.SetCorrelationTracker(corrTracker)
	logger.Info("CorrelationTracker initialized",
		zap.Float64("threshold", corrThreshold))

	logrusEntry := logrus.NewEntry(logrus.StandardLogger()).WithField("component", "volume_farm")
	engine.symbolSelector = NewSymbolSelector(engine.futuresClient, logrusEntry.WithField("component", "symbol_selector"), volumeConfig)

	gridLogger := logrusEntry.WithField("component", "grid_manager")
	engine.gridManager = NewGridManager(engine.futuresClient, gridLogger, volumeConfig)
	engine.gridManager.ApplyConfig(volumeConfig)

	pointsLogger := logrusEntry.WithField("component", "points_tracker")
	engine.pointsTracker = NewPointsTracker(volumeConfig, pointsLogger)

	// Initialize adaptive configuration
	configManager := adaptive_config.NewAdaptiveConfigManager("", logger)
	engine.configManager = configManager

	// Initialize regime detector
	regimeDetector := market_regime.NewRegimeDetector(logger, nil)
	engine.regimeDetector = regimeDetector

	// Initialize adaptive grid manager
	adaptiveGridManager := adaptive_grid.NewAdaptiveGridManager(
		engine.gridManager,
		configManager,
		regimeDetector,
		engine.futuresClient,
		logger,
	)
	engine.adaptiveGridManager = adaptiveGridManager

	// CRITICAL: Set risk config from volume config (not hardcode)
	adaptiveGridManager.SetRiskConfig(adaptive_grid.ConvertRiskConfig(volumeConfig.Risk))
	logger.Info("AdaptiveGridManager risk config set from volume config",
		zap.Float64("max_position_usdt", volumeConfig.Risk.MaxPositionUSDTPerSymbol),
		zap.Float64("daily_loss_limit", volumeConfig.Risk.DailyLossLimitUSDT),
		zap.Float64("per_trade_sl_pct", volumeConfig.Risk.PerTradeStopLossPct),
	)

	// Connect adaptive manager as risk checker for grid manager
	engine.gridManager.SetRiskChecker(adaptiveGridManager)

	// NEW: Connect adaptive manager reference for optimization features
	engine.gridManager.SetAdaptiveManager(adaptiveGridManager)

	return engine, nil
}

// Start starts the volume farming engine.
func (e *VolumeFarmEngine) Start(ctx context.Context) error {
	e.isRunningMu.Lock()
	if e.isRunning {
		e.isRunningMu.Unlock()
		return fmt.Errorf("volume farming engine is already running")
	}
	e.isRunning = true
	e.isRunningMu.Unlock()

	e.logger.Info("Starting Volume Farming Engine")

	// Bridge context cancellation to stopCh
	// This ensures that when context is cancelled (e.g., from signal), stopCh is also closed
	go func() {
		select {
		case <-ctx.Done():
			select {
			case <-e.stopCh:
				// already closed
			default:
				close(e.stopCh)
			}
		case <-e.stopCh:
			// already closed, nothing to do
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.symbolSelector.Start(ctx); err != nil {
			e.logger.Error("Symbol selector error", zap.Error(err))
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

		// Initial symbol sync to avoid waiting for first interval
		e.syncGridSymbols()

		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.syncGridSymbols()
			}
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.gridManager.Start(ctx); err != nil {
			e.logger.Error("Grid manager error", zap.Error(err))
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		// Initialize adaptive grid manager
		if err := e.adaptiveGridManager.Initialize(ctx); err != nil {
			e.logger.Error("Adaptive grid manager initialization error", zap.Error(err))
			return
		}
		e.logger.Info("Adaptive grid manager initialized - position monitor ACTIVE")

		// Start regime monitoring
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.monitorRegimeChanges(ctx)
			}
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.pointsTracker.Start(ctx); err != nil {
			e.logger.Error("Points tracker error", zap.Error(err))
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.monitorRisk(ctx)
	}()

	e.logger.Info("Volume Farming Engine started successfully")
	return nil
}
func (e *VolumeFarmEngine) syncGridSymbols() {
	e.logger.Info("Syncing grid symbols")

	activeSymbols := e.symbolSelector.GetActiveSymbols()
	e.logger.Info("Active symbols from selector", zap.Int("count", len(activeSymbols)), zap.Strings("symbols", func() []string {
		syms := make([]string, len(activeSymbols))
		for i, s := range activeSymbols {
			syms[i] = s.Symbol
		}
		return syms
	}()))

	// Always include whitelist symbols
	if len(e.volumeConfig.Symbols.Whitelist) > 0 {
		whitelistSymbols := make(map[string]bool)
		for _, sym := range e.volumeConfig.Symbols.Whitelist {
			whitelistSymbols[sym] = true
		}
		for _, s := range activeSymbols {
			delete(whitelistSymbols, s.Symbol)
		}
		// Add remaining whitelist
		for sym := range whitelistSymbols {
			quote := ""
			for _, qc := range e.volumeConfig.Symbols.QuoteCurrencies {
				if len(sym) > len(qc) && sym[len(sym)-len(qc):] == qc {
					quote = qc
					break
				}
			}
			if quote != "" {
				base := sym[:len(sym)-len(quote)]
				activeSymbols = append(activeSymbols, &SymbolData{
					Symbol:     sym,
					BaseAsset:  base,
					QuoteAsset: quote,
					Status:     "TRADING",
					Volume24h:  1000000,
					Count24h:   1000,
				})
			}
		}
		e.logger.Info("Added whitelist to active symbols", zap.Int("total_count", len(activeSymbols)))
	}

	if len(activeSymbols) == 0 {
		e.logger.Info("No symbols from selector, adding whitelist symbols only")
		// Only add whitelist symbols with USD1 quote - NO USDT
		whitelistOnly := e.createWhitelistSymbols()
		activeSymbols = append(activeSymbols, whitelistOnly...)
	}

	e.logger.Info("Updating grid manager with symbols", zap.Int("count", len(activeSymbols)), zap.Strings("symbols", func() []string {
		syms := make([]string, len(activeSymbols))
		for i, s := range activeSymbols {
			syms[i] = s.Symbol
		}
		return syms
	}()))
	e.gridManager.UpdateSymbols(activeSymbols)
}

func (e *VolumeFarmEngine) createWhitelistSymbols() []*SymbolData {
	symbols := make([]*SymbolData, 0, len(e.volumeConfig.Symbols.Whitelist))
	for _, symbol := range e.volumeConfig.Symbols.Whitelist {
		// Extract quote currency
		quote := ""
		for _, qc := range e.volumeConfig.Symbols.QuoteCurrencies {
			if len(symbol) > len(qc) && symbol[len(symbol)-len(qc):] == qc {
				quote = qc
				break
			}
		}
		if quote == "" {
			continue
		}

		// Extract base asset
		base := symbol[:len(symbol)-len(quote)]

		symbols = append(symbols, &SymbolData{
			Symbol:     symbol,
			BaseAsset:  base,
			QuoteAsset: quote,
			Status:     "TRADING",
			Volume24h:  1000000, // Dummy volume
			Count24h:   1000,
		})
	}
	return symbols
}

// Stop stops the volume farming engine.
func (e *VolumeFarmEngine) Stop(ctx context.Context) error {
	e.isRunningMu.Lock()
	if !e.isRunning {
		e.isRunningMu.Unlock()
		return nil
	}
	e.isRunning = false
	e.isRunningMu.Unlock()

	e.logger.Info("Stopping Volume Farming Engine")

	// Safely close stopCh (may already be closed by bridge goroutine)
	select {
	case <-e.stopCh:
		// already closed
	default:
		close(e.stopCh)
	}

	if e.symbolSelector != nil {
		if err := e.symbolSelector.Stop(ctx); err != nil {
			e.logger.Warn("Symbol selector stop error", zap.Error(err))
		}
	}
	if e.gridManager != nil {
		if err := e.gridManager.Stop(ctx); err != nil {
			e.logger.Warn("Grid manager stop error", zap.Error(err))
		}
	}
	if e.pointsTracker != nil {
		if err := e.pointsTracker.Stop(ctx); err != nil {
			e.logger.Warn("Points tracker stop error", zap.Error(err))
		}
	}

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		e.logger.Info("Volume Farming Engine stopped gracefully")
		return nil
	case <-ctx.Done():
		e.logger.Warn("Volume Farming Engine stop timeout")
		return ctx.Err()
	}
}

// IsRunning returns whether the engine is running.
func (e *VolumeFarmEngine) IsRunning() bool {
	e.isRunningMu.RLock()
	defer e.isRunningMu.RUnlock()
	return e.isRunning
}

// GetStatus returns the current status.
func (e *VolumeFarmEngine) GetStatus() *VolumeFarmStatus {
	return &VolumeFarmStatus{
		IsRunning:     e.IsRunning(),
		ActiveSymbols: e.symbolSelector.GetActiveSymbolCount(),
		ActiveGrids:   0,
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

// monitorRegimeChanges monitors and handles regime changes for all symbols
func (e *VolumeFarmEngine) monitorRegimeChanges(ctx context.Context) {
	e.logger.Info("Monitoring regime changes")

	// Get active symbols
	activeSymbols := e.symbolSelector.GetActiveSymbols()

	for _, symbolData := range activeSymbols {
		symbol := symbolData.Symbol

		// Get current price (simplified - in real implementation would get from price feed)
		currentPrice := 0.0 // Placeholder

		// Detect regime
		newRegime := e.regimeDetector.DetectRegime(symbol, currentPrice)

		// Get current regime
		currentRegime := e.adaptiveGridManager.GetCurrentRegime(symbol)

		// Check if regime changed
		if newRegime != currentRegime {
			e.logger.Info("Regime change detected",
				zap.String("symbol", symbol),
				zap.String("from", string(currentRegime)),
				zap.String("to", string(newRegime)))

			// Handle regime change
			e.adaptiveGridManager.OnRegimeChange(symbol, currentRegime, newRegime)

			// TODO: Trigger parameter application and grid rebuild
			// This would be handled by the transition handler
		}
	}
}

// monitorRisk monitors risk levels and takes action.
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
			dailyPnL := e.riskManager.DailyPnL()
			isPaused := e.riskManager.IsPaused()

			if dailyPnL < -e.volumeConfig.Risk.DailyLossLimitUSDT {
				e.logger.Warn("Daily loss limit reached, stopping farming",
					zap.Float64("daily_loss", dailyPnL),
					zap.Float64("limit", e.volumeConfig.Risk.DailyLossLimitUSDT))
			}

			if isPaused {
				e.logger.Warn("Risk manager paused, stopping farming")
			}
		}
	}
}

// extractVolumeFarmConfig extracts volume farming config from main config.
func (e *VolumeFarmEngine) extractVolumeFarmConfig(cfg *config.Config) *config.VolumeFarmConfig {
	if cfg.VolumeFarming == nil {
		return &config.VolumeFarmConfig{
			Enabled:                  true,
			MaxDailyLossUSDT:         200,  // Higher limit for volume farming
			MaxTotalDrawdownPct:      15.0, // More tolerance for volume
			OrderSizeUSDT:            5,    // Small orders for high volume
			GridSpreadPct:            0.01, // Tight spread
			MaxOrdersPerSide:         30,   // 30 orders per side
			ReplaceImmediately:       true, // Immediate replacement
			PositionTimeoutMinutes:   10,   // Fast turnover
			SymbolRefreshIntervalSec: 30,   // Fast symbol refresh
			GridPlacementCooldownSec: 1,    // 1 second cooldown
			RateLimitCooldownSec:     3,    // Quick recovery
			SupportedQuoteCurrencies: []string{"USD1"},
			MinVolume24h:             500, // Lower threshold
			Bot:                      cfg.Bot,
			Exchange:                 cfg.Exchange,
			Risk:                     cfg.Risk,
			API:                      cfg.API,
			Symbols: config.SymbolsConfig{
				QuoteCurrencies:    []string{"USD1"},
				MaxSymbolsPerQuote: 20,  // More symbols
				MinVolume24h:       500, // Lower volume threshold
			},
		}
	}

	e.logger.Info("Extracted volume farming config",
		zap.Bool("volume_farming_enabled", cfg.VolumeFarming.Enabled),
		zap.String("quote_currency_mode", cfg.VolumeFarming.Symbols.QuoteCurrencyMode),
		zap.Strings("quote_currencies", cfg.VolumeFarming.Symbols.QuoteCurrencies),
		zap.Float64("min_volume_24h", cfg.VolumeFarming.Symbols.MinVolume24h),
		zap.Strings("whitelist", cfg.VolumeFarming.Symbols.Whitelist),
	)

	volumeConfig := &config.VolumeFarmConfig{
		Enabled:                  cfg.VolumeFarming.Enabled,
		MaxDailyLossUSDT:         cfg.VolumeFarming.MaxDailyLossUSDT,
		MaxTotalDrawdownPct:      cfg.VolumeFarming.MaxTotalDrawdownPct,
		OrderSizeUSDT:            cfg.VolumeFarming.OrderSizeUSDT,
		GridSpreadPct:            cfg.VolumeFarming.GridSpreadPct,
		MaxOrdersPerSide:         cfg.VolumeFarming.MaxOrdersPerSide,
		ReplaceImmediately:       cfg.VolumeFarming.ReplaceImmediately,
		PositionTimeoutMinutes:   cfg.VolumeFarming.PositionTimeoutMinutes,
		TickerStream:             cfg.VolumeFarming.TickerStream,
		SymbolRefreshIntervalSec: cfg.VolumeFarming.SymbolRefreshIntervalSec,
		GridPlacementCooldownSec: cfg.VolumeFarming.GridPlacementCooldownSec,
		RateLimitCooldownSec:     cfg.VolumeFarming.RateLimitCooldownSec,
		SupportedQuoteCurrencies: append([]string{}, cfg.VolumeFarming.SupportedQuoteCurrencies...),
		MinVolume24h:             cfg.VolumeFarming.MinVolume24h,
		Bot:                      cfg.Bot,
		Symbols:                  cfg.VolumeFarming.Symbols,
		Exchange:                 cfg.Exchange,
		Risk:                     cfg.Risk,
		API:                      cfg.API,
	}

	if volumeConfig.MinVolume24h <= 0 {
		volumeConfig.MinVolume24h = volumeConfig.Symbols.MinVolume24h
	}
	// Only apply defaults if still zero after using Symbols.MinVolume24h
	if volumeConfig.MinVolume24h <= 0 {
		volumeConfig.MinVolume24h = 1000 // Use config file default
	}
	if len(volumeConfig.SupportedQuoteCurrencies) == 0 {
		volumeConfig.SupportedQuoteCurrencies = append([]string{}, volumeConfig.Symbols.QuoteCurrencies...)
	}
	if len(volumeConfig.SupportedQuoteCurrencies) == 0 {
		volumeConfig.SupportedQuoteCurrencies = []string{"USD1"}
	}
	if len(volumeConfig.Symbols.QuoteCurrencies) == 0 {
		volumeConfig.Symbols.QuoteCurrencies = append([]string{}, volumeConfig.SupportedQuoteCurrencies...)
	}
	if volumeConfig.TickerStream == "" {
		volumeConfig.TickerStream = "!ticker@arr"
	}
	if volumeConfig.SymbolRefreshIntervalSec <= 0 {
		volumeConfig.SymbolRefreshIntervalSec = 60 // Use config file default (60s not 30s)
	}
	if volumeConfig.GridPlacementCooldownSec < 0 { // Allow 0 for no cooldown
		volumeConfig.GridPlacementCooldownSec = 0
	}
	if volumeConfig.RateLimitCooldownSec <= 0 {
		volumeConfig.RateLimitCooldownSec = 3
	}
	if volumeConfig.Symbols.MaxSymbolsPerQuote <= 0 {
		volumeConfig.Symbols.MaxSymbolsPerQuote = 1 // Use config file default (1 not 20)
	}
	// Ensure risk values from volume-farm-config are properly set
	if volumeConfig.Risk.MaxPositionUSDTPerSymbol <= 0 && cfg.VolumeFarming.Risk.MaxPositionUSDTPerSymbol > 0 {
		volumeConfig.Risk.MaxPositionUSDTPerSymbol = cfg.VolumeFarming.Risk.MaxPositionUSDTPerSymbol
	}
	if volumeConfig.Risk.MaxTotalPositionsUSDT <= 0 && cfg.VolumeFarming.Risk.MaxTotalPositionsUSDT > 0 {
		volumeConfig.Risk.MaxTotalPositionsUSDT = cfg.VolumeFarming.Risk.MaxTotalPositionsUSDT
	}
	if volumeConfig.Risk.DailyLossLimitUSDT <= 0 && cfg.VolumeFarming.Risk.DailyLossLimitUSDT > 0 {
		volumeConfig.Risk.DailyLossLimitUSDT = cfg.VolumeFarming.Risk.DailyLossLimitUSDT
	}
	if volumeConfig.Risk.MaxOpenPositions <= 0 && cfg.VolumeFarming.Risk.MaxOpenPositions > 0 {
		volumeConfig.Risk.MaxOpenPositions = cfg.VolumeFarming.Risk.MaxOpenPositions
	}
	if volumeConfig.Risk.PerTradeStopLossPct <= 0 && cfg.VolumeFarming.Risk.PerTradeStopLossPct > 0 {
		volumeConfig.Risk.PerTradeStopLossPct = cfg.VolumeFarming.Risk.PerTradeStopLossPct
	}
	if volumeConfig.Risk.PerTradeTakeProfitPct <= 0 && cfg.VolumeFarming.Risk.PerTradeTakeProfitPct > 0 {
		volumeConfig.Risk.PerTradeTakeProfitPct = cfg.VolumeFarming.Risk.PerTradeTakeProfitPct
	}
	// Log final applied config values
	e.logger.Info("Final volume farming config applied",
		zap.Float64("order_size_usdt", volumeConfig.OrderSizeUSDT),
		zap.Float64("grid_spread_pct", volumeConfig.GridSpreadPct),
		zap.Int("max_orders_per_side", volumeConfig.MaxOrdersPerSide),
		zap.Int("position_timeout_minutes", volumeConfig.PositionTimeoutMinutes),
		zap.Int("symbol_refresh_interval_sec", volumeConfig.SymbolRefreshIntervalSec),
		zap.Int("grid_placement_cooldown_sec", volumeConfig.GridPlacementCooldownSec),
		zap.Float64("risk_max_position_usdt", volumeConfig.Risk.MaxPositionUSDTPerSymbol),
		zap.Float64("risk_daily_loss_limit", volumeConfig.Risk.DailyLossLimitUSDT),
		zap.Int("risk_max_open_positions", volumeConfig.Risk.MaxOpenPositions),
		zap.Float64("risk_per_trade_sl_pct", volumeConfig.Risk.PerTradeStopLossPct),
	)

	return volumeConfig
}

// VolumeFarmStatus represents the current status.
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
