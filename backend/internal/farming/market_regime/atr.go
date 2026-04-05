package market_regime

import (
	"math"
)

// calculateATR calculates Average True Range (ATR)
func calculateATR(prices []float64) float64 {
	if len(prices) < 2 {
		return 0
	}
	
	sum := 0.0
	for i := 1; i < len(prices); i++ {
		sum += math.Abs(prices[i] - prices[i-1])
	}
	
	return sum / float64(len(prices)-1)
}
