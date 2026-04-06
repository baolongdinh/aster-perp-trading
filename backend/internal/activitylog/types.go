// Package activitylog provides structured activity logging for bot operations.
package activitylog

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// ============================================================================
// Severity Levels
// ============================================================================

// Severity represents the importance/urgency of an event.
type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarn     Severity = "WARN"
	SeverityError    Severity = "ERROR"
	SeverityCritical Severity = "CRITICAL"
)

// ============================================================================
// Event Types
// ============================================================================

// EventType represents the category and specific type of an activity event.
type EventType string

// Order lifecycle events.
const (
	EventOrderPlaced          EventType = "ORDER_PLACED"
	EventOrderFilled          EventType = "ORDER_FILLED"
	EventOrderPartiallyFilled EventType = "ORDER_PARTIALLY_FILLED"
	EventOrderCancelled       EventType = "ORDER_CANCELLED"
	EventOrderFailed          EventType = "ORDER_FAILED"
	EventOrderExpired         EventType = "ORDER_EXPIRED"
)

// Position lifecycle events.
const (
	EventPositionOpened    EventType = "POSITION_OPENED"
	EventPositionClosed    EventType = "POSITION_CLOSED"
	EventPositionUpdated   EventType = "POSITION_UPDATED"
	EventPositionLiquidated EventType = "POSITION_LIQUIDATED"
)

// Strategy lifecycle events.
const (
	EventStrategyStarted EventType = "STRATEGY_STARTED"
	EventStrategyStopped EventType = "STRATEGY_STOPPED"
	EventStrategyPaused  EventType = "STRATEGY_PAUSED"
	EventStrategyResumed EventType = "STRATEGY_RESUMED"
	EventStrategyError   EventType = "STRATEGY_ERROR"
	EventGridCreated     EventType = "GRID_CREATED"
	EventGridAdjusted    EventType = "GRID_ADJUSTED"
	EventGridClosed      EventType = "GRID_CLOSED"
)

// Risk management events.
const (
	EventRiskLimitHit          EventType = "RISK_LIMIT_HIT"
	EventStoplossTriggered     EventType = "STOPLOSS_TRIGGERED"
	EventTakeprofitTriggered   EventType = "TAKEPROFIT_TRIGGERED"
	EventDrawdownAlert         EventType = "DRAWDOWN_ALERT"
	EventMarginCallWarning     EventType = "MARGIN_CALL_WARNING"
)

// Profit & Loss events.
const (
	EventRealizedPnL         EventType = "REALIZED_PNL"
	EventUnrealizedPnLUpdate EventType = "UNREALIZED_PNL_UPDATE"
	EventDailyPnLReset       EventType = "DAILY_PNL_RESET"
)

// System-level events.
const (
	EventBotStarted         EventType = "BOT_STARTED"
	EventBotStopped         EventType = "BOT_STOPPED"
	EventConfigReloaded     EventType = "CONFIG_RELOADED"
	EventConnectionLost     EventType = "CONNECTION_LOST"
	EventConnectionRestored EventType = "CONNECTION_RESTORED"
)

// ============================================================================
// Core Log Entry
// ============================================================================

// LogEntry is the core structured log entry for all bot activities.
type LogEntry struct {
	// Core identification
	ID        ulid.ULID `json:"event_id"`
	TraceID   string    `json:"trace_id"`
	Timestamp time.Time `json:"timestamp"`

	// Event classification
	EventType EventType `json:"event_type"`
	Severity  Severity  `json:"severity"`
	Version   string    `json:"version"` // Schema version

	// Context provides traceability
	Context EntryContext `json:"context"`

	// Payload contains event-specific data (stored as JSON in DB)
	Payload map[string]interface{} `json:"payload"`

	// Metadata contains technical details
	Metadata EntryMetadata `json:"metadata"`
}

// EntryContext provides business context for traceability.
type EntryContext struct {
	Symbol       string `json:"symbol"`
	StrategyID   string `json:"strategy_id"`
	StrategyName string `json:"strategy_name"`
	BotInstance  string `json:"bot_instance"`
}

