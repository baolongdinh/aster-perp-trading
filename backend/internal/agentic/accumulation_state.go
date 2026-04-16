package agentic

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// AccumulationStateHandler handles the ACCUMULATION state logic
// Pre-breakout accumulation with Wyckoff pattern detection
type AccumulationStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine
	
	// Position tracking
	positionSize     map[string]float64
	accumulationTime map[string]time.Time
	entryPrice       map[string]float64
	
	// Wyckoff pattern tracking
	wyckoffPhase     map[string]WyckoffPhase
	volumeProfile    map[string]*VolumeProfile
	
	// Parameters
	maxAccumulationTime time.Duration
	maxPositionSize     float64
	breakoutThreshold   float64
}

// WyckoffPhase represents Wyckoff accumulation phase
type WyckoffPhase int

const (
	PhasePreliminarySupport WyckoffPhase = iota
	PhaseSellingClimax
	PhaseAutomaticRally
	PhaseSecondaryTest
	PhaseSpring
	PhaseBreakout
	PhaseSignOfStrength
)

// VolumeProfile tracks volume during accumulation
type VolumeProfile struct {
	BuyVolume  float64
	SellVolume float64
	NetVolume  float64
	Imbalance  float64 // -1 to 1
}

// NewAccumulationStateHandler creates a new ACCUMULATION state handler
func NewAccumulationStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *AccumulationStateHandler {
	return &AccumulationStateHandler{
		logger:            logger.With(zap.String("state_handler", "ACCUMULATION")),
		scoreEngine:       scoreEngine,
		positionSize:      make(map[string]float64),
		accumulationTime:  make(map[string]time.Time),
		entryPrice:        make(map[string]float64),
		wyckoffPhase:      make(map[string]WyckoffPhase),
		volumeProfile:     make(map[string]*VolumeProfile),
		maxAccumulationTime: 8 * time.Hour,
		maxPositionSize:      0.03, // 3% max position
		breakoutThreshold:    0.02, // 2% breakout
	}
}

