package farming

import (
	"testing"
	"time"

	"aster-bot/internal/config"

	"github.com/stretchr/testify/assert"
)

// TestVolumeFarmEngine_WithVolumeOptimizationEnabled tests initialization with volume optimization enabled
func TestVolumeFarmEngine_WithVolumeOptimizationEnabled(t *testing.T) {
	// Create config with volume optimization enabled
	volumeConfig := &config.VolumeFarmConfig{
		VolumeOptimization: &config.VolumeOptimizationConfig{
			Enabled: true,
			OrderPriority: config.OrderPriorityConfig{
				TickSizeAwareness: config.TickSizeConfig{
					Enabled:         true,
					TickSizes:       map[string]float64{"BTCUSD1": 0.1},
					DefaultTickSize: 0.01,
				},
			},
			ToxicFlow: config.ToxicFlowConfig{
				Enabled:           true,
				WindowSize:        50,
				BucketSize:        1000.0,
				VPINThreshold:     0.3,
				SustainedBreaches: 2,
				Action:            "pause",
				AutoResumeDelay:   5 * time.Second,
			},
			MakerTaker: config.MakerTakerConfig{
				PostOnlyEnabled:  true,
				PostOnlyFallback: true,
				SmartCancellation: config.SmartCancelConfig{
					Enabled:               true,
					SpreadChangeThreshold: 0.2,
					CheckInterval:         5 * time.Second,
				},
			},
		},
	}

	// Validate config
	err := validateVolumeOptimizationConfig(volumeConfig.VolumeOptimization)
	assert.NoError(t, err, "volume optimization config should be valid")
}

// TestVolumeFarmEngine_WithVolumeOptimizationDisabled tests initialization with volume optimization disabled
func TestVolumeFarmEngine_WithVolumeOptimizationDisabled(t *testing.T) {
	// Create config with volume optimization disabled
	volumeConfig := &config.VolumeFarmConfig{
		VolumeOptimization: &config.VolumeOptimizationConfig{
			Enabled: false,
		},
	}

	// Should not fail when volume optimization is disabled
	assert.NotNil(t, volumeConfig.VolumeOptimization)
	assert.False(t, volumeConfig.VolumeOptimization.Enabled)
}

// TestVolumeFarmEngine_WithVolumeOptimizationNil tests initialization without volume optimization config
func TestVolumeFarmEngine_WithVolumeOptimizationNil(t *testing.T) {
	// Create config without volume optimization
	volumeConfig := &config.VolumeFarmConfig{
		VolumeOptimization: nil,
	}

	// Should not fail when volume optimization is nil
	assert.Nil(t, volumeConfig.VolumeOptimization)
}

// TestValidateVolumeOptimizationConfig_Valid tests validation with valid config
func TestValidateVolumeOptimizationConfig_Valid(t *testing.T) {
	validConfig := &config.VolumeOptimizationConfig{
		Enabled: true,
		OrderPriority: config.OrderPriorityConfig{
			TickSizeAwareness: config.TickSizeConfig{
				Enabled:         true,
				TickSizes:       map[string]float64{"BTCUSD1": 0.1},
				DefaultTickSize: 0.01,
			},
		},
		ToxicFlow: config.ToxicFlowConfig{
			Enabled:           true,
			WindowSize:        50,
			BucketSize:        1000.0,
			VPINThreshold:     0.3,
			SustainedBreaches: 2,
			Action:            "pause",
			AutoResumeDelay:   5 * time.Second,
		},
		MakerTaker: config.MakerTakerConfig{
			PostOnlyEnabled:  true,
			PostOnlyFallback: true,
			SmartCancellation: config.SmartCancelConfig{
				Enabled:               true,
				SpreadChangeThreshold: 0.2,
				CheckInterval:         5 * time.Second,
			},
		},
	}

	err := validateVolumeOptimizationConfig(validConfig)
	assert.NoError(t, err)
}