// EntryMetadata contains technical metadata about the log entry.
type EntryMetadata struct {
	SourceFile string  `json:"source_file"`
	SourceLine int     `json:"source_line"`
	LatencyMs  float64 `json:"latency_ms"` // Processing latency
}

// ============================================================================
// Event-Specific Payload Types
// ============================================================================

// OrderPlacedPayload contains data for ORDER_PLACED events.
type OrderPlacedPayload struct {
	OrderID       string  `json:"order_id"`
	ClientOrderID string  `json:"client_order_id"`
	Side          string  `json:"side"` // BUY | SELL
	Type          string  `json:"type"` // LIMIT | MARKET
	Price         float64 `json:"price"`
	Quantity      float64 `json:"quantity"`
	TimeInForce   string  `json:"time_in_force"`
	Reason        string  `json:"reason"` // Why this order was placed
}

// OrderFilledPayload contains data for ORDER_FILLED events.
type OrderFilledPayload struct {
	OrderID         string  `json:"order_id"`
	ClientOrderID   string  `json:"client_order_id"`
	Side            string  `json:"side"`
	FilledPrice     float64 `json:"filled_price"`
	FilledQuantity  float64 `json:"filled_quantity"`
	FilledValue     float64 `json:"filled_value"`
	Fee             float64 `json:"fee"`
	FeeAsset        string  `json:"fee_asset"`
	ExecutionTimeMs int64   `json:"execution_time_ms"`
	GridLevel       *int    `json:"grid_level,omitempty"`
}

// OrderCancelledPayload contains data for ORDER_CANCELLED events.
type OrderCancelledPayload struct {
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	Reason        string `json:"reason"`
}

// OrderFailedPayload contains data for ORDER_FAILED events.
type OrderFailedPayload struct {
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	Error         string `json:"error"`
	ErrorCode     string `json:"error_code,omitempty"`
	WillRetry     bool   `json:"will_retry"`
}

// GridCreatedPayload contains data for GRID_CREATED events.
type GridCreatedPayload struct {
	GridID         string  `json:"grid_id"`
	Symbol         string  `json:"symbol"`
	Levels         int     `json:"levels"`
	LowerPrice     float64 `json:"lower_price"`
	UpperPrice     float64 `json:"upper_price"`
	GridSize       float64 `json:"grid_size"`
	TotalLiquidity float64 `json:"total_liquidity"`
}

// GridAdjustedPayload contains data for GRID_ADJUSTED events.
type GridAdjustedPayload struct {
	AdjustmentType  string  `json:"adjustment_type"`
	OldLowerPrice   float64 `json:"old_lower_price"`
	OldUpperPrice   float64 `json:"old_upper_price"`
	NewLowerPrice   float64 `json:"new_lower_price"`
	NewUpperPrice   float64 `json:"new_upper_price"`
	Reason          string  `json:"reason"`
	OrdersCancelled int     `json:"orders_cancelled"`
	OrdersPlaced    int     `json:"orders_placed"`
}

// GridClosedPayload contains data for GRID_CLOSED events.
type GridClosedPayload struct {
	GridID        string  `json:"grid_id"`
	Reason        string  `json:"reason"`
	RealizedPnL   float64 `json:"realized_pnl"`
	DurationHours float64 `json:"duration_hours"`
}

// StoplossTriggeredPayload contains data for STOPLOSS_TRIGGERED events.
type StoplossTriggeredPayload struct {
	PositionID      string  `json:"position_id"`
	TriggerPrice    float64 `json:"trigger_price"`
	MarkPrice       float64 `json:"mark_price_at_trigger"`
	StoplossType    string  `json:"stoploss_type"`
	RealizedPnL     float64 `json:"realized_pnl"`
	ClosePercentage float64 `json:"close_percentage"`
}

