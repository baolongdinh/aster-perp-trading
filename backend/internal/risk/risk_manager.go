// Package risk provides trading risk controls.
package risk

import (
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/config"
	"go.uber.org/zap"
)

// Manager enforces trading risk rules.
type Manager struct {
	cfg config.RiskConfig
	log *zap.Logger

	mu             sync.Mutex
	dailyPnL       float64    // running daily P&L
	dailyPnLReset  time.Time  // reset timestamp
	openPositions  int        // current open position count
	paused         bool       // true when daily loss limit hit
}

// NewManager creates a new risk manager.
func NewManager(cfg config.RiskConfig, log *zap.Logger) *Manager {
	return &Manager{
		cfg:           cfg,
		log:           log,
		dailyPnLReset: todayUTC(),
	}
}

// CanEnter checks if a new position can be opened given current risk state.
// Returns an error describing why the trade is blocked, or nil if allowed.
func (m *Manager) CanEnter(symbol string, notionalUSDT float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.maybeDailyReset()

	if m.paused {
		return fmt.Errorf("risk: bot paused — daily loss limit reached (%.2f USDT)", m.cfg.DailyLossLimitUSDT)
	}
	if m.openPositions >= m.cfg.MaxOpenPositions {
		return fmt.Errorf("risk: max open positions reached (%d)", m.cfg.MaxOpenPositions)
	}
	if notionalUSDT > m.cfg.MaxPositionUSDT {
		return fmt.Errorf("risk: requested notional %.2f USDT exceeds max %.2f USDT", notionalUSDT, m.cfg.MaxPositionUSDT)
	}
	return nil
}

// OnPositionOpened records that a new position was opened.
func (m *Manager) OnPositionOpened() {
	m.mu.Lock()
	m.openPositions++
	m.mu.Unlock()
}

// OnPositionClosed records that a position was closed with some realised PnL.
func (m *Manager) OnPositionClosed(realizedPnL float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maybeDailyReset()
	m.openPositions--
	if m.openPositions < 0 {
		m.openPositions = 0
	}
	m.dailyPnL += realizedPnL
	if m.dailyPnL <= -m.cfg.DailyLossLimitUSDT {
		m.paused = true
		m.log.Warn("risk: daily loss limit hit, bot paused",
			zap.Float64("daily_pnl", m.dailyPnL),
			zap.Float64("limit", m.cfg.DailyLossLimitUSDT),
		)
	}
}

// StopLossPrice calculates the SL price given entry and side.
func (m *Manager) StopLossPrice(entryPrice float64, side string) float64 {
	pct := m.cfg.PerTradeStopLossPct / 100.0
	if side == "BUY" {
		return entryPrice * (1 - pct)
	}
	return entryPrice * (1 + pct)
}

// DailyPnL returns the current daily P&L.
func (m *Manager) DailyPnL() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dailyPnL
}

// IsPaused returns true if the bot is paused due to risk limits.
func (m *Manager) IsPaused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused
}

// Resume manually resumes the bot (e.g. at start of new day).
func (m *Manager) Resume() {
	m.mu.Lock()
	m.paused = false
	m.mu.Unlock()
}

// OpenPositions returns the current tracked open position count.
func (m *Manager) OpenPositions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openPositions
}

// SetOpenPositions sets the open positions count (e.g. after reconcile).
func (m *Manager) SetOpenPositions(n int) {
	m.mu.Lock()
	m.openPositions = n
	m.mu.Unlock()
}

func (m *Manager) maybeDailyReset() {
	now := todayUTC()
	if now.After(m.dailyPnLReset) {
		m.dailyPnL = 0
		m.paused = false
		m.dailyPnLReset = now
		m.log.Info("risk: daily P&L reset")
	}
}

func todayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}
