package agentic

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"aster-bot/internal/config"

	"go.uber.org/zap"
)

// DecisionEngine is the centralized decision-making component
// It collects signals from all workers and makes unified state transition decisions
type DecisionEngine struct {
	config      *config.DecisionEngineConfig
	v2Config    *config.AgenticV2Config // Phase 0: AgenticV2 config
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine

	// State management
	stateVersion uint64 // Atomic counter for CAS operations
	symbolStates map[string]*SymbolTradingState
	stateMu      sync.RWMutex

	// Event publishing
	eventSubscribers []chan<- StateTransition
	eventMu          sync.RWMutex

	// Metrics
	metrics   *StateManagerMetrics
	metricsMu sync.RWMutex

	// Flip-flop prevention
	lastTransitions map[string]time.Time
	flipFlopCount   map[string]int

	// Phase 0: Rate limiting
	transitionHistory map[string][]time.Time // symbol -> timestamps of recent transitions
}

// NewDecisionEngine creates a new centralized decision engine
func NewDecisionEngine(
	cfg *config.DecisionEngineConfig,
	v2Cfg *config.AgenticV2Config,
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *DecisionEngine {
	if cfg == nil {
		cfg = &config.DecisionEngineConfig{
			Enabled:             true,
			TransitionTimeout:   5 * time.Second,
			SmoothingDuration:   5 * time.Second,
			MaxFlipFlopsPerHour: 3,
		}
	}
	if !cfg.Enabled {
		cfg.Enabled = true
	}
	if cfg.TransitionTimeout <= 0 {
		cfg.TransitionTimeout = 5 * time.Second
	}
	if cfg.SmoothingDuration <= 0 {
		cfg.SmoothingDuration = 5 * time.Second
	}
	if cfg.MaxFlipFlopsPerHour <= 0 {
		cfg.MaxFlipFlopsPerHour = 3
	}

	return &DecisionEngine{
		config:           cfg,
		v2Config:         v2Cfg,
		logger:           logger.With(zap.String("component", "decision_engine")),
		scoreEngine:      scoreEngine,
		stateVersion:     0,
		symbolStates:     make(map[string]*SymbolTradingState),
		eventSubscribers: make([]chan<- StateTransition, 0),
		metrics: &StateManagerMetrics{
			SuccessfulModes: make(map[TradingMode]int),
		},
		lastTransitions:   make(map[string]time.Time),
		flipFlopCount:     make(map[string]int),
		transitionHistory: make(map[string][]time.Time),
	}
}

// EvaluateAndDecide evaluates market conditions and decides on state transitions
// This is the main entry point called by the agentic engine on each detection cycle
func (de *DecisionEngine) EvaluateAndDecide(
	symbol string,
	inputs *ScoreInputs,
) (*StateTransition, error) {
	if !de.config.Enabled {
		return nil, fmt.Errorf("decision engine is disabled")
	}

	// 1. Get current state
	currentState := de.getCurrentState(symbol)

	// 2. Calculate all mode scores
	scores := de.scoreEngine.CalculateAllScores(inputs)

	// 3. Determine best mode with hysteresis
	bestMode, bestScore, shouldTransition := de.determineBestMode(
		symbol, currentState, scores,
	)

	// 4. Check for flip-flop
	if shouldTransition && de.isFlipFlop(symbol, currentState, bestMode) {
		de.logger.Warn("Flip-flop detected, maintaining current state",
			zap.String("symbol", symbol),
			zap.String("current", string(currentState)),
			zap.String("proposed", string(bestMode)),
		)
		de.recordFlipFlop(symbol)
		shouldTransition = false
	}

	// 5. Execute transition if needed
	if shouldTransition {
		intent := TransitionIntent{
			Symbol:    symbol,
			FromState: currentState,
			ToState:   bestMode,
			Trigger:   "score_evaluation",
			Score:     bestScore,
			Timestamp: time.Now(),
		}
		transition, err := de.CommitTransition(intent)
		if err != nil {
			return nil, fmt.Errorf("transition execution failed: %w", err)
		}

		return transition, nil
	}

	// No transition needed
	return nil, nil
}

// CommitTransition validates a handler-produced intent and commits it as the
// single source of truth for state changes.
func (de *DecisionEngine) CommitTransition(intent TransitionIntent) (*StateTransition, error) {
	if !de.config.Enabled {
		return nil, fmt.Errorf("decision engine is disabled")
	}
	if intent.Symbol == "" {
		return nil, fmt.Errorf("transition intent missing symbol")
	}
	if intent.ToState == "" {
		return nil, fmt.Errorf("transition intent missing target state")
	}
	if intent.Timestamp.IsZero() {
		intent.Timestamp = time.Now()
	}

	// Phase 0: Rate Limiting
	if de.v2Config != nil && de.v2Config.MaxTransitionsPerMin > 0 {
		if de.isRateLimited(intent.Symbol) {
			de.logger.Warn("Transition rate limited",
				zap.String("symbol", intent.Symbol),
				zap.Int("max_per_min", de.v2Config.MaxTransitionsPerMin))
			return nil, nil
		}
	}

	currentState := de.getCurrentState(intent.Symbol)
	if intent.FromState == "" {
		intent.FromState = currentState
	}
	if currentState != intent.FromState {
		de.logger.Warn("Transition intent using stale source state",
			zap.String("symbol", intent.Symbol),
			zap.String("expected", string(currentState)),
			zap.String("intent_from", string(intent.FromState)),
		)
		intent.FromState = currentState
	}
	if intent.ToState == intent.FromState {
		return nil, nil
	}
	if de.isFlipFlop(intent.Symbol, intent.FromState, intent.ToState) {
		de.logger.Warn("Flip-flop detected, rejecting transition intent",
			zap.String("symbol", intent.Symbol),
			zap.String("from", string(intent.FromState)),
			zap.String("to", string(intent.ToState)),
		)
		de.recordFlipFlop(intent.Symbol)
		return nil, nil
	}

	transition, err := de.executeTransition(intent)
	if err != nil {
		return nil, err
	}

	// Record for rate limiting
	de.recordTransition(intent.Symbol, intent.Timestamp)

	de.publishTransition(*transition)
	de.recordSuccessfulTransition(intent.Symbol, intent.ToState)
	return transition, nil
}

func (de *DecisionEngine) isRateLimited(symbol string) bool {
	de.stateMu.RLock()
	defer de.stateMu.RUnlock()

	history := de.transitionHistory[symbol]
	if len(history) < de.v2Config.MaxTransitionsPerMin {
		return false
	}

	// Check how many transitions in last minute
	cutoff := time.Now().Add(-1 * time.Minute)
	count := 0
	for _, t := range history {
		if t.After(cutoff) {
			count++
		}
	}

	return count >= de.v2Config.MaxTransitionsPerMin
}

func (de *DecisionEngine) recordTransition(symbol string, timestamp time.Time) {
	de.stateMu.Lock()
	defer de.stateMu.Unlock()

	de.transitionHistory[symbol] = append(de.transitionHistory[symbol], timestamp)

	// Keep only last 5 minutes of history for rate limiting
	cutoff := time.Now().Add(-5 * time.Minute)
	var filtered []time.Time
	for _, t := range de.transitionHistory[symbol] {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	de.transitionHistory[symbol] = filtered
}

// getCurrentState returns the current trading mode for a symbol
func (de *DecisionEngine) getCurrentState(symbol string) TradingMode {
	de.stateMu.RLock()
	defer de.stateMu.RUnlock()

	if state, ok := de.symbolStates[symbol]; ok {
		return state.CurrentMode
	}

	// Default to IDLE for new symbols
	return TradingModeIdle
}

// determineBestMode evaluates scores and determines if transition should occur
func (de *DecisionEngine) determineBestMode(
	symbol string,
	currentMode TradingMode,
	scores map[TradingMode]*TradingModeScore,
) (TradingMode, float64, bool) {
	bestMode := TradingModeIdle
	var bestScore float64

	for mode, score := range scores {
		if mode == currentMode {
			continue
		}

		// Skip if below threshold
		if score.Score < score.Threshold {
			continue
		}

		// Apply hysteresis for mode switching
		effectiveThreshold := score.Threshold
		if mode != currentMode {
			// Need higher score to switch (hysteresis buffer)
			effectiveThreshold += de.scoreEngine.config.HysteresisBuffer
		}

		if score.Score >= effectiveThreshold && score.Score > bestScore {
			bestMode = mode
			bestScore = score.Score
		}
	}

	shouldTransition := bestScore > 0 && bestMode != currentMode

	return bestMode, bestScore, shouldTransition
}

// executeTransition performs the state transition with atomic CAS
func (de *DecisionEngine) executeTransition(intent TransitionIntent) (*StateTransition, error) {
	// CAS operation: increment version and swap state atomically
	newVersion := atomic.AddUint64(&de.stateVersion, 1)

	de.stateMu.Lock()
	defer de.stateMu.Unlock()

	// Get or create symbol state
	state, ok := de.symbolStates[intent.Symbol]
	if !ok {
		state = &SymbolTradingState{
			Symbol:            intent.Symbol,
			CurrentMode:       intent.FromState,
			ModeScores:        make(map[TradingMode]*TradingModeScore),
			TransitionHistory: make([]StateTransition, 0),
			StateEnteredAt:    time.Now(),
		}
		de.symbolStates[intent.Symbol] = state
	}

	// Verify we haven't raced (optional check)
	if state.CurrentMode != intent.FromState {
		de.logger.Warn("State changed during transition evaluation",
			zap.String("symbol", intent.Symbol),
			zap.String("expected", string(intent.FromState)),
			zap.String("actual", string(state.CurrentMode)),
		)
	}

	// T013: Reset losses if transitioning out of RECOVERY
	if intent.FromState == TradingModeRecovery {
		state.ConsecutiveLosses = 0
	}

	// Create transition record
	transition := StateTransition{
		FromState:         intent.FromState,
		ToState:           intent.ToState,
		Trigger:           intent.Trigger,
		Score:             intent.Score,
		SmoothingDuration: de.config.SmoothingDuration,
		Timestamp:         intent.Timestamp,
	}

	// Update state
	state.PreviousMode = state.CurrentMode
	state.CurrentMode = intent.ToState
	state.TransitionConfidence = intent.Score
	state.LastTransition = intent.Timestamp
	state.StateEnteredAt = intent.Timestamp
	state.TransitionHistory = append(state.TransitionHistory, transition)
	state.LastIntentAt = intent.Timestamp
	state.LastIntentID = fmt.Sprintf("%s:%d", intent.Symbol, intent.Timestamp.UnixNano())
	state.PendingExecution = true

	// Keep only last 10 transitions
	if len(state.TransitionHistory) > 10 {
		state.TransitionHistory = state.TransitionHistory[1:]
	}

	// Start transition smoothing
	if de.config.SmoothingDuration > 0 {
		state.IsTransitioning = true
		state.TargetMode = intent.ToState
		state.BlendWeight = 0.0

		// Start smoothing goroutine
		go de.smoothTransition(intent.Symbol, intent.FromState, intent.ToState, de.config.SmoothingDuration)
	}

	de.logger.Info("State transition executed",
		zap.String("symbol", intent.Symbol),
		zap.String("from", string(intent.FromState)),
		zap.String("to", string(intent.ToState)),
		zap.Float64("score", intent.Score),
		zap.Uint64("version", newVersion),
	)

	return &transition, nil
}

// smoothTransition performs smooth weight blending during transition
func (de *DecisionEngine) smoothTransition(
	symbol string,
	fromMode, toMode TradingMode,
	duration time.Duration,
) {
	steps := 10
	stepDuration := duration / time.Duration(steps)

	for i := 0; i <= steps; i++ {
		weight := float64(i) / float64(steps)

		de.stateMu.Lock()
		if state, ok := de.symbolStates[symbol]; ok {
			state.BlendWeight = weight
		}
		de.stateMu.Unlock()

		if i < steps {
			time.Sleep(stepDuration)
		}
	}

	// Mark transition complete
	de.stateMu.Lock()
	if state, ok := de.symbolStates[symbol]; ok {
		state.IsTransitioning = false
		state.BlendWeight = 1.0
	}
	de.stateMu.Unlock()

	de.logger.Debug("Transition smoothing complete",
		zap.String("symbol", symbol),
		zap.String("mode", string(toMode)),
	)
}

// isFlipFlop checks if this transition would be a flip-flop
func (de *DecisionEngine) isFlipFlop(symbol string, currentMode, proposedMode TradingMode) bool {
	de.stateMu.RLock()
	defer de.stateMu.RUnlock()

	state, ok := de.symbolStates[symbol]
	if !ok || len(state.TransitionHistory) < 1 {
		return false
	}

	// Get last transition
	lastTransition := state.TransitionHistory[len(state.TransitionHistory)-1]

	// Flip-flop: going back to previous mode within short time
	if lastTransition.FromState == proposedMode && lastTransition.ToState == currentMode {
		// Check time since last transition
		timeSinceLast := time.Since(lastTransition.Timestamp)
		if timeSinceLast < 5*time.Minute { // 5 minute window
			return true
		}
	}

	return false
}

// recordFlipFlop increments flip-flop counter
func (de *DecisionEngine) recordFlipFlop(symbol string) {
	de.metricsMu.Lock()
	defer de.metricsMu.Unlock()

	de.metrics.FlipFlopCount++
	de.metrics.LastFlipFlop = time.Now()
	de.flipFlopCount[symbol]++
}

// recordSuccessfulTransition updates metrics
func (de *DecisionEngine) recordSuccessfulTransition(symbol string, mode TradingMode) {
	de.metricsMu.Lock()
	defer de.metricsMu.Unlock()

	de.metrics.TotalTransitions++
	de.metrics.SuccessfulModes[mode]++
}

// publishTransition publishes state change to all subscribers
func (de *DecisionEngine) publishTransition(transition StateTransition) {
	de.eventMu.RLock()
	subscribers := make([]chan<- StateTransition, len(de.eventSubscribers))
	copy(subscribers, de.eventSubscribers)
	de.eventMu.RUnlock()

	// Publish to all subscribers (non-blocking)
	for _, ch := range subscribers {
		select {
		case ch <- transition:
			// Published successfully
		default:
			// Channel full, log warning
			de.logger.Warn("Event subscriber channel full, dropping transition",
				zap.String("from", string(transition.FromState)),
				zap.String("to", string(transition.ToState)),
			)
		}
	}
}

// SubscribeToTransitions registers a channel to receive state transitions
func (de *DecisionEngine) SubscribeToTransitions(ch chan<- StateTransition) {
	de.eventMu.Lock()
	defer de.eventMu.Unlock()

	de.eventSubscribers = append(de.eventSubscribers, ch)
}

// UnsubscribeFromTransitions removes a subscriber
func (de *DecisionEngine) UnsubscribeFromTransitions(ch chan<- StateTransition) {
	de.eventMu.Lock()
	defer de.eventMu.Unlock()

	for i, subscriber := range de.eventSubscribers {
		if subscriber == ch {
			de.eventSubscribers = append(
				de.eventSubscribers[:i],
				de.eventSubscribers[i+1:]...,
			)
			break
		}
	}
}

// GetSymbolState returns the current state for a symbol
func (de *DecisionEngine) GetSymbolState(symbol string) (*SymbolTradingState, bool) {
	de.stateMu.RLock()
	defer de.stateMu.RUnlock()

	state, ok := de.symbolStates[symbol]
	return state, ok
}

// GetSymbolTradingState returns the current trading state for a symbol (pointer only)
func (de *DecisionEngine) GetSymbolTradingState(symbol string) *SymbolTradingState {
	de.stateMu.RLock()
	defer de.stateMu.RUnlock()

	return de.symbolStates[symbol]
}

// GetMetrics returns current metrics
func (de *DecisionEngine) GetMetrics() StateManagerMetrics {
	de.metricsMu.RLock()
	defer de.metricsMu.RUnlock()

	// Return copy
	metrics := *de.metrics
	metrics.SuccessfulModes = make(map[TradingMode]int)
	for k, v := range de.metrics.SuccessfulModes {
		metrics.SuccessfulModes[k] = v
	}
	return metrics
}

// IncrementConsecutiveLosses increments the counter for a specific symbol
func (de *DecisionEngine) IncrementConsecutiveLosses(symbol string) {
	de.stateMu.Lock()
	defer de.stateMu.Unlock()

	state, ok := de.symbolStates[symbol]
	if !ok {
		state = &SymbolTradingState{
			Symbol:            symbol,
			CurrentMode:       TradingModeIdle,
			ModeScores:        make(map[TradingMode]*TradingModeScore),
			TransitionHistory: make([]StateTransition, 0),
			StateEnteredAt:    time.Now(),
		}
		de.symbolStates[symbol] = state
	}
	state.ConsecutiveLosses++
	de.logger.Info("Incremented consecutive losses",
		zap.String("symbol", symbol),
		zap.Int("count", state.ConsecutiveLosses),
	)
}

// RecordExitContext stores the most recent exit context used by recovery logic.
func (de *DecisionEngine) RecordExitContext(symbol string, pnl float64, reason string) {
	de.stateMu.Lock()
	defer de.stateMu.Unlock()

	state, ok := de.symbolStates[symbol]
	if !ok {
		state = &SymbolTradingState{
			Symbol:            symbol,
			CurrentMode:       TradingModeIdle,
			ModeScores:        make(map[TradingMode]*TradingModeScore),
			TransitionHistory: make([]StateTransition, 0),
			StateEnteredAt:    time.Now(),
		}
		de.symbolStates[symbol] = state
	}

	state.LastExitPnL = pnl
	state.LastExitReason = reason
}

// RecordExecutionResult stores execution acknowledgements from the VF layer.
func (de *DecisionEngine) RecordExecutionResult(symbol string, success bool, reason string) {
	de.stateMu.Lock()
	defer de.stateMu.Unlock()

	state, ok := de.symbolStates[symbol]
	if !ok {
		return
	}

	state.LastExecutionAckAt = time.Now()
	state.PendingExecution = false
	if reason != "" {
		state.LastExitReason = reason
	}
	if success {
		state.StateStuckCount = 0
	}
}

// IncrementStateStuckCount increments the watchdog stuck counter for a symbol.
func (de *DecisionEngine) IncrementStateStuckCount(symbol string) {
	de.stateMu.Lock()
	defer de.stateMu.Unlock()

	state, ok := de.symbolStates[symbol]
	if !ok {
		return
	}
	state.StateStuckCount++
}

// ForceTransition forces a state transition (for emergency use)
func (de *DecisionEngine) ForceTransition(symbol string, toMode TradingMode, reason string) error {
	currentState := de.getCurrentState(symbol)

	transition, err := de.executeTransition(TransitionIntent{
		Symbol:    symbol,
		FromState: currentState,
		ToState:   toMode,
		Trigger:   "forced: " + reason,
		Score:     1.0,
		Timestamp: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("force transition failed: %w", err)
	}
	de.publishTransition(*transition)

	de.logger.Warn("Forced state transition",
		zap.String("symbol", symbol),
		zap.String("to", string(toMode)),
		zap.String("reason", reason),
	)

	return nil
}

// HasPendingExecutionAck reports whether the execution acknowledgement for the
// latest committed transition is overdue.
func (de *DecisionEngine) HasPendingExecutionAck(symbol string, timeout time.Duration) bool {
	de.stateMu.RLock()
	defer de.stateMu.RUnlock()

	state, ok := de.symbolStates[symbol]
	if !ok || !state.PendingExecution || timeout <= 0 {
		return false
	}
	return time.Since(state.LastIntentAt) > timeout
}
