package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config is the root config for the bot.
type Config struct {
	Bot           BotConfig           `mapstructure:"bot"`
	Exchange      ExchangeConfig      `mapstructure:"exchange"`
	Risk          RiskConfig          `mapstructure:"risk"`
	API           APIConfig           `mapstructure:"api"`
	Log           LogConfig           `mapstructure:"log"`
	Strategies    []StrategyConfig    `mapstructure:"strategies"`
	VolumeFarming *VolumeFarmConfig   `mapstructure:"volume_farming,omitempty"`
	Optimization  *OptimizationConfig `mapstructure:"optimization,omitempty"`
	Agentic       *AgenticConfig      `mapstructure:"agentic,omitempty"`
}

type BotConfig struct {
	DryRun bool `mapstructure:"dry_run"`
}

type ExchangeConfig struct {
	// V1 Authentication (deprecated)
	APIKey    string `mapstructure:"api_key"`
	APISecret string `mapstructure:"-"` // loaded from env only (ASTER_API_SECRET)

	// V3 Authentication - API Wallet/Agent model
	UserWallet   string `mapstructure:"user_wallet"` // main wallet address
	APISigner    string `mapstructure:"api_signer"`  // API wallet address
	APISignerKey string `mapstructure:"-"`           // loaded from env only (ASTER_API_SIGNER_KEY)

	FuturesRESTBase   string `mapstructure:"futures_rest_base"`
	FuturesWSBase     string `mapstructure:"futures_ws_base"`
	RecvWindow        int    `mapstructure:"recv_window"` // ms, default 5000
	RequestsPerSecond int    `mapstructure:"requests_per_second"`
}

type RiskConfig struct {
	MaxPositionUSDTPerSymbol    float64                         `mapstructure:"max_position_usdt_per_symbol"`
	MaxTotalPositionsUSDT       float64                         `mapstructure:"max_total_positions_usdt"`
	FeeLossThresholdPct         float64                         `mapstructure:"fee_loss_threshold_pct"`
	PositionTimeoutMinutes      int                             `mapstructure:"position_timeout_minutes"`
	MaxPositionUSDT             float64                         `mapstructure:"max_position_usdt"`
	MaxOpenPositions            int                             `mapstructure:"max_open_positions"`
	MaxTradesPerSymbol          int                             `mapstructure:"max_trades_per_symbol"`
	MaxGlobalPendingLimitOrders int                             `mapstructure:"max_global_pending_limit_orders"`
	MaxPendingPerSide           int                             `mapstructure:"max_pending_per_side"`
	DailyLossLimitUSDT          float64                         `mapstructure:"daily_loss_limit_usdt"`
	DailyDrawdownPct            float64                         `mapstructure:"daily_drawdown_pct"`
	PerTradeStopLossPct         float64                         `mapstructure:"per_trade_stop_loss_pct"`
	PerTradeTakeProfitPct       float64                         `mapstructure:"per_trade_take_profit_pct"`
	TakeProfitRRatio            float64                         `mapstructure:"take_profit_rr_ratio"` // Target R:R ratio (e.g., 1.5 = 1.5:1)
	MinTakeProfitPct            float64                         `mapstructure:"min_take_profit_pct"`  // Minimum TP as % (e.g., 0.01 = 1%)
	MaxTakeProfitPct            float64                         `mapstructure:"max_take_profit_pct"`  // Maximum TP as % (e.g., 0.05 = 5%)
	RiskPerTradeUSDT            float64                         `mapstructure:"risk_per_trade_usdt"`
	ATRMultiplier               float64                         `mapstructure:"atr_multiplier"`
	PositionMode                string                          `mapstructure:"position_mode"` // one_way | hedge
	CorrelationThreshold        float64                         `mapstructure:"correlation_threshold"`
	MakerPriority               bool                            `mapstructure:"maker_priority"`
	PnLRiskControl              *PnLRiskControlConfig           `mapstructure:"pnl_risk_control"`
	MarketConditionEvaluator    *MarketConditionEvaluatorConfig `mapstructure:"market_condition_evaluator"`
	OverSize                    *OverSizeConfig                 `mapstructure:"over_size"`
	DefensiveState              *DefensiveStateConfig           `mapstructure:"defensive_state"`
	RecoveryState               *RecoveryStateConfig            `mapstructure:"recovery_state"`
}

type PnLRiskControlConfig struct {
	Enabled               bool    `mapstructure:"enabled"`
	PartialLossUSDT       float64 `mapstructure:"partial_loss_usdt"`
	FullLossUSDT          float64 `mapstructure:"full_loss_usdt"`
	RecoveryThresholdUSDT float64 `mapstructure:"recovery_threshold_usdt"`
	PartialClosePct       float64 `mapstructure:"partial_close_pct"`
}

