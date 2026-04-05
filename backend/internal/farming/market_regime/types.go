package market_regime

// MarketRegime represents different market conditions for adaptive trading
type MarketRegime string

const (
	// RegimeTrending indicates strong directional movement in the market
	RegimeTrending MarketRegime = "trending"

	// RegimeRanging indicates sideways price action with no clear direction
	RegimeRanging MarketRegime = "ranging"

	// RegimeVolatile indicates high volatility with frequent direction changes
	RegimeVolatile MarketRegime = "volatile"

	// RegimeUnknown indicates insufficient data for regime detection
	RegimeUnknown MarketRegime = "unknown"
)

// GetRegimeDescription returns human-readable description for logging
func GetRegimeDescription(regime MarketRegime) string {
	switch regime {
	case RegimeTrending:
		return "Strong directional movement detected"
	case RegimeRanging:
		return "Sideways price action detected"
	case RegimeVolatile:
		return "High volatility with unclear direction"
	case RegimeUnknown:
		return "Insufficient data for detection"
	default:
		return "Unknown regime"
	}
}
