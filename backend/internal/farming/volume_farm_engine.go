package farming

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"aster-bot/internal/agentic"
	"aster-bot/internal/auth"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_config"
	"aster-bot/internal/farming/adaptive_grid"
	"aster-bot/internal/farming/market_regime"
	farmsync "aster-bot/internal/farming/sync"
	"aster-bot/internal/farming/tradingmode"
	"aster-bot/internal/risk"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

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
	userStream          *stream.UserStream

	// NEW: Continuous Volume Farming components
	modeManager  *tradingmode.ModeManager
	exitExecutor *ExitExecutor
	syncManager  *farmsync.SyncManager
	wsClient     *client.WebSocketClient

	stopCh chan struct{}
	wg     sync.WaitGroup
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

	// Create shared WebSocket client for both SymbolSelector and GridManager
	zapLogger, _ := zap.NewDevelopment()
	wsURL := buildTickerStreamURL(volumeConfig.Exchange.FuturesWSBase, volumeConfig.TickerStream)
	if wsURL == "" {
		wsURL = "wss://fstream.asterdex.com/ws/!ticker@arr"
	}
	sharedWSClient := client.NewWebSocketClient(wsURL, zapLogger)

	engine.symbolSelector = NewSymbolSelector(engine.futuresClient, logrusEntry.WithField("component", "symbol_selector"), volumeConfig)
	engine.symbolSelector.SetWebSocketClient(sharedWSClient)

	gridLogger := logrusEntry.WithField("component", "grid_manager")
	engine.gridManager = NewGridManager(engine.futuresClient, gridLogger, volumeConfig)
	engine.gridManager.SetWebSocketClient(sharedWSClient)
	engine.gridManager.ApplyConfig(volumeConfig)

	pointsLogger := logrusEntry.WithField("component", "points_tracker")
	engine.pointsTracker = NewPointsTracker(volumeConfig, pointsLogger)

	// Initialize adaptive configuration
	configManager := adaptive_config.NewAdaptiveConfigManager("config/adaptive_config.yaml", logger)
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
		engine.gridManager, // GridManager implements PositionProvider interface
		logger,
	)
	engine.adaptiveGridManager = adaptiveGridManager

	stateMachine := adaptive_grid.NewGridStateMachine(logger)
	engine.gridManager.SetStateMachine(stateMachine)
	engine.adaptiveGridManager.SetStateMachine(stateMachine)

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

	// NEW: Initialize Continuous Volume Farming components
	// ModeManager - evaluates and switches trading modes based on market conditions
	modeConfig := volumeConfig.TradingModes
	if modeConfig == nil {
		modeConfig = &config.TradingModesConfig{
			MicroMode:        config.MicroModeConfig{Enabled: true},
			StandardMode:     config.StandardModeConfig{Enabled: true},
			TrendAdaptedMode: config.TrendAdaptedModeConfig{Enabled: true},
		}
	}
	engine.modeManager = tradingmode.NewModeManager(modeConfig, logger)
	logger.Info("ModeManager initialized",
		zap.Bool("micro_enabled", modeConfig.MicroMode.Enabled),
		zap.Bool("standard_enabled", modeConfig.StandardMode.Enabled),
		zap.Bool("trend_enabled", modeConfig.TrendAdaptedMode.Enabled))

	// ExitExecutor - handles fast exit on breakouts
	engine.exitExecutor = NewExitExecutor(engine.futuresClient, sharedWSClient, 5*time.Second, logger)
	logger.Info("ExitExecutor initialized", zap.Duration("timeout", 5*time.Second))

	// NEW: Wire ExitExecutor into AdaptiveGridManager for breakout handling
	adaptiveGridManager.SetExitExecutor(engine.exitExecutor)
	logger.Info("ExitExecutor wired into AdaptiveGridManager")

	// NEW: Wire ModeManager into AdaptiveGridManager for trading mode evaluation
	adaptiveGridManager.SetModeManager(engine.modeManager)
	logger.Info("ModeManager wired into AdaptiveGridManager")

	// SyncManager - coordinates order/position/balance sync workers
	engine.syncManager = farmsync.NewSyncManager(sharedWSClient, logger)
	engine.syncManager.Initialize()
	logger.Info("SyncManager initialized with all workers")

	// Store WebSocket client reference
	engine.wsClient = sharedWSClient

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

	// CRITICAL: Initial symbol sync MUST complete before starting grid manager
	// to ensure activeGrids is populated before rebalancer worker starts
	e.logger.Info("Initial symbol sync before starting grid manager...")
	e.syncGridSymbols()
	e.logger.Info("Initial symbol sync complete - starting grid manager")

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

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

		// CRITICAL: Load optimization config BEFORE Initialize
		// Priority 1: Use unified config if available (from agentic-vf-config.yaml)
		// Priority 2: Load from separate files in config/ directory
		var optConfig *config.OptimizationConfig

		if e.config.Optimization != nil {
			// Use optimization config from unified config file
			optConfig = e.config.Optimization
			e.logger.Info("Using optimization config from unified config file",
				zap.Bool("micro_grid_enabled", optConfig.MicroGrid != nil && optConfig.MicroGrid.Enabled),
				zap.Bool("fast_range_enabled", optConfig.FastRange != nil && optConfig.FastRange.Enabled),
				zap.Bool("adx_filter_enabled", optConfig.ADXFilter != nil && optConfig.ADXFilter.Enabled),
				zap.Bool("dynamic_leverage_enabled", optConfig.DynamicLeverage != nil && optConfig.DynamicLeverage.Enabled),
				zap.Bool("multi_layer_liq_enabled", optConfig.MultiLayerLiq != nil && optConfig.MultiLayerLiq.Enabled))
		} else {
			// Fall back to loading from separate files
			configDir := "config" // Default config directory
			var err error
			optConfig, err = config.LoadOptimizationConfig(configDir)
			if err != nil {
				e.logger.Warn("Failed to load optimization config from files, using defaults",
					zap.Error(err),
					zap.String("config_dir", configDir))
			} else {
				e.logger.Info("Optimization config loaded from separate files",
					zap.Bool("time_filter_enabled", optConfig.TimeFilter != nil && optConfig.TimeFilter.Enabled),
					zap.Int("time_slots", len(optConfig.TimeFilter.Slots)))
			}
		}

		// Pass config to adaptive grid manager BEFORE Initialize
		if optConfig != nil {
			e.adaptiveGridManager.SetOptimizationConfig(optConfig)
		}

		e.adaptiveGridManager.InitializeDynamicLeverage(nil)

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

	// NEW: Initialize and start UserStream for real-time order updates
	e.initUserStream(ctx)

	// NEW: Start SyncManager for order/position/balance sync
	if e.syncManager != nil {
		e.syncManager.Start()
		e.logger.Info("SyncManager started")
	}

	e.logger.Info("Volume Farming Engine started successfully")
	return nil
}

