package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

	// Subscriptions
	subscribedSymbols map[string]bool
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(baseURL string, logger *zap.Logger) *WebSocketClient {
	return &WebSocketClient{
		baseURL:           baseURL,
		logger:            logger,
		stopCh:            make(chan struct{}),
		tickerCh:          make(chan map[string]interface{}, 100),
		subscribedSymbols: make(map[string]bool),
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
	defer ws.wg.Done()

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
		// Convert to lowercase for Aster API
		stream := fmt.Sprintf("%s@ticker", strings.ToLower(symbol))
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

// processMessage handles incoming WebSocket messages from Aster API
func (ws *WebSocketClient) processMessage(msg map[string]interface{}) {
	// Check if it's a ticker stream message from Aster API
	if stream, ok := msg["stream"].(string); ok {
		// Look for @ticker streams
		if len(stream) > 7 && stream[len(stream)-7:] == "@ticker" {
			select {
			case ws.tickerCh <- msg:
				ws.logger.Debug("Ticker message processed", zap.String("stream", stream))
			default:
				ws.logger.Warn("Ticker channel full, dropping message")
			}
			return
		}
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

// processArrayMessage handles array format messages (e.g., !ticker@arr)
func (ws *WebSocketClient) processArrayMessage(msgArray []interface{}) {
	// Create a wrapper message for array format
	wrapperMsg := map[string]interface{}{
		"stream": "!ticker@arr",
		"data":   msgArray,
	}

	select {
	case ws.tickerCh <- wrapperMsg:
		// ws.logger.Debug("Array ticker message processed", zap.Int("count", len(msgArray)))
	default:
		ws.logger.Warn("Ticker channel full, dropping array message")
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
