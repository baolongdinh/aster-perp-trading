package agentic

import (
	"context"
	"testing"

	"aster-bot/internal/realtime"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewTradingGridStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)

	handler := NewTradingGridStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.Equal(t, -0.03, handler.maxGridLoss)
	assert.Equal(t, 0.05, handler.maxPositionSize)
}

func TestCalculateSignalEntropy(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTradingGridStateHandler(scoreEngine, logger)

	t.Run("Low entropy (signals agree)", func(t *testing.T) {
		signals := &SignalBundle{
			FVGSignal:       0.8,
			LiquiditySignal: 0.75,
			MeanReversion:   0.78,
			BreakoutSignal:  0.72,
		}

		entropy := handler.calculateSignalEntropy(signals)
		assert.True(t, entropy < 0.3, "Should have low entropy when signals agree")
	})

	t.Run("High entropy (signals disagree)", func(t *testing.T) {
		signals := &SignalBundle{
			FVGSignal:       0.2,
			LiquiditySignal: 0.8,
			MeanReversion:   0.3,
			BreakoutSignal:  0.7,
		}

		entropy := handler.calculateSignalEntropy(signals)
		assert.True(t, entropy > 0.5, "Should have high entropy when signals disagree")
	})

	t.Run("Nil signals", func(t *testing.T) {
		entropy := handler.calculateSignalEntropy(nil)
		assert.Equal(t, 0.5, entropy)
	})
}

func TestShouldRebalance(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTradingGridStateHandler(scoreEngine, logger)

	status := &GridStatus{
		FilledLevels: 5,
		TotalLevels:  10,
	}

	snapshot := realtime.SymbolRuntimeSnapshot{
		InventoryNotional: 100,
		MakerFillRatio:    0.4,
	}

	assert.True(t, handler.shouldRebalance("BTCUSD1", status, snapshot))

	snapshot.MakerFillRatio = 0.8
	assert.False(t, handler.shouldRebalance("BTCUSD1", status, snapshot))
}

func TestTradingGridTransitions(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTradingGridStateHandler(scoreEngine, logger)

	t.Run("TRADING_GRID to TRENDING on trend emergence", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.9,
			ADX:        45,
			ATR14:      0.008,
			BBWidth:    0.09,
		}

		transition, err := handler.HandleState(
			context.Background(),
			"BTCUSD1",
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:     50000.0,
				PositionNotional: 0.03,
			},
			nil,
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeTrending, transition.ToState)
		assert.Equal(t, "trend_emergence", transition.Trigger)
	})

	t.Run("TRADING_GRID to DEFENSIVE on max loss", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        15,
			ATR14:      0.003,
		}

		transition, err := handler.HandleState(
			context.Background(),
			"BTCUSD1",
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:     50000.0,
				PositionNotional: 0.03,
				UnrealizedPnL:    -0.04, // -4%
			},
			nil,
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeDefensive, transition.ToState)
		assert.Equal(t, "max_loss_reached", transition.Trigger)
	})

	t.Run("TRADING_GRID to DEFENSIVE on position size limit", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        15,
			ATR14:      0.003,
		}

		// Set position size above limit
		positionSize := 0.06 // 6%

		transition, err := handler.HandleState(
			context.Background(),
			"BTCUSD1",
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:     50000.0,
				PositionNotional: positionSize,
			},
			nil,
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeOverSize, transition.ToState)
		assert.Equal(t, "position_size_limit", transition.Trigger)
	})
}

func TestGridStatus(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTradingGridStateHandler(scoreEngine, logger)

	status := handler.calculateGridStatus("BTCUSD1", realtime.SymbolRuntimeSnapshot{
		UnrealizedPnL:     0.02,
		RealizedPnL:       0.05,
		PositionAgeSec:    120,
		InventoryNotional: 500,
		MakerFillRatio:    0.9,
	}, nil)

	assert.NotNil(t, status)
	assert.Equal(t, "BTCUSD1", status.Symbol)
	assert.Equal(t, 0.02, status.UnrealizedPnL)
	assert.Equal(t, 500.0, status.InventoryNotional)
}

func TestTradingGridManagementIntegration(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTradingGridStateHandler(scoreEngine, logger)

	t.Run("Signal blending adjusts intensity", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.8,
			ADX:        18,
			ATR14:      0.003,
		}

		lowEntropySignals := &SignalBundle{
			FVGSignal:       0.8,
			LiquiditySignal: 0.75,
			MeanReversion:   0.78,
			BreakoutSignal:  0.72,
			OverallStrength: 0.75,
		}

		// Should not transition (stay in grid)
		transition, err := handler.HandleState(
			context.Background(),
			"BTCUSD1",
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:     50000.0,
				PositionNotional: 0.02,
			},
			lowEntropySignals,
		)

		assert.NoError(t, err)
		assert.Nil(t, transition) // Should stay in GRID
	})
}
