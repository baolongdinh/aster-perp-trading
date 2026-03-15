// Package engine is the bot orchestrator — ties together strategies, streams, risk, and orders.
package engine

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/ordermanager"
	"aster-bot/internal/risk"
	"aster-bot/internal/strategy"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

// Engine orchestrates bot operations.
type Engine struct {
	cfg        *config.Config
	futures    *client.FuturesClient
	market     *client.MarketClient
	risk       *risk.Manager
	orders     *ordermanager.Manager
	strategies []strategy.Strategy
	log        *zap.Logger

	marketStream *stream.MarketStream
	userStream   *stream.UserStream

	mu        sync.RWMutex
	positions map[string]*client.Position // symbol -> current position
	prices    map[string]float64          // symbol -> latest mark price
	tickers   map[string]stream.WsBookTicker // symbol -> latest best bid/ask
	workers   map[string]*MarketWorker    // symbol -> worker

	running   bool
}


// New creates a new Engine.
func New(
	cfg *config.Config,
	futures *client.FuturesClient,
	market *client.MarketClient,
	riskMgr *risk.Manager,
	orderMgr *ordermanager.Manager,
	strategies []strategy.Strategy,
	log *zap.Logger,
) *Engine {
	return &Engine{
		cfg:        cfg,
		futures:    futures,
		market:     market,
		risk:       riskMgr,
		orders:     orderMgr,
		strategies: strategies,
		log:        log,
		positions:  make(map[string]*client.Position),
		prices:     make(map[string]float64),
		tickers:    make(map[string]stream.WsBookTicker),
		workers:    make(map[string]*MarketWorker),

	}
}


// Start initialises everything and begins the trading loop.
func (e *Engine) Start(ctx context.Context) error {
	e.log.Info("engine starting",
		zap.Bool("dry_run", e.cfg.Bot.DryRun),
		zap.Int("strategies", len(e.strategies)),
	)

	// 1. Reconcile open orders from exchange
	if err := e.orders.Reconcile(ctx); err != nil {
		return fmt.Errorf("engine: reconcile: %w", err)
	}

	// 2. Fetch current positions
	if err := e.refreshPositions(ctx); err != nil {
		e.log.Warn("engine: could not refresh positions on start", zap.Error(err))
	}

	// 3. Set leverage for all symbols
	e.initLeverage(ctx)

	// 4. Pre-warm strategies with historical klines
	e.prewarmStrategies(ctx)

	// 5. Build stream subscriptions (all symbols across all strategies)
	symbols := e.allSymbols()
	streams := buildStreams(symbols, e.cfg)

	// 6. Wire WebSocket handlers
	handlers := stream.MarketHandlers{
		OnKline:      e.onKline,
		OnMarkPrice:  e.onMarkPrice,
		OnAggTrade:   nil,
		OnBookTicker: e.onBookTicker,
	}

	e.marketStream = stream.NewMarketStream(e.cfg.Exchange.FuturesWSBase, streams, handlers, e.log)

	userHandlers := stream.UserStreamHandlers{
		OnOrderUpdate:   e.orders.OnOrderUpdate,
		OnAccountUpdate: e.onAccountUpdate,
	}
	e.userStream = stream.NewUserStream(
		e.cfg.Exchange.FuturesWSBase,
		func(ctx context.Context) (string, error) { return e.futures.StartListenKey(ctx) },
		func(ctx context.Context) error { return e.futures.KeepaliveListenKey(ctx) },
		userHandlers,
		e.log,
	)

	e.mu.Lock()
	e.running = true
	e.mu.Unlock()

	// 7. Start streams in goroutines
	go e.marketStream.Run(ctx)
	go e.userStream.Run(ctx)

	// 8. Spawn parallel market workers (one per symbol)
	for _, sym := range symbols {
		worker := NewMarketWorker(sym, e, e.log)
		e.workers[sym] = worker
		go worker.Run(ctx)
	}

	// 9. Position refresh loop (every 30s for unrealized PnL)

	go e.positionRefreshLoop(ctx)

	e.log.Info("engine started")
	return nil
}

// Stop gracefully shuts down the engine.
func (e *Engine) Stop() {
	e.mu.Lock()
	e.running = false
	e.mu.Unlock()
	e.log.Info("engine stopped")
}

// Strategies returns the list of strategies (for API use).
func (e *Engine) Strategies() []strategy.Strategy {
	return e.strategies
}

