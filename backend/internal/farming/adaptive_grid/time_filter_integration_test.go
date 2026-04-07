package adaptive_grid

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestTimeFilterEndToEnd tests the complete time filter flow
func TestTimeFilterEndToEnd(t *testing.T) {
	logger := zap.NewNop()

	// Create a config that always allows trading (for testing)
	config := &TradingHoursConfig{
		Mode:                  TimeFilterAll,
		Timezone:              "Asia/Ho_Chi_Minh",
		DefaultSizeMultiplier: 1.0,
		DefaultMaxExposurePct: 0.3,
		Slots:                 []TimeSlotConfig{},
	}

	timeFilter, err := NewTimeFilter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create TimeFilter: %v", err)
	}

	// Test CanTrade
	if !timeFilter.CanTrade() {
		t.Error("Expected CanTrade to return true for 'all' mode")
	}

	// Test GetSizeMultiplier
	sizeMult := timeFilter.GetSizeMultiplier()
	if sizeMult != 1.0 {
		t.Errorf("Expected size multiplier 1.0, got %v", sizeMult)
	}

	// Test GetSpreadMultiplier
	spreadMult := timeFilter.GetSpreadMultiplier()
	if spreadMult != 1.0 {
		t.Errorf("Expected spread multiplier 1.0, got %v", spreadMult)
	}

	// Test GetCurrentStatus
	status := timeFilter.GetCurrentStatus()
	if status == nil {
		t.Error("Expected status to not be nil")
	}

	mode, ok := status["mode"].(string)
	if !ok || mode != "all" {
		t.Errorf("Expected mode 'all' in status, got %v", mode)
	}
}

// TestTimeFilterWithAdaptiveGridManager tests integration with AdaptiveGridManager
func TestTimeFilterWithAdaptiveGridManager(t *testing.T) {
	logger := zap.NewNop()

	// Create TimeFilter with specific time slots
	config := &TradingHoursConfig{
		Mode:                  TimeFilterSelect,
		Timezone:              "Asia/Ho_Chi_Minh",
		DefaultSizeMultiplier: 1.0,
		DefaultMaxExposurePct: 0.3,
		Slots: []TimeSlotConfig{
			{
				Window: TimeWindow{
					Start:    "00:00",
					End:      "23:59",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          true,
				SizeMultiplier:   0.8,
				MaxExposurePct:   0.25,
				SpreadMultiplier: 1.2,
				Description:      "All Day Test Slot",
			},
		},
	}

	timeFilter, err := NewTimeFilter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create TimeFilter: %v", err)
	}

	// Verify TimeFilter reports correct values
	sizeMult := timeFilter.GetSizeMultiplier()
	if sizeMult != 0.8 {
		t.Errorf("Expected size multiplier 0.8, got %v", sizeMult)
	}

	spreadMult := timeFilter.GetSpreadMultiplier()
	if spreadMult != 1.2 {
		t.Errorf("Expected spread multiplier 1.2, got %v", spreadMult)
	}

	maxExposure := timeFilter.GetMaxExposurePct()
	if maxExposure != 0.25 {
		t.Errorf("Expected max exposure 0.25, got %v", maxExposure)
	}

	// Verify TimeFilter reports trading allowed
	if !timeFilter.CanTrade() {
		t.Error("Expected CanTrade to return true for enabled slot")
	}
}

// TestSlotTransitionHandlerWithTimeFilter tests the handler with actual TimeFilter
func TestSlotTransitionHandlerWithTimeFilter(t *testing.T) {
	logger := zap.NewNop()

	// Create TimeFilter that is currently outside trading hours
	config := &TradingHoursConfig{
		Mode:     TimeFilterSelect,
		Timezone: "Asia/Ho_Chi_Minh",
		Slots: []TimeSlotConfig{
			{
				Window: TimeWindow{
					Start:    "01:00", // Very early slot
					End:      "02:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          true,
				SizeMultiplier:   0.5,
				MaxExposurePct:   0.2,
				SpreadMultiplier: 1.5,
				Description:      "Early Morning",
			},
		},
	}

	timeFilter, err := NewTimeFilter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create TimeFilter: %v", err)
	}

	// Create mock grid manager
	mockGrid := &mockGridManager{}

	// Create adaptive manager with initialized maps
	adaptiveMgr := NewAdaptiveGridManager(nil, nil, nil, nil, logger)

	// Create transition handler
	handler := NewSlotTransitionHandler(mockGrid, adaptiveMgr, logger)
	handler.SetCooldownPeriod(0) // Disable cooldown for testing

	// Get current slot
	currentSlot := timeFilter.GetCurrentSlot()
	t.Logf("Current slot: %v", currentSlot)

	// Test transition handling
	ctx := context.Background()
	symbol := "BTCUSDT"

	// Test disabled slot transition
	disabledSlot := &TimeSlotConfig{
		Enabled:          false,
		SizeMultiplier:   0,
		MaxExposurePct:   0,
		SpreadMultiplier: 1.0,
		Description:      "Disabled Slot",
	}

	err = handler.HandleTransition(ctx, symbol, currentSlot, disabledSlot)
	if err != nil {
		t.Errorf("HandleTransition failed: %v", err)
	}

	// For disabled slot, orders should be cancelled
	if !mockGrid.cancelCalled {
		t.Error("Expected CancelAllOrders to be called for disabled slot")
	}
}