type MarketConditionEvaluatorConfig struct {
	Enabled                bool    `mapstructure:"enabled"`
	EvaluationIntervalSec  int     `mapstructure:"evaluation_interval_sec"`
	MinConfidenceThreshold float64 `mapstructure:"min_confidence_threshold"`
	StateStabilityDuration int     `mapstructure:"state_stability_duration"`
}

type OverSizeConfig struct {
	ThresholdPct float64 `mapstructure:"threshold_pct"` // Percentage of MaxPositionUSDT to trigger OVER_SIZE (e.g., 0.8 = 80%)
	RecoveryPct  float64 `mapstructure:"recovery_pct"`  // Percentage to exit OVER_SIZE (e.g., 0.6 = 60%)
}

type DefensiveStateConfig struct {
	ATRMultiplierThreshold float64 `mapstructure:"atr_multiplier_threshold"` // ATR multiplier for DEFENSIVE (e.g., 3.0)
	BBWidthThreshold       float64 `mapstructure:"bb_width_threshold"`       // BB width % for DEFENSIVE (e.g., 0.05)
	SpreadMultiplier       float64 `mapstructure:"spread_multiplier"`        // Spread multiplier in DEFENSIVE (e.g., 2.0)
	SLMultiplier           float64 `mapstructure:"sl_multiplier"`            // SL multiplier in DEFENSIVE (e.g., 0.8)
	AllowNewPositions      bool    `mapstructure:"allow_new_positions"`      // Allow new positions in DEFENSIVE
}

type RecoveryStateConfig struct {
	RecoveryThresholdUSDT float64 `mapstructure:"recovery_threshold_usdt"` // PnL threshold for RECOVERY
	SizeMultiplier        float64 `mapstructure:"size_multiplier"`         // Order size multiplier in RECOVERY (e.g., 0.5)
	SpreadMultiplier      float64 `mapstructure:"spread_multiplier"`       // Spread multiplier in RECOVERY (e.g., 1.5)
	StableDurationMin     int     `mapstructure:"stable_duration_min"`     // Minimum stable PnL duration (minutes)
}

type APIConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type LogConfig struct {
	Level string `mapstructure:"level"` // debug | info | warn | error
	File  string `mapstructure:"file"`  // path to log file
}

type StrategyConfig struct {
	Name    string                 `mapstructure:"name"`
	Enabled bool                   `mapstructure:"enabled"`
	Symbols []string               `mapstructure:"symbols"`
	Params  map[string]interface{} `mapstructure:"params"`
}

