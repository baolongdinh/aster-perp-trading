package adaptive_grid

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestGridStateMachine_BasicTransitions tests all valid state transitions
func TestGridStateMachine_BasicTransitions(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name      string
		event     GridEvent
		wantState GridState
		wantOk    bool
	}{
		{"IDLE -> ENTER_GRID", EventRangeConfirmed, GridStateEnterGrid, true},
		{"ENTER_GRID -> TRADING", EventEntryPlaced, GridStateTrading, true},
		{"TRADING -> EXIT_ALL (trend)", EventTrendExit, GridStateExitAll, true},
		{"EXIT_ALL -> WAIT_NEW_RANGE", EventPositionsClosed, GridStateWaitNewRange, true},
		{"WAIT_NEW_RANGE -> ENTER_GRID", EventNewRangeReady, GridStateEnterGrid, true},
		{"IDLE -> invalid (EntryPlaced)", EventEntryPlaced, GridStateIdle, false},
		{"ENTER_GRID -> invalid (TrendExit)", EventTrendExit, GridStateEnterGrid, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewGridStateMachine(logger) // fresh instance
			// Setup initial state based on test
			switch tt.name {
			case "ENTER_GRID -> TRADING":
				sm.ForceState("TEST", GridStateEnterGrid)
			case "TRADING -> EXIT_ALL (trend)":
				sm.ForceState("TEST", GridStateTrading)
			case "EXIT_ALL -> WAIT_NEW_RANGE":
				sm.ForceState("TEST", GridStateExitAll)
			case "WAIT_NEW_RANGE -> ENTER_GRID":
				sm.ForceState("TEST", GridStateWaitNewRange)
			case "ENTER_GRID -> invalid (TrendExit)":
				sm.ForceState("TEST", GridStateEnterGrid)
			}

			ok := sm.Transition("TEST", tt.event)
			assert.Equal(t, tt.wantOk, ok, "Transition success mismatch")

			if tt.wantOk {
				assert.Equal(t, tt.wantState, sm.GetState("TEST"), "Final state mismatch")
			}
		})
	}
}

// TestGridStateMachine_CanTransition validates CanTransition helper
func TestGridStateMachine_CanTransition(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)

	// Test from IDLE
	assert.True(t, sm.CanTransition("TEST", EventRangeConfirmed))
	assert.False(t, sm.CanTransition("TEST", EventEntryPlaced))
	assert.False(t, sm.CanTransition("TEST", EventTrendExit))

	// Test from TRADING (after forcing state)
	sm.ForceState("TEST", GridStateTrading)
	assert.True(t, sm.CanTransition("TEST", EventTrendExit))
	assert.True(t, sm.CanTransition("TEST", EventEmergencyExit))
	assert.False(t, sm.CanTransition("TEST", EventRangeConfirmed))
}

// TestGridStateMachine_ShouldEnqueuePlacement tests placement gating
func TestGridStateMachine_ShouldEnqueuePlacement(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)

	// IDLE - should NOT enqueue
	assert.False(t, sm.ShouldEnqueuePlacement("TEST"), "IDLE should not enqueue")

	// ENTER_GRID - should enqueue
	sm.ForceState("TEST", GridStateEnterGrid)
	assert.True(t, sm.ShouldEnqueuePlacement("TEST"), "ENTER_GRID should enqueue")

	// TRADING - should enqueue
	sm.ForceState("TEST", GridStateTrading)
	assert.True(t, sm.ShouldEnqueuePlacement("TEST"), "TRADING should enqueue")

	// EXIT_ALL - should NOT enqueue
	sm.ForceState("TEST", GridStateExitAll)
	assert.False(t, sm.ShouldEnqueuePlacement("TEST"), "EXIT_ALL should not enqueue")

	// WAIT_NEW_RANGE - should NOT enqueue
	sm.ForceState("TEST", GridStateWaitNewRange)
	assert.False(t, sm.ShouldEnqueuePlacement("TEST"), "WAIT_NEW_RANGE should not enqueue")
}

