package agentic

import (
	"aster-bot/internal/realtime"
	"context"
	"math"
	"time"

	"go.uber.org/zap"
)

// IdleStateHandler handles the IDLE state logic
// The bot waits in IDLE until clear opportunity emerges
type IdleStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine

	// State tracking
	scoreTrendWindow []float64 // Last 5 score samples (5 min window)
	maxTimeInIdle    time.Duration
}

// NewIdleStateHandler creates a new IDLE state handler
func NewIdleStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *IdleStateHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &IdleStateHandler{
		logger:           logger.With(zap.String("state_handler", "IDLE")),
		scoreEngine:      scoreEngine,
		scoreTrendWindow: make([]float64, 0, 5),
		maxTimeInIdle:    300 * time.Second, // 5 minutes max in IDLE
	}
}

// HandleState executes the IDLE state strategy
// Returns the recommended transition, or nil if should stay in IDLE
func (h *IdleStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	snapshot realtime.SymbolRuntimeSnapshot,
) (*StateTransition, error) {
	_ = snapshot // Reserved for Phase 2+ validation (e.g. liquidity/spread check)
	h.logger.Debug("Executing IDLE state strategy",
		zap.String("symbol", symbol),
	)

	// 1. Calculate GRID score
	gridInputs := h.buildScoreInputs(symbol, regimeSnapshot, 0) // No signals yet
	gridScore := h.scoreEngine.CalculateGridScore(gridInputs)

	// 2. Calculate TREND score
	trendScore := h.scoreEngine.CalculateTrendScore(gridInputs)

	// 3. Update score trend window
	h.updateScoreTrendWindow(gridScore.Score, trendScore.Score)

	// 4. Decision logic
	// Priority: TREND > GRID (trend has higher threshold)

	// Check TREND first (threshold 0.75)
	if trendScore.Score > 0.75 {
		h.logger.Info("TREND opportunity detected from IDLE",
			zap.String("symbol", symbol),
			zap.Float64("trend_score", trendScore.Score),
			zap.Float64("grid_score", gridScore.Score),
		)

		return &StateTransition{
			FromState:         TradingModeIdle,
			ToState:           TradingModeTrending,
			Trigger:           "trend_score_above_threshold",
			Score:             trendScore.Score,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Phase 2: GRID_FARM as default (threshold 0.5)
	// Lower threshold for grid to encourage volume farming
	if gridScore.Score > 0.5 {
		h.logger.Info("GRID opportunity detected from IDLE (Volume-First)",
			zap.String("symbol", symbol),
			zap.Float64("grid_score", gridScore.Score),
			zap.Float64("trend_score", trendScore.Score),
		)

		return &StateTransition{
			FromState:         TradingModeIdle,
			ToState:           TradingModeWaitNewRange,
			Trigger:           "grid_score_above_threshold",
			Score:             gridScore.Score,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 5. Check if both scores are too low (stay in IDLE)
	if gridScore.Score < 0.3 && trendScore.Score < 0.3 {
		h.logger.Debug("Scores too low, remaining in IDLE",
			zap.String("symbol", symbol),
			zap.Float64("grid_score", gridScore.Score),
			zap.Float64("trend_score", trendScore.Score),
		)
		return nil, nil
	}

	// Default: stay in IDLE
	return nil, nil
}

// buildScoreInputs constructs score inputs from regime snapshot
func (h *IdleStateHandler) buildScoreInputs(
	symbol string,
	regimeSnapshot RegimeSnapshot,
	signalStrength float64,
) *ScoreInputs {
	meanReversion := signalStrength
	fvgSignal := signalStrength
	liquiditySignal := signalStrength
	breakoutSignal := signalStrength
	momentumSignal := signalStrength

	if regimeSnapshot.Regime == RegimeSideways || regimeSnapshot.Regime == RegimeRecovery {
		sidewaysConfidence := regimeSnapshot.Confidence
		if regimeSnapshot.ADX <= 18 {
			sidewaysConfidence += 0.1
		}
		if regimeSnapshot.BBWidth > 0 && regimeSnapshot.BBWidth <= 0.03 {
			sidewaysConfidence += 0.1
		}
		meanReversion = math.Min(1, math.Max(signalStrength, sidewaysConfidence))
		fvgSignal = math.Min(1, meanReversion*0.85)
		liquiditySignal = math.Min(1, meanReversion*0.8)
	}

	if regimeSnapshot.Regime == RegimeTrending || regimeSnapshot.Regime == RegimeVolatile {
		trendStrength := regimeSnapshot.Confidence
		if regimeSnapshot.ADX >= 30 {
			trendStrength += 0.1
		}
		if regimeSnapshot.BBWidth >= 0.05 {
			trendStrength += 0.05
		}
		trendStrength = math.Min(1, trendStrength)
		breakoutSignal = math.Max(signalStrength, trendStrength*0.9)
		momentumSignal = math.Max(signalStrength, trendStrength*0.85)
	}

	volumeConfirm := 0.5
	if regimeSnapshot.Volume24h >= 2_000_000 {
		volumeConfirm = 0.9
	} else if regimeSnapshot.Volume24h >= 1_000_000 {
		volumeConfirm = 0.75
	} else if regimeSnapshot.Volume24h >= 500_000 {
		volumeConfirm = 0.6
	}

	return &ScoreInputs{
		Symbol:               symbol,
		RegimeSnapshot:       regimeSnapshot,
		MeanReversionSignals: meanReversion,
		FVGSignal:            fvgSignal,
		LiquiditySignal:      liquiditySignal,
		BreakoutSignal:       breakoutSignal,
		MomentumSignal:       momentumSignal,
		VolumeConfirm:        volumeConfirm,
		Volatility:           regimeSnapshot.ATR14,
		HistoricalWeight:     1.0, // Default neutral
	}
}

// updateScoreTrendWindow maintains the last 5 score samples
func (h *IdleStateHandler) updateScoreTrendWindow(gridScore, trendScore float64) {
	// Add average of both scores
	avgScore := (gridScore + trendScore) / 2
	h.scoreTrendWindow = append(h.scoreTrendWindow, avgScore)

	// Keep only last 5
	if len(h.scoreTrendWindow) > 5 {
		h.scoreTrendWindow = h.scoreTrendWindow[1:]
	}
}

// GetScoreTrend returns the score trend (increasing/decreasing)
func (h *IdleStateHandler) GetScoreTrend() string {
	if len(h.scoreTrendWindow) < 2 {
		return "insufficient_data"
	}

	first := h.scoreTrendWindow[0]
	last := h.scoreTrendWindow[len(h.scoreTrendWindow)-1]

	if last > first*1.1 {
		return "increasing"
	} else if last < first*0.9 {
		return "decreasing"
	}
	return "stable"
}

// ShouldTimeout checks if IDLE state has exceeded max time
func (h *IdleStateHandler) ShouldTimeout(entryTime time.Time) bool {
	return time.Since(entryTime) > h.maxTimeInIdle
}

// GetMaxTimeInIdle returns the configured max time in IDLE
func (h *IdleStateHandler) GetMaxTimeInIdle() time.Duration {
	return h.maxTimeInIdle
}
