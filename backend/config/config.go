package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// OptimizationConfig holds all grid optimization configurations
type OptimizationConfig struct {
	DynamicGrid     *DynamicGridConfig     `yaml:"dynamic_grid"`
	InventorySkew   *InventorySkewConfig   `yaml:"inventory_skew"`
	ClusterStopLoss *ClusterStopLossConfig `yaml:"cluster_stop_loss"`
	TrendDetection  *TrendDetectionConfig  `yaml:"trend_detection"`
	Safeguards      *SafeguardsConfig      `yaml:"safeguards"`
	TimeFilter      *TradingHoursConfig    `yaml:"time_filter"`
}

// DynamicGridConfig mirrors adaptive_grid.DynamicSpreadConfig
type DynamicGridConfig struct {
	Enabled              bool              `yaml:"enabled"`
	ATRPeriod            int               `yaml:"atr_period"`
	ATRTimeframe         string            `yaml:"atr_timeframe"`
	BaseSpreadPct        float64           `yaml:"base_spread_pct"`
	SpreadMultipliers    SpreadMultipliers `yaml:"spread_multipliers"`
	LevelAdjustments     LevelAdjustments  `yaml:"level_adjustments"`
	ATRThresholds        ATRThresholds     `yaml:"atr_thresholds"`
	UpdateInterval       string            `yaml:"update_interval"`
	PriceChangeThreshold float64           `yaml:"price_change_threshold"`
	LogVolatilityChanges bool              `yaml:"log_volatility_changes"`
	LogInterval          string            `yaml:"log_interval"`
}

type SpreadMultipliers struct {
	Low     float64 `yaml:"low_volatility"`
	Normal  float64 `yaml:"normal"`
	High    float64 `yaml:"high_volatility"`
	Extreme float64 `yaml:"extreme"`
}

type LevelAdjustments struct {
	IncreaseThreshold float64 `yaml:"increase_threshold"`
	DecreaseThreshold float64 `yaml:"decrease_threshold"`
	MinLevels         int     `yaml:"min_levels"`
	MaxLevels         int     `yaml:"max_levels"`
}

type ATRThresholds struct {
	Low    float64 `yaml:"low"`
	Normal float64 `yaml:"normal"`
	High   float64 `yaml:"high"`
}

// InventorySkewConfig mirrors adaptive_grid.InventoryConfig
type InventorySkewConfig struct {
	Enabled           bool                  `yaml:"enabled"`
	MaxInventoryPct   float64               `yaml:"max_inventory_pct"`
	Thresholds        InventoryThresholds   `yaml:"thresholds"`
	TakeProfitAdj     TakeProfitAdjustments `yaml:"take_profit_adjustments"`
	Rebalancing       RebalancingConfig     `yaml:"rebalancing"`
	LogInterval       string                `yaml:"log_interval"`
	AlertOnSkewChange bool                  `yaml:"alert_on_skew_change"`
}

type InventoryThresholds struct {
	Low      ThresholdConfig `yaml:"low"`
	Moderate ThresholdConfig `yaml:"moderate"`
	High     ThresholdConfig `yaml:"high"`
	Critical ThresholdConfig `yaml:"critical"`
}

type ThresholdConfig struct {
	Threshold           float64 `yaml:"threshold"`
	SizeReduction       float64 `yaml:"size_reduction"`
	PauseSide           bool    `yaml:"pause_side"`
	Action              string  `yaml:"action"`
	TakeProfitReduction float64 `yaml:"take_profit_reduction,omitempty"`
	EmergencyClose      bool    `yaml:"emergency_close,omitempty"`
}

type TakeProfitAdjustments struct {
	Skew05_07   float64 `yaml:"skew_0.5_0.7"`
	Skew07_09   float64 `yaml:"skew_0.7_0.9"`
	SkewAbove09 float64 `yaml:"skew_above_0.9"`
}

type RebalancingConfig struct {
	CloseFurthestFirst bool `yaml:"close_furthest_first"`
	BreakevenExit      bool `yaml:"breakeven_exit_enabled"`
	MaxPositionsClose  int  `yaml:"max_positions_to_close"`
}

// ClusterStopLossConfig mirrors adaptive_grid.ClusterStopLossConfig
type ClusterStopLossConfig struct {
	Enabled            bool                `yaml:"enabled"`
	TimeThresholds     TimeThresholds      `yaml:"time_thresholds"`
	DrawdownThresholds DrawdownThresholds  `yaml:"drawdown_thresholds"`
	BreakevenExit      BreakevenExitConfig `yaml:"breakeven_exit"`
	ClusterDef         ClusterDefinition   `yaml:"cluster_definition"`
	HeatMap            HeatMapConfig       `yaml:"heat_map"`
	Alerts             ClusterAlerts       `yaml:"alerts"`
}

type TimeThresholds struct {
	Monitor   float64 `yaml:"monitor_hours"`
	Emergency float64 `yaml:"emergency_hours"`
	Stale     float64 `yaml:"stale_hours"`
}

