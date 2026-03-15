package meanrev

import (
	"fmt"
	"math"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type SRBounceConfig struct {
	Enabled       bool
	Symbols       []string
	Lookback      int     // candles to find S/R
	BouncePct     float64 // price must be within this % of S/R
	OrderSizeUSDT float64
	Leverage      int
	Timeframe     string
}

type SRBounceStrategy struct {
	cfg     SRBounceConfig
	log     *zap.Logger
	enabled bool

	mu        sync.RWMutex
	highs     map[string][]float64
	lows      map[string][]float64
	lastClose map[string]float64 // last close for proximity filtering
}

func NewSRBounce(cfg SRBounceConfig, log *zap.Logger) *SRBounceStrategy {
	return &SRBounceStrategy{
		cfg:       cfg,
		log:       log,
		enabled:   cfg.Enabled,
		highs:     make(map[string][]float64),
		lows:      make(map[string][]float64),
		lastClose: make(map[string]float64),
	}
}

func (s *SRBounceStrategy) Name() string      { return "sr_bounce" }
func (s *SRBounceStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *SRBounceStrategy) IsEnabled() bool   { return s.enabled }
func (s *SRBounceStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *SRBounceStrategy) State(symbol string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	highs := s.highs[symbol]
	lows := s.lows[symbol]
	if len(highs) < s.cfg.Lookback {
		return fmt.Sprintf("warming up (%d/%d)", len(highs), s.cfg.Lookback)
	}
	
	res := 0.0
	sup := math.MaxFloat64
	for i := 0; i < len(highs)-1; i++ {
		if highs[i] > res { res = highs[i] }
		if lows[i] < sup { sup = lows[i] }
	}
	price := highs[len(highs)-1]
	wait := fmt.Sprintf("Wait for Price >= %.2f or <= %.2f", res*(1-s.cfg.BouncePct/100), sup*(1+s.cfg.BouncePct/100))
	return fmt.Sprintf("Price:%.2f Lvl:[Sup:%.2f Res:%.2f] | %s", price, sup, res, wait)
}

func (s *SRBounceStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	s.mu.Lock()
	s.lastClose[sym] = k.Kline.Close
	defer s.mu.Unlock()

	s.highs[sym] = append(s.highs[sym], k.Kline.High)
	s.lows[sym] = append(s.lows[sym], k.Kline.Low)

	if len(s.highs[sym]) > s.cfg.Lookback+1 {
		s.highs[sym] = s.highs[sym][1:]
		s.lows[sym] = s.lows[sym][1:]
	}
}

func (s *SRBounceStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *SRBounceStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *SRBounceStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *SRBounceStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	highs := s.highs[symbol]
	lows := s.lows[symbol]
	s.mu.RUnlock()

	if len(highs) < s.cfg.Lookback {
		return nil
	}

	// Simple S/R: highest high and lowest low of the lookback
	res := 0.0
	sup := math.MaxFloat64
	for i := 0; i < len(highs)-1; i++ { // exclude the current candle
		if highs[i] > res {
			res = highs[i]
		}
		if lows[i] < sup {
			sup = lows[i]
		}
	}

	var sigs []*strategy.Signal

	// Proximity filter: Only signal the closer S/R level to avoid "noise" orders.
	// For S/R levels, we use the range between support & resistance as the band.
	srRange := res - sup
	if srRange <= 0 || sup <= 0 || sup >= math.MaxFloat64 || res <= 0 {
		return nil
	}

	// Get last close price for proximity check
	s.mu.RLock()
	lastClose := s.lastClose[symbol]
	s.mu.RUnlock()

	if lastClose == 0 {
		return nil
	}

	relPos := (lastClose - sup) / srRange // 0.0 = at support, 1.0 = at resistance

	// Only buy if price is in the lower 40% of the S/R range (close to support)
	if relPos < 0.4 {
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Price:        fmt.Sprintf("%.2f", sup),
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			TakeProfit:   res, // full mean reversion = TP at resistance
			Reason:       fmt.Sprintf("SR Support Limit @ %.2f (proximity: %.0f%%)", sup, relPos*100),
			StrategyName: s.Name(),
		})
	}

	// Only sell if price is in the upper 40% of the S/R range (close to resistance)
	if relPos > 0.6 {
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Price:        fmt.Sprintf("%.2f", res),
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			TakeProfit:   sup, // full mean reversion = TP at support
			Reason:       fmt.Sprintf("SR Resistance Limit @ %.2f (proximity: %.0f%%)", res, relPos*100),
			StrategyName: s.Name(),
		})
	}

	return sigs
}
