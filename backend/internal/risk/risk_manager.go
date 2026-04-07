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
	dailyPnL            float64            // running daily P&L (realized)
	dailyUnrealizedPnL  float64            // current unrealized P&L across all positions
	symUnrealizedPnL    map[string]float64 // symbol -> current unrealized PnL
	dailyPnLReset       time.Time          // reset timestamp
	dailyStartingEquity float64            // equity at the start of the day
	openPositions       int                // current open total position count
	symPositions        map[string]int     // symbol -> open position count
	symNotional         map[string]float64 // symbol -> open position notional (USDT)
	lastCumulativePnL   map[string]float64 // symbol -> last known total realized PnL
	pendingOrders       map[string]float64 // symbol -> total pending notional (NEW/PARTIALLY_FILLED)
	pendingMargin       float64            // total initial margin of pending orders (NEW/PARTIALLY_FILLED)
	availableBalance    float64            // current exchange available USDT balance
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
	DailyUnrealizedPnL  float64            `json:"daily_unrealized_pnl"`
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
		symNotional:       make(map[string]float64),
		symUnrealizedPnL:  make(map[string]float64),
		pendingOrders:     make(map[string]float64),
		lastCumulativePnL: make(map[string]float64),
	}
}

// CanEnter checks if a new position can be opened given current risk state.
// Returns an error describing why the trade is blocked, or nil if allowed.
// NOTE: Pending limit order slot enforcement is handled separately in the Engine.
func (m *Manager) CanEnter(symbol string, notional float64, leverage float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.maybeDailyReset()
	if m.paused {
		return fmt.Errorf("risk: bot paused — daily loss limit reached (%.2f USDT)", m.cfg.DailyLossLimitUSDT)
	}

	// 1. Max Global Open Positions
	if m.openPositions >= m.cfg.MaxOpenPositions {
		if m.symPositions[symbol] == 0 {
			return fmt.Errorf("risk: max open positions reached (%d)", m.cfg.MaxOpenPositions)
		}
	}

	// 1.5. Total Position Notional Limit
	totalNotional := 0.0
	for _, positionNotional := range m.symNotional {
		totalNotional += positionNotional
	}
	if m.cfg.MaxTotalPositionsUSDT > 0 && (totalNotional+notional) > m.cfg.MaxTotalPositionsUSDT {
		return fmt.Errorf("risk: total position notional limit would be exceeded (current: %.2f, new: %.2f, limit: %.2f)",
			totalNotional, notional, m.cfg.MaxTotalPositionsUSDT)
	}

	// 2. No Stacking Check: Prevent increasing existing positions or duplicate entries
	if m.symPositions[symbol] > 0 {
		return fmt.Errorf("risk: stacking blocked — %s already has an open position", symbol)
	}
	// We no longer check m.pendingOrders here. We rely on engine.VerifyNoStackingServer.

	// 3. Correlation Check
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

	// 4. Affordability check including OTHER pending margin
	if leverage <= 0 {
		leverage = 1.0
	}
	marginReq := notional / leverage
	usableBalance := m.availableBalance * 0.95 // 5% buffer

	if (marginReq + m.pendingMargin) > usableBalance {
		return fmt.Errorf("risk: insufficient balance after pending reserve (req:%.2f, pend_margin:%.2f, available:%.2f)",
			marginReq, m.pendingMargin, m.availableBalance)
	}

	// ATOMIC RESERVATION: Locking in the notional exposure and margin immediately will occur in the Engine via AddPending when the order actually fires.
	// We no longer mutate m.pendingOrders here to avoid memory leaks on filtered signals.
	return nil
}

// AddPending explicitely reserves notional/margin when an order is definitely sent to the exchange.
func (m *Manager) AddPending(symbol string, notional float64, leverage float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingOrders[symbol] += notional

	if leverage <= 0 {
		leverage = 20.0
	}
	m.pendingMargin += (notional / leverage)
}

// RemovePending removes a pending order's notional and margin.
func (m *Manager) RemovePending(symbol string, notional float64, leverage float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pendingOrders[symbol] -= notional
	if m.pendingOrders[symbol] < 0 {
		m.pendingOrders[symbol] = 0
	}

	if leverage <= 0 {
		leverage = 20.0 // default
	}
	m.pendingMargin -= (notional / leverage)
	if m.pendingMargin < 0 {
		m.pendingMargin = 0
	}
}

