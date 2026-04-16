package agentic

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// EnterGridStateHandler handles the ENTER_GRID state logic
// Prepares and places grid orders with signal-triggered entry
type EnterGridStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine
	
	// Entry tracking
	entryAttempts   map[string]int
	entryStartTime  map[string]time.Time
	maxEntryTime    time.Duration
	minSignalStrength float64
}

// GridParams contains parameters for grid placement
type GridParams struct {
	Symbol         string
	Levels         int
	BuySpread      float64
	SellSpread     float64
	SizeMultiplier float64
	TotalExposure  float64
}

// SignalBundle aggregates signals from strategies
type SignalBundle struct {
	FVGSignal       float64
	LiquiditySignal float64
	BreakoutSignal  float64
	MeanReversion   float64
	OverallStrength float64
}

// NewEnterGridStateHandler creates a new ENTER_GRID state handler
func NewEnterGridStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *EnterGridStateHandler {
	return &EnterGridStateHandler{
		logger:            logger.With(zap.String("state_handler", "ENTER_GRID")),
		scoreEngine:       scoreEngine,
		entryAttempts:     make(map[string]int),
		entryStartTime:    make(map[string]time.Time),
		maxEntryTime:      60 * time.Second, // 60s max to enter
		minSignalStrength: 0.5,                // Min signal to trigger entry
	}
}

// HandleState executes the ENTER_GRID state strategy
func (h *EnterGridStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	currentPrice float64,
	rangeBoundaries *RangeBoundaries,
	signals *SignalBundle,
) (*StateTransition, error) {
	h.logger.Debug("Executing ENTER_GRID state strategy",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
	)
	
	// Track entry attempt
	if _, ok := h.entryStartTime[symbol]; !ok {
		h.entryStartTime[symbol] = time.Now()
	}
	h.entryAttempts[symbol]++
	
	// 1. Calculate optimal grid parameters
	gridParams := h.calculateGridParameters(symbol, regimeSnapshot, currentPrice)
	
	// 2. Apply asymmetric spread based on signals
	if signals != nil {
		gridParams = h.applyAsymmetricSpread(gridParams, signals)
	}
	
	// 3. Signal-triggered entry (hybrid mode)
	if h.shouldWaitForSignal(symbol) {
		bestSignal := h.getBestSignal(signals)
		
		if bestSignal < h.minSignalStrength {
			// Check for timeout
			if h.isEntryTimeout(symbol) {
				// Timeout → enter anyway with reduced size
				h.logger.Warn("Entry timeout, placing grid with reduced size",
					zap.String("symbol", symbol),
					zap.Float64("signal", bestSignal),
				)
				gridParams.SizeMultiplier *= 0.6 // 60% size
			} else {
				h.logger.Debug("Waiting for stronger signal",
					zap.String("symbol", symbol),
					zap.Float64("current_signal", bestSignal),
					zap.Float64("required", h.minSignalStrength),
				)
				return nil, nil // Keep waiting
			}
		}
	}
	
	// 4. Place grid orders (simulated - would integrate with actual order placement)
	h.logger.Info("Placing grid orders",
		zap.String("symbol", symbol),
		zap.Int("levels", gridParams.Levels),
		zap.Float64("buy_spread", gridParams.BuySpread),
		zap.Float64("sell_spread", gridParams.SellSpread),
		zap.Float64("size_mult", gridParams.SizeMultiplier),
	)
	
	// 5. Transition to TRADING (GRID mode)
	transition := &StateTransition{
		FromState:         TradingModeGrid,
		ToState:           TradingModeGrid,
		Trigger:           "grid_placed",
		Score:             0.8,
		SmoothingDuration: 3 * time.Second,
		Timestamp:         time.Now(),
	}
	
	// Clear entry tracking
	delete(h.entryAttempts, symbol)
	delete(h.entryStartTime, symbol)
	
	return transition, nil
}

