package agentic

import (
	"context"
	"testing"
	"time"

	"aster-bot/internal/realtime"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewRecoveryStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)

	handler := NewRecoveryStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.Equal(t, 5*time.Minute, handler.maxRecoveryTime)
	assert.Equal(t, 2*time.Minute, handler.minCooldownTime)
	assert.Equal(t, 3, handler.maxConsecutiveLosses)
}

func TestAssessLossSeverity(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewRecoveryStateHandler(scoreEngine, logger)

	// Severe: >3% loss
	assert.Equal(t, "severe", handler.assessLossSeverity(-0.035, "stop_loss", 1))

	// Severe: too many consecutive losses
	assert.Equal(t, "severe", handler.assessLossSeverity(-0.01, "stop_loss", 3))

	// Moderate: 2-3% loss
	assert.Equal(t, "moderate", handler.assessLossSeverity(-0.025, "max_loss", 1))

	// Moderate: emergency exit
	assert.Equal(t, "severe", handler.assessLossSeverity(-0.01, "emergency_exit", 1))

	// Minor: <2% loss
	assert.Equal(t, "minor", handler.assessLossSeverity(-0.015, "stop_loss", 1))
	assert.Equal(t, "minor", handler.assessLossSeverity(-0.005, "timeout", 1))
}

func TestCalculateAdjustments(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewRecoveryStateHandler(scoreEngine, logger)

	// Severe loss adjustments
	adj := handler.calculateAdjustments(-0.035, "max_loss", 3)
	assert.True(t, adj.PositionSizeAdj < 0.5)
	assert.True(t, adj.GridSizeMultiplier < 0.8)

	// Moderate loss adjustments
	adj = handler.calculateAdjustments(-0.025, "stop_loss", 2)
	assert.True(t, adj.PositionSizeAdj < 0.9)
	assert.True(t, adj.StopLossAdj > 1.0)

	// Minor loss adjustments (minimal)
	adj = handler.calculateAdjustments(-0.01, "timeout", 1)
	assert.True(t, adj.PositionSizeAdj >= 0.9)
}

func TestIsMarketReadyForReentry(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewRecoveryStateHandler(scoreEngine, logger)

	// Not ready: high volatility
	regime := RegimeSnapshot{
		Regime:     RegimeTrending,
		Confidence: 0.7,
		ADX:        25,
		ATR14:      0.015, // High ATR
	}
	assert.False(t, handler.isMarketReadyForReentry("BTCUSD1", regime))

	// Not ready: volatile regime
	regime.Regime = RegimeVolatile
	regime.ATR14 = 0.005
	assert.False(t, handler.isMarketReadyForReentry("BTCUSD1", regime))

	// Not ready: low confidence
	regime.Regime = RegimeSideways
	regime.Confidence = 0.3
	assert.False(t, handler.isMarketReadyForReentry("BTCUSD1", regime))

	// Not ready: low ADX
	regime.Confidence = 0.7
	regime.ADX = 12
	assert.False(t, handler.isMarketReadyForReentry("BTCUSD1", regime))

	// Ready
	regime.ADX = 25
	assert.True(t, handler.isMarketReadyForReentry("BTCUSD1", regime))
}

func TestRecoveryTransitions(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewRecoveryStateHandler(scoreEngine, logger)

	t.Run("RECOVERY to GRID on minor loss", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.recoveryStart[symbol] = time.Now().Add(-3 * time.Minute) // > 2min cooldown
		handler.exitPnL[symbol] = -0.01
		handler.exitReason[symbol] = "stop_loss"
		handler.adjustments[symbol] = &ParameterAdjustments{
			PositionSizeAdj: 0.9,
		}

		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.8,
			ADX:        30,
			ATR14:      0.005,
		}

		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				NetPnLAfterFees: 0.0,
			},
			-0.01, // Minor loss
			"stop_loss",
			1, // 1 consecutive loss
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeEnterGrid, transition.ToState)
		assert.Equal(t, "recovery_complete", transition.Trigger)
	})

	t.Run("RECOVERY to IDLE on moderate loss", func(t *testing.T) {
		symbol := "ETHUSD1"
		handler.recoveryStart[symbol] = time.Now().Add(-4 * time.Minute) // > 2.5min (half of max)
		handler.exitPnL[symbol] = -0.025
		handler.exitReason[symbol] = "max_loss"
		handler.adjustments[symbol] = &ParameterAdjustments{
			PositionSizeAdj: 0.8,
		}

		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.8,
			ADX:        30,
			ATR14:      0.005,
		}

		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{},
			-0.025, // Moderate loss
			"max_loss",
			2, // 2 consecutive losses
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeIdle, transition.ToState)
		assert.Equal(t, "recovery_to_idle", transition.Trigger)
	})

	t.Run("RECOVERY to IDLE on timeout", func(t *testing.T) {
		symbol := "SOLUSD1"
		handler.recoveryStart[symbol] = time.Now().Add(-6 * time.Minute) // > 5min max
		handler.exitPnL[symbol] = -0.02
		handler.exitReason[symbol] = "stop_loss"
		handler.adjustments[symbol] = &ParameterAdjustments{
			PositionSizeAdj: 0.8,
		}

		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.8,
			ADX:        30,
			ATR14:      0.005,
		}

		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{},
			-0.02,
			"stop_loss",
			1,
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeIdle, transition.ToState)
		assert.Equal(t, "recovery_timeout", transition.Trigger)
	})

	t.Run("Stay in RECOVERY during cooldown", func(t *testing.T) {
		symbol := "AVAXUSD1"
		handler.recoveryStart[symbol] = time.Now().Add(-30 * time.Second) // < 2min cooldown
		handler.exitPnL[symbol] = -0.01
		handler.exitReason[symbol] = "stop_loss"

		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.8,
			ADX:        30,
			ATR14:      0.005,
		}

		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{},
			-0.01,
			"stop_loss",
			1,
		)

		assert.NoError(t, err)
		assert.Nil(t, transition) // Stay in RECOVERY
	})
}

func TestGetAdjustments(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewRecoveryStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	expectedAdj := &ParameterAdjustments{
		PositionSizeAdj: 0.8,
	}
	handler.adjustments[symbol] = expectedAdj

	adj := handler.GetAdjustments(symbol)
	assert.Equal(t, expectedAdj, adj)

	// Non-existent symbol
	assert.Nil(t, handler.GetAdjustments("NONEXISTENT"))
}

func TestGetRecoveryTime(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewRecoveryStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	handler.recoveryStart[symbol] = time.Now().Add(-3 * time.Minute)

	recoveryTime := handler.GetRecoveryTime(symbol)
	assert.True(t, recoveryTime >= 3*time.Minute)

	// Non-existent symbol
	assert.Equal(t, time.Duration(0), handler.GetRecoveryTime("NONEXISTENT"))
}

func TestRecoveryStrategiesIntegration(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewRecoveryStateHandler(scoreEngine, logger)

	t.Run("Parameter adjustments calculation", func(t *testing.T) {
		// Severe loss
		adj := handler.calculateAdjustments(-0.04, "max_loss", 3)
		assert.True(t, adj.PositionSizeAdj <= 0.5)
		assert.True(t, adj.GridSizeMultiplier <= 0.7)

		// Moderate loss
		adj = handler.calculateAdjustments(-0.02, "stop_loss", 2)
		assert.True(t, adj.PositionSizeAdj >= 0.7 && adj.PositionSizeAdj <= 0.9)

		// Minor loss
		adj = handler.calculateAdjustments(-0.01, "timeout", 1)
		assert.True(t, adj.PositionSizeAdj >= 0.9)
	})
}
