package strategy

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/risk"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

// Router acts as the master strategy.
// It implements Strategy but delegates decisions to sub-strategies
// based on the current market regime.
type Router struct {
	classifiers map[string]*regime.Classifier
	strategies  map[string]Strategy // map sub-strategy name to instance
	activeSubs  []string            // which sub-strategies are enabled in config

	log  *zap.Logger
	cfg  RouterConfig
	risk *risk.Manager

	// positionOwner tracks which strategy entered a trade for a symbol.
	// Used for consensus exit logic.
	positionOwner map[string]string // symbol -> strategyName
	mu            sync.RWMutex
}

type RouterConfig struct {
	Enabled bool
	Symbols []string
}

func NewRouter(cfg RouterConfig, riskMgr *risk.Manager, log *zap.Logger) *Router {
	r := &Router{
		classifiers:   make(map[string]*regime.Classifier),
		strategies:    make(map[string]Strategy),
		activeSubs:    make([]string, 0),
		positionOwner: make(map[string]string),
		log:           log,
		cfg:           cfg,
		risk:          riskMgr,
	}
	// Pre-initialize for configured symbols
	for _, sym := range cfg.Symbols {
		r.classifiers[sym] = regime.NewClassifier(14, 20, 2.0)
	}
	return r
}

func (r *Router) getClassifier(symbol string) *regime.Classifier {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.classifiers[symbol]
	if !ok {
		c = regime.NewClassifier(14, 20, 2.0)
		r.classifiers[symbol] = c
	}
	return c
}

// GetATR returns the current ATR for a symbol (using default 14 period).
func (r *Router) GetATR(symbol string) float64 {
	return r.getClassifier(symbol).GetATR("5m", 14)
}

// Register adds a sub-strategy to the router.
func (r *Router) Register(s Strategy) {
	r.strategies[s.Name()] = s
	if s.IsEnabled() {
		r.activeSubs = append(r.activeSubs, s.Name())
	}
}

// Name returns the router name.
func (r *Router) Name() string { return "router_meta" }

// Symbols returns the master list of symbols watched.
func (r *Router) Symbols() []string { return r.cfg.Symbols }

func (r *Router) IsEnabled() bool   { return r.cfg.Enabled }
func (r *Router) SetEnabled(v bool) { r.cfg.Enabled = v }

// State returns the overarching regime state + details
func (r *Router) State(symbol string) string {
	r.mu.RLock()
	owner := r.positionOwner[symbol]
	r.mu.RUnlock()

	cf := r.getClassifier(symbol)
	reg, adx, bbw := cf.Current()
	bias := cf.HTFTrendBias()
	squeeze := cf.IsSqueezing()

	biasStr := "Neutral"
	if bias > 0 {
		biasStr = "Bullish (1h)"
	} else if bias < 0 {
		biasStr = "Bearish (1h)"
	}

	res := fmt.Sprintf("[%s] ADX:%.1f | 1h:%s", reg, adx, biasStr)
	if squeeze {
		res += " | [SQUEEZE]"
	}
	res += fmt.Sprintf(" | BBW:%.2f%%", bbw)

	// Append active strategies and their current conditions
	candidates := r.getCandidates(reg)
	if len(candidates) > 0 {
		res += " | Active: ["
		for i, name := range candidates {
			s, ok := r.strategies[name]
			if !ok || !s.IsEnabled() {
				continue
			}
			res += name
			if i < len(candidates)-1 {
				res += " "
			}
		}
		res += "]"

		// Detailed sub-state of the primary candidate (or first active one)
		for _, name := range candidates {
			s, ok := r.strategies[name]
			if ok && s.IsEnabled() {
				res += fmt.Sprintf(" | Focus: %s", s.State(symbol))
				break
			}
		}
	}

	if owner != "" {
		res += fmt.Sprintf(" | OWNER:%s", owner)
	}

	return res
}