// SetPendingOrders synchronizes known pending state from Engine.
func (m *Manager) SetPendingOrders(notionals map[string]float64, totalMargin float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingOrders = notionals
	m.pendingMargin = totalMargin
}

// OnPositionOpened records that a new position was opened.
func (m *Manager) OnPositionOpened(symbol string, notionalUSDT float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openPositions++
	m.symPositions[symbol]++
	m.symNotional[symbol] += notionalUSDT
	m.pendingOrders[symbol] -= notionalUSDT
	if m.pendingOrders[symbol] < 0 {
		m.pendingOrders[symbol] = 0
	}
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

	// 2. PERCENTAGE LIMIT (Drawdown) - Corrected to include unrealized PnL
	if m.dailyStartingEquity > 0 {
		// Calculate total equity including both realized and unrealized PnL
		totalEquity := m.dailyStartingEquity + m.dailyPnL + m.dailyUnrealizedPnL
		drawdownPct := ((m.dailyStartingEquity - totalEquity) / m.dailyStartingEquity) * 100

		if drawdownPct >= m.cfg.DailyDrawdownPct {
			m.paused = true
			m.log.Warn("risk: daily drawdown percentage hit, bot paused",
				zap.Float64("drawdown_pct", drawdownPct),
				zap.Float64("limit_pct", m.cfg.DailyDrawdownPct),
				zap.Float64("starting_equity", m.dailyStartingEquity),
				zap.Float64("current_equity", totalEquity),
				zap.Float64("realized_pnl", m.dailyPnL),
				zap.Float64("unrealized_pnl", m.dailyUnrealizedPnL),
			)
		}
	}

	// Save state after PnL update
	m.saveState()
}

// CalculatePositionSize returns the recommended notional size in USDT based on the exact Stop Loss distance.
func (m *Manager) CalculatePositionSize(symbol string, entryPrice float64, slPrice float64) (float64, error) {
	var notional float64

	if slPrice > 0 && slPrice != entryPrice {
		// Risk per unit is the absolute price diff between entry and stop loss
		riskPerUnit := math.Abs(entryPrice - slPrice)

		// Quantity = DollarRisk / RiskPerUnit
		qty := m.cfg.RiskPerTradeUSDT / riskPerUnit

		// Final size in notional
		notional = qty * entryPrice
		m.log.Debug("IQ-RISK: SL-based sizing",
			zap.String("symbol", symbol),
			zap.Float64("notional", notional),
			zap.Float64("risk_distance", riskPerUnit),
			zap.Float64("target_risk_usd", m.cfg.RiskPerTradeUSDT))
	} else {
		// FALLBACK: Use %-based SL logic if no explicit valid SL was provided
		// Asset size = RiskPerTrade / SLPct
		slPct := m.cfg.PerTradeStopLossPct / 100.0
		if slPct <= 0 {
			slPct = 0.02 // Default 2%
		}
		notional = m.cfg.RiskPerTradeUSDT / slPct
		m.log.Info("IQ-RISK: Strategy SL missing - using fallback %-based sizing",
			zap.String("symbol", symbol),
			zap.Float64("notional", notional),
			zap.Float64("sl_pct", slPct*100),
		)
	}

	// Check against max position size limit (per-symbol if available, otherwise general)
	maxPositionLimit := m.cfg.MaxPositionUSDTPerSymbol
	if maxPositionLimit <= 0 {
		maxPositionLimit = m.cfg.MaxPositionUSDT
	}
	if notional > maxPositionLimit {
		m.log.Info("IQ-RISK: Capping notional size to max limit",
			zap.String("symbol", symbol),
			zap.Float64("requested_notional", notional),
			zap.Float64("max_limit", maxPositionLimit),
		)
		notional = maxPositionLimit
	}

	return notional, nil
}

// SetAvailableBalance updates the current available USDT balance from the exchange.
func (m *Manager) SetAvailableBalance(balance float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.availableBalance = balance
}

