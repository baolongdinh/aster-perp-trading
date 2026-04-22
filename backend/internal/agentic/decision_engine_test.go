package agentic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewDecisionEngine(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)

	// Test with nil config
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)
	assert.NotNil(t, engine)
	assert.NotNil(t, engine.config)
	assert.True(t, engine.config.Enabled)
	assert.Equal(t, 3, engine.config.MaxFlipFlopsPerHour)
}

func TestGetCurrentState(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)

	// Test default state for new symbol
	state := engine.getCurrentState("BTCUSD1")
	assert.Equal(t, TradingModeIdle, state)
}

func TestDetermineBestMode(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)

	// Test 1: GRID wins
	scores := map[TradingMode]*TradingModeScore{
		TradingModeGrid: {
			Mode:      TradingModeGrid,
			Score:     0.7,
			Threshold: 0.6,
		},
		TradingModeTrending: {
			Mode:      TradingModeTrending,
			Score:     0.5,
			Threshold: 0.75,
		},
	}

	bestMode, bestScore, shouldTransition := engine.determineBestMode("BTCUSD1", TradingModeIdle, scores)
	assert.True(t, shouldTransition)
	assert.Equal(t, TradingModeGrid, bestMode)
	assert.Equal(t, 0.7, bestScore)

	// Test 2: Hysteresis prevents switching
	bestMode2, bestScore2, shouldTransition2 := engine.determineBestMode("BTCUSD1", TradingModeGrid, scores)
	// GRID is already active, same score shouldn't trigger transition
	assert.False(t, shouldTransition2, "Should not transition to same mode")
	assert.Equal(t, TradingModeIdle, bestMode2) // Zero value
	assert.Equal(t, 0.0, bestScore2)
}

func TestIsFlipFlop(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)

	// Setup state with transition history
	engine.symbolStates["BTCUSD1"] = &SymbolTradingState{
		Symbol:      "BTCUSD1",
		CurrentMode: TradingModeTrending,
		TransitionHistory: []StateTransition{
			{
				FromState: TradingModeIdle,
				ToState:   TradingModeGrid,
				Timestamp: time.Now().Add(-10 * time.Minute),
			},
			{
				FromState: TradingModeGrid,
				ToState:   TradingModeTrending,
				Timestamp: time.Now().Add(-1 * time.Minute), // Recent
			},
		},
	}

	// Test flip-flop: going back to GRID immediately
	isFlip := engine.isFlipFlop("BTCUSD1", TradingModeTrending, TradingModeGrid)
	assert.True(t, isFlip, "Going back to previous mode within 5min should be flip-flop")

	// Test not flip-flop: going to new mode
	isFlip2 := engine.isFlipFlop("BTCUSD1", TradingModeTrending, TradingModeAccumulation)
	assert.False(t, isFlip2, "Going to new mode should not be flip-flop")
}

func TestEventPublishing(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)

	// Create subscriber channel
	ch := make(chan StateTransition, 10)
	engine.SubscribeToTransitions(ch)
	assert.Equal(t, 1, len(engine.eventSubscribers))

	// Publish a transition
	transition := StateTransition{
		FromState: TradingModeIdle,
		ToState:   TradingModeGrid,
		Score:     0.8,
	}
	engine.publishTransition(transition)

	// Receive the event
	select {
	case received := <-ch:
		assert.Equal(t, TradingModeIdle, received.FromState)
		assert.Equal(t, TradingModeGrid, received.ToState)
	case <-time.After(time.Second):
		t.Fatal("Did not receive transition event")
	}

	// Unsubscribe
	engine.UnsubscribeFromTransitions(ch)
	assert.Equal(t, 0, len(engine.eventSubscribers))
}

func TestGetSymbolState(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)

	// Test non-existent symbol
	state, ok := engine.GetSymbolState("NONEXISTENT")
	assert.False(t, ok)
	assert.Nil(t, state)

	// Manually add state
	engine.symbolStates["BTCUSD1"] = &SymbolTradingState{
		Symbol:      "BTCUSD1",
		CurrentMode: TradingModeGrid,
	}

	state, ok = engine.GetSymbolState("BTCUSD1")
	assert.True(t, ok)
	assert.NotNil(t, state)
	assert.Equal(t, TradingModeGrid, state.CurrentMode)
}

func TestGetMetrics(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)
	// Record some transitions
	engine.recordSuccessfulTransition("BTCUSD1", TradingModeGrid)
	engine.recordSuccessfulTransition("BTCUSD1", TradingModeTrending)
	engine.recordSuccessfulTransition("BTCUSD1", TradingModeGrid)

	metrics := engine.GetMetrics()
	assert.Equal(t, 3, metrics.TotalTransitions)
	assert.Equal(t, 2, metrics.SuccessfulModes[TradingModeGrid])
	assert.Equal(t, 1, metrics.SuccessfulModes[TradingModeTrending])
}

func TestForceTransition(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)

	// Create subscriber to verify event is published
	ch := make(chan StateTransition, 1)
	engine.SubscribeToTransitions(ch)

	// Force transition
	err := engine.ForceTransition("BTCUSD1", TradingModeDefensive, "emergency")
	assert.NoError(t, err)

	// Verify state changed
	state, ok := engine.GetSymbolState("BTCUSD1")
	assert.True(t, ok)
	assert.Equal(t, TradingModeDefensive, state.CurrentMode)

	// Verify event was published
	select {
	case transition := <-ch:
		assert.Equal(t, TradingModeDefensive, transition.ToState)
		assert.Contains(t, transition.Trigger, "forced")
	case <-time.After(time.Second):
		t.Fatal("Did not receive forced transition event")
	}
}

func TestFlipFlopPrevention(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	engine := NewDecisionEngine(nil, nil, scoreEngine, logger)

	// Setup initial state
	engine.symbolStates["BTCUSD1"] = &SymbolTradingState{
		Symbol:      "BTCUSD1",
		CurrentMode: TradingModeTrending,
		TransitionHistory: []StateTransition{
			{
				FromState: TradingModeGrid,
				ToState:   TradingModeTrending,
				Timestamp: time.Now().Add(-30 * time.Second), // Very recent
			},
		},
	}

	// Try to go back to GRID immediately (flip-flop)
	isFlip := engine.isFlipFlop("BTCUSD1", TradingModeTrending, TradingModeGrid)
	assert.True(t, isFlip)

	// Record the flip-flop
	engine.recordFlipFlop("BTCUSD1")
	assert.Equal(t, 1, engine.metrics.FlipFlopCount)
	assert.Equal(t, 1, engine.flipFlopCount["BTCUSD1"])
}
