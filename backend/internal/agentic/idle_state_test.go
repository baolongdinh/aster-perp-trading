package agentic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewIdleStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	
	handler := NewIdleStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.NotNil(t, handler.scoreEngine)
	assert.Equal(t, 300*time.Second, handler.maxTimeInIdle)
}

func TestIdleToWaitNewRange(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewIdleStateHandler(scoreEngine, logger)
	
	// Create regime snapshot that favors GRID
	regimeSnapshot := RegimeSnapshot{
		Regime:     RegimeSideways,
		Confidence: 0.9,
		ADX:        15, // Low ADX = sideways
		ATR14:      0.002,
		BBWidth:    0.025,
		Volume24h:  1000000,
		Timestamp:  time.Now(),
	}
	
	transition, err := handler.HandleState(context.Background(), "BTCUSD1", regimeSnapshot)
	assert.NoError(t, err)
	assert.NotNil(t, transition)
	assert.Equal(t, TradingModeWaitNewRange, transition.ToState)
	assert.Equal(t, "grid_score_above_threshold", transition.Trigger)
}

func TestIdleToTrending(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewIdleStateHandler(scoreEngine, logger)
	
	// Create regime snapshot that favors TREND
	regimeSnapshot := RegimeSnapshot{
		Regime:     RegimeTrending,
		Confidence: 0.85,
		ADX:        35, // High ADX = trending
		ATR14:      0.005,
		BBWidth:    0.06,
		Volume24h:  2000000,
		Timestamp:  time.Now(),
	}
	
	transition, err := handler.HandleState(context.Background(), "BTCUSD1", regimeSnapshot)
	assert.NoError(t, err)
	assert.NotNil(t, transition)
	assert.Equal(t, TradingModeTrending, transition.ToState)
	assert.Equal(t, "trend_score_above_threshold", transition.Trigger)
}

func TestIdleStayInIdle(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewIdleStateHandler(scoreEngine, logger)
	
	// Create regime snapshot with low confidence
	regimeSnapshot := RegimeSnapshot{
		Regime:     RegimeSideways,
		Confidence: 0.3,
		ADX:        20,
		ATR14:      0.003,
		BBWidth:    0.04,
		Volume24h:  500000,
		Timestamp:  time.Now(),
	}
	
	transition, err := handler.HandleState(context.Background(), "BTCUSD1", regimeSnapshot)
	assert.NoError(t, err)
	assert.Nil(t, transition) // Should stay in IDLE
}

func TestGetScoreTrend(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewIdleStateHandler(scoreEngine, logger)
	
	// Initially insufficient data
	trend := handler.GetScoreTrend()
	assert.Equal(t, "insufficient_data", trend)
	
	// Add increasing scores
	handler.updateScoreTrendWindow(0.5, 0.5)
	handler.updateScoreTrendWindow(0.55, 0.55)
	handler.updateScoreTrendWindow(0.6, 0.6)
	
	trend = handler.GetScoreTrend()
	assert.Equal(t, "increasing", trend)
	
	// Add decreasing scores
	handler.scoreTrendWindow = []float64{0.7, 0.6, 0.5}
	trend = handler.GetScoreTrend()
	assert.Equal(t, "decreasing", trend)
}

func TestShouldTimeout(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewIdleStateHandler(scoreEngine, logger)
	
	// Not timed out
	entryTime := time.Now().Add(-200 * time.Second)
	assert.False(t, handler.ShouldTimeout(entryTime))
	
	// Timed out
	entryTime = time.Now().Add(-350 * time.Second)
	assert.True(t, handler.ShouldTimeout(entryTime))
}

// Integration test for IDLE state transitions
func TestIdleTransitions(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewIdleStateHandler(scoreEngine, logger)
	
	t.Run("IDLE to WAIT_NEW_RANGE on strong sideways", func(t *testing.T) {
		regimeSnapshot := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.9,
			ADX:        12,
			ATR14:      0.0015,
			BBWidth:    0.018,
			Volume24h:  1500000,
			Timestamp:  time.Now(),
		}
		
		transition, err := handler.HandleState(context.Background(), "BTCUSD1", regimeSnapshot)
		assert.NoError(t, err)
		assert.NotNil(t, transition, "Should transition from IDLE")
		assert.Equal(t, TradingModeWaitNewRange, transition.ToState)
		assert.True(t, transition.Score > 0.6)
	})
	
	t.Run("IDLE to TRENDING on strong trend", func(t *testing.T) {
		regimeSnapshot := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.85,
			ADX:        40,
			ATR14:      0.006,
			BBWidth:    0.08,
			Volume24h:  3000000,
			Timestamp:  time.Now(),
		}
		
		transition, err := handler.HandleState(context.Background(), "ETHUSD1", regimeSnapshot)
		assert.NoError(t, err)
		assert.NotNil(t, transition, "Should transition to TRENDING")
		assert.Equal(t, TradingModeTrending, transition.ToState)
		assert.True(t, transition.Score > 0.75)
	})
	
	t.Run("IDLE remains in IDLE on low scores", func(t *testing.T) {
		regimeSnapshot := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.3,
			ADX:        20,
			ATR14:      0.003,
			BBWidth:    0.04,
			Volume24h:  500000,
			Timestamp:  time.Now(),
		}
		
		transition, err := handler.HandleState(context.Background(), "SOLUSD1", regimeSnapshot)
		assert.NoError(t, err)
		assert.Nil(t, transition, "Should stay in IDLE")
	})
	
	t.Run("IDLE timeout after max time", func(t *testing.T) {
		entryTime := time.Now().Add(-350 * time.Second) // > 5 minutes
		assert.True(t, handler.ShouldTimeout(entryTime), "Should timeout after 5 minutes")
	})
}
