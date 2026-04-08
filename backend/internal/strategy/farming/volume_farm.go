package strategy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming"
	"aster-bot/internal/ordermanager"
	"aster-bot/internal/risk"

	"github.com/sirupsen/logrus"
)

// VolumeFarmStrategy implements volume farming strategy
type VolumeFarmStrategy struct {
	name             string
	cfg              *config.StrategyConfig
	futuresClient    *client.FuturesClient
	marketClient     *client.MarketClient
	riskManager      *risk.Manager
	orderManager     *ordermanager.Manager
	precisionManager *client.PrecisionManager
	logger           *logrus.Entry

	// Volume farming specific components
	symbolSelector *farming.SymbolSelector
	gridManager    *farming.GridManager
	pointsTracker  *farming.PointsTracker
	volumeFarmCfg  *config.VolumeFarmConfig

	// State management
	isRunning   bool
	isRunningMu sync.RWMutex
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// NewVolumeFarmStrategy creates a new volume farming strategy
func NewVolumeFarmStrategy(
	cfg *config.StrategyConfig,
	futuresClient *client.FuturesClient,
	marketClient *client.MarketClient,
	riskManager *risk.Manager,
	orderManager *ordermanager.Manager,
	precisionManager *client.PrecisionManager,
	log *logrus.Entry,
) *VolumeFarmStrategy {
	strategy := &VolumeFarmStrategy{
		name:             cfg.Name,
		cfg:              cfg,
		futuresClient:    futuresClient,
		marketClient:     marketClient,
		riskManager:      riskManager,
		orderManager:     orderManager,
		precisionManager: precisionManager,
		logger:           log.WithField("strategy", cfg.Name),
		stopCh:           make(chan struct{}),
	}

	// Initialize volume farming components
	strategy.volumeFarmCfg = buildVolumeFarmConfig(cfg)
	strategy.symbolSelector = farming.NewSymbolSelector(futuresClient, strategy.logger, strategy.volumeFarmCfg)
	strategy.gridManager = farming.NewGridManager(futuresClient, strategy.logger, strategy.volumeFarmCfg)
	strategy.pointsTracker = farming.NewPointsTracker(strategy.volumeFarmCfg, strategy.logger)

	return strategy
}

// Start starts the volume farming strategy
func (s *VolumeFarmStrategy) Start(ctx context.Context) error {
	s.isRunningMu.Lock()
	if s.isRunning {
		s.isRunningMu.Unlock()
		return fmt.Errorf("volume farming strategy is already running")
	}
	s.isRunning = true
	s.isRunningMu.Unlock()

	s.logger.Info("🚀 Starting Volume Farming Strategy")

	// Start symbol selector
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.symbolSelector.Start(ctx); err != nil {
			s.logger.WithError(err).Error("Symbol selector error")
		}
	}()

	// Start grid manager
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.gridManager.Start(ctx); err != nil {
			s.logger.WithError(err).Error("Grid manager error")
		}
	}()

	// Start points tracker
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.pointsTracker.Start(ctx); err != nil {
			s.logger.WithError(err).Error("Points tracker error")
		}
	}()

	// Start main farming loop
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.farmingLoop(ctx)
	}()

	s.logger.Info("✅ Volume Farming Strategy started successfully")
	return nil
}

// Stop stops the volume farming strategy
func (s *VolumeFarmStrategy) Stop(ctx context.Context) error {
	s.isRunningMu.Lock()
	if !s.isRunning {
		s.isRunningMu.Unlock()
		return nil
	}
	s.isRunning = false
	s.isRunningMu.Unlock()

	s.logger.Info("🛑 Stopping Volume Farming Strategy")

	// Signal all goroutines to stop
	close(s.stopCh)

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("✅ Volume Farming Strategy stopped gracefully")
		return nil
	case <-ctx.Done():
		s.logger.Warn("⚠️  Volume Farming Strategy stop timeout")
		return ctx.Err()
	}
}

// OnKline handles kline data
// TODO: Define engine.Kline type
// func (s *VolumeFarmStrategy) OnKline(kline *engine.Kline) {
// 	// Volume farming strategy doesn't need kline data
// }

// OnMarkPrice handles mark price updates
// TODO: Define engine.MarkPrice type and gridManager.OnMarkPrice method
// func (s *VolumeFarmStrategy) OnMarkPrice(markPrice *engine.MarkPrice) {
// 	if !s.IsRunning() {
// 		return
// 	}
//
// 	// Forward mark price to grid manager
// 	s.gridManager.OnMarkPrice(markPrice)
// }

// OnOrderUpdate handles order updates
// TODO: Define engine.OrderUpdate type and related methods
// func (s *VolumeFarmStrategy) OnOrderUpdate(order *engine.OrderUpdate) {
// 	if !s.IsRunning() {
// 		return
// 	}
//
// 	// Forward order update to grid manager
// 	s.gridManager.OnOrderUpdate(order)
//
// 	// Track points for filled orders
// 	if order.Status == "FILLED" {
// 		s.pointsTracker.OnOrderFilled(order)
// 	}
// }

// OnAccountUpdate handles account updates
// TODO: Define engine.AccountUpdate type and riskManager.OnAccountUpdate method
// func (s *VolumeFarmStrategy) OnAccountUpdate(account *engine.AccountUpdate) {
// 	if !s.IsRunning() {
// 		return
// 	}
//
// 	// Forward account update to risk manager
// 	s.riskManager.OnAccountUpdate(account)
// }

