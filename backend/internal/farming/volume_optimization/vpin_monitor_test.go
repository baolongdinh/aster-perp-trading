package volume_optimization

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewVPINMonitor(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        50,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}

	vpin := NewVPINMonitor(config, logger)

	if vpin.windowSize != 50 {
		t.Errorf("Expected window size 50, got %d", vpin.windowSize)
	}
	if vpin.threshold != 0.3 {
		t.Errorf("Expected threshold 0.3, got %v", vpin.threshold)
	}
}

func TestUpdateVolume(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}

	vpin := NewVPINMonitor(config, logger)

	// Add volume less than bucket size - should accumulate
	vpin.UpdateVolume(500.0, 500.0)
	stats := vpin.GetStats()
	bucketsFilled := stats["buckets_filled"].(int)
	if bucketsFilled != 0 {
		t.Errorf("Expected 0 buckets filled, got %d", bucketsFilled)
	}

	// Add volume to fill bucket - should trigger bucket fill
	vpin.UpdateVolume(500.0, 500.0)
	stats = vpin.GetStats()
	bucketsFilled = stats["buckets_filled"].(int)
	if bucketsFilled != 1 {
		t.Errorf("Expected 1 bucket filled, got %d", bucketsFilled)
	}
}

func TestCalculateVPIN(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}

	vpin := NewVPINMonitor(config, logger)

	// Not enough data
	vpinValue := vpin.CalculateVPIN()
	if vpinValue != 0 {
		t.Errorf("Expected VPIN 0 with insufficient data, got %v", vpinValue)
	}

	// Fill all buckets with balanced volume
	for i := 0; i < 3; i++ {
		vpin.UpdateVolume(500.0, 500.0)
	}

	vpinValue = vpin.CalculateVPIN()
	if vpinValue != 0 {
		t.Errorf("Expected VPIN 0 for balanced volume, got %v", vpinValue)
	}

	// Fill with imbalanced volume
	vpin.Reset()
	for i := 0; i < 3; i++ {
		vpin.UpdateVolume(800.0, 200.0)
	}

	vpinValue = vpin.CalculateVPIN()
	expected := 0.6 // |2400 - 600| / 3000 = 0.6
	if vpinValue != expected {
		t.Errorf("Expected VPIN %v, got %v", expected, vpinValue)
	}
}

func TestIsToxic(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}

	vpin := NewVPINMonitor(config, logger)

	// Fill with balanced volume (not toxic)
	for i := 0; i < 3; i++ {
		vpin.UpdateVolume(500.0, 500.0)
	}

	if vpin.IsToxic() {
		t.Error("Expected not toxic for balanced volume")
	}

	// Fill with highly imbalanced volume (toxic)
	vpin.Reset()
	for i := 0; i < 3; i++ {
		vpin.UpdateVolume(900.0, 100.0)
	}

	if !vpin.IsToxic() {
		t.Error("Expected toxic for imbalanced volume")
	}
}

func TestTriggerPause(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   1 * time.Second, // Short for testing
	}

	vpin := NewVPINMonitor(config, logger)

	vpin.TriggerPause()

	if !vpin.IsPaused() {
		t.Error("Expected paused after trigger")
	}
	if vpin.sustainedBreaches != 1 {
		t.Errorf("Expected 1 sustained breach, got %d", vpin.sustainedBreaches)
	}
}

func TestAutoResume(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   100 * time.Millisecond, // Short for testing
	}

	vpin := NewVPINMonitor(config, logger)

	vpin.TriggerPause()

	// Should still be paused immediately
	if !vpin.IsPaused() {
		t.Error("Expected paused immediately after trigger")
	}

	// Wait for auto-resume delay
	time.Sleep(150 * time.Millisecond)

	// Should auto-resume
	if vpin.IsPaused() {
		t.Error("Expected auto-resume after delay")
	}
}

func TestResume(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}

	vpin := NewVPINMonitor(config, logger)

	vpin.TriggerPause()
	vpin.Resume()

	if vpin.IsPaused() {
		t.Error("Expected not paused after resume")
	}
	if vpin.sustainedBreaches != 0 {
		t.Errorf("Expected 0 sustained breaches after resume, got %d", vpin.sustainedBreaches)
	}
}

func TestReset(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}

	vpin := NewVPINMonitor(config, logger)

	// Add some data
	for i := 0; i < 3; i++ {
		vpin.UpdateVolume(500.0, 500.0)
	}
	vpin.TriggerPause()

	// Reset
	vpin.Reset()

	if vpin.currentBucket != 0 {
		t.Errorf("Expected bucket index 0 after reset, got %d", vpin.currentBucket)
	}
	if vpin.isPaused {
		t.Error("Expected not paused after reset")
	}
	if vpin.sustainedBreaches != 0 {
		t.Errorf("Expected 0 sustained breaches after reset, got %d", vpin.sustainedBreaches)
	}
}

func TestGetStats(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}

	vpin := NewVPINMonitor(config, logger)

	stats := vpin.GetStats()

	if stats["threshold"] != 0.3 {
		t.Errorf("Expected threshold 0.3, got %v", stats["threshold"])
	}
	if stats["window_size"] != 3 {
		t.Errorf("Expected window size 3, got %v", stats["window_size"])
	}
}

func TestVPINConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	config := VPINConfig{
		WindowSize:        3,
		BucketSize:        1000.0,
		VPINThreshold:     0.3,
		SustainedBreaches: 2,
		AutoResumeDelay:   5 * time.Second,
	}

	vpin := NewVPINMonitor(config, logger)

	// Test concurrent access
	done := make(chan bool)

	// Goroutine 1: Update volume
	go func() {
		for i := 0; i < 100; i++ {
			vpin.UpdateVolume(500.0, 500.0)
		}
		done <- true
	}()

	// Goroutine 2: Calculate VPIN
	go func() {
		for i := 0; i < 100; i++ {
			vpin.CalculateVPIN()
		}
		done <- true
	}()

	// Goroutine 3: Check toxic
	go func() {
		for i := 0; i < 100; i++ {
			vpin.IsToxic()
		}
		done <- true
	}()

	<-done
	<-done
	<-done

	// If we get here without panic, concurrent access works
}
