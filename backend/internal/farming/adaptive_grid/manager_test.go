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

// =============================================================================
// FUNDING RATE AWARENESS TESTS
// =============================================================================

// TestFundingRateMonitor_GetFundingBias tests funding bias calculation
func TestFundingRateMonitor_GetFundingBias(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultFundingProtectionConfig()
	monitor := NewFundingRateMonitor(config, nil, logger)

	tests := []struct {
		name         string
		rate         float64
		wantSide     string
		wantStrength float64
		wantSkip     bool
	}{
		{
			name:         "Zero funding - no bias",
			rate:         0,
			wantSide:     "",
			wantStrength: 0,
			wantSkip:     false,
		},
		{
			name:         "Low positive funding - no bias",
			rate:         0.0005, // 0.05%
			wantSide:     "",
			wantStrength: 0,
			wantSkip:     false,
		},
		{
			name:         "High positive funding - bias SHORT",
			rate:         0.002, // 0.2%
			wantSide:     "SHORT",
			wantStrength: 0.7,
			wantSkip:     false,
		},
		{
			name:         "High negative funding - bias LONG",
			rate:         -0.002, // -0.2%
			wantSide:     "LONG",
			wantStrength: 0.7,
			wantSkip:     false,
		},
		{
			name:         "Extreme positive funding - skip",
			rate:         0.015, // 1.5%
			wantSide:     "",
			wantStrength: 0,
			wantSkip:     true,
		},
		{
			name:         "Extreme negative funding - skip",
			rate:         -0.015, // -1.5%
			wantSide:     "",
			wantStrength: 0,
			wantSkip:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the rate directly
			monitor.mu.Lock()
			monitor.rates["BTCUSDT"] = &FundingRateInfo{
				Symbol: "BTCUSDT",
				Rate:   tt.rate,
			}
			monitor.mu.Unlock()

			side, strength, shouldSkip := monitor.GetFundingBias("BTCUSDT")
			assert.Equal(t, tt.wantSide, side, "Bias side mismatch")
			assert.Equal(t, tt.wantStrength, strength, "Bias strength mismatch")
			assert.Equal(t, tt.wantSkip, shouldSkip, "Skip decision mismatch")
		})
	}
}

// TestInventoryManager_GetAdjustedOrderSizeWithFunding tests size adjustment with funding bias
func TestInventoryManager_GetAdjustedOrderSizeWithFunding(t *testing.T) {
	logger := zap.NewNop()
	config := &InventoryConfig{MaxInventoryPct: 0.30}
	im := NewInventoryManager(config, logger)

	// Set funding bias: SHORT side favored with 70% strength
	im.SetBias("BTCUSDT", "SHORT", 0.7)

	// Base size $50, trying to open LONG (against bias) should be reduced
	baseSize := 50.0
	adjustedSize := im.GetAdjustedOrderSizeWithFunding("BTCUSDT", "LONG", baseSize)

	// LONG against SHORT bias: size = 50 * (1 - 0.7) = 15
	expected := baseSize * (1 - 0.7)
	assert.InDelta(t, expected, adjustedSize, 0.01, "LONG size should be reduced against SHORT bias")

	// SHORT aligned with bias: size should stay same (or close to it)
	adjustedSizeShort := im.GetAdjustedOrderSizeWithFunding("BTCUSDT", "SHORT", baseSize)
	assert.InDelta(t, baseSize, adjustedSizeShort, 0.01, "SHORT size should not be reduced when aligned with bias")
}

// =============================================================================
// DYNAMIC SPREAD ADJUSTMENT TESTS
// =============================================================================

