package tradingmode

import (
	"fmt"
	"time"
)

// TradingMode represents the current trading mode
type TradingMode int

const (
	TradingModeUnknown TradingMode = iota
	TradingModeMicro        // Entry mode: ATR bands, reduced size
	TradingModeStandard     // Normal mode: BB bands, full size
	TradingModeTrendAdapted // Trend mode: BB bands, reduced size, trend bias
	TradingModeCooldown     // Pause mode: no orders, wait period
)

// String returns the string representation of the mode
func (m TradingMode) String() string {
	switch m {
	case TradingModeMicro:
		return "MICRO"
	case TradingModeStandard:
		return "STANDARD"
	case TradingModeTrendAdapted:
		return "TREND_ADAPTED"
	case TradingModeCooldown:
		return "COOLDOWN"
	default:
		return "UNKNOWN"
	}
}

// IsTrading returns true if mode allows trading
func (m TradingMode) IsTrading() bool {
	return m == TradingModeMicro || m == TradingModeStandard || m == TradingModeTrendAdapted
}

// DynamicParameters holds mode-specific settings
type DynamicParameters struct {
	// Sizing
	SizeMultiplier   float64
	MinOrderSizeUSDT float64
	MaxOrderSizeUSDT float64

	// Grid structure
	SpreadMultiplier float64
	LevelCount       int

	// Bands
	UseBBBands    bool
	ATRMultiplier float64

	// Trend
	TrendBiasEnabled bool
	TrendBiasRatio   float64

	// Timing
	CooldownDuration time.Duration
	MinModeDuration  time.Duration
}

// GetDefaultParameters returns default parameters for a mode
func GetDefaultParameters(mode TradingMode) DynamicParameters {
	switch mode {
	case TradingModeMicro:
		return DynamicParameters{
			SizeMultiplier:   0.4,
			SpreadMultiplier: 2.0,
			LevelCount:       3,
			UseBBBands:       false,
			ATRMultiplier:    1.5,
			TrendBiasEnabled: false,
			MinModeDuration:  30 * time.Second,
		}
	case TradingModeStandard:
		return DynamicParameters{
			SizeMultiplier:   1.0,
			SpreadMultiplier: 1.0,
			LevelCount:       5,
			UseBBBands:       true,
			ATRMultiplier:    0,
			TrendBiasEnabled: false,
			MinModeDuration:  60 * time.Second,
		}
	case TradingModeTrendAdapted:
		return DynamicParameters{
			SizeMultiplier:   0.3,
			SpreadMultiplier: 2.0,
			LevelCount:       2,
			UseBBBands:       true,
			ATRMultiplier:    0,
			TrendBiasEnabled: true,
			TrendBiasRatio:   0.6,
			MinModeDuration:  30 * time.Second,
		}
	case TradingModeCooldown:
		return DynamicParameters{
			SizeMultiplier:   0,
			SpreadMultiplier: 0,
			LevelCount:       0,
			UseBBBands:       false,
			CooldownDuration: 60 * time.Second,
		}
	default:
		return DynamicParameters{}
	}
}

// ModeTransition represents a mode change
type ModeTransition struct {
	From      TradingMode
	To        TradingMode
	Timestamp time.Time
	Reason    string
}

// String returns a human-readable transition description
func (t ModeTransition) String() string {
	return fmt.Sprintf("%s → %s at %s (reason: %s)",
		t.From.String(), t.To.String(),
		t.Timestamp.Format("15:04:05"), t.Reason)
}