// Load reads config from a YAML file + environment variables.
// Env vars override YAML. Private key is always from env only.
func Load(cfgPath string) (*Config, error) {
	// Load .env file if present
	_ = godotenv.Load(".env")

	v := viper.New()

	// Defaults
	v.SetDefault("bot.dry_run", true)
	v.SetDefault("exchange.futures_rest_base", "https://fapi.asterdex.com")
	v.SetDefault("exchange.futures_ws_base", "wss://fstream.asterdex.com")
	v.SetDefault("exchange.recv_window", 5000)
	v.SetDefault("exchange.requests_per_second", 10)
	v.SetDefault("api.host", "0.0.0.0")
	v.SetDefault("api.port", 8080)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.file", "logs/bot.log")

	v.SetDefault("risk.position_mode", "one_way")

	// Volume farming defaults - optimized for high volume
	v.SetDefault("volume_farming.enabled", true)
	v.SetDefault("volume_farming.max_daily_loss_usdt", 200)
	v.SetDefault("volume_farming.order_size_usdt", 5)      // Small orders
	v.SetDefault("volume_farming.grid_spread_pct", 0.01)   // Tight spread
	v.SetDefault("volume_farming.max_orders_per_side", 30) // Many orders
	v.SetDefault("volume_farming.replace_immediately", true)
	v.SetDefault("volume_farming.position_timeout_minutes", 10) // Fast turnover
	v.SetDefault("volume_farming.ticker_stream", "!ticker@arr")
	v.SetDefault("volume_farming.symbol_refresh_interval_seconds", 30) // 30s refresh
	v.SetDefault("volume_farming.grid_placement_cooldown_seconds", 1)  // 1s cooldown
	v.SetDefault("volume_farming.rate_limit_cooldown_seconds", 3)      // 3s recovery
	v.SetDefault("volume_farming.supported_quote_currencies", []string{"USD1"})
	v.SetDefault("volume_farming.min_volume_24h", 1000000)
	v.SetDefault("volume_farming.symbols.auto_discover", true)
	v.SetDefault("volume_farming.symbols.quote_currency_mode", "flexible")
	v.SetDefault("volume_farming.symbols.min_volume_24h", 1000000)
	v.SetDefault("volume_farming.symbols.max_spread_pct", 10.0) // 10% spread threshold
	v.SetDefault("volume_farming.symbols.boosted_only", false)
	v.SetDefault("volume_farming.symbols.max_symbols_per_quote", 10)
	v.SetDefault("volume_farming.symbols.spread_ranking", true)
	v.SetDefault("volume_farming.symbols.volume_weighting", 0.7)
	v.SetDefault("volume_farming.symbols.min_liquidity_score", 0.0) // Allow all by default
	v.SetDefault("volume_farming.symbols.exclude_high_fee_symbols", true)
	v.SetDefault("volume_farming.symbols.allow_mixed_quotes", true)
	v.SetDefault("volume_farming.symbols.quote_currencies", []string{"USD1"})
	v.SetDefault("volume_farming.symbols.whitelist", []string{})

	// YAML file
	if cfgPath != "" {
		v.SetConfigFile(cfgPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: read file %s: %w", cfgPath, err)
		}
	}

	// Env overrides: ASTER_BOT_DRY_RUN -> bot.dry_run etc.
	v.SetEnvPrefix("ASTER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// API Secret: env only (V1)
	cfg.Exchange.APISecret = os.Getenv("ASTER_API_SECRET")

	// V3 Credentials: env only for security
	if cfg.Exchange.UserWallet == "" {
		cfg.Exchange.UserWallet = os.Getenv("ASTER_USER_WALLET")
	}
	if cfg.Exchange.APISigner == "" {
		cfg.Exchange.APISigner = os.Getenv("ASTER_API_SIGNER")
	}
	cfg.Exchange.APISignerKey = os.Getenv("ASTER_API_SIGNER_KEY")

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config: validation: %w", err)
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	// Check for either V1 or V3 authentication
	hasV1Auth := cfg.Exchange.APIKey != "" && cfg.Exchange.APISecret != ""
	hasV3Auth := cfg.Exchange.UserWallet != "" && cfg.Exchange.APISigner != "" && cfg.Exchange.APISignerKey != ""

	if !hasV1Auth && !hasV3Auth {
		return fmt.Errorf("either V1 (api_key + ASTER_API_SECRET) or V3 (user_wallet + api_signer + ASTER_API_SIGNER_KEY) authentication is required")
	}

	if cfg.Risk.MaxPositionUSDT <= 0 {
		cfg.Risk.MaxPositionUSDT = 500
	}
	if cfg.Risk.MaxOpenPositions <= 0 {
		cfg.Risk.MaxOpenPositions = 5
	}
	if cfg.Exchange.RecvWindow <= 0 {
		cfg.Exchange.RecvWindow = 5000
	}
	if cfg.Exchange.RequestsPerSecond <= 0 {
		cfg.Exchange.RequestsPerSecond = 5
	}
	if cfg.Risk.DailyLossLimitUSDT <= 0 {
		cfg.Risk.DailyLossLimitUSDT = 100
	}
	if cfg.Risk.DailyDrawdownPct <= 0 {
		cfg.Risk.DailyDrawdownPct = 5.0 // 5% max daily drawdown
	}
	if cfg.Risk.CorrelationThreshold <= 0 {
		cfg.Risk.CorrelationThreshold = 0.8
	}
	if cfg.Risk.RiskPerTradeUSDT <= 0 {
		cfg.Risk.RiskPerTradeUSDT = 10.0 // Risk $10 per signal by default
	}
	if cfg.Risk.ATRMultiplier <= 0 {
		cfg.Risk.ATRMultiplier = 2.0 // Stop loss = 2 * ATR
	}
	cfg.Risk.MakerPriority = true // Default to true
	_ = time.Now()                // suppress import
	return nil
}

// VolumeFarmConfig contains volume farming specific configuration
type VolumeFarmConfig struct {
	Enabled                  bool                `mapstructure:"enabled"`
	MaxDailyLossUSDT         float64             `mapstructure:"max_daily_loss_usdt"`
	MaxTotalDrawdownPct      float64             `mapstructure:"max_total_drawdown_pct"`
	OrderSizeUSDT            float64             `mapstructure:"order_size_usdt"`
	GridSpreadPct            float64             `mapstructure:"grid_spread_pct"`
	MaxOrdersPerSide         int                 `mapstructure:"max_orders_per_side"`
	ReplaceImmediately       bool                `mapstructure:"replace_immediately"`
	PositionTimeoutMinutes   int                 `mapstructure:"position_timeout_minutes"`
	TickerStream             string              `mapstructure:"ticker_stream"`
	SymbolRefreshIntervalSec int                 `mapstructure:"symbol_refresh_interval_seconds"`
	GridPlacementCooldownSec int                 `mapstructure:"grid_placement_cooldown_seconds"`
	RateLimitCooldownSec     int                 `mapstructure:"rate_limit_cooldown_seconds"`
	RateLimiterCapacity      int                 `mapstructure:"rate_limiter_capacity"`
	RateLimiterRefillRate    float64             `mapstructure:"rate_limiter_refill_rate"`
	SupportedQuoteCurrencies []string            `mapstructure:"supported_quote_currencies"`
	MinVolume24h             float64             `mapstructure:"min_volume_24h"`
	Bot                      BotConfig           `mapstructure:"bot"`
	Symbols                  SymbolsConfig       `mapstructure:"symbols"`
	Exchange                 ExchangeConfig      `mapstructure:"exchange"`
	Risk                     RiskConfig          `mapstructure:"risk"`
	API                      APIConfig           `mapstructure:"api"`
	TradingModes             *TradingModesConfig `mapstructure:"trading_modes,omitempty"`
	PartialClose             *PartialCloseConfig `mapstructure:"partial_close,omitempty"`
}

// TradingModesConfig holds configuration for all trading modes
type TradingModesConfig struct {
	MicroMode        MicroModeConfig        `mapstructure:"micro_mode"`
	StandardMode     StandardModeConfig     `mapstructure:"standard_mode"`
	TrendAdaptedMode TrendAdaptedModeConfig `mapstructure:"trend_adapted_mode"`
	CooldownMode     CooldownModeConfig     `mapstructure:"cooldown_mode"`
	Transitions      ModeTransitionsConfig  `mapstructure:"transitions"`
}

// MicroModeConfig for MICRO trading mode (bypass range gate)
type MicroModeConfig struct {
	Enabled            bool    `mapstructure:"enabled"`
	SizeMultiplier     float64 `mapstructure:"size_multiplier"`
	LevelCount         int     `mapstructure:"level_count"`
	SpreadMultiplier   float64 `mapstructure:"spread_multiplier"`
	MinATRMultiplier   float64 `mapstructure:"min_atr_multiplier"`
	MinModeDurationSec int     `mapstructure:"min_mode_duration_seconds"`
}

// StandardModeConfig for STANDARD trading mode (normal BB-based)
type StandardModeConfig struct {
	Enabled            bool    `mapstructure:"enabled"`
	SizeMultiplier     float64 `mapstructure:"size_multiplier"`
	LevelCount         int     `mapstructure:"level_count"`
	SpreadMultiplier   float64 `mapstructure:"spread_multiplier"`
	MinModeDurationSec int     `mapstructure:"min_mode_duration_seconds"`
}

// TrendAdaptedModeConfig for TREND_ADAPTED mode (reduced risk)
type TrendAdaptedModeConfig struct {
	Enabled            bool    `mapstructure:"enabled"`
	SizeMultiplier     float64 `mapstructure:"size_multiplier"`
	LevelCount         int     `mapstructure:"level_count"`
	SpreadMultiplier   float64 `mapstructure:"spread_multiplier"`
	TrendBiasEnabled   bool    `mapstructure:"trend_bias_enabled"`
	TrendBiasRatio     float64 `mapstructure:"trend_bias_ratio"`
	MinModeDurationSec int     `mapstructure:"min_mode_duration_seconds"`
}

// CooldownModeConfig for COOLDOWN mode (pause after exit)
type CooldownModeConfig struct {
	DurationSec int `mapstructure:"duration_seconds"`
}

// PartialCloseConfig holds configuration for partial take-profit strategy
type PartialCloseConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	TP1              TPLevelConfig `mapstructure:"tp1"`
	TP2              TPLevelConfig `mapstructure:"tp2"`
	TP3              TPLevelConfig `mapstructure:"tp3"`
	TrailingAfterTP2 bool          `mapstructure:"trailing_after_tp2"`
	TrailingDistance float64       `mapstructure:"trailing_distance"`
}

