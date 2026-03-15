package trend

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type TrailingSHConfig struct {
	Enabled       bool
	Symbols       []string
	SwingPeriod   int // Lookback to define a swing high/low
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type TrailingSHStrategy struct {
	cfg     TrailingSHConfig
	log     *zap.Logger
	enabled bool

	mu      sync.RWMutex
	highs   map[string][]float64
	lows    map[string][]float64

	// Structure state
	lastHH map[string]float64
	lastHL map[string]float64
	lastLH map[string]float64
	lastLL map[string]float64
}

func NewTrailingSH(cfg TrailingSHConfig, log *zap.Logger) *TrailingSHStrategy {
	return &TrailingSHStrategy{
		cfg:     cfg,
		log:     log,
		enabled: cfg.Enabled,
		highs:   make(map[string][]float64),
		lows:    make(map[string][]float64),
		lastHH:  make(map[string]float64),
		lastHL:  make(map[string]float64),
		lastLH:  make(map[string]float64),
		lastLL:  make(map[string]float64),
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

func (s *TrailingSHStrategy) OnKline(k stream.WsKline) {
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
}

func (s *TrailingSHStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *TrailingSHStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *TrailingSHStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *TrailingSHStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()

	highs := s.highs[symbol]
	lows := s.lows[symbol]

	if len(highs) < s.cfg.SwingPeriod*2+1 {
		return nil
	}

	// Detect Swing Points
	idx := len(highs) - 1 - s.cfg.SwingPeriod
	isSwingHigh := true
	isSwingLow := true

	for j := idx - s.cfg.SwingPeriod; j <= idx+s.cfg.SwingPeriod; j++ {
		if j == idx {
			continue
		}
		if highs[j] >= highs[idx] {
			isSwingHigh = false
		}
		if lows[j] <= lows[idx] {
			isSwingLow = false
		}
	}

	currentHigh := highs[idx]
	currentLow := lows[idx]

	// Update Structure
	if isSwingHigh {
		if currentHigh > s.lastHH[symbol] {
			s.lastHH[symbol] = currentHigh
			s.log.Debug("new HH", zap.String("sym", symbol), zap.Float64("price", currentHigh))
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

	// Signal Logic
	lastClose := highs[len(highs)-1] // rough approx

	// Bullish: Price breaks above last HH after a HL was formed
	if lastClose > s.lastHH[symbol] && s.lastHL[symbol] > s.lastLL[symbol] && s.lastHH[symbol] > 0 {
		return []*strategy.Signal{{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "HH/HL Bullish Breakout",
		}}
	}

	// Bearish: Price breaks below last LL after a LH was formed
	if lastClose < s.lastLL[symbol] && s.lastLH[symbol] < s.lastHH[symbol] && s.lastLL[symbol] > 0 {
		return []*strategy.Signal{{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "LH/LL Bearish Breakout",
		}}
	}

	return nil
}
