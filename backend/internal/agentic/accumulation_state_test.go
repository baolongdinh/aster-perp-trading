package agentic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewAccumulationStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	
	handler := NewAccumulationStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.Equal(t, 8*time.Hour, handler.maxAccumulationTime)
	assert.Equal(t, 0.03, handler.maxPositionSize)
}

func TestDetectWyckoffPhase(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewAccumulationStateHandler(scoreEngine, logger)
	
	symbol := "BTCUSD1"
	handler.entryPrice[symbol] = 50000
	handler.accumulationTime[symbol] = time.Now()
	handler.wyckoffPhase[symbol] = PhasePreliminarySupport
	
	regime := RegimeSnapshot{
		Regime:     RegimeSideways,
		Confidence: 0.7,
		ADX:        15,
		ATR14:      0.002,
		BBWidth:    0.02,
	}
	
	handler.detectWyckoffPhase(symbol, regime, 50000)
	phase := handler.GetWyckoffPhase(symbol)
	assert.Equal(t, "Preliminary Support", handler.getPhaseName(phase))
}

func TestIsBreakoutDetected(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewAccumulationStateHandler(scoreEngine, logger)
	
	symbol := "BTCUSD1"
	handler.entryPrice[symbol] = 50000
	
	regime := RegimeSnapshot{
		Regime:     RegimeTrending,
		Confidence: 0.8,
		ADX:        35,
		ATR14:      0.005,
		BBWidth:    0.045,
	}
	
	// Price above threshold
	assert.True(t, handler.isBreakoutDetected(symbol, 51000, regime))
	
	// Price below threshold
	assert.False(t, handler.isBreakoutDetected(symbol, 50100, regime))
}

func TestIsSignOfStrength(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewAccumulationStateHandler(scoreEngine, logger)
	
	regime := RegimeSnapshot{
		Regime:     RegimeTrending,
		Confidence: 0.9,
		ADX:        40,
		ATR14:      0.008,
		BBWidth:    0.06,
	}
	
	assert.True(t, handler.isSignOfStrength("BTCUSD1", regime))
	
	regime.ADX = 20
	regime.BBWidth = 0.02
	assert.False(t, handler.isSignOfStrength("BTCUSD1", regime))
}

func TestIsFailedAccumulation(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewAccumulationStateHandler(scoreEngine, logger)
	
	symbol := "BTCUSD1"
	handler.entryPrice[symbol] = 50000
	
	regime := RegimeSnapshot{
		Regime:     RegimeSideways,
		Confidence: 0.5,
		ADX:        12,
		ATR14:      0.002,
	}
	
	// Price dropped 6% with low momentum
	assert.True(t, handler.isFailedAccumulation(symbol, 47000, regime))
	
	// Price only dropped 2%
	assert.False(t, handler.isFailedAccumulation(symbol, 49000, regime))
}

func TestAccumulationTransitions(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewAccumulationStateHandler(scoreEngine, logger)
	
	t.Run("ACCUMULATION to TRENDING on breakout", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.entryPrice[symbol] = 50000
		handler.accumulationTime[symbol] = time.Now()
		
		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.85,
			ADX:        40,
			ATR14:      0.006,
			BBWidth:    0.05,
		}
		
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			51000, // 2% above entry
			1000000,
		)
		
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeTrending, transition.ToState)
		assert.Equal(t, "breakout", transition.Trigger)
	})
	
	t.Run("ACCUMULATION to TRENDING on Sign of Strength", func(t *testing.T) {
		symbol := "ETHUSD1"
		handler.entryPrice[symbol] = 3000
		handler.accumulationTime[symbol] = time.Now()
		
		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.9,
			ADX:        45,
			ATR14:      0.008,
			BBWidth:    0.07,
		}
		
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			3050,
			500000,
		)
		
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeTrending, transition.ToState)
		assert.Equal(t, "sign_of_strength", transition.Trigger)
	})
	
	t.Run("ACCUMULATION to DEFENSIVE on time limit", func(t *testing.T) {
		symbol := "SOLUSD1"
		handler.entryPrice[symbol] = 100
		handler.accumulationTime[symbol] = time.Now().Add(-9 * time.Hour) // > 8hr
		
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.6,
			ADX:        15,
			ATR14:      0.003,
		}
		
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			100,
			200000,
		)
		
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeDefensive, transition.ToState)
		assert.Equal(t, "time_limit", transition.Trigger)
	})
	
	t.Run("ACCUMULATION to DEFENSIVE on volatility spike", func(t *testing.T) {
		symbol := "AVAXUSD1"
		handler.entryPrice[symbol] = 30
		handler.accumulationTime[symbol] = time.Now()
		
		regime := RegimeSnapshot{
			Regime:     RegimeVolatile,
			Confidence: 0.7,
			ADX:        35,
			ATR14:      0.015, // High ATR
		}
		
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			30,
			100000,
		)
		
		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeDefensive, transition.ToState)
		assert.Equal(t, "volatility_spike", transition.Trigger)
	})
}

func TestGetAccumulationStatus(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewAccumulationStateHandler(scoreEngine, logger)
	
	symbol := "BTCUSD1"
	handler.entryPrice[symbol] = 50000
	handler.accumulationTime[symbol] = time.Now()
	handler.wyckoffPhase[symbol] = PhaseAutomaticRally
	handler.positionSize[symbol] = 0.02
	
	phase := handler.GetWyckoffPhase(symbol)
	assert.Equal(t, PhaseAutomaticRally, phase)
	
	size := handler.GetPositionSize(symbol)
	assert.Equal(t, 0.02, size)
	
	timeInAcc := handler.GetAccumulationTime(symbol)
	assert.True(t, timeInAcc >= 0)
}

func TestAccumulationStrategyIntegration(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewAccumulationStateHandler(scoreEngine, logger)
	
	t.Run("Wyckoff phase progression", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.entryPrice[symbol] = 50000
		handler.accumulationTime[symbol] = time.Now()
		handler.wyckoffPhase[symbol] = PhasePreliminarySupport
		
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        15,
			ATR14:      0.002,
			BBWidth:    0.02,
		}
		
		handler.detectWyckoffPhase(symbol, regime, 50000)
		assert.Equal(t, "Preliminary Support", handler.getPhaseName(handler.GetWyckoffPhase(symbol)))
	})
	
	t.Run("Position building", func(t *testing.T) {
		symbol := "ETHUSD1"
		handler.entryPrice[symbol] = 3000
		handler.accumulationTime[symbol] = time.Now()
		handler.wyckoffPhase[symbol] = PhaseSellingClimax
		
		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.7,
			ADX:        15,
			ATR14:      0.002,
		}
		
		handler.buildPosition(symbol, 3000, regime)
		assert.True(t, handler.GetPositionSize(symbol) > 0)
	})
}
