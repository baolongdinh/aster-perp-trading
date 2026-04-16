package agentic

import (
	"context"
	"math"
	"time"

	"go.uber.org/zap"
)

// TradingGridStateHandler handles the TRADING (GRID) state logic
// Manages active grid positions with continuous monitoring and adaptation
type TradingGridStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine
	
	// Position tracking
	positionPnL     map[string]float64
	gridFillLevels  map[string]int
	entryTime       map[string]time.Time
	
	// Risk parameters
	maxGridLoss     float64 // -3%
	maxPositionSize float64 // 5%
	maxTimeInGrid   time.Duration
}

// GridStatus tracks the current state of grid trading
type GridStatus struct {
	Symbol           string
	FilledLevels     int
	TotalLevels      int
	UnrealizedPnL    float64
	RunningTime      time.Duration
	SignalBlend      float64 // 0-1 entropy-weighted
	TrendScore       float64
	LastRebalance    time.Time
}

// NewTradingGridStateHandler creates a new TRADING (GRID) state handler
func NewTradingGridStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *TradingGridStateHandler {
	return &TradingGridStateHandler{
		logger:          logger.With(zap.String("state_handler", "TRADING_GRID")),
		scoreEngine:     scoreEngine,
		positionPnL:     make(map[string]float64),
		gridFillLevels:  make(map[string]int),
		entryTime:       make(map[string]time.Time),
		maxGridLoss:     -0.03, // -3% max loss
		maxPositionSize: 0.05,  // 5% max position
		maxTimeInGrid:   4 * time.Hour,
	}
}

