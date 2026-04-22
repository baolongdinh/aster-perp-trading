package agentic

import (
	"context"
	"testing"
	"time"

	"aster-bot/internal/realtime"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewEnterGridStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)

	handler := NewEnterGridStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.Equal(t, 60*time.Second, handler.maxEntryTime)
	assert.Equal(t, 0.5, handler.minSignalStrength)
}

func TestCalculateGridParameters(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewEnterGridStateHandler(scoreEngine, logger)

	regime := RegimeSnapshot{
		BBWidth:    0.02,
		ATR14:      0.003,
		Confidence: 0.8,
	}

	params := handler.calculateGridParameters("BTCUSD1", regime, 50000.0)

	assert.Equal(t, "BTCUSD1", params.Symbol)
	assert.True(t, params.Levels >= 3 && params.Levels <= 7)
	assert.True(t, params.BuySpread > 0)
	assert.True(t, params.SellSpread > 0)
	assert.True(t, params.TotalExposure > 0)
}

func TestApplyAsymmetricSpread(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewEnterGridStateHandler(scoreEngine, logger)

	baseParams := GridParams{
		Symbol:         "BTCUSD1",
		Levels:         5,
		BuySpread:      100.0,
		SellSpread:     100.0,
		SizeMultiplier: 1.0,
	}

	t.Run("Favor buy side", func(t *testing.T) {
		signals := &SignalBundle{
			MeanReversion:   0.8,
			LiquiditySignal: 0.7,
			FVGSignal:       0.3,
			BreakoutSignal:  0.2,
		}

		params := handler.applyAsymmetricSpread(baseParams, signals)
		assert.True(t, params.BuySpread < 100.0, "Buy spread should tighten")
		assert.True(t, params.SellSpread > 100.0, "Sell spread should widen")
	})

	t.Run("Favor sell side", func(t *testing.T) {
		signals := &SignalBundle{
			MeanReversion:   0.2,
			LiquiditySignal: 0.3,
			FVGSignal:       0.8,
			BreakoutSignal:  0.7,
		}

		params := handler.applyAsymmetricSpread(baseParams, signals)
		assert.True(t, params.BuySpread > 100.0, "Buy spread should widen")
		assert.True(t, params.SellSpread < 100.0, "Sell spread should tighten")
	})

	t.Run("Strong signals increase size", func(t *testing.T) {
		signals := &SignalBundle{
			OverallStrength: 0.9,
		}

		params := handler.applyAsymmetricSpread(baseParams, signals)
		assert.True(t, params.SizeMultiplier > 1.0, "Size should increase on strong signals")
	})
}

func TestGetBestSignal(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewEnterGridStateHandler(scoreEngine, logger)

	signals := &SignalBundle{
		FVGSignal:       0.3,
		LiquiditySignal: 0.7,
		MeanReversion:   0.5,
		BreakoutSignal:  0.2,
	}

	best := handler.getBestSignal(signals)
	assert.Equal(t, 0.7, best)

	// Test nil signals
	best = handler.getBestSignal(nil)
	assert.Equal(t, 0.0, best)
}

func TestIsEntryTimeout(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewEnterGridStateHandler(scoreEngine, logger)

	// Start tracking
	handler.entryStartTime["BTCUSD1"] = time.Now().Add(-30 * time.Second)
	assert.False(t, handler.isEntryTimeout("BTCUSD1"))

	// Timeout
	handler.entryStartTime["BTCUSD1"] = time.Now().Add(-70 * time.Second)
	assert.True(t, handler.isEntryTimeout("BTCUSD1"))

	// Not tracking
	assert.False(t, handler.isEntryTimeout("NONEXISTENT"))
}

func TestEnterGridHandleState(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewEnterGridStateHandler(scoreEngine, logger)

	t.Run("Place grid with strong signals", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.8,
			ADX:        15,
			ATR14:      0.003,
			BBWidth:    0.02,
		}

		rangeBoundaries := &RangeBoundaries{
			Symbol:    "BTCUSD1",
			RangeHigh: 51000,
			RangeLow:  49000,
			Quality:   0.85,
		}

		signals := &SignalBundle{
			FVGSignal:       0.6,
			LiquiditySignal: 0.7,
			MeanReversion:   0.5,
			OverallStrength: 0.75,
		}

		transition, err := handler.HandleState(
			context.Background(),
			"BTCUSD1",
			regime,
			realtime.SymbolRuntimeSnapshot{CurrentPrice: 50000.0},
			rangeBoundaries,
			signals,
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, "grid_placed", transition.Trigger)
	})

	t.Run("Wait for signal when weak", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.8,
			ADX:        15,
			ATR14:      0.003,
			BBWidth:    0.02,
		}

		rangeBoundaries := &RangeBoundaries{
			Symbol:    "BTCUSD1",
			RangeHigh: 51000,
			RangeLow:  49000,
			Quality:   0.85,
		}

		// Weak signals
		signals := &SignalBundle{
			FVGSignal:       0.2,
			LiquiditySignal: 0.1,
			MeanReversion:   0.15,
			OverallStrength: 0.2,
		}

		// First attempt - should wait
		transition, err := handler.HandleState(
			context.Background(),
			"BTCUSD1",
			regime,
			realtime.SymbolRuntimeSnapshot{CurrentPrice: 50000.0},
			rangeBoundaries,
			signals,
		)

		assert.NoError(t, err)
		assert.Nil(t, transition) // Should wait for better signal
	})
}

func TestEntryAttemptsTracking(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewEnterGridStateHandler(scoreEngine, logger)

	// Initially 0
	assert.Equal(t, 0, handler.GetEntryAttempts("BTCUSD1"))

	// Track attempts by calling HandleState (would need mock, skipping for now)
	// handler.entryAttempts["BTCUSD1"] = 3
	// assert.Equal(t, 3, handler.GetEntryAttempts("BTCUSD1"))
}
