package momentum

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

type MomentumROCConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	ROCPeriod     int      `yaml:"roc_period"`
	ROCThreshold  float64  `yaml:"roc_threshold"` // Roc above this % to trigger
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type MomentumROCStrategy struct {
	cfg         MomentumROCConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	closes      map[string][]float64
	classifiers map[string]*regime.Classifier
}

func NewMomentumROC(cfg MomentumROCConfig, log *zap.Logger) *MomentumROCStrategy {
	return &MomentumROCStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		closes:      make(map[string][]float64),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *MomentumROCStrategy) RequiredIntervals() []string {
	return []string{s.cfg.Timeframe}
}

func (s *MomentumROCStrategy) Name() string      { return "momentum_roc" }
func (s *MomentumROCStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *MomentumROCStrategy) IsEnabled() bool   { return s.enabled }
func (s *MomentumROCStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *MomentumROCStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *MomentumROCStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	closes := s.closes[symbol]
	if len(closes) < s.cfg.ROCPeriod+1 {
		return fmt.Sprintf("warming up (%d/%d)", len(closes), s.cfg.ROCPeriod+1)
	}
	val := indicators.ROC(closes, s.cfg.ROCPeriod)
	wait := fmt.Sprintf("Wait for ROC > %.1f%% or < -%.1f%% and accelerating", s.cfg.ROCThreshold, s.cfg.ROCThreshold)
	return fmt.Sprintf("ROC(%d):%.2f%% | %s", s.cfg.ROCPeriod, val, wait)
}

func (s *MomentumROCStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closes[sym] = append(s.closes[sym], k.Kline.Close)
	if len(s.closes[sym]) > 50 {
		s.closes[sym] = s.closes[sym][1:]
	}
}

func (s *MomentumROCStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *MomentumROCStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *MomentumROCStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *MomentumROCStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	closes := s.closes[symbol]
	cf, cfOk := s.classifiers[symbol]
	s.mu.RUnlock()

	if len(closes) < s.cfg.ROCPeriod+2 {
		return nil
	}

	rocNow := indicators.ROC(closes, s.cfg.ROCPeriod)
	rocPrev := indicators.ROC(closes[:len(closes)-1], s.cfg.ROCPeriod)
	
	atr := 0.0
	if cfOk && cf != nil {
		atr = cf.GetATR(s.cfg.Timeframe, 14)
	}
	lastPrice := closes[len(closes)-1]

	// Bullish: ROC is high and accelerating
	if rocNow > s.cfg.ROCThreshold && rocNow > rocPrev {
		var sl, tp float64
		if atr > 0 {
			sl = lastPrice - (atr * 2.0)
			tp = lastPrice + (atr * 4.0)
		}
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   tp,
			Reason:       fmt.Sprintf("Momentum Accelerating: %.2f%%", rocNow),
			StrategyName: s.Name(),
		}}
	}
	// Bearish: ROC is low and accelerating downwards
	if rocNow < -s.cfg.ROCThreshold && rocNow < rocPrev {
		var sl, tp float64
		if atr > 0 {
			sl = lastPrice + (atr * 2.0)
			tp = lastPrice - (atr * 4.0)
		}
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   tp,
			Reason:       fmt.Sprintf("Momentum Accelerating: %.2f%%", rocNow),
			StrategyName: s.Name(),
		}}
	}

	return nil
}