// TestGridStateMachine_IsTrading checks trading state detection
func TestGridStateMachine_IsTrading(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)

	// IDLE - not trading
	assert.False(t, sm.IsTrading("TEST"))

	// TRADING - yes
	sm.ForceState("TEST", GridStateTrading)
	assert.True(t, sm.IsTrading("TEST"))

	// EXIT_ALL - not trading
	sm.ForceState("TEST", GridStateExitAll)
	assert.False(t, sm.IsTrading("TEST"))
}

// TestGridStateMachine_ConsecutiveLosses tracks loss streaks
func TestGridStateMachine_ConsecutiveLosses(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)

	// Initially 0
	assert.Equal(t, 0, sm.GetConsecutiveLosses("TEST"))

	// Record 3 losses
	sm.RecordConsecutiveLoss("TEST")
	sm.RecordConsecutiveLoss("TEST")
	sm.RecordConsecutiveLoss("TEST")
	assert.Equal(t, 3, sm.GetConsecutiveLosses("TEST"))

	// Reset
	sm.ResetConsecutiveLosses("TEST")
	assert.Equal(t, 0, sm.GetConsecutiveLosses("TEST"))
}

// TestGridStateMachine_RegridCooldown manages cooldown state
func TestGridStateMachine_RegridCooldown(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)

	// Initially no cooldown
	assert.False(t, sm.IsRegridCooldownActive("TEST"))

	// Activate cooldown
	sm.ActivateRegridCooldown("TEST", 100*time.Millisecond)
	assert.True(t, sm.IsRegridCooldownActive("TEST"))

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)
	assert.False(t, sm.IsRegridCooldownActive("TEST"))

	// Clear manually
	sm.ActivateRegridCooldown("TEST", 1*time.Minute)
	assert.True(t, sm.IsRegridCooldownActive("TEST"))
	sm.ClearRegridCooldown("TEST")
	assert.False(t, sm.IsRegridCooldownActive("TEST"))
}

// TestGridStateMachine_FullLifecycle simulates complete trading lifecycle
func TestGridStateMachine_FullLifecycle(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)
	symbol := "BTCUSD1"

	// 1. Initial: IDLE
	assert.Equal(t, GridStateIdle, sm.GetState(symbol))
	assert.False(t, sm.ShouldEnqueuePlacement(symbol))

	// 2. Range confirmed: IDLE -> ENTER_GRID
	ok := sm.Transition(symbol, EventRangeConfirmed)
	assert.True(t, ok)
	assert.Equal(t, GridStateEnterGrid, sm.GetState(symbol))
	assert.True(t, sm.ShouldEnqueuePlacement(symbol))

	// 3. Entry placed: ENTER_GRID -> TRADING
	ok = sm.Transition(symbol, EventEntryPlaced)
	assert.True(t, ok)
	assert.Equal(t, GridStateTrading, sm.GetState(symbol))
	assert.True(t, sm.IsTrading(symbol))
	assert.True(t, sm.ShouldEnqueuePlacement(symbol))

	// 4. Trend exit: TRADING -> EXIT_ALL
	ok = sm.Transition(symbol, EventTrendExit)
	assert.True(t, ok)
	assert.Equal(t, GridStateExitAll, sm.GetState(symbol))
	assert.False(t, sm.IsTrading(symbol))
	assert.False(t, sm.ShouldEnqueuePlacement(symbol))

	// 5. Positions closed: EXIT_ALL -> WAIT_NEW_RANGE
	ok = sm.Transition(symbol, EventPositionsClosed)
	assert.True(t, ok)
	assert.Equal(t, GridStateWaitNewRange, sm.GetState(symbol))
	assert.False(t, sm.ShouldEnqueuePlacement(symbol))

	// 6. New range ready: WAIT_NEW_RANGE -> ENTER_GRID
	ok = sm.Transition(symbol, EventNewRangeReady)
	assert.True(t, ok)
	assert.Equal(t, GridStateEnterGrid, sm.GetState(symbol))
	assert.True(t, sm.ShouldEnqueuePlacement(symbol))

	// Validate state info
	stateInfo := sm.GetSymbolState(symbol)
	assert.NotNil(t, stateInfo)
	assert.True(t, stateInfo.LastTransition.After(time.Time{}))
}

