package adaptive_grid

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// GridState represents the current state of the trading lifecycle
// This is distinct from RangeState - it governs the trading bot's lifecycle
// while RangeState governs the market range detection
type GridState int

const (
	GridStateIdle GridState = iota
	GridStateEnterGrid
	GridStateTrading
	GridStateExitHalf // Partial exit - cut 50% position
	GridStateExitAll
	GridStateWaitNewRange
	GridStateOverSize  // Position size exceeds limit
	GridStateDefensive // Extreme volatility - defensive mode
	GridStateRecovery  // Recovery mode after losses
)

// String returns the string representation of a GridState
func (s GridState) String() string {
	switch s {
	case GridStateIdle:
		return "IDLE"
	case GridStateEnterGrid:
		return "ENTER_GRID"
	case GridStateTrading:
		return "TRADING"
	case GridStateExitHalf:
		return "EXIT_HALF"
	case GridStateExitAll:
		return "EXIT_ALL"
	case GridStateWaitNewRange:
		return "WAIT_NEW_RANGE"
	case GridStateOverSize:
		return "OVER_SIZE"
	case GridStateDefensive:
		return "DEFENSIVE"
	case GridStateRecovery:
		return "RECOVERY"
	default:
		return "UNKNOWN"
	}
}

// GridEvent represents events that trigger state transitions
type GridEvent int

const (
	EventRangeConfirmed       GridEvent = iota // WAIT_NEW_RANGE -> ENTER_GRID
	EventEntryPlaced                           // ENTER_GRID -> TRADING
	EventPartialLoss                           // TRADING -> EXIT_HALF
	EventFullLoss                              // TRADING/EXIT_HALF -> EXIT_ALL
	EventRecovery                              // EXIT_HALF -> TRADING
	EventTrendExit                             // TRADING -> EXIT_ALL
	EventEmergencyExit                         // TRADING -> EXIT_ALL (emergency)
	EventPositionsClosed                       // EXIT_ALL -> WAIT_NEW_RANGE
	EventNewRangeReady                         // WAIT_NEW_RANGE -> ENTER_GRID
	EventOverSizeLimit                         // TRADING -> OVER_SIZE
	EventSizeNormalized                        // OVER_SIZE -> TRADING
	EventExtremeVolatility                     // TRADING -> DEFENSIVE
	EventVolatilityNormalized                  // DEFENSIVE -> TRADING
	EventRecoveryStart                         // EXIT_HALF/EXIT_ALL -> RECOVERY
	EventRecoveryComplete                      // RECOVERY -> TRADING
)

// String returns the string representation of a GridEvent
func (e GridEvent) String() string {
	switch e {
	case EventRangeConfirmed:
		return "RANGE_CONFIRMED"
	case EventEntryPlaced:
		return "ENTRY_PLACED"
	case EventPartialLoss:
		return "PARTIAL_LOSS"
	case EventFullLoss:
		return "FULL_LOSS"
	case EventRecovery:
		return "RECOVERY"
	case EventTrendExit:
		return "TREND_EXIT"
	case EventEmergencyExit:
		return "EMERGENCY_EXIT"
	case EventPositionsClosed:
		return "POSITIONS_CLOSED"
	case EventNewRangeReady:
		return "NEW_RANGE_READY"
	case EventOverSizeLimit:
		return "OVER_SIZE_LIMIT"
	case EventSizeNormalized:
		return "SIZE_NORMALIZED"
	case EventExtremeVolatility:
		return "EXTREME_VOLATILITY"
	case EventVolatilityNormalized:
		return "VOLATILITY_NORMALIZED"
	case EventRecoveryStart:
		return "RECOVERY_START"
	case EventRecoveryComplete:
		return "RECOVERY_COMPLETE"
	default:
		return "UNKNOWN"
	}
}

// SymbolState tracks the state for a specific symbol
type SymbolState struct {
	State                GridState
	LastTransition       time.Time
	EntryTime            time.Time
	ExitTime             time.Time
	ConsecutiveLosses    int
	RegridCooldownActive bool
	RegridCooldownUntil  time.Time
}

// GridStateMachine manages the trading lifecycle state for all symbols
type GridStateMachine struct {
	states map[string]*SymbolState
	mu     sync.RWMutex
	logger *zap.Logger
}

// NewGridStateMachine creates a new state machine
func NewGridStateMachine(logger *zap.Logger) *GridStateMachine {
	return &GridStateMachine{
		states: make(map[string]*SymbolState),
		logger: logger,
	}
}

// GetState returns the current state for a symbol
func (sm *GridStateMachine) GetState(symbol string) GridState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.states[symbol]
	if !exists {
		return GridStateIdle
	}
	return state.State
}

// GetStateTime returns the timestamp of the last state transition for a symbol
func (sm *GridStateMachine) GetStateTime(symbol string) time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.states[symbol]
	if !exists {
		return time.Time{}
	}
	return state.LastTransition
}