// Positions returns the current positions snapshot.
func (e *Engine) Positions() map[string]*client.Position {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]*client.Position, len(e.positions))
	for k, v := range e.positions {
		cp := *v
		out[k] = &cp
	}
	return out
}

// IsRunning returns whether the engine is active.
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// --- Internal handlers ---

func (e *Engine) onKline(k stream.WsKline) {
	if k.Kline.IsClosed {
		// e.log.Debug("kline closed", zap.String("symbol", k.Symbol), zap.String("interval", k.Kline.Interval), zap.Float64("close", k.Kline.Close))
	}
	for _, s := range e.strategies {
		if s.IsEnabled() {
			s.OnKline(k)
		}
	}
}

func (e *Engine) onMarkPrice(mp stream.WsMarkPrice) {
	e.mu.Lock()
	e.prices[mp.Symbol] = mp.MarkPrice
	e.mu.Unlock()

	for _, s := range e.strategies {
		if s.IsEnabled() {
			s.OnMarkPrice(mp)
		}
	}
}


func (e *Engine) onBookTicker(bt stream.WsBookTicker) {
	e.mu.Lock()
	e.tickers[bt.Symbol] = bt
	e.mu.Unlock()
}


func (e *Engine) onAccountUpdate(u stream.WsAccountUpdate) {
	for _, s := range e.strategies {
		s.OnAccountUpdate(u)
	}
	// Update position state from account update
	e.mu.Lock()
	for _, pos := range u.Update.Positions {
		if pos.PositionAmt != 0 {
			e.positions[pos.Symbol] = &client.Position{
				Symbol:           pos.Symbol,
				PositionAmt:      pos.PositionAmt,
				EntryPrice:       pos.EntryPrice,
				UnrealizedProfit: pos.UnrealizedPnL,
				PositionSide:     pos.PositionSide,
			}
		} else {
			// Check if we just closed a position to record PnL
			if old, ok := e.positions[pos.Symbol]; ok && old.PositionAmt != 0 {
				e.risk.OnPositionClosed(pos.Symbol, pos.AccumulatedRealPnL) // Note: this is a total, not a delta. TODO: refine delta calculation.
			}
			delete(e.positions, pos.Symbol)
		}
	}
	e.mu.Unlock()
}



// Position refresh loop (every 30s for unrealized PnL)
func (e *Engine) positionRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.refreshPositions(ctx); err != nil {
				e.log.Warn("position refresh error", zap.Error(err))
			}

			ticks++
			// Print heartbeat every 5 minutes (10 ticks)
			if ticks >= 10 {
				ticks = 0
				e.mu.RLock()
				pnl := e.risk.DailyPnL()
				openPos := len(e.positions)
				e.mu.RUnlock()
				e.log.Info("bot heartbeat",
					zap.Int("open_positions", openPos),
					zap.Float64("daily_pnl", pnl),
					zap.Bool("risk_paused", e.risk.IsPaused()),
				)
			}
		}
	}
}

func (e *Engine) executeSignal(ctx context.Context, sig *strategy.Signal, currentPos *client.Position) {

	switch sig.Type {
	case strategy.SignalEnter:
		// Check risk
		notional := parseFloat(sig.Quantity)
		if err := e.risk.CanEnter(sig.Symbol, notional); err != nil {
			e.log.Warn("risk blocked entry", zap.Error(err))
			return
		}

		// PHASE 3: Slippage/Spread Protection
		e.mu.RLock()
		ticker, ok := e.tickers[sig.Symbol]
		e.mu.RUnlock()
		if ok && ticker.AskPrice > 0 {
			spread := (ticker.AskPrice - ticker.BidPrice) / ticker.AskPrice
			if spread > 0.002 { // 0.2%
				e.log.Warn("IQ-FILTER: Entry blocked due to high spread", 
					zap.String("symbol", sig.Symbol), 
					zap.Float64("spread_pct", spread*100),
				)
				return
			}
		}

		slPrice := 0.0

		// SL will be computed after fill for MARKET orders (we don't know entry price yet)
		// For now, pass 0 and the order manager handles it post-fill
		if _, err := e.orders.PlaceMarketEntry(ctx, sig.Symbol, string(sig.Side), sig.Quantity, slPrice, sig.TakeProfit, sig.StrategyName); err != nil {
			e.log.Error("place entry failed", zap.Error(err), zap.String("symbol", sig.Symbol))
			return
		}
		e.risk.OnPositionOpened(sig.Symbol)


	case strategy.SignalExit:
		if currentPos == nil || currentPos.PositionAmt == 0 {
			return
		}
		qty := strconv.FormatFloat(abs(currentPos.PositionAmt), 'f', 8, 64)
		if _, err := e.orders.PlaceCloseOrder(ctx, sig.Symbol, string(sig.Side), qty); err != nil {
			e.log.Error("place close failed", zap.Error(err), zap.String("symbol", sig.Symbol))
		}
	}
}

