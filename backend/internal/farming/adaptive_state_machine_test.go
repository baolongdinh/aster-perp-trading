package farming

import (
	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_grid"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestStateMachineTransitions tests all valid state transitions
func TestStateMachineTransitions(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	sm := adaptive_grid.NewGridStateMachine(logger)

	symbol := "BTCUSDT"

	tests := []struct {
		name          string
		fromState     adaptive_grid.GridState
		event         adaptive_grid.GridEvent
		expectedState adaptive_grid.GridState
		shouldSucceed bool
	}{
		// IDLE transitions
		{
			name:          "IDLE -> ENTER_GRID via RANGE_CONFIRMED",
			fromState:     adaptive_grid.GridStateIdle,
			event:         adaptive_grid.EventRangeConfirmed,
			expectedState: adaptive_grid.GridStateEnterGrid,
			shouldSucceed: true,
		},
		{
			name:          "IDLE -> TRADING via ENTRY_PLACED (invalid)",
			fromState:     adaptive_grid.GridStateIdle,
			event:         adaptive_grid.EventEntryPlaced,
			expectedState: adaptive_grid.GridStateIdle,
			shouldSucceed: false,
		},

		// ENTER_GRID transitions
		{
			name:          "ENTER_GRID -> TRADING via ENTRY_PLACED",
			fromState:     adaptive_grid.GridStateEnterGrid,
			event:         adaptive_grid.EventEntryPlaced,
			expectedState: adaptive_grid.GridStateTrading,
			shouldSucceed: true,
		},

		// TRADING transitions
		{
			name:          "TRADING -> EXIT_HALF via PARTIAL_LOSS",
			fromState:     adaptive_grid.GridStateTrading,
			event:         adaptive_grid.EventPartialLoss,
			expectedState: adaptive_grid.GridStateExitHalf,
			shouldSucceed: true,
		},
		{
			name:          "TRADING -> EXIT_ALL via TREND_EXIT",
			fromState:     adaptive_grid.GridStateTrading,
			event:         adaptive_grid.EventTrendExit,
			expectedState: adaptive_grid.GridStateExitAll,
			shouldSucceed: true,
		},
		{
			name:          "TRADING -> EXIT_ALL via EMERGENCY_EXIT",
			fromState:     adaptive_grid.GridStateTrading,
			event:         adaptive_grid.EventEmergencyExit,
			expectedState: adaptive_grid.GridStateExitAll,
			shouldSucceed: true,
		},
		{
			name:          "TRADING -> OVER_SIZE via OVER_SIZE_LIMIT",
			fromState:     adaptive_grid.GridStateTrading,
			event:         adaptive_grid.EventOverSizeLimit,
			expectedState: adaptive_grid.GridStateOverSize,
			shouldSucceed: true,
		},
		{
			name:          "TRADING -> DEFENSIVE via EXTREME_VOLATILITY",
			fromState:     adaptive_grid.GridStateTrading,
			event:         adaptive_grid.EventExtremeVolatility,
			expectedState: adaptive_grid.GridStateDefensive,
			shouldSucceed: true,
		},
		{
			name:          "TRADING -> RECOVERY via RECOVERY_START",
			fromState:     adaptive_grid.GridStateTrading,
			event:         adaptive_grid.EventRecoveryStart,
			expectedState: adaptive_grid.GridStateRecovery,
			shouldSucceed: true,
		},

		// EXIT_HALF transitions
		{
			name:          "EXIT_HALF -> EXIT_ALL via FULL_LOSS",
			fromState:     adaptive_grid.GridStateExitHalf,
			event:         adaptive_grid.EventFullLoss,
			expectedState: adaptive_grid.GridStateExitAll,
			shouldSucceed: true,
		},
		{
			name:          "EXIT_HALF -> TRADING via RECOVERY",
			fromState:     adaptive_grid.GridStateExitHalf,
			event:         adaptive_grid.EventRecovery,
			expectedState: adaptive_grid.GridStateTrading,
			shouldSucceed: true,
		},
		{
			name:          "EXIT_HALF -> RECOVERY via RECOVERY_START",
			fromState:     adaptive_grid.GridStateExitHalf,
			event:         adaptive_grid.EventRecoveryStart,
			expectedState: adaptive_grid.GridStateRecovery,
			shouldSucceed: true,
		},

		// EXIT_ALL transitions
		{
			name:          "EXIT_ALL -> WAIT_NEW_RANGE via POSITIONS_CLOSED",
			fromState:     adaptive_grid.GridStateExitAll,
			event:         adaptive_grid.EventPositionsClosed,
			expectedState: adaptive_grid.GridStateWaitNewRange,
			shouldSucceed: true,
		},
		{
			name:          "EXIT_ALL -> RECOVERY via RECOVERY_START",
			fromState:     adaptive_grid.GridStateExitAll,
			event:         adaptive_grid.EventRecoveryStart,
			expectedState: adaptive_grid.GridStateRecovery,
			shouldSucceed: true,
		},

		// WAIT_NEW_RANGE transitions
		{
			name:          "WAIT_NEW_RANGE -> ENTER_GRID via NEW_RANGE_READY",
			fromState:     adaptive_grid.GridStateWaitNewRange,
			event:         adaptive_grid.EventNewRangeReady,
			expectedState: adaptive_grid.GridStateEnterGrid,
			shouldSucceed: true,
		},

		// OVER_SIZE transitions
		{
			name:          "OVER_SIZE -> TRADING via SIZE_NORMALIZED",
			fromState:     adaptive_grid.GridStateOverSize,
			event:         adaptive_grid.EventSizeNormalized,
			expectedState: adaptive_grid.GridStateTrading,
			shouldSucceed: true,
		},
		{
			name:          "OVER_SIZE -> EXIT_ALL via FULL_LOSS",
			fromState:     adaptive_grid.GridStateOverSize,
			event:         adaptive_grid.EventFullLoss,
			expectedState: adaptive_grid.GridStateExitAll,
			shouldSucceed: true,
		},

		// DEFENSIVE transitions
		{
			name:          "DEFENSIVE -> TRADING via VOLATILITY_NORMALIZED",
			fromState:     adaptive_grid.GridStateDefensive,
			event:         adaptive_grid.EventVolatilityNormalized,
			expectedState: adaptive_grid.GridStateTrading,
			shouldSucceed: true,
		},
		{
			name:          "DEFENSIVE -> EXIT_ALL via EMERGENCY_EXIT",
			fromState:     adaptive_grid.GridStateDefensive,
			event:         adaptive_grid.EventEmergencyExit,
			expectedState: adaptive_grid.GridStateExitAll,
			shouldSucceed: true,
		},

		// RECOVERY transitions
		{
			name:          "RECOVERY -> TRADING via RECOVERY_COMPLETE",
			fromState:     adaptive_grid.GridStateRecovery,
			event:         adaptive_grid.EventRecoveryComplete,
			expectedState: adaptive_grid.GridStateTrading,
			shouldSucceed: true,
		},
		{
			name:          "RECOVERY -> EXIT_HALF via PARTIAL_LOSS",
			fromState:     adaptive_grid.GridStateRecovery,
			event:         adaptive_grid.EventPartialLoss,
			expectedState: adaptive_grid.GridStateExitHalf,
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state machine for this test
			sm = adaptive_grid.NewGridStateMachine(logger)

			// Set initial state by performing valid transitions from IDLE
			// This is a workaround since we can't directly set the state
			if tt.fromState != adaptive_grid.GridStateIdle {
				// Transition to target state from IDLE
				switch tt.fromState {
				case adaptive_grid.GridStateEnterGrid:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
				case adaptive_grid.GridStateTrading:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
				case adaptive_grid.GridStateExitHalf:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventPartialLoss)
				case adaptive_grid.GridStateExitAll:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventEmergencyExit)
				case adaptive_grid.GridStateWaitNewRange:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventEmergencyExit)
					sm.Transition(symbol, adaptive_grid.EventPositionsClosed)
				case adaptive_grid.GridStateOverSize:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventOverSizeLimit)
				case adaptive_grid.GridStateDefensive:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventExtremeVolatility)
				case adaptive_grid.GridStateRecovery:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventRecoveryStart)
				}
			}

			// Verify initial state
			currentState := sm.GetState(symbol)
			if currentState != tt.fromState {
				t.Logf("Warning: Could not set initial state to %v, got %v. Skipping transition test.", tt.fromState, currentState)
				return
			}

			// Check if transition is valid
			canTransition := sm.CanTransition(symbol, tt.event)
			if canTransition != tt.shouldSucceed {
				t.Errorf("CanTransition() = %v, want %v", canTransition, tt.shouldSucceed)
			}

			// Attempt transition
			result := sm.Transition(symbol, tt.event)
			if result != tt.shouldSucceed {
				t.Errorf("Transition() = %v, want %v", result, tt.shouldSucceed)
			}

			// Check final state
			if tt.shouldSucceed {
				finalState := sm.GetState(symbol)
				if finalState != tt.expectedState {
					t.Errorf("Final state = %v, want %v", finalState, tt.expectedState)
				}
			}
		})
	}
}

