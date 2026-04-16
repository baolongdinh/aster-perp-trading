package adaptive_grid

import (
	"testing"
	"time"

	"aster-bot/internal/farming/volume_optimization"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestVPINMonitor_GetPauseStartTime tests GetPauseStartTime method
func TestVPINMonitor_GetPauseStartTime(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := volume_optimization.VPINConfig{
		WindowSize:        50,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}
	vpinMonitor := volume_optimization.NewVPINMonitor(config, logger)

	// Before pause, should be zero time
	pauseStart := vpinMonitor.GetPauseStartTime()
	assert.True(t, pauseStart.IsZero())

	// After pause, should have valid time
	vpinMonitor.TriggerPause()
	pauseStart = vpinMonitor.GetPauseStartTime()
	assert.False(t, pauseStart.IsZero())
}

// TestVPINMonitor_GetAutoResumeDelay tests GetAutoResumeDelay method
func TestVPINMonitor_GetAutoResumeDelay(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := volume_optimization.VPINConfig{
		WindowSize:        50,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}
	vpinMonitor := volume_optimization.NewVPINMonitor(config, logger)

	delay := vpinMonitor.GetAutoResumeDelay()
	assert.Equal(t, 5*time.Second, delay)
}

// TestAdaptiveGridManager_VPINPauseTrigger tests VPIN pause trigger in CanPlaceOrder
func TestAdaptiveGridManager_VPINPauseTrigger(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	// Create AdaptiveGridManager (minimal setup for testing)
	// Note: This is a simplified test focusing on VPIN integration
	// Full integration test would require more setup

	config := volume_optimization.VPINConfig{
		WindowSize:        50,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}
	vpinMonitor := volume_optimization.NewVPINMonitor(config, logger)

	// Trigger pause
	vpinMonitor.TriggerPause()

	// Verify pause state
	assert.True(t, vpinMonitor.IsPaused())
	assert.False(t, vpinMonitor.GetPauseStartTime().IsZero())

	// Test auto-resume logic
	time.Sleep(100 * time.Millisecond) // Small delay

	// Still should be paused (not enough time)
	assert.True(t, vpinMonitor.IsPaused())

	// Manually resume to test
	vpinMonitor.Resume()
	assert.False(t, vpinMonitor.IsPaused())
}

// TestAdaptiveGridManager_VPINPauseResumeCycle tests full pause/resume cycle
func TestAdaptiveGridManager_VPINPauseResumeCycle(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := volume_optimization.VPINConfig{
		WindowSize:        50,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   1 * time.Second, // Short delay for testing
	}
	vpinMonitor := volume_optimization.NewVPINMonitor(config, logger)

	// Initial state
	assert.False(t, vpinMonitor.IsPaused())

	// Trigger pause
	vpinMonitor.TriggerPause()
	assert.True(t, vpinMonitor.IsPaused())

	pauseStart := vpinMonitor.GetPauseStartTime()
	assert.False(t, pauseStart.IsZero())

	// Wait for auto-resume delay
	time.Sleep(1100 * time.Millisecond)

	// Check if auto-resumed
	vpinMonitor.IsToxic() // This will trigger auto-resume check internally

	// After delay, should be auto-resumed
	assert.False(t, vpinMonitor.IsPaused())
}
