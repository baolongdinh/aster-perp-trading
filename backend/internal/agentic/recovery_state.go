package agentic

import (
	"context"
	"time"

	"aster-bot/internal/realtime"

	"go.uber.org/zap"
)

// RecoveryStateHandler handles the RECOVERY state logic
// Loss analysis and parameter adjustment after defensive exits
type RecoveryStateHandler struct {
	logger      *zap.Logger
	scoreEngine *ScoreCalculationEngine

	// Recovery tracking
	exitPnL       map[string]float64
	exitReason    map[string]string
	recoveryStart map[string]time.Time
	adjustments   map[string]*ParameterAdjustments

	// Parameters
	maxRecoveryTime      time.Duration
	minCooldownTime      time.Duration
	maxConsecutiveLosses int
}

// ParameterAdjustments stores adjusted parameters based on loss analysis
type ParameterAdjustments struct {
	GridSizeMultiplier float64
	TrendThresholdAdj  float64
	PositionSizeAdj    float64
	StopLossAdj        float64
}

// NewRecoveryStateHandler creates a new RECOVERY state handler
func NewRecoveryStateHandler(
	scoreEngine *ScoreCalculationEngine,
	logger *zap.Logger,
) *RecoveryStateHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RecoveryStateHandler{
		logger:               logger.With(zap.String("state_handler", "RECOVERY")),
		scoreEngine:          scoreEngine,
		exitPnL:              make(map[string]float64),
		exitReason:           make(map[string]string),
		recoveryStart:        make(map[string]time.Time),
		adjustments:          make(map[string]*ParameterAdjustments),
		maxRecoveryTime:      5 * time.Minute,
		minCooldownTime:      2 * time.Minute,
		maxConsecutiveLosses: 3,
	}
}

// HandleState executes the RECOVERY state strategy
func (h *RecoveryStateHandler) HandleState(
	ctx context.Context,
	symbol string,
	regimeSnapshot RegimeSnapshot,
	snapshot realtime.SymbolRuntimeSnapshot,
	exitPnL float64,
	exitReason string,
	consecutiveLosses int,
) (*StateTransition, error) {
	h.logger.Debug("Executing RECOVERY state strategy",
		zap.String("symbol", symbol),
		zap.Float64("exit_pnl", exitPnL),
		zap.String("reason", exitReason),
	)

	// Initialize if new
	if _, ok := h.recoveryStart[symbol]; !ok {
		h.recoveryStart[symbol] = time.Now()
		h.exitPnL[symbol] = exitPnL
		h.exitReason[symbol] = exitReason

		// Calculate parameter adjustments based on loss analysis
		h.adjustments[symbol] = h.calculateAdjustments(exitPnL, exitReason, consecutiveLosses)

		h.logger.Info("Recovery mode started",
			zap.String("symbol", symbol),
			zap.Float64("exit_pnl", exitPnL),
			zap.String("reason", exitReason),
			zap.Int("consecutive_losses", consecutiveLosses),
		)
	}

	// 1. Check cooldown period (Phase 4: Smart Cooldown)
	cooldown := h.minCooldownTime
	if regimeSnapshot.ATR14 > 0.005 {
		cooldown *= 2 // Double cooldown in high volatility
	}

	if time.Since(h.recoveryStart[symbol]) < cooldown {
		return nil, nil
	}

	// 2. Hard timeout
	if time.Since(h.recoveryStart[symbol]) > h.maxRecoveryTime {
		h.clearRecoveryData(symbol)
		return &StateTransition{
			FromState:         TradingModeRecovery,
			ToState:           TradingModeIdle,
			Trigger:           "recovery_timeout",
			Score:             0.6,
			SmoothingDuration: 5 * time.Second,
			Timestamp:         time.Now(),
		}, nil
	}

	// 3. Check market readiness
	marketReady := h.isMarketReadyForReentry(symbol, regimeSnapshot)

	// 4. Determine severity and next state
	severity := h.assessLossSeverity(exitPnL, exitReason, consecutiveLosses)

	switch severity {
	case "minor":
		// Minor loss - can re-enter with adjusted parameters
		if marketReady {
			h.clearRecoveryData(symbol)
			return &StateTransition{
				FromState:         TradingModeRecovery,
				ToState:           TradingModeEnterGrid,
				Trigger:           "recovery_complete",
				Score:             0.7,
				SmoothingDuration: 5 * time.Second,
				Timestamp:         time.Now(),
			}, nil
		}

	case "moderate":
		// Moderate loss - go to IDLE first, wait for better conditions
		if marketReady && time.Since(h.recoveryStart[symbol]) > h.maxRecoveryTime/2 {
			h.clearRecoveryData(symbol)
			return &StateTransition{
				FromState:         TradingModeRecovery,
				ToState:           TradingModeIdle,
				Trigger:           "recovery_to_idle",
				Score:             0.6,
				SmoothingDuration: 5 * time.Second,
				Timestamp:         time.Now(),
			}, nil
		}

	case "severe":
		// Severe loss - stay in recovery, maybe skip this symbol
		if time.Since(h.recoveryStart[symbol]) > h.maxRecoveryTime {
			h.clearRecoveryData(symbol)
			return &StateTransition{
				FromState:         TradingModeRecovery,
				ToState:           TradingModeIdle,
				Trigger:           "recovery_timeout",
				Score:             0.5,
				SmoothingDuration: 10 * time.Second,
				Timestamp:         time.Now(),
			}, nil
		}
	}

	return nil, nil
}

