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

	// Dynamic Balance-Based Sizing
	UseDynamicSizing bool    `mapstructure:"use_dynamic_sizing" yaml:"use_dynamic_sizing"`
	BaseNotionalUSD  float64 `mapstructure:"base_notional_usd" yaml:"base_notional_usd"` // Base order size in USD
	MinNotionalUSD   float64 `mapstructure:"min_notional_usd" yaml:"min_notional_usd"`   // Minimum order size
	MaxNotionalUSD   float64 `mapstructure:"max_notional_usd" yaml:"max_notional_usd"`   // Maximum order size per symbol

	// Micro Profit Optimization
	MicroProfitMode     bool    `mapstructure:"micro_profit_mode" yaml:"micro_profit_mode"`
	MicroGridSpacingBps float64 `mapstructure:"micro_grid_spacing_bps" yaml:"micro_grid_spacing_bps"` // Ultra-tight: 0.1 bps = 0.001%
	MicroGridLevels     int     `mapstructure:"micro_grid_levels" yaml:"micro_grid_levels"`           // Number of levels each side
	MicroMinNotionalUSD float64 `mapstructure:"micro_min_notional_usd" yaml:"micro_min_notional_usd"` // Min notional per order

	// Position Bias Protection
	PositionBiasThreshold float64 `mapstructure:"position_bias_threshold" yaml:"position_bias_threshold"`   // 0.3 = 30% of max position
	PositionBiasReducePct float64 `mapstructure:"position_bias_reduce_pct" yaml:"position_bias_reduce_pct"` // Reduce orders by X% when biased

	// Toxic Flow Protection
	ToxicFlowDetection bool    `mapstructure:"toxic_flow_detection" yaml:"toxic_flow_detection"`
	ToxicFlowThreshold float64 `mapstructure:"toxic_flow_threshold" yaml:"toxic_flow_threshold"`   // 0.6 = 60% buy volume = toxic
	ToxicFlowReducePct float64 `mapstructure:"toxic_flow_reduce_pct" yaml:"toxic_flow_reduce_pct"` // Reduce exposure when toxic detected
}

func DefaultConfig() *Config {
	return &Config{
		Symbols:              []string{"btcusd1", "ethusd1"},
		DefaultSpreadBps:     2,
		MaxLeverage:          150,
		MaxPositionUSDT:      3000,
		MaxTotalExposureUSDT: 15000,
		RebalanceThreshold:   0.2,
		LiquidationBuffer:    0.05,
		DailyLossLimitPct:    0.02,
		OrderToTradeLimit:    10,
		CheckInterval:        5 * time.Second,

		// Dynamic Balance-Based Sizing (DEFAULT: ON)
		UseDynamicSizing: true,
		BaseNotionalUSD:  100, // Base order size = $100
		MinNotionalUSD:   20,  // Minimum = $20
		MaxNotionalUSD:   500, // Maximum per symbol = $500

		// Micro Profit Optimization (DEFAULT: ON)
		MicroProfitMode:     true,
		MicroGridSpacingBps: 0.1, // 0.1 bps = 0.001% (ultra-tight for max fills)
		MicroGridLevels:     50,  // 50 levels each side = 100 orders total
		MicroMinNotionalUSD: 5,   // Minimum $5 per order

		// Position Bias Protection
		PositionBiasThreshold: 0.3, // Start reducing at 30% of max position
		PositionBiasReducePct: 0.5, // Reduce orders by 50% when biased

		// Toxic Flow Protection
		ToxicFlowDetection: true,
		ToxicFlowThreshold: 0.6, // 60% one-sided = toxic flow
		ToxicFlowReducePct: 0.5, // Cut exposure in half
	}
}
