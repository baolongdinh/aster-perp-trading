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

	"aster-bot/internal/client"
	"aster-bot/internal/config"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// GridManager manages trading grids for multiple symbols.
type GridManager struct {
	futuresClient *client.FuturesClient
	logger        *logrus.Entry
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

	// Configuration.
	orderSizeUSDT float64
	gridSpreadPct float64
	maxOrdersSide int

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

// NewGridManager creates a new grid manager.
func NewGridManager(futuresClient *client.FuturesClient, logger *logrus.Entry) *GridManager {
	zapLogger, _ := zap.NewDevelopment()
	return &GridManager{
		futuresClient:         futuresClient,
		logger:                logger,
		activeGrids:           make(map[string]*SymbolGrid),
		pendingOrders:         make(map[string]*GridOrder),
		activeOrders:          make(map[string]*GridOrder),
		filledOrders:          make(map[string]*GridOrder),
		stopCh:                make(chan struct{}),
		orderSizeUSDT:         25.0,
		gridSpreadPct:         0.12,
		maxOrdersSide:         1,
		placementQueue:        make(chan string, 256), // Increased queue size
		gridPlacementCooldown: 10 * time.Second,       // Reduced default cooldown
		rateLimitCooldown:     30 * time.Second,       // Reduced rate limit cooldown
		tickerStreamURL:       "wss://fstream.asterdex.com/ws/!ticker@arr",
		rateLimiter:           NewRateLimiter(10, 2, zapLogger), // 10 capacity, 2 tokens/sec
		precisionMgr:          client.NewPrecisionManager(),
	}
}

// ApplyConfig updates runtime grid settings from volume farming config.
func (g *GridManager) ApplyConfig(cfg *config.VolumeFarmConfig) {
	if cfg == nil {
		return
	}
	if cfg.OrderSizeUSDT > 0 {
		g.orderSizeUSDT = cfg.OrderSizeUSDT
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
	exchangeInfo, err := marketClient.ExchangeInfo(context.Background())
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

	// Start multiple placement workers for concurrency
	numWorkers := 5 // Tăng từ 3 lên 5 workers
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

	// For volume farming, we want to place orders more frequently
	// Only skip if currently busy placing orders
	if grid.PlacementBusy {
		g.logger.WithField("symbol", grid.Symbol).Info("Not scheduling: placement busy")
		return false
	}

	// Dynamic cooldown: increase if consecutive failures, but keep it short for farming
	dynamicCooldown := g.gridPlacementCooldown
	if g.consecutiveFailures > 0 && time.Since(g.lastFailureTime) < 5*time.Minute {
		dynamicCooldown *= time.Duration(1 + g.consecutiveFailures)
		if dynamicCooldown > 30*time.Second {
			dynamicCooldown = 30 * time.Second
		}
	}

	// For volume farming, allow more frequent placement
	// If no recent attempt, or cooldown has passed, allow placement
	if grid.LastAttempt.IsZero() || time.Since(grid.LastAttempt) >= dynamicCooldown {
		g.logger.WithField("symbol", grid.Symbol).Info("Scheduling placement allowed for volume farming")
		return true
	}

	// Also allow if price changed significantly (but keep threshold low for farming)
	priceChangeThreshold := 0.001 // 0.1% change
	if oldPrice > 0 {
		priceChangePct := math.Abs(grid.CurrentPrice-oldPrice) / oldPrice
		if priceChangePct >= priceChangeThreshold {
			g.logger.WithFields(logrus.Fields{
				"symbol":       grid.Symbol,
				"price_change": priceChangePct,
				"threshold":    priceChangeThreshold,
			}).Info("Scheduling placement due to significant price change")
			return true
		}
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":     grid.Symbol,
		"since_last": time.Since(grid.LastAttempt),
		"cooldown":   dynamicCooldown,
	}).Info("Not scheduling: cooldown active")
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
		// Force initial placement for new grids
		g.enqueuePlacement(symbol)
	}
	for _, symbol := range removed {
		g.logger.WithField("symbol", symbol).Info("Removed grid")
	}
}

