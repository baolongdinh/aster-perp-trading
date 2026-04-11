package agent

import (
	"math"
)

// IndicatorCalculator provides technical indicator calculations
type IndicatorCalculator struct {
	adxPeriod  int
	bbPeriod   int
	atrPeriod  int
	emaPeriods []int
}

// NewIndicatorCalculator creates a new calculator with default periods
func NewIndicatorCalculator() *IndicatorCalculator {
	return &IndicatorCalculator{
		adxPeriod:  14,
		bbPeriod:   20,
		atrPeriod:  14,
		emaPeriods: []int{9, 21, 50, 200},
	}
}

// Candle represents a single candlestick for technical analysis
type Candle struct {
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// CalculateADX calculates the Average Directional Index
func (ic *IndicatorCalculator) CalculateADX(candles []Candle, period int) float64 {
	if len(candles) < period*2 {
		return 0
	}

	// Calculate +DM and -DM
	plusDM := make([]float64, len(candles))
	minusDM := make([]float64, len(candles))
	tr := make([]float64, len(candles))

	for i := 1; i < len(candles); i++ {
		highDiff := candles[i].High - candles[i-1].High
		lowDiff := candles[i-1].Low - candles[i].Low

		if highDiff > lowDiff && highDiff > 0 {
			plusDM[i] = highDiff
		}
		if lowDiff > highDiff && lowDiff > 0 {
			minusDM[i] = lowDiff
		}

		// True Range
		tr[i] = math.Max(candles[i].High-candles[i].Low,
			math.Max(math.Abs(candles[i].High-candles[i-1].Close),
				math.Abs(candles[i].Low-candles[i-1].Close)))
	}

	// Smooth DM and TR
	smoothPlusDM := ic.smoothSlice(plusDM, period)
	smoothMinusDM := ic.smoothSlice(minusDM, period)
	smoothTR := ic.smoothSlice(tr, period)

	if len(smoothTR) == 0 || smoothTR[len(smoothTR)-1] == 0 {
		return 0
	}

	// Calculate +DI and -DI
	lastIdx := len(smoothPlusDM) - 1
	plusDI := 100 * smoothPlusDM[lastIdx] / smoothTR[lastIdx]
	minusDI := 100 * smoothMinusDM[lastIdx] / smoothTR[lastIdx]

	// Calculate DX
	dx := 100 * math.Abs(plusDI-minusDI) / (plusDI + minusDI)
	if math.IsNaN(dx) {
		return 0
	}

	// Smooth DX to get ADX (simplified - using last value)
	return dx
}

// CalculateBollingerBands calculates Bollinger Bands and returns width
func (ic *IndicatorCalculator) CalculateBollingerBands(candles []Candle, period int, stdDev float64) (upper, middle, lower, width float64) {
	if len(candles) < period {
		return 0, 0, 0, 0
	}

	// Calculate SMA (middle band)
	var sum float64
	start := len(candles) - period
	for i := start; i < len(candles); i++ {
		sum += candles[i].Close
	}
	middle = sum / float64(period)

	// Calculate standard deviation
	var variance float64
	for i := start; i < len(candles); i++ {
		diff := candles[i].Close - middle
		variance += diff * diff
	}
	std := math.Sqrt(variance / float64(period))

	// Calculate bands
	upper = middle + stdDev*std
	lower = middle - stdDev*std

	// Calculate width as percentage
	if middle > 0 {
		width = (upper - lower) / middle * 100
	}

	return upper, middle, lower, width
}

// CalculateATR calculates Average True Range
func (ic *IndicatorCalculator) CalculateATR(candles []Candle, period int) float64 {
	if len(candles) < period+1 {
		return 0
	}

	tr := make([]float64, len(candles))
	for i := 1; i < len(candles); i++ {
		tr[i] = math.Max(candles[i].High-candles[i].Low,
			math.Max(math.Abs(candles[i].High-candles[i-1].Close),
				math.Abs(candles[i].Low-candles[i-1].Close)))
	}

	// Simple moving average of TR
	return ic.calculateSMA(tr[len(tr)-period:], period)
}

// CalculateEMA calculates Exponential Moving Average
func (ic *IndicatorCalculator) CalculateEMA(candles []Candle, period int) float64 {
	if len(candles) < period {
		return 0
	}

	// Calculate SMA for first value
	var sma float64
	for i := 0; i < period; i++ {
		sma += candles[i].Close
	}
	sma /= float64(period)

	// Calculate multiplier
	multiplier := 2.0 / (float64(period) + 1.0)

	// Calculate EMA
	ema := sma
	for i := period; i < len(candles); i++ {
		ema = (candles[i].Close-ema)*multiplier + ema
	}

	return ema
}

// CalculateVolumeMA calculates Volume Moving Average
func (ic *IndicatorCalculator) CalculateVolumeMA(candles []Candle, period int) float64 {
	if len(candles) < period {
		return 0
	}

	var sum float64
	start := len(candles) - period
	for i := start; i < len(candles); i++ {
		sum += candles[i].Volume
	}

	return sum / float64(period)
}

// CalculateAllEMAs calculates all required EMAs (9, 21, 50, 200)
func (ic *IndicatorCalculator) CalculateAllEMAs(candles []Candle) map[int]float64 {
	result := make(map[int]float64)
	for _, period := range ic.emaPeriods {
		result[period] = ic.CalculateEMA(candles, period)
	}
	return result
}

// IsEMALinedUp checks if EMAs are in bullish alignment (9 > 21 > 50 > 200)
func (ic *IndicatorCalculator) IsEMALinedUp(emas map[int]float64) bool {
	return emas[9] > emas[21] && emas[21] > emas[50] && emas[50] > emas[200]
}

// IsEMALinedDown checks if EMAs are in bearish alignment (9 < 21 < 50 < 200)
func (ic *IndicatorCalculator) IsEMALinedDown(emas map[int]float64) bool {
	return emas[9] < emas[21] && emas[21] < emas[50] && emas[50] < emas[200]
}

// smoothSlice applies Wilder's smoothing to a slice
func (ic *IndicatorCalculator) smoothSlice(data []float64, period int) []float64 {
	if len(data) < period {
		return []float64{0}
	}

	result := make([]float64, len(data))

	// First value is simple sum
	var sum float64
	for i := 0; i < period && i < len(data); i++ {
		sum += data[i]
	}
	result[period-1] = sum

	// Remaining values use smoothing
	for i := period; i < len(data); i++ {
		result[i] = result[i-1] - (result[i-1] / float64(period)) + data[i]
	}

	return result
}

// calculateSMA calculates Simple Moving Average
func (ic *IndicatorCalculator) calculateSMA(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}

	var sum float64
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}

	return sum / float64(period)
}

