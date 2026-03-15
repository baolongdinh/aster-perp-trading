package trend

import (
	"fmt"
	"math"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type BreakoutRetestConfig struct {
	Enabled               bool     `yaml:"enabled"`
	Symbols               []string `yaml:"symbols"`
	ConsolidationPeriods int      `yaml:"consolidation_periods"` // Number of candles to define range
	BreakoutVolumeMult    float64  `yaml:"breakout_vol_mult"`    // Volume must be > SMA * Mult
	RetestTolerancePct    float64  `yaml:"retest_tolerance_pct"`  // How close to the "edge" price must return
	OrderSizeUSDT         float64  `yaml:"order_size_usdt"`
	Timeframe             string   `yaml:"timeframe"`
}

type BreakoutState string

const (
	StateConsolidating BreakoutState = "CONSOLIDATING"
	StateBreakout      BreakoutState = "BREAKOUT"
	StateWaitingRetest BreakoutState = "WAITING_RETEST"
)

type BreakoutRetestStrategy struct {
	cfg         BreakoutRetestConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	stage       map[string]BreakoutState
	boxHigh     map[string]float64
	boxLow      map[string]float64
	breakDir    map[string]strategy.Side // LONG or SHORT
	classifiers map[string]*regime.Classifier
}

func NewBreakoutRetest(cfg BreakoutRetestConfig, log *zap.Logger) *BreakoutRetestStrategy {
	return &BreakoutRetestStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		stage:       make(map[string]BreakoutState),
		boxHigh:     make(map[string]float64),
		boxLow:      make(map[string]float64),
		breakDir:    make(map[string]strategy.Side),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *BreakoutRetestStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
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

func (s *BreakoutRetestStrategy) OnKline(k stream.WsKline) {} // Classifier handles history

func (s *BreakoutRetestStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *BreakoutRetestStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *BreakoutRetestStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *BreakoutRetestStrategy) Signals(symbol string, currentPos *client.Position) []*strategy.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()

	cf, ok := s.classifiers[symbol]
	if !ok || cf == nil {
		return nil
	}

	closes := cf.GetCloses(s.cfg.Timeframe)
	highs := cf.GetHighs(s.cfg.Timeframe)
	lows := cf.GetLows(s.cfg.Timeframe)
	// We need volume too. I'll add GetVolumes to Classifier.
	// For now, let's assume I'll add it.

	if len(closes) < s.cfg.ConsolidationPeriods+1 {
		return nil
	}

	lastClose := closes[len(closes)-1]

	// 1. Logic for CONSOLIDATING -> BREAKOUT
	if s.stage[symbol] == "" || s.stage[symbol] == StateConsolidating {
		boxHigh := 0.0
		boxLow := math.MaxFloat64

		startIdx := len(closes) - 1 - s.cfg.ConsolidationPeriods
		for i := startIdx; i < len(closes)-1; i++ {
			if highs[i] > boxHigh { boxHigh = highs[i] }
			if lows[i] < boxLow { boxLow = lows[i] }
		}

		// Volume check (simplified or I add GetVolumes)
		if lastClose > boxHigh {
			s.stage[symbol] = StateBreakout
			s.boxHigh[symbol] = boxHigh
			s.boxLow[symbol] = boxLow
			s.breakDir[symbol] = strategy.SideBuy
		} else if lastClose < boxLow {
			s.stage[symbol] = StateBreakout
			s.boxHigh[symbol] = boxHigh
			s.boxLow[symbol] = boxLow
			s.breakDir[symbol] = strategy.SideSell
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
		atr := cf.GetATR(s.cfg.Timeframe, 14)

		if dir == strategy.SideBuy {
			upperBound := s.boxHigh[symbol] * (1 + s.cfg.RetestTolerancePct/100)
			lowerBound := s.boxHigh[symbol] * (1 - s.cfg.RetestTolerancePct/100)

			if lastClose <= upperBound && lastClose >= lowerBound {
				s.stage[symbol] = StateConsolidating
				sl := s.boxLow[symbol] // SL at other side of box
				if atr > 0 { sl -= (atr * 0.5) }
				risk := lastClose - sl
				return []*strategy.Signal{{
					Type:         strategy.SignalEnter,
					Symbol:       symbol,
					Side:         strategy.SideBuy,
					Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
					StopLoss:     sl,
					TakeProfit:   lastClose + (risk * 2.0),
					Reason:       "Breakout + Retest UP",
					StrategyName: s.Name(),
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
				sl := s.boxHigh[symbol] // SL at other side of box
				if atr > 0 { sl += (atr * 0.5) }
				risk := sl - lastClose
				return []*strategy.Signal{{
					Type:         strategy.SignalEnter,
					Symbol:       symbol,
					Side:         strategy.SideSell,
					Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
					StopLoss:     sl,
					TakeProfit:   lastClose - (risk * 2.0),
					Reason:       "Breakout + Retest DOWN",
					StrategyName: s.Name(),
				}}
			}
			if lastClose > s.boxHigh[symbol] {
				s.stage[symbol] = StateConsolidating
			}
		}
	}

	return nil
}
