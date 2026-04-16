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
}

// NewDecisionEngine creates a new centralized decision engine
func NewDecisionEngine(
	cfg *config.DecisionEngineConfig,
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

	return &DecisionEngine{
		config:           cfg,
		logger:           logger.With(zap.String("component", "decision_engine")),
		scoreEngine:      scoreEngine,
		stateVersion:     0,
		symbolStates:     make(map[string]*SymbolTradingState),
		eventSubscribers: make([]chan<- StateTransition, 0),
		metrics: &StateManagerMetrics{
			SuccessfulModes: make(map[TradingMode]int),
		},
		lastTransitions: make(map[string]time.Time),
		flipFlopCount:   make(map[string]int),
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
		transition, err := de.executeTransition(symbol, currentState, bestMode, bestScore)
		if err != nil {
			return nil, fmt.Errorf("transition execution failed: %w", err)
		}

		// Publish event
		de.publishTransition(*transition)

		// Update metrics
		de.recordSuccessfulTransition(symbol, bestMode)

		return transition, nil
	}

	// No transition needed
	return nil, nil
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
	var bestMode TradingMode
	var bestScore float64

	for mode, score := range scores {
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
func (de *DecisionEngine) executeTransition(
	symbol string,
	fromMode, toMode TradingMode,
	score float64,
) (*StateTransition, error) {
	// CAS operation: increment version and swap state atomically
	newVersion := atomic.AddUint64(&de.stateVersion, 1)

	de.stateMu.Lock()
	defer de.stateMu.Unlock()

	// Get or create symbol state
	state, ok := de.symbolStates[symbol]
	if !ok {
		state = &SymbolTradingState{
			Symbol:            symbol,
			CurrentMode:       fromMode,
			ModeScores:        make(map[TradingMode]*TradingModeScore),
			TransitionHistory: make([]StateTransition, 0),
		}
		de.symbolStates[symbol] = state
	}

	// Verify we haven't raced (optional check)
	if state.CurrentMode != fromMode {
		de.logger.Warn("State changed during transition evaluation",
			zap.String("symbol", symbol),
			zap.String("expected", string(fromMode)),
			zap.String("actual", string(state.CurrentMode)),
		)
	}

	// Create transition record
	transition := StateTransition{
		FromState:         fromMode,
		ToState:           toMode,
		Trigger:           "score_evaluation",
		Score:             score,
		SmoothingDuration: de.config.SmoothingDuration,
		Timestamp:         time.Now(),
	}

	// Update state
	state.PreviousMode = state.CurrentMode
	state.CurrentMode = toMode
	state.TransitionConfidence = score
	state.LastTransition = time.Now()
	state.TransitionHistory = append(state.TransitionHistory, transition)

	// Keep only last 10 transitions
	if len(state.TransitionHistory) > 10 {
		state.TransitionHistory = state.TransitionHistory[1:]
	}

	// Start transition smoothing
	if de.config.SmoothingDuration > 0 {
		state.IsTransitioning = true
		state.TargetMode = toMode
		state.BlendWeight = 0.0

		// Start smoothing goroutine
		go de.smoothTransition(symbol, fromMode, toMode, de.config.SmoothingDuration)
	}

	de.logger.Info("State transition executed",
		zap.String("symbol", symbol),
		zap.String("from", string(fromMode)),
		zap.String("to", string(toMode)),
		zap.Float64("score", score),
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
	if !ok || len(state.TransitionHistory) < 2 {
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

// ForceTransition forces a state transition (for emergency use)
func (de *DecisionEngine) ForceTransition(symbol string, toMode TradingMode, reason string) error {
	currentState := de.getCurrentState(symbol)

	transition, err := de.executeTransition(symbol, currentState, toMode, 1.0)
	if err != nil {
		return fmt.Errorf("force transition failed: %w", err)
	}

	transition.Trigger = "forced: " + reason
	de.publishTransition(*transition)

	de.logger.Warn("Forced state transition",
		zap.String("symbol", symbol),
		zap.String("to", string(toMode)),
		zap.String("reason", reason),
	)

	return nil
}
