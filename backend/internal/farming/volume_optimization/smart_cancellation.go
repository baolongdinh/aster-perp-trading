package volume_optimization

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SmartCancellationManager manages intelligent order cancellation when spread changes
type SmartCancellationManager struct {
	enabled               bool
	spreadChangeThreshold float64
	checkInterval         time.Duration
	
	// Track spread history per symbol
	spreadHistory map[string][]SpreadSnapshot
	lastCheck     map[string]time.Time
	
	// Callbacks
	onSpreadChange func(symbol string, oldSpread, newSpread float64, changePct float64)
	onCancelOrders func(ctx context.Context, symbol string) error
	onRebuildGrid  func(ctx context.Context, symbol string) error
	
	mu     sync.RWMutex
	logger *zap.Logger
	stopCh chan struct{}
}

// SpreadSnapshot captures spread at a point in time
type SpreadSnapshot struct {
	Timestamp time.Time
	BestBid   float64
	BestAsk   float64
	Spread    float64
	SpreadPct float64
}

// SmartCancelConfig holds configuration for smart cancellation
type SmartCancelConfig struct {
	Enabled               bool
	SpreadChangeThreshold float64
	CheckInterval         time.Duration
}

// NewSmartCancellationManager creates a new smart cancellation manager
func NewSmartCancellationManager(config SmartCancelConfig, logger *zap.Logger) *SmartCancellationManager {
	if config.CheckInterval == 0 {
		config.CheckInterval = 5 * time.Second
	}
	if config.SpreadChangeThreshold == 0 {
		config.SpreadChangeThreshold = 0.2 // 20% default
	}
	
	return &SmartCancellationManager{
		enabled:               config.Enabled,
		spreadChangeThreshold: config.SpreadChangeThreshold,
		checkInterval:         config.CheckInterval,
		spreadHistory:         make(map[string][]SpreadSnapshot),
		lastCheck:             make(map[string]time.Time),
		logger:                logger,
		stopCh:                make(chan struct{}),
	}
}

// Start begins the background monitoring goroutine
func (s *SmartCancellationManager) Start(ctx context.Context) {
	if !s.enabled {
		s.logger.Info("SmartCancellationManager disabled, not starting")
		return
	}
	
	s.logger.Info("Starting SmartCancellationManager",
		zap.Float64("spread_change_threshold", s.spreadChangeThreshold),
		zap.Duration("check_interval", s.checkInterval))
	
	go s.monitorLoop(ctx)
}

// Stop stops the background monitoring
func (s *SmartCancellationManager) Stop() {
	close(s.stopCh)
	s.logger.Info("SmartCancellationManager stopped")
}

// monitorLoop runs the continuous monitoring
func (s *SmartCancellationManager) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("SmartCancellationManager context cancelled")
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkAllSymbols(ctx)
		}
	}
}

// checkAllSymbols checks spread changes for all tracked symbols
func (s *SmartCancellationManager) checkAllSymbols(ctx context.Context) {
	s.mu.RLock()
	symbols := make([]string, 0, len(s.spreadHistory))
	for symbol := range s.spreadHistory {
		symbols = append(symbols, symbol)
	}
	s.mu.RUnlock()
	
	for _, symbol := range symbols {
		s.checkSpreadChange(ctx, symbol)
	}
}

