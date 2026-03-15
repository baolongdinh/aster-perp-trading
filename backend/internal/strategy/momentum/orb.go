package momentum

import (
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type ORBConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type ORBStrategy struct {
	cfg         ORBConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	orbHigh     map[string]float64
	orbLow      map[string]float64
	lastDay     map[string]int // track day of month to reset range
	rangeSet    map[string]bool
	classifiers map[string]*regime.Classifier
}

func NewORB(cfg ORBConfig, log *zap.Logger) *ORBStrategy {
	return &ORBStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		orbHigh:     make(map[string]float64),
		orbLow:      make(map[string]float64),
		lastDay:     make(map[string]int),
		rangeSet:    make(map[string]bool),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *ORBStrategy) RequiredIntervals() []string {
	return []string{s.cfg.Timeframe}
}

func (s *ORBStrategy) Name() string      { return "orb" }
func (s *ORBStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *ORBStrategy) IsEnabled() bool   { return s.enabled }
func (s *ORBStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *ORBStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *ORBStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.rangeSet[symbol] {
		return "waiting for session start (00:00 UTC)"
	}
	wait := fmt.Sprintf("Wait for Close > %.2f or < %.2f", s.orbHigh[symbol], s.orbLow[symbol])
	return fmt.Sprintf("Range:[%.2f|%.2f] | %s", s.orbLow[symbol], s.orbHigh[symbol], wait)
}

func (s *ORBStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use Daily open as the range setter (or first candle of the day in UTC)
	now := time.Unix(k.Kline.StartTime/1000, 0).UTC()
	day := now.Day()

	if day != s.lastDay[sym] {
		s.lastDay[sym] = day
		s.orbHigh[sym] = k.Kline.High
		s.orbLow[sym] = k.Kline.Low
		s.rangeSet[sym] = true
		s.log.Info("ORB range set for today", zap.String("sym", sym), zap.Float64("h", k.Kline.High), zap.Float64("l", k.Kline.Low))
	}
}

func (s *ORBStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *ORBStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *ORBStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *ORBStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	isSet := s.rangeSet[symbol]
	oHigh := s.orbHigh[symbol]
	oLow := s.orbLow[symbol]
	s.mu.RUnlock()

	if !isSet || ok && cf == nil {
		return nil
	}

	cl := cf.GetCloses(s.cfg.Timeframe)
	if len(cl) == 0 { return nil }
	lastPrice := cl[len(cl)-1]
	atr := cf.GetATR(s.cfg.Timeframe, 14)

	if lastPrice > oHigh && oHigh > 0 {
		sl := oLow
		if atr > 0 { sl -= (atr * 0.5) }
		risk := lastPrice - sl
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   lastPrice + (risk * 2.0),
			Reason:       "ORB Breakout UP",
			StrategyName: s.Name(),
		}}
	} else if lastPrice < oLow && oLow > 0 {
		sl := oHigh
		if atr > 0 { sl += (atr * 0.5) }
		risk := sl - lastPrice
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   lastPrice - (risk * 2.0),
			Reason:       "ORB Breakout DOWN",
			StrategyName: s.Name(),
		}}
	}

	return nil
}
