package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"go.uber.org/zap"
)

// FuturesClient handles signed futures trading endpoints.
type FuturesClient struct {
	http   *HTTPClient
	dryRun bool
	log    *zap.Logger
}

// NewFuturesClient creates a new FuturesClient.
func NewFuturesClient(h *HTTPClient, dryRun bool, log *zap.Logger) *FuturesClient {
	return &FuturesClient{http: h, dryRun: dryRun, log: log}
}

// PlaceOrder places a new futures order. Returns the placed Order or a dry-run stub.
func (f *FuturesClient) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*Order, error) {
	if f.dryRun {
		f.log.Info("[DRY-RUN] PlaceOrder",
			zap.String("symbol", req.Symbol),
			zap.String("side", req.Side),
			zap.String("type", req.Type),
			zap.String("qty", req.Quantity),
			zap.String("price", req.Price),
		)
		return &Order{
			OrderID:       -1,
			ClientOrderID: req.ClientOrderID,
			Symbol:        req.Symbol,
			Side:          req.Side,
			Type:          req.Type,
			Status:        "DRY_RUN",
		}, nil
	}

	params := f.placeOrderParams(req)
	data, err := f.http.PostSigned(ctx, "/fapi/v1/order", params)
	if err != nil {
		return nil, fmt.Errorf("place order: %w", err)
	}
	var order Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("place order parse: %w", err)
	}
	return &order, nil
}

// CancelOrder cancels an existing order.
func (f *FuturesClient) CancelOrder(ctx context.Context, req CancelOrderRequest) (*Order, error) {
	if f.dryRun {
		f.log.Info("[DRY-RUN] CancelOrder", zap.String("symbol", req.Symbol), zap.Int64("orderId", req.OrderID))
		return &Order{Symbol: req.Symbol, OrderID: req.OrderID, Status: "CANCELED_DRY_RUN"}, nil
	}
	params := map[string]string{"symbol": req.Symbol}
	if req.OrderID > 0 {
		params["orderId"] = strconv.FormatInt(req.OrderID, 10)
	} else if req.ClientOrderID != "" {
		params["origClientOrderId"] = req.ClientOrderID
	}
	data, err := f.http.DeleteSigned(ctx, "/fapi/v1/order", params)
	if err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}
	var order Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// CancelAllOpenOrders cancels all open orders for a symbol.
func (f *FuturesClient) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	if f.dryRun {
		f.log.Info("[DRY-RUN] CancelAllOpenOrders", zap.String("symbol", symbol))
		return nil
	}
	_, err := f.http.DeleteSigned(ctx, "/fapi/v1/allOpenOrders", map[string]string{"symbol": symbol})
	return err
}

// GetOpenOrders returns all open orders (optionally filtered by symbol).
func (f *FuturesClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}
	data, err := f.http.GetSigned(ctx, "/fapi/v1/openOrders", params)
	if err != nil {
		return nil, err
	}
	var orders []Order
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

// GetAccount returns account info including balances and positions.
func (f *FuturesClient) GetAccount(ctx context.Context) (*AccountInfo, error) {
	data, err := f.http.GetSigned(ctx, "/fapi/v1/account", nil)
	if err != nil {
		return nil, err
	}
	var account AccountInfo
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

// GetBalances returns per-asset balances.
func (f *FuturesClient) GetBalances(ctx context.Context) ([]Balance, error) {
	data, err := f.http.GetSigned(ctx, "/fapi/v1/balance", nil)
	if err != nil {
		return nil, err
	}
	var balances []Balance
	if err := json.Unmarshal(data, &balances); err != nil {
		return nil, err
	}
	return balances, nil
}

// GetPositions returns open positions (optionally filtered by symbol).
func (f *FuturesClient) GetPositions(ctx context.Context, symbol string) ([]Position, error) {
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}
	data, err := f.http.GetSigned(ctx, "/fapi/v1/positionRisk", params)
	if err != nil {
		return nil, err
	}
	var positions []Position
	if err := json.Unmarshal(data, &positions); err != nil {
		return nil, err
	}
	return positions, nil
}

// SetLeverage sets the leverage for a symbol.
func (f *FuturesClient) SetLeverage(ctx context.Context, req SetLeverageRequest) error {
	if f.dryRun {
		f.log.Info("[DRY-RUN] SetLeverage", zap.String("symbol", req.Symbol), zap.Int("leverage", req.Leverage))
		return nil
	}
	params := map[string]string{
		"symbol":   req.Symbol,
		"leverage": strconv.Itoa(req.Leverage),
	}
	_, err := f.http.PostSigned(ctx, "/fapi/v1/leverage", params)
	return err
}

// StartListenKey creates or refreshes a user data stream listenKey.
func (f *FuturesClient) StartListenKey(ctx context.Context) (string, error) {
	data, err := f.http.PostSigned(ctx, "/fapi/v1/listenKey", nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		ListenKey string `json:"listenKey"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	return resp.ListenKey, nil
}

// KeepaliveListenKey extends the listenKey validity.
func (f *FuturesClient) KeepaliveListenKey(ctx context.Context) error {
	_, err := f.http.PutSigned(ctx, "/fapi/v1/listenKey", nil)
	return err
}

// CloseListenKey closes the user data stream.
func (f *FuturesClient) CloseListenKey(ctx context.Context) error {
	_, err := f.http.DeleteSigned(ctx, "/fapi/v1/listenKey", nil)
	return err
}

// placeOrderParams converts a PlaceOrderRequest to the string map expected by the signed client.
func (f *FuturesClient) placeOrderParams(req PlaceOrderRequest) map[string]string {
	p := map[string]string{
		"symbol": req.Symbol,
		"side":   req.Side,
		"type":   req.Type,
	}
	if req.PositionSide != "" {
		p["positionSide"] = req.PositionSide
	}
	if req.TimeInForce != "" {
		p["timeInForce"] = req.TimeInForce
	}
	if req.Quantity != "" {
		p["quantity"] = req.Quantity
	}
	if req.Price != "" {
		p["price"] = req.Price
	}
	if req.StopPrice != "" {
		p["stopPrice"] = req.StopPrice
	}
	if req.ReduceOnly {
		p["reduceOnly"] = "true"
	}
	if req.ClosePosition {
		p["closePosition"] = "true"
	}
	if req.CallbackRate != "" {
		p["callbackRate"] = req.CallbackRate
	}
	if req.ActivationPrice != "" {
		p["activationPrice"] = req.ActivationPrice
	}
	if req.WorkingType != "" {
		p["workingType"] = req.WorkingType
	}
	if req.ClientOrderID != "" {
		p["newClientOrderId"] = req.ClientOrderID
	}
	return p
}
