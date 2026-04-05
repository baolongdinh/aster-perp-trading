package market_regime

import (
	"testing"
)

func TestGetRegimeDescription(t *testing.T) {
	tests := map[string]struct {
		regime   MarketRegime
		expected string
	}{
		"Trending description": {RegimeTrending, "Strong directional movement detected"},
		"Ranging description":  {RegimeRanging, "Sideways price action detected"},
		"Volatile description": {RegimeVolatile, "High volatility with unclear direction"},
		"Unknown description":  {RegimeUnknown, "Insufficient data for detection"},
		"Empty regime":         {MarketRegime(""), "Insufficient data for detection"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := GetRegimeDescription(tt.regime); got != tt.expected {
				t.Errorf("GetRegimeDescription(%v) = %v, want %v", tt.regime, got, tt.expected)
			}
		})
	}
}
