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
	e.syncRiskExposure()

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
		OnOrderUpdate:   e.onOrderUpdate,
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

	// 10. Initial Safety Check & Safety Monitor Loop
	e.runSafetyCheck(ctx)
	go e.safetyMonitorLoop(ctx)

	// 11. Periodic Order Reconciliation (every 30s)
	go e.reconcileLoop(ctx)

	// 12. Aggressive Order Chasing (every 2s)
	go e.chaseLoop(ctx)

	e.log.Info("engine started")
	return nil
}

func (e *Engine) chaseLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			openOrders := e.orders.GetAll()
			for _, lo := range openOrders {
				if !lo.IsChasing || lo.Purpose != "ENTRY" {
					continue
				}
				if lo.Status != "NEW" && lo.Status != "PARTIALLY_FILLED" {
					continue
				}

				// Check Top of Book
				e.mu.RLock()
				ticker, ok := e.tickers[lo.Symbol]
				e.mu.RUnlock()
				if !ok {
					continue
				}

				targetPrice := 0.0
				if string(lo.Side) == "BUY" {
					targetPrice = ticker.BidPrice
				} else {
					targetPrice = ticker.AskPrice
				}

				if targetPrice == 0 || targetPrice == lo.Price {
					continue
				}

				// Price mismatch - trigger handleLimitEntry
				// Reconstruct signal
				sig := &strategy.Signal{
					StrategyName: lo.StrategyName,
					Symbol:       lo.Symbol,
					Type:         strategy.SignalEnter,
					Side:         strategy.Side(lo.Side),
					Price:        strconv.FormatFloat(targetPrice, 'f', -1, 64),
					Quantity:     strconv.FormatFloat(lo.OrigQty*lo.Price, 'f', -1, 64), // original notional
					StopLoss:     lo.SLPrice,
					TakeProfit:   lo.TPPrice,
				}
				// handleLimitEntry will handle the replacement/drift check (0.00001 threshold for chasing)
				e.handleLimitEntry(ctx, sig, true)
			}
		}
	}
}

func (e *Engine) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.orders.Reconcile(ctx); err != nil {
				e.log.Warn("engine: periodic reconcile error", zap.Error(err))
			} else {
				e.syncRiskExposure()
			}
		}
	}
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

