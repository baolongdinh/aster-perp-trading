package adaptive_grid

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TimeFilterMode defines how time filtering operates
type TimeFilterMode string

const (
	TimeFilterAll    TimeFilterMode = "all"    // Trade all hours (no filter)
	TimeFilterNone   TimeFilterMode = "none"   // No trading at all
	TimeFilterSelect TimeFilterMode = "select" // Trade only specific hours
)

// TimeWindow defines a time range for trading
type TimeWindow struct {
	Start    string `yaml:"start"`    // Format "HH:MM" (24h)
	End      string `yaml:"end"`      // Format "HH:MM" (24h)
	Timezone string `yaml:"timezone"` // e.g., "Asia/Ho_Chi_Minh", "UTC", "America/New_York"
}

// TimeSlotConfig defines config for a specific time slot
type TimeSlotConfig struct {
	Window           TimeWindow `yaml:"window"`
	Enabled          bool       `yaml:"enabled"`           // Có trade không
	SizeMultiplier   float64    `yaml:"size_multiplier"`   // Hệ số giảm size (1.0 = normal, 0.5 = giảm 50%)
	MaxExposurePct   float64    `yaml:"max_exposure_pct"`  // Max exposure % trong slot này (override global)
	SpreadMultiplier float64    `yaml:"spread_multiplier"` // Hệ số tăng spread (1.0 = normal, 2.0 = tăng gấp đôi)
	Description      string     `yaml:"description"`       // Mô tả slot (e.g., "US Session")
}

// TradingHoursConfig holds complete time-based trading configuration
type TradingHoursConfig struct {
	Mode                  TimeFilterMode   `yaml:"mode"`                     // all, none, select
	Timezone              string           `yaml:"timezone"`                 // Default timezone
	Slots                 []TimeSlotConfig `yaml:"slots"`                    // Các khung giờ cụ thể
	DefaultSizeMultiplier float64          `yaml:"default_size_multiplier"`  // Mặc định nếu không match slot nào
	DefaultMaxExposurePct float64          `yaml:"default_max_exposure_pct"` // Mặc định max exposure
}

