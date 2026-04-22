package agentic

import (
	"context"
	"testing"
	"time"

	"aster-bot/internal/realtime"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewDefensiveStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)

	handler := NewDefensiveStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.Equal(t, 0.005, handler.breakevenThreshold)
	assert.Equal(t, 0.01, handler.exitHalfPnL)
	assert.Equal(t, 0.02, handler.exitAllPnL)
}

func TestIsRecoveryOpportunity(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewDefensiveStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	handler.entryPrice[symbol] = 50000

	// Recovery opportunity
	assert.True(t, handler.isRecoveryOpportunity(symbol, 50500, 0.02))

	// Not recovery
	assert.False(t, handler.isRecoveryOpportunity(symbol, 49500, -0.01))
}

func TestDefensiveTransitions(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewDefensiveStateHandler(scoreEngine, logger)

	t.Run("DEFENSIVE to IDLE on EXIT_ALL", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.entryPrice[symbol] = 50000
		handler.exitTime[symbol] = time.Now()
		handler.exitStage[symbol] = ExitStageHalf
		handler.breakevenHit[symbol] = true

		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        18,
			ATR14:      0.003,
		}

		// PnL at exit_all threshold
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:  51000,
				PositionSize:  0.1,
				UnrealizedPnL: 0.025,
			},
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeIdle, transition.ToState)
		assert.Equal(t, "exit_all", transition.Trigger)
	})

	t.Run("DEFENSIVE to TRENDING on recovery", func(t *testing.T) {
		symbol := "ETHUSD1"
		handler.entryPrice[symbol] = 3000
		handler.exitTime[symbol] = time.Now()
		handler.exitStage[symbol] = ExitStageHalf
		handler.breakevenHit[symbol] = true

		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.8,
			ADX:        35,
			ATR14:      0.006,
		}

		// Recovery opportunity
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:  3100,
				PositionSize:  0.05,
				UnrealizedPnL: 0.03,
			},
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeTrending, transition.ToState)
		assert.Equal(t, "recovery", transition.Trigger)
	})

	t.Run("DEFENSIVE to IDLE on emergency exit", func(t *testing.T) {
		symbol := "SOLUSD1"
		handler.entryPrice[symbol] = 100
		handler.exitTime[symbol] = time.Now()
		handler.exitStage[symbol] = ExitStageBreakeven
		handler.breakevenHit[symbol] = true

		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.7,
			ADX:        30,
			ATR14:      0.005,
		}

		// Large loss triggers emergency exit
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:  98,
				PositionSize:  0.1,
				UnrealizedPnL: -0.03,
			},
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeRecovery, transition.ToState)
		assert.Equal(t, "emergency_exit", transition.Trigger)
	})

	t.Run("DEFENSIVE to IDLE on time limit", func(t *testing.T) {
		symbol := "AVAXUSD1"
		handler.entryPrice[symbol] = 30
		handler.exitTime[symbol] = time.Now().Add(-35 * time.Minute) // > 30 min
		handler.exitStage[symbol] = ExitStageHalf
		handler.breakevenHit[symbol] = true

		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.6,
			ADX:        18,
			ATR14:      0.004,
		}

		// PnL below exit threshold, but time limit reached
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:  30,
				PositionSize:  0.1,
				UnrealizedPnL: 0.005,
			},
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeIdle, transition.ToState)
		assert.Equal(t, "time_limit", transition.Trigger)
	})
}

func TestDefensiveStateProgression(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewDefensiveStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	handler.entryPrice[symbol] = 50000
	handler.exitTime[symbol] = time.Now()
	handler.exitStage[symbol] = ExitStageInitial
	handler.breakevenHit[symbol] = false

	regime := RegimeSnapshot{
		Regime:     RegimeSideways,
		Confidence: 0.7,
		ADX:        18,
		ATR14:      0.003,
	}

	t.Run("Breakeven hit", func(t *testing.T) {
		// First, trigger breakeven
		_, _ = handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:  50250,
				PositionSize:  0.1,
				UnrealizedPnL: 0.006,
			},
		)

		assert.True(t, handler.IsBreakevenHit(symbol))
		assert.Equal(t, ExitStageBreakeven, handler.GetExitStage(symbol))
	})

	t.Run("Exit half executed", func(t *testing.T) {
		// Then, trigger exit_half
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:  50500,
				PositionSize:  0.1,
				UnrealizedPnL: 0.012,
			},
		)

		assert.NoError(t, err)
		// Should not return transition here, just update stage
		assert.Nil(t, transition)
		assert.Equal(t, ExitStageHalf, handler.GetExitStage(symbol))
	})
}

func TestGetExitTime(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewDefensiveStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	handler.exitTime[symbol] = time.Now().Add(-10 * time.Minute)

	exitTime := handler.GetExitTime(symbol)
	assert.True(t, exitTime >= 10*time.Minute)

	// Non-existent symbol
	assert.Equal(t, time.Duration(0), handler.GetExitTime("NONEXISTENT"))
}

func TestDefensiveIntegration(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewDefensiveStateHandler(scoreEngine, logger)

	t.Run("Full exit lifecycle", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.entryPrice[symbol] = 50000
		handler.exitTime[symbol] = time.Now()
		handler.exitStage[symbol] = ExitStageInitial
		handler.breakevenHit[symbol] = false

		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        18,
			ATR14:      0.003,
		}

		// Stage 1: Initial → Breakeven
		_, _ = handler.HandleState(context.Background(), symbol, regime, realtime.SymbolRuntimeSnapshot{CurrentPrice: 50250, PositionSize: 0.1, UnrealizedPnL: 0.006})
		assert.True(t, handler.IsBreakevenHit(symbol))

		// Stage 2: Breakeven → Half
		_, _ = handler.HandleState(context.Background(), symbol, regime, realtime.SymbolRuntimeSnapshot{CurrentPrice: 50500, PositionSize: 0.1, UnrealizedPnL: 0.012})
		assert.Equal(t, ExitStageHalf, handler.GetExitStage(symbol))

		// Stage 3: Half → All (exit to IDLE)
		transition, err := handler.HandleState(context.Background(), symbol, regime, realtime.SymbolRuntimeSnapshot{CurrentPrice: 51000, PositionSize: 0.05, UnrealizedPnL: 0.025})
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeIdle, transition.ToState)
		assert.Equal(t, "exit_all", transition.Trigger)
	})
}
