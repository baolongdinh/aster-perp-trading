package sync

import (
	"sync"
	"time"

	"aster-bot/internal/client"
	"go.uber.org/zap"
)

// BalanceSyncWorker syncs balance state between internal and exchange
type BalanceSyncWorker struct {
	wsClient           *client.WebSocketClient
	internalBalance    client.Balance
	mu                 sync.RWMutex
	interval           time.Duration
	logger             *zap.Logger
	onLowBalance       func(available float64, required float64)
	lowBalanceThreshold float64
}

// NewBalanceSyncWorker creates a new balance sync worker
func NewBalanceSyncWorker(
	wsClient *client.WebSocketClient,
	interval time.Duration,
	logger *zap.Logger,
) *BalanceSyncWorker {
	if interval == 0 {
		interval = 5 * time.Second
	}

	return &BalanceSyncWorker{
		wsClient:            wsClient,
		interval:            interval,
		logger:              logger.With(zap.String("worker", "balance_sync")),
		lowBalanceThreshold: 100.0, // $100 USDT default threshold
	}
}

// SetOnLowBalanceCallback sets callback for low balance alert
func (w *BalanceSyncWorker) SetOnLowBalanceCallback(fn func(available float64, required float64)) {
	w.onLowBalance = fn
}

// SetLowBalanceThreshold sets the threshold for low balance alerts
func (w *BalanceSyncWorker) SetLowBalanceThreshold(threshold float64) {
	w.lowBalanceThreshold = threshold
}

// UpdateInternalBalance updates internal balance state
func (w *BalanceSyncWorker) UpdateInternalBalance(balance client.Balance) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.internalBalance = balance
}

// GetInternalBalance returns a copy of internal balance state
func (w *BalanceSyncWorker) GetInternalBalance() client.Balance {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.internalBalance
}

// GetAvailableBalance returns available balance
func (w *BalanceSyncWorker) GetAvailableBalance() float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.internalBalance.AvailableBalance
}

// Run starts the sync worker loop
func (w *BalanceSyncWorker) Run(stopCh <-chan struct{}) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.sync()
		case <-stopCh:
			w.logger.Info("Balance sync worker stopped")
			return
		}
	}
}

// sync performs one sync iteration
func (w *BalanceSyncWorker) sync() {
	// Get balance from WebSocket cache
	exchangeBalance := w.wsClient.GetCachedBalance()

	w.mu.Lock()
	prevBalance := w.internalBalance
	w.internalBalance = exchangeBalance
	w.mu.Unlock()

	// Log significant changes
	if prevBalance.AvailableBalance != exchangeBalance.AvailableBalance {
		diff := exchangeBalance.AvailableBalance - prevBalance.AvailableBalance
		w.logger.Info("Balance updated",
			zap.Float64("available", exchangeBalance.AvailableBalance),
			zap.Float64("previous", prevBalance.AvailableBalance),
			zap.Float64("change", diff))
	}

	// Check for low balance
	if exchangeBalance.AvailableBalance < w.lowBalanceThreshold {
		w.logger.Warn("Low balance alert",
			zap.Float64("available", exchangeBalance.AvailableBalance),
			zap.Float64("threshold", w.lowBalanceThreshold))

		if w.onLowBalance != nil {
			w.onLowBalance(exchangeBalance.AvailableBalance, w.lowBalanceThreshold)
		}
	}
}

// CheckMarginSufficient checks if available margin is sufficient for required amount
func (w *BalanceSyncWorker) CheckMarginSufficient(required float64) bool {
	available := w.GetAvailableBalance()
	return available >= required
}
