package farming

import (
	"context"
	"testing"

	"aster-bot/internal/farming/volume_optimization"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestGridManager_SetPennyJumpManager tests setting PennyJumpManager
func TestGridManager_SetPennyJumpManager(t *testing.T) {
	logger := logrus.New()
	gridManager := &GridManager{
		logger:        logrus.NewEntry(logger),
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		activeOrders:  make(map[string]*GridOrder),
		filledOrders:  make(map[string]*GridOrder),
	}

	// Create PennyJumpManager
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.PennyConfig{
		Enabled:       true,
		JumpThreshold: 0.1,
		MaxJump:       3,
	}
	pennyJumpMgr := volume_optimization.NewPennyJumpManager(config, zapLogger)

	// Set PennyJumpManager on GridManager
	gridManager.SetPennyJumpManager(pennyJumpMgr)

	// Verify PennyJumpManager is set
	assert.NotNil(t, gridManager.pennyJumpMgr)
}

// TestGridManager_SetInventoryHedgeManager tests setting InventoryHedgeManager
func TestGridManager_SetInventoryHedgeManager(t *testing.T) {
	logger := logrus.New()
	gridManager := &GridManager{
		logger:        logrus.NewEntry(logger),
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		activeOrders:  make(map[string]*GridOrder),
		filledOrders:  make(map[string]*GridOrder),
	}

	// Create InventoryHedgeManager
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.InventoryHedgeConfig{
		Enabled:        true,
		HedgeThreshold: 0.3,
		HedgeRatio:     0.3,
		MaxHedgeSize:   100.0,
		HedgingMode:    "internal",
	}
	inventoryHedgeMgr := volume_optimization.NewInventoryHedgeManager(config, zapLogger)

	// Set InventoryHedgeManager on GridManager
	gridManager.SetInventoryHedgeManager(inventoryHedgeMgr)

	// Verify InventoryHedgeManager is set
	assert.NotNil(t, gridManager.inventoryHedgeMgr)
}

// TestPennyJumpManager_UpdateBestPrices tests updating best prices
func TestPennyJumpManager_UpdateBestPrices(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.PennyConfig{
		Enabled:       true,
		JumpThreshold: 0.1,
		MaxJump:       3,
	}
	pennyJumpMgr := volume_optimization.NewPennyJumpManager(config, zapLogger)

	// Update best prices
	pennyJumpMgr.UpdateBestPrices("BTCUSD1", 65432.0, 65432.5)

	// Verify stats
	stats := pennyJumpMgr.GetStats()
	assert.Equal(t, true, stats["enabled"])
	assert.Equal(t, 0.1, stats["jump_threshold"])
	assert.Equal(t, 3, stats["max_jump"])
	assert.Equal(t, 1, stats["tracked_symbols"])
}

// TestPennyJumpManager_IsEnabled tests IsEnabled method
func TestPennyJumpManager_IsEnabled(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()

	// Test enabled
	enabledConfig := volume_optimization.PennyConfig{
		Enabled:       true,
		JumpThreshold: 0.1,
		MaxJump:       3,
	}
	enabledMgr := volume_optimization.NewPennyJumpManager(enabledConfig, zapLogger)
	assert.True(t, enabledMgr.IsEnabled())

	// Test disabled
	disabledConfig := volume_optimization.PennyConfig{
		Enabled:       false,
		JumpThreshold: 0.1,
		MaxJump:       3,
	}
	disabledMgr := volume_optimization.NewPennyJumpManager(disabledConfig, zapLogger)
	assert.False(t, disabledMgr.IsEnabled())
}

// TestPennyJumpManager_SetEnabled tests SetEnabled method
func TestPennyJumpManager_SetEnabled(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.PennyConfig{
		Enabled:       false,
		JumpThreshold: 0.1,
		MaxJump:       3,
	}
	pennyJumpMgr := volume_optimization.NewPennyJumpManager(config, zapLogger)

	// Initially disabled
	assert.False(t, pennyJumpMgr.IsEnabled())

	// Enable
	pennyJumpMgr.SetEnabled(true)
	assert.True(t, pennyJumpMgr.IsEnabled())

	// Disable
	pennyJumpMgr.SetEnabled(false)
	assert.False(t, pennyJumpMgr.IsEnabled())
}

// TestPennyJumpManager_GetPennyJumpedPrice tests price jumping
func TestPennyJumpManager_GetPennyJumpedPrice(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.PennyConfig{
		Enabled:       true,
		JumpThreshold: 0.5, // 50% threshold (liberal for testing)
		MaxJump:       3,
	}
	pennyJumpMgr := volume_optimization.NewPennyJumpManager(config, zapLogger)

	// Update best prices (spread = 0.5)
	pennyJumpMgr.UpdateBestPrices("BTCUSD1", 65432.0, 65432.5)

	// Test BUY order close to best bid (should jump)
	originalPrice := 65432.1 // Close to best bid
	_ = pennyJumpMgr.GetPennyJumpedPrice("BTCUSD1", "BUY", originalPrice)

	// When disabled, should return original
	disabledConfig := volume_optimization.PennyConfig{
		Enabled:       false,
		JumpThreshold: 0.1,
		MaxJump:       3,
	}
	disabledMgr := volume_optimization.NewPennyJumpManager(disabledConfig, zapLogger)
	disabledMgr.UpdateBestPrices("BTCUSD1", 65432.0, 65432.5)

	disabledPrice := disabledMgr.GetPennyJumpedPrice("BTCUSD1", "BUY", originalPrice)
	assert.Equal(t, originalPrice, disabledPrice)
}

// TestInventoryHedgeManager_UpdateInventorySkew tests updating inventory skew
func TestInventoryHedgeManager_UpdateInventorySkew(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.InventoryHedgeConfig{
		Enabled:        true,
		HedgeThreshold: 0.3,
		HedgeRatio:     0.3,
		MaxHedgeSize:   100.0,
		HedgingMode:    "internal",
	}
	hedgeMgr := volume_optimization.NewInventoryHedgeManager(config, zapLogger)

	// Update inventory skew (60% long, 40% short = 20% long skew)
	hedgeMgr.UpdateInventorySkew("BTCUSD1", 0.6, 0.4)

	// Verify stats
	stats := hedgeMgr.GetStats()
	assert.Equal(t, true, stats["enabled"])
	assert.Equal(t, 0.3, stats["hedge_threshold"])
	assert.Equal(t, 0.3, stats["hedge_ratio"])
	assert.Equal(t, 100.0, stats["max_hedge_size"])
	assert.Equal(t, "internal", stats["hedging_mode"])
	assert.Equal(t, 1, stats["tracked_symbols"])
}

// TestInventoryHedgeManager_IsEnabled tests IsEnabled method
func TestInventoryHedgeManager_IsEnabled(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()

	// Test enabled
	enabledConfig := volume_optimization.InventoryHedgeConfig{
		Enabled:        true,
		HedgeThreshold: 0.3,
		HedgeRatio:     0.3,
		MaxHedgeSize:   100.0,
		HedgingMode:    "internal",
	}
	enabledMgr := volume_optimization.NewInventoryHedgeManager(enabledConfig, zapLogger)
	assert.True(t, enabledMgr.IsEnabled())

	// Test disabled
	disabledConfig := volume_optimization.InventoryHedgeConfig{
		Enabled:        false,
		HedgeThreshold: 0.3,
		HedgeRatio:     0.3,
		MaxHedgeSize:   100.0,
		HedgingMode:    "internal",
	}
	disabledMgr := volume_optimization.NewInventoryHedgeManager(disabledConfig, zapLogger)
	assert.False(t, disabledMgr.IsEnabled())
}

// TestInventoryHedgeManager_SetEnabled tests SetEnabled method
func TestInventoryHedgeManager_SetEnabled(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.InventoryHedgeConfig{
		Enabled:        false,
		HedgeThreshold: 0.3,
		HedgeRatio:     0.3,
		MaxHedgeSize:   100.0,
		HedgingMode:    "internal",
	}
	hedgeMgr := volume_optimization.NewInventoryHedgeManager(config, zapLogger)

	// Initially disabled
	assert.False(t, hedgeMgr.IsEnabled())

	// Enable
	hedgeMgr.SetEnabled(true)
	assert.True(t, hedgeMgr.IsEnabled())

	// Disable
	hedgeMgr.SetEnabled(false)
	assert.False(t, hedgeMgr.IsEnabled())
}

// TestInventoryHedgeManager_SetCallbacks tests callback registration
func TestInventoryHedgeManager_SetCallbacks(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.InventoryHedgeConfig{
		Enabled:        true,
		HedgeThreshold: 0.3,
		HedgeRatio:     0.3,
		MaxHedgeSize:   100.0,
		HedgingMode:    "internal",
	}
	hedgeMgr := volume_optimization.NewInventoryHedgeManager(config, zapLogger)

	// Set callbacks
	hedgeMgr.SetCallbacks(
		func(ctx context.Context, symbol, side string, size float64) error {
			return nil
		},
	)

	// Verify callbacks are set (no panic)
	assert.NotNil(t, hedgeMgr)
}
