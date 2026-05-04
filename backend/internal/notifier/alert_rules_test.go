package notifier

import (
	"testing"
)

func TestAlertRules(t *testing.T) {
	config := DefaultAlertConfig

	// Test High Volatility
	if !HasHighVolatility(100, 103, config) {
		t.Error("Expected high volatility alert for 3% jump")
	}

	if HasHighVolatility(100, 101, config) {
		t.Error("Did not expect high volatility alert for 1% jump")
	}

	// Test Drawdown Critical
	m1 := GridMetrics{DrawdownPct: 6.0}
	if !IsDrawdownCritical(m1, config) {
		t.Error("Expected critical drawdown alert")
	}

	m2 := GridMetrics{DrawdownPct: 2.0}
	if IsDrawdownCritical(m2, config) {
		t.Error("Did not expect critical drawdown alert")
	}

	// Test Grid Breakout
	m3 := GridMetrics{CurrentPrice: 1000, GridMinPrice: 900, GridMaxPrice: 1100}
	if HasGridBreakout(m3) {
		t.Error("Did not expect grid breakout when inside range")
	}

	m4 := GridMetrics{CurrentPrice: 800, GridMinPrice: 900, GridMaxPrice: 1100}
	if !HasGridBreakout(m4) {
		t.Error("Expected grid breakout when below min bounds")
	}
}
