package strategy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/engine"
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
	symbolSelector *SymbolSelector
	gridManager    *GridManager
	pointsTracker  *PointsTracker

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
	strategy.symbolSelector = farming.NewSymbolSelector(futuresClient, strategy.logger)
	strategy.gridManager = farming.NewGridManager(futuresClient, strategy.logger)
	strategy.pointsTracker = farming.NewPointsTracker(extractVolumeFarmConfig(cfg), strategy.logger)

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
func (s *VolumeFarmStrategy) OnKline(kline *engine.Kline) {
	// Volume farming strategy doesn't need kline data
}

// OnMarkPrice handles mark price updates
func (s *VolumeFarmStrategy) OnMarkPrice(markPrice *engine.MarkPrice) {
	if !s.IsRunning() {
		return
	}

	// Forward mark price to grid manager
	s.gridManager.OnMarkPrice(markPrice)
}

// OnOrderUpdate handles order updates
func (s *VolumeFarmStrategy) OnOrderUpdate(order *engine.OrderUpdate) {
	if !s.IsRunning() {
		return
	}

	// Forward order update to grid manager
	s.gridManager.OnOrderUpdate(order)

	// Track points for filled orders
	if order.Status == "FILLED" {
		s.pointsTracker.OnOrderFilled(order)
	}
}

// OnAccountUpdate handles account updates
func (s *VolumeFarmStrategy) OnAccountUpdate(account *engine.AccountUpdate) {
	if !s.IsRunning() {
		return
	}

	// Forward account update to risk manager
	s.riskManager.OnAccountUpdate(account)
}

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
		ActiveGrids:   s.gridManager.GetActiveGridCount(),
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
			// Check risk limits
			if s.riskManager.ShouldStop() {
				s.logger.Warn("Risk limits reached, stopping grid manager")
				s.gridManager.EmergencyStop()
			}

			// Update symbol selection
			symbolUpdates := s.symbolSelector.GetSymbolUpdates()
			for _, update := range symbolUpdates {
				s.gridManager.UpdateSymbol(update)
			}
		}
	}
}

// extractVolumeFarmConfig extracts volume farming config from strategy config
func extractVolumeFarmConfig(cfg *config.StrategyConfig) *VolumeFarmConfig {
	// Extract volume farming specific config from strategy parameters
	volumeConfig := &VolumeFarmConfig{
		Enabled:                cfg.Params["enabled"].(bool),
		MaxDailyLossUSDT:       cfg.Params["max_daily_loss_usdt"].(float64),
		OrderSizeUSDT:          cfg.Params["order_size_usdt"].(float64),
		GridSpreadPct:          cfg.Params["grid_spread_pct"].(float64),
		MaxOrdersPerSide:       int(cfg.Params["max_orders_per_side"].(float64)),
		ReplaceImmediately:     cfg.Params["replace_immediately"].(bool),
		PositionTimeoutMinutes: int(cfg.Params["position_timeout_minutes"].(float64)),
	}

	// Extract symbols config
	symbolsConfig := SymbolsConfig{
		AutoDiscover:              cfg.Params["symbols.auto_discover"].(bool),
		QuoteCurrencyMode:         cfg.Params["symbols.quote_currency_mode"].(string),
		MinVolume24h:              cfg.Params["symbols.min_volume_24h"].(float64),
		MaxSpreadPct:              cfg.Params["symbols.max_spread_pct"].(float64),
		BoostedOnly:               cfg.Params["symbols.boosted_only"].(bool),
		MaxSymbolsPerQuote:        int(cfg.Params["symbols.max_symbols_per_quote"].(float64)),
		SpreadRanking:             cfg.Params["symbols.spread_ranking"].(bool),
		VolumeWeighting:           cfg.Params["symbols.volume_weighting"].(float64),
		MinLiquidityScore:         cfg.Params["symbols.min_liquidity_score"].(float64),
		SpreadVolatilityThreshold: cfg.Params["symbols.spread_volatility_threshold"].(float64),
		ExcludeHighFeeSymbols:     cfg.Params["symbols.exclude_high_fee_symbols"].(bool),
		QuoteCurrencies:           cfg.Params["symbols.quote_currencies"].([]string),
		AllowMixedQuotes:          cfg.Params["symbols.allow_mixed_quotes"].(bool),
	}

	volumeConfig.Symbols = symbolsConfig

	return volumeConfig
}

// VolumeFarmConfig represents volume farming configuration
type VolumeFarmConfig struct {
	Enabled                bool          `json:"enabled"`
	MaxDailyLossUSDT       float64       `json:"max_daily_loss_usdt"`
	OrderSizeUSDT          float64       `json:"order_size_usdt"`
	GridSpreadPct          float64       `json:"grid_spread_pct"`
	MaxOrdersPerSide       int           `json:"max_orders_per_side"`
	ReplaceImmediately     bool          `json:"replace_immediately"`
	PositionTimeoutMinutes int           `json:"position_timeout_minutes"`
	Symbols                SymbolsConfig `json:"symbols"`
}

// SymbolsConfig represents symbol selection configuration
type SymbolsConfig struct {
	AutoDiscover              bool     `json:"auto_discover"`
	QuoteCurrencyMode         string   `json:"quote_currency_mode"`
	MinVolume24h              float64  `json:"min_volume_24h"`
	MaxSpreadPct              float64  `json:"max_spread_pct"`
	BoostedOnly               bool     `json:"boosted_only"`
	MaxSymbolsPerQuote        int      `json:"max_symbols_per_quote"`
	SpreadRanking             bool     `json:"spread_ranking"`
	VolumeWeighting           float64  `json:"volume_weighting"`
	MinLiquidityScore         float64  `json:"min_liquidity_score"`
	SpreadVolatilityThreshold float64  `json:"spread_volatility_threshold"`
	ExcludeHighFeeSymbols     bool     `json:"exclude_high_fee_symbols"`
	QuoteCurrencies           []string `json:"quote_currencies"`
	AllowMixedQuotes          bool     `json:"allow_mixed_quotes"`
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
