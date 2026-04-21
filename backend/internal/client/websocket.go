package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// WebSocketClient handles real-time data streaming using proper WebSocket protocol
type WebSocketClient struct {
	baseURL   string
	logger    *zap.Logger
	mu        sync.RWMutex
	isRunning bool
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// WebSocket connection
	conn *websocket.Conn

	// Data channels
	tickerCh chan map[string]interface{}
	klineCh  chan KlineMessage

	// Subscriptions
	subscribedSymbols map[string]bool

	// Cache for orders, positions, balance (WebSocket-only data)
	orderCache      map[string]map[int64]Order // symbol -> orderID -> Order
	positionCache   map[string]Position        // symbol -> Position
	balanceCache    Balance                    // Account balance
	lastOrderUpdate time.Time
	lastPosUpdate   time.Time
	lastBalUpdate   time.Time

	// Runtime data hub additions
	lastMarketUpdate time.Time
	klineBuffer      map[string][]KlineMessage // symbol:interval -> klines (ring buffer)
	tickerCache      map[string]tickerData     // symbol -> ticker data
}

type tickerData struct {
	BestBid    float64
	BestAsk    float64
	Volume24h  float64
	LastPrice  float64
	UpdateTime time.Time
}

// TickerSnapshot is an exported helper type for tests and runtime bootstrap.
type TickerSnapshot struct {
	Symbol    string
	LastPrice float64
	BidPrice  float64
	AskPrice  float64
	Volume24h float64
	EventTime int64
}

// KlineMessage represents a kline/candlestick message from WebSocket
type KlineMessage struct {
	Symbol    string
	Interval  string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	IsClosed  bool
	StartTime int64
	EndTime   int64
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(baseURL string, logger *zap.Logger) *WebSocketClient {
	return &WebSocketClient{
		baseURL:           baseURL,
		logger:            logger,
		stopCh:            make(chan struct{}),
		tickerCh:          make(chan map[string]interface{}, 2000),
		klineCh:           make(chan KlineMessage, 5000),
		subscribedSymbols: make(map[string]bool),
		orderCache:        make(map[string]map[int64]Order),
		positionCache:     make(map[string]Position),
		klineBuffer:       make(map[string][]KlineMessage),
		tickerCache:       make(map[string]tickerData),
	}
}

// Connect establishes WebSocket connection to Aster API
func (ws *WebSocketClient) Connect(ctx context.Context) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.isRunning {
		return fmt.Errorf("websocket already running")
	}

	// Parse WebSocket URL
	_, err := url.Parse(ws.baseURL)
	if err != nil {
		return fmt.Errorf("invalid websocket URL: %w", err)
	}

	// Connect to Aster API WebSocket
	ws.logger.Info("Connecting to Aster API WebSocket", zap.String("url", ws.baseURL))

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(ws.baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	ws.conn = conn
	ws.isRunning = true

	// Start WebSocket message handler
	ws.wg.Add(1)
	go ws.realWebSocketHandler(ctx)

	ws.logger.Info("WebSocket connection established to Aster API")
	return nil
}

// realWebSocketHandler handles real WebSocket messages from Aster API
func (ws *WebSocketClient) realWebSocketHandler(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			ws.logger.Error("WebSocket handler goroutine panic recovered",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
		ws.wg.Done()
	}()

	// Set read deadline and ping handler
	ws.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	ws.conn.SetPongHandler(func(string) error {
		ws.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start ping goroutine
	go ws.pingHandler(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ws.stopCh:
			return
		default:
			// Read message from WebSocket - try both object and array formats
			var rawMsg json.RawMessage
			err := ws.conn.ReadJSON(&rawMsg)
			if err != nil {
				ws.logger.Error("WebSocket read error", zap.Error(err))
				return
			}

			// Try to parse as object first (for individual streams)
			var msgObj map[string]interface{}
			if err := json.Unmarshal(rawMsg, &msgObj); err == nil {
				// This is an object format (e.g., individual symbol streams)
				ws.processMessage(msgObj)
				continue
			}

			// Try to parse as array (for !ticker@arr)
			var msgArray []interface{}
			if err := json.Unmarshal(rawMsg, &msgArray); err == nil {
				// This is an array format (e.g., !ticker@arr)
				ws.processArrayMessage(msgArray)
				continue
			}

			ws.logger.Debug("Unknown WebSocket message format", zap.String("raw", string(rawMsg)))
		}
	}
}

// pingHandler sends periodic pings to keep connection alive
func (ws *WebSocketClient) pingHandler(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			ws.logger.Error("WebSocket ping handler panic recovered",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ws.stopCh:
			return
		case <-ticker.C:
			if err := ws.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				ws.logger.Error("WebSocket ping failed", zap.Error(err))
				return
			}
		}
	}
}

