package adaptive_grid

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_config"
	"aster-bot/internal/farming/market_regime"

	"go.uber.org/zap"
)

// GridManagerInterface defines the interface needed from GridManager
type GridManagerInterface interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetActivePositions(symbol string) ([]interface{}, error)
	CancelAllOrders(ctx context.Context, symbol string) error
	ClearGrid(ctx context.Context, symbol string) error
	RebuildGrid(ctx context.Context, symbol string) error
	SetOrderSize(size float64)
	SetGridSpread(spread float64)
	SetMaxOrdersPerSide(max int)
	SetPositionTimeout(minutes int)
}

// ConfigManagerInterface defines methods needed from config manager
type ConfigManagerInterface interface {
	LoadConfig() error
	IsEnabled() bool
	GetRegimeConfig(regime string) (*config.RegimeConfig, bool)
	GetLastReload() time.Time
}

// AdaptiveGridManager extends GridManager with regime-aware functionality
type AdaptiveGridManager struct {
	gridManager    GridManagerInterface
	configManager  ConfigManagerInterface
	regimeDetector *market_regime.RegimeDetector
	logger         *zap.Logger
	mu             sync.RWMutex

	// Adaptive state
	currentRegime      map[string]market_regime.MarketRegime
	lastRegimeChange   map[string]time.Time
	transitionCooldown map[string]time.Time

	// Risk management - Circuit Breaker
	circuitBreakers   map[string]*CircuitBreaker
	trendingStrength  map[string]float64 // 0-1, đo lường độ mạnh của xu hướng
	tradingPaused     map[string]bool    // true = tạm dừng trading
	maxPositionSize   map[string]float64 // Giới hạn position size (USDT)
	unhedgedExposure  map[string]float64 // Vị thế chưa hedged
	trailingStopPrice map[string]float64 // Giá trailing stop
}

// CircuitBreaker tự động dừng trading khi trending quá mạnh
type CircuitBreaker struct {
	maxTrendingStrength float64       // Ngưỡng trending để dừng (0.7 = 70%)
	cooldownDuration    time.Duration // Thời gian nghỉ sau khi dừng
	lastTriggered       time.Time     // Lần cuối trigger
	triggerCount        int           // Số lần trigger trong ngày
}

// NewCircuitBreaker tạo circuit breaker mới
func NewCircuitBreaker(maxStrength float64, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxTrendingStrength: maxStrength,
		cooldownDuration:    cooldown,
		triggerCount:        0,
	}
}

// ShouldBreak kiểm tra có nên dừng trading không
func (cb *CircuitBreaker) ShouldBreak(trendingStrength float64) bool {
	if trendingStrength > cb.maxTrendingStrength {
		cb.lastTriggered = time.Now()
		cb.triggerCount++
		return true
	}
	return false
}

// IsInCooldown kiểm tra có đang trong thời gian nghỉ không
func (cb *CircuitBreaker) IsInCooldown() bool {
	return time.Since(cb.lastTriggered) < cb.cooldownDuration
}
func NewAdaptiveGridManager(
	baseGrid GridManagerInterface,
	configManager *adaptive_config.AdaptiveConfigManager,
	regimeDetector *market_regime.RegimeDetector,
	logger *zap.Logger,
) *AdaptiveGridManager {
	return &AdaptiveGridManager{
		gridManager:        baseGrid,
		configManager:      configManager,
		regimeDetector:     regimeDetector,
		logger:             logger,
		currentRegime:      make(map[string]market_regime.MarketRegime),
		lastRegimeChange:   make(map[string]time.Time),
		transitionCooldown: make(map[string]time.Time),
		mu:                 sync.RWMutex{},
	}
}

// Initialize sets up the adaptive grid manager
func (a *AdaptiveGridManager) Initialize(ctx context.Context) error {
	a.logger.Info("Initializing adaptive grid manager")

	// Load adaptive configuration
	if err := a.configManager.LoadConfig(); err != nil {
		return err
	}

	// Check if adaptive config is enabled
	if !a.configManager.IsEnabled() {
		a.logger.Warn("Adaptive configuration is disabled, using base grid manager")
		return nil
	}

	a.logger.Info("Adaptive grid manager initialized successfully",
		zap.Bool("enabled", a.configManager.IsEnabled()),
		zap.Time("last_reload", a.configManager.GetLastReload()))

	return nil
}