// UpdateSpread updates the current spread for a symbol
func (s *SmartCancellationManager) UpdateSpread(symbol string, bestBid, bestAsk float64) {
	if !s.enabled {
		return
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	spread := bestAsk - bestBid
	midPrice := (bestBid + bestAsk) / 2
	spreadPct := 0.0
	if midPrice > 0 {
		spreadPct = spread / midPrice
	}
	
	snapshot := SpreadSnapshot{
		Timestamp: time.Now(),
		BestBid:   bestBid,
		BestAsk:   bestAsk,
		Spread:    spread,
		SpreadPct: spreadPct,
	}
	
	// Keep last 10 snapshots
	history := s.spreadHistory[symbol]
	if len(history) >= 10 {
		history = history[1:]
	}
	history = append(history, snapshot)
	s.spreadHistory[symbol] = history
	s.lastCheck[symbol] = time.Now()
	
	s.logger.Debug("Spread updated",
		zap.String("symbol", symbol),
		zap.Float64("bid", bestBid),
		zap.Float64("ask", bestAsk),
		zap.Float64("spread", spread),
		zap.Float64("spread_pct", spreadPct))
}

// checkSpreadChange checks if spread has changed significantly
func (s *SmartCancellationManager) checkSpreadChange(ctx context.Context, symbol string) {
	s.mu.RLock()
	history := s.spreadHistory[symbol]
	s.mu.RUnlock()
	
	if len(history) < 2 {
		return
	}
	
	// Compare current spread with average of previous spreads
	current := history[len(history)-1]
	previousAvg := s.calculateAverageSpread(history[:len(history)-1])
	
	if previousAvg == 0 {
		return
	}
	
	changePct := math.Abs(current.Spread-previousAvg) / previousAvg
	
	if changePct > s.spreadChangeThreshold {
		s.logger.Warn("Significant spread change detected",
			zap.String("symbol", symbol),
			zap.Float64("current_spread", current.Spread),
			zap.Float64("previous_avg_spread", previousAvg),
			zap.Float64("change_pct", changePct),
			zap.Float64("threshold", s.spreadChangeThreshold))
		
		// Trigger callbacks
		if s.onSpreadChange != nil {
			s.onSpreadChange(symbol, previousAvg, current.Spread, changePct)
		}
		
		// Cancel and rebuild
		s.executeRebuild(ctx, symbol)
	}
}

// calculateAverageSpread calculates average spread from snapshots
func (s *SmartCancellationManager) calculateAverageSpread(snapshots []SpreadSnapshot) float64 {
	if len(snapshots) == 0 {
		return 0
	}
	
	sum := 0.0
	for _, s := range snapshots {
		sum += s.Spread
	}
	return sum / float64(len(snapshots))
}

// executeRebuild cancels orders and rebuilds grid
func (s *SmartCancellationManager) executeRebuild(ctx context.Context, symbol string) {
	s.logger.Info("Executing grid rebuild",
		zap.String("symbol", symbol))
	
	// Cancel existing orders
	if s.onCancelOrders != nil {
		if err := s.onCancelOrders(ctx, symbol); err != nil {
			s.logger.Error("Failed to cancel orders during rebuild",
				zap.String("symbol", symbol),
				zap.Error(err))
			return
		}
	}
	
	// Rebuild grid
	if s.onRebuildGrid != nil {
		if err := s.onRebuildGrid(ctx, symbol); err != nil {
			s.logger.Error("Failed to rebuild grid",
				zap.String("symbol", symbol),
				zap.Error(err))
			return
		}
	}
	
	s.logger.Info("Grid rebuild completed",
		zap.String("symbol", symbol))
}

// SetCallbacks sets the callback functions
func (s *SmartCancellationManager) SetCallbacks(
	onSpreadChange func(symbol string, oldSpread, newSpread float64, changePct float64),
	onCancelOrders func(ctx context.Context, symbol string) error,
	onRebuildGrid func(ctx context.Context, symbol string) error,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.onSpreadChange = onSpreadChange
	s.onCancelOrders = onCancelOrders
	s.onRebuildGrid = onRebuildGrid
	
	s.logger.Info("SmartCancellationManager callbacks set")
}

// GetSpreadHistory returns spread history for a symbol
func (s *SmartCancellationManager) GetSpreadHistory(symbol string) []SpreadSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	history := s.spreadHistory[symbol]
	result := make([]SpreadSnapshot, len(history))
	copy(result, history)
	return result
}

// GetLastSpread returns the most recent spread for a symbol
func (s *SmartCancellationManager) GetLastSpread(symbol string) (SpreadSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	history := s.spreadHistory[symbol]
	if len(history) == 0 {
		return SpreadSnapshot{}, false
	}
	return history[len(history)-1], true
}

// IsEnabled returns whether smart cancellation is enabled
func (s *SmartCancellationManager) IsEnabled() bool {
	return s.enabled
}

// SetEnabled enables/disables smart cancellation
func (s *SmartCancellationManager) SetEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.enabled = enabled
	s.logger.Info("Smart cancellation enabled state changed",
		zap.Bool("enabled", enabled))
}

// GetStats returns current statistics
func (s *SmartCancellationManager) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	return map[string]interface{}{
		"enabled":                 s.enabled,
		"spread_change_threshold": s.spreadChangeThreshold,
		"check_interval":          s.checkInterval.Seconds(),
		"tracked_symbols":         len(s.spreadHistory),
	}
}
