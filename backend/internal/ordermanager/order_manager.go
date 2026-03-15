// Package ordermanager maintains a local shadow of open orders,
// reconciles with the exchange on startup, and manages SL/TP placement.
package ordermanager

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/stream"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// LocalOrder is the bot's local representation of an order.
type LocalOrder struct {
	client.Order
	SLPrice      float64 // Intended SL price for limits
	TPPrice      float64 // Intended TP price for limits
	SLID         string  // client order ID of paired stop-loss order
	TPID         string  // client order ID of paired take-profit order
	Purpose      string  // PurposeEntry | PurposeExit | PurposeSL | PurposeTP
	StrategyName string  // which strategy owns this order
}

const (
	PurposeEntry = "ENTRY"
	PurposeExit  = "EXIT"
	PurposeSL    = "SL"
	PurposeTP    = "TP"
)

// Manager maintains local order state and handles SL/TP placement.
type Manager struct {
	futures *client.FuturesClient
	log     *zap.Logger

	mu     sync.RWMutex
	orders map[int64]*LocalOrder // orderId -> order
	prec   *client.PrecisionManager
}

// NewManager creates a new order manager.
func NewManager(futures *client.FuturesClient, prec *client.PrecisionManager, log *zap.Logger) *Manager {
	return &Manager{
		futures: futures,
		log:     log,
		orders:  make(map[int64]*LocalOrder),
		prec:    prec,
	}
}

// Reconcile fetches all open orders from the exchange and syncs local state.
func (m *Manager) Reconcile(ctx context.Context) error {
	orders, err := m.futures.GetOpenOrders(ctx, "")
	if err != nil {
		return fmt.Errorf("ordermanager: reconcile: %w", err)
	}
	m.mu.Lock()
	m.orders = make(map[int64]*LocalOrder, len(orders))
	for i := range orders {
		o := orders[i]
		m.orders[o.OrderID] = &LocalOrder{Order: o}
	}
	m.mu.Unlock()
	m.log.Info("order manager reconciled", zap.Int("open_orders", len(orders)))
	return nil
}

// PlaceMarketEntry places a MARKET entry order and optionally SL/TP orders.
func (m *Manager) PlaceMarketEntry(
	ctx context.Context,
	symbol, side, qty string,
	slPrice, tpPrice float64,
	strategyName string,
) (*LocalOrder, error) {
	m.log.Info("[DEBUG ENTRY] PlaceMarketEntry called", zap.String("symbol", symbol), zap.String("side", side), zap.String("qty", qty))
	clientID := "bot_" + uuid.New().String()[:12]

	order, err := m.futures.PlaceOrder(ctx, client.PlaceOrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          "MARKET",
		Quantity:      qty,
		ClientOrderID: clientID,
	})
	if err != nil {
		return nil, fmt.Errorf("ordermanager: entry: %w", err)
	}

	lo := &LocalOrder{
		Order:        *order,
		Purpose:      PurposeEntry,
		StrategyName: strategyName,
	}
	m.mu.Lock()
	m.orders[order.OrderID] = lo
	m.mu.Unlock()

	rr := 0.0
	if slPrice > 0 && tpPrice > 0 && order.AvgPrice > 0 {
		riskVal := math.Abs(order.AvgPrice - slPrice)
		rewardVal := math.Abs(tpPrice - order.AvgPrice)
		if riskVal > 0 {
			rr = rewardVal / riskVal
		}
	}

	m.log.Info("🚀 TRADE ENTERED",
		zap.String("strategy", strategyName),
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.Float64("entry", order.AvgPrice),
		zap.Float64("sl", slPrice),
		zap.Float64("tp", tpPrice),
		zap.Float64("rr", rr),
		zap.String("qty", qty),
		zap.Int64("orderId", order.OrderID),
	)

	// Place SL/TP as STOP_MARKET/TAKE_PROFIT_MARKET
	oppositeSide := "SELL"
	if side == "SELL" {
		oppositeSide = "BUY"
	}

	if slPrice > 0 {
		slID, err := m.placeStopOrder(ctx, symbol, oppositeSide, qty, slPrice, "STOP_MARKET")
		if err != nil {
			m.log.Error("failed to place SL order", zap.Error(err))
		} else {
			m.mu.Lock()
			lo.SLID = slID
			m.mu.Unlock()
		}
	}

	if tpPrice > 0 {
		tpID, err := m.placeStopOrder(ctx, symbol, oppositeSide, qty, tpPrice, "TAKE_PROFIT_MARKET")
		if err != nil {
			m.log.Error("failed to place TP order", zap.Error(err))
		} else {
			m.mu.Lock()
			lo.TPID = tpID
			m.mu.Unlock()
		}
	}

	return lo, nil
}

