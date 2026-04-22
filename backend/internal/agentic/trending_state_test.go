package agentic

import (
	"context"
	"testing"
	"time"

	"aster-bot/internal/realtime"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewTrendingStateHandler(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)

	handler := NewTrendingStateHandler(scoreEngine, logger)
	assert.NotNil(t, handler)
	assert.Equal(t, -0.03, handler.maxTrendLoss)
	assert.Equal(t, 2.5, handler.trailingATRMult)
}

func TestTrendingCalculateHybridScore(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTrendingStateHandler(scoreEngine, logger)

	regime := RegimeSnapshot{
		Regime:     RegimeTrending,
		Confidence: 0.85,
		ADX:        40,
		ATR14:      0.006,
	}

	score := handler.calculateHybridTrendScore(regime, 50000, 48000)
	assert.True(t, score > 0.5)
	assert.True(t, score <= 1.0)
}

func TestIsInProfitZone(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTrendingStateHandler(scoreEngine, logger)

	zone := FVGZone{
		Price: 50000,
		Side:  "buy",
	}

	// In profit zone for long
	assert.True(t, handler.isInProfitZone(50500, zone, TrendUp))

	// Not in profit zone for long
	assert.False(t, handler.isInProfitZone(49000, zone, TrendUp))
}

func TestUpdateTrailingStop(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTrendingStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	handler.direction[symbol] = TrendUp
	handler.trailingStop[symbol] = 49000

	// Price moves up - trailing stop should increase
	handler.updateTrailingStop(symbol, 51000, 500)
	assert.True(t, handler.trailingStop[symbol] > 49000)
}

func TestIsStopLossHit(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTrendingStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	handler.direction[symbol] = TrendUp
	handler.stopLoss[symbol] = 49000

	// Stop loss hit
	assert.True(t, handler.isStopLossHit(symbol, 48500))

	// Stop loss not hit
	assert.False(t, handler.isStopLossHit(symbol, 50000))
}

func TestCalculateUnrealizedPnL(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTrendingStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	handler.entryPrice[symbol] = 50000
	handler.direction[symbol] = TrendUp

	// Profit
	pnl := handler.calculateUnrealizedPnL(symbol, 52000)
	assert.True(t, pnl > 0)

	// Loss
	pnl = handler.calculateUnrealizedPnL(symbol, 48000)
	assert.True(t, pnl < 0)
}

func TestTrendingTransitions(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTrendingStateHandler(scoreEngine, logger)

	t.Run("TRENDING to DEFENSIVE on stop loss", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.entryTime[symbol] = time.Now().Add(-10 * time.Minute)
		handler.entryPrice[symbol] = 50000
		handler.direction[symbol] = TrendUp
		handler.stopLoss[symbol] = 49000
		handler.trailingStop[symbol] = 49500

		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.7,
			ADX:        30,
			ATR14:      0.005,
		}

		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice: 48500,
			},
			MarketStateVector{},
			48000,
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeDefensive, transition.ToState)
		assert.Equal(t, "stop_loss_or_trailing", transition.Trigger)
	})

	t.Run("TRENDING to GRID on sideways return", func(t *testing.T) {
		symbol := "ETHUSD1"
		handler.entryTime[symbol] = time.Now().Add(-5 * time.Minute)
		handler.entryPrice[symbol] = 3000
		handler.direction[symbol] = TrendUp

		regime := RegimeSnapshot{
			Regime:     RegimeSideways,
			Confidence: 0.8,
			ADX:        12,
			ATR14:      0.002,
			BBWidth:    0.018,
		}

		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice: 3050,
			},
			MarketStateVector{},
			2800,
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeEnterGrid, transition.ToState)
		assert.Equal(t, "sideways_return", transition.Trigger)
	})

	t.Run("TRENDING to DEFENSIVE on max loss", func(t *testing.T) {
		symbol := "SOLUSD1"
		handler.entryTime[symbol] = time.Now().Add(-5 * time.Minute)
		handler.entryPrice[symbol] = 100
		handler.direction[symbol] = TrendUp

		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.6,
			ADX:        25,
			ATR14:      0.004,
		}

		// Current price is 4% below entry - exceeds max loss
		transition, err := handler.HandleState(
			context.Background(),
			symbol,
			regime,
			realtime.SymbolRuntimeSnapshot{
				CurrentPrice:  96, // -4% loss
				UnrealizedPnL: -0.04,
			},
			MarketStateVector{},
			90,
		)

		assert.NoError(t, err)
		assert.NotNil(t, transition)
		assert.Equal(t, TradingModeDefensive, transition.ToState)
		assert.Equal(t, "max_loss", transition.Trigger)
	})
}

func TestGetTrendStatus(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTrendingStateHandler(scoreEngine, logger)

	symbol := "BTCUSD1"
	handler.entryPrice[symbol] = 50000
	handler.direction[symbol] = TrendUp
	handler.stopLoss[symbol] = 49000
	handler.trailingStop[symbol] = 49500

	status := handler.GetTrendStatus(symbol, 51000)
	assert.NotNil(t, status)
	assert.Equal(t, symbol, status.Symbol)
	assert.Equal(t, TrendUp, status.Direction)
	assert.Equal(t, 50000.0, status.EntryPrice)
}

func TestTrendingHybridStrategyIntegration(t *testing.T) {
	logger := zap.NewNop()
	scoreEngine := NewScoreCalculationEngine(nil, logger)
	handler := NewTrendingStateHandler(scoreEngine, logger)

	t.Run("Hybrid score calculation", func(t *testing.T) {
		regime := RegimeSnapshot{
			Regime:     RegimeTrending,
			Confidence: 0.9,
			ADX:        50,
			ATR14:      0.008,
		}

		score := handler.calculateHybridTrendScore(regime, 50000, 48000)
		assert.True(t, score > 0.7, "Strong trend should have high hybrid score")
	})

	t.Run("Trailing stop logic", func(t *testing.T) {
		symbol := "BTCUSD1"
		handler.direction[symbol] = TrendUp
		handler.entryPrice[symbol] = 50000
		handler.stopLoss[symbol] = 49000
		handler.trailingStop[symbol] = 49000

		// Price moves up progressively
		handler.updateTrailingStop(symbol, 50500, 500)
		handler.updateTrailingStop(symbol, 51000, 500)
		handler.updateTrailingStop(symbol, 51500, 500)

		assert.True(t, handler.trailingStop[symbol] > 49000)
	})
}
