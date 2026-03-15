package momentum

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/indicators"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type MomentumROCConfig struct {
	Enabled        bool
	Symbols        []string
	ROCPeriod      int
	ROCThreshold   float64 // Roc above this % to trigger
	OrderSizeUSDT  float64
	Leverage       int
	Timeframe      string
}

type MomentumROCStrategy struct {
	cfg     MomentumROCConfig
	log     *zap.Logger
	enabled bool

	mu     sync.RWMutex
	closes map[string][]float64
}

func NewMomentumROC(cfg MomentumROCConfig, log *zap.Logger) *MomentumROCStrategy {
	return &MomentumROCStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		closes:  make(map[string][]float64),
	}
}

func (s *MomentumROCStrategy) Name() string      { return "momentum_roc" }
func (s *MomentumROCStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *MomentumROCStrategy) IsEnabled() bool   { return s.enabled }
func (s *MomentumROCStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *MomentumROCStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	closes := s.closes[symbol]
	if len(closes) < s.cfg.ROCPeriod+1 {
		return "warming up"
	}
	val := indicators.ROC(closes, s.cfg.ROCPeriod)
	return fmt.Sprintf("ROC(%d): %.2f%%", s.cfg.ROCPeriod, val)
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

func (s *MomentumROCStrategy) Signal(symbol string, pos *client.Position) *strategy.Signal {
	s.mu.RLock()
	closes := s.closes[symbol]
	s.mu.RUnlock()

	if len(closes) < s.cfg.ROCPeriod+2 {
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	rocNow := indicators.ROC(closes, s.cfg.ROCPeriod)
	rocPrev := indicators.ROC(closes[:len(closes)-1], s.cfg.ROCPeriod)

	// Bullish: ROC is high and accelerating
	if rocNow > s.cfg.ROCThreshold && rocNow > rocPrev {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("Momentum Accelerating: %.2f%%", rocNow),
		}
	}
	// Bearish: ROC is low and accelerating downwards
	if rocNow < -s.cfg.ROCThreshold && rocNow < rocPrev {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("Momentum Accelerating: %.2f%%", rocNow),
		}
	}

	return &strategy.Signal{Type: strategy.SignalNone}
}
