package agentic

import (
	"time"
)

// AgenticConfig holds the agentic decision layer configuration
type AgenticConfig struct {
	Enabled              bool                  `mapstructure:"enabled" yaml:"enabled"`
	Universe             UniverseConfig        `mapstructure:"universe" yaml:"universe"`
	RegimeDetection      RegimeDetectionConfig `mapstructure:"regime_detection" yaml:"regime_detection"`
	Scoring              ScoringConfig         `mapstructure:"scoring" yaml:"scoring"`
	PositionSizing       PositionSizingConfig  `mapstructure:"position_sizing" yaml:"position_sizing"`
	WhitelistManagement  WhitelistConfig       `mapstructure:"whitelist_management" yaml:"whitelist_management"`
	CircuitBreakers      CircuitBreakerConfig  `mapstructure:"circuit_breakers" yaml:"circuit_breakers"`
}

// UniverseConfig defines the symbol universe for monitoring
type UniverseConfig struct {
	Symbols          []string `mapstructure:"symbols" yaml:"symbols"`
	AutoDiscover     bool     `mapstructure:"auto_discover" yaml:"auto_discover"`
	TopVolumeCount   int      `mapstructure:"top_volume_count" yaml:"top_volume_count"`
	Min24hVolumeUSD  float64  `mapstructure:"min_24h_volume_usd" yaml:"min_24h_volume_usd"`
}

// RegimeDetectionConfig configures regime detection parameters
type RegimeDetectionConfig struct {
	UpdateInterval time.Duration         `mapstructure:"update_interval" yaml:"update_interval"`
	ADXPeriod      int                   `mapstructure:"adx_period" yaml:"adx_period"`
	BBPeriod       int                   `mapstructure:"bb_period" yaml:"bb_period"`
	ATRPeriod      int                   `mapstructure:"atr_period" yaml:"atr_period"`
	CandleInterval string                `mapstructure:"candle_interval" yaml:"candle_interval"`
	Thresholds     RegimeThresholdsConfig  `mapstructure:"thresholds" yaml:"thresholds"`
}

// RegimeThresholdsConfig defines thresholds for regime classification
type RegimeThresholdsConfig struct {
	SidewaysADXMax   float64 `mapstructure:"sideways_adx_max" yaml:"sideways_adx_max"`
	TrendingADXMin   float64 `mapstructure:"trending_adx_min" yaml:"trending_adx_min"`
	VolatileATRSpike float64 `mapstructure:"volatile_atr_spike" yaml:"volatile_atr_spike"`
}

// ScoringConfig configures the opportunity scoring system
type ScoringConfig struct {
	Weights    ScoringWeightsConfig    `mapstructure:"weights" yaml:"weights"`
	Thresholds  ScoringThresholdsConfig  `mapstructure:"thresholds" yaml:"thresholds"`
}

// ScoringWeightsConfig defines weights for score calculation
type ScoringWeightsConfig struct {
	Trend      float64 `mapstructure:"trend" yaml:"trend"`
	Volatility float64 `mapstructure:"volatility" yaml:"volatility"`
	Volume     float64 `mapstructure:"volume" yaml:"volume"`
	Structure  float64 `mapstructure:"structure" yaml:"structure"`
}

// ScoringThresholdsConfig defines score thresholds for recommendations
type ScoringThresholdsConfig struct {
	HighScore   float64 `mapstructure:"high_score" yaml:"high_score"`
	MediumScore float64 `mapstructure:"medium_score" yaml:"medium_score"`
	LowScore    float64 `mapstructure:"low_score" yaml:"low_score"`
	SkipScore   float64 `mapstructure:"skip_score" yaml:"skip_score"`
}

// PositionSizingConfig configures dynamic position sizing
type PositionSizingConfig struct {
	ScoreMultipliers   map[string]float64 `mapstructure:"score_multipliers" yaml:"score_multipliers"`
	RegimeMultipliers  map[string]float64 `mapstructure:"regime_multipliers" yaml:"regime_multipliers"`
}