// SubscribeToTicker subscribes to ticker updates for specific symbols using Aster API format
func (ws *WebSocketClient) SubscribeToTicker(symbols []string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if !ws.isRunning {
		return fmt.Errorf("websocket not connected")
	}

	// Build subscription message in Aster API format
	subscribeMsg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": []string{},
		"id":     1,
	}

	for _, symbol := range symbols {
		var stream string
		if strings.HasPrefix(symbol, "!") {
			// Special streams like !ticker@arr - use as-is
			stream = symbol
		} else {
			// Regular symbol streams - convert to lowercase and append @ticker
			stream = fmt.Sprintf("%s@ticker", strings.ToLower(symbol))
		}
		subscribeMsg["params"] = append(subscribeMsg["params"].([]string), stream)
		ws.subscribedSymbols[symbol] = true
	}

	ws.logger.Info("Subscribing to ticker streams",
		zap.Strings("symbols", symbols),
		zap.Any("message", subscribeMsg))

	// Send subscription message over WebSocket
	if err := ws.conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("failed to send subscription: %w", err)
	}

	ws.logger.Info("Ticker subscription completed", zap.Strings("symbols", symbols))
	return nil
}

// SubscribeToKlines subscribes to kline (candlestick) streams for specific symbols
// Used for RangeDetector warm-up phase to collect OHLC data via WebSocket
func (ws *WebSocketClient) SubscribeToKlines(symbols []string, interval string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if !ws.isRunning {
		return fmt.Errorf("websocket not connected")
	}

	// Build subscription message in Aster API format
	subscribeMsg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": []string{},
		"id":     2,
	}

	for _, symbol := range symbols {
		// Convert symbol to lowercase and format as kline stream
		stream := fmt.Sprintf("%s@kline_%s", strings.ToLower(symbol), interval)
		subscribeMsg["params"] = append(subscribeMsg["params"].([]string), stream)
		ws.subscribedSymbols[symbol+"_kline"] = true
	}

	ws.logger.Info("Subscribing to kline streams",
		zap.Strings("symbols", symbols),
		zap.String("interval", interval),
		zap.Any("message", subscribeMsg))

	// Send subscription message over WebSocket
	if err := ws.conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("failed to send kline subscription: %w", err)
	}

	ws.logger.Info("Kline subscription completed", zap.Strings("symbols", symbols), zap.String("interval", interval))
	return nil
}

