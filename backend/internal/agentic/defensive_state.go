package agentic

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// DefensiveStateHandler handles the DEFENSIVE state logic
// Graduated exit with risk protection
type DefensiveStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine
	
	// Exit tracking
	exitStage      map[string]DefensiveExitStage
	entryPrice     map[string]float64
	exitTime       map[string]time.Time
	breakevenHit   map[string]bool
	
	// Parameters
	breakevenThreshold float64
	exitHalfPnL        float64
	exitAllPnL         float64
	recoveryThreshold  float64
}

// DefensiveExitStage represents the exit stage
type DefensiveExitStage int

const (
	ExitStageInitial DefensiveExitStage = iota
	ExitStageBreakeven
	ExitStageHalf
	ExitStageAll
	ExitStageRecovery
)

// NewDefensiveStateHandler creates a new DEFENSIVE state handler
func NewDefensiveStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *DefensiveStateHandler {
	return &DefensiveStateHandler{
		logger:             logger.With(zap.String("state_handler", "DEFENSIVE")),
		scoreEngine:        scoreEngine,
		exitStage:          make(map[string]DefensiveExitStage),
		entryPrice:         make(map[string]float64),
		exitTime:           make(map[string]time.Time),
		breakevenHit:      make(map[string]bool),
		breakevenThreshold: 0.005, // 0.5% breakeven
		exitHalfPnL:        0.01,  // 1% profit for half exit
		exitAllPnL:         0.02,  // 2% profit for all exit
		recoveryThreshold:  0.015, // 1.5% drop to trigger recovery
	}
}

// HandleState executes the DEFENSIVE state strategy
func (h *DefensiveStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	currentPrice float64,
	positionSize float64,
	unrealizedPnL float64,
) (*StateTransition, error) {
	h.logger.Debug("Executing DEFENSIVE state strategy",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
		zap.Float64("pnl", unrealizedPnL),
	)
	
	// Initialize if new
	if _, ok := h.exitTime[symbol]; !ok {
		h.exitTime[symbol] = time.Now()
		h.exitStage[symbol] = ExitStageInitial
		h.entryPrice[symbol] = currentPrice
	}
	
	// 1. Check for breakeven
	if unrealizedPnL > h.breakevenThreshold && !h.breakevenHit[symbol] {
		h.breakevenHit[symbol] = true
		h.exitStage[symbol] = ExitStageBreakeven
		
		h.logger.Info("Breakeven hit, setting breakeven stop",
			zap.String("symbol", symbol),
			zap.Float64("pnl", unrealizedPnL),
		)
		
		// Would set breakeven stop here
	}
	
	// 2. Execute graduated exit based on PnL
	if unrealizedPnL >= h.exitHalfPnL && h.exitStage[symbol] == ExitStageBreakeven {
		h.exitStage[symbol] = ExitStageHalf
		
		h.logger.Info("EXIT_HALF executed",
			zap.String("symbol", symbol),
			zap.Float64("pnl", unrealizedPnL),
		)
		
		// Execute half exit
		h.executeHalfExit(symbol, positionSize)
	}
	
	if unrealizedPnL >= h.exitAllPnL && (h.exitStage[symbol] == ExitStageHalf || h.exitStage[symbol] == ExitStageBreakeven) {
		h.exitStage[symbol] = ExitStageAll
		
		h.logger.Info("EXIT_ALL executed",
			zap.String("symbol", symbol),
			zap.Float64("pnl", unrealizedPnL),
		)
		
		// Exit all and return to IDLE
		return &StateTransition{
			FromState:         TradingModeDefensive,
			ToState:           TradingModeIdle,
			Trigger:           "exit_all",
			Score:             0.9,
			SmoothingDuration: 3 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// 3. Check for recovery opportunity
	if h.exitStage[symbol] == ExitStageHalf {
		if h.isRecoveryOpportunity(symbol, currentPrice, unrealizedPnL) {
			h.exitStage[symbol] = ExitStageRecovery
			
			h.logger.Info("Recovery opportunity detected",
				zap.String("symbol", symbol),
			)
			
			return &StateTransition{
				FromState:         TradingModeDefensive,
				ToState:           TradingModeTrending,
				Trigger:           "recovery",
				Score:             0.7,
				SmoothingDuration: 5 * time.Second,
				Timestamp:         time.Now(),
			}, nil
		}
	}
	
	// 4. Check for max loss (emergency exit)
	if unrealizedPnL < -0.02 { // -2% max loss in defensive
		h.logger.Warn("Max loss in defensive mode, emergency exit",
			zap.String("symbol", symbol),
			zap.Float64("pnl", unrealizedPnL),
		)
		
		return &StateTransition{
			FromState:         TradingModeDefensive,
			ToState:           TradingModeIdle,
			Trigger:           "emergency_exit",
			Score:             0.95,
			SmoothingDuration: 2 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// 5. Check for time limit (force exit after 30 min in defensive)
	if time.Since(h.exitTime[symbol]) > 30*time.Minute {
		h.logger.Info("Max time in defensive reached, forcing exit",
			zap.String("symbol", symbol),
		)
		
		return &StateTransition{
			FromState:         TradingModeDefensive,
			ToState:           TradingModeIdle,
			Trigger:           "time_limit",
			Score:             0.8,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}
	
	// Default: stay in DEFENSIVE
	return nil, nil
}

// executeHalfExit executes a 50% position exit
func (h *DefensiveStateHandler) executeHalfExit(symbol string, positionSize float64) {
	h.logger.Info("Executing half exit",
		zap.String("symbol", symbol),
		zap.Float64("position_size", positionSize),
		zap.Float64("exit_size", positionSize*0.5),
	)
	// Would integrate with actual order placement
}

// isRecoveryOpportunity checks if market conditions support recovery
func (h *DefensiveStateHandler) isRecoveryOpportunity(
	symbol string,
	currentPrice float64,
	unrealizedPnL float64,
) bool {
	entryPrice := h.entryPrice[symbol]
	
	// Recovery: price drops but then recovers with momentum
	// Simplified: check if PnL turned positive after being negative
	if unrealizedPnL > 0 && currentPrice > entryPrice*1.01 {
		return true
	}
	
	return false
}

// GetExitStage returns current exit stage
func (h *DefensiveStateHandler) GetExitStage(symbol string) DefensiveExitStage {
	return h.exitStage[symbol]
}

// IsBreakevenHit returns if breakeven has been hit
func (h *DefensiveStateHandler) IsBreakevenHit(symbol string) bool {
	return h.breakevenHit[symbol]
}

// GetExitTime returns time since exit started
func (h *DefensiveStateHandler) GetExitTime(symbol string) time.Duration {
	if exitTime, ok := h.exitTime[symbol]; ok {
		return time.Since(exitTime)
	}
	return 0
}
