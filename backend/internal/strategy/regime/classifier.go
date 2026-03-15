package regime

import (
	"math"
	"sync"
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
	mu        sync.RWMutex
	adxPeriod int
	bbPeriod  int
	bbStdDev  float64

	// Multi-timeframe data
	highs  map[string][]float64 // timeframe -> history
	lows   map[string][]float64
	closes map[string][]float64
	volumes map[string][]float64

	// Cached values for 5m (primary timeframe)
	tr  []float64 // True Range
	pDM []float64 // +DM
	nDM []float64 // -DM

	// BB Width history for squeeze detection (primary timeframe)
	bbwHistory []float64

	// Sentiment/Context data for this specific symbol
	fundingRate float64
}


// NewClassifier creates a new regime classifier. Typical settings: ADX(14) and BB(20, 2).
func NewClassifier(adxPeriod, bbPeriod int, bbStdDev float64) *Classifier {
	return &Classifier{
		adxPeriod:    adxPeriod,
		bbPeriod:     bbPeriod,
		bbStdDev:     bbStdDev,
		highs:        make(map[string][]float64),
		lows:         make(map[string][]float64),
		closes:       make(map[string][]float64),
		volumes:      make(map[string][]float64),
		tr:           make([]float64, 0),
		pDM:          make([]float64, 0),
		nDM:          make([]float64, 0),
		bbwHistory:   make([]float64, 0),
	}
}




// AddKline inserts a CLOSED kline into the classifier.
func (c *Classifier) AddKline(timeframe string, high, low, closePrice, volume float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.closes[timeframe]; !ok {
		c.highs[timeframe] = make([]float64, 0)
		c.lows[timeframe] = make([]float64, 0)
		c.closes[timeframe] = make([]float64, 0)
		c.volumes[timeframe] = make([]float64, 0)
	}

	c.highs[timeframe] = append(c.highs[timeframe], high)
	c.lows[timeframe] = append(c.lows[timeframe], low)
	c.closes[timeframe] = append(c.closes[timeframe], closePrice)
	c.volumes[timeframe] = append(c.volumes[timeframe], volume)

	// Keep buffer sizing bounded
	maxLen := 200
	if timeframe == "1h" {
		maxLen = 100 // usually enough for filtering
	}

	if len(c.closes[timeframe]) > maxLen {
		c.highs[timeframe] = c.highs[timeframe][1:]
		c.lows[timeframe] = c.lows[timeframe][1:]
		c.closes[timeframe] = c.closes[timeframe][1:]
		c.volumes[timeframe] = c.volumes[timeframe][1:]
	}

	if timeframe == "5m" {
		c.recalcDM()
		// Record BB Width for squeeze detection
		if len(c.closes["5m"]) >= c.bbPeriod {
			bw := c.calcBBWidth("5m")
			c.bbwHistory = append(c.bbwHistory, bw)
			if len(c.bbwHistory) > 200 {
				c.bbwHistory = c.bbwHistory[1:]
			}
		}
	}
}


