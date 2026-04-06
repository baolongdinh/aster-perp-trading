package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"aster-bot/internal/activitylog"
)

// ActivityFilter defines filtering criteria for activity stream clients.
type ActivityFilter struct {
	EventTypes  []activitylog.EventType `json:"event_types,omitempty"`
	Symbols     []string                `json:"symbols,omitempty"`
	Strategies  []string                `json:"strategies,omitempty"`
	MinSeverity activitylog.Severity    `json:"min_severity,omitempty"`
}

// ShouldInclude checks if a log entry matches the filter criteria.
func (f *ActivityFilter) ShouldInclude(entry activitylog.LogEntry) bool {
	// Check event type
	if len(f.EventTypes) > 0 {
		found := false
		for _, et := range f.EventTypes {
			if et == entry.EventType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check symbol
	if len(f.Symbols) > 0 && entry.Context.Symbol != "" {
		found := false
		for _, sym := range f.Symbols {
			if sym == entry.Context.Symbol {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check strategy
	if len(f.Strategies) > 0 && entry.Context.StrategyID != "" {
		found := false
		for _, strat := range f.Strategies {
			if strat == entry.Context.StrategyID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check severity
	severityOrder := map[activitylog.Severity]int{
		activitylog.SeverityInfo:     0,
		activitylog.SeverityWarn:     1,
		activitylog.SeverityError:    2,
		activitylog.SeverityCritical: 3,
	}

	if f.MinSeverity != "" {
		if severityOrder[entry.Severity] < severityOrder[f.MinSeverity] {
			return false
		}
	}

	return true
}

// Client message types
const (
	MsgSubscribe   = "subscribe"
	MsgUnsubscribe = "unsubscribe"
	MsgPing        = "ping"
	MsgFilter      = "filter"
)

// Server message types
const (
	MsgActivity      = "activity"
	MsgActivityBatch = "activity_batch"
	MsgAlert         = "alert"
	MsgPong          = "pong"
)

// ActivityClient represents a WebSocket client subscribed to activity stream.
type ActivityClient struct {
	client *wsClient
	filter ActivityFilter
}

// ActivityStreamHub manages activity streaming to WebSocket clients.
type ActivityStreamHub struct {
	hub     *WsHub
	mu      sync.RWMutex
	clients map[*wsClient]ActivityFilter
}

// NewActivityStreamHub creates a new activity stream hub.
func NewActivityStreamHub(hub *WsHub) *ActivityStreamHub {
	return &ActivityStreamHub{
		hub:     hub,
		clients: make(map[*wsClient]ActivityFilter),
	}
}

// BroadcastActivity sends an activity entry to all matching clients.
func (h *ActivityStreamHub) BroadcastActivity(entry activitylog.LogEntry) {
	h.mu.RLock()
	clients := make([]*wsClient, 0, len(h.clients))
	filters := make([]ActivityFilter, 0, len(h.clients))

	for client, filter := range h.clients {
		if filter.ShouldInclude(entry) {
			clients = append(clients, client)
			filters = append(filters, filter)
		}
	}
	h.mu.RUnlock()

	// Determine message type based on severity
	msgType := MsgActivity
	if entry.Severity == activitylog.SeverityError || entry.Severity == activitylog.SeverityCritical {
		msgType = MsgAlert
	}

	msg := WsMessage{
		Type:    msgType,
		Payload: entry,
		Time:    entry.Timestamp.UnixMilli(),
	}

	// Send to matching clients
	for _, client := range clients {
		select {
		case client.send <- msg:
		default:
			// Client buffer full, skip
		}
	}
}

// BroadcastBatch sends a batch of activity entries to clients.
func (h *ActivityStreamHub) BroadcastBatch(entries []activitylog.LogEntry) {
	if len(entries) == 0 {
		return
	}

	h.mu.RLock()
	clients := make([]*wsClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	msg := WsMessage{
		Type:    MsgActivityBatch,
		Payload: entries,
		Time:    entries[0].Timestamp.UnixMilli(),
	}

	for _, client := range clients {
		select {
		case client.send <- msg:
		default:
			// Client buffer full, skip
		}
	}
}

// Subscribe adds a client to the activity stream with optional filter.
func (h *ActivityStreamHub) Subscribe(client *wsClient, filter ActivityFilter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client] = filter
}

// Unsubscribe removes a client from the activity stream.
func (h *ActivityStreamHub) Unsubscribe(client *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, client)
}

// UpdateFilter updates the filter for an existing client.
func (h *ActivityStreamHub) UpdateFilter(client *wsClient, filter ActivityFilter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.clients[client]; exists {
		h.clients[client] = filter
	}
}

// ClientCount returns the number of subscribed clients.
func (h *ActivityStreamHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ServeActivityStream handles WebSocket connections for activity streaming.
func (h *WsHub) ServeActivityStream(w http.ResponseWriter, r *http.Request) {
	// This method should be called from the HTTP handler
	// For now, it's a placeholder that delegates to ServeWS
	h.ServeWS(w, r)
}

// HandleActivityMessage processes incoming WebSocket messages for activity stream.
func (h *ActivityStreamHub) HandleActivityMessage(client *wsClient, data []byte) {
	var msg struct {
		Type   string         `json:"type"`
		Filter ActivityFilter `json:"filter,omitempty"`
	}

	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	switch msg.Type {
	case MsgSubscribe:
		h.Subscribe(client, msg.Filter)
	case MsgUnsubscribe:
		h.Unsubscribe(client)
	case MsgFilter:
		h.UpdateFilter(client, msg.Filter)
	case MsgPing:
		// Send pong response
		pong := WsMessage{
			Type:    MsgPong,
			Payload: map[string]interface{}{"time": timestampMs()},
			Time:    timestampMs(),
		}
		select {
		case client.send <- pong:
		default:
		}
	}
}

func timestampMs() int64 {
	return time.Now().UnixMilli()
}
