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
	s.mu.RUnlock()

	if len(closes) < s.cfg.Period {
		return nil
	}

	upper, mid, lower := s.bb.Calculate(closes)
	last := closes[len(closes)-1]

	// Exit Logic: Close when price returns to Mid Band
	if pos != nil && pos.PositionAmt != 0 {
		if pos.PositionAmt > 0 && last >= mid {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: "BB Mean Reverted to Mid",
			}}
		}
		if pos.PositionAmt < 0 && last <= mid {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: "BB Mean Reverted to Mid",
			}}
		}
		return nil
	}

	var sigs []*strategy.Signal

	// Proactive Entry Logic: Place Limit orders at Upper and Lower Bands
	// Support (Buy Limit at Lower Band)
	sigs = append(sigs, &strategy.Signal{
		Type:         strategy.SignalEnter,
		Symbol:       symbol,
		Side:         strategy.SideBuy,
		Price:        fmt.Sprintf("%.2f", lower),
		Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
		Reason:       fmt.Sprintf("Proactive BB Lower Limit @ %.2f", lower),
		StrategyName: s.Name(),
	})

	// Resistance (Sell Limit at Upper Band)
	sigs = append(sigs, &strategy.Signal{
		Type:         strategy.SignalEnter,
		Symbol:       symbol,
		Side:         strategy.SideSell,
		Price:        fmt.Sprintf("%.2f", upper),
		Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
		Reason:       fmt.Sprintf("Proactive BB Upper Limit @ %.2f", upper),
		StrategyName: s.Name(),
	})

	return sigs
}
