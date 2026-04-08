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
	// With 50% buffer, we should trigger at $49,750 (0.5% away from mark)
	markPrice := 50000.0
	liqPrice := 49750.0 // Exactly at 50% buffer threshold

	result := manager.isNearLiquidation("BTCUSD1", markPrice, liqPrice, 0.1)
	assert.True(t, result, "Should trigger liquidation warning at 50% buffer for 100x leverage")

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

	// With 25% buffer, trigger at 6.25% distance (25% of 25% = 6.25%)
	markPrice2 := 3000.0
	liqPrice2 := 2812.5 // Exactly at 25% buffer threshold (6.25% from mark)

	result = manager.isNearLiquidation("ETHUSD1", markPrice2, liqPrice2, 1.0)
	assert.True(t, result, "Should trigger liquidation warning at 25% buffer for 20x leverage")

	// Test without symbol (uses default buffer)
	result = manager.isNearLiquidation("", markPrice, liqPrice, 0.1)
	assert.True(t, result, "Should use default buffer when symbol not provided")

	// Test with empty position
	result = manager.isNearLiquidation("UNKNOWN", markPrice, liqPrice, 0.1)
	assert.True(t, result, "Should use default buffer when position not found")
}
