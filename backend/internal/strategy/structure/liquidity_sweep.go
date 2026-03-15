package structure

import (
	"fmt"
	"math"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type LiquiditySweepConfig struct {
	Enabled       bool
	Symbols       []string
	Lookback      int
	TolerancePct  float64 // for defining "Equal" highs/lows
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type LiquiditySweepStrategy struct {
	cfg     LiquiditySweepConfig
	log     *zap.Logger
	enabled bool

	mu    sync.RWMutex
	highs map[string][]float64
	lows  map[string][]float64
}

func NewLiquiditySweep(cfg LiquiditySweepConfig, log *zap.Logger) *LiquiditySweepStrategy {
	return &LiquiditySweepStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		highs:   make(map[string][]float64),
		lows:    make(map[string][]float64),
	}
}

func (s *LiquiditySweepStrategy) Name() string      { return "liquidity_sweep" }
func (s *LiquiditySweepStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *LiquiditySweepStrategy) IsEnabled() bool   { return s.enabled }
func (s *LiquiditySweepStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *LiquiditySweepStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	highs := s.highs[symbol]
	if len(highs) < s.cfg.Lookback {
		return fmt.Sprintf("warming up (%d/%d)", len(highs), s.cfg.Lookback)
	}
	
	maxHigh := 0.0
	minLow := math.MaxFloat64
	for i := 0; i < len(highs)-1; i++ {
		if highs[i] > maxHigh { maxHigh = highs[i] }
		if s.lows[symbol][i] < minLow { minLow = s.lows[symbol][i] }
	}
	wait := fmt.Sprintf("Wait for Wick > %.2f or < %.2f", maxHigh, minLow)
	return fmt.Sprintf("Pools:[L:%.2f H:%.2f] | %s", minLow, maxHigh, wait)
}

func (s *LiquiditySweepStrategy) OnKline(k stream.WsKline) {
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

func (s *LiquiditySweepStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *LiquiditySweepStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *LiquiditySweepStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *LiquiditySweepStrategy) Signal(symbol string, pos *client.Position) *strategy.Signal {
	s.mu.RLock()
	highs := s.highs[symbol]
	lows := s.lows[symbol]
	s.mu.RUnlock()

	if len(highs) < s.cfg.Lookback {
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	// 1. Find the local maximum/minimum in the lookback (Liquidity Pool)
	maxHigh := 0.0
	minLow := math.MaxFloat64
	for i := 0; i < len(highs)-1; i++ {
		if highs[i] > maxHigh {
			maxHigh = highs[i]
		}
		if lows[i] < minLow {
			minLow = lows[i]
		}
	}

	// 2. Detect Sweep
	// Current candle's High/Low wicks through but Close stays inside
	return &strategy.Signal{Type: strategy.SignalNone} // Logic requires Close data
}
