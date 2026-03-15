package structure

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

type LiquiditySweepConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	Lookback      int      `yaml:"lookback"`
	TolerancePct  float64  `yaml:"tolerance_pct"` // for defining "Equal" highs/lows
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type LiquiditySweepStrategy struct {
	cfg         LiquiditySweepConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	classifiers map[string]*regime.Classifier
}

func NewLiquiditySweep(cfg LiquiditySweepConfig, log *zap.Logger) *LiquiditySweepStrategy {
	return &LiquiditySweepStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *LiquiditySweepStrategy) Name() string      { return "liquidity_sweep" }
func (s *LiquiditySweepStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *LiquiditySweepStrategy) IsEnabled() bool   { return s.enabled }
func (s *LiquiditySweepStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *LiquiditySweepStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *LiquiditySweepStrategy) State(symbol string) string {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil {
		return "warming up (no classifier)"
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	lows := cf.GetLows(s.cfg.Timeframe)
	if len(highs) < s.cfg.Lookback {
		return fmt.Sprintf("warming up (%d/%d)", len(highs), s.cfg.Lookback)
	}

	maxHigh := 0.0
	minLow := math.MaxFloat64
	for i := len(highs) - s.cfg.Lookback; i < len(highs)-1; i++ {
		if highs[i] > maxHigh { maxHigh = highs[i] }
		if lows[i] < minLow { minLow = lows[i] }
	}
	wait := fmt.Sprintf("Wait for Wick > %.2f or < %.2f", maxHigh, minLow)
	return fmt.Sprintf("Pools:[L:%.2f H:%.2f] | %s", minLow, maxHigh, wait)
}

func (s *LiquiditySweepStrategy) OnKline(k stream.WsKline) {} // Classifier handles history

func (s *LiquiditySweepStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *LiquiditySweepStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *LiquiditySweepStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *LiquiditySweepStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil {
		return nil
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	lows := cf.GetLows(s.cfg.Timeframe)
	closes := cf.GetCloses(s.cfg.Timeframe)

	if len(highs) < s.cfg.Lookback+1 {
		return nil
	}

	maxHigh := 0.0
	minLow := math.MaxFloat64
	for i := len(highs) - s.cfg.Lookback - 1; i < len(highs)-1; i++ {
		if highs[i] > maxHigh { maxHigh = highs[i] }
		if lows[i] < minLow { minLow = lows[i] }
	}

	lastHigh := highs[len(highs)-1]
	lastLow := lows[len(lows)-1]
	lastClose := closes[len(closes)-1]
	atr := cf.GetATR(s.cfg.Timeframe, 14)

	// Bearish Sweep (Liquidity Grab UP)
	if lastHigh > maxHigh && lastClose < maxHigh {
		sl := lastHigh
		if atr > 0 { sl += atr }
		risk := sl - lastClose
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   lastClose - (risk * 2.0),
			Reason:       fmt.Sprintf("Bearish Sweep of %.4f", maxHigh),
			StrategyName: s.Name(),
		}}
	}

	// Bullish Sweep (Liquidity Grab DOWN)
	if lastLow < minLow && lastClose > minLow {
		sl := lastLow
		if atr > 0 { sl -= atr }
		risk := lastClose - sl
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   lastClose + (risk * 2.0),
			Reason:       fmt.Sprintf("Bullish Sweep of %.4f", minLow),
			StrategyName: s.Name(),
		}}
	}

	return nil
}