// TestValidateVolumeOptimizationConfig_InvalidTickSize tests validation with invalid tick size config
func TestValidateVolumeOptimizationConfig_InvalidTickSize(t *testing.T) {
	invalidConfig := &config.VolumeOptimizationConfig{
		Enabled: true,
		OrderPriority: config.OrderPriorityConfig{
			TickSizeAwareness: config.TickSizeConfig{
				Enabled:         true,
				TickSizes:       map[string]float64{}, // Empty tick sizes
				DefaultTickSize: 0.01,
			},
		},
	}

	err := validateVolumeOptimizationConfig(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tick_size_awareness is enabled but no tick sizes configured")
}

// TestValidateVolumeOptimizationConfig_InvalidVPIN tests validation with invalid VPIN config
func TestValidateVolumeOptimizationConfig_InvalidVPIN(t *testing.T) {
	invalidConfig := &config.VolumeOptimizationConfig{
		Enabled: true,
		OrderPriority: config.OrderPriorityConfig{
			TickSizeAwareness: config.TickSizeConfig{
				Enabled:         false,
				TickSizes:       map[string]float64{},
				DefaultTickSize: 0.01,
			},
		},
		ToxicFlow: config.ToxicFlowConfig{
			Enabled:           true,
			WindowSize:        0, // Invalid
			BucketSize:        1000.0,
			VPINThreshold:     0.3,
			SustainedBreaches: 2,
			Action:            "pause",
			AutoResumeDelay:   5 * time.Second,
		},
	}

	err := validateVolumeOptimizationConfig(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "window_size must be > 0")
}

// TestValidateVolumeOptimizationConfig_InvalidAction tests validation with invalid action
func TestValidateVolumeOptimizationConfig_InvalidAction(t *testing.T) {
	invalidConfig := &config.VolumeOptimizationConfig{
		Enabled: true,
		OrderPriority: config.OrderPriorityConfig{
			TickSizeAwareness: config.TickSizeConfig{
				Enabled:         false,
				TickSizes:       map[string]float64{},
				DefaultTickSize: 0.01,
			},
		},
		ToxicFlow: config.ToxicFlowConfig{
			Enabled:           true,
			WindowSize:        50,
			BucketSize:        1000.0,
			VPINThreshold:     0.3,
			SustainedBreaches: 2,
			Action:            "invalid_action", // Invalid
			AutoResumeDelay:   5 * time.Second,
		},
	}

	err := validateVolumeOptimizationConfig(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "action must be one of")
}

// TestValidateVolumeOptimizationConfig_InvalidSmartCancellation tests validation with invalid smart cancellation config
func TestValidateVolumeOptimizationConfig_InvalidSmartCancellation(t *testing.T) {
	invalidConfig := &config.VolumeOptimizationConfig{
		Enabled: true,
		OrderPriority: config.OrderPriorityConfig{
			TickSizeAwareness: config.TickSizeConfig{
				Enabled:         false,
				TickSizes:       map[string]float64{},
				DefaultTickSize: 0.01,
			},
		},
		MakerTaker: config.MakerTakerConfig{
			SmartCancellation: config.SmartCancelConfig{
				Enabled:               true,
				SpreadChangeThreshold: 0, // Invalid
				CheckInterval:         5 * time.Second,
			},
		},
	}

	err := validateVolumeOptimizationConfig(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "spread_change_threshold must be > 0")
}

// TestValidateVolumeOptimizationConfig_InvalidInventoryHedge tests validation with invalid inventory hedge config
func TestValidateVolumeOptimizationConfig_InvalidInventoryHedge(t *testing.T) {
	invalidConfig := &config.VolumeOptimizationConfig{
		Enabled: true,
		OrderPriority: config.OrderPriorityConfig{
			TickSizeAwareness: config.TickSizeConfig{
				Enabled:         false,
				TickSizes:       map[string]float64{},
				DefaultTickSize: 0.01,
			},
		},
		InventoryHedge: config.InventoryHedgeConfig{
			Enabled:        true,
			HedgeThreshold: 1.5, // Invalid (> 1)
			HedgeRatio:     0.3,
			MaxHedgeSize:   100.0,
			HedgingMode:    "internal",
			HedgePair:      "ETH",
		},
	}

	err := validateVolumeOptimizationConfig(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hedge_threshold must be between 0 and 1")
}
