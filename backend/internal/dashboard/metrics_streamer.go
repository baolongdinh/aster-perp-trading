package dashboard

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Metric represents a real-time trading metric
type Metric struct {
	Type      string                 `json:"type"` // state_transition, market_condition, position_size, order_placed, order_filled, etc.
	Symbol    string                 `json:"symbol"`
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
}

// MetricsStreamer handles WebSocket connections and broadcasts metrics
type MetricsStreamer struct {
	clients  map[*websocket.Conn]bool
	mu       sync.RWMutex
	logger   *zap.Logger
	upgrader websocket.Upgrader
}

// NewMetricsStreamer creates a new metrics streamer
func NewMetricsStreamer(logger *zap.Logger) *MetricsStreamer {
	return &MetricsStreamer{
		clients: make(map[*websocket.Conn]bool),
		logger:  logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins
			},
		},
	}
}

// HandleWebSocket handles WebSocket connections
func (s *MetricsStreamer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()

	s.logger.Info("Dashboard client connected", zap.String("remote_addr", r.RemoteAddr))

	// Send initial connection message
	s.sendToClient(conn, Metric{
		Type:      "connection",
		Data:      map[string]interface{}{"status": "connected"},
		Timestamp: time.Now(),
	})

	// Keep connection alive
	go s.readPump(conn)
	s.writePump(conn)
}

// readPump reads messages from client (keep-alive)
func (s *MetricsStreamer) readPump(conn *websocket.Conn) {
	defer func() {
		conn.Close()
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			s.mu.Lock()
			delete(s.clients, conn)
			s.mu.Unlock()
			s.logger.Info("Dashboard client disconnected")
			break
		}
	}
}

// writePump writes metrics to client
func (s *MetricsStreamer) writePump(conn *websocket.Conn) {
	defer func() {
		conn.Close()
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Send ping to keep connection alive
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// BroadcastMetric broadcasts a metric to all connected clients
func (s *MetricsStreamer) BroadcastMetric(metricType string, symbol string, data map[string]interface{}, timestamp time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.clients) == 0 {
		return // No clients connected
	}

	metric := Metric{
		Type:      metricType,
		Symbol:    symbol,
		Data:      data,
		Timestamp: timestamp,
	}

	jsonData, err := json.Marshal(metric)
	if err != nil {
		s.logger.Error("Failed to marshal metric", zap.Error(err))
		return
	}

	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, jsonData); err != nil {
			s.logger.Error("Failed to send metric to client", zap.Error(err))
			conn.Close()
			delete(s.clients, conn)
		}
	}
}

// sendToClient sends a metric to a specific client
func (s *MetricsStreamer) sendToClient(conn *websocket.Conn, metric Metric) {
	data, err := json.Marshal(metric)
	if err != nil {
		s.logger.Error("Failed to marshal metric", zap.Error(err))
		return
	}

	conn.WriteMessage(websocket.TextMessage, data)
}

// ClientCount returns the number of connected clients
func (s *MetricsStreamer) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// BroadcastContinuousState broadcasts continuous state dimensions to dashboard
func (s *MetricsStreamer) BroadcastContinuousState(symbol string, positionSize, volatility, risk, trend, skew float64) {
	data := map[string]interface{}{
		"position_size": positionSize,
		"volatility":    volatility,
		"risk":          risk,
		"trend":         trend,
		"skew":          skew,
	}
	s.BroadcastMetric("continuous_state", symbol, data, time.Now())
}
