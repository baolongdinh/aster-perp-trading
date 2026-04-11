package agent

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentConfig holds all configuration for the intelligent trading agent
type AgentConfig struct {
	Agent AgentSettings `yaml:"agent"`
}

type AgentSettings struct {
	Enabled         bool                  `yaml:"enabled"`
	RegimeDetection RegimeDetectionConfig `yaml:"regime_detection"`
	Factors         FactorsConfig         `yaml:"factors"`
	PositionSizing  PositionSizingConfig  `yaml:"position_sizing"`
	CircuitBreakers CircuitBreakersConfig `yaml:"circuit_breakers"`
	Patterns        PatternsConfig        `yaml:"patterns"`
	Logging         LoggingConfig         `yaml:"logging"`
	Alerting        AlertingConfig        `yaml:"alerting"`
}

type RegimeDetectionConfig struct {
	UpdateInterval time.Duration    `yaml:"update_interval"`
	ADXPeriod      int              `yaml:"adx_period"`
	BBPeriod       int              `yaml:"bb_period"`
	ATRPeriod      int              `yaml:"atr_period"`
	Thresholds     RegimeThresholds `yaml:"thresholds"`
}

type RegimeThresholds struct {
	SidewaysADXMax   float64 `yaml:"sideways_adx_max"`
	TrendingADXMin   float64 `yaml:"trending_adx_min"`
	VolatileATRSpike float64 `yaml:"volatile_atr_spike"`
}

type FactorsConfig struct {
	Weights FactorWeights `yaml:"weights"`
	Scoring ScoringConfig `yaml:"scoring"`
}

type FactorWeights struct {
	Trend      float64 `yaml:"trend"`
	Volatility float64 `yaml:"volatility"`
	Volume     float64 `yaml:"volume"`
	Structure  float64 `yaml:"structure"`
}

type ScoringConfig struct {
	DeployFullThreshold    float64 `yaml:"deploy_full_threshold"`
	DeployReducedThreshold float64 `yaml:"deploy_reduced_threshold"`
	WaitThreshold          float64 `yaml:"wait_threshold"`
}

type PositionSizingConfig struct {
	ScoreMultipliers      ScoreMultipliers      `yaml:"score_multipliers"`
	VolatilityMultipliers VolatilityMultipliers `yaml:"volatility_multipliers"`
	GridSpacing           GridSpacingConfig     `yaml:"grid_spacing"`
}

type ScoreMultipliers struct {
	Full    float64 `yaml:"full"`
	Reduced float64 `yaml:"reduced"`
	None    float64 `yaml:"none"`
}

type VolatilityMultipliers struct {
	Normal  float64 `yaml:"normal"`
	High    float64 `yaml:"high"`
	Extreme float64 `yaml:"extreme"`
}

type GridSpacingConfig struct {
	LowVol    float64 `yaml:"low_vol"`
	NormalVol float64 `yaml:"normal_vol"`
	HighVol   float64 `yaml:"high_vol"`
}

type CircuitBreakersConfig struct {
	VolatilitySpike   VolatilityBreakerConfig `yaml:"volatility_spike"`
	LiquidityCrisis   LiquidityBreakerConfig  `yaml:"liquidity_crisis"`
	ConsecutiveLosses LossesBreakerConfig     `yaml:"consecutive_losses"`
	DrawdownLimit     DrawdownBreakerConfig   `yaml:"drawdown_limit"`
	ConnectionFailure ConnectionBreakerConfig `yaml:"connection_failure"`
}

type VolatilityBreakerConfig struct {
	Enabled       bool          `yaml:"enabled"`
	ATRMultiplier float64       `yaml:"atr_multiplier"`
	Window        time.Duration `yaml:"window"`
}

type LiquidityBreakerConfig struct {
	Enabled          bool    `yaml:"enabled"`
	SpreadMultiplier float64 `yaml:"spread_multiplier"`
}

type LossesBreakerConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Threshold     int     `yaml:"threshold"`
	SizeReduction float64 `yaml:"size_reduction"`
}

type DrawdownBreakerConfig struct {
	Enabled     bool    `yaml:"enabled"`
	MaxDrawdown float64 `yaml:"max_drawdown"`
}

type ConnectionBreakerConfig struct {
	Enabled     bool `yaml:"enabled"`
	MaxFailures int  `yaml:"max_failures"`
}

type PatternsConfig struct {
	Enabled             bool    `yaml:"enabled"`
	StoragePath         string  `yaml:"storage_path"`
	MinTradesToActivate int     `yaml:"min_trades_to_activate"`
	DecayHalfLifeDays   int     `yaml:"decay_half_life_days"`
	MaxImpactPoints     float64 `yaml:"max_impact_points"`
	SimilarityThreshold float64 `yaml:"similarity_threshold"`
}