// PositionUpdatedPayload contains data for POSITION_UPDATED events.
type PositionUpdatedPayload struct {
	PositionID       string  `json:"position_id"`
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	Size             float64 `json:"size"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	LiquidationPrice float64 `json:"liquidation_price,omitempty"`
	MarginRatio      float64 `json:"margin_ratio,omitempty"`
}

// RiskLimitHitPayload contains data for RISK_LIMIT_HIT events.
type RiskLimitHitPayload struct {
	LimitType    string  `json:"limit_type"`    // daily_loss, position_size, margin_ratio
	CurrentValue float64 `json:"current_value"`
	LimitValue   float64 `json:"limit_value"`
	ActionTaken  string  `json:"action_taken"` // pause, reduce_position, close_all
}

// PnLUpdatePayload contains data for REALIZED_PNL and UNREALIZED_PNL_UPDATE events.
type PnLUpdatePayload struct {
	RealizedPnL      float64 `json:"realized_pnl,omitempty"`
	UnrealizedPnL    float64 `json:"unrealized_pnl,omitempty"`
	TotalPnL         float64 `json:"total_pnl,omitempty"`
	DailyPnL         float64 `json:"daily_pnl,omitempty"`
	DailyPnLPct      float64 `json:"daily_pnl_pct,omitempty"`
	WinCount         int     `json:"win_count,omitempty"`
	LossCount        int     `json:"loss_count,omitempty"`
	WinRate          float64 `json:"win_rate,omitempty"`
	AvgWinAmount     float64 `json:"avg_win_amount,omitempty"`
	AvgLossAmount    float64 `json:"avg_loss_amount,omitempty"`
}

// StrategyErrorPayload contains data for STRATEGY_ERROR events.
type StrategyErrorPayload struct {
	Error       string `json:"error"`
	ErrorType   string `json:"error_type"` // connection, validation, execution, system
	Recoverable bool   `json:"recoverable"`
	ActionTaken string `json:"action_taken"`
}

// ============================================================================
// Query DSL
// ============================================================================

// TimeRange represents a time range for queries.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Pagination represents pagination options for queries.
type Pagination struct {
	Limit  int    `json:"limit"`  // Default: 100, Max: 1000
	Offset int    `json:"offset"`
	Cursor string `json:"cursor,omitempty"` // For cursor-based pagination
}

// SortOrder represents the sort direction.
type SortOrder string

const (
	SortAsc  SortOrder = "ASC"
	SortDesc SortOrder = "DESC"
)

// Query represents the query DSL for activity logs.
type Query struct {
	TimeRange  TimeRange   `json:"time_range"`
	EventTypes []EventType `json:"event_types,omitempty"`
	Symbols    []string    `json:"symbols,omitempty"`
	Strategies []string    `json:"strategies,omitempty"`
	Severities []Severity  `json:"severities,omitempty"`
	TraceID    string      `json:"trace_id,omitempty"`
	FullText   string      `json:"full_text,omitempty"`
	Pagination Pagination  `json:"pagination"`
	SortOrder  SortOrder   `json:"sort_order"` // Default: DESC
}

// QueryResult represents the result of a query.
type QueryResult struct {
	Entries    []LogEntry `json:"entries"`
	Total      int64      `json:"total"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// ============================================================================
// Aggregation Types
// ============================================================================

// AggregationType represents the type of aggregation operation.
type AggregationType string

const (
	AggCount AggregationType = "count"
	AggSum   AggregationType = "sum"
	AggAvg   AggregationType = "avg"
	AggMin   AggregationType = "min"
	AggMax   AggregationType = "max"
)

// Aggregation represents an aggregation query.
type Aggregation struct {
	Type      AggregationType `json:"type"`
	Field     string          `json:"field"`     // Field to aggregate
	GroupBy   []string        `json:"group_by"`  // Group by these fields
	TimeRange TimeRange       `json:"time_range"`
}

// AggregationResult represents the result of an aggregation.
type AggregationResult struct {
	Results []AggregationGroup `json:"results"`
}

// AggregationGroup represents a single group in an aggregation result.
type AggregationGroup struct {
	GroupKey   map[string]interface{} `json:"group_key"`
	Value      float64                `json:"value"`
	Count      int64                  `json:"count"`
}
