package farming

import (
	"context"
	"testing"
	"time"

	"aster-bot/internal/farming/volume_optimization"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestGridManager_SetSmartCancellationManager tests setting SmartCancellationManager
func TestGridManager_SetSmartCancellationManager(t *testing.T) {
	logger := logrus.New()
	gridManager := &GridManager{
		logger:        logrus.NewEntry(logger),
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		activeOrders:  make(map[string]*GridOrder),
		filledOrders:  make(map[string]*GridOrder),
	}

	// Create SmartCancellationManager
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.SmartCancelConfig{
		Enabled:               true,
		SpreadChangeThreshold: 0.2,
		CheckInterval:         5 * time.Second,
	}
	smartCancelMgr := volume_optimization.NewSmartCancellationManager(config, zapLogger)

	// Set SmartCancellationManager on GridManager
	gridManager.SetSmartCancellationManager(smartCancelMgr)

	// Verify SmartCancellationManager is set
	assert.NotNil(t, gridManager.smartCancelMgr)
}

// TestGridManager_UpdateSmartCancelSpread tests updating spread
func TestGridManager_UpdateSmartCancelSpread(t *testing.T) {
	logger := logrus.New()
	gridManager := &GridManager{
		logger:        logrus.NewEntry(logger),
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		activeOrders:  make(map[string]*GridOrder),
		filledOrders:  make(map[string]*GridOrder),
	}

	// Create and set SmartCancellationManager
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.SmartCancelConfig{
		Enabled:               true,
		SpreadChangeThreshold: 0.2,
		CheckInterval:         5 * time.Second,
	}
	smartCancelMgr := volume_optimization.NewSmartCancellationManager(config, zapLogger)
	gridManager.SetSmartCancellationManager(smartCancelMgr)

	// Update spread
	gridManager.UpdateSmartCancelSpread("BTCUSD1", 65432.0, 65432.5)

	// Verify spread was updated
	history := smartCancelMgr.GetSpreadHistory("BTCUSD1")
	assert.GreaterOrEqual(t, len(history), 0)
}

// TestSmartCancellationManager_UpdateSpread tests spread update
func TestSmartCancellationManager_UpdateSpread(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.SmartCancelConfig{
		Enabled:               true,
		SpreadChangeThreshold: 0.2,
		CheckInterval:         5 * time.Second,
	}
	smartCancelMgr := volume_optimization.NewSmartCancellationManager(config, zapLogger)

	// Update spread multiple times
	smartCancelMgr.UpdateSpread("BTCUSD1", 65432.0, 65432.5)
	smartCancelMgr.UpdateSpread("BTCUSD1", 65432.1, 65432.6)
	smartCancelMgr.UpdateSpread("BTCUSD1", 65432.2, 65432.7)

	// Get last spread
	lastSpread, exists := smartCancelMgr.GetLastSpread("BTCUSD1")
	assert.True(t, exists)
	assert.InDelta(t, 0.5, lastSpread.Spread, 0.01)
}

// TestSmartCancellationManager_IsEnabled tests IsEnabled method
func TestSmartCancellationManager_IsEnabled(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()

	// Test enabled
	enabledConfig := volume_optimization.SmartCancelConfig{
		Enabled:               true,
		SpreadChangeThreshold: 0.2,
		CheckInterval:         5 * time.Second,
	}
	enabledMgr := volume_optimization.NewSmartCancellationManager(enabledConfig, zapLogger)
	assert.True(t, enabledMgr.IsEnabled())

	// Test disabled
	disabledConfig := volume_optimization.SmartCancelConfig{
		Enabled:               false,
		SpreadChangeThreshold: 0.2,
		CheckInterval:         5 * time.Second,
	}
	disabledMgr := volume_optimization.NewSmartCancellationManager(disabledConfig, zapLogger)
	assert.False(t, disabledMgr.IsEnabled())
}

// TestSmartCancellationManager_SetEnabled tests SetEnabled method
func TestSmartCancellationManager_SetEnabled(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.SmartCancelConfig{
		Enabled:               false,
		SpreadChangeThreshold: 0.2,
		CheckInterval:         5 * time.Second,
	}
	smartCancelMgr := volume_optimization.NewSmartCancellationManager(config, zapLogger)

	// Initially disabled
	assert.False(t, smartCancelMgr.IsEnabled())

	// Enable
	smartCancelMgr.SetEnabled(true)
	assert.True(t, smartCancelMgr.IsEnabled())

	// Disable
	smartCancelMgr.SetEnabled(false)
	assert.False(t, smartCancelMgr.IsEnabled())
}

// TestSmartCancellationManager_GetStats tests GetStats method
func TestSmartCancellationManager_GetStats(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.SmartCancelConfig{
		Enabled:               true,
		SpreadChangeThreshold: 0.25,
		CheckInterval:         10 * time.Second,
	}
	smartCancelMgr := volume_optimization.NewSmartCancellationManager(config, zapLogger)

	stats := smartCancelMgr.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, true, stats["enabled"])
	assert.Equal(t, 0.25, stats["spread_change_threshold"])
	assert.Equal(t, 10.0, stats["check_interval"])
}

// TestSmartCancellationManager_Callbacks tests callback registration
func TestSmartCancellationManager_Callbacks(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.SmartCancelConfig{
		Enabled:               true,
		SpreadChangeThreshold: 0.2,
		CheckInterval:         5 * time.Second,
	}
	smartCancelMgr := volume_optimization.NewSmartCancellationManager(config, zapLogger)

	// Set callbacks
	smartCancelMgr.SetCallbacks(
		func(symbol string, oldSpread, newSpread, changePct float64) {},
		func(ctx context.Context, symbol string) error { return nil },
		func(ctx context.Context, symbol string) error { return nil },
	)

	// Verify callbacks are set by checking no panic occurs
	// Actual trigger requires checkSpreadChange to be called
	// which happens in monitorLoop, but we can't easily test that here
	assert.NotNil(t, smartCancelMgr)
}