// initUserStream initializes and starts the user data stream WebSocket
func (e *VolumeFarmEngine) initUserStream(ctx context.Context) {
	wsBase := e.config.Exchange.FuturesWSBase
	if wsBase == "" {
		wsBase = "wss://fstream.asterdex.com"
	}

	userStream := stream.NewUserStream(
		wsBase,
		e.futuresClient.StartListenKey,
		e.futuresClient.KeepaliveListenKey,
		stream.UserStreamHandlers{
			OnOrderUpdate: func(u stream.WsOrderUpdate) {
				// Convert WsOrderUpdate to local OrderUpdate
				orderUpdate := &OrderUpdate{
					Symbol:      u.Order.Symbol,
					OrderID:     strconv.FormatInt(u.Order.OrderID, 10),
					ClientID:    u.Order.ClientOrderID,
					Side:        u.Order.Side,
					Type:        u.Order.OrderType,
					Status:      u.Order.OrderStatus,
					Price:       u.Order.AvgPrice,
					Quantity:    u.Order.FilledQty,
					ExecutedQty: u.Order.CumFilledQty,
					Timestamp:   u.EventTime,
				}
				// Only process FILLED orders
				if orderUpdate.Status == "FILLED" {
					e.gridManager.OnOrderUpdate(orderUpdate)
				}
			},
			OnAccountUpdate: func(u stream.WsAccountUpdate) {
				// Update WebSocket cache with positions and balances
				e.logger.Debug("Account update received via WebSocket",
					zap.Int("positions", len(u.Update.Positions)),
					zap.Int("balances", len(u.Update.Balances)))

				// Update position cache
				for _, pos := range u.Update.Positions {
					position := client.Position{
						Symbol:           pos.Symbol,
						PositionAmt:      pos.PositionAmt,
						EntryPrice:       pos.EntryPrice,
						MarkPrice:        pos.EntryPrice, // Use EntryPrice as fallback
						UnrealizedProfit: pos.UnrealizedPnL,
						PositionSide:     pos.PositionSide,
					}
					e.wsClient.UpdatePositionCache(position)
				}

				// Update balance cache
				for _, bal := range u.Update.Balances {
					if bal.Asset == "USDT" {
						balance := client.Balance{
							Asset:            bal.Asset,
							AvailableBalance: bal.CrossWalletBalance, // Use CrossWalletBalance as available
							WalletBalance:    bal.WalletBalance,
							MarginBalance:    bal.WalletBalance, // Use WalletBalance as margin
						}
						e.wsClient.UpdateBalanceCache(balance)
					}
				}
			},
		},
		e.logger,
	)
	e.userStream = userStream

	// Start user stream in background
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.logger.Info("Starting UserStream for real-time order updates")
		userStream.Run(ctx)
		e.logger.Info("UserStream stopped")
	}()
}
func (e *VolumeFarmEngine) syncGridSymbols() {
	e.logger.Info("Syncing grid symbols")

	activeSymbols := e.symbolSelector.GetActiveSymbols()

	// Get whitelist from symbol selector (updated by Agentic layer)
	whitelist := e.symbolSelector.GetWhitelist()

	// FALLBACK: If agentic whitelist is empty, use config whitelist
	if len(whitelist) == 0 && len(e.volumeConfig.Symbols.Whitelist) > 0 {
		whitelist = e.volumeConfig.Symbols.Whitelist
		e.logger.Info("Using config whitelist (agentic whitelist empty)", zap.Strings("whitelist", whitelist))
	}

	e.logger.Info("Active symbols from selector", zap.Int("count", len(activeSymbols)), zap.Strings("symbols", func() []string {
		syms := make([]string, len(activeSymbols))
		for i, s := range activeSymbols {
			syms[i] = s.Symbol
		}
		return syms
	}()), zap.Strings("whitelist_from_selector", whitelist))

	// Always include whitelist symbols
	if len(whitelist) > 0 {
		whitelistSymbols := make(map[string]bool)
		for _, sym := range whitelist {
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

	if len(activeSymbols) == 0 && len(whitelist) > 0 {
		e.logger.Info("No symbols from selector, adding whitelist symbols only")
		// Only add whitelist symbols with USD1 quote - NO USDT
		whitelistOnly := e.createWhitelistSymbolsFromList(whitelist)
		activeSymbols = append(activeSymbols, whitelistOnly...)
	}

	// NEW: Set correct leverage for each symbol from exchange info
	e.setLeverageForSymbols(activeSymbols)

	e.logger.Info("Updating grid manager with symbols", zap.Int("count", len(activeSymbols)), zap.Strings("symbols", func() []string {
		syms := make([]string, len(activeSymbols))
		for i, s := range activeSymbols {
			syms[i] = s.Symbol
		}
		return syms
	}()))
	e.gridManager.UpdateSymbols(activeSymbols)
}

// setLeverageForSymbols sets the correct leverage for each symbol based on exchange info
func (e *VolumeFarmEngine) setLeverageForSymbols(symbols []*SymbolData) {
	// CRITICAL: Ensure exchange info is loaded before checking leverage
	// This may be called before grid manager Start() completes
	if e.gridManager.precisionMgr.GetMaxLeverage("BTCUSD1") == 0 {
		e.logger.Info("Exchange info not loaded yet, fetching directly for leverage setup")
		marketClient := client.NewMarketClient(e.futuresClient.GetHTTPClient())
		exchangeInfo, err := marketClient.ExchangeInfo(context.Background())
		if err != nil {
			e.logger.Warn("Failed to fetch exchange info for leverage setup", zap.Error(err))
		} else {
			exchangeInfoBytes := []byte(exchangeInfo)
			e.gridManager.precisionMgr.UpdateFromExchangeInfo(exchangeInfoBytes)
			e.logger.Info("Exchange info loaded for leverage setup", zap.Int("bytes", len(exchangeInfoBytes)))
		}
	}

	for _, sym := range symbols {
		maxLeverage := e.gridManager.precisionMgr.GetMaxLeverage(sym.Symbol)
		if maxLeverage > 0 {
			e.logger.Info("Setting leverage for symbol",
				zap.String("symbol", sym.Symbol),
				zap.Float64("max_leverage", maxLeverage))

			// CRITICAL: Use dynamic leverage based on BB/ADX market conditions
			// Falls back to 80% of max if dynamic calculator not available
			calculatedLeverage := maxLeverage * 0.8
			if e.adaptiveGridManager != nil {
				calculatedLeverage = e.adaptiveGridManager.GetOptimalLeverage()
				e.logger.Info("Dynamic leverage calculated",
					zap.String("symbol", sym.Symbol),
					zap.Float64("calculated_leverage", calculatedLeverage),
					zap.Float64("max_leverage", maxLeverage))
			}

			// Clamp to max leverage and ensure minimum of 1
			targetLeverage := int(calculatedLeverage)
			if targetLeverage > int(maxLeverage) {
				targetLeverage = int(maxLeverage)
			}
			if targetLeverage < 1 {
				targetLeverage = 1
			}

			if err := e.futuresClient.SetLeverage(context.Background(), client.SetLeverageRequest{
				Symbol:   sym.Symbol,
				Leverage: targetLeverage,
			}); err != nil {
				e.logger.Warn("Failed to set leverage",
					zap.String("symbol", sym.Symbol),
					zap.Error(err))
			} else {
				e.logger.Info("Leverage set successfully",
					zap.String("symbol", sym.Symbol),
					zap.Int("leverage", targetLeverage))
			}
		} else {
			e.logger.Warn("Max leverage unknown for symbol, skipping leverage set",
				zap.String("symbol", sym.Symbol))
		}
	}
}

func (e *VolumeFarmEngine) createWhitelistSymbols() []*SymbolData {
	// Use whitelist from symbol selector (updated by Agentic layer)
	whitelist := e.symbolSelector.GetWhitelist()
	return e.createWhitelistSymbolsFromList(whitelist)
}

func (e *VolumeFarmEngine) createWhitelistSymbolsFromList(whitelist []string) []*SymbolData {
	symbols := make([]*SymbolData, 0, len(whitelist))
	for _, symbol := range whitelist {
		// Extract quote currency
		quote := ""
		for _, qc := range e.volumeConfig.Symbols.QuoteCurrencies {
			if len(symbol) > len(qc) && symbol[len(symbol)-len(qc):] == qc {
				quote = qc
				break
			}
		}
		if quote == "" {
			e.logger.Warn("Cannot extract quote currency for whitelist symbol", zap.String("symbol", symbol))
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
		e.logger.Info("Created whitelist symbol", zap.String("symbol", symbol), zap.String("base", base), zap.String("quote", quote))
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

	// NEW: Stop SyncManager
	if e.syncManager != nil {
		e.syncManager.Stop()
		e.logger.Info("SyncManager stopped")
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
	_ = ctx
	e.logger.Debug("Regime monitoring delegated to AdaptiveGridManager range/ADX state machine")
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

// UpdateWhitelist updates the whitelist for symbol selection (called by Agentic layer)
func (e *VolumeFarmEngine) UpdateWhitelist(symbols []string) error {
	e.logger.Info("Updating whitelist from Agentic layer",
		zap.Strings("symbols", symbols))

	// Update the symbol selector whitelist
	e.symbolSelector.SetWhitelist(symbols)

	// Trigger a resync of grid symbols
	e.syncGridSymbols()

	return nil
}

// GetActivePositions returns the current active positions across all symbols
func (e *VolumeFarmEngine) GetActivePositions() ([]agentic.PositionStatus, error) {
	if e.adaptiveGridManager == nil {
		return nil, nil
	}

	tracked := e.adaptiveGridManager.GetAllPositions()
	positions := make([]agentic.PositionStatus, 0, len(tracked))
	for symbol, pos := range tracked {
		if pos == nil {
			continue
		}

		side := ""
		if pos.PositionAmt > 0 {
			side = "LONG"
		} else if pos.PositionAmt < 0 {
			side = "SHORT"
		}

		positions = append(positions, agentic.PositionStatus{
			Symbol:        symbol,
			HasPosition:   pos.PositionAmt != 0,
			Size:          math.Abs(pos.PositionAmt),
			UnrealizedPnL: pos.UnrealizedPnL,
			Side:          side,
		})
	}

	return positions, nil
}
