package agentic

import (
	"context"
	"math"
	"time"

	"go.uber.org/zap"
)

// TrendingStateHandler handles the TRENDING state logic
// Hybrid trend following with breakout + momentum
type TrendingStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine

	// Trade tracking
	entryPrice   map[string]float64
	positionSize map[string]float64
	stopLoss     map[string]float64
	trailingStop map[string]float64
	direction    map[string]TrendDirection
	entryTime    map[string]time.Time

	// Parameters
	maxTrendLoss    float64 // -3%
	maxTimeInTrend  time.Duration
	trailingATRMult float64 // 2-3x ATR
}

// TrendDirection represents trend direction
type TrendDirection int

const (
	TrendUp TrendDirection = iota
	TrendDown
)

// TrendStatus tracks current trend trade status
type TrendStatus struct {
	Symbol        string
	Direction     TrendDirection
	EntryPrice    float64
	CurrentPrice  float64
	UnrealizedPnL float64
	StopLoss      float64
	TrailingStop  float64
	RunningTime   time.Duration
	BreakoutLevel float64
}

// NewTrendingStateHandler creates a new TRENDING state handler
func NewTrendingStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *TrendingStateHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TrendingStateHandler{
		logger:          logger.With(zap.String("state_handler", "TRENDING")),
		scoreEngine:     scoreEngine,
		entryPrice:      make(map[string]float64),
		positionSize:    make(map[string]float64),
		stopLoss:        make(map[string]float64),
		trailingStop:    make(map[string]float64),
		direction:       make(map[string]TrendDirection),
		entryTime:       make(map[string]time.Time),
		maxTrendLoss:    -0.03, // -3% max loss
		maxTimeInTrend:  4 * time.Hour,
		trailingATRMult: 2.5, // 2.5x ATR trailing stop
	}
}

