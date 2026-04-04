package farming

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

func TestGridManager_DynamicCooldown(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()
	gm := &GridManager{
		logger:                logrus.NewEntry(logger),
		activeGrids:           make(map[string]*SymbolGrid),
		gridPlacementCooldown: 10 * time.Second,
		rateLimiter:           NewRateLimiter(10, 2, zapLogger),
	}

	grid := &SymbolGrid{
		Symbol:        "BTCUSD1",
		IsActive:      true,
		CurrentPrice:  50000,
		LastAttempt:   time.Now().Add(-15 * time.Second), // 15s ago > 10s cooldown
		OrdersPlaced:  false,
		PlacementBusy: false,
	}

	// No failures, should allow
	if !gm.shouldSchedulePlacement(grid, 49999) {
		t.Error("Expected to allow placement with no failures")
	}

	// Simulate failures
	gm.consecutiveFailures = 2
	gm.lastFailureTime = time.Now()

	// Should not allow due to increased cooldown
	if gm.shouldSchedulePlacement(grid, 49999) {
		t.Error("Expected to deny placement due to dynamic cooldown")
	}

	// Wait for cooldown
	time.Sleep(25 * time.Second) // 2x base cooldown + buffer

	if !gm.shouldSchedulePlacement(grid, 49999) {
		t.Error("Expected to allow after dynamic cooldown")
	}
}

func TestGridManager_ResetStaleOrders(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()
	gm := &GridManager{
		logger:      logrus.NewEntry(logger),
		activeGrids: make(map[string]*SymbolGrid),
		rateLimiter: NewRateLimiter(10, 2, zapLogger),
	}

	grid := &SymbolGrid{
		Symbol:       "BTCUSD1",
		IsActive:     true,
		CurrentPrice: 50000,
		LastAttempt:  time.Now().Add(-35 * time.Second), // 35s ago
		OrdersPlaced: true,
	}

	gm.activeGrids["BTCUSD1"] = grid

	gm.resetStaleOrders()

	if grid.OrdersPlaced {
		t.Error("Expected OrdersPlaced to be reset after timeout")
	}
}

func TestGridManager_FinishPlacement(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()
	gm := &GridManager{
		logger:      logrus.NewEntry(logger),
		activeGrids: make(map[string]*SymbolGrid),
		rateLimiter: NewRateLimiter(10, 2, zapLogger),
	}

	grid := &SymbolGrid{
		Symbol:        "BTCUSD1",
		IsActive:      true,
		CurrentPrice:  50000,
		OrdersPlaced:  false,
		PlacementBusy: true,
	}

	gm.activeGrids["BTCUSD1"] = grid

	// Test successful placement
	gm.finishPlacement("BTCUSD1", true)
	if !grid.OrdersPlaced || grid.PlacementBusy {
		t.Error("Expected OrdersPlaced=true and PlacementBusy=false on success")
	}
	if gm.consecutiveFailures != 0 {
		t.Error("Expected consecutiveFailures to reset on success")
	}

	// Reset for failure test
	grid.OrdersPlaced = false
	grid.PlacementBusy = true
	gm.consecutiveFailures = 0

	// Test failed placement
	gm.finishPlacement("BTCUSD1", false)
	if grid.OrdersPlaced || grid.PlacementBusy {
		t.Error("Expected OrdersPlaced=false and PlacementBusy=false on failure")
	}
	if gm.consecutiveFailures != 1 {
		t.Error("Expected consecutiveFailures to increment on failure")
	}
}