// CanAfford checks if the account has enough margin to cover the trade.
// RequiredMargin = Notional / Leverage.
// We apply a 5% safety buffer (only use 95% of available balance).
func (m *Manager) CanAfford(symbol string, notional float64, leverage float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if leverage <= 0 {
		leverage = 1.0
	}

	requiredMargin := notional / leverage
	usableBalance := m.availableBalance * 0.95 // 5% safety buffer

	if requiredMargin > usableBalance {
		return fmt.Errorf("risk: insufficient available balance for symbol %s (margin_req:%.2f, available:%.2f, usable:%.2f)",
			symbol, requiredMargin, m.availableBalance, usableBalance)
	}

	return nil
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
	if pct <= 0 {
		pct = 0.01 // Default 1% SL if unconfigured or 0
	}
	if side == "BUY" {
		return entryPrice * (1 - pct)
	}
	return entryPrice * (1 + pct)
}

// TakeProfitPrice calculates the TP price given entry and side.
// reg is the current market regime from strategy/regime package.
func (m *Manager) TakeProfitPrice(entryPrice float64, side string, slPrice float64, reg string) float64 {
	dist := math.Abs(entryPrice - slPrice)
	if dist <= 0 {
		// SL not yet calculated properly or invalid, fallback to fixed %
		dist = entryPrice * 0.01
	}
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

// UpdateUnrealizedPnL updates the unrealized PnL for a symbol and recalculates total
func (m *Manager) UpdateUnrealizedPnL(symbol string, unrealizedPnL float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update per-symbol unrealized PnL
	oldPnL := m.symUnrealizedPnL[symbol]
	m.symUnrealizedPnL[symbol] = unrealizedPnL

	// Recalculate total unrealized PnL
	m.dailyUnrealizedPnL = 0
	for _, pnl := range m.symUnrealizedPnL {
		m.dailyUnrealizedPnL += pnl
	}

	// Check drawdown immediately on significant unrealized loss changes
	if m.dailyStartingEquity > 0 && unrealizedPnL < oldPnL {
		totalEquity := m.dailyStartingEquity + m.dailyPnL + m.dailyUnrealizedPnL
		drawdownPct := ((m.dailyStartingEquity - totalEquity) / m.dailyStartingEquity) * 100

		if drawdownPct >= m.cfg.DailyDrawdownPct && !m.paused {
			m.paused = true
			m.log.Warn("risk: daily drawdown percentage hit during unrealized update, bot paused",
				zap.Float64("drawdown_pct", drawdownPct),
				zap.Float64("limit_pct", m.cfg.DailyDrawdownPct),
				zap.String("symbol", symbol),
				zap.Float64("symbol_unrealized", unrealizedPnL),
			)
			m.saveState()
		}
	}
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
	defer m.mu.Unlock()
	m.openPositions = len(positions)
	m.symPositions = make(map[string]int)
	m.symNotional = make(map[string]float64)
	for sym, pos := range positions {
		m.symPositions[sym] = 1
		m.symNotional[sym] = math.Abs(pos.PositionAmt) * pos.EntryPrice
	}
}

func (m *Manager) maybeDailyReset() {
	now := todayUTC()
	if now.After(m.dailyPnLReset) {
		m.dailyStartingEquity = 0 // will be re-set by next account update
		m.paused = false
		m.dailyPnLReset = now
		m.dailyPnL = 0
		m.dailyUnrealizedPnL = 0
		m.symUnrealizedPnL = make(map[string]float64)
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
		m.dailyUnrealizedPnL = s.DailyUnrealizedPnL
		m.dailyStartingEquity = s.DailyStartingEquity
		m.dailyPnLReset = s.DailyPnLReset
		m.paused = s.Paused
		if s.LastCumulativePnL != nil {
			m.lastCumulativePnL = s.LastCumulativePnL
		}
		m.log.Info("risk: state loaded from disk",
			zap.Float64("daily_pnl", m.dailyPnL),
			zap.Float64("daily_unrealized_pnl", m.dailyUnrealizedPnL),
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
		DailyUnrealizedPnL:  m.dailyUnrealizedPnL,
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