// IsRunning returns whether the strategy is running
func (s *VolumeFarmStrategy) IsRunning() bool {
	s.isRunningMu.RLock()
	defer s.isRunningMu.RUnlock()
	return s.isRunning
}

// GetName returns the strategy name
func (s *VolumeFarmStrategy) GetName() string {
	return s.name
}

// GetStatus returns the current status
func (s *VolumeFarmStrategy) GetStatus() *VolumeFarmStatus {
	return &VolumeFarmStatus{
		IsRunning:     s.IsRunning(),
		ActiveSymbols: s.symbolSelector.GetActiveSymbolCount(),
		// TODO: Implement gridManager.GetActiveGridCount
		// ActiveGrids:   s.gridManager.GetActiveGridCount(),
		ActiveGrids:   0,
		CurrentPoints: s.pointsTracker.GetCurrentPoints(),
		CurrentVolume: s.pointsTracker.GetCurrentVolume(),
		LastUpdate:    time.Now(),
	}
}

// farmingLoop is the main farming loop
func (s *VolumeFarmStrategy) farmingLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			// TODO: Implement riskManager.ShouldStop and gridManager.EmergencyStop
			// Check risk limits
			// if s.riskManager.ShouldStop() {
			// 	s.logger.Warn("Risk limits reached, stopping grid manager")
			// 	s.gridManager.EmergencyStop()
			// }

			// TODO: Implement symbolSelector.GetSymbolUpdates and gridManager.UpdateSymbol
			// Update symbol selection
			// symbolUpdates := s.symbolSelector.GetSymbolUpdates()
			// for _, update := range symbolUpdates {
			// 	s.gridManager.UpdateSymbol(update)
			// }
		}
	}
}

// buildVolumeFarmConfig extracts volume farming config from strategy config
func buildVolumeFarmConfig(cfg *config.StrategyConfig) *config.VolumeFarmConfig {
	// Build volume farming config from strategy parameters
	vc := &config.VolumeFarmConfig{}

	if v, ok := cfg.Params["enabled"].(bool); ok {
		vc.Enabled = v
	}
	if v, ok := cfg.Params["max_daily_loss_usdt"].(float64); ok {
		vc.MaxDailyLossUSDT = v
	}
	if v, ok := cfg.Params["order_size_usdt"].(float64); ok {
		vc.OrderSizeUSDT = v
	}
	if v, ok := cfg.Params["grid_spread_pct"].(float64); ok {
		vc.GridSpreadPct = v
	}
	if v, ok := cfg.Params["max_orders_per_side"].(float64); ok {
		vc.MaxOrdersPerSide = int(v)
	}
	if v, ok := cfg.Params["replace_immediately"].(bool); ok {
		vc.ReplaceImmediately = v
	}
	if v, ok := cfg.Params["position_timeout_minutes"].(float64); ok {
		vc.PositionTimeoutMinutes = int(v)
	}

	// Build symbols config
	sc := config.SymbolsConfig{}
	if v, ok := cfg.Params["symbols.auto_discover"].(bool); ok {
		sc.AutoDiscover = v
	}
	if v, ok := cfg.Params["symbols.quote_currency_mode"].(string); ok {
		sc.QuoteCurrencyMode = v
	}
	if v, ok := cfg.Params["symbols.min_volume_24h"].(float64); ok {
		sc.MinVolume24h = v
	}
	if v, ok := cfg.Params["symbols.max_spread_pct"].(float64); ok {
		sc.MaxSpreadPct = v
	}
	if v, ok := cfg.Params["symbols.boosted_only"].(bool); ok {
		sc.BoostedOnly = v
	}
	if v, ok := cfg.Params["symbols.max_symbols_per_quote"].(float64); ok {
		sc.MaxSymbolsPerQuote = int(v)
	}
	if v, ok := cfg.Params["symbols.spread_ranking"].(bool); ok {
		sc.SpreadRanking = v
	}
	if v, ok := cfg.Params["symbols.volume_weighting"].(float64); ok {
		sc.VolumeWeighting = v
	}
	if v, ok := cfg.Params["symbols.min_liquidity_score"].(float64); ok {
		sc.MinLiquidityScore = v
	}
	if v, ok := cfg.Params["symbols.spread_volatility_threshold"].(float64); ok {
		sc.SpreadVolatilityThreshold = v
	}
	if v, ok := cfg.Params["symbols.exclude_high_fee_symbols"].(bool); ok {
		sc.ExcludeHighFeeSymbols = v
	}
	if v, ok := cfg.Params["symbols.quote_currencies"].([]string); ok {
		sc.QuoteCurrencies = v
	}
	if v, ok := cfg.Params["symbols.allow_mixed_quotes"].(bool); ok {
		sc.AllowMixedQuotes = v
	}

	vc.Symbols = sc

	return vc
}

// VolumeFarmStatus represents the current status
type VolumeFarmStatus struct {
	IsRunning     bool      `json:"is_running"`
	ActiveSymbols int       `json:"active_symbols"`
	ActiveGrids   int       `json:"active_grids"`
	CurrentPoints int64     `json:"current_points"`
	CurrentVolume float64   `json:"current_volume"`
	LastUpdate    time.Time `json:"last_update"`
}
