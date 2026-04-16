package volume_optimization

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// VPINMonitor tracks buy/sell volume and detects toxic flow conditions
type VPINMonitor struct {
	windowSize           int
	bucketSize           float64
	buyVolume            []float64
	sellVolume           []float64
	currentBucket        int
	currentVol           float64
	bucketsFilled        int // Track number of buckets filled
	threshold            float64
	sustainedBreaches    int
	maxSustainedBreaches int
	isPaused             bool
	pauseStartTime       time.Time
	autoResumeDelay      time.Duration
	mu                   sync.RWMutex
	logger               *zap.Logger
}

// VPINConfig holds configuration for VPIN monitoring
type VPINConfig struct {
	WindowSize        int           // Number of buckets
	BucketSize        float64       // Volume per bucket
	VPINThreshold     float64       // VPIN threshold (0-1)
	SustainedBreaches int           // Number of consecutive breaches
	AutoResumeDelay   time.Duration // Delay before auto-resume
}

// NewVPINMonitor creates a new VPIN monitor
func NewVPINMonitor(config VPINConfig, logger *zap.Logger) *VPINMonitor {
	return &VPINMonitor{
		windowSize:           config.WindowSize,
		bucketSize:           config.BucketSize,
		buyVolume:            make([]float64, config.WindowSize),
		sellVolume:           make([]float64, config.WindowSize),
		threshold:            config.VPINThreshold,
		sustainedBreaches:    0,
		maxSustainedBreaches: config.SustainedBreaches,
		autoResumeDelay:      config.AutoResumeDelay,
		logger:               logger,
	}
}

// UpdateVolume updates buy/sell volume for VPIN calculation
func (v *VPINMonitor) UpdateVolume(buyVol, sellVol float64) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.currentVol += buyVol + sellVol

	if v.currentVol >= v.bucketSize {
		// Bucket is full, save accumulated volume and move to next bucket
		v.buyVolume[v.currentBucket] += buyVol
		v.sellVolume[v.currentBucket] += sellVol
		v.currentBucket = (v.currentBucket + 1) % v.windowSize
		v.currentVol = 0

		if v.bucketsFilled < v.windowSize {
			v.bucketsFilled++
		}

		v.logger.Debug("VPIN bucket filled",
			zap.Int("bucket", v.currentBucket),
			zap.Int("buckets_filled", v.bucketsFilled),
			zap.Float64("buy_vol", buyVol),
			zap.Float64("sell_vol", sellVol))
	}
}

// CalculateVPIN calculates the current VPIN value
// VPIN = |Buy - Sell| / (Buy + Sell)
func (v *VPINMonitor) CalculateVPIN() float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.bucketsFilled < v.windowSize {
		// Not enough data yet
		return 0
	}

	totalBuy := 0.0
	totalSell := 0.0
	for i := 0; i < v.windowSize; i++ {
		totalBuy += v.buyVolume[i]
		totalSell += v.sellVolume[i]
	}

	totalVolume := totalBuy + totalSell
	if totalVolume == 0 {
		return 0
	}

	vpin := math.Abs(totalBuy-totalSell) / totalVolume
	return vpin
}

// IsToxic checks if current VPIN indicates toxic flow
func (v *VPINMonitor) IsToxic() bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.isPaused {
		// Check if we should auto-resume
		if time.Since(v.pauseStartTime) > v.autoResumeDelay {
			v.logger.Info("Auto-resuming from toxic flow pause")
			v.isPaused = false
			v.sustainedBreaches = 0
			return false
		}
		return true
	}

	vpin := v.CalculateVPIN()
	if vpin > v.threshold {
		v.logger.Warn("Toxic flow detected",
			zap.Float64("vpin", vpin),
			zap.Float64("threshold", v.threshold))
		return true
	}

	return false
}

// TriggerPause pauses trading due to toxic flow
func (v *VPINMonitor) TriggerPause() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.isPaused = true
	v.pauseStartTime = time.Now()
	v.sustainedBreaches++

	v.logger.Warn("Trading paused due to toxic flow",
		zap.Int("sustained_breaches", v.sustainedBreaches),
		zap.Time("pause_start", v.pauseStartTime))
}

// Resume resumes trading after toxic flow clears
func (v *VPINMonitor) Resume() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.isPaused = false
	v.sustainedBreaches = 0

	v.logger.Info("Trading resumed after toxic flow cleared")
}

// GetVPIN returns the current VPIN value
func (v *VPINMonitor) GetVPIN() float64 {
	return v.CalculateVPIN()
}

// IsPaused returns whether trading is currently paused
func (v *VPINMonitor) IsPaused() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.isPaused
}

// Reset resets the VPIN monitor state
func (v *VPINMonitor) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.buyVolume = make([]float64, v.windowSize)
	v.sellVolume = make([]float64, v.windowSize)
	v.currentBucket = 0
	v.currentVol = 0
	v.bucketsFilled = 0
	v.sustainedBreaches = 0
	v.isPaused = false

	v.logger.Info("VPIN monitor reset")
}

// GetStats returns current VPIN statistics
func (v *VPINMonitor) GetStats() map[string]interface{} {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return map[string]interface{}{
		"vpin":               v.CalculateVPIN(),
		"threshold":          v.threshold,
		"is_paused":          v.isPaused,
		"sustained_breaches": v.sustainedBreaches,
		"current_bucket":     v.currentBucket,
		"buckets_filled":     v.bucketsFilled,
		"window_size":        v.windowSize,
	}
}
