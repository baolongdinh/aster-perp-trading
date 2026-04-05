package config

import (
	"fmt"
)

// ValidateAdaptiveConfig validates adaptive configuration parameters
func ValidateAdaptiveConfig(config *AdaptiveConfig) error {
	if config == nil {
		return fmt.Errorf("adaptive config cannot be nil")
	}
	
	// Validate detection settings
	if config.Detection.UpdateIntervalSec < 30 {
		return fmt.Errorf("update interval must be at least 30 seconds")
	}
	if config.Detection.UpdateIntervalSec > 3600 {
		return fmt.Errorf("update interval cannot exceed 1 hour")
	}
	
	if config.Detection.ATRPeriod < 5 || config.Detection.ATRPeriod > 100 {
		return fmt.Errorf("ATR period must be between 5 and 100")
	}
	
	if config.Detection.MomentumShort < 5 || config.Detection.MomentumShort > 50 {
		return fmt.Errorf("momentum short period must be between 5 and 50")
	}
	
	if config.Detection.MomentumLong < 10 || config.Detection.MomentumLong > 200 {
		return fmt.Errorf("momentum long period must be between 10 and 200")
	}
	
	// Validate regime configurations
	for regimeName, regimeConfig := range config.Regimes {
		if regimeConfig.OrderSizeUSDT < 1 {
			return fmt.Errorf("order size for regime %s must be at least 1 USDT", regimeName)
		}
		if regimeConfig.OrderSizeUSDT > 1000 {
			return fmt.Errorf("order size for regime %s cannot exceed 1000 USDT", regimeName)
		}
		
		if regimeConfig.GridSpreadPct < 0.001 || regimeConfig.GridSpreadPct > 1.0 {
			return fmt.Errorf("grid spread for regime %s must be between 0.001%% and 1.0%%", regimeName)
		}
		
		if regimeConfig.MaxOrdersPerSide < 1 || regimeConfig.MaxOrdersPerSide > 20 {
			return fmt.Errorf("max orders per side for regime %s must be between 1 and 20", regimeName)
		}
		
		if regimeConfig.MaxDailyLossUSDT < 1 || regimeConfig.MaxDailyLossUSDT > 10000 {
			return fmt.Errorf("daily loss limit for regime %s must be between 1 and 10000 USDT", regimeName)
		}
		
		if regimeConfig.PositionTimeoutMinutes < 1 || regimeConfig.PositionTimeoutMinutes > 1440 {
			return fmt.Errorf("position timeout for regime %s must be between 1 minute and 24 hours", regimeName)
		}
	}
	
	return nil
}
