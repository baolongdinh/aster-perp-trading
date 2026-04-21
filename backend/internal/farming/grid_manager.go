package farming

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"aster-bot/internal/activitylog"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_grid"
	"aster-bot/internal/farming/volume_optimization"
	"aster-bot/internal/stream"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// PricePoint represents a price data point for ATR calculation
type PricePoint struct {
	High  float64
	Low   float64
	Close float64
	Time  time.Time
}

// RiskChecker interface for checking if trading is allowed
type RiskChecker interface {
	CanPlaceOrder(symbol string) bool
}

// DynamicSizeCalculatorFunc function type for dynamic order size calculation
type DynamicSizeCalculatorFunc func(symbol string, baseSize float64, currentPrice float64) float64

// GridManager manages trading grids for multiple symbols.
type GridManager struct {
	futuresClient *client.FuturesClient
	logger        *logrus.Entry
	activityLog   *activitylog.ActivityLogger // Activity logging
	activeGrids   map[string]*SymbolGrid
	gridsMu       sync.RWMutex
	isRunning     bool
	isRunningMu   sync.RWMutex
	stopCh        chan struct{}
	wg            sync.WaitGroup

	// WebSocket client for real-time data.
	wsClient *client.WebSocketClient

	// Warm-up tracking for RangeDetector via WebSocket klines
	warmupActive map[string]bool // Symbol -> is in warmup phase
	warmupMu     sync.RWMutex

	// Order tracking.
	pendingOrders map[string]*GridOrder
	ordersMu      sync.RWMutex

	// Active orders tracking for fill detection
	activeOrders map[string]*GridOrder // OrderID -> GridOrder
	filledOrders map[string]*GridOrder // OrderID -> GridOrder

	// Configuration - Now using Notional Value based sizing
	baseNotionalUSD float64 // Base order size in USD (e.g., $100)
	minNotionalUSD  float64 // Minimum order in USD (e.g., $20)

	// PnL Risk Control configuration
	pnlRiskConfig *config.PnLRiskControlConfig

	// Market Condition Evaluator for adaptive state selection
	marketConditionEvaluator *adaptive_grid.MarketConditionEvaluator

	// Adaptive state configurations
	overSizeConfig  *config.OverSizeConfig
	defensiveConfig *config.DefensiveStateConfig
	recoveryConfig  *config.RecoveryStateConfig

	// Track which symbols have been warned about missing evaluator
	evaluatorNotLogged map[string]bool

	// Metrics streamer for real-time dashboard
	metricsStreamer interface{}

	maxNotionalUSD      float64 // Maximum order per symbol in USD (e.g., $300)
	maxTotalNotionalUSD float64 // Maximum total exposure across all symbols in USD (e.g., $500)
	gridSpreadPct       float64
	maxOrdersSide       int
	useDynamicSizing    bool // Use ATR-based dynamic sizing

	placementQueue        chan string
	gridPlacementCooldown time.Duration
	rateLimitCooldown     time.Duration
	tickerStreamURL       string
	rateLimitUntil        time.Time
	rateLimitMu           sync.RWMutex

	// New: Rate limiter for adaptive throttling.
	rateLimiter *RateLimiter

	// Precision manager for symbol-specific formatting.
	precisionMgr *client.PrecisionManager

	// TickSize manager for price rounding to valid ticks
	tickSizeMgr interface{}

	// PostOnly handler for maker order optimization
	postOnlyHandler interface{}

	// SmartCancellation manager for spread-based order management
	smartCancelMgr interface{}

	// PennyJump manager for order priority optimization
	pennyJumpMgr interface{}

	// InventoryHedge manager for skew hedging
	inventoryHedgeMgr interface{}

	// Dynamic cooldown tracking
	consecutiveFailures int
	lastFailureTime     time.Time

	// Volume farming metrics
	totalVolumeUSDT   float64
	totalOrdersPlaced int
	totalOrdersFilled int
	volumeMetricsMu   sync.RWMutex

	// Risk checker callback
	riskChecker   RiskChecker
	riskCheckerMu sync.RWMutex

	// Dynamic size calculator callback for enhanced sizing
	dynamicSizeCalculator   DynamicSizeCalculatorFunc
	dynamicSizeCalculatorMu sync.RWMutex

	// ATR tracking for dynamic sizing
	priceHistory   map[string][]PricePoint // symbol -> price history for ATR
	priceHistoryMu sync.RWMutex
	atrPeriod      int

	// NEW: Safeguard components
	orderLockMgr   *adaptive_grid.OrderLockManager
	deduplicator   *adaptive_grid.FillEventDeduplicator
	stateValidator *adaptive_grid.StateValidator

	// NEW: Reference to AdaptiveGridManager for optimization features
	adaptiveMgr *adaptive_grid.AdaptiveGridManager

	// NEW: Adaptive grid geometry for adaptive spread, order count, spacing
	gridGeometry *adaptive_grid.AdaptiveGridGeometry

	// NEW: Fluid flow parameters for "soft like water" behavior
	flowParameters map[string]adaptive_grid.FlowParameters // symbol -> flow params
	flowParamsMu   sync.RWMutex

	// NEW: Equity curve tracking for position sizing
	initialEquity      float64                     // Initial equity at bot start
	currentEquity      float64                     // Current equity
	equityHistory      []EquitySnapshot            // History of equity snapshots
	consecutiveWins    int                         // Consecutive winning trades
	consecutiveLosses  int                         // Consecutive losing trades
	equityMu           sync.RWMutex                // Mutex for equity tracking
	equitySizingConfig *config.DynamicSizingConfig // Equity sizing configuration

	// NEW: VPIN monitor for toxic flow detection
	vpinMonitor *volume_optimization.VPINMonitor

	// NEW: Take profit manager for micro profit feature
	takeProfitMgr *adaptive_grid.TakeProfitManager

	// NEW: Trading lifecycle state machine for strict state transitions
	stateMachine *adaptive_grid.GridStateMachine

	// Position rebalancer settings
	rebalanceThresholdPct   float64       // Threshold to start rebalancing (e.g., 0.8 = 80% of max)
	rebalanceAggressiveness float64       // How much to reduce (0.0-1.0, e.g., 0.3 = reduce by 30%)
	rebalanceInterval       time.Duration // How often to check position size

	// NOTE: Exchange order cache removed - using wsClient.GetCachedOrders() as single source of truth
	// exchangeOrderCache REMOVED
	// exchangeOrderCacheMu REMOVED
	// exchangeOrderCacheTTL REMOVED
	// ExchangeOrderCacheEntry struct REMOVED

	// NOTE: Position cache removed - using wsClient.GetCachedPositions() as single source of truth
	// cachedPositions REMOVED
	// cachedPositionsMu REMOVED
	// lastPositionUpdate REMOVED

	// NEW: Singleton kline processor - only one goroutine reads from kline channel
	warmupOnce   sync.Once
	warmupStopCh chan struct{}
	warmupWg     sync.WaitGroup

	// NEW: Callback when order is placed (for sync worker integration)
	onOrderPlaced func(symbol string, order client.Order)
}

// EquitySnapshot represents a snapshot of equity at a point in time
type EquitySnapshot struct {
	Timestamp   time.Time
	Equity      float64
	RealizedPnL float64
	WinRate     float64
}

// SetActivityLogger sets the activity logger for the grid manager.
func (g *GridManager) SetActivityLogger(al *activitylog.ActivityLogger) {
	g.activityLog = al
}

// SetOnOrderPlacedCallback sets callback when order is placed (for sync worker integration)
func (g *GridManager) SetOnOrderPlacedCallback(fn func(symbol string, order client.Order)) {
	g.onOrderPlaced = fn
}

// SetDynamicSizeCalculator sets the dynamic size calculator callback
func (g *GridManager) SetDynamicSizeCalculator(fn DynamicSizeCalculatorFunc) {
	g.dynamicSizeCalculatorMu.Lock()
	defer g.dynamicSizeCalculatorMu.Unlock()
	g.dynamicSizeCalculator = fn
	g.logger.Info("Dynamic size calculator set on GridManager")
}

// SetWebSocketClient sets an external WebSocket client to share connection
func (g *GridManager) SetWebSocketClient(wsClient *client.WebSocketClient) {
	g.wsClient = wsClient
}

// SymbolGrid represents a grid for a specific symbol.
type SymbolGrid struct {
	Symbol          string    `json:"symbol"`
	QuoteCurrency   string    `json:"quote_currency"`
	GridSpreadPct   float64   `json:"grid_spread"`
	MaxOrdersSide   int       `json:"max_orders"`
	CurrentPrice    float64   `json:"current_price"`
	MidPrice        float64   `json:"mid_price"`
	GridCenterPrice float64   `json:"grid_center_price"` // Track grid center for dynamic recentering
	IsActive        bool      `json:"is_active"`
	LastUpdate      time.Time `json:"last_update"`
	OrdersPlaced    bool      `json:"orders_placed"`
	PlacementBusy   bool      `json:"placement_busy"`
	LastAttempt     time.Time `json:"last_attempt"`
	LastRecenter    time.Time `json:"last_recenter"` // Prevent excessive recentering
}

