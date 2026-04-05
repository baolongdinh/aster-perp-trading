package adaptive_config

import (
	"fmt"
	"strings"

	"aster-bot/internal/config"
)

// ParameterValidator validates and sanitizes adaptive configuration parameters
type ParameterValidator struct{}

// NewParameterValidator creates a new parameter validator
func NewParameterValidator() *ParameterValidator {
	return &ParameterValidator{}
}

// ValidateDetectionConfig validates detection configuration parameters
func (v *ParameterValidator) ValidateDetectionConfig(config config.DetectionConfig) error {
	// Validate method
	validMethods := []string{"hybrid", "atr", "momentum"}
	if !contains(validMethods, config.Method) {
		return fmt.Errorf("invalid detection method: %s (must be one of: %s)", config.Method, strings.Join(validMethods, ", "))
	}

	// Validate intervals
	if config.UpdateIntervalSec < 30 || config.UpdateIntervalSec > 3600 {
		return fmt.Errorf("update interval must be between 30 seconds and 1 hour")
	}

	// Validate periods
	if config.ATRPeriod < 5 || config.ATRPeriod > 100 {
		return fmt.Errorf("ATR period must be between 5 and 100")
	}

	if config.MomentumShort < 5 || config.MomentumShort > 50 {
		return fmt.Errorf("momentum short period must be between 5 and 50")
	}

	if config.MomentumLong < 10 || config.MomentumLong > 200 {
		return fmt.Errorf("momentum long period must be between 10 and 200")
	}

	// Validate period relationship
	if config.MomentumShort >= config.MomentumLong {
		return fmt.Errorf("momentum short period must be less than long period")
	}

	return nil
}

// ValidateRegimeConfig validates regime-specific configuration
func (v *ParameterValidator) ValidateRegimeConfig(regime string, config config.RegimeConfig) error {
	// Validate order size
	if config.OrderSizeUSDT < 1 || config.OrderSizeUSDT > 1000 {
		return fmt.Errorf("order size for regime %s must be between 1 and 1000 USDT", regime)
	}

	// Validate grid spread
	if config.GridSpreadPct < 0.001 || config.GridSpreadPct > 1.0 {
		return fmt.Errorf("grid spread for regime %s must be between 0.001%% and 1.0%%", regime)
	}

	// Validate orders per side
	if config.MaxOrdersPerSide < 1 || config.MaxOrdersPerSide > 20 {
		return fmt.Errorf("max orders per side for regime %s must be between 1 and 20", regime)
	}

	// Validate daily loss limit
	if config.MaxDailyLossUSDT < 1 || config.MaxDailyLossUSDT > 10000 {
		return fmt.Errorf("daily loss limit for regime %s must be between 1 and 10000 USDT", regime)
	}

	// Validate position timeout
	if config.PositionTimeoutMinutes < 1 || config.PositionTimeoutMinutes > 1440 {
		return fmt.Errorf("position timeout for regime %s must be between 1 minute and 24 hours", regime)
	}

	// Regime-specific validations
	switch regime {
	case "trending":
		return v.validateTrendingConfig(config)
	case "ranging":
		return v.validateRangingConfig(config)
	case "volatile":
		return v.validateVolatileConfig(config)
	default:
		return fmt.Errorf("unknown regime: %s", regime)
	}
}

// validateTrendingConfig validates trending-specific configuration
func (v *ParameterValidator) validateTrendingConfig(config config.RegimeConfig) error {
	if config.OrderSizeUSDT > 5 {
		return fmt.Errorf("trending regime order size should not exceed 5 USDT for risk management")
	}

	if config.GridSpreadPct < 0.05 {
		return fmt.Errorf("trending regime grid spread should be at least 0.05%% for trend protection")
	}

	return nil
}

// validateRangingConfig validates ranging-specific configuration
func (v *ParameterValidator) validateRangingConfig(config config.RegimeConfig) error {
	if config.OrderSizeUSDT > 10 {
		return fmt.Errorf("ranging regime order size should not exceed 10 USDT")
	}

	if config.GridSpreadPct > 0.05 {
		return fmt.Errorf("ranging regime grid spread should not exceed 0.05%% for optimal fill rates")
	}

	return nil
}

// validateVolatileConfig validates volatile-specific configuration
func (v *ParameterValidator) validateVolatileConfig(config config.RegimeConfig) error {
	if config.OrderSizeUSDT > 5 {
		return fmt.Errorf("volatile regime order size should not exceed 5 USDT for risk management")
	}

	if config.GridSpreadPct < 0.03 || config.GridSpreadPct > 0.1 {
		return fmt.Errorf("volatile regime grid spread should be between 0.03%% and 0.1%%")
	}

	return nil
}

// contains checks if string is in slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
