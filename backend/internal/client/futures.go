package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"go.uber.org/zap"
)

// FuturesClient provides a typed client for the Aster Futures API.
type FuturesClient struct {
	http   *HTTPClient
	dryRun bool
	log    *zap.Logger
}

// NewFuturesClient creates a new FuturesClient.
func NewFuturesClient(h *HTTPClient, dryRun bool, log *zap.Logger) *FuturesClient {
	return &FuturesClient{
		http:   h,
		dryRun: dryRun,
		log:    log,
	}
}

// PlaceOrder places a new futures order. Returns the placed Order or a dry-run stub.
func (f *FuturesClient) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*Order, error) {
	// Rate limiting removed to avoid artificial delays

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

// Get24hrTicker returns 24hr ticker price change statistics for all symbols
func (f *FuturesClient) Get24hrTicker() ([]Ticker, error) {
	data, err := f.http.GetPublic(context.Background(), "/fapi/v1/ticker/24hr", nil)
	if err != nil {
		return nil, err
	}

	var tickers []Ticker
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, err
	}
	return tickers, nil
}

// placeOrderParams converts PlaceOrderRequest to API parameters
func (f *FuturesClient) placeOrderParams(req PlaceOrderRequest) map[string]string {
	params := map[string]string{
		"symbol":    req.Symbol,
		"side":      req.Side,
		"type":      req.Type,
		"quantity":  req.Quantity,
		"newClient": req.ClientOrderID,
	}
	if req.Type == "LIMIT" {
		params["price"] = req.Price
	}
	if req.TimeInForce != "" {
		params["timeInForce"] = req.TimeInForce
	}
	return params
}

// GetOpenOrders gets all open orders
func (f *FuturesClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := f.http.GetSigned(ctx, "/fapi/v1/openOrders", params)
	if err != nil {
		return nil, fmt.Errorf("get open orders: %w", err)
	}

	var orders []Order
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("parse open orders: %w", err)
	}
	return orders, nil
}

// CancelOrder cancels an existing order
func (f *FuturesClient) CancelOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	params := map[string]string{
		"symbol":  symbol,
		"orderId": strconv.FormatInt(orderID, 10),
	}

	data, err := f.http.DeleteSigned(ctx, "/fapi/v1/order", params)
	if err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}

	var order Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("parse cancel order: %w", err)
	}
	return &order, nil
}

// GetAccountInfo gets account information
func (f *FuturesClient) GetAccountInfo(ctx context.Context) (*AccountInfo, error) {
	data, err := f.http.GetSigned(ctx, "/fapi/v2/account", nil)
	if err != nil {
		return nil, fmt.Errorf("get account info: %w", err)
	}

	var account AccountInfo
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("parse account info: %w", err)
	}
	return &account, nil
}

// GetPositions gets all positions
func (f *FuturesClient) GetPositions(ctx context.Context) ([]Position, error) {
	data, err := f.http.GetSigned(ctx, "/fapi/v2/positionRisk", nil)
	if err != nil {
		return nil, fmt.Errorf("get positions: %w", err)
	}

	var positions []Position
	if err := json.Unmarshal(data, &positions); err != nil {
		return nil, fmt.Errorf("parse positions: %w", err)
	}
	return positions, nil
}
