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

	upper, _, lower := s.bb.Calculate(closes)
	last := closes[len(closes)-1]

	// Exit Logic: Close when price fully reverts to the opposite band
	if pos != nil && pos.PositionAmt != 0 {
		// Long opened at lower band → take profit at upper band
		if pos.PositionAmt > 0 && last >= upper {
			return []*strategy.Signal{{
				Type:         strategy.SignalExit,
				Symbol:       symbol,
				Side:         strategy.SideSell,
				Reason:       fmt.Sprintf("BB Full Reversion to Upper @ %.2f", upper),
				StrategyName: s.Name(),
			}}
		}
		// Short opened at upper band → take profit at lower band
		if pos.PositionAmt < 0 && last <= lower {
			return []*strategy.Signal{{
				Type:         strategy.SignalExit,
				Symbol:       symbol,
				Side:         strategy.SideBuy,
				Reason:       fmt.Sprintf("BB Full Reversion to Lower @ %.2f", lower),
				StrategyName: s.Name(),
			}}
		}
		return nil
	}

	var sigs []*strategy.Signal

	// Proximity filter: only signal the CLOSER side IF price is within 1.5% of the band.
	// Don't spread both nets at the same time — only near misses are valid setups.
	bandRange := upper - lower
	if bandRange <= 0 {
		return nil
	}
	lowerProximity := (last - lower) / bandRange  // 0.0 = AT lower, 1.0 = AT upper
	upperProximity := (upper - last) / bandRange  // 0.0 = AT upper, 1.0 = AT lower

	// Only post a buy limit if we are in the lower 40% of the band
	if lowerProximity < 0.4 {
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Price:        fmt.Sprintf("%.2f", lower),
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			TakeProfit:   upper,  // full mean reversion = TP
			Reason:       fmt.Sprintf("BB Lower Limit @ %.2f (proximity: %.0f%%)", lower, lowerProximity*100),
			StrategyName: s.Name(),
		})
	}

	// Only post a sell limit if we are in the upper 40% of the band
	if upperProximity < 0.4 {
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Price:        fmt.Sprintf("%.2f", upper),
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			TakeProfit:   lower,  // full mean reversion = TP
			Reason:       fmt.Sprintf("BB Upper Limit @ %.2f (proximity: %.0f%%)", upper, upperProximity*100),
			StrategyName: s.Name(),
		})
	}

	return sigs
}
