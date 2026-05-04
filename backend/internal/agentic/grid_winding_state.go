package agentic

import (
	"context"
	"time"

	"aster-bot/internal/realtime"

	"go.uber.org/zap"
)

// GridWindingStateHandler manages the GRID_WINDING state
// FIX #1: Grace period for time-stop - stop placing new grid orders,
// let existing orders fill naturally, then transition to appropriate state

type GridWindingStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine
	// Track when we entered GRID_WINDING for grace period
	windingStartTime map[string]time.Time
	// Track last position size for comparison
	lastPositionSize map[string]float64
	// Grace period duration (default 2 minutes)
	gracePeriod time.Duration
}

// NewGridWindingStateHandler creates a new GRID_WINDING state handler
func NewGridWindingStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *GridWindingStateHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &GridWindingStateHandler{
		logger:           logger.With(zap.String("state_handler", "GRID_WINDING")),
		scoreEngine:      scoreEngine,
		windingStartTime: make(map[string]time.Time),
		lastPositionSize: make(map[string]float64),
		gracePeriod:      120 * time.Second, // Default 2 minutes
	}
}

// HandleState executes the GRID_WINDING state strategy
// Returns StateTransition to either:
// - WAIT_NEW_RANGE: If position reduced to near zero after grace period
// - DEFENSIVE (with soft exit): If position still significant after grace period
// - GRID (resume): If time-stop condition cleared (rare edge case)
func (h *GridWindingStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	snapshot realtime.SymbolRuntimeSnapshot,
) (*StateTransition, error) {

	h.logger.Debug("Executing GRID_WINDING state strategy",
		zap.String("symbol", symbol),
		zap.Float64("position_size", snapshot.PositionSize),
	)

	// Initialize winding start time if first entry
	if _, exists := h.windingStartTime[symbol]; !exists {
		h.windingStartTime[symbol] = time.Now()
		h.lastPositionSize[symbol] = snapshot.PositionSize
		h.logger.Info("GRID_WINDING: Started grace period",
			zap.String("symbol", symbol),
			zap.Float64("position_size", snapshot.PositionSize),
			zap.Duration("grace_period", h.gracePeriod))
	}

	windingStart := h.windingStartTime[symbol]
	elapsed := time.Since(windingStart)
	gracePeriod := h.gracePeriod

	// Check if grace period has expired
	if elapsed < gracePeriod {
		// Still in grace period - monitor position reduction
		positionReduced := snapshot.PositionSize < h.lastPositionSize[symbol]*0.5
		h.logger.Debug("GRID_WINDING: In grace period",
			zap.String("symbol", symbol),
			zap.Duration("elapsed", elapsed),
			zap.Duration("remaining", gracePeriod-elapsed),
			zap.Float64("position_size", snapshot.PositionSize),
			zap.Bool("position_reduced_50pct", positionReduced))

		// Update last known size
		h.lastPositionSize[symbol] = snapshot.PositionSize

		// Stay in GRID_WINDING (return nil = no transition)
		return nil, nil
	}

	// Grace period expired - determine next state based on position
	h.logger.Info("GRID_WINDING: Grace period expired",
		zap.String("symbol", symbol),
		zap.Float64("position_size", snapshot.PositionSize),
		zap.Float64("unrealized_pnl", snapshot.UnrealizedPnL))

	// Clean up tracking
	delete(h.windingStartTime, symbol)
	delete(h.lastPositionSize, symbol)

	// Decision based on remaining position
	if snapshot.PositionSize < 0.001 {
		// Position closed naturally - go to WAIT_NEW_RANGE
		return &StateTransition{
			FromState:         TradingModeGridWinding,
			ToState:           TradingModeWaitNewRange,
			Trigger:           "winding_complete_position_closed",
			Score:             0.9,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Still have position - transition to DEFENSIVE
	// FIX #4: Use soft exit (aggressive limit) - VF will handle this
	return &StateTransition{
		FromState:         TradingModeGridWinding,
		ToState:           TradingModeDefensive,
		Trigger:           "winding_complete_position_remains",
		Score:             0.85,
		SmoothingDuration: 5 * time.Second,
		Timestamp:         time.Now(),
	}, nil
}

// Reset clears tracking data for a symbol (called on state exit)
func (h *GridWindingStateHandler) Reset(symbol string) {
	delete(h.windingStartTime, symbol)
	delete(h.lastPositionSize, symbol)
}

// IsWinding checks if symbol is currently in winding state
func (h *GridWindingStateHandler) IsWinding(symbol string) bool {
	_, exists := h.windingStartTime[symbol]
	return exists
}
