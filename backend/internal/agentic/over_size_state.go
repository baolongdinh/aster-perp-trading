package agentic

import (
	"context"
	"time"

	"aster-bot/internal/realtime"

	"go.uber.org/zap"
)

// OverSizeStateHandler handles the OVER_SIZE state logic
// Gradually reduces position size when exceeding limits
type OverSizeStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine

	// Position tracking
	positionSize   map[string]float64
	targetSize     map[string]float64
	reductionStart map[string]time.Time
	lastReduction  map[string]time.Time

	// Parameters
	maxPositionSize   float64       // 5%
	reductionChunk    float64       // 50% of excess per reduction
	reductionInterval time.Duration // 30s between reductions
	maxReductionTime  time.Duration // 1 minute max
}

// NewOverSizeStateHandler creates a new OVER_SIZE state handler
func NewOverSizeStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *OverSizeStateHandler {
	return &OverSizeStateHandler{
		logger:            logger.With(zap.String("state_handler", "OVER_SIZE")),
		scoreEngine:       scoreEngine,
		positionSize:      make(map[string]float64),
		targetSize:        make(map[string]float64),
		reductionStart:    make(map[string]time.Time),
		lastReduction:     make(map[string]time.Time),
		maxPositionSize:   0.05, // 5% max
		reductionChunk:    0.5,  // 50% of excess
		reductionInterval: 30 * time.Second,
		maxReductionTime:  60 * time.Second, // 1 minute
	}
}

// HandleState executes the OVER_SIZE state strategy
func (h *OverSizeStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	snapshot realtime.SymbolRuntimeSnapshot,
) (*StateTransition, error) {
	currentPrice := snapshot.CurrentPrice
	currentSize := snapshot.PositionNotional
	if currentSize <= 0 {
		currentSize = snapshot.PositionSize
	}

	_ = currentPrice
	h.logger.Debug("Executing OVER_SIZE state strategy",
		zap.String("symbol", symbol),
		zap.Float64("current_size", currentSize),
		zap.Float64("max_size", h.maxPositionSize),
	)

	// Initialize if new
	if _, ok := h.reductionStart[symbol]; !ok {
		h.reductionStart[symbol] = time.Now()
		h.positionSize[symbol] = currentSize
		h.targetSize[symbol] = h.maxPositionSize * 0.8 // Target 80% of max
		h.lastReduction[symbol] = time.Now()

		h.logger.Info("Position size reduction started",
			zap.String("symbol", symbol),
			zap.Float64("current", currentSize),
			zap.Float64("target", h.targetSize[symbol]),
		)
	}

	// 1. Check if position normalized
	if currentSize <= h.maxPositionSize*0.85 {
		h.logger.Info("Position size normalized, returning to TRADING",
			zap.String("symbol", symbol),
			zap.Float64("size", currentSize),
		)

		// Clear tracking
		delete(h.reductionStart, symbol)
		delete(h.positionSize, symbol)
		delete(h.targetSize, symbol)
		delete(h.lastReduction, symbol)

		return &StateTransition{
			FromState:         TradingModeOverSize,
			ToState:           TradingModeGrid,
			Trigger:           "size_normalized",
			Score:             0.8,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 2. Calculate reduction amount
	excess := currentSize - h.targetSize[symbol]
	if excess > 0 {
		reduceAmount := excess * h.reductionChunk

		// Check if enough time passed since last reduction
		if time.Since(h.lastReduction[symbol]) >= h.reductionInterval {
			h.executeReduction(symbol, reduceAmount)
			h.lastReduction[symbol] = time.Now()
		}
	}

	// 3. Check max reduction time (emergency exit if can't reduce in time)
	if time.Since(h.reductionStart[symbol]) > h.maxReductionTime {
		h.logger.Warn("Cannot reduce position in time, emergency exit",
			zap.String("symbol", symbol),
			zap.Duration("time_spent", time.Since(h.reductionStart[symbol])),
		)

		return &StateTransition{
			FromState:         TradingModeOverSize,
			ToState:           TradingModeIdle,
			Trigger:           "emergency_exit",
			Score:             0.9,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 4. Check for extreme volatility (fast exit)
	if regimeSnapshot.ATR14 > 0.015 || regimeSnapshot.Regime == RegimeVolatile {
		h.logger.Warn("Extreme volatility during size reduction, fast exit",
			zap.String("symbol", symbol),
			zap.Float64("atr", regimeSnapshot.ATR14),
		)

		return &StateTransition{
			FromState:         TradingModeOverSize,
			ToState:           TradingModeIdle,
			Trigger:           "volatility_spike",
			Score:             0.95,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Default: stay in OVER_SIZE (continue reducing)
	return nil, nil
}

// executeReduction executes a position size reduction
func (h *OverSizeStateHandler) executeReduction(symbol string, amount float64) {
	h.logger.Info("Executing position size reduction",
		zap.String("symbol", symbol),
		zap.Float64("reduce_amount", amount),
	)

	// Update tracked position size
	h.positionSize[symbol] -= amount
	if h.positionSize[symbol] < 0 {
		h.positionSize[symbol] = 0
	}
}

// GetReductionProgress returns reduction progress (0-1, 1 = fully reduced)
func (h *OverSizeStateHandler) GetReductionProgress(symbol string) float64 {
	if _, ok := h.reductionStart[symbol]; !ok {
		return 1.0 // Not in OVER_SIZE, consider done
	}

	current := h.positionSize[symbol]
	target := h.targetSize[symbol]

	// Calculate how much excess has been reduced
	if current <= target {
		return 1.0
	}

	// Return progress (inverted - closer to target = higher progress)
	return 1.0 - ((current - target) / h.maxPositionSize)
}

// GetReductionTime returns time since reduction started
func (h *OverSizeStateHandler) GetReductionTime(symbol string) time.Duration {
	if start, ok := h.reductionStart[symbol]; ok {
		return time.Since(start)
	}
	return 0
}

// IsOverSize checks if position is still oversized
func (h *OverSizeStateHandler) IsOverSize(currentSize float64) bool {
	return currentSize > h.maxPositionSize
}