// TestDynamicSpreadCalculator_CalculateSpread tests ATR-based spread calculation
func TestDynamicSpreadCalculator_CalculateSpread(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultDynamicSpreadConfig()
	calc := NewDynamicSpreadCalculator(config, logger)

	// Test initial state - should return base spread
	spread := calc.GetDynamicSpread()
	assert.Equal(t, config.BaseSpreadPct, spread, "Initial spread should be base spread")

	// Simulate low volatility (ATR < 0.3%)
	// Add price data with small movements
	basePrice := 50000.0
	for i := 0; i < 10; i++ {
		high := basePrice * (1 + 0.001) // 0.1% up
		low := basePrice * (1 - 0.001)  // 0.1% down
		close := basePrice
		calc.UpdateATR(high, low, close)
	}

	// After low vol data, multiplier should be 0.8
	assert.Equal(t, VolatilityLow, calc.GetVolatilityLevel(), "Should detect low volatility")
	assert.InDelta(t, 0.8, calc.GetMultiplier(), 0.01, "Low vol multiplier should be 0.8")

	// Simulate high volatility (ATR > 0.8%)
	for i := 0; i < 10; i++ {
		high := basePrice * (1 + 0.01) // 1% up
		low := basePrice * (1 - 0.01)  // 1% down
		close := basePrice
		calc.UpdateATR(high, low, close)
	}

	// After high vol data, should detect high volatility
	level := calc.GetVolatilityLevel()
	assert.True(t, level == VolatilityHigh || level == VolatilityExtreme,
		"Should detect high or extreme volatility, got %v", level)
}

// TestAdaptiveGridManager_CalculateDynamicSpread tests spread calculation with fallback
func TestAdaptiveGridManager_CalculateDynamicSpread(t *testing.T) {
	logger := zap.NewNop()

	// Test without dynamic spread calc (fallback to config)
	manager := &AdaptiveGridManager{
		logger: logger,
		riskConfig: &RiskConfig{
			BaseGridSpreadPct: 0.0015, // 0.15%
		},
		dynamicSpreadCalc: nil,
	}

	spread := manager.CalculateDynamicSpread("BTCUSDT")
	assert.Equal(t, 0.0015, spread, "Should fallback to BaseGridSpreadPct when calc is nil")
}

// =============================================================================
// SMART POSITION SIZING TESTS (KELLY CRITERION)
// =============================================================================

// TestCalculateSmartSize_KellyCriterion tests Kelly-based sizing
func TestCalculateSmartSize_KellyCriterion(t *testing.T) {
	config := &SmartSizingConfig{
		Enabled:              true,
		KellyFraction:        0.25, // Conservative 25%
		ConsecutiveLossDecay: 0.8,  // 20% reduction per loss
		MinSize:              5.0,
		MaxSize:              100.0,
	}

	baseSize := 50.0

	// Test 1: 50% win rate, 0 consecutive losses
	// Kelly = (0.5*1.5 - 0.5)/1.5 = 0.1667, * 0.25 = 0.0417
	// size = 50 * 0.0417 = 2.08, clamped to min 5
	size := CalculateSmartSize(baseSize, 0.5, 0, config)
	assert.InDelta(t, 5.0, size, 0.5, "With 50% win rate, should be near minimum")

	// Test 2: 60% win rate, 0 consecutive losses
	// Kelly = (0.6*1.5 - 0.4)/1.5 = 0.333, * 0.25 = 0.083
	// size = 50 * 0.083 = 4.15, clamped to 5
	size = CalculateSmartSize(baseSize, 0.6, 0, config)
	assert.InDelta(t, 5.0, size, 0.5, "With 60% win rate, should be at or above minimum")

	// Test 3: 70% win rate, 0 consecutive losses
	// Kelly = (0.7*1.5 - 0.3)/1.5 = 0.5, * 0.25 = 0.125
	// size = 50 * 0.125 = 6.25
	size = CalculateSmartSize(baseSize, 0.7, 0, config)
	assert.InDelta(t, 6.25, size, 0.5, "With 70% win rate, should calculate correctly")

	// Test 4: 70% win rate, 2 consecutive losses with decay
	// Base from test 3: 6.25, decay = 0.8^2 = 0.64, final = 4.0, clamped to 5
	size = CalculateSmartSize(baseSize, 0.7, 2, config)
	assert.InDelta(t, 5.0, size, 0.5, "After 2 consecutive losses, should be reduced and clamped")

	// Test 5: 80% win rate, 0 consecutive losses
	// Kelly = (0.8*1.5 - 0.2)/1.5 = 0.667, * 0.25 = 0.167
	// size = 50 * 0.167 = 8.33
	size = CalculateSmartSize(baseSize, 0.8, 0, config)
	assert.InDelta(t, 8.33, size, 0.5, "With 80% win rate, should be higher")
}

