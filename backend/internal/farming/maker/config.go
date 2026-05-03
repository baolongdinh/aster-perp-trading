package maker

import "time"

type Config struct {
	DefaultSpreadBps     float64       `mapstructure:"default_spread_bps" yaml:"default_spread_bps"`
	MaxLeverage          int           `mapstructure:"max_leverage" yaml:"max_leverage"`
	MaxPositionUSDT      float64       `mapstructure:"max_position_usdt" yaml:"max_position_usdt"`
	MaxTotalExposureUSDT float64       `mapstructure:"max_total_exposure_usdt" yaml:"max_total_exposure_usdt"`
	RebalanceThreshold   float64       `mapstructure:"rebalance_threshold" yaml:"rebalance_threshold"`
	LiquidationBuffer    float64       `mapstructure:"liquidation_buffer" yaml:"liquidation_buffer"`
	DailyLossLimitPct    float64       `mapstructure:"daily_loss_limit_pct" yaml:"daily_loss_limit_pct"`
	OrderToTradeLimit    int           `mapstructure:"order_to_trade_limit" yaml:"order_to_trade_limit"`
	CheckInterval        time.Duration `mapstructure:"check_interval" yaml:"check_interval"`
	Symbols              []string      `mapstructure:"symbols" yaml:"symbols"`
}

func DefaultConfig() *Config {
	return &Config{
		Symbols:              []string{"btcusd1", "ethusd1"},
		DefaultSpreadBps:     2,
		MaxLeverage:          100,   // High leverage for volume farming
		MaxPositionUSDT:      2000,  // Increase position size
		MaxTotalExposureUSDT: 10000, // More exposure for volume
		RebalanceThreshold:   0.2,
		LiquidationBuffer:    0.05,
		DailyLossLimitPct:    0.02,
		OrderToTradeLimit:    10,
		CheckInterval:        5 * time.Second,
	}
}
