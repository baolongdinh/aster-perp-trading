package maker

import (
	"time"

	"aster-bot/internal/farming/volume_optimization"
)

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

	// Momentum Protection
	MomentumDetection    bool    `mapstructure:"momentum_detection" yaml:"momentum_detection"`
	MomentumThresholdPct float64 `mapstructure:"momentum_threshold_pct" yaml:"momentum_threshold_pct"` // 0.03 = 3% price move
	MomentumTimeWindow   int     `mapstructure:"momentum_time_window" yaml:"momentum_time_window"`     // seconds

	// Order Book Imbalance Detection (OBI)
	OBIDetectionEnabled bool    `mapstructure:"obi_detection_enabled" yaml:"obi_detection_enabled"`
	OBIWindowSize       int     `mapstructure:"obi_window_size" yaml:"obi_window_size"`             // Number of snapshots to track
	OBIThreshold        float64 `mapstructure:"obi_threshold" yaml:"obi_threshold"`                 // What constitutes "strong" imbalance
	OBISpreadAdjustment bool    `mapstructure:"obi_spread_adjustment" yaml:"obi_spread_adjustment"` // Adjust spread based on OBI
	OBISizeAdjustment   bool    `mapstructure:"obi_size_adjustment" yaml:"obi_size_adjustment"`     // Adjust order size based on OBI

	// Market Microstructure Analyzer (combines VPIN + OBI)
	MicrostructureAnalysisEnabled bool `mapstructure:"microstructure_analysis_enabled" yaml:"microstructure_analysis_enabled"`
	AggressivenessLevel           int  `mapstructure:"aggressiveness_level" yaml:"aggressiveness_level"` // 1-5 scale

	// Volume Optimization Modules
	PennyJumpingEnabled      bool                                     `mapstructure:"penny_jumping_enabled" yaml:"penny_jumping_enabled"`
	PennyJumpingConfig       volume_optimization.PennyConfig          `mapstructure:"penny_jumping_config" yaml:"penny_jumping_config"`
	InventoryHedgingEnabled  bool                                     `mapstructure:"inventory_hedging_enabled" yaml:"inventory_hedging_enabled"`
	InventoryHedgingConfig   volume_optimization.InventoryHedgeConfig `mapstructure:"inventory_hedging_config" yaml:"inventory_hedging_config"`
	SmartCancellationEnabled bool                                     `mapstructure:"smart_cancellation_enabled" yaml:"smart_cancellation_enabled"`
	SmartCancellationConfig  volume_optimization.SmartCancelConfig    `mapstructure:"smart_cancellation_config" yaml:"smart_cancellation_config"`
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

		// Momentum Protection
		MomentumDetection:    true,
		MomentumThresholdPct: 0.03, // 3% price move
		MomentumTimeWindow:   30,   // 30 seconds

		// Order Book Imbalance Detection (OBI) - DEFAULT: ON
		OBIDetectionEnabled: true,
		OBIWindowSize:       10,   // Track 10 snapshots (10 seconds @ 1Hz)
		OBIThreshold:        0.5,  // 50% imbalance = significant
		OBISpreadAdjustment: true, // Adjust spread based on OBI signal
		OBISizeAdjustment:   true, // Adjust order size based on OBI signal

		// Market Microstructure Analyzer (combines VPIN + OBI) - DEFAULT: ON
		MicrostructureAnalysisEnabled: true,
		AggressivenessLevel:           3, // 1-5 scale, 3 = medium

		// Volume Optimization Modules (DEFAULT: ON)
		PennyJumpingEnabled: true,
		PennyJumpingConfig: volume_optimization.PennyConfig{
			Enabled:       true,
			JumpThreshold: 0.1,
			MaxJump:       1,
		},
		InventoryHedgingEnabled: true,
		InventoryHedgingConfig: volume_optimization.InventoryHedgeConfig{
			Enabled:        true,
			HedgeThreshold: 0.3,
			HedgeRatio:     0.3,
			MaxHedgeSize:   100.0,
			HedgingMode:    "internal",
			CheckInterval:  30 * time.Second,
		},
		SmartCancellationEnabled: true,
		SmartCancellationConfig: volume_optimization.SmartCancelConfig{
			Enabled:               true,
			SpreadChangeThreshold: 0.2,
			CheckInterval:         5 * time.Second,
		},
	}
}