// TestTradeTracker_GetConsecutiveLosses tests consecutive loss counting
func TestTradeTracker_GetConsecutiveLosses(t *testing.T) {
	tracker := NewTradeTracker(24)

	// No trades yet
	assert.Equal(t, 0, tracker.GetConsecutiveLosses(), "Should be 0 with no trades")

	// Add wins
	tracker.RecordTrade("BTC", 1.0)
	tracker.RecordTrade("ETH", 2.0)
	assert.Equal(t, 0, tracker.GetConsecutiveLosses(), "Should be 0 after wins")

	// Add losses
	tracker.RecordTrade("BTC", -1.0)
	tracker.RecordTrade("ETH", -2.0)
	assert.Equal(t, 2, tracker.GetConsecutiveLosses(), "Should count consecutive losses")

	// Add another loss
	tracker.RecordTrade("BTC", -0.5)
	assert.Equal(t, 3, tracker.GetConsecutiveLosses(), "Should count 3 consecutive losses")

	// Add a win - resets counter
	tracker.RecordTrade("BTC", 1.0)
	assert.Equal(t, 0, tracker.GetConsecutiveLosses(), "Should reset after win")
}

// TestTradeTracker_GetWinRate tests win rate calculation
func TestTradeTracker_GetWinRate(t *testing.T) {
	tracker := NewTradeTracker(24)

	// Default when no trades
	assert.Equal(t, 0.5, tracker.GetWinRate(), "Should return 0.5 default with no trades")

	// 3 wins, 1 loss = 75% win rate
	tracker.RecordTrade("BTC", 1.0)
	tracker.RecordTrade("BTC", 1.0)
	tracker.RecordTrade("BTC", -1.0)
	tracker.RecordTrade("BTC", 1.0)

	assert.InDelta(t, 0.75, tracker.GetWinRate(), 0.01, "Win rate should be 75%")
}

// =============================================================================
// PARTIAL CLOSE STRATEGY TESTS
// =============================================================================

// TestPartialCloseConfig_Defaults tests default configuration values
func TestPartialCloseConfig_Defaults(t *testing.T) {
	config := DefaultPartialCloseConfig()

	assert.True(t, config.Enabled, "Should be enabled by default")
	assert.Equal(t, 0.30, config.TP1_ClosePct, "TP1 should close 30%")
	assert.Equal(t, 0.005, config.TP1_ProfitPct, "TP1 at 0.5% profit")
	assert.Equal(t, 0.40, config.TP2_ClosePct, "TP2 should close 40%")
	assert.Equal(t, 0.01, config.TP2_ProfitPct, "TP2 at 1.0% profit")
	assert.Equal(t, 0.30, config.TP3_ClosePct, "TP3 should close 30%")
	assert.Equal(t, 0.015, config.TP3_ProfitPct, "TP3 at 1.5% profit")
	assert.True(t, config.TrailingAfterTP2, "Should enable trailing after TP2")
	assert.Equal(t, 0.005, config.TrailingDistance, "Trailing distance 0.5%")
}

// TestCalculateSmartSize_Disabled tests disabled smart sizing
func TestCalculateSmartSize_Disabled(t *testing.T) {
	config := &SmartSizingConfig{
		Enabled: false,
	}

	baseSize := 50.0
	size := CalculateSmartSize(baseSize, 0.5, 5, config)
	assert.Equal(t, baseSize, size, "When disabled, should return base size unchanged")
}