// ModeTransitionsConfig holds thresholds for mode switching
type ModeTransitionsConfig struct {
	ADXThresholdSideways      float64 `mapstructure:"adx_threshold_sideways"`
	ADXThresholdTrending      float64 `mapstructure:"adx_threshold_trending"`
	VolatilitySpikeMultiplier float64 `mapstructure:"volatility_spike_multiplier"`
	BreakoutConfirmations     int     `mapstructure:"breakout_confirmations"`
}

// SymbolsConfig contains symbol selection and management for volume farming
type SymbolsConfig struct {
	AutoDiscover              bool      `mapstructure:"auto_discover"`
	QuoteCurrencyMode         string    `mapstructure:"quote_currency_mode"`
	MinVolume24h              float64   `mapstructure:"min_volume_24h"`
	MaxSpreadPct              float64   `mapstructure:"max_spread_pct"`
	BoostedOnly               bool      `mapstructure:"boosted_only"`
	MaxSymbolsPerQuote        int       `mapstructure:"max_symbols_per_quote"`
	SpreadRanking             bool      `mapstructure:"spread_ranking"`
	VolumeWeighting           float64   `mapstructure:"volume_weighting"`
	MinLiquidityScore         float64   `mapstructure:"min_liquidity_score"`
	OptimalSpreadRange        []float64 `mapstructure:"optimal_spread_range"`
	SpreadVolatilityThreshold float64   `mapstructure:"spread_volatility_threshold"`
	ExcludeHighFeeSymbols     bool      `mapstructure:"exclude_high_fee_symbols"`
	QuoteCurrencies           []string  `mapstructure:"quote_currencies"`
	AllowMixedQuotes          bool      `mapstructure:"allow_mixed_quotes"`
	Whitelist                 []string  `mapstructure:"whitelist"`
	Blacklist                 []string  `mapstructure:"blacklist"`
}

