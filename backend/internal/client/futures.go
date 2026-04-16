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

// getMaxLeverageForSymbol returns the maximum leverage allowed for a symbol
// BTC: 149x, Other symbols: 99x
func getMaxLeverageForSymbol(symbol string) int {
	// Check if symbol is BTC (case-insensitive)
	symbolUpper := strings.ToUpper(symbol)
	if strings.HasPrefix(symbolUpper, "BTC") {
		return 149
	}
	return 99
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
// If leverage exceeds maximum (error -2027), it automatically adjusts with per-symbol max leverage:
// - BTC: max 149x
// - Other symbols: max 99x
// Fallback tries safe leverage first (20, 50) then max leverage for the symbol.
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
			f.log.Warn("Leverage exceeds maximum detected, trying fallback leverages",
				zap.String("symbol", req.Symbol),
				zap.Int("error_code", apiErr.Code),
				zap.String("error_msg", apiErr.Message))

			// Get per-symbol max leverage
			maxLeverage := getMaxLeverageForSymbol(req.Symbol)
			f.log.Info("Using per-symbol max leverage",
				zap.String("symbol", req.Symbol),
				zap.Int("max_leverage", maxLeverage))

			// Try multiple fallback leverage levels
			// Start with safe leverage (20), then 50, then max leverage
			// Filter to only include values <= max leverage for the symbol
			allFallbacks := []int{20, 50, 99, 149}
			var fallbacks []int
			for _, lev := range allFallbacks {
				if lev <= maxLeverage {
					fallbacks = append(fallbacks, lev)
				}
			}

			for _, leverage := range fallbacks {
				f.log.Info("Attempting to set leverage",
					zap.String("symbol", req.Symbol),
					zap.Int("leverage", leverage),
					zap.Int("max_allowed", maxLeverage))

				if leverageErr := f.SetLeverage(ctx, SetLeverageRequest{
					Symbol:   req.Symbol,
					Leverage: leverage,
				}); leverageErr != nil {
					f.log.Warn("Failed to set leverage, trying next fallback",
						zap.String("symbol", req.Symbol),
						zap.Int("leverage", leverage),
						zap.Error(leverageErr))
					continue
				}

				f.log.Info("Leverage adjusted, retrying order placement",
					zap.String("symbol", req.Symbol),
					zap.Int("leverage", leverage))

				// Wait for rate limiter again before retry
				if rateErr := f.rateLimiter.Wait(ctx); rateErr != nil {
					return nil, fmt.Errorf("rate limit wait on retry: %w", rateErr)
				}

				// Retry the order
				data, err = f.http.PostSigned(ctx, f.apiPath("/fapi/v1/order"), params)
				if err == nil {
					f.log.Info("Order placed successfully after leverage adjustment",
						zap.String("symbol", req.Symbol),
						zap.Int("leverage", leverage))
					break
				}

				// If still leverage error, continue to next fallback
				var retryErr *APIError
				if errors.As(err, &retryErr) && retryErr.IsLeverageError() {
					f.log.Warn("Still leverage error, trying next fallback",
						zap.String("symbol", req.Symbol),
						zap.Int("tried_leverage", leverage))
					continue
				}

				// Other error, return it
				return nil, fmt.Errorf("place order (after leverage adjustment to x%d): %w", leverage, err)
			}

			if err != nil {
				return nil, fmt.Errorf("place order: failed after trying all leverage fallbacks: %w", err)
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
func (f *FuturesClient) Get24hrTicker(ctx context.Context) ([]Ticker, error) {
	// Wait for rate limiter
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	data, err := f.http.GetPublic(ctx, "/fapi/v1/ticker/24hr", nil)
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
	if req.ReduceOnly {
		params["reduceOnly"] = "true"
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

// CancelAllOpenOrders cancels all open orders for a specific symbol
// Uses DELETE /fapi/v1/allOpenOrders endpoint
func (f *FuturesClient) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	// Wait for rate limiter
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit wait: %w", err)
	}

	if f.dryRun {
		f.log.Info("[DRY-RUN] CancelAllOpenOrders",
			zap.String("symbol", symbol))
		return nil
	}

	params := map[string]string{
		"symbol": symbol,
	}

	_, err := f.http.DeleteSigned(ctx, f.apiPath("/fapi/v1/allOpenOrders"), params)
	if err != nil {
		return fmt.Errorf("cancel all open orders: %w", err)
	}

	f.log.Info("All open orders cancelled",
		zap.String("symbol", symbol))
	return nil
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

// StartListenKey creates a new listen key for user data stream
// POST /fapi/v3/listenKey (signed) → { "listenKey": "..." }
func (f *FuturesClient) StartListenKey(ctx context.Context) (string, error) {
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limit wait: %w", err)
	}

	data, err := f.http.PostSigned(ctx, f.apiPath("/fapi/v3/listenKey"), nil)
	if err != nil {
		return "", fmt.Errorf("start listen key: %w", err)
	}

	var result struct {
		ListenKey string `json:"listenKey"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse listen key: %w", err)
	}

	f.log.Info("User data stream listen key created", zap.String("listenKey", result.ListenKey[:10]+"..."))
	return result.ListenKey, nil
}

// KeepaliveListenKey extends the validity of a listen key
// PUT /fapi/v3/listenKey (signed) - call every 30 min
func (f *FuturesClient) KeepaliveListenKey(ctx context.Context) error {
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit wait: %w", err)
	}

	_, err := f.http.PutSigned(ctx, f.apiPath("/fapi/v3/listenKey"), nil)
	if err != nil {
		return fmt.Errorf("keepalive listen key: %w", err)
	}

	f.log.Debug("User data stream listen key keepalive success")
	return nil
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
