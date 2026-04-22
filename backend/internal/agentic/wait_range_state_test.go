package agentic

import (
	"context"
	"testing"
	"time"

	"aster-bot/internal/realtime"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewWaitRangeStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)

	handler := NewWaitRangeStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.Equal(t, 120*time.Second, handler.maxWaitTime)
}

func TestDetectRange(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewWaitRangeStateHandler(scoreEngine, logger)

	// Test with BB width
	regime := RegimeSnapshot{
		BBWidth: 0.02,
		ATR14:   0.003,
	}

	high, low := handler.detectRange("BTCUSD1", 50000.0, regime)
	assert.True(t, high > 50000)
	assert.True(t, low < 50000)
	assert.True(t, high > low)
}

func TestCalculateRangeQuality(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewWaitRangeStateHandler(scoreEngine, logger)

	// Test good range
	quality := handler.calculateRangeQuality(51000, 49000, RegimeSnapshot{
		Regime:     RegimeSideways,
		Confidence: 0.8,
		ATR14:      0.002,
		BBWidth:    0.02,
	})
	assert.True(t, quality > 0.6)

	// Test invalid range
	quality = handler.calculateRangeQuality(49000, 51000, RegimeSnapshot{})
	assert.Equal(t, 0.0, quality)
}

func TestIsCompressionDetected(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewWaitRangeStateHandler(scoreEngine, logger)

	// Test compression
	assert.True(t, handler.isCompressionDetected(RegimeSnapshot{
		BBWidth: 0.015,
		ATR14:   0.002,
	}))

	// Test no compression
	assert.False(t, handler.isCompressionDetected(RegimeSnapshot{
		BBWidth: 0.05,
		ATR14:   0.008,
	}))
}

func TestWaitRangeTransitions(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewWaitRangeStateHandler(scoreEngine, logger)

	t.Run("WAIT_NEW_RANGE to ENTER_GRID on range confirmed", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.85,
			ADX:        15,
			ATR14:      0.002,
			BBWidth:    0.018,
		}

		transition, err := handler.HandleState(context.Background(), "BTCUSD1", regime, realtime.SymbolRuntimeSnapshot{CurrentPrice: 50000.0})
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeEnterGrid, transition.ToState)
		assert.Equal(t, "range_confirmed", transition.Trigger)
	})

	t.Run("WAIT_NEW_RANGE to ACCUMULATION on compression", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        12,
			ATR14:      0.002,
			BBWidth:    0.015, // Compression
		}

		transition, err := handler.HandleState(context.Background(), "BTCUSD1", regime, realtime.SymbolRuntimeSnapshot{CurrentPrice: 50000.0})
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeAccumulation, transition.ToState)
		assert.Equal(t, "compression_detected", transition.Trigger)
	})

	t.Run("WAIT_NEW_RANGE to TRENDING on trend detection", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.85,
			ADX:        40,
			ATR14:      0.006,
			BBWidth:    0.08,
		}

		transition, err := handler.HandleState(context.Background(), "BTCUSD1", regime, realtime.SymbolRuntimeSnapshot{CurrentPrice: 50000.0})
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeTrending, transition.ToState)
		assert.Equal(t, "trend_detected_during_wait", transition.Trigger)
	})

	t.Run("WAIT_NEW_RANGE to DEFENSIVE on volatility", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeVolatile,
			Confidence: 0.8,
			ADX:        35,
			ATR14:      0.015, // High ATR
			BBWidth:    0.08,
		}

		transition, err := handler.HandleState(context.Background(), "BTCUSD1", regime, realtime.SymbolRuntimeSnapshot{CurrentPrice: 50000.0})
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeDefensive, transition.ToState)
		assert.Equal(t, "extreme_volatility", transition.Trigger)
	})
}

func TestRangeBoundariesStorage(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewWaitRangeStateHandler(scoreEngine, logger)

	// Store boundaries
	handler.setRangeBoundaries("BTCUSD1", 51000, 49000, 0.85)

	// Retrieve
	boundaries, ok := handler.GetRangeBoundaries("BTCUSD1")
	assert.True(t, ok)
	assert.Equal(t, 51000.0, boundaries.RangeHigh)
	assert.Equal(t, 49000.0, boundaries.RangeLow)
	assert.Equal(t, 0.85, boundaries.Quality)
}

func TestWaitRangeShouldTimeout(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewWaitRangeStateHandler(scoreEngine, logger)

	// Not timed out
	entryTime := time.Now().Add(-100 * time.Second)
	assert.False(t, handler.ShouldTimeout(entryTime))

	// Timed out
	entryTime = time.Now().Add(-130 * time.Second)
	assert.True(t, handler.ShouldTimeout(entryTime))
}
