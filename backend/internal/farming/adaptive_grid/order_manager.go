package adaptive_grid

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/farming/market_regime"

	"go.uber.org/zap"
)

// OrderManager handles order cancellation and rebuilding during transitions
type OrderManager struct {
	gridManager     GridManagerInterface
	adaptiveManager *AdaptiveGridManager
	logger          *zap.Logger
	mu              sync.RWMutex
}

// NewOrderManager creates a new order manager
func NewOrderManager(
	gridManager GridManagerInterface,
	adaptiveManager *AdaptiveGridManager,
	logger *zap.Logger,
) *OrderManager {
	return &OrderManager{
		gridManager:     gridManager,
		adaptiveManager: adaptiveManager,
		logger:          logger,
		mu:              sync.RWMutex{},
	}
}

// CancelAndRebuild cancels existing orders and rebuilds grid for regime change
func (o *OrderManager) CancelAndRebuild(
	ctx context.Context,
	symbol string,
	oldRegime, newRegime market_regime.MarketRegime,
) error {
	o.logger.Info("Starting order cancellation and rebuild process",
		zap.String("symbol", symbol),
		zap.String("from", string(oldRegime)),
		zap.String("to", string(newRegime)))

	// Phase 1: Cancel all existing orders
	if err := o.cancelAllOrders(ctx, symbol); err != nil {
		return fmt.Errorf("failed to cancel orders: %w", err)
	}

	// Phase 2: Rebuild grid with new regime
	if err := o.rebuildGridForRegime(ctx, symbol, newRegime); err != nil {
		return fmt.Errorf("failed to rebuild grid: %w", err)
	}

	o.logger.Info("Order cancellation and rebuild completed successfully",
		zap.String("symbol", symbol),
		zap.String("final_regime", string(newRegime)))

	return nil
}

// cancelAllOrders cancels all orders for a symbol
func (o *OrderManager) cancelAllOrders(ctx context.Context, symbol string) error {
	o.logger.Info("Cancelling all orders for symbol", zap.String("symbol", symbol))

	// Get active positions
	positions, err := o.gridManager.GetActivePositions(symbol)
	if err != nil {
		return err
	}

	// Cancel orders for each position
	for i, position := range positions {
		_ = position
		if err := o.gridManager.CancelAllOrders(ctx, symbol); err != nil {
			o.logger.Error("Failed to cancel orders for position",
				zap.Int("position_index", i),
				zap.String("symbol", symbol),
				zap.Error(err))
			return err
		}
	}

	o.logger.Info("All orders cancelled successfully", zap.String("symbol", symbol))
	return nil
}

// rebuildGridForRegime rebuilds grid with regime-specific parameters
func (o *OrderManager) rebuildGridForRegime(
	ctx context.Context,
	symbol string,
	regime market_regime.MarketRegime,
) error {
	o.logger.Info("Rebuilding grid for regime",
		zap.String("symbol", symbol),
		zap.String("regime", string(regime)))

	// Clear existing grid
	if err := o.gridManager.ClearGrid(ctx, symbol); err != nil {
		return err
	}

	// Apply regime-specific parameters using adaptive manager
	if err := o.adaptiveManager.HandleRegimeTransition(ctx, symbol, "", regime); err != nil {
		return err
	}

	// Rebuild grid
	if err := o.gridManager.RebuildGrid(ctx, symbol); err != nil {
		return err
	}

	o.logger.Info("Grid rebuilt successfully for regime",
		zap.String("symbol", symbol),
		zap.String("regime", string(regime)))

	return nil
}

// GetTransitionStatus returns transition status for a symbol
func (o *OrderManager) GetTransitionStatus(symbol string) (bool, time.Time) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	inCooldown := o.adaptiveManager.IsInTransitionCooldown(symbol)
	lastChange := o.adaptiveManager.lastRegimeChange[symbol]

	return inCooldown, lastChange
}
