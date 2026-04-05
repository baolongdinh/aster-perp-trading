package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

// LoadAdaptiveConfig loads adaptive configuration from file
func LoadAdaptiveConfig(configPath string) (*AdaptiveConfig, error) {
	if configPath == "" {
		return nil, fmt.Errorf("adaptive config path cannot be empty")
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("adaptive config file not found: %s", configPath)
	}

	// Set config file
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Set environment variable prefix
	viper.SetEnvPrefix("ADAPTIVE")

	// Read config
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read adaptive config: %w", err)
	}

	var config AdaptiveConfig
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal adaptive config: %w", err)
	}

	// Validate configuration
	if err := ValidateAdaptiveConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid adaptive config: %w", err)
	}

	return &config, nil
}

// GetDefaultAdaptiveConfig returns default adaptive configuration
func GetDefaultAdaptiveConfig() *AdaptiveConfig {
	return &AdaptiveConfig{
		Enabled: true,
		Detection: DetectionConfig{
			Method:            "hybrid",
			UpdateIntervalSec: 300, // 5 minutes
			ATRPeriod:         14,
			MomentumShort:     10,
			MomentumLong:      20,
		},
		Regimes: map[string]RegimeConfig{
			"trending": {
				OrderSizeUSDT:          2.0,
				GridSpreadPct:          0.1,
				MaxOrdersPerSide:       5,
				MaxDailyLossUSDT:       10.0,
				PositionTimeoutMinutes: 30,
			},
			"ranging": {
				OrderSizeUSDT:          5.0,
				GridSpreadPct:          0.02,
				MaxOrdersPerSide:       10,
				MaxDailyLossUSDT:       25.0,
				PositionTimeoutMinutes: 60,
			},
			"volatile": {
				OrderSizeUSDT:          3.0,
				GridSpreadPct:          0.05,
				MaxOrdersPerSide:       7,
				MaxDailyLossUSDT:       15.0,
				PositionTimeoutMinutes: 45,
			},
		},
	}
}
