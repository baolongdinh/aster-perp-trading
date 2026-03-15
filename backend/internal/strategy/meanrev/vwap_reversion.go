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

type VWAPReversionConfig struct {
	Enabled       bool
	Symbols       []string
	DevThreshold  float64 // price deviation from VWAP in %
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type VWAPReversionStrategy struct {
	cfg     VWAPReversionConfig
	log     *zap.Logger
	enabled bool

	mu    sync.RWMutex
	vwaps map[string]*indicators.VWAPState
	lastPrice map[string]float64
}

func NewVWAPReversion(cfg VWAPReversionConfig, log *zap.Logger) *VWAPReversionStrategy {
	return &VWAPReversionStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		vwaps:   make(map[string]*indicators.VWAPState),
		lastPrice:   make(map[string]float64),
	}
}

func (s *VWAPReversionStrategy) Name() string      { return "vwap_reversion" }
func (s *VWAPReversionStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *VWAPReversionStrategy) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}
func (s *VWAPReversionStrategy) SetEnabled(v bool) {
	s.mu.Lock()
	s.enabled = v
	s.mu.Unlock()
}

func (s *VWAPReversionStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vwap, ok := s.vwaps[symbol]
	if !ok {
		return "init"
	}
	val := vwap.Value()
	price := s.lastPrice[symbol]
	diff := (price - val) / val * 100
	wait := fmt.Sprintf("Wait for Dev >= %.1f%% or <= -%.1f%%", s.cfg.DevThreshold, s.cfg.DevThreshold)
	return fmt.Sprintf("VWAP:%.2f Price:%.2f Dev:%+.2f%% | %s", val, price, diff, wait)
}

func (s *VWAPReversionStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed {
		return
	}
	// Only care about our timeframe
	if k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.vwaps[sym]; !ok {
		s.vwaps[sym] = indicators.NewVWAPState()
	}

	typicalPrice := (k.Kline.High + k.Kline.Low + k.Kline.Close) / 3
	s.vwaps[sym].Add(typicalPrice, k.Kline.Volume)
	s.lastPrice[sym] = k.Kline.Close
}

func (s *VWAPReversionStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *VWAPReversionStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *VWAPReversionStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *VWAPReversionStrategy) Signal(symbol string, pos *client.Position) *strategy.Signal {
	s.mu.RLock()
	vwapState, ok := s.vwaps[symbol]
	price := s.lastPrice[symbol]
	s.mu.RUnlock()

	if !ok || vwapState.Value() == 0 {
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	vwap := vwapState.Value()
	deviation := (price - vwap) / vwap * 100

	// Exit Logic: Close when price returns to VWAP
	if pos != nil && pos.PositionAmt != 0 {
		if pos.PositionAmt > 0 && price >= vwap {
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Side:   strategy.SideSell,
				Reason: "VWAP Target Reached (Long)",
			}
		}
		if pos.PositionAmt < 0 && price <= vwap {
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Side:   strategy.SideBuy,
				Reason: "VWAP Target Reached (Short)",
			}
		}
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	// Entry Logic
	if deviation <= -s.cfg.DevThreshold {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("VWAP Oversold Dev: %.2f%%", deviation),
		}
	} else if deviation >= s.cfg.DevThreshold {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("VWAP Overbought Dev: %.2f%%", deviation),
		}
	}

	return &strategy.Signal{Type: strategy.SignalNone}
}
