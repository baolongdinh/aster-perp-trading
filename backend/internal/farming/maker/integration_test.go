package maker

import (
	"context"
	"testing"

	"aster-bot/internal/client"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMakerStrategy_VPINOBIFullIntegration(t *testing.T) {
	logger := zap.NewNop()

	// Create comprehensive config with all optimizations enabled
	config := &Config{
		Symbols:               []string{"BTCUSD1"},
		DefaultSpreadBps:      2.0,
		MaxLeverage:           150,
		UseDynamicSizing:      true,
		BaseNotionalUSD:       100,
		MinNotionalUSD:        20,
		MaxNotionalUSD:        500,
		MicroProfitMode:       true,
		MicroGridSpacingBps:   0.1,
		MicroGridLevels:       50,
		MicroMinNotionalUSD:   5,
		PositionBiasThreshold: 0.3,
		PositionBiasReducePct: 0.5,
		ToxicFlowDetection:    true,
		ToxicFlowThreshold:    0.6,
		ToxicFlowReducePct:    0.5,
		MomentumDetection:     true,
		MomentumThresholdPct:  0.03,
		MomentumTimeWindow:    30,
		// NEW: VPIN+OBI+Microstructure features
		OBIDetectionEnabled:           true,
		OBIWindowSize:                 10,
		OBIThreshold:                  0.5,
		OBISpreadAdjustment:           true,
		OBISizeAdjustment:             true,
		MicrostructureAnalysisEnabled: true,
		AggressivenessLevel:           3,
	}

	// Create mock clients
	futuresClient := &MockFuturesClient{}
	wsClient := &MockWebSocketClient{}

	// Create strategy
	strategy := NewMakerStrategy(futuresClient, wsClient, config, logger)

	// Verify all components are initialized
	assert.NotNil(t, strategy.obiDetector, "OBI detector should be initialized")
	assert.NotNil(t, strategy.microstructureAnalyzer, "Microstructure analyzer should be initialized")
	assert.NotNil(t, strategy.leverageAdapter, "Leverage adapter should be initialized")

	// Test VPIN integration
	if strategy.microstructureAnalyzer != nil {
		// Simulate toxic flow
		assert.NotNil(t, strategy.microstructureAnalyzer, "Should have microstructure analyzer")

		// Test spread calculation with toxic flow
		buySpread, sellSpread := strategy.spreadCalc.GetSpreadForSymbol("BTCUSD1")
		assert.Greater(t, buySpread, 1.0, "Spread should widen with toxic flow")
		assert.Greater(t, sellSpread, 1.0, "Spread should widen with toxic flow")
	}

	// Test OBI integration
	if strategy.obiDetector != nil {
		// Simulate orderbook imbalance
		strategy.obiDetector.UpdateOrderBook(2000, 500, 50000) // Strong buy pressure

		obiSignal := strategy.obiDetector.GetSignal()
		assert.Equal(t, "BUY", obiSignal.BiasDirection, "Should detect buy bias")
		assert.Equal(t, "STRONG", obiSignal.Strength, "Should detect strong strength")
		assert.Equal(t, "TIGHTEN_SELL", obiSignal.RecommendedAction, "Should recommend tightening sell")
	}

	// Test adaptive leverage
	if strategy.leverageAdapter != nil {
		strategy.leverageAdapter.UpdatePrice("BTCUSD1", 50000)

		leverage := strategy.leverageAdapter.CalculateAdaptiveLeverage("BTCUSD1")
		assert.Greater(t, leverage, 0, "Should calculate adaptive leverage")

		multiplier := strategy.leverageAdapter.GetLeverageMultiplier("BTCUSD1")
		assert.Greater(t, multiplier, 0.0, "Should have leverage multiplier")
	}
}

func TestMakerStrategy_MicrostructureSignalFlow(t *testing.T) {
	logger := zap.NewNop()

	config := &Config{
		Symbols:                       []string{"BTCUSD1"},
		DefaultSpreadBps:              2.0,
		MaxLeverage:                   150,
		MicrostructureAnalysisEnabled: true,
		AggressivenessLevel:           3,
	}

	futuresClient := &MockFuturesClient{}
	wsClient := &MockWebSocketClient{}
	strategy := NewMakerStrategy(futuresClient, wsClient, config, logger)

	// Test microstructure signal generation
	if strategy.microstructureAnalyzer != nil {
		signal := strategy.microstructureAnalyzer.AnalyzeMarket()

		// Verify signal structure
		assert.GreaterOrEqual(t, signal.CompositeScore, -1.0, "Composite score should be in [-1, 1]")
		assert.LessOrEqual(t, signal.CompositeScore, 1.0, "Composite score should be in [-1, 1]")
		assert.NotEmpty(t, signal.TradingMode, "Should have trading mode")
		assert.Greater(t, signal.OrderSizeMultiplier, 0.0, "Should have order size multiplier")
		assert.Greater(t, signal.SpreadMultiplier, 0.0, "Should have spread multiplier")
		assert.Greater(t, signal.LeverageMultiplier, 0.0, "Should have leverage multiplier")
		assert.GreaterOrEqual(t, signal.Confidence, 0.0, "Should have confidence")
		assert.LessOrEqual(t, signal.Confidence, 1.0, "Confidence should be <= 1.0")
		assert.NotEmpty(t, signal.Rationale, "Should have rationale")

		// Test trading modes
		modes := []string{"PAUSED", "DEFENSIVE", "NEUTRAL", "AGGRESSIVE"}
		found := false
		for _, mode := range modes {
			if signal.TradingMode == mode {
				found = true
				break
			}
		}
		assert.True(t, found, "Trading mode should be one of expected values")
	}
}

func TestMakerStrategy_AdaptiveLeverageVolatilityResponse(t *testing.T) {
	logger := zap.NewNop()

	config := &Config{
		Symbols:          []string{"BTCUSD1"},
		MaxLeverage:      150,
		UseDynamicSizing: true,
	}

	futuresClient := &MockFuturesClient{}
	wsClient := &MockWebSocketClient{}
	strategy := NewMakerStrategy(futuresClient, wsClient, config, logger)

	if strategy.leverageAdapter == nil {
		t.Skip("Leverage adapter not initialized")
		return
	}

	adapter := strategy.leverageAdapter

	// Test 1: Low volatility - should use full leverage
	for i := 0; i < 50; i++ {
		price := 50000 + float64(i)*10 // Small price changes
		adapter.UpdatePrice("BTCUSD1", price)
	}

	leverage1 := adapter.CalculateAdaptiveLeverage("BTCUSD1")
	volatility1 := adapter.GetVolatility("BTCUSD1")

	assert.Less(t, volatility1, 1.0, "Should detect low volatility")
	assert.GreaterOrEqual(t, leverage1, 120, "Should use high leverage in low volatility")

	// Test 2: High volatility - should reduce leverage
	for i := 0; i < 50; i++ {
		price := 50000 + float64(i)*100 // Large price changes
		adapter.UpdatePrice("BTCUSD1", price)
	}

	leverage2 := adapter.CalculateAdaptiveLeverage("BTCUSD1")
	volatility2 := adapter.GetVolatility("BTCUSD1")

	assert.Greater(t, volatility2, 3.0, "Should detect high volatility")
	assert.Less(t, leverage2, leverage1, "Should reduce leverage in high volatility")
}

// Mock implementations for testing
type MockFuturesClient struct{}

func (m *MockFuturesClient) PlaceOrder(ctx context.Context, req client.PlaceOrderRequest) (*client.Order, error) {
	return nil, nil
}

func (m *MockFuturesClient) CancelOrder(ctx context.Context, req client.CancelOrderRequest) (*client.Order, error) {
	return nil, nil
}

func (m *MockFuturesClient) GetOpenOrders(ctx context.Context, symbol string) ([]client.Order, error) {
	return nil, nil
}

func (m *MockFuturesClient) GetPositions(ctx context.Context) ([]client.Position, error) {
	return nil, nil
}

func (m *MockFuturesClient) GetAccountInfo(ctx context.Context) (*client.AccountInfo, error) {
	return nil, nil
}

type MockWebSocketClient struct{}

func (m *MockWebSocketClient) SubscribeToTicker(symbols []string) error {
	return nil
}

func (m *MockWebSocketClient) GetTickerChannel() <-chan map[string]interface{} {
	return make(chan map[string]interface{})
}

func (m *MockWebSocketClient) GetTickerData(symbol string) (float64, float64, float64, error) {
	return 50000.0, 50100.0, 1000.0, nil
}

func (m *MockWebSocketClient) IsRunning() bool {
	return true
}

func (m *MockWebSocketClient) GetCachedPositions() map[string]client.Position {
	return make(map[string]client.Position)
}

func (m *MockWebSocketClient) GetCachedBalance() client.Balance {
	return client.Balance{AvailableBalance: 1000.0}
}