func (e *Engine) syncRiskExposure() {
	m := make(map[string]float64)
	orders := e.orders.GetAll()
	var totalPendingMargin float64

	for _, lo := range orders {
		if lo.Purpose == ordermanager.PurposeEntry && (lo.Status == "NEW" || lo.Status == "PARTIALLY_FILLED") {
			// Phase 13: Use remaining quantity to avoid double-counting
			remaining := lo.OrigQty - lo.ExecutedQty
			if remaining > 0 {
				notional := lo.Price * remaining
				m[lo.Symbol] += notional

				// Fetch leverage for this strategy to calculate margin
				leverage := 20.0
				for _, sc := range e.cfg.Strategies {
					if sc.Name == lo.StrategyName {
						if l, ok := sc.Params["leverage"]; ok {
							switch v := l.(type) {
							case int:
								leverage = float64(v)
							case float64:
								leverage = v
							}
						}
						break
					}
				}
				totalPendingMargin += (notional / leverage)
			}
		}
	}
	e.risk.SetPendingOrders(m, totalPendingMargin)
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

func (e *Engine) onOrderUpdate(ou stream.WsOrderUpdate) {
	e.orders.OnOrderUpdate(ou)
	// Reactive Sync: Update pending exposure immediately on any trade/cancel/fill
	e.syncRiskExposure()
}

func (e *Engine) onBookTicker(bt stream.WsBookTicker) {
	e.mu.Lock()
	e.tickers[bt.Symbol] = bt
	e.mu.Unlock()
}

func (e *Engine) onAccountUpdate(u stream.WsAccountUpdate) {
	// Phase 17: Sync Available Balance to RiskManager
	for _, b := range u.Update.Balances {
		if b.Asset == "USDT" {
			e.risk.SetAvailableBalance(b.CrossWalletBalance)
			break
		}
	}

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

	// Sync risk manager with new position state
	e.refreshRiskPositions()
	e.syncRiskExposure()
}

func (e *Engine) refreshRiskPositions() {
	e.mu.RLock()
	m := make(map[string]*client.Position)
	for sym, pos := range e.positions {
		m[sym] = pos
	}
	e.mu.RUnlock()
	e.risk.SetOpenPositions(m)
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
		// Phase 15 & 19: Centralized Position Sizing
		// All entries now derive size from RiskManager (ATR-based or fallback %-based)
		atr, _ := e.getMarketContext(sig.Symbol)
		e.mu.RLock()
		lastPrice := e.prices[sig.Symbol]
		// Fallback to ticker mid if mark price missing
		if lastPrice == 0 {
			if t, ok := e.tickers[sig.Symbol]; ok && t.BidPrice > 0 {
				lastPrice = (t.BidPrice + t.AskPrice) / 2
			}
		}
		e.mu.RUnlock()

		// --- Derive SL/TP from RiskManager if strategy didn't set them ---
		sl := sig.StopLoss
		tp := sig.TakeProfit
		atr, reg := e.getMarketContext(sig.Symbol)
		if sl == 0 {
			sl = e.risk.StopLossPrice(lastPrice, string(sig.Side), atr)
		}
		if tp == 0 {
			tp = e.risk.TakeProfitPrice(lastPrice, string(sig.Side), sl, reg)
		}

		if lastPrice <= 0 {
			e.log.Warn("IQ-ENGINE: Cannot size position, price unknown", zap.String("symbol", sig.Symbol))
			return
		}

		notional, err := e.risk.CalculatePositionSize(sig.Symbol, lastPrice, sl)
		if err != nil || notional <= 0 {
			e.log.Warn("IQ-ENGINE: Risk sizing failed", zap.Error(err), zap.String("symbol", sig.Symbol))
			return
		}
		sig.Quantity = strconv.FormatFloat(notional, 'f', -1, 64)

		e.log.Info("[IQ-ENGINE] Final sized entry",
			zap.String("strategy", sig.StrategyName),
			zap.String("symbol", sig.Symbol),
			zap.Float64("notional", notional),
			zap.Float64("price", lastPrice),
			zap.Float64("sl_price", sl),
			zap.Float64("tp_price", tp),
		)

		// Phase 17: Margin Awareness - check if account can afford the trade
		leverage := 20.0 // Default fallback
		for _, sc := range e.cfg.Strategies {
			if sc.Name == sig.StrategyName {
				if l, ok := sc.Params["leverage"]; ok {
					switch v := l.(type) {
					case int:
						leverage = float64(v)
					case float64:
						leverage = v
					}
				}
				break
			}
		}

		if err := e.risk.CanAfford(sig.Symbol, notional, leverage); err != nil {
			e.log.Warn("margin blocked entry", zap.Error(err))
			return
		}

		if err := e.risk.CanEnter(sig.Symbol, notional, leverage); err != nil {
			e.log.Warn("risk blocked entry", zap.Error(err))
			return
		}

		// MAKER PRIORITY: Convert empty price signals to Limit at Best Bid/Ask
		isChasing := false
		if sig.Price == "" && e.cfg.Risk.MakerPriority {
			isChasing = true
			e.mu.RLock()
			ticker, ok := e.tickers[sig.Symbol]
			e.mu.RUnlock()
			if ok {
				if sig.Side == strategy.SideBuy && ticker.BidPrice > 0 {
					sig.Price = e.prec.RoundPrice(sig.Symbol, ticker.BidPrice)
					e.log.Info("IQ-MAKER: Converting Market to Limit (Bid)", zap.String("symbol", sig.Symbol), zap.String("price", sig.Price))
				} else if sig.Side == strategy.SideSell && ticker.AskPrice > 0 {
					sig.Price = e.prec.RoundPrice(sig.Symbol, ticker.AskPrice)
					e.log.Info("IQ-MAKER: Converting Market to Limit (Ask)", zap.String("symbol", sig.Symbol), zap.String("price", sig.Price))
				}
			}
		}

		// LIMIT ORDER PATH
		if sig.Price != "" {
			e.handleLimitEntry(ctx, sig, isChasing)
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
		lastPrice = e.prices[sig.Symbol]
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

		// SL/TP are already calculated at the top before sizing.

		// --- REALTIME SERVER CHECK BEFORE PLACEMENT ---
		if err := e.VerifyNoStackingServer(ctx, sig.Symbol); err != nil {
			e.log.Warn("IQ-SERVER: Server blocked entry (stacking)", zap.Error(err), zap.String("symbol", sig.Symbol))
			return
		}

		if _, err := e.orders.PlaceMarketEntry(ctx, sig.Symbol, string(sig.Side), roundedQty, sl, tp, sig.StrategyName); err != nil {
			e.log.Error("place entry failed", zap.Error(err), zap.String("symbol", sig.Symbol))
			return
		}
		e.risk.OnPositionOpened(sig.Symbol, notional)

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

func (e *Engine) handleLimitEntry(ctx context.Context, sig *strategy.Signal, isChasing bool) {
	newPriceFloat, _ := strconv.ParseFloat(sig.Price, 64)
	if newPriceFloat == 0 {
		return
	}

	// 1. Round Price FIRST (needed for drift check)
	roundedPriceStr := e.prec.RoundPrice(sig.Symbol, newPriceFloat)
	roundedPriceFloat, _ := strconv.ParseFloat(roundedPriceStr, 64)

	// SLOT RULE: If a pending entry already exists on this side for THIS strategy, only proceed if price has drifted.
	// We check THIS first to allow "replacements" to bypass the global cap.
	existing := e.orders.FindEntryByStrategy(sig.Symbol, sig.StrategyName, string(sig.Side))
	if existing != nil {
		// Drift check
		drift := math.Abs(roundedPriceFloat-existing.Price) / existing.Price

		// If chasing, any price change counts. If standard limit, needs 0.1% drift.
		threshold := 0.001
		if existing.IsChasing || isChasing {
			threshold = 0.00001
		}

		if drift < threshold {
			e.log.Debug("IQ-LIMIT: Drift below threshold, skipping update",
				zap.String("symbol", sig.Symbol),
				zap.Float64("drift", drift),
				zap.Float64("threshold", threshold),
			)
			return
		}

		e.log.Info("IQ-LIMIT: Price change detected, updating limit order",
			zap.String("strategy", sig.StrategyName),
			zap.String("symbol", sig.Symbol),
			zap.Float64("old_price", existing.Price),
			zap.Float64("new_price", roundedPriceFloat),
			zap.Bool("is_chasing", existing.IsChasing || isChasing),
		)
		// Cancel old before placing new
		_, cancelErr := e.futures.CancelOrder(ctx, client.CancelOrderRequest{
			Symbol:        sig.Symbol,
			ClientOrderID: existing.ClientOrderID,
		})
		if cancelErr == nil {
			e.orders.SetOrderStatus(existing.OrderID, "CANCELED")
		}
	} else {
		// New entry slot - check caps
		maxGlobalPending := e.cfg.Risk.MaxGlobalPendingLimitOrders
		if maxGlobalPending == 0 {
			maxGlobalPending = 10 // Increased default
		}
		if e.orders.CountAllPendingEntries() >= maxGlobalPending {
			e.log.Warn("IQ-SLOT: Global limit order cap reached, skipping",
				zap.String("symbol", sig.Symbol),
				zap.Int("cap", maxGlobalPending),
			)
			return
		}

		maxPerSide := e.cfg.Risk.MaxPendingPerSide
		if maxPerSide == 0 {
			maxPerSide = 1 // default
		}
		if e.orders.CountPendingBySide(sig.Symbol, string(sig.Side)) >= maxPerSide {
			e.log.Info("IQ-SLOT: Per-side limit order cap reached, skipping",
				zap.String("symbol", sig.Symbol),
				zap.String("side", string(sig.Side)),
				zap.Int("cap", maxPerSide),
			)
			return
		}
	}

	// 3. Derive SL/TP BEFORE sizing
	sl := sig.StopLoss
	tp := sig.TakeProfit
	atr, reg := e.getMarketContext(sig.Symbol)
	if sl == 0 {
		sl = e.risk.StopLossPrice(roundedPriceFloat, string(sig.Side), atr)
	}
	if tp == 0 {
		tp = e.risk.TakeProfitPrice(roundedPriceFloat, string(sig.Side), sl, reg)
	}

	// Phase 19: Centralized Sizing for Limit Orders based on true SL distance
	notional, err := e.risk.CalculatePositionSize(sig.Symbol, roundedPriceFloat, sl)
	if err != nil || notional <= 0 {
		e.log.Warn("IQ-LIMIT: Risk sizing failed", zap.Error(err), zap.String("symbol", sig.Symbol))
		return
	}

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

	// --- REALTIME SERVER CHECK BEFORE PLACEMENT ---
	if err := e.VerifyNoStackingServer(ctx, sig.Symbol); err != nil {
		e.log.Warn("IQ-SERVER: Server blocked limit entry (stacking)", zap.Error(err), zap.String("symbol", sig.Symbol))
		return
	}

	// Place new limit order
	_, err = e.orders.PlaceLimitEntry(ctx, sig.Symbol, string(sig.Side), roundedQtyStr, roundedPriceStr, sl, tp, sig.StrategyName, isChasing)
	if err != nil {
		e.log.Error("place limit entry failed", zap.Error(err), zap.String("symbol", sig.Symbol))
	}
}

// VerifyNoStackingServer queries the exchange to absolutely guarantee no stacking occurs.
func (e *Engine) VerifyNoStackingServer(ctx context.Context, symbol string) error {
	// 1. Check Positions
	positions, err := e.futures.GetPositions(ctx, symbol)
	if err != nil {
		return fmt.Errorf("failed to verify positions: %w", err)
	}
	for _, p := range positions {
		if math.Abs(p.PositionAmt) > 0 {
			return fmt.Errorf("active position already exists on server")
		}
	}

	// 2. Check Open Orders
	openOrders, err := e.futures.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("failed to verify open orders: %w", err)
	}
	
	entryFound := false
	for _, o := range openOrders {
		// Stop/Take profit orders are fine, but limit entries are not
		if o.Type == "LIMIT" || o.Type == "MARKET" {
			entryFound = true
			break
		}
	}

	if entryFound {
		return fmt.Errorf("active limit/market entry order already exists on server")
	}

	return nil
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
				e.risk.RemovePending(lo.Symbol, lo.Price*lo.OrigQty, 20.0) // fallback leverage
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
	e.refreshRiskPositions()

	// Update starting equity and available balance
	account, err := e.futures.GetAccount(ctx)
	if err == nil {
		e.log.Info("IQ-RISK: Initializing account balance",
			zap.Float64("total_margin", account.TotalMarginBalance),
			zap.Float64("available", account.AvailableBalance),
		)
		e.risk.SetInitialEquity(account.TotalMarginBalance)
		e.risk.SetAvailableBalance(account.AvailableBalance)
	} else {
		e.log.Warn("IQ-RISK: Failed to fetch account balance on startup", zap.Error(err))
	}

	return nil
}

func (e *Engine) initLeverage(ctx context.Context) {
	e.log.Info("[DEBUG LEVERAGE] initLeverage starting", zap.Int("strategy_configs", len(e.cfg.Strategies)))
	var maxLvgs = make(map[string]int)

	for _, sc := range e.cfg.Strategies {
		if !sc.Enabled {
			continue
		}
		var stratLvg int
		if v, ok := sc.Params["leverage"]; ok {
			switch t := v.(type) {
			case int:
				stratLvg = t
			case int32:
				stratLvg = int(t)
			case int64:
				stratLvg = int(t)
			case float64:
				stratLvg = int(t)
			case float32:
				stratLvg = int(t)
			case string:
				if parsed, err := strconv.Atoi(t); err == nil {
					stratLvg = parsed
				}
			default:
				// Fallback to sprintf
				if parsed, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil {
					stratLvg = parsed
				}
			}
		}
		if stratLvg <= 0 {
			stratLvg = 5 // fallback safely
		}

		for _, sym := range sc.Symbols {
			e.log.Info("[DEBUG LEVERAGE] Raw maxLvgs parsing", zap.String("strategy", sc.Name), zap.String("symbol", sym), zap.Int("parsedLvg", stratLvg))
			if stratLvg > maxLvgs[sym] {
				maxLvgs[sym] = stratLvg
			}
		}
	}

	for sym, lvg := range maxLvgs {
		e.log.Info("[DEBUG LEVERAGE] Sending SetLeverage to API", zap.String("symbol", sym), zap.Int("leverage", lvg))
		if err := e.futures.SetLeverage(ctx, client.SetLeverageRequest{
			Symbol:   sym,
			Leverage: lvg,
		}); err != nil {
			e.log.Warn("[DEBUG LEVERAGE] set leverage error", zap.String("symbol", sym), zap.Error(err))
		} else {
			e.log.Info("[DEBUG LEVERAGE] leverage configured SUCCESS", zap.String("symbol", sym), zap.Int("leverage", lvg))
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
		for _, interval := range s.RequiredIntervals() {
			if interval != "" {
				intervals[interval] = true
			}
		}
	}

	var out []string
	for k := range intervals {
		out = append(out, k)
	}
	return out
}

func (e *Engine) getMarketContext(symbol string) (float64, string) {
	for _, s := range e.strategies {
		if r, ok := s.(*strategy.Router); ok {
			return r.GetMarketContext(symbol)
		}
	}
	return 0, ""
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

// safetyMonitorLoop checks every 60s if open positions have SL and TP, and heals them if not.
func (e *Engine) safetyMonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.runSafetyCheck(ctx)
		}
	}
}

func (e *Engine) runSafetyCheck(ctx context.Context) {
	e.mu.RLock()
	positions := make([]*client.Position, 0, len(e.positions))
	for _, pos := range e.positions {
		cp := *pos
		positions = append(positions, &cp)
	}
	e.mu.RUnlock()

	for _, pos := range positions {
		side := "BUY"
		if pos.PositionAmt < 0 {
			side = "SELL"
		}

		atr, reg := e.getMarketContext(pos.Symbol)
		sl := e.risk.StopLossPrice(pos.EntryPrice, side, atr)
		tp := e.risk.TakeProfitPrice(pos.EntryPrice, side, sl, reg)

		// Use MarkPrice for validation
		e.mu.RLock()
		curPrice := e.prices[pos.Symbol]
		e.mu.RUnlock()

		e.orders.EnsureProtectiveOrders(ctx, pos.Symbol, string(pos.PositionSide), pos.PositionAmt, sl, tp, curPrice)
	}
}
