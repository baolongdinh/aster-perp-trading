package config

import "time"

// VolumeOptimizationConfig holds configuration for volume farming optimization features
type VolumeOptimizationConfig struct {
	Enabled bool `yaml:"enabled"`

	// Order Priority
	OrderPriority OrderPriorityConfig `yaml:"order_priority"`

	// Toxic Flow Detection
	ToxicFlow ToxicFlowConfig `yaml:"toxic_flow_detection"`

	// Maker/Taker Optimization
	MakerTaker MakerTakerConfig `yaml:"maker_taker_optimization"`

	// Inventory Hedging (Phase 3)
	InventoryHedge InventoryHedgeConfig `yaml:"inventory_hedging"`
}

// OrderPriorityConfig holds configuration for order priority optimization
type OrderPriorityConfig struct {
	TickSizeAwareness TickSizeConfig `yaml:"tick_size_awareness"`
	PennyJumping      PennyConfig     `yaml:"penny_jumping"`
}

// TickSizeConfig holds configuration for tick-size awareness
type TickSizeConfig struct {
	Enabled    bool              `yaml:"enabled"`
	TickSizes  map[string]float64 `yaml:"tick_sizes"`
	DefaultTickSize float64       `yaml:"default_tick_size"`
}

// PennyConfig holds configuration for penny jumping strategy
type PennyConfig struct {
	Enabled        bool    `yaml:"enabled"`
	JumpThreshold float64 `yaml:"jump_threshold"` // % of spread
	MaxJump        int     `yaml:"max_jump"`        // Max ticks to jump
}

// ToxicFlowConfig holds configuration for VPIN toxic flow detection
type ToxicFlowConfig struct {
	Enabled            bool          `yaml:"enabled"`
	WindowSize         int           `yaml:"window_size"`
	BucketSize         float64       `yaml:"bucket_size"`
	VPINThreshold      float64       `yaml:"vpin_threshold"`
	SustainedBreaches  int           `yaml:"sustained_breaches"`
	Action             string        `yaml:"action"` // pause, widen_spread, reduce_size
	AutoResumeDelay    time.Duration `yaml:"auto_resume_delay"`
}

// MakerTakerConfig holds configuration for maker/taker optimization
type MakerTakerConfig struct {
	PostOnlyEnabled  bool            `yaml:"post_only_enabled"`
	PostOnlyFallback bool           `yaml:"post_only_fallback"`
	SmartCancellation SmartCancelConfig `yaml:"smart_cancellation"`
}

// SmartCancelConfig holds configuration for smart cancellation
type SmartCancelConfig struct {
	Enabled               bool          `yaml:"enabled"`
	SpreadChangeThreshold float64       `yaml:"spread_change_threshold"`
	CheckInterval         time.Duration `yaml:"check_interval"`
}

// InventoryHedgeConfig holds configuration for inventory hedging
type InventoryHedgeConfig struct {
	Enabled        bool    `yaml:"enabled"`
	HedgeThreshold float64 `yaml:"hedge_threshold"`
	HedgeRatio     float64 `yaml:"hedge_ratio"`
	MaxHedgeSize   float64 `yaml:"max_hedge_size"`
	HedgingMode    string  `yaml:"hedging_mode"` // internal, cross_pair, scalping
	HedgePair      string  `yaml:"hedge_pair"`
}

// DefaultVolumeOptimizationConfig returns default configuration
func DefaultVolumeOptimizationConfig() *VolumeOptimizationConfig {
	return &VolumeOptimizationConfig{
		Enabled: true,
		OrderPriority: OrderPriorityConfig{
			TickSizeAwareness: TickSizeConfig{
				Enabled:          true,
				TickSizes:        map[string]float64{},
				DefaultTickSize:  0.01,
			},
			PennyJumping: PennyConfig{
				Enabled:        false,
				JumpThreshold: 0.1,
				MaxJump:        3,
			},
		},
		ToxicFlow: ToxicFlowConfig{
			Enabled:           true,
			WindowSize:        50,
			BucketSize:        1000.0,
			VPINThreshold:     0.3,
			SustainedBreaches: 2,
			Action:            "pause",
			AutoResumeDelay:   5 * time.Second,
		},
		MakerTaker: MakerTakerConfig{
			PostOnlyEnabled:  true,
			PostOnlyFallback: true,
			SmartCancellation: SmartCancelConfig{
				Enabled:               true,
				SpreadChangeThreshold: 0.2,
				CheckInterval:         5 * time.Second,
			},
		},
		InventoryHedge: InventoryHedgeConfig{
			Enabled:        false, // Phase 3
			HedgeThreshold: 0.3,
			HedgeRatio:     0.3,
			MaxHedgeSize:   100.0,
			HedgingMode:    "internal",
			HedgePair:      "ETH",
		},
	}
}