// PlaceCloseOrder places a market close (reduceOnly) for the entire position.
func (m *Manager) PlaceCloseOrder(ctx context.Context, symbol, side, qty string) (*client.Order, error) {
	order, err := m.futures.PlaceOrder(ctx, client.PlaceOrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          "MARKET",
		Quantity:      qty,
		ReduceOnly:    true,
		ClientOrderID: "bot_close_" + uuid.New().String()[:8],
	})
	if err != nil {
		return nil, fmt.Errorf("ordermanager: close: %w", err)
	}
	m.log.Info("close order placed", zap.String("symbol", symbol), zap.String("side", side))
	return order, nil
}

// OnOrderUpdate processes a WS order update event, updating local state.
func (m *Manager) OnOrderUpdate(u stream.WsOrderUpdate) {
	o := u.Order
	m.mu.Lock()
	defer m.mu.Unlock()

	lo, ok := m.orders[o.OrderID]
	if !ok {
		lo = &LocalOrder{}
		m.orders[o.OrderID] = lo
	}
	lo.Status = o.OrderStatus
	lo.ExecutedQty = o.CumFilledQty
	lo.AvgPrice = o.AvgPrice
	lo.UpdateTime = u.EventTime

	// 1. Bracket Order Logic: If an ENTRY limit order is filled, place SL/TP
	if lo.Purpose == PurposeEntry && lo.Status == "FILLED" {
		// Place SL/TP if they were specified for this limit order
		if lo.SLPrice > 0 || lo.TPPrice > 0 {
			m.log.Info("Bracket trigger: Limit entry filled, placing SL/TP",
				zap.String("symbol", o.Symbol),
				zap.Float64("price", o.AvgPrice),
			)
			go m.PlaceBracketForFilledEntry(context.Background(), lo)
		}

		// OFAC: One-Fills-All-Cancels
		// If an ENTRY order is filled, cancel all other ENTRY orders for the same symbol/side
		m.log.Info("OFAC: Entry order filled, triggering mass cancellation",
			zap.String("symbol", o.Symbol),
			zap.Int64("orderId", o.OrderID),
		)
		// Run cancellation in background to avoid blocking the update handler
		go m.CancelAllEntriesForSymbol(context.Background(), o.Symbol, o.OrderID, string(o.Side))
	}
}

// GetAll returns a snapshot of all local orders.
func (m *Manager) GetAll() []LocalOrder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]LocalOrder, 0, len(m.orders))
	for _, o := range m.orders {
		out = append(out, *o)
	}
	return out
}

// SetOrderStatus forces a status update on a local order (useful for proactive cancellations).
func (m *Manager) SetOrderStatus(orderID int64, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lo, ok := m.orders[orderID]; ok {
		lo.Status = status
	}
}

// CancelSLTP cancels paired SL and TP orders for a given local order.
func (m *Manager) CancelSLTP(ctx context.Context, symbol string, lo *LocalOrder) {
	m.mu.RLock()
	slID, tpID := lo.SLID, lo.TPID
	m.mu.RUnlock()

	if slID != "" {
		if err := m.cancelByClientID(ctx, symbol, slID); err != nil {
			m.log.Error("cancel SL error", zap.Error(err))
		}
	}
	if tpID != "" {
		if err := m.cancelByClientID(ctx, symbol, tpID); err != nil {
			m.log.Error("cancel TP error", zap.Error(err))
		}
	}
}

func (m *Manager) placeStopOrder(ctx context.Context, symbol, side, qty string, stopPrice float64, orderType string) (string, error) {
	clientID := "bot_sl_" + uuid.New().String()[:8]
	_, err := m.futures.PlaceOrder(ctx, client.PlaceOrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          orderType,
		StopPrice:     m.prec.RoundPrice(symbol, stopPrice),
		ClosePosition: true,
		ClientOrderID: clientID,
		WorkingType:   "MARK_PRICE",
	})
	if err != nil {
		return "", err
	}
	m.log.Info("stop order placed",
		zap.String("type", orderType),
		zap.String("symbol", symbol),
		zap.Float64("price", stopPrice),
	)
	return clientID, nil
}

func (m *Manager) cancelByClientID(ctx context.Context, symbol, clientID string) error {
	_, err := m.futures.CancelOrder(ctx, client.CancelOrderRequest{
		Symbol:        symbol,
		ClientOrderID: clientID,
	})
	return err
}

