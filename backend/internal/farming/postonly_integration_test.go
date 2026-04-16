package farming

import (
	"testing"

	"aster-bot/internal/farming/volume_optimization"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestGridManager_SetPostOnlyHandler tests that PostOnlyHandler can be set on GridManager
func TestGridManager_SetPostOnlyHandler(t *testing.T) {
	// Create a minimal GridManager for testing
	logger := logrus.New()
	gridManager := &GridManager{
		logger:        logrus.NewEntry(logger),
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		activeOrders:  make(map[string]*GridOrder),
		filledOrders:  make(map[string]*GridOrder),
	}

	// Create PostOnlyHandler
	zapLogger, _ := zap.NewDevelopment()
	postOnlyConfig := volume_optimization.PostOnlyConfig{
		Enabled:    true,
		Fallback:   true,
		MaxRetries: 3,
	}
	postOnlyHandler := volume_optimization.NewPostOnlyHandler(postOnlyConfig, zapLogger)

	// Set PostOnlyHandler on GridManager
	gridManager.SetPostOnlyHandler(postOnlyHandler)

	// Verify PostOnlyHandler is set
	assert.NotNil(t, gridManager.postOnlyHandler)
}

// TestGridManager_SetPostOnlyHandler_Nil tests setting nil PostOnlyHandler
func TestGridManager_SetPostOnlyHandler_Nil(t *testing.T) {
	logger := logrus.New()
	gridManager := &GridManager{
		logger:        logrus.NewEntry(logger),
		activeGrids:   make(map[string]*SymbolGrid),
		pendingOrders: make(map[string]*GridOrder),
		activeOrders:  make(map[string]*GridOrder),
		filledOrders:  make(map[string]*GridOrder),
	}

	// Setting nil should not panic
	gridManager.SetPostOnlyHandler(nil)
	assert.Nil(t, gridManager.postOnlyHandler)
}

// TestPostOnlyHandler_IsEnabled tests IsEnabled method
func TestPostOnlyHandler_IsEnabled(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()

	// Test enabled handler
	enabledConfig := volume_optimization.PostOnlyConfig{
		Enabled:    true,
		Fallback:   true,
		MaxRetries: 3,
	}
	enabledHandler := volume_optimization.NewPostOnlyHandler(enabledConfig, zapLogger)
	assert.True(t, enabledHandler.IsEnabled())

	// Test disabled handler
	disabledConfig := volume_optimization.PostOnlyConfig{
		Enabled:    false,
		Fallback:   true,
		MaxRetries: 3,
	}
	disabledHandler := volume_optimization.NewPostOnlyHandler(disabledConfig, zapLogger)
	assert.False(t, disabledHandler.IsEnabled())
}

// TestPostOnlyHandler_SetEnabled tests SetEnabled method
func TestPostOnlyHandler_SetEnabled(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.PostOnlyConfig{
		Enabled:    false,
		Fallback:   true,
		MaxRetries: 3,
	}
	handler := volume_optimization.NewPostOnlyHandler(config, zapLogger)

	// Initially disabled
	assert.False(t, handler.IsEnabled())

	// Enable
	handler.SetEnabled(true)
	assert.True(t, handler.IsEnabled())

	// Disable
	handler.SetEnabled(false)
	assert.False(t, handler.IsEnabled())
}

// TestPostOnlyHandler_GetStats tests GetStats method
func TestPostOnlyHandler_GetStats(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	config := volume_optimization.PostOnlyConfig{
		Enabled:    true,
		Fallback:   true,
		MaxRetries: 5,
	}
	handler := volume_optimization.NewPostOnlyHandler(config, zapLogger)

	stats := handler.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, true, stats["enabled"])
	assert.Equal(t, true, stats["fallback"])
	assert.Equal(t, 5, stats["max_retries"])
}
