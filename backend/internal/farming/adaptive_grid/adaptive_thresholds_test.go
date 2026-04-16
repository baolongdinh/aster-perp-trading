package adaptive_grid

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAdaptiveThresholdManager_NewAdaptiveThresholdManager(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{
		BasePositionThreshold:   0.8,
		BaseVolatilityThreshold: 0.8,
		BaseRiskThreshold:       0.6,
		BaseTrendThreshold:      0.7,
		AdaptationRate:          0.1,
		MinThreshold:            0.1,
		MaxThreshold:            1.0,
		EnableLearning:          true,
		LearningRate:            0.05,
	}

	atm := NewAdaptiveThresholdManager(logger, config)

	if atm == nil {
		t.Fatal("NewAdaptiveThresholdManager returned nil")
	}

	if atm.config == nil {
		t.Fatal("config not initialized")
	}

	if atm.symbolThresholds == nil {
		t.Fatal("symbolThresholds not initialized")
	}

	if atm.performanceHistory == nil {
		t.Fatal("performanceHistory not initialized")
	}
}

func TestAdaptiveThresholdManager_GetThreshold(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{
		BasePositionThreshold:   0.8,
		BaseVolatilityThreshold: 0.8,
		BaseRiskThreshold:       0.6,
		BaseTrendThreshold:      0.7,
		AdaptationRate:          0.1,
		MinThreshold:            0.1,
		MaxThreshold:            1.0,
		EnableLearning:          true,
		LearningRate:            0.05,
	}

	atm := NewAdaptiveThresholdManager(logger, config)

	tests := []struct {
		name        string
		symbol      string
		dimension   string
		expectedMin float64
		expectedMax float64
	}{
		{"Position threshold", "BTCUSD1", "position", 0.7, 0.9},
		{"Volatility threshold", "BTCUSD1", "volatility", 0.7, 0.9},
		{"Risk threshold", "BTCUSD1", "risk", 0.5, 0.7},
		{"Trend threshold", "BTCUSD1", "trend", 0.6, 0.8},
		{"Unknown dimension", "BTCUSD1", "unknown", 0.4, 0.6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := atm.GetThreshold(tt.symbol, tt.dimension)
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("GetThreshold(%s, %s) = %f, expected range [%f, %f]",
					tt.symbol, tt.dimension, result, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestAdaptiveThresholdManager_CalculateSymbolThreshold(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{
		BasePositionThreshold: 0.8,
		AdaptationRate:        0.1,
		MinThreshold:          0.1,
		MaxThreshold:          1.0,
	}

	atm := NewAdaptiveThresholdManager(logger, config)

	// Test with no symbol-specific thresholds
	result := atm.CalculateSymbolThreshold("BTCUSD1", "position", 0.8)
	if result != 0.8 {
		t.Errorf("Expected 0.8, got %f", result)
	}

	// Add symbol-specific threshold
	atm.mu.Lock()
	atm.symbolThresholds["BTCUSD1"] = &SymbolThresholds{
		Symbol:             "BTCUSD1",
		PositionMultiplier: 1.2,
		LastUpdated:        time.Now(),
	}
	atm.mu.Unlock()

	// Test with symbol-specific threshold
	result = atm.CalculateSymbolThreshold("BTCUSD1", "position", 0.8)
	expected := 0.8 * 1.2
	if result < expected-0.01 || result > expected+0.01 {
		t.Errorf("Expected %f, got %f", expected, result)
	}
}

func TestAdaptiveThresholdManager_CalculateRegimeThreshold(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{}
	atm := NewAdaptiveThresholdManager(logger, config)

	// Placeholder - should return base threshold
	result := atm.CalculateRegimeThreshold("ranging", "position", 0.8)
	if result != 0.8 {
		t.Errorf("Expected 0.8, got %f", result)
	}
}

func TestAdaptiveThresholdManager_CalculatePerformanceThreshold(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{
		BasePositionThreshold: 0.8,
		MinThreshold:          0.1,
		MaxThreshold:          1.0,
		EnableLearning:        true,
		LearningRate:          0.05,
	}

	atm := NewAdaptiveThresholdManager(logger, config)

	// Test with no performance history
	result := atm.CalculatePerformanceThreshold("BTCUSD1", "position", 0.8)
	if result != 0.8 {
		t.Errorf("Expected 0.8, got %f", result)
	}

	// Add performance history with high win rate
	atm.mu.Lock()
	atm.performanceHistory["BTCUSD1"] = &PerformanceHistory{
		Symbol:      "BTCUSD1",
		WinRate:     0.7,
		AvgWin:      10,
		AvgLoss:     -5,
		TotalTrades: 20,
	}
	atm.mu.Unlock()

	// Test with high win rate (should tighten threshold)
	result = atm.CalculatePerformanceThreshold("BTCUSD1", "position", 0.8)
	// Multiplier: 1.0 + (0.7 - 0.5) * 0.5 = 1.1
	// Threshold: 0.8 * 1.1 = 0.88
	if result < 0.85 || result > 0.91 {
		t.Errorf("Expected around 0.88, got %f", result)
	}

	// Add performance history with low win rate
	atm.mu.Lock()
	atm.performanceHistory["BTCUSD1"] = &PerformanceHistory{
		Symbol:      "BTCUSD1",
		WinRate:     0.4,
		AvgWin:      10,
		AvgLoss:     -5,
		TotalTrades: 20,
	}
	atm.mu.Unlock()

	// Test with low win rate (should loosen threshold)
	result = atm.CalculatePerformanceThreshold("BTCUSD1", "position", 0.8)
	// Multiplier: 1.0 + (0.4 - 0.5) * 0.5 = 0.95
	// Threshold: 0.8 * 0.95 = 0.76
	if result < 0.73 || result > 0.79 {
		t.Errorf("Expected around 0.76, got %f", result)
	}
}

func TestAdaptiveThresholdManager_CalculateTimeThreshold(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{}
	atm := NewAdaptiveThresholdManager(logger, config)

	// Placeholder - should return base threshold
	result := atm.CalculateTimeThreshold(time.Now(), "position", 0.8)
	if result != 0.8 {
		t.Errorf("Expected 0.8, got %f", result)
	}
}

func TestAdaptiveThresholdManager_CalculateFundingThreshold(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{}
	atm := NewAdaptiveThresholdManager(logger, config)

	// Placeholder - should return base threshold
	result := atm.CalculateFundingThreshold("0.01", "position", 0.8)
	if result != 0.8 {
		t.Errorf("Expected 0.8, got %f", result)
	}
}

func TestAdaptiveThresholdManager_UpdateThreshold(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{
		BasePositionThreshold: 0.8,
		AdaptationRate:        0.1,
		MinThreshold:          0.1,
		MaxThreshold:          1.0,
	}

	atm := NewAdaptiveThresholdManager(logger, config)

	// Update threshold with good performance
	atm.UpdateThreshold("BTCUSD1", "position", 0.8, 0.5)

	// Check that multiplier was updated
	atm.mu.RLock()
	symbolThresholds := atm.symbolThresholds["BTCUSD1"]
	atm.mu.RUnlock()

	if symbolThresholds == nil {
		t.Fatal("symbolThresholds not created")
	}

	// Good performance (0.5) should tighten threshold
	// Adjustment: 1.0 - (0.5 * 0.1) = 0.95
	expectedMultiplier := 0.95
	if symbolThresholds.PositionMultiplier < expectedMultiplier-0.01 || symbolThresholds.PositionMultiplier > expectedMultiplier+0.01 {
		t.Errorf("Expected multiplier around %f, got %f", expectedMultiplier, symbolThresholds.PositionMultiplier)
	}
}

func TestAdaptiveThresholdManager_AdaptThresholdsBasedOnPerformance(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{
		EnableLearning: true,
		LearningRate:   0.05,
	}

	atm := NewAdaptiveThresholdManager(logger, config)

	// Add performance history
	atm.mu.Lock()
	atm.performanceHistory["BTCUSD1"] = &PerformanceHistory{
		Symbol:      "BTCUSD1",
		WinRate:     0.7,
		AvgWin:      10,
		AvgLoss:     -5,
		TotalTrades: 20,
	}
	atm.mu.Unlock()

	// Adapt thresholds
	atm.AdaptThresholdsBasedOnPerformance()

	// Check that multipliers were updated
	atm.mu.RLock()
	symbolThresholds := atm.symbolThresholds["BTCUSD1"]
	atm.mu.RUnlock()

	if symbolThresholds == nil {
		t.Fatal("symbolThresholds not created")
	}

	// High win rate (0.7) should tighten thresholds
	// Performance score: (0.7 - 0.5) * 2 = 0.4
	// Adjustment: 1.0 - (0.4 * 0.05) = 0.98
	expectedMultiplier := 0.98
	if symbolThresholds.PositionMultiplier < expectedMultiplier-0.01 || symbolThresholds.PositionMultiplier > expectedMultiplier+0.01 {
		t.Errorf("Expected multiplier around %f, got %f", expectedMultiplier, symbolThresholds.PositionMultiplier)
	}
}

func TestAdaptiveThresholdManager_RecordPerformance(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{}
	atm := NewAdaptiveThresholdManager(logger, config)

	// Record performance
	atm.RecordPerformance("BTCUSD1", 0.7, 10, -5, 20)

	// Check that performance was recorded
	atm.mu.RLock()
	perf := atm.performanceHistory["BTCUSD1"]
	atm.mu.RUnlock()

	if perf == nil {
		t.Fatal("Performance not recorded")
	}

	if perf.Symbol != "BTCUSD1" {
		t.Errorf("Expected symbol BTCUSD1, got %s", perf.Symbol)
	}
	if perf.WinRate != 0.7 {
		t.Errorf("Expected win rate 0.7, got %f", perf.WinRate)
	}
	if perf.TotalTrades != 20 {
		t.Errorf("Expected total trades 20, got %d", perf.TotalTrades)
	}
}

func TestAdaptiveThresholdManager_GetOptimalThresholds(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{
		BasePositionThreshold:   0.8,
		BaseVolatilityThreshold: 0.8,
		BaseRiskThreshold:       0.6,
		BaseTrendThreshold:      0.7,
		MinThreshold:            0.1,
		MaxThreshold:            1.0,
	}

	atm := NewAdaptiveThresholdManager(logger, config)

	// Get optimal thresholds
	thresholds := atm.GetOptimalThresholds("BTCUSD1")

	if thresholds == nil {
		t.Fatal("GetOptimalThresholds returned nil")
	}

	if len(thresholds) != 4 {
		t.Errorf("Expected 4 thresholds, got %d", len(thresholds))
	}

	// Check that thresholds are in valid range
	for dimension, threshold := range thresholds {
		if threshold < 0.1 || threshold > 1.0 {
			t.Errorf("Threshold %s out of range: %f", dimension, threshold)
		}
	}
}

func TestAdaptiveThresholdManager_UpdateOptimalThresholds(t *testing.T) {
	logger := zap.NewNop()
	config := &AdaptiveThresholdConfig{
		BasePositionThreshold:   0.8,
		BaseVolatilityThreshold: 0.8,
		BaseRiskThreshold:       0.6,
		BaseTrendThreshold:      0.7,
	}

	atm := NewAdaptiveThresholdManager(logger, config)

	// Update optimal thresholds
	newThresholds := map[string]float64{
		"position":   0.9,
		"volatility": 0.9,
		"risk":       0.7,
		"trend":      0.8,
	}
	atm.UpdateOptimalThresholds("BTCUSD1", newThresholds)

	// Check that multipliers were updated
	atm.mu.RLock()
	symbolThresholds := atm.symbolThresholds["BTCUSD1"]
	atm.mu.RUnlock()

	if symbolThresholds == nil {
		t.Fatal("symbolThresholds not created")
	}

	// Position: 0.9 / 0.8 = 1.125
	expectedPosMultiplier := 1.125
	if symbolThresholds.PositionMultiplier < expectedPosMultiplier-0.01 || symbolThresholds.PositionMultiplier > expectedPosMultiplier+0.01 {
		t.Errorf("Expected position multiplier around %f, got %f", expectedPosMultiplier, symbolThresholds.PositionMultiplier)
	}
}
