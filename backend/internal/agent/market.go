package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	BaseURL = "https://fapi.asterdex.com"
)

// Client provides market data fetching capabilities
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new market data client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    BaseURL,
	}
}

// KlineResponse represents a single kline from the API
type KlineResponse struct {
	OpenTime            int64  `json:"openTime"`
	Open                string `json:"open"`
	High                string `json:"high"`
	Low                 string `json:"low"`
	Close               string `json:"close"`
	Volume              string `json:"volume"`
	CloseTime           int64  `json:"closeTime"`
	QuoteVolume         string `json:"quoteVolume"`
	Trades              int    `json:"trades"`
	TakerBuyBaseVolume  string `json:"takerBuyBaseVolume"`
	TakerBuyQuoteVolume string `json:"takerBuyQuoteVolume"`
}

// FetchKlines fetches kline data from the REST API
func (c *Client) FetchKlines(symbol, interval string, limit int) ([]Candle, error) {
	url := fmt.Sprintf("%s/fapi/v3/klines?symbol=%s&interval=%s&limit=%d",
		c.baseURL, symbol, interval, limit)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch klines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Binance/Aster API returns klines as array of arrays
	var rawKlines [][]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawKlines); err != nil {
		return nil, fmt.Errorf("failed to decode klines: %w", err)
	}

	candles := make([]Candle, len(rawKlines))
	for i, k := range rawKlines {
		candle, err := parseKline(k)
		if err != nil {
			return nil, fmt.Errorf("failed to parse kline %d: %w", i, err)
		}
		candles[i] = candle
	}

	return candles, nil
}

// parseKline converts raw kline array to Candle struct
func parseKline(k []interface{}) (Candle, error) {
	if len(k) < 6 {
		return Candle{}, fmt.Errorf("invalid kline data length")
	}

	open, err := parseFloat(k[1])
	if err != nil {
		return Candle{}, fmt.Errorf("invalid open: %w", err)
	}

	high, err := parseFloat(k[2])
	if err != nil {
		return Candle{}, fmt.Errorf("invalid high: %w", err)
	}

	low, err := parseFloat(k[3])
	if err != nil {
		return Candle{}, fmt.Errorf("invalid low: %w", err)
	}

	close, err := parseFloat(k[4])
	if err != nil {
		return Candle{}, fmt.Errorf("invalid close: %w", err)
	}

	volume, err := parseFloat(k[5])
	if err != nil {
		return Candle{}, fmt.Errorf("invalid volume: %w", err)
	}

	return Candle{
		Open:   open,
		High:   high,
		Low:    low,
		Close:  close,
		Volume: volume,
	}, nil
}

// parseFloat converts interface{} to float64
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// FetchInitialData fetches the initial 200 candles needed for indicators
func (c *Client) FetchInitialData(symbol string) ([]Candle, error) {
	// Fetch 200 candles at 1m interval
	return c.FetchKlines(symbol, "1m", 200)
}

// DataProvider provides a higher-level interface for feeding data to the detector
type DataProvider struct {
	client   *Client
	symbol   string
	interval string
}

// NewDataProvider creates a new data provider for a specific symbol
func NewDataProvider(symbol, interval string) *DataProvider {
	return &DataProvider{
		client:   NewClient(),
		symbol:   symbol,
		interval: interval,
	}
}

// GetInitialCandles fetches the initial candle data
func (dp *DataProvider) GetInitialCandles() ([]Candle, error) {
	return dp.client.FetchInitialData(dp.symbol)
}
