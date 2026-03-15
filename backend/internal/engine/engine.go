// Package engine is the bot orchestrator — ties together strategies, streams, risk, and orders.
package engine

import (
	"context"
	"fmt"
	"math"
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
	prec       *client.PrecisionManager
	log        *zap.Logger

	marketStream *stream.MarketStream
	userStream   *stream.UserStream

	mu        sync.RWMutex
	positions map[string]*client.Position    // symbol -> current position
	prices    map[string]float64             // symbol -> latest mark price
	tickers   map[string]stream.WsBookTicker // symbol -> latest best bid/ask
	workers   map[string]*MarketWorker       // symbol -> worker

	running bool
}

// New creates a new Engine.
func New(
	cfg *config.Config,
	futures *client.FuturesClient,
	market *client.MarketClient,
	riskMgr *risk.Manager,
	orderMgr *ordermanager.Manager,
	prec *client.PrecisionManager, // NEW
	strategies []strategy.Strategy,
	log *zap.Logger,
) *Engine {
	return &Engine{
		cfg:        cfg,
		futures:    futures,
		market:     market,
		risk:       riskMgr,
		orders:     orderMgr,
		prec:       prec, // Assigned prec
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
	intervals := e.requiredIntervals()
	streams := buildStreams(symbols, e.cfg, intervals)

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

		// LIMIT ORDER PATH
		if sig.Price != "" {
			e.handleLimitEntry(ctx, sig)
			return
		}

		// MARKET ORDER PATH
		// PHASE 9: Position Awareness - If we already have a position, don't enter more (One-Way)
		if currentPos != nil && math.Abs(currentPos.PositionAmt) > 0 {
			e.log.Debug("IQ-ENGINE: Side already has position, skipping market entry", zap.String("symbol", sig.Symbol))
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

		// --- Quantity Conversion & Rounding ---
		e.mu.RLock()
		lastPrice := e.prices[sig.Symbol]
		// Fallback: if mark price not yet received from stream, use book ticker mid-price
		if lastPrice == 0 {
			if t, ok := e.tickers[sig.Symbol]; ok && t.BidPrice > 0 {
				lastPrice = (t.BidPrice + t.AskPrice) / 2
			}
		}
		e.mu.RUnlock()
		if lastPrice == 0 {
			e.log.Warn("cannot enter: price unknown, skipping (will retry next tick)",
				zap.String("symbol", sig.Symbol))
			return
		}

		coinQty := notional / lastPrice
		roundedQty := e.prec.RoundQty(sig.Symbol, coinQty)

		// check if quantity is zero after rounding (order too small)
		if q, _ := strconv.ParseFloat(roundedQty, 64); q <= 0 {
			e.log.Warn("IQ-FILTER: Order too small after rounding, skipping",
				zap.String("symbol", sig.Symbol),
				zap.Float64("notional", notional),
				zap.Float64("price", lastPrice),
				zap.Float64("raw_qty", coinQty),
			)
			return
		}

		// --- Derive SL/TP from RiskManager if strategy didn't set them ---
		sl := sig.StopLoss
		tp := sig.TakeProfit
		if sl == 0 {
			sl = e.risk.StopLossPrice(lastPrice, string(sig.Side))
		}
		if tp == 0 {
			tp = e.risk.TakeProfitPrice(lastPrice, string(sig.Side))
		}

		if _, err := e.orders.PlaceMarketEntry(ctx, sig.Symbol, string(sig.Side), roundedQty, sl, tp, sig.StrategyName); err != nil {
			e.log.Error("place entry failed", zap.Error(err), zap.String("symbol", sig.Symbol))
			return
		}
		e.risk.OnPositionOpened(sig.Symbol)

	case strategy.SignalExit:
		if currentPos == nil || currentPos.PositionAmt == 0 {
			return
		}
		// Quantity for exit is current position amount
		qty := e.prec.RoundQty(sig.Symbol, math.Abs(currentPos.PositionAmt))
		if _, err := e.orders.PlaceCloseOrder(ctx, sig.Symbol, string(sig.Side), qty); err != nil {
			e.log.Error("place close failed", zap.Error(err), zap.String("symbol", sig.Symbol))
		}
	}
}

func (e *Engine) handleLimitEntry(ctx context.Context, sig *strategy.Signal) {
	newPriceFloat, _ := strconv.ParseFloat(sig.Price, 64)
	if newPriceFloat == 0 {
		return
	}

	// 1. Round Price FIRST (needed for drift check)
	roundedPriceStr := e.prec.RoundPrice(sig.Symbol, newPriceFloat)
	roundedPriceFloat, _ := strconv.ParseFloat(roundedPriceStr, 64)

	// SLOT RULE 1: Global cap
	maxGlobalPending := e.cfg.Risk.MaxGlobalPendingLimitOrders
	if maxGlobalPending == 0 {
		maxGlobalPending = 5 // fallback
	}
	if e.orders.CountAllPendingEntries() >= maxGlobalPending {
		e.log.Warn("IQ-SLOT: Global limit order cap reached, skipping",
			zap.String("symbol", sig.Symbol),
			zap.Int("cap", maxGlobalPending),
		)
		return
	}

	// SLOT RULE 2 + DRIFT CHECK:
	// If a pending entry already exists on this side, only proceed if price has drifted enough.
	existing := e.orders.FindEntryByStrategy(sig.Symbol, sig.StrategyName, string(sig.Side))
	if existing != nil {
		// A pending order already holds this slot — only replace if price drifted > 0.1%
		drift := math.Abs(roundedPriceFloat-existing.Price) / existing.Price
		if drift < 0.001 {
			return // same price, no action needed
		}
		e.log.Info("IQ-LIMIT: Price drift detected, updating limit order",
			zap.String("strategy", sig.StrategyName),
			zap.String("symbol", sig.Symbol),
			zap.Float64("old_price", existing.Price),
			zap.Float64("new_price", roundedPriceFloat),
		)
		// Cancel the old order to free the slot before placing new one
		_, cancelErr := e.futures.CancelOrder(ctx, client.CancelOrderRequest{
			Symbol:        sig.Symbol,
			ClientOrderID: existing.ClientOrderID,
		})
		if cancelErr == nil {
			e.orders.SetOrderStatus(existing.OrderID, "CANCELED")
			e.risk.RemovePending(sig.Symbol, existing.Price*existing.OrigQty)
		}
	} else {
		maxPerSide := e.cfg.Risk.MaxPendingPerSide
		if maxPerSide == 0 {
			maxPerSide = 1 // fallback
		}
		if e.orders.CountPendingBySide(sig.Symbol, string(sig.Side)) >= maxPerSide {
			// Another strategy already holds this side's slot
			return
		}
	}

	notional := parseFloat(sig.Quantity)
	coinQty := notional / roundedPriceFloat
	roundedQtyStr := e.prec.RoundQty(sig.Symbol, coinQty)

	// check if quantity is zero after rounding (order too small)
	if q, _ := strconv.ParseFloat(roundedQtyStr, 64); q <= 0 {
		e.log.Warn("IQ-LIMIT: Order too small after rounding, skipping",
			zap.String("symbol", sig.Symbol),
			zap.Float64("notional", notional),
			zap.Float64("price", roundedPriceFloat),
			zap.Float64("raw_qty", coinQty),
		)
		return
	}

	// 3. Derive SL/TP from RiskManager if strategy didn't set them
	sl := sig.StopLoss
	tp := sig.TakeProfit
	if sl == 0 {
		sl = e.risk.StopLossPrice(roundedPriceFloat, string(sig.Side))
	}
	if tp == 0 {
		tp = e.risk.TakeProfitPrice(roundedPriceFloat, string(sig.Side))
	}

	// PHASE 9: Risk Tracking - Add to pending BEFORE placing
	e.risk.AddPending(sig.Symbol, notional)

	// Place new limit order
	_, err := e.orders.PlaceLimitEntry(ctx, sig.Symbol, string(sig.Side), roundedQtyStr, roundedPriceStr, sl, tp, sig.StrategyName)
	if err != nil {
		e.log.Error("place limit entry failed", zap.Error(err), zap.String("symbol", sig.Symbol))
		e.risk.RemovePending(sig.Symbol, notional)
	}
}

// gcOrders cross-references open entry orders with current signals.
// If a signal for a symbol/side/price is no longer there, pull the order.
func (e *Engine) gcOrders(ctx context.Context, symbol string, latestSignals []*strategy.Signal) {
	openOrders := e.orders.GetAll()

	// Build a set of "active signal keys" using exchange-rounded prices for exact match
	activeSigs := make(map[string]bool)
	for _, s := range latestSignals {
		if s.Type == strategy.SignalEnter && s.Price != "" {
			// Round to exchange precision to match stored order price
			rounded := e.prec.RoundPrice(s.Symbol, parseFloat(s.Price))
			key := fmt.Sprintf("%s|%s|%s", s.Symbol, string(s.Side), rounded)
			activeSigs[key] = true
		}
	}

	for _, lo := range openOrders {
		if lo.Symbol != symbol || lo.Purpose != ordermanager.PurposeEntry {
			continue
		}
		if lo.Type == "MARKET" {
			continue // Never GC a market order
		}
		if lo.Status != "NEW" && lo.Status != "PARTIALLY_FILLED" {
			continue
		}

		// Use same rounded representation to ensure key matches
		orderPriceStr := e.prec.RoundPrice(lo.Symbol, lo.Price)
		key := fmt.Sprintf("%s|%s|%s", lo.Symbol, string(lo.Side), orderPriceStr)

		if !activeSigs[key] {
			e.log.Info("IQ-GC: Pulling stale limit order",
				zap.String("symbol", lo.Symbol),
				zap.Float64("price", lo.Price),
				zap.String("strategy", lo.StrategyName),
			)
			_, cancelErr := e.futures.CancelOrder(ctx, client.CancelOrderRequest{
				Symbol:        lo.Symbol,
				ClientOrderID: lo.ClientOrderID,
			})
			if cancelErr == nil {
				// Immediately hide from GC to prevent loop while waiting for WS update
				e.orders.SetOrderStatus(lo.OrderID, "CANCELED")
				// Only release pending exposure after confirmed cancel
				e.risk.RemovePending(lo.Symbol, lo.Price*lo.OrigQty)
			}
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

func (e *Engine) requiredIntervals() []string {
	intervals := map[string]bool{"1h": true} // 1h is always required for HTF filtering

	for _, s := range e.strategies {
		if !s.IsEnabled() {
			continue
		}
		// Try to find timeframe in strategy config via reflection or well-known interface
		// For now, since we know our strategies, we can just assume they might use different ones.
		// A better way is to add a GetIntervals() method to Strategy interface if we have many.
		// However, Router already knows his sub-strategies.
		// Let's check them specifically or add a way to query them.

		// For now, we'll brute force common ones or better: ask strategies for their symbols/intervals.
		// Wait, most of our strategies have a Timeframe field in their config.
		// Let's assume we can get it or we keep a list of common ones.

		// Actually, let's just subscribe to the most common ones used in config.yaml:
		intervals["5m"] = true
		intervals["15m"] = true
		intervals["1h"] = true
	}

	var out []string
	for k := range intervals {
		out = append(out, k)
	}
	return out
}

func (e *Engine) prewarmStrategies(ctx context.Context) {
	e.log.Info("pre-warming strategies with historical data")
	intervals := e.requiredIntervals()

	for _, strat := range e.strategies {
		if !strat.IsEnabled() {
			continue
		}

		for _, interval := range intervals {
			limit := 100
			if interval == "1h" {
				limit = 60
			}

			for _, sym := range strat.Symbols() {
				klines, err := e.market.Klines(ctx, sym, interval, limit)
				if err != nil {
					e.log.Warn("failed to fetch historical klines",
						zap.String("symbol", sym),
						zap.String("interval", interval),
						zap.Error(err),
					)
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
				e.log.Info("pre-warmed symbol",
					zap.String("strategy", strat.Name()),
					zap.String("symbol", sym),
					zap.String("interval", interval),
					zap.Int("klines", len(klines)),
				)
			}
		}
	}
}

func buildStreams(symbols []string, _ *config.Config, intervals []string) []string {
	return stream.BuildStreams(symbols, intervals)
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