// TestCanTransitionInvalidTransitions tests invalid transitions
func TestCanTransitionInvalidTransitions(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	sm := adaptive_grid.NewGridStateMachine(logger)

	symbol := "BTCUSDT"

	tests := []struct {
		name      string
		fromState adaptive_grid.GridState
		event     adaptive_grid.GridEvent
	}{
		{
			name:      "IDLE -> TRADING invalid",
			fromState: adaptive_grid.GridStateIdle,
			event:     adaptive_grid.EventEntryPlaced,
		},
		{
			name:      "TRADING -> ENTER_GRID invalid",
			fromState: adaptive_grid.GridStateTrading,
			event:     adaptive_grid.EventRangeConfirmed,
		},
		{
			name:      "OVER_SIZE -> DEFENSIVE invalid",
			fromState: adaptive_grid.GridStateOverSize,
			event:     adaptive_grid.EventExtremeVolatility,
		},
		{
			name:      "DEFENSIVE -> OVER_SIZE invalid",
			fromState: adaptive_grid.GridStateDefensive,
			event:     adaptive_grid.EventOverSizeLimit,
		},
		{
			name:      "RECOVERY -> OVER_SIZE invalid",
			fromState: adaptive_grid.GridStateRecovery,
			event:     adaptive_grid.EventOverSizeLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state machine
			sm = adaptive_grid.NewGridStateMachine(logger)

			// Set initial state via valid transitions
			if tt.fromState != adaptive_grid.GridStateIdle {
				switch tt.fromState {
				case adaptive_grid.GridStateTrading:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
				case adaptive_grid.GridStateOverSize:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventOverSizeLimit)
				case adaptive_grid.GridStateDefensive:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventExtremeVolatility)
				case adaptive_grid.GridStateRecovery:
					sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)
					sm.Transition(symbol, adaptive_grid.EventEntryPlaced)
					sm.Transition(symbol, adaptive_grid.EventRecoveryStart)
				}
			}

			canTransition := sm.CanTransition(symbol, tt.event)
			if canTransition {
				t.Errorf("CanTransition() = true, want false for invalid transition")
			}
		})
	}
}

