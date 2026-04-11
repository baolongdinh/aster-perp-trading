package agentic

import (
	"math"
)

// IndicatorCalculator provides technical indicator calculations
type IndicatorCalculator struct {
	adxPeriod  int
	bbPeriod   int
	atrPeriod  int
}

// NewIndicatorCalculator creates a new calculator with specified periods
func NewIndicatorCalculator(adxPeriod, bbPeriod, atrPeriod int) *IndicatorCalculator {
	return &IndicatorCalculator{
		adxPeriod: adxPeriod,
		bbPeriod:  bbPeriod,
		atrPeriod: atrPeriod,
	}
}

// CalculateAll calculates all indicators for a series of candles
func (ic *IndicatorCalculator) CalculateAll(candles []Candle) *IndicatorValues {
	if len(candles) == 0 {
		return &IndicatorValues{}
	}

	// Extract close prices, highs, lows
	closes := make([]float64, len(candles))
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	volumes := make([]float64, len(candles))

	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
		volumes[i] = c.Volume
	}

	// Calculate indicators
	bbUpper, bbLower, bbMiddle := ic.calculateBollingerBands(closes)
	atr := ic.calculateATR(highs, lows, closes)
	adx := ic.calculateADX(highs, lows, closes)

	// Volume 24h (sum of all available volume)
	var volume24h float64
	for _, v := range volumes {
		volume24h += v
	}

	// Price change from first to last
	var priceChange float64
	if len(closes) > 0 && closes[0] != 0 {
		priceChange = (closes[len(closes)-1] - closes[0]) / closes[0] * 100
	}

	// BB Width
	bbWidth := 0.0
	if bbMiddle != 0 {
		bbWidth = (bbUpper - bbLower) / bbMiddle
	}

	return &IndicatorValues{
		ADX:         adx,
		ATR14:       atr,
		BBUpper:     bbUpper,
		BBLower:     bbLower,
		BBMiddle:    bbMiddle,
		BBWidth:     bbWidth,
		Volume24h:   volume24h,
		PriceChange: priceChange,
	}
}

// calculateBollingerBands calculates Bollinger Bands
func (ic *IndicatorCalculator) calculateBollingerBands(closes []float64) (upper, lower, middle float64) {
	period := ic.bbPeriod
	if len(closes) < period {
		period = len(closes)
	}
	if period == 0 {
		return 0, 0, 0
	}

	// Calculate middle band (SMA)
	sum := 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		sum += closes[i]
	}
	middle = sum / float64(period)

	// Calculate standard deviation
	variance := 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		diff := closes[i] - middle
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	// Upper and lower bands (2 standard deviations)
	upper = middle + 2*stdDev
	lower = middle - 2*stdDev

	return upper, lower, middle
}

// calculateATR calculates Average True Range
func (ic *IndicatorCalculator) calculateATR(highs, lows, closes []float64) float64 {
	period := ic.atrPeriod
	if len(highs) < 2 || len(lows) < 2 {
		return 0
	}

	// Calculate True Ranges
	trueRanges := make([]float64, 0, len(highs)-1)
	for i := 1; i < len(highs); i++ {
		// Current high - current low
		highLow := highs[i] - lows[i]
		// |Current high - previous close|
		highClose := math.Abs(highs[i] - closes[i-1])
		// |Current low - previous close|
		lowClose := math.Abs(lows[i] - closes[i-1])

		// True Range is the maximum of the three
		tr := highLow
		if highClose > tr {
			tr = highClose
		}
		if lowClose > tr {
			tr = lowClose
		}
		trueRanges = append(trueRanges, tr)
	}

	// Calculate ATR (simple moving average of TR)
	if len(trueRanges) < period {
		period = len(trueRanges)
	}
	if period == 0 {
		return 0
	}

	sum := 0.0
	for i := len(trueRanges) - period; i < len(trueRanges); i++ {
		sum += trueRanges[i]
	}

	return sum / float64(period)
}

// calculateADX calculates Average Directional Index
func (ic *IndicatorCalculator) calculateADX(highs, lows, closes []float64) float64 {
	period := ic.adxPeriod
	if len(highs) < period*2 || len(lows) < period*2 {
		// Not enough data, return neutral value
		return 20.0
	}

	// Calculate +DM and -DM
	plusDM := make([]float64, len(highs)-1)
	minusDM := make([]float64, len(lows)-1)
	trueRanges := make([]float64, len(highs)-1)

	for i := 1; i < len(highs); i++ {
		// True Range
		highLow := highs[i] - lows[i]
		highClose := math.Abs(highs[i] - closes[i-1])
		lowClose := math.Abs(lows[i] - closes[i-1])
		tr := highLow
		if highClose > tr {
			tr = highClose
		}
		if lowClose > tr {
			tr = lowClose
		}
		trueRanges[i-1] = tr

		// +DM and -DM
		highDiff := highs[i] - highs[i-1]
		lowDiff := lows[i-1] - lows[i]

		if highDiff > lowDiff && highDiff > 0 {
			plusDM[i-1] = highDiff
		} else {
			plusDM[i-1] = 0
		}

		if lowDiff > highDiff && lowDiff > 0 {
			minusDM[i-1] = lowDiff
		} else {
			minusDM[i-1] = 0
		}
	}

	// Calculate smoothed +DI and -DI
	smoothedPlusDI := ic.smoothSeries(plusDM, trueRanges, period)
	smoothedMinusDI := ic.smoothSeries(minusDM, trueRanges, period)

	// Calculate DX
	dxSum := 0.0
	count := 0
	for i := len(smoothedPlusDI) - period; i < len(smoothedPlusDI); i++ {
		sumDI := smoothedPlusDI[i] + smoothedMinusDI[i]
		if sumDI > 0 {
			diff := math.Abs(smoothedPlusDI[i] - smoothedMinusDI[i])
			dxSum += (diff / sumDI) * 100
			count++
		}
	}

	if count == 0 {
		return 20.0
	}

	return dxSum / float64(count)
}

// smoothSeries calculates smoothed values (Wilder's smoothing)
func (ic *IndicatorCalculator) smoothSeries(dm, tr []float64, period int) []float64 {
	if len(dm) != len(tr) || len(dm) < period {
		return nil
	}

	smoothedDM := make([]float64, len(dm)-period+1)
	smoothedTR := make([]float64, len(tr)-period+1)

	// Initial values (sum of first period)
	sumDM := 0.0
	sumTR := 0.0
	for i := 0; i < period; i++ {
		sumDM += dm[i]
		sumTR += tr[i]
	}
	smoothedDM[0] = sumDM
	smoothedTR[0] = sumTR

	// Apply smoothing formula
	multiplier := (float64(period) - 1) / float64(period)
	for i := 1; i < len(smoothedDM); i++ {
		smoothedDM[i] = smoothedDM[i-1]*multiplier + dm[i+period-1]
		smoothedTR[i] = smoothedTR[i-1]*multiplier + tr[i+period-1]
	}

	// Calculate DI values
	di := make([]float64, len(smoothedDM))
	for i := 0; i < len(di); i++ {
		if smoothedTR[i] > 0 {
			di[i] = (smoothedDM[i] / smoothedTR[i]) * 100
		} else {
			di[i] = 0
		}
	}

	return di
}
