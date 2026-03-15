package meanrev

import (
	"fmt"
	"math"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type SRBounceConfig struct {
	Enabled       bool
	Symbols       []string
	Lookback      int     // candles to find S/R
	BouncePct     float64 // price must be within this % of S/R
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type SRBounceStrategy struct {
	cfg     SRBounceConfig
	log     *zap.Logger
	enabled bool

	mu    sync.RWMutex
	highs map[string][]float64
	lows  map[string][]float64
}

func NewSRBounce(cfg SRBounceConfig, log *zap.Logger) *SRBounceStrategy {
	return &SRBounceStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		highs:   make(map[string][]float64),
		lows:    make(map[string][]float64),
	}
}

func (s *SRBounceStrategy) Name() string      { return "sr_bounce" }
func (s *SRBounceStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *SRBounceStrategy) IsEnabled() bool   { return s.enabled }
func (s *SRBounceStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *SRBounceStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("hunting S/R bounce (%d candles lookback)", s.cfg.Lookback)
}

func (s *SRBounceStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()

	s.highs[sym] = append(s.highs[sym], k.Kline.High)
	s.lows[sym] = append(s.lows[sym], k.Kline.Low)

	if len(s.highs[sym]) > s.cfg.Lookback+1 {
		s.highs[sym] = s.highs[sym][1:]
		s.lows[sym] = s.lows[sym][1:]
	}
}

func (s *SRBounceStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *SRBounceStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *SRBounceStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *SRBounceStrategy) Signal(symbol string, pos *client.Position) *strategy.Signal {
	s.mu.RLock()
	highs := s.highs[symbol]
	lows := s.lows[symbol]
	s.mu.RUnlock()

	if len(highs) < s.cfg.Lookback {
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	// Simple S/R: highest high and lowest low of the lookback
	res := 0.0
	sup := math.MaxFloat64
	for i := 0; i < len(highs)-1; i++ { // exclude the current candle
		if highs[i] > res {
			res = highs[i]
		}
		if lows[i] < sup {
			sup = lows[i]
		}
	}

	lastClose := highs[len(highs)-1] // rough

	// Entry Logic
	if lastClose <= sup*(1+s.cfg.BouncePct/100) {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("Support Bounce @ %.2f", sup),
		}
	} else if lastClose >= res*(1-s.cfg.BouncePct/100) {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("Resistance Bounce @ %.2f", res),
		}
	}

	return &strategy.Signal{Type: strategy.SignalNone}
}
