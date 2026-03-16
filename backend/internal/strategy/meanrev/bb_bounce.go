package meanrev

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/indicators"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type BBBounceConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	Period        int      `yaml:"period"`
	StdDev        float64  `yaml:"std_dev"`
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type BBBounceStrategy struct {
	cfg         BBBounceConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	closes      map[string][]float64
	bb          *indicators.BBState
	classifiers map[string]*regime.Classifier
}

func NewBBBounce(cfg BBBounceConfig, log *zap.Logger) *BBBounceStrategy {
	return &BBBounceStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		closes:      make(map[string][]float64),
		bb:          indicators.NewBBState(cfg.Period, cfg.StdDev),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *BBBounceStrategy) RequiredIntervals() []string {
	return []string{s.cfg.Timeframe}
}

func (s *BBBounceStrategy) Name() string      { return "bb_bounce" }
func (s *BBBounceStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *BBBounceStrategy) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}
func (s *BBBounceStrategy) SetEnabled(v bool) {
	s.mu.Lock()
	s.enabled = v
	s.mu.Unlock()
}

func (s *BBBounceStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *BBBounceStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	closes := s.closes[symbol]
	if len(closes) < s.cfg.Period {
		return fmt.Sprintf("warming up (%d/%d)", len(closes), s.cfg.Period)
	}
	up, mid, low := s.bb.Calculate(closes)
	last := closes[len(closes)-1]
	wait := fmt.Sprintf("Wait for Price <= %.2f or >= %.2f", low, up)
	return fmt.Sprintf("Last:%.2f BB:[%.2f|%.2f|%.2f] | %s", last, low, mid, up, wait)
}

func (s *BBBounceStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closes[sym] = append(s.closes[sym], k.Kline.Close)
	if len(s.closes[sym]) > 100 {
		s.closes[sym] = s.closes[sym][1:]
	}
}

func (s *BBBounceStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *BBBounceStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *BBBounceStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *BBBounceStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	closes := s.closes[symbol]
	cf, cfOk := s.classifiers[symbol]
	s.mu.RUnlock()

	if len(closes) < s.cfg.Period {
		return nil
	}

	upper, mid, lower := s.bb.Calculate(closes)
	last := closes[len(closes)-1]

	// Exit Logic: Close when price reverts to the mean (Middle Band).
	// We only exit if the trade has moved in the correct direction (in profit relative to entry).
	// This prevents premature exits immediately after entry when price is already near the mid.
	if pos != nil && pos.PositionAmt != 0 {
		if pos.PositionAmt > 0 && last >= mid && last > pos.EntryPrice {
			return []*strategy.Signal{{
				Type:         strategy.SignalExit,
				Symbol:       symbol,
				Side:         strategy.SideSell,
				Reason:       fmt.Sprintf("BB Mean Reversion to Mid @ %.2f (entry: %.2f)", mid, pos.EntryPrice),
				StrategyName: s.Name(),
			}}
		}
		if pos.PositionAmt < 0 && last <= mid && last < pos.EntryPrice {
			return []*strategy.Signal{{
				Type:         strategy.SignalExit,
				Symbol:       symbol,
				Side:         strategy.SideBuy,
				Reason:       fmt.Sprintf("BB Mean Reversion to Mid @ %.2f (entry: %.2f)", mid, pos.EntryPrice),
				StrategyName: s.Name(),
			}}
		}
		return nil
	}

	atr := 0.0
	if cfOk && cf != nil {
		atr = cf.GetATR(s.cfg.Timeframe, 14)
	}

	var sigs []*strategy.Signal

	bandRange := upper - lower
	if bandRange <= 0 {
		return nil
	}
	lowerProximity := (last - lower) / bandRange 
	upperProximity := (upper - last) / bandRange 

	if lowerProximity < 0.4 {
		var sl float64
		if atr > 0 {
			sl = lower - (atr * 2.0)
		}
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Price:        fmt.Sprintf("%.4f", lower),
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   mid,
			Reason:       fmt.Sprintf("BB Lower Bounce @ %.4f", lower),
			StrategyName: s.Name(),
		})
	}

	if upperProximity < 0.4 {
		var sl float64
		if atr > 0 {
			sl = upper + (atr * 2.0)
		}
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Price:        fmt.Sprintf("%.4f", upper),
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   mid,
			Reason:       fmt.Sprintf("BB Upper Bounce @ %.4f", upper),
			StrategyName: s.Name(),
		})
	}

	return sigs
}
