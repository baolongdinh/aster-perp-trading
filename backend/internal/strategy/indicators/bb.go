package indicators

import "math"

type BBState struct {
	period int
	stdDev float64
}

func NewBBState(period int, stdDev float64) *BBState {
	return &BBState{period: period, stdDev: stdDev}
}

// Calculate returns (Upper, Mid, Lower)
func (b *BBState) Calculate(data []float64) (float64, float64, float64) {
	if len(data) < b.period {
		return 0, 0, 0
	}

	window := data[len(data)-b.period:]
	
	// Mid (SMA)
	sum := 0.0
	for _, v := range window {
		sum += v
	}
	mid := sum / float64(b.period)

	// StdDev
	sqSum := 0.0
	for _, v := range window {
		diff := v - mid
		sqSum += diff * diff
	}
	sd := math.Sqrt(sqSum / float64(b.period))

	upper := mid + (sd * b.stdDev)
	lower := mid - (sd * b.stdDev)

	return upper, mid, lower
}
