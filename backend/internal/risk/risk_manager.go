// Package risk provides trading risk controls.
package risk

import (
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"go.uber.org/zap"
)


// Manager enforces trading risk rules.
type Manager struct {
	cfg config.RiskConfig
	log *zap.Logger

	mu             sync.Mutex
	dailyPnL       float64           // running daily P&L
	dailyPnLReset  time.Time         // reset timestamp
	dailyStartingEquity float64      // equity at the start of the day
	openPositions  int               // current open total position count
	symPositions   map[string]int    // symbol -> open position count
	pendingOrders  map[string]float64 // symbol -> total pending notional (NEW/PARTIALLY_FILLED)
	paused         bool              // true when daily loss limit hit
}


// NewManager creates a new risk manager.
func NewManager(cfg config.RiskConfig, log *zap.Logger) *Manager {
	return &Manager{
		cfg:           cfg,
		log:           log,
		dailyPnLReset: todayUTC(),
		symPositions:  make(map[string]int),
		pendingOrders: make(map[string]float64),
	}
}


// CanEnter checks if a new position can be opened given current risk state.
// Returns an error describing why the trade is blocked, or nil if allowed.
// NOTE: Pending limit order slot enforcement is handled separately in the Engine.
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

	return nil
}

// AddPending records a new pending order's notional.
func (m *Manager) AddPending(symbol string, notional float64) {
	m.mu.Lock()
	m.pendingOrders[symbol] += notional
	m.mu.Unlock()
}

// RemovePending removes a pending order's notional.
func (m *Manager) RemovePending(symbol string, notional float64) {
	m.mu.Lock()
	m.pendingOrders[symbol] -= notional
	if m.pendingOrders[symbol] < 0 {
		m.pendingOrders[symbol] = 0
	}
	m.mu.Unlock()
}

// OnPositionOpened records that a new position was opened.
func (m *Manager) OnPositionOpened(symbol string) {
	m.mu.Lock()
	m.openPositions++
	m.symPositions[symbol]++
	m.mu.Unlock()
}


// OnPositionClosed records that a position was closed with some realised PnL.
func (m *Manager) OnPositionClosed(symbol string, realizedPnL float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maybeDailyReset()

	m.openPositions--
	if m.openPositions < 0 {
		m.openPositions = 0
	}

	m.symPositions[symbol]--
	if m.symPositions[symbol] < 0 {
		m.symPositions[symbol] = 0
	}

	m.dailyPnL += realizedPnL
	
	// 1. ABSOLUTE LIMIT
	if m.dailyPnL <= -m.cfg.DailyLossLimitUSDT {
		m.paused = true
		m.log.Warn("risk: absolute daily loss limit hit, bot paused",
			zap.Float64("daily_pnl", m.dailyPnL),
			zap.Float64("limit", m.cfg.DailyLossLimitUSDT),
		)
	}

	// 2. PERCENTAGE LIMIT (Drawdown)
	if m.dailyStartingEquity > 0 {
		drawdown := (m.dailyPnL / m.dailyStartingEquity) * 100
		if drawdown <= -m.cfg.DailyDrawdownPct {
			m.paused = true
			m.log.Warn("risk: daily drawdown percentage hit, bot paused",
				zap.Float64("drawdown_pct", drawdown),
				zap.Float64("limit_pct", m.cfg.DailyDrawdownPct),
			)
		}
	}
}

// CalculatePositionSize returns the recommended notional size in USDT based on ATR and capital risk.
func (m *Manager) CalculatePositionSize(symbol string, price float64, atr float64) (float64, error) {
	if atr <= 0 {
		return 0, fmt.Errorf("invalid ATR (warming up?)")
	}

	// Risk per unit = ATR * Multiplier
	riskPerUnit := atr * m.cfg.ATRMultiplier
	if riskPerUnit <= 0 {
		return 0, fmt.Errorf("invalid risk per unit calculation")
	}

	// Quantity = DollarRisk / RiskPerUnit
	qty := m.cfg.RiskPerTradeUSDT / riskPerUnit
	
	// Final size in notional
	notional := qty * price

	// Check against max position size limit
	if notional > m.cfg.MaxPositionUSDT {
		notional = m.cfg.MaxPositionUSDT
		m.log.Debug("risk: capping notional due to max_position_usdt", zap.String("symbol", symbol))
	}

	return notional, nil
}

// SetInitialEquity sets the starting equity for drawdown calculations.
func (m *Manager) SetInitialEquity(equity float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dailyStartingEquity == 0 {
		m.dailyStartingEquity = equity
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

// TakeProfitPrice calculates the TP price given entry and side.
func (m *Manager) TakeProfitPrice(entryPrice float64, side string) float64 {
	pct := m.cfg.PerTradeTakeProfitPct / 100.0
	if pct == 0 {
		// Fallback to 1.5x Risk-Reward if not explicitly set
		pct = (m.cfg.PerTradeStopLossPct * 1.5) / 100.0
	}
	if side == "BUY" {
		return entryPrice * (1 + pct)
	}
	return entryPrice * (1 - pct)
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
func (m *Manager) SetOpenPositions(positions map[string]*client.Position) {
	m.mu.Lock()
	m.openPositions = len(positions)
	m.symPositions = make(map[string]int)
	for sym := range positions {
		m.symPositions[sym] = 1
	}
	m.mu.Unlock()
}


func (m *Manager) maybeDailyReset() {
	now := todayUTC()
	if now.After(m.dailyPnLReset) {
		m.dailyPnL = 0
		m.dailyStartingEquity = 0 // will be re-set by next account update
		m.paused = false
		m.dailyPnLReset = now
		m.log.Info("risk: daily P&L reset")
	}
}


func todayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}
