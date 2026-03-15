package strategy

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

// Router acts as the master strategy.
// It implements Strategy but delegates decisions to sub-strategies
// based on the current market regime.
type Router struct {
	classifier *regime.Classifier
	strategies map[string]Strategy // map sub-strategy name to instance
	activeSubs []string            // which sub-strategies are enabled in config

	log *zap.Logger
	cfg RouterConfig

	// positionOwner tracks which strategy entered a trade for a symbol.
	// Used for consensus exit logic.
	positionOwner map[string]string // symbol -> strategyName
	mu            sync.RWMutex
}

type RouterConfig struct {
	Enabled bool
	Symbols []string
}

func NewRouter(cfg RouterConfig, log *zap.Logger) *Router {
	return &Router{
		// ADX is usually 14 periods, BB is 20 with 2 stddev
		classifier:    regime.NewClassifier(14, 20, 2.0),
		strategies:    make(map[string]Strategy),
		activeSubs:    make([]string, 0),
		positionOwner: make(map[string]string),
		log:           log,
		cfg:           cfg,
	}
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

// State returns the overarching regime state + details from the primary sub-strategy
func (r *Router) State(symbol string) string {
	r.mu.RLock()
	owner := r.positionOwner[symbol]
	r.mu.RUnlock()

	reg, adx, bbw := r.classifier.Current()
	candidates := r.getCandidates(reg)

	// Build active candidate string
	var activeNames []string
	for _, name := range candidates {
		if s, ok := r.strategies[name]; ok && s.IsEnabled() {
			activeNames = append(activeNames, name)
		}
	}

	res := fmt.Sprintf("[%s] ADX:%.1f BBW:%.2f%%", reg, adx, bbw)
	if owner != "" {
		res += fmt.Sprintf(" | OWNER:%s", owner)
	}
	res += fmt.Sprintf(" | Active: %v", activeNames)

	// If we have an active candidate, show the status of the first one
	if len(activeNames) > 0 {
		top := activeNames[0]
		if strat, ok := r.strategies[top]; ok {
			res += fmt.Sprintf(" | Focus: %s", strat.State(symbol))
		}
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

// OnKline proxies data to the classifier AND all sub-strategies (so they stay warm).
func (r *Router) OnKline(k stream.WsKline) {
	// Feed the classifier closed data to track HTF regime
	if k.Kline.IsClosed {
		r.classifier.AddKline(k.Kline.High, k.Kline.Low, k.Kline.Close)
	}

	for _, sName := range r.activeSubs {
		strat, ok := r.strategies[sName]
		if ok {
			strat.OnKline(k)
		}
	}
}

// Proxy other events to all active sub-strategies blindly so they can track their state.
func (r *Router) OnMarkPrice(mp stream.WsMarkPrice) {
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

// Signal routes the final decision based on Market Regime.
func (r *Router) Signal(symbol string, pos *client.Position) *Signal {
	reg, _, _ := r.classifier.Current()

	candidates := r.getCandidates(reg)
	if candidates == nil {
		return &Signal{Type: SignalNone}
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

	// 1. EXIT LOGIC
	if pos != nil && pos.PositionAmt != 0 {
		warnings := 0
		var mainWarning *Signal

		for _, sName := range r.activeSubs {
			str, ok := r.strategies[sName]
			if !ok {
				continue
			}
			sig := str.Signal(symbol, pos)
			if sig != nil && sig.Type == SignalExit {
				if sName == owner {
					// Primary Owner wants out - follow immediately.
					sig.StrategyName = sName
					r.mu.Lock()
					delete(r.positionOwner, symbol)
					r.mu.Unlock()
					return sig
				}

				// External warning.
				warnings++
				mainWarning = sig
				mainWarning.StrategyName = sName
			}
		}

		// Quorum Exit: If at least 2 OTHER strategies say "Get out".
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
			return mainWarning
		}
	}

	// 2. ENTRY LOGIC: Only check strategies recommended for the current regime
	for _, name := range candidates {
		// Only check if strategy is actually loaded and enabled
		strat, ok := r.strategies[name]
		if !ok || !strat.IsEnabled() {
			continue
		}

		sig := strat.Signal(symbol, pos)
		if sig != nil && sig.Type == SignalEnter {
			sig.StrategyName = name
			r.mu.Lock()
			r.positionOwner[symbol] = name
			r.mu.Unlock()
			return sig
		}
	}

	return &Signal{Type: SignalNone}
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
