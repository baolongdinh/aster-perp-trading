package agentic

import (
	"time"
)

// RegimeType represents the market regime classification
type RegimeType string

const (
	RegimeSideways RegimeType = "SIDEWAYS"
	RegimeTrending RegimeType = "TRENDING"
	RegimeVolatile RegimeType = "VOLATILE"
	RegimeRecovery RegimeType = "RECOVERY"
)

// Recommendation represents the trading recommendation based on score
type Recommendation string

const (
	RecHigh   Recommendation = "HIGH"   // Score >= 75 - Deploy full
	RecMedium Recommendation = "MEDIUM" // Score 60-75 - Deploy reduced
	RecLow    Recommendation = "LOW"    // Score 40-60 - Monitor only
	RecSkip   Recommendation = "SKIP"   // Score < 40 - Skip
)

// SymbolScore contains the evaluation result for a symbol
type SymbolScore struct {
	Symbol         string
	Score          float64            // 0-100 overall score
	Regime         RegimeType         // Current regime
	Confidence     float64            // Regime detection confidence (0-1)
	Factors        map[string]float64 // Individual factor scores
	LastUpdated    time.Time
	Recommendation Recommendation
	RawADX         float64
	RawATR14       float64
	RawBBWidth     float64
}

// RegimeSnapshot captures the current regime state
type RegimeSnapshot struct {
	Regime     RegimeType
	ADX        float64
	ATR14      float64
	BBWidth    float64
	Volume24h  float64
	Timestamp  time.Time
	Confidence float64
}

// IndicatorValues holds calculated indicator values
type IndicatorValues struct {
	ADX         float64
	ATR14       float64
	BBUpper     float64
	BBLower     float64
	BBMiddle    float64
	BBWidth     float64
	Volume24h   float64
	PriceChange float64
}

// Candle represents a price candle for technical analysis
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp time.Time
}

// WhitelistDecision represents the whitelist management decision
type WhitelistDecision struct {
	AddSymbols    []string
	RemoveSymbols []string
	KeepSymbols   []string          // Symbols to keep despite low scores (due to open positions)
	Reasoning     map[string]string // Why each symbol was added/removed/kept
}

// DetectionResult holds the complete detection output for all symbols
type DetectionResult struct {
	Scores      map[string]SymbolScore
	UpdatedAt   time.Time
	BestSymbols []string // Sorted by score descending
}

// PositionStatus represents the current position state for a symbol
type PositionStatus struct {
	Symbol        string
	HasPosition   bool
	Size          float64
	UnrealizedPnL float64
	Side          string // LONG or SHORT
}

// ============================================================================
// ADAPTIVE STATE MANAGEMENT - Phase 2 Implementation
// ============================================================================

// TradingMode represents the trading mode the bot operates in
type TradingMode string

const (
	TradingModeIdle         TradingMode = "IDLE"           // Waiting for opportunity
	TradingModeWaitNewRange TradingMode = "WAIT_NEW_RANGE" // Detecting range
	TradingModeEnterGrid    TradingMode = "ENTER_GRID"     // Preparing grid placement
	TradingModeGrid         TradingMode = "GRID_TRADING"   // Active volume farming / grid trading
	TradingModeTrending     TradingMode = "TRENDING"       // Trend following
	TradingModeAccumulation TradingMode = "ACCUMULATION"   // Pre-breakout accumulation
	TradingModeDefensive    TradingMode = "DEFENSIVE"      // Risk protection mode
	TradingModeRecovery     TradingMode = "RECOVERY"       // Post-loss recovery
	TradingModeOverSize     TradingMode = "OVER_SIZE"      // Position size too large
)

// GridState represents the state machine state (alias for backward compatibility)
type GridState = TradingMode

const (
	GridStateIdle         = TradingModeIdle
	GridStateEnterGrid    = TradingModeEnterGrid
	GridStateTrading      = TradingModeGrid
	GridStateTrending     = TradingModeTrending
	GridStateExitHalf     = TradingModeDefensive
	GridStateExitAll      = TradingModeDefensive
	GridStateWaitNewRange = TradingModeWaitNewRange
	GridStateOverSize     = TradingModeOverSize
	GridStateDefensive    = TradingModeDefensive
	GridStateRecovery     = TradingModeRecovery
)

// TradingModeScore contains score calculation for a specific trading mode
type TradingModeScore struct {
	Mode       TradingMode        // Which mode
	Score      float64            // 0-1 confidence score
	Threshold  float64            // Min score to activate
	Components map[string]float64 // Score breakdown
	Timestamp  time.Time
	IsActive   bool // Currently active
}

// StateTransition represents a transition request between states
type StateTransition struct {
	FromState         TradingMode
	ToState           TradingMode
	Trigger           string        // What triggered transition
	Score             float64       // Decision confidence
	SmoothingDuration time.Duration // Time to complete transition
	Timestamp         time.Time
}

// TransitionIntent is the handler-produced transition request before the
// decision engine validates and commits it.
type TransitionIntent struct {
	Symbol           string
	FromState        TradingMode
	ToState          TradingMode
	Trigger          string
	Score            float64
	MarketState      MarketStateVector
	ExecutionContext ExecutionContext
	LifecyclePolicy  TradeLifecyclePolicy
	Timestamp        time.Time
}