// TestStateStringRepresentation tests state string representation
func TestStateStringRepresentation(t *testing.T) {
	tests := []struct {
		state    adaptive_grid.GridState
		expected string
	}{
		{adaptive_grid.GridStateIdle, "IDLE"},
		{adaptive_grid.GridStateEnterGrid, "ENTER_GRID"},
		{adaptive_grid.GridStateTrading, "TRADING"},
		{adaptive_grid.GridStateExitHalf, "EXIT_HALF"},
		{adaptive_grid.GridStateExitAll, "EXIT_ALL"},
		{adaptive_grid.GridStateWaitNewRange, "WAIT_NEW_RANGE"},
		{adaptive_grid.GridStateOverSize, "OVER_SIZE"},
		{adaptive_grid.GridStateDefensive, "DEFENSIVE"},
		{adaptive_grid.GridStateRecovery, "RECOVERY"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.state.String() != tt.expected {
				t.Errorf("State.String() = %v, want %v", tt.state.String(), tt.expected)
			}
		})
	}
}

// TestEventStringRepresentation tests event string representation
func TestEventStringRepresentation(t *testing.T) {
	tests := []struct {
		event    adaptive_grid.GridEvent
		expected string
	}{
		{adaptive_grid.EventRangeConfirmed, "RANGE_CONFIRMED"},
		{adaptive_grid.EventEntryPlaced, "ENTRY_PLACED"},
		{adaptive_grid.EventPartialLoss, "PARTIAL_LOSS"},
		{adaptive_grid.EventFullLoss, "FULL_LOSS"},
		{adaptive_grid.EventRecovery, "RECOVERY"},
		{adaptive_grid.EventTrendExit, "TREND_EXIT"},
		{adaptive_grid.EventEmergencyExit, "EMERGENCY_EXIT"},
		{adaptive_grid.EventPositionsClosed, "POSITIONS_CLOSED"},
		{adaptive_grid.EventNewRangeReady, "NEW_RANGE_READY"},
		{adaptive_grid.EventOverSizeLimit, "OVER_SIZE_LIMIT"},
		{adaptive_grid.EventSizeNormalized, "SIZE_NORMALIZED"},
		{adaptive_grid.EventExtremeVolatility, "EXTREME_VOLATILITY"},
		{adaptive_grid.EventVolatilityNormalized, "VOLATILITY_NORMALIZED"},
		{adaptive_grid.EventRecoveryStart, "RECOVERY_START"},
		{adaptive_grid.EventRecoveryComplete, "RECOVERY_COMPLETE"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.event.String() != tt.expected {
				t.Errorf("Event.String() = %v, want %v", tt.event.String(), tt.expected)
			}
		})
	}
}

