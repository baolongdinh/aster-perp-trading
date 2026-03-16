package meanrev

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/indicators"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type VWAPReversionConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	DevThreshold  float64  `yaml:"dev_threshold_pct"` // price deviation from VWAP in %
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type VWAPReversionStrategy struct {
	cfg         VWAPReversionConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	vwaps       map[string]*indicators.VWAPState
	lastPrice   map[string]float64
	classifiers map[string]*regime.Classifier
}

func NewVWAPReversion(cfg VWAPReversionConfig, log *zap.Logger) *VWAPReversionStrategy {
	return &VWAPReversionStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		vwaps:       make(map[string]*indicators.VWAPState),
		lastPrice:   make(map[string]float64),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *VWAPReversionStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *VWAPReversionStrategy) RequiredIntervals() []string {
	return []string{s.cfg.Timeframe}
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
	if val == 0 { return "waiting for vwap" }
	diff := (price - val) / val * 100
	wait := fmt.Sprintf("Wait for Dev >= %.1f%% or <= -%.1f%%", s.cfg.DevThreshold, s.cfg.DevThreshold)
	return fmt.Sprintf("VWAP:%.2f Price:%.2f Dev:%+.2f%% | %s", val, price, diff, wait)
}

func (s *VWAPReversionStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
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

func (s *VWAPReversionStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	vwapState, ok := s.vwaps[symbol]
	price := s.lastPrice[symbol]
	cf, cfOk := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || vwapState.Value() == 0 {
		return nil
	}

	vwap := vwapState.Value()
	deviation := (price - vwap) / vwap * 100

	// Exit Logic: Close when price returns to VWAP.
	// Guard: only exit if the position is in profit (price moved in the trade's direction from entry).
	if pos != nil && pos.PositionAmt != 0 {
		if pos.PositionAmt > 0 && price >= vwap && price > pos.EntryPrice {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Side:   strategy.SideSell,
				Reason: fmt.Sprintf("VWAP Target Reached (Long @ %.2f, entry: %.2f)", vwap, pos.EntryPrice),
			}}
		}
		if pos.PositionAmt < 0 && price <= vwap && price < pos.EntryPrice {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Side:   strategy.SideBuy,
				Reason: fmt.Sprintf("VWAP Target Reached (Short @ %.2f, entry: %.2f)", vwap, pos.EntryPrice),
			}}
		}
		return nil
	}

	atr := 0.0
	if cfOk && cf != nil {
		atr = cf.GetATR(s.cfg.Timeframe, 14)
	}

	var sigs []*strategy.Signal

	// Entry Logic
	if deviation <= -s.cfg.DevThreshold {
		var sl float64
		if atr > 0 {
			sl = price - (atr * 2.0)
		}
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   vwap,
			Reason:       fmt.Sprintf("VWAP Oversold Dev: %.2f%%", deviation),
			StrategyName: s.Name(),
		})
	} else if deviation >= s.cfg.DevThreshold {
		var sl float64
		if atr > 0 {
			sl = price + (atr * 2.0)
		}
		sigs = append(sigs, &strategy.Signal{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   vwap,
			Reason:       fmt.Sprintf("VWAP Overbought Dev: %.2f%%", deviation),
			StrategyName: s.Name(),
		})
	}

	return sigs
}
