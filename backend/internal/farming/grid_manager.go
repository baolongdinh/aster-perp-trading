package farming

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"aster-bot/internal/activitylog"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_grid"

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

	// Order tracking.
	pendingOrders map[string]*GridOrder
	ordersMu      sync.RWMutex

	// Active orders tracking for fill detection
	activeOrders map[string]*GridOrder // OrderID -> GridOrder
	filledOrders map[string]*GridOrder // OrderID -> GridOrder

	// Configuration - Now using Notional Value based sizing
	baseNotionalUSD  float64 // Base order size in USD (e.g., $100)
	minNotionalUSD   float64 // Minimum order in USD (e.g., $20)
	maxNotionalUSD   float64 // Maximum order in USD (e.g., $500)
	gridSpreadPct    float64
	maxOrdersSide    int
	useDynamicSizing bool // Use ATR-based dynamic sizing

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
}

// SetActivityLogger sets the activity logger for the grid manager.
func (g *GridManager) SetActivityLogger(al *activitylog.ActivityLogger) {
	g.activityLog = al
}

// SymbolGrid represents a grid for a specific symbol.
type SymbolGrid struct {
	Symbol        string    `json:"symbol"`
	QuoteCurrency string    `json:"quote_currency"`
	GridSpreadPct float64   `json:"grid_spread"`
	MaxOrdersSide int       `json:"max_orders"`
	CurrentPrice  float64   `json:"current_price"`
	MidPrice      float64   `json:"mid_price"`
	IsActive      bool      `json:"is_active"`
	LastUpdate    time.Time `json:"last_update"`
	OrdersPlaced  bool      `json:"orders_placed"`
	PlacementBusy bool      `json:"placement_busy"`
	LastAttempt   time.Time `json:"last_attempt"`
}

