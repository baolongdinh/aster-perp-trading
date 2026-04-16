package agentic

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AgenticVFBridge connects AgenticEngine (decision) with VolumeFarmEngine (execution)
// Implements the hybrid approach: Agentic decides WHAT to do, VF decides HOW to do it
type AgenticVFBridge struct {
	eventBus     *StateEventBus
	stateTracker *StateTracker
	logger       *zap.Logger

	// Execution status tracking
	pendingTransitions   map[string]StateTransitionEvent
	completedTransitions map[string]ExecutionResult
}

// StateTracker tracks the current state of each symbol
type StateTracker struct {
	currentStates map[string]SymbolTradingState
	mu            sync.RWMutex
}

// NewAgenticVFBridge creates a new bridge between Agentic and VF
func NewAgenticVFBridge(eventBus *StateEventBus, logger *zap.Logger) *AgenticVFBridge {
	bridge := &AgenticVFBridge{
		eventBus:             eventBus,
		stateTracker:         NewStateTracker(),
		logger:               logger.With(zap.String("component", "agentic_vf_bridge")),
		pendingTransitions:   make(map[string]StateTransitionEvent),
		completedTransitions: make(map[string]ExecutionResult),
	}

	// Subscribe to execution results
	eventBus.SubscribeToResults(bridge)

	return bridge
}

// NewStateTracker creates a new state tracker
func NewStateTracker() *StateTracker {
	return &StateTracker{
		currentStates: make(map[string]SymbolTradingState),
	}
}

// RequestStateTransition requests a state transition for a symbol
// Called by AgenticEngine when it decides to change state
func (bridge *AgenticVFBridge) RequestStateTransition(
	ctx context.Context,
	symbol string,
	fromState TradingMode,
	toState TradingMode,
	trigger string,
	score float64,
	regime RegimeSnapshot,
) error {
	bridge.logger.Info("Requesting state transition",
		zap.String("symbol", symbol),
		zap.String("from", string(fromState)),
		zap.String("to", string(toState)),
		zap.String("trigger", trigger),
		zap.Float64("score", score),
	)

	// Build execution parameters based on target state
	params := bridge.buildExecutionParams(toState, regime, symbol)

	// Determine priority
	priority := bridge.calculatePriority(toState, trigger, score)

	event := StateTransitionEvent{
		Symbol:    symbol,
		FromState: fromState,
		ToState:   toState,
		Trigger:   trigger,
		Score:     score,
		Timestamp: time.Now(),
		Priority:  priority,
		Params:    params,
		Regime:    regime,
	}

	// Track pending transition
	bridge.pendingTransitions[symbol] = event

	// Publish to VolumeFarmEngine
	if err := bridge.eventBus.GetPublisher().Publish(ctx, event); err != nil {
		delete(bridge.pendingTransitions, symbol)
		return fmt.Errorf("failed to publish state transition: %w", err)
	}

	return nil
}

// buildExecutionParams creates execution parameters based on state and regime
func (bridge *AgenticVFBridge) buildExecutionParams(
	targetState TradingMode,
	regime RegimeSnapshot,
	symbol string,
) ExecutionParams {
	params := ExecutionParams{
		PositionSizeMultiplier: 1.0,
	}

	switch targetState {
	case TradingModeGrid:
		// Grid-specific parameters
		params.RangeLow = 0    // Will be calculated by VF based on current price
		params.RangeHigh = 0   // Will be calculated by VF
		params.GridLevels = 10 // Suggested, VF can adjust
		params.AsymmetricBias = "neutral"

		// Adjust based on regime
		if regime.Regime == RegimeSideways {
			params.GridLevels = 15 // More levels in sideways
			params.PositionSizeMultiplier = 1.0
		} else {
			params.GridLevels = 8 // Fewer levels when less certain
			params.PositionSizeMultiplier = 0.7
		}

	case TradingModeTrending:
		// Trend-specific parameters
		params.TrendDirection = "up" // Default, VF should determine
		params.TrailingStop = true
		params.PositionSizeMultiplier = 0.8 // Smaller size for trend

		// Stop loss based on ATR
		if regime.ATR14 > 0 {
			params.StopLoss = regime.ATR14 * 2.5 // 2.5x ATR
		}

	case TradingModeAccumulation:
		// Accumulation parameters
		params.WyckoffPhase = "spring"      // VF should detect
		params.TargetPosition = 0.03        // 3% max position
		params.PositionSizeMultiplier = 0.5 // Gradual building

	case TradingModeDefensive:
		// Defensive parameters
		params.ExitPercentage = 1.0 // 100% exit by default
		params.ExitReason = "risk_protection"
		params.PositionSizeMultiplier = 0.0 // No new positions

	case TradingModeOverSize:
		// Position reduction parameters
		params.ExitPercentage = 0.5 // Reduce 50%
		params.PositionSizeMultiplier = 0.5

	case TradingModeRecovery:
		// Recovery parameters - reduced size
		params.PositionSizeMultiplier = 0.6 // 60% normal size

	case TradingModeIdle:
		// Idle - no positions
		params.PositionSizeMultiplier = 0.0
		params.ExitPercentage = 1.0
	}

	return params
}

