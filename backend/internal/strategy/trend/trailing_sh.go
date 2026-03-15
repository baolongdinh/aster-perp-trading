package trend

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type TrailingSHConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	SwingPeriod   int      `yaml:"swing_period"` // Lookback to define a swing high/low
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type TrailingSHStrategy struct {
	cfg         TrailingSHConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	lastHH      map[string]float64
	lastHL      map[string]float64
	lastLH      map[string]float64
	lastLL      map[string]float64
	classifiers map[string]*regime.Classifier
}

func NewTrailingSH(cfg TrailingSHConfig, log *zap.Logger) *TrailingSHStrategy {
	return &TrailingSHStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		lastHH:      make(map[string]float64),
		lastHL:      make(map[string]float64),
		lastLH:      make(map[string]float64),
		lastLL:      make(map[string]float64),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *TrailingSHStrategy) Name() string      { return "trailing_sh" }
func (s *TrailingSHStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *TrailingSHStrategy) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}
func (s *TrailingSHStrategy) SetEnabled(v bool) {
	s.mu.Lock()
	s.enabled = v
	s.mu.Unlock()
}

func (s *TrailingSHStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *TrailingSHStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hh := s.lastHH[symbol]
	ll := s.lastLL[symbol]
	if hh == 0 && ll == 0 {
		return "hunting for initial structure"
	}
	wait := fmt.Sprintf("Wait for Price > %.2f (UP) or < %.2f (DOWN)", hh, ll)
	return fmt.Sprintf("HH:%.2f LL:%.2f | %s", hh, ll, wait)
}

func (s *TrailingSHStrategy) OnKline(k stream.WsKline) {} // Classifier handles history

func (s *TrailingSHStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *TrailingSHStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *TrailingSHStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *TrailingSHStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()

	cf, ok := s.classifiers[symbol]
	if !ok || cf == nil {
		return nil
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	lows := cf.GetLows(s.cfg.Timeframe)
	closes := cf.GetCloses(s.cfg.Timeframe)

	if len(highs) < s.cfg.SwingPeriod*2+1 {
		return nil
	}

	// Detect Swing Points
	idx := len(highs) - 1 - s.cfg.SwingPeriod
	isSwingHigh := true
	isSwingLow := true

	for j := idx - s.cfg.SwingPeriod; j <= idx+s.cfg.SwingPeriod; j++ {
		if j == idx { continue }
		if highs[j] >= highs[idx] { isSwingHigh = false }
		if lows[j] <= lows[idx] { isSwingLow = false }
	}

	currentHigh := highs[idx]
	currentLow := lows[idx]
	atr := cf.GetATR(s.cfg.Timeframe, 14)

	// Update Structure
	if isSwingHigh {
		if currentHigh > s.lastHH[symbol] {
			s.lastHH[symbol] = currentHigh
		} else if currentHigh < s.lastHH[symbol] {
			s.lastLH[symbol] = currentHigh
		}
	}
	if isSwingLow {
		if currentLow > s.lastLL[symbol] {
			s.lastHL[symbol] = currentLow
		} else if currentLow < s.lastLL[symbol] {
			s.lastLL[symbol] = currentLow
		}
	}

	lastClose := closes[len(closes)-1]

	// Bullish Signal
	if lastClose > s.lastHH[symbol] && s.lastHL[symbol] > s.lastLL[symbol] && s.lastHH[symbol] > 0 {
		sl := s.lastHL[symbol]
		if atr > 0 { sl -= (atr * 0.5) }
		risk := lastClose - sl
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   lastClose + (risk * 2.0),
			Reason:       "HH/HL Bullish Breakout",
			StrategyName: s.Name(),
		}}
	}

	// Bearish Signal
	if lastClose < s.lastLL[symbol] && s.lastLH[symbol] < s.lastHH[symbol] && s.lastLL[symbol] > 0 {
		sl := s.lastLH[symbol]
		if atr > 0 { sl += (atr * 0.5) }
		risk := sl - lastClose
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   lastClose - (risk * 2.0),
			Reason:       "LH/LL Bearish Breakout",
			StrategyName: s.Name(),
		}}
	}

	return nil
}