// GridOrder represents an order in the grid.
type GridOrder struct {
	Symbol            string    `json:"symbol"`
	OrderID           string    `json:"order_id"`
	Side              string    `json:"side"`
	Size              float64   `json:"size"`
	Price             float64   `json:"price"`
	OrderType         string    `json:"order_type"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	FilledAt          time.Time `json:"filled_at,omitempty"`
	FeePaid           float64   `json:"fee_paid"`
	PointsEarned      int64     `json:"points_earned"`
	GridLevel         int       `json:"grid_level"`
	ReduceOnly        bool      `json:"reduce_only"`                    // For position rebalancing orders
	IsRebalance       bool      `json:"is_rebalance"`                   // True if this is a rebalancing order
	TakeProfitOrderID *string   `json:"take_profit_order_id,omitempty"` // ID of associated take profit order
}

// initTakeProfitManager initializes the take profit manager with configuration
func initTakeProfitManager(logger *zap.Logger) *adaptive_grid.TakeProfitManager {
	// Load micro profit config from file
	config, err := adaptive_grid.LoadFromFile("backend/config/micro_profit.yaml")
	if err != nil {
		logger.Warn("Failed to load micro profit config, using defaults",
			zap.Error(err))
		config = adaptive_grid.DefaultConfig()
	}

	// Create take profit manager
	tpMgr := adaptive_grid.NewTakeProfitManager(logger, config)
	logger.Info("Take profit manager initialized",
		zap.Bool("enabled", config.Enabled),
		zap.Float64("spread_pct", config.SpreadPct),
		zap.Int("timeout_seconds", config.TimeoutSeconds))

	// Start config watcher for hot-reload (only if enabled)
	if config.Enabled {
		if err := tpMgr.StartConfigWatcher("backend/config/micro_profit.yaml"); err != nil {
			logger.Warn("Failed to start config watcher for take profit manager",
				zap.Error(err))
		}
	}

	return tpMgr
}

// SetPnLRiskConfig sets the PnL risk control configuration
func (g *GridManager) SetPnLRiskConfig(cfg *config.PnLRiskControlConfig) {
	g.pnlRiskConfig = cfg
}

// SetMarketConditionEvaluator sets the market condition evaluator
func (g *GridManager) SetMarketConditionEvaluator(eval *adaptive_grid.MarketConditionEvaluator) {
	g.marketConditionEvaluator = eval
}

// SetOverSizeConfig sets the OVER_SIZE state configuration
func (g *GridManager) SetOverSizeConfig(cfg *config.OverSizeConfig) {
	g.overSizeConfig = cfg
}

// SetDefensiveConfig sets the DEFENSIVE state configuration
func (g *GridManager) SetDefensiveConfig(cfg *config.DefensiveStateConfig) {
	g.defensiveConfig = cfg
}

// SetRecoveryConfig sets the RECOVERY state configuration
func (g *GridManager) SetRecoveryConfig(cfg *config.RecoveryStateConfig) {
	g.recoveryConfig = cfg
}

// GetStateParameters returns state-specific parameter multipliers for the given symbol
func (g *GridManager) GetStateParameters(symbol string) (spreadMultiplier, sizeMultiplier float64) {
	if g.stateMachine == nil {
		return 1.0, 1.0 // Default multipliers
	}

	state := g.stateMachine.GetState(symbol)

	switch state {
	case adaptive_grid.GridStateDefensive:
		if g.defensiveConfig != nil {
			return g.defensiveConfig.SpreadMultiplier, 1.0
		}
	case adaptive_grid.GridStateRecovery:
		if g.recoveryConfig != nil {
			return g.recoveryConfig.SpreadMultiplier, g.recoveryConfig.SizeMultiplier
		}
	case adaptive_grid.GridStateOverSize:
		// OVER_SIZE doesn't have explicit multipliers in config, use conservative defaults
		return 1.5, 0.5 // Wider spread, smaller size
	case adaptive_grid.GridStateExitHalf, adaptive_grid.GridStateExitAll:
		// Exit states use conservative parameters
		return 1.2, 0.8
	}

	// Default for TRADING, IDLE, etc.
	return 1.0, 1.0
}

// checkStateTimeouts checks all active symbols for state timeouts and forces transitions
func (g *GridManager) checkStateTimeouts() {
	if g.stateMachine == nil {
		return
	}

	g.gridsMu.RLock()
	symbols := make([]string, 0, len(g.activeGrids))
	for symbol := range g.activeGrids {
		symbols = append(symbols, symbol)
	}
	g.gridsMu.RUnlock()

	for _, symbol := range symbols {
		isTimedOut, timeoutDuration, timeRemaining := g.stateMachine.CheckStateTimeout(symbol)
		if isTimedOut {
			g.logger.WithFields(logrus.Fields{
				"symbol":           symbol,
				"timeout_duration": timeoutDuration,
			}).Warn("State timeout detected, forcing transition")

			event := g.stateMachine.ForceTransitionOnTimeout(symbol)
			if event != adaptive_grid.GridEvent(-1) {
				g.logger.WithFields(logrus.Fields{
					"symbol":      symbol,
					"event":       event.String(),
					"forced":      true,
					"timeout_sec": timeoutDuration.Seconds(),
				}).Warn("State timeout: forced transition executed")
			}
		} else if timeoutDuration > 0 {
			// Log remaining time for states with timeouts (debug level)
			g.logger.WithFields(logrus.Fields{
				"symbol":        symbol,
				"timeout_sec":   timeoutDuration.Seconds(),
				"remaining_sec": timeRemaining.Seconds(),
			}).Debug("State timeout check: not timed out yet")
		}
	}
}

// stateTimeoutChecker runs continuously to check for state timeouts
// This is independent of warm-up phase and runs for the entire bot lifetime
func (g *GridManager) stateTimeoutChecker(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("State timeout checker goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	g.logger.Warn(" State timeout checker started (continuous)")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			g.logger.Warn("State timeout checker stopped")
			return
		case <-ticker.C:
			g.checkStateTimeouts()
		}
	}
}

// SetMetricsStreamer sets the metrics streamer for real-time dashboard
func (g *GridManager) SetMetricsStreamer(streamer interface{}) {
	g.metricsStreamer = streamer
}

// SetTickSizeManager sets the tick-size manager for price rounding
func (g *GridManager) SetTickSizeManager(tickSizeMgr interface{}) {
	g.tickSizeMgr = tickSizeMgr
	g.logger.Info("TickSizeManager set on GridManager")
}

// SetPostOnlyHandler sets the post-only handler for maker order optimization
func (g *GridManager) SetPostOnlyHandler(postOnlyHandler interface{}) {
	g.postOnlyHandler = postOnlyHandler
	g.logger.Info("PostOnlyHandler set on GridManager")
}

// SetSmartCancellationManager sets the smart cancellation manager
func (g *GridManager) SetSmartCancellationManager(smartCancelMgr interface{}) {
	g.smartCancelMgr = smartCancelMgr
	g.logger.Info("SmartCancellationManager set on GridManager")
}

// SetPennyJumpManager sets the penny jump manager
func (g *GridManager) SetPennyJumpManager(pennyJumpMgr interface{}) {
	g.pennyJumpMgr = pennyJumpMgr
	g.logger.Info("PennyJumpManager set on GridManager")
}

// SetInventoryHedgeManager sets the inventory hedge manager
func (g *GridManager) SetInventoryHedgeManager(inventoryHedgeMgr interface{}) {
	g.inventoryHedgeMgr = inventoryHedgeMgr
	g.logger.Info("InventoryHedgeManager set on GridManager")
}

// SetEquitySizingConfig sets the equity sizing configuration
func (g *GridManager) SetEquitySizingConfig(cfg *config.DynamicSizingConfig) {
	g.equityMu.Lock()
	defer g.equityMu.Unlock()
	g.equitySizingConfig = cfg
	g.logger.Info("EquitySizingConfig set on GridManager")
}

// InitializeEquityTracking initializes equity tracking with the initial balance
func (g *GridManager) InitializeEquityTracking(initialBalance float64) {
	g.equityMu.Lock()
	defer g.equityMu.Unlock()

	g.initialEquity = initialBalance
	g.currentEquity = initialBalance
	g.equityHistory = []EquitySnapshot{
		{
			Timestamp:   time.Now(),
			Equity:      initialBalance,
			RealizedPnL: 0,
			WinRate:     0,
		},
	}
	g.consecutiveWins = 0
	g.consecutiveLosses = 0
	g.logger.WithField("initial_equity", initialBalance).Info("Equity tracking initialized")
}

// UpdateEquity updates equity after a position close
func (g *GridManager) UpdateEquity(realizedPnL float64, isWin bool) {
	g.equityMu.Lock()
	defer g.equityMu.Unlock()

	g.currentEquity += realizedPnL

	// Update consecutive wins/losses
	if isWin {
		g.consecutiveWins++
		g.consecutiveLosses = 0
	} else {
		g.consecutiveLosses++
		g.consecutiveWins = 0
	}

	// Calculate win rate from history
	winRate := g.calculateWinRate24h()

	// Add snapshot
	snapshot := EquitySnapshot{
		Timestamp:   time.Now(),
		Equity:      g.currentEquity,
		RealizedPnL: realizedPnL,
		WinRate:     winRate,
	}

	// Keep only last 1000 snapshots
	g.equityHistory = append(g.equityHistory, snapshot)
	if len(g.equityHistory) > 1000 {
		g.equityHistory = g.equityHistory[len(g.equityHistory)-1000:]
	}

	g.logger.WithFields(logrus.Fields{
		"current_equity":     g.currentEquity,
		"realized_pnl":       realizedPnL,
		"is_win":             isWin,
		"consecutive_wins":   g.consecutiveWins,
		"consecutive_losses": g.consecutiveLosses,
		"win_rate":           winRate,
	}).Info("Equity updated")
}

// calculateWinRate24h calculates win rate from the last 24 hours
func (g *GridManager) calculateWinRate24h() float64 {
	if len(g.equityHistory) < 2 {
		return 0
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	wins := 0
	total := 0

	for _, snap := range g.equityHistory {
		if snap.Timestamp.After(cutoff) {
			total++
			if snap.RealizedPnL > 0 {
				wins++
			}
		}
	}

	if total == 0 {
		return 0
	}

	return float64(wins) / float64(total)
}

// GetWinRate24h returns the win rate over the last 24 hours
func (g *GridManager) GetWinRate24h() float64 {
	g.equityMu.RLock()
	defer g.equityMu.RUnlock()
	return g.calculateWinRate24h()
}

// CalculateEquityBasedSize calculates order size based on equity curve using Kelly Criterion
func (g *GridManager) CalculateEquityBasedSize(baseSize float64) float64 {
	g.equityMu.RLock()
	defer g.equityMu.RUnlock()

	if g.equitySizingConfig == nil || !g.equitySizingConfig.Enabled {
		return baseSize
	}

	// Kelly Criterion: Size = Kelly% × Equity / Leverage
	kellyFraction := g.equitySizingConfig.KellyFraction
	winRate := g.calculateWinRate24h()

	// Adjust Kelly based on win rate
	if winRate > 0.6 {
		// Higher win rate, can be more aggressive
		kellyFraction = math.Min(kellyFraction*1.2, g.equitySizingConfig.MaxKelly)
	} else if winRate < 0.4 {
		// Lower win rate, be more conservative
		kellyFraction = math.Max(kellyFraction*0.8, g.equitySizingConfig.MinKelly)
	}

	// Apply consecutive loss decay
	if g.consecutiveLosses > 0 {
		decay := math.Pow(g.equitySizingConfig.LossDecayRate, float64(g.consecutiveLosses))
		kellyFraction *= decay
	}

	// Apply consecutive win recovery
	if g.consecutiveWins > 0 {
		recovery := math.Pow(g.equitySizingConfig.WinRecoveryRate, float64(g.consecutiveWins))
		kellyFraction *= math.Min(recovery, g.equitySizingConfig.MaxRecoveryMultiplier)
	}

	// Calculate size
	leverage := 50.0 // Default leverage normalization
	equitySize := kellyFraction * g.currentEquity / leverage

	// Apply drawdown factor
	drawdownPct := (g.initialEquity - g.currentEquity) / g.initialEquity
	if drawdownPct > 0 {
		drawdownFactor := g.equitySizingConfig.DrawdownFactor - drawdownPct
		drawdownFactor = math.Max(drawdownFactor, g.equitySizingConfig.MinKelly)
		equitySize *= drawdownFactor
	}

	// Apply size limits
	minSize := g.equitySizingConfig.MinSizeUSDT
	maxSize := g.equitySizingConfig.MaxSizeUSDT
	equitySize = math.Max(equitySize, minSize)
	equitySize = math.Min(equitySize, maxSize)

	// Use the smaller of equity-based size and base size
	finalSize := math.Min(equitySize, baseSize)

	g.logger.WithFields(logrus.Fields{
		"base_size":      baseSize,
		"equity_size":    equitySize,
		"final_size":     finalSize,
		"kelly_fraction": kellyFraction,
		"win_rate":       winRate,
	}).Debug("Equity-based size calculated")

	return finalSize
}

// UpdateSmartCancelSpread updates spread for smart cancellation monitoring
func (g *GridManager) UpdateSmartCancelSpread(symbol string, bestBid, bestAsk float64) {
	if g.smartCancelMgr != nil {
		if scm, ok := g.smartCancelMgr.(interface {
			UpdateSpread(symbol string, bestBid, bestAsk float64)
		}); ok {
			scm.UpdateSpread(symbol, bestBid, bestAsk)
		}
	}
}

// UpdatePennyJumpPrices updates best bid/ask for penny jumping
func (g *GridManager) UpdatePennyJumpPrices(symbol string, bestBid, bestAsk float64) {
	if g.pennyJumpMgr != nil {
		if pjm, ok := g.pennyJumpMgr.(interface {
			UpdateBestPrices(symbol string, bestBid, bestAsk float64)
		}); ok {
			pjm.UpdateBestPrices(symbol, bestBid, bestAsk)
		}
	}
}

// broadcastMetric sends a metric to the dashboard if metricsStreamer is available
func (g *GridManager) broadcastMetric(metricType string, symbol string, data map[string]interface{}) {
	if g.metricsStreamer == nil {
		return
	}

	// Use type assertion to call BroadcastMetric method
	if streamer, ok := g.metricsStreamer.(interface {
		BroadcastMetric(metricType string, symbol string, data map[string]interface{}, timestamp time.Time)
	}); ok {
		streamer.BroadcastMetric(metricType, symbol, data, time.Now())
	}
}

// GetTotalVolume returns the total volume traded
func (g *GridManager) GetTotalVolume() float64 {
	g.volumeMetricsMu.RLock()
	defer g.volumeMetricsMu.RUnlock()
	return g.totalVolumeUSDT
}

// GetActiveOrderCount returns the number of active orders
func (g *GridManager) GetActiveOrderCount() int {
	g.ordersMu.RLock()
	defer g.ordersMu.RUnlock()
	return len(g.activeOrders)
}

// NewGridManager creates a new grid manager with volume farm config.
func NewGridManager(futuresClient *client.FuturesClient, logger *logrus.Entry, cfg *config.VolumeFarmConfig) *GridManager {
	zapLogger, _ := zap.NewDevelopment()

	// Use config values or defaults
	baseNotional := 20.0
	gridSpread := 0.005
	maxOrdersSide := 3
	placementCooldown := 500 * time.Millisecond
	rateLimitCooldown := 3 * time.Second
	maxNotional := 50.0 // Default max position size per order

	if cfg != nil {
		if cfg.OrderSizeUSDT > 0 {
			baseNotional = cfg.OrderSizeUSDT
		}
		if cfg.GridSpreadPct > 0 {
			gridSpread = cfg.GridSpreadPct
		}
		if cfg.MaxOrdersPerSide > 0 {
			maxOrdersSide = cfg.MaxOrdersPerSide
		}
		if cfg.GridPlacementCooldownSec > 0 {
			placementCooldown = time.Duration(cfg.GridPlacementCooldownSec) * time.Second
		}
		if cfg.RateLimitCooldownSec > 0 {
			rateLimitCooldown = time.Duration(cfg.RateLimitCooldownSec) * time.Second
		}
		// Use Risk config for max position size
		if cfg.Risk.MaxPositionUSDTPerSymbol > 0 {
			maxNotional = cfg.Risk.MaxPositionUSDTPerSymbol
		}
	}

	return &GridManager{
		futuresClient:         futuresClient,
		logger:                logger,
		activeGrids:           make(map[string]*SymbolGrid),
		pendingOrders:         make(map[string]*GridOrder),
		activeOrders:          make(map[string]*GridOrder),
		filledOrders:          make(map[string]*GridOrder),
		warmupActive:          make(map[string]bool),
		stopCh:                make(chan struct{}),
		baseNotionalUSD:       baseNotional,
		minNotionalUSD:        5.0,
		maxNotionalUSD:        maxNotional,
		useDynamicSizing:      true,
		gridSpreadPct:         gridSpread,
		maxOrdersSide:         maxOrdersSide,
		placementQueue:        make(chan string, 1024),
		gridPlacementCooldown: placementCooldown,
		rateLimitCooldown:     rateLimitCooldown,
		tickerStreamURL:       "wss://fstream.asterdex.com/ws/!ticker@arr",
		rateLimiter:           NewRateLimiter(100, 20, zapLogger),
		precisionMgr:          client.NewPrecisionManager(),
		priceHistory:          make(map[string][]PricePoint),
		atrPeriod:             14,
		orderLockMgr:          adaptive_grid.NewOrderLockManager(zapLogger),
		deduplicator:          adaptive_grid.NewFillEventDeduplicator(zapLogger),
		stateValidator:        adaptive_grid.NewStateValidator(zapLogger),
		gridGeometry:          adaptive_grid.NewAdaptiveGridGeometry(zapLogger),
		flowParameters:        make(map[string]adaptive_grid.FlowParameters),
		// Initialize take profit manager with micro profit config
		takeProfitMgr: initTakeProfitManager(zapLogger),
		// Position rebalancer defaults - OPTIMIZED for faster response
		rebalanceThresholdPct:   0.75,            // Start earlier at 75% of max
		rebalanceAggressiveness: 0.50,            // Reduce by 50% of excess (faster)
		rebalanceInterval:       5 * time.Second, // Check every 5s (quicker response)
		// NOTE: Exchange order cache removed - using wsClient.GetCachedOrders() as single source of truth
		// NOTE: Position cache removed - using wsClient.GetCachedPositions() as single source of truth
		// Equity tracking initialization
		equityHistory:     make([]EquitySnapshot, 0),
		consecutiveWins:   0,
		consecutiveLosses: 0,
	}
}

// ApplyConfig updates runtime grid settings from volume farming config.
func (g *GridManager) ApplyConfig(cfg *config.VolumeFarmConfig) {
	if cfg == nil {
		return
	}
	if cfg.OrderSizeUSDT > 0 {
		g.baseNotionalUSD = cfg.OrderSizeUSDT // Treat as notional USD
	}
	if cfg.GridSpreadPct > 0 {
		g.gridSpreadPct = cfg.GridSpreadPct
	}
	if cfg.MaxOrdersPerSide > 0 {
		g.maxOrdersSide = cfg.MaxOrdersPerSide
	}
	if cfg.GridPlacementCooldownSec > 0 {
		g.gridPlacementCooldown = time.Duration(cfg.GridPlacementCooldownSec) * time.Second
	}
	if cfg.RateLimitCooldownSec > 0 {
		g.rateLimitCooldown = time.Duration(cfg.RateLimitCooldownSec) * time.Second
	}
	// Update max position size from Risk config
	if cfg.Risk.MaxPositionUSDTPerSymbol > 0 {
		g.maxNotionalUSD = cfg.Risk.MaxPositionUSDTPerSymbol
		g.logger.WithField("max_notional_usd", g.maxNotionalUSD).Info("Max position size updated from config")
	}

	// Update max total exposure from Risk config (for global exposure limit)
	if cfg.Risk.MaxTotalPositionsUSDT > 0 {
		g.maxTotalNotionalUSD = cfg.Risk.MaxTotalPositionsUSDT
		g.logger.WithField("max_total_notional_usd", g.maxTotalNotionalUSD).Info("Max total exposure updated from config")
	}

	// Update equity sizing config from DynamicSizing
	if cfg.Risk.DynamicSizing != nil {
		g.SetEquitySizingConfig(cfg.Risk.DynamicSizing)
		g.logger.Info("Equity sizing config updated from DynamicSizing")
	}

	if wsURL := buildTickerStreamURL(cfg.Exchange.FuturesWSBase, cfg.TickerStream); wsURL != "" {
		g.tickerStreamURL = wsURL
	}

	// Update rate limiter if config provided
	if cfg.RateLimiterCapacity > 0 && cfg.RateLimiterRefillRate > 0 {
		zapLogger, _ := zap.NewDevelopment()
		g.rateLimiter = NewRateLimiter(float64(cfg.RateLimiterCapacity), cfg.RateLimiterRefillRate, zapLogger)
	}
}

// Start starts the grid manager.
func (g *GridManager) Start(ctx context.Context) error {
	g.isRunningMu.Lock()
	if g.isRunning {
		g.isRunningMu.Unlock()
		return fmt.Errorf("grid manager is already running")
	}
	g.isRunning = true
	g.isRunningMu.Unlock()

	g.logger.Info("Starting Grid Manager")

	// Fetch exchange info to populate precision manager
	marketClient := client.NewMarketClient(g.futuresClient.GetHTTPClient())
	exchangeInfo, err := marketClient.ExchangeInfo(ctx)
	if err != nil {
		g.logger.WithError(err).Warn("Failed to fetch exchange info, using default precision")
	} else {
		// Convert json.RawMessage to []byte for precision manager
		exchangeInfoBytes := []byte(exchangeInfo)
		g.precisionMgr.UpdateFromExchangeInfo(exchangeInfoBytes)
		g.logger.WithField("symbols_count", len(exchangeInfoBytes)).Info("Exchange info loaded successfully")
	}

	// Only create WebSocket client if not already set (e.g., via SetWebSocketClient for sharing)
	if g.wsClient == nil {
		zapLogger, _ := zap.NewDevelopment()
		g.wsClient = client.NewWebSocketClient(g.tickerStreamURL, zapLogger)

		if err := g.wsClient.Connect(ctx); err != nil {
			g.logger.WithError(err).Error("Failed to connect WebSocket to Aster API")
			return fmt.Errorf("failed to connect WebSocket: %w", err)
		}

		// Subscribe to all ticker array stream
		if err := g.wsClient.SubscribeToTicker([]string{"!ticker@arr"}); err != nil {
			g.logger.WithError(err).Warn("Failed to subscribe to ticker stream")
		}
	}

	g.wg.Add(1)
	go g.websocketProcessor(ctx)

	g.wg.Add(1)
	go g.metricsReporter(ctx)

	// Note: placementWorkers are started below (20 workers)

	g.wg.Add(1)
	go g.ordersResetWorker(ctx)

	// NOTE: UserStream is handled by VolumeFarmEngine to avoid duplicate connections
	// GridManager only handles ticker stream via wsClient

	// NEW: Poll for filled orders as fallback
	g.wg.Add(1)
	go g.orderFillPoller(ctx)

	// NEW: Log real exchange data for accurate dashboard display
	g.wg.Add(1)
	go g.exchangeDataReporter(ctx)

	// NEW: Start take profit timeout checker
	if g.takeProfitMgr != nil {
		g.takeProfitMgr.SetGridManager(g)
		g.takeProfitMgr.StartTimeoutChecker(ctx)
		g.logger.Info("Take profit timeout checker started")
	}

	// NEW: Start position rebalancer to manage oversized positions
	g.wg.Add(1)
	go g.positionRebalancerWorker(ctx)

	// NEW: Start grid limit enforcer to cancel excess orders beyond max_orders_per_side
	g.wg.Add(1)
	go g.gridLimitEnforcerWorker(ctx)

	// CRITICAL: Start global kline processor immediately for continuous price feed
	// This ensures isReadyForRegrid is evaluated continuously even without warmup
	// Warmup logic remains unchanged - this just ensures price feed is always available
	g.warmupOnce.Do(func() {
		g.warmupStopCh = make(chan struct{})
		g.warmupWg.Add(1)
		go g.globalKlineProcessor()
	})

	// Start continuous state timeout checker (independent of warm-up phase)
	// This prevents states from getting stuck in deadlocks
	g.wg.Add(1)
	go g.stateTimeoutChecker(ctx)

	// Start multiple placement workers for high volume concurrency
	numWorkers := 20 // 20 workers for massive volume farming
	for i := 0; i < numWorkers; i++ {
		g.wg.Add(1)
		go g.placementWorker(ctx, i)
	}

	// Trigger warm-up for any existing grids immediately (don't wait for UpdateSymbols)
	g.triggerWarmupForExistingGrids()

	g.logger.Warn("Grid Manager started successfully")
	return nil
}

// triggerWarmupForExistingGrids starts warm-up for grids that exist at startup
func (g *GridManager) triggerWarmupForExistingGrids() {
	g.gridsMu.RLock()
	existingGrids := make([]string, 0, len(g.activeGrids))
	for symbol := range g.activeGrids {
		existingGrids = append(existingGrids, symbol)
	}
	g.gridsMu.RUnlock()

	for _, symbol := range existingGrids {
		g.warmupMu.RLock()
		_, warmupStarted := g.warmupActive[symbol]
		g.warmupMu.RUnlock()

		if !warmupStarted {
			g.logger.WithField("symbol", symbol).Info("🔄 Startup: Existing grid needs warm-up - starting now")
			go g.startKlineWarmup(symbol)
		}
	}
}

// websocketProcessor processes real-time WebSocket data.
func (g *GridManager) websocketProcessor(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("WebSocket processor goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	tickerCh := g.wsClient.GetTickerChannel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case msg := <-tickerCh:
			g.processWebSocketTicker(msg)
		}
	}
}

// processWebSocketTicker processes real-time ticker data from WebSocket.
func (g *GridManager) processWebSocketTicker(msg map[string]interface{}) {
	g.logger.Info("ENTER processWebSocketTicker")
	data, ok := msg["data"].([]interface{})
	if !ok {
		g.logger.Debug("WebSocket message missing data field or wrong format")
		return
	}

	g.logger.WithField("ticker_count", len(data)).Info("Processing WebSocket ticker data")

	// Log all symbols in activeGrids for comparison
	g.gridsMu.RLock()
	activeSymbols := make([]string, 0, len(g.activeGrids))
	for sym := range g.activeGrids {
		activeSymbols = append(activeSymbols, sym)
	}
	g.gridsMu.RUnlock()
	g.logger.WithField("active_grids", activeSymbols).Warn(">>> ACTIVE GRIDS <<<")

	var symbolsToEnqueue []string

	for _, item := range data {
		ticker, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		symbolRaw, ok := ticker["s"].(string)
		if !ok {
			continue
		}
		// Normalize symbol to uppercase for consistent comparison
		symbol := strings.ToUpper(symbolRaw)
		g.logger.WithField("symbol", symbol).Debug(">>> PROCESSING TICKER SYMBOL <<<")

		lastPriceStr, ok := ticker["c"].(string)
		if !ok {
			continue
		}

		lastPrice, err := strconv.ParseFloat(lastPriceStr, 64)
		if err != nil {
			g.logger.WithError(err).Debug("Failed to parse last price")
			continue
		}

		g.gridsMu.Lock()
		grid, exists := g.activeGrids[symbol]
		if exists {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"last_price": lastPrice,
				"old_price":  grid.CurrentPrice,
			}).Info("Processing ticker for active grid")
		} else {
			// Only log first few non-active symbols to avoid spam
			if len(data) <= 5 {
				g.logger.WithField("symbol", symbol).Debug("Ignoring ticker for non-active symbol")
			}
			g.gridsMu.Unlock()
			continue
		}
		oldPrice := grid.CurrentPrice
		g.logger.WithFields(logrus.Fields{
			"symbol":    symbol,
			"oldPrice":  oldPrice,
			"lastPrice": lastPrice,
		}).Info("CHECK oldPrice value")
		grid.CurrentPrice = lastPrice
		grid.MidPrice = lastPrice
		grid.LastUpdate = time.Now()

		// NEW: Update spread for SmartCancellationManager
		if g.smartCancelMgr != nil {
			// Estimate spread based on grid spread percentage
			spreadAmount := lastPrice * (grid.GridSpreadPct / 100)
			bestBid := lastPrice - (spreadAmount / 2)
			bestAsk := lastPrice + (spreadAmount / 2)
			g.UpdateSmartCancelSpread(symbol, bestBid, bestAsk)
		}

		// NEW: Update best prices for PennyJumpManager
		if g.pennyJumpMgr != nil {
			// Estimate spread based on grid spread percentage
			spreadAmount := lastPrice * (grid.GridSpreadPct / 100)
			bestBid := lastPrice - (spreadAmount / 2)
			bestAsk := lastPrice + (spreadAmount / 2)
			g.UpdatePennyJumpPrices(symbol, bestBid, bestAsk)
		}

		// NEW: Feed price data to AdaptiveGridManager calculators
		if g.adaptiveMgr != nil {
			g.logger.WithField("symbol", symbol).Warn(">>> BEFORE UpdatePriceData <<<")
			// Use lastPrice as high, low, close (ticker only gives last price)
			// Pass 0 for bid/ask since ticker doesn't provide them
			g.adaptiveMgr.UpdatePriceData(symbol, lastPrice, lastPrice, lastPrice, 0, 0)
			g.logger.WithField("symbol", symbol).Warn(">>> AFTER UpdatePriceData <<<")
		}
		g.logger.WithField("symbol", symbol).Warn(">>> AFTER adaptiveMgr block <<<")

		// NEW: Check if grid needs recentering for dynamic grid
		if recentered := g.checkAndRecenterGrid(grid); recentered {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"old_center": oldPrice,
				"new_center": grid.GridCenterPrice,
			}).Info("DYNAMIC GRID: Grid recentered, will rebuild with new center")
		}

		if oldPrice != lastPrice && oldPrice != 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":    symbol,
				"old_price": oldPrice,
				"new_price": lastPrice,
				"source":    "websocket",
			}).Debug("Grid price updated")
		}

		g.logger.WithField("oldPrice", oldPrice).Warn(">>> ABOUT TO CHECK oldPrice == 0 <<<")
		if oldPrice == 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"last_price":     lastPrice,
				"placement_busy": grid.PlacementBusy,
				"grid_exists":    exists,
			}).Warn(">>> FIRST PRICE RECEIVED - WILL ENQUEUE PLACEMENT <<<")
			// Initialize grid center with first price
			grid.GridCenterPrice = lastPrice
			grid.PlacementBusy = true
			symbolsToEnqueue = append(symbolsToEnqueue, symbol)
		} else if g.shouldSchedulePlacement(grid, oldPrice) {
			grid.PlacementBusy = true
			symbolsToEnqueue = append(symbolsToEnqueue, symbol)
			g.logger.WithField("symbol", symbol).Debug("Scheduling grid placement due to price update")
		} else {
			g.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"old_price":      oldPrice,
				"current_price":  grid.CurrentPrice,
				"placement_busy": grid.PlacementBusy,
				"orders_placed":  grid.OrdersPlaced,
			}).Debug("Not scheduling placement")
		}
		g.gridsMu.Unlock()
	}

	g.logger.WithFields(logrus.Fields{
		"ticker_count":       len(data),
		"symbols_to_enqueue": symbolsToEnqueue,
		"enqueue_count":      len(symbolsToEnqueue),
	}).Debug("Completed processing WebSocket ticker batch")

	g.logger.WithFields(logrus.Fields{
		"symbols_to_enqueue": symbolsToEnqueue,
		"enqueue_count":      len(symbolsToEnqueue),
	}).Info("Enqueuing check completed")

	if len(symbolsToEnqueue) > 0 {
		g.logger.WithFields(logrus.Fields{
			"symbols": symbolsToEnqueue,
			"count":   len(symbolsToEnqueue),
		}).Info("Enqueuing symbols for placement - WILL PLACE ORDERS")
	}
	for _, sym := range symbolsToEnqueue {
		g.enqueuePlacement(sym)
	}
}

func (g *GridManager) canPlaceForSymbol(symbol string) bool {
	g.logger.WithField("symbol", symbol).Info("=== CHECKING canPlaceForSymbol ===")

	// NEW: Root cause fix - Bypass checks if stuck in ENTER_GRID > 30 seconds
	// This prevents single point of failure where ENTER_GRID state gets stuck
	if g.stateMachine != nil {
		gridState := g.stateMachine.GetState(symbol)
		if gridState == adaptive_grid.GridStateEnterGrid {
			stateTime := g.stateMachine.GetStateTime(symbol)
			timeInState := time.Since(stateTime)
			if timeInState > 30*time.Second {
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"time_in_state": timeInState,
				}).Warn(">>> BYPASSING GATES: ENTER_GRID stuck > 30s, forcing placement <<<")
				return true // Force bypass all checks
			}
		}
	}

	if g.adaptiveMgr != nil {
		g.logger.WithField("symbol", symbol).Info("Checking AdaptiveGridManager.CanPlaceOrder")
		// AdaptiveGridManager.CanPlaceOrder handles all trading decision checks
		// CircuitBreaker is the single source of truth for trading decisions
		canPlace := g.adaptiveMgr.CanPlaceOrder(symbol)
		g.logger.WithFields(logrus.Fields{
			"symbol":    symbol,
			"can_place": canPlace,
		}).Info("AdaptiveGridManager.CanPlaceOrder result")
		if !canPlace {
			g.logger.WithField("symbol", symbol).Warn(">>> GATE BLOCKED: Adaptive manager disallows order <<<")
			return false
		}
	}

	if g.stateMachine != nil {
		shouldEnqueue := g.stateMachine.ShouldEnqueuePlacement(symbol)
		gridState := g.stateMachine.GetState(symbol)
		g.logger.WithFields(logrus.Fields{
			"symbol":         symbol,
			"should_enqueue": shouldEnqueue,
			"grid_state":     gridState,
		}).Info("StateMachine.ShouldEnqueuePlacement result")
		if !shouldEnqueue {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_state": gridState,
			}).Warn(">>> GATE BLOCKED: Grid state machine not in placement state <<<")
			return false
		}
	}

	g.logger.WithField("symbol", symbol).Info(">>> ALL GATES PASSED - CAN PLACE <<<")
	return true
}

func (g *GridManager) shouldSchedulePlacement(grid *SymbolGrid, oldPrice float64) bool {
	if grid == nil || !grid.IsActive || grid.CurrentPrice <= 0 {
		symbol := ""
		if grid != nil {
			symbol = grid.Symbol
		}
		g.logger.WithField("symbol", symbol).Debug("Not scheduling: grid inactive or no price")
		return false
	}

	if !g.canPlaceForSymbol(grid.Symbol) {
		return false
	}

	// NEW: Force initial placement when grid is first created (OrdersPlaced = false)
	// This ensures bot places orders even if price is stable
	if !grid.OrdersPlaced {
		g.logger.WithFields(logrus.Fields{
			"symbol": grid.Symbol,
		}).Info("Scheduling initial placement (first time)")
		return true
	}

	// For price-based triggering, use tiny threshold for sensitive triggers
	// This check comes FIRST to allow rebalancing when price moves significantly
	priceChangeThreshold := 0.0001 // 0.01% change (ultra sensitive)
	if oldPrice > 0 {
		priceChangePct := math.Abs(grid.CurrentPrice-oldPrice) / oldPrice
		if priceChangePct >= priceChangeThreshold {
			// Check if we have stale orders that need updating
			expected := grid.MaxOrdersSide * 2
			actual := g.countActiveGridOrders(grid.Symbol)

			if grid.OrdersPlaced && actual >= expected {
				// Grid is "complete" but price moved - check if we should rebalance
				if time.Since(grid.LastAttempt) > 5*time.Second {
					// Enough time passed, allow rebalancing to update order prices
					g.logger.WithFields(logrus.Fields{
						"symbol":       grid.Symbol,
						"price_change": priceChangePct,
						"old_price":    oldPrice,
						"new_price":    grid.CurrentPrice,
					}).Info("Scheduling rebalancing due to price change (grid complete but stale)")
					return true
				}
				g.logger.WithFields(logrus.Fields{
					"symbol":       grid.Symbol,
					"price_change": priceChangePct,
					"since_last":   time.Since(grid.LastAttempt),
				}).Debug("Grid complete, skipping rebalancing (cooldown)")
				return false
			}

			g.logger.WithFields(logrus.Fields{
				"symbol":       grid.Symbol,
				"price_change": priceChangePct,
				"threshold":    priceChangeThreshold,
			}).Info("Scheduling placement due to price change")
			return true
		}
	}

	// For volume farming, we want to place orders more frequently
	// Allow placement even if grid was partially placed before
	if grid.OrdersPlaced {
		// Check if grid is complete (has expected number of active orders)
		expected := grid.MaxOrdersSide * 2
		actual := g.countActiveGridOrders(grid.Symbol)
		if actual >= expected {
			g.logger.WithFields(logrus.Fields{
				"symbol":   grid.Symbol,
				"expected": expected,
				"actual":   actual,
			}).Debug("Grid complete, skipping placement")
			return false
		}
		// Grid incomplete - allow re-placement to fill missing orders
		g.logger.WithFields(logrus.Fields{
			"symbol":   grid.Symbol,
			"expected": expected,
			"actual":   actual,
		}).Info("Grid incomplete, allowing re-placement")
		return true
	}

	// Only skip if currently busy placing orders
	if grid.PlacementBusy {
		g.logger.WithField("symbol", grid.Symbol).Debug("Not scheduling: placement busy")
		return false
	}

	// Volume farming: ultra-short cooldown (200ms base)
	baseCooldown := 200 * time.Millisecond
	dynamicCooldown := baseCooldown

	// Increase cooldown only if there are consecutive failures
	if g.consecutiveFailures > 2 && time.Since(g.lastFailureTime) < 5*time.Minute {
		dynamicCooldown = 3 * time.Second
	}

	// For volume farming, allow very frequent placement attempts
	// If no recent attempt, or short cooldown has passed, allow placement
	if grid.LastAttempt.IsZero() || time.Since(grid.LastAttempt) >= dynamicCooldown {
		g.logger.WithField("symbol", grid.Symbol).Info("Scheduling placement allowed for volume farming")
		return true
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":     grid.Symbol,
		"since_last": time.Since(grid.LastAttempt),
		"cooldown":   dynamicCooldown,
	}).Debug("Not scheduling: cooldown active")
	return false
}

// checkAndRecenterGrid checks if grid needs recentering due to price movement
// Returns true if recentering was performed
func (g *GridManager) checkAndRecenterGrid(grid *SymbolGrid) bool {
	if grid == nil || !grid.IsActive || grid.CurrentPrice <= 0 {
		return false
	}

	// Initialize center if not set
	if grid.GridCenterPrice <= 0 {
		grid.GridCenterPrice = grid.CurrentPrice
		return false
	}

	// Calculate how far price moved from center
	center := grid.GridCenterPrice
	current := grid.CurrentPrice
	distancePct := math.Abs(current-center) / center

	// Calculate grid range (half of total grid span)
	// Grid spans from: center - (spread * maxOrders) to center + (spread * maxOrders)
	gridSpread := grid.GridSpreadPct / 100
	halfGridRange := gridSpread * float64(grid.MaxOrdersSide)

	// Recenter threshold: when price moves beyond 85% of half grid range
	// Higher threshold for micro profit - let orders live longer to capture fills
	// 85% means outermost orders are still within effective range
	recenterThreshold := halfGridRange * 0.85

	// Minimum recenter interval: 2 minutes for micro grid stability
	// Shorter intervals cause missed fills due to frequent cancellations
	minRecenterInterval := 2 * time.Minute
	if time.Since(grid.LastRecenter) < minRecenterInterval {
		return false
	}

	// CRITICAL: For micro profit, check if we have pending fills near current price
	// Don't recenter if recent fills happened - let grid complete its job
	recentFill := false
	if g.filledOrders != nil {
		for _, order := range g.filledOrders {
			if order.Symbol == grid.Symbol && time.Since(order.FilledAt) < 30*time.Second {
				recentFill = true
				break
			}
		}
	}

	if recentFill {
		g.logger.WithField("symbol", grid.Symbol).Debug("Skipping recenter - recent fill detected, let grid stabilize")
		return false
	}

	if distancePct >= recenterThreshold {
		g.logger.WithFields(logrus.Fields{
			"symbol":          grid.Symbol,
			"center":          center,
			"current":         current,
			"distance_pct":    distancePct,
			"threshold":       recenterThreshold,
			"half_grid_range": halfGridRange,
			"cooldown":        minRecenterInterval,
		}).Info("DYNAMIC GRID: Recentering grid due to significant price movement")

		// Cancel all existing grid orders (they're too far from new center)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cancelled := g.cancelAllGridOrders(ctx, grid.Symbol)
		g.logger.WithFields(logrus.Fields{
			"symbol":    grid.Symbol,
			"cancelled": cancelled,
		}).Info("DYNAMIC GRID: Cancelled old orders for recentering")

		// Update center to current price
		grid.GridCenterPrice = current
		grid.MidPrice = current
		grid.LastRecenter = time.Now()
		grid.OrdersPlaced = false // Force full rebuild

		return true
	}

	return false
}

// cancelAllGridOrders cancels all active grid orders for a symbol via API
func (g *GridManager) cancelAllGridOrders(ctx context.Context, symbol string) int {
	g.ordersMu.Lock()
	var ordersToCancel []*GridOrder
	for _, order := range g.activeOrders {
		if order.Symbol == symbol && order.Status != "FILLED" && order.Status != "CANCELLED" {
			ordersToCancel = append(ordersToCancel, order)
		}
	}
	g.ordersMu.Unlock()

	cancelled := 0
	for _, order := range ordersToCancel {
		orderIDInt, _ := strconv.ParseInt(order.OrderID, 10, 64)
		if orderIDInt == 0 {
			// Skip orders without valid order ID
			continue
		}
		_, err := g.futuresClient.CancelOrder(ctx, client.CancelOrderRequest{
			Symbol:  order.Symbol,
			OrderID: orderIDInt,
		})
		if err != nil {
			g.logger.WithError(err).WithFields(logrus.Fields{
				"symbol":  order.Symbol,
				"orderID": order.OrderID,
			}).Warn("Failed to cancel order during recenter")
		} else {
			g.ordersMu.Lock()
			if o, exists := g.activeOrders[order.OrderID]; exists {
				o.Status = "CANCELLED"
			}
			delete(g.activeOrders, order.OrderID)
			g.ordersMu.Unlock()
			cancelled++
		}
	}

	return cancelled
}

// UpdateSymbols updates the list of symbols to manage.
func (g *GridManager) UpdateSymbols(symbols []*SymbolData) {
	desired := make(map[string]*SymbolData, len(symbols))
	for _, symbolData := range symbols {
		desired[symbolData.Symbol] = symbolData
	}

	var created []string
	var removed []string

	g.gridsMu.Lock()
	for symbol, symbolData := range desired {
		if _, exists := g.activeGrids[symbol]; exists {
			continue
		}
		g.activeGrids[symbol] = &SymbolGrid{
			Symbol:          symbolData.Symbol,
			QuoteCurrency:   symbolData.QuoteAsset,
			GridSpreadPct:   g.gridSpreadPct,
			MaxOrdersSide:   g.maxOrdersSide,
			IsActive:        true,
			GridCenterPrice: 0,                                        // Will be set when first price received
			LastAttempt:     time.Now().Add(-g.gridPlacementCooldown), // Force initial placement
		}
		g.logger.WithFields(logrus.Fields{
			"symbol":          symbol,
			"max_orders_side": g.maxOrdersSide,
		}).Info("DEBUG: Created SymbolGrid with maxOrdersSide")
		created = append(created, symbolData.Symbol)
	}

	for symbol := range g.activeGrids {
		if _, found := desired[symbol]; found {
			// Ensure range detector is initialized for existing grid
			if g.adaptiveMgr != nil {
				rangeState := g.adaptiveMgr.GetRangeState(symbol)
				if rangeState == adaptive_grid.RangeStateUnknown {
					g.logger.WithField("symbol", symbol).Warn("Existing grid missing range detector - initializing now")
					g.adaptiveMgr.InitializeRangeDetector(symbol, nil)
				}
			}

			// Check if existing grid needs warm-up (not yet warmed up)
			g.warmupMu.RLock()
			_, warmupStarted := g.warmupActive[symbol]
			g.warmupMu.RUnlock()
			if !warmupStarted {
				// Grid exists but warm-up not started - start it now
				g.logger.WithField("symbol", symbol).Info("Existing grid needs warm-up - starting now")
				go g.startKlineWarmup(symbol)
			}
			continue
		}
		delete(g.activeGrids, symbol)
		removed = append(removed, symbol)
	}
	g.gridsMu.Unlock()

	for _, symbol := range created {
		g.logger.WithField("symbol", symbol).Info("Created new grid")

		// Initialize range detector for this symbol
		if g.adaptiveMgr != nil {
			g.adaptiveMgr.InitializeRangeDetector(symbol, nil)
			g.logger.WithField("symbol", symbol).Info("Range detector initialized")
		}

		// Log grid creation
		if g.activityLog != nil {
			g.activityLog.Log(context.Background(), activitylog.EventGridCreated, activitylog.SeverityInfo,
				activitylog.EntryContext{Symbol: symbol, StrategyName: "grid"},
				activitylog.GridCreatedPayload{
					GridID: fmt.Sprintf("grid_%s_%d", symbol, time.Now().Unix()),
					Symbol: symbol,
					Levels: g.maxOrdersSide * 2,
				},
			)
		}
		// Start WebSocket kline warm-up phase for RangeDetector
		// This feeds OHLC data via WebSocket instead of REST API to avoid spam
		g.startKlineWarmup(symbol)
	}
	for _, symbol := range removed {
		g.logger.WithField("symbol", symbol).Info("Removed grid")
	}
}

// getActiveGridLevels returns the grid levels that already have active orders
func (g *GridManager) getActiveGridLevels(symbol string) (map[int]bool, map[int]bool) {
	g.ordersMu.RLock()
	defer g.ordersMu.RUnlock()

	buyLevels := make(map[int]bool)
	sellLevels := make(map[int]bool)

	for _, order := range g.activeOrders {
		if order.Symbol == symbol && order.Status != "FILLED" && order.Status != "CANCELLED" {
			if order.Side == "BUY" {
				buyLevels[order.GridLevel] = true
			} else if order.Side == "SELL" {
				sellLevels[order.GridLevel] = true
			}
		}
	}

	return buyLevels, sellLevels
}

// startKlineWarmup starts the WebSocket kline warm-up phase for a symbol
// This feeds OHLC data to RangeDetector via WebSocket instead of REST API
func (g *GridManager) startKlineWarmup(symbol string) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Start kline warmup goroutine panic recovered",
				zap.String("symbol", symbol),
				zap.Any("panic", r))
		}
	}()

	if g.wsClient == nil {
		g.logger.WithField("symbol", symbol).Warn("Cannot start warm-up: WebSocket client not available")
		return
	}

	if g.adaptiveMgr == nil {
		g.logger.WithField("symbol", symbol).Warn("Cannot start warm-up: adaptive manager not available")
		return
	}

	// Mark symbol as in warmup phase
	g.warmupMu.Lock()
	g.warmupActive[symbol] = true
	g.warmupMu.Unlock()

	g.logger.WithField("symbol", symbol).Info("🚀 Starting WebSocket kline warm-up phase")

	// Subscribe to kline stream for this symbol (1m interval)
	if err := g.wsClient.SubscribeToKlines([]string{symbol}, "1m"); err != nil {
		g.logger.WithFields(logrus.Fields{
			"symbol": symbol,
			"error":  err.Error(),
		}).Warn("Failed to subscribe to kline stream, will use ticker fallback")
		// Fallback: just wait for ticker
		g.warmupMu.Lock()
		delete(g.warmupActive, symbol)
		g.warmupMu.Unlock()
		return
	}

	// Start per-symbol warmup completion checker
	g.wg.Add(1)
	go g.warmupCompletionChecker(symbol)
}

// globalKlineProcessor is a SINGLETON that reads from kline channel and broadcasts
// to ALL symbols in warm-up phase. This fixes race condition where multiple goroutines
// read from the same channel causing messages to be lost.
func (g *GridManager) globalKlineProcessor() {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Global kline processor goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.warmupWg.Done()

	g.logger.Info("🌐 Global kline processor started (singleton)")

	if g.wsClient == nil {
		g.logger.Error("GLOBAL WARM-UP: WebSocket client is nil!")
		return
	}

	klineCh := g.wsClient.GetKlineChannel()
	if klineCh == nil {
		g.logger.Error("GLOBAL WARM-UP: Kline channel is nil!")
		return
	}

	// Start periodic depth monitor
	depthMonitor := time.NewTicker(10 * time.Second)
	defer depthMonitor.Stop()

	for {
		select {
		case <-g.warmupStopCh:
			g.logger.Info("Global kline processor stopped")
			return

		case kline := <-klineCh:
			symbol := strings.ToUpper(kline.Symbol)

			// ALWAYS feed klines to AdaptiveGridManager for continuous reentry checks
			// This ensures isReadyForRegrid is evaluated continuously even after warm-up
			g.logger.WithFields(logrus.Fields{
				"symbol":    symbol,
				"is_closed": kline.IsClosed,
				"close":     kline.Close,
			}).Debug("GLOBAL: feeding kline to RangeDetector")

			// Feed OHLC data to RangeDetector for this symbol
			g.adaptiveMgr.UpdatePriceForRange(symbol, kline.High, kline.Low, kline.Close)

			// Check PnL-based risk control
			g.checkPnLRisk(symbol)

			// Check position size for OVER_SIZE state
			g.checkPositionSize(symbol)

			// Evaluate market conditions and recommend state (if enabled)
			if g.marketConditionEvaluator != nil {
				g.logger.WithFields(logrus.Fields{
					"symbol":    symbol,
					"evaluator": "market_condition_evaluator",
				}).Debug("Calling market condition evaluator")

				recommendation, err := g.marketConditionEvaluator.Evaluate(symbol)
				if err == nil {
					g.logger.WithFields(logrus.Fields{
						"symbol":     symbol,
						"state":      recommendation.State.String(),
						"confidence": recommendation.Confidence,
						"reason":     recommendation.Reason,
					}).Debug("Market condition evaluation")

					// Trigger state transition based on recommendation and confidence
					evalConfig := g.marketConditionEvaluator.GetConfig()
					if recommendation.Confidence >= evalConfig.MinConfidenceThreshold {
						g.triggerStateTransitionFromRecommendation(symbol, recommendation)
					}
				} else {
					g.logger.WithFields(logrus.Fields{
						"symbol": symbol,
						"error":  err,
					}).Error("Market condition evaluation failed")
				}
			}

			// NEW: Calculate FluidFlow parameters for "soft like water" behavior
			if g.adaptiveMgr != nil {
				// Get market condition data for flow calculation
				positions := g.wsClient.GetCachedPositions()
				position, hasPosition := positions[symbol]
				positionSize := 0.0
				if hasPosition && position.PositionAmt != 0 {
					positionSize = math.Abs(position.PositionAmt * position.MarkPrice)
				}

				// Get max position for normalization
				maxPosition := 100.0
				if g.maxNotionalUSD > 0 {
					maxPosition = g.maxNotionalUSD
				}
				normalizedPositionSize := positionSize / maxPosition

				// Get volatility from range detector (ATR)
				volatility := 0.5 // default
				rangeDetector := g.adaptiveMgr.GetRangeDetector(symbol)
				if rangeDetector != nil {
					atr := rangeDetector.GetATR()
					if atr > 0 && kline.Close > 0 {
						volatility = math.Min(atr/kline.Close, 1.0)
					}
				}

				// Get trend from range detector (ADX)
				trend := 0.5 // neutral
				if rangeDetector != nil {
					adx := rangeDetector.GetCurrentADX()
					if adx > 25 {
						trend = 0.8 // trending
					} else if adx < 20 {
						trend = 0.2 // sideways
					}
				}

				// Get risk from unrealized PnL
				risk := 0.0
				if hasPosition {
					risk = math.Abs(position.UnrealizedProfit) / positionSize
					if risk > 0.1 {
						risk = 0.9 // high risk
					}
				}

				// Calculate flow parameters
				intensity := (1.0 - normalizedPositionSize) * (1.0 - volatility) * (1.0 - risk)
				intensity += trend * 0.2
				intensity = math.Max(0, math.Min(1, intensity))

				// Store flow parameters for use in order placement
				g.flowParamsMu.Lock()
				g.flowParameters[symbol] = adaptive_grid.FlowParameters{
					Intensity:        intensity,
					Direction:        0.5, // neutral for now
					Velocity:         0.0,
					SizeMultiplier:   intensity * (1.0 - risk*0.3),
					SpreadMultiplier: (2.0 - intensity) + volatility*1.5,
				}
				g.flowParamsMu.Unlock()

				g.logger.WithFields(logrus.Fields{
					"symbol":            symbol,
					"intensity":         intensity,
					"size_multiplier":   g.flowParameters[symbol].SizeMultiplier,
					"spread_multiplier": g.flowParameters[symbol].SpreadMultiplier,
				}).Debug("Fluid flow parameters calculated")
			}

			// Log once per symbol that evaluator is not available
			if g.marketConditionEvaluator == nil {
				if _, logged := g.evaluatorNotLogged[symbol]; !logged {
					g.logger.WithFields(logrus.Fields{
						"symbol": symbol,
					}).Warn("Market condition evaluator not initialized - adaptive states disabled")
					if g.evaluatorNotLogged == nil {
						g.evaluatorNotLogged = make(map[string]bool)
					}
					g.evaluatorNotLogged[symbol] = true
				}
			}

		case <-depthMonitor.C:
			// Periodic channel depth check
			depth := g.wsClient.GetKlineChannelDepth()
			if depth > 1000 {
				g.logger.WithField("depth", depth).Warn("GLOBAL: kline channel depth high - consumer may be slow")
			}
		}
	}
}

// checkPnLRisk checks unrealized PnL and triggers state transitions for risk control
func (g *GridManager) checkPnLRisk(symbol string) {
	// Check if PnL risk control is enabled
	if g.pnlRiskConfig == nil || !g.pnlRiskConfig.Enabled {
		return
	}

	// Get position from WebSocket cache
	positions := g.wsClient.GetCachedPositions()
	position, hasPosition := positions[symbol]

	if !hasPosition || position.PositionAmt == 0 {
		// No position, nothing to check
		return
	}

	unrealizedPnL := position.UnrealizedProfit
	currentState := g.adaptiveMgr.GetStateMachine().GetState(symbol)

	g.logger.WithFields(logrus.Fields{
		"symbol":         symbol,
		"unrealized_pnl": unrealizedPnL,
		"state":          currentState.String(),
	}).Debug("PnL Risk Check")

	// Get thresholds from config
	partialLossThreshold := -g.pnlRiskConfig.PartialLossUSDT
	fullLossThreshold := -g.pnlRiskConfig.FullLossUSDT
	recoveryThreshold := g.pnlRiskConfig.RecoveryThresholdUSDT

	switch currentState {
	case adaptive_grid.GridStateTrading:
		// TRADING: Check if loss > threshold → trigger EXIT_HALF
		if unrealizedPnL <= partialLossThreshold {
			g.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"unrealized_pnl": unrealizedPnL,
				"threshold":      partialLossThreshold,
			}).Warn("PnL Risk: Partial loss threshold hit, triggering EXIT_HALF")

			g.triggerExitHalf(symbol, position, unrealizedPnL)
		}

	case adaptive_grid.GridStateExitHalf:
		// EXIT_HALF: Check if loss > full threshold → trigger EXIT_ALL
		// OR if recovered → trigger RECOVERY back to TRADING
		if unrealizedPnL <= fullLossThreshold {
			g.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"unrealized_pnl": unrealizedPnL,
				"threshold":      fullLossThreshold,
			}).Error("PnL Risk: Full loss threshold hit in EXIT_HALF, triggering EXIT_ALL")

			g.adaptiveMgr.GetStateMachine().Transition(symbol, adaptive_grid.EventFullLoss)
			g.executeExitAll(symbol, "Full loss in EXIT_HALF state")
		} else if unrealizedPnL >= recoveryThreshold {
			g.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"unrealized_pnl": unrealizedPnL,
				"threshold":      recoveryThreshold,
			}).Info("PnL Risk: Position recovered, triggering RECOVERY to TRADING")

			g.adaptiveMgr.GetStateMachine().Transition(symbol, adaptive_grid.EventRecovery)
		}
	}
}

// checkPositionSize checks position size and triggers OVER_SIZE state transitions
func (g *GridManager) checkPositionSize(symbol string) {
	// Check if OVER_SIZE config is enabled
	if g.overSizeConfig == nil {
		return
	}

	// Get position from WebSocket cache
	positions := g.wsClient.GetCachedPositions()
	position, hasPosition := positions[symbol]

	if !hasPosition || position.PositionAmt == 0 {
		// No position, nothing to check
		return
	}

	// Calculate position notional value
	positionNotional := math.Abs(position.PositionAmt * position.MarkPrice)
	currentState := g.adaptiveMgr.GetStateMachine().GetState(symbol)

	// Get max position USDT from config (default to 100 if not set)
	maxPositionUSDT := 100.0
	if g.maxNotionalUSD > 0 {
		maxPositionUSDT = g.maxNotionalUSD
	}

	threshold := maxPositionUSDT * g.overSizeConfig.ThresholdPct
	recoveryLevel := maxPositionUSDT * g.overSizeConfig.RecoveryPct

	g.logger.WithFields(logrus.Fields{
		"symbol":            symbol,
		"position_notional": positionNotional,
		"max_position_usdt": maxPositionUSDT,
		"threshold":         threshold,
		"recovery_level":    recoveryLevel,
		"state":             currentState.String(),
	}).Info("Position Size Check")

	switch currentState {
	case adaptive_grid.GridStateTrading:
		// TRADING: Check if position size > threshold → trigger OVER_SIZE
		if positionNotional > threshold {
			// NEW: Check state stability duration before transition
			lastTransitionTime := g.adaptiveMgr.GetStateMachine().GetStateTime(symbol)
			timeSinceLastTransition := time.Since(lastTransitionTime)
			minStabilityDuration := 30 * time.Second // Default 30s cooldown

			if timeSinceLastTransition < minStabilityDuration {
				g.logger.WithFields(logrus.Fields{
					"symbol":                     symbol,
					"position_notional":          positionNotional,
					"threshold":                  threshold,
					"time_since_last_transition": timeSinceLastTransition.Seconds(),
				}).Debug("Position Size: OVER_SIZE transition skipped due to stability duration")
				return
			}

			g.logger.WithFields(logrus.Fields{
				"symbol":            symbol,
				"position_notional": positionNotional,
				"threshold":         threshold,
			}).Warn("Position Size: Size exceeds threshold, triggering OVER_SIZE")

			g.adaptiveMgr.GetStateMachine().Transition(symbol, adaptive_grid.EventOverSizeLimit)
		}

	case adaptive_grid.GridStateOverSize:
		// OVER_SIZE: Check if position size <= recovery level → trigger TRADING
		if positionNotional <= recoveryLevel {
			// NEW: Check state stability duration before transition
			lastTransitionTime := g.adaptiveMgr.GetStateMachine().GetStateTime(symbol)
			timeSinceLastTransition := time.Since(lastTransitionTime)
			minStabilityDuration := 30 * time.Second // Default 30s cooldown

			if timeSinceLastTransition < minStabilityDuration {
				g.logger.WithFields(logrus.Fields{
					"symbol":                     symbol,
					"position_notional":          positionNotional,
					"recovery_level":             recoveryLevel,
					"time_since_last_transition": timeSinceLastTransition.Seconds(),
				}).Debug("Position Size: TRADING transition skipped due to stability duration")
				return
			}

			g.logger.WithFields(logrus.Fields{
				"symbol":            symbol,
				"position_notional": positionNotional,
				"recovery_level":    recoveryLevel,
			}).Info("Position Size: Size normalized, triggering TRADING")

			g.adaptiveMgr.GetStateMachine().Transition(symbol, adaptive_grid.EventSizeNormalized)
		}
	}
}

// triggerStateTransitionFromRecommendation triggers state transition based on market condition evaluator recommendation
func (g *GridManager) triggerStateTransitionFromRecommendation(symbol string, recommendation *adaptive_grid.StateRecommendation) {
	currentState := g.adaptiveMgr.GetStateMachine().GetState(symbol)

	// Only transition if recommended state is different from current state
	if recommendation.State == currentState {
		return
	}

	// NEW: Enforce state stability duration to prevent rapid state changes
	// Default cooldown: 30 seconds (can be configured)
	stateStabilityDuration := 30 * time.Second
	if g.marketConditionEvaluator != nil {
		evalConfig := g.marketConditionEvaluator.GetConfig()
		if evalConfig.StateStabilityDuration > 0 {
			stateStabilityDuration = time.Duration(evalConfig.StateStabilityDuration) * time.Second
		}
	}

	lastTransitionTime := g.adaptiveMgr.GetStateMachine().GetStateTime(symbol)
	timeSinceLastTransition := time.Since(lastTransitionTime)

	if timeSinceLastTransition < stateStabilityDuration {
		g.logger.WithFields(logrus.Fields{
			"symbol":                     symbol,
			"current_state":              currentState.String(),
			"recommended_state":          recommendation.State.String(),
			"time_since_last_transition": timeSinceLastTransition.Seconds(),
			"stability_duration":         stateStabilityDuration.Seconds(),
		}).Debug("Market Condition: State transition skipped due to stability duration")
		return
	}

	var event adaptive_grid.GridEvent

	// Map recommended state to appropriate event
	switch recommendation.State {
	case adaptive_grid.GridStateOverSize:
		event = adaptive_grid.EventOverSizeLimit
	case adaptive_grid.GridStateDefensive:
		event = adaptive_grid.EventExtremeVolatility
	case adaptive_grid.GridStateRecovery:
		event = adaptive_grid.EventRecoveryStart
	case adaptive_grid.GridStateExitHalf:
		event = adaptive_grid.EventPartialLoss
	case adaptive_grid.GridStateExitAll:
		event = adaptive_grid.EventEmergencyExit
	case adaptive_grid.GridStateTrading:
		// For TRADING, need to determine which event based on current state
		switch currentState {
		case adaptive_grid.GridStateOverSize:
			event = adaptive_grid.EventSizeNormalized
		case adaptive_grid.GridStateDefensive:
			event = adaptive_grid.EventVolatilityNormalized
		case adaptive_grid.GridStateRecovery:
			event = adaptive_grid.EventRecoveryComplete
		case adaptive_grid.GridStateExitHalf:
			event = adaptive_grid.EventRecovery
		default:
			g.logger.WithFields(logrus.Fields{
				"symbol":        symbol,
				"current_state": currentState.String(),
				"recommended":   recommendation.State.String(),
			}).Warn("Market Condition: Cannot transition to TRADING from current state")
			return
		}
	default:
		g.logger.WithFields(logrus.Fields{
			"symbol":      symbol,
			"recommended": recommendation.State.String(),
		}).Warn("Market Condition: Unsupported recommended state")
		return
	}

	// Check if transition is valid
	if !g.adaptiveMgr.GetStateMachine().CanTransition(symbol, event) {
		g.logger.WithFields(logrus.Fields{
			"symbol":        symbol,
			"current_state": currentState.String(),
			"event":         event.String(),
			"recommended":   recommendation.State.String(),
		}).Warn("Market Condition: Invalid transition")
		return
	}

	// Perform transition
	g.logger.WithFields(logrus.Fields{
		"symbol":     symbol,
		"from":       currentState.String(),
		"to":         recommendation.State.String(),
		"event":      event.String(),
		"confidence": recommendation.Confidence,
		"reason":     recommendation.Reason,
	}).Info("Market Condition: Triggering state transition")

	g.adaptiveMgr.GetStateMachine().Transition(symbol, event)

	// Execute state-specific actions
	switch recommendation.State {
	case adaptive_grid.GridStateExitHalf:
		positions := g.wsClient.GetCachedPositions()
		if position, hasPos := positions[symbol]; hasPos {
			g.triggerExitHalf(symbol, position, position.UnrealizedProfit)
		}
	case adaptive_grid.GridStateExitAll:
		g.executeExitAll(symbol, "Market condition recommendation")
	}
}

// triggerExitHalf executes partial exit: cut 50% position, cancel opposite side grid
func (g *GridManager) triggerExitHalf(symbol string, position client.Position, unrealizedPnL float64) {
	ctx := context.Background()

	// Transition state
	g.adaptiveMgr.GetStateMachine().Transition(symbol, adaptive_grid.EventPartialLoss)

	// Calculate close amount from config (default 50%)
	closePct := 0.5
	if g.pnlRiskConfig != nil && g.pnlRiskConfig.PartialClosePct > 0 {
		closePct = g.pnlRiskConfig.PartialClosePct
	}
	closeAmt := math.Abs(position.PositionAmt) * closePct
	closeSide := "SELL"
	if position.PositionSide == "SHORT" {
		closeSide = "BUY"
	}

	// Place market order to close 50%
	orderReq := client.PlaceOrderRequest{
		Symbol:       symbol,
		Side:         closeSide,
		Type:         "MARKET",
		Quantity:     fmt.Sprintf("%.6f", closeAmt),
		PositionSide: position.PositionSide,
		ReduceOnly:   true,
	}

	order, err := g.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		g.logger.WithFields(logrus.Fields{
			"symbol": symbol,
			"error":  err,
		}).Error("Failed to place partial close order for EXIT_HALF")
		return
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":    symbol,
		"order_id":  order.OrderID,
		"close_amt": closeAmt,
	}).Info("EXIT_HALF: Partial close order placed")

	// Cancel opposite side grid orders (keep TP/SL and reduce-only orders)
	g.cancelOppositeSideGridOrders(ctx, symbol, position.PositionSide)
}

// executeExitAll executes full exit: cancel all orders, close position
func (g *GridManager) executeExitAll(symbol, reason string) {
	ctx := context.Background()

	g.logger.WithFields(logrus.Fields{
		"symbol": symbol,
		"reason": reason,
	}).Warn("Executing EXIT_ALL")

	// Cancel all orders
	if err := g.CancelAllOrders(ctx, symbol); err != nil {
		g.logger.WithFields(logrus.Fields{
			"symbol": symbol,
			"error":  err,
		}).Error("Failed to cancel orders during EXIT_ALL")
	}

	// Close position
	positions := g.wsClient.GetCachedPositions()
	position, hasPosition := positions[symbol]
	if hasPosition && position.PositionAmt != 0 {
		closeAmt := math.Abs(position.PositionAmt)
		closeSide := "SELL"
		if position.PositionSide == "SHORT" {
			closeSide = "BUY"
		}

		orderReq := client.PlaceOrderRequest{
			Symbol:       symbol,
			Side:         closeSide,
			Type:         "MARKET",
			Quantity:     fmt.Sprintf("%.6f", closeAmt),
			PositionSide: position.PositionSide,
			ReduceOnly:   true,
		}

		_, err := g.futuresClient.PlaceOrder(ctx, orderReq)
		if err != nil {
			g.logger.WithFields(logrus.Fields{
				"symbol": symbol,
				"error":  err,
			}).Error("Failed to close position during EXIT_ALL")
		}
	}

	// Clear grid
	if err := g.ClearGrid(ctx, symbol); err != nil {
		g.logger.WithFields(logrus.Fields{
			"symbol": symbol,
			"error":  err,
		}).Error("Failed to clear grid during EXIT_ALL")
	}
}

// cancelOppositeSideGridOrders cancels grid orders on the opposite side, keeps TP/SL and reduce-only
func (g *GridManager) cancelOppositeSideGridOrders(ctx context.Context, symbol, positionSide string) {
	orders, err := g.futuresClient.GetOpenOrders(ctx, symbol)
	if err != nil {
		g.logger.WithFields(logrus.Fields{
			"symbol": symbol,
			"error":  err,
		}).Error("Failed to get open orders for opposite side cancellation")
		return
	}

	cancelledCount := 0
	for _, order := range orders {
		// Skip reduce-only orders (TP/SL)
		if order.ReduceOnly {
			continue
		}

		// Determine if this order is on the opposite side
		orderSide := order.Side
		shouldCancel := false

		if positionSide == "LONG" && orderSide == "BUY" {
			// LONG position: cancel BUY orders (would increase position)
			shouldCancel = true
		} else if positionSide == "SHORT" && orderSide == "SELL" {
			// SHORT position: cancel SELL orders (would increase position)
			shouldCancel = true
		}

		if shouldCancel {
			cancelReq := client.CancelOrderRequest{
				Symbol:  symbol,
				OrderID: order.OrderID,
			}
			_, err := g.futuresClient.CancelOrder(ctx, cancelReq)
			if err != nil {
				g.logger.WithFields(logrus.Fields{
					"symbol":   symbol,
					"order_id": order.OrderID,
					"error":    err,
				}).Error("Failed to cancel opposite side order")
			} else {
				cancelledCount++
			}
		}
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":          symbol,
		"cancelled_count": cancelledCount,
	}).Info("Cancelled opposite side grid orders")
}

// warmupCompletionChecker monitors a single symbol's warm-up progress
// and triggers placement when range becomes active
func (g *GridManager) warmupCompletionChecker(symbol string) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Warmup completion checker goroutine panic recovered",
				zap.String("symbol", symbol),
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	g.logger.WithField("symbol", symbol).Info("📊 Warm-up completion checker started")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case <-g.stopCh:
			g.logger.WithField("symbol", symbol).Info("Warm-up checker stopped")
			return

		case <-g.warmupStopCh:
			g.logger.WithField("symbol", symbol).Info("Warm-up checker stopped (global)")
			return

		case <-timeout.C:
			g.logger.WithField("symbol", symbol).Warn("⏰ Warm-up TIMEOUT after 5 minutes")
			g.warmupMu.Lock()
			delete(g.warmupActive, symbol)
			g.warmupMu.Unlock()
			return

		case <-ticker.C:
			// Check if still in warm-up
			g.warmupMu.RLock()
			inWarmup := g.warmupActive[symbol]
			g.warmupMu.RUnlock()

			if !inWarmup {
				// Warm-up was cancelled or completed by another path
				return
			}

			// Check range state
			rangeState := g.adaptiveMgr.GetRangeState(symbol)
			g.logger.WithFields(logrus.Fields{
				"symbol":      symbol,
				"range_state": rangeState,
				"expected":    adaptive_grid.RangeStateActive,
			}).Info("Warm-up checking range state")

			// VOLUME FARMING: Skip warm-up requirement - allow immediate trading
			// AdaptiveGridManager.CanPlaceOrder will handle MICRO mode with ATR bands
			if rangeState == adaptive_grid.RangeStateActive {
				g.logger.WithFields(logrus.Fields{
					"symbol":      symbol,
					"range_state": rangeState,
				}).Info("✅ Warm-up COMPLETE - RangeDetector ready")
			} else {
				g.logger.WithFields(logrus.Fields{
					"symbol":      symbol,
					"range_state": rangeState,
				}).Warn("⚡ Warm-up SKIPPED - Allowing immediate trading (MICRO mode with ATR bands)")
			}

			// Mark warm-up complete regardless of range state
			g.warmupMu.Lock()
			delete(g.warmupActive, symbol)
			g.warmupMu.Unlock()

			// Finalize and trigger placement
			g.finalizeWarmup(symbol)
			return
		}
	}
}

// finalizeWarmup completes the warmup phase and triggers order placement
func (g *GridManager) finalizeWarmup(symbol string) {
	// Mark warmup as complete
	g.warmupMu.Lock()
	delete(g.warmupActive, symbol)
	g.warmupMu.Unlock()

	// Unsubscribe from klines (optional - can keep for continuous BB updates)
	// g.wsClient.UnsubscribeFromKlines([]string{symbol}, "1m")

	// Get current price from range detector
	rangeInfo := g.adaptiveMgr.GetRangeInfo(symbol)
	currentPrice, _ := rangeInfo["last_price"].(float64)
	if currentPrice == 0 {
		g.logger.WithField("symbol", symbol).Warn("Warm-up complete but no valid price, waiting for ticker")
		return
	}

	// Update grid price
	g.gridsMu.Lock()
	if grid, exists := g.activeGrids[symbol]; exists {
		grid.CurrentPrice = currentPrice
		grid.GridCenterPrice = currentPrice
	}
	g.gridsMu.Unlock()

	g.logger.WithFields(logrus.Fields{
		"symbol":    symbol,
		"price":     currentPrice,
		"next_step": "enqueue_placement",
	}).Info("🎯 Warm-up finalized - triggering placement")

	// Trigger placement
	g.enqueuePlacement(symbol)
}

// placeGridOrders places initial grid orders for a symbol concurrently.
// For rebuilds, it only places orders at levels that don't have active orders.
func (g *GridManager) placeGridOrders(ctx context.Context, symbol string, grid *SymbolGrid) int {
	g.logger.WithField("symbol", symbol).Info("Placing grid orders for volume farming (concurrent)")

	// CRITICAL: Micro grid takes PRECEDENCE over BB bands
	// BB/ADX gates permission to trade, micro grid determines order geometry
	// This ensures consistent 0.05% spread + 5 orders/side as per spec
	if g.adaptiveMgr != nil && g.adaptiveMgr.IsMicroGridEnabled() {
		return g.placeMicroGridOrders(ctx, symbol, grid)
	}

	// FALLBACK: Use BB range-based grid placement if micro grid disabled
	// BB bands are used for geometry only when micro grid is not available
	if g.adaptiveMgr != nil {
		upper, lower, mid, valid := g.adaptiveMgr.GetBBRangeBands(symbol)
		if valid && upper > 0 && lower > 0 && mid > 0 {
			return g.placeBBGridOrders(ctx, symbol, grid, upper, lower, mid)
		}
	}

	if grid.CurrentPrice == 0 {
		g.logger.WithField("symbol", symbol).Error("Cannot place orders: current price is 0")
		return 0
	}

	// Get levels that already have active orders (for rebuild scenario)
	existingBuyLevels, existingSellLevels := g.getActiveGridLevels(symbol)
	g.logger.WithFields(logrus.Fields{
		"symbol":               symbol,
		"existing_buy_levels":  len(existingBuyLevels),
		"existing_sell_levels": len(existingSellLevels),
	}).Info("Checked existing grid levels")

	// NEW: Get dynamic spread from AdaptiveGridGeometry if available
	spreadPct := grid.GridSpreadPct
	adaptiveSpread, adaptiveOrderCount, useAdaptive := g.CalculateAdaptiveGeometry(symbol, grid.CurrentPrice)
	if useAdaptive && adaptiveSpread > 0 {
		// For volume farming, use adaptive spread if it's reasonable
		// Prefer tighter spreads for volume farming
		if adaptiveSpread < spreadPct {
			spreadPct = adaptiveSpread
			g.logger.WithFields(logrus.Fields{
				"symbol":               symbol,
				"adaptive_spread":      adaptiveSpread,
				"base_spread":          grid.GridSpreadPct,
				"final_spread":         spreadPct,
				"adaptive_order_count": adaptiveOrderCount,
			}).Info("Using adaptive spread for grid")
		} else {
			g.logger.WithFields(logrus.Fields{
				"symbol":               symbol,
				"adaptive_spread":      adaptiveSpread,
				"base_spread":          grid.GridSpreadPct,
				"final_spread":         spreadPct,
				"adaptive_order_count": adaptiveOrderCount,
			}).Info("Adaptive spread wider than config - using config spread")
		}

		// Adjust order count if adaptive geometry suggests different count
		if adaptiveOrderCount > 0 && adaptiveOrderCount != grid.MaxOrdersSide {
			// For safety, don't exceed configured max by too much
			if adaptiveOrderCount > grid.MaxOrdersSide*2 {
				adaptiveOrderCount = grid.MaxOrdersSide * 2
			}
			if adaptiveOrderCount < 2 {
				adaptiveOrderCount = 2
			}
			grid.MaxOrdersSide = adaptiveOrderCount
			g.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"adaptive_count": adaptiveOrderCount,
				"base_count":     grid.MaxOrdersSide,
			}).Info("Adjusted order count based on adaptive geometry")
		}
	}

	// For volume farming, use ultra-small spreads for maximum fills
	spreadAmount := grid.CurrentPrice * (spreadPct / 100)

	// NEW: Apply fluid flow spread multiplier for "soft like water" behavior
	g.flowParamsMu.RLock()
	if flowParams, hasFlow := g.flowParameters[symbol]; hasFlow {
		spreadAmount *= flowParams.SpreadMultiplier
		g.logger.WithFields(logrus.Fields{
			"symbol":            symbol,
			"flow_intensity":    flowParams.Intensity,
			"spread_multiplier": flowParams.SpreadMultiplier,
		}).Debug("Applied fluid flow spread multiplier")
	}
	g.flowParamsMu.RUnlock()

	// NEW: Apply state-specific spread multiplier
	stateSpreadMultiplier, _ := g.GetStateParameters(symbol)
	if stateSpreadMultiplier != 1.0 {
		originalSpread := spreadAmount
		spreadAmount *= stateSpreadMultiplier
		g.logger.WithFields(logrus.Fields{
			"symbol":                  symbol,
			"state_spread_multiplier": stateSpreadMultiplier,
			"original_spread":         originalSpread,
			"adjusted_spread":         spreadAmount,
		}).Info("Applied state-specific spread multiplier")
	}

	// Ensure minimum spread amount for very low prices or tiny percentages
	minSpreadAmount := grid.CurrentPrice * 0.0001 // 0.01% minimum
	if spreadAmount < minSpreadAmount {
		spreadAmount = minSpreadAmount
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":        symbol,
		"current_price": grid.CurrentPrice,
		"spread_pct":    spreadPct,
		"spread_amount": spreadAmount,
		"min_spread":    minSpreadAmount,
	}).Info("Calculated grid spread for volume farming")

	// Collect all orders to place (only for levels without existing orders)
	var orders []*GridOrder

	// Track how many orders we're skipping vs creating
	skippedBuyLevels := 0
	skippedSellLevels := 0

	g.logger.WithFields(logrus.Fields{
		"symbol":          symbol,
		"max_orders_side": grid.MaxOrdersSide,
		"current_price":   grid.CurrentPrice,
		"spread_pct":      spreadPct,
		"spread_amount":   spreadAmount,
	}).Info("Starting to build grid orders (smart rebuild)")

	// Place BUY orders below current price (skip levels that already have orders)
	buyOrdersCreated := 0
	for i := 1; i <= grid.MaxOrdersSide; i++ {
		gridLevel := -i

		// Skip if this level already has an active order
		if existingBuyLevels[gridLevel] {
			skippedBuyLevels++
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": gridLevel,
				"side":       "BUY",
			}).Debug("Skipping BUY order - level already has active order")
			continue
		}

		buyPrice := grid.CurrentPrice - (spreadAmount * float64(i))

		// Apply tick-size rounding if TickSizeManager is available
		if g.tickSizeMgr != nil {
			if tsm, ok := g.tickSizeMgr.(interface {
				RoundToTickForSymbol(symbol string, price float64) float64
			}); ok {
				originalPrice := buyPrice
				buyPrice = tsm.RoundToTickForSymbol(symbol, buyPrice)
				if originalPrice != buyPrice {
					g.logger.WithFields(logrus.Fields{
						"symbol":         symbol,
						"side":           "BUY",
						"original_price": originalPrice,
						"rounded_price":  buyPrice,
						"grid_level":     -i,
					}).Debug("Price rounded to valid tick size")
				}
			}
		}

		// Apply penny jumping if PennyJumpManager is available
		if g.pennyJumpMgr != nil {
			if pjm, ok := g.pennyJumpMgr.(interface {
				GetPennyJumpedPrice(symbol, side string, price float64) float64
			}); ok {
				originalPrice := buyPrice
				buyPrice = pjm.GetPennyJumpedPrice(symbol, "BUY", buyPrice)
				if originalPrice != buyPrice {
					g.logger.WithFields(logrus.Fields{
						"symbol":         symbol,
						"side":           "BUY",
						"original_price": originalPrice,
						"jumped_price":   buyPrice,
						"grid_level":     -i,
					}).Info("Penny jump applied to BUY order")
				}
			}
		}

		if buyPrice <= 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": i,
				"buy_price":  buyPrice,
			}).Warn("Skipping BUY order: price <= 0")
			continue
		}
		orderSize := g.baseNotionalUSD / buyPrice

		// NEW: Apply dynamic size calculator if available
		g.dynamicSizeCalculatorMu.RLock()
		if g.dynamicSizeCalculator != nil {
			baseNotional := g.baseNotionalUSD
			dynamicSize := g.dynamicSizeCalculator(symbol, baseNotional, buyPrice)
			if dynamicSize > 0 {
				orderSize = dynamicSize / buyPrice
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"grid_level":    i,
					"side":          "BUY",
					"base_notional": baseNotional,
					"dynamic_size":  dynamicSize,
					"final_size":    orderSize,
				}).Info("Dynamic size calculator applied to BUY order")
			}
		}
		g.dynamicSizeCalculatorMu.RUnlock()

		// NEW: Apply time-based size multiplier from TimeFilter
		if g.adaptiveMgr != nil {
			sizeMultiplier := g.adaptiveMgr.GetTimeBasedSizeMultiplier()
			if sizeMultiplier > 0 && sizeMultiplier != 1.0 {
				adjustedSize := orderSize * sizeMultiplier
				currentSlot := g.adaptiveMgr.GetCurrentSlot()
				slotDesc := "unknown"
				if currentSlot != nil {
					slotDesc = currentSlot.Description
				}
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"grid_level":    i,
					"side":          "BUY",
					"original_size": orderSize,
					"multiplier":    sizeMultiplier,
					"adjusted_size": adjustedSize,
					"time_slot":     slotDesc,
				}).Debug("Order size adjusted by time filter")
				orderSize = adjustedSize
			}
		}

		// NEW: Apply inventory-adjusted sizing if AdaptiveGridManager available
		if g.adaptiveMgr != nil {
			adjustedSize := g.adaptiveMgr.GetInventoryAdjustedSize(symbol, "LONG", orderSize)
			if adjustedSize <= 0 {
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"original_size": orderSize,
					"adjusted_size": adjustedSize,
					"side":          "BUY",
					"grid_level":    i,
				}).Warn("BUY order adjusted to 0, using original size")
				adjustedSize = orderSize // Use original if adjusted is 0
			}
			if adjustedSize != orderSize {
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"original_size": orderSize,
					"adjusted_size": adjustedSize,
					"side":          "BUY",
				}).Info("Order size adjusted for inventory skew")
			}
			orderSize = adjustedSize
		}

		// NEW: Apply equity-based sizing if enabled
		if g.equitySizingConfig != nil && g.equitySizingConfig.Enabled {
			baseNotional := orderSize * buyPrice
			equityBasedNotional := g.CalculateEquityBasedSize(baseNotional)
			equityBasedSize := equityBasedNotional / buyPrice
			if equityBasedSize != orderSize {
				g.logger.WithFields(logrus.Fields{
					"symbol":          symbol,
					"grid_level":      i,
					"side":            "BUY",
					"base_notional":   baseNotional,
					"equity_notional": equityBasedNotional,
					"original_size":   orderSize,
					"equity_size":     equityBasedSize,
				}).Info("Order size adjusted by equity curve")
				orderSize = equityBasedSize
			}
		}

		// NEW: Apply state-specific size multiplier
		_, stateSizeMultiplier := g.GetStateParameters(symbol)
		if stateSizeMultiplier != 1.0 {
			originalSize := orderSize
			orderSize *= stateSizeMultiplier
			g.logger.WithFields(logrus.Fields{
				"symbol":                symbol,
				"grid_level":            i,
				"side":                  "BUY",
				"state_size_multiplier": stateSizeMultiplier,
				"original_size":         originalSize,
				"adjusted_size":         orderSize,
			}).Info("Order size adjusted by state-specific multiplier")
		}

		// NEW: Apply fluid flow size multiplier for "soft like water" behavior
		g.flowParamsMu.RLock()
		if flowParams, hasFlow := g.flowParameters[symbol]; hasFlow {
			orderSize *= flowParams.SizeMultiplier
			g.logger.WithFields(logrus.Fields{
				"symbol":          symbol,
				"flow_intensity":  flowParams.Intensity,
				"size_multiplier": flowParams.SizeMultiplier,
			}).Debug("Applied fluid flow size multiplier to BUY order")
		}
		g.flowParamsMu.RUnlock()

		if orderSize <= 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": i,
				"buy_price":  buyPrice,
			}).Warn("Skipping BUY order due to zero/negative size")
			continue
		}

		finalSize := g.calculateOrderSize(symbol, orderSize, buyPrice)
		if finalSize <= 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": -i,
				"side":       "BUY",
				"price":      buyPrice,
				"reason":     "size_calculation_failed",
			}).Warn("Skipping BUY order: calculateOrderSize returned 0")
			continue
		}
		order := &GridOrder{
			Symbol:    symbol,
			Side:      "BUY",
			Size:      finalSize,
			Price:     buyPrice,
			OrderType: "LIMIT",
			Status:    "NEW",
			CreatedAt: time.Now(),
			GridLevel: -i,
		}
		orders = append(orders, order)
		buyOrdersCreated++
	}

	// Place SELL orders above current price (skip levels that already have orders)
	sellOrdersCreated := 0
	for i := 1; i <= grid.MaxOrdersSide; i++ {
		gridLevel := i

		// Skip if this level already has an active order
		if existingSellLevels[gridLevel] {
			skippedSellLevels++
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": gridLevel,
				"side":       "SELL",
			}).Debug("Skipping SELL order - level already has active order")
			continue
		}

		sellPrice := grid.CurrentPrice + (spreadAmount * float64(i))

		// Apply tick-size rounding if TickSizeManager is available
		if g.tickSizeMgr != nil {
			if tsm, ok := g.tickSizeMgr.(interface {
				RoundToTickForSymbol(symbol string, price float64) float64
			}); ok {
				originalPrice := sellPrice
				sellPrice = tsm.RoundToTickForSymbol(symbol, sellPrice)
				if originalPrice != sellPrice {
					g.logger.WithFields(logrus.Fields{
						"symbol":         symbol,
						"side":           "SELL",
						"original_price": originalPrice,
						"rounded_price":  sellPrice,
						"grid_level":     i,
					}).Debug("Price rounded to valid tick size")
				}
			}
		}

		// Apply penny jumping if PennyJumpManager is available
		if g.pennyJumpMgr != nil {
			if pjm, ok := g.pennyJumpMgr.(interface {
				GetPennyJumpedPrice(symbol, side string, price float64) float64
			}); ok {
				originalPrice := sellPrice
				sellPrice = pjm.GetPennyJumpedPrice(symbol, "SELL", sellPrice)
				if originalPrice != sellPrice {
					g.logger.WithFields(logrus.Fields{
						"symbol":         symbol,
						"side":           "SELL",
						"original_price": originalPrice,
						"jumped_price":   sellPrice,
						"grid_level":     i,
					}).Info("Penny jump applied to SELL order")
				}
			}
		}

		orderSize := g.baseNotionalUSD / sellPrice

		// NEW: Apply dynamic size calculator if available
		g.dynamicSizeCalculatorMu.RLock()
		if g.dynamicSizeCalculator != nil {
			baseNotional := g.baseNotionalUSD
			dynamicSize := g.dynamicSizeCalculator(symbol, baseNotional, sellPrice)
			if dynamicSize > 0 {
				orderSize = dynamicSize / sellPrice
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"grid_level":    i,
					"side":          "SELL",
					"base_notional": baseNotional,
					"dynamic_size":  dynamicSize,
					"final_size":    orderSize,
				}).Info("Dynamic size calculator applied to SELL order")
			}
		}
		g.dynamicSizeCalculatorMu.RUnlock()

		// NEW: Apply time-based size multiplier from TimeFilter
		if g.adaptiveMgr != nil {
			sizeMultiplier := g.adaptiveMgr.GetTimeBasedSizeMultiplier()
			if sizeMultiplier > 0 && sizeMultiplier != 1.0 {
				adjustedSize := orderSize * sizeMultiplier
				currentSlot := g.adaptiveMgr.GetCurrentSlot()
				slotDesc := "unknown"
				if currentSlot != nil {
					slotDesc = currentSlot.Description
				}
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"grid_level":    i,
					"side":          "SELL",
					"original_size": orderSize,
					"multiplier":    sizeMultiplier,
					"adjusted_size": adjustedSize,
					"time_slot":     slotDesc,
				}).Debug("Order size adjusted by time filter")
				orderSize = adjustedSize
			}
		}

		if g.adaptiveMgr != nil {
			adjustedSize := g.adaptiveMgr.GetInventoryAdjustedSize(symbol, "SHORT", orderSize)
			if adjustedSize <= 0 {
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"original_size": orderSize,
					"adjusted_size": adjustedSize,
					"side":          "SELL",
					"grid_level":    i,
				}).Warn("SELL order adjusted to 0, using original size")
				adjustedSize = orderSize
			}
			if adjustedSize != orderSize {
				g.logger.WithFields(logrus.Fields{
					"symbol":        symbol,
					"original_size": orderSize,
					"adjusted_size": adjustedSize,
					"side":          "SELL",
				}).Info("Order size adjusted for inventory skew")
			}
			orderSize = adjustedSize
		}

		// NEW: Apply equity-based sizing if enabled
		if g.equitySizingConfig != nil && g.equitySizingConfig.Enabled {
			baseNotional := orderSize * sellPrice
			equityBasedNotional := g.CalculateEquityBasedSize(baseNotional)
			equityBasedSize := equityBasedNotional / sellPrice
			if equityBasedSize != orderSize {
				g.logger.WithFields(logrus.Fields{
					"symbol":          symbol,
					"grid_level":      i,
					"side":            "SELL",
					"base_notional":   baseNotional,
					"equity_notional": equityBasedNotional,
					"original_size":   orderSize,
					"equity_size":     equityBasedSize,
				}).Info("Order size adjusted by equity curve")
				orderSize = equityBasedSize
			}
		}

		// NEW: Apply state-specific size multiplier
		_, stateSizeMultiplier := g.GetStateParameters(symbol)
		if stateSizeMultiplier != 1.0 {
			originalSize := orderSize
			orderSize *= stateSizeMultiplier
			g.logger.WithFields(logrus.Fields{
				"symbol":                symbol,
				"grid_level":            i,
				"side":                  "SELL",
				"state_size_multiplier": stateSizeMultiplier,
				"original_size":         originalSize,
				"adjusted_size":         orderSize,
			}).Info("Order size adjusted by state-specific multiplier")
		}

		// NEW: Apply fluid flow size multiplier for "soft like water" behavior
		g.flowParamsMu.RLock()
		if flowParams, hasFlow := g.flowParameters[symbol]; hasFlow {
			orderSize *= flowParams.SizeMultiplier
			g.logger.WithFields(logrus.Fields{
				"symbol":          symbol,
				"flow_intensity":  flowParams.Intensity,
				"size_multiplier": flowParams.SizeMultiplier,
			}).Debug("Applied fluid flow size multiplier to SELL order")
		}
		g.flowParamsMu.RUnlock()

		if orderSize <= 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": i,
				"sell_price": sellPrice,
			}).Warn("Skipping SELL order due to zero/negative size")
			continue
		}

		finalSize := g.calculateOrderSize(symbol, orderSize, sellPrice)
		if finalSize <= 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": i,
				"side":       "SELL",
				"price":      sellPrice,
				"reason":     "size_calculation_failed",
			}).Warn("Skipping SELL order: calculateOrderSize returned 0")
			continue
		}
		order := &GridOrder{
			Symbol:    symbol,
			Side:      "SELL",
			Size:      finalSize,
			Price:     sellPrice,
			OrderType: "LIMIT",
			Status:    "NEW",
			CreatedAt: time.Now(),
			GridLevel: i,
		}
		orders = append(orders, order)
		sellOrdersCreated++
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":              symbol,
		"buy_orders_created":  buyOrdersCreated,
		"sell_orders_created": sellOrdersCreated,
		"buy_orders_skipped":  skippedBuyLevels,
		"sell_orders_skipped": skippedSellLevels,
		"total_orders":        len(orders),
	}).Info("Grid orders prepared for placement (smart rebuild)")

	// Place all orders concurrently
	var wg sync.WaitGroup
	successChan := make(chan bool, len(orders))

	for _, order := range orders {
		wg.Add(1)
		go g.placeOrderAsync(ctx, order, &wg, successChan)
	}

	// Wait for all orders to complete in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				g.logger.Error("WaitGroup goroutine panic recovered",
					zap.String("symbol", symbol),
					zap.Any("panic", r))
				close(successChan)
			}
		}()
		wg.Wait()
		close(successChan)
	}()

	// Count successes
	placedOrders := 0
	for success := range successChan {
		if success {
			placedOrders++
		}
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":       symbol,
		"buy_orders":   grid.MaxOrdersSide,
		"sell_orders":  grid.MaxOrdersSide,
		"total_orders": placedOrders,
		"spread_pct":   grid.GridSpreadPct,
	}).Info("Grid orders placed for volume farming (concurrent)")

	return placedOrders
}

// placeBBGridOrders places grid orders at Bollinger Bands levels
// Buy orders are placed from lower band up to mid, sell orders from mid up to upper band
func (g *GridManager) placeBBGridOrders(ctx context.Context, symbol string, grid *SymbolGrid, upper, lower, mid float64) int {
	g.logger.WithFields(logrus.Fields{
		"symbol":      symbol,
		"bb_upper":    upper,
		"bb_lower":    lower,
		"bb_mid":      mid,
		"range_width": upper - lower,
	}).Info("Placing BB range-based grid orders")

	// Get levels that already have active orders
	existingBuyLevels, existingSellLevels := g.getActiveGridLevels(symbol)

	// Calculate grid levels within BB range
	// Buy side: from lower band up to mid
	// Sell side: from mid up to upper band
	buyRange := mid - lower
	sellRange := upper - mid

	// Divide range into levels
	numLevels := float64(grid.MaxOrdersSide)
	buySpacing := buyRange / numLevels
	sellSpacing := sellRange / numLevels

	var orders []*GridOrder

	// Place BUY orders from lower band up to mid
	for i := 0; i < grid.MaxOrdersSide; i++ {
		gridLevel := -i - 1
		if existingBuyLevels[gridLevel] {
			continue
		}

		// Price increases as we go up from lower band
		buyPrice := lower + (buySpacing * float64(i))

		// Apply tick-size rounding if TickSizeManager is available
		if g.tickSizeMgr != nil {
			if tsm, ok := g.tickSizeMgr.(interface {
				RoundToTickForSymbol(symbol string, price float64) float64
			}); ok {
				originalPrice := buyPrice
				buyPrice = tsm.RoundToTickForSymbol(symbol, buyPrice)
				if originalPrice != buyPrice {
					g.logger.WithFields(logrus.Fields{
						"symbol":         symbol,
						"side":           "BUY",
						"original_price": originalPrice,
						"rounded_price":  buyPrice,
						"grid_level":     -i - 1,
					}).Debug("BB grid price rounded to valid tick size")
				}
			}
		}

		// Apply penny jumping if PennyJumpManager is available
		if g.pennyJumpMgr != nil {
			if pjm, ok := g.pennyJumpMgr.(interface {
				GetPennyJumpedPrice(symbol, side string, price float64) float64
			}); ok {
				originalPrice := buyPrice
				buyPrice = pjm.GetPennyJumpedPrice(symbol, "BUY", buyPrice)
				if originalPrice != buyPrice {
					g.logger.WithFields(logrus.Fields{
						"symbol":         symbol,
						"side":           "BUY",
						"original_price": originalPrice,
						"jumped_price":   buyPrice,
						"grid_level":     -i - 1,
					}).Info("Penny jump applied to BB BUY order")
				}
			}
		}

		if buyPrice <= 0 || buyPrice >= mid {
			continue
		}

		orderSize := g.baseNotionalUSD / buyPrice

		// NEW: Apply time-based size multiplier from TimeFilter
		if g.adaptiveMgr != nil {
			sizeMultiplier := g.adaptiveMgr.GetTimeBasedSizeMultiplier()
			if sizeMultiplier > 0 && sizeMultiplier != 1.0 {
				orderSize = orderSize * sizeMultiplier
			}
		}

		if g.adaptiveMgr != nil {
			orderSize = g.adaptiveMgr.GetInventoryAdjustedSize(symbol, "LONG", orderSize)
		}

		finalSize := g.calculateOrderSize(symbol, orderSize, buyPrice)
		if finalSize <= 0 {
			continue
		}
		orders = append(orders, &GridOrder{
			Symbol:    symbol,
			Side:      "BUY",
			Size:      finalSize,
			Price:     buyPrice,
			OrderType: "LIMIT",
			Status:    "NEW",
			CreatedAt: time.Now(),
			GridLevel: gridLevel,
		})
	}

	// Place SELL orders from mid up to upper band
	for i := 0; i < grid.MaxOrdersSide; i++ {
		gridLevel := i + 1
		if existingSellLevels[gridLevel] {
			continue
		}

		// Price increases as we go up from mid to upper band
		sellPrice := mid + (sellSpacing * float64(i+1))

		// Apply tick-size rounding if TickSizeManager is available
		if g.tickSizeMgr != nil {
			if tsm, ok := g.tickSizeMgr.(interface {
				RoundToTickForSymbol(symbol string, price float64) float64
			}); ok {
				originalPrice := sellPrice
				sellPrice = tsm.RoundToTickForSymbol(symbol, sellPrice)
				if originalPrice != sellPrice {
					g.logger.WithFields(logrus.Fields{
						"symbol":         symbol,
						"side":           "SELL",
						"original_price": originalPrice,
						"rounded_price":  sellPrice,
						"grid_level":     i + 1,
					}).Debug("BB grid price rounded to valid tick size")
				}
			}
		}

		// Apply penny jumping if PennyJumpManager is available
		if g.pennyJumpMgr != nil {
			if pjm, ok := g.pennyJumpMgr.(interface {
				GetPennyJumpedPrice(symbol, side string, price float64) float64
			}); ok {
				originalPrice := sellPrice
				sellPrice = pjm.GetPennyJumpedPrice(symbol, "SELL", sellPrice)
				if originalPrice != sellPrice {
					g.logger.WithFields(logrus.Fields{
						"symbol":         symbol,
						"side":           "SELL",
						"original_price": originalPrice,
						"jumped_price":   sellPrice,
						"grid_level":     i + 1,
					}).Info("Penny jump applied to BB SELL order")
				}
			}
		}

		if sellPrice <= mid || sellPrice > upper {
			continue
		}

		orderSize := g.baseNotionalUSD / sellPrice

		// NEW: Apply time-based size multiplier from TimeFilter
		if g.adaptiveMgr != nil {
			sizeMultiplier := g.adaptiveMgr.GetTimeBasedSizeMultiplier()
			if sizeMultiplier > 0 && sizeMultiplier != 1.0 {
				orderSize = orderSize * sizeMultiplier
			}
		}

		if g.adaptiveMgr != nil {
			orderSize = g.adaptiveMgr.GetInventoryAdjustedSize(symbol, "SHORT", orderSize)
		}

		finalSize := g.calculateOrderSize(symbol, orderSize, sellPrice)
		if finalSize <= 0 {
			continue
		}
		orders = append(orders, &GridOrder{
			Symbol:    symbol,
			Side:      "SELL",
			Size:      finalSize,
			Price:     sellPrice,
			OrderType: "LIMIT",
			Status:    "NEW",
			CreatedAt: time.Now(),
			GridLevel: gridLevel,
		})
	}

	// Place orders SEQUENTIALLY to avoid hitting notional limits
	// Changed from concurrent to sequential to prevent "max notional value" errors
	placedOrders := 0
	for _, order := range orders {
		// Add small delay between orders to avoid rate limits
		time.Sleep(100 * time.Millisecond)

		successChan := make(chan bool, 1)
		go func(o *GridOrder) {
			defer func() {
				if r := recover(); r != nil {
					g.logger.Error("Order placement goroutine panic recovered",
						zap.String("symbol", order.Symbol),
						zap.Any("panic", r))
					successChan <- false
				}
			}()
			err := g.placeOrder(o)
			successChan <- (err == nil)
		}(order)

		select {
		case success := <-successChan:
			if success {
				placedOrders++
			}
		case <-time.After(5 * time.Second):
			g.logger.Warn("Order placement timeout", zap.String("symbol", order.Symbol))
		}
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":       symbol,
		"total_orders": placedOrders,
		"bb_upper":     upper,
		"bb_lower":     lower,
		"bb_mid":       mid,
	}).Info("BB range-based grid orders placed")

	return placedOrders
}

// placeMicroGridOrders places grid orders using micro grid configuration
// Optimized for high-frequency trading with tight spreads (0.05%) and small sizes ($3)
func (g *GridManager) placeMicroGridOrders(ctx context.Context, symbol string, grid *SymbolGrid) int {
	g.logger.WithFields(logrus.Fields{
		"symbol": symbol,
		"mode":   "micro_grid",
	}).Info("Placing micro grid orders for volume farming")

	if grid.CurrentPrice == 0 {
		g.logger.WithField("symbol", symbol).Error("Cannot place micro grid orders: current price is 0")
		return 0
	}

	// Get micro grid calculator from adaptive manager
	if g.adaptiveMgr == nil {
		g.logger.WithField("symbol", symbol).Error("Adaptive manager not available for micro grid")
		return 0
	}

	// Get micro grid prices
	buyPrices, sellPrices := g.adaptiveMgr.GetMicroGridPrices(grid.CurrentPrice)
	if len(buyPrices) == 0 && len(sellPrices) == 0 {
		g.logger.WithField("symbol", symbol).Error("Micro grid returned no prices")
		return 0
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":        symbol,
		"current_price": grid.CurrentPrice,
		"buy_levels":    len(buyPrices),
		"sell_levels":   len(sellPrices),
		"buy_prices":    buyPrices,
		"sell_prices":   sellPrices,
	}).Info("Micro grid prices calculated")

	// Get order size from micro grid config
	orderSize := g.adaptiveMgr.GetMicroGridOrderSize(grid.CurrentPrice)

	// NEW: Apply dynamic size calculator if available
	g.dynamicSizeCalculatorMu.RLock()
	if g.dynamicSizeCalculator != nil {
		baseNotional := orderSize * grid.CurrentPrice // Convert back to notional
		dynamicSize := g.dynamicSizeCalculator(symbol, baseNotional, grid.CurrentPrice)
		if dynamicSize > 0 {
			orderSize = dynamicSize / grid.CurrentPrice
			g.logger.WithFields(logrus.Fields{
				"symbol":        symbol,
				"base_notional": baseNotional,
				"dynamic_size":  dynamicSize,
				"final_size":    orderSize,
			}).Info("Dynamic size calculator applied to micro grid orders")
		}
	}
	g.dynamicSizeCalculatorMu.RUnlock()

	var orders []*GridOrder
	orderID := time.Now().UnixNano()

	// Create BUY orders at micro grid prices
	for i, price := range buyPrices {
		if price <= 0 || price >= grid.CurrentPrice {
			continue // Skip invalid prices
		}

		gridOrder := &GridOrder{
			OrderID:    fmt.Sprintf("%s-micro-buy-%d-%d", symbol, orderID, i),
			Symbol:     symbol,
			Side:       "BUY",
			Price:      price,
			Size:       orderSize,
			OrderType:  "LIMIT",
			ReduceOnly: false,
			GridLevel:  -(i + 1),
			Status:     "PENDING",
			CreatedAt:  time.Now(),
		}
		orders = append(orders, gridOrder)
	}

	// Create SELL orders at micro grid prices
	for i, price := range sellPrices {
		if price <= 0 || price <= grid.CurrentPrice {
			continue // Skip invalid prices
		}

		gridOrder := &GridOrder{
			OrderID:    fmt.Sprintf("%s-micro-sell-%d-%d", symbol, orderID, i),
			Symbol:     symbol,
			Side:       "SELL",
			Price:      price,
			Size:       orderSize,
			OrderType:  "LIMIT",
			ReduceOnly: false,
			GridLevel:  i + 1,
			Status:     "PENDING",
			CreatedAt:  time.Now(),
		}
		orders = append(orders, gridOrder)
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":       symbol,
		"total_orders": len(orders),
		"order_size":   orderSize,
	}).Info("Micro grid orders prepared")

	// Place orders concurrently
	placedOrders := 0
	successChan := make(chan bool, len(orders))

	for _, order := range orders {
		go func(o *GridOrder) {
			defer func() {
				if r := recover(); r != nil {
					g.logger.Error("Micro grid order placement goroutine panic recovered",
						zap.String("symbol", order.Symbol),
						zap.Any("panic", r))
					successChan <- false
				}
			}()
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			g.placeOrderAsync(ctx, o, &sync.WaitGroup{}, successChan)
		}(order)
	}

	// Wait for results
	for i := 0; i < len(orders); i++ {
		select {
		case success := <-successChan:
			if success {
				placedOrders++
			}
		case <-time.After(5 * time.Second):
			g.logger.Warn("Micro grid order placement timeout", zap.String("symbol", symbol))
		}
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":       symbol,
		"total_orders": placedOrders,
		"mode":         "micro_grid",
		"spread_pct":   0.05,
		"order_size":   orderSize,
	}).Info("Micro grid orders placed")

	return placedOrders
}

// calculateOrderSize calculates the final order size with fallbacks
func (g *GridManager) calculateOrderSize(symbol string, orderSize, price float64) float64 {
	var finalSize float64
	var notional float64

	// Try symbol-specific precision first
	if g.precisionMgr != nil {
		roundedSize := g.precisionMgr.RoundQty(symbol, orderSize)
		parsedSize, parseErr := strconv.ParseFloat(roundedSize, 64)
		if parseErr == nil && parsedSize > 0 {
			notional = parsedSize * price
			// CRITICAL: Check if precision rounding inflated size beyond max notional
			// This can happen when min qty step is large (e.g., BTC 0.01)
			if notional > g.maxNotionalUSD {
				// Scale down to max allowed notional
				scaledSize := g.maxNotionalUSD / price
				roundedSize = g.precisionMgr.RoundQty(symbol, scaledSize)
				parsedSize, _ = strconv.ParseFloat(roundedSize, 64)
				if parsedSize > 0 {
					notional = parsedSize * price
				}
				// If still exceeds max (due to rounding up), skip this order
				if notional > g.maxNotionalUSD {
					g.logger.WithFields(logrus.Fields{
						"symbol":   symbol,
						"price":    price,
						"min_step": roundedSize,
						"reason":   "min_qty_step_too_large",
					}).Warn("Order size cannot satisfy max notional with symbol's min qty step, skipping")
					return 0 // Signal to skip this order
				}
			}
			finalSize = parsedSize
		}
	}

	// Fallback: Ensure minimum notional (5.0 USD with safety margin)
	if finalSize == 0 || notional < 5.0 {
		minRequired := 5.0 * 1.02 // 5.1 USD minimum
		minSize := minRequired / price
		if minSize > 0 {
			// Calculate precision based on price magnitude
			// BTC ~$100k needs precision=5 (0.00001), ETH ~$2k needs precision=4 (0.0001)
			var precision int
			switch {
			case price >= 50000: // BTC and very high-priced assets
				precision = 5 // 0.00001 BTC = $0.5 at $50k, $5 at $100k
			case price >= 10000:
				precision = 4 // 0.0001
			case price >= 1000:
				precision = 3 // 0.001
			case price >= 100:
				precision = 2 // 0.01
			case price >= 10:
				precision = 3 // 0.001
			case price >= 1:
				precision = 4 // 0.0001
			default:
				precision = 6 // 0.000001 for sub-$1 assets
			}

			multiplier := math.Pow(10, float64(precision))
			// CRITICAL: Use Ceil to round UP, ensuring notional >= 5.0
			roundedSize := math.Ceil(minSize*multiplier) / multiplier
			adjustedNotional := roundedSize * price

			// Log for debugging BTC precision
			if strings.Contains(symbol, "BTC") {
				g.logger.WithFields(logrus.Fields{
					"symbol":           symbol,
					"price":            price,
					"precision":        precision,
					"multiplier":       multiplier,
					"minSize":          minSize,
					"roundedSize":      roundedSize,
					"adjustedNotional": adjustedNotional,
				}).Debug("BTC precision calculation")
			}

			// Safety loop: ensure notional meets minimum
			for adjustedNotional < 5.0 {
				roundedSize += 1.0 / multiplier
				adjustedNotional = roundedSize * price
			}
			// CRITICAL: Ensure fallback size doesn't exceed max notional
			if adjustedNotional > g.maxNotionalUSD {
				scaledSize := g.maxNotionalUSD / price
				scaledRounded := math.Ceil(scaledSize*multiplier) / multiplier
				if scaledRounded > 0 && scaledRounded*price <= g.maxNotionalUSD {
					roundedSize = scaledRounded
					adjustedNotional = roundedSize * price
				} else {
					g.logger.WithFields(logrus.Fields{
						"symbol":    symbol,
						"price":     price,
						"precision": precision,
						"reason":    "precision_too_low_for_max_notional",
					}).Warn("Fallback size cannot satisfy max notional, skipping")
					return 0
				}
			}
			finalSize = roundedSize
		}
	}

	// FINAL GUARD: Ensure we never exceed max notional regardless of path taken
	if finalSize > 0 && finalSize*price > g.maxNotionalUSD {
		g.logger.WithFields(logrus.Fields{
			"symbol":       symbol,
			"price":        price,
			"final_size":   finalSize,
			"notional":     finalSize * price,
			"max_notional": g.maxNotionalUSD,
		}).Error("FINAL GUARD: Order size exceeds max notional, returning 0")
		return 0
	}

	// Ensure minimum reasonable size
	if finalSize < 0.000001 {
		finalSize = 0.000001
	}

	// CRITICAL: Re-apply precision manager rounding to ensure we meet symbol's step size
	// This prevents "Quantity less than zero" errors for high-priced symbols like BTC
	if g.precisionMgr != nil && finalSize > 0 {
		roundedSize := g.precisionMgr.RoundQty(symbol, finalSize)
		parsedSize, parseErr := strconv.ParseFloat(roundedSize, 64)
		if parseErr == nil && parsedSize > 0 {
			finalSize = parsedSize
		} else if roundedSize == "0" || roundedSize == "0.000" {
			// If precision manager rounds to zero, the order is too small for this symbol
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"price":      price,
				"final_size": finalSize,
				"rounded":    roundedSize,
				"reason":     "step_size_too_large_for_micro_order",
			}).Warn("Order size rounds to zero with symbol's step size, skipping")
			return 0
		}
	}

	return finalSize
}

// placeOrderAsync places an order asynchronously with context cancellation support
func (g *GridManager) placeOrderAsync(ctx context.Context, order *GridOrder, wg *sync.WaitGroup, successChan chan<- bool) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("placeOrderAsync goroutine panic recovered",
				zap.String("symbol", order.Symbol),
				zap.Any("panic", r))
			successChan <- false
		}
	}()
	defer wg.Done()

	g.logger.WithFields(logrus.Fields{
		"symbol":     order.Symbol,
		"side":       order.Side,
		"price":      order.Price,
		"size":       order.Size,
		"grid_level": order.GridLevel,
	}).Debug("placeOrderAsync started - concurrent order placement")

	// Check context before attempting
	select {
	case <-ctx.Done():
		g.logger.WithField("symbol", order.Symbol).Debug("Context cancelled before placing order")
		successChan <- false
		return
	case <-g.stopCh:
		g.logger.WithField("symbol", order.Symbol).Debug("Stop signal received before placing order")
		successChan <- false
		return
	default:
	}

	if err := g.placeOrder(order); err != nil {
		g.logger.WithError(err).WithFields(logrus.Fields{
			"symbol":     order.Symbol,
			"side":       order.Side,
			"price":      order.Price,
			"grid_level": order.GridLevel,
		}).Warn("Failed to place order async")
		successChan <- false
	} else {
		g.logger.WithFields(logrus.Fields{
			"symbol":     order.Symbol,
			"side":       order.Side,
			"grid_level": order.GridLevel,
		}).Debug("Order placed successfully in async worker")
		successChan <- true
	}
}
func (g *GridManager) placeOrder(order *GridOrder) error {
	g.logger.WithFields(logrus.Fields{
		"symbol":       order.Symbol,
		"side":         order.Side,
		"price":        order.Price,
		"size":         order.Size,
		"reduce_only":  order.ReduceOnly,
		"is_rebalance": order.IsRebalance,
	}).Info("Attempting to place order")

	// CRITICAL: Check max position size before placing order
	// Skip ONLY for rebalancing orders (both ReduceOnly AND IsRebalance must be true)
	notional := order.Size * order.Price
	shouldCheckNotional := !(order.ReduceOnly && order.IsRebalance)
	if shouldCheckNotional && notional > g.maxNotionalUSD {
		// Instead of rejecting, reduce order size to fit within max notional
		reducedSize := (g.maxNotionalUSD / order.Price) * 0.95 // 5% buffer
		if reducedSize < 0.001 {                               // Minimum order size
			g.logger.WithFields(logrus.Fields{
				"symbol":       order.Symbol,
				"side":         order.Side,
				"order_size":   order.Size,
				"notional":     notional,
				"max_notional": g.maxNotionalUSD,
				"reduced_size": reducedSize,
			}).Warn("ORDER SKIPPED: Reduced size below minimum for max notional limit")
			return fmt.Errorf("reduced size %.6f below minimum for max notional", reducedSize)
		}

		// Update order with reduced size
		oldSize := order.Size
		order.Size = reducedSize
		newNotional := order.Size * order.Price

		g.logger.WithFields(logrus.Fields{
			"symbol":       order.Symbol,
			"side":         order.Side,
			"old_size":     oldSize,
			"new_size":     order.Size,
			"old_notional": notional,
			"new_notional": newNotional,
			"max_notional": g.maxNotionalUSD,
		}).Info("ORDER SIZE REDUCED to fit max notional limit")
	}

	// CRITICAL: Check total position exposure before adding new order
	// Skip ONLY for rebalancing orders (both ReduceOnly AND IsRebalance must be true)
	shouldCheckExposure := !(order.ReduceOnly && order.IsRebalance)
	if shouldCheckExposure {
		currentExposure := g.calculateCurrentExposure(context.Background(), order.Symbol)
		newTotalExposure := currentExposure + notional
		// Use maxTotalNotionalUSD for global exposure limit (across all symbols)
		// Fallback to maxNotionalUSD * 2 if maxTotalNotionalUSD not set
		exposureLimit := g.maxTotalNotionalUSD
		if exposureLimit == 0 {
			exposureLimit = g.maxNotionalUSD * 2.0 // Fallback: 2x per-symbol limit
		}
		if newTotalExposure > exposureLimit {
			// Instead of rejecting, reduce order size to fit within limit
			maxAllowedNotional := exposureLimit - currentExposure
			if maxAllowedNotional <= 0 {
				g.logger.WithFields(logrus.Fields{
					"symbol":             order.Symbol,
					"current_exposure":   currentExposure,
					"exposure_limit":     exposureLimit,
					"max_total_notional": g.maxTotalNotionalUSD,
					"max_per_symbol":     g.maxNotionalUSD,
				}).Warn("ORDER SKIPPED: Exposure already at limit, cannot place any order")
				return fmt.Errorf("exposure at limit %.2f, cannot place order", currentExposure)
			}

			// Calculate reduced order size
			reducedSize := (maxAllowedNotional / order.Price) * 0.95 // 5% buffer
			if reducedSize < 0.001 {                                 // Minimum order size
				g.logger.WithFields(logrus.Fields{
					"symbol":           order.Symbol,
					"current_exposure": currentExposure,
					"exposure_limit":   exposureLimit,
					"max_allowed":      maxAllowedNotional,
					"reduced_size":     reducedSize,
				}).Warn("ORDER SKIPPED: Reduced size below minimum")
				return fmt.Errorf("reduced size %.6f below minimum", reducedSize)
			}

			// Update order with reduced size
			oldSize := order.Size
			order.Size = reducedSize
			newNotional := order.Size * order.Price

			g.logger.WithFields(logrus.Fields{
				"symbol":             order.Symbol,
				"side":               order.Side,
				"old_size":           oldSize,
				"new_size":           order.Size,
				"old_notional":       notional,
				"new_notional":       newNotional,
				"current_exposure":   currentExposure,
				"new_total_exposure": currentExposure + newNotional,
				"exposure_limit":     exposureLimit,
			}).Info("ORDER SIZE REDUCED to fit exposure limit")
		}
	}

	// NEW: Check if trading is allowed by time filter - DISABLED for volume farming (trade 24/7)
	// if g.adaptiveMgr != nil && !g.adaptiveMgr.CanTrade() {
	// 	g.logger.WithFields(logrus.Fields{
	// 		"symbol": order.Symbol,
	// 		"side":   order.Side,
	// 	}).Warn("Order placement blocked - outside trading hours")
	// 	return fmt.Errorf("trading not allowed: outside configured trading hours")
	// }

	// NEW: Check state transition validity (only for existing orders)
	if order.OrderID != "" {
		fromState := adaptive_grid.OrderState(order.Status)
		toState := adaptive_grid.OrderStatePending
		if !g.stateValidator.IsValidTransition(fromState, toState) {
			g.logger.WithFields(logrus.Fields{
				"order_id": order.OrderID,
				"from":     order.Status,
				"to":       "PENDING",
			}).Warn("Invalid order state transition")
			return fmt.Errorf("invalid order state transition from %s to PENDING", order.Status)
		}
	}

	// NEW: Acquire per-symbol order lock - DISABLED for concurrent grid orders
	// The lock was preventing multiple orders for the same symbol from being placed concurrently
	// For volume farming, we need to place many orders at once, so we skip this lock
	// The deduplicator and stateValidator still provide protection against duplicate fills
	/*
		if !g.orderLockMgr.LockOrderProcessing(order.Symbol, order.OrderID) {
			return fmt.Errorf("failed to acquire order lock for symbol %s", order.Symbol)
		}
		defer g.orderLockMgr.UnlockOrderProcessing(order.Symbol)
	*/

	g.logger.WithFields(logrus.Fields{
		"symbol":     order.Symbol,
		"side":       order.Side,
		"price":      order.Price,
		"size":       order.Size,
		"grid_level": order.GridLevel,
	}).Debug("Lock bypassed for volume farming - placing order immediately")

	// Use adaptive rate limiter instead of hard block
	if !g.rateLimiter.WaitForToken(5 * time.Second) {
		return fmt.Errorf("rate limiter timeout: no tokens available")
	}

	g.logger.WithFields(logrus.Fields{
		"symbol": order.Symbol,
		"side":   order.Side,
		"size":   order.Size,
		"price":  order.Price,
	}).Info("Placing grid order")

	// Full PostOnlyHandler integration with retry logic
	if g.postOnlyHandler != nil {
		if poh, ok := g.postOnlyHandler.(interface {
			PlaceOrderWithPostOnly(ctx context.Context, symbol, side string, price, quantity float64, placeOrder func(ctx context.Context, symbol, side string, price, quantity float64, postOnly bool) error) error
		}); ok {
			// Create wrapper function for actual order placement
			placeOrderFunc := func(ctx context.Context, symbol, side string, price, quantity float64, postOnly bool) error {
				timeInForce := "GTC"
				if postOnly {
					timeInForce = "GTX"
				}

				orderReq := &client.PlaceOrderRequest{
					Symbol:      symbol,
					Side:        side,
					Type:        order.OrderType,
					Quantity:    g.precisionMgr.RoundQty(symbol, quantity),
					Price:       g.precisionMgr.RoundPrice(symbol, price),
					TimeInForce: timeInForce,
					ReduceOnly:  order.ReduceOnly,
				}

				// Verify notional after rounding
				qty, _ := strconv.ParseFloat(orderReq.Quantity, 64)
				priceStr, _ := strconv.ParseFloat(orderReq.Price, 64)
				notional := qty * priceStr

				// Only enforce min notional for non-reduce-only orders
				if notional < 5.0 && !order.ReduceOnly {
					minQty := 5.0 / priceStr
					minQty = minQty * 1.02
					orderReq.Quantity = g.precisionMgr.RoundQty(symbol, minQty)
				}

				response, err := g.futuresClient.PlaceOrder(ctx, *orderReq)
				if err != nil {
					return err
				}

				// Update order with response data
				order.OrderID = fmt.Sprintf("%d", response.OrderID)
				order.Status = response.Status

				// Track the order for fill monitoring
				g.ordersMu.Lock()
				g.activeOrders[order.OrderID] = order
				g.ordersMu.Unlock()

				// Notify sync worker about the placed order
				if g.onOrderPlaced != nil {
					orderIDInt, _ := strconv.ParseInt(order.OrderID, 10, 64)
					g.onOrderPlaced(order.Symbol, client.Order{
						OrderID: orderIDInt,
						Symbol:  order.Symbol,
						Side:    order.Side,
						Type:    order.OrderType,
						Price:   order.Price,
						OrigQty: order.Size,
						Status:  "NEW",
					})
				}

				return nil
			}

			// Use PostOnlyHandler with retry logic
			err := poh.PlaceOrderWithPostOnly(context.Background(), order.Symbol, order.Side, order.Price, order.Size, placeOrderFunc)
			if err != nil {
				g.handlePlaceOrderError(err)
				return fmt.Errorf("failed to place order with post-only handler: %w", err)
			}

			g.logger.WithFields(logrus.Fields{
				"symbol": order.Symbol,
				"side":   order.Side,
				"price":  order.Price,
				"size":   order.Size,
			}).Info("Order placed via PostOnlyHandler")
			return nil
		}
	}

	// Fallback: Simple placement without PostOnlyHandler
	timeInForce := "GTC"
	orderReq := &client.PlaceOrderRequest{
		Symbol:      order.Symbol,
		Side:        order.Side,
		Type:        order.OrderType,
		Quantity:    g.precisionMgr.RoundQty(order.Symbol, order.Size),
		Price:       g.precisionMgr.RoundPrice(order.Symbol, order.Price),
		TimeInForce: timeInForce,
		ReduceOnly:  order.ReduceOnly,
	}

	// Verify notional after rounding
	qty, _ := strconv.ParseFloat(orderReq.Quantity, 64)
	price, _ := strconv.ParseFloat(orderReq.Price, 64)
	notional = qty * price

	if notional < 5.0 && !order.ReduceOnly {
		minQty := 5.0 / price
		minQty = minQty * 1.02
		orderReq.Quantity = g.precisionMgr.RoundQty(order.Symbol, minQty)
	}

	response, err := g.futuresClient.PlaceOrder(context.Background(), *orderReq)
	if err != nil {
		g.handlePlaceOrderError(err)
		return fmt.Errorf("failed to place order: %w", err)
	}

	order.OrderID = fmt.Sprintf("%d", response.OrderID)
	order.Status = response.Status

	// Track the order for fill monitoring
	g.ordersMu.Lock()
	g.activeOrders[order.OrderID] = order
	g.ordersMu.Unlock()

	// Notify sync worker about the placed order
	if g.onOrderPlaced != nil {
		// Parse OrderID from string to int64
		orderIDInt, _ := strconv.ParseInt(order.OrderID, 10, 64)
		g.onOrderPlaced(order.Symbol, client.Order{
			OrderID: orderIDInt,
			Symbol:  order.Symbol,
			Side:    order.Side,
			Type:    order.OrderType,
			Price:   order.Price,
			OrigQty: order.Size,
			Status:  "NEW",
		})
	}

	// Log order placement
	if g.activityLog != nil {
		g.activityLog.Log(context.Background(), activitylog.EventOrderPlaced, activitylog.SeverityInfo,
			activitylog.EntryContext{Symbol: order.Symbol, StrategyName: "grid"},
			activitylog.OrderPlacedPayload{
				OrderID:       order.OrderID,
				ClientOrderID: "",
				Side:          order.Side,
				Type:          order.OrderType,
				Price:         order.Price,
				Quantity:      order.Size,
				TimeInForce:   "GTC",
				Reason:        fmt.Sprintf("grid_level_%d", order.GridLevel),
			},
		)
	}

	// Update volume metrics
	g.volumeMetricsMu.Lock()
	g.totalOrdersPlaced++
	g.totalVolumeUSDT += order.Size * order.Price
	g.volumeMetricsMu.Unlock()

	g.logger.WithFields(logrus.Fields{
		"symbol":  order.Symbol,
		"side":    order.Side,
		"orderID": order.OrderID,
		"status":  order.Status,
	}).Info("Grid order placed successfully")

	// Broadcast order placed metric to dashboard
	g.broadcastMetric("order_placed", order.Symbol, map[string]interface{}{
		"order_id":   order.OrderID,
		"side":       order.Side,
		"price":      order.Price,
		"size":       order.Size,
		"notional":   order.Size * order.Price,
		"grid_level": order.GridLevel,
	})

	return nil
}

// handleOrderFill handles when an order is filled and triggers rebalancing
func (g *GridManager) handleOrderFill(orderID string, symbol string) {
	g.logger.WithFields(logrus.Fields{
		"order_id": orderID,
		"symbol":   symbol,
	}).Info("handleOrderFill called - processing fill event")

	// Broadcast order filled metric to dashboard
	g.broadcastMetric("order_filled", symbol, map[string]interface{}{
		"order_id": orderID,
	})

	// NEW: Check for duplicate fill event
	if g.deduplicator.IsDuplicate(orderID, time.Now()) {
		g.logger.WithFields(logrus.Fields{
			"orderID": orderID,
			"symbol":  symbol,
		}).Warn("Duplicate fill event detected - skipping")
		return
	}
	// Record the fill event
	g.deduplicator.RecordEvent(orderID, time.Now())
	g.logger.WithField("order_id", orderID).Debug("Fill event recorded in deduplicator")

	g.ordersMu.Lock()
	order, exists := g.activeOrders[orderID]
	if !exists {
		g.ordersMu.Unlock()
		g.logger.WithField("orderID", orderID).Warn("Order not found in active orders")
		return
	}

	// NEW: Validate state transition before processing
	oldState := adaptive_grid.OrderState(order.Status)
	newState := adaptive_grid.OrderStateFilled

	// Special case: If order is CANCELLED but detected as filled, log and allow transition
	// This handles edge case where cancellation request was sent but order was already filled
	if oldState == adaptive_grid.OrderStateCancelled {
		g.logger.WithFields(logrus.Fields{
			"orderID": orderID,
			"symbol":  symbol,
		}).Info("Order marked as CANCELLED but detected as filled, allowing transition (edge case)")
		// Reset to PENDING to allow the transition to FILLED
		order.Status = "PENDING"
		oldState = adaptive_grid.OrderStatePending
	}

	if !g.stateValidator.IsValidTransition(oldState, newState) {
		g.ordersMu.Unlock()
		g.logger.WithFields(logrus.Fields{
			"orderID": orderID,
			"from":    oldState,
			"to":      newState,
		}).Warn("Invalid fill state transition - skipping")
		return
	}

	// Move to filled orders
	order.Status = "FILLED"
	order.FilledAt = time.Now()
	g.filledOrders[orderID] = order
	delete(g.activeOrders, orderID)
	g.ordersMu.Unlock()

	// Update filled orders metrics
	g.volumeMetricsMu.Lock()
	g.totalOrdersFilled++
	g.volumeMetricsMu.Unlock()

	// Log order fill
	if g.activityLog != nil {
		g.activityLog.Log(context.Background(), activitylog.EventOrderFilled, activitylog.SeverityInfo,
			activitylog.EntryContext{Symbol: symbol, StrategyName: "grid"},
			activitylog.OrderFilledPayload{
				OrderID:         orderID,
				ClientOrderID:   "",
				Side:            order.Side,
				FilledPrice:     order.Price,
				FilledQuantity:  order.Size,
				FilledValue:     order.Size * order.Price,
				Fee:             0,
				FeeAsset:        "USDT",
				ExecutionTimeMs: 0,
				GridLevel:       &order.GridLevel,
			},
		)
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":  symbol,
		"orderID": orderID,
		"side":    order.Side,
		"size":    order.Size,
		"price":   order.Price,
	}).Info("Order filled - checking risk before rebalancing")

	// NEW: Track filled position in AdaptiveGridManager
	if g.adaptiveMgr != nil {
		// Track in InventoryManager
		g.adaptiveMgr.TrackInventoryPosition(symbol, order.Side, order.Size, order.Price, order.GridLevel)

		// Track in ClusterManager
		positions := []adaptive_grid.PositionInfo{
			{
				Symbol:     symbol,
				Side:       order.Side,
				Size:       order.Size,
				EntryPrice: order.Price,
				GridLevel:  order.GridLevel,
				EntryTime:  order.FilledAt,
			},
		}
		g.adaptiveMgr.TrackClusterEntry(symbol, order.GridLevel, order.Side, positions)

		g.logger.WithFields(logrus.Fields{
			"symbol":     symbol,
			"side":       order.Side,
			"size":       order.Size,
			"grid_level": order.GridLevel,
		}).Info("Position tracked in inventory and cluster managers")

		// NEW: Initialize partial close tracking for this position
		g.adaptiveMgr.InitializePartialClose(symbol, order.Size, order.Price)
		g.logger.WithFields(logrus.Fields{
			"symbol":      symbol,
			"side":        order.Side,
			"size":        order.Size,
			"entry_price": order.Price,
		}).Info("Partial close tracking initialized for filled position")
	}

	// NEW: Place take profit order for micro profit feature
	if g.takeProfitMgr != nil {
		tpOrderID, err := g.takeProfitMgr.PlaceTakeProfitOrder(context.Background(), symbol, order.Side, order.Price, order.Size, orderID)
		if err != nil {
			g.logger.WithFields(logrus.Fields{
				"symbol":   symbol,
				"order_id": orderID,
				"error":    err,
			}).Warn("Failed to place take profit order, continuing without it")
		} else if tpOrderID != "" {
			g.ordersMu.Lock()
			order.TakeProfitOrderID = &tpOrderID
			g.ordersMu.Unlock()
			g.logger.WithFields(logrus.Fields{
				"symbol":               symbol,
				"order_id":             orderID,
				"take_profit_order_id": tpOrderID,
			}).Info("Take profit order placed successfully")
		}
	}

	// Trigger immediate rebalancing for this symbol
	// Risk is already controlled by AdaptiveGridManager.CanPlaceOrder and StateMachine gates
	// No need for additional canRebalance check - it's redundant and causes deadlock
	g.logger.WithFields(logrus.Fields{
		"symbol":   symbol,
		"order_id": orderID,
		"side":     order.Side,
	}).Info("Order filled - triggering rebalance")
	go g.enqueuePlacement(symbol)
}

// GetVolumeMetrics returns current volume farming metrics
func (g *GridManager) GetVolumeMetrics() (float64, int, int, float64) {
	g.volumeMetricsMu.RLock()
	defer g.volumeMetricsMu.RUnlock()

	fillRate := 0.0
	if g.totalOrdersPlaced > 0 {
		fillRate = float64(g.totalOrdersFilled) / float64(g.totalOrdersPlaced)
	}

	return g.totalVolumeUSDT, g.totalOrdersPlaced, g.totalOrdersFilled, fillRate
}

// LogVolumeMetrics logs current volume farming performance
func (g *GridManager) LogVolumeMetrics() {
	volume, placed, filled, fillRate := g.GetVolumeMetrics()

	g.logger.WithFields(logrus.Fields{
		"total_volume_usdt": volume,
		"orders_placed":     placed,
		"orders_filled":     filled,
		"fill_rate":         fmt.Sprintf("%.2f%%", fillRate*100),
		"active_orders":     len(g.activeOrders),
	}).Info("Volume Farming Metrics")
}

// metricsReporter reports volume farming metrics periodically
func (g *GridManager) metricsReporter(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Metrics reporter goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	ticker := time.NewTicker(30 * time.Second) // Report every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.LogVolumeMetrics()
			g.checkWebSocketHealth()
		}
	}
}

// checkWebSocketHealth checks if WebSocket connection is healthy
func (g *GridManager) checkWebSocketHealth() {
	if g.wsClient == nil {
		g.logger.Warn("WebSocket health check: wsClient is nil - ticker stream may not be connected")
		return
	}

	// Note: For more detailed health check, we would need to track last message timestamps
	// For now, just log that wsClient exists
	g.logger.Debug("WebSocket health check: wsClient is connected")
}

// exchangeDataReporter queries real exchange data and logs for dashboard
func (g *GridManager) exchangeDataReporter(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Exchange data reporter goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	ticker := time.NewTicker(10 * time.Second) // Report every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.logExchangeData(ctx)
		}
	}
}

// logExchangeData queries exchange for real open orders and positions
func (g *GridManager) logExchangeData(ctx context.Context) {
	// Get real open orders from exchange
	openOrders, err := g.futuresClient.GetOpenOrders(ctx, "")
	if err != nil {
		g.logger.WithError(err).Debug("Failed to get open orders for dashboard")
		return
	}

	// Count orders per symbol
	orderCountBySymbol := make(map[string]int)
	var totalNotional float64
	for _, order := range openOrders {
		orderCountBySymbol[order.Symbol]++
		notional := order.OrigQty * order.Price
		totalNotional += notional
	}

	// Get positions from WebSocket cache or API
	positions, err := g.GetCachedPositions(ctx)
	if err != nil {
		g.logger.WithError(err).Debug("Failed to get positions for dashboard")
		return
	}

	// Build position summary
	positionSummary := []map[string]interface{}{}
	for _, pos := range positions {
		if pos.PositionAmt != 0 {
			side := "BUY"
			if pos.PositionAmt < 0 {
				side = "SELL"
			}
			notional := math.Abs(pos.PositionAmt) * pos.MarkPrice
			positionSummary = append(positionSummary, map[string]interface{}{
				"symbol":        pos.Symbol,
				"side":          side,
				"size":          math.Abs(pos.PositionAmt),
				"notional":      notional,
				"entry":         pos.EntryPrice,
				"mark":          pos.MarkPrice,
				"unrealizedPnL": pos.UnrealizedProfit,
			})
		}
	}

	// Log real exchange data for dashboard
	g.logger.WithFields(logrus.Fields{
		"exchange_open_orders":      len(openOrders),
		"exchange_total_notional":   totalNotional,
		"exchange_positions_count":  len(positionSummary),
		"exchange_orders_by_symbol": orderCountBySymbol,
		"exchange_positions":        positionSummary,
	}).Info("Exchange Real Data")
}

func (g *GridManager) placementWorker(ctx context.Context, workerID int) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Placement worker goroutine panic recovered",
				zap.Int("worker_id", workerID),
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	g.logger.WithField("worker_id", workerID).Info("Placement worker started")

	for {
		select {
		case <-ctx.Done():
			g.logger.WithField("worker_id", workerID).Debug("Placement worker shutting down (context done)")
			return
		case <-g.stopCh:
			g.logger.WithField("worker_id", workerID).Debug("Placement worker shutting down (stop signal)")
			return
		case symbol := <-g.placementQueue:
			start := time.Now()
			g.logger.WithFields(logrus.Fields{
				"worker_id": workerID,
				"symbol":    symbol,
			}).Info("Processing placement for symbol")
			g.processPlacement(ctx, symbol)
			duration := time.Since(start)
			g.logger.WithFields(logrus.Fields{
				"worker_id":          workerID,
				"symbol":             symbol,
				"processing_time_ms": duration.Milliseconds(),
			}).Debug("Placement processing completed")
		}
	}
}

func (g *GridManager) ordersResetWorker(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Orders reset worker goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	ticker := time.NewTicker(10 * time.Second) // 10s for grid reset
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.resetStaleOrders()
			g.triggerRegularPlacement()
		}
	}
}

// triggerRegularPlacement checks and triggers placement for active grids
func (g *GridManager) triggerRegularPlacement() {
	g.gridsMu.RLock()
	defer g.gridsMu.RUnlock()

	for symbol, grid := range g.activeGrids {
		// Skip if already placing
		if grid.PlacementBusy {
			continue
		}

		// Check if we need to place orders (not placed or stale)
		if !grid.OrdersPlaced || time.Since(grid.LastAttempt) > 5*time.Second {
			// Check if symbol can place orders via AdaptiveGridManager
			if g.adaptiveMgr != nil && g.adaptiveMgr.CanPlaceOrder(symbol) {
				g.logger.WithFields(logrus.Fields{
					"symbol":       symbol,
					"ordersPlaced": grid.OrdersPlaced,
					"lastAttempt":  time.Since(grid.LastAttempt).Seconds(),
				}).Info("Triggering regular placement check")
				go g.enqueuePlacement(symbol)
			}
		}
	}
}

func (g *GridManager) resetStaleOrders() {
	// Get snapshot of grids needing check first
	g.gridsMu.RLock()
	type gridInfo struct {
		symbol string
		grid   *SymbolGrid
	}
	var toCheck []gridInfo
	for symbol, grid := range g.activeGrids {
		if grid.OrdersPlaced && time.Since(grid.LastAttempt) > 3*time.Second {
			toCheck = append(toCheck, gridInfo{symbol, grid})
		}
	}
	g.gridsMu.RUnlock()

	for _, info := range toCheck {
		symbol := info.symbol
		grid := info.grid
		expected := grid.MaxOrdersSide * 2
		actual := 0
		staleOrders := 0

		g.ordersMu.RLock()
		for _, order := range g.activeOrders {
			if order.Symbol == symbol && order.Status != "FILLED" && order.Status != "CANCELLED" {
				actual++
				if time.Since(order.CreatedAt) > 60*time.Second {
					staleOrders++
				}
			}
		}
		g.ordersMu.RUnlock()

		// Reset if grid is incomplete
		if actual < expected {
			grid.OrdersPlaced = false
			g.logger.WithFields(logrus.Fields{
				"symbol":   symbol,
				"expected": expected,
				"actual":   actual,
			}).Info("Resetting incomplete grid for re-placement")
			go g.enqueuePlacement(symbol)
			continue
		}

		if actual > 0 && staleOrders == actual {
			grid.OrdersPlaced = false
			g.logger.WithFields(logrus.Fields{
				"symbol":       symbol,
				"expected":     expected,
				"actual":       actual,
				"stale_orders": staleOrders,
			}).Info("Resetting stale grid - all orders pending too long without fills")
			go g.enqueuePlacement(symbol)
		}
	}
}

func (g *GridManager) processPlacement(ctx context.Context, symbol string) {
	g.logger.WithField("symbol", symbol).Info("Starting placement process for symbol")
	placementStart := time.Now()

	// Use wsClient.GetCachedOrders (single source of truth)
	// Fallback to API if cache stale (> 5s)
	exchangeOrders := g.wsClient.GetCachedOrders(symbol)
	if g.wsClient.IsCacheStale("order") || len(exchangeOrders) == 0 {
		// Cache stale or empty - fetch with timeout
		fetchCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()

		orders, err := g.fetchExchangeDataAsync(fetchCtx, symbol)
		if err != nil {
			// Fallback - skip exchange check on timeout/error, proceed with placement
			g.logger.WithError(err).WithFields(logrus.Fields{
				"symbol":   symbol,
				"duration": time.Since(placementStart).Milliseconds(),
			}).Warn("[PLACEMENT] Exchange fetch failed/timeout, proceeding without exchange check")
			exchangeOrders = []client.Order{} // Use empty slice
		} else {
			exchangeOrders = orders
		}
	} else {
		g.logger.WithField("symbol", symbol).Debug("[PLACEMENT] Using wsClient cached orders")
	}

	// Process exchange orders if available
	if len(exchangeOrders) > 0 {
		exchangeBuyCount := 0
		exchangeSellCount := 0
		for _, order := range exchangeOrders {
			if order.Status == "NEW" || order.Status == "PARTIALLY_FILLED" {
				if order.Side == "BUY" {
					exchangeBuyCount++
				} else if order.Side == "SELL" {
					exchangeSellCount++
				}
			}
		}
		g.logger.WithFields(logrus.Fields{
			"symbol":        symbol,
			"exchange_buy":  exchangeBuyCount,
			"exchange_sell": exchangeSellCount,
			"max_per_side":  g.maxOrdersSide,
			"source":        "wsClient",
		}).Info("[PLACEMENT] Exchange order count before placing")

		// Skip if already at or over limit (either side)
		if exchangeBuyCount >= g.maxOrdersSide || exchangeSellCount >= g.maxOrdersSide {
			g.logger.WithFields(logrus.Fields{
				"symbol":        symbol,
				"exchange_buy":  exchangeBuyCount,
				"exchange_sell": exchangeSellCount,
				"max_per_side":  g.maxOrdersSide,
			}).Warn("[PLACEMENT] Exchange at max orders, skipping placement")
			return
		}
	}

	g.gridsMu.Lock()
	grid, exists := g.activeGrids[symbol]
	if !exists {
		g.gridsMu.Unlock()
		g.logger.WithField("symbol", symbol).Warn("Grid not found for symbol, skipping placement")
		return
	}

	// T021: Wait for price if not available (max 5s wait)
	priceWaitStart := time.Now()
	for grid.CurrentPrice == 0 && time.Since(priceWaitStart) < 5*time.Second {
		g.gridsMu.Unlock()
		g.logger.WithField("symbol", symbol).Debug("[PLACEMENT] Waiting for price update...")
		time.Sleep(100 * time.Millisecond)
		g.gridsMu.Lock()
		grid = g.activeGrids[symbol]
	}

	// T022: Check if we got a price
	if grid.CurrentPrice == 0 {
		g.logger.WithFields(logrus.Fields{
			"symbol":  symbol,
			"wait_ms": time.Since(priceWaitStart).Milliseconds(),
		}).Warn("[PLACEMENT] Price still 0 after wait, skipping placement")
		grid.PlacementBusy = false
		g.gridsMu.Unlock()
		g.finishPlacement(symbol, false)
		return
	}

	// Mark as busy immediately to prevent duplicate scheduling
	grid.PlacementBusy = true
	grid.LastAttempt = time.Now()
	snapshot := *grid
	g.gridsMu.Unlock()

	if !g.canPlaceForSymbol(symbol) {
		g.logger.WithField("symbol", symbol).Info("[PLACEMENT] Blocked by runtime state/range gate")
		g.finishPlacement(symbol, false)
		return
	}

	// T024: Log placement price wait duration
	g.logger.WithFields(logrus.Fields{
		"symbol":           symbol,
		"current_price":    grid.CurrentPrice,
		"price_wait_ms":    time.Since(priceWaitStart).Milliseconds(),
		"total_elapsed_ms": time.Since(placementStart).Milliseconds(),
	}).Info("[PLACEMENT] Price verified, proceeding with placement")

	if ctx.Err() != nil {
		g.logger.WithField("symbol", symbol).Warn("Context cancelled during placement")
		g.finishPlacement(symbol, false)
		return
	}

	if maxLeverage := g.precisionMgr.GetMaxLeverage(symbol); maxLeverage > 0 && g.adaptiveMgr != nil {
		calculatedLeverage := g.adaptiveMgr.GetOptimalLeverage()
		targetLeverage := int(calculatedLeverage)
		if targetLeverage > int(maxLeverage) {
			targetLeverage = int(maxLeverage)
		}
		if targetLeverage < 1 {
			targetLeverage = 1
		}
		if err := g.futuresClient.SetLeverage(ctx, client.SetLeverageRequest{
			Symbol:   symbol,
			Leverage: targetLeverage,
		}); err != nil {
			g.logger.WithError(err).WithFields(logrus.Fields{
				"symbol":           symbol,
				"target_leverage":  targetLeverage,
				"calculated_value": calculatedLeverage,
			}).Warn("[PLACEMENT] Failed to set leverage before placement")
		}
	}

	placed := g.placeGridOrders(ctx, symbol, &snapshot)
	expectedOrders := snapshot.MaxOrdersSide * 2 // BUY + SELL sides

	if placed > 0 && g.stateMachine != nil && g.stateMachine.GetState(symbol) == adaptive_grid.GridStateEnterGrid {
		if g.stateMachine.CanTransition(symbol, adaptive_grid.EventEntryPlaced) {
			g.stateMachine.Transition(symbol, adaptive_grid.EventEntryPlaced)
		}
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":   symbol,
		"placed":   placed,
		"expected": expectedOrders,
	}).Info("Completed placement process for symbol")

	// Mark as complete if at least 80% of orders were placed (allow partial success)
	minSuccessRate := 0.8
	successRate := float64(placed) / float64(expectedOrders)
	g.finishPlacement(symbol, successRate >= minSuccessRate || placed > 0)
}

// T015: fetchExchangeDataAsync fetches exchange orders with context timeout support
func (g *GridManager) fetchExchangeDataAsync(ctx context.Context, symbol string) ([]client.Order, error) {
	return g.futuresClient.GetOpenOrders(ctx, symbol)
}

// NOTE: getCachedExchangeOrders and cacheExchangeOrders removed - using wsClient.GetCachedOrders() as single source of truth

func (g *GridManager) countActiveGridOrders(symbol string) int {
	g.ordersMu.RLock()
	defer g.ordersMu.RUnlock()

	count := 0
	for _, order := range g.activeOrders {
		if order.Symbol == symbol && order.Status != "FILLED" && order.Status != "CANCELLED" {
			count++
		}
	}
	return count
}

func (g *GridManager) finishPlacement(symbol string, placed bool) {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()

	grid, exists := g.activeGrids[symbol]
	if !exists {
		return
	}
	grid.PlacementBusy = false
	if placed {
		grid.OrdersPlaced = true
		g.consecutiveFailures = 0 // Reset on success
	} else {
		g.consecutiveFailures++
		g.lastFailureTime = time.Now()
	}
}

func (g *GridManager) enqueuePlacement(symbol string) {
	g.logger.WithField("symbol", symbol).Warn(">>> CALLING ENQUEUE PLACEMENT <<<")

	if !g.canPlaceForSymbol(symbol) {
		g.logger.WithField("symbol", symbol).Info("Skipping enqueue: runtime gate blocks placement")
		g.gridsMu.Lock()
		if grid, exists := g.activeGrids[symbol]; exists {
			grid.PlacementBusy = false
		}
		g.gridsMu.Unlock()
		return
	}

	// T027: Blocking enqueue with 500ms timeout instead of immediate skip
	start := time.Now()
	select {
	case g.placementQueue <- symbol:
		// T029: Log queue wait duration
		waitMs := time.Since(start).Milliseconds()
		g.logger.WithFields(logrus.Fields{
			"symbol":        symbol,
			"queue_wait_ms": waitMs,
		}).Warn(">>> ENQUEUED PLACEMENT SUCCESS <<<")
	case <-time.After(500 * time.Millisecond):
		// T027: Timeout - queue full for too long
		g.logger.WithFields(logrus.Fields{
			"symbol":        symbol,
			"timeout_ms":    500,
			"queue_wait_ms": time.Since(start).Milliseconds(),
		}).Error(">>> PLACEMENT QUEUE FULL - TIMEOUT SKIPPING <<<")
		g.gridsMu.Lock()
		if grid, exists := g.activeGrids[symbol]; exists {
			grid.PlacementBusy = false
		}
		g.gridsMu.Unlock()
	}
}

func (g *GridManager) handlePlaceOrderError(err error) {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || !apiErr.IsRateLimit() {
		return
	}

	// Apply penalty to rate limiter
	penalty := 30 * time.Second
	if until := parseBanExpiry(apiErr.Message); !until.IsZero() {
		penalty = time.Until(until)
		if penalty < 10*time.Second {
			penalty = 10 * time.Second
		}
	}
	g.rateLimiter.ApplyPenalty(penalty)

	// Keep old rateLimitUntil for backward compatibility
	until := time.Now().Add(penalty)
	g.rateLimitMu.Lock()
	if until.After(g.rateLimitUntil) {
		g.rateLimitUntil = until
	}
	g.rateLimitMu.Unlock()

	g.logger.WithField("until", until.Format(time.RFC3339)).Warn("Rate limit detected, applying penalty")
}

func (g *GridManager) rateLimitRemaining() time.Duration {
	g.rateLimitMu.RLock()
	defer g.rateLimitMu.RUnlock()

	if g.rateLimitUntil.IsZero() {
		return 0
	}
	return time.Until(g.rateLimitUntil)
}

func parseBanExpiry(message string) time.Time {
	re := regexp.MustCompile(`until (\d{13})`)
	matches := re.FindStringSubmatch(message)
	if len(matches) != 2 {
		return time.Time{}
	}

	ms, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func buildTickerStreamURL(base, stream string) string {
	base = strings.TrimRight(base, "/")
	stream = strings.TrimLeft(stream, "/")
	if base == "" || stream == "" {
		return ""
	}
	return fmt.Sprintf("%s/ws/%s", base, stream)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetTakeProfitMetrics returns micro profit metrics if take profit manager is available
func (g *GridManager) GetTakeProfitMetrics() map[string]interface{} {
	if g.takeProfitMgr == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}
	return g.takeProfitMgr.GetMicroProfitMetrics()
}

// Stop stops the grid manager.
func (g *GridManager) Stop(ctx context.Context) error {
	g.isRunningMu.Lock()
	if !g.isRunning {
		g.isRunningMu.Unlock()
		return nil
	}
	g.isRunning = false
	g.isRunningMu.Unlock()

	g.logger.Info("Stopping Grid Manager")

	// Safely close stopCh (may already be closed)
	select {
	case <-g.stopCh:
		// already closed
	default:
		close(g.stopCh)
	}

	// Shutdown take profit manager
	if g.takeProfitMgr != nil {
		g.takeProfitMgr.Shutdown(ctx)
		g.logger.Info("Take profit manager shutdown complete")
	}

	// Cleanup safeguard components
	if g.deduplicator != nil {
		g.deduplicator.Reset()
		g.logger.Info("Fill deduplicator reset")
	}
	if g.orderLockMgr != nil {
		g.orderLockMgr.CleanupStaleLocks()
		g.logger.Info("Order locks cleaned up")
	}

	if g.wsClient != nil {
		if err := g.wsClient.Close(); err != nil {
			g.logger.WithError(err).Error("Error closing WebSocket connection")
		}
	}

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				g.logger.Error("GridManager WaitGroup goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		g.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		g.logger.Info("Grid Manager stopped gracefully")
		return nil
	case <-ctx.Done():
		g.logger.Warn("Grid Manager stop timeout")
		return ctx.Err()
	}
}

// SetOrderSize sets the order size (kept for backward compatibility, use SetNotionalSize)
func (g *GridManager) SetOrderSize(size float64) {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()
	g.baseNotionalUSD = size // Treat as notional size
	g.logger.WithField("notional_size", size).Info("Order size (notional) updated")
}

// SetGridSpread sets the grid spread percentage
func (g *GridManager) SetGridSpread(spread float64) {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()
	g.gridSpreadPct = spread
	g.logger.WithField("grid_spread", spread).Info("Grid spread updated")
}

// calculateCurrentExposure calculates total exposure across ALL symbols from wsClient cache
// This is GLOBAL exposure check - total notional value of all open positions
func (g *GridManager) calculateCurrentExposure(ctx context.Context, symbol string) float64 {
	// Read from wsClient cache directly (single source of truth)
	positions := g.wsClient.GetCachedPositions()

	totalExposure := 0.0
	for _, pos := range positions {
		if pos.PositionAmt != 0 {
			exposure := math.Abs(pos.PositionAmt) * pos.MarkPrice
			totalExposure += exposure
			g.logger.WithFields(logrus.Fields{
				"symbol":     pos.Symbol,
				"position":   pos.PositionAmt,
				"mark_price": pos.MarkPrice,
				"exposure":   exposure,
			}).Debug("Position exposure calculated")
		}
	}

	g.logger.WithFields(logrus.Fields{
		"target_symbol":  symbol,
		"total_exposure": totalExposure,
		"positions":      len(positions),
		"source":         "wsClient",
	}).Debug("Total exposure calculated across all symbols")

	return totalExposure
}

// orderFillPoller polls for filled orders since UserStream is not connected
func (g *GridManager) orderFillPoller(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Order fill poller goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	ticker := time.NewTicker(30 * time.Second) // Poll every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.checkForFilledOrders(ctx)
		}
	}
}

// checkForFilledOrders checks open orders from API and detects fills
func (g *GridManager) checkForFilledOrders(ctx context.Context) {
	g.ordersMu.RLock()
	activeOrderIDs := make(map[string]bool)
	for orderID := range g.activeOrders {
		activeOrderIDs[orderID] = true
	}
	g.ordersMu.RUnlock()

	if len(activeOrderIDs) == 0 {
		return // No active orders to check
	}

	// Get all open orders from exchange
	openOrders, err := g.futuresClient.GetOpenOrders(ctx, "")
	if err != nil {
		g.logger.WithError(err).Warn("Failed to get open orders for fill detection")
		return
	}

	// Build map of open order IDs from exchange
	openOrderIDs := make(map[string]bool)
	for _, order := range openOrders {
		openOrderIDs[strconv.FormatInt(order.OrderID, 10)] = true
	}

	// Find orders that are in our activeOrders but not in exchange open orders
	// These are likely filled (or cancelled)
	filledCount := 0
	for orderID := range activeOrderIDs {
		if !openOrderIDs[orderID] {
			// Order not found in open orders - likely filled
			g.ordersMu.RLock()
			order, exists := g.activeOrders[orderID]
			g.ordersMu.RUnlock()

			if exists {
				g.logger.WithFields(logrus.Fields{
					"order_id": orderID,
					"symbol":   order.Symbol,
					"side":     order.Side,
				}).Info("Order detected as filled via polling")

				g.handleOrderFill(orderID, order.Symbol)
				filledCount++
			}
		}
	}

	if filledCount > 0 {
		g.logger.WithField("filled_count", filledCount).Info("Detected filled orders via polling")
	}
}

// SetMaxOrdersPerSide sets the maximum orders per side
func (g *GridManager) SetMaxOrdersPerSide(max int) {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()
	g.maxOrdersSide = max
	g.logger.WithField("max_orders", max).Info("Max orders per side updated")
}

// SetPositionTimeout sets the position timeout in minutes (for future use)
func (g *GridManager) SetPositionTimeout(minutes int) {
	g.logger.WithField("timeout_minutes", minutes).Info("Position timeout updated")
}

// SetRiskChecker sets the risk checker callback
func (g *GridManager) SetRiskChecker(checker RiskChecker) {
	g.riskCheckerMu.Lock()
	defer g.riskCheckerMu.Unlock()
	g.riskChecker = checker
	g.logger.Info("Risk checker set")
}

// SetAdaptiveManager sets the adaptive grid manager reference
func (g *GridManager) SetAdaptiveManager(adaptiveMgr *adaptive_grid.AdaptiveGridManager) {
	g.adaptiveMgr = adaptiveMgr
	g.logger.Info("AdaptiveGridManager reference set on GridManager")
}

// SetGridGeometry sets the adaptive grid geometry calculator
func (g *GridManager) SetGridGeometry(geometry *adaptive_grid.AdaptiveGridGeometry) {
	g.gridGeometry = geometry
	g.logger.Info("AdaptiveGridGeometry set on GridManager")
}

// CalculateAdaptiveGeometry calculates adaptive grid parameters using AdaptiveGridGeometry
// Returns (spread, orderCount, useAdaptive) where useAdaptive indicates if adaptive geometry was used
func (g *GridManager) CalculateAdaptiveGeometry(symbol string, currentPrice float64) (float64, int, bool) {
	if g.gridGeometry == nil {
		return 0, 0, false
	}

	// Get market data from adaptive manager
	atr := 0.0
	skew := 0.0
	funding := 0.0
	depth := 0.5
	risk := 0.5
	trend := 0.5
	regime := "RANGING"

	if g.adaptiveMgr != nil {
		// Get ATR
		if rangeDetector := g.adaptiveMgr.GetRangeDetector(symbol); rangeDetector != nil {
			if currentRange := rangeDetector.GetCurrentRange(); currentRange != nil {
				atr = currentRange.ATR
			}
		}

		// Get skew from inventory manager (default to 0 if not available)
		skew = 0.0

		// Get funding rate - skip if method doesn't exist
		funding = 0.0

		// Get regime
		regime = "RANGING"
		currentRegime := g.adaptiveMgr.GetCurrentRegime(symbol)
		// MarketRegime is a string type, compare directly
		if currentRegime == "TRENDING" {
			regime = "TRENDING"
		} else if currentRegime == "VOLATILE" {
			regime = "VOLATILE"
		}
	}

	// Calculate full geometry
	currentTime := time.Now()
	spread, _, orderCount, _, _, _, _ := g.gridGeometry.CalculateFullGeometry(
		atr, skew, funding, depth, risk, trend, regime, currentTime,
	)

	g.logger.WithFields(logrus.Fields{
		"symbol":      symbol,
		"atr":         atr,
		"regime":      regime,
		"spread":      spread,
		"order_count": orderCount,
	}).Info("Calculated adaptive geometry")

	return spread, orderCount, true
}

// SetTakeProfitManager sets the take profit manager lifecycle state machine
func (g *GridManager) SetStateMachine(sm *adaptive_grid.GridStateMachine) {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()
	g.stateMachine = sm
	g.logger.Info("Grid state machine set")
}

// GetActivePositions returns active positions for a symbol
func (g *GridManager) GetActivePositions(symbol string) ([]interface{}, error) {
	g.ordersMu.RLock()
	defer g.ordersMu.RUnlock()

	var positions []interface{}
	for orderID, order := range g.activeOrders {
		if order.Symbol == symbol {
			positions = append(positions, map[string]interface{}{
				"order_id": orderID,
				"symbol":   order.Symbol,
				"side":     order.Side,
				"size":     order.Size,
				"price":    order.Price,
				"status":   order.Status,
			})
		}
	}
	return positions, nil
}

// CancelAllOrders cancels all orders for a symbol on the exchange and clears local cache
func (g *GridManager) CancelAllOrders(ctx context.Context, symbol string) error {
	g.logger.WithField("symbol", symbol).Info("Cancelling all orders for symbol")

	// CRITICAL: First cancel all orders on the exchange to free up margin
	if g.futuresClient != nil {
		if err := g.futuresClient.CancelAllOpenOrders(ctx, symbol); err != nil {
			g.logger.WithFields(logrus.Fields{
				"symbol": symbol,
				"error":  err,
			}).Warn("Failed to cancel all orders on exchange")
			// Continue to clear local cache even if API call fails
		} else {
			g.logger.WithField("symbol", symbol).Info("All orders cancelled on exchange")
		}

		// Wait a short time for margin to be released on the exchange
		// This is critical to prevent "Margin is insufficient" errors when closing positions
		time.Sleep(500 * time.Millisecond)
	}

	g.ordersMu.Lock()
	defer g.ordersMu.Unlock()

	// Clear active orders from local cache
	for orderID, order := range g.activeOrders {
		if order.Symbol == symbol {
			delete(g.activeOrders, orderID)
			g.logger.WithFields(logrus.Fields{
				"order_id": orderID,
				"symbol":   symbol,
			}).Info("Order removed from local cache")
		}
	}

	return nil
}

// ClearGrid clears the grid for a symbol
func (g *GridManager) ClearGrid(ctx context.Context, symbol string) error {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()

	g.logger.WithField("symbol", symbol).Info("Clearing grid for symbol")

	if grid, exists := g.activeGrids[symbol]; exists {
		grid.IsActive = false
		grid.OrdersPlaced = false
		g.logger.WithField("symbol", symbol).Info("Grid cleared")
	}

	return nil
}

// RebuildGrid rebuilds the grid for a symbol
func (g *GridManager) RebuildGrid(ctx context.Context, symbol string) error {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()

	g.logger.WithField("symbol", symbol).Info("Rebuilding grid for symbol")

	if grid, exists := g.activeGrids[symbol]; exists {
		grid.IsActive = true
		grid.OrdersPlaced = false
		grid.PlacementBusy = false

		// Enqueue placement for this symbol
		g.enqueuePlacement(symbol)
		g.logger.WithField("symbol", symbol).Info("Grid rebuild scheduled")
	}

	return nil
}

// OnOrderUpdate handles real-time order updates from UserStream WebSocket
// This is the WebSocket alternative to polling for fills and syncs order state
func (g *GridManager) OnOrderUpdate(orderUpdate *OrderUpdate) {
	g.logger.WithFields(logrus.Fields{
		"order_id":  orderUpdate.OrderID,
		"symbol":    orderUpdate.Symbol,
		"side":      orderUpdate.Side,
		"status":    orderUpdate.Status,
		"quantity":  orderUpdate.Quantity,
		"price":     orderUpdate.Price,
		"websocket": true,
	}).Debug("WebSocket order update received")

	switch orderUpdate.Status {
	case "FILLED":
		g.logger.WithFields(logrus.Fields{
			"order_id": orderUpdate.OrderID,
			"symbol":   orderUpdate.Symbol,
		}).Info("WebSocket order fill detected - processing immediately")

		// NEW: Update VPIN monitor with fill volume
		if g.adaptiveMgr != nil {
			vpinMonitorInterface := g.adaptiveMgr.GetVPINMonitor()
			if vpinMonitorInterface != nil {
				// Type assert to concrete VPINMonitor
				if vpinMonitor, ok := vpinMonitorInterface.(*volume_optimization.VPINMonitor); ok {
					// Calculate fill volume (price * quantity)
					fillVolume := orderUpdate.Price * orderUpdate.Quantity
					// Determine buy/sell volume based on side
					var buyVol, sellVol float64
					if orderUpdate.Side == "BUY" {
						buyVol = fillVolume
						sellVol = 0
					} else {
						buyVol = 0
						sellVol = fillVolume
					}
					vpinMonitor.UpdateVolume(buyVol, sellVol)
					g.logger.WithFields(logrus.Fields{
						"symbol":   orderUpdate.Symbol,
						"side":     orderUpdate.Side,
						"buy_vol":  buyVol,
						"sell_vol": sellVol,
					}).Debug("VPIN monitor updated with fill volume")
				}
			}
		}

		// Check if this is a take profit order fill
		if g.takeProfitMgr != nil && len(orderUpdate.OrderID) > 3 && orderUpdate.OrderID[:3] == "TP-" {
			g.logger.WithFields(logrus.Fields{
				"order_id": orderUpdate.OrderID,
				"symbol":   orderUpdate.Symbol,
			}).Info("Take profit order fill detected - processing")
			err := g.takeProfitMgr.HandleTakeProfitFill(orderUpdate.OrderID, orderUpdate.Price)
			if err != nil {
				g.logger.WithFields(logrus.Fields{
					"order_id": orderUpdate.OrderID,
					"symbol":   orderUpdate.Symbol,
					"error":    err,
				}).Error("Failed to handle take profit fill")
			}
		} else {
			// Call handleOrderFill to process the fill event
			g.handleOrderFill(orderUpdate.OrderID, orderUpdate.Symbol)
		}

	case "CANCELED":
		g.logger.WithFields(logrus.Fields{
			"order_id": orderUpdate.OrderID,
			"symbol":   orderUpdate.Symbol,
		}).Info("WebSocket order cancellation detected - removing from active orders")
		// Remove from active orders
		g.ordersMu.Lock()
		delete(g.activeOrders, orderUpdate.OrderID)
		g.ordersMu.Unlock()

	case "EXPIRED", "REJECTED":
		g.logger.WithFields(logrus.Fields{
			"order_id": orderUpdate.OrderID,
			"symbol":   orderUpdate.Symbol,
			"status":   orderUpdate.Status,
		}).Warn("WebSocket order expired/rejected - removing from active orders")
		// Remove from active orders
		g.ordersMu.Lock()
		delete(g.activeOrders, orderUpdate.OrderID)
		g.ordersMu.Unlock()

	case "NEW":
		g.logger.WithFields(logrus.Fields{
			"order_id": orderUpdate.OrderID,
			"symbol":   orderUpdate.Symbol,
		}).Debug("WebSocket new order detected - should be tracked in activeOrders")

	case "PARTIALLY_FILLED":
		g.logger.WithFields(logrus.Fields{
			"order_id": orderUpdate.OrderID,
			"symbol":   orderUpdate.Symbol,
			"quantity": orderUpdate.Quantity,
		}).Debug("WebSocket partial fill detected - order still active")
		// Order still active, no action needed

	default:
		g.logger.WithFields(logrus.Fields{
			"order_id": orderUpdate.OrderID,
			"symbol":   orderUpdate.Symbol,
			"status":   orderUpdate.Status,
		}).Warn("WebSocket order update with unknown status")
	}
}

// OnAccountUpdate handles real-time position updates from UserStream WebSocket
// Only updates AdaptiveGridManager position tracking. wsClient cache is updated in volume_farm_engine.go.
func (g *GridManager) OnAccountUpdate(accountUpdate stream.WsAccountUpdate) {
	g.logger.WithFields(logrus.Fields{
		"positions_count": len(accountUpdate.Update.Positions),
		"balances_count":  len(accountUpdate.Update.Balances),
	}).Info("WebSocket account update received")

	// Only update AdaptiveGridManager's position tracking
	// wsClient cache is already updated in volume_farm_engine.go (single source of truth)
	for _, pos := range accountUpdate.Update.Positions {
		if pos.PositionAmt != 0 {
			// Convert WebSocket position to client.Position
			cachedPos := &client.Position{
				Symbol:           pos.Symbol,
				PositionAmt:      pos.PositionAmt,
				EntryPrice:       pos.EntryPrice,
				MarkPrice:        pos.EntryPrice, // Use entry price as fallback
				UnrealizedProfit: pos.UnrealizedPnL,
				Leverage:         1.0, // Default leverage
				MarginType:       pos.MarginType,
				PositionSide:     pos.PositionSide,
			}

			// CRITICAL: Update AdaptiveGridManager's position tracking
			// This ensures state machine transitions (e.g., EXIT_ALL → WAIT_NEW_RANGE) work correctly
			if g.adaptiveMgr != nil {
				g.adaptiveMgr.UpdatePositionTracking(pos.Symbol, cachedPos)
			}
		} else {
			// Position closed - update AdaptiveGridManager to zero
			if g.adaptiveMgr != nil {
				zeroPos := &client.Position{
					Symbol:      pos.Symbol,
					PositionAmt: 0,
					EntryPrice:  0,
					MarkPrice:   0,
				}
				g.adaptiveMgr.UpdatePositionTracking(pos.Symbol, zeroPos)
			}
		}
	}

	g.logger.Info("AdaptiveGridManager position tracking updated from WebSocket")
}

// GetCachedPositions returns positions from wsClient cache (single source of truth)
// WebSocket updates wsClient cache directly. Fallback to API if cache stale.
func (g *GridManager) GetCachedPositions(ctx context.Context) ([]client.Position, error) {
	// Read from wsClient cache (single source of truth)
	positions := g.wsClient.GetCachedPositions()

	// Fallback to API if wsClient cache stale (> 5s)
	if g.wsClient.IsCacheStale("position") {
		g.logger.WithFields(logrus.Fields{
			"stale": true,
		}).Warn("wsClient position cache stale, falling back to API call")
		return g.futuresClient.GetPositions(ctx)
	}

	// Convert map to slice
	result := make([]client.Position, 0, len(positions))
	for _, pos := range positions {
		result = append(result, pos)
	}

	g.logger.WithFields(logrus.Fields{
		"count":  len(result),
		"source": "wsClient",
	}).Debug("Positions retrieved from wsClient cache")

	return result, nil
}

// positionRebalancerWorker monitors position sizes and places reduce-only orders when needed
func (g *GridManager) positionRebalancerWorker(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Position rebalancer worker goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	g.rebalanceInterval = 5 * time.Second
	g.rebalanceAggressiveness = 0.5

	ticker := time.NewTicker(g.rebalanceInterval)
	defer ticker.Stop()

	g.logger.Info("Position rebalancer started - checking every 5s with 50% excess reduction (OPTIMIZED)",
		logrus.Fields{
			"threshold_pct":  g.rebalanceThresholdPct,
			"aggressiveness": g.rebalanceAggressiveness,
			"check_interval": g.rebalanceInterval,
		})

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.checkAndRebalancePositions(ctx)
		}
	}
}

// checkAndRebalancePositions checks all active positions and rebalances if needed
func (g *GridManager) checkAndRebalancePositions(ctx context.Context) {
	g.gridsMu.RLock()
	symbols := make([]string, 0, len(g.activeGrids))
	for symbol := range g.activeGrids {
		symbols = append(symbols, symbol)
	}
	g.gridsMu.RUnlock()

	for _, symbol := range symbols {
		g.checkAndRebalanceSymbol(ctx, symbol)
	}
}

// checkAndRebalanceSymbol checks position for a specific symbol and rebalances if needed
func (g *GridManager) checkAndRebalanceSymbol(ctx context.Context, symbol string) {
	// Get current position from WebSocket cache or API
	positions, err := g.GetCachedPositions(ctx)
	if err != nil {
		g.logger.WithError(err).Warn("Failed to get positions for rebalancing")
		return
	}

	var position *client.Position
	for i := range positions {
		if positions[i].Symbol == symbol && positions[i].PositionAmt != 0 {
			position = &positions[i]
			break
		}
	}

	if position == nil {
		return // No position to rebalance
	}

	positionSize := math.Abs(position.PositionAmt)
	positionNotional := positionSize * position.MarkPrice
	positionSide := "LONG"
	if position.PositionAmt < 0 {
		positionSide = "SHORT"
	}

	// Calculate threshold
	thresholdNotional := g.maxNotionalUSD * g.rebalanceThresholdPct

	g.logger.WithFields(logrus.Fields{
		"symbol":            symbol,
		"position_side":     positionSide,
		"position_size":     positionSize,
		"position_notional": positionNotional,
		"max_notional":      g.maxNotionalUSD,
		"threshold":         thresholdNotional,
	}).Debug("Checking position for rebalancing")

	// Check if position exceeds threshold
	if positionNotional <= thresholdNotional {
		return // Position within acceptable range
	}

	// Position too large - need to reduce
	excessNotional := positionNotional - g.maxNotionalUSD
	reduceNotional := excessNotional * g.rebalanceAggressiveness
	if reduceNotional < 5.0 {
		reduceNotional = 5.0 // Minimum $5 notional
	}

	// Determine rebalance order side (opposite of position)
	rebalanceSide := "SELL"
	if positionSide == "SHORT" {
		rebalanceSide = "BUY"
	}

	// Get current price for rebalancing order
	var rebalancePrice float64
	if rebalanceSide == "SELL" {
		// For SELL to close LONG, place slightly above mark price for better fill
		rebalancePrice = position.MarkPrice * 1.0002 // 0.02% above mark
	} else {
		// For BUY to close SHORT, place slightly below mark price
		rebalancePrice = position.MarkPrice * 0.9998 // 0.02% below mark
	}

	reduceSize := reduceNotional / rebalancePrice

	g.logger.WithFields(logrus.Fields{
		"symbol":            symbol,
		"position_side":     positionSide,
		"position_notional": positionNotional,
		"excess_notional":   excessNotional,
		"reduce_notional":   reduceNotional,
		"rebalance_side":    rebalanceSide,
		"rebalance_price":   rebalancePrice,
		"reduce_size":       reduceSize,
		"mark_price":        position.MarkPrice,
	}).Warn("REBALANCING: Position too large, placing reduce-only order")

	// Create rebalancing order
	order := &GridOrder{
		Symbol:      symbol,
		Side:        rebalanceSide,
		Size:        reduceSize,
		Price:       rebalancePrice,
		OrderType:   "LIMIT",
		Status:      "NEW",
		CreatedAt:   time.Now(),
		ReduceOnly:  true, // Critical: only reduces position
		IsRebalance: true,
		GridLevel:   999, // Special level for rebalancing orders
	}

	// Place the rebalancing order
	if err := g.placeOrder(order); err != nil {
		g.logger.WithError(err).Error("Failed to place rebalancing order")
	} else {
		g.logger.WithFields(logrus.Fields{
			"symbol":   symbol,
			"side":     rebalanceSide,
			"size":     reduceSize,
			"price":    rebalancePrice,
			"notional": reduceNotional,
		}).Info("Rebalancing order placed successfully")
	}
}

// SetRebalanceThreshold sets the threshold for position rebalancing
func (g *GridManager) SetRebalanceThreshold(thresholdPct float64) {
	g.rebalanceThresholdPct = thresholdPct
	g.logger.WithField("threshold_pct", thresholdPct).Info("Rebalance threshold updated")
}

// SetRebalanceAggressiveness sets how aggressive the rebalancing should be
func (g *GridManager) SetRebalanceAggressiveness(aggressiveness float64) {
	g.rebalanceAggressiveness = aggressiveness
	g.logger.WithField("aggressiveness", aggressiveness).Info("Rebalance aggressiveness updated")
}

// gridLimitEnforcerWorker monitors and cancels excess orders beyond max_orders_per_side
func (g *GridManager) gridLimitEnforcerWorker(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("Grid limit enforcer worker goroutine panic recovered",
				zap.Any("panic", r))
		}
	}()
	defer g.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	g.logger.Info("[LIMIT ENFORCER] Started - checking every 10s for excess orders")

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.enforceGridLimits(ctx)
		}
	}
}

// enforceGridLimits checks all symbols and cancels excess orders beyond max_orders_per_side
func (g *GridManager) enforceGridLimits(ctx context.Context) {
	g.gridsMu.RLock()
	symbols := make([]string, 0, len(g.activeGrids))
	for symbol := range g.activeGrids {
		symbols = append(symbols, symbol)
	}
	g.gridsMu.RUnlock()

	for _, symbol := range symbols {
		g.enforceSymbolLimits(ctx, symbol)
	}
}

// enforceSymbolLimits checks and cancels excess orders for a specific symbol
func (g *GridManager) enforceSymbolLimits(ctx context.Context, symbol string) {
	maxOrders := g.maxOrdersSide

	g.ordersMu.RLock()
	var buyOrders []*GridOrder
	var sellOrders []*GridOrder
	for _, order := range g.activeOrders {
		if order.Symbol != symbol || order.Status == "FILLED" || order.Status == "CANCELLED" {
			continue
		}
		if order.Side == "BUY" {
			buyOrders = append(buyOrders, order)
		} else if order.Side == "SELL" {
			sellOrders = append(sellOrders, order)
		}
	}
	g.ordersMu.RUnlock()

	// Cancel excess BUY orders
	if len(buyOrders) > maxOrders {
		excess := len(buyOrders) - maxOrders
		g.logger.WithFields(logrus.Fields{
			"symbol":    symbol,
			"side":      "BUY",
			"current":   len(buyOrders),
			"max":       maxOrders,
			"to_cancel": excess,
		}).Info("[LIMIT ENFORCER] Canceling excess BUY orders")
		g.cancelExcessOrders(ctx, symbol, buyOrders, excess)
	}

	// Cancel excess SELL orders
	if len(sellOrders) > maxOrders {
		excess := len(sellOrders) - maxOrders
		g.logger.WithFields(logrus.Fields{
			"symbol":    symbol,
			"side":      "SELL",
			"current":   len(sellOrders),
			"max":       maxOrders,
			"to_cancel": excess,
		}).Info("[LIMIT ENFORCER] Canceling excess SELL orders")
		g.cancelExcessOrders(ctx, symbol, sellOrders, excess)
	}
}

// cancelExcessOrders cancels the specified number of orders from the list
func (g *GridManager) cancelExcessOrders(ctx context.Context, symbol string, orders []*GridOrder, count int) {
	for i := 0; i < count && i < len(orders); i++ {
		order := orders[i]
		if order.OrderID == "" {
			continue
		}

		orderIDInt, _ := strconv.ParseInt(order.OrderID, 10, 64)
		if orderIDInt == 0 {
			continue
		}

		_, err := g.futuresClient.CancelOrder(ctx, client.CancelOrderRequest{
			Symbol:  symbol,
			OrderID: orderIDInt,
		})
		if err != nil {
			g.logger.WithFields(logrus.Fields{
				"symbol":   symbol,
				"order_id": order.OrderID,
				"error":    err,
			}).Warn("[LIMIT ENFORCER] Failed to cancel excess order")
			continue
		}

		g.ordersMu.Lock()
		if o, exists := g.activeOrders[order.OrderID]; exists {
			o.Status = "CANCELLED"
			delete(g.activeOrders, order.OrderID)
		}
		g.ordersMu.Unlock()

		g.logger.WithFields(logrus.Fields{
			"symbol":   symbol,
			"order_id": order.OrderID,
			"side":     order.Side,
		}).Info("[LIMIT ENFORCER] Cancelled excess order")
	}
}

// GetActiveGrids returns the list of currently active symbol grids
func (g *GridManager) GetActiveGrids() []*SymbolGrid {
	g.gridsMu.RLock()
	defer g.gridsMu.RUnlock()

	grids := make([]*SymbolGrid, 0, len(g.activeGrids))
	for _, grid := range g.activeGrids {
		grids = append(grids, grid)
	}
	return grids
}
