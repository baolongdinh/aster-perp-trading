package farming

import (
	"testing"

	"aster-bot/internal/client"
	"aster-bot/internal/config"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// FuturesClientInterface for testing
type FuturesClientInterface interface {
	Get24hrTicker() ([]client.Ticker, error)
	GetExchangeInfo() (interface{}, error)
}

// Mock FuturesClient for testing
type MockFuturesClient struct {
	tickers []client.Ticker
}

func (m *MockFuturesClient) Get24hrTicker() ([]client.Ticker, error) {
	return m.tickers, nil
}

func (m *MockFuturesClient) GetExchangeInfo() (interface{}, error) {
	return struct{}{}, nil
}

func TestSymbolSelector_MeetsBasicCriteria(t *testing.T) {
	// Create test config
	cfg := &config.VolumeFarmConfig{
		Symbols: config.SymbolsConfig{
			MinVolume24h:    1000000,
			QuoteCurrencies: []string{"USDT", "USD1"},
			Whitelist:       []string{},
		},
	}

	// Create logger
	logger := logrus.NewEntry(logrus.New())

	// Create symbol selector
	selector := &SymbolSelector{
		config: cfg,
		logger: logger,
	}

	// Test cases
	testCases := []struct {
		name        string
		symbol      string
		volume24h   int
		count       int
		expected    bool
		description string
	}{
		{
			name:        "BTCUSDT with high volume",
			symbol:      "BTCUSDT",
			volume24h:   10000000,
			count:       100000,
			expected:    true,
			description: "Should pass with high volume",
		},
		{
			name:        "LOWVOL with low volume",
			symbol:      "LOWVOLUSDT",
			volume24h:   100000,
			count:       1000,
			expected:    false,
			description: "Should fail due to low volume",
		},
		{
			name:        "Zero volume",
			symbol:      "ZEROVOLUSDT",
			volume24h:   0,
			count:       0,
			expected:    false,
			description: "Should fail with zero volume",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := selector.meetsBasicCriteria(tc.symbol, tc.volume24h, tc.count)
			assert.Equal(t, tc.expected, result, tc.description)
		})
	}
}

func TestSymbolSelector_ExtractQuoteCurrency(t *testing.T) {
	logger := logrus.NewEntry(logrus.New())
	selector := &SymbolSelector{logger: logger}

	testCases := []struct {
		symbol   string
		expected string
	}{
		{"BTCUSDT", "USDT"},
		{"ETHUSD1", "USD1"},
		{"BNBBUSD", "BUSD"},
		{"ADAUSDC", "USDC"},
		{"BTCPERP", "PERP"},
		{"BTC", ""}, // No quote currency
	}

	for _, tc := range testCases {
		t.Run(tc.symbol, func(t *testing.T) {
			result := selector.extractQuoteCurrency(tc.symbol)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSymbolSelector_IsQuoteCurrencyAllowed(t *testing.T) {
	cfg := &config.VolumeFarmConfig{
		Symbols: config.SymbolsConfig{
			QuoteCurrencies: []string{"USDT", "USD1"},
		},
	}
	logger := logrus.NewEntry(logrus.New())
	selector := &SymbolSelector{
		config: cfg,
		logger: logger,
	}

	testCases := []struct {
		quoteCurrency string
		expected      bool
	}{
		{"USDT", true},
		{"USD1", true},
		{"BUSD", false},
		{"USDC", false},
		{"BTC", false},
	}

	for _, tc := range testCases {
		t.Run(tc.quoteCurrency, func(t *testing.T) {
			result := selector.isQuoteCurrencySupported(tc.quoteCurrency)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSymbolSelector_CalculateSpread(t *testing.T) {
	logger := logrus.NewEntry(logrus.New())
	selector := &SymbolSelector{logger: logger}

	testCases := []struct {
		name     string
		ticker   client.Ticker
		expected float64
	}{
		{
			name: "Normal spread",
			ticker: client.Ticker{
				HighPrice:        110.0,
				LowPrice:         90.0,
				WeightedAvgPrice: 100.0,
			},
			expected: 20.0, // (110-90)/100 * 100
		},
		{
			name: "Zero weighted avg price",
			ticker: client.Ticker{
				HighPrice:        110.0,
				LowPrice:         90.0,
				WeightedAvgPrice: 0.0,
			},
			expected: 0.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := selector.calculateSpread(tc.ticker)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSymbolSelector_CalculateLiquidityScore(t *testing.T) {
	cfg := &config.VolumeFarmConfig{
		Symbols: config.SymbolsConfig{
			VolumeWeighting: 0.7,
		},
	}
	logger := logrus.NewEntry(logrus.New())
	selector := &SymbolSelector{
		config: cfg,
		logger: logger,
	}

	testCases := []struct {
		name     string
		metrics  *SymbolMetrics
		expected float64
	}{
		{
			name: "High volume, low spread",
			metrics: &SymbolMetrics{
				Volume24h:     10000000,
				CurrentSpread: 0.1,
			},
			expected: 0.7, // High volume weight
		},
		{
			name: "Low volume, high spread",
			metrics: &SymbolMetrics{
				Volume24h:     100000,
				CurrentSpread: 5.0,
			},
			expected: 0.0, // Low score
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := selector.calculateLiquidityScore(tc.metrics)
			assert.InDelta(t, tc.expected, result, 0.1)
		})
	}
}

// TODO: Fix integration test after interface issues resolved
/*
func TestSymbolSelector_DiscoverSymbols_Integration(t *testing.T) {
	// Create mock data
	mockTickers := []client.Ticker{
		{
			Symbol:           "BTCUSDT",
			Volume:           10000000,
			Count:            100000,
			LastPrice:        50000.0,
			HighPrice:        51000.0,
			LowPrice:         49000.0,
			WeightedAvgPrice: 50000.0,
		},
		{
			Symbol:           "ETHUSDT",
			Volume:           5000000,
			Count:            50000,
			LastPrice:        3000.0,
			HighPrice:        3100.0,
			LowPrice:         2900.0,
			WeightedAvgPrice: 3000.0,
		},
		{
			Symbol:           "LOWVOLUSDT",
			Volume:           100000,
			Count:            1000,
			LastPrice:        1.0,
			HighPrice:        1.1,
			LowPrice:         0.9,
			WeightedAvgPrice: 1.0,
		},
	}

	// Create config with permissive settings
	cfg := &config.VolumeFarmConfig{
		Symbols: config.SymbolsConfig{
			MinVolume24h:        1000000,
			MaxSpreadPct:        1.0,
			MinLiquidityScore:   0.0,
			QuoteCurrencies:     []string{"USDT"},
			MaxSymbolsPerQuote:  10,
			VolumeWeighting:     0.7,
		},
	}

	// Create symbol selector
	logger := logrus.NewEntry(logrus.New())
	mockClient := &MockFuturesClient{tickers: mockTickers}

	selector := &SymbolSelector{
		config:        cfg,
		futuresClient: mockClient,
		logger:        logger,
		activeSymbols: make(map[string]*SelectedSymbol),
	}

	// Test discoverSymbols
	err := selector.discoverSymbols(nil)
	assert.NoError(t, err)

	// Check results
	assert.Greater(t, len(selector.activeSymbols), 0, "Should have selected at least one symbol")

	// Log selected symbols for debugging
	t.Logf("Selected %d symbols:", len(selector.activeSymbols))
	for symbol, selected := range selector.activeSymbols {
		t.Logf("  - %s: Volume=%.0f, Spread=%.2f%%",
			symbol, selected.Volume24h, selected.CurrentSpread)
	}
}
*/