type DrawdownThresholds struct {
	Monitor   float64 `yaml:"monitor"`
	Emergency float64 `yaml:"emergency"`
}

type BreakevenExitConfig struct {
	Enabled              bool    `yaml:"enabled"`
	Close50PctAt         float64 `yaml:"close_50_pct_at"`
	MinDrawdownFor50Pct  float64 `yaml:"min_drawdown_for_50_pct"`
	Close100PctAt        float64 `yaml:"close_100_pct_at"`
	MinDrawdownFor100Pct float64 `yaml:"min_drawdown_for_100_pct"`
}

type ClusterDefinition struct {
	MaxLevelsPerCluster      int     `yaml:"max_levels_per_cluster"`
	MaxDistanceBetweenLevels float64 `yaml:"max_distance_between_levels"`
}

type HeatMapConfig struct {
	Enabled                bool   `yaml:"enabled"`
	LogInterval            string `yaml:"log_interval"`
	IncludeRecommendations bool   `yaml:"include_recommendations"`
}

type ClusterAlerts struct {
	OnEmergencyClose bool `yaml:"on_emergency_close"`
	OnStaleClose     bool `yaml:"on_stale_close"`
	OnBreakevenExit  bool `yaml:"on_breakeven_exit"`
}

// TrendDetectionConfig mirrors adaptive_grid.TrendDetectionConfig
type TrendDetectionConfig struct {
	Enabled         bool                `yaml:"enabled"`
	RSI             RSIConfig           `yaml:"rsi"`
	Thresholds      RSIThresholdsConfig `yaml:"thresholds"`
	Persistence     PersistenceConfig   `yaml:"persistence"`
	TrendScore      TrendScoreConfig    `yaml:"trend_score"`
	Pause           PauseConfig         `yaml:"pause"`
	CounterTrend    CounterTrendConfig  `yaml:"counter_trend_reduction"`
	Exhaustion      ExhaustionConfig    `yaml:"exhaustion"`
	LogTrendChanges bool                `yaml:"log_trend_changes"`
	LogInterval     string              `yaml:"log_interval"`
}

type RSIConfig struct {
	Period         int    `yaml:"period"`
	Timeframe      string `yaml:"timeframe"`
	UpdateInterval string `yaml:"update_interval"`
}

type RSIThresholdsConfig struct {
	StrongOverbought float64 `yaml:"strong_overbought"`
	MildOverbought   float64 `yaml:"mild_overbought"`
	NeutralHigh      float64 `yaml:"neutral_high"`
	NeutralLow       float64 `yaml:"neutral_low"`
	MildOversold     float64 `yaml:"mild_oversold"`
	StrongOversold   float64 `yaml:"strong_oversold"`
}

type PersistenceConfig struct {
	RequiredDuration string `yaml:"required_duration"`
	MinConfirmations int    `yaml:"min_confirmations"`
}

type TrendScoreConfig struct {
	RSIContribution         int `yaml:"rsi_contribution"`
	EMAContribution         int `yaml:"ema_contribution"`
	VolumeContribution      int `yaml:"volume_contribution"`
	PriceActionContribution int `yaml:"price_action_contribution"`
}

type PauseConfig struct {
	StrongTrendPauseDuration string `yaml:"strong_trend_pause_duration"`
	MildTrendPauseDuration   string `yaml:"mild_trend_pause_duration"`
	ScoreThresholdStrong     int    `yaml:"score_threshold_strong"`
	ScoreThresholdMild       int    `yaml:"score_threshold_mild"`
}

type CounterTrendConfig struct {
	Mild    float64 `yaml:"mild"`
	Strong  float64 `yaml:"strong"`
	Extreme float64 `yaml:"extreme"`
}

type ExhaustionConfig struct {
	Enabled          bool   `yaml:"enabled"`
	DetectDivergence bool   `yaml:"detect_divergence"`
	MinTrendDuration string `yaml:"min_trend_duration"`
}

// SafeguardsConfig mirrors safeguards configuration
type SafeguardsConfig struct {
	Enabled           bool                    `yaml:"enabled"`
	AntiReplay        AntiReplayConfig        `yaml:"anti_replay"`
	StateValidation   StateValidationConfig   `yaml:"state_validation"`
	SpreadProtection  SpreadProtectionConfig  `yaml:"spread_protection"`
	Slippage          SlippageConfig          `yaml:"slippage"`
	FundingProtection FundingProtectionConfig `yaml:"funding_protection"`
	CircuitBreaker    CircuitBreakerConfig    `yaml:"circuit_breaker"`
	Performance       PerformanceConfig       `yaml:"performance"`
	Alerts            SafeguardsAlerts        `yaml:"alerts"`
}

type AntiReplayConfig struct {
	Enabled             bool   `yaml:"enabled"`
	DeduplicationWindow string `yaml:"deduplication_window"`
	MaxStoredEvents     int    `yaml:"max_stored_events"`
	LockTimeout         string `yaml:"lock_timeout"`
	CleanupInterval     string `yaml:"cleanup_interval"`
}

