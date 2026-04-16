package volume_optimization

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// InventoryHedgeManager manages inventory hedging for skewed positions
type InventoryHedgeManager struct {
	enabled        bool
	tickSizeMgr    interface {
		GetTickSize(symbol string) float64
		RoundToTickForSymbol(symbol string, price float64) float64
	}
	hedgeThreshold float64 // Skew threshold to trigger hedge (e.g., 0.3 = 30%)
	hedgeRatio      float64 // % of skew to hedge (e.g., 0.3 = 30%)
	maxHedgeSize    float64 // Max hedge size in base currency
	hedgingMode     string  // "internal", "cross_pair", "scalping"
	hedgePair       string  // Symbol to hedge with (for cross_pair mode)
	
	// Track inventory state
	inventorySkew map[string]float64 // symbol -> skew ratio (-1 to 1, negative = short skew)
	hedgePositions map[string]float64  // symbol -> current hedge size
	
	// Callbacks
	onPlaceHedgeOrder func(ctx context.Context, symbol, side string, size float64) error
	
	mu     sync.RWMutex
	logger *zap.Logger
	stopCh chan struct{}
}

// InventoryHedgeConfig holds configuration for inventory hedging
type InventoryHedgeConfig struct {
	Enabled        bool
	HedgeThreshold float64       // Skew threshold (0-1)
	HedgeRatio     float64       // Hedge ratio (0-1)
	MaxHedgeSize   float64       // Max hedge size
	HedgingMode    string        // "internal", "cross_pair", "scalping"
	HedgePair      string        // For cross_pair mode
	CheckInterval  time.Duration // How often to check inventory
}

// NewInventoryHedgeManager creates a new inventory hedge manager
func NewInventoryHedgeManager(config InventoryHedgeConfig, logger *zap.Logger) *InventoryHedgeManager {
	if config.HedgeThreshold == 0 {
		config.HedgeThreshold = 0.3 // 30% default
	}
	if config.HedgeRatio == 0 {
		config.HedgeRatio = 0.3 // 30% default
	}
	if config.MaxHedgeSize == 0 {
		config.MaxHedgeSize = 100.0 // Default 100 units
	}
	if config.CheckInterval == 0 {
		config.CheckInterval = 30 * time.Second
	}
	
	return &InventoryHedgeManager{
		enabled:        config.Enabled,
		tickSizeMgr:    nil,
		hedgeThreshold: config.HedgeThreshold,
		hedgeRatio:     config.HedgeRatio,
		maxHedgeSize:   config.MaxHedgeSize,
		hedgingMode:    config.HedgingMode,
		hedgePair:      config.HedgePair,
		inventorySkew:  make(map[string]float64),
		hedgePositions: make(map[string]float64),
		logger:         logger,
		stopCh:         make(chan struct{}),
	}
}

// Start begins the background monitoring
func (h *InventoryHedgeManager) Start(ctx context.Context) {
	if !h.enabled {
		h.logger.Info("InventoryHedgeManager disabled, not starting")
		return
	}
	
	h.logger.Info("Starting InventoryHedgeManager",
		zap.Float64("hedge_threshold", h.hedgeThreshold),
		zap.Float64("hedge_ratio", h.hedgeRatio),
		zap.String("hedging_mode", h.hedgingMode))
	
	go h.monitorLoop(ctx)
}

// Stop stops the background monitoring
func (h *InventoryHedgeManager) Stop() {
	close(h.stopCh)
	h.logger.Info("InventoryHedgeManager stopped")
}

// monitorLoop runs continuous inventory monitoring
func (h *InventoryHedgeManager) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Check every 30s
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("InventoryHedgeManager context cancelled")
			return
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.checkAndHedgeAll(ctx)
		}
	}
}

// UpdateInventorySkew updates the current inventory skew for a symbol
// skewRatio: -1 to 1, where -1 = 100% short, 1 = 100% long
func (h *InventoryHedgeManager) UpdateInventorySkew(symbol string, longSize, shortSize float64) {
	totalSize := longSize + shortSize
	if totalSize == 0 {
		return
	}
	
	// Calculate skew: positive = long skew, negative = short skew
	skewRatio := (longSize - shortSize) / totalSize
	
	h.mu.Lock()
	h.inventorySkew[symbol] = skewRatio
	h.mu.Unlock()
	
	h.logger.Debug("Inventory skew updated",
		zap.String("symbol", symbol),
		zap.Float64("long_size", longSize),
		zap.Float64("short_size", shortSize),
		zap.Float64("skew_ratio", skewRatio))
}

