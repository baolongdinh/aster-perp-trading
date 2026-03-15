package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
}

// WsMessage is a typed message pushed to FE clients.
type WsMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
	Time    int64       `json:"time"`
}

// wsClient is a single connected FE WebSocket client.
type wsClient struct {
	conn *websocket.Conn
	send chan WsMessage
}

// WsHub manages connected FE WebSocket clients and broadcasts events.
type WsHub struct {
	log     *zap.Logger
	mu      sync.RWMutex
	clients map[*wsClient]bool
}

// NewWsHub creates a new WsHub.
func NewWsHub(log *zap.Logger) *WsHub {
	return &WsHub{
		log:     log,
		clients: make(map[*wsClient]bool),
	}
}

// ServeWS upgrades the HTTP connection to WebSocket and registers the client.
func (h *WsHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("ws upgrade error", zap.Error(err))
		return
	}
	c := &wsClient{conn: conn, send: make(chan WsMessage, 64)}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()

	h.log.Info("ws client connected")
	go h.writePump(c)
	h.readPump(c) // blocks until disconnect
}

// Broadcast sends a message to all connected FE clients.
func (h *WsHub) Broadcast(msgType string, payload interface{}) {
	msg := WsMessage{Type: msgType, Payload: payload, Time: time.Now().UnixMilli()}
	h.mu.RLock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.send <- msg:
		default:
			// Slow client — drop to avoid blocking
			h.log.Warn("ws client send buffer full, dropping message")
		}
	}
}

func (h *WsHub) readPump(c *wsClient) {
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		c.conn.Close()
		h.log.Info("ws client disconnected")
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (h *WsHub) writePump(c *wsClient) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
