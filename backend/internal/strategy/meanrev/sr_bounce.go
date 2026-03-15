package meanrev

import (
	"fmt"
	"math"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

// SRBounceConfig handles mean reversion at support/resistance.
type SRBounceConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"` // e.g. "5m"
	Lookback      int      `yaml:"lookback"`
	BouncePct     float64  `yaml:"bounce_pct"`
}

type SRBounceStrategy struct {
	cfg         SRBounceConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	lastSig     map[string]strategy.Side
	classifiers map[string]*regime.Classifier
}

func NewSRBounce(cfg SRBounceConfig, log *zap.Logger) *SRBounceStrategy {
	return &SRBounceStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		lastSig:     make(map[string]strategy.Side),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *SRBounceStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *SRBounceStrategy) Name() string      { return "sr_bounce" }
func (s *SRBounceStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *SRBounceStrategy) IsEnabled() bool   { return s.enabled }
func (s *SRBounceStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *SRBounceStrategy) State(symbol string) string {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()
	if !ok || cf == nil {
		return "warming up (no classifier)"
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	lows := cf.GetLows(s.cfg.Timeframe)
	if len(highs) < s.cfg.Lookback {
		return fmt.Sprintf("warming up (%d/%d)", len(highs), s.cfg.Lookback)
	}

	res := 0.0
	sup := math.MaxFloat64
	for i := len(highs) - s.cfg.Lookback; i < len(highs)-1; i++ {
		if highs[i] > res { res = highs[i] }
		if lows[i] < sup { sup = lows[i] }
	}
	
	lastClose := 0.0
	closes := cf.GetCloses(s.cfg.Timeframe)
	if len(closes) > 0 {
		lastClose = closes[len(closes)-1]
	}

	return fmt.Sprintf("Price:%.4f [Sup:%.4f Res:%.4f] | Wait for bounce", lastClose, sup, res)
}

func (s *SRBounceStrategy) OnKline(k stream.WsKline) {}

func (s *SRBounceStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *SRBounceStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *SRBounceStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *SRBounceStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil {
		return nil
	}

	highs := cf.GetHighs(s.cfg.Timeframe)
	lows := cf.GetLows(s.cfg.Timeframe)
	closes := cf.GetCloses(s.cfg.Timeframe)

	if len(highs) < s.cfg.Lookback+1 {
		return nil
	}

	// Calculate S/R from history
	res := 0.0
	sup := math.MaxFloat64
	for i := len(highs) - s.cfg.Lookback - 1; i < len(highs)-1; i++ {
		if highs[i] > res { res = highs[i] }
		if lows[i] < sup { sup = lows[i] }
	}

	lastClose := closes[len(closes)-1]
	r := res - sup
	if r <= 0 { return nil }

	relPos := (lastClose - sup) / r
	
	atr := cf.GetATR(s.cfg.Timeframe, 14)
	if atr <= 0 { return nil }

	var sigs []*strategy.Signal

	// Buy near support
	if relPos < (s.cfg.BouncePct / 100) {
		sl := sup - (atr * 1.5)
		tp := sup + (res-sup)*0.5
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Price:        fmt.Sprintf("%.4f", sup), // Assuming Price should be near support
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   tp,
			Reason:       fmt.Sprintf("SR Support Limit @ %.2f (proximity: %.0f%%)", sup, relPos*100),
			StrategyName: s.Name(),
		}}
	}

	// Sell near resistance
	if relPos > (1.0 - s.cfg.BouncePct/100) {
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Price:        fmt.Sprintf("%.4f", res),
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     res + atr, // 1 ATR above resistance
			TakeProfit:   sup,       // Mean reversion to support
			Reason:       fmt.Sprintf("SR Resistance Bounce @ %.4f", res),
			StrategyName: s.Name(),
		})
	}

	return sigs
}