// checkAndHedgeAll checks all symbols and hedges if needed
func (h *InventoryHedgeManager) checkAndHedgeAll(ctx context.Context) {
	h.mu.RLock()
	symbols := make([]string, 0, len(h.inventorySkew))
	for symbol := range h.inventorySkew {
		symbols = append(symbols, symbol)
	}
	h.mu.RUnlock()
	
	for _, symbol := range symbols {
		h.checkAndHedge(ctx, symbol)
	}
}

// checkAndHedge checks a single symbol and hedges if threshold exceeded
func (h *InventoryHedgeManager) checkAndHedge(ctx context.Context, symbol string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	skewRatio, exists := h.inventorySkew[symbol]
	if !exists {
		return
	}
	
	// Check if skew exceeds threshold
	if math.Abs(skewRatio) < h.hedgeThreshold {
		return // Within normal range, no hedge needed
	}
	
	// Calculate hedge size
	currentHedge := h.hedgePositions[symbol]
	targetHedge := h.calculateTargetHedge(symbol, skewRatio)
	
	// Only hedge if we need to increase hedge position
	if targetHedge > currentHedge {
		hedgeSize := targetHedge - currentHedge
		if hedgeSize > h.maxHedgeSize {
			hedgeSize = h.maxHedgeSize
		}
		
		// Determine hedge side (opposite of skew)
		hedgeSide := "SELL"
		if skewRatio > 0 {
			// Long skew - hedge by selling
			hedgeSide = "SELL"
		} else {
			// Short skew - hedge by buying
			hedgeSide = "BUY"
		}
		
		h.logger.Info("Hedge required",
			zap.String("symbol", symbol),
			zap.Float64("skew_ratio", skewRatio),
			zap.String("hedge_side", hedgeSide),
			zap.Float64("hedge_size", hedgeSize))
		
		// Place hedge order
		if h.onPlaceHedgeOrder != nil {
			if err := h.onPlaceHedgeOrder(ctx, symbol, hedgeSide, hedgeSize); err != nil {
				h.logger.Error("Failed to place hedge order",
					zap.String("symbol", symbol),
					zap.Error(err))
				return
			}
		}
		
		h.hedgePositions[symbol] = targetHedge
		h.logger.Info("Hedge position updated",
			zap.String("symbol", symbol),
			zap.Float64("hedge_size", targetHedge))
	}
}

// calculateTargetHedge calculates the target hedge size based on skew
func (h *InventoryHedgeManager) calculateTargetHedge(symbol string, skewRatio float64) float64 {
	// Hedge ratio of the skew
	skewAbs := math.Abs(skewRatio)
	excessSkew := skewAbs - h.hedgeThreshold // Only hedge excess beyond threshold
	
	if excessSkew <= 0 {
		return 0
	}
	
	targetHedge := excessSkew * h.hedgeRatio * h.maxHedgeSize
	return math.Min(targetHedge, h.maxHedgeSize)
}

// SetCallbacks sets the callback functions
func (h *InventoryHedgeManager) SetCallbacks(
	onPlaceHedgeOrder func(ctx context.Context, symbol, side string, size float64) error,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.onPlaceHedgeOrder = onPlaceHedgeOrder
	h.logger.Info("InventoryHedgeManager callbacks set")
}

// SetTickSizeManager sets the tick size manager
func (h *InventoryHedgeManager) SetTickSizeManager(tickSizeMgr interface {
	GetTickSize(symbol string) float64
	RoundToTickForSymbol(symbol string, price float64) float64
}) {
	h.tickSizeMgr = tickSizeMgr
	h.logger.Info("TickSizeManager set on InventoryHedgeManager")
}

// IsEnabled returns whether inventory hedging is enabled
func (h *InventoryHedgeManager) IsEnabled() bool {
	return h.enabled
}

// SetEnabled enables/disables inventory hedging
func (h *InventoryHedgeManager) SetEnabled(enabled bool) {
	h.enabled = enabled
	h.logger.Info("Inventory hedging enabled state changed",
		zap.Bool("enabled", enabled))
}

// GetStats returns current statistics
func (h *InventoryHedgeManager) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	return map[string]interface{}{
		"enabled":          h.enabled,
		"hedge_threshold":  h.hedgeThreshold,
		"hedge_ratio":      h.hedgeRatio,
		"max_hedge_size":   h.maxHedgeSize,
		"hedging_mode":     h.hedgingMode,
		"hedge_pair":       h.hedgePair,
		"tracked_symbols":  len(h.inventorySkew),
		"active_hedges":    len(h.hedgePositions),
	}
}
