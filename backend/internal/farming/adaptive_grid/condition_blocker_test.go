package adaptive_grid

import (
	"math"
	"testing"

	"go.uber.org/zap"
)

func TestConditionBlocker_NewConditionBlocker(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

	if cb == nil {
		t.Fatal("NewConditionBlocker returned nil")
	}

	if cb.config == nil {
		t.Fatal("config not initialized")
	}

	// Verify default weights
	if cb.config.PositionSizeWeight != 0.3 {
		t.Errorf("Expected PositionSizeWeight 0.3, got %f", cb.config.PositionSizeWeight)
	}
	if cb.config.VolatilityWeight != 0.2 {
		t.Errorf("Expected VolatilityWeight 0.2, got %f", cb.config.VolatilityWeight)
	}
	if cb.config.RiskWeight != 0.25 {
		t.Errorf("Expected RiskWeight 0.25, got %f", cb.config.RiskWeight)
	}
	if cb.config.TrendWeight != 0.1 {
		t.Errorf("Expected TrendWeight 0.1, got %f", cb.config.TrendWeight)
	}
	if cb.config.SkewWeight != 0.15 {
		t.Errorf("Expected SkewWeight 0.15, got %f", cb.config.SkewWeight)
	}
}

func TestConditionBlocker_SetConfig(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

	newConfig := &ConditionBlockerConfig{
		PositionSizeWeight: 0.4,
		VolatilityWeight:   0.3,
		RiskWeight:         0.2,
		TrendWeight:        0.05,
		SkewWeight:         0.05,
		BlockingThreshold:  0.8,
		MicroModeMin:       0.15,
	}

	cb.SetConfig(newConfig)

	if cb.config.PositionSizeWeight != 0.4 {
		t.Errorf("Expected PositionSizeWeight 0.4, got %f", cb.config.PositionSizeWeight)
	}
	if cb.config.BlockingThreshold != 0.8 {
		t.Errorf("Expected BlockingThreshold 0.8, got %f", cb.config.BlockingThreshold)
	}
}

func TestConditionBlocker_NormalizePositionSize(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

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
		{"Zero max (should return 0)", 50, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cb.NormalizePositionSize(tt.positionNotional, tt.maxPosition)
			if result != tt.expected {
				t.Errorf("NormalizePositionSize(%f, %f) = %f, expected %f",
					tt.positionNotional, tt.maxPosition, result, tt.expected)
			}
		})
	}
}

func TestConditionBlocker_NormalizeVolatility(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

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
			result := cb.NormalizeVolatility(tt.atrPct, tt.bbWidthPct)
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("NormalizeVolatility(%f, %f) = %f, expected range [%f, %f]",
					tt.atrPct, tt.bbWidthPct, result, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestConditionBlocker_NormalizeRisk(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

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
			result := cb.NormalizeRisk(tt.pnl, tt.drawdown)
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("NormalizeRisk(%f, %f) = %f, expected range [%f, %f]",
					tt.pnl, tt.drawdown, result, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestConditionBlocker_NormalizeTrend(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

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
			result := cb.NormalizeTrend(tt.adx)
			// Allow small floating point error
			if math.Abs(result-tt.expected) > 0.01 {
				t.Errorf("NormalizeTrend(%f) = %f, expected %f", tt.adx, result, tt.expected)
			}
		})
	}
}

func TestConditionBlocker_NormalizeSkew(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

	tests := []struct {
		name     string
		skew     float64
		expected float64
	}{
		{"Balanced", 0, 0},
		{"Long bias", 0.5, 0.5},
		{"Short bias", -0.5, 0.5},
		{"Extreme long (clamped)", 1.5, 1},
		{"Extreme short (clamped)", -1.5, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cb.NormalizeSkew(tt.skew)
			if result != tt.expected {
				t.Errorf("NormalizeSkew(%f) = %f, expected %f", tt.skew, result, tt.expected)
			}
		})
	}
}

func TestConditionBlocker_CalculateBlockingFactor(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

	tests := []struct {
		name              string
		positionSizeScore float64
		volatilityScore   float64
		riskScore         float64
		trendScore        float64
		skewScore         float64
		expectedMin       float64
		expectedMax       float64
	}{
		{"All zeros (no blocking)", 0, 0, 0, 0, 0, 0.95, 1},
		{"All ones (full block)", 1, 1, 1, 1, 1, 0, 0.05},
		{"Mixed - moderate risk", 0.5, 0.5, 0.5, 0.5, 0.5, 0.45, 0.55},
		{"High position size only", 0.9, 0.1, 0.1, 0.1, 0.1, 0.63, 0.73},
		{"High volatility only", 0.1, 0.9, 0.1, 0.1, 0.1, 0.73, 0.83},
		{"High risk only", 0.1, 0.1, 0.9, 0.1, 0.1, 0.68, 0.78},
		{"High skew only", 0.1, 0.1, 0.1, 0.1, 0.9, 0.76, 0.86},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cb.CalculateBlockingFactor(
				tt.positionSizeScore,
				tt.volatilityScore,
				tt.riskScore,
				tt.trendScore,
				tt.skewScore,
			)
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("CalculateBlockingFactor() = %f, expected range [%f, %f]",
					result, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestConditionBlocker_ShouldBlock(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

	tests := []struct {
		name           string
		blockingFactor float64
		expected       bool
	}{
		{"No block (1.0)", 1.0, false},
		{"No block (0.8)", 0.8, false},
		{"At threshold (0.7)", 0.7, false},
		{"Should block (0.6)", 0.6, true},
		{"Should block (0.0)", 0.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cb.ShouldBlock(tt.blockingFactor)
			if result != tt.expected {
				t.Errorf("ShouldBlock(%f) = %v, expected %v", tt.blockingFactor, result, tt.expected)
			}
		})
	}
}

func TestConditionBlocker_IsMicroMode(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

	tests := []struct {
		name           string
		blockingFactor float64
		expected       bool
	}{
		{"Full mode (1.0)", 1.0, false},
		{"Full mode (0.8)", 0.8, false},
		{"MICRO mode (0.5)", 0.5, true},
		{"MICRO mode (0.2)", 0.2, true},
		{"At MICRO min (0.1)", 0.1, false},
		{"Below MICRO min (0.05)", 0.05, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cb.IsMicroMode(tt.blockingFactor)
			if result != tt.expected {
				t.Errorf("IsMicroMode(%f) = %v, expected %v", tt.blockingFactor, result, tt.expected)
			}
		})
	}
}

func TestConditionBlocker_GetSizeMultiplier(t *testing.T) {
	logger := zap.NewNop()
	cb := NewConditionBlocker(logger)

	tests := []struct {
		name           string
		blockingFactor float64
		expected       float64
	}{
		{"Full mode (1.0)", 1.0, 1.0},
		{"Full mode (0.8)", 0.8, 0.8},
		{"MICRO mode (0.5)", 0.5, 0.1},
		{"MICRO mode (0.2)", 0.2, 0.1},
		{"Blocked (0.05)", 0.05, 0},
		{"Blocked (0.0)", 0.0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cb.GetSizeMultiplier(tt.blockingFactor)
			if result != tt.expected {
				t.Errorf("GetSizeMultiplier(%f) = %f, expected %f", tt.blockingFactor, result, tt.expected)
			}
		})
	}
}