func (e *Engine) refreshPositions(ctx context.Context) error {
	positions, err := e.futures.GetPositions(ctx, "")
	if err != nil {
		return err
	}
	e.mu.Lock()
	m := make(map[string]*client.Position)
	for i := range positions {
		p := positions[i]
		if p.PositionAmt != 0 {
			e.positions[p.Symbol] = &p
			m[p.Symbol] = &p
		}
	}
	e.mu.Unlock()
	e.risk.SetOpenPositions(m)

	// Update starting equity for drawdown circuit breaker
	account, err := e.futures.GetAccount(ctx)
	if err == nil {
		e.risk.SetInitialEquity(account.TotalMarginBalance)
	}

	return nil
}





func (e *Engine) initLeverage(ctx context.Context) {

	for _, strat := range e.strategies {
		if !strat.IsEnabled() {
			continue
		}
		// Get leverage from strategy config (cast if supported)
		// For now, default to the risk config; strategies may override per symbol
		for _, sym := range strat.Symbols() {
			if err := e.futures.SetLeverage(ctx, client.SetLeverageRequest{
				Symbol:   sym,
				Leverage: 5, // default; TODO: pull from strategy params
			}); err != nil {
				e.log.Warn("set leverage error", zap.String("symbol", sym), zap.Error(err))
			}
		}
	}
}

func (e *Engine) allSymbols() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range e.strategies {
		for _, sym := range s.Symbols() {
			if !seen[sym] {
				seen[sym] = true
				out = append(out, sym)
			}
		}
	}
	return out
}

func (e *Engine) prewarmStrategies(ctx context.Context) {
	e.log.Info("pre-warming strategies with historical data")
	for _, strat := range e.strategies {
		if !strat.IsEnabled() {
			continue
		}
		
		// 1. Pre-warm primary execution timeframe (5m)
		limit := 100 
		interval := "5m" 

		for _, sym := range strat.Symbols() {
			klines, err := e.market.Klines(ctx, sym, interval, limit)
			if err != nil {
				e.log.Warn("failed to fetch historical 5m klines", zap.String("symbol", sym), zap.Error(err))
				continue
			}

			for i, k := range klines {
				isClosed := i < len(klines)-1
				wk := stream.WsKline{Symbol: sym}
				wk.Kline.Interval = interval
				wk.Kline.High = k.High
				wk.Kline.Low = k.Low
				wk.Kline.Close = k.Close
				wk.Kline.IsClosed = isClosed
				strat.OnKline(wk)
			}
			e.log.Info("pre-warmed symbol (5m)", zap.String("strategy", strat.Name()), zap.String("symbol", sym), zap.Int("klines", len(klines)))

			// 2. Pre-warm HTF timeframe (1h) for filtering
			hLimit := 60 // 60 hours is enough for EMA 50
			hInterval := "1h"
			hKlines, err := e.market.Klines(ctx, sym, hInterval, hLimit)
			if err == nil {
				for i, k := range hKlines {
					isClosed := i < len(hKlines)-1
					wk := stream.WsKline{Symbol: sym}
					wk.Kline.Interval = hInterval
					wk.Kline.High = k.High
					wk.Kline.Low = k.Low
					wk.Kline.Close = k.Close
					wk.Kline.IsClosed = isClosed
					strat.OnKline(wk)
				}
				e.log.Info("pre-warmed symbol (1h)", zap.String("strategy", strat.Name()), zap.String("symbol", sym), zap.Int("klines", len(hKlines)))
			}
		}
	}
}


func buildStreams(symbols []string, _ *config.Config) []string {
	// Default kline intervals: 5m for signals, 1h for HTF trend filtering
	return stream.BuildStreams(symbols, []string{"5m", "1h"})
}



func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