// getMapKeys returns a slice of keys from a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// processKlineMessage extracts and processes kline data from WebSocket message
// Aster API format: {"e":"kline","s":"ETHUSD1","k":{...}}
func (ws *WebSocketClient) processKlineMessage(msg map[string]interface{}) bool {
	// Check if this is a kline event
	eventType, ok := msg["e"].(string)
	if !ok {
		ws.logger.Debug("processKlineMessage: 'e' field not found or not string", zap.Any("msg_keys", getMapKeys(msg)))
		return false
	}
	if eventType != "kline" {
		return false
	}

	// Extract symbol directly
	symbolRaw, ok := msg["s"].(string)
	if !ok {
		ws.logger.Warn("processKlineMessage: 's' field not found or not string", zap.Any("msg", msg))
		return false
	}
	symbol := strings.ToUpper(symbolRaw)

	// Extract kline data directly from "k" field
	kline, ok := msg["k"].(map[string]interface{})
	if !ok {
		ws.logger.Warn("processKlineMessage: 'k' field not found or not map", zap.Any("msg_keys", getMapKeys(msg)))
		return false
	}

	// Parse OHLC values (handle both string and float formats)
	var open, high, low, close, volume float64
	var isClosed bool
	var interval string
	var startTime, endTime int64

	// Helper to parse float from string or float64
	parseFloat := func(v interface{}) float64 {
		switch val := v.(type) {
		case string:
			f, _ := strconv.ParseFloat(val, 64)
			return f
		case float64:
			return val
		default:
			return 0
		}
	}

	open = parseFloat(kline["o"])
	high = parseFloat(kline["h"])
	low = parseFloat(kline["l"])
	close = parseFloat(kline["c"])
	volume = parseFloat(kline["v"])

	if v, ok := kline["x"].(bool); ok {
		isClosed = v
	}
	if v, ok := kline["i"].(string); ok {
		interval = v
	}
	startTime = int64(parseFloat(kline["t"]))
	endTime = int64(parseFloat(kline["T"]))

	klineMsg := KlineMessage{
		Symbol:    symbol,
		Interval:  interval,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
		IsClosed:  isClosed,
		StartTime: startTime,
		EndTime:   endTime,
	}

	// Store in kline buffer for runtime hub access
	ws.storeKlineInBuffer(klineMsg)

	// Circular buffer: when full, drop oldest and insert new
	// For trading, we want the MOST RECENT data, not stale old data
	select {
	case ws.klineCh <- klineMsg:
		// Log channel depth periodically (every 100 messages when > 50% full)
		depth := len(ws.klineCh)
		if depth > 2500 && depth%100 == 0 {
			ws.logger.Warn("Kline channel depth high",
				zap.String("symbol", symbol),
				zap.Int("depth", depth),
				zap.Int("capacity", cap(ws.klineCh)))
		} else {
			ws.logger.Debug("Kline message processed",
				zap.String("symbol", symbol),
				zap.String("interval", interval),
				zap.Bool("is_closed", isClosed))
		}
	default:
		// Channel full: drop oldest message to make room for new one
		<-ws.klineCh           // Drop oldest
		ws.klineCh <- klineMsg // Insert new
	}
	return true
}

// GetKlineChannel returns the kline update channel for RangeDetector warm-up
func (ws *WebSocketClient) GetKlineChannel() <-chan KlineMessage {
	return ws.klineCh
}

// GetKlineChannelDepth returns current kline channel depth for monitoring
func (ws *WebSocketClient) GetKlineChannelDepth() int {
	return len(ws.klineCh)
}

// UnsubscribeFromKlines unsubscribes from kline streams after warm-up complete
func (ws *WebSocketClient) UnsubscribeFromKlines(symbols []string, interval string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if !ws.isRunning {
		return fmt.Errorf("websocket not connected")
	}

	unsubscribeMsg := map[string]interface{}{
		"method": "UNSUBSCRIBE",
		"params": []string{},
		"id":     3,
	}

	for _, symbol := range symbols {
		stream := fmt.Sprintf("%s@kline_%s", strings.ToLower(symbol), interval)
		unsubscribeMsg["params"] = append(unsubscribeMsg["params"].([]string), stream)
		delete(ws.subscribedSymbols, symbol+"_kline")
	}

	ws.logger.Info("Unsubscribing from kline streams", zap.Strings("symbols", symbols), zap.String("interval", interval))

	if err := ws.conn.WriteJSON(unsubscribeMsg); err != nil {
		return fmt.Errorf("failed to send kline unsubscription: %w", err)
	}

	return nil
}

// SubscribeToUserData subscribes to user data stream for order/account updates
func (ws *WebSocketClient) SubscribeToUserData(listenKey string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if !ws.isRunning {
		return fmt.Errorf("websocket not connected")
	}

	subscribeMsg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": []string{listenKey},
		"id":     4,
	}

	ws.logger.Info("Subscribing to user data stream", zap.String("listenKey", listenKey))

	if err := ws.conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("failed to send user data subscription: %w", err)
	}

	return nil
}

