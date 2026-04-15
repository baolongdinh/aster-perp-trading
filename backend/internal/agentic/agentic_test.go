package agentic

import (
	"testing"
	"time"

	"aster-bot/internal/config"
)

func TestDefaultAgenticConfig(t *testing.T) {
	cfg := DefaultAgenticConfig()

	if !cfg.Enabled {
		t.Error("Expected agentic to be enabled by default")
	}

	if len(cfg.Universe.Symbols) == 0 {
		t.Error("Expected symbols in universe")
	}

	if cfg.WhitelistManagement.MaxSymbols <= 0 {
		t.Error("Expected MaxSymbols > 0")
	}
}

func TestOpportunityScorer(t *testing.T) {
	scoringConfig := config.ScoringConfig{
		Weights: config.ScoringWeightsConfig{
			Trend:      0.30,
			Volatility: 0.25,
			Volume:     0.25,
			Structure:  0.20,
		},
		Thresholds: config.ScoringThresholdsConfig{
			HighScore:   75,
			MediumScore: 60,
			LowScore:    40,
			SkipScore:   0,
		},
	}

	scorer := NewOpportunityScorer(scoringConfig)

	// Test HIGH score recommendation
	highScore := scorer.CalculateRecommendation(80)
	if highScore != RecHigh {
		t.Errorf("Expected HIGH for score 80, got %s", highScore)
	}

	// Test MEDIUM score recommendation
	medScore := scorer.CalculateRecommendation(65)
	if medScore != RecMedium {
		t.Errorf("Expected MEDIUM for score 65, got %s", medScore)
	}

	// Test SKIP score recommendation
	skipScore := scorer.CalculateRecommendation(30)
	if skipScore != RecSkip {
		t.Errorf("Expected SKIP for score 30, got %s", skipScore)
	}
}

func TestPositionSizer(t *testing.T) {
	sizingConfig := config.PositionSizingConfig{
		ScoreMultipliers: map[string]float64{
			"HIGH":   1.0,
			"MEDIUM": 0.6,
			"LOW":    0.3,
		},
		RegimeMultipliers: map[string]float64{
			"SIDEWAYS": 1.0,
			"TRENDING": 0.7,
			"VOLATILE": 0.5,
		},
	}

	sizer := NewPositionSizer(sizingConfig)

	// Test HIGH + SIDEWAYS = 1.0 * 1.0 = 1.0
	highSideways := SymbolScore{
		Recommendation: RecHigh,
		Regime:         RegimeSideways,
	}
	mult := sizer.CalculateSizeMultiplier(highSideways)
	if mult != 1.0 {
		t.Errorf("Expected 1.0 for HIGH/SIDEWAYS, got %.2f", mult)
	}

	// Test HIGH + VOLATILE = 1.0 * 0.5 = 0.5
	highVolatile := SymbolScore{
		Recommendation: RecHigh,
		Regime:         RegimeVolatile,
	}
	mult = sizer.CalculateSizeMultiplier(highVolatile)
	if mult != 0.5 {
		t.Errorf("Expected 0.5 for HIGH/VOLATILE, got %.2f", mult)
	}

	// Test MEDIUM + SIDEWAYS = 0.6 * 1.0 = 0.6
	medSideways := SymbolScore{
		Recommendation: RecMedium,
		Regime:         RegimeSideways,
	}
	mult = sizer.CalculateSizeMultiplier(medSideways)
	if mult != 0.6 {
		t.Errorf("Expected 0.6 for MEDIUM/SIDEWAYS, got %.2f", mult)
	}
}

