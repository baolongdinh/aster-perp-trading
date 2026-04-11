package agent

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Detector performs market regime detection
type Detector struct {
	config        *RegimeDetectionConfig
	calculator    *IndicatorCalculator
	currentRegime RegimeSnapshot
	candleBuffer  []Candle
	atrHistory    []float64
	lastUpdate    time.Time
	mu            sync.RWMutex

	// Hysteresis tracking (T023a)
	lastDetectedRegime  RegimeType
	consecutiveCount    int
	hysteresisThreshold int // Require 2 consecutive readings (default)
}

// NewDetector creates a new regime detector
func NewDetector(config *RegimeDetectionConfig) *Detector {
	return &Detector{
		config:              config,
		calculator:          NewIndicatorCalculator(),
		candleBuffer:        make([]Candle, 0, 1000),
		atrHistory:          make([]float64, 0, 100),
		hysteresisThreshold: 2, // T023a: Require 2 consecutive readings
	}
}

// Detect performs regime detection and returns the current regime
func (d *Detector) Detect() (RegimeSnapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Need at least 200 candles for EMA200
	if len(d.candleBuffer) < 200 {
		return RegimeSnapshot{
			ID:         uuid.New(),
			Timestamp:  time.Now(),
			Regime:     RegimeUnknown,
			Confidence: 0,
		}, nil
	}

	// Calculate all indicators
	values := d.calculator.CalculateAll(d.candleBuffer)
	if values == nil {
		return RegimeSnapshot{
			ID:         uuid.New(),
			Timestamp:  time.Now(),
			Regime:     RegimeUnknown,
			Confidence: 0,
		}, nil
	}

	// Update ATR history for spike detection
	d.updateATRHistory(values.ATR14)

	// Detect regime based on indicators
	detectedRegime, confidence := d.classifyRegime(values)

	// T023a: Hysteresis - require 2 consecutive readings to change regime
	finalRegime := d.applyHysteresis(detectedRegime)

	snapshot := RegimeSnapshot{
		ID:         uuid.New(),
		Timestamp:  time.Now(),
		Regime:     finalRegime,
		Confidence: confidence,
		Indicators: IndicatorSnapshot{
			ADX:           values.ADX,
			BBWidth:       values.BBWidth,
			ATR14:         values.ATR14,
			VolumeMA20:    values.VolumeMA20,
			CurrentVolume: values.CurrentVolume,
			EMA9:          values.EMA9,
			EMA21:         values.EMA21,
			EMA50:         values.EMA50,
			EMA200:        values.EMA200,
		},
		DetectedAt: time.Now(),
	}

	d.currentRegime = snapshot
	d.lastUpdate = time.Now()

	return snapshot, nil
}

// applyHysteresis implements T023a: require 2 consecutive readings to change regime
func (d *Detector) applyHysteresis(detectedRegime RegimeType) RegimeType {
	// If same as last detected, increment counter
	if detectedRegime == d.lastDetectedRegime {
		d.consecutiveCount++
	} else {
		// Different regime detected, reset counter
		d.lastDetectedRegime = detectedRegime
		d.consecutiveCount = 1
	}

	// Only change if we have enough consecutive readings
	if d.consecutiveCount >= d.hysteresisThreshold {
		return detectedRegime
	}

	// Return current regime (no change yet)
	return d.currentRegime.Regime
}

// GetCurrent returns the current regime snapshot
func (d *Detector) GetCurrent() RegimeSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentRegime
}

// Update adds a new candle to the buffer
func (d *Detector) Update(candle Candle) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.candleBuffer = append(d.candleBuffer, candle)

	// Keep buffer size manageable (max 1000 candles for pattern detection)
	if len(d.candleBuffer) > 1000 {
		d.candleBuffer = d.candleBuffer[len(d.candleBuffer)-1000:]
	}
}

// UpdateCandles replaces the entire candle buffer
func (d *Detector) UpdateCandles(candles []Candle) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(candles) > 1000 {
		d.candleBuffer = candles[len(candles)-1000:]
	} else {
		d.candleBuffer = make([]Candle, len(candles))
		copy(d.candleBuffer, candles)
	}
}

