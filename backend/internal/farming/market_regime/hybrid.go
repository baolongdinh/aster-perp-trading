package market_regime

import (
	"math"
)

// detectHybridRegime implements hybrid regime detection
func detectHybridRegime(prices []float64, atr float64, momentum float64) MarketRegime {
	// ATR-based detection
	atrRatio := atr / prices[len(prices)-1]
	atrRegime := RegimeUnknown
	if atrRatio > 0.03 {
		atrRegime = RegimeVolatile
	} else if atrRatio > 0.015 {
		atrRegime = RegimeRanging
	}

	// Momentum-based detection
	momentumRegime := RegimeUnknown
	if math.Abs(momentum) > 0.02 {
		momentumRegime = RegimeTrending
	} else if math.Abs(momentum) > 0.005 {
		momentumRegime = RegimeRanging
	}

	// Hybrid decision (60% ATR, 40% momentum)
	finalRegime := RegimeUnknown
	if atrRegime != RegimeUnknown && momentumRegime != RegimeUnknown {
		if atrRegime == momentumRegime {
			finalRegime = atrRegime
		} else {
			finalRegime = momentumRegime
		}
	}

	return finalRegime
}