func TestWhitelistManager(t *testing.T) {
	cfg := config.WhitelistConfig{
		MaxSymbols:         3,
		MinScoreToAdd:      60,
		ScoreToRemove:      35,
		KeepIfPositionOpen: true,
	}

	// Mock VF controller
	mockVF := &mockVFController{
		positions: []PositionStatus{},
	}

	manager := NewWhitelistManager(cfg, mockVF, nil)

	// Test score update
	scores := map[string]SymbolScore{
		"BTCUSD1": {Symbol: "BTCUSD1", Score: 85, Recommendation: RecHigh},
		"ETHUSD1": {Symbol: "ETHUSD1", Score: 70, Recommendation: RecMedium},
		"SOLUSD1": {Symbol: "SOLUSD1", Score: 45, Recommendation: RecLow},
	}

	// Test that we can store scores
	manager.currentScores = scores
	retrieved := manager.GetCurrentScores()

	if len(retrieved) != 3 {
		t.Errorf("Expected 3 scores, got %d", len(retrieved))
	}
}

func TestCircuitBreaker(t *testing.T) {
	cfg := config.AgenticCircuitBreakerConfig{
		VolatilitySpike: config.VolatilityBreakerConfig{
			Enabled:       true,
			ATRMultiplier: 3.0,
		},
		ConsecutiveLosses: config.ConsecutiveLossBreakerConfig{
			Enabled:       true,
			Threshold:     3,
			SizeReduction: 0.5,
		},
	}

	cb := NewCircuitBreaker(cfg, nil)

	testSymbol := "BTCUSD1"

	// Test that circuit breaker is not tripped initially
	if cb.IsTripped(testSymbol) {
		t.Error("Circuit breaker should not be tripped initially")
	}

	// Test recording consecutive losses
	cb.RecordTradeOutcome(testSymbol, false) // loss
	cb.RecordTradeOutcome(testSymbol, false) // loss
	cb.RecordTradeOutcome(testSymbol, false) // loss

	// Check if tripped after 3 losses
	if !cb.IsTripped(testSymbol) {
		t.Error("Circuit breaker should trip after 3 consecutive losses")
	}

	// Test reset by recording a win
	cb.RecordTradeOutcome(testSymbol, true) // win
	if cb.IsTripped(testSymbol) {
		t.Error("Circuit breaker should not be tripped after recording a win")
	}
}

func TestIndicatorCalculator(t *testing.T) {
	calc := NewIndicatorCalculator(14, 20, 14)

	// Create test candles
	candles := []Candle{
		{Open: 100, High: 105, Low: 98, Close: 102, Volume: 1000, Timestamp: time.Now().Add(-5 * time.Minute)},
		{Open: 102, High: 106, Low: 99, Close: 104, Volume: 1200, Timestamp: time.Now().Add(-4 * time.Minute)},
		{Open: 104, High: 108, Low: 101, Close: 103, Volume: 1100, Timestamp: time.Now().Add(-3 * time.Minute)},
		{Open: 103, High: 107, Low: 100, Close: 106, Volume: 1300, Timestamp: time.Now().Add(-2 * time.Minute)},
		{Open: 106, High: 110, Low: 104, Close: 108, Volume: 1400, Timestamp: time.Now().Add(-1 * time.Minute)},
	}

	values := calc.CalculateAll(candles)

	// Basic validation - values should be non-negative
	if values.ADX < 0 {
		t.Errorf("ADX should be non-negative, got %.2f", values.ADX)
	}

	if values.ATR14 < 0 {
		t.Errorf("ATR should be non-negative, got %.2f", values.ATR14)
	}

	if values.BBWidth < 0 {
		t.Errorf("BB Width should be non-negative, got %.2f", values.BBWidth)
	}

	if values.Volume24h <= 0 {
		t.Error("Volume should be positive")
	}
}

// Mock VF controller for testing
type mockVFController struct {
	positions []PositionStatus
}

func (m *mockVFController) UpdateWhitelist(symbols []string) error {
	return nil
}

func (m *mockVFController) GetActivePositions() ([]PositionStatus, error) {
	return m.positions, nil
}

func (m *mockVFController) TriggerEmergencyExit(reason string) error {
	return nil
}

func (m *mockVFController) TriggerForcePlacement() error {
	return nil
}
