package adaptive_grid

import (
	"testing"

	"go.uber.org/zap"
)

// setupTestTimeFilter creates a TimeFilter with test configuration
func setupTestTimeFilter() (*TimeFilter, *zap.Logger) {
	logger := zap.NewNop()
	config := &TradingHoursConfig{
		Mode:                  TimeFilterSelect,
		Timezone:              "Asia/Ho_Chi_Minh",
		DefaultSizeMultiplier: 1.0,
		DefaultMaxExposurePct: 0.3,
		Slots: []TimeSlotConfig{
			{
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
			},
			{
				Window: TimeWindow{
					Start:    "14:00",
					End:      "17:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          true,
				SizeMultiplier:   0.5,
				MaxExposurePct:   0.2,
				SpreadMultiplier: 1.5,
				Description:      "Afternoon Session",
			},
			{
				Window: TimeWindow{
					Start:    "22:00",
					End:      "02:00", // Overnight slot
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          false,
				SizeMultiplier:   0.0,
				MaxExposurePct:   0.0,
				SpreadMultiplier: 1.0,
				Description:      "Overnight Session (Disabled)",
			},
		},
	}

	tf, _ := NewTimeFilter(config, logger)
	return tf, logger
}

func TestNewTimeFilter(t *testing.T) {
	tf, _ := setupTestTimeFilter()

	if tf == nil {
		t.Fatal("Expected TimeFilter to be created")
	}

	if tf.config.Mode != TimeFilterSelect {
		t.Errorf("Expected mode to be 'select', got %s", tf.config.Mode)
	}

	if len(tf.config.Slots) != 3 {
		t.Errorf("Expected 3 slots, got %d", len(tf.config.Slots))
	}
}

func TestTimeFilter_CanTrade(t *testing.T) {
	tests := []struct {
		name     string
		mode     TimeFilterMode
		expected bool
	}{
		{
			name:     "Mode 'all' allows trading",
			mode:     TimeFilterAll,
			expected: true,
		},
		{
			name:     "Mode 'none' blocks trading",
			mode:     TimeFilterNone,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			config := &TradingHoursConfig{
				Mode:     tt.mode,
				Timezone: "Asia/Ho_Chi_Minh",
			}
			tf, err := NewTimeFilter(config, logger)
			if err != nil {
				t.Fatalf("Failed to create TimeFilter: %v", err)
			}

			result := tf.CanTrade()
			if result != tt.expected {
				t.Errorf("CanTrade() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestTimeFilter_GetSizeMultiplier(t *testing.T) {
	tests := []struct {
		name           string
		mode           TimeFilterMode
		slotMultiplier float64
		expected       float64
	}{
		{
			name:           "Mode 'all' returns default",
			mode:           TimeFilterAll,
			slotMultiplier: 0.0,
			expected:       1.0, // Default from config
		},
		{
			name:           "Mode 'none' returns 0",
			mode:           TimeFilterNone,
			slotMultiplier: 1.0,
			expected:       0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			config := &TradingHoursConfig{
				Mode:                  tt.mode,
				Timezone:              "Asia/Ho_Chi_Minh",
				DefaultSizeMultiplier: 1.0,
				Slots: []TimeSlotConfig{
					{
						Window: TimeWindow{
							Start:    "00:00",
							End:      "23:59",
							Timezone: "Asia/Ho_Chi_Minh",
						},
						Enabled:        true,
						SizeMultiplier: tt.slotMultiplier,
					},
				},
			}
			tf, err := NewTimeFilter(config, logger)
			if err != nil {
				t.Fatalf("Failed to create TimeFilter: %v", err)
			}

			result := tf.GetSizeMultiplier()
			if result != tt.expected {
				t.Errorf("GetSizeMultiplier() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestTimeFilter_GetSpreadMultiplier(t *testing.T) {
	tests := []struct {
		name           string
		mode           TimeFilterMode
		slotSpreadMult float64
		expected       float64
	}{
		{
			name:           "Mode 'all' returns 1.0",
			mode:           TimeFilterAll,
			slotSpreadMult: 2.0,
			expected:       1.0,
		},
		{
			name:           "Mode 'select' with disabled slot returns 1.0",
			mode:           TimeFilterSelect,
			slotSpreadMult: 0.0,
			expected:       1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			config := &TradingHoursConfig{
				Mode:     tt.mode,
				Timezone: "Asia/Ho_Chi_Minh",
			}
			tf, err := NewTimeFilter(config, logger)
			if err != nil {
				t.Fatalf("Failed to create TimeFilter: %v", err)
			}

			result := tf.GetSpreadMultiplier()
			if result != tt.expected {
				t.Errorf("GetSpreadMultiplier() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestTimeFilter_isTimeInRange(t *testing.T) {
	tf, _ := setupTestTimeFilter()

	tests := []struct {
		name     string
		current  string
		start    string
		end      string
		expected bool
	}{
		{
			name:     "Time in normal range",
			current:  "10:00",
			start:    "09:00",
			end:      "12:00",
			expected: true,
		},
		{
			name:     "Time before range",
			current:  "08:00",
			start:    "09:00",
			end:      "12:00",
			expected: false,
		},
		{
			name:     "Time after range",
			current:  "13:00",
			start:    "09:00",
			end:      "12:00",
			expected: false,
		},
		{
			name:     "Time in overnight range (after start)",
			current:  "23:00",
			start:    "22:00",
			end:      "02:00",
			expected: true,
		},
		{
			name:     "Time in overnight range (before end)",
			current:  "01:00",
			start:    "22:00",
			end:      "02:00",
			expected: true,
		},
		{
			name:     "Time outside overnight range",
			current:  "12:00",
			start:    "22:00",
			end:      "02:00",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tf.isTimeInRange(tt.current, tt.start, tt.end)
			if result != tt.expected {
				t.Errorf("isTimeInRange(%s, %s, %s) = %v, expected %v",
					tt.current, tt.start, tt.end, result, tt.expected)
			}
		})
	}
}

func TestTimeFilter_HasSlotChanged(t *testing.T) {
	tf, _ := setupTestTimeFilter()

	// Initial call should return false (previousSlot is nil, current is nil or first slot)
	changed := tf.HasSlotChanged()
	// We can't predict the exact result without controlling time
	// but we can verify it doesn't panic
	t.Logf("Initial HasSlotChanged() = %v", changed)

	// After updating tracking, should return false for same slot
	tf.UpdateSlotTracking()
	changed = tf.HasSlotChanged()
	t.Logf("After UpdateSlotTracking(), HasSlotChanged() = %v", changed)
}

func TestTimeFilter_RegisterSlotChangeCallback(t *testing.T) {
	tf, _ := setupTestTimeFilter()

	callbackCalled := false
	var oldSlot, newSlot *TimeSlotConfig

	callback := func(o, n *TimeSlotConfig) {
		callbackCalled = true
		oldSlot = o
		newSlot = n
	}

	tf.RegisterSlotChangeCallback(callback)

	if len(tf.slotChangeCallbacks) != 1 {
		t.Errorf("Expected 1 callback, got %d", len(tf.slotChangeCallbacks))
	}

	// Force a slot change detection and update
	tf.UpdateSlotTracking()

	// Callback might or might not be called depending on current time
	t.Logf("Callback called: %v, old: %v, new: %v", callbackCalled, oldSlot, newSlot)
}

func TestTimeFilter_GetNextTradingSlot(t *testing.T) {
	tf, _ := setupTestTimeFilter()

	nextSlot := tf.GetNextTradingSlot()

	// Should return either the next slot or the first enabled slot for tomorrow
	if nextSlot != nil && !nextSlot.Enabled {
		t.Error("GetNextTradingSlot should return an enabled slot")
	}

	t.Logf("Next trading slot: %v", nextSlot)
}

func TestTimeFilter_UpdateConfig(t *testing.T) {
	tf, _ := setupTestTimeFilter()

	newConfig := &TradingHoursConfig{
		Mode:     TimeFilterAll,
		Timezone: "UTC",
		Slots:    []TimeSlotConfig{},
	}

	err := tf.UpdateConfig(newConfig)
	if err != nil {
		t.Errorf("UpdateConfig failed: %v", err)
	}

	if tf.config.Mode != TimeFilterAll {
		t.Errorf("Expected mode to be updated to 'all', got %s", tf.config.Mode)
	}
}

func TestTimeFilter_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *TradingHoursConfig
		wantErr bool
	}{
		{
			name: "Valid config with all mode",
			config: &TradingHoursConfig{
				Mode:     TimeFilterAll,
				Timezone: "UTC",
			},
			wantErr: false,
		},
		{
			name: "Valid config with none mode",
			config: &TradingHoursConfig{
				Mode:     TimeFilterNone,
				Timezone: "UTC",
			},
			wantErr: false,
		},
		{
			name: "Valid config with select mode and slots",
			config: &TradingHoursConfig{
				Mode:     TimeFilterSelect,
				Timezone: "UTC",
				Slots: []TimeSlotConfig{
					{
						Window:  TimeWindow{Start: "09:00", End: "17:00"},
						Enabled: true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid mode",
			config: &TradingHoursConfig{
				Mode:     "invalid",
				Timezone: "UTC",
			},
			wantErr: true,
		},
		{
			name: "Invalid time format",
			config: &TradingHoursConfig{
				Mode:     TimeFilterSelect,
				Timezone: "UTC",
				Slots: []TimeSlotConfig{
					{
						Window:  TimeWindow{Start: "invalid", End: "17:00"},
						Enabled: true,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid size multiplier",
			config: &TradingHoursConfig{
				Mode:     TimeFilterSelect,
				Timezone: "UTC",
				Slots: []TimeSlotConfig{
					{
						Window:         TimeWindow{Start: "09:00", End: "17:00"},
						Enabled:        true,
						SizeMultiplier: 3.0, // Invalid: > 2.0
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			tf, err := NewTimeFilter(tt.config, logger)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tf == nil {
					t.Error("Expected TimeFilter, got nil")
				}
			}
		})
	}
}