// calculatePriority determines event priority
func (bridge *AgenticVFBridge) calculatePriority(
	targetState TradingMode,
	trigger string,
	score float64,
) EventPriority {
	switch targetState {
	case TradingModeDefensive:
		if trigger == "emergency_exit" || trigger == "max_loss" {
			return PriorityCritical
		}
		return PriorityHigh

	case TradingModeOverSize:
		return PriorityHigh

	case TradingModeRecovery:
		return PriorityNormal

	case TradingModeTrending:
		if score > 0.85 {
			return PriorityHigh // Strong trend signal
		}
		return PriorityNormal

	case TradingModeGrid:
		if score > 0.75 {
			return PriorityNormal // Good grid opportunity
		}
		return PriorityLow

	case TradingModeIdle:
		if trigger == "exit_all" {
			return PriorityHigh
		}
		return PriorityNormal

	default:
		return PriorityNormal
	}
}

// HandleExecutionResult processes execution results from VolumeFarmEngine
func (bridge *AgenticVFBridge) HandleExecutionResult(ctx context.Context, result ExecutionResult) error {
	bridge.logger.Info("Received execution result",
		zap.String("symbol", result.Symbol),
		zap.String("to_state", result.ToState),
		zap.Bool("success", result.Success),
	)

	// Remove from pending
	delete(bridge.pendingTransitions, result.Symbol)

	// Store result
	bridge.completedTransitions[result.Symbol] = result

	// Update state tracker
	if result.Success {
		bridge.stateTracker.UpdateState(result.Symbol, TradingMode(result.ToState))
	}

	return nil
}

// GetCurrentState returns the current trading state for a symbol
func (bridge *AgenticVFBridge) GetCurrentState(symbol string) (SymbolTradingState, bool) {
	return bridge.stateTracker.GetState(symbol)
}

// GetPendingTransitions returns transitions waiting for execution
func (bridge *AgenticVFBridge) GetPendingTransitions() map[string]StateTransitionEvent {
	result := make(map[string]StateTransitionEvent)
	for k, v := range bridge.pendingTransitions {
		result[k] = v
	}
	return result
}

// IsTransitionPending checks if a symbol has a pending transition
func (bridge *AgenticVFBridge) IsTransitionPending(symbol string) bool {
	_, exists := bridge.pendingTransitions[symbol]
	return exists
}

// UpdateState updates the current state for a symbol
func (st *StateTracker) UpdateState(symbol string, state TradingMode) {
	st.mu.Lock()
	defer st.mu.Unlock()

	current := st.currentStates[symbol]
	current.Symbol = symbol
	current.PreviousMode = current.CurrentMode
	current.CurrentMode = state
	current.LastTransition = time.Now()
	st.currentStates[symbol] = current
}

// GetState returns the current state for a symbol
func (st *StateTracker) GetState(symbol string) (SymbolTradingState, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	state, exists := st.currentStates[symbol]
	return state, exists
}

// GetAllStates returns all tracked states
func (st *StateTracker) GetAllStates() map[string]SymbolTradingState {
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make(map[string]SymbolTradingState)
	for k, v := range st.currentStates {
		result[k] = v
	}
	return result
}
