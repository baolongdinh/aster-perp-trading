package farming

import (
	"testing"

	"aster-bot/internal/farming/volume_optimization"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestGridManager_SetTickSizeManager tests that TickSizeManager can be set on GridManager
func TestGridManager_SetTickSizeManager(t *testing.T) {
	// Create a minimal GridManager for testing
	logger := logrus.New()
	gridManager := &GridManager{
		logger:        logrus.NewEntry(logger),
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		activeOrders:  make(map[string]*GridOrder),
		filledOrders:  make(map[string]*GridOrder),
	}

	// Create TickSizeManager
	zapLogger, _ := zap.NewDevelopment()
	tickSizeMgr := volume_optimization.NewTickSizeManager(zapLogger)

	// Set tick sizes
	tickSizeMgr.SetTickSize("BTCUSD1", 0.1)
	tickSizeMgr.SetTickSize("ETHUSD1", 0.01)

	// Set TickSizeManager on GridManager
	gridManager.SetTickSizeManager(tickSizeMgr)

	// Verify TickSizeManager is set
	assert.NotNil(t, gridManager.tickSizeMgr)
}

// TestTickSizeManager_RoundToTickForSymbol tests price rounding for different symbols
func TestTickSizeManager_RoundToTickForSymbol(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	tickSizeMgr := volume_optimization.NewTickSizeManager(zapLogger)

	// Set tick sizes
	tickSizeMgr.SetTickSize("BTCUSD1", 0.1)
	tickSizeMgr.SetTickSize("ETHUSD1", 0.01)

	// Test BTC rounding (tick size 0.1)
	btcPrice := 65432.17
	roundedBTC := tickSizeMgr.RoundToTickForSymbol("BTCUSD1", btcPrice)
	assert.InDelta(t, 65432.2, roundedBTC, 0.0001)

	// Test ETH rounding (tick size 0.01)
	ethPrice := 3456.789
	roundedETH := tickSizeMgr.RoundToTickForSymbol("ETHUSD1", ethPrice)
	assert.InDelta(t, 3456.79, roundedETH, 0.0001)

	// Test unknown symbol (should use default 0.01)
	unknownPrice := 123.456
	roundedUnknown := tickSizeMgr.RoundToTickForSymbol("UNKNOWN", unknownPrice)
	assert.InDelta(t, 123.46, roundedUnknown, 0.0001)
}

// TestTickSizeManager_GetTickSize tests getting tick sizes
func TestTickSizeManager_GetTickSize(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	tickSizeMgr := volume_optimization.NewTickSizeManager(zapLogger)

	// Set tick size
	tickSizeMgr.SetTickSize("BTCUSD1", 0.1)

	// Get existing tick size
	tickSize := tickSizeMgr.GetTickSize("BTCUSD1")
	assert.Equal(t, 0.1, tickSize)

	// Get default tick size for unknown symbol
	defaultTickSize := tickSizeMgr.GetTickSize("UNKNOWN")
	assert.Equal(t, 0.01, defaultTickSize)
}

// TestTickSizeManager_RoundToTick tests the core rounding logic
func TestTickSizeManager_RoundToTick(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	tickSizeMgr := volume_optimization.NewTickSizeManager(zapLogger)

	// Test various rounding scenarios
	tests := []struct {
		price    float64
		tickSize float64
		expected float64
	}{
		{65432.17, 0.1, 65432.2},
		{65432.12, 0.1, 65432.1},
		{3456.789, 0.01, 3456.79},
		{3456.781, 0.01, 3456.78},
		{123.4567, 0.001, 123.457},
		{123.4562, 0.001, 123.456},
		{100.0, 0.1, 100.0},
		{0.0, 0.1, 0.0},
	}

	for _, tt := range tests {
		result := tickSizeMgr.RoundToTick(tt.price, tt.tickSize)
		assert.InDelta(t, tt.expected, result, 0.0001)
	}
}

// TestTickSizeManager_ZeroTickSize tests handling of zero tick size
func TestTickSizeManager_ZeroTickSize(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	tickSizeMgr := volume_optimization.NewTickSizeManager(zapLogger)

	// Zero tick size should return original price
	price := 123.45
	result := tickSizeMgr.RoundToTick(price, 0)
	assert.Equal(t, price, result)
}

// TestGridManager_SetTickSizeManager_Nil tests setting nil TickSizeManager
func TestGridManager_SetTickSizeManager_Nil(t *testing.T) {
	logger := logrus.New()
	gridManager := &GridManager{
		logger:        logrus.NewEntry(logger),
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		activeOrders:  make(map[string]*GridOrder),
		filledOrders:  make(map[string]*GridOrder),
	}

	// Setting nil should not panic
	gridManager.SetTickSizeManager(nil)
	assert.Nil(t, gridManager.tickSizeMgr)
}