// recalcDM updates True Range and Directional Movement arrays for the primary (5m) timeframe.
func (c *Classifier) recalcDM() {
	closes := c.closes["5m"]
	highs := c.highs["5m"]
	lows := c.lows["5m"]

	if len(closes) < 2 {
		return
	}

	c.tr = make([]float64, len(closes))
	c.pDM = make([]float64, len(closes))
	c.nDM = make([]float64, len(closes))

	for i := 1; i < len(closes); i++ {
		high := highs[i]
		low := lows[i]
		prevHigh := highs[i-1]
		prevLow := lows[i-1]
		prevClose := closes[i-1]

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


// Current returns the regime, ADX, and BB Width for the primary timeframe.
func (c *Classifier) Current() (RegimeType, float64, float64) {
	closes5m := c.closes["5m"]
	if len(closes5m) < c.adxPeriod*2 {
		return RegimeUnknown, 0, 0
	}

	adx := c.calcADX()
	bbw := c.calcBBWidth("5m")

	// Heuristics for Regime Classification
	if adx > 25 {
		return RegimeTrend, adx, bbw
	}

	// Squeeze Detection Logic
	if c.IsSqueezing() {
		// If volatility is abnormally low, we are in a pre-breakout state
		return RegimeRanging, adx, bbw // Still RANGING until it breaks, but IsSqueezing() will be used by Router
	}

	return RegimeRanging, adx, bbw
}

// calcADX computes the Wilder's Smoothing ADX value for 5m.
func (c *Classifier) calcADX() float64 {
	closes := c.closes["5m"]
	// 1. Initial smoothed TR, +DM, -DM over 14 periods
	str := 0.0
	spDM := 0.0
	snDM := 0.0

	for i := 1; i <= c.adxPeriod; i++ {
		str += c.tr[i]
		spDM += c.pDM[i]
		snDM += c.nDM[i]
	}

	dx := make([]float64, len(closes))
	// 2. Loop to calculate subsequent smoothed values and DX
	for i := c.adxPeriod; i < len(closes); i++ {
		if i > c.adxPeriod {
			str = str - (str / float64(c.adxPeriod)) + c.tr[i]
			spDM = spDM - (spDM / float64(c.adxPeriod)) + c.pDM[i]
			snDM = snDM - (snDM / float64(c.adxPeriod)) + c.nDM[i]
		}

		if str == 0 {
			dx[i] = 0
			continue
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


// calcBBWidth calculates Bollinger Band Width for a given timeframe.
func (c *Classifier) calcBBWidth(tf string) float64 {
	closes := c.closes[tf]
	if len(closes) < c.bbPeriod {
		return 0
	}

	periodData := closes[len(closes)-c.bbPeriod:]

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
	return (upper - lower) / sma * 100
}

// IsSqueezing returns true if BB Width is in the bottom 25% of history.
func (c *Classifier) IsSqueezing() bool {
	if len(c.bbwHistory) < 50 { // need some history
		return false
	}
	current := c.bbwHistory[len(c.bbwHistory)-1]
	
	// Poor man's percentile: count how many historical values are lower than current
	lowerCount := 0
	for _, val := range c.bbwHistory {
		if val < current {
			lowerCount++
		}
	}
	percentile := float64(lowerCount) / float64(len(c.bbwHistory))
	return percentile < 0.25
}

// isSqueezeWidth returns true if the given BBW value qualifies as a squeeze.
func (c *Classifier) isSqueezeWidth(bw float64) bool {
	if len(c.bbwHistory) < 50 {
		return false
	}
	lowerCount := 0
	for _, val := range c.bbwHistory {
		if val < bw {
			lowerCount++
		}
	}
	percentile := float64(lowerCount) / float64(len(c.bbwHistory))
	return percentile < 0.25
}

// HTFTrendBias returns 1 for Bullish, -1 for Bearish, 0 for Neutral using 1h timeframe.
func (c *Classifier) HTFTrendBias() int {
	closes1h := c.closes["1h"]
	if len(closes1h) < 50 { // need enough data for EMA 50
		return 0
	}

	ema20 := calcEMA(closes1h, 20)
	ema50 := calcEMA(closes1h, 50)

	if ema20 > ema50 {
		return 1
	} else if ema20 < ema50 {
		return -1
	}
	return 0
}

func calcEMA(data []float64, n int) float64 {
	if len(data) == 0 {
		return 0
	}
	k := 2.0 / float64(n+1)
	result := data[0]
	for i := 1; i < len(data); i++ {
		result = data[i]*k + result*(1-k)
	}
	return result
}

// GetATR returns the Average True Range (Wilder's Smoothing) for a given timeframe and period.
func (c *Classifier) GetATR(tf string, period int) float64 {
	if _, ok := c.closes[tf]; !ok {
		return 0
	}
	
	// We need TR data. If it's the 5m timeframe, we use c.tr
	// If it's another timeframe, we'd need to calculate TR on the fly.
	// For now, most sizing uses the primary execution timeframe (5m).
	var trs []float64
	if tf == "5m" {
		trs = c.tr
	} else {
		// Calculate TR on the fly for other timeframes if needed
		h := c.highs[tf]
		l := c.lows[tf]
		cl := c.closes[tf]
		if len(cl) < 2 {
			return 0
		}
		trs = make([]float64, len(cl))
		for i := 1; i < len(cl); i++ {
			tr1 := h[i] - l[i]
			tr2 := math.Abs(h[i] - cl[i-1])
			tr3 := math.Abs(l[i] - cl[i-1])
			trs[i] = math.Max(math.Max(tr1, tr2), tr3)
		}
	}

	if len(trs) < period+1 {
		return 0
	}

	// Wilder's Smoothing for ATR
	atr := 0.0
	// Initial sum
	for i := 1; i <= period; i++ {
		atr += trs[i]
	}
	atr /= float64(period)

	// Subsequent smoothed values
	for i := period + 1; i < len(trs); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// GetCloses returns the historical close prices for a given timeframe.
func (c *Classifier) GetCloses(tf string) []float64 {
	return c.closes[tf]
}

// GetLows returns the historical low prices for a given timeframe.
func (c *Classifier) GetLows(tf string) []float64 {
	return c.lows[tf]
}

// GetHighs returns the historical high prices for a given timeframe.
func (c *Classifier) GetHighs(tf string) []float64 {
	return c.highs[tf]
}

// GetVolumes returns the historical volume for a given timeframe.
func (c *Classifier) GetVolumes(tf string) []float64 {
	return c.volumes[tf]
}

// SetFundingRate updates the current funding rate for this symbol.
func (c *Classifier) SetFundingRate(rate float64) {
	c.fundingRate = rate
}

// GetFundingRate returns the current funding rate for this symbol.
func (c *Classifier) GetFundingRate() float64 {
	return c.fundingRate
}
// WasSqueezingRecently returns true if BBW was below the squeeze threshold
// in at least one of the last `lookback` bars. Used to guard breakout regime promotion.
func (c *Classifier) WasSqueezingRecently(lookback int) bool {
	n := len(c.bbwHistory)
	if n == 0 {
		return false
	}
	start := n - lookback
	if start < 0 {
		start = 0
	}
	for _, bw := range c.bbwHistory[start:] {
		if c.isSqueezeWidth(bw) {
			return true
		}
	}
	return false
}


// GetLogReturns returns the log-returns (ln(p1/p0)) for a timeframe.
func (c *Classifier) GetLogReturns(timeframe string, periods int) []float64 {
c.mu.RLock()
defer c.mu.RUnlock()

closes, ok := c.closes[timeframe]
if !ok || len(closes) < 2 {
return nil
}
if periods > len(closes)-1 {
periods = len(closes) - 1
}

out := make([]float64, periods)
for i := 0; i < periods; i++ {
idx := len(closes) - 1 - i
// Log return = ln(price_t / price_{t-1})
out[i] = math.Log(closes[idx] / closes[idx-1])
}
return out
}
