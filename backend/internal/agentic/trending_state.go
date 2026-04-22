package agentic

import (
	"context"
	"math"
	"time"

	"aster-bot/internal/realtime"

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
	isFollow     map[string]bool // true if follow, false if probe

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
	IsFollow      bool
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
		isFollow:        make(map[string]bool),
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
	snapshot realtime.SymbolRuntimeSnapshot,
	vector MarketStateVector,
	breakoutLevel float64,
) (*StateTransition, error) {
	currentPrice := snapshot.CurrentPrice
	h.logger.Debug("Executing TRENDING state strategy",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
	)

	// Initialize if new trend trade
	if _, ok := h.entryTime[symbol]; !ok {
		h.entryTime[symbol] = time.Now()
		h.entryPrice[symbol] = currentPrice

		// Phase 3: Trend probe vs follow
		// If persistence is low but breakout is strong, it's a probe
		h.isFollow[symbol] = vector.TrendPersistence > 0.6

		// Determine direction from breakout level
		if currentPrice > breakoutLevel {
			h.direction[symbol] = TrendUp
		} else {
			h.direction[symbol] = TrendDown
		}

		// Set initial stop loss
		atr := regimeSnapshot.ATR14
		if h.direction[symbol] == TrendUp {
			h.stopLoss[symbol] = breakoutLevel - atr*2
		} else {
			h.stopLoss[symbol] = breakoutLevel + atr*2
		}
		h.trailingStop[symbol] = h.stopLoss[symbol]

		h.logger.Info("Trend trade initiated",
			zap.String("symbol", symbol),
			zap.Bool("is_follow", h.isFollow[symbol]),
			zap.Float64("stop_loss", h.stopLoss[symbol]),
		)
	}

	// 1. Hybrid Trend Detection (already in trend, but check continuation)
	hybridScore := h.calculateHybridTrendScore(regimeSnapshot, currentPrice, breakoutLevel)

	// 2. Micro-profit taking (Phase 3: Partial TP)
	unrealizedPnL := snapshot.UnrealizedPnL
	if unrealizedPnL > 0.01 { // 1% profit
		h.takeMicroProfit(symbol, 0.5) // Take 50% profit
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

	if gridScore.Score > 0.7 && hybridScore < 0.4 {
		h.logger.Info("Sideways returning, switching to GRID",
			zap.String("symbol", symbol),
			zap.Float64("grid_score", gridScore.Score),
		)

		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeEnterGrid,
			Trigger:           "sideways_return",
			Score:             gridScore.Score,
			SmoothingDuration: 8 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 5. Trend exhaustion detection
	if h.isTrendExhausted(symbol, regimeSnapshot, vector) {
		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive,
			Trigger:           "trend_exhaustion",
			Score:             0.85,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 6. Risk checks
	if h.isStopLossHit(symbol, currentPrice) || h.isTrailingStopHit(symbol, currentPrice) {
		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive,
			Trigger:           "stop_loss_or_trailing",
			Score:             0.95,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	if unrealizedPnL < h.maxTrendLoss {
		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive,
			Trigger:           "max_loss",
			Score:             0.95,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// Time limit (Shorter for probe)
	maxTime := h.maxTimeInTrend
	if !h.isFollow[symbol] {
		maxTime = 30 * time.Minute // Probes are short-lived
	}

	if time.Since(h.entryTime[symbol]) > maxTime {
		return &StateTransition{
			FromState:         TradingModeTrending,
			ToState:           TradingModeDefensive,
			Trigger:           "time_limit",
			Score:             0.8,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

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
	vector MarketStateVector,
) bool {
	// If it was a follow but persistence dropped significantly
	if h.isFollow[symbol] && vector.TrendPersistence < 0.3 {
		return true
	}

	// Check momentum divergence
	if regimeSnapshot.ADX < 18 {
		return true
	}

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
		IsFollow:     h.isFollow[symbol],
	}
}

// FVGZone represents a Fair Value Gap zone
type FVGZone struct {
	Price float64
	Side  string // "buy" or "sell"
}
