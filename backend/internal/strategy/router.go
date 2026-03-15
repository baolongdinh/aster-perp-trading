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

// Signal routes the final decision based on Market Regime.
func (r *Router) Signal(symbol string, pos *client.Position) *Signal {
	cf := r.getClassifier(symbol)
	reg, _, _ := cf.Current()
	bias := cf.HTFTrendBias()
	squeeze := cf.IsSqueezing()

	// If we were squeezing and volatility just expanded (broken bands), promote to BREAKOUT
	if !squeeze && reg != regime.RegimeTrend {
		// Calculate current price relative to bands (simplified)
		// For now, if we are not squeezing but were recently, it's a breakout opportunity
		// More advanced: check if price > upper or < lower
		reg = regime.RegimeBreakout
	}

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

	// 1. EXIT LOGIC (Consensus)
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
					sig.StrategyName = sName
					r.mu.Lock()
					delete(r.positionOwner, symbol)
					r.mu.Unlock()
					return sig
				}
				warnings++
				mainWarning = sig
				mainWarning.StrategyName = sName
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
			return mainWarning
		}
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

		sig := strat.Signal(symbol, pos)
		if sig == nil || sig.Type == SignalNone {
			// Periodically log why we are skipping if we want ultra-detail,
			// but for now let's just log when a strategy reports NO signal.
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

			// PHASE 2: Dynamic ATR Sizing
			atr := cf.GetATR("5m", 14)
			closes := cf.GetCloses("5m")
			if len(closes) == 0 {
				r.log.Warn("IQ-ROUTER: Missing price data for sizing", zap.String("symbol", symbol))
				continue
			}
			price := closes[len(closes)-1]

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

			qty, err := r.risk.CalculatePositionSize(symbol, price, atr)

			if err != nil {
				r.log.Warn("IQ-RISK: Could not calculate ATR size", zap.String("symbol", symbol), zap.Error(err))
				// fallback to sub-strategy's fixed qty if available
				if sig.Quantity == "" || sig.Quantity == "0" {
					continue
				}
			} else {
				r.log.Info("IQ-RISK: Dynamic sizing applied",
					zap.String("symbol", symbol),
					zap.String("qty", qty),
					zap.Float64("atr", atr),
				)
				sig.Quantity = qty
			}

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
