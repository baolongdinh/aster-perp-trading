package market_regime

import (
	"math"
	"time"

	"go.uber.org/zap"

	"backend/internal/farming/momentum"
)

// detectAndUpdateRegime detects market regime and updates current state
func (d *RegimeDetector) detectAndUpdateRegime(symbol string) {
	// Get price history for analysis
	prices := d.priceHistory[symbol]
	if len(prices) < 20 {
		// Not enough data for detection
		return
	}

	// Calculate ATR (Average True Range)
	atr := momentum.CalculateATR(prices)

	// Calculate momentum using SMAs
	shortMA := calculateSMA(prices, 10)
	longMA := calculateSMA(prices, 20)
	momentum := (shortMA - longMA) / longMA

	// Detect regime using hybrid approach
	newRegime := detectHybridRegime(prices, atr, momentum)

	// Update current regime if changed
	if currentRegime, exists := d.currentRegime[symbol]; !exists || currentRegime != newRegime {
		d.currentRegime[symbol] = newRegime
		d.lastUpdate[symbol] = time.Now()

		// Log regime transition
		d.logger.Info("Regime transition detected",
			zap.String("symbol", symbol),
			zap.String("from", string(d.currentRegime[symbol])),
			zap.String("to", string(newRegime)),
			zap.Time("timestamp", d.lastUpdate[symbol]))
	}
}

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
