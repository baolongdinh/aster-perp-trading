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

type RSIDivergenceConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Symbols       []string `yaml:"symbols"`
	RsiPeriod     int      `yaml:"rsi_period"`
	Overbought    float64  `yaml:"overbought"`
	Oversold      float64  `yaml:"oversold"`
	OrderSizeUSDT float64  `yaml:"order_size_usdt"`
	Timeframe     string   `yaml:"timeframe"`
}

type RSIDivergenceStrategy struct {
	cfg         RSIDivergenceConfig
	log         *zap.Logger
	rsi         map[string]*indicators.RSIState
	mu          sync.RWMutex
	classifiers map[string]*regime.Classifier
}

func NewRSIDivergence(cfg RSIDivergenceConfig, log *zap.Logger) *RSIDivergenceStrategy {
	return &RSIDivergenceStrategy{
		cfg:         cfg,
		log:         log,
		rsi:         make(map[string]*indicators.RSIState),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *RSIDivergenceStrategy) Name() string      { return "rsi_divergence" }
func (s *RSIDivergenceStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *RSIDivergenceStrategy) IsEnabled() bool   { return s.cfg.Enabled }
func (s *RSIDivergenceStrategy) SetEnabled(v bool) { s.cfg.Enabled = v }

func (s *RSIDivergenceStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *RSIDivergenceStrategy) getThresholds(symbol string) (float64, float64) {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	oversold := s.cfg.Oversold
	overbought := s.cfg.Overbought

	if ok && cf != nil {
		_, adx, _ := cf.Current()
		// Dynamic Thresholds: If ADX < 15 (quiet market), widen the entry criteria to 35/65
		if adx > 0 && adx < 15.0 {
			oversold = 35.0
			overbought = 65.0
		}
	}
	return oversold, overbought
}

func (s *RSIDivergenceStrategy) State(symbol string) string {
	s.mu.RLock()
	rsiState, ok := s.rsi[symbol]
	cf, cfOk := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok {
		return "waiting for data"
	}
	val := rsiState.Value()
	oversold, overbought := s.getThresholds(symbol)

	ctxStr := ""
	if cfOk && cf != nil {
		_, adx, _ := cf.Current()
		if adx > 0 && adx < 15.0 {
			ctxStr = fmt.Sprintf(" | LowVol (ADX:%.1f) dynamic thresholds enabled", adx)
		}
	}

	wait := fmt.Sprintf("Wait for RSI <= %.0f or >= %.0f", oversold, overbought)
	return fmt.Sprintf("RSI: %.2f | %s%s", val, wait, ctxStr)
}

func (s *RSIDivergenceStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed || k.Kline.Interval != s.cfg.Timeframe {
		return 
	}
	sym := k.Symbol
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rsi[sym]; !ok {
		s.rsi[sym] = indicators.NewRSIState(s.cfg.RsiPeriod)
	}
	s.rsi[sym].Add(k.Kline.Close)
}

func (s *RSIDivergenceStrategy) OnMarkPrice(mp stream.WsMarkPrice)        {}
func (s *RSIDivergenceStrategy) OnOrderUpdate(u stream.WsOrderUpdate)     {}
func (s *RSIDivergenceStrategy) OnAccountUpdate(u stream.WsAccountUpdate) {}

func (s *RSIDivergenceStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	rsiState, ok := s.rsi[symbol]
	cf, cfOk := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil
	}
	val := rsiState.Value()

	// Exit Logic (Counter-Signal)
	if pos != nil && pos.PositionAmt != 0 {
		oversold, overbought := s.getThresholds(symbol)
		if pos.PositionAmt > 0 && val >= overbought {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: fmt.Sprintf("RSI Mean Reverted to Overbought (%.1f >= %.0f)", val, overbought),
			}}
		}
		if pos.PositionAmt < 0 && val <= oversold {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: fmt.Sprintf("RSI Mean Reverted to Oversold (%.1f <= %.0f)", val, oversold),
			}}
		}
		return nil
	}

	oversold, overbought := s.getThresholds(symbol)
	
	atr := 0.0
	if cfOk && cf != nil {
		atr = cf.GetATR(s.cfg.Timeframe, 14)
	}

	lastClose := 0.0
	if cfOk && cf != nil {
		cl := cf.GetCloses(s.cfg.Timeframe)
		if len(cl) > 0 {
			lastClose = cl[len(cl)-1]
		}
	}

	if val <= oversold && lastClose > 0 {
		var sl, tp float64
		if atr > 0 {
			sl = lastClose - (atr * 2.0)
			tp = lastClose + (atr * 4.0) // 1:2 RR
		}
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideBuy,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   tp,
			Reason:       fmt.Sprintf("RSI Oversold Bounce (%.2f <= %.0f)", val, oversold),
			StrategyName: s.Name(),
		}}
	} else if val >= overbought && lastClose > 0 {
		var sl, tp float64
		if atr > 0 {
			sl = lastClose + (atr * 2.0)
			tp = lastClose - (atr * 4.0) // 1:2 RR
		}
		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         strategy.SideSell,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   tp,
			Reason:       fmt.Sprintf("RSI Overbought Drop (%.2f >= %.0f)", val, overbought),
			StrategyName: s.Name(),
		}}
	}

	return nil
}
