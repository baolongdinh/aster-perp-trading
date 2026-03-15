package meanrev

import (
	"fmt"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/indicators"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type RSIDivergenceConfig struct {
	Enabled       bool
	Symbols       []string
	RsiPeriod     int
	Overbought    float64
	Oversold      float64
	Leverage      int
	OrderSizeUSDT float64
	Timeframe     string
}

type RSIDivergenceStrategy struct {
	cfg RSIDivergenceConfig
	log *zap.Logger
	rsi map[string]*indicators.RSIState
}

func NewRSIDivergence(cfg RSIDivergenceConfig, log *zap.Logger) *RSIDivergenceStrategy {
	return &RSIDivergenceStrategy{
		cfg: cfg,
		log: log,
		rsi: make(map[string]*indicators.RSIState),
	}
}

func (s *RSIDivergenceStrategy) Name() string        { return "rsi_divergence" }
func (s *RSIDivergenceStrategy) Symbols() []string   { return s.cfg.Symbols }
func (s *RSIDivergenceStrategy) IsEnabled() bool     { return s.cfg.Enabled }
func (s *RSIDivergenceStrategy) SetEnabled(v bool)   { s.cfg.Enabled = v }

func (s *RSIDivergenceStrategy) State(symbol string) string {
	rsiState, ok := s.rsi[symbol]
	if !ok {
		return "waiting for data"
	}
	val := rsiState.Value()
	wait := fmt.Sprintf("Wait for RSI <= %.0f or >= %.0f", s.cfg.Oversold, s.cfg.Overbought)
	return fmt.Sprintf("RSI: %.2f | %s", val, wait)
}

func (s *RSIDivergenceStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed {
		return // only feed closed candles to RSI
	}
	sym := k.Symbol
	if _, ok := s.rsi[sym]; !ok {
		s.rsi[sym] = indicators.NewRSIState(s.cfg.RsiPeriod)
	}
	s.rsi[sym].Add(k.Kline.Close)
}

func (s *RSIDivergenceStrategy) OnMarkPrice(mp stream.WsMarkPrice)     {}
func (s *RSIDivergenceStrategy) OnOrderUpdate(u stream.WsOrderUpdate)  {}
func (s *RSIDivergenceStrategy) OnAccountUpdate(u stream.WsAccountUpdate) {}

func (s *RSIDivergenceStrategy) Signal(symbol string, pos *client.Position) *strategy.Signal {
	rsiState, ok := s.rsi[symbol]
	if !ok {
		return &strategy.Signal{Type: strategy.SignalNone}
	}
	val := rsiState.Value()

	// Exit Logic
	if pos != nil && pos.PositionAmt != 0 {
		if pos.PositionAmt > 0 && val >= 50.0 {
			// Long exit when mean reverts to center
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: "RSI Reverted to Mean (Long)",
			}
		}
		if pos.PositionAmt < 0 && val <= 50.0 {
			// Short exit when mean reverts to center
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: "RSI Reverted to Mean (Short)",
			}
		}
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	// Entry Logic (Mean Reversion assumes chopping market)
	if val <= s.cfg.Oversold {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "RSI Oversold Bounce",
		}
	} else if val >= s.cfg.Overbought {
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   "RSI Overbought Drop",
		}
	}

	// No signal, log the reason/state for user transparency
	s.log.Info("strategy evaluation",
		zap.String("strategy", s.Name()),
		zap.String("symbol", symbol),
		zap.String("state", s.State(symbol)),
	)

	return &strategy.Signal{Type: strategy.SignalNone}
}



func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