func (r *Router) getCandidates(reg regime.RegimeType) []string {
	switch reg {
	case regime.RegimeTrend:
		return []string{"structure_bos", "breakout_retest", "flag_pennant", "trailing_sh", "ema_cross"}
	case regime.RegimeRanging:
		return []string{"rsi_divergence", "vwap_reversion", "bb_bounce", "sr_bounce", "fvg_fill", "liquidity_sweep"}
	case regime.RegimeBreakout:
		return []string{"volume_spike", "orb", "momentum_roc"}
	default:
		return nil
	}
}

// wasSqueezingRecently returns true if the classifier recorded a squeeze
// in the last N bars (default: 10). Used to guard breakout regime promotion.
func (r *Router) wasSqueezingRecently(symbol string) bool {
	cf := r.getClassifier(symbol)
	// Check if the classifier itself says it is/was squeezing
	// The classifier's IsSqueezing() reflects the current state; for "recently",
	// we rely on the fact that an actual squeeze is expected to have ended just now.
	// We use the BBW history to detect if BBW was low recently.
	return cf.WasSqueezingRecently(10)
}

// OnKline proxies data to the classifier AND all sub-strategies (so they stay warm).
func (r *Router) OnKline(k stream.WsKline) {
	// Feed the classifier closed data to track regime across frame
	if k.Kline.IsClosed {
		r.getClassifier(k.Symbol).AddKline(k.Kline.Interval, k.Kline.High, k.Kline.Low, k.Kline.Close)
	}

	// Forward all intervals to sub-strategies (they will filter internally)

	for _, sName := range r.activeSubs {
		strat, ok := r.strategies[sName]
		if ok {
			strat.OnKline(k)
		}
	}
}

// Proxy other events to all active sub-strategies blindly so they can track their state.
func (r *Router) OnMarkPrice(mp stream.WsMarkPrice) {
	// Phase 3: Track funding rate for filtering
	r.getClassifier(mp.Symbol).SetFundingRate(mp.FundingRate)

	for _, sName := range r.activeSubs {
		if s, ok := r.strategies[sName]; ok {
			s.OnMarkPrice(mp)
		}
	}
}

func (r *Router) OnOrderUpdate(u stream.WsOrderUpdate) {
	for _, sName := range r.activeSubs {
		if s, ok := r.strategies[sName]; ok {
			s.OnOrderUpdate(u)
		}
	}
}
func (r *Router) OnAccountUpdate(u stream.WsAccountUpdate) {
	for _, sName := range r.activeSubs {
		if s, ok := r.strategies[sName]; ok {
			s.OnAccountUpdate(u)
		}
	}
}

