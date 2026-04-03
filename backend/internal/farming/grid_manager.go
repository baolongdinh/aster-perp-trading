package farming

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"aster-bot/internal/client"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// GridManager manages trading grids for multiple symbols
type GridManager struct {
	futuresClient *client.FuturesClient
	logger        *logrus.Entry
	activeGrids   map[string]*SymbolGrid
	gridsMu       sync.RWMutex
	isRunning     bool
	isRunningMu   sync.RWMutex
	stopCh        chan struct{}
	wg            sync.WaitGroup

	// WebSocket client for real-time data
	wsClient *client.WebSocketClient

	// Order tracking
	pendingOrders map[string]*GridOrder
	ordersMu      sync.RWMutex

	// Configuration
	orderSizeUSDT float64
	gridSpreadPct float64
	maxOrdersSide int
}

// SymbolGrid represents a grid for a specific symbol
type SymbolGrid struct {
	Symbol        string    `json:"symbol"`
	QuoteCurrency string    `json:"quote_currency"`
	GridSpreadPct float64   `json:"grid_spread"`
	MaxOrdersSide int       `json:"max_orders"`
	CurrentPrice  float64   `json:"current_price"`
	MidPrice      float64   `json:"mid_price"`
	IsActive      bool      `json:"is_active"`
	LastUpdate    time.Time `json:"last_update"`
}

// GridOrder represents an order in the grid
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
	GridLevel    int       `json:"grid_level"` // Distance from mid price
}

// NewGridManager creates a new grid manager
func NewGridManager(futuresClient *client.FuturesClient, logger *logrus.Entry) *GridManager {
	return &GridManager{
		futuresClient: futuresClient,
		logger:        logger,
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		stopCh:        make(chan struct{}),

		// Realistic configuration for volume farming
		orderSizeUSDT: 50.0, // $50 per order (reasonable size)
		gridSpreadPct: 0.02, // 0.02% spread (tight for maker)
		maxOrdersSide: 2,    // 2 orders per side (4 total)
	}
}

// Start starts the grid manager
func (g *GridManager) Start(ctx context.Context) error {
	g.isRunningMu.Lock()
	if g.isRunning {
		g.isRunningMu.Unlock()
		return fmt.Errorf("grid manager is already running")
	}
	g.isRunning = true
	g.isRunningMu.Unlock()

	g.logger.Info("🚀 Starting Grid Manager")

	// Initialize WebSocket client for real-time data from Aster API
	zapLogger, _ := zap.NewDevelopment()
	g.wsClient = client.NewWebSocketClient("wss://fstream.asterdex.com/ws/!ticker@arr", zapLogger)

	if err := g.wsClient.Connect(ctx); err != nil {
		g.logger.WithError(err).Error("Failed to connect WebSocket to Aster API")
		return fmt.Errorf("failed to connect WebSocket: %w", err)
	}

	// Start WebSocket message processor
	g.wg.Add(1)
	go g.websocketProcessor(ctx)

	g.logger.Info("✅ Grid Manager started successfully")
	return nil
}

// websocketProcessor processes real-time WebSocket data
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

// processWebSocketTicker processes real-time ticker data from WebSocket
func (g *GridManager) processWebSocketTicker(msg map[string]interface{}) {
	// Extract data from Aster API WebSocket message format
	data, ok := msg["data"].([]interface{})
	if !ok {
		g.logger.Debug("WebSocket message missing data field or wrong format")
		return
	}

	// Process all tickers in the array
	for _, item := range data {
		ticker, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// Extract symbol and price from Aster API ticker format
		symbol, ok := ticker["s"].(string) // Aster API uses "s" for symbol
		if !ok {
			g.logger.Debug("WebSocket message missing symbol field")
			continue
		}

		lastPriceStr, ok := ticker["c"].(string) // Aster API uses "c" for last price as string
		if !ok {
			g.logger.Debug("WebSocket message missing last price field")
			continue
		}

		lastPrice, err := strconv.ParseFloat(lastPriceStr, 64)
		if err != nil {
			g.logger.WithError(err).Debug("Failed to parse last price")
			continue
		}

		// Update grid prices in real-time
		g.gridsMu.Lock()
		grid, exists := g.activeGrids[symbol]
		if exists {
			oldPrice := grid.CurrentPrice
			grid.CurrentPrice = lastPrice
			grid.MidPrice = lastPrice // Use last price as mid price for simplicity
			grid.LastUpdate = time.Now()

			// Log price updates
			if oldPrice != lastPrice && oldPrice != 0 {
				g.logger.WithFields(logrus.Fields{
					"symbol":    symbol,
					"old_price": oldPrice,
					"new_price": lastPrice,
					"source":    "websocket",
				}).Debug("Grid price updated")
			}

			// If this is the first price, start placing orders
			if oldPrice == 0 {
				g.logger.WithField("symbol", symbol).Info("Grid initialized with first price")
				// Start placing grid orders
				go g.placeGridOrders(symbol, grid)
			}
		}
		g.gridsMu.Unlock()
	}
}