// GetCurrentRegime returns the current regime for a symbol
func (a *AdaptiveGridManager) GetCurrentRegime(symbol string) market_regime.MarketRegime {
	a.mu.RLock()
	defer a.mu.RUnlock()

	regime, exists := a.currentRegime[symbol]
	if !exists {
		regime = market_regime.RegimeUnknown
	}

	return regime
}

// OnRegimeChange handles regime change notifications
func (a *AdaptiveGridManager) OnRegimeChange(symbol string, oldRegime, newRegime market_regime.MarketRegime) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.currentRegime[symbol] = newRegime
	a.lastRegimeChange[symbol] = time.Now()

	a.logger.Info("Regime change detected",
		zap.String("symbol", symbol),
		zap.String("from", string(oldRegime)),
		zap.String("to", string(newRegime)),
		zap.Time("timestamp", a.lastRegimeChange[symbol]))
}

// IsInTransitionCooldown checks if symbol is in transition cooldown
func (a *AdaptiveGridManager) IsInTransitionCooldown(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	cooldown, exists := a.transitionCooldown[symbol]
	if !exists {
		return false
	}

	return time.Since(cooldown) < 30*time.Second
}

// SetTransitionCooldown sets the transition cooldown for a symbol
func (a *AdaptiveGridManager) SetTransitionCooldown(symbol string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.transitionCooldown[symbol] = time.Now()
}

// HandleRegimeTransition handles regime change with smooth transition
func (a *AdaptiveGridManager) HandleRegimeTransition(
	ctx context.Context,
	symbol string,
	oldRegime, newRegime market_regime.MarketRegime,
) error {
	a.logger.Info("Starting regime transition",
		zap.String("symbol", symbol),
		zap.String("from", string(oldRegime)),
		zap.String("to", string(newRegime)))

	// Set transition cooldown
	a.SetTransitionCooldown(symbol)

	// Phase 1: Cancel existing orders
	if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
		return err
	}

	// Phase 2: Wait for transition period
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
		a.logger.Info("Transition cooldown completed", zap.String("symbol", symbol))
	case <-ctx.Done():
		return ctx.Err()
	}

	// Phase 3: Apply new regime parameters
	if err := a.applyNewRegimeParameters(ctx, symbol, newRegime); err != nil {
		return err
	}

	// Phase 4: Rebuild grid with new parameters
	if err := a.gridManager.RebuildGrid(ctx, symbol); err != nil {
		return err
	}

	a.logger.Info("Regime transition completed successfully",
		zap.String("symbol", symbol),
		zap.String("final_regime", string(newRegime)))

	return nil
}

// applyNewRegimeParameters applies new regime-specific parameters
func (a *AdaptiveGridManager) applyNewRegimeParameters(
	ctx context.Context,
	symbol string,
	newRegime market_regime.MarketRegime,
) error {
	a.logger.Info("Applying new regime parameters",
		zap.String("symbol", symbol),
		zap.String("regime", string(newRegime)))

	// Get regime configuration
	regimeConfig, exists := a.configManager.GetRegimeConfig(string(newRegime))
	if !exists {
		return fmt.Errorf("no configuration found for regime: %s", newRegime)
	}

	// Apply parameters to grid manager
	a.gridManager.SetOrderSize(regimeConfig.OrderSizeUSDT)
	a.gridManager.SetGridSpread(regimeConfig.GridSpreadPct)
	a.gridManager.SetMaxOrdersPerSide(regimeConfig.MaxOrdersPerSide)
	a.gridManager.SetPositionTimeout(regimeConfig.PositionTimeoutMinutes)

	a.logger.Info("New regime parameters applied successfully",
		zap.String("symbol", symbol),
		zap.String("regime", string(newRegime)))

	return nil
}
