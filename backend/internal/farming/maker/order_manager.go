package maker

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"aster-bot/internal/client"

	"go.uber.org/zap"
)

type OrderManagerImpl struct {
	futuresClient FuturesClientInterface
	logger        *zap.Logger
	openOrders    map[string]map[int64]*Order
	mu            sync.RWMutex
}

func NewOrderManager(futuresClient FuturesClientInterface, logger *zap.Logger) *OrderManagerImpl {
	return &OrderManagerImpl{
		futuresClient: futuresClient,
		logger:        logger,
		openOrders:    make(map[string]map[int64]*Order),
	}
}

func (m *OrderManagerImpl) PlaceLimitOrder(ctx context.Context, req LimitOrderRequest) (*Order, error) {
	// Round to correct precision for the symbol
	priceStr := formatPrice(req.Symbol, req.Price)
	qtyStr := formatQuantity(req.Symbol, req.Quantity)

	orderReq := client.PlaceOrderRequest{
		Symbol:      req.Symbol,
		Side:        string(req.Side),
		Type:        "LIMIT",
		Quantity:    qtyStr,
		Price:       priceStr,
		TimeInForce: req.TimeInForce,
	}

	if orderReq.TimeInForce == "" {
		orderReq.TimeInForce = "GTX"
	}

	resp, err := m.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		m.logger.Error("Failed to place limit order",
			zap.String("symbol", req.Symbol),
			zap.String("side", string(req.Side)),
			zap.Float64("price", req.Price),
			zap.Float64("quantity", req.Quantity),
			zap.Error(err))
		return nil, fmt.Errorf("place order failed: %w", err)
	}

	order := &Order{
		OrderID:       resp.OrderID,
		Symbol:        resp.Symbol,
		Side:          OrderSide(resp.Side),
		Type:          OrderType(resp.Type),
		Price:         resp.Price,
		OrigQty:       resp.OrigQty,
		ExecutedQty:   resp.ExecutedQty,
		Status:        OrderStatus(resp.Status),
		TimeInForce:   resp.TimeInForce,
		UpdateTime:    resp.UpdateTime,
		ClientOrderID: resp.ClientOrderID,
		PlacedTime:    time.Now(),
		AgeSeconds:    0,
	}

	m.mu.Lock()
	if m.openOrders[req.Symbol] == nil {
		m.openOrders[req.Symbol] = make(map[int64]*Order)
	}
	m.openOrders[req.Symbol][order.OrderID] = order
	m.mu.Unlock()

	m.logger.Info("Limit order placed",
		zap.Int64("order_id", order.OrderID),
		zap.String("symbol", order.Symbol),
		zap.String("side", string(order.Side)),
		zap.Float64("price", order.Price),
		zap.Float64("quantity", order.OrigQty))

	return order, nil
}

func (m *OrderManagerImpl) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	_, err := m.futuresClient.CancelOrder(ctx, client.CancelOrderRequest{
		Symbol:  symbol,
		OrderID: orderID,
	})
	if err != nil {
		m.logger.Error("Failed to cancel order",
			zap.String("symbol", symbol),
			zap.Int64("order_id", orderID),
			zap.Error(err))
		return fmt.Errorf("cancel order failed: %w", err)
	}

	m.mu.Lock()
	if orders, ok := m.openOrders[symbol]; ok {
		delete(orders, orderID)
	}
	m.mu.Unlock()

	m.logger.Info("Order cancelled",
		zap.String("symbol", symbol),
		zap.Int64("order_id", orderID))

	return nil
}

func (m *OrderManagerImpl) GetOpenOrders(symbol string) []Order {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []Order
	if orders, ok := m.openOrders[symbol]; ok {
		for _, order := range orders {
			result = append(result, *order)
		}
	}
	return result
}

func (m *OrderManagerImpl) GetAllOpenOrders() map[string][]Order {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]Order)
	for symbol, orders := range m.openOrders {
		for _, order := range orders {
			result[symbol] = append(result[symbol], *order)
		}
	}
	return result
}

func (m *OrderManagerImpl) BatchCancel(symbol string, orderIDs []int64) error {
	for _, orderID := range orderIDs {
		ctx, cancel := context.WithTimeout(context.Background(), 5)
		err := m.CancelOrder(ctx, symbol, orderID)
		cancel()
		if err != nil {
			m.logger.Warn("Failed to cancel order in batch",
				zap.Int64("order_id", orderID),
				zap.Error(err))
		}
	}
	return nil
}

func (m *OrderManagerImpl) UpdateOrderFromFill(symbol string, orderID int64, filledQty float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if orders, ok := m.openOrders[symbol]; ok {
		if order, ok := orders[orderID]; ok {
			order.ExecutedQty += filledQty
			if order.ExecutedQty >= order.OrigQty {
				order.Status = OrderStatusFilled
				delete(orders, orderID)
				m.logger.Info("Order fully filled and removed from tracking",
					zap.Int64("order_id", orderID),
					zap.Float64("executed_qty", order.ExecutedQty))
			} else {
				order.Status = OrderStatusPartially
			}
		}
	}
}

func (m *OrderManagerImpl) RemoveOrder(symbol string, orderID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if orders, ok := m.openOrders[symbol]; ok {
		delete(orders, orderID)
	}
}

func (m *OrderManagerImpl) ClearOrders(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.openOrders, symbol)
}

func (m *OrderManagerImpl) GetOrderCount(symbol string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if orders, ok := m.openOrders[symbol]; ok {
		return len(orders)
	}
	return 0
}

// formatPrice rounds price to correct precision for symbol
func formatPrice(symbol string, price float64) string {
	// ETHUSD1: tick size 0.1 (price precision 1)
	if strings.EqualFold(symbol, "ETHUSD1") {
		rounded := math.Round(price*10) / 10
		return strconv.FormatFloat(rounded, 'f', 1, 64)
	}
	// BTCUSD1: tick size 0.1 (price precision 1)
	if strings.EqualFold(symbol, "BTCUSD1") {
		rounded := math.Round(price*10) / 10
		return strconv.FormatFloat(rounded, 'f', 1, 64)
	}
	// Default: 2 decimals
	rounded := math.Round(price*100) / 100
	return strconv.FormatFloat(rounded, 'f', 2, 64)
}

// formatQuantity rounds quantity to correct precision for symbol
func formatQuantity(symbol string, qty float64) string {
	// ETHUSD1: quantity precision 3 (step size 0.001)
	if strings.EqualFold(symbol, "ETHUSD1") || strings.EqualFold(symbol, "BTCUSD1") {
		rounded := math.Round(qty*1000) / 1000
		if rounded < 0.001 {
			rounded = 0.001 // Minimum step
		}
		return strconv.FormatFloat(rounded, 'f', 3, 64)
	}
	// Default: 3 decimals
	rounded := math.Round(qty*1000) / 1000
	return strconv.FormatFloat(rounded, 'f', 3, 64)
}