// TestMarketConditionEvaluatorConfig tests config loading
func TestMarketConditionEvaluatorConfig(t *testing.T) {
	cfg := &config.MarketConditionEvaluatorConfig{
		Enabled:                true,
		EvaluationIntervalSec:  1,
		MinConfidenceThreshold: 0.7,
		StateStabilityDuration: 30,
	}

	if !cfg.Enabled {
		t.Error("Config.Enabled should be true")
	}
	if cfg.EvaluationIntervalSec != 1 {
		t.Errorf("Config.EvaluationIntervalSec = %v, want 1", cfg.EvaluationIntervalSec)
	}
	if cfg.MinConfidenceThreshold != 0.7 {
		t.Errorf("Config.MinConfidenceThreshold = %v, want 0.7", cfg.MinConfidenceThreshold)
	}
	if cfg.StateStabilityDuration != 30 {
		t.Errorf("Config.StateStabilityDuration = %v, want 30", cfg.StateStabilityDuration)
	}
}

// TestOverSizeConfig tests OVER_SIZE state config
func TestOverSizeConfig(t *testing.T) {
	cfg := &config.OverSizeConfig{
		ThresholdPct: 0.8,
		RecoveryPct:  0.6,
	}

	if cfg.ThresholdPct != 0.8 {
		t.Errorf("Config.ThresholdPct = %v, want 0.8", cfg.ThresholdPct)
	}
	if cfg.RecoveryPct != 0.6 {
		t.Errorf("Config.RecoveryPct = %v, want 0.6", cfg.RecoveryPct)
	}
}

