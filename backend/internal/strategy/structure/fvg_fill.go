package structure

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type FVGConfig struct {
	Enabled       bool
	Symbols       []string
	MinGapPct     float64
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type FVGStrategy struct {
	cfg     FVGConfig
	log     *zap.Logger
	enabled bool

	mu    sync.RWMutex
	highs map[string][]float64
	lows  map[string][]float64
}

func NewFVG(cfg FVGConfig, log *zap.Logger) *FVGStrategy {
	return &FVGStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		highs:   make(map[string][]float64),
		lows:    make(map[string][]float64),
	}
}

func (s *FVGStrategy) Name() string      { return "fvg_fill" }
func (s *FVGStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *FVGStrategy) IsEnabled() bool   { return s.enabled }
func (s *FVGStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *FVGStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	highs := s.highs[symbol]
	if len(highs) < 3 {
		return fmt.Sprintf("warming up (%d/3)", len(highs))
	}
	return "Wait for Fair Value Gap formation (3-candle sequence)"
}

func (s *FVGStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()

	s.highs[sym] = append(s.highs[sym], k.Kline.High)
	s.lows[sym] = append(s.lows[sym], k.Kline.Low)

	if len(s.highs[sym]) > 5 {
		s.highs[sym] = s.highs[sym][1:]
		s.lows[sym] = s.lows[sym][1:]
	}
}

func (s *FVGStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *FVGStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *FVGStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *FVGStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	highs := s.highs[symbol]
	lows := s.lows[symbol]
	s.mu.RUnlock()

	if len(highs) < 3 {
		return nil
	}

	c1h := highs[len(highs)-3]
	c1l := lows[len(lows)-3]
	c3h := highs[len(highs)-1]
	c3l := lows[len(lows)-1]

	// Bullish FVG (Upside Gap)
	if c3l > c1h {
		gap := c3l - c1h
		gapPct := (gap / c1h) * 100
		if gapPct >= s.cfg.MinGapPct {
			return []*strategy.Signal{{
				Type:     strategy.SignalEnter,
				Symbol:   symbol,
				Side:     strategy.SideBuy,
				Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
				Reason:   "Bullish FVG",
			}}
		}
	}

	// Bearish FVG (Downside Gap)
	if c3h < c1l {
		gap := c1l - c3h
		gapPct := (gap / c1l) * 100
		if gapPct >= s.cfg.MinGapPct {
			return []*strategy.Signal{{
				Type:     strategy.SignalEnter,
				Symbol:   symbol,
				Side:     strategy.SideSell,
				Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
				Reason:   "Bearish FVG",
			}}
		}
	}

	return nil
}
