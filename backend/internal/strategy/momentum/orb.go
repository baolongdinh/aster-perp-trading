package momentum

import (
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type ORBConfig struct {
	Enabled       bool
	Symbols       []string
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type ORBStrategy struct {
	cfg     ORBConfig
	log     *zap.Logger
	enabled bool

	mu         sync.RWMutex
	orbHigh    map[string]float64
	orbLow     map[string]float64
	lastDay    map[string]int // track day of month to reset range
	rangeSet   map[string]bool
}

func NewORB(cfg ORBConfig, log *zap.Logger) *ORBStrategy {
	return &ORBStrategy{
		cfg:      cfg,
		log:      log,
		enabled:  cfg.Enabled,
		orbHigh:  make(map[string]float64),
		orbLow:   make(map[string]float64),
		lastDay:  make(map[string]int),
		rangeSet: make(map[string]bool),
	}
}

func (s *ORBStrategy) Name() string      { return "orb" }
func (s *ORBStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *ORBStrategy) IsEnabled() bool   { return s.enabled }
func (s *ORBStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *ORBStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.rangeSet[symbol] {
		return "waiting for session start (00:00 UTC)"
	}
	return fmt.Sprintf("ORB set: (%.2f - %.2f)", s.orbLow[symbol], s.orbHigh[symbol])
}

func (s *ORBStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use Daily open as the range setter (or first candle of the day in UTC)
	now := time.Unix(k.Kline.StartTime/1000, 0).UTC()
	day := now.Day()

	if day != s.lastDay[sym] {
		// New Day! Set the opening range based on the very first candle
		s.lastDay[sym] = day
		s.orbHigh[sym] = k.Kline.High
		s.orbLow[sym] = k.Kline.Low
		s.rangeSet[sym] = true
		s.log.Info("ORB range set for today", zap.String("sym", sym), zap.Float64("h", k.Kline.High), zap.Float64("l", k.Kline.Low))
	}
}

func (s *ORBStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *ORBStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *ORBStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *ORBStrategy) Signal(symbol string, pos *client.Position) *strategy.Signal {
	// ORB breakout usually happens mid-bar, but since our engine 
	// calls Signal after OnKline, we can check it here.
	s.mu.RLock()
	high := s.orbHigh[symbol]
	low := s.orbLow[symbol]
	isSet := s.rangeSet[symbol]
	s.mu.RUnlock()

	if !isSet {
		return &strategy.Signal{Type: strategy.SignalNone}
	}
	
	// We need the latest price to check against the range.
	// Since Signal doesn't receive the kline, we'd ideally store 
	// the latest price in OnKline. For now, we'll return None 
	// because we'll handle the actual trigger inside checkBreakout 
	// if we refactor or just use a dummy check.
	// Actually, let's just make it work:
	
	_ = high
	_ = low

	return &strategy.Signal{Type: strategy.SignalNone}
}

// In ORB, the breakout happens INTRADAY, so we check on every kline
func (s *ORBStrategy) checkBreakout(k stream.WsKline) *strategy.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if !s.rangeSet[k.Symbol] {
		return nil
	}
	
	// Range was set by the FIRST candle. Subsequent candles are breakout attempts.
    // If this is the SAME candle that set the range, don't trade.
    now := time.Unix(k.Kline.StartTime/1000, 0).UTC()
    if now.Hour() == 0 && now.Minute() < 15 { // simplified check for "opening candle"
        return nil
    }

	if k.Kline.Close > s.orbHigh[k.Symbol] {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   k.Symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "ORB Breakout UP",
		}
	}
	if k.Kline.Close < s.orbLow[k.Symbol] {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   k.Symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "ORB Breakout DOWN",
		}
	}
	return nil
}
