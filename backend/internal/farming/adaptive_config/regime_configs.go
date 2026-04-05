package adaptive_config

import (
	"fmt"

	"aster-bot/internal/config"
)

// RegimeConfigFactory creates regime-specific configurations
type RegimeConfigFactory struct{}

// CreateTrendingConfig creates configuration for trending markets
func (f *RegimeConfigFactory) CreateTrendingConfig() *config.RegimeConfig {
	return &config.RegimeConfig{
		OrderSizeUSDT:          2.0,  // Conservative sizing
		GridSpreadPct:          0.1,  // Wider spreads for trend protection
		MaxOrdersPerSide:       5,    // Fewer orders in trends
		MaxDailyLossUSDT:       10.0, // Lower loss tolerance
		PositionTimeoutMinutes: 30,   // Faster exits in trends
	}
}

// CreateRangingConfig creates configuration for ranging markets
func (f *RegimeConfigFactory) CreateRangingConfig() *config.RegimeConfig {
	return &config.RegimeConfig{
		OrderSizeUSDT:          5.0,  // Optimal sizing for volume
		GridSpreadPct:          0.02, // Tight spreads for fill rates
		MaxOrdersPerSide:       10,   // Maximum orders for volume generation
		MaxDailyLossUSDT:       25.0, // Moderate loss tolerance
		PositionTimeoutMinutes: 60,   // Standard timeout
	}
}

// CreateVolatileConfig creates configuration for volatile markets
func (f *RegimeConfigFactory) CreateVolatileConfig() *config.RegimeConfig {
	return &config.RegimeConfig{
		OrderSizeUSDT:          3.0,  // Balanced sizing
		GridSpreadPct:          0.05, // Moderate spreads
		MaxOrdersPerSide:       7,    // Reduced orders for risk management
		MaxDailyLossUSDT:       15.0, // Conservative loss tolerance
		PositionTimeoutMinutes: 45,   // Faster exits for volatility
	}
}

// GetRegimeConfig returns configuration for specific regime type
func (f *RegimeConfigFactory) GetRegimeConfig(regime string) (*config.RegimeConfig, error) {
	switch regime {
	case "trending":
		return f.CreateTrendingConfig(), nil
	case "ranging":
		return f.CreateRangingConfig(), nil
	case "volatile":
		return f.CreateVolatileConfig(), nil
	default:
		return nil, fmt.Errorf("unknown regime type: %s", regime)
	}
}

// ValidateRegimeConfig validates a regime configuration
func (f *RegimeConfigFactory) ValidateRegimeConfig(config *config.RegimeConfig) error {
	if config == nil {
		return fmt.Errorf("regime config cannot be nil")
	}

	if config.OrderSizeUSDT < 1 || config.OrderSizeUSDT > 1000 {
		return fmt.Errorf("order size must be between 1 and 1000 USDT")
	}

	if config.GridSpreadPct < 0.001 || config.GridSpreadPct > 1.0 {
		return fmt.Errorf("grid spread must be between 0.001%% and 1.0%%")
	}

	if config.MaxOrdersPerSide < 1 || config.MaxOrdersPerSide > 20 {
		return fmt.Errorf("max orders per side must be between 1 and 20")
	}

	if config.MaxDailyLossUSDT < 1 || config.MaxDailyLossUSDT > 10000 {
		return fmt.Errorf("daily loss limit must be between 1 and 10000 USDT")
	}

	if config.PositionTimeoutMinutes < 1 || config.PositionTimeoutMinutes > 1440 {
		return fmt.Errorf("position timeout must be between 1 minute and 24 hours")
	}

	return nil
}
