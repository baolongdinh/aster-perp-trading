package sync

import (
	"sync"

	"aster-bot/internal/client"

	"go.uber.org/zap"
)

// SyncManager coordinates all sync workers
type SyncManager struct {
	orderWorker    *OrderSyncWorker
	positionWorker *PositionSyncWorker
	balanceWorker  *BalanceSyncWorker

	wsClient *client.WebSocketClient
	logger   *zap.Logger

	stopCh  chan struct{}
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// NewSyncManager creates a new sync manager
func NewSyncManager(
	wsClient *client.WebSocketClient,
	logger *zap.Logger,
) *SyncManager {
	return &SyncManager{
		wsClient: wsClient,
		logger:   logger.With(zap.String("component", "sync_manager")),
		stopCh:   make(chan struct{}),
	}
}

// Initialize creates and initializes all sync workers
func (m *SyncManager) Initialize() {
	// Create order sync worker
	m.orderWorker = NewOrderSyncWorker(m.wsClient, 0, m.logger)
	m.orderWorker.SetOnMismatchCallback(func(symbol string, mismatches []OrderMismatch) {
		m.logger.Warn("Order mismatches detected",
			zap.String("symbol", symbol),
			zap.Int("count", len(mismatches)))
	})

	// Create position sync worker
	m.positionWorker = NewPositionSyncWorker(m.wsClient, 0, m.logger)
	m.positionWorker.SetOnCriticalMismatchCallback(func(symbol string, mismatchType string, details map[string]interface{}) {
		m.logger.Error("Critical position mismatch",
			zap.String("symbol", symbol),
			zap.String("type", mismatchType),
			zap.Any("details", details))
	})

	// Create balance sync worker
	m.balanceWorker = NewBalanceSyncWorker(m.wsClient, 0, m.logger)
}

// SetOnOrderMissingCallback sets callback when order is missing (filled/cancelled)
func (m *SyncManager) SetOnOrderMissingCallback(fn func(symbol string, orderID int64)) {
	if m.orderWorker != nil {
		m.orderWorker.SetOnOrderMissingCallback(fn)
	}
}

// Start starts all sync workers
func (m *SyncManager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	m.running = true
	m.stopCh = make(chan struct{})

	// Start order sync worker
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.orderWorker.Run(m.stopCh)
	}()

	// Start position sync worker
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.positionWorker.Run(m.stopCh)
	}()

	// Start balance sync worker
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.balanceWorker.Run(m.stopCh)
	}()

	m.logger.Info("Sync manager started all workers")
}

// Stop stops all sync workers
func (m *SyncManager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	close(m.stopCh)
	m.wg.Wait()

	m.logger.Info("Sync manager stopped all workers")
}

// IsRunning returns true if sync manager is running
func (m *SyncManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// GetOrderWorker returns the order sync worker
func (m *SyncManager) GetOrderWorker() *OrderSyncWorker {
	return m.orderWorker
}

// GetPositionWorker returns the position sync worker
func (m *SyncManager) GetPositionWorker() *PositionSyncWorker {
	return m.positionWorker
}

// GetBalanceWorker returns the balance sync worker
func (m *SyncManager) GetBalanceWorker() *BalanceSyncWorker {
	return m.balanceWorker
}

// UpdateOrder updates internal order state
func (m *SyncManager) UpdateOrder(symbol string, order client.Order) {
	if m.orderWorker != nil {
		m.orderWorker.UpdateInternalOrder(symbol, order)
	}
}

// UpdatePosition updates internal position state
func (m *SyncManager) UpdatePosition(position client.Position) {
	if m.positionWorker != nil {
		m.positionWorker.UpdateInternalPosition(position)
	}
}