// processMessage handles incoming WebSocket messages from Aster API
func (ws *WebSocketClient) processMessage(msg map[string]interface{}) {
	// Check if it's a kline stream message first
	if ws.processKlineMessage(msg) {
		return
	}

	// Check if it's a ticker stream message from Aster API
	if stream, ok := msg["stream"].(string); ok {
		// Look for @ticker streams
		if len(stream) > 7 && stream[len(stream)-7:] == "@ticker" {
			// Update ticker cache
			ws.updateTickerCacheFromStream(msg)
			// Circular buffer: when full, drop oldest and insert new
			select {
			case ws.tickerCh <- msg:
				ws.logger.Debug("Ticker message processed", zap.String("stream", stream))
			default:
				<-ws.tickerCh      // Drop oldest
				ws.tickerCh <- msg // Insert new
				ws.logger.Warn("Ticker channel full, dropped oldest message to insert new",
					zap.String("stream", stream),
					zap.Int("depth", len(ws.tickerCh)),
					zap.Int("capacity", cap(ws.tickerCh)))
			}
			return
		}
	}

	// Handle user data events (ACCOUNT_UPDATE, ORDER_TRADE_UPDATE)
	if eventType, ok := msg["e"].(string); ok {
		switch eventType {
		case "ACCOUNT_UPDATE":
			ws.processAccountUpdate(msg)
		case "ORDER_TRADE_UPDATE":
			ws.processOrderTradeUpdate(msg)
		default:
			ws.logger.Debug("Unknown user data event", zap.String("event", eventType))
		}
		return
	}

	// Handle subscription responses
	if result, ok := msg["result"]; ok {
		if result == nil {
			ws.logger.Info("WebSocket subscription successful")
		}
		return
	}

	// Log other messages for debugging
	ws.logger.Debug("WebSocket message received", zap.Any("msg", msg))
}

// processAccountUpdate handles ACCOUNT_UPDATE events (positions & balance)
func (ws *WebSocketClient) processAccountUpdate(msg map[string]interface{}) {
	data, ok := msg["a"].(map[string]interface{})
	if !ok {
		return
	}

	// Update balance - aggregate both USD1 and USDT for USD-M futures
	if balanceData, ok := data["B"].([]interface{}); ok && len(balanceData) > 0 {
		// Aggregate balances for USD1 and USDT assets
		totalWalletBalance := 0.0
		totalAvailableBalance := 0.0
		totalMarginBalance := 0.0
		assets := []string{}

		for _, bal := range balanceData {
			if balanceItem, ok := bal.(map[string]interface{}); ok {
				asset := getString(balanceItem, "a")
				walletBalance := getFloat64(balanceItem, "wb")
				availableBalance := getFloat64(balanceItem, "ab")
				marginBalance := getFloat64(balanceItem, "m")

				// Only aggregate USD1 and USDT for USD-M futures
				if asset == "USD1" || asset == "USDT" {
					totalWalletBalance += walletBalance
					totalAvailableBalance += availableBalance
					totalMarginBalance += marginBalance
					assets = append(assets, asset)

					ws.logger.Debug("Balance asset found",
						zap.String("asset", asset),
						zap.Float64("wallet", walletBalance),
						zap.Float64("available", availableBalance))
				}
			}
		}

		if len(assets) > 0 {
			balance := Balance{
				Asset:            "USD1+USDT", // Combined asset name
				WalletBalance:    totalWalletBalance,
				AvailableBalance: totalAvailableBalance,
				MarginBalance:    totalMarginBalance,
			}
			ws.UpdateBalanceCache(balance)
			ws.logger.Info("Balance updated from WebSocket (aggregated)",
				zap.Strings("assets", assets),
				zap.Float64("total_available", totalAvailableBalance),
				zap.Float64("total_wallet", totalWalletBalance))
		} else {
			// Fallback: use first balance if no USD1/USDT found
			if firstBalance, ok := balanceData[0].(map[string]interface{}); ok {
				balance := Balance{
					Asset:            getString(firstBalance, "a"),
					WalletBalance:    getFloat64(firstBalance, "wb"),
					AvailableBalance: getFloat64(firstBalance, "ab"),
					MarginBalance:    getFloat64(firstBalance, "m"),
				}
				ws.UpdateBalanceCache(balance)
				ws.logger.Warn("No USD1/USDT balance found, using fallback asset",
					zap.String("asset", balance.Asset))
			}
		}
	}

	// Update positions
	if positions, ok := data["P"].([]interface{}); ok {
		for _, posData := range positions {
			if pos, ok := posData.(map[string]interface{}); ok {
				position := Position{
					Symbol:           getString(pos, "s"),
					PositionAmt:      getFloat64(pos, "pa"),
					PositionSide:     getString(pos, "ps"),
					EntryPrice:       getFloat64(pos, "ep"),
					UnrealizedProfit: getFloat64(pos, "up"),
				}
				ws.UpdatePositionCache(position)
				ws.logger.Debug("Position updated from WebSocket",
					zap.String("symbol", position.Symbol),
					zap.Float64("amt", position.PositionAmt))
			}
		}
	}
}

