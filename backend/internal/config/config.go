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
	Bot        BotConfig        `mapstructure:"bot"`
	Exchange   ExchangeConfig   `mapstructure:"exchange"`
	Risk       RiskConfig       `mapstructure:"risk"`
	API        APIConfig        `mapstructure:"api"`
	Log        LogConfig        `mapstructure:"log"`
	Strategies []StrategyConfig `mapstructure:"strategies"`
}

type BotConfig struct {
	DryRun bool `mapstructure:"dry_run"`
}

type ExchangeConfig struct {
	APIKey          string `mapstructure:"api_key"`
	APISecret       string `mapstructure:"-"` // loaded from env only (ASTER_API_SECRET)
	FuturesRESTBase string `mapstructure:"futures_rest_base"`
	FuturesWSBase   string `mapstructure:"futures_ws_base"`
	RecvWindow      int    `mapstructure:"recv_window"` // ms, default 5000
}

type RiskConfig struct {
	MaxPositionUSDT     float64 `mapstructure:"max_position_usdt"`
	MaxOpenPositions    int     `mapstructure:"max_open_positions"`
	MaxTradesPerSymbol  int     `mapstructure:"max_trades_per_symbol"`
	DailyLossLimitUSDT  float64 `mapstructure:"daily_loss_limit_usdt"`
	DailyDrawdownPct    float64 `mapstructure:"daily_drawdown_pct"`
	PerTradeStopLossPct float64 `mapstructure:"per_trade_stop_loss_pct"`
	RiskPerTradeUSDT    float64 `mapstructure:"risk_per_trade_usdt"`
	ATRMultiplier       float64 `mapstructure:"atr_multiplier"`
	PositionMode        string  `mapstructure:"position_mode"` // one_way | hedge
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
	v.SetDefault("api.host", "0.0.0.0")
	v.SetDefault("api.port", 8080)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.file", "logs/bot.log")

	v.SetDefault("risk.position_mode", "one_way")

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

	// API Secret: env only
	cfg.Exchange.APISecret = os.Getenv("ASTER_API_SECRET")

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config: validation: %w", err)
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	if cfg.Exchange.APIKey == "" {
		return fmt.Errorf("exchange.api_key or ASTER_EXCHANGE_API_KEY is required")
	}
	if cfg.Exchange.APISecret == "" {
		return fmt.Errorf("ASTER_API_SECRET env var is required")
	}
	if cfg.Risk.MaxPositionUSDT <= 0 {
		cfg.Risk.MaxPositionUSDT = 500
	}
	if cfg.Risk.MaxOpenPositions <= 0 {
		cfg.Risk.MaxOpenPositions = 3
	}
	if cfg.Risk.MaxTradesPerSymbol <= 0 {
		cfg.Risk.MaxTradesPerSymbol = 1
	}

	if cfg.Risk.DailyLossLimitUSDT <= 0 {
		cfg.Risk.DailyLossLimitUSDT = 100
	}
	if cfg.Risk.DailyDrawdownPct <= 0 {
		cfg.Risk.DailyDrawdownPct = 5.0 // 5% max daily drawdown
	}
	if cfg.Risk.RiskPerTradeUSDT <= 0 {
		cfg.Risk.RiskPerTradeUSDT = 10.0 // Risk $10 per signal by default
	}
	if cfg.Risk.ATRMultiplier <= 0 {
		cfg.Risk.ATRMultiplier = 2.0 // Stop loss = 2 * ATR
	}
	_ = time.Now() // suppress import
	return nil
}