// CalculateAll calculates all indicators at once
func (ic *IndicatorCalculator) CalculateAll(candles []Candle) *IndicatorValues {
	if len(candles) < 200 {
		return nil
	}

	adx := ic.CalculateADX(candles, ic.adxPeriod)
	_, _, _, bbWidth := ic.CalculateBollingerBands(candles, ic.bbPeriod, 2.0)
	atr := ic.CalculateATR(candles, ic.atrPeriod)
	volMA := ic.CalculateVolumeMA(candles, 20)
	emas := ic.CalculateAllEMAs(candles)

	return &IndicatorValues{
		ADX:           adx,
		BBWidth:       bbWidth,
		ATR14:         atr,
		VolumeMA20:    volMA,
		CurrentVolume: candles[len(candles)-1].Volume,
		EMA9:          emas[9],
		EMA21:         emas[21],
		EMA50:         emas[50],
		EMA200:        emas[200],
		IsBullish:     ic.IsEMALinedUp(emas),
		IsBearish:     ic.IsEMALinedDown(emas),
	}
}

// IndicatorValues holds all calculated indicator values
type IndicatorValues struct {
	ADX           float64
	BBWidth       float64
	ATR14         float64
	VolumeMA20    float64
	CurrentVolume float64
	EMA9          float64
	EMA21         float64
	EMA50         float64
	EMA200        float64
	IsBullish     bool
	IsBearish     bool
}

// CalculateATRSpike checks if current ATR is spiking (> threshold × average)
func (iv *IndicatorValues) CalculateATRSpike(atrHistory []float64, threshold float64) bool {
	if len(atrHistory) < 20 || iv.ATR14 == 0 {
		return false
	}

	// Calculate average ATR (excluding recent values)
	var sum float64
	for i := 0; i < len(atrHistory)-5; i++ {
		sum += atrHistory[i]
	}
	avgATR := sum / float64(len(atrHistory)-5)

	if avgATR == 0 {
		return false
	}

	return iv.ATR14/avgATR >= threshold
}