// HandleState executes the TRADING (GRID) state strategy
func (h *TradingGridStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	currentPrice float64,
	positionSize float64,
	blendedSignal *SignalBundle,
) (*StateTransition, error) {
	h.logger.Debug("Executing TRADING_GRID state strategy",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
		zap.Float64("position_size", positionSize),
	)
	
	// Initialize tracking if new
	if _, ok := h.entryTime[symbol]; !ok {
		h.entryTime[symbol] = time.Now()
	}
	
	// 1. Calculate grid status
	status := h.calculateGridStatus(symbol, currentPrice, blendedSignal)
	
	// 2. Continuous signal blending - adjust intensity
	if blendedSignal != nil {
		entropy := h.calculateSignalEntropy(blendedSignal)
		
		if entropy < 0.3 {
			// Strong agreement - increase intensity
			h.adjustGridIntensity(symbol, 1.2)
			h.logger.Debug("Increasing grid intensity (low entropy)",
				zap.String("symbol", symbol),
				zap.Float64("entropy", entropy),
			)
		} else if entropy > 0.7 {
			// High disagreement - decrease intensity
			h.adjustGridIntensity(symbol, 0.7)
			h.logger.Debug("Decreasing grid intensity (high entropy)",
				zap.String("symbol", symbol),
				zap.Float64("entropy", entropy),
			)
		}
	}
	
	// 3. Check for rebalancing (50% levels filled)
	if h.shouldRebalance(symbol, status) {
		h.rebalanceGrid(symbol, status)
	}
	
	// 4. Check for trend emergence (switch to TRENDING)
	trendScore := h.scoreEngine.CalculateTrendScore(&ScoreInputs{
		Symbol:         symbol,
		RegimeSnapshot: regimeSnapshot,
		BreakoutSignal: 0.5,
		MomentumSignal: 0.5,
	})
	
	gridScore := h.scoreEngine.CalculateGridScore(&ScoreInputs{
		Symbol:         symbol,
		RegimeSnapshot: regimeSnapshot,
	})
	
	if trendScore.Score > 0.8 && trendScore.Score > gridScore.Score*1.2 {
		h.logger.Info("Strong trend emerging, initiating graceful exit to TRENDING",
			zap.String("symbol", symbol),
			zap.Float64("trend_score", trendScore.Score),
			zap.Float64("grid_score", gridScore.Score),
		)
		
		// Initiate graceful exit
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeTrending,
			Trigger:           "trend_emergence",
			Score:             trendScore.Score,
			SmoothingDuration: 10 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// 5. Risk checks
	
	// Check max loss (-3%)
	unrealizedPnL := h.getUnrealizedPnL(symbol)
	if unrealizedPnL < h.maxGridLoss {
		h.logger.Warn("Max grid loss reached, exiting to EXIT_HALF",
			zap.String("symbol", symbol),
			zap.Float64("pnl", unrealizedPnL),
		)
		
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeDefensive, // EXIT_HALF mapped to DEFENSIVE
			Trigger:           "max_loss_reached",
			Score:             0.9,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// Check position size (5%)
	if positionSize > h.maxPositionSize {
		h.logger.Warn("Position size limit reached, going to OVER_SIZE",
			zap.String("symbol", symbol),
			zap.Float64("size", positionSize),
		)
		
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeDefensive, // OVER_SIZE mapped to DEFENSIVE
			Trigger:           "position_size_limit",
			Score:             0.8,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// Check extreme volatility
	if regimeSnapshot.ATR14 > 0.01 || regimeSnapshot.Regime == RegimeVolatile {
		h.logger.Warn("Extreme volatility detected, going DEFENSIVE",
			zap.String("symbol", symbol),
			zap.Float64("atr", regimeSnapshot.ATR14),
		)
		
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeDefensive,
			Trigger:           "extreme_volatility",
			Score:             0.9,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// 6. Check range broken
	if h.isRangeBroken(symbol, currentPrice) {
		h.logger.Info("Range broken, exiting grid",
			zap.String("symbol", symbol),
			zap.Float64("price", currentPrice),
		)
		
		return &StateTransition{
			FromState:         TradingModeGrid,
			ToState:           TradingModeDefensive, // EXIT_ALL mapped to DEFENSIVE
			Trigger:           "range_broken",
			Score:             0.85,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// Default: stay in TRADING_GRID
	return nil, nil
}

// calculateGridStatus computes current grid status
func (h *TradingGridStateHandler) calculateGridStatus(
	symbol string,
	currentPrice float64,
	signals *SignalBundle,
) *GridStatus {
	status := &GridStatus{
		Symbol:        symbol,
		FilledLevels:  h.gridFillLevels[symbol],
		UnrealizedPnL: h.positionPnL[symbol],
	}
	
	if entryTime, ok := h.entryTime[symbol]; ok {
		status.RunningTime = time.Since(entryTime)
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
func (h *TradingGridStateHandler) shouldRebalance(symbol string, status *GridStatus) bool {
	if status.TotalLevels == 0 {
		return false
	}
	
	// Rebalance when 50% levels filled
	fillRatio := float64(status.FilledLevels) / float64(status.TotalLevels)
	return fillRatio >= 0.5
}

// rebalanceGrid adds opposite side orders
func (h *TradingGridStateHandler) rebalanceGrid(symbol string, status *GridStatus) {
	h.logger.Info("Rebalancing grid",
		zap.String("symbol", symbol),
		zap.Int("filled_levels", status.FilledLevels),
	)
	
	// Update last rebalance time
	status.LastRebalance = time.Now()
}

// getUnrealizedPnL returns current PnL for symbol
func (h *TradingGridStateHandler) getUnrealizedPnL(symbol string) float64 {
	return h.positionPnL[symbol]
}

// isRangeBroken checks if price broke out of range
func (h *TradingGridStateHandler) isRangeBroken(symbol string, currentPrice float64) bool {
	// Would check against stored range boundaries
	// For now, use simple ATR-based check
	return false // Placeholder
}

// UpdatePnL updates unrealized PnL for symbol
func (h *TradingGridStateHandler) UpdatePnL(symbol string, pnl float64) {
	h.positionPnL[symbol] = pnl
}

// UpdateFilledLevels updates filled grid levels
func (h *TradingGridStateHandler) UpdateFilledLevels(symbol string, levels int) {
	h.gridFillLevels[symbol] = levels
}

// GetGridStatus returns current grid status
func (h *TradingGridStateHandler) GetGridStatus(symbol string) *GridStatus {
	return h.calculateGridStatus(symbol, 0, nil)
}