// WhitelistConfig configures whitelist management
type WhitelistConfig struct {
	MaxSymbols            int    `mapstructure:"max_symbols" yaml:"max_symbols"`
	MinScoreToAdd         float64 `mapstructure:"min_score_to_add" yaml:"min_score_to_add"`
	ScoreToRemove         float64 `mapstructure:"score_to_remove" yaml:"score_to_remove"`
	KeepIfPositionOpen    bool   `mapstructure:"keep_if_position_open" yaml:"keep_if_position_open"`
	ReplaceImmediately    bool   `mapstructure:"replace_immediately" yaml:"replace_immediately"`
}

// CircuitBreakerConfig configures circuit breaker rules
type CircuitBreakerConfig struct {
	VolatilitySpike    VolatilityBreakerConfig    `mapstructure:"volatility_spike" yaml:"volatility_spike"`
	ConsecutiveLosses  ConsecutiveLossBreakerConfig `mapstructure:"consecutive_losses" yaml:"consecutive_losses"`
}

// VolatilityBreakerConfig configures volatility-based circuit breaker
type VolatilityBreakerConfig struct {
	Enabled       bool    `mapstructure:"enabled" yaml:"enabled"`
	ATRMultiplier float64 `mapstructure:"atr_multiplier" yaml:"atr_multiplier"`
}

// ConsecutiveLossBreakerConfig configures loss-based circuit breaker
type ConsecutiveLossBreakerConfig struct {
	Enabled         bool    `mapstructure:"enabled" yaml:"enabled"`
	Threshold       int     `mapstructure:"threshold" yaml:"threshold"`
	SizeReduction   float64 `mapstructure:"size_reduction" yaml:"size_reduction"`
}

// DefaultAgenticConfig returns sensible defaults
func DefaultAgenticConfig() *AgenticConfig {
	return &AgenticConfig{
		Enabled: true,
		Universe: UniverseConfig{
			Symbols: []string{
				"BTCUSD1", "ETHUSD1", "SOLUSD1", "LINKUSD1",
				"DOGEUSD1", "XRPUSD1", "ADAUSD1", "AVAXUSD1",
				"MATICUSD1", "DOTUSD1",
			},
			AutoDiscover:    false,
			TopVolumeCount:  20,
			Min24hVolumeUSD: 10000000,
		},
		RegimeDetection: RegimeDetectionConfig{
			UpdateInterval: 30 * time.Second,
			ADXPeriod:      14,
			BBPeriod:       20,
			ATRPeriod:      14,
			CandleInterval: "5m",
			Thresholds: RegimeThresholdsConfig{
				SidewaysADXMax:   25,
				TrendingADXMin:   25,
				VolatileATRSpike: 3.0,
			},
		},
		Scoring: ScoringConfig{
			Weights: ScoringWeightsConfig{
				Trend:      0.30,
				Volatility: 0.25,
				Volume:     0.25,
				Structure:  0.20,
			},
			Thresholds: ScoringThresholdsConfig{
				HighScore:   75,
				MediumScore: 60,
				LowScore:    40,
				SkipScore:   0,
			},
		},
		PositionSizing: PositionSizingConfig{
			ScoreMultipliers: map[string]float64{
				"HIGH":   1.0,
				"MEDIUM": 0.6,
				"LOW":    0.3,
			},
			RegimeMultipliers: map[string]float64{
				"SIDEWAYS": 1.0,
				"TRENDING": 0.7,
				"VOLATILE": 0.5,
				"RECOVERY": 0.8,
			},
		},
		WhitelistManagement: WhitelistConfig{
			MaxSymbols:         5,
			MinScoreToAdd:      60,
			ScoreToRemove:      35,
			KeepIfPositionOpen: true,
			ReplaceImmediately: true,
		},
		CircuitBreakers: CircuitBreakerConfig{
			VolatilitySpike: VolatilityBreakerConfig{
				Enabled:       true,
				ATRMultiplier: 3.0,
			},
			ConsecutiveLosses: ConsecutiveLossBreakerConfig{
				Enabled:       true,
				Threshold:     3,
				SizeReduction: 0.5,
			},
		},
	}
}