// GetSymbolState returns the full state info for a symbol
func (sm *GridStateMachine) GetSymbolState(symbol string) *SymbolState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.states[symbol]
	if !exists {
		return &SymbolState{State: GridStateIdle}
	}
	return state
}

// CanTransition checks if a transition is valid from the current state
func (sm *GridStateMachine) CanTransition(symbol string, event GridEvent) bool {
	currentState := sm.GetState(symbol)

	switch currentState {
	case GridStateIdle:
		// From IDLE, only RangeConfirmed is valid (to ENTER_GRID)
		return event == EventRangeConfirmed

	case GridStateEnterGrid:
		// From ENTER_GRID, EntryPlaced moves to TRADING
		return event == EventEntryPlaced

	case GridStateTrading:
		// From TRADING: PartialLoss/OverSizeLimit/ExtremeVolatility/RecoveryStart -> EXIT_HALF/OVER_SIZE/DEFENSIVE/RECOVERY
		// TrendExit/EmergencyExit -> EXIT_ALL
		return event == EventPartialLoss || event == EventTrendExit || event == EventEmergencyExit ||
			event == EventOverSizeLimit || event == EventExtremeVolatility || event == EventRecoveryStart

	case GridStateExitHalf:
		// From EXIT_HALF: FullLoss -> EXIT_ALL, RecoveryStart -> RECOVERY, Recovery -> TRADING
		return event == EventFullLoss || event == EventRecovery || event == EventRecoveryStart

	case GridStateExitAll:
		// From EXIT_ALL: PositionsClosed -> WAIT_NEW_RANGE, RecoveryStart -> RECOVERY
		return event == EventPositionsClosed || event == EventRecoveryStart

	case GridStateWaitNewRange:
		// From WAIT_NEW_RANGE: NewRangeReady -> ENTER_GRID
		return event == EventNewRangeReady

	case GridStateOverSize:
		// From OVER_SIZE: SizeNormalized -> TRADING, FullLoss -> EXIT_ALL
		return event == EventSizeNormalized || event == EventFullLoss

	case GridStateDefensive:
		// From DEFENSIVE: VolatilityNormalized -> TRADING, EmergencyExit -> EXIT_ALL
		return event == EventVolatilityNormalized || event == EventEmergencyExit

	case GridStateRecovery:
		// From RECOVERY: RecoveryComplete -> TRADING, PartialLoss -> EXIT_HALF
		return event == EventRecoveryComplete || event == EventPartialLoss

	default:
		return false
	}
}

// Transition performs a state transition if valid
// Returns true if transition was successful, false otherwise
func (sm *GridStateMachine) Transition(symbol string, event GridEvent) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Initialize state if not exists
	state, exists := sm.states[symbol]
	if !exists {
		state = &SymbolState{State: GridStateIdle}
		sm.states[symbol] = state
	}

	// Check if transition is valid
	oldState := state.State
	var newState GridState

	switch oldState {
	case GridStateIdle:
		if event == EventRangeConfirmed {
			newState = GridStateEnterGrid
		} else {
			return false
		}

	case GridStateEnterGrid:
		if event == EventEntryPlaced {
			newState = GridStateTrading
			state.EntryTime = time.Now()
		} else {
			return false
		}

	case GridStateTrading:
		if event == EventPartialLoss {
			newState = GridStateExitHalf
		} else if event == EventOverSizeLimit {
			newState = GridStateOverSize
		} else if event == EventExtremeVolatility {
			newState = GridStateDefensive
		} else if event == EventRecoveryStart {
			newState = GridStateRecovery
		} else if event == EventTrendExit || event == EventEmergencyExit {
			newState = GridStateExitAll
			state.ExitTime = time.Now()
		} else {
			return false
		}

	case GridStateExitHalf:
		if event == EventFullLoss {
			newState = GridStateExitAll
			state.ExitTime = time.Now()
		} else if event == EventRecovery {
			newState = GridStateTrading
		} else if event == EventRecoveryStart {
			newState = GridStateRecovery
		} else {
			return false
		}

	case GridStateExitAll:
		if event == EventPositionsClosed {
			newState = GridStateWaitNewRange
		} else if event == EventRecoveryStart {
			newState = GridStateRecovery
		} else {
			return false
		}

	case GridStateWaitNewRange:
		if event == EventNewRangeReady {
			newState = GridStateEnterGrid
		} else {
			return false
		}

	case GridStateOverSize:
		if event == EventSizeNormalized {
			newState = GridStateTrading
		} else if event == EventFullLoss {
			newState = GridStateExitAll
			state.ExitTime = time.Now()
		} else {
			return false
		}

	case GridStateDefensive:
		if event == EventVolatilityNormalized {
			newState = GridStateTrading
		} else if event == EventEmergencyExit {
			newState = GridStateExitAll
			state.ExitTime = time.Now()
		} else {
			return false
		}

	case GridStateRecovery:
		if event == EventRecoveryComplete {
			newState = GridStateTrading
		} else if event == EventPartialLoss {
			newState = GridStateExitHalf
		} else {
			return false
		}

	default:
		return false
	}

	// Perform the transition
	state.State = newState
	state.LastTransition = time.Now()

	// JSONL logging as per spec
	sm.logger.Info("state_transition",
		zap.String("symbol", symbol),
		zap.String("from_state", oldState.String()),
		zap.String("to_state", newState.String()),
		zap.String("event", event.String()),
		zap.Time("timestamp", time.Now()),
	)

	return true
}

