package adaptive_grid

import (
	"math"
	"testing"

	"go.uber.org/zap"
)

func TestNewFluidFlowEngine(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	if engine == nil {
		t.Error("Expected non-nil engine")
	}
	if engine.flowIntensity == nil {
		t.Error("Expected flowIntensity map to be initialized")
	}
	if engine.flowDirection == nil {
		t.Error("Expected flowDirection map to be initialized")
	}
	if engine.flowVelocity == nil {
		t.Error("Expected flowVelocity map to be initialized")
	}
}

func TestCalculateFlow(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Test normal conditions
	params := engine.CalculateFlow("BTC", 0.5, 0.3, 0.2, 0.5, 0.0, 0.8)

	if params.Intensity < 0 || params.Intensity > 1 {
		t.Errorf("Expected intensity in [0,1], got %v", params.Intensity)
	}
	if params.Direction < -1 || params.Direction > 1 {
		t.Errorf("Expected direction in [-1,1], got %v", params.Direction)
	}
	if params.SizeMultiplier < 0.1 || params.SizeMultiplier > 1 {
		t.Errorf("Expected size multiplier in [0.1,1], got %v", params.SizeMultiplier)
	}
	if params.SpreadMultiplier < 0.5 || params.SpreadMultiplier > 2 {
		t.Errorf("Expected spread multiplier in [0.5,2], got %v", params.SpreadMultiplier)
	}
}

func TestCalculateFlow_HighRisk(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// High risk should reduce intensity
	params := engine.CalculateFlow("BTC", 0.5, 0.3, 0.9, 0.5, 0.0, 0.8)

	if params.Intensity > 0.6 {
		t.Errorf("Expected reduced intensity for high risk, got %v", params.Intensity)
	}
	if params.SizeMultiplier > 0.4 {
		t.Errorf("Expected low size multiplier for high risk, got %v", params.SizeMultiplier)
	}
}

func TestCalculateFlow_HighVolatility(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// High volatility should reduce intensity and widen spread
	params := engine.CalculateFlow("BTC", 0.5, 0.9, 0.2, 0.5, 0.0, 0.8)

	if params.Intensity > 0.6 {
		t.Errorf("Expected moderate intensity for high volatility, got %v", params.Intensity)
	}
	if params.SpreadMultiplier < 1.0 {
		t.Errorf("Expected wider spread for high volatility, got %v", params.SpreadMultiplier)
	}
}

func TestCalculateFlow_ExtremeSkew(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Extreme skew should reduce intensity and affect direction
	params := engine.CalculateFlow("BTC", 0.5, 0.3, 0.2, 0.5, 0.8, 0.8)

	if params.Intensity > 0.7 {
		t.Errorf("Expected reduced intensity for extreme skew, got %v", params.Intensity)
	}
	// Skew = 0.8 (long bias), direction should be negative (long bias)
	if params.Direction > -0.3 {
		t.Errorf("Expected negative direction for long skew, got %v", params.Direction)
	}
}

func TestCalculateFlow_HighLiquidity(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// High liquidity should increase intensity
	params := engine.CalculateFlow("BTC", 0.5, 0.3, 0.2, 0.5, 0.0, 1.0)

	if params.Intensity < 0.5 {
		t.Errorf("Expected higher intensity for high liquidity, got %v", params.Intensity)
	}
}

func TestShouldPauseTrading(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Calculate flow with very poor conditions
	engine.CalculateFlow("BTC", 0.9, 0.9, 0.9, 0.1, 0.0, 0.1)

	if !engine.ShouldPauseTrading("BTC") {
		t.Error("Expected trading to be paused with very low intensity")
	}

	// Calculate flow with normal conditions
	engine.CalculateFlow("BTC", 0.5, 0.3, 0.2, 0.5, 0.0, 0.8)

	if engine.ShouldPauseTrading("BTC") {
		t.Error("Expected trading not to be paused with normal intensity")
	}
}

func TestShouldAggressiveMode(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Calculate flow with excellent conditions
	engine.CalculateFlow("BTC", 0.1, 0.2, 0.1, 0.8, 0.0, 0.9)

	if !engine.ShouldAggressiveMode("BTC") {
		t.Error("Expected aggressive mode with high intensity")
	}

	// Calculate flow with normal conditions
	engine.CalculateFlow("BTC", 0.5, 0.3, 0.2, 0.5, 0.0, 0.8)

	if engine.ShouldAggressiveMode("BTC") {
		t.Error("Expected not aggressive mode with normal intensity")
	}
}

func TestShouldDefensiveMode(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Calculate flow with poor conditions
	engine.CalculateFlow("BTC", 0.7, 0.5, 0.4, 0.3, 0.0, 0.5)

	if !engine.ShouldDefensiveMode("BTC") {
		t.Error("Expected defensive mode with low intensity")
	}

	// Calculate flow with excellent conditions
	engine.CalculateFlow("BTC", 0.1, 0.2, 0.1, 0.8, 0.0, 0.9)

	if engine.ShouldDefensiveMode("BTC") {
		t.Error("Expected not defensive mode with high intensity")
	}
}

func TestUpdateWeights(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Update weights
	engine.UpdateWeights(0.5, 0.2, 0.2, 0.05, 0.05)

	// Verify weights sum to 1
	total := engine.weights.Volatility + engine.weights.Trend + engine.weights.Risk + engine.weights.Skew + engine.weights.Liquidity
	if math.Abs(total-1.0) > 0.01 {
		t.Errorf("Expected weights to sum to 1.0, got %v", total)
	}
}

func TestGetFlowParameters(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Calculate flow first
	params := engine.CalculateFlow("BTC", 0.5, 0.3, 0.2, 0.5, 0.0, 0.8)

	// Get flow parameters
	retrievedParams := engine.GetFlowParameters("BTC")

	if retrievedParams.Intensity != params.Intensity {
		t.Errorf("Expected intensity %v, got %v", params.Intensity, retrievedParams.Intensity)
	}
	if retrievedParams.Direction != params.Direction {
		t.Errorf("Expected direction %v, got %v", params.Direction, retrievedParams.Direction)
	}
}

func TestGetFlowState(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Calculate flow
	engine.CalculateFlow("BTC", 0.5, 0.3, 0.2, 0.5, 0.0, 0.8)

	// Get flow state
	state := engine.GetFlowState("BTC")

	if state["intensity"] == nil {
		t.Error("Expected intensity in flow state")
	}
	if state["direction"] == nil {
		t.Error("Expected direction in flow state")
	}
	if state["velocity"] == nil {
		t.Error("Expected velocity in flow state")
	}
	if state["weights"] == nil {
		t.Error("Expected weights in flow state")
	}
}

func TestConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	engine := NewFluidFlowEngine(logger)

	// Test concurrent access
	done := make(chan bool)

	// Goroutine 1: Calculate flow
	go func() {
		for i := 0; i < 100; i++ {
			engine.CalculateFlow("BTC", 0.5, 0.3, 0.2, 0.5, 0.0, 0.8)
		}
		done <- true
	}()

	// Goroutine 2: Get flow parameters
	go func() {
		for i := 0; i < 100; i++ {
			engine.GetFlowParameters("BTC")
		}
		done <- true
	}()

	// Goroutine 3: Update weights
	go func() {
		for i := 0; i < 100; i++ {
			engine.UpdateWeights(0.5, 0.2, 0.2, 0.05, 0.05)
		}
		done <- true
	}()

	<-done
	<-done
	<-done

	// If we get here without panic, concurrent access works
}