// calculateGridParameters calculates optimal grid parameters
func (h *EnterGridStateHandler) calculateGridParameters(
	symbol string,
	regimeSnapshot RegimeSnapshot,
	currentPrice float64,
) GridParams {
	// Base parameters from ATR
	atr := regimeSnapshot.ATR14
	if atr <= 0 {
		atr = currentPrice * 0.005 // Default 0.5%
	}
	
	// Calculate spread as ATR * multiplier
	spread := atr * 1.5 // 1.5x ATR between levels
	
	// Number of levels based on volatility
	levels := 5
	if regimeSnapshot.BBWidth > 0.03 {
		levels = 7 // More levels in wider range
	} else if regimeSnapshot.BBWidth < 0.015 {
		levels = 3 // Fewer levels in tight range
	}
	
	// Size multiplier based on regime confidence
	sizeMult := 1.0
	if regimeSnapshot.Confidence > 0.7 {
		sizeMult = 1.2 // Larger size when confident
	} else if regimeSnapshot.Confidence < 0.5 {
		sizeMult = 0.8 // Smaller size when uncertain
	}
	
	// Total exposure limit (max 5% of price range)
	totalExposure := currentPrice * 0.05
	
	return GridParams{
		Symbol:         symbol,
		Levels:         levels,
		BuySpread:      spread,
		SellSpread:     spread,
		SizeMultiplier: sizeMult,
		TotalExposure:  totalExposure,
	}
}

// applyAsymmetricSpread adjusts spreads based on signal direction
func (h *EnterGridStateHandler) applyAsymmetricSpread(
	params GridParams,
	signals *SignalBundle,
) GridParams {
	if signals == nil {
		return params
	}
	
	// Determine signal bias
	buySignal := signals.MeanReversion + signals.LiquiditySignal
	sellSignal := signals.FVGSignal + signals.BreakoutSignal
	
	if buySignal > sellSignal*1.3 {
		// Strong buy signals → tighten buy side, widen sell side
		params.BuySpread *= 0.7  // Tighten 30%
		params.SellSpread *= 1.2 // Widen 20%
		h.logger.Debug("Asymmetric spread: favoring buys",
			zap.Float64("buy_spread", params.BuySpread),
			zap.Float64("sell_spread", params.SellSpread),
		)
	} else if sellSignal > buySignal*1.3 {
		// Strong sell signals → tighten sell side, widen buy side
		params.BuySpread *= 1.2  // Widen 20%
		params.SellSpread *= 0.7 // Tighten 30%
		h.logger.Debug("Asymmetric spread: favoring sells",
			zap.Float64("buy_spread", params.BuySpread),
			zap.Float64("sell_spread", params.SellSpread),
		)
	}
	
	// Adjust size based on overall signal strength
	if signals.OverallStrength > 0.8 {
		params.SizeMultiplier *= 1.3 // 30% larger on strong signals
	} else if signals.OverallStrength < 0.3 {
		params.SizeMultiplier *= 0.7 // 30% smaller on weak signals
	}
	
	return params
}

// shouldWaitForSignal determines if we should wait for signal confirmation
func (h *EnterGridStateHandler) shouldWaitForSignal(symbol string) bool {
	// Always wait for signal in hybrid mode
	// (Can be configured based on strategy)
	return true
}

// getBestSignal returns the strongest signal from the bundle
func (h *EnterGridStateHandler) getBestSignal(signals *SignalBundle) float64 {
	if signals == nil {
		return 0
	}
	
	best := signals.FVGSignal
	if signals.LiquiditySignal > best {
		best = signals.LiquiditySignal
	}
	if signals.MeanReversion > best {
		best = signals.MeanReversion
	}
	if signals.BreakoutSignal > best {
		best = signals.BreakoutSignal
	}
	
	return best
}

// isEntryTimeout checks if entry attempt has timed out
func (h *EnterGridStateHandler) isEntryTimeout(symbol string) bool {
	startTime, ok := h.entryStartTime[symbol]
	if !ok {
		return false
	}
	return time.Since(startTime) > h.maxEntryTime
}

// GetEntryAttempts returns number of entry attempts for symbol
func (h *EnterGridStateHandler) GetEntryAttempts(symbol string) int {
	return h.entryAttempts[symbol]
}

// GetMaxEntryTime returns the configured max entry time
func (h *EnterGridStateHandler) GetMaxEntryTime() time.Duration {
	return h.maxEntryTime
}

// GetMinSignalStrength returns the minimum required signal strength
func (h *EnterGridStateHandler) GetMinSignalStrength() float64 {
	return h.minSignalStrength
}