// TestTimeFilterSlotChangeCallback tests the callback mechanism
func TestTimeFilterSlotChangeCallback(t *testing.T) {
	logger := zap.NewNop()

	config := &TradingHoursConfig{
		Mode:     TimeFilterSelect,
		Timezone: "Asia/Ho_Chi_Minh",
		Slots: []TimeSlotConfig{
			{
				Window: TimeWindow{
					Start:    "00:00",
					End:      "23:59",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          true,
				SizeMultiplier:   1.0,
				MaxExposurePct:   0.3,
				SpreadMultiplier: 1.0,
				Description:      "All Day",
			},
		},
	}

	timeFilter, err := NewTimeFilter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create TimeFilter: %v", err)
	}

	callbackInvoked := make(chan bool, 1)
	var receivedOld, receivedNew *TimeSlotConfig

	// Register callback
	timeFilter.RegisterSlotChangeCallback(func(old, new *TimeSlotConfig) {
		receivedOld = old
		receivedNew = new
		callbackInvoked <- true
	})

	// Initial tracking update
	changed := timeFilter.UpdateSlotTracking()
	t.Logf("Initial UpdateSlotTracking changed: %v", changed)

	// Verify callback was invoked (or not) depending on initial state
	select {
	case invoked := <-callbackInvoked:
		t.Logf("Callback invoked: %v, old: %v, new: %v", invoked, receivedOld, receivedNew)
	case <-time.After(100 * time.Millisecond):
		t.Log("Callback not invoked (expected for initial call with no change)")
	}

	// Second update should not change (same slot)
	changed = timeFilter.UpdateSlotTracking()
	if changed {
		t.Log("Slot changed on second update (might happen if time crossed slot boundary)")
	}
}

// TestTimeFilterHotReload tests config update functionality
func TestTimeFilterHotReload(t *testing.T) {
	logger := zap.NewNop()

	// Initial config - all mode
	initialConfig := &TradingHoursConfig{
		Mode:                  TimeFilterAll,
		Timezone:              "UTC",
		DefaultSizeMultiplier: 1.0,
	}

	timeFilter, err := NewTimeFilter(initialConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create TimeFilter: %v", err)
	}

	// Verify initial state
	if timeFilter.config.Mode != TimeFilterAll {
		t.Error("Expected initial mode to be 'all'")
	}

	// New config - select mode with no slots (no trading)
	newConfig := &TradingHoursConfig{
		Mode:     TimeFilterSelect,
		Timezone: "UTC",
		Slots:    []TimeSlotConfig{},
	}

	// Update config
	err = timeFilter.UpdateConfig(newConfig)
	if err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// Verify updated state
	if timeFilter.config.Mode != TimeFilterSelect {
		t.Error("Expected updated mode to be 'select'")
	}

	// With no slots, CanTrade should return false
	if timeFilter.CanTrade() {
		t.Error("Expected CanTrade to return false after switching to select mode with no slots")
	}
}

// BenchmarkTimeFilterCanTrade benchmarks the CanTrade check
func BenchmarkTimeFilterCanTrade(b *testing.B) {
	logger := zap.NewNop()
	config := &TradingHoursConfig{
		Mode:     TimeFilterSelect,
		Timezone: "Asia/Ho_Chi_Minh",
		Slots: []TimeSlotConfig{
			{
				Window: TimeWindow{
					Start:    "00:00",
					End:      "23:59",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled: true,
			},
		},
	}

	timeFilter, _ := NewTimeFilter(config, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = timeFilter.CanTrade()
	}
}

// BenchmarkTimeFilterGetCurrentSlot benchmarks slot lookup
func BenchmarkTimeFilterGetCurrentSlot(b *testing.B) {
	logger := zap.NewNop()
	config := &TradingHoursConfig{
		Mode:     TimeFilterSelect,
		Timezone: "Asia/Ho_Chi_Minh",
		Slots: []TimeSlotConfig{
			{
				Window: TimeWindow{
					Start:    "00:00",
					End:      "23:59",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled: true,
			},
		},
	}

	timeFilter, _ := NewTimeFilter(config, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = timeFilter.GetCurrentSlot()
	}
}