// Signals routes the final decisions based on Market Regime.
func (r *Router) Signals(symbol string, pos *client.Position) []*Signal {
	cf := r.getClassifier(symbol)
	reg, _, _ := cf.Current()
	bias := cf.HTFTrendBias()
	squeeze := cf.IsSqueezing()

	// Correct Breakout Promotion: Only promote if we were PREVIOUSLY squeezing.
	// A market that was never squeezing should NOT be treated as a Breakout.
	wasSqueezingRecently := r.wasSqueezingRecently(symbol)
	if wasSqueezingRecently && !squeeze && reg != regime.RegimeTrend {
		reg = regime.RegimeBreakout
	}

	candidates := r.getCandidates(reg)
	if candidates == nil {
		return nil
	}

	r.mu.RLock()
	owner := r.positionOwner[symbol]
	r.mu.RUnlock()

	// 0. RESET: If no position exists, clear the owner just in case.
	if pos == nil || pos.PositionAmt == 0 {
		r.mu.Lock()
		delete(r.positionOwner, symbol)
		r.mu.Unlock()
		owner = ""
	}

	var allSignals []*Signal

	// 1. EXIT LOGIC
	if pos != nil && pos.PositionAmt != 0 {
		warnings := 0
		var mainWarning *Signal

		// ALWAYS check owner strategy first, regardless of current regime
		// This prevents a position being stuck open after a regime shift
		if owner != "" {
			if ownerStrat, ok := r.strategies[owner]; ok {
				for _, sig := range ownerStrat.Signals(symbol, pos) {
					if sig.Type == SignalExit {
						sig.StrategyName = owner
						r.mu.Lock()
						delete(r.positionOwner, symbol)
						r.mu.Unlock()
						r.log.Info("Owner exit triggered",
							zap.String("strategy", owner),
							zap.String("symbol", symbol),
							zap.String("reason", sig.Reason),
						)
						return []*Signal{sig}
					}
				}
			}
		}

		// Consensus check: poll all active strategies for additional exit warnings
		for _, sName := range r.activeSubs {
			if sName == owner {
				continue // already handled above
			}
			str, ok := r.strategies[sName]
			if !ok {
				continue
			}
			sigs := str.Signals(symbol, pos)
			for _, sig := range sigs {
				if sig.Type == SignalExit {
					warnings++
					mainWarning = sig
					mainWarning.StrategyName = sName
				}
			}
		}

		if warnings >= 2 {
			mainWarning.Reason = fmt.Sprintf("Consensus Exit (%d warnings)", warnings)
			r.log.Warn("🚨 CONSENSUS EXIT TRIGGERED",
				zap.String("symbol", symbol),
				zap.Int("warnings", warnings),
				zap.String("owner", owner),
			)
			r.mu.Lock()
			delete(r.positionOwner, symbol)
			r.mu.Unlock()
			return []*Signal{mainWarning}
		}
		return nil // No exit consensus reached
	}

	// 2. ENTRY LOGIC: Only check strategies recommended for the current regime
	for _, name := range candidates {
		strat, ok := r.strategies[name]
		if !ok || !strat.IsEnabled() {
			continue
		}

		// PHASE 5: Feed context for dynamic thresholds
		if mcr, ok := strat.(MarketContextReceiver); ok {
			_, adx, _ := cf.Current()
			mcr.SetMarketContext(symbol, adx)
		}

		sigs := strat.Signals(symbol, pos)
		for _, sig := range sigs {
			if sig == nil || sig.Type == SignalNone {
				continue
			}

			if sig.Type == SignalEnter {
				// PHASE 1 IQ: HTF Trend Filter
				if reg == regime.RegimeTrend {
					if sig.Side == SideBuy && bias < 0 {
						r.log.Info("IQ-FILTER: Long signal blocked", zap.String("symbol", symbol), zap.String("strategy", name), zap.String("reason", "Bearish 1h bias"))
						continue
					}
					if sig.Side == SideSell && bias > 0 {
						r.log.Info("IQ-FILTER: Short signal blocked", zap.String("symbol", symbol), zap.String("strategy", name), zap.String("reason", "Bullish 1h bias"))
						continue
					}
				}

				// PHASE 3: Funding Rate Filter
				funding := cf.GetFundingRate()
				if sig.Side == SideBuy && funding > 0.0003 { // 0.03%
					r.log.Info("IQ-FILTER: Long signal blocked", zap.String("symbol", symbol), zap.String("strategy", name), zap.String("reason", "high funding"), zap.Float64("funding", funding))
					continue
				}
				if sig.Side == SideSell && funding < -0.0003 { // -0.03%
					r.log.Info("IQ-FILTER: Short signal blocked", zap.String("symbol", symbol), zap.String("strategy", name), zap.String("reason", "negative funding"), zap.Float64("funding", funding))
					continue
				}

				// PHASE 2: Dynamic ATR Sizing
				// ONLY calculate size if strategy didn't provide one
				if sig.Quantity == "" || sig.Quantity == "0" {
					atr := cf.GetATR("5m", 14)
					closes := cf.GetCloses("5m")
					if len(closes) > 0 {
						price := closes[len(closes)-1]
						notional, err := r.risk.CalculatePositionSize(symbol, price, atr)
						if err == nil {
							sig.Quantity = fmt.Sprintf("%.4f", notional)
						} else {
							r.log.Warn("IQ-RISK: Could not calculate ATR size", zap.String("symbol", symbol), zap.Error(err))
						}
					} else {
						r.log.Warn("IQ-ROUTER: Missing price data for sizing", zap.String("symbol", symbol))
					}
				}

				sig.StrategyName = name
				allSignals = append(allSignals, sig)
			}
		}
	}

	return allSignals
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
