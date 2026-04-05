package market_regime

// calculateSMA calculates Simple Moving Average
func calculateSMA(prices []float64, period int) float64 {
	if len(prices) == 0 || period <= 0 {
		return 0
	}

	sum := 0.0
	start := len(prices) - period
	if start < 0 {
		start = 0
	}

	for i := start; i < len(prices); i++ {
		sum += prices[i]
	}

	return sum / float64(period)
}

// calculateMomentum calculates price momentum
func calculateMomentum(prices []float64, shortPeriod, longPeriod int) float64 {
	if len(prices) < 2 || shortPeriod <= 0 || longPeriod <= 0 {
		return 0
	}

	shortMA := calculateSMA(prices, shortPeriod)
	longMA := calculateSMA(prices, longPeriod)

	return (shortMA - longMA) / longMA
}
