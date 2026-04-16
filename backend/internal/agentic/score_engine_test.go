package agentic

import (
	"testing"

	"aster-bot/internal/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewScoreCalculationEngine(t *testing.T) {
	logger := zap.NewNop()
	
	// Test with nil config (should use defaults)
	engine := NewScoreCalculationEngine(nil, logger)
	assert.NotNil(t, engine)
	assert.NotNil(t, engine.config)
	assert.Equal(t, 0.6, engine.config.GridThreshold)
	assert.Equal(t, 0.75, engine.config.TrendThreshold)
}

func TestCalculateGridScore(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.ScoreEngineConfig{
		GridThreshold:    0.6,
		TrendThreshold:   0.75,
		HysteresisBuffer: 0.1,
		RegimeWeight:     0.4,
		SignalWeight:     0.6,
	}
	engine := NewScoreCalculationEngine(cfg, logger)
	
	// Test 1: Strong sideways regime + strong signals
	inputs := &ScoreInputs{
		Symbol:               "BTCUSD1",
		RegimeSnapshot:       RegimeSnapshot{Regime: RegimeSideways, Confidence: 0.9, BBWidth: 0.03, ATR14: 0.002},
		MeanReversionSignals: 0.8,
		FVGSignal:            0.7,
		LiquiditySignal:      0.6,
		Volatility:           0.002,
	}
	
	score := engine.CalculateGridScore(inputs)
	assert.NotNil(t, score)
	assert.Equal(t, TradingModeGrid, score.Mode)
	assert.True(t, score.Score > 0.6, "Score should be > 0.6 for strong signals")
	assert.True(t, score.Score <= 1.0, "Score should be <= 1.0")
	
	// Test 2: Trending regime (should have low grid score)
	inputs.RegimeSnapshot.Regime = RegimeTrending
	inputs.RegimeSnapshot.Confidence = 0.8
	score2 := engine.CalculateGridScore(inputs)
	assert.True(t, score2.Score < score.Score, "Grid score should be lower in trending regime")
}

func TestCalculateTrendScore(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.ScoreEngineConfig{
		GridThreshold:    0.6,
		TrendThreshold:   0.75,
		HysteresisBuffer: 0.1,
		RegimeWeight:     0.3,
		SignalWeight:     0.7,
	}
	engine := NewScoreCalculationEngine(cfg, logger)
	
	// Test 1: Strong trending regime + breakout + momentum
	inputs := &ScoreInputs{
		Symbol:           "BTCUSD1",
		RegimeSnapshot:   RegimeSnapshot{Regime: RegimeTrending, Confidence: 0.85},
		BreakoutSignal:   0.8,
		MomentumSignal:   0.75,
		VolumeConfirm:    0.9,
		Volatility:       0.003,
	}
	
	score := engine.CalculateTrendScore(inputs)
	assert.NotNil(t, score)
	assert.Equal(t, TradingModeTrending, score.Mode)
	assert.True(t, score.Score > 0.75, "Score should be > 0.75 for strong trend signals")
	
	// Test 2: Volume multiplier effect
	inputs.VolumeConfirm = 1.0
	scoreHighVol := engine.CalculateTrendScore(inputs)
	inputs.VolumeConfirm = 0.5
	scoreLowVol := engine.CalculateTrendScore(inputs)
	assert.True(t, scoreHighVol.Score > scoreLowVol.Score, "Higher volume should increase score")
}

func TestCalculateHybridTrendScore(t *testing.T) {
	logger := zap.NewNop()
	engine := NewScoreCalculationEngine(nil, logger)
	
	// Test 1: Strong agreement (similar signals)
	hybrid1 := engine.CalculateHybridTrendScore(0.8, 0.75)
	assert.True(t, hybrid1 > 0.8, "Should have agreement bonus")
	
	// Test 2: Disagreement (different signals)
	hybrid2 := engine.CalculateHybridTrendScore(0.8, 0.3)
	assert.True(t, hybrid2 < hybrid1, "Should be lower without agreement")
	
	// Test 3: Max/Min bounds
	hybrid3 := engine.CalculateHybridTrendScore(1.0, 1.0)
	assert.True(t, hybrid3 <= 1.0, "Should not exceed 1.0")
}

func TestCalculateAccumulationScore(t *testing.T) {
	logger := zap.NewNop()
	engine := NewScoreCalculationEngine(nil, logger)
	
	// Test 1: Strong compression
	inputs := &ScoreInputs{
		Symbol:         "BTCUSD1",
		RegimeSnapshot: RegimeSnapshot{BBWidth: 0.015, ATR14: 0.002},
		VolumeConfirm:  0.8,
	}
	
	score := engine.calculateAccumulationScore(inputs)
	assert.True(t, score.Score > 0.7, "Strong compression should have high score")
	
	// Test 2: No compression
	inputs.RegimeSnapshot.BBWidth = 0.08
	score2 := engine.calculateAccumulationScore(inputs)
	assert.True(t, score2.Score < 0.4, "Wide BB width should have low score")
}

func TestGetBestMode(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.ScoreEngineConfig{
		GridThreshold:    0.6,
		TrendThreshold:   0.75,
		HysteresisBuffer: 0.1,
	}
	engine := NewScoreCalculationEngine(cfg, logger)
	
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
	
	bestMode, bestScore, found := engine.GetBestMode(scores, TradingModeIdle)
	assert.True(t, found)
	assert.Equal(t, TradingModeGrid, bestMode)
	assert.Equal(t, 0.7, bestScore)
	
	// Test 2: Hysteresis - harder to switch from current mode
	bestMode2, _, found2 := engine.GetBestMode(scores, TradingModeGrid)
	assert.False(t, found2, "Should not transition when already in best mode")
	assert.Equal(t, TradingModeIdle, bestMode2) // Returns zero value when not found
}

func TestUpdatePerformance(t *testing.T) {
	logger := zap.NewNop()
	engine := NewScoreCalculationEngine(nil, logger)
	
	// Update with wins
	engine.UpdatePerformance("BTCUSD1", TradingModeGrid, 100.0)
	engine.UpdatePerformance("BTCUSD1", TradingModeGrid, 50.0)
	engine.UpdatePerformance("BTCUSD1", TradingModeGrid, -20.0)
	
	// Check historical weight
	weight := engine.getHistoricalWeight("BTCUSD1", TradingModeGrid)
	assert.True(t, weight > 0.8 && weight < 1.2, "Weight should be in valid range")
}
