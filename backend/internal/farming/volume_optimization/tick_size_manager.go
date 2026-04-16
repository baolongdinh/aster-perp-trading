package volume_optimization

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TickSizeManager manages tick-size information for trading symbols
type TickSizeManager struct {
	tickSizes map[string]float64 // symbol -> tick size
	mu        sync.RWMutex
	logger    *zap.Logger
}

// NewTickSizeManager creates a new tick-size manager
func NewTickSizeManager(logger *zap.Logger) *TickSizeManager {
	return &TickSizeManager{
		tickSizes: make(map[string]float64),
		logger:    logger,
	}
}

// SetTickSize sets the tick-size for a symbol
func (t *TickSizeManager) SetTickSize(symbol string, tickSize float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.tickSizes[symbol] = tickSize
	t.logger.Debug("Tick-size set",
		zap.String("symbol", symbol),
		zap.Float64("tick_size", tickSize))
}

// GetTickSize returns the tick-size for a symbol
// Returns default (0.01) if not found
func (t *TickSizeManager) GetTickSize(symbol string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if tickSize, exists := t.tickSizes[symbol]; exists {
		return tickSize
	}

	// Default tick-size for unknown symbols
	t.logger.Warn("Unknown tick-size, using default",
		zap.String("symbol", symbol),
		zap.Float64("default_tick_size", 0.01))
	return 0.01
}

// RoundToTick rounds a price to the nearest valid tick
func (t *TickSizeManager) RoundToTick(price, tickSize float64) float64 {
	if tickSize <= 0 {
		return price
	}
	return math.Round(price/tickSize) * tickSize
}

// RoundToTickForSymbol rounds a price to the nearest valid tick for a specific symbol
func (t *TickSizeManager) RoundToTickForSymbol(symbol string, price float64) float64 {
	tickSize := t.GetTickSize(symbol)
	return t.RoundToTick(price, tickSize)
}

// RefreshTickSizes refreshes tick-sizes from exchange API (placeholder)
// In production, this would call the exchange API to fetch current tick-sizes
func (t *TickSizeManager) RefreshTickSizes() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Placeholder: In production, fetch from exchange API
	// For now, set common tick-sizes manually
	commonTickSizes := map[string]float64{
		"BTCUSD1":  0.1,
		"ETHUSD1":  0.01,
		"SOLUSD1":  0.001,
		"XRPUSD1":  0.0001,
		"DOGEUSD1": 0.00001,
	}

	for symbol, tickSize := range commonTickSizes {
		t.tickSizes[symbol] = tickSize
	}

	t.logger.Info("Tick-sizes refreshed", zap.Int("count", len(t.tickSizes)))
	return nil
}

// StartPeriodicRefresh starts a goroutine to periodically refresh tick-sizes
func (t *TickSizeManager) StartPeriodicRefresh(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := t.RefreshTickSizes(); err != nil {
				t.logger.Error("Failed to refresh tick-sizes", zap.Error(err))
			}
		case <-ctx.Done():
			t.logger.Info("Tick-size refresh stopped")
			return
		}
	}
}

// GetAllTickSizes returns a copy of all tick-sizes
func (t *TickSizeManager) GetAllTickSizes() map[string]float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]float64)
	for k, v := range t.tickSizes {
		result[k] = v
	}
	return result
}
