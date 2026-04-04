package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MarketClient wraps public (unsigned) market data endpoints.
type MarketClient struct {
	http *HTTPClient
}

// NewMarketClient creates a new MarketClient.
func NewMarketClient(h *HTTPClient) *MarketClient {
	return &MarketClient{http: h}
}

// Ping checks server connectivity.
func (m *MarketClient) Ping(ctx context.Context) error {
	_, err := m.http.GetPublic(ctx, m.apiPath("/fapi/v1/ping"), nil)
	return err
}

// ServerTime returns the server time in milliseconds.
func (m *MarketClient) ServerTime(ctx context.Context) (int64, error) {
	data, err := m.http.GetPublic(ctx, m.apiPath("/fapi/v1/time"), nil)
	if err != nil {
		return 0, err
	}
	var resp struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, err
	}
	return resp.ServerTime, nil
}

// Klines fetches OHLCV candlestick data.
func (m *MarketClient) Klines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	params := map[string]string{
		"symbol":   symbol,
		"interval": interval,
		"limit":    strconv.Itoa(limit),
	}
	data, err := m.http.GetPublic(ctx, m.apiPath("/fapi/v1/klines"), params)
	if err != nil {
		return nil, err
	}
	// Klines come as array of arrays
	var raw [][]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	klines := make([]Kline, 0, len(raw))
	for _, r := range raw {
		if len(r) < 9 {
			continue
		}
		k := Kline{}
		json.Unmarshal(r[0], &k.OpenTime)
		parseFloatJSON(r[1], &k.Open)
		parseFloatJSON(r[2], &k.High)
		parseFloatJSON(r[3], &k.Low)
		parseFloatJSON(r[4], &k.Close)
		parseFloatJSON(r[5], &k.Volume)
		json.Unmarshal(r[6], &k.CloseTime)
		parseFloatJSON(r[7], &k.QuoteVolume)
		json.Unmarshal(r[8], &k.TradeCount)
		klines = append(klines, k)
	}
	return klines, nil
}

// MarkPrice fetches the current mark price and funding rate for a symbol.
func (m *MarketClient) MarkPrice(ctx context.Context, symbol string) (*MarkPrice, error) {
	params := map[string]string{"symbol": symbol}
	data, err := m.http.GetPublic(ctx, m.apiPath("/fapi/v1/premiumIndex"), params)
	if err != nil {
		return nil, err
	}
	var mp MarkPrice
	if err := json.Unmarshal(data, &mp); err != nil {
		return nil, err
	}
	return &mp, nil
}

// AllMarkPrices fetches mark prices for all symbols.
func (m *MarketClient) AllMarkPrices(ctx context.Context) ([]MarkPrice, error) {
	data, err := m.http.GetPublic(ctx, m.apiPath("/fapi/v1/premiumIndex"), nil)
	if err != nil {
		return nil, err
	}
	var mps []MarkPrice
	if err := json.Unmarshal(data, &mps); err != nil {
		return nil, err
	}
	return mps, nil
}

// BookTicker fetches best bid/ask for a symbol.
func (m *MarketClient) BookTicker(ctx context.Context, symbol string) (*BookTicker, error) {
	params := map[string]string{"symbol": symbol}
	data, err := m.http.GetPublic(ctx, m.apiPath("/fapi/v1/ticker/bookTicker"), params)
	if err != nil {
		return nil, err
	}
	var bt BookTicker
	if err := json.Unmarshal(data, &bt); err != nil {
		return nil, err
	}
	return &bt, nil
}

// FundingRates returns recent funding rates for a symbol.
func (m *MarketClient) FundingRates(ctx context.Context, symbol string, limit int) ([]FundingRate, error) {
	params := map[string]string{
		"symbol": symbol,
		"limit":  strconv.Itoa(limit),
	}
	data, err := m.http.GetPublic(ctx, m.apiPath("/fapi/v1/fundingRate"), params)
	if err != nil {
		return nil, err
	}
	var rates []FundingRate
	if err := json.Unmarshal(data, &rates); err != nil {
		return nil, err
	}
	return rates, nil
}

// helper: unmarshal float from JSON string or number
func parseFloatJSON(raw json.RawMessage, dst *float64) {
	// Try string first
	var s string
	if json.Unmarshal(raw, &s) == nil {
		f, _ := strconv.ParseFloat(s, 64)
		*dst = f
		return
	}
	json.Unmarshal(raw, dst)
}

// ExchangeInfo fetches exchange info (symbols, filters, rate limits).
func (m *MarketClient) ExchangeInfo(ctx context.Context) (json.RawMessage, error) {
	data, err := m.http.GetPublic(ctx, m.apiPath("/fapi/v1/exchangeInfo"), nil)
	if err != nil {
		return nil, fmt.Errorf("exchange info: %w", err)
	}
	return json.RawMessage(data), nil
}

func (m *MarketClient) apiPath(path string) string {
	if m.http != nil && m.http.v3Signer != nil {
		return strings.Replace(path, "/fapi/v1/", "/fapi/v3/", 1)
	}
	return path
}
