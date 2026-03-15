package indicators

// SMA calculates the Simple Moving Average
func SMA(data []float64, period int) float64 {
	if len(data) < period || period <= 0 {
		return 0
	}
	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}
	return sum / float64(period)
}

// EMA calculates the Exponential Moving Average iteratively from the end.
// For a production bot, it's often better to maintain a running EMA object,
// but for simple array-based queries, this computes it from a slice.
func EMA(data []float64, period int) float64 {
	if len(data) < period || period <= 0 {
		return 0
	}

	// Multiplier
	k := 2.0 / (float64(period) + 1.0)

	// Start by calculating the SMA for the first 'period' window
	// to use as the initial EMA seed.
	startIndex := len(data) - period*2
	if startIndex < 0 {
		startIndex = 0
	}

	// We seed with SMA of the first slice we have room for
	initialSlice := data[startIndex : startIndex+period]
	currentEMA := SMA(initialSlice, period)

	// Iterate forward calculating EMA
	for i := startIndex + period; i < len(data); i++ {
		currentEMA = (data[i] * k) + (currentEMA * (1.0 - k))
	}

	return currentEMA
}

// EMAState maintains a running EMA without needing full slice history.
type EMAState struct {
	period int
	k      float64
	value  float64
	count  int
	sum    float64 // used for initial SMA seed
}

func NewEMAState(period int) *EMAState {
	return &EMAState{
		period: period,
		k:      2.0 / (float64(period) + 1.0),
	}
}

func (e *EMAState) Add(price float64) float64 {
	e.count++
	if e.count <= e.period {
		e.sum += price
		if e.count == e.period {
			e.value = e.sum / float64(e.period)
		}
		return e.value // Returns 0 or partial until seeded
	}

	e.value = (price * e.k) + (e.value * (1.0 - e.k))
	return e.value
}

func (e *EMAState) Value() float64 {
	return e.value
}
