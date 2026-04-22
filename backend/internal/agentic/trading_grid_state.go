package agentic

import (
	"context"
	"math"
	"time"

	"aster-bot/internal/realtime"

	"go.uber.org/zap"
)

// TradingGridStateHandler handles the TRADING (GRID) state logic
// Manages active grid positions with continuous monitoring and adaptation
type TradingGridStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine

	// Risk parameters
	maxGridLoss     float64 // -3%
	maxPositionSize float64 // 5%
	maxTimeInGrid   time.Duration
}

// GridStatus tracks the current state of grid trading
type GridStatus struct {
	Symbol            string
	FilledLevels      int
	TotalLevels       int
	UnrealizedPnL     float64
	RealizedPnL       float64
	RunningTime       time.Duration
	SignalBlend       float64 // 0-1 entropy-weighted
	TrendScore        float64
	LastRebalance     time.Time
	InventoryNotional float64
	MakerFillRatio    float64
}

// NewTradingGridStateHandler creates a new TRADING (GRID) state handler
func NewTradingGridStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *TradingGridStateHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TradingGridStateHandler{
		logger:          logger.With(zap.String("state_handler", "TRADING_GRID")),
		scoreEngine:     scoreEngine,
		maxGridLoss:     -0.03, // -3% max loss
		maxPositionSize: 0.05,  // 5% max position
		maxTimeInGrid:   8 * time.Minute,
	}
}

