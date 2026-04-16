package adaptive_grid

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestContinuousState_NewContinuousState(t *testing.T) {
	cs := NewContinuousState()

	if cs == nil {
		t.Fatal("NewContinuousState returned nil")
	}

	// Check default values
	if cs.smoothedRisk != 0.5 {
		t.Errorf("Expected smoothedRisk 0.5, got %f", cs.smoothedRisk)
	}
	if cs.smoothedTrend != 0.5 {
		t.Errorf("Expected smoothedTrend 0.5, got %f", cs.smoothedTrend)
	}
	if cs.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestContinuousState_CalculatePositionSize(t *testing.T) {
	cs := NewContinuousState()

	tests := []struct {
		name             string
		positionNotional float64
		maxPosition      float64
		expected         float64
	}{
		{"Zero position", 0, 100, 0},
		{"50% of max", 50, 100, 0.5},
		{"100% of max", 100, 100, 1},
		{"150% of max (clamped)", 150, 100, 1},
		{"Zero max", 50, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cs.CalculatePositionSize(tt.positionNotional, tt.maxPosition)
			if result != tt.expected {
				t.Errorf("CalculatePositionSize(%f, %f) = %f, expected %f",
					tt.positionNotional, tt.maxPosition, result, tt.expected)
			}
		})
	}
}