// TestGridStateMachine_EmergencyExit tests emergency exit path
func TestGridStateMachine_EmergencyExit(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)
	symbol := "BTCUSD1"

	// Setup in TRADING
	sm.ForceState(symbol, GridStateTrading)

	// Emergency exit
	ok := sm.Transition(symbol, EventEmergencyExit)
	assert.True(t, ok)
	assert.Equal(t, GridStateExitAll, sm.GetState(symbol))
}

// TestGridStateMachine_InvalidTransitions tests invalid transitions are rejected
func TestGridStateMachine_InvalidTransitions(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)
	symbol := "BTCUSD1"

	// From IDLE, invalid transitions
	assert.False(t, sm.Transition(symbol, EventEntryPlaced))
	assert.False(t, sm.Transition(symbol, EventTrendExit))
	assert.False(t, sm.Transition(symbol, EventPositionsClosed))

	// From TRADING, invalid transitions
	sm.ForceState(symbol, GridStateTrading)
	assert.False(t, sm.Transition(symbol, EventRangeConfirmed))
	assert.False(t, sm.Transition(symbol, EventEntryPlaced))
	assert.False(t, sm.Transition(symbol, EventNewRangeReady))
}

// TestGridStateMachine_ThreadSafe tests concurrent access
func TestGridStateMachine_ThreadSafe(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)

	// Concurrent reads and writes
	done := make(chan bool, 10)

	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				sm.GetState("TEST")
				sm.CanTransition("TEST", EventRangeConfirmed)
			}
			done <- true
		}()
	}

	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				sm.Transition("TEST", EventRangeConfirmed)
				sm.Transition("TEST", EventEntryPlaced)
				sm.Transition("TEST", EventTrendExit)
				sm.Transition("TEST", EventPositionsClosed)
				sm.Transition("TEST", EventNewRangeReady)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// No panic = success for thread safety
}

// TestValidateStateTransition tests the validation helper
func TestValidateStateTransition(t *testing.T) {
	// Valid transitions
	assert.NoError(t, ValidateStateTransition(GridStateIdle, EventRangeConfirmed, GridStateEnterGrid))
	assert.NoError(t, ValidateStateTransition(GridStateEnterGrid, EventEntryPlaced, GridStateTrading))
	assert.NoError(t, ValidateStateTransition(GridStateTrading, EventTrendExit, GridStateExitAll))
	assert.NoError(t, ValidateStateTransition(GridStateExitAll, EventPositionsClosed, GridStateWaitNewRange))
	assert.NoError(t, ValidateStateTransition(GridStateWaitNewRange, EventNewRangeReady, GridStateEnterGrid))

	// Invalid transitions
	assert.Error(t, ValidateStateTransition(GridStateIdle, EventEntryPlaced, GridStateEnterGrid))
	assert.Error(t, ValidateStateTransition(GridStateTrading, EventRangeConfirmed, GridStateEnterGrid))

	// Wrong target state
	assert.Error(t, ValidateStateTransition(GridStateIdle, EventRangeConfirmed, GridStateTrading))
}

// TestGridState_String tests state string representations
func TestGridState_String(t *testing.T) {
	assert.Equal(t, "IDLE", GridStateIdle.String())
	assert.Equal(t, "ENTER_GRID", GridStateEnterGrid.String())
	assert.Equal(t, "TRADING", GridStateTrading.String())
	assert.Equal(t, "EXIT_ALL", GridStateExitAll.String())
	assert.Equal(t, "WAIT_NEW_RANGE", GridStateWaitNewRange.String())
	assert.Equal(t, "UNKNOWN", GridState(999).String())
}

// TestGridEvent_String tests event string representations
func TestGridEvent_String(t *testing.T) {
	assert.Equal(t, "RANGE_CONFIRMED", EventRangeConfirmed.String())
	assert.Equal(t, "ENTRY_PLACED", EventEntryPlaced.String())
	assert.Equal(t, "TREND_EXIT", EventTrendExit.String())
	assert.Equal(t, "EMERGENCY_EXIT", EventEmergencyExit.String())
	assert.Equal(t, "POSITIONS_CLOSED", EventPositionsClosed.String())
	assert.Equal(t, "NEW_RANGE_READY", EventNewRangeReady.String())
	assert.Equal(t, "UNKNOWN", GridEvent(999).String())
}