// processOrderTradeUpdate handles ORDER_TRADE_UPDATE events
func (ws *WebSocketClient) processOrderTradeUpdate(msg map[string]interface{}) {
	data, ok := msg["o"].(map[string]interface{})
	if !ok {
		return
	}

	order := Order{
		OrderID:     getInt64(data, "i"),
		Symbol:      getString(data, "s"),
		Status:      getString(data, "X"), // Current status
		Side:        getString(data, "S"),
		Type:        getString(data, "o"),
		Price:       getFloat64(data, "p"),
		OrigQty:     getFloat64(data, "q"),
		ExecutedQty: getFloat64(data, "z"),
		UpdateTime:  getInt64(data, "T"),
	}

	// Update or remove from cache based on status
	switch order.Status {
	case "FILLED", "CANCELED", "EXPIRED", "REJECTED":
		ws.RemoveOrderCache(order.Symbol, order.OrderID)
		ws.logger.Debug("Order removed from cache", zap.Int64("order_id", order.OrderID), zap.String("status", order.Status))
	default:
		ws.UpdateOrderCache(order)
		ws.logger.Debug("Order updated in cache", zap.Int64("order_id", order.OrderID), zap.String("status", order.Status))
	}
}

// Helper functions for safe type conversion
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	if v, ok := m[key].(string); ok {
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	if v, ok := m[key].(string); ok {
		i, _ := strconv.ParseInt(v, 10, 64)
		return i
	}
	return 0
}

// processArrayMessage handles array format messages (e.g., !ticker@arr)
func (ws *WebSocketClient) processArrayMessage(msgArray []interface{}) {
	// Create a wrapper message for array format
	wrapperMsg := map[string]interface{}{
		"stream": "!ticker@arr",
		"data":   msgArray,
	}

	// Circular buffer: when full, drop oldest and insert new
	select {
	case ws.tickerCh <- wrapperMsg:
		// ws.logger.Debug("Array ticker message processed", zap.Int("count", len(msgArray)))
	default:
		<-ws.tickerCh             // Drop oldest
		ws.tickerCh <- wrapperMsg // Insert new
		ws.logger.Warn("Ticker channel full, dropped oldest array message to insert new",
			zap.Int("array_count", len(msgArray)),
			zap.Int("depth", len(ws.tickerCh)),
			zap.Int("capacity", cap(ws.tickerCh)))
	}
}

// GetTickerChannel returns the ticker update channel
func (ws *WebSocketClient) GetTickerChannel() <-chan map[string]interface{} {
	return ws.tickerCh
}

