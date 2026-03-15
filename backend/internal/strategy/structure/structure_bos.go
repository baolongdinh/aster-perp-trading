package structure

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type BOSConfig struct {
	Enabled       bool
	Symbols       []string
	SwingPeriod   int
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type BOSStrategy struct {
	cfg     BOSConfig
	log     *zap.Logger
	enabled bool

	mu    sync.RWMutex
	highs map[string][]float64
	lows  map[string][]float64

	lastHH map[string]float64
	lastLL map[string]float64
}

func NewBOS(cfg BOSConfig, log *zap.Logger) *BOSStrategy {
	return &BOSStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		highs:   make(map[string][]float64),
		lows:    make(map[string][]float64),
		lastHH:  make(map[string]float64),
		lastLL:  make(map[string]float64),
	}
}

func (s *BOSStrategy) Name() string      { return "structure_bos" }
func (s *BOSStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *BOSStrategy) IsEnabled() bool   { return s.enabled }
func (s *BOSStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *BOSStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hh := s.lastHH[symbol]
	ll := s.lastLL[symbol]
	highs := s.highs[symbol]
	
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
	defer s.mu.Unlock()

	s.highs[sym] = append(s.highs[sym], k.Kline.High)
	s.lows[sym] = append(s.lows[sym], k.Kline.Low)

	if len(s.highs[sym]) > 50 {
		s.highs[sym] = s.highs[sym][1:]
		s.lows[sym] = s.lows[sym][1:]
	}

	// Update Swing High/Low
	if len(s.highs[sym]) < s.cfg.SwingPeriod*2+1 {
		return
	}

	idx := len(s.highs[sym]) - 1 - s.cfg.SwingPeriod
	isSH := true
	isSL := true
	for i := idx - s.cfg.SwingPeriod; i <= idx+s.cfg.SwingPeriod; i++ {
		if i == idx {
			continue
		}
		if s.highs[sym][i] >= s.highs[sym][idx] {
			isSH = false
		}
		if s.lows[sym][i] <= s.lows[sym][idx] {
			isSL = false
		}
	}

	if isSH {
		if s.highs[sym][idx] > s.lastHH[sym] {
			s.lastHH[sym] = s.highs[sym][idx]
		}
	}
	if isSL {
		if s.lastLL[sym] == 0 || s.lows[sym][idx] < s.lastLL[sym] {
			s.lastLL[sym] = s.lows[sym][idx]
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
	highs := s.highs[symbol]
	s.mu.RUnlock()

	if len(highs) == 0 || hh == 0 || ll == 0 {
		return nil
	}

	lastClose := highs[len(highs)-1]

	if lastClose > hh {
		return []*strategy.Signal{{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "Bullish BOS (Break of Swing High)",
		}}
	}

	if lastClose < ll {
		return []*strategy.Signal{{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "Bearish BOS (Break of Swing Low)",
		}}
	}

	return nil
}
