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

	// Market context for dynamic adjustments
	muCtx sync.RWMutex
	adx   map[string]float64
}

func NewRSIDivergence(cfg RSIDivergenceConfig, log *zap.Logger) *RSIDivergenceStrategy {
	return &RSIDivergenceStrategy{
		cfg: cfg,
		log: log,
		rsi: make(map[string]*indicators.RSIState),
		adx: make(map[string]float64),
	}
}

func (s *RSIDivergenceStrategy) Name() string      { return "rsi_divergence" }
func (s *RSIDivergenceStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *RSIDivergenceStrategy) IsEnabled() bool   { return s.cfg.Enabled }
func (s *RSIDivergenceStrategy) SetEnabled(v bool) { s.cfg.Enabled = v }

func (s *RSIDivergenceStrategy) getThresholds(symbol string) (float64, float64) {
	s.muCtx.RLock()
	adx := s.adx[symbol]
	s.muCtx.RUnlock()

	oversold := s.cfg.Oversold
	overbought := s.cfg.Overbought

	// Dynamic Thresholds: If ADX < 15 (quiet market), widen the entry criteria to 35/65
	if adx > 0 && adx < 15.0 {
		oversold = 35.0
		overbought = 65.0
	}
	return oversold, overbought
}

func (s *RSIDivergenceStrategy) State(symbol string) string {
	rsiState, ok := s.rsi[symbol]
	if !ok {
		return "waiting for data"
	}
	val := rsiState.Value()
	oversold, overbought := s.getThresholds(symbol)

	s.muCtx.RLock()
	adx := s.adx[symbol]
	s.muCtx.RUnlock()

	ctxStr := ""
	if adx > 0 && adx < 15.0 {
		ctxStr = fmt.Sprintf(" | LowVol (ADX:%.1f) dynamic thresholds enabled", adx)
	}

	wait := fmt.Sprintf("Wait for RSI <= %.0f or >= %.0f", oversold, overbought)
	return fmt.Sprintf("RSI: %.2f | %s%s", val, wait, ctxStr)
}

func (s *RSIDivergenceStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed {
		return // only feed closed candles to RSI
	}
	// Only care about our timeframe
	if k.Kline.Interval != s.cfg.Timeframe {
		return
	}
	sym := k.Symbol
	if _, ok := s.rsi[sym]; !ok {
		s.rsi[sym] = indicators.NewRSIState(s.cfg.RsiPeriod)
	}
	s.rsi[sym].Add(k.Kline.Close)
}

func (s *RSIDivergenceStrategy) OnMarkPrice(mp stream.WsMarkPrice)        {}
func (s *RSIDivergenceStrategy) OnOrderUpdate(u stream.WsOrderUpdate)     {}
func (s *RSIDivergenceStrategy) OnAccountUpdate(u stream.WsAccountUpdate) {}

func (s *RSIDivergenceStrategy) SetMarketContext(symbol string, adx float64) {
	s.muCtx.Lock()
	defer s.muCtx.Unlock()
	s.adx[symbol] = adx
}

func (s *RSIDivergenceStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	rsiState, ok := s.rsi[symbol]
	if !ok {
		return nil
	}
	val := rsiState.Value()

	// Exit Logic
	if pos != nil && pos.PositionAmt != 0 {
		oversold, overbought := s.getThresholds(symbol)
		// Long: exit when RSI reaches overbought (full mean reversion), not just 50
		if pos.PositionAmt > 0 && val >= overbought {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: fmt.Sprintf("RSI Mean Reverted to Overbought (%.1f >= %.0f)", val, overbought),
			}}
		}
		// Short: exit when RSI reaches oversold (full mean reversion), not just 50
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

	if val <= oversold {
		return []*strategy.Signal{{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideBuy,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("RSI Oversold Bounce (%.2f <= %.0f)", val, oversold),
		}}
	} else if val >= overbought {
		return []*strategy.Signal{{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     strategy.SideSell,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("RSI Overbought Drop (%.2f >= %.0f)", val, overbought),
		}}
	}

	// Transparency: Log why we didn't Enter if we are getting close (within 5 points)
	if val <= oversold+5.0 || val >= overbought-5.0 {
		s.log.Info("strategy evaluation (near boundary)",
			zap.String("strategy", s.Name()),
			zap.String("symbol", symbol),
			zap.Float64("rsi", val),
			zap.Float64("oversold", oversold),
			zap.Float64("overbought", overbought),
		)
	}

	// No signal, log the reason/state for user transparency
	s.log.Info("strategy evaluation",
		zap.String("strategy", s.Name()),
		zap.String("symbol", symbol),
		zap.String("state", s.State(symbol)),
	)

	return nil
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
