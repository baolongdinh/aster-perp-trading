// Package risk provides trading risk controls.
package risk

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/strategy/regime"
	"go.uber.org/zap"
)


// Manager enforces trading risk rules.
type Manager struct {
	cfg config.RiskConfig
	log *zap.Logger

	mu                  sync.Mutex
	dailyPnL            float64            // running daily P&L
	dailyPnLReset       time.Time          // reset timestamp
	dailyStartingEquity float64            // equity at the start of the day
	openPositions       int                // current open total position count
	symPositions        map[string]int     // symbol -> open position count
	lastCumulativePnL   map[string]float64 // symbol -> last known total realized PnL
	pendingOrders       map[string]float64 // symbol -> total pending notional (NEW/PARTIALLY_FILLED)
	paused              bool               // true when daily loss limit hit
	corrTracker         *regime.CorrelationTracker
}

func (m *Manager) SetCorrelationTracker(t *regime.CorrelationTracker) {
	m.mu.Lock()
	m.corrTracker = t
	m.mu.Unlock()
}

// PersistentState defines the risk data saved to disk.
type PersistentState struct {
	DailyPnL            float64            `json:"daily_pnl"`
	DailyStartingEquity float64            `json:"daily_starting_equity"`
	DailyPnLReset       time.Time          `json:"daily_pnl_reset"`
	LastCumulativePnL   map[string]float64 `json:"last_cumulative_pnl"`
	Paused              bool               `json:"paused"`
}


// NewManager creates a new risk manager.
func NewManager(cfg config.RiskConfig, log *zap.Logger) *Manager {
	return &Manager{
		cfg:               cfg,
		log:               log,
		dailyPnLReset:     todayUTC(),
		symPositions:      make(map[string]int),
		pendingOrders:     make(map[string]float64),
		lastCumulativePnL: make(map[string]float64),
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

	// Correlation Check
	if m.corrTracker != nil {
		var active []string
		for sym, count := range m.symPositions {
			if count > 0 {
				active = append(active, sym)
			}
		}
		
		highlyCorr := m.corrTracker.GetHighlyCorrelated(symbol, active)
		if len(highlyCorr) > 0 {
			return fmt.Errorf("risk: correlation limit — %s highly correlated with existing position(s) %v", symbol, highlyCorr)
		}
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

	// Calculate delta PnL to avoid using cumulative account totals
	last := m.lastCumulativePnL[symbol]
	delta := realizedPnL - last
	m.dailyPnL += delta
	m.lastCumulativePnL[symbol] = realizedPnL

	if delta != 0 {
		m.log.Info("risk: position pnl recorded",
			zap.String("symbol", symbol),
			zap.Float64("delta", delta),
			zap.Float64("daily_pnl", m.dailyPnL),
		)
	}
	
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

	// Save state after PnL update
	m.saveState()
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
// If atr is > 0, it uses ATR-based stop loss (preferred).
func (m *Manager) StopLossPrice(entryPrice float64, side string, atr float64) float64 {
	if atr > 0 {
		mult := m.cfg.ATRMultiplier
		if mult <= 0 {
			mult = 2.0
		}
		if side == "BUY" {
			return entryPrice - (atr * mult)
		}
		return entryPrice + (atr * mult)
	}

	pct := m.cfg.PerTradeStopLossPct / 100.0
	if side == "BUY" {
		return entryPrice * (1 - pct)
	}
	return entryPrice * (1 + pct)
}

// TakeProfitPrice calculates the TP price given entry and side.
// reg is the current market regime from strategy/regime package.
func (m *Manager) TakeProfitPrice(entryPrice float64, side string, slPrice float64, reg string) float64 {
	dist := math.Abs(entryPrice - slPrice)
	rr := 1.5 // Default RR

	// Dynamic RR based on Regime
	switch regime.RegimeType(reg) {
	case regime.RegimeTrend:
		rr = 2.5 // Aim higher in trends
	case regime.RegimeRanging:
		rr = 1.2 // Take profits earlier in ranges
	case regime.RegimeBreakout:
		rr = 2.0 // Decent follow-through expected
	}

	if side == "BUY" {
		return entryPrice + (dist * rr)
	}
	return entryPrice - (dist * rr)
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
		m.dailyStartingEquity = 0 // will be re-set by next account update
		m.paused = false
		m.dailyPnLReset = now
		m.lastCumulativePnL = make(map[string]float64) // reset cumulative deltas for the new day
		m.log.Info("risk: daily P&L reset")
		m.saveState()
	}
}

const stateFile = "bot_state.json"

// LoadState reads the risk state from disk.
func (m *Manager) LoadState() {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			m.log.Warn("risk: could not read state file", zap.Error(err))
		}
		return
	}

	var s PersistentState
	if err := json.Unmarshal(data, &s); err != nil {
		m.log.Warn("risk: could not decode state file", zap.Error(err))
		return
	}

	// Only load if the reset date is still valid (same day)
	if !todayUTC().After(s.DailyPnLReset) {
		m.dailyPnL = s.DailyPnL
		m.dailyStartingEquity = s.DailyStartingEquity
		m.dailyPnLReset = s.DailyPnLReset
		m.paused = s.Paused
		if s.LastCumulativePnL != nil {
			m.lastCumulativePnL = s.LastCumulativePnL
		}
		m.log.Info("risk: state loaded from disk",
			zap.Float64("daily_pnl", m.dailyPnL),
			zap.Bool("paused", m.paused),
		)
	} else {
		m.log.Info("risk: stale state file on disk ignored (different day)")
	}
}

func (m *Manager) saveState() {
	// Assumes lock is already held by caller if called internally, 
	// but actually this is called from OnPositionClosed and maybeDailyReset.
	// Let's make it safe or assume caller manages it.
	// Given current design, let's just make it a helper that takes what it needs.
	
	s := PersistentState{
		DailyPnL:            m.dailyPnL,
		DailyStartingEquity: m.dailyStartingEquity,
		DailyPnLReset:       m.dailyPnLReset,
		LastCumulativePnL:   m.lastCumulativePnL,
		Paused:              m.paused,
	}

	data, _ := json.MarshalIndent(s, "", "  ")
	
	// Write to temp file first then rename for atomic safety
	tmp := stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		m.log.Warn("risk: could not write temp state file", zap.Error(err))
		return
	}
	if err := os.Rename(tmp, stateFile); err != nil {
		m.log.Warn("risk: could not rename state file", zap.Error(err))
	}
}


func todayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}
