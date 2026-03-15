package regime

import (
	"math"
)

// RegimeType defines the current market context.
type RegimeType string

const (
	RegimeTrend    RegimeType = "TRENDING"
	RegimeRanging  RegimeType = "RANGING"
	RegimeBreakout RegimeType = "BREAKOUT"
	RegimeUnknown  RegimeType = "UNKNOWN"
)

// Classifier tracks HTF data and classifies the market.
type Classifier struct {
	adxPeriod int
	bbPeriod  int
	bbStdDev  float64

	highs  []float64
	lows   []float64
	closes []float64

	// Cached values
	tr  []float64 // True Range
	pDM []float64 // +DM
	nDM []float64 // -DM
}

// NewClassifier creates a new regime classifier. Typical settings: ADX(14) and BB(20, 2).
func NewClassifier(adxPeriod, bbPeriod int, bbStdDev float64) *Classifier {
	// Need enough data to calculate smoothed ADX. Typically 2 * adxPeriod + 10 is safe.
	return &Classifier{
		adxPeriod: adxPeriod,
		bbPeriod:  bbPeriod,
		bbStdDev:  bbStdDev,
		highs:     make([]float64, 0),
		lows:      make([]float64, 0),
		closes:    make([]float64, 0),
		tr:        make([]float64, 0),
		pDM:       make([]float64, 0),
		nDM:       make([]float64, 0),
	}
}

// AddKline inserts a CLOSED kline into the classifier.
func (c *Classifier) AddKline(high, low, closePrice float64) {
	c.highs = append(c.highs, high)
	c.lows = append(c.lows, low)
	c.closes = append(c.closes, closePrice)

	// Keep buffer sizing bounded (e.g. max 100 periods)
	maxLen := 100
	if len(c.closes) > maxLen {
		c.highs = c.highs[1:]
		c.lows = c.lows[1:]
		c.closes = c.closes[1:]
	}

	c.recalcDM()
}

// recalcDM updates True Range and Directional Movement arrays.
func (c *Classifier) recalcDM() {
	if len(c.closes) < 2 {
		return
	}

	c.tr = make([]float64, len(c.closes))
	c.pDM = make([]float64, len(c.closes))
	c.nDM = make([]float64, len(c.closes))

	// TR and DM start at index 1
	for i := 1; i < len(c.closes); i++ {
		high := c.highs[i]
		low := c.lows[i]
		prevHigh := c.highs[i-1]
		prevLow := c.lows[i-1]
		prevClose := c.closes[i-1]

		// True Range
		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)
		c.tr[i] = math.Max(math.Max(tr1, tr2), tr3)

		// Directional Movement
		upMove := high - prevHigh
		downMove := prevLow - low

		if upMove > downMove && upMove > 0 {
			c.pDM[i] = upMove
		} else {
			c.pDM[i] = 0
		}

		if downMove > upMove && downMove > 0 {
			c.nDM[i] = downMove
		} else {
			c.nDM[i] = 0
		}
	}
}

// CurrentReturns the regime, ADX, and BB Width
func (c *Classifier) Current() (RegimeType, float64, float64) {
	if len(c.closes) < c.adxPeriod*2 {
		return RegimeUnknown, 0, 0
	}

	adx := c.calcADX()
	bbw := c.calcBBWidth()

	// Heuristics for Regime Classification
	// ADX > 25 indicates a strong trend.
	// Low BBW indicates squeeze (consolidation/ranging)
	if adx > 25 {
		return RegimeTrend, adx, bbw
	}

	// For simplicity, if not trending, it's ranging.
	// You can add more complex BB Squeeze logic here for "Breakout" later.
	return RegimeRanging, adx, bbw
}

// calcADX computes the Wilder's Smoothing ADX value.
func (c *Classifier) calcADX() float64 {
	// 1. Initial smoothed TR, +DM, -DM over 14 periods
	str := 0.0
	spDM := 0.0
	snDM := 0.0

	for i := 1; i <= c.adxPeriod; i++ {
		str += c.tr[i]
		spDM += c.pDM[i]
		snDM += c.nDM[i]
	}

	dx := make([]float64, len(c.closes))
	// 2. Loop to calculate subsequent smoothed values and DX
	for i := c.adxPeriod; i < len(c.closes); i++ {
		if i > c.adxPeriod {
			str = str - (str / float64(c.adxPeriod)) + c.tr[i]
			spDM = spDM - (spDM / float64(c.adxPeriod)) + c.pDM[i]
			snDM = snDM - (snDM / float64(c.adxPeriod)) + c.nDM[i]
		}

		pDI := 100 * (spDM / str)
		nDI := 100 * (snDM / str)

		diff := math.Abs(pDI - nDI)
		sum := pDI + nDI
		if sum == 0 {
			dx[i] = 0
		} else {
			dx[i] = 100 * (diff / sum)
		}
	}

	// 3. Smooth DX to get ADX
	adx := 0.0
	// Initial ADX is simple average of first 14 DX values
	startIdx := c.adxPeriod
	endIdx := startIdx + c.adxPeriod - 1
	if endIdx >= len(dx) {
		endIdx = len(dx) - 1
	}

	count := 0
	for i := startIdx; i <= endIdx; i++ {
		adx += dx[i]
		count++
	}
	if count > 0 {
		adx /= float64(count)
	}

	// Subsequent smoothed ADX
	for i := endIdx + 1; i < len(dx); i++ {
		adx = ((adx * float64(c.adxPeriod-1)) + dx[i]) / float64(c.adxPeriod)
	}

	return adx
}

// calcBBWidth calculates Bollinger Band Width = (Upper - Lower) / SMA
func (c *Classifier) calcBBWidth() float64 {
	if len(c.closes) < c.bbPeriod {
		return 0
	}

	periodData := c.closes[len(c.closes)-c.bbPeriod:]

	sum := 0.0
	for _, val := range periodData {
		sum += val
	}
	sma := sum / float64(c.bbPeriod)

	varianceSum := 0.0
	for _, val := range periodData {
		varianceSum += math.Pow(val-sma, 2)
	}
	variance := varianceSum / float64(c.bbPeriod)
	stdDev := math.Sqrt(variance)

	upper := sma + (c.bbStdDev * stdDev)
	lower := sma - (c.bbStdDev * stdDev)

	if sma == 0 {
		return 0
	}
	return (upper - lower) / sma * 100 // return percentage width
}