func TestContinuousState_CalculateVolatility(t *testing.T) {
	cs := NewContinuousState()

	tests := []struct {
		name        string
		atrPct      float64
		bbWidthPct  float64
		expectedMin float64
		expectedMax float64
	}{
		{"Zero volatility", 0, 0, 0, 0},
		{"Low volatility", 0.001, 0.01, 0.08, 0.15},
		{"Normal volatility", 0.01, 0.025, 0.4, 0.6},
		{"High volatility", 0.02, 0.05, 0.8, 1},
		{"Extreme volatility (clamped)", 0.03, 0.1, 0.95, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cs.CalculateVolatility(tt.atrPct, tt.bbWidthPct)
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("CalculateVolatility(%f, %f) = %f, expected range [%f, %f]",
					tt.atrPct, tt.bbWidthPct, result, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestContinuousState_CalculateRisk(t *testing.T) {
	cs := NewContinuousState()

	tests := []struct {
		name        string
		pnl         float64
		drawdown    float64
		expectedMin float64
		expectedMax float64
	}{
		{"Profitable, no drawdown", 10, 0, 0, 0.2},
		{"Break even, no drawdown", 0, 0, 0.3, 0.7},
		{"Small loss, no drawdown", -5, 0, 0.5, 1},
		{"Large loss (clamped)", -20, 0, 0.65, 0.75},
		{"Break even, 10% drawdown", 0, 0.1, 0.35, 0.75},
		{"Break even, 20% drawdown", 0, 0.2, 0.4, 0.8},
		{"Loss, high drawdown", -10, 0.15, 0.7, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cs.CalculateRisk(tt.pnl, tt.drawdown)
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("CalculateRisk(%f, %f) = %f, expected range [%f, %f]",
					tt.pnl, tt.drawdown, result, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestContinuousState_CalculateTrend(t *testing.T) {
	cs := NewContinuousState()

	tests := []struct {
		name     string
		adx      float64
		expected float64
	}{
		{"No trend", 0, 0},
		{"Weak trend", 20, 0.333},
		{"Moderate trend", 40, 0.667},
		{"Strong trend", 60, 1},
		{"Extreme trend (clamped)", 80, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cs.CalculateTrend(tt.adx)
			// Allow small floating point error
			if result < tt.expected-0.01 || result > tt.expected+0.01 {
				t.Errorf("CalculateTrend(%f) = %f, expected %f", tt.adx, result, tt.expected)
			}
		})
	}
}

func TestContinuousState_CalculateSkew(t *testing.T) {
	cs := NewContinuousState()

	tests := []struct {
		name      string
		inventory float64
		expected  float64
	}{
		{"Balanced", 0, 0},
		{"Long bias", 5, 0.5},
		{"Short bias", -5, -0.5},
		{"Extreme long (clamped)", 15, 1},
		{"Extreme short (clamped)", -15, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cs.CalculateSkew(tt.inventory)
			if result != tt.expected {
				t.Errorf("CalculateSkew(%f) = %f, expected %f", tt.inventory, result, tt.expected)
			}
		})
	}
}

func TestContinuousState_UpdateContinuousState(t *testing.T) {
	logger := zap.NewNop()
	cs := NewContinuousState()
	config := &ContinuousStateConfig{
		SmoothingAlpha: 0.3,
	}

	// First update
	cs.UpdateContinuousState(
		50, 100, // positionNotional, maxPosition
		0.01, 0.025, // atrPct, bbWidthPct
		-5, 0, // pnl, drawdown
		30, // adx
		2,  // inventory
		config, logger,
	)

	// Check raw values
	if cs.PositionSize != 0.5 {
		t.Errorf("Expected PositionSize 0.5, got %f", cs.PositionSize)
	}
	if cs.Volatility < 0.4 || cs.Volatility > 0.6 {
		t.Errorf("Expected Volatility in [0.4, 0.6], got %f", cs.Volatility)
	}

	// Check smoothing is applied (first update returns raw value when previous is 0)
	if cs.smoothedPositionSize != 0.5 {
		t.Errorf("Expected smoothedPositionSize 0.5 on first update, got %f", cs.smoothedPositionSize)
	}

	// Second update - should smooth further
	cs.UpdateContinuousState(
		80, 100, // positionNotional, maxPosition
		0.02, 0.05, // atrPct, bbWidthPct
		-10, 0.1, // pnl, drawdown
		40, // adx
		5,  // inventory
		config, logger,
	)

	// Check that smoothed values changed (should be between 0.5 and 0.8)
	// EMA: 0.3 * 0.8 + 0.7 * 0.5 = 0.24 + 0.35 = 0.59
	if cs.smoothedPositionSize < 0.55 || cs.smoothedPositionSize > 0.65 {
		t.Errorf("Expected smoothedPositionSize in [0.55, 0.65], got %f", cs.smoothedPositionSize)
	}
}

func TestContinuousState_GetSmoothedState(t *testing.T) {
	cs := NewContinuousState()
	logger := zap.NewNop()
	config := &ContinuousStateConfig{SmoothingAlpha: 0.3}

	cs.UpdateContinuousState(
		50, 100, 0.01, 0.025, -5, 0, 30, 2, config, logger,
	)

	posSize, vol, risk, trend, skew := cs.GetSmoothedState()

	if posSize < 0 || posSize > 1 {
		t.Errorf("posSize should be in [0,1], got %f", posSize)
	}
	if vol < 0 || vol > 1 {
		t.Errorf("vol should be in [0,1], got %f", vol)
	}
	if risk < 0 || risk > 1 {
		t.Errorf("risk should be in [0,1], got %f", risk)
	}
	if trend < 0 || trend > 1 {
		t.Errorf("trend should be in [0,1], got %f", trend)
	}
	if skew < -1 || skew > 1 {
		t.Errorf("skew should be in [-1,1], got %f", skew)
	}
}

func TestContinuousState_GetRawState(t *testing.T) {
	cs := NewContinuousState()
	logger := zap.NewNop()
	config := &ContinuousStateConfig{SmoothingAlpha: 0.3}

	cs.UpdateContinuousState(
		50, 100, 0.01, 0.025, -5, 0, 30, 2, config, logger,
	)

	posSize, vol, risk, trend, skew := cs.GetRawState()

	// Raw values should match the calculated values
	if posSize != cs.PositionSize {
		t.Errorf("Expected posSize %f, got %f", cs.PositionSize, posSize)
	}
	if vol != cs.Volatility {
		t.Errorf("Expected vol %f, got %f", cs.Volatility, vol)
	}
	if risk != cs.Risk {
		t.Errorf("Expected risk %f, got %f", cs.Risk, risk)
	}
	if trend != cs.Trend {
		t.Errorf("Expected trend %f, got %f", cs.Trend, trend)
	}
	if skew != cs.Skew {
		t.Errorf("Expected skew %f, got %f", cs.Skew, skew)
	}
}

func TestContinuousState_GetStateVector(t *testing.T) {
	cs := NewContinuousState()
	logger := zap.NewNop()
	config := &ContinuousStateConfig{SmoothingAlpha: 0.3}

	cs.UpdateContinuousState(
		50, 100, 0.01, 0.025, -5, 0, 30, 2, config, logger,
	)

	// Test smoothed vector
	vector := cs.GetStateVector(true)
	if len(vector) != 5 {
		t.Errorf("Expected vector length 5, got %d", len(vector))
	}

	// Test raw vector
	vector = cs.GetStateVector(false)
	if len(vector) != 5 {
		t.Errorf("Expected vector length 5, got %d", len(vector))
	}

	// Verify raw vector matches raw state
	posSize, vol, risk, trend, skew := cs.GetRawState()
	if vector[0] != posSize {
		t.Errorf("Expected vector[0] %f, got %f", posSize, vector[0])
	}
	if vector[1] != vol {
		t.Errorf("Expected vector[1] %f, got %f", vol, vector[1])
	}
	if vector[2] != risk {
		t.Errorf("Expected vector[2] %f, got %f", risk, vector[2])
	}
	if vector[3] != trend {
		t.Errorf("Expected vector[3] %f, got %f", trend, vector[3])
	}
	if vector[4] != skew {
		t.Errorf("Expected vector[4] %f, got %f", skew, vector[4])
	}
}

func TestContinuousState_UpdateContinuousState_DefaultAlpha(t *testing.T) {
	logger := zap.NewNop()
	cs := NewContinuousState()
	config := &ContinuousStateConfig{SmoothingAlpha: 0} // Invalid, should use default

	cs.UpdateContinuousState(
		50, 100, 0.01, 0.025, -5, 0, 30, 2, config, logger,
	)

	// Should have applied smoothing with default alpha (0.3)
	if cs.smoothedPositionSize == 0 {
		t.Error("smoothedPositionSize should have been updated with default alpha")
	}
}

func TestContinuousState_Timestamp(t *testing.T) {
	cs := NewContinuousState()
	logger := zap.NewNop()
	config := &ContinuousStateConfig{SmoothingAlpha: 0.3}

	initialTime := cs.Timestamp
	time.Sleep(10 * time.Millisecond)

	cs.UpdateContinuousState(
		50, 100, 0.01, 0.025, -5, 0, 30, 2, config, logger,
	)

	if !cs.Timestamp.After(initialTime) {
		t.Error("Timestamp should be updated on each update")
	}
}
