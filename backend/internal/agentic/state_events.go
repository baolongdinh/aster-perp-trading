package agentic

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// StateTransitionEvent represents a request to transition to a new trading state
// This is used for hybrid integration between AgenticEngine and VolumeFarmEngine
type StateTransitionEvent struct {
	Symbol    string          `json:"symbol"`
	FromState TradingMode     `json:"from_state"`
	ToState   TradingMode     `json:"to_state"`
	Trigger   string          `json:"trigger"`
	Score     float64         `json:"score"`
	Timestamp time.Time       `json:"timestamp"`
	Priority  EventPriority   `json:"priority"`
	Params    ExecutionParams `json:"params"` // Parameters for execution
	Regime    RegimeSnapshot  `json:"regime"` // Current market regime
}

// EventPriority defines the urgency of state transition
type EventPriority int

const (
	PriorityLow EventPriority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// ExecutionParams contains parameters for execution layer
type ExecutionParams struct {
	// Grid-specific
	RangeLow       float64 `json:"range_low,omitempty"`
	RangeHigh      float64 `json:"range_high,omitempty"`
	GridLevels     int     `json:"grid_levels,omitempty"`
	AsymmetricBias string  `json:"asymmetric_bias,omitempty"` // "long", "short", "neutral"

	// Trend-specific
	TrendDirection string  `json:"trend_direction,omitempty"` // "up", "down"
	EntryPrice     float64 `json:"entry_price,omitempty"`
	StopLoss       float64 `json:"stop_loss,omitempty"`
	TakeProfit     float64 `json:"take_profit,omitempty"`
	TrailingStop   bool    `json:"trailing_stop,omitempty"`

	// Defensive-specific
	ExitPercentage float64 `json:"exit_percentage,omitempty"` // 0.5 = 50% exit
	ExitReason     string  `json:"exit_reason,omitempty"`

	// Accumulation-specific
	WyckoffPhase   string  `json:"wyckoff_phase,omitempty"`
	TargetPosition float64 `json:"target_position,omitempty"`

	// Position sizing adjustments
	PositionSizeMultiplier float64 `json:"position_size_multiplier,omitempty"`
	MaxPositionUSDT        float64 `json:"max_position_usdt,omitempty"`
}

// StateEventPublisher publishes state transition events to subscribers
type StateEventPublisher struct {
	mu          sync.RWMutex
	subscribers []StateTransitionHandler
	logger      *zap.Logger
}

// StateTransitionHandler is the interface for handling state transitions
type StateTransitionHandler interface {
	HandleStateTransition(ctx context.Context, event StateTransitionEvent) error
	GetHandlerName() string
}

// NewStateEventPublisher creates a new state event publisher
func NewStateEventPublisher(logger *zap.Logger) *StateEventPublisher {
	return &StateEventPublisher{
		subscribers: make([]StateTransitionHandler, 0),
		logger:      logger.With(zap.String("component", "state_event_publisher")),
	}
}

// Subscribe registers a handler for state transition events
func (sep *StateEventPublisher) Subscribe(handler StateTransitionHandler) {
	sep.mu.Lock()
	defer sep.mu.Unlock()
	sep.subscribers = append(sep.subscribers, handler)
	sep.logger.Info("State transition handler subscribed",
		zap.String("handler", handler.GetHandlerName()),
		zap.Int("total_handlers", len(sep.subscribers)),
	)
}

// Unsubscribe removes a handler from subscribers
func (sep *StateEventPublisher) Unsubscribe(handler StateTransitionHandler) {
	sep.mu.Lock()
	defer sep.mu.Unlock()
	for i, h := range sep.subscribers {
		if h.GetHandlerName() == handler.GetHandlerName() {
			sep.subscribers = append(sep.subscribers[:i], sep.subscribers[i+1:]...)
			break
		}
	}
}

// Publish broadcasts a state transition event to all subscribers
func (sep *StateEventPublisher) Publish(ctx context.Context, event StateTransitionEvent) error {
	sep.logger.Info("Publishing state transition event",
		zap.String("symbol", event.Symbol),
		zap.String("from", string(event.FromState)),
		zap.String("to", string(event.ToState)),
		zap.String("trigger", event.Trigger),
		zap.Float64("score", event.Score),
		zap.Int("priority", int(event.Priority)),
	)

	sep.mu.RLock()
	handlers := make([]StateTransitionHandler, len(sep.subscribers))
	copy(handlers, sep.subscribers)
	sep.mu.RUnlock()

	var lastError error
	for _, handler := range handlers {
		if err := handler.HandleStateTransition(ctx, event); err != nil {
			sep.logger.Error("Handler failed to process state transition",
				zap.String("handler", handler.GetHandlerName()),
				zap.String("symbol", event.Symbol),
				zap.Error(err),
			)
			lastError = err
		} else {
			sep.logger.Debug("Handler processed state transition",
				zap.String("handler", handler.GetHandlerName()),
				zap.String("symbol", event.Symbol),
			)
		}
	}

	return lastError
}

// GetSubscriberCount returns the number of subscribers
func (sep *StateEventPublisher) GetSubscriberCount() int {
	sep.mu.RLock()
	defer sep.mu.RUnlock()
	return len(sep.subscribers)
}

// ExecutionResult represents the result of executing a state transition
type ExecutionResult struct {
	Success     bool      `json:"success"`
	Symbol      string    `json:"symbol"`
	ToState     string    `json:"to_state"`
	Error       string    `json:"error,omitempty"`
	ExecutionID string    `json:"execution_id"`
	Timestamp   time.Time `json:"timestamp"`
	Trigger     string    `json:"trigger,omitempty"`
	ExitReason  string    `json:"exit_reason,omitempty"`

	// Execution details
	OrdersPlaced    int     `json:"orders_placed,omitempty"`
	OrdersCancelled int     `json:"orders_cancelled,omitempty"`
	PositionSize    float64 `json:"position_size,omitempty"`
	PositionValue   float64 `json:"position_value,omitempty"`
}

// ExecutionResultHandler handles execution results from VolumeFarmEngine
type ExecutionResultHandler interface {
	HandleExecutionResult(ctx context.Context, result ExecutionResult) error
}

// StateEventBus provides bidirectional communication between Agentic and VF
type StateEventBus struct {
	publisher        *StateEventPublisher
	resultHandlersMu sync.RWMutex
	resultHandlers   []ExecutionResultHandler
	logger           *zap.Logger
}

// NewStateEventBus creates a new state event bus
func NewStateEventBus(logger *zap.Logger) *StateEventBus {
	return &StateEventBus{
		publisher:      NewStateEventPublisher(logger),
		resultHandlers: make([]ExecutionResultHandler, 0),
		logger:         logger.With(zap.String("component", "state_event_bus")),
	}
}

// GetPublisher returns the state event publisher
func (seb *StateEventBus) GetPublisher() *StateEventPublisher {
	return seb.publisher
}

// SubscribeToResults registers a handler for execution results
func (seb *StateEventBus) SubscribeToResults(handler ExecutionResultHandler) {
	seb.resultHandlersMu.Lock()
	defer seb.resultHandlersMu.Unlock()
	seb.resultHandlers = append(seb.resultHandlers, handler)
}

// PublishResult broadcasts an execution result
func (seb *StateEventBus) PublishResult(ctx context.Context, result ExecutionResult) {
	seb.logger.Info("Publishing execution result",
		zap.String("symbol", result.Symbol),
		zap.String("to_state", result.ToState),
		zap.Bool("success", result.Success),
	)

	seb.resultHandlersMu.RLock()
	handlers := make([]ExecutionResultHandler, len(seb.resultHandlers))
	copy(handlers, seb.resultHandlers)
	seb.resultHandlersMu.RUnlock()

	for _, handler := range handlers {
		if err := handler.HandleExecutionResult(ctx, result); err != nil {
			seb.logger.Error("Result handler failed",
				zap.Error(err),
			)
		}
	}
}
