package adaptive_grid

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestCalculateLiquidationBuffer tests dynamic liquidation buffer calculation
func TestCalculateLiquidationBuffer(t *testing.T) {
	logger := zap.NewNop()
	manager := &AdaptiveGridManager{
		logger: logger,
		riskConfig: &RiskConfig{
			LiquidationBufferPct: 0.35, // Default fallback
		},
	}

	tests := []struct {
		name     string
		leverage float64
		expected float64
	}{
		{
			name:     "100x leverage should have 50% buffer",
			leverage: 100,
			expected: 0.50,
		},
		{
			name:     "125x leverage should have 50% buffer",
			leverage: 125,
			expected: 0.50,
		},
		{
			name:     "50x leverage should have 35% buffer",
			leverage: 50,
			expected: 0.35,
		},
		{
			name:     "75x leverage should have 35% buffer",
			leverage: 75,
			expected: 0.35,
		},
		{
			name:     "20x leverage should have 25% buffer",
			leverage: 20,
			expected: 0.25,
		},
		{
			name:     "30x leverage should have 25% buffer",
			leverage: 30,
			expected: 0.25,
		},
		{
			name:     "10x leverage should have 20% buffer",
			leverage: 10,
			expected: 0.20,
		},
		{
			name:     "1x leverage should have 20% buffer",
			leverage: 1,
			expected: 0.20,
		},
		{
			name:     "0 leverage should have 20% buffer",
			leverage: 0,
			expected: 0.20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CalculateLiquidationBuffer(tt.leverage)
			assert.Equal(t, tt.expected, result, "Buffer percentage mismatch for leverage %f", tt.leverage)
		})
	}
}

// TestIsNearLiquidationWithDynamicBuffer tests liquidation check with dynamic buffer
func TestIsNearLiquidationWithDynamicBuffer(t *testing.T) {
	logger := zap.NewNop()
	manager := &AdaptiveGridManager{
		logger:     logger,
		positions:  make(map[string]*SymbolPosition),
		riskConfig: &RiskConfig{LiquidationBufferPct: 0.35},
	}

	// Test with 100x leverage position - 50% buffer
	manager.positions["BTCUSD1"] = &SymbolPosition{
		PositionAmt: 0.1,
		MarkPrice:   50000,
		Leverage:    100,
	}

	// Liquidation price for long 100x at $50k entry is roughly $49,500 (1% away)
	// With 50% buffer, effective buffer = 1/100 * 0.50 = 0.005 (0.5%)
	// We should trigger when distance < 0.5%
	markPrice := 50000.0
	liqPrice := 49760.0 // 0.48% away (< 0.5%, should trigger)

	result := manager.isNearLiquidation("BTCUSD1", markPrice, liqPrice, 0.1)
	assert.True(t, result, "Should trigger liquidation warning when within effective buffer for 100x leverage")

	// Further away should not trigger
	liqPriceFar := 49000.0 // 2% away
	result = manager.isNearLiquidation("BTCUSD1", markPrice, liqPriceFar, 0.1)
	assert.False(t, result, "Should not trigger when further than buffer")

	// Test with 20x leverage position - 25% buffer
	manager.positions["ETHUSD1"] = &SymbolPosition{
		PositionAmt: 1.0,
		MarkPrice:   3000,
		Leverage:    20,
	}

	// With 25% buffer on 20x: effective buffer = 1/20 * 0.25 = 0.0125 (1.25%)
	// Trigger when distance < 1.25%
	markPrice2 := 3000.0
	liqPrice2 := 2963.0 // 1.233% away (< 1.25%, should trigger)

	result = manager.isNearLiquidation("ETHUSD1", markPrice2, liqPrice2, 1.0)
	assert.True(t, result, "Should trigger liquidation warning when distance < effective buffer for 20x leverage")

	// Test without symbol (uses fallback 100x leverage assumption)
	// effective buffer = 0.01 * 0.35 = 0.0035 (0.35%)
	// liqPrice 49750 is 0.5% away (> 0.35%), should NOT trigger
	result = manager.isNearLiquidation("", markPrice, liqPrice, 0.1)
	assert.False(t, result, "Should use fallback buffer and not trigger when beyond effective buffer")

	// Test with empty position - same fallback logic
	result = manager.isNearLiquidation("UNKNOWN", markPrice, liqPrice, 0.1)
	assert.False(t, result, "Should use fallback buffer when position not found")
}
