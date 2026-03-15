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

type BreakoutRetestConfig struct {
	Enabled               bool
	Symbols               []string
	ConsolidationPeriods int     // Number of candles to define range
	BreakoutVolumeMult    float64 // Volume must be > SMA * Mult
	RetestTolerancePct    float64 // How close to the "edge" price must return
	OrderSizeUSDT         float64
	Leverage              int
	Timeframe             string
}

type BreakoutState string

const (
	StateConsolidating BreakoutState = "CONSOLIDATING"
	StateBreakout      BreakoutState = "BREAKOUT"
	StateWaitingRetest BreakoutState = "WAITING_RETEST"
)

type BreakoutRetestStrategy struct {
	cfg     BreakoutRetestConfig
	log     *zap.Logger
	enabled bool

	mu      sync.RWMutex
	highs   map[string][]float64
	lows    map[string][]float64
	closes  map[string][]float64
	volumes map[string][]float64

	// Track stage for each symbol
	stage      map[string]BreakoutState
	boxHigh    map[string]float64
	boxLow     map[string]float64
	breakDir   map[string]strategy.Side // LONG or SHORT
}

func NewBreakoutRetest(cfg BreakoutRetestConfig, log *zap.Logger) *BreakoutRetestStrategy {
	return &BreakoutRetestStrategy{
		cfg:      cfg,
		log:      log,
		enabled:  cfg.Enabled,
		highs:    make(map[string][]float64),
		lows:     make(map[string][]float64),
		closes:   make(map[string][]float64),
		volumes:  make(map[string][]float64),
		stage:    make(map[string]BreakoutState),
		boxHigh:  make(map[string]float64),
		boxLow:   make(map[string]float64),
		breakDir: make(map[string]strategy.Side),
	}
}

func (s *BreakoutRetestStrategy) Name() string      { return "breakout_retest" }
func (s *BreakoutRetestStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *BreakoutRetestStrategy) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}
func (s *BreakoutRetestStrategy) SetEnabled(v bool) {
	s.mu.Lock()
	s.enabled = v
	s.mu.Unlock()
}

func (s *BreakoutRetestStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.stage[symbol]
	if !ok {
		return "warming up"
	}
	high := s.boxHigh[symbol]
	low := s.boxLow[symbol]
	var wait string
	switch st {
	case StateConsolidating:
		wait = fmt.Sprintf("Wait for Break Close > %.2f or < %.2f", high, low)
	case StateBreakout, StateWaitingRetest:
		if s.breakDir[symbol] == strategy.SideBuy {
			wait = fmt.Sprintf("Wait for Retest of %.2f (UP)", high)
		} else {
			wait = fmt.Sprintf("Wait for Retest of %.2f (DOWN)", low)
		}
	}
	return fmt.Sprintf("Stage:%s Box:[%.2f|%.2f] | %s", st, low, high, wait)
}

func (s *BreakoutRetestStrategy) OnKline(k stream.WsKline) {
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

	maxLen := s.cfg.ConsolidationPeriods * 3 // keep some extra history
	if len(s.closes[sym]) > maxLen {
		s.highs[sym] = s.highs[sym][1:]
		s.lows[sym] = s.lows[sym][1:]
		s.closes[sym] = s.closes[sym][1:]
		s.volumes[sym] = s.volumes[sym][1:]
	}
}

func (s *BreakoutRetestStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *BreakoutRetestStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *BreakoutRetestStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *BreakoutRetestStrategy) Signals(symbol string, currentPos *client.Position) []*strategy.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()

	closes := s.closes[symbol]
	highs := s.highs[symbol]
	lows := s.lows[symbol]
	vols := s.volumes[symbol]

	if len(closes) < s.cfg.ConsolidationPeriods+1 {
		return nil
	}

	lastClose := closes[len(closes)-1]
	lastVol := vols[len(vols)-1]

	// 1. Logic for CONSOLIDATING -> BREAKOUT
	if s.stage[symbol] == "" || s.stage[symbol] == StateConsolidating {
		boxHigh := 0.0
		boxLow := math.MaxFloat64
		volSum := 0.0

		startIdx := len(closes) - 1 - s.cfg.ConsolidationPeriods
		for i := startIdx; i < len(closes)-1; i++ {
			if highs[i] > boxHigh {
				boxHigh = highs[i]
			}
			if lows[i] < boxLow {
				boxLow = lows[i]
			}
			volSum += vols[i]
		}
		avgVol := volSum / float64(s.cfg.ConsolidationPeriods)

		if lastClose > boxHigh && lastVol > avgVol*s.cfg.BreakoutVolumeMult {
			s.stage[symbol] = StateBreakout
			s.boxHigh[symbol] = boxHigh
			s.boxLow[symbol] = boxLow
			s.breakDir[symbol] = strategy.SideBuy
			s.log.Info("breakout detected UP", zap.String("symbol", symbol), zap.Float64("high", boxHigh))
		} else if lastClose < boxLow && lastVol > avgVol*s.cfg.BreakoutVolumeMult {
			s.stage[symbol] = StateBreakout
			s.boxHigh[symbol] = boxHigh
			s.boxLow[symbol] = boxLow
			s.breakDir[symbol] = strategy.SideSell
			s.log.Info("breakout detected DOWN", zap.String("symbol", symbol), zap.Float64("low", boxLow))
		} else {
			s.stage[symbol] = StateConsolidating
		}
	}

	// 2. Logic for BREAKOUT -> WAITING_RETEST
	if s.stage[symbol] == StateBreakout {
		s.stage[symbol] = StateWaitingRetest
	}

	// 3. Logic for WAITING_RETEST (Retest logic)
	if s.stage[symbol] == StateWaitingRetest {
		dir := s.breakDir[symbol]

		if dir == strategy.SideBuy {
			upperBound := s.boxHigh[symbol] * (1 + s.cfg.RetestTolerancePct/100)
			lowerBound := s.boxHigh[symbol] * (1 - s.cfg.RetestTolerancePct/100)

			if lastClose <= upperBound && lastClose >= lowerBound {
				s.stage[symbol] = StateConsolidating
				return []*strategy.Signal{{
					Type:     strategy.SignalEnter,
					Symbol:   symbol,
					Side:     strategy.SideBuy,
					Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
					Reason:   "Breakout + Retest UP",
				}}
			}
			if lastClose < s.boxLow[symbol] {
				s.stage[symbol] = StateConsolidating
			}
		} else if dir == strategy.SideSell {
			upperBound := s.boxLow[symbol] * (1 + s.cfg.RetestTolerancePct/100)
			lowerBound := s.boxLow[symbol] * (1 - s.cfg.RetestTolerancePct/100)

			if lastClose <= upperBound && lastClose >= lowerBound {
				s.stage[symbol] = StateConsolidating
				return []*strategy.Signal{{
					Type:     strategy.SignalEnter,
					Symbol:   symbol,
					Side:     strategy.SideSell,
					Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
					Reason:   "Breakout + Retest DOWN",
				}}
			}
			if lastClose > s.boxHigh[symbol] {
				s.stage[symbol] = StateConsolidating
			}
		}
	}

	return nil
}
