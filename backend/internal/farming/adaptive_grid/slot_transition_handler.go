package adaptive_grid

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SlotTransitionHandler handles time slot transitions and manages grid operations
type SlotTransitionHandler struct {
	gridManager     GridManagerInterface
	adaptiveManager *AdaptiveGridManager
	logger          *zap.Logger
	mu              sync.RWMutex

	// Track transition cooldown per symbol
	lastTransition map[string]time.Time
	cooldownPeriod time.Duration
}

// NewSlotTransitionHandler creates a new slot transition handler
func NewSlotTransitionHandler(
	gridManager GridManagerInterface,
	adaptiveManager *AdaptiveGridManager,
	logger *zap.Logger,
) *SlotTransitionHandler {
	return &SlotTransitionHandler{
		gridManager:     gridManager,
		adaptiveManager: adaptiveManager,
		logger:          logger,
		lastTransition:  make(map[string]time.Time),
		cooldownPeriod:  2 * time.Minute, // 2 minute cooldown between transitions
	}
}

// SetCooldownPeriod sets the cooldown period between transitions
func (h *SlotTransitionHandler) SetCooldownPeriod(period time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cooldownPeriod = period
}

// CanTransition checks if transition is allowed (cooldown expired)
func (h *SlotTransitionHandler) CanTransition(symbol string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	lastTransition, exists := h.lastTransition[symbol]
	if !exists {
		return true
	}

	return time.Since(lastTransition) >= h.cooldownPeriod
}

// HandleTransition handles the full transition process for a symbol
func (h *SlotTransitionHandler) HandleTransition(
	ctx context.Context,
	symbol string,
	oldSlot, newSlot *TimeSlotConfig,
) error {
	// Check cooldown
	if !h.CanTransition(symbol) {
		h.logger.Info("Transition cooldown active, skipping",
			zap.String("symbol", symbol),
			zap.Duration("cooldown", h.cooldownPeriod))
		return nil
	}

	// Log transition details
	if oldSlot != nil && newSlot != nil {
		h.logger.Info("Handling slot transition",
			zap.String("symbol", symbol),
			zap.String("from", oldSlot.Description),
			zap.String("to", newSlot.Description),
			zap.Bool("old_enabled", oldSlot.Enabled),
			zap.Bool("new_enabled", newSlot.Enabled),
			zap.Float64("new_size_mult", newSlot.SizeMultiplier),
			zap.Float64("new_spread_mult", newSlot.SpreadMultiplier))
	} else if newSlot != nil {
		h.logger.Info("Entering time slot",
			zap.String("symbol", symbol),
			zap.String("slot", newSlot.Description),
			zap.Bool("enabled", newSlot.Enabled))
	} else if oldSlot != nil {
		h.logger.Info("Exiting time slot",
			zap.String("symbol", symbol),
			zap.String("slot", oldSlot.Description))
	}

	// Handle based on new slot state
	if newSlot == nil || !newSlot.Enabled {
		// New slot is disabled - just cancel orders, don't rebuild
		return h.handleDisabledTransition(ctx, symbol, oldSlot, newSlot)
	}

	// New slot is enabled - cancel, clear, and rebuild with new params
	return h.handleEnabledTransition(ctx, symbol, oldSlot, newSlot)
}

// handleDisabledTransition handles transition to a disabled slot
func (h *SlotTransitionHandler) handleDisabledTransition(
	ctx context.Context,
	symbol string,
	oldSlot, newSlot *TimeSlotConfig,
) error {
	h.logger.Info("Transitioning to disabled slot - cancelling orders",
		zap.String("symbol", symbol))

	// 1. Cancel all existing orders
	if err := h.gridManager.CancelAllOrders(ctx, symbol); err != nil {
		h.logger.Error("Failed to cancel orders during disabled transition",
			zap.String("symbol", symbol),
			zap.Error(err))
		// Continue anyway - don't block
	}

	// 2. Clear the grid
	if err := h.gridManager.ClearGrid(ctx, symbol); err != nil {
		h.logger.Error("Failed to clear grid during disabled transition",
			zap.String("symbol", symbol),
			zap.Error(err))
		// Continue anyway
	}

	// 3. Pause trading via adaptive manager
	if h.adaptiveManager != nil {
		h.adaptiveManager.pauseTrading(symbol)
	}

	// 4. Update transition timestamp
	h.updateTransitionTime(symbol)

	h.logger.Info("Disabled transition complete - trading paused",
		zap.String("symbol", symbol))

	return nil
}

