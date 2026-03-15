package trend

import (
	"fmt"
	"math"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type FlagPennantConfig struct {
	Enabled               bool
	Symbols               []string
	ImpulseMinPct         float64 // Min % move to count as impulse
	ImpulseCandles       int     // Max candles for impulse move
	FlagMaxRetracePct     float64 // e.g. 38.2
	FlagCandles          int     // Range of candles for flag
	OrderSizeUSDT         float64
	Leverage              int
	Timeframe             string
}

type FlagPennantStrategy struct {
	cfg     FlagPennantConfig
	log     *zap.Logger
	enabled bool

	mu      sync.RWMutex
	highs   map[string][]float64
	lows    map[string][]float64
	closes  map[string][]float64
	volumes map[string][]float64

	// Impulse tracking
	impulseHigh map[string]float64
	impulseLow  map[string]float64
	impulseDir  map[string]strategy.Side
}

func NewFlagPennant(cfg FlagPennantConfig, log *zap.Logger) *FlagPennantStrategy {
	return &FlagPennantStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		highs:       make(map[string][]float64),
		lows:        make(map[string][]float64),
		closes:      make(map[string][]float64),
		volumes:     make(map[string][]float64),
		impulseHigh: make(map[string]float64),
		impulseLow:  make(map[string]float64),
		impulseDir:  make(map[string]strategy.Side),
	}
}

func (s *FlagPennantStrategy) Name() string      { return "flag_pennant" }
func (s *FlagPennantStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *FlagPennantStrategy) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}
func (s *FlagPennantStrategy) SetEnabled(v bool) {
	s.mu.Lock()
	s.enabled = v
	s.mu.Unlock()
}

func (s *FlagPennantStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir, ok := s.impulseDir[symbol]
	if !ok || dir == "" {
		return "hunting for impulse move"
	}
	wait := "Wait for Flag Breakout"
	if dir == strategy.SideBuy {
		wait += " UP"
	} else {
		wait += " DOWN"
	}
	return fmt.Sprintf("Impulse:%s | %s", dir, wait)
}

func (s *FlagPennantStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()

	s.highs[sym] = append(s.highs[sym], k.Kline.High)
	s.lows[sym] = append(s.lows[sym], k.Kline.Low)
	s.closes[sym] = append(s.closes[sym], k.Kline.Close)
	s.volumes[sym] = append(s.volumes[sym], k.Kline.Volume)

	maxLen := 100
	if len(s.closes[sym]) > maxLen {
		s.highs[sym] = s.highs[sym][1:]
		s.lows[sym] = s.lows[sym][1:]
		s.closes[sym] = s.closes[sym][1:]
		s.volumes[sym] = s.volumes[sym][1:]
	}
}

func (s *FlagPennantStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *FlagPennantStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *FlagPennantStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *FlagPennantStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()

	closes := s.closes[symbol]
	if len(closes) < 20 {
		return nil
	}

	// 1. Detect Impulse
	flagStart := len(closes) - s.cfg.FlagCandles
	if flagStart < s.cfg.ImpulseCandles {
		return nil
	}

	foundImpulse := false
	for i := flagStart - 1; i >= flagStart-s.cfg.ImpulseCandles; i-- {
		startPrice := closes[i]
		endPrice := closes[flagStart]
		movePct := (endPrice - startPrice) / startPrice * 100

		if math.Abs(movePct) >= s.cfg.ImpulseMinPct {
			foundImpulse = true
			if movePct > 0 {
				s.impulseDir[symbol] = strategy.SideBuy
				s.impulseLow[symbol] = startPrice
				s.impulseHigh[symbol] = endPrice
			} else {
				s.impulseDir[symbol] = strategy.SideSell
				s.impulseLow[symbol] = endPrice
				s.impulseHigh[symbol] = startPrice
			}
			break
		}
	}

	if !foundImpulse {
		s.impulseDir[symbol] = ""
		return nil
	}

	// 2. Validate Flag (Retracement < 38.2%)
	dir := s.impulseDir[symbol]
	impHigh := s.impulseHigh[symbol]
	impLow := s.impulseLow[symbol]
	impSize := impHigh - impLow

	flagHigh := 0.0
	flagLow := math.MaxFloat64
	for i := flagStart; i < len(closes); i++ {
		if s.highs[symbol][i] > flagHigh {
			flagHigh = s.highs[symbol][i]
		}
		if s.lows[symbol][i] < flagLow {
			flagLow = s.lows[symbol][i]
		}
	}

	if dir == strategy.SideBuy {
		retrace := impHigh - flagLow
		retracePct := (retrace / impSize) * 100
		if retracePct > s.cfg.FlagMaxRetracePct || flagLow < impLow {
			return nil
		}

		// 3. Breakout of Flag High
		lastClose := closes[len(closes)-1]
		if lastClose > flagHigh {
			return []*strategy.Signal{{
				Type:     strategy.SignalEnter,
				Symbol:   symbol,
				Side:     strategy.SideBuy,
				Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
				Reason:   fmt.Sprintf("Flag Breakout UP (Retrace: %.1f%%)", retracePct),
			}}
		}
	} else if dir == strategy.SideSell {
		retrace := flagHigh - impLow
		retracePct := (retrace / impSize) * 100
		if retracePct > s.cfg.FlagMaxRetracePct || flagHigh > impHigh {
			return nil
		}

		// 3. Breakout of Flag Low
		lastClose := closes[len(closes)-1]
		if lastClose < flagLow {
			return []*strategy.Signal{{
				Type:     strategy.SignalEnter,
				Symbol:   symbol,
				Side:     strategy.SideSell,
				Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
				Reason:   fmt.Sprintf("Flag Breakout DOWN (Retrace: %.1f%%)", retracePct),
			}}
		}
	}

	return nil
}