// Close closes the WebSocket connection
func (ws *WebSocketClient) Close() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if !ws.isRunning {
		return nil
	}

	close(ws.stopCh)

	// Close WebSocket connection if it exists
	if ws.conn != nil {
		if err := ws.conn.Close(); err != nil {
			ws.logger.Error("Error closing WebSocket connection", zap.Error(err))
		}
	}

	ws.wg.Wait()

	ws.isRunning = false
	ws.logger.Info("WebSocket connection closed")
	return nil
}

// IsRunning returns whether WebSocket is connected
func (ws *WebSocketClient) IsRunning() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.isRunning
}

// GetCachedOrders returns cached orders for a symbol
// If symbol is empty, returns all cached orders across all symbols
func (ws *WebSocketClient) GetCachedOrders(symbol string) []Order {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// If no symbol specified, aggregate all orders
	if symbol == "" {
		totalOrders := 0
		for _, symbolOrders := range ws.orderCache {
			totalOrders += len(symbolOrders)
		}
		orders := make([]Order, 0, totalOrders)
		for _, symbolOrders := range ws.orderCache {
			for _, order := range symbolOrders {
				orders = append(orders, order)
			}
		}
		return orders
	}

	symbolOrders, exists := ws.orderCache[symbol]
	if !exists {
		return []Order{}
	}

	orders := make([]Order, 0, len(symbolOrders))
	for _, order := range symbolOrders {
		orders = append(orders, order)
	}
	return orders
}

// GetCachedPositions returns cached positions
func (ws *WebSocketClient) GetCachedPositions() map[string]Position {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	// Return a copy
	positions := make(map[string]Position)
	for symbol, pos := range ws.positionCache {
		positions[symbol] = pos
	}
	return positions
}

// GetCachedBalance returns cached balance
func (ws *WebSocketClient) GetCachedBalance() Balance {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.balanceCache
}

// UpdateOrderCache updates order in cache (called when WebSocket receives order update)
func (ws *WebSocketClient) UpdateOrderCache(order Order) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if _, exists := ws.orderCache[order.Symbol]; !exists {
		ws.orderCache[order.Symbol] = make(map[int64]Order)
	}
	ws.orderCache[order.Symbol][order.OrderID] = order
	ws.lastOrderUpdate = time.Now()
}

// RemoveOrderCache removes order from cache
func (ws *WebSocketClient) RemoveOrderCache(symbol string, orderID int64) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if symbolOrders, exists := ws.orderCache[symbol]; exists {
		delete(symbolOrders, orderID)
	}
}

// UpdatePositionCache updates position in cache
func (ws *WebSocketClient) UpdatePositionCache(position Position) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	ws.positionCache[position.Symbol] = position
	ws.lastPosUpdate = time.Now()
}

// UpdateBalanceCache updates balance in cache
func (ws *WebSocketClient) UpdateBalanceCache(balance Balance) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	ws.balanceCache = balance
	ws.lastBalUpdate = time.Now()
}

// IsCacheStale checks if cache data is stale (> 5 seconds old)
// Supports both singular and plural aliases: order/orders, position/positions, balance/balances
func (ws *WebSocketClient) IsCacheStale(cacheType string) bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	threshold := 5 * time.Second
	now := time.Now()

	switch cacheType {
	case "orders", "order":
		return now.Sub(ws.lastOrderUpdate) > threshold
	case "positions", "position":
		return now.Sub(ws.lastPosUpdate) > threshold
	case "balance", "balances":
		return now.Sub(ws.lastBalUpdate) > threshold
	case "ticker", "market":
		return now.Sub(ws.lastMarketUpdate) > threshold
	default:
		return true
	}
}

// GetLastEventTimes returns the last update times for market, account, and order events
func (ws *WebSocketClient) GetLastEventTimes() (market, account, order time.Time) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	accountUpdate := ws.lastBalUpdate
	if ws.lastPosUpdate.After(accountUpdate) {
		accountUpdate = ws.lastPosUpdate
	}
	return ws.lastMarketUpdate, accountUpdate, ws.lastOrderUpdate
}

