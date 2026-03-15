package structure

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type FVGConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	MinGapPct     float64  `yaml:"min_gap_pct"`
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type FVGStrategy struct {
	cfg         FVGConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	classifiers map[string]*regime.Classifier
}

func NewFVG(cfg FVGConfig, log *zap.Logger) *FVGStrategy {
	return &FVGStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *FVGStrategy) Name() string      { return "fvg_fill" }
func (s *FVGStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *FVGStrategy) IsEnabled() bool   { return s.enabled }
func (s *FVGStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *FVGStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *FVGStrategy) State(symbol string) string {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil {
		return "warming up (no classifier)"
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	if len(highs) < 3 {
		return fmt.Sprintf("warming up (%d/3)", len(highs))
	}
	return "Wait for Fair Value Gap formation (3-candle sequence)"
}

func (s *FVGStrategy) OnKline(k stream.WsKline) {} // Classifier handles history

func (s *FVGStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *FVGStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *FVGStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *FVGStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil {
		return nil
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	lows := cf.GetLows(s.cfg.Timeframe)
	closes := cf.GetCloses(s.cfg.Timeframe)

	if len(highs) < 3 {
		return nil
	}

	c1h := highs[len(highs)-3]
	c1l := lows[len(lows)-3]
	c3h := highs[len(highs)-1]
	c3l := lows[len(lows)-1]
	lastClose := closes[len(closes)-1]
	atr := cf.GetATR(s.cfg.Timeframe, 14)

	// Bullish FVG (Upside Gap)
	if c3l > c1h {
		gap := c3l - c1h
		gapPct := (gap / c1h) * 100
		if gapPct >= s.cfg.MinGapPct {
			var sl, tp float64
			if atr > 0 {
				sl = lastClose - (atr * 2.0)
				tp = lastClose + (atr * 4.0)
			}
			return []*strategy.Signal{{
				Type:         strategy.SignalEnter,
				Symbol:       symbol,
				Side:         strategy.SideBuy,
				Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
				StopLoss:     sl,
				TakeProfit:   tp,
				Reason:       "Bullish FVG",
				StrategyName: s.Name(),
			}}
		}
	}

	// Bearish FVG (Downside Gap)
	if c3h < c1l {
		gap := c1l - c3h
		gapPct := (gap / c1l) * 100
		if gapPct >= s.cfg.MinGapPct {
			var sl, tp float64
			if atr > 0 {
				sl = lastClose + (atr * 2.0)
				tp = lastClose - (atr * 4.0)
			}
			return []*strategy.Signal{{
				Type:         strategy.SignalEnter,
				Symbol:       symbol,
				Side:         strategy.SideSell,
				Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
				StopLoss:     sl,
				TakeProfit:   tp,
				Reason:       "Bearish FVG",
				StrategyName: s.Name(),
			}}
		}
	}

	return nil
}