// SymbolTradingState tracks trading state for a specific symbol
type SymbolTradingState struct {
	Symbol               string
	CurrentMode          TradingMode
	PreviousMode         TradingMode
	ModeScores           map[TradingMode]*TradingModeScore
	TransitionConfidence float64
	LastTransition       time.Time
	StateEnteredAt       time.Time // When current state was entered (for watchdog)
	TransitionHistory    []StateTransition
	IsTransitioning      bool
	TargetMode           TradingMode // During transition
	BlendWeight          float64     // 0-1 for smooth transition

	// Recovery & loss tracking
	ConsecutiveLosses  int
	LastExitPnL        float64
	LastExitReason     string
	LastExecutionAckAt time.Time
	LastIntentAt       time.Time
	LastIntentID       string
	PendingExecution   bool

	// Watchdog metrics
	StateStuckCount int
}

// ModeObjective tracks the relative priorities of the execution engine.
type ModeObjective struct {
	VolumeWeight float64
	ProfitWeight float64
	RiskWeight   float64
}

// MarketStateVector represents the multidimensional state of the market
type MarketStateVector struct {
	Regime              RegimeType
	TrendStrength       float64
	RangeQuality        float64
	VolatilityState     float64
	LiquidityQuality    float64
	BreakoutPersistence float64
	TrendPersistence    float64
	OrderflowImbalance  float64
	SpreadStress        float64
}

func (v MarketStateVector) toRegimeSnapshot(price float64) RegimeSnapshot {
	return RegimeSnapshot{
		Regime:     v.Regime,
		ADX:        v.TrendStrength * 50,
		ATR14:      v.VolatilityState / 100,
		BBWidth:    max(0.001, v.RangeQuality*0.05),
		Volume24h:  max(price, 0),
		Timestamp:  time.Now(),
		Confidence: max(v.RangeQuality, v.TrendStrength),
	}
}

// ExecutionContext contains live execution-sensitive inputs from the runtime
// layer that handlers and the VF bridge use to make cost-aware decisions.
type ExecutionContext struct {
	CurrentPrice      float64
	BestBid           float64
	BestAsk           float64
	SpreadBps         float64
	SlippageEstBps    float64
	FundingImpactBps  float64
	PositionSize      float64
	InventoryNotional float64
	PositionAgeSec    float64
	PendingOrders     int
	RealizedPnL       float64
	UnrealizedPnL     float64
	MakerFillRatio    float64
	Regime            RegimeType // Added for Phase 5 audit
}

// TPBand represents a maker-first take-profit rung.
type TPBand struct {
	TargetBps  float64 `json:"target_bps,omitempty"`
	CloseRatio float64 `json:"close_ratio,omitempty"`
	MakerOnly  bool    `json:"maker_only,omitempty"`
}

// SLPolicy defines soft/hard stop behavior.
type SLPolicy struct {
	SoftATRMultiplier float64 `json:"soft_atr_multiplier,omitempty"`
	HardLossBps       float64 `json:"hard_loss_bps,omitempty"`
	TimeStopSec       int64   `json:"time_stop_sec,omitempty"`
}

// RegridPolicy defines when the bot is allowed to place a fresh grid.
type RegridPolicy struct {
	AllowImmediate  bool    `json:"allow_immediate,omitempty"`
	MinRangeQuality float64 `json:"min_range_quality,omitempty"`
	FlattenFirst    bool    `json:"flatten_first,omitempty"`
}

// TradeLifecyclePolicy captures execution rules shared between Agentic and VF.
type TradeLifecyclePolicy struct {
	TPBands           []TPBand
	SLPolicy          SLPolicy
	RegridPolicy      RegridPolicy
	MakerOnly         bool
	MaxPositionAgeSec int64
	FeeBudgetBps      float64
	InventorySkew     float64
	Objective         ModeObjective
}

// ScoreComponents breaks down how scores are calculated
type ScoreComponents struct {
	RegimeComponent  float64 // Market regime contribution
	SignalComponent  float64 // Strategy signals contribution
	VolumeComponent  float64 // Volume confirmation
	HistoricalWeight float64 // Past performance weight
	RiskAdjustment   float64 // Risk-based adjustment
}

// HybridTrendSignals contains breakout and momentum signals
type HybridTrendSignals struct {
	BreakoutStrength float64 // Range breakout signal
	MomentumStrength float64 // ROC + velocity signal
	VolumeConfirm    float64 // Volume spike confirmation
	AgreementBonus   float64 // Bonus when signals align
	HybridScore      float64 // Combined score
}

// AdaptiveThresholds contains dynamic thresholds
type AdaptiveThresholds struct {
	GridThreshold    float64 // Min score for GRID mode
	TrendThreshold   float64 // Min score for TREND mode
	HysteresisBuffer float64 // Buffer to prevent flip-flop
	VolatilityFactor float64 // Adjust thresholds by volatility
}

// StateManagerMetrics tracks state manager performance
type StateManagerMetrics struct {
	TotalTransitions  int
	SuccessfulModes   map[TradingMode]int
	FailedTransitions int
	AvgTransitionTime time.Duration
	FlipFlopCount     int
	LastFlipFlop      time.Time
}