// updateATRHistory maintains rolling ATR history
func (d *Detector) updateATRHistory(atr float64) {
	d.atrHistory = append(d.atrHistory, atr)
	if len(d.atrHistory) > 50 {
		d.atrHistory = d.atrHistory[len(d.atrHistory)-50:]
	}
}

// classifyRegime determines the market regime based on indicator values
func (d *Detector) classifyRegime(values *IndicatorValues) (RegimeType, float64) {
	// Priority 1: Check for volatile regime (ATR spike)
	if d.isVolatile(values) {
		return RegimeVolatile, 90.0
	}

	// Priority 2: Check for trending regime
	if trendConfidence := d.isTrending(values); trendConfidence > 50 {
		return RegimeTrending, trendConfidence
	}

	// Priority 3: Check for recovery (post-volatile normalization)
	if d.isRecovery(values) {
		return RegimeRecovery, 75.0
	}

	// Priority 4: Sideways (default)
	if sidewaysConfidence := d.isSideways(values); sidewaysConfidence > 50 {
		return RegimeSideways, sidewaysConfidence
	}

	return RegimeUnknown, 50.0
}

// isVolatile checks for volatile regime conditions
func (d *Detector) isVolatile(values *IndicatorValues) bool {
	// Check ATR spike > 3× average
	if len(d.atrHistory) >= 20 {
		var avgATR float64
		for i := 0; i < len(d.atrHistory)-5; i++ {
			avgATR += d.atrHistory[i]
		}
		avgATR /= float64(len(d.atrHistory) - 5)

		if avgATR > 0 && values.ATR14/avgATR >= d.config.Thresholds.VolatileATRSpike {
			return true
		}
	}

	// Check extreme BB expansion
	if values.BBWidth > 8.0 { // > 8% bandwidth is extreme
		return true
	}

	return false
}

// isTrending checks for trending regime conditions
func (d *Detector) isTrending(values *IndicatorValues) float64 {
	// ADX > 25 indicates trend strength
	if values.ADX < d.config.Thresholds.TrendingADXMin {
		return 0
	}

	// Check EMA alignment
	confidence := (values.ADX - 25) * 2 // Scale ADX 25-75 to 0-100

	if values.IsBullish || values.IsBearish {
		confidence += 20 // Bonus for clear EMA alignment
	}

	if confidence > 100 {
		confidence = 100
	}

	return confidence
}

// isSideways checks for sideways regime conditions
func (d *Detector) isSideways(values *IndicatorValues) float64 {
	// ADX < 25 indicates weak trend (sideways)
	if values.ADX > d.config.Thresholds.SidewaysADXMax {
		return 0
	}

	confidence := (25 - values.ADX) * 3 // Scale ADX 0-25 to 75-0

	// Bonus for narrow BB width
	if values.BBWidth < 3.0 {
		confidence += 15
	}

	// Bonus for low volatility
	if len(d.atrHistory) > 20 {
		var avgATR float64
		for _, atr := range d.atrHistory {
			avgATR += atr
		}
		avgATR /= float64(len(d.atrHistory))

		if avgATR > 0 && values.ATR14/avgATR < 0.5 {
			confidence += 10 // Low ATR relative to average
		}
	}

	if confidence > 100 {
		confidence = 100
	}

	return confidence
}

// isRecovery checks for recovery regime (post-volatile normalization)
func (d *Detector) isRecovery(values *IndicatorValues) bool {
	if len(d.atrHistory) < 10 {
		return false
	}

	// Check if ATR is normalizing (decreasing from high values)
	recentATR := values.ATR14
	var maxRecentATR float64
	for i := len(d.atrHistory) - 10; i < len(d.atrHistory); i++ {
		if i >= 0 && d.atrHistory[i] > maxRecentATR {
			maxRecentATR = d.atrHistory[i]
		}
	}

	// Recovery = ATR dropped significantly from recent peak
	if maxRecentATR > 0 && recentATR < maxRecentATR*0.6 {
		// And current ADX suggests trendless (consolidation after volatility)
		if values.ADX < 30 {
			return true
		}
	}

	return false
}

// GetLastUpdate returns the time of last regime update
func (d *Detector) GetLastUpdate() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastUpdate
}

// GetCandleCount returns the number of candles in buffer
func (d *Detector) GetCandleCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.candleBuffer)
}
