package sync

import (
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/client"

	"go.uber.org/zap"
)

// OrderSyncWorker syncs order state between internal and exchange
type OrderSyncWorker struct {
	wsClient          *client.WebSocketClient
	internalOrders    map[string]map[int64]client.Order // symbol -> orderID -> order
	mu                sync.RWMutex
	interval          time.Duration
	mismatchThreshold time.Duration
	logger            *zap.Logger
	onMismatch        func(symbol string, mismatches []OrderMismatch)
	onOrderMissing    func(symbol string, orderID int64) // Callback when order is missing (filled/cancelled)
}

// OrderMismatch represents an order state mismatch
type OrderMismatch struct {
	Type     string // "missing", "unknown", "status_diff"
	OrderID  string
	Internal interface{}
	Exchange interface{}
	Severity string // "warning", "critical"
}

// NewOrderSyncWorker creates a new order sync worker
func NewOrderSyncWorker(
	wsClient *client.WebSocketClient,
	interval time.Duration,
	logger *zap.Logger,
) *OrderSyncWorker {
	if interval == 0 {
		interval = 5 * time.Second
	}

	return &OrderSyncWorker{
		wsClient:          wsClient,
		internalOrders:    make(map[string]map[int64]client.Order),
		interval:          interval,
		mismatchThreshold: 10 * time.Second,
		logger:            logger.With(zap.String("worker", "order_sync")),
	}
}

// SetOnMismatchCallback sets callback for mismatch handling
func (w *OrderSyncWorker) SetOnMismatchCallback(fn func(symbol string, mismatches []OrderMismatch)) {
	w.onMismatch = fn
}

// SetOnOrderMissingCallback sets callback when order is missing (filled/cancelled)
func (w *OrderSyncWorker) SetOnOrderMissingCallback(fn func(symbol string, orderID int64)) {
	w.onOrderMissing = fn
}

// UpdateInternalOrder updates internal order state
func (w *OrderSyncWorker) UpdateInternalOrder(symbol string, order client.Order) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.internalOrders[symbol]; !exists {
		w.internalOrders[symbol] = make(map[int64]client.Order)
	}
	w.internalOrders[symbol][order.OrderID] = order
}

// RemoveInternalOrder removes an order from internal state
func (w *OrderSyncWorker) RemoveInternalOrder(symbol string, orderID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if symbolOrders, exists := w.internalOrders[symbol]; exists {
		delete(symbolOrders, orderID)
	}
}

// Run starts the sync worker loop
func (w *OrderSyncWorker) Run(stopCh <-chan struct{}) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.sync()
		case <-stopCh:
			w.logger.Info("Order sync worker stopped")
			return
		}
	}
}

// sync performs one sync iteration
func (w *OrderSyncWorker) sync() {
	// Get all symbols with internal orders
	w.mu.RLock()
	symbols := make([]string, 0, len(w.internalOrders))
	for symbol := range w.internalOrders {
		symbols = append(symbols, symbol)
	}
	w.mu.RUnlock()

	for _, symbol := range symbols {
		mismatches := w.syncSymbol(symbol)
		if len(mismatches) > 0 && w.onMismatch != nil {
			w.onMismatch(symbol, mismatches)
		}
	}
}

// syncSymbol syncs orders for a single symbol
func (w *OrderSyncWorker) syncSymbol(symbol string) []OrderMismatch {
	var mismatches []OrderMismatch

	// Get internal orders
	w.mu.RLock()
	internalOrders := make(map[int64]client.Order)
	if orders, exists := w.internalOrders[symbol]; exists {
		for id, order := range orders {
			internalOrders[id] = order
		}
	}
	w.mu.RUnlock()

	// Get exchange orders from WebSocket cache
	exchangeOrders := w.wsClient.GetCachedOrders(symbol)
	exchangeOrderMap := make(map[int64]client.Order)
	for _, order := range exchangeOrders {
		exchangeOrderMap[order.OrderID] = order
	}

	// Check for missing orders (we have, exchange doesn't)
	for id, intOrder := range internalOrders {
		if _, exists := exchangeOrderMap[id]; !exists {
			// Order was filled or cancelled externally
			mismatches = append(mismatches, OrderMismatch{
				Type:     "missing",
				OrderID:  fmt.Sprintf("%d", id),
				Internal: intOrder.Status,
				Exchange: "not_found",
				Severity: "warning",
			})

			w.logger.Info("Order missing on exchange, assuming filled/cancelled",
				zap.String("symbol", symbol),
				zap.Int64("order_id", id))

			// Notify GridManager to handle order fill
			if w.onOrderMissing != nil {
				w.onOrderMissing(symbol, id)
			}

			// Remove from internal state
			w.RemoveInternalOrder(symbol, id)
		}
	}

	// Check for unknown orders (exchange has, we don't)
	for id, extOrder := range exchangeOrderMap {
		if _, exists := internalOrders[id]; !exists {
			mismatches = append(mismatches, OrderMismatch{
				Type:     "unknown",
				OrderID:  fmt.Sprintf("%d", id),
				Internal: "not_found",
				Exchange: extOrder.Status,
				Severity: "warning",
			})

			w.logger.Info("Unknown order on exchange, adding to internal state",
				zap.String("symbol", symbol),
				zap.Int64("order_id", id))

			// Add to internal state
			w.UpdateInternalOrder(symbol, extOrder)
		}
	}

	// Check for status mismatches
	for id, intOrder := range internalOrders {
		if extOrder, exists := exchangeOrderMap[id]; exists {
			if intOrder.Status != extOrder.Status {
				mismatches = append(mismatches, OrderMismatch{
					Type:     "status_diff",
					OrderID:  fmt.Sprintf("%d", id),
					Internal: intOrder.Status,
					Exchange: extOrder.Status,
					Severity: "critical",
				})

				w.logger.Warn("Order status mismatch, syncing from exchange",
					zap.String("symbol", symbol),
					zap.Int64("order_id", id),
					zap.String("internal_status", intOrder.Status),
					zap.String("exchange_status", extOrder.Status))

				// Sync from exchange (exchange is ground truth)
				w.UpdateInternalOrder(symbol, extOrder)
			}
		}
	}

	return mismatches
}

// GetInternalOrders returns a copy of internal order state
func (w *OrderSyncWorker) GetInternalOrders(symbol string) map[int64]client.Order {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make(map[int64]client.Order)
	if orders, exists := w.internalOrders[symbol]; exists {
		for id, order := range orders {
			result[id] = order
		}
	}
	return result
}
