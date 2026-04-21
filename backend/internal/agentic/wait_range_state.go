package agentic

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// WaitRangeStateHandler handles the WAIT_NEW_RANGE state logic
// Detects range boundaries and decides between grid, trend, or accumulation
type WaitRangeStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine

	// Range tracking
	rangeBoundaries map[string]*RangeBoundaries
	maxWaitTime     time.Duration
}

// RangeBoundaries stores detected range for a symbol
type RangeBoundaries struct {
	Symbol     string
	RangeHigh  float64
	RangeLow   float64
	Quality    float64 // 0-1 range quality score
	DetectedAt time.Time
}

// NewWaitRangeStateHandler creates a new WAIT_NEW_RANGE state handler
func NewWaitRangeStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *WaitRangeStateHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &WaitRangeStateHandler{
		logger:          logger.With(zap.String("state_handler", "WAIT_NEW_RANGE")),
		scoreEngine:     scoreEngine,
		rangeBoundaries: make(map[string]*RangeBoundaries),
		maxWaitTime:     120 * time.Second, // 2 minutes max wait
	}
}

// HandleState executes the WAIT_NEW_RANGE state strategy
// Returns the recommended transition, or nil if should stay
func (h *WaitRangeStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	currentPrice float64,
) (*StateTransition, error) {
	h.logger.Debug("Executing WAIT_NEW_RANGE state strategy",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
	)

	// 1. Detect range boundaries
	rangeHigh, rangeLow := h.detectRange(symbol, currentPrice, regimeSnapshot)

	// 2. Calculate range quality score
	rangeScore := h.calculateRangeQuality(rangeHigh, rangeLow, regimeSnapshot)

	// 3. Check for trend emergence during wait
	trendSignal := regimeSnapshot.Confidence
	if regimeSnapshot.ADX >= 35 {
		trendSignal += 0.1
	}
	if regimeSnapshot.BBWidth >= 0.06 {
		trendSignal += 0.05
	}
	if trendSignal > 1 {
		trendSignal = 1
	}

	trendScore := h.scoreEngine.CalculateTrendScore(&ScoreInputs{
		Symbol:         symbol,
		RegimeSnapshot: regimeSnapshot,
		BreakoutSignal: trendSignal,
		MomentumSignal: trendSignal * 0.95,
		VolumeConfirm:  0.75,
	})

	if trendScore.Score > 0.75 && trendScore.Score > rangeScore {
		h.logger.Info("Trend detected during range wait, switching to TRENDING",
			zap.String("symbol", symbol),
			zap.Float64("trend_score", trendScore.Score),
			zap.Float64("range_score", rangeScore),
		)

		return &StateTransition{
			FromState:         TradingModeWaitNewRange,
			ToState:           TradingModeTrending,
			Trigger:           "trend_detected_during_wait",
			Score:             trendScore.Score,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 5. Range confirmed → Enter Grid
	if h.isCompressionDetected(regimeSnapshot) {
		h.logger.Info("Compression detected, transitioning to ACCUMULATION",
			zap.String("symbol", symbol),
			zap.Float64("bb_width", regimeSnapshot.BBWidth),
		)

		return &StateTransition{
			FromState:         TradingModeWaitNewRange,
			ToState:           TradingModeAccumulation,
			Trigger:           "compression_detected",
			Score:             0.7,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	if rangeScore > 0.7 {
		h.setRangeBoundaries(symbol, rangeHigh, rangeLow, rangeScore)

		h.logger.Info("Range confirmed, transitioning to ENTER_GRID",
			zap.String("symbol", symbol),
			zap.Float64("range_high", rangeHigh),
			zap.Float64("range_low", rangeLow),
			zap.Float64("quality", rangeScore),
		)

		return &StateTransition{
			FromState:         TradingModeWaitNewRange,
			ToState:           TradingModeGrid,
			Trigger:           "range_confirmed",
			Score:             rangeScore,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 6. Check for extreme volatility → Defensive
	if regimeSnapshot.ATR14 > 0.01 || regimeSnapshot.Regime == RegimeVolatile {
		h.logger.Warn("Extreme volatility during range wait, going defensive",
			zap.String("symbol", symbol),
			zap.Float64("atr", regimeSnapshot.ATR14),
		)

		return &StateTransition{
			FromState:         TradingModeWaitNewRange,
			ToState:           TradingModeDefensive,
			Trigger:           "extreme_volatility",
			Score:             0.9,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Default: stay in WAIT_NEW_RANGE
	return nil, nil
}

// detectRange calculates range boundaries based on price and indicators
func (h *WaitRangeStateHandler) detectRange(
	symbol string,
	currentPrice float64,
	regimeSnapshot RegimeSnapshot,
) (high, low float64) {
	// Use Bollinger Bands if available
	if regimeSnapshot.BBWidth > 0 {
		// Estimate BB bands from width and assuming price is near middle
		bandWidth := currentPrice * regimeSnapshot.BBWidth
		high = currentPrice + bandWidth/2
		low = currentPrice - bandWidth/2
	} else {
		// Use ATR-based range
		atr := regimeSnapshot.ATR14
		high = currentPrice + atr*2
		low = currentPrice - atr*2
	}

	return high, low
}

// calculateRangeQuality scores the quality of a detected range
func (h *WaitRangeStateHandler) calculateRangeQuality(
	high, low float64,
	regimeSnapshot RegimeSnapshot,
) float64 {
	if high <= low {
		return 0
	}

	// Width factor: optimal range is 1-3% of price
	width := (high - low) / low
	widthScore := 1.0
	if width < 0.005 {
		widthScore = 0.5 // Too tight
	} else if width > 0.05 {
		widthScore = 0.7 // Too wide
	}

	// Regime factor: sideways is best
	regimeScore := 0.5
	if regimeSnapshot.Regime == RegimeSideways {
		regimeScore = regimeSnapshot.Confidence
	} else if regimeSnapshot.Regime == RegimeTrending {
		regimeScore = 0.2
	}

	// Volatility factor: moderate volatility is good
	volScore := 1.0
	if regimeSnapshot.ATR14 > 0.01 {
		volScore = 0.5 // Too volatile
	} else if regimeSnapshot.ATR14 < 0.001 {
		volScore = 0.7 // Too calm
	}

	// Combine scores
	quality := (widthScore*0.3 + regimeScore*0.5 + volScore*0.2)

	return quality
}

// isCompressionDetected checks for pre-breakout compression
func (h *WaitRangeStateHandler) isCompressionDetected(regimeSnapshot RegimeSnapshot) bool {
	bbWidth := regimeSnapshot.BBWidth
	atr := regimeSnapshot.ATR14

	// Compression should be materially tighter than a normal gridable range.
	if bbWidth <= 0.016 && atr <= 0.0025 && regimeSnapshot.ADX <= 15 {
		return true
	}

	// Very tight squeeze can also qualify if the market is not already trending.
	if bbWidth < 0.014 && regimeSnapshot.Regime != RegimeTrending {
		return true
	}

	return false
}

// setRangeBoundaries stores detected range for symbol
func (h *WaitRangeStateHandler) setRangeBoundaries(
	symbol string,
	high, low, quality float64,
) {
	h.rangeBoundaries[symbol] = &RangeBoundaries{
		Symbol:     symbol,
		RangeHigh:  high,
		RangeLow:   low,
		Quality:    quality,
		DetectedAt: time.Now(),
	}
}

// GetRangeBoundaries returns stored range for symbol
func (h *WaitRangeStateHandler) GetRangeBoundaries(symbol string) (*RangeBoundaries, bool) {
	boundaries, ok := h.rangeBoundaries[symbol]
	return boundaries, ok
}

// ShouldTimeout checks if WAIT_NEW_RANGE has exceeded max time
func (h *WaitRangeStateHandler) ShouldTimeout(entryTime time.Time) bool {
	return time.Since(entryTime) > h.maxWaitTime
}

// GetMaxWaitTime returns the configured max wait time
func (h *WaitRangeStateHandler) GetMaxWaitTime() time.Duration {
	return h.maxWaitTime
}