// CancelAllEntriesForSymbol cancels all pending entry orders for a symbol.
func (m *Manager) CancelAllEntriesForSymbol(ctx context.Context, symbol string, filledOrderID int64, side string) {
	m.mu.RLock()
	var toCancel []int64
	for id, lo := range m.orders {
		// In one-way mode, we usually cancel the opposite side too if it was an entry trap
		if lo.Symbol == symbol && lo.Purpose == PurposeEntry && id != filledOrderID && (lo.Status == "NEW" || lo.Status == "PARTIALLY_FILLED") {
			toCancel = append(toCancel, id)
		}
	}
	m.mu.RUnlock()

	for _, id := range toCancel {
		m.log.Info("OFAC: Canceling redundant entry order", zap.String("symbol", symbol), zap.Int64("orderId", id))
		_, err := m.futures.CancelOrder(ctx, client.CancelOrderRequest{
			Symbol:  symbol,
			OrderID: id,
		})
		if err != nil {
			m.log.Warn("OFAC: Failed to cancel order", zap.Int64("orderId", id), zap.Error(err))
		}
	}
}

// PlaceLimitEntry places a LIMIT entry order (GTC).
func (m *Manager) PlaceLimitEntry(
	ctx context.Context,
	symbol, side, qty, price string,
	slPrice, tpPrice float64,
	strategyName string,
) (*LocalOrder, error) {
	m.log.Info("[DEBUG ENTRY] PlaceLimitEntry called", zap.String("symbol", symbol), zap.String("side", side), zap.String("qty", qty), zap.String("price", price))
	clientID := "bot_limit_" + uuid.New().String()[:8]

	order, err := m.futures.PlaceOrder(ctx, client.PlaceOrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          "LIMIT",
		Quantity:      qty,
		Price:         price,
		TimeInForce:   "GTC",
		ClientOrderID: clientID,
	})
	if err != nil {
		return nil, fmt.Errorf("ordermanager: limit entry: %w", err)
	}

	lo := &LocalOrder{
		Order:        *order,
		Purpose:      PurposeEntry,
		StrategyName: strategyName,
		SLPrice:      slPrice,
		TPPrice:      tpPrice,
	}
	m.mu.Lock()
	m.orders[order.OrderID] = lo
	m.mu.Unlock()

	m.log.Info("🎯 LIMIT ORDER POSTED",
		zap.String("strategy", strategyName),
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("price", price),
		zap.String("qty", qty),
	)

	return lo, nil
}

// PlaceBracketForFilledEntry places SL and TP orders for an already filled entry.
func (m *Manager) PlaceBracketForFilledEntry(ctx context.Context, lo *LocalOrder) {
	side := string(lo.Side)
	symbol := lo.Symbol
	qtyStr := strconv.FormatFloat(lo.ExecutedQty, 'f', -1, 64)

	oppositeSide := "SELL"
	if side == "SELL" {
		oppositeSide = "BUY"
	}

	if lo.SLPrice > 0 {
		slID, err := m.placeStopOrder(ctx, symbol, oppositeSide, qtyStr, lo.SLPrice, "STOP_MARKET")
		if err != nil {
			m.log.Error("bracket SL error", zap.Error(err))
		} else {
			m.mu.Lock()
			lo.SLID = slID
			m.mu.Unlock()
		}
	}

	if lo.TPPrice > 0 {
		tpID, err := m.placeStopOrder(ctx, symbol, oppositeSide, qtyStr, lo.TPPrice, "TAKE_PROFIT_MARKET")
		if err != nil {
			m.log.Error("bracket TP error", zap.Error(err))
		} else {
			m.mu.Lock()
			lo.TPID = tpID
			m.mu.Unlock()
		}
	}
}

// FindEntryByStrategy returns the current open entry order for a strategy/symbol if it exists.
func (m *Manager) FindEntryByStrategy(symbol, strategyName, side string) *LocalOrder {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, lo := range m.orders {
		if lo.Symbol == symbol && lo.StrategyName == strategyName && lo.Purpose == PurposeEntry &&
			string(lo.Side) == side && (lo.Status == "NEW" || lo.Status == "PARTIALLY_FILLED") {
			return lo
		}
	}
	return nil
}

// CountPendingBySide returns the number of NEW/PARTIALLY_FILLED entry orders for a symbol+side.
func (m *Manager) CountPendingBySide(symbol, side string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, lo := range m.orders {
		if lo.Symbol == symbol && lo.Purpose == PurposeEntry &&
			string(lo.Side) == side && (lo.Status == "NEW" || lo.Status == "PARTIALLY_FILLED") {
			count++
		}
	}
	return count
}

// CountAllPendingEntries returns the total number of pending entry orders across all symbols.
func (m *Manager) CountAllPendingEntries() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, lo := range m.orders {
		if lo.Purpose == PurposeEntry && (lo.Status == "NEW" || lo.Status == "PARTIALLY_FILLED") {
			count++
		}
	}
	return count
}
