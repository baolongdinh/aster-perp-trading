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
	eventBus       *StateEventBus
	stateTracker   *StateTracker
	decisionEngine *DecisionEngine
	logger         *zap.Logger

	// Execution status tracking (protected by mu)
	mu                   sync.Mutex
	pendingTransitions   map[string]StateTransitionEvent
	completedTransitions map[string]ExecutionResult
}

// StateTracker tracks the current state of each symbol
type StateTracker struct {
	currentStates map[string]SymbolTradingState
	mu            sync.RWMutex
}

// NewAgenticVFBridge creates a new bridge between Agentic and VF
func NewAgenticVFBridge(eventBus *StateEventBus, decisionEngine *DecisionEngine, logger *zap.Logger) *AgenticVFBridge {
	bridge := &AgenticVFBridge{
		eventBus:             eventBus,
		stateTracker:         NewStateTracker(),
		decisionEngine:       decisionEngine,
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
	intent TransitionIntent,
) error {
	bridge.logger.Info("Requesting state transition",
		zap.String("symbol", symbol),
		zap.String("from", string(intent.FromState)),
		zap.String("to", string(intent.ToState)),
		zap.String("trigger", intent.Trigger),
		zap.Float64("score", intent.Score),
	)

	// Build execution parameters based on target state
	params := bridge.buildExecutionParams(intent, symbol)

	// Determine priority
	priority := bridge.calculatePriority(intent.ToState, intent.Trigger, intent.Score)

	event := StateTransitionEvent{
		Symbol:    symbol,
		FromState: intent.FromState,
		ToState:   intent.ToState,
		Trigger:   intent.Trigger,
		Score:     intent.Score,
		Timestamp: time.Now(),
		Priority:  priority,
		Params:    params,
		Regime:    intent.MarketState.toRegimeSnapshot(intent.ExecutionContext.CurrentPrice),
		IntentID:  fmt.Sprintf("%s:%d", symbol, intent.Timestamp.UnixNano()),
	}

	// Track pending transition
	bridge.mu.Lock()
	bridge.pendingTransitions[symbol] = event
	bridge.mu.Unlock()

	// Publish to VolumeFarmEngine
	if err := bridge.eventBus.GetPublisher().Publish(ctx, event); err != nil {
		bridge.mu.Lock()
		delete(bridge.pendingTransitions, symbol)
		bridge.mu.Unlock()
		return fmt.Errorf("failed to publish state transition: %w", err)
	}

	return nil
}

// buildExecutionParams creates execution parameters based on state and regime
func (bridge *AgenticVFBridge) buildExecutionParams(
	intent TransitionIntent,
	symbol string,
) ExecutionParams {
	params := ExecutionParams{
		PositionSizeMultiplier: 1.0,
		TPBands:                append([]TPBand(nil), intent.LifecyclePolicy.TPBands...),
		SLPolicy:               intent.LifecyclePolicy.SLPolicy,
		TimeStopSec:            intent.LifecyclePolicy.SLPolicy.TimeStopSec,
		MaxPositionAgeSec:      intent.LifecyclePolicy.MaxPositionAgeSec,
		RegridPolicy:           intent.LifecyclePolicy.RegridPolicy,
		InventorySkew:          intent.LifecyclePolicy.InventorySkew,
		MakerOnly:              intent.LifecyclePolicy.MakerOnly,
		FeeBudgetBps:           intent.LifecyclePolicy.FeeBudgetBps,
		ExecutionContext:       intent.ExecutionContext,
	}

	switch intent.ToState {
	case TradingModeEnterGrid, TradingModeGrid:
		// Grid-specific parameters
		params.RangeLow = 0    // Will be calculated by VF based on current price
		params.RangeHigh = 0   // Will be calculated by VF
		params.GridLevels = 10 // Suggested, VF can adjust
		params.AsymmetricBias = "neutral"

		// Adjust based on regime
		if intent.MarketState.Regime == RegimeSideways {
			params.GridLevels = 15 // More levels in sideways
			params.PositionSizeMultiplier = 1.0
		} else {
			params.GridLevels = 8 // Fewer levels when less certain
			params.PositionSizeMultiplier = 0.7
		}
		if intent.ExecutionContext.InventoryNotional > 0 {
			params.AsymmetricBias = "inventory_rebalance"
		}

	case TradingModeTrending:
		// Trend-specific parameters
		params.TrendDirection = "up"
		params.TrailingStop = true
		params.PositionSizeMultiplier = 0.8 // Smaller size for trend

		// Stop loss based on ATR
		if intent.MarketState.TrendStrength < 0.5 {
			params.PositionSizeMultiplier = 0.35
		}
		params.StopLoss = intent.ExecutionContext.CurrentPrice * (intent.LifecyclePolicy.SLPolicy.HardLossBps / 10000)

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

	bridge.mu.Lock()
	// Remove from pending
	delete(bridge.pendingTransitions, result.Symbol)
	// Store result
	bridge.completedTransitions[result.Symbol] = result
	bridge.mu.Unlock()

	// Update state tracker
	if result.Success {
		bridge.stateTracker.UpdateState(result.Symbol, TradingMode(result.ToState))
	}

	if bridge.decisionEngine != nil {
		bridge.decisionEngine.RecordExecutionResult(result.Symbol, result.Success, result.ExitReason)
		if !result.Success {
			bridge.decisionEngine.IncrementConsecutiveLosses(result.Symbol)
		}
	}

	return nil
}

// GetCurrentState returns the current trading state for a symbol
func (bridge *AgenticVFBridge) GetCurrentState(symbol string) (SymbolTradingState, bool) {
	return bridge.stateTracker.GetState(symbol)
}

// GetPendingTransitions returns transitions waiting for execution
func (bridge *AgenticVFBridge) GetPendingTransitions() map[string]StateTransitionEvent {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	result := make(map[string]StateTransitionEvent)
	for k, v := range bridge.pendingTransitions {
		result[k] = v
	}
	return result
}

// IsTransitionPending checks if a symbol has a pending transition
func (bridge *AgenticVFBridge) IsTransitionPending(symbol string) bool {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
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