// HandleState executes the TRENDING state strategy
func (h *TrendingStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	currentPrice float64,
	breakoutLevel float64,
	fvgZones []FVGZone,
) (*StateTransition, error) {
	h.logger.Debug("Executing TRENDING state strategy",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
	)

	// Initialize if new trend trade
	if _, ok := h.entryTime[symbol]; !ok {
		h.entryTime[symbol] = time.Now()
		if _, ok := h.entryPrice[symbol]; !ok {
			h.entryPrice[symbol] = currentPrice
		}

		// Determine direction from breakout level
		if _, ok := h.direction[symbol]; !ok {
			if currentPrice > breakoutLevel {
				h.direction[symbol] = TrendUp
			} else {
				h.direction[symbol] = TrendDown
			}
		}

		// Set initial stop loss
		atr := regimeSnapshot.ATR14
		if _, ok := h.stopLoss[symbol]; !ok {
			if h.direction[symbol] == TrendUp {
				h.stopLoss[symbol] = breakoutLevel - atr*2
			} else {
				h.stopLoss[symbol] = breakoutLevel + atr*2
			}
		}
		if _, ok := h.trailingStop[symbol]; !ok {
			h.trailingStop[symbol] = h.stopLoss[symbol]
		}
	}

	// 1. Hybrid Trend Detection (already in trend, but check continuation)
	hybridScore := h.calculateHybridTrendScore(regimeSnapshot, currentPrice, breakoutLevel)

	// 2. Micro-profit taking at FVG zones
	if len(fvgZones) > 0 {
		for _, zone := range fvgZones {
			if h.isInProfitZone(currentPrice, zone, h.direction[symbol]) {
				// Take 25% profit
				h.takeMicroProfit(symbol, 0.25)
				h.logger.Info("Micro-profit taken at FVG zone",
					zap.String("symbol", symbol),
					zap.Float64("zone_price", zone.Price),
					zap.Float64("current", currentPrice),
				)
			}
		}
	}

	// 3. Update trailing stop
	h.updateTrailingStop(symbol, currentPrice, regimeSnapshot.ATR14)

	// 4. Check for grid opportunity (sideways returning)
	gridScore := h.scoreEngine.CalculateGridScore(&ScoreInputs{
		Symbol:               symbol,
		RegimeSnapshot:       regimeSnapshot,
		MeanReversionSignals: regimeSnapshot.Confidence,
		FVGSignal:            regimeSnapshot.Confidence * 0.8,
		LiquiditySignal:      regimeSnapshot.Confidence * 0.75,
		Volatility:           regimeSnapshot.ATR14,
	})

	if gridScore.Score > 0.7 && hybridScore < 0.5 {
		h.logger.Info("Sideways returning, switching to GRID",
			zap.String("symbol", symbol),
			zap.Float64("grid_score", gridScore.Score),
			zap.Float64("hybrid_score", hybridScore),
		)

		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeGrid,
			Trigger:           "sideways_return",
			Score:             gridScore.Score,
			SmoothingDuration: 8 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 5. Trend exhaustion detection
	if h.isTrendExhausted(symbol, regimeSnapshot) {
		h.logger.Info("Trend exhausted, exiting",
			zap.String("symbol", symbol),
		)

		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive, // EXIT_ALL
			Trigger:           "trend_exhaustion",
			Score:             0.85,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 6. Risk checks

	// Stop loss hit
	if h.isStopLossHit(symbol, currentPrice) {
		h.logger.Warn("Stop loss hit",
			zap.String("symbol", symbol),
			zap.Float64("stop_loss", h.stopLoss[symbol]),
			zap.Float64("trailing", h.trailingStop[symbol]),
			zap.Float64("current", currentPrice),
		)

		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive, // EXIT_ALL
			Trigger:           "stop_loss",
			Score:             0.95,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Trailing stop hit
	if h.isTrailingStopHit(symbol, currentPrice) {
		h.logger.Info("Trailing stop hit - taking profit",
			zap.String("symbol", symbol),
			zap.Float64("trailing_stop", h.trailingStop[symbol]),
			zap.Float64("current", currentPrice),
		)

		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive, // EXIT_ALL
			Trigger:           "trailing_stop",
			Score:             0.9,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Max loss check
	unrealizedPnL := h.calculateUnrealizedPnL(symbol, currentPrice)
	if unrealizedPnL < h.maxTrendLoss {
		h.logger.Warn("Max trend loss reached",
			zap.String("symbol", symbol),
			zap.Float64("pnl", unrealizedPnL),
		)

		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive, // EXIT_ALL
			Trigger:           "max_loss",
			Score:             0.95,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Time limit
	if time.Since(h.entryTime[symbol]) > h.maxTimeInTrend {
		h.logger.Info("Max time in trend reached",
			zap.String("symbol", symbol),
			zap.Duration("time_in_trend", time.Since(h.entryTime[symbol])),
		)

		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive, // EXIT_ALL
			Trigger:           "time_limit",
			Score:             0.8,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Update PnL tracking
	h.updatePnL(symbol, unrealizedPnL)

	// Default: stay in TRENDING
	return nil, nil
}

// calculateHybridTrendScore evaluates trend continuation strength
func (h *TrendingStateHandler) calculateHybridTrendScore(
	regimeSnapshot RegimeSnapshot,
	currentPrice, breakoutLevel float64,
) float64 {
	// Breakout strength should decay aggressively once the regime is no longer trending.
	breakoutSignal := 0.15
	if regimeSnapshot.Regime == RegimeTrending {
		breakoutSignal = regimeSnapshot.Confidence
	} else if regimeSnapshot.Regime == RegimeVolatile {
		breakoutSignal = regimeSnapshot.Confidence * 0.4
	} else if regimeSnapshot.Regime == RegimeRecovery {
		breakoutSignal = regimeSnapshot.Confidence * 0.3
	}
	if breakoutLevel > 0 {
		expansion := math.Abs(currentPrice-breakoutLevel) / breakoutLevel
		if expansion > 0.01 {
			breakoutSignal = math.Min(1.0, breakoutSignal+math.Min(0.2, expansion*2))
		}
	}

	// Momentum (ROC + velocity)
	momentumSignal := math.Min(0.25, regimeSnapshot.ADX/100)
	if regimeSnapshot.ADX > 25 {
		momentumSignal = math.Min(1.0, regimeSnapshot.ADX/50)
	}

	// Calculate hybrid
	maxSignal := math.Max(breakoutSignal, momentumSignal)
	minSignal := math.Min(breakoutSignal, momentumSignal)

	hybrid := maxSignal*0.6 + minSignal*0.4

	// Agreement bonus
	if math.Abs(breakoutSignal-momentumSignal) < 0.2 {
		hybrid *= 1.15
	}

	return math.Min(1.0, hybrid)
}

// isInProfitZone checks if price is in FVG profit zone
func (h *TrendingStateHandler) isInProfitZone(
	currentPrice float64,
	zone FVGZone,
	direction TrendDirection,
) bool {
	if direction == TrendUp {
		return currentPrice > zone.Price && currentPrice < zone.Price*1.02
	}
	return currentPrice < zone.Price && currentPrice > zone.Price*0.98
}

// takeMicroProfit takes partial profit
func (h *TrendingStateHandler) takeMicroProfit(symbol string, percentage float64) {
	h.logger.Info("Taking micro-profit",
		zap.String("symbol", symbol),
		zap.Float64("percentage", percentage),
	)
}

// updateTrailingStop updates the trailing stop price
func (h *TrendingStateHandler) updateTrailingStop(
	symbol string,
	currentPrice, atr float64,
) {
	if _, ok := h.trailingStop[symbol]; !ok {
		return
	}

	currentTrailing := h.trailingStop[symbol]
	atrDistance := atr * h.trailingATRMult

	if h.direction[symbol] == TrendUp {
		// For long: trail below price
		newStop := currentPrice - atrDistance
		if newStop > currentTrailing {
			h.trailingStop[symbol] = newStop
			h.logger.Debug("Trailing stop updated (long)",
				zap.String("symbol", symbol),
				zap.Float64("new_stop", newStop),
			)
		}
	} else {
		// For short: trail above price
		newStop := currentPrice + atrDistance
		if newStop < currentTrailing || currentTrailing == 0 {
			h.trailingStop[symbol] = newStop
			h.logger.Debug("Trailing stop updated (short)",
				zap.String("symbol", symbol),
				zap.Float64("new_stop", newStop),
			)
		}
	}
}

// isTrendExhausted detects trend exhaustion
func (h *TrendingStateHandler) isTrendExhausted(
	symbol string,
	regimeSnapshot RegimeSnapshot,
) bool {
	if regimeSnapshot.Regime == RegimeSideways || regimeSnapshot.Regime == RegimeRecovery {
		return false
	}

	// Check momentum divergence
	if regimeSnapshot.ADX < 18 {
		return true
	}

	// Check for volume decrease (would need volume data)
	return false
}

// isStopLossHit checks if stop loss is hit
func (h *TrendingStateHandler) isStopLossHit(symbol string, currentPrice float64) bool {
	stopLoss, ok := h.stopLoss[symbol]
	if !ok {
		return false
	}

	if h.direction[symbol] == TrendUp {
		return currentPrice < stopLoss
	}
	return currentPrice > stopLoss
}

// isTrailingStopHit checks if trailing stop is hit
func (h *TrendingStateHandler) isTrailingStopHit(symbol string, currentPrice float64) bool {
	trailingStop, ok := h.trailingStop[symbol]
	if !ok {
		return false
	}

	if h.direction[symbol] == TrendUp {
		return currentPrice < trailingStop
	}
	return currentPrice > trailingStop
}

// calculateUnrealizedPnL calculates current PnL
func (h *TrendingStateHandler) calculateUnrealizedPnL(
	symbol string,
	currentPrice float64,
) float64 {
	entryPrice, ok := h.entryPrice[symbol]
	if !ok || entryPrice == 0 {
		return 0
	}

	if h.direction[symbol] == TrendUp {
		return (currentPrice - entryPrice) / entryPrice
	}
	return (entryPrice - currentPrice) / entryPrice
}

// updatePnL tracks PnL for the symbol
func (h *TrendingStateHandler) updatePnL(symbol string, pnl float64) {
	// Store for tracking
}

// GetTrendStatus returns current trend status
func (h *TrendingStateHandler) GetTrendStatus(symbol string, currentPrice float64) *TrendStatus {
	return &TrendStatus{
		Symbol:       symbol,
		Direction:    h.direction[symbol],
		EntryPrice:   h.entryPrice[symbol],
		CurrentPrice: currentPrice,
		StopLoss:     h.stopLoss[symbol],
		TrailingStop: h.trailingStop[symbol],
		RunningTime:  time.Since(h.entryTime[symbol]),
	}
}

// FVGZone represents a Fair Value Gap zone
type FVGZone struct {
	Price float64
	Side  string // "buy" or "sell"
}