// handleEnabledTransition handles transition to an enabled slot
func (h *SlotTransitionHandler) handleEnabledTransition(
	ctx context.Context,
	symbol string,
	oldSlot, newSlot *TimeSlotConfig,
) error {
	h.logger.Info("Transitioning to enabled slot - rebuilding grid",
		zap.String("symbol", symbol),
		zap.String("slot", newSlot.Description),
		zap.Float64("size_multiplier", newSlot.SizeMultiplier),
		zap.Float64("spread_multiplier", newSlot.SpreadMultiplier))

	// 1. Cancel all existing orders
	if err := h.gridManager.CancelAllOrders(ctx, symbol); err != nil {
		h.logger.Error("Failed to cancel orders during enabled transition",
			zap.String("symbol", symbol),
			zap.Error(err))
		// Continue anyway - we'll try to rebuild anyway
	}

	// 2. Clear the grid
	if err := h.gridManager.ClearGrid(ctx, symbol); err != nil {
		h.logger.Error("Failed to clear grid during enabled transition",
			zap.String("symbol", symbol),
			zap.Error(err))
		// Continue anyway
	}

	// 3. Apply new slot parameters via adaptive manager
	if h.adaptiveManager != nil {
		// Resume trading if it was paused
		h.adaptiveManager.resumeTrading(symbol)

		// Apply size multiplier if available
		if newSlot.SizeMultiplier > 0 {
			// The actual size calculation happens in GetAdaptiveOrderSize
			// which will use GetSizeMultiplier() from timeFilter
			h.logger.Info("Grid will use new size multiplier",
				zap.String("symbol", symbol),
				zap.Float64("multiplier", newSlot.SizeMultiplier))
		}

		// Apply spread multiplier if available
		if newSlot.SpreadMultiplier > 0 {
			// The spread calculation happens dynamically
			h.logger.Info("Grid will use new spread multiplier",
				zap.String("symbol", symbol),
				zap.Float64("multiplier", newSlot.SpreadMultiplier))
		}
	}

	// 4. Rebuild the grid with new parameters
	if err := h.gridManager.RebuildGrid(ctx, symbol); err != nil {
		return fmt.Errorf("failed to rebuild grid during transition: %w", err)
	}

	// 5. Update transition timestamp
	h.updateTransitionTime(symbol)

	h.logger.Info("Enabled transition complete - grid rebuilt",
		zap.String("symbol", symbol),
		zap.String("slot", newSlot.Description))

	return nil
}

// updateTransitionTime updates the last transition timestamp for a symbol
func (h *SlotTransitionHandler) updateTransitionTime(symbol string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastTransition[symbol] = time.Now()
}

// GetLastTransition returns the last transition time for a symbol
func (h *SlotTransitionHandler) GetLastTransition(symbol string) (time.Time, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	time, exists := h.lastTransition[symbol]
	return time, exists
}

// GetTransitionStatus returns the cooldown status for a symbol
func (h *SlotTransitionHandler) GetTransitionStatus(symbol string) map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	lastTransition, exists := h.lastTransition[symbol]
	canTransition := true
	timeRemaining := time.Duration(0)

	if exists {
		elapsed := time.Since(lastTransition)
		if elapsed < h.cooldownPeriod {
			canTransition = false
			timeRemaining = h.cooldownPeriod - elapsed
		}
	}

	return map[string]interface{}{
		"can_transition":  canTransition,
		"cooldown_period": h.cooldownPeriod.String(),
		"time_remaining":  timeRemaining.String(),
		"last_transition": lastTransition,
	}
}