// DefaultTradingHoursConfig returns sensible defaults for VN timezone
func DefaultTradingHoursConfig() *TradingHoursConfig {
	return &TradingHoursConfig{
		Mode:                  "select",
		Timezone:              "Asia/Ho_Chi_Minh",
		DefaultSizeMultiplier: 1.0,
		DefaultMaxExposurePct: 0.3,
		Slots: []TimeSlotConfig{
			{
				Window: TimeWindow{
					Start:    "07:00",
					End:      "12:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          true,
				SizeMultiplier:   1.0, // Full size - thị trường yên ả
				MaxExposurePct:   0.3, // Normal exposure
				SpreadMultiplier: 1.0, // Normal spread
				Description:      "Phiên Á - Thấp điểm (Lý tưởng cho Grid)",
			},
			{
				Window: TimeWindow{
					Start:    "12:00",
					End:      "13:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:        false, // Nghỉ trưa - biến động không rõ ràng
				SizeMultiplier: 0.0,
				Description:    "Nghỉ trưa - Không trade",
			},
			{
				Window: TimeWindow{
					Start:    "13:00",
					End:      "18:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          true,
				SizeMultiplier:   0.7,  // Giảm 30% - Phiên Âu bắt đầu
				MaxExposurePct:   0.25, // Giảm exposure
				SpreadMultiplier: 1.3,  // Tăng spread 30%
				Description:      "Phiên Âu - Trung bình (Cẩn trọng)",
			},
			{
				Window: TimeWindow{
					Start:    "18:00",
					End:      "19:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:        false, // Nghỉ chờ phiên Mỹ
				SizeMultiplier: 0.0,
				Description:    "Chuẩn bị phiên Mỹ - Không trade",
			},
			{
				Window: TimeWindow{
					Start:    "19:00",
					End:      "23:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          true,
				SizeMultiplier:   0.3,  // Giảm 70% - Phiên Mỹ rất mạnh
				MaxExposurePct:   0.15, // Giảm mạnh exposure
				SpreadMultiplier: 2.0,  // Tăng gấp đôi spread
				Description:      "Phiên Mỹ - Cao điểm (Very High Risk)",
			},
			{
				Window: TimeWindow{
					Start:    "23:00",
					End:      "01:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:        false, // Đóng cửa phiên Mỹ
				SizeMultiplier: 0.0,
				Description:    "Đóng cửa - Không trade",
			},
			{
				Window: TimeWindow{
					Start:    "01:00",
					End:      "07:00",
					Timezone: "Asia/Ho_Chi_Minh",
				},
				Enabled:          true,
				SizeMultiplier:   0.8, // Giảm 20% - Khuya yên ả nhưng cần cẩn trọng
				MaxExposurePct:   0.25,
				SpreadMultiplier: 1.2,
				Description:      "Phiên khuya - Trung bình thấp",
			},
		},
	}
}

// TimeFilter manages time-based trading rules
type TimeFilter struct {
	config   *TradingHoursConfig
	logger   *zap.Logger
	mu       sync.RWMutex
	location *time.Location // Cached timezone
}

// NewTimeFilter creates new time filter
func NewTimeFilter(config *TradingHoursConfig, logger *zap.Logger) (*TimeFilter, error) {
	if config == nil {
		config = DefaultTradingHoursConfig()
	}

	// Load timezone
	loc, err := time.LoadLocation(config.Timezone)
	if err != nil {
		logger.Warn("Failed to load timezone, using UTC", zap.String("timezone", config.Timezone), zap.Error(err))
		loc = time.UTC
	}

	tf := &TimeFilter{
		config:   config,
		logger:   logger,
		location: loc,
	}

	if err := tf.ValidateConfig(); err != nil {
		return nil, err
	}

	return tf, nil
}

// ValidateConfig validates the time filter configuration
func (t *TimeFilter) ValidateConfig() error {
	if t.config.Mode != TimeFilterAll && t.config.Mode != TimeFilterNone && t.config.Mode != TimeFilterSelect {
		return fmt.Errorf("invalid time filter mode: %s", t.config.Mode)
	}

	// Validate each slot
	for i, slot := range t.config.Slots {
		if slot.Window.Start == "" || slot.Window.End == "" {
			return fmt.Errorf("slot %d: start and end time required", i)
		}

		// Parse times to validate format
		_, err := time.Parse("15:04", slot.Window.Start)
		if err != nil {
			return fmt.Errorf("slot %d: invalid start time format: %s", i, slot.Window.Start)
		}

		_, err = time.Parse("15:04", slot.Window.End)
		if err != nil {
			return fmt.Errorf("slot %d: invalid end time format: %s", i, slot.Window.End)
		}

		// Validate timezone
		if slot.Window.Timezone != "" {
			_, err = time.LoadLocation(slot.Window.Timezone)
			if err != nil {
				return fmt.Errorf("slot %d: invalid timezone: %s", i, slot.Window.Timezone)
			}
		}

		// Validate multipliers
		if slot.SizeMultiplier < 0 || slot.SizeMultiplier > 2.0 {
			return fmt.Errorf("slot %d: size_multiplier must be between 0 and 2.0", i)
		}
	}

	return nil
}

// GetCurrentSlot returns the current time slot configuration
func (t *TimeFilter) GetCurrentSlot() *TimeSlotConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now().In(t.location)
	currentTime := now.Format("15:04")

	for i := range t.config.Slots {
		slot := &t.config.Slots[i]

		// Parse slot times
		startTime := slot.Window.Start
		endTime := slot.Window.End

		// Check if current time falls within this slot
		if t.isTimeInRange(currentTime, startTime, endTime) {
			return slot
		}
	}

	return nil
}

// isTimeInRange checks if current time is within start-end range
func (t *TimeFilter) isTimeInRange(current, start, end string) bool {
	// Handle overnight slots (e.g., 23:00 - 01:00)
	if end < start {
		// Overnight slot: either after start OR before end
		return current >= start || current <= end
	}
	// Normal slot: between start and end
	return current >= start && current <= end
}

// CanTrade returns true if trading is allowed at current time
func (t *TimeFilter) CanTrade() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	switch t.config.Mode {
	case TimeFilterAll:
		return true
	case TimeFilterNone:
		return false
	case TimeFilterSelect:
		slot := t.GetCurrentSlot()
		if slot == nil {
			// No matching slot - use default
			return false
		}
		return slot.Enabled
	}

	return false
}

// GetSizeMultiplier returns the size multiplier for current time
func (t *TimeFilter) GetSizeMultiplier() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.config.Mode == TimeFilterNone {
		return 0.0
	}

	if t.config.Mode == TimeFilterAll {
		return t.config.DefaultSizeMultiplier
	}

	slot := t.GetCurrentSlot()
	if slot == nil || !slot.Enabled {
		return 0.0
	}

	return slot.SizeMultiplier
}

// GetMaxExposurePct returns max exposure % for current time
func (t *TimeFilter) GetMaxExposurePct() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.config.Mode == TimeFilterNone {
		return 0.0
	}

	slot := t.GetCurrentSlot()
	if slot == nil || !slot.Enabled {
		return 0.0
	}

	if slot.MaxExposurePct > 0 {
		return slot.MaxExposurePct
	}

	return t.config.DefaultMaxExposurePct
}

// GetSpreadMultiplier returns spread multiplier for current time
func (t *TimeFilter) GetSpreadMultiplier() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.config.Mode != TimeFilterSelect {
		return 1.0
	}

	slot := t.GetCurrentSlot()
	if slot == nil || !slot.Enabled {
		return 1.0
	}

	if slot.SpreadMultiplier > 0 {
		return slot.SpreadMultiplier
	}

	return 1.0
}

// GetCurrentStatus returns detailed status for current time
func (t *TimeFilter) GetCurrentStatus() map[string]interface{} {
	slot := t.GetCurrentSlot()
	now := time.Now().In(t.location)

	status := map[string]interface{}{
		"current_time": now.Format("15:04"),
		"timezone":     t.location.String(),
		"mode":         string(t.config.Mode),
		"can_trade":    t.CanTrade(),
	}

	if slot != nil {
		status["slot_start"] = slot.Window.Start
		status["slot_end"] = slot.Window.End
		status["slot_description"] = slot.Description
		status["slot_enabled"] = slot.Enabled
		status["size_multiplier"] = slot.SizeMultiplier
		status["max_exposure_pct"] = slot.MaxExposurePct
		status["spread_multiplier"] = slot.SpreadMultiplier
	}

	return status
}

// IsHighVolatilityPeriod returns true if currently in high volatility slot
func (t *TimeFilter) IsHighVolatilityPeriod() bool {
	slot := t.GetCurrentSlot()
	if slot == nil {
		return false
	}
	// Consider high volatility if size multiplier < 0.5
	return slot.SizeMultiplier < 0.5
}

// GetNextTradingSlot returns the next upcoming trading slot
func (t *TimeFilter) GetNextTradingSlot() *TimeSlotConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now().In(t.location)
	currentTime := now.Format("15:04")

	// Find next enabled slot
	for i := range t.config.Slots {
		slot := &t.config.Slots[i]
		if !slot.Enabled {
			continue
		}

		// If this slot is in the future
		if slot.Window.Start > currentTime {
			return slot
		}
	}

	// If no future slot, return first enabled slot (tomorrow)
	for i := range t.config.Slots {
		slot := &t.config.Slots[i]
		if slot.Enabled {
			return slot
		}
	}

	return nil
}

// UpdateConfig updates the time filter config (hot reload)
func (t *TimeFilter) UpdateConfig(config *TradingHoursConfig) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Validate new config
	_, err := NewTimeFilter(config, t.logger)
	if err != nil {
		return err
	}

	// Update location
	if config.Timezone != "" {
		loc, err := time.LoadLocation(config.Timezone)
		if err == nil {
			t.location = loc
		}
	}

	t.config = config
	t.logger.Info("TimeFilter config updated", zap.String("mode", string(config.Mode)))
	return nil
}

// GetAllSlots returns all configured slots
func (t *TimeFilter) GetAllSlots() []TimeSlotConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Return copy to prevent external modification
	slots := make([]TimeSlotConfig, len(t.config.Slots))
	copy(slots, t.config.Slots)
	return slots
}