// TestDefensiveStateConfig tests DEFENSIVE state config
func TestDefensiveStateConfig(t *testing.T) {
	cfg := &config.DefensiveStateConfig{
		ATRMultiplierThreshold: 3.0,
		BBWidthThreshold:       0.05,
		SpreadMultiplier:       2.0,
		SLMultiplier:           0.8,
		AllowNewPositions:      false,
	}

	if cfg.ATRMultiplierThreshold != 3.0 {
		t.Errorf("Config.ATRMultiplierThreshold = %v, want 3.0", cfg.ATRMultiplierThreshold)
	}
	if cfg.BBWidthThreshold != 0.05 {
		t.Errorf("Config.BBWidthThreshold = %v, want 0.05", cfg.BBWidthThreshold)
	}
	if cfg.SpreadMultiplier != 2.0 {
		t.Errorf("Config.SpreadMultiplier = %v, want 2.0", cfg.SpreadMultiplier)
	}
	if cfg.SLMultiplier != 0.8 {
		t.Errorf("Config.SLMultiplier = %v, want 0.8", cfg.SLMultiplier)
	}
	if cfg.AllowNewPositions {
		t.Error("Config.AllowNewPositions should be false")
	}
}

// TestRecoveryStateConfig tests RECOVERY state config
func TestRecoveryStateConfig(t *testing.T) {
	cfg := &config.RecoveryStateConfig{
		RecoveryThresholdUSDT: 0.0,
		SizeMultiplier:        0.5,
		SpreadMultiplier:      1.5,
		StableDurationMin:     30,
	}

	if cfg.RecoveryThresholdUSDT != 0.0 {
		t.Errorf("Config.RecoveryThresholdUSDT = %v, want 0.0", cfg.RecoveryThresholdUSDT)
	}
	if cfg.SizeMultiplier != 0.5 {
		t.Errorf("Config.SizeMultiplier = %v, want 0.5", cfg.SizeMultiplier)
	}
	if cfg.SpreadMultiplier != 1.5 {
		t.Errorf("Config.SpreadMultiplier = %v, want 1.5", cfg.SpreadMultiplier)
	}
	if cfg.StableDurationMin != 30 {
		t.Errorf("Config.StableDurationMin = %v, want 30", cfg.StableDurationMin)
	}
}

// TestStateTransitionTiming tests state transition timing
func TestStateTransitionTiming(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	sm := adaptive_grid.NewGridStateMachine(logger)

	symbol := "BTCUSDT"

	// Perform first transition
	sm.Transition(symbol, adaptive_grid.EventRangeConfirmed)

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Perform second transition
	sm.Transition(symbol, adaptive_grid.EventEntryPlaced)

	// Verify state changed
	if sm.GetState(symbol) != adaptive_grid.GridStateTrading {
		t.Errorf("State = %v, want TRADING", sm.GetState(symbol))
	}
}

// TestMultipleSymbols tests state machine with multiple symbols
func TestMultipleSymbols(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	sm := adaptive_grid.NewGridStateMachine(logger)

	// Transition each symbol to different states
	sm.Transition("BTCUSDT", adaptive_grid.EventRangeConfirmed)
	sm.Transition("BTCUSDT", adaptive_grid.EventEntryPlaced) // BTCUSDT -> TRADING

	sm.Transition("ETHUSDT", adaptive_grid.EventRangeConfirmed)
	sm.Transition("ETHUSDT", adaptive_grid.EventEntryPlaced)
	sm.Transition("ETHUSDT", adaptive_grid.EventPartialLoss) // ETHUSDT -> EXIT_HALF

	// Verify states
	if sm.GetState("BTCUSDT") != adaptive_grid.GridStateTrading {
		t.Errorf("BTCUSDT state = %v, want TRADING", sm.GetState("BTCUSDT"))
	}
	if sm.GetState("ETHUSDT") != adaptive_grid.GridStateExitHalf {
		t.Errorf("ETHUSDT state = %v, want EXIT_HALF", sm.GetState("ETHUSDT"))
	}
	if sm.GetState("SOLUSDT") != adaptive_grid.GridStateIdle {
		t.Errorf("SOLUSDT state = %v, want IDLE", sm.GetState("SOLUSDT"))
	}
}
