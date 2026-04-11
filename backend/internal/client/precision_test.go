package client

import (
	"strconv"
	"testing"
)

// TestRoundQty_BTC_DoesNotRoundToZero tests that small BTC quantities don't round to 0
// This was the root cause of "Quantity less than zero" errors for BTC at 72k+
func TestRoundQty_BTC_DoesNotRoundToZero(t *testing.T) {
	pm := NewPrecisionManager()

	// Simulate BTC precision from exchange info
	// BTC typically has StepSize=0.001 (precision 3)
	pm.symbols["BTCUSD1"] = SymbolPrecision{
		Symbol:       "BTCUSD1",
		StepSize:     0.001,
		QuantityPrec: 3,
		TickSize:     0.01,
		PricePrec:    2,
	}

	// Test case: BTC at 72k with small notional (e.g., $10 budget per grid)
	// quantity = 10 / 72756 = ~0.000137
	// With old Floor logic: math.Floor(0.000137/0.001) = math.Floor(0.137) = 0 -> "0.000"
	// With new Ceil logic: math.Ceil(0.000137/0.001) = math.Ceil(0.137) = 1 -> "0.001"

	testCases := []struct {
		name     string
		qty      float64
		expected string
		minQty   float64 // minimum quantity we expect
	}{
		{
			name:     "Small BTC quantity - should round UP to 0.001",
			qty:      0.000137, // ~$10 at 72k
			expected: "0.001",
			minQty:   0.001,
		},
		{
			name:     "Just below step size - should round UP",
			qty:      0.0005,
			expected: "0.001",
			minQty:   0.001,
		},
		{
			name:     "Exactly at step size - should stay",
			qty:      0.001,
			expected: "0.001",
			minQty:   0.001,
		},
		{
			name:     "Multiple of step size",
			qty:      0.0025,
			expected: "0.003",
			minQty:   0.003,
		},
		{
			name:     "Larger quantity",
			qty:      0.01,
			expected: "0.010",
			minQty:   0.01,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := pm.RoundQty("BTCUSD1", tc.qty)
			if result == "0" || result == "0.000" {
				t.Errorf("RoundQty(%f) returned %s - quantity rounded to zero! This causes 'Quantity less than zero' errors",
					tc.qty, result)
			}

			parsed, _ := strconv.ParseFloat(result, 64)
			if parsed < tc.minQty {
				t.Errorf("RoundQty(%f) = %s, want at least %f",
					tc.qty, result, tc.minQty)
			}

			t.Logf("RoundQty(%f) = %s (min expected: %f)", tc.qty, result, tc.minQty)
		})
	}
}

// TestRoundQty_ETH works correctly for ETH with smaller step size
func TestRoundQty_ETH(t *testing.T) {
	pm := NewPrecisionManager()

	// ETH typically has StepSize=0.0001 (precision 4)
	pm.symbols["ETHUSDT"] = SymbolPrecision{
		Symbol:       "ETHUSDT",
		StepSize:     0.0001,
		QuantityPrec: 4,
		TickSize:     0.01,
		PricePrec:    2,
	}

	testCases := []struct {
		name string
		qty  float64
	}{
		{
			name: "Small ETH quantity",
			qty:  0.001, // ~$2 at $2000/ETH
		},
		{
			name: "Very small ETH quantity",
			qty:  0.00005, // ~$0.10 at $2000/ETH
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := pm.RoundQty("ETHUSDT", tc.qty)
			if result == "0" || result == "0.0000" {
				t.Errorf("RoundQty(%f) returned %s - quantity rounded to zero!",
					tc.qty, result)
			}
			t.Logf("RoundQty(%f) = %s", tc.qty, result)
		})
	}
}
