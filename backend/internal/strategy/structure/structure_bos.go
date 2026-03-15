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

type BOSConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	SwingPeriod   int      `yaml:"swing_period"`
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type BOSStrategy struct {
	cfg         BOSConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	lastHH      map[string]float64
	lastLL      map[string]float64
	classifiers map[string]*regime.Classifier
}

func NewBOS(cfg BOSConfig, log *zap.Logger) *BOSStrategy {
	return &BOSStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		lastHH:      make(map[string]float64),
		lastLL:      make(map[string]float64),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *BOSStrategy) RequiredIntervals() []string {
	return []string{s.cfg.Timeframe}
}

func (s *BOSStrategy) Name() string      { return "structure_bos" }
func (s *BOSStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *BOSStrategy) IsEnabled() bool   { return s.enabled }
func (s *BOSStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *BOSStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *BOSStrategy) State(symbol string) string {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	hh := s.lastHH[symbol]
	ll := s.lastLL[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil {
		return "warming up (no classifier)"
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	req := s.cfg.SwingPeriod*2 + 1
	if hh == 0 && ll == 0 {
		return fmt.Sprintf("mapping structure (%d/%d bars)", len(highs), req)
	}
	wait := fmt.Sprintf("Wait for BOS > %.2f or < %.2f", hh, ll)
	return fmt.Sprintf("Range:[%.2f|%.2f] | %s", ll, hh, wait)
}

func (s *BOSStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	cf, ok := s.classifiers[sym]
	s.mu.Unlock()

	if !ok || cf == nil {
		return
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	lows := cf.GetLows(s.cfg.Timeframe)

	if len(highs) < s.cfg.SwingPeriod*2+1 {
		return
	}

	idx := len(highs) - 1 - s.cfg.SwingPeriod
	isSH := true
	isSL := true
	for i := idx - s.cfg.SwingPeriod; i <= idx+s.cfg.SwingPeriod; i++ {
		if i == idx { continue }
		if highs[i] >= highs[idx] { isSH = false }
		if lows[i] <= lows[idx] { isSL = false }
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if isSH {
		if highs[idx] > s.lastHH[sym] {
			s.lastHH[sym] = highs[idx]
		}
	}
	if isSL {
		if s.lastLL[sym] == 0 || lows[idx] < s.lastLL[sym] {
			s.lastLL[sym] = lows[idx]
		}
	}
}

func (s *BOSStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *BOSStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *BOSStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *BOSStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	hh := s.lastHH[symbol]
	ll := s.lastLL[symbol]
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil || hh == 0 || ll == 0 {
		return nil
	}

	cl := cf.GetCloses(s.cfg.Timeframe)
	if len(cl) == 0 { return nil }
	lastClose := cl[len(cl)-1]
	atr := cf.GetATR(s.cfg.Timeframe, 14)

	if lastClose > hh {
		sl := ll
		if atr > 0 { sl -= (atr * 0.5) }
		risk := lastClose - sl
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   lastClose + (risk * 2.0),
			Reason:       "Bullish BOS (Break of Swing High)",
			StrategyName: s.Name(),
		}}
	}

	if lastClose < ll {
		sl := hh
		if atr > 0 { sl += (atr * 0.5) }
		risk := sl - lastClose
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   lastClose - (risk * 2.0),
			Reason:       "Bearish BOS (Break of Swing Low)",
			StrategyName: s.Name(),
		}}
	}

	return nil
}
