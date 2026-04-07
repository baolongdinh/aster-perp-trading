package adaptive_grid

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

// mockGridManager is a mock implementation for testing
type mockGridManager struct {
	cancelCalled  bool
	clearCalled   bool
	rebuildCalled bool
	lastSymbol    string
}

func (m *mockGridManager) Start(ctx context.Context) error { return nil }
func (m *mockGridManager) Stop(ctx context.Context) error  { return nil }
func (m *mockGridManager) GetActivePositions(symbol string) ([]interface{}, error) {
	return nil, nil
}
func (m *mockGridManager) CancelAllOrders(ctx context.Context, symbol string) error {
	m.cancelCalled = true
	m.lastSymbol = symbol
	return nil
}
func (m *mockGridManager) ClearGrid(ctx context.Context, symbol string) error {
	m.clearCalled = true
	return nil
}
func (m *mockGridManager) RebuildGrid(ctx context.Context, symbol string) error {
	m.rebuildCalled = true
	return nil
}
func (m *mockGridManager) SetOrderSize(size float64)      {}
func (m *mockGridManager) SetGridSpread(spread float64)   {}
func (m *mockGridManager) SetMaxOrdersPerSide(max int)    {}
func (m *mockGridManager) SetPositionTimeout(minutes int) {}

func TestNewSlotTransitionHandler(t *testing.T) {
	logger := zap.NewNop()
	mockGrid := &mockGridManager{}
	mockAdaptive := &AdaptiveGridManager{}

	handler := NewSlotTransitionHandler(mockGrid, mockAdaptive, logger)

	if handler == nil {
		t.Fatal("Expected SlotTransitionHandler to be created")
	}

	if handler.cooldownPeriod != 2*time.Minute {
		t.Errorf("Expected default cooldown of 2 minutes, got %v", handler.cooldownPeriod)
	}
}

func TestSlotTransitionHandler_SetCooldownPeriod(t *testing.T) {
	logger := zap.NewNop()
	handler := NewSlotTransitionHandler(nil, nil, logger)

	newPeriod := 5 * time.Minute
	handler.SetCooldownPeriod(newPeriod)

	if handler.cooldownPeriod != newPeriod {
		t.Errorf("Expected cooldown %v, got %v", newPeriod, handler.cooldownPeriod)
	}
}

func TestSlotTransitionHandler_CanTransition(t *testing.T) {
	logger := zap.NewNop()
	handler := NewSlotTransitionHandler(nil, nil, logger)

	symbol := "BTCUSDT"

	// Should allow first transition
	if !handler.CanTransition(symbol) {
		t.Error("Expected CanTransition to return true for first transition")
	}

	// Update transition time
	handler.updateTransitionTime(symbol)

	// Should block during cooldown
	if handler.CanTransition(symbol) {
		t.Error("Expected CanTransition to return false during cooldown")
	}
}

func TestSlotTransitionHandler_HandleTransition_DisabledSlot(t *testing.T) {
	logger := zap.NewNop()
	mockGrid := &mockGridManager{}
	mockAdaptive := NewAdaptiveGridManager(nil, nil, nil, nil, logger)
	// Initialize required maps
	mockAdaptive.tradingPaused = make(map[string]bool)

	handler := NewSlotTransitionHandler(mockGrid, mockAdaptive, logger)
	handler.SetCooldownPeriod(0) // Disable cooldown for testing

	ctx := context.Background()
	symbol := "BTCUSDT"

	// Create a disabled slot
	disabledSlot := &TimeSlotConfig{
		Window: TimeWindow{
			Start:    "22:00",
			End:      "02:00",
			Timezone: "Asia/Ho_Chi_Minh",
		},
		Enabled:          false,
		SizeMultiplier:   0.0,
		MaxExposurePct:   0.0,
		SpreadMultiplier: 1.0,
		Description:      "Overnight (Disabled)",
	}

	// Test transition to disabled slot
	err := handler.HandleTransition(ctx, symbol, nil, disabledSlot)
	if err != nil {
		t.Errorf("HandleTransition returned error: %v", err)
	}

	if !mockGrid.cancelCalled {
		t.Error("Expected CancelAllOrders to be called for disabled slot")
	}

	if mockGrid.lastSymbol != symbol {
		t.Errorf("Expected symbol %s, got %s", symbol, mockGrid.lastSymbol)
	}
}