// updateTickerCacheFromStream updates the ticker cache from a WebSocket stream message
func (ws *WebSocketClient) updateTickerCacheFromStream(msg map[string]interface{}) {
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		return
	}

	symbol := strings.ToUpper(getString(data, "s"))
	if symbol == "" {
		return
	}

	ws.mu.Lock()
	defer ws.mu.Unlock()

	ws.tickerCache[symbol] = tickerData{
		BestBid:    getFloat64(data, "b"),
		BestAsk:    getFloat64(data, "a"),
		Volume24h:  getFloat64(data, "v"),
		LastPrice:  getFloat64(data, "c"),
		UpdateTime: time.Now(),
	}
	ws.lastMarketUpdate = time.Now()
}

// GetTickerData returns best bid, best ask, and 24h volume for a symbol
func (ws *WebSocketClient) GetTickerData(symbol string) (bestBid, bestAsk, volume24h float64, err error) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	ticker, exists := ws.tickerCache[strings.ToUpper(symbol)]
	if !exists {
		return 0, 0, 0, fmt.Errorf("no ticker data for symbol %s", symbol)
	}
	return ticker.BestBid, ticker.BestAsk, ticker.Volume24h, nil
}

// GetLastPrice returns the last known price for a symbol
func (ws *WebSocketClient) GetLastPrice(symbol string) (float64, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	ticker, exists := ws.tickerCache[strings.ToUpper(symbol)]
	if !exists {
		return 0, false
	}
	return ticker.LastPrice, true
}

// UpsertTickerSnapshot injects or updates ticker cache state.
func (ws *WebSocketClient) UpsertTickerSnapshot(snapshot TickerSnapshot) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	updateTime := time.Now()
	if snapshot.EventTime > 0 {
		updateTime = time.UnixMilli(snapshot.EventTime)
	}

	ws.tickerCache[strings.ToUpper(snapshot.Symbol)] = tickerData{
		BestBid:    snapshot.BidPrice,
		BestAsk:    snapshot.AskPrice,
		Volume24h:  snapshot.Volume24h,
		LastPrice:  snapshot.LastPrice,
		UpdateTime: updateTime,
	}
	ws.lastMarketUpdate = updateTime
}

// BootstrapKlines seeds the kline buffer with historical data from REST API
func (ws *WebSocketClient) BootstrapKlines(symbol, interval string, klines []KlineMessage) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	key := strings.ToUpper(symbol) + ":" + interval
	ws.klineBuffer[key] = klines
	ws.logger.Info("Bootstrapped kline buffer",
		zap.String("symbol", symbol),
		zap.String("interval", interval),
		zap.Int("count", len(klines)))
}

// GetRecentKlines returns the most recent klines from the buffer for a symbol
func (ws *WebSocketClient) GetRecentKlines(symbol, interval string, limit int) []KlineMessage {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	key := strings.ToUpper(symbol) + ":" + interval
	history, exists := ws.klineBuffer[key]
	if !exists {
		return nil
	}

	if len(history) <= limit {
		result := make([]KlineMessage, len(history))
		copy(result, history)
		return result
	}

	// Return the most recent 'limit' klines
	start := len(history) - limit
	result := make([]KlineMessage, limit)
	copy(result, history[start:])
	return result
}

// storeKlineInBuffer stores a kline in the internal ring buffer
func (ws *WebSocketClient) storeKlineInBuffer(kline KlineMessage) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	key := strings.ToUpper(kline.Symbol) + ":" + kline.Interval
	history := ws.klineBuffer[key]

	// Check if we already have this kline (by start time)
	for i, existing := range history {
		if existing.StartTime == kline.StartTime {
			// Update existing kline with new data
			history[i] = kline
			ws.klineBuffer[key] = history
			return
		}
	}

	// Add new kline
	history = append(history, kline)

	// Keep only the most recent 500 klines (ring buffer)
	maxKlines := 500
	if len(history) > maxKlines {
		history = history[len(history)-maxKlines:]
	}

	ws.klineBuffer[key] = history
}