// calculateAdjustments determines parameter adjustments based on loss
func (h *RecoveryStateHandler) calculateAdjustments(
	exitPnL float64,
	exitReason string,
	consecutiveLosses int,
) *ParameterAdjustments {
	adj := &ParameterAdjustments{
		GridSizeMultiplier: 1.0,
		TrendThresholdAdj:  1.0,
		PositionSizeAdj:    1.0,
		StopLossAdj:        1.0,
	}

	// Reduce position size based on loss magnitude
	if exitPnL < -0.02 { // > 2% loss
		adj.PositionSizeAdj = 0.6 // Reduce to 60%
		adj.GridSizeMultiplier = 0.8
	} else if exitPnL < -0.01 { // 1-2% loss
		adj.PositionSizeAdj = 0.8
		adj.GridSizeMultiplier = 0.9
	}

	// Adjust based on exit reason
	switch exitReason {
	case "stop_loss":
		adj.StopLossAdj = 1.3 // Widen stop by 30%
	case "max_loss":
		adj.PositionSizeAdj *= 0.9 // Further reduce
	case "volatility_spike":
		adj.TrendThresholdAdj = 1.2 // Require stronger trend signal
	}

	// Reduce based on consecutive losses
	if consecutiveLosses >= h.maxConsecutiveLosses {
		adj.PositionSizeAdj *= 0.5 // Halve position size
		adj.GridSizeMultiplier *= 0.7
	} else if consecutiveLosses >= 2 {
		adj.PositionSizeAdj *= 0.9
	}

	return adj
}

// isMarketReadyForReentry checks if market conditions allow re-entry
func (h *RecoveryStateHandler) isMarketReadyForReentry(
	symbol string,
	regimeSnapshot RegimeSnapshot,
) bool {
	// Wait for volatility to normalize
	if regimeSnapshot.ATR14 > 0.01 {
		return false
	}

	// Wait for clear regime (not volatile/uncertain)
	if regimeSnapshot.Regime == RegimeVolatile || regimeSnapshot.Confidence < 0.5 {
		return false
	}

	// Good ADX for trend detection
	if regimeSnapshot.ADX < 15 {
		return false
	}

	return true
}

// assessLossSeverity categorizes loss severity
func (h *RecoveryStateHandler) assessLossSeverity(
	exitPnL float64,
	exitReason string,
	consecutiveLosses int,
) string {
	// Check consecutive losses first
	if consecutiveLosses >= h.maxConsecutiveLosses {
		return "severe"
	}

	// Check PnL
	if exitPnL < -0.03 { // > 3% loss
		return "severe"
	}
	if exitPnL < -0.02 { // 2-3% loss
		return "moderate"
	}

	// Check exit reason
	if exitReason == "emergency_exit" || exitReason == "flash_crash" {
		return "severe"
	}

	return "minor"
}

// clearRecoveryData clears recovery tracking data
func (h *RecoveryStateHandler) clearRecoveryData(symbol string) {
	delete(h.exitPnL, symbol)
	delete(h.exitReason, symbol)
	delete(h.recoveryStart, symbol)
	delete(h.adjustments, symbol)
}

// GetAdjustments returns calculated parameter adjustments
func (h *RecoveryStateHandler) GetAdjustments(symbol string) *ParameterAdjustments {
	return h.adjustments[symbol]
}

// GetRecoveryTime returns time since recovery started
func (h *RecoveryStateHandler) GetRecoveryTime(symbol string) time.Duration {
	if start, ok := h.recoveryStart[symbol]; ok {
		return time.Since(start)
	}
	return 0
}
