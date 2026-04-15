package sync

import (
	"math"
	"sync"
	"time"

	"aster-bot/internal/client"

	"go.uber.org/zap"
)

// PositionSyncWorker syncs position state between internal and exchange
type PositionSyncWorker struct {
	wsClient           *client.WebSocketClient
	internalPositions  map[string]client.Position // symbol -> position
	mu                 sync.RWMutex
	interval           time.Duration
	logger             *zap.Logger
	onCriticalMismatch func(symbol string, mismatchType string, details map[string]interface{})
}

// NewPositionSyncWorker creates a new position sync worker
func NewPositionSyncWorker(
	wsClient *client.WebSocketClient,
	interval time.Duration,
	logger *zap.Logger,
) *PositionSyncWorker {
	if interval == 0 {
		interval = 5 * time.Second
	}

	return &PositionSyncWorker{
		wsClient:          wsClient,
		internalPositions: make(map[string]client.Position),
		interval:          interval,
		logger:            logger.With(zap.String("worker", "position_sync")),
	}
}

// SetOnCriticalMismatchCallback sets callback for critical mismatch handling
func (w *PositionSyncWorker) SetOnCriticalMismatchCallback(fn func(symbol string, mismatchType string, details map[string]interface{})) {
	w.onCriticalMismatch = fn
}

// UpdateInternalPosition updates internal position state
func (w *PositionSyncWorker) UpdateInternalPosition(position client.Position) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.internalPositions[position.Symbol] = position
}

// RemoveInternalPosition removes a position from internal state
func (w *PositionSyncWorker) RemoveInternalPosition(symbol string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.internalPositions, symbol)
}

// Run starts the sync worker loop
func (w *PositionSyncWorker) Run(stopCh <-chan struct{}) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.sync()
		case <-stopCh:
			w.logger.Info("Position sync worker stopped")
			return
		}
	}
}

// sync performs one sync iteration
func (w *PositionSyncWorker) sync() {
	// Get internal positions
	w.mu.RLock()
	symbols := make([]string, 0, len(w.internalPositions))
	for symbol := range w.internalPositions {
		symbols = append(symbols, symbol)
	}
	w.mu.RUnlock()

	// Get exchange positions from WebSocket
	exchangePositions := w.wsClient.GetCachedPositions()

	// Check each internal position
	for _, symbol := range symbols {
		w.syncPosition(symbol, exchangePositions)
	}

	// Check for unknown positions (exchange has, we don't)
	for symbol, extPos := range exchangePositions {
		w.mu.RLock()
		_, exists := w.internalPositions[symbol]
		w.mu.RUnlock()

		if !exists && extPos.PositionAmt != 0 {
			w.logger.Info("Unknown position on exchange, syncing to internal",
				zap.String("symbol", symbol),
				zap.Float64("size", extPos.PositionAmt),
				zap.String("side", extPos.PositionSide))

			w.UpdateInternalPosition(extPos)
		}
	}
}

// syncPosition syncs a single position
func (w *PositionSyncWorker) syncPosition(symbol string, exchangePositions map[string]client.Position) {
	w.mu.RLock()
	intPos, exists := w.internalPositions[symbol]
	w.mu.RUnlock()

	if !exists {
		return
	}

	extPos, exists := exchangePositions[symbol]
	if !exists || extPos.PositionAmt == 0 {
		// Position no longer exists on exchange
		if intPos.PositionAmt != 0 {
			w.logger.Info("Position closed on exchange, updating internal",
				zap.String("symbol", symbol),
				zap.Float64("internal_size", intPos.PositionAmt))

			// Update internal to reflect closed position
			intPos.PositionAmt = 0
			w.UpdateInternalPosition(intPos)
		}
		return
	}

	// Check size mismatch (> 0.001 threshold)
	sizeDiff := math.Abs(intPos.PositionAmt - extPos.PositionAmt)
	if sizeDiff > 0.001 {
		w.logger.Warn("Position size mismatch, syncing from exchange",
			zap.String("symbol", symbol),
			zap.Float64("internal_size", intPos.PositionAmt),
			zap.Float64("exchange_size", extPos.PositionAmt),
			zap.Float64("diff", sizeDiff))

		w.UpdateInternalPosition(extPos)
	}

	// Check side mismatch (CRITICAL)
	if intPos.PositionSide != extPos.PositionSide && intPos.PositionAmt != 0 && extPos.PositionAmt != 0 {
		w.logger.Error("CRITICAL: Position side mismatch detected!",
			zap.String("symbol", symbol),
			zap.String("internal_side", intPos.PositionSide),
			zap.String("exchange_side", extPos.PositionSide))

		if w.onCriticalMismatch != nil {
			w.onCriticalMismatch(symbol, "SIDE_MISMATCH", map[string]interface{}{
				"internal_side": intPos.PositionSide,
				"exchange_side": extPos.PositionSide,
				"internal_size": intPos.PositionAmt,
				"exchange_size": extPos.PositionAmt,
			})
		}

		// Sync from exchange (ground truth)
		w.UpdateInternalPosition(extPos)
	}

	// Sync PnL for reporting (non-critical)
	if intPos.UnrealizedProfit != extPos.UnrealizedProfit {
		intPos.UnrealizedProfit = extPos.UnrealizedProfit
		w.UpdateInternalPosition(intPos)
	}
}

// GetInternalPosition returns a copy of internal position state
func (w *PositionSyncWorker) GetInternalPosition(symbol string) (client.Position, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	pos, exists := w.internalPositions[symbol]
	return pos, exists
}

// GetAllInternalPositions returns all internal positions
func (w *PositionSyncWorker) GetAllInternalPositions() map[string]client.Position {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make(map[string]client.Position)
	for symbol, pos := range w.internalPositions {
		result[symbol] = pos
	}
	return result
}
