package notifier

import (
	"math"
	"time"
)

// AlertConfig contains thresholds for triggering alerts.
type AlertConfig struct {
	DrawdownThresholdPct float64
	NoOrderMinutes       float64
	VolatilityPct        float64
}

// DefaultAlertConfig provides sensible baseline values for Grid Bots.
var DefaultAlertConfig = AlertConfig{
	DrawdownThresholdPct: 5.0,  // 5% max drawdown
	NoOrderMinutes:       15.0, // Alert if no orders filled in 15 mins
	VolatilityPct:        2.0,  // 2% price movement in a short time
}

// HasGridBreakout checks if the price has exited the active grid ranges.
func HasGridBreakout(metrics GridMetrics) bool {
	// A simple breakout is when price breaches min or max bounds
	if metrics.GridMinPrice > 0 && metrics.GridMaxPrice > 0 {
		if metrics.CurrentPrice <= metrics.GridMinPrice || metrics.CurrentPrice >= metrics.GridMaxPrice {
			return true
		}
	}
	return false
}

// IsDrawdownCritical checks if current drawdown breaches the threshold.
func IsDrawdownCritical(metrics GridMetrics, config AlertConfig) bool {
	return metrics.DrawdownPct >= config.DrawdownThresholdPct
}

// IsNoOrdersCritical checks if the bot has stopped ordering/filling recently.
func IsNoOrdersCritical(metrics GridMetrics, config AlertConfig) bool {
	if metrics.LastOrderTime.IsZero() {
		return false
	}
	return time.Since(metrics.LastOrderTime).Minutes() >= config.NoOrderMinutes
}

// HasHighVolatility checks if recent price velocity acts as high volatility pump/dump.
// Note: While this can be done internally by the bot, a generic check uses price comparisons over time.
func HasHighVolatility(lastPrice, currentPrice float64, config AlertConfig) bool {
	if lastPrice <= 0 {
		return false
	}
	velocity := math.Abs(currentPrice-lastPrice) / lastPrice * 100
	return velocity >= config.VolatilityPct
}