// placeGridOrders places initial grid orders for a symbol.
func (g *GridManager) placeGridOrders(symbol string, grid *SymbolGrid) int {
	g.logger.WithField("symbol", symbol).Info("Placing grid orders for volume farming")

	if grid.CurrentPrice == 0 {
		g.logger.WithField("symbol", symbol).Error("Cannot place orders: current price is 0")
		return 0
	}

	// For volume farming, use ultra-small spreads for maximum fills
	spreadAmount := grid.CurrentPrice * (grid.GridSpreadPct / 100)
	if spreadAmount < 0.001 { // Minimum spread of $0.001 for volume farming
		spreadAmount = 0.001
	}

	placedOrders := 0

	// Place BUY orders below current price
	for i := 1; i <= grid.MaxOrdersSide; i++ {
		buyPrice := grid.CurrentPrice - (spreadAmount * float64(i))
		if buyPrice <= 0 {
			continue
		}
		orderSize := g.orderSizeUSDT / buyPrice

		// Enhanced order size calculation with multiple fallbacks
		var finalSize float64
		var notional float64

		// Try symbol-specific precision first
		if g.precisionMgr != nil {
			roundedSize := g.precisionMgr.RoundQty(symbol, orderSize)
			parsedSize, parseErr := strconv.ParseFloat(roundedSize, 64)
			if parseErr == nil && parsedSize > 0 {
				notional = parsedSize * buyPrice
				g.logger.WithFields(logrus.Fields{
					"symbol":       symbol,
					"price":        buyPrice,
					"calc_size":    orderSize,
					"rounded_size": parsedSize,
					"notional":     notional,
				}).Info("Order size calculated successfully")
				finalSize = parsedSize
			}
		}

		// Fallback 1: Ensure minimum notional (5.0 USD with safety margin)
		if finalSize == 0 || notional < 5.0 {
			// Add 2% safety margin to ensure no -4164 errors
			minRequired := 5.0 * 1.02 // 5.1 USD minimum
			minSize := minRequired / buyPrice
			if minSize > 0 {
				// Apply reasonable precision based on price range
				var precision int
				if buyPrice < 1 {
					precision = 6 // For sub-dollar assets
				} else if buyPrice < 100 {
					precision = 4 // For moderate assets
				} else {
					precision = 2 // For high-value assets
				}

				multiplier := math.Pow(10, float64(precision))
				// Use Round instead of Floor to ensure notional >= 5.0
				roundedSize := math.Round(minSize*multiplier) / multiplier
				adjustedNotional := roundedSize * buyPrice

				// Ensure minimum notional is met
				for adjustedNotional < 5.0 {
					roundedSize += 1.0 / multiplier // Increment by smallest unit
					adjustedNotional = roundedSize * buyPrice
				}

				g.logger.WithFields(logrus.Fields{
					"symbol":       symbol,
					"price":        buyPrice,
					"min_required": minRequired,
					"calc_size":    minSize,
					"rounded_size": roundedSize,
					"notional":     adjustedNotional,
					"precision":    precision,
				}).Info("Applied minimum notional with safety margin")
				finalSize = roundedSize
			}
		}

		// Fallback 2: Ensure minimum reasonable size
		if finalSize < 0.000001 {
			finalSize = 0.000001
			g.logger.WithFields(logrus.Fields{
				"symbol": symbol,
				"size":   finalSize,
			}).Info("Applied minimum size fallback")
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

		if err := g.placeOrder(order); err != nil {
			g.logger.WithError(err).WithField("symbol", symbol).Warn("Failed to place BUY order")
		} else {
			placedOrders++
			g.logger.WithFields(logrus.Fields{
				"symbol": symbol,
				"side":   "BUY",
				"price":  buyPrice,
				"level":  -i,
			}).Info("Placed BUY order for volume farming")
		}
	}

	// Place SELL orders above current price
	for i := 1; i <= grid.MaxOrdersSide; i++ {
		sellPrice := grid.CurrentPrice + (spreadAmount * float64(i))
		orderSize := g.orderSizeUSDT / sellPrice

		// Enhanced order size calculation with multiple fallbacks
		var finalSize float64
		var notional float64

		// Try symbol-specific precision first
		if g.precisionMgr != nil {
			roundedSize := g.precisionMgr.RoundQty(symbol, orderSize)
			parsedSize, parseErr := strconv.ParseFloat(roundedSize, 64)
			if parseErr == nil && parsedSize > 0 {
				notional = parsedSize * sellPrice
				g.logger.WithFields(logrus.Fields{
					"symbol":       symbol,
					"price":        sellPrice,
					"calc_size":    orderSize,
					"rounded_size": parsedSize,
					"notional":     notional,
				}).Info("Order size calculated successfully")
				finalSize = parsedSize
			}
		}

		// Fallback 1: Ensure minimum notional (5.0 USD with safety margin)
		if finalSize == 0 || notional < 5.0 {
			// Add 2% safety margin to ensure no -4164 errors
			minRequired := 5.0 * 1.02 // 5.1 USD minimum
			minSize := minRequired / sellPrice
			if minSize > 0 {
				// Apply reasonable precision based on price range
				var precision int
				if sellPrice < 1 {
					precision = 6 // For sub-dollar assets
				} else if sellPrice < 100 {
					precision = 4 // For moderate assets
				} else {
					precision = 2 // For high-value assets
				}

				multiplier := math.Pow(10, float64(precision))
				// Use Round instead of Floor to ensure notional >= 5.0
				roundedSize := math.Round(minSize*multiplier) / multiplier
				adjustedNotional := roundedSize * sellPrice

				// Ensure minimum notional is met
				for adjustedNotional < 5.0 {
					roundedSize += 1.0 / multiplier // Increment by smallest unit
					adjustedNotional = roundedSize * sellPrice
				}

				g.logger.WithFields(logrus.Fields{
					"symbol":       symbol,
					"price":        sellPrice,
					"min_required": minRequired,
					"calc_size":    minSize,
					"rounded_size": roundedSize,
					"notional":     adjustedNotional,
					"precision":    precision,
				}).Info("Applied minimum notional with safety margin")
				finalSize = roundedSize
			}
		}

		// Fallback 2: Ensure minimum reasonable size
		if finalSize < 0.000001 {
			finalSize = 0.000001
			g.logger.WithFields(logrus.Fields{
				"symbol": symbol,
				"size":   finalSize,
			}).Info("Applied minimum size fallback")
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

		if err := g.placeOrder(order); err != nil {
			g.logger.WithError(err).WithField("symbol", symbol).Warn("Failed to place SELL order")
		} else {
			placedOrders++
			g.logger.WithFields(logrus.Fields{
				"symbol": symbol,
				"side":   "SELL",
				"price":  sellPrice,
				"level":  i,
			}).Info("Placed SELL order for volume farming")
		}
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":       symbol,
		"buy_orders":   grid.MaxOrdersSide,
		"sell_orders":  grid.MaxOrdersSide,
		"total_orders": placedOrders,
		"spread_pct":   grid.GridSpreadPct,
	}).Info("Grid orders placed for volume farming")

	return placedOrders
}

// placeOrder places a single order using the futures client.
func (g *GridManager) placeOrder(order *GridOrder) error {
	g.logger.WithFields(logrus.Fields{
		"symbol": order.Symbol,
		"side":   order.Side,
		"price":  order.Price,
		"size":   order.Size,
	}).Info("Attempting to place order")

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
	g.ordersMu.Lock()
	order, exists := g.activeOrders[orderID]
	if !exists {
		g.ordersMu.Unlock()
		g.logger.WithField("orderID", orderID).Warn("Order not found in active orders")
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

	g.logger.WithFields(logrus.Fields{
		"symbol":  symbol,
		"orderID": orderID,
		"side":    order.Side,
		"size":    order.Size,
		"price":   order.Price,
	}).Info("Order filled - triggering rebalancing")

	// Trigger immediate rebalancing for this symbol
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

	ticker := time.NewTicker(15 * time.Second) // Giảm từ 30 xuống 15 giây
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
		if grid.OrdersPlaced && time.Since(grid.LastAttempt) > 15*time.Second { // Giảm từ 30 xuống 15
			// Assume no fills, reset for re-placement
			grid.OrdersPlaced = false
			g.logger.WithField("symbol", symbol).Debug("Resetting orders for re-placement after timeout")
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
	grid.LastAttempt = time.Now()
	snapshot := *grid
	g.gridsMu.Unlock()

	if ctx.Err() != nil {
		g.logger.WithField("symbol", symbol).Warn("Context cancelled during placement")
		g.finishPlacement(symbol, false)
		return
	}

	placed := g.placeGridOrders(symbol, &snapshot)
	g.logger.WithField("symbol", symbol).WithField("placed", placed).Info("Completed placement process for symbol")
	g.finishPlacement(symbol, placed > 0)
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

	close(g.stopCh)

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