// HandleState executes the ACCUMULATION state strategy
func (h *AccumulationStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	currentPrice float64,
	volume24h float64,
) (*StateTransition, error) {
	h.logger.Debug("Executing ACCUMULATION state strategy",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
	)
	
	// Initialize if new
	if _, ok := h.accumulationTime[symbol]; !ok {
		h.accumulationTime[symbol] = time.Now()
		h.entryPrice[symbol] = currentPrice
		h.wyckoffPhase[symbol] = PhasePreliminarySupport
		h.volumeProfile[symbol] = &VolumeProfile{}
	}
	
	// 1. Detect Wyckoff phase
	h.detectWyckoffPhase(symbol, regimeSnapshot, currentPrice)
	
	// 2. Build position gradually based on phase
	h.buildPosition(symbol, currentPrice, regimeSnapshot)
	
	// 3. Check for breakout
	if h.isBreakoutDetected(symbol, currentPrice, regimeSnapshot) {
		h.logger.Info("Breakout detected from accumulation",
			zap.String("symbol", symbol),
			zap.String("phase", h.getPhaseName(h.wyckoffPhase[symbol])),
		)
		
		return &StateTransition{
			FromState:         TradingModeAccumulation,
			ToState:           TradingModeTrending,
			Trigger:           "breakout",
			Score:             0.85,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// 4. Check for Sign of Strength
	if h.isSignOfStrength(symbol, regimeSnapshot) {
		h.logger.Info("Sign of Strength detected",
			zap.String("symbol", symbol),
		)
		
		return &StateTransition{
			FromState:         TradingModeAccumulation,
			ToState:           TradingModeTrending,
			Trigger:           "sign_of_strength",
			Score:             0.8,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// 5. Risk checks
	
	// Time limit
	if time.Since(h.accumulationTime[symbol]) > h.maxAccumulationTime {
		h.logger.Warn("Max accumulation time reached, exiting",
			zap.String("symbol", symbol),
		)
		
		return &StateTransition{
			FromState:         TradingModeAccumulation,
			ToState:           TradingModeDefensive,
			Trigger:           "time_limit",
			Score:             0.7,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// Position size limit
	if h.positionSize[symbol] > h.maxPositionSize {
		h.logger.Warn("Max position size reached in accumulation",
			zap.String("symbol", symbol),
			zap.Float64("size", h.positionSize[symbol]),
		)
		
		return &StateTransition{
			FromState:         TradingModeAccumulation,
			ToState:           TradingModeDefensive,
			Trigger:           "position_size_limit",
			Score:             0.75,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// Volatility spike - abort accumulation
	if regimeSnapshot.ATR14 > 0.01 || regimeSnapshot.Regime == RegimeVolatile {
		h.logger.Warn("Volatility spike during accumulation, exiting",
			zap.String("symbol", symbol),
			zap.Float64("atr", regimeSnapshot.ATR14),
		)
		
		return &StateTransition{
			FromState:         TradingModeAccumulation,
			ToState:           TradingModeDefensive,
			Trigger:           "volatility_spike",
			Score:             0.9,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// 6. Check for failed accumulation (range broken without volume)
	if h.isFailedAccumulation(symbol, currentPrice, regimeSnapshot) {
		h.logger.Warn("Accumulation failed, exiting",
			zap.String("symbol", symbol),
		)
		
		return &StateTransition{
			FromState:         TradingModeAccumulation,
			ToState:           TradingModeDefensive,
			Trigger:           "accumulation_failed",
			Score:             0.8,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// Default: stay in ACCUMULATION
	return nil, nil
}

// detectWyckoffPhase determines current Wyckoff accumulation phase
func (h *AccumulationStateHandler) detectWyckoffPhase(
	symbol string,
	regimeSnapshot RegimeSnapshot,
	currentPrice float64,
) {
	entryPrice := h.entryPrice[symbol]
	bbWidth := regimeSnapshot.BBWidth
	adx := regimeSnapshot.ADX
	
	// Simple phase detection based on indicators
	currentPhase := h.wyckoffPhase[symbol]
	
	switch currentPhase {
	case PhasePreliminarySupport:
		// Low volatility, low ADX = preliminary support
		if bbWidth < 0.025 && adx < 20 {
			h.wyckoffPhase[symbol] = PhaseSellingClimax
		}
		
	case PhaseSellingClimax:
		// Sudden drop in price (would need historical data)
		// For now, progress to automatic rally after some time
		if time.Since(h.accumulationTime[symbol]) > 30*time.Minute {
			h.wyckoffPhase[symbol] = PhaseAutomaticRally
		}
		
	case PhaseAutomaticRally:
		// Price recovery
		if currentPrice > entryPrice*0.98 {
			h.wyckoffPhase[symbol] = PhaseSecondaryTest
		}
		
	case PhaseSecondaryTest:
		// Retest of lows
		if currentPrice < entryPrice*0.97 {
			h.wyckoffPhase[symbol] = PhaseSpring
		}
		
	case PhaseSpring:
		// Final consolidation before breakout
		if bbWidth < 0.015 {
			h.wyckoffPhase[symbol] = PhaseBreakout
		}
		
	case PhaseBreakout:
		// Breakout confirmed
		h.wyckoffPhase[symbol] = PhaseSignOfStrength
	}
	
	h.logger.Debug("Wyckoff phase",
		zap.String("symbol", symbol),
		zap.String("phase", h.getPhaseName(h.wyckoffPhase[symbol])),
	)
}

// buildPosition gradually builds position during accumulation
func (h *AccumulationStateHandler) buildPosition(
	symbol string,
	currentPrice float64,
	regimeSnapshot RegimeSnapshot,
) {
	phase := h.wyckoffPhase[symbol]
	
	// Position sizing based on phase
	var buildRate float64
	switch phase {
	case PhasePreliminarySupport:
		buildRate = 0.2 // 20% of target
	case PhaseSellingClimax:
		buildRate = 0.3 // 30% of target
	case PhaseAutomaticRally:
		buildRate = 0.1 // 10% of target
	case PhaseSecondaryTest:
		buildRate = 0.2 // 20% of target
	case PhaseSpring:
		buildRate = 0.2 // 20% of target
	default:
		buildRate = 0
	}
	
	// Increment position
	h.positionSize[symbol] += buildRate * 0.01 // Base unit
	
	h.logger.Debug("Building position in accumulation",
		zap.String("symbol", symbol),
		zap.String("phase", h.getPhaseName(phase)),
		zap.Float64("current_size", h.positionSize[symbol]),
		zap.Float64("build_rate", buildRate),
	)
}

// isBreakoutDetected checks if price has broken out of accumulation range
func (h *AccumulationStateHandler) isBreakoutDetected(
	symbol string,
	currentPrice float64,
	regimeSnapshot RegimeSnapshot,
) bool {
	entryPrice := h.entryPrice[symbol]
	
	// Breakout: price > entry + threshold
	if currentPrice > entryPrice*(1+h.breakoutThreshold) {
		return true
	}
	
	// Also check for strong momentum
	if regimeSnapshot.ADX > 30 && regimeSnapshot.BBWidth > 0.04 {
		return true
	}
	
	return false
}

// isSignOfStrength checks for Wyckoff SOS (Sign of Strength)
func (h *AccumulationStateHandler) isSignOfStrength(
	symbol string,
	regimeSnapshot RegimeSnapshot,
) bool {
	// SOS: Strong move on high volume
	// Would need volume data, using indicators as proxy
	if regimeSnapshot.ADX > 35 && regimeSnapshot.BBWidth > 0.05 {
		return true
	}
	
	return false
}

// isFailedAccumulation checks if accumulation pattern has failed
func (h *AccumulationStateHandler) isFailedAccumulation(
	symbol string,
	currentPrice float64,
	regimeSnapshot RegimeSnapshot,
) bool {
	entryPrice := h.entryPrice[symbol]
	
	// Failed: price drops significantly below entry without volume confirmation
	if currentPrice < entryPrice*0.95 && regimeSnapshot.ADX < 15 {
		return true
	}
	
	return false
}

// getPhaseName returns string representation of Wyckoff phase
func (h *AccumulationStateHandler) getPhaseName(phase WyckoffPhase) string {
	switch phase {
	case PhasePreliminarySupport:
		return "Preliminary Support"
	case PhaseSellingClimax:
		return "Selling Climax"
	case PhaseAutomaticRally:
		return "Automatic Rally"
	case PhaseSecondaryTest:
		return "Secondary Test"
	case PhaseSpring:
		return "Spring"
	case PhaseBreakout:
		return "Breakout"
	case PhaseSignOfStrength:
		return "Sign of Strength"
	default:
		return "Unknown"
	}
}

// GetWyckoffPhase returns current Wyckoff phase for symbol
func (h *AccumulationStateHandler) GetWyckoffPhase(symbol string) WyckoffPhase {
	return h.wyckoffPhase[symbol]
}

// GetPositionSize returns current position size
func (h *AccumulationStateHandler) GetPositionSize(symbol string) float64 {
	return h.positionSize[symbol]
}

// GetAccumulationTime returns time since accumulation started
func (h *AccumulationStateHandler) GetAccumulationTime(symbol string) time.Duration {
	if entryTime, ok := h.accumulationTime[symbol]; ok {
		return time.Since(entryTime)
	}
	return 0
}