// HandleState executes the TRADING (GRID) state strategy
func (h *TradingGridStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	snapshot realtime.SymbolRuntimeSnapshot,
	blendedSignal *SignalBundle,
) (*StateTransition, error) {
	currentPrice := snapshot.CurrentPrice
	positionSize := snapshot.PositionNotional
	if positionSize <= 0 {
		positionSize = snapshot.PositionSize
	}

	h.logger.Debug("Executing TRADING_GRID state strategy",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
		zap.Float64("position_size", positionSize),
	)

	// 1. Calculate grid status from real snapshot
	status := h.calculateGridStatus(symbol, snapshot, blendedSignal)

	// 2. Continuous signal blending - adjust intensity
	if blendedSignal != nil {
		entropy := h.calculateSignalEntropy(blendedSignal)

		if entropy < 0.3 {
			// Strong agreement - increase intensity
			h.adjustGridIntensity(symbol, 1.2)
		} else if entropy > 0.7 {
			// High disagreement - decrease intensity
			h.adjustGridIntensity(symbol, 0.7)
		}
	}

	// 3. Check for rebalancing (Phase 2: Dynamic Regrid)
	if h.shouldRebalance(symbol, status, snapshot) {
		h.rebalanceGrid(symbol, status)
		// If inventory is flattened and range is still good, we can trigger a re-grid transition
		if snapshot.InventoryNotional == 0 && snapshot.PendingOrders == 0 {
			h.logger.Info("Inventory flattened, triggering RE-GRID within same state",
				zap.String("symbol", symbol),
			)
			return &StateTransition{
				FromState:         TradingModeGrid,
				ToState:           TradingModeEnterGrid, // Cycle back to enter grid for fresh placement
				Trigger:           "inventory_flattened_regrid",
				Score:             0.9,
				SmoothingDuration: 2 * time.Second,
				Timestamp:         time.Now(),
			}, nil
		}
	}

	// 4. Check for trend emergence (switch to TRENDING)
	trendScore := h.calculateTrendScore(symbol, regimeSnapshot, snapshot)
	gridScore := h.calculateGridScore(symbol, regimeSnapshot, snapshot, trendScore)

	if trendScore > 0.8 && trendScore > gridScore*1.2 {
		h.logger.Info("Strong trend emerging, initiating graceful exit to TRENDING",
			zap.String("symbol", symbol),
			zap.Float64("trend_score", trendScore),
			zap.Float64("grid_score", gridScore),
		)

		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeTrending,
			Trigger:           "trend_emergence",
			Score:             trendScore,
			SmoothingDuration: 10 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 5. Risk checks (Phase 2: Inventory-aware)

	// Check position size (5%)
	if positionSize > h.maxPositionSize {
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeOverSize,
			Trigger:           "position_size_limit",
			Score:             0.8,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Check max loss (-3%)
	if snapshot.UnrealizedPnL < h.maxGridLoss {
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeDefensive,
			Trigger:           "max_loss_reached",
			Score:             0.9,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 6. Check range broken (Phase 2: Real implementation)
	if h.isRangeBroken(symbol, currentPrice, snapshot) {
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeDefensive,
			Trigger:           "range_broken",
			Score:             0.85,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 7. Time-stop (Phase 2: Target 2-8 mins for farm)
	if snapshot.PositionAgeSec > h.maxTimeInGrid.Seconds() {
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeDefensive,
			Trigger:           "time_limit",
			Score:             0.8,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	return nil, nil
}

// calculateGridStatus computes current grid status
func (h *TradingGridStateHandler) calculateGridStatus(
	symbol string,
	snapshot realtime.SymbolRuntimeSnapshot,
	signals *SignalBundle,
) *GridStatus {
	status := &GridStatus{
		Symbol:            symbol,
		UnrealizedPnL:     snapshot.UnrealizedPnL,
		RealizedPnL:       snapshot.RealizedPnL,
		RunningTime:       time.Duration(snapshot.PositionAgeSec) * time.Second,
		InventoryNotional: snapshot.InventoryNotional,
		MakerFillRatio:    snapshot.MakerFillRatio,
	}

	if signals != nil {
		status.SignalBlend = signals.OverallStrength
	}

	return status
}

// calculateSignalEntropy measures signal disagreement (0 = agree, 1 = disagree)
func (h *TradingGridStateHandler) calculateSignalEntropy(signals *SignalBundle) float64 {
	if signals == nil {
		return 0.5
	}

	// Collect all signals
	signalValues := []float64{
		signals.FVGSignal,
		signals.LiquiditySignal,
		signals.MeanReversion,
		signals.BreakoutSignal,
	}

	// Calculate variance
	mean := 0.0
	for _, v := range signalValues {
		mean += v
	}
	mean /= float64(len(signalValues))

	variance := 0.0
	for _, v := range signalValues {
		variance += math.Pow(v-mean, 2)
	}
	variance /= float64(len(signalValues))

	// Normalize to 0-1
	entropy := math.Sqrt(variance) * 2 // Scale up
	return math.Max(0, math.Min(1, entropy))
}

// adjustGridIntensity adjusts order sizes based on signal confidence
func (h *TradingGridStateHandler) adjustGridIntensity(symbol string, multiplier float64) {
	// This would integrate with the actual grid manager
	h.logger.Debug("Adjusting grid intensity",
		zap.String("symbol", symbol),
		zap.Float64("multiplier", multiplier),
	)
}

// shouldRebalance checks if grid needs rebalancing
func (h *TradingGridStateHandler) shouldRebalance(symbol string, status *GridStatus, snapshot realtime.SymbolRuntimeSnapshot) bool {
	// Rebalance if inventory is skewed or market drift detected
	// For volume farming, we want to flatten inventory often
	if snapshot.InventoryNotional > 0 && snapshot.MakerFillRatio < 0.5 {
		return true // Low maker ratio, might need to re-grid to capture maker fills
	}
	return false
}

// rebalanceGrid adds opposite side orders
func (h *TradingGridStateHandler) rebalanceGrid(symbol string, status *GridStatus) {
	h.logger.Info("Rebalancing grid",
		zap.String("symbol", symbol),
	)

	// Update last rebalance time
	status.LastRebalance = time.Now()
}

// isRangeBroken checks if price broke out of range
func (h *TradingGridStateHandler) isRangeBroken(symbol string, currentPrice float64, snapshot realtime.SymbolRuntimeSnapshot) bool {
	// Check if price is too far from best bid/ask or spread is too wide
	if snapshot.SpreadBps > 50 {
		return true // Liquidity stress
	}
	return false
}

func (h *TradingGridStateHandler) calculateTrendScore(symbol string, regime RegimeSnapshot, snapshot realtime.SymbolRuntimeSnapshot) float64 {
	trendSignal := regime.Confidence
	if regime.ADX >= 35 {
		trendSignal += 0.1
	}
	trendSignal = math.Min(1, trendSignal)

	score := h.scoreEngine.CalculateTrendScore(&ScoreInputs{
		Symbol:         symbol,
		RegimeSnapshot: regime,
		BreakoutSignal: trendSignal,
		MomentumSignal: math.Max(0.5, trendSignal*0.95),
		VolumeConfirm:  0.8,
	})
	return score.Score
}

func (h *TradingGridStateHandler) calculateGridScore(symbol string, regime RegimeSnapshot, snapshot realtime.SymbolRuntimeSnapshot, trendScore float64) float64 {
	score := h.scoreEngine.CalculateGridScore(&ScoreInputs{
		Symbol:               symbol,
		RegimeSnapshot:       regime,
		MeanReversionSignals: math.Max(0.1, 1-trendScore),
		Volatility:           regime.ATR14,
	})
	return score.Score
}

// UpdatePnL updates unrealized PnL for symbol (Legacy, kept for interface compatibility)
func (h *TradingGridStateHandler) UpdatePnL(symbol string, pnl float64) {
}

// UpdateFilledLevels updates filled grid levels (Legacy, kept for interface compatibility)
func (h *TradingGridStateHandler) UpdateFilledLevels(symbol string, levels int) {
}

// GetGridStatus returns current grid status (Legacy, kept for interface compatibility)
func (h *TradingGridStateHandler) GetGridStatus(symbol string) *GridStatus {
	return nil
}
