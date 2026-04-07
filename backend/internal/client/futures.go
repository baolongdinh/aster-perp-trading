package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// FuturesClient provides a typed client for the Aster Futures API.
type FuturesClient struct {
	http        *HTTPClient
	dryRun      bool
	log         *zap.Logger
	rateLimiter *rate.Limiter
}

// NewFuturesClient creates a new FuturesClient.
func NewFuturesClient(h *HTTPClient, dryRun bool, log *zap.Logger, requestsPerSecond int) *FuturesClient {
	// Create rate limiter - very conservative to avoid bans
	// Minimum 2 seconds between requests
	rateLimiter := rate.NewLimiter(rate.Limit(requestsPerSecond), 1)

	client := &FuturesClient{
		http:        h,
		dryRun:      dryRun,
		log:         log,
		rateLimiter: rateLimiter,
	}
	return client
}

// GetHTTPClient returns the underlying HTTP client for market data access.
func (f *FuturesClient) GetHTTPClient() *HTTPClient {
	return f.http
}

// PlaceOrder places a new futures order. Returns the placed Order or a dry-run stub.
// If leverage exceeds maximum (error -2027), it automatically adjusts to x99 and retries.
func (f *FuturesClient) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*Order, error) {
	// Wait for rate limiter
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

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
	data, err := f.http.PostSigned(ctx, f.apiPath("/fapi/v1/order"), params)
	if err != nil {
		// Check if this is a leverage error (-2027)
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.IsLeverageError() {
			f.log.Warn("Leverage exceeds maximum detected, adjusting to x99 and retrying",
				zap.String("symbol", req.Symbol),
				zap.Int("error_code", apiErr.Code),
				zap.String("error_msg", apiErr.Message))

			// Set leverage to maximum (x99)
			if leverageErr := f.SetLeverage(ctx, SetLeverageRequest{
				Symbol:   req.Symbol,
				Leverage: 99,
			}); leverageErr != nil {
				f.log.Error("Failed to set leverage to x99",
					zap.String("symbol", req.Symbol),
					zap.Error(leverageErr))
				return nil, fmt.Errorf("place order: %w (failed to adjust leverage: %v)", err, leverageErr)
			}

			f.log.Info("Leverage adjusted to x99, retrying order placement",
				zap.String("symbol", req.Symbol))

			// Wait for rate limiter again before retry
			if rateErr := f.rateLimiter.Wait(ctx); rateErr != nil {
				return nil, fmt.Errorf("rate limit wait on retry: %w", rateErr)
			}

			// Retry the order
			data, err = f.http.PostSigned(ctx, f.apiPath("/fapi/v1/order"), params)
			if err != nil {
				return nil, fmt.Errorf("place order (after leverage adjustment): %w", err)
			}
		} else {
			return nil, fmt.Errorf("place order: %w", err)
		}
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

func (f *FuturesClient) apiPath(path string) string {
	if f.http != nil && f.http.v3Signer != nil {
		if strings.HasPrefix(path, "/fapi/v1/") {
			return strings.Replace(path, "/fapi/v1/", "/fapi/v3/", 1)
		}
		if strings.HasPrefix(path, "/fapi/v2/") {
			return strings.Replace(path, "/fapi/v2/", "/fapi/v3/", 1)
		}
	}
	return path
}

// placeOrderParams converts PlaceOrderRequest to API parameters
func (f *FuturesClient) placeOrderParams(req PlaceOrderRequest) map[string]string {
	params := map[string]string{
		"symbol": req.Symbol,
		"side":   req.Side,
		"type":   req.Type,
	}
	if req.Quantity != "" {
		params["quantity"] = req.Quantity
	}
	if req.ClientOrderID != "" {
		params["newClientOrderId"] = req.ClientOrderID
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
	// Wait for rate limiter
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := f.http.GetSigned(ctx, f.apiPath("/fapi/v1/openOrders"), params)
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
func (f *FuturesClient) CancelOrder(ctx context.Context, req CancelOrderRequest) (*Order, error) {
	// Wait for rate limiter
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	params := map[string]string{
		"symbol": req.Symbol,
	}

	if req.OrderID > 0 {
		params["orderId"] = strconv.FormatInt(req.OrderID, 10)
	} else if req.ClientOrderID != "" {
		params["origClientOrderId"] = req.ClientOrderID
	} else {
		return nil, fmt.Errorf("cancel order: orderId or clientOrderId required")
	}

	data, err := f.http.DeleteSigned(ctx, f.apiPath("/fapi/v1/order"), params)
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
	// Wait for rate limiter
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	data, err := f.http.GetSigned(ctx, f.apiPath("/fapi/v2/account"), nil)
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
	// Wait for rate limiter
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	data, err := f.http.GetSigned(ctx, f.apiPath("/fapi/v2/positionRisk"), nil)
	if err != nil {
		return nil, fmt.Errorf("get positions: %w", err)
	}

	var positions []Position
	if err := json.Unmarshal(data, &positions); err != nil {
		return nil, fmt.Errorf("parse positions: %w", err)
	}
	return positions, nil
}

// SetLeverage sets the leverage for a symbol
func (f *FuturesClient) SetLeverage(ctx context.Context, req SetLeverageRequest) error {
	// Wait for rate limiter
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit wait: %w", err)
	}

	if f.dryRun {
		f.log.Info("[DRY-RUN] SetLeverage",
			zap.String("symbol", req.Symbol),
			zap.Int("leverage", req.Leverage),
		)
		return nil
	}

	params := map[string]string{
		"symbol":   req.Symbol,
		"leverage": strconv.Itoa(req.Leverage),
	}

	_, err := f.http.PostSigned(ctx, f.apiPath("/fapi/v1/leverage"), params)
	if err != nil {
		return fmt.Errorf("set leverage: %w", err)
	}

	return nil
}