type StateValidationConfig struct {
	Enabled               bool `yaml:"enabled"`
	LogInvalidTransitions bool `yaml:"log_invalid_transitions"`
	AlertOnInvalid        bool `yaml:"alert_on_invalid"`
}

type SpreadProtectionConfig struct {
	Enabled                bool    `yaml:"enabled"`
	PauseThreshold         float64 `yaml:"pause_threshold"`
	EmergencyThreshold     float64 `yaml:"emergency_threshold"`
	ResumeAfter            string  `yaml:"resume_after"`
	OrderbookCheckInterval string  `yaml:"orderbook_check_interval"`
	SamplesBeforeResume    int     `yaml:"samples_before_resume"`
}

type SlippageConfig struct {
	Enabled        bool    `yaml:"enabled"`
	AlertThreshold float64 `yaml:"alert_threshold"`
	MaxStoredFills int     `yaml:"max_stored_fills"`
}

type FundingProtectionConfig struct {
	Enabled         bool               `yaml:"enabled"`
	HighThreshold   float64            `yaml:"high_threshold"`
	CheckInterval   string             `yaml:"check_interval"`
	LevelAdjustment int                `yaml:"level_adjustment"`
	CostTracking    CostTrackingConfig `yaml:"cost_tracking"`
}

type CostTrackingConfig struct {
	Enabled         bool    `yaml:"enabled"`
	CompareToProfit bool    `yaml:"compare_to_profit"`
	AlertRatio      float64 `yaml:"alert_ratio"`
}

type CircuitBreakerConfig struct {
	Enabled                bool    `yaml:"enabled"`
	FallbackToSafeDefaults bool    `yaml:"fallback_to_safe_defaults"`
	SafeSpreadPct          float64 `yaml:"safe_spread_pct"`
	SafeSizeMultiplier     float64 `yaml:"safe_size_multiplier"`
	RetryInterval          string  `yaml:"retry_interval"`
	MaxRetries             int     `yaml:"max_retries"`
}

type PerformanceConfig struct {
	Enabled           bool `yaml:"enabled"`
	LogSlowOperations bool `yaml:"log_slow_operations"`
	SlowThreshold     int  `yaml:"slow_threshold"`
}

type SafeguardsAlerts struct {
	OnSpreadPause       bool `yaml:"on_spread_pause"`
	OnFundingHigh       bool `yaml:"on_funding_high"`
	OnDuplicateFill     bool `yaml:"on_duplicate_fill"`
	OnInvalidTransition bool `yaml:"on_invalid_transition"`
	OnCircuitBreaker    bool `yaml:"on_circuit_breaker"`
}

// TradingHoursConfig for time filter
type TradingHoursConfig struct {
	Enabled  bool       `yaml:"enabled"`
	Mode     string     `yaml:"mode"` // "all", "none", "select"
	Timezone string     `yaml:"timezone"`
	Slots    []TimeSlot `yaml:"slots"`
}

type TimeWindow struct {
	Start    string `yaml:"start"`
	End      string `yaml:"end"`
	Timezone string `yaml:"timezone"`
}

type TimeSlot struct {
	Window           TimeWindow `yaml:"window"`
	Enabled          bool       `yaml:"enabled"`
	SizeMultiplier   float64    `yaml:"size_multiplier"`
	MaxExposurePct   float64    `yaml:"max_exposure_pct"`
	SpreadMultiplier float64    `yaml:"spread_multiplier"`
	Description      string     `yaml:"description"`
}

// LoadOptimizationConfig loads all optimization configs from directory
func LoadOptimizationConfig(configPath string) (*OptimizationConfig, error) {
	config := &OptimizationConfig{}

	// Load dynamic grid config
	if err := loadYAML(configPath+"/dynamic_grid.yaml", &config.DynamicGrid); err != nil {
		return nil, fmt.Errorf("failed to load dynamic_grid config: %w", err)
	}

	// Load inventory skew config
	if err := loadYAML(configPath+"/inventory_skew.yaml", &config.InventorySkew); err != nil {
		return nil, fmt.Errorf("failed to load inventory_skew config: %w", err)
	}

	// Load cluster stop-loss config
	if err := loadYAML(configPath+"/cluster_stoploss.yaml", &config.ClusterStopLoss); err != nil {
		return nil, fmt.Errorf("failed to load cluster_stoploss config: %w", err)
	}

	// Load trend detection config
	if err := loadYAML(configPath+"/trend_detection.yaml", &config.TrendDetection); err != nil {
		return nil, fmt.Errorf("failed to load trend_detection config: %w", err)
	}

	// Load safeguards config
	if err := loadYAML(configPath+"/safeguards.yaml", &config.Safeguards); err != nil {
		return nil, fmt.Errorf("failed to load safeguards config: %w", err)
	}

	// Load time filter config
	if err := loadYAML(configPath+"/trading_hours.yaml", &config.TimeFilter); err != nil {
		return nil, fmt.Errorf("failed to load trading_hours config: %w", err)
	}

	return config, nil
}

// loadYAML loads a single YAML file
func loadYAML(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to parse YAML %s: %w", path, err)
	}

	return nil
}
