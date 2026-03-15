package engine

import (
	"context"
	"time"

	"aster-bot/internal/strategy"
	"go.uber.org/zap"
)

// MarketWorker handles logic for a single symbol.
type MarketWorker struct {
	symbol string
	engine *Engine
	log    *zap.Logger
}

func NewMarketWorker(symbol string, engine *Engine, log *zap.Logger) *MarketWorker {
	return &MarketWorker{
		symbol: symbol,
		engine: engine,
		log:    log,
	}
}

func (w *MarketWorker) Run(ctx context.Context) {

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	w.log.Debug("market worker started", zap.String("symbol", w.symbol))

	ticks := 0
	for {
		select {
		case <-ctx.Done():
			w.log.Debug("market worker stopping", zap.String("symbol", w.symbol))
			return
		case <-ticker.C:
			w.engine.evaluateSignalsForSymbol(ctx, w.symbol, ticks)
			ticks++
			if ticks >= 60 { // Reset every minute, heartbeat is every 15 ticks
				ticks = 0
			}
		}
	}
}

func (e *Engine) evaluateSignalsForSymbol(ctx context.Context, sym string, ticks int) {
	if e.risk.IsPaused() {
		return
	}

	e.mu.RLock()
	pos := e.positions[sym]
	e.mu.RUnlock()

	for _, s := range e.strategies {
		if !s.IsEnabled() {
			continue
		}
		
		// If it's a router, it will handle its own symbol filtering inside Signal()
		// If it's a basic strategy, we should check if it's interested in this symbol
		if !contains(s.Symbols(), sym) {
			continue
		}

		// Periodic status heartbeat
		if ticks%15 == 0 {
			e.log.Info("strategy status",
				zap.String("strategy", s.Name()),
				zap.String("symbol", sym),
				zap.String("state", s.State(sym)),
			)
		}

		sig := s.Signal(sym, pos)

		if sig == nil || sig.Type == strategy.SignalNone {
			continue
		}

		e.log.Info("signal",
			zap.String("strategy", sig.StrategyName),
			zap.String("symbol", sym),
			zap.String("type", string(sig.Type)),
			zap.String("side", string(sig.Side)),
			zap.String("reason", sig.Reason),
		)
		e.executeSignal(ctx, sig, pos)
	}
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