// GridOrder represents an order in the grid.
type GridOrder struct {
	Symbol       string    `json:"symbol"`
	OrderID      string    `json:"order_id"`
	Side         string    `json:"side"`
	Size         float64   `json:"size"`
	Price        float64   `json:"price"`
	OrderType    string    `json:"order_type"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	FilledAt     time.Time `json:"filled_at,omitempty"`
	FeePaid      float64   `json:"fee_paid"`
	PointsEarned int64     `json:"points_earned"`
	GridLevel    int       `json:"grid_level"`
}

// NewGridManager creates a new grid manager with volume farm config.
func NewGridManager(futuresClient *client.FuturesClient, logger *logrus.Entry, cfg *config.VolumeFarmConfig) *GridManager {
	zapLogger, _ := zap.NewDevelopment()

	// Use config values or defaults
	baseNotional := 5.0
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

	zapLogger, _ := zap.NewDevelopment()
	g.wsClient = client.NewWebSocketClient(g.tickerStreamURL, zapLogger)

	if err := g.wsClient.Connect(ctx); err != nil {
		g.logger.WithError(err).Error("Failed to connect WebSocket to Aster API")
		return fmt.Errorf("failed to connect WebSocket: %w", err)
	}

	g.wg.Add(1)
	go g.websocketProcessor(ctx)

	g.wg.Add(1)
	go g.metricsReporter(ctx)

	g.wg.Add(1)
	go g.placementWorker(ctx)

	g.wg.Add(1)
	go g.ordersResetWorker(ctx)

	// Start multiple placement workers for high volume concurrency
	numWorkers := 20 // 20 workers for massive volume farming
	for i := 0; i < numWorkers; i++ {
		g.wg.Add(1)
		go g.placementWorker(ctx)
	}

	g.logger.Info("Grid Manager started successfully")
	return nil
}

// websocketProcessor processes real-time WebSocket data.
func (g *GridManager) websocketProcessor(ctx context.Context) {
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
	data, ok := msg["data"].([]interface{})
	if !ok {
		g.logger.Debug("WebSocket message missing data field or wrong format")
		return
	}

	g.logger.WithField("ticker_count", len(data)).Debug("Processing WebSocket ticker data")

	var symbolsToEnqueue []string

	for _, item := range data {
		ticker, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		symbol, ok := ticker["s"].(string)
		if !ok {
			continue
		}

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
			g.logger.WithField("symbol", symbol).Debug("Processing ticker for existing grid")
		} else {
			g.logger.WithField("symbol", symbol).Debug("Ignoring ticker for non-active symbol")
			g.gridsMu.Unlock()
			continue
		}
		oldPrice := grid.CurrentPrice
		grid.CurrentPrice = lastPrice
		grid.MidPrice = lastPrice
		grid.LastUpdate = time.Now()

		// NEW: Feed price data to AdaptiveGridManager calculators
		if g.adaptiveMgr != nil {
			// Use lastPrice as high, low, close (ticker only gives last price)
			// Pass 0 for bid/ask since ticker doesn't provide them
			g.adaptiveMgr.UpdatePriceData(symbol, lastPrice, lastPrice, lastPrice, 0, 0)
		}

		if oldPrice != lastPrice && oldPrice != 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":    symbol,
				"old_price": oldPrice,
				"new_price": lastPrice,
				"source":    "websocket",
			}).Info("Grid price updated")
		}

		if oldPrice == 0 {
			g.logger.WithField("symbol", symbol).Info("Grid initialized with first price")
		}

		if g.shouldSchedulePlacement(grid, oldPrice) {
			grid.PlacementBusy = true
			symbolsToEnqueue = append(symbolsToEnqueue, symbol)
			g.logger.WithField("symbol", symbol).Info("Scheduling grid placement due to price update")
		}
		g.gridsMu.Unlock()
	}

	for _, sym := range symbolsToEnqueue {
		g.enqueuePlacement(sym)
	}
}

func (g *GridManager) shouldSchedulePlacement(grid *SymbolGrid, oldPrice float64) bool {
	if grid == nil || !grid.IsActive || grid.CurrentPrice <= 0 {
		g.logger.WithField("symbol", grid.Symbol).Info("Not scheduling: grid inactive or no price")
		return false
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
		g.logger.WithField("symbol", grid.Symbol).Info("Not scheduling: placement busy")
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
			Symbol:        symbolData.Symbol,
			QuoteCurrency: symbolData.QuoteAsset,
			GridSpreadPct: g.gridSpreadPct,
			MaxOrdersSide: g.maxOrdersSide,
			IsActive:      true,
			LastAttempt:   time.Now().Add(-g.gridPlacementCooldown), // Force initial placement
		}
		created = append(created, symbolData.Symbol)
	}

	for symbol := range g.activeGrids {
		if _, found := desired[symbol]; found {
			continue
		}
		delete(g.activeGrids, symbol)
		removed = append(removed, symbol)
	}
	g.gridsMu.Unlock()

	for _, symbol := range created {
		g.logger.WithField("symbol", symbol).Info("Created new grid")
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
		// Only enqueue if we already have a price, otherwise wait for WebSocket
		g.gridsMu.RLock()
		grid, exists := g.activeGrids[symbol]
		hasPrice := exists && grid.CurrentPrice > 0
		g.gridsMu.RUnlock()

		if hasPrice {
			g.enqueuePlacement(symbol)
		} else {
			g.logger.WithField("symbol", symbol).Info("Grid created without price - waiting for WebSocket price update")
		}
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

// placeGridOrders places initial grid orders for a symbol concurrently.
// For rebuilds, it only places orders at levels that don't have active orders.
func (g *GridManager) placeGridOrders(ctx context.Context, symbol string, grid *SymbolGrid) int {
	g.logger.WithField("symbol", symbol).Info("Placing grid orders for volume farming (concurrent)")

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

	// NEW: Get dynamic spread from AdaptiveGridManager if available
	spreadPct := grid.GridSpreadPct
	if g.adaptiveMgr != nil {
		dynamicSpread := g.adaptiveMgr.GetDynamicSpread()
		// Only use dynamic spread if it's SMALLER than our configured spread
		// For volume farming, we want tight spreads, not wide ones
		if dynamicSpread > 0 && dynamicSpread < spreadPct {
			spreadPct = dynamicSpread
			g.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"dynamic_spread": dynamicSpread,
				"base_spread":    grid.GridSpreadPct,
				"final_spread":   spreadPct,
			}).Info("Using tighter dynamic spread for grid")
		} else if dynamicSpread > 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"dynamic_spread": dynamicSpread,
				"base_spread":    grid.GridSpreadPct,
				"final_spread":   spreadPct,
			}).Warn("Dynamic spread wider than config - using config spread")
		}
	}

	// For volume farming, use ultra-small spreads for maximum fills
	spreadAmount := grid.CurrentPrice * (spreadPct / 100)

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
		if buyPrice <= 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": i,
				"buy_price":  buyPrice,
			}).Warn("Skipping BUY order: price <= 0")
			continue
		}
		orderSize := g.baseNotionalUSD / buyPrice

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

		if orderSize <= 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": i,
				"buy_price":  buyPrice,
			}).Warn("Skipping BUY order due to zero/negative size")
			continue
		}

		finalSize := g.calculateOrderSize(symbol, orderSize, buyPrice)
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
		orderSize := g.baseNotionalUSD / sellPrice

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

		if orderSize <= 0 {
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"grid_level": i,
				"sell_price": sellPrice,
			}).Warn("Skipping SELL order due to zero/negative size")
			continue
		}

		finalSize := g.calculateOrderSize(symbol, orderSize, sellPrice)
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
			finalSize = parsedSize
		}
	}

	// Fallback: Ensure minimum notional (5.0 USD with safety margin)
	if finalSize == 0 || notional < 5.0 {
		minRequired := 5.0 * 1.02 // 5.1 USD minimum
		minSize := minRequired / price
		if minSize > 0 {
			var precision int
			if price < 1 {
				precision = 6
			} else if price < 100 {
				precision = 4
			} else {
				precision = 2
			}

			multiplier := math.Pow(10, float64(precision))
			roundedSize := math.Round(minSize*multiplier) / multiplier
			adjustedNotional := roundedSize * price

			for adjustedNotional < 5.0 {
				roundedSize += 1.0 / multiplier
				adjustedNotional = roundedSize * price
			}
			finalSize = roundedSize
		}
	}

	// Ensure minimum reasonable size
	if finalSize < 0.000001 {
		finalSize = 0.000001
	}

	return finalSize
}

// placeOrderAsync places an order asynchronously with context cancellation support
func (g *GridManager) placeOrderAsync(ctx context.Context, order *GridOrder, wg *sync.WaitGroup, successChan chan<- bool) {
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
		"symbol": order.Symbol,
		"side":   order.Side,
		"price":  order.Price,
		"size":   order.Size,
	}).Info("Attempting to place order")

	// CRITICAL: Check max position size before placing order
	notional := order.Size * order.Price
	if notional > g.maxNotionalUSD {
		g.logger.WithFields(logrus.Fields{
			"symbol":       order.Symbol,
			"side":         order.Side,
			"order_size":   order.Size,
			"price":        order.Price,
			"notional":     notional,
			"max_notional": g.maxNotionalUSD,
		}).Error("ORDER REJECTED: Exceeds max notional limit")
		return fmt.Errorf("order rejected: notional %.2f exceeds max %.2f", notional, g.maxNotionalUSD)
	}

	// CRITICAL: Check total position exposure before adding new order
	currentExposure := g.calculateCurrentExposure(context.Background(), order.Symbol)
	newTotalExposure := currentExposure + notional
	if newTotalExposure > g.maxNotionalUSD*2 { // Allow 2x for both sides
		g.logger.WithFields(logrus.Fields{
			"symbol":           order.Symbol,
			"current_exposure": currentExposure,
			"new_order":        notional,
			"total_exposure":   newTotalExposure,
			"max_exposure":     g.maxNotionalUSD * 2,
		}).Error("ORDER REJECTED: Total exposure would exceed limit - position too large!")
		return fmt.Errorf("order rejected: total exposure %.2f would exceed max %.2f", newTotalExposure, g.maxNotionalUSD*2)
	}

	// NEW: Check if trading is allowed by time filter
	if g.adaptiveMgr != nil && !g.adaptiveMgr.CanTrade() {
		g.logger.WithFields(logrus.Fields{
			"symbol": order.Symbol,
			"side":   order.Side,
		}).Warn("Order placement blocked - outside trading hours")
		return fmt.Errorf("trading not allowed: outside configured trading hours")
	}

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

	orderReq := &client.PlaceOrderRequest{
		Symbol:      order.Symbol,
		Side:        order.Side,
		Type:        order.OrderType,
		Quantity:    g.precisionMgr.RoundQty(order.Symbol, order.Size),
		Price:       g.precisionMgr.RoundPrice(order.Symbol, order.Price),
		TimeInForce: "GTC",
	}

	// Verify notional after rounding - fix for SOL and other high-price assets
	qty, _ := strconv.ParseFloat(orderReq.Quantity, 64)
	price, _ := strconv.ParseFloat(orderReq.Price, 64)
	notional = qty * price

	if notional < 5.0 {
		// Increase quantity to meet minimum notional
		minQty := 5.0 / price
		// Add 2% safety margin
		minQty = minQty * 1.02
		orderReq.Quantity = g.precisionMgr.RoundQty(order.Symbol, minQty)

		g.logger.WithFields(logrus.Fields{
			"symbol":       order.Symbol,
			"side":         order.Side,
			"original_qty": qty,
			"adjusted_qty": orderReq.Quantity,
			"price":        price,
			"notional":     notional,
		}).Warn("Adjusted order quantity to meet minimum notional requirement")
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

	return nil
}

// handleOrderFill handles when an order is filled and triggers rebalancing
func (g *GridManager) handleOrderFill(orderID string, symbol string) {
	g.logger.WithFields(logrus.Fields{
		"order_id": orderID,
		"symbol":   symbol,
	}).Info("handleOrderFill called - processing fill event")

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
	}

	// Check if rebalancing is allowed (risk limits not exceeded)
	canRebalance := g.canRebalance(symbol)
	g.logger.WithFields(logrus.Fields{
		"symbol":        symbol,
		"can_rebalance": canRebalance,
		"order_id":      orderID,
		"side":          order.Side,
	}).Info("Order filled - checking rebalance status")

	if !canRebalance {
		g.logger.WithField("symbol", symbol).Warn("Rebalancing blocked due to risk limits - SKIPPING REBALANCE")
		return
	}

	// Trigger immediate rebalancing for this symbol
	g.logger.WithField("symbol", symbol).Info("Triggering rebalance enqueue for filled order")
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
		}
	}
}

func (g *GridManager) placementWorker(ctx context.Context) {
	defer g.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case symbol := <-g.placementQueue:
			g.logger.WithField("symbol", symbol).Info("Processing placement for symbol")
			g.processPlacement(ctx, symbol)
		}
	}
}

func (g *GridManager) ordersResetWorker(ctx context.Context) {
	defer g.wg.Done()

	ticker := time.NewTicker(3 * time.Second) // 3s for rapid grid reset
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.resetStaleOrders()
		}
	}
}

func (g *GridManager) resetStaleOrders() {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()

	for symbol, grid := range g.activeGrids {
		// Check if grid has been placed but timeout passed
		if grid.OrdersPlaced && time.Since(grid.LastAttempt) > 3*time.Second {
			// Check if we have all expected orders
			expected := grid.MaxOrdersSide * 2
			actual := 0
			staleOrders := 0
			g.ordersMu.RLock()
			for _, order := range g.activeOrders {
				if order.Symbol == symbol && order.Status != "FILLED" && order.Status != "CANCELLED" {
					actual++
					// Count orders that have been pending for too long (>60s)
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
				// QUAN TRỌNG: Enqueue placement để rebuild grid ngay lập tức
				go g.enqueuePlacement(symbol)
				continue
			}

			// Also reset if all orders are stale (not getting filled after 60s)
			// This handles the case where price moved away from order prices
			if actual > 0 && staleOrders == actual {
				grid.OrdersPlaced = false
				g.logger.WithFields(logrus.Fields{
					"symbol":       symbol,
					"expected":     expected,
					"actual":       actual,
					"stale_orders": staleOrders,
				}).Info("Resetting stale grid - all orders pending too long without fills")
				// Enqueue placement để rebuild grid với giá mới
				go g.enqueuePlacement(symbol)
			}
		}
	}
}

func (g *GridManager) processPlacement(ctx context.Context, symbol string) {
	g.logger.WithField("symbol", symbol).Info("Starting placement process for symbol")

	g.gridsMu.Lock()
	grid, exists := g.activeGrids[symbol]
	if !exists {
		g.gridsMu.Unlock()
		g.logger.WithField("symbol", symbol).Warn("Grid not found for symbol, skipping placement")
		return
	}

	// Mark as busy immediately to prevent duplicate scheduling
	grid.PlacementBusy = true
	grid.LastAttempt = time.Now()
	snapshot := *grid
	g.gridsMu.Unlock()

	if ctx.Err() != nil {
		g.logger.WithField("symbol", symbol).Warn("Context cancelled during placement")
		g.finishPlacement(symbol, false)
		return
	}

	placed := g.placeGridOrders(ctx, symbol, &snapshot)
	expectedOrders := snapshot.MaxOrdersSide * 2 // BUY + SELL sides

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
	select {
	case g.placementQueue <- symbol:
		g.logger.WithField("symbol", symbol).Info("Enqueued placement for symbol")
	default:
		g.logger.WithField("symbol", symbol).Warn("Placement queue full, skipping grid seed")
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

// calculateCurrentExposure calculates total exposure for a symbol from exchange positions
func (g *GridManager) calculateCurrentExposure(ctx context.Context, symbol string) float64 {
	// Get actual positions from exchange
	positions, err := g.futuresClient.GetPositions(ctx)
	if err != nil {
		g.logger.WithError(err).Warn("Failed to get positions for exposure check")
		return 0
	}

	totalExposure := 0.0
	for _, pos := range positions {
		if pos.Symbol == symbol && pos.PositionAmt != 0 {
			exposure := math.Abs(pos.PositionAmt) * pos.MarkPrice
			totalExposure += exposure
			g.logger.WithFields(logrus.Fields{
				"symbol":     symbol,
				"position":   pos.PositionAmt,
				"mark_price": pos.MarkPrice,
				"exposure":   exposure,
			}).Debug("Position exposure calculated")
		}
	}

	return totalExposure
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
func (g *GridManager) SetAdaptiveManager(mgr *adaptive_grid.AdaptiveGridManager) {
	g.riskCheckerMu.Lock()
	defer g.riskCheckerMu.Unlock()
	g.adaptiveMgr = mgr
	g.logger.Info("Adaptive grid manager reference set")
}

// canRebalance checks if rebalancing is allowed for a symbol
func (g *GridManager) canRebalance(symbol string) bool {
	g.riskCheckerMu.RLock()
	defer g.riskCheckerMu.RUnlock()
	if g.riskChecker == nil {
		return true
	}
	return g.riskChecker.CanPlaceOrder(symbol)
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

// CancelAllOrders cancels all orders for a symbol
func (g *GridManager) CancelAllOrders(ctx context.Context, symbol string) error {
	g.ordersMu.Lock()
	defer g.ordersMu.Unlock()

	g.logger.WithField("symbol", symbol).Info("Cancelling all orders for symbol")

	// Cancel active orders for this symbol
	for orderID, order := range g.activeOrders {
		if order.Symbol == symbol {
			// Remove from active orders
			delete(g.activeOrders, orderID)
			g.logger.WithFields(logrus.Fields{
				"order_id": orderID,
				"symbol":   symbol,
			}).Info("Order cancelled")
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
