package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config is the root config for the bot.
type Config struct {
	Bot           BotConfig         `mapstructure:"bot"`
	Exchange      ExchangeConfig    `mapstructure:"exchange"`
	Risk          RiskConfig        `mapstructure:"risk"`
	API           APIConfig         `mapstructure:"api"`
	Log           LogConfig         `mapstructure:"log"`
	Strategies    []StrategyConfig  `mapstructure:"strategies"`
	VolumeFarming *VolumeFarmConfig `mapstructure:"volume_farming,omitempty"`
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
	MaxPositionUSDTPerSymbol    float64 `mapstructure:"max_position_usdt_per_symbol"`
	MaxTotalPositionsUSDT       float64 `mapstructure:"max_total_positions_usdt"`
	FeeLossThresholdPct         float64 `mapstructure:"fee_loss_threshold_pct"`
	PositionTimeoutMinutes      int     `mapstructure:"position_timeout_minutes"`
	MaxPositionUSDT             float64 `mapstructure:"max_position_usdt"`
	MaxOpenPositions            int     `mapstructure:"max_open_positions"`
	MaxTradesPerSymbol          int     `mapstructure:"max_trades_per_symbol"`
	MaxGlobalPendingLimitOrders int     `mapstructure:"max_global_pending_limit_orders"`
	MaxPendingPerSide           int     `mapstructure:"max_pending_per_side"`
	DailyLossLimitUSDT          float64 `mapstructure:"daily_loss_limit_usdt"`
	DailyDrawdownPct            float64 `mapstructure:"daily_drawdown_pct"`
	PerTradeStopLossPct         float64 `mapstructure:"per_trade_stop_loss_pct"`
	PerTradeTakeProfitPct       float64 `mapstructure:"per_trade_take_profit_pct"`
	RiskPerTradeUSDT            float64 `mapstructure:"risk_per_trade_usdt"`
	ATRMultiplier               float64 `mapstructure:"atr_multiplier"`
	PositionMode                string  `mapstructure:"position_mode"` // one_way | hedge
	CorrelationThreshold        float64 `mapstructure:"correlation_threshold"`
	MakerPriority               bool    `mapstructure:"maker_priority"`
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
	Enabled                  bool           `mapstructure:"enabled"`
	MaxDailyLossUSDT         float64        `mapstructure:"max_daily_loss_usdt"`
	MaxTotalDrawdownPct      float64        `mapstructure:"max_total_drawdown_pct"`
	OrderSizeUSDT            float64        `mapstructure:"order_size_usdt"`
	GridSpreadPct            float64        `mapstructure:"grid_spread_pct"`
	MaxOrdersPerSide         int            `mapstructure:"max_orders_per_side"`
	ReplaceImmediately       bool           `mapstructure:"replace_immediately"`
	PositionTimeoutMinutes   int            `mapstructure:"position_timeout_minutes"`
	TickerStream             string         `mapstructure:"ticker_stream"`
	SymbolRefreshIntervalSec int            `mapstructure:"symbol_refresh_interval_seconds"`
	GridPlacementCooldownSec int            `mapstructure:"grid_placement_cooldown_seconds"`
	RateLimitCooldownSec     int            `mapstructure:"rate_limit_cooldown_seconds"`
	RateLimiterCapacity      int            `mapstructure:"rate_limiter_capacity"`
	RateLimiterRefillRate    float64        `mapstructure:"rate_limiter_refill_rate"`
	SupportedQuoteCurrencies []string       `mapstructure:"supported_quote_currencies"`
	MinVolume24h             float64        `mapstructure:"min_volume_24h"`
	Bot                      BotConfig      `mapstructure:"bot"`
	Symbols                  SymbolsConfig  `mapstructure:"symbols"`
	Exchange                 ExchangeConfig `mapstructure:"exchange"`
	Risk                     RiskConfig     `mapstructure:"risk"`
	API                      APIConfig      `mapstructure:"api"`
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
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal volume farming config: %w", err)
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
	viper.SetDefault("bot.dry_run", true)

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
