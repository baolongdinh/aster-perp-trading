package adaptive_grid

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OrderLockManager manages per-symbol order processing locks
type OrderLockManager struct {
	locks   map[string]*orderLock
	mu      sync.RWMutex
	logger  *zap.Logger
	timeout time.Duration
}

// orderLock represents a lock for a specific symbol
type orderLock struct {
	mu       sync.Mutex
	locked   bool
	lockedAt time.Time
	owner    string // identifier for debugging
}

// NewOrderLockManager creates a new order lock manager
func NewOrderLockManager(logger *zap.Logger) *OrderLockManager {
	return &OrderLockManager{
		locks:   make(map[string]*orderLock),
		logger:  logger,
		timeout: 5 * time.Second,
	}
}

// LockOrderProcessing acquires a lock for symbol processing
// Returns true if lock acquired, false if already locked
func (m *OrderLockManager) LockOrderProcessing(symbol string, owner string) bool {
	m.mu.Lock()
	lock, exists := m.locks[symbol]
	if !exists {
		lock = &orderLock{}
		m.locks[symbol] = lock
	}
	m.mu.Unlock()

	// Try to acquire lock with timeout
	done := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("Lock acquisition goroutine panic recovered",
					zap.String("symbol", symbol),
					zap.Any("panic", r))
				done <- false
			}
		}()
		lock.mu.Lock()
		if !lock.locked {
			lock.locked = true
			lock.lockedAt = time.Now()
			lock.owner = owner
			done <- true
		} else {
			done <- false
		}
	}()

	select {
	case acquired := <-done:
		if acquired {
			m.logger.Debug("Order processing lock acquired",
				zap.String("symbol", symbol),
				zap.String("owner", owner))
		}
		return acquired
	case <-time.After(m.timeout):
		// Timeout - check if lock is stale
		if m.IsStaleLock(symbol) {
			m.ForceUnlock(symbol)
			return m.LockOrderProcessing(symbol, owner+"-retry")
		}
		m.logger.Warn("Order processing lock timeout",
			zap.String("symbol", symbol),
			zap.String("owner", owner),
			zap.Duration("timeout", m.timeout))
		return false
	}
}

// UnlockOrderProcessing releases the lock for a symbol
func (m *OrderLockManager) UnlockOrderProcessing(symbol string) {
	m.mu.RLock()
	lock, exists := m.locks[symbol]
	m.mu.RUnlock()

	if !exists {
		return
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()

	if lock.locked {
		lock.locked = false
		lock.owner = ""
		m.logger.Debug("Order processing lock released",
			zap.String("symbol", symbol))
	}
}

// IsLocked checks if a symbol is currently locked
func (m *OrderLockManager) IsLocked(symbol string) bool {
	m.mu.RLock()
	lock, exists := m.locks[symbol]
	m.mu.RUnlock()

	if !exists {
		return false
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()
	return lock.locked
}

// IsStaleLock checks if a lock has been held too long
func (m *OrderLockManager) IsStaleLock(symbol string) bool {
	m.mu.RLock()
	lock, exists := m.locks[symbol]
	m.mu.RUnlock()

	if !exists {
		return false
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()

	if !lock.locked {
		return false
	}

	return time.Since(lock.lockedAt) > m.timeout
}

// ForceUnlock forcibly releases a lock
func (m *OrderLockManager) ForceUnlock(symbol string) {
	m.mu.RLock()
	lock, exists := m.locks[symbol]
	m.mu.RUnlock()

	if !exists {
		return
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()

	if lock.locked {
		m.logger.Warn("Force unlocking stale lock",
			zap.String("symbol", symbol),
			zap.String("owner", lock.owner),
			zap.Time("locked_at", lock.lockedAt))
		lock.locked = false
		lock.owner = ""
	}
}

// GetLockInfo returns information about a lock
func (m *OrderLockManager) GetLockInfo(symbol string) map[string]interface{} {
	m.mu.RLock()
	lock, exists := m.locks[symbol]
	m.mu.RUnlock()

	if !exists {
		return map[string]interface{}{
			"symbol": symbol,
			"exists": false,
			"locked": false,
		}
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()

	return map[string]interface{}{
		"symbol":    symbol,
		"exists":    true,
		"locked":    lock.locked,
		"owner":     lock.owner,
		"locked_at": lock.lockedAt,
		"duration":  time.Since(lock.lockedAt).String(),
		"stale":     time.Since(lock.lockedAt) > m.timeout,
	}
}

// CleanupStaleLocks removes all stale locks
func (m *OrderLockManager) CleanupStaleLocks() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleaned := 0
	for symbol, lock := range m.locks {
		lock.mu.Lock()
		if lock.locked && time.Since(lock.lockedAt) > m.timeout*2 {
			m.logger.Warn("Cleaning up stale lock",
				zap.String("symbol", symbol),
				zap.String("owner", lock.owner))
			lock.locked = false
			lock.owner = ""
			cleaned++
		}
		lock.mu.Unlock()
	}

	return cleaned
}

// WithLock executes a function with lock protection
func (m *OrderLockManager) WithLock(symbol string, owner string, fn func() error) error {
	if !m.LockOrderProcessing(symbol, owner) {
		return fmt.Errorf("failed to acquire lock for %s", symbol)
	}
	defer m.UnlockOrderProcessing(symbol)

	return fn()
}
