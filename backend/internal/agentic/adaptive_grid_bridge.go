package agentic

import (
	"aster-bot/internal/client"
	"context"
	"time"

	"go.uber.org/zap"
)

// AdaptiveGridBridge provides a bridge between old adaptive_grid logic and new agentic logic
// It allows gradual migration by delegating decisions to AgenticGatekeeper while keeping
// existing execution infrastructure in adaptive_grid
type AdaptiveGridBridge struct {
	gatekeeper       *AgenticGatekeeper
	signalAggregator *SignalAggregator // For seeding historical data
	logger           *zap.Logger
}

// NewAdaptiveGridBridge creates a new bridge
func NewAdaptiveGridBridge(gatekeeper *AgenticGatekeeper, logger *zap.Logger) *AdaptiveGridBridge {
	return &AdaptiveGridBridge{
		gatekeeper: gatekeeper,
		logger:     logger.With(zap.String("component", "adaptive_grid_bridge")),
	}
}

// SetSignalAggregator sets the signal aggregator for historical data seeding
func (b *AdaptiveGridBridge) SetSignalAggregator(sa *SignalAggregator) {
	b.signalAggregator = sa
	b.logger.Info("SignalAggregator set for historical data seeding")
}

// SeedHistoricalData seeds historical klines into SignalAggregator
// Called by AdaptiveGridManager after fetching historical data
func (b *AdaptiveGridBridge) SeedHistoricalData(symbol string, klines []client.Kline) {
	if b.signalAggregator == nil {
		b.logger.Debug("SignalAggregator not set, skipping historical data seeding")
		return
	}

	// Convert client.Kline to agentic.Candle
	candles := make([]Candle, len(klines))
	for i, k := range klines {
		candles[i] = Candle{
			Symbol:    symbol,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
			Timestamp: time.Unix(k.OpenTime/1000, 0),
		}
	}

	b.signalAggregator.SeedHistoricalData(symbol, candles)
}

// CanPlaceOrder delegates to AgenticGatekeeper
// This is called from adaptive_grid/manager.go CanPlaceOrder method
func (b *AdaptiveGridBridge) CanPlaceOrder(ctx context.Context, symbol string, orderType string) (bool, string) {
	if b.gatekeeper == nil {
		b.logger.Warn("Gatekeeper not set, allowing order placement")
		return true, ""
	}

	return b.gatekeeper.CanPlaceOrder(ctx, symbol, orderType)
}

// ShouldExecuteDefensive checks if defensive execution is needed
func (b *AdaptiveGridBridge) ShouldExecuteDefensive(symbol string, currentDrawdown float64) bool {
	// Delegate to risk manager through gatekeeper
	return false // Placeholder - implement based on risk manager
}

// GetRecommendedMode gets the recommended trading mode from agentic engine
func (b *AdaptiveGridBridge) GetRecommendedMode(symbol string) string {
	// This would query the AgenticEngine for the current mode
	// For now, return empty string to indicate no recommendation
	return ""
}

// UpdatePositionState updates position state in agentic risk manager
func (b *AdaptiveGridBridge) UpdatePositionState(symbol string, positionUSDT float64) {
	if b.gatekeeper != nil && b.gatekeeper.riskManager != nil {
		b.gatekeeper.riskManager.UpdatePosition(symbol, positionUSDT)
	}
}

// RecordTradeOutcome records trade outcome for risk tracking
func (b *AdaptiveGridBridge) RecordTradeOutcome(symbol string, profitLossUSDT float64) {
	if b.gatekeeper != nil && b.gatekeeper.riskManager != nil {
		if profitLossUSDT < 0 {
			b.gatekeeper.riskManager.RecordLoss(symbol, -profitLossUSDT)
		}
	}
}

// UpdateEquity updates equity for risk calculations
func (b *AdaptiveGridBridge) UpdateEquity(equity float64) {
	if b.gatekeeper != nil && b.gatekeeper.riskManager != nil {
		b.gatekeeper.riskManager.UpdateEquity(equity)
	}
}

// IsCircuitBreakerTripped checks if circuit breaker is tripped
func (b *AdaptiveGridBridge) IsCircuitBreakerTripped(symbol string) bool {
	if b.gatekeeper == nil || b.gatekeeper.circuitBreaker == nil {
		return false
	}
	return b.gatekeeper.circuitBreaker.IsTripped(symbol)
}
