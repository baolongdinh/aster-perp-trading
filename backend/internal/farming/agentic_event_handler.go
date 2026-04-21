package farming

import (
	"context"
	"time"

	"aster-bot/internal/agentic"
	"go.uber.org/zap"
)

// AgenticEventHandler handles state transition events from AgenticEngine
// This implements the hybrid integration: Agentic decides WHAT, VF decides HOW
type AgenticEventHandler struct {
	engine *VolumeFarmEngine
	logger *zap.Logger

	// Track current state per symbol
	currentStates map[string]agentic.TradingMode
}

// NewAgenticEventHandler creates a new event handler
func NewAgenticEventHandler(engine *VolumeFarmEngine, logger *zap.Logger) *AgenticEventHandler {
	return &AgenticEventHandler{
		engine:        engine,
		logger:        logger.With(zap.String("component", "agentic_event_handler")),
		currentStates: make(map[string]agentic.TradingMode),
	}
}

// GetHandlerName returns the name of this handler
func (h *AgenticEventHandler) GetHandlerName() string {
	return "VolumeFarmEngine"
}

// HandleStateTransition processes state transition events
func (h *AgenticEventHandler) HandleStateTransition(ctx context.Context, event agentic.StateTransitionEvent) error {
	h.logger.Info("Processing state transition",
		zap.String("symbol", event.Symbol),
		zap.String("from", string(event.FromState)),
		zap.String("to", string(event.ToState)),
		zap.String("trigger", event.Trigger),
		zap.Float64("score", event.Score),
	)

	// Track start time
	start := time.Now()

	var err error

	// Execute based on target state
	switch event.ToState {
	case agentic.TradingModeGrid:
		err = h.engine.ExecuteGridEntry(ctx, event.Symbol, event.Params)

	case agentic.TradingModeTrending:
		err = h.engine.ExecuteTrendEntry(ctx, event.Symbol, event.Params)

	case agentic.TradingModeAccumulation:
		err = h.engine.ExecuteAccumulation(ctx, event.Symbol, event.Params)

	case agentic.TradingModeDefensive:
		exitPct := event.Params.ExitPercentage
		if exitPct == 0 {
			exitPct = 1.0 // Default full exit
		}
		err = h.engine.ExecuteDefensive(ctx, event.Symbol, exitPct, event.Trigger)

	case agentic.TradingModeOverSize:
		exitPct := event.Params.ExitPercentage
		if exitPct == 0 {
			exitPct = 0.5 // Default 50% reduction
		}
		err = h.engine.ExecuteDefensive(ctx, event.Symbol, exitPct, "over_size")

	case agentic.TradingModeRecovery:
		err = h.engine.ExecuteRecovery(ctx, event.Symbol, event.Params)

	case agentic.TradingModeIdle:
		err = h.engine.ExecuteIdle(ctx, event.Symbol)

	default:
		h.logger.Warn("Unknown target state", zap.String("state", string(event.ToState)))
	}

	// Build result
	result := agentic.ExecutionResult{
		Symbol:      event.Symbol,
		ToState:     string(event.ToState),
		Timestamp:   time.Now(),
		ExecutionID: "",
		Success:     err == nil,
		Trigger:     event.Trigger,
		ExitReason:  event.Params.ExitReason,
	}

	if err != nil {
		result.Error = err.Error()
		h.logger.Error("State transition execution failed",
			zap.String("symbol", event.Symbol),
			zap.String("to_state", string(event.ToState)),
			zap.Error(err),
		)
	} else {
		h.logger.Info("State transition executed successfully",
			zap.String("symbol", event.Symbol),
			zap.String("to_state", string(event.ToState)),
			zap.Duration("duration", time.Since(start)),
		)
		// Update tracked state
		h.currentStates[event.Symbol] = event.ToState
	}

	h.engine.PublishExecutionResult(ctx, h.engine.GetStateEventBus(), result)

	return err
}

// GetCurrentState returns the current state for a symbol
func (h *AgenticEventHandler) GetCurrentState(symbol string) agentic.TradingMode {
	state, exists := h.currentStates[symbol]
	if !exists {
		return agentic.TradingModeIdle
	}
	return state
}
