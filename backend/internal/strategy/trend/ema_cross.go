package trend

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

// EMACrossConfig holds parameters for the EMA crossover strategy.
type EMACrossConfig struct {
	FastPeriod    int
	SlowPeriod    int
	Leverage      int
	OrderSizeUSDT float64
	Timeframe     string // e.g. "5m"
	Symbols       []string
	Enabled       bool
}

// EMACrossStrategy enters LONG when fast EMA crosses above slow EMA,
// and SHORT when fast EMA crosses below slow EMA.
type EMACrossStrategy struct {
	cfg     EMACrossConfig
	log     *zap.Logger
	enabled bool

	mu      sync.RWMutex
	closes  map[string][]float64 // symbol -> recent close prices (ring buffer)
	lastSig map[string]strategy.Side      // last signal per symbol to avoid re-entry
}

// NewEMACross creates a new EMA cross strategy.
func NewEMACross(cfg EMACrossConfig, log *zap.Logger) *EMACrossStrategy {
	s := &EMACrossStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		closes:  make(map[string][]float64),
		lastSig: make(map[string]strategy.Side),
	}
	// Pre-allocate buffers
	for _, sym := range cfg.Symbols {
		s.closes[sym] = make([]float64, 0, cfg.SlowPeriod*2)
	}
	return s
}

func (e *EMACrossStrategy) Name() string      { return "ema_cross" }
func (e *EMACrossStrategy) Symbols() []string { return e.cfg.Symbols }
func (e *EMACrossStrategy) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}
func (e *EMACrossStrategy) SetEnabled(v bool) {
	e.mu.Lock()
	e.enabled = v
	e.mu.Unlock()
}

// State returns a human-readable status of the strategy.
func (e *EMACrossStrategy) State(symbol string) string {
	e.mu.RLock()
	closes := e.closes[symbol]
	e.mu.RUnlock()

	req := e.cfg.SlowPeriod + 1
	if len(closes) < req {
		return fmt.Sprintf("warming up (%d/%d)", len(closes), req)
	}

	fastNow := ema(closes, e.cfg.FastPeriod)
	slowNow := ema(closes, e.cfg.SlowPeriod)
	fastPrev := ema(closes[:len(closes)-1], e.cfg.FastPeriod)
	slowPrev := ema(closes[:len(closes)-1], e.cfg.SlowPeriod)

	diff := (fastNow - slowNow) / slowNow * 100
	var trend, wait string
	if fastPrev <= slowPrev && fastNow > slowNow {
		trend = "GOLDEN CROSS"
		wait = "Wait for Signal Confirmation"
	} else if fastPrev >= slowPrev && fastNow < slowNow {
		trend = "DEATH CROSS"
		wait = "Wait for Signal Confirmation"
	} else if fastNow > slowNow {
		trend = "BULLISH"
		wait = "Wait for Cross Down"
	} else {
		trend = "BEARISH"
		wait = "Wait for Cross Up"
	}
	return fmt.Sprintf("%s | gap:%+.3f%% | %s", trend, diff, wait)
}

// OnKline accumulates closed bars.
func (e *EMACrossStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed {
		return // only process closed bars
	}
	// Only care about our timeframe
	if k.Kline.Interval != e.cfg.Timeframe {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	sym := k.Symbol
	e.closes[sym] = append(e.closes[sym], k.Kline.Close)
	// Keep buffer at 2x slow period max
	maxLen := e.cfg.SlowPeriod * 2
	if len(e.closes[sym]) > maxLen {
		e.closes[sym] = e.closes[sym][len(e.closes[sym])-maxLen:]
	}

	e.log.Info("strategy accumulating klines",
		zap.String("strategy", e.Name()),
		zap.String("symbol", sym),
		zap.Float64("close", k.Kline.Close),
		zap.Int("accumulated", len(e.closes[sym])),
		zap.Int("required_for_signal", e.cfg.SlowPeriod+1),
	)
}

func (e *EMACrossStrategy) OnMarkPrice(_ stream.WsMarkPrice)         {}
func (e *EMACrossStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (e *EMACrossStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

// Signal checks EMA cross and returns entry/exit signal.
func (e *EMACrossStrategy) Signal(symbol string, currentPos *client.Position) *strategy.Signal {
	e.mu.RLock()
	closes := e.closes[symbol]
	lastSig := e.lastSig[symbol]
	e.mu.RUnlock()

	if len(closes) < e.cfg.SlowPeriod+1 {
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	fastNow := ema(closes, e.cfg.FastPeriod)
	slowNow := ema(closes, e.cfg.SlowPeriod)
	fastPrev := ema(closes[:len(closes)-1], e.cfg.FastPeriod)
	slowPrev := ema(closes[:len(closes)-1], e.cfg.SlowPeriod)

	// Cross up: fast crosses above slow → LONG
	crossUp := fastPrev <= slowPrev && fastNow > slowNow
	// Cross down: fast crosses below slow → SHORT
	crossDown := fastPrev >= slowPrev && fastNow < slowNow

	// Exit current position if counter-signal
	if currentPos != nil && currentPos.PositionAmt != 0 {
		if currentPos.PositionAmt > 0 && crossDown {
			e.mu.Lock()
			e.lastSig[symbol] = strategy.SideSell
			e.mu.Unlock()
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Side:   strategy.SideSell,
				Reason: "ema_cross_exit_long",
			}
		}
		if currentPos.PositionAmt < 0 && crossUp {
			e.mu.Lock()
			e.lastSig[symbol] = strategy.SideBuy
			e.mu.Unlock()
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Side:   strategy.SideBuy,
				Reason: "ema_cross_exit_short",
			}
		}
	}

	// New entry
	if crossUp && lastSig != strategy.SideBuy {
		qty := fmt.Sprintf("%.4f", e.cfg.OrderSizeUSDT)
		e.mu.Lock()
		e.lastSig[symbol] = strategy.SideBuy
		e.mu.Unlock()
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: qty,
			Reason:   fmt.Sprintf("ema_cross_long fast=%.4f slow=%.4f", fastNow, slowNow),
		}
	}

	if crossDown && lastSig != strategy.SideSell {
		qty := fmt.Sprintf("%.4f", e.cfg.OrderSizeUSDT)
		e.mu.Lock()
		e.lastSig[symbol] = strategy.SideSell
		e.mu.Unlock()
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: qty,
			Reason:   fmt.Sprintf("ema_cross_short fast=%.4f slow=%.4f", fastNow, slowNow),
		}
	}

	return &strategy.Signal{Type: strategy.SignalNone}
}

// ema calculates the Exponential Moving Average of the last n values in data.
func ema(data []float64, n int) float64 {
	if len(data) == 0 || n <= 0 {
		return 0
	}
	if len(data) < n {
		n = len(data)
	}
	k := 2.0 / float64(n+1)
	result := data[len(data)-n] // seed with first value in window
	for i := len(data) - n + 1; i < len(data); i++ {
		result = data[i]*k + result*(1-k)
	}
	return result
}