type LoggingConfig struct {
	RetentionDays int    `yaml:"retention_days"`
	LogLevel      string `yaml:"log_level"`
	LogDecisions  bool   `yaml:"log_decisions"`
}

type AlertingConfig struct {
	Enabled          bool          `yaml:"enabled"`
	WebhookURL       string        `yaml:"webhook_url"`
	OnRegimeChange   bool          `yaml:"on_regime_change"`
	OnCircuitBreaker bool          `yaml:"on_circuit_breaker"`
	OnHighDrawdown   bool          `yaml:"on_high_drawdown"`
	RateLimitWindow  time.Duration `yaml:"rate_limit_window"` // Default: 5m
}

// Note: Telegram config now loaded from env vars:
// TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID, TELEGRAM_ENABLED

// LoadConfig loads the agent configuration from a YAML file
func LoadConfig(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config AgentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults where needed
	config.applyDefaults()

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// applyDefaults sets default values for optional configuration
func (c *AgentConfig) applyDefaults() {
	if c.Agent.RegimeDetection.UpdateInterval == 0 {
		c.Agent.RegimeDetection.UpdateInterval = 30 * time.Second
	}
	if c.Agent.RegimeDetection.ADXPeriod == 0 {
		c.Agent.RegimeDetection.ADXPeriod = 14
	}
	if c.Agent.RegimeDetection.BBPeriod == 0 {
		c.Agent.RegimeDetection.BBPeriod = 20
	}
	if c.Agent.RegimeDetection.ATRPeriod == 0 {
		c.Agent.RegimeDetection.ATRPeriod = 14
	}
	if c.Agent.Patterns.DecayHalfLifeDays == 0 {
		c.Agent.Patterns.DecayHalfLifeDays = 14
	}
	if c.Agent.Patterns.MinTradesToActivate == 0 {
		c.Agent.Patterns.MinTradesToActivate = 200
	}
	if c.Agent.Patterns.MaxImpactPoints == 0 {
		c.Agent.Patterns.MaxImpactPoints = 5
	}
}

// Validate checks that the configuration is valid
func (c *AgentConfig) Validate() error {
	// Check factor weights sum to 1.0
	weights := c.Agent.Factors.Weights
	total := weights.Trend + weights.Volatility + weights.Volume + weights.Structure
	if total < 0.99 || total > 1.01 {
		return fmt.Errorf("factor weights must sum to 1.0, got %.2f", total)
	}

	// Check thresholds are valid
	scoring := c.Agent.Factors.Scoring
	if scoring.DeployFullThreshold <= scoring.DeployReducedThreshold {
		return fmt.Errorf("deploy_full_threshold must be > deploy_reduced_threshold")
	}
	if scoring.DeployReducedThreshold <= scoring.WaitThreshold {
		return fmt.Errorf("deploy_reduced_threshold must be > wait_threshold")
	}

	// Check multipliers are valid
	scoreMult := c.Agent.PositionSizing.ScoreMultipliers
	if scoreMult.Full <= scoreMult.Reduced || scoreMult.Reduced <= scoreMult.None {
		return fmt.Errorf("score multipliers must be decreasing: full > reduced > none")
	}

	return nil
}

// GetFactorWeight returns the weight for a specific factor
func (c *AgentConfig) GetFactorWeight(factor FactorType) float64 {
	switch factor {
	case FactorTrend:
		return c.Agent.Factors.Weights.Trend
	case FactorVolatility:
		return c.Agent.Factors.Weights.Volatility
	case FactorVolume:
		return c.Agent.Factors.Weights.Volume
	case FactorStructure:
		return c.Agent.Factors.Weights.Structure
	default:
		return 0
	}
}

// GetScoreMultiplier returns the multiplier for a given score
func (c *AgentConfig) GetScoreMultiplier(score float64) float64 {
	scoring := c.Agent.Factors.Scoring
	if score >= scoring.DeployFullThreshold {
		return c.Agent.PositionSizing.ScoreMultipliers.Full
	}
	if score >= scoring.DeployReducedThreshold {
		return c.Agent.PositionSizing.ScoreMultipliers.Reduced
	}
	return c.Agent.PositionSizing.ScoreMultipliers.None
}

// GetVolatilityMultiplier returns the multiplier for volatility level
func (c *AgentConfig) GetVolatilityMultiplier(atr float64, atrMA float64) float64 {
	atrRatio := atr / atrMA
	// ATR > 3× spike is extreme (circuit breaker)
	if atrRatio >= 3.0 {
		return c.Agent.PositionSizing.VolatilityMultipliers.Extreme
	}
	// ATR > 1.5× is high volatility
	if atrRatio >= 1.5 {
		return c.Agent.PositionSizing.VolatilityMultipliers.High
	}
	return c.Agent.PositionSizing.VolatilityMultipliers.Normal
}