func TestSlotTransitionHandler_HandleTransition_EnabledSlot(t *testing.T) {
	logger := zap.NewNop()
	mockGrid := &mockGridManager{}
	mockAdaptive := NewAdaptiveGridManager(nil, nil, nil, nil, logger)
	// Initialize required maps
	mockAdaptive.tradingPaused = make(map[string]bool)

	handler := NewSlotTransitionHandler(mockGrid, mockAdaptive, logger)
	handler.SetCooldownPeriod(0) // Disable cooldown for testing

	ctx := context.Background()
	symbol := "BTCUSDT"

	// Create an enabled slot
	enabledSlot := &TimeSlotConfig{
		Window: TimeWindow{
			Start:    "09:00",
			End:      "12:00",
			Timezone: "Asia/Ho_Chi_Minh",
		},
		Enabled:          true,
		SizeMultiplier:   1.0,
		MaxExposurePct:   0.3,
		SpreadMultiplier: 1.0,
		Description:      "Morning Session",
	}

	// Test transition to enabled slot
	err := handler.HandleTransition(ctx, symbol, nil, enabledSlot)
	if err != nil {
		t.Errorf("HandleTransition returned error: %v", err)
	}

	if !mockGrid.cancelCalled {
		t.Error("Expected CancelAllOrders to be called")
	}

	if !mockGrid.clearCalled {
		t.Error("Expected ClearGrid to be called")
	}

	if !mockGrid.rebuildCalled {
		t.Error("Expected RebuildGrid to be called for enabled slot")
	}
}

func TestSlotTransitionHandler_HandleTransition_CooldownActive(t *testing.T) {
	logger := zap.NewNop()
	mockGrid := &mockGridManager{}

	handler := NewSlotTransitionHandler(mockGrid, nil, logger)
	// Keep default cooldown

	ctx := context.Background()
	symbol := "BTCUSDT"

	// Set last transition to now
	handler.updateTransitionTime(symbol)

	enabledSlot := &TimeSlotConfig{
		Enabled:     true,
		Description: "Test Slot",
	}

	// Should skip due to cooldown
	err := handler.HandleTransition(ctx, symbol, nil, enabledSlot)
	if err != nil {
		t.Errorf("HandleTransition returned error: %v", err)
	}

	// No operations should be called during cooldown
	if mockGrid.cancelCalled {
		t.Error("Expected no operations during cooldown")
	}
}

func TestSlotTransitionHandler_GetLastTransition(t *testing.T) {
	logger := zap.NewNop()
	handler := NewSlotTransitionHandler(nil, nil, logger)

	symbol := "BTCUSDT"

	// Before any transition
	_, exists := handler.GetLastTransition(symbol)
	if exists {
		t.Error("Expected no last transition for new symbol")
	}

	// After transition
	handler.updateTransitionTime(symbol)

	lastTime, exists := handler.GetLastTransition(symbol)
	if !exists {
		t.Error("Expected last transition to exist after update")
	}

	if lastTime.IsZero() {
		t.Error("Expected non-zero transition time")
	}
}

func TestSlotTransitionHandler_GetTransitionStatus(t *testing.T) {
	logger := zap.NewNop()
	handler := NewSlotTransitionHandler(nil, nil, logger)
	symbol := "BTCUSDT"

	// Initial status
	status := handler.GetTransitionStatus(symbol)

	canTransition, ok := status["can_transition"].(bool)
	if !ok || !canTransition {
		t.Error("Expected can_transition to be true initially")
	}

	// After transition
	handler.updateTransitionTime(symbol)
	status = handler.GetTransitionStatus(symbol)

	canTransition, ok = status["can_transition"].(bool)
	if !ok || canTransition {
		t.Error("Expected can_transition to be false after transition")
	}

	timeRemaining, ok := status["time_remaining"].(time.Duration)
	if !ok || timeRemaining == 0 {
		t.Error("Expected non-zero time remaining during cooldown")
	}
}
