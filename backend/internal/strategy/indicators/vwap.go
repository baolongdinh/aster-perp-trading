package indicators

type VWAPState struct {
	sumCumPriceVol float64
	sumCumVol      float64
	value          float64
}

func NewVWAPState() *VWAPState {
	return &VWAPState{}
}

// Add updates the VWAP with a new kline.
// price should be the Typical Price: (High + Low + Close) / 3
func (v *VWAPState) Add(price float64, volume float64) float64 {
	v.sumCumPriceVol += price * volume
	v.sumCumVol += volume

	if v.sumCumVol == 0 {
		return 0
	}

	v.value = v.sumCumPriceVol / v.sumCumVol
	return v.value
}

func (v *VWAPState) Value() float64 {
	return v.value
}

// Reset clears the cumulative state (usually done at the start of a trading day).
func (v *VWAPState) Reset() {
	v.sumCumPriceVol = 0
	v.sumCumVol = 0
	v.value = 0
}
