package agentic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewOverSizeStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	
	handler := NewOverSizeStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.Equal(t, 0.05, handler.maxPositionSize)
	assert.Equal(t, 0.5, handler.reductionChunk)
	assert.Equal(t, 30*time.Second, handler.reductionInterval)
}

func TestIsOverSize(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewOverSizeStateHandler(scoreEngine, logger)
	
	assert.True(t, handler.IsOverSize(0.06))  // 6% > 5%
	assert.False(t, handler.IsOverSize(0.04)) // 4% < 5%
	assert.False(t, handler.IsOverSize(0.05)) // 5% = 5% (not over)
}

func TestGetReductionProgress(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewOverSizeStateHandler(scoreEngine, logger)
	
	symbol := "BTCUSD1"
	// Not in OVER_SIZE
	progress := handler.GetReductionProgress(symbol)
	assert.Equal(t, 1.0, progress)
	
	// Initialize tracking
	handler.reductionStart[symbol] = time.Now()
	handler.positionSize[symbol] = 0.08 // 8%
	handler.targetSize[symbol] = 0.04   // Target 4%
	
	progress = handler.GetReductionProgress(symbol)
	assert.True(t, progress >= 0 && progress <= 1)
}

func TestOverSizeTransitions(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewOverSizeStateHandler(scoreEngine, logger)
	
	t.Run("OVER_SIZE to GRID on normalized", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.reductionStart[symbol] = time.Now()
		handler.positionSize[symbol] = 0.042 // Just above threshold
		handler.targetSize[symbol] = 0.04
		handler.lastReduction[symbol] = time.Now()
		
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        18,
			ATR14:      0.003,
		}
		
		// Position size normalized (below 85% of max = 0.0425)
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			50000,
			0.04, // 4% - normalized
		)
		
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeGrid, transition.ToState)
		assert.Equal(t, "size_normalized", transition.Trigger)
	})
	
	t.Run("OVER_SIZE to IDLE on emergency timeout", func(t *testing.T) {
		symbol := "ETHUSD1"
		handler.reductionStart[symbol] = time.Now().Add(-70 * time.Second) // > 1min
		handler.positionSize[symbol] = 0.08
		handler.targetSize[symbol] = 0.04
		handler.lastReduction[symbol] = time.Now()
		
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        18,
			ATR14:      0.003,
		}
		
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			3000,
			0.07, // Still oversized after timeout
		)
		
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeIdle, transition.ToState)
		assert.Equal(t, "emergency_exit", transition.Trigger)
	})
	
	t.Run("OVER_SIZE to IDLE on volatility spike", func(t *testing.T) {
		symbol := "SOLUSD1"
		handler.reductionStart[symbol] = time.Now()
		handler.positionSize[symbol] = 0.08
		handler.targetSize[symbol] = 0.04
		handler.lastReduction[symbol] = time.Now()
		
		regime := RegimeSnapshot{
			Regime:     RegimeVolatile,
			Confidence: 0.8,
			ADX:        40,
			ATR14:      0.02, // High ATR
		}
		
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			100,
			0.07,
		)
		
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeIdle, transition.ToState)
		assert.Equal(t, "volatility_spike", transition.Trigger)
	})
}

func TestGetReductionTime(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewOverSizeStateHandler(scoreEngine, logger)
	
	symbol := "BTCUSD1"
	handler.reductionStart[symbol] = time.Now().Add(-30 * time.Second)
	
	reductionTime := handler.GetReductionTime(symbol)
	assert.True(t, reductionTime >= 30*time.Second)
	
	// Non-existent symbol
	assert.Equal(t, time.Duration(0), handler.GetReductionTime("NONEXISTENT"))
}

func TestRiskProtectionIntegration(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewOverSizeStateHandler(scoreEngine, logger)
	
	t.Run("Position size reduction cycle", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.reductionStart[symbol] = time.Now()
		handler.positionSize[symbol] = 0.10 // 10% - way over
		handler.targetSize[symbol] = 0.04   // 4% target
		handler.lastReduction[symbol] = time.Now().Add(-35 * time.Second) // > 30s
		
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        18,
			ATR14:      0.003,
		}
		
		// Should execute reduction (time passed > interval)
		_, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			50000,
			0.08, // Still oversized
		)
		
		assert.NoError(t, err)
		// Position should be reduced
		assert.True(t, handler.positionSize[symbol] < 0.10)
	})
}
