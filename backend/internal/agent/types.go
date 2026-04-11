package agent

import (
	"time"

	"github.com/google/uuid"
)

// RegimeType represents the current market regime
type RegimeType string

const (
	RegimeSideways RegimeType = "SIDEWAYS"
	RegimeTrending RegimeType = "TRENDING"
	RegimeVolatile RegimeType = "VOLATILE"
	RegimeRecovery RegimeType = "RECOVERY"
	RegimeUnknown  RegimeType = "UNKNOWN"
)

// DecisionType represents the action to take
type DecisionType string

const (
	DecisionDeploy DecisionType = "DEPLOY"
	DecisionPause  DecisionType = "PAUSE"
	DecisionAdjust DecisionType = "ADJUST"
	DecisionClose  DecisionType = "CLOSE"
	DecisionHold   DecisionType = "HOLD"
)

// FactorType represents the type of decision factor
type FactorType string

const (
	FactorTrend      FactorType = "TREND"
	FactorVolatility FactorType = "VOLATILITY"
	FactorVolume     FactorType = "VOLUME"
	FactorStructure  FactorType = "STRUCTURE"
	FactorTime       FactorType = "TIME"
)

// BreakerType represents the type of circuit breaker
type BreakerType string

const (
	BreakerVolatility BreakerType = "VOLATILITY"
	BreakerLiquidity  BreakerType = "LIQUIDITY"
	BreakerLosses     BreakerType = "LOSSES"
	BreakerDrawdown   BreakerType = "DRAWDOWN"
	BreakerConnection BreakerType = "CONNECTION"
)

// RegimeSnapshot captures the market regime at a point in time
type RegimeSnapshot struct {
	ID         uuid.UUID         `json:"id"`
	Timestamp  time.Time         `json:"timestamp"`
	Regime     RegimeType        `json:"regime"`
	Confidence float64           `json:"confidence"` // 0-100
	Indicators IndicatorSnapshot `json:"indicators"`
	DetectedAt time.Time         `json:"detected_at"`
}

// IndicatorSnapshot holds all technical indicator values
type IndicatorSnapshot struct {
	ADX           float64 `json:"adx"`
	BBWidth       float64 `json:"bb_width"`
	ATR14         float64 `json:"atr_14"`
	VolumeMA20    float64 `json:"volume_ma20"`
	CurrentVolume float64 `json:"current_volume"`
	EMA9          float64 `json:"ema_9"`
	EMA21         float64 `json:"ema_21"`
	EMA50         float64 `json:"ema_50"`
	EMA200        float64 `json:"ema_200"`
}

// DecisionFactor represents a single factor in the decision score
type DecisionFactor struct {
	ID              uuid.UUID  `json:"id"`
	Name            FactorType `json:"name"`
	CurrentValue    float64    `json:"current_value"`
	NormalizedScore float64    `json:"normalized_score"` // 0-100
	Weight          float64    `json:"weight"`
	Contribution    float64    `json:"contribution"` // value * weight
	CalculatedAt    time.Time  `json:"calculated_at"`
}

// GridParams holds grid trading parameters
type GridParams struct {
	GridSpacing      float64 `json:"grid_spacing"`
	PositionSize     float64 `json:"position_size"`
	StopLossDistance float64 `json:"stop_loss_distance"` // ATR multiplier
}

// PatternMatch represents a matching historical pattern
type PatternMatch struct {
	PatternID       string  `json:"pattern_id"`
	SimilarityScore float64 `json:"similarity_score"` // 0-1
	HistoricalPnL   float64 `json:"historical_pnl"`
	Weight          float64 `json:"weight"` // decay weight
}

// TradingDecision represents a complete trading decision
type TradingDecision struct {
	ID             uuid.UUID        `json:"id"`
	Timestamp      time.Time        `json:"timestamp"`
	DecisionType   DecisionType     `json:"decision_type"`
	RegimeSnapshot RegimeSnapshot   `json:"regime_snapshot"`
	FinalScore     float64          `json:"final_score"` // 0-100
	Factors        []DecisionFactor `json:"factors"`

	// Position Sizing
	BaseSize        float64 `json:"base_size"`
	ScoreMultiplier float64 `json:"score_multiplier"` // 1.0, 0.5, 0.0
	VolMultiplier   float64 `json:"vol_multiplier"`   // 1.0, 0.5, 0.0
	FinalSize       float64 `json:"final_size"`       // base * score * vol

	// Grid Parameters
	GridSpacing      float64 `json:"grid_spacing"`
	StopLossDistance float64 `json:"stop_loss_distance"`

	// Pattern Matching
	PatternMatches []PatternMatch `json:"pattern_matches,omitempty"`
	PatternImpact  float64        `json:"pattern_impact,omitempty"` // ±5 max

	Rationale string `json:"rationale"`
	Executed  bool   `json:"executed"`
}

// TradeOutcome captures the result of a trade for pattern learning
type TradeOutcome struct {
	PnL         float64       `json:"pnl"`
	Duration    time.Duration `json:"duration"`
	MaxDrawdown float64       `json:"max_drawdown"`
	CompletedAt time.Time     `json:"completed_at"`
}

// CircuitBreakerEvent records when a circuit breaker was triggered
type CircuitBreakerEvent struct {
	ID               uuid.UUID   `json:"id"`
	BreakerType      BreakerType `json:"breaker_type"`
	TriggerValue     float64     `json:"trigger_value"`
	Threshold        float64     `json:"threshold"`
	TriggeredAt      time.Time   `json:"triggered_at"`
	ActionTaken      string      `json:"action_taken"`
	PositionsClosed  int         `json:"positions_closed"`
	OperatorNotified bool        `json:"operator_notified"`
}

// HistoricalPattern stores a pattern for learning
type HistoricalPattern struct {
	ID              string     `json:"id"`
	CreatedAt       time.Time  `json:"created_at"`
	ContextVector   []float64  `json:"context_vector"` // [trend_norm, vol_norm, volume_norm, structure_norm]
	Regime          RegimeType `json:"regime"`
	GridParams      GridParams `json:"grid_params"`
	OutcomePnL      float64    `json:"outcome_pnl"`
	OutcomeDuration int        `json:"outcome_duration_minutes"`
	MaxDrawdown     float64    `json:"max_drawdown"`
}
