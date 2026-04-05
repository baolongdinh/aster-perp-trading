package adaptive_grid

import (
	"fmt"

	"go.uber.org/zap"
)

// OrderState represents the state of an order
type OrderState string

const (
	OrderStatePending   OrderState = "PENDING"
	OrderStateFilled    OrderState = "FILLED"
	OrderStateCancelled OrderState = "CANCELLED"
	OrderStateRejected  OrderState = "REJECTED"
	OrderStateUnknown   OrderState = "UNKNOWN"
)

// StateValidator validates order state transitions
type StateValidator struct {
	logger *zap.Logger
}

// NewStateValidator creates a new state validator
func NewStateValidator(logger *zap.Logger) *StateValidator {
	return &StateValidator{logger: logger}
}

// ValidTransitions defines allowed state transitions
var ValidTransitions = map[OrderState][]OrderState{
	OrderStatePending:   {OrderStateFilled, OrderStateCancelled, OrderStateRejected},
	OrderStateFilled:    {}, // Terminal state
	OrderStateCancelled: {}, // Terminal state
	OrderStateRejected:  {}, // Terminal state
	OrderStateUnknown:   {OrderStatePending, OrderStateFilled, OrderStateCancelled},
}

// IsValidTransition checks if a state transition is valid
func (v *StateValidator) IsValidTransition(from, to OrderState) bool {
	allowedStates, exists := ValidTransitions[from]
	if !exists {
		return false
	}

	for _, allowed := range allowedStates {
		if allowed == to {
			return true
		}
	}
	return false
}

// ValidateAndLog validates transition and logs if invalid
func (v *StateValidator) ValidateAndLog(orderID string, from, to OrderState) error {
	if v.IsValidTransition(from, to) {
		return nil
	}

	// Invalid transition detected
	err := fmt.Errorf("invalid order state transition: %s -> %s for order %s", from, to, orderID)
	
	v.logger.Error("State transition validation failed",
		zap.String("orderID", orderID),
		zap.String("from_state", string(from)),
		zap.String("to_state", string(to)),
		zap.Error(err))

	return err
}

// IsTerminalState checks if state is terminal
func IsTerminalState(state OrderState) bool {
	return state == OrderStateFilled || 
		   state == OrderStateCancelled || 
		   state == OrderStateRejected
}

// CanModify checks if order can still be modified/cancelled
func CanModify(state OrderState) bool {
	return state == OrderStatePending || state == OrderStateUnknown
}

// StateTransition represents a recorded state change
type StateTransition struct {
	OrderID   string
	From      OrderState
	To        OrderState
	Timestamp int64
	Valid     bool
}

// TransitionHistory tracks state changes for audit
type TransitionHistory struct {
	transitions []StateTransition
	maxSize     int
}

// NewTransitionHistory creates history tracker
func NewTransitionHistory() *TransitionHistory {
	return &TransitionHistory{
		transitions: make([]StateTransition, 0, 1000),
		maxSize:     10000,
	}
}

// Record records a state transition
func (h *TransitionHistory) Record(orderID string, from, to OrderState, timestamp int64, valid bool) {
	transition := StateTransition{
		OrderID:   orderID,
		From:      from,
		To:        to,
		Timestamp: timestamp,
		Valid:     valid,
	}

	h.transitions = append(h.transitions, transition)

	// Maintain max size
	if len(h.transitions) > h.maxSize {
		h.transitions = h.transitions[len(h.transitions)-h.maxSize:]
	}
}

// GetInvalidTransitions returns all recorded invalid transitions
func (h *TransitionHistory) GetInvalidTransitions() []StateTransition {
	invalid := make([]StateTransition, 0)
	for _, t := range h.transitions {
		if !t.Valid {
			invalid = append(invalid, t)
		}
	}
	return invalid
}

// GetRecentTransitions returns N most recent transitions for an order
func (h *TransitionHistory) GetRecentTransitions(orderID string, n int) []StateTransition {
	result := make([]StateTransition, 0, n)
	count := 0
	
	// Iterate backwards to get most recent
	for i := len(h.transitions) - 1; i >= 0; i-- {
		if h.transitions[i].OrderID == orderID {
			result = append(result, h.transitions[i])
			count++
			if count >= n {
				break
			}
		}
	}
	
	// Reverse to get chronological order
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	
	return result
}
