package indicators

// RSIState maintains a running RSI calculation
// which is much faster than recalculating from an array history every tick.
type RSIState struct {
	period int
	count  int

	prevPrice float64

	avgGain float64
	avgLoss float64

	value float64
}

func NewRSIState(period int) *RSIState {
	return &RSIState{
		period: period,
	}
}

// Add updates the running RSI with the new closing price.
// Note: It's best to feed RSI only CLOSED candles to prevent volatile intraday swinging RSI.
func (r *RSIState) Add(price float64) float64 {
	if r.count == 0 {
		r.prevPrice = price
		r.count++
		return 50.0 // Default center before calculation
	}

	diff := price - r.prevPrice
	r.prevPrice = price

	gain := 0.0
	loss := 0.0
	if diff > 0 {
		gain = diff
	} else if diff < 0 {
		loss = -diff
	}

	r.count++

	// Seed the first period
	if r.count <= r.period+1 {
		r.avgGain += gain
		r.avgLoss += loss

		if r.count == r.period+1 {
			r.avgGain /= float64(r.period)
			r.avgLoss /= float64(r.period)
			r.calculateRSI()
		}
		return r.value
	}

	// Smoothed Moving Average (RMA) logic for subsequent periods
	r.avgGain = ((r.avgGain * float64(r.period-1)) + gain) / float64(r.period)
	r.avgLoss = ((r.avgLoss * float64(r.period-1)) + loss) / float64(r.period)
	r.calculateRSI()

	return r.value
}

func (r *RSIState) calculateRSI() {
	if r.avgLoss == 0 {
		r.value = 100
		return
	}
	rs := r.avgGain / r.avgLoss
	r.value = 100.0 - (100.0 / (1.0 + rs))
}

func (r *RSIState) Value() float64 {
	return r.value
}
