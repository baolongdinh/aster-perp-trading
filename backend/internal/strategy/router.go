package strategy

import (
	"fmt"

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
}

type RouterConfig struct {
	Enabled bool
	Symbols []string
}

func NewRouter(cfg RouterConfig, log *zap.Logger) *Router {
	return &Router{
		// ADX is usually 14 periods, BB is 20 with 2 stddev
		classifier: regime.NewClassifier(14, 20, 2.0),
		strategies: make(map[string]Strategy),
		activeSubs: make([]string, 0),
		log:        log,
		cfg:        cfg,
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
	reg, adx, bbw := r.classifier.Current()
	candidates := r.getCandidates(reg)

	// Build active candidate string
	var activeNames []string
	for _, name := range candidates {
		if s, ok := r.strategies[name]; ok && s.IsEnabled() {
			activeNames = append(activeNames, name)
		}
	}

	res := fmt.Sprintf("[%s] ADX:%.1f BBW:%.2f%% | Active: %v", reg, adx, bbw, activeNames)

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

	// 1. EXIT LOGIC: Always allow any strategy to fire an exit for an open position
	if pos != nil && pos.PositionAmt != 0 {
		for _, sName := range r.activeSubs {
			str, ok := r.strategies[sName]
			if !ok {
				continue
			}
			sig := str.Signal(symbol, pos)
			if sig != nil && sig.Type == SignalExit {
				sig.StrategyName = sName
				return sig
			}
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
			return sig
		}
	}

	return &Signal{Type: SignalNone}
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