// UpdateSymbols updates the list of symbols to manage
func (g *GridManager) UpdateSymbols(symbols []*SymbolData) {
	g.gridsMu.Lock()
	defer g.gridsMu.Unlock()

	// Create grids for new symbols
	for _, symbolData := range symbols {
		if _, exists := g.activeGrids[symbolData.Symbol]; !exists {
			grid := &SymbolGrid{
				Symbol:        symbolData.Symbol,
				QuoteCurrency: symbolData.QuoteAsset,
				GridSpreadPct: g.gridSpreadPct,
				MaxOrdersSide: g.maxOrdersSide,
				IsActive:      true,
			}
			g.activeGrids[symbolData.Symbol] = grid
			g.logger.WithField("symbol", symbolData.Symbol).Info("Created new grid")
		}
	}

	// Remove grids for symbols no longer active
	for symbol := range g.activeGrids {
		found := false
		for _, symbolData := range symbols {
			if symbolData.Symbol == symbol {
				found = true
				break
			}
		}
		if !found {
			delete(g.activeGrids, symbol)
			g.logger.WithField("symbol", symbol).Info("Removed grid")
		}
	}
}

// placeGridOrders places initial grid orders for a symbol
func (g *GridManager) placeGridOrders(symbol string, grid *SymbolGrid) {
	g.logger.WithField("symbol", symbol).Info("🎯 Placing grid orders")

	if grid.CurrentPrice == 0 {
		g.logger.WithField("symbol", symbol).Error("Cannot place orders: current price is 0")
		return
	}

	// Calculate grid levels
	spreadAmount := grid.CurrentPrice * (grid.GridSpreadPct / 100)

	// Place BUY orders below current price
	for i := 1; i <= grid.MaxOrdersSide; i++ {
		buyPrice := grid.CurrentPrice - (spreadAmount * float64(i))
		orderSize := g.orderSizeUSDT / buyPrice

		order := &GridOrder{
			Symbol:    symbol,
			Side:      "BUY",
			Size:      orderSize,
			Price:     buyPrice,
			OrderType: "LIMIT",
			Status:    "NEW",
			CreatedAt: time.Now(),
			GridLevel: -i, // Negative for BUY orders
		}

		if err := g.placeOrder(order); err != nil {
			g.logger.WithError(err).Error("Failed to place BUY order")
		}
	}

	// Place SELL orders above current price
	for i := 1; i <= grid.MaxOrdersSide; i++ {
		sellPrice := grid.CurrentPrice + (spreadAmount * float64(i))
		orderSize := g.orderSizeUSDT / sellPrice

		order := &GridOrder{
			Symbol:    symbol,
			Side:      "SELL",
			Size:      orderSize,
			Price:     sellPrice,
			OrderType: "LIMIT",
			Status:    "NEW",
			CreatedAt: time.Now(),
			GridLevel: i, // Positive for SELL orders
		}

		if err := g.placeOrder(order); err != nil {
			g.logger.WithError(err).Error("Failed to place SELL order")
		}
	}

	g.logger.WithFields(logrus.Fields{
		"symbol":       symbol,
		"buy_orders":   grid.MaxOrdersSide,
		"sell_orders":  grid.MaxOrdersSide,
		"total_orders": grid.MaxOrdersSide * 2,
	}).Info("✅ Grid orders placed successfully")
}

// placeOrder places a single order using the futures client
func (g *GridManager) placeOrder(order *GridOrder) error {
	g.logger.WithFields(logrus.Fields{
		"symbol": order.Symbol,
		"side":   order.Side,
		"size":   order.Size,
		"price":  order.Price,
	}).Info("📈 Placing grid order")

	// Create order request
	orderReq := &client.PlaceOrderRequest{
		Symbol:      order.Symbol,
		Side:        order.Side,
		Type:        order.OrderType,
		Quantity:    fmt.Sprintf("%.8f", order.Size),
		Price:       fmt.Sprintf("%.8f", order.Price),
		TimeInForce: "GTC",
	}

	// Place the order
	response, err := g.futuresClient.PlaceOrder(context.Background(), *orderReq)
	if err != nil {
		return fmt.Errorf("failed to place order: %w", err)
	}

	// Update order with response data
	order.OrderID = fmt.Sprintf("%d", response.OrderID)
	order.Status = response.Status

	g.logger.WithFields(logrus.Fields{
		"symbol":  order.Symbol,
		"side":    order.Side,
		"orderID": order.OrderID,
		"status":  order.Status,
	}).Info("✅ Grid order placed successfully")

	return nil
}

// Stop stops the grid manager
func (g *GridManager) Stop(ctx context.Context) error {
	g.isRunningMu.Lock()
	if !g.isRunning {
		g.isRunningMu.Unlock()
		return nil
	}
	g.isRunning = false
	g.isRunningMu.Unlock()

	g.logger.Info("🛑 Stopping Grid Manager")

	close(g.stopCh)

	// Close WebSocket connection
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
		g.logger.Info("✅ Grid Manager stopped gracefully")
		return nil
	case <-ctx.Done():
		g.logger.Warn("⚠️  Grid Manager stop timeout")
		return ctx.Err()
	}
}
