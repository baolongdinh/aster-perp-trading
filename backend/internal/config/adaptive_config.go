package config

// AdaptiveConfig contains configuration for adaptive volume farming
type AdaptiveConfig struct {
	Enabled     bool                   `yaml:"enabled"`
	Detection   DetectionConfig         `yaml:"detection"`
	Regimes     map[string]RegimeConfig `yaml:"regimes"`
}

// DetectionConfig contains market regime detection settings
type DetectionConfig struct {
	Method              string `yaml:"method"`              // "hybrid", "atr", "momentum"
	UpdateIntervalSec   int    `yaml:"update_interval_seconds"`
	ATRPeriod          int    `yaml:"atr_period"`
	MomentumShort      int    `yaml:"momentum_short"`
	MomentumLong       int    `yaml:"momentum_long"`
}

// RegimeConfig contains regime-specific trading parameters
type RegimeConfig struct {
	OrderSizeUSDT           float64 `yaml:"order_size_usdt"`
	GridSpreadPct          float64 `yaml:"grid_spread_pct"`
	MaxOrdersPerSide        int     `yaml:"max_orders_per_side"`
	MaxDailyLossUSDT       float64 `yaml:"max_daily_loss_usdt"`
	PositionTimeoutMinutes   int     `yaml:"position_timeout_minutes"`
}