// ForceState forces a state change (for initialization/recovery)
func (sm *GridStateMachine) ForceState(symbol string, state GridState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.states[symbol] = &SymbolState{
		State:          state,
		LastTransition: time.Now(),
	}

	sm.logger.Info("state_forced",
		zap.String("symbol", symbol),
		zap.String("state", state.String()),
		zap.Time("timestamp", time.Now()),
	)
}

// IsTrading returns true if the symbol is in TRADING state
func (sm *GridStateMachine) IsTrading(symbol string) bool {
	return sm.GetState(symbol) == GridStateTrading
}

// CanPlaceOrders returns true if orders can be placed (ENTER_GRID or TRADING state)
func (sm *GridStateMachine) CanPlaceOrders(symbol string) bool {
	state := sm.GetState(symbol)
	return state == GridStateEnterGrid || state == GridStateTrading
}

// ShouldEnqueuePlacement returns true if placement should be enqueued
// This is the primary gate for the GridManager's shouldSchedulePlacement
func (sm *GridStateMachine) ShouldEnqueuePlacement(symbol string) bool {
	state := sm.GetState(symbol)
	// Only enqueue in states where we're allowed to place orders
	return state == GridStateEnterGrid || state == GridStateTrading
}

// RecordConsecutiveLoss increments consecutive losses for a symbol
func (sm *GridStateMachine) RecordConsecutiveLoss(symbol string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, exists := sm.states[symbol]
	if !exists {
		state = &SymbolState{State: GridStateIdle}
		sm.states[symbol] = state
	}
	state.ConsecutiveLosses++
}

// ResetConsecutiveLosses resets consecutive losses for a symbol
func (sm *GridStateMachine) ResetConsecutiveLosses(symbol string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, exists := sm.states[symbol]
	if !exists {
		return
	}
	state.ConsecutiveLosses = 0
}

// GetConsecutiveLosses returns the consecutive loss count for a symbol
func (sm *GridStateMachine) GetConsecutiveLosses(symbol string) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.states[symbol]
	if !exists {
		return 0
	}
	return state.ConsecutiveLosses
}

// ActivateRegridCooldown sets the regrid cooldown for a symbol
func (sm *GridStateMachine) ActivateRegridCooldown(symbol string, duration time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, exists := sm.states[symbol]
	if !exists {
		state = &SymbolState{State: GridStateIdle}
		sm.states[symbol] = state
	}
	state.RegridCooldownActive = true
	state.RegridCooldownUntil = time.Now().Add(duration)
}

// IsRegridCooldownActive checks if regrid cooldown is active for a symbol
func (sm *GridStateMachine) IsRegridCooldownActive(symbol string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.states[symbol]
	if !exists {
		return false
	}

	if !state.RegridCooldownActive {
		return false
	}

	if time.Now().After(state.RegridCooldownUntil) {
		// Cooldown expired
		return false
	}

	return true
}

// ClearRegridCooldown clears the regrid cooldown for a symbol
func (sm *GridStateMachine) ClearRegridCooldown(symbol string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, exists := sm.states[symbol]
	if !exists {
		return
	}
	state.RegridCooldownActive = false
}

// GetAllStates returns a copy of all symbol states for monitoring
func (sm *GridStateMachine) GetAllStates() map[string]GridState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]GridState)
	for symbol, state := range sm.states {
		result[symbol] = state.State
	}
	return result
}

// ValidateStateTransition validates if a transition from oldState to newState via event is valid
// This is a helper for testing and debugging
func ValidateStateTransition(oldState GridState, event GridEvent, newState GridState) error {
	var expectedNewState GridState
	valid := false

	switch oldState {
	case GridStateIdle:
		if event == EventRangeConfirmed {
			expectedNewState = GridStateEnterGrid
			valid = true
		}
	case GridStateEnterGrid:
		if event == EventEntryPlaced {
			expectedNewState = GridStateTrading
			valid = true
		}
	case GridStateTrading:
		if event == EventTrendExit || event == EventEmergencyExit {
			expectedNewState = GridStateExitAll
			valid = true
		}
	case GridStateExitAll:
		if event == EventPositionsClosed {
			expectedNewState = GridStateWaitNewRange
			valid = true
		}
	case GridStateWaitNewRange:
		if event == EventNewRangeReady {
			expectedNewState = GridStateEnterGrid
			valid = true
		}
	}

	if !valid {
		return fmt.Errorf("invalid transition: %s + %s", oldState.String(), event.String())
	}

	if newState != expectedNewState {
		return fmt.Errorf("transition result mismatch: expected %s, got %s", expectedNewState.String(), newState.String())
	}

	return nil
}
