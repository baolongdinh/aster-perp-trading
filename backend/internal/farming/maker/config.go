package maker

import "time"

type Config struct {
	// Existing config
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

	// === NEW: Active Zone Grid (FR1) ===
	ActiveZoneRange float64 `mapstructure:"active_zone_range" yaml:"active_zone_range"` // 0.1% = 0.001
	GridSpacingMin  float64 `mapstructure:"grid_spacing_min" yaml:"grid_spacing_min"`   // 0.05% = 0.0005
	GridLevels      int     `mapstructure:"grid_levels" yaml:"grid_levels"`             // 5-10 levels

	// === NEW: Position Timeout (FR4) ===
	PositionTimeoutSeconds int `mapstructure:"position_timeout_seconds" yaml:"position_timeout_seconds"` // 60s

	// === NEW: Trailing Stop (FR5) ===
	TrailingActivationPct float64 `mapstructure:"trailing_activation_pct" yaml:"trailing_activation_pct"` // 0.1% = 0.001
	TrailingCallbackPct   float64 `mapstructure:"trailing_callback_pct" yaml:"trailing_callback_pct"`     // 0.03% = 0.0003

	// === NEW: Max Position Limits (FR6) ===
	MaxPositionsPerSide int `mapstructure:"max_positions_per_side" yaml:"max_positions_per_side"` // 5

	// === NEW: Daily Reset (FR8) ===
	DailyResetHour int `mapstructure:"daily_reset_hour" yaml:"daily_reset_hour"` // 23 (UTC)

	// === NEW: Margin Buffer (FR14) ===
	MarginEquityRatio float64 `mapstructure:"margin_equity_ratio" yaml:"margin_equity_ratio"` // 0.75

	// === NEW: Order Size (FR2, FR15) ===
	MinOrderSizeUSD float64 `mapstructure:"min_order_size_usd" yaml:"min_order_size_usd"` // 2.0
	MaxOrderSizeUSD float64 `mapstructure:"max_order_size_usd" yaml:"max_order_size_usd"` // 10.0

	// === NEW: Spread Protection (FR9) ===
	SpreadThreshold float64 `mapstructure:"spread_threshold" yaml:"spread_threshold"` // 0.1% = 0.001

	// === NEW: Zone-Based Sizing (FR7) ===
	EMAPeriod               int     `mapstructure:"ema_period" yaml:"ema_period"`                                 // 30
	ZoneAboveEMAMultiplier  float64 `mapstructure:"zone_above_ema_multiplier" yaml:"zone_above_ema_multiplier"`   // 0.5
	ZoneNormalDipMultiplier float64 `mapstructure:"zone_normal_dip_multiplier" yaml:"zone_normal_dip_multiplier"` // 1.0
	ZoneStrongDipMultiplier float64 `mapstructure:"zone_strong_dip_multiplier" yaml:"zone_strong_dip_multiplier"` // 1.5
	ZoneHardDipMultiplier   float64 `mapstructure:"zone_hard_dip_multiplier" yaml:"zone_hard_dip_multiplier"`     // 0.0

	// === NEW: Post-Only (FR3) ===
	UsePostOnly bool `mapstructure:"use_post_only" yaml:"use_post_only"` // true
}

func DefaultConfig() *Config {
	return &Config{
		// Existing defaults
		Symbols:              []string{"btcusd1", "ethusd1"},
		DefaultSpreadBps:     2,
		MaxLeverage:          150,   // Ultra-high leverage for maximum volume
		MaxPositionUSDT:      3000,  // Larger position size
		MaxTotalExposureUSDT: 15000, // Maximum exposure for volume
		RebalanceThreshold:   0.2,
		LiquidationBuffer:    0.05,
		DailyLossLimitPct:    0.02,
		OrderToTradeLimit:    10,
		CheckInterval:        5 * time.Second,

		// === NEW: Active Zone Grid (FR1) ===
		ActiveZoneRange: 0.001,  // 0.1%
		GridSpacingMin:  0.0005, // 0.05%
		GridLevels:      8,      // 5-10 levels

		// === NEW: Position Timeout (FR4) - DISABLED for continuous farming ===
		PositionTimeoutSeconds: 0, // 0 = disabled, no force close

		// === NEW: Trailing Stop (FR5) - DISABLED for continuous farming ===
		TrailingActivationPct: 0, // 0 = disabled, no force close
		TrailingCallbackPct:   0,

		// === NEW: Max Position Limits (FR6) ===
		MaxPositionsPerSide: 5, // 5 positions per side

		// === NEW: Daily Reset (FR8) - DISABLED for continuous farming ===
		DailyResetHour: -1, // -1 = disabled, no daily reset

		// === NEW: Margin Buffer (FR14) ===
		MarginEquityRatio: 0.75, // 75% equity for trading

		// === NEW: Order Size (FR2, FR15) ===
		MinOrderSizeUSD: 2.0,  // Minimum $2
		MaxOrderSizeUSD: 10.0, // Maximum $10

		// === NEW: Spread Protection (FR9) ===
		SpreadThreshold: 0.001, // 0.1%

		// === NEW: Zone-Based Sizing (FR7) ===
		EMAPeriod:               30,
		ZoneAboveEMAMultiplier:  0.5,
		ZoneNormalDipMultiplier: 1.0,
		ZoneStrongDipMultiplier: 1.5,
		ZoneHardDipMultiplier:   0.0, // No buy in hard dip

		// === NEW: Post-Only (FR3) ===
		UsePostOnly: true,
	}
}