// LoadVolumeFarming loads volume farming configuration from file
func LoadVolumeFarming(configPath string) (*VolumeFarmConfig, error) {
	viper.SetConfigFile(configPath)
	viper.AutomaticEnv()
	viper.SetEnvPrefix("FARMING")

	// Set defaults for volume farming
	setVolumeFarmDefaults()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read volume farming config: %w", err)
	}

	var cfg VolumeFarmConfig

	// Try to unmarshal from "volume_farming" section first (unified config)
	if viper.IsSet("volume_farming") {
		if err := viper.UnmarshalKey("volume_farming", &cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal volume_farming section: %w", err)
		}
	} else {
		// Fallback to root level (legacy config)
		if err := viper.Unmarshal(&cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal volume farming config: %w", err)
		}
	}

	if err := validateVolumeFarmConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid volume farming config: %w", err)
	}

	return &cfg, nil
}

// setVolumeFarmDefaults sets default values for volume farming - OPTIMIZED FOR HIGH VOLUME
func setVolumeFarmDefaults() {
	viper.SetDefault("enabled", true)
	viper.SetDefault("max_daily_loss_usdt", 200)
	viper.SetDefault("max_total_drawdown_pct", 15.0)
	viper.SetDefault("order_size_usdt", 5)      // Small orders for volume
	viper.SetDefault("grid_spread_pct", 0.01)   // Tight spread
	viper.SetDefault("max_orders_per_side", 30) // Many orders per side
	viper.SetDefault("replace_immediately", true)
	viper.SetDefault("position_timeout_minutes", 10) // Fast turnover
	viper.SetDefault("ticker_stream", "!ticker@arr")
	viper.SetDefault("symbol_refresh_interval_seconds", 30) // Fast refresh
	viper.SetDefault("grid_placement_cooldown_seconds", 1)  // 1s cooldown
	viper.SetDefault("rate_limit_cooldown_seconds", 3)      // Quick recovery
	viper.SetDefault("supported_quote_currencies", []string{"USD1"})
	viper.SetDefault("min_volume_24h", 500) // Lower threshold

	// Bot defaults
	viper.SetDefault("bot.dry_run", false)

	// Symbol defaults - optimized for volume
	viper.SetDefault("symbols.auto_discover", true)
	viper.SetDefault("symbols.quote_currency_mode", "USD1")
	viper.SetDefault("symbols.min_volume_24h", 500)  // Lower volume threshold
	viper.SetDefault("symbols.max_spread_pct", 15.0) // Allow higher spread
	viper.SetDefault("symbols.boosted_only", false)
	viper.SetDefault("symbols.max_symbols_per_quote", 20) // More symbols
	viper.SetDefault("symbols.spread_ranking", false)
	viper.SetDefault("symbols.volume_weighting", 0.2)    // Less weight on volume
	viper.SetDefault("symbols.min_liquidity_score", 0.0) // Allow all
	viper.SetDefault("symbols.optimal_spread_range", []float64{0.0, 2.0})
	viper.SetDefault("symbols.spread_volatility_threshold", 0.2)
	viper.SetDefault("symbols.exclude_high_fee_symbols", false)
	viper.SetDefault("symbols.quote_currencies", []string{"USD1"})
	viper.SetDefault("symbols.allow_mixed_quotes", true)

	// Exchange defaults (reuse from main config)
	viper.SetDefault("exchange.futures_rest_base", "https://fapi.asterdex.com")
	viper.SetDefault("exchange.futures_ws_base", "wss://fstream.asterdex.com")
	viper.SetDefault("exchange.recv_window", 5000)
	viper.SetDefault("exchange.requests_per_second", 10)

	// API defaults
	viper.SetDefault("api.host", "0.0.0.0")
	viper.SetDefault("api.port", 8081)
}

