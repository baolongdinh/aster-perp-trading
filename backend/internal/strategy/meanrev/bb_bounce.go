package meanrev

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/indicators"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type BBBounceConfig struct {
	Enabled       bool
	Symbols       []string
	Period        int
	StdDev        float64
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type BBBounceStrategy struct {
	cfg     BBBounceConfig
	log     *zap.Logger
	enabled bool

	mu     sync.RWMutex
	closes map[string][]float64
	bb     *indicators.BBState
}

func NewBBBounce(cfg BBBounceConfig, log *zap.Logger) *BBBounceStrategy {
	return &BBBounceStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		closes:  make(map[string][]float64),
		bb:      indicators.NewBBState(cfg.Period, cfg.StdDev),
	}
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

func (s *BBBounceStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	closes := s.closes[symbol]
	if len(closes) < s.cfg.Period {
		return "waiting for candles"
	}
	up, mid, low := s.bb.Calculate(closes)
	last := closes[len(closes)-1]
	return fmt.Sprintf("Last: %.2f | BB: (%.2f / %.2f / %.2f)", last, up, mid, low)
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

func (s *BBBounceStrategy) Signal(symbol string, pos *client.Position) *strategy.Signal {
	s.mu.RLock()
	closes := s.closes[symbol]
	s.mu.RUnlock()

	if len(closes) < s.cfg.Period {
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	upper, mid, lower := s.bb.Calculate(closes)
	last := closes[len(closes)-1]

	// Exit Logic: Close when price returns to Mid Band
	if pos != nil && pos.PositionAmt != 0 {
		if pos.PositionAmt > 0 && last >= mid {
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: "BB Mean Reverted to Mid",
			}
		}
		if pos.PositionAmt < 0 && last <= mid {
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: "BB Mean Reverted to Mid",
			}
		}
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	// Entry Logic
	if last <= lower {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "BB Lower Band Touch",
		}
	} else if last >= upper {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "BB Upper Band Touch",
		}
	}

	return &strategy.Signal{Type: strategy.SignalNone}
}