// validateVolumeFarmConfig validates volume farming configuration
func validateVolumeFarmConfig(cfg *VolumeFarmConfig) error {
	if cfg.OrderSizeUSDT <= 0 {
		return fmt.Errorf("order_size_usdt must be positive")
	}
	if cfg.GridSpreadPct <= 0 {
		return fmt.Errorf("grid_spread_pct must be positive")
	}
	if cfg.MaxOrdersPerSide <= 0 || cfg.MaxOrdersPerSide > 50 {
		return fmt.Errorf("max_orders_per_side must be between 1 and 50")
	}

	validQuoteModes := []string{"USDT", "USD1", "flexible", "all"}
	isValidMode := false
	for _, mode := range validQuoteModes {
		if cfg.Symbols.QuoteCurrencyMode == mode {
			isValidMode = true
			break
		}
	}
	if !isValidMode {
		return fmt.Errorf("quote_currency_mode must be one of: %v", validQuoteModes)
	}

	return nil
}

// OptimizationConfig holds all grid optimization configurations
type OptimizationConfig struct {
	DynamicGrid       *DynamicGridConfig           `yaml:"dynamic_grid"`
	InventorySkew     *InventorySkewConfig         `yaml:"inventory_skew"`
	ClusterStopLoss   *ClusterStopLossConfig       `yaml:"cluster_stop_loss"`
	TrendDetection    *TrendDetectionConfig        `yaml:"trend_detection"`
	Safeguards        *SafeguardsConfig            `yaml:"safeguards"`
	TimeFilter        *TradingHoursConfig          `yaml:"time_filter"`
	MicroGrid         *MicroGridConfig             `yaml:"micro_grid,omitempty"`              // NEW: Micro grid for high-frequency
	FastRange         *FastRangeConfig             `yaml:"fast_range,omitempty"`              // NEW: Fast range detection (10 periods)
	ADXFilter         *ADXFilterConfig             `yaml:"adx_filter,omitempty"`              // NEW: ADX-based sideways filter
	DynamicLeverage   *DynamicLeverageConfig       `yaml:"dynamic_leverage,omitempty"`        // NEW: Adaptive leverage by volatility
	MultiLayerLiq     *MultiLayerLiquidationConfig `yaml:"multi_layer_liquidation,omitempty"` // NEW: 4-tier liquidation protection
	MicroPartialClose *MicroPartialCloseConfig     `yaml:"micro_partial_close,omitempty"`     // NEW: Micro TP levels (8-40 bps)
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

// TrendDetectionYAML is a wrapper for trend_detection.yaml root key
type TrendDetectionYAML struct {
	TrendDetection TrendDetectionConfig `yaml:"trend_detection"`
}

// DynamicGridYAML is a wrapper for dynamic_grid.yaml root key
type DynamicGridYAML struct {
	DynamicGrid DynamicGridConfig `yaml:"dynamic_grid"`
}

// InventorySkewYAML is a wrapper for inventory_skew.yaml root key
type InventorySkewYAML struct {
	InventorySkew InventorySkewConfig `yaml:"inventory_skew"`
}

// ClusterStopLossYAML is a wrapper for cluster_stoploss.yaml root key
type ClusterStopLossYAML struct {
	ClusterStopLoss ClusterStopLossConfig `yaml:"cluster_stop_loss"`
}

// SafeguardsYAML is a wrapper for safeguards.yaml root key
type SafeguardsYAML struct {
	Safeguards SafeguardsConfig `yaml:"safeguards"`
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

// MicroGridConfig mirrors adaptive_grid.MicroGridConfig
type MicroGridConfig struct {
	Enabled          bool    `yaml:"enabled" mapstructure:"enabled"`
	SpreadPct        float64 `yaml:"spread_pct" mapstructure:"spread_pct"`
	OrdersPerSide    int     `yaml:"orders_per_side" mapstructure:"orders_per_side"`
	OrderSizeUSDT    float64 `yaml:"order_size_usdt" mapstructure:"order_size_usdt"`
	MinProfitPerFill float64 `yaml:"min_profit_per_fill" mapstructure:"min_profit_per_fill"`
}

// FastRangeConfig mirrors adaptive_grid.EnhancedRangeConfig for fast detection
type FastRangeConfig struct {
	Enabled          bool    `yaml:"enabled" mapstructure:"enabled"`
	BBPeriod         int     `yaml:"bb_period" mapstructure:"bb_period"`
	ADXPeriod        int     `yaml:"adx_period" mapstructure:"adx_period"`
	SidewaysADXMax   float64 `yaml:"sideways_adx_max" mapstructure:"sideways_adx_max"`
	StabilizationMin int     `yaml:"stabilization_min" mapstructure:"stabilization_min"`
	EnableADXFilter  bool    `yaml:"enable_adx_filter" mapstructure:"enable_adx_filter"`
}

// ADXFilterConfig for ADX-based sideways confirmation
type ADXFilterConfig struct {
	Enabled        bool    `yaml:"enabled" mapstructure:"enabled"`
	ADXPeriod      int     `yaml:"adx_period" mapstructure:"adx_period"`
	SidewaysADXMax float64 `yaml:"sideways_adx_max" mapstructure:"sideways_adx_max"`
}

// DynamicLeverageConfig mirrors adaptive_grid.DynamicLeverageConfig
type DynamicLeverageConfig struct {
	Enabled               bool    `yaml:"enabled" mapstructure:"enabled"`
	BaseLeverage          float64 `yaml:"base_leverage" mapstructure:"base_leverage"`
	MinLeverage           float64 `yaml:"min_leverage" mapstructure:"min_leverage"`
	MaxLeverage           float64 `yaml:"max_leverage" mapstructure:"max_leverage"`
	ATRThresholdHigh      float64 `yaml:"atr_threshold_high" mapstructure:"atr_threshold_high"`
	ATRThresholdLow       float64 `yaml:"atr_threshold_low" mapstructure:"atr_threshold_low"`
	ADXThresholdTrending  float64 `yaml:"adx_threshold_trending" mapstructure:"adx_threshold_trending"`
	BBWidthThresholdTight float64 `yaml:"bb_width_threshold_tight" mapstructure:"bb_width_threshold_tight"`
}

// MultiLayerLiquidationConfig mirrors adaptive_grid.MultiLayerLiquidationConfig
type MultiLayerLiquidationConfig struct {
	Enabled          bool    `yaml:"enabled" mapstructure:"enabled"`
	Layer1WarnPct    float64 `yaml:"layer1_warn_pct" mapstructure:"layer1_warn_pct"`
	Layer2ReducePct  float64 `yaml:"layer2_reduce_pct" mapstructure:"layer2_reduce_pct"`
	Layer3ClosePct   float64 `yaml:"layer3_close_pct" mapstructure:"layer3_close_pct"`
	Layer4HedgePct   float64 `yaml:"layer4_hedge_pct" mapstructure:"layer4_hedge_pct"`
	ReducePositionBy float64 `yaml:"reduce_position_by" mapstructure:"reduce_position_by"`
}

// TPLevelConfig represents a single take-profit level
type TPLevelConfig struct {
	TargetPct float64 `yaml:"target_pct" mapstructure:"target_pct"`
	ClosePct  float64 `yaml:"close_pct" mapstructure:"close_pct"`
	ProfitPct float64 `yaml:"profit_pct" mapstructure:"profit_pct"`
}

// MicroPartialCloseConfig mirrors adaptive_grid partial close with micro TP levels
type MicroPartialCloseConfig struct {
	Enabled          bool            `yaml:"enabled" mapstructure:"enabled"`
	TPLevels         []TPLevelConfig `yaml:"tp_levels" mapstructure:"tp_levels"`
	TrailingAfterTP3 bool            `yaml:"trailing_after_tp3" mapstructure:"trailing_after_tp3"`
	TrailingDistance float64         `yaml:"trailing_distance" mapstructure:"trailing_distance"`
}

// LoadOptimizationConfig loads all optimization configs from directory
func LoadOptimizationConfig(configPath string) (*OptimizationConfig, error) {
	config := &OptimizationConfig{}

	// Load dynamic grid config with wrapper for root key
	var gridWrapper DynamicGridYAML
	if err := loadYAML(configPath+"/dynamic_grid.yaml", &gridWrapper); err != nil {
		return nil, fmt.Errorf("failed to load dynamic_grid config: %w", err)
	}
	config.DynamicGrid = &gridWrapper.DynamicGrid

	// Load inventory skew config with wrapper for root key
	var invWrapper InventorySkewYAML
	if err := loadYAML(configPath+"/inventory_skew.yaml", &invWrapper); err != nil {
		return nil, fmt.Errorf("failed to load inventory_skew config: %w", err)
	}
	config.InventorySkew = &invWrapper.InventorySkew

	// Load cluster stop-loss config with wrapper for root key
	var clusterWrapper ClusterStopLossYAML
	if err := loadYAML(configPath+"/cluster_stoploss.yaml", &clusterWrapper); err != nil {
		return nil, fmt.Errorf("failed to load cluster_stoploss config: %w", err)
	}
	config.ClusterStopLoss = &clusterWrapper.ClusterStopLoss

	// Load trend detection config with wrapper for root key
	var trendWrapper TrendDetectionYAML
	if err := loadYAML(configPath+"/trend_detection.yaml", &trendWrapper); err != nil {
		return nil, fmt.Errorf("failed to load trend_detection config: %w", err)
	}
	config.TrendDetection = &trendWrapper.TrendDetection

	// Load safeguards config with wrapper for root key
	var safeWrapper SafeguardsYAML
	if err := loadYAML(configPath+"/safeguards.yaml", &safeWrapper); err != nil {
		return nil, fmt.Errorf("failed to load safeguards config: %w", err)
	}
	config.Safeguards = &safeWrapper.Safeguards

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

// AgenticConfig holds the agentic decision layer configuration
type AgenticConfig struct {
	Enabled             bool                        `mapstructure:"enabled" yaml:"enabled"`
	Universe            UniverseConfig              `mapstructure:"universe" yaml:"universe"`
	RegimeDetection     RegimeDetectionConfig       `mapstructure:"regime_detection" yaml:"regime_detection"`
	Scoring             ScoringConfig               `mapstructure:"scoring" yaml:"scoring"`
	PositionSizing      PositionSizingConfig        `mapstructure:"position_sizing" yaml:"position_sizing"`
	WhitelistManagement WhitelistConfig             `mapstructure:"whitelist_management" yaml:"whitelist_management"`
	CircuitBreakers     AgenticCircuitBreakerConfig `mapstructure:"circuit_breakers" yaml:"circuit_breakers"`
}

// UniverseConfig defines the symbol universe for monitoring
type UniverseConfig struct {
	Symbols         []string `mapstructure:"symbols" yaml:"symbols"`
	AutoDiscover    bool     `mapstructure:"auto_discover" yaml:"auto_discover"`
	TopVolumeCount  int      `mapstructure:"top_volume_count" yaml:"top_volume_count"`
	Min24hVolumeUSD float64  `mapstructure:"min_24h_volume_usd" yaml:"min_24h_volume_usd"`
}

// RegimeDetectionConfig configures regime detection parameters
type RegimeDetectionConfig struct {
	UpdateInterval string                 `mapstructure:"update_interval" yaml:"update_interval"`
	ADXPeriod      int                    `mapstructure:"adx_period" yaml:"adx_period"`
	BBPeriod       int                    `mapstructure:"bb_period" yaml:"bb_period"`
	ATRPeriod      int                    `mapstructure:"atr_period" yaml:"atr_period"`
	CandleInterval string                 `mapstructure:"candle_interval" yaml:"candle_interval"`
	Thresholds     RegimeThresholdsConfig `mapstructure:"thresholds" yaml:"thresholds"`
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
	Thresholds ScoringThresholdsConfig `mapstructure:"thresholds" yaml:"thresholds"`
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
	ScoreMultipliers  map[string]float64 `mapstructure:"score_multipliers" yaml:"score_multipliers"`
	RegimeMultipliers map[string]float64 `mapstructure:"regime_multipliers" yaml:"regime_multipliers"`
}

// WhitelistConfig configures whitelist management
type WhitelistConfig struct {
	Enabled            bool    `mapstructure:"enabled" yaml:"enabled"`
	MaxSymbols         int     `mapstructure:"max_symbols" yaml:"max_symbols"`
	MinScoreToAdd      float64 `mapstructure:"min_score_to_add" yaml:"min_score_to_add"`
	ScoreToRemove      float64 `mapstructure:"score_to_remove" yaml:"score_to_remove"`
	KeepIfPositionOpen bool    `mapstructure:"keep_if_position_open" yaml:"keep_if_position_open"`
	ReplaceImmediately bool    `mapstructure:"replace_immediately" yaml:"replace_immediately"`
}

// AgenticCircuitBreakerConfig configures circuit breaker rules for agentic
type AgenticCircuitBreakerConfig struct {
	VolatilitySpike   VolatilityBreakerConfig      `mapstructure:"volatility_spike" yaml:"volatility_spike"`
	ConsecutiveLosses ConsecutiveLossBreakerConfig `mapstructure:"consecutive_losses" yaml:"consecutive_losses"`
}

// VolatilityBreakerConfig configures volatility-based circuit breaker
type VolatilityBreakerConfig struct {
	Enabled       bool    `mapstructure:"enabled" yaml:"enabled"`
	ATRMultiplier float64 `mapstructure:"atr_multiplier" yaml:"atr_multiplier"`
}

// ConsecutiveLossBreakerConfig configures loss-based circuit breaker
type ConsecutiveLossBreakerConfig struct {
	Enabled       bool    `mapstructure:"enabled" yaml:"enabled"`
	Threshold     int     `mapstructure:"threshold" yaml:"threshold"`
	SizeReduction float64 `mapstructure:"size_reduction" yaml:"size_reduction"`
}
