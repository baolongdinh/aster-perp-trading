package adaptive_grid

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"aster-bot/internal/client"
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

// FuturesClientInterface defines methods needed from futures client for position tracking
type FuturesClientInterface interface {
	GetPositions(ctx context.Context) ([]client.Position, error)
	GetAccountInfo(ctx context.Context) (*client.AccountInfo, error)
	PlaceOrder(ctx context.Context, req client.PlaceOrderRequest) (*client.Order, error)
}

// ConfigManagerInterface defines methods needed from config manager
type ConfigManagerInterface interface {
	LoadConfig() error
	IsEnabled() bool
	GetRegimeConfig(regime string) (*config.RegimeConfig, bool)
	GetLastReload() time.Time
}

// SymbolPosition tracks position state for a symbol
type SymbolPosition struct {
	PositionAmt      float64 // Positive = Long, Negative = Short
	EntryPrice       float64
	MarkPrice        float64
	UnrealizedPnL    float64
	LiquidationPrice float64
	Leverage         float64
	NotionalValue    float64 // |PositionAmt| * MarkPrice
	LastUpdated      time.Time
}

// AdaptiveGridManager extends GridManager with regime-aware functionality and risk management
type AdaptiveGridManager struct {
	gridManager    GridManagerInterface
	configManager  ConfigManagerInterface
	regimeDetector *market_regime.RegimeDetector
	futuresClient  FuturesClientInterface
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

	// Position tracking from exchange
	positions          map[string]*SymbolPosition // symbol -> position
	positionStopLoss   map[string]float64         // symbol -> stop loss price
	positionTakeProfit map[string]float64         // symbol -> take profit price
	maxUnrealizedLoss  map[string]float64         // symbol -> max allowed unrealized loss (USDT)

	// Risk parameters
	riskConfig *RiskConfig

	// NEW: RiskMonitor for dynamic sizing and exposure
	riskMonitor *RiskMonitor

	// NEW: RangeDetector for breakout detection
	rangeDetectors map[string]*RangeDetector // symbol -> range detector

	// NEW: VolumeScaler for pyramid/tapered sizing
	volumeScalers map[string]*VolumeScaler // symbol -> volume scaler

	// NEW: TimeFilter for time-based trading rules
	timeFilter *TimeFilter

	// Control channels
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// RiskConfig holds risk management parameters
type RiskConfig struct {
	MaxPositionUSDT         float64       // Max position size per symbol (USDT)
	MaxUnhedgedExposureUSDT float64       // Max unhedged exposure (USDT)
	MaxUnrealizedLossUSDT   float64       // Max unrealized loss before emergency close
	StopLossPct             float64       // Stop loss percentage from entry (e.g., 0.02 = 2%)
	TrailingStopPct         float64       // Trailing stop activation (e.g., 0.01 = 1% profit)
	TrailingStopDistancePct float64       // Trailing stop distance (e.g., 0.005 = 0.5%)
	LiquidationBufferPct    float64       // Close before liquidation (e.g., 0.2 = 20% away)
	PositionCheckInterval   time.Duration // How often to check positions
	TrendingThreshold       float64       // Trending strength to pause trading
}

// DefaultRiskConfig returns default risk configuration
func DefaultRiskConfig() *RiskConfig {
	return &RiskConfig{
		MaxPositionUSDT:         300.0, // Max 1000 USDT per symbol
		MaxUnhedgedExposureUSDT: 200.0, // Max 500 USDT unhedged
		MaxUnrealizedLossUSDT:   3.0,   // Close if unrealized loss > 3 USDT
		StopLossPct:             0.01,  // 1% stop loss
		TrailingStopPct:         0.01,  // Activate at 1% profit
		TrailingStopDistancePct: 0.005, // 0.5% trailing distance
		LiquidationBufferPct:    0.2,   // Close at 20% away from liquidation
		PositionCheckInterval:   1 * time.Second,
		TrendingThreshold:       0.7, // Pause if trending > 70%
	}
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

// NewAdaptiveGridManager creates a new adaptive grid manager
func NewAdaptiveGridManager(
	baseGrid GridManagerInterface,
	configManager *adaptive_config.AdaptiveConfigManager,
	regimeDetector *market_regime.RegimeDetector,
	futuresClient FuturesClientInterface,
	logger *zap.Logger,
) *AdaptiveGridManager {
	return &AdaptiveGridManager{
		gridManager:        baseGrid,
		configManager:      configManager,
		regimeDetector:     regimeDetector,
		futuresClient:      futuresClient,
		logger:             logger,
		currentRegime:      make(map[string]market_regime.MarketRegime),
		lastRegimeChange:   make(map[string]time.Time),
		transitionCooldown: make(map[string]time.Time),
		circuitBreakers:    make(map[string]*CircuitBreaker),
		trendingStrength:   make(map[string]float64),
		tradingPaused:      make(map[string]bool),
		maxPositionSize:    make(map[string]float64),
		unhedgedExposure:   make(map[string]float64),
		trailingStopPrice:  make(map[string]float64),
		positions:          make(map[string]*SymbolPosition),
		positionStopLoss:   make(map[string]float64),
		positionTakeProfit: make(map[string]float64),
		maxUnrealizedLoss:  make(map[string]float64),
		riskConfig:         DefaultRiskConfig(),
		rangeDetectors:     make(map[string]*RangeDetector), // NEW: Range detectors
		volumeScalers:      make(map[string]*VolumeScaler),  // NEW: Volume scalers
		timeFilter:         nil,                             // NEW: Time filter (init later)
		stopCh:             make(chan struct{}),
		mu:                 sync.RWMutex{},
	}
}

// SetRiskConfig updates risk configuration
func (a *AdaptiveGridManager) SetRiskConfig(config *RiskConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.riskConfig = config
	a.logger.Info("Risk config updated",
		zap.Float64("max_position", config.MaxPositionUSDT),
		zap.Float64("max_unhedged", config.MaxUnhedgedExposureUSDT),
		zap.Float64("max_unrealized_loss", config.MaxUnrealizedLossUSDT),
		zap.Float64("stop_loss_pct", config.StopLossPct),
	)
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

	// NEW: Initialize RiskMonitor for dynamic sizing
	enhancedConfig := DefaultEnhancedRiskConfig()
	a.riskMonitor = NewRiskMonitor(a.futuresClient, enhancedConfig, a.logger)
	a.riskMonitor.Start(ctx)
	a.logger.Info("RiskMonitor started with dynamic sizing",
		zap.Float64("base_notional", enhancedConfig.BaseOrderNotional),
		zap.Float64("max_exposure_pct", enhancedConfig.MaxTotalExposurePct),
	)

	// NEW: Initialize TimeFilter for time-based trading rules
	timeFilterConfig := DefaultTradingHoursConfig()
	timeFilter, err := NewTimeFilter(timeFilterConfig, a.logger)
	if err != nil {
		a.logger.Warn("Failed to initialize TimeFilter, using default", zap.Error(err))
	} else {
		a.timeFilter = timeFilter
		a.logger.Info("TimeFilter initialized",
			zap.String("mode", string(timeFilterConfig.Mode)),
			zap.String("timezone", timeFilterConfig.Timezone))
	}

	// Start position monitoring goroutine
	a.wg.Add(1)
	go a.positionMonitor(ctx)

	a.logger.Info("Adaptive grid manager initialized successfully",
		zap.Bool("enabled", a.configManager.IsEnabled()),
		zap.Time("last_reload", a.configManager.GetLastReload()))

	return nil
}

// positionMonitor monitors positions and triggers risk controls
func (a *AdaptiveGridManager) positionMonitor(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.riskConfig.PositionCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.checkAndManageRisk(ctx)
		}
	}
}

// checkAndManageRisk checks positions and applies risk controls
func (a *AdaptiveGridManager) checkAndManageRisk(ctx context.Context) {
	if a.futuresClient == nil {
		return
	}

	// Fetch positions from exchange
	positions, err := a.futuresClient.GetPositions(ctx)
	if err != nil {
		a.logger.Warn("Failed to fetch positions", zap.Error(err))
		return
	}

	for _, pos := range positions {
		if pos.PositionAmt == 0 {
			continue // Skip empty positions
		}

		symbol := pos.Symbol

		// Update position tracking
		a.updatePositionTracking(symbol, &pos)

		// Check risk limits
		a.evaluateRiskAndAct(ctx, symbol, &pos)
	}
}

// updatePositionTracking updates internal position state
func (a *AdaptiveGridManager) updatePositionTracking(symbol string, pos *client.Position) {
	a.mu.Lock()
	defer a.mu.Unlock()

	notional := math.Abs(pos.PositionAmt) * pos.MarkPrice

	a.positions[symbol] = &SymbolPosition{
		PositionAmt:      pos.PositionAmt,
		EntryPrice:       pos.EntryPrice,
		MarkPrice:        pos.MarkPrice,
		UnrealizedPnL:    pos.UnrealizedProfit,
		LiquidationPrice: pos.Liquidation,
		Leverage:         pos.Leverage,
		NotionalValue:    notional,
		LastUpdated:      time.Now(),
	}

	// Initialize stop loss if not set
	if _, exists := a.positionStopLoss[symbol]; !exists {
		a.setInitialStopLoss(symbol, pos.EntryPrice, pos.PositionAmt)
	}

	// Update trailing stop if in profit
	a.updateTrailingStop(symbol, pos.MarkPrice, pos.PositionAmt)
}

// setInitialStopLoss sets the initial stop loss price
func (a *AdaptiveGridManager) setInitialStopLoss(symbol string, entryPrice, positionAmt float64) {
	stopLossPct := a.riskConfig.StopLossPct

	if positionAmt > 0 { // Long position
		a.positionStopLoss[symbol] = entryPrice * (1 - stopLossPct)
	} else { // Short position
		a.positionStopLoss[symbol] = entryPrice * (1 + stopLossPct)
	}

	a.logger.Info("Stop loss set",
		zap.String("symbol", symbol),
		zap.Float64("entry", entryPrice),
		zap.Float64("stop_loss", a.positionStopLoss[symbol]),
		zap.Float64("pct", stopLossPct*100))
}

// updateTrailingStop updates trailing stop if in profit
func (a *AdaptiveGridManager) updateTrailingStop(symbol string, markPrice, positionAmt float64) {
	entryPrice := a.positions[symbol].EntryPrice
	trailingActivation := a.riskConfig.TrailingStopPct
	trailingDistance := a.riskConfig.TrailingStopDistancePct

	var profitPct float64
	var newStopPrice float64

	if positionAmt > 0 { // Long
		profitPct = (markPrice - entryPrice) / entryPrice
		if profitPct > trailingActivation {
			newStopPrice = markPrice * (1 - trailingDistance)
			if currentStop, exists := a.trailingStopPrice[symbol]; !exists || newStopPrice > currentStop {
				a.trailingStopPrice[symbol] = newStopPrice
				a.logger.Info("Trailing stop updated (Long)",
					zap.String("symbol", symbol),
					zap.Float64("mark_price", markPrice),
					zap.Float64("new_stop", newStopPrice),
					zap.Float64("profit_pct", profitPct*100))
			}
		}
	} else { // Short
		profitPct = (entryPrice - markPrice) / entryPrice
		if profitPct > trailingActivation {
			newStopPrice = markPrice * (1 + trailingDistance)
			if currentStop, exists := a.trailingStopPrice[symbol]; !exists || newStopPrice < currentStop {
				a.trailingStopPrice[symbol] = newStopPrice
				a.logger.Info("Trailing stop updated (Short)",
					zap.String("symbol", symbol),
					zap.Float64("mark_price", markPrice),
					zap.Float64("new_stop", newStopPrice),
					zap.Float64("profit_pct", profitPct*100))
			}
		}
	}
}

// evaluateRiskAndAct evaluates risk and takes action if needed
func (a *AdaptiveGridManager) evaluateRiskAndAct(ctx context.Context, symbol string, pos *client.Position) {
	a.mu.RLock()
	position := a.positions[symbol]
	stopLoss := a.positionStopLoss[symbol]
	trailingStop := a.trailingStopPrice[symbol]
	a.mu.RUnlock()

	if position == nil {
		return
	}

	markPrice := position.MarkPrice
	notional := position.NotionalValue
	unrealizedPnL := position.UnrealizedPnL
	liquidationPrice := position.LiquidationPrice

	// 1. Check stop loss
	if a.isStopLossHit(symbol, markPrice, pos.PositionAmt, stopLoss, trailingStop) {
		a.logger.Warn("STOP LOSS TRIGGERED - Closing position",
			zap.String("symbol", symbol),
			zap.Float64("mark_price", markPrice),
			zap.Float64("unrealized_pnl", unrealizedPnL))
		a.emergencyClosePosition(ctx, symbol, pos.PositionAmt)
		return
	}

	// 2. Check liquidation proximity
	if a.isNearLiquidation(markPrice, liquidationPrice, pos.PositionAmt) {
		a.logger.Warn("NEAR LIQUIDATION - Emergency closing",
			zap.String("symbol", symbol),
			zap.Float64("mark_price", markPrice),
			zap.Float64("liquidation_price", liquidationPrice),
			zap.Float64("distance_pct", math.Abs(markPrice-liquidationPrice)/markPrice*100))
		a.emergencyClosePosition(ctx, symbol, pos.PositionAmt)
		return
	}

	// 3. Check max unrealized loss
	if unrealizedPnL < -a.riskConfig.MaxUnrealizedLossUSDT {
		a.logger.Warn("MAX UNREALIZED LOSS EXCEEDED - Closing position",
			zap.String("symbol", symbol),
			zap.Float64("unrealized_pnl", unrealizedPnL),
			zap.Float64("max_loss", a.riskConfig.MaxUnrealizedLossUSDT))
		a.emergencyClosePosition(ctx, symbol, pos.PositionAmt)
		return
	}

	// 4. Check max position size
	if notional > a.riskConfig.MaxPositionUSDT {
		a.logger.Warn("MAX POSITION SIZE EXCEEDED - Pausing new orders",
			zap.String("symbol", symbol),
			zap.Float64("notional", notional),
			zap.Float64("max_allowed", a.riskConfig.MaxPositionUSDT))
		a.pauseTrading(symbol)
		return
	}
}

// isStopLossHit checks if stop loss is hit
func (a *AdaptiveGridManager) isStopLossHit(symbol string, markPrice, positionAmt, stopLoss, trailingStop float64) bool {
	if positionAmt == 0 {
		return false
	}

	// Check fixed stop loss
	if positionAmt > 0 && markPrice <= stopLoss { // Long
		return true
	}
	if positionAmt < 0 && markPrice >= stopLoss { // Short
		return true
	}

	// Check trailing stop
	if trailingStop > 0 {
		if positionAmt > 0 && markPrice <= trailingStop { // Long trailing
			return true
		}
		if positionAmt < 0 && markPrice >= trailingStop { // Short trailing
			return true
		}
	}

	return false
}

// isNearLiquidation checks if position is near liquidation
func (a *AdaptiveGridManager) isNearLiquidation(markPrice, liquidationPrice, positionAmt float64) bool {
	if liquidationPrice == 0 || positionAmt == 0 {
		return false
	}

	distance := math.Abs(markPrice - liquidationPrice)
	distancePct := distance / markPrice

	return distancePct < a.riskConfig.LiquidationBufferPct
}

// emergencyClosePosition closes position immediately with market order
func (a *AdaptiveGridManager) emergencyClosePosition(ctx context.Context, symbol string, positionAmt float64) {
	if positionAmt == 0 || a.futuresClient == nil {
		return
	}

	side := "SELL"
	if positionAmt < 0 {
		side = "BUY" // Close short
	}

	qty := fmt.Sprintf("%.6f", math.Abs(positionAmt))

	orderReq := client.PlaceOrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          "MARKET",
		Quantity:      qty,
		ReduceOnly:    true,
		ClosePosition: true,
	}

	a.logger.Warn("EMERGENCY CLOSE - Placing market order",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("qty", qty))

	order, err := a.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		a.logger.Error("Failed to emergency close position", zap.Error(err))
		return
	}

	a.logger.Info("Position closed successfully",
		zap.String("symbol", symbol),
		zap.Int64("order_id", order.OrderID))

	// Clear position tracking
	a.mu.Lock()
	delete(a.positions, symbol)
	delete(a.positionStopLoss, symbol)
	delete(a.trailingStopPrice, symbol)
	a.mu.Unlock()

	// Pause trading for this symbol
	a.pauseTrading(symbol)
}

// pauseTrading pauses trading for a symbol
func (a *AdaptiveGridManager) pauseTrading(symbol string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tradingPaused[symbol] = true
}

// resumeTrading resumes trading for a symbol
func (a *AdaptiveGridManager) resumeTrading(symbol string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.tradingPaused, symbol)
}

// IsTradingPaused checks if trading is paused for a symbol
func (a *AdaptiveGridManager) IsTradingPaused(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tradingPaused[symbol]
}

// GetPosition returns current position for a symbol
func (a *AdaptiveGridManager) GetPosition(symbol string) *SymbolPosition {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.positions[symbol]
}

// GetAllPositions returns all tracked positions
func (a *AdaptiveGridManager) GetAllPositions() map[string]*SymbolPosition {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]*SymbolPosition)
	for k, v := range a.positions {
		result[k] = v
	}
	return result
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

	// Check if we have position before clearing
	position := a.GetPosition(symbol)
	if position != nil && math.Abs(position.PositionAmt) > 0 {
		// In trending regime, consider closing position
		if newRegime == market_regime.RegimeTrending {
			a.logger.Warn("Transitioning to trending regime with open position - considering close",
				zap.String("symbol", symbol),
				zap.Float64("position_amt", position.PositionAmt),
				zap.Float64("unrealized_pnl", position.UnrealizedPnL))
			// Note: We don't auto-close here, let risk monitor handle it
		}
	}

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

	// Phase 4: Rebuild grid with new parameters (only if not paused)
	if !a.IsTradingPaused(symbol) {
		if err := a.gridManager.RebuildGrid(ctx, symbol); err != nil {
			return err
		}
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

	// Adjust risk parameters based on regime
	a.mu.Lock()
	defer a.mu.Unlock()

	switch newRegime {
	case market_regime.RegimeTrending:
		// In trending: tighten stop loss, reduce position size
		a.riskConfig.StopLossPct = 0.01      // 1% stop
		a.riskConfig.MaxPositionUSDT = 500.0 // Reduce max position
		a.tradingPaused[symbol] = true       // Pause until trend stabilizes
		a.logger.Warn("TRENDING regime: Trading paused, tight stops enabled")

	case market_regime.RegimeRanging:
		// In ranging: normal parameters, resume trading
		a.riskConfig.StopLossPct = 0.015      // 1.5% stop
		a.riskConfig.MaxPositionUSDT = 1000.0 // Normal max position
		delete(a.tradingPaused, symbol)
		a.logger.Info("RANGING regime: Trading resumed with normal parameters")

	case market_regime.RegimeVolatile:
		// In volatile: wider stops, smaller positions
		a.riskConfig.StopLossPct = 0.02      // 2% stop
		a.riskConfig.MaxPositionUSDT = 300.0 // Smaller positions
		a.logger.Warn("VOLATILE regime: Reduced position size, wider stops")
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

// Stop stops the adaptive grid manager
func (a *AdaptiveGridManager) Stop(ctx context.Context) error {
	a.logger.Info("Stopping adaptive grid manager")
	close(a.stopCh)

	// NEW: Stop RiskMonitor
	if a.riskMonitor != nil {
		a.riskMonitor.Stop()
		a.logger.Info("RiskMonitor stopped")
	}

	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		a.logger.Info("Adaptive grid manager stopped gracefully")
		return nil
	case <-ctx.Done():
		a.logger.Warn("Adaptive grid manager stop timeout")
		return ctx.Err()
	}
}

// CanPlaceOrder checks if new orders can be placed for a symbol
func (a *AdaptiveGridManager) CanPlaceOrder(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Check if trading is paused
	if a.tradingPaused[symbol] {
		return false
	}

	// Check if in cooldown
	if cooldown, exists := a.transitionCooldown[symbol]; exists {
		if time.Since(cooldown) < 30*time.Second {
			return false
		}
	}

	// Check position limit
	if position, exists := a.positions[symbol]; exists {
		if position.NotionalValue >= a.riskConfig.MaxPositionUSDT {
			return false
		}
	}

	// NEW: Check RiskMonitor exposure limits
	if a.riskMonitor != nil {
		_, maxExposure, utilization := a.riskMonitor.GetExposureStats()
		if utilization >= 1.0 {
			a.logger.Warn("CanPlaceOrder blocked: max exposure reached",
				zap.Float64("utilization", utilization*100),
				zap.Float64("max_exposure", maxExposure))
			return false
		}
	}

	// NEW: Check range state - only trade when range is active
	if detector, exists := a.rangeDetectors[symbol]; exists {
		if !detector.ShouldTrade() {
			a.logger.Debug("CanPlaceOrder blocked: range not active",
				zap.String("symbol", symbol),
				zap.String("state", detector.GetStateString()))
			return false
		}
	}

	// NEW: Check TimeFilter - only trade during allowed hours
	if a.timeFilter != nil {
		if !a.timeFilter.CanTrade() {
			a.logger.Debug("CanPlaceOrder blocked: outside trading hours",
				zap.String("symbol", symbol))
			return false
		}
	}

	return true
}

// InitializeTimeFilter creates or reinitializes the time filter with config
func (a *AdaptiveGridManager) InitializeTimeFilter(config *TradingHoursConfig) error {
	filter, err := NewTimeFilter(config, a.logger)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.timeFilter = filter

	a.logger.Info("TimeFilter initialized/reloaded",
		zap.String("mode", string(config.Mode)),
		zap.Int("slots", len(config.Slots)))
	return nil
}

// GetTimeFilterStatus returns current time filter status
func (a *AdaptiveGridManager) GetTimeFilterStatus() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return map[string]interface{}{"error": "time filter not initialized"}
	}

	return a.timeFilter.GetCurrentStatus()
}

// GetTimeBasedSizeMultiplier returns size multiplier for current time
func (a *AdaptiveGridManager) GetTimeBasedSizeMultiplier() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return 1.0 // Default
	}

	return a.timeFilter.GetSizeMultiplier()
}

// GetTimeBasedSpreadMultiplier returns spread multiplier for current time
func (a *AdaptiveGridManager) GetTimeBasedSpreadMultiplier() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return 1.0 // Default
	}

	return a.timeFilter.GetSpreadMultiplier()
}

// IsHighVolatilityTime returns true if currently in high volatility period
func (a *AdaptiveGridManager) IsHighVolatilityTime() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return false
	}

	return a.timeFilter.IsHighVolatilityPeriod()
}

// GetCurrentTimeSlot returns current time slot info
func (a *AdaptiveGridManager) GetCurrentTimeSlot() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return map[string]interface{}{"error": "time filter not initialized"}
	}

	return a.timeFilter.GetCurrentStatus()
}

// GetOrderSize returns dynamically calculated order size
func (a *AdaptiveGridManager) GetOrderSize(symbol string, currentPrice float64, isLong bool) (float64, error) {
	if a.riskMonitor == nil {
		return 0, fmt.Errorf("risk monitor not initialized")
	}

	regime := a.GetCurrentRegime(symbol)
	size, err := a.riskMonitor.GetOrderSize(symbol, currentPrice, isLong, regime)
	if err != nil {
		return 0, err
	}

	return size, nil
}

// AddPriceData adds price data for ATR calculation
func (a *AdaptiveGridManager) AddPriceData(high, low, close float64) {
	if a.riskMonitor != nil {
		a.riskMonitor.AddPriceData(high, low, close)
	}
}

// RecordTradeResult records trade result for loss tracking
func (a *AdaptiveGridManager) RecordTradeResult(symbol string, isWin bool) {
	if a.riskMonitor != nil {
		a.riskMonitor.RecordTradeResult(isWin)
	}
}

// GetExposureStats returns current exposure statistics
func (a *AdaptiveGridManager) GetExposureStats() (totalExposure, maxExposure, utilization float64) {
	if a.riskMonitor == nil {
		return 0, 0, 0
	}
	return a.riskMonitor.GetExposureStats()
}

// InitializeRangeDetector creates a range detector for a symbol
func (a *AdaptiveGridManager) InitializeRangeDetector(symbol string, config *RangeConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if config == nil {
		config = DefaultRangeConfig()
	}

	a.rangeDetectors[symbol] = NewRangeDetector(config, a.logger)
	a.logger.Info("RangeDetector initialized for symbol",
		zap.String("symbol", symbol),
		zap.String("method", config.Method))
}

// UpdatePriceForRange updates price data for range detection
func (a *AdaptiveGridManager) UpdatePriceForRange(symbol string, high, low, close float64) {
	a.mu.RLock()
	detector, exists := a.rangeDetectors[symbol]
	a.mu.RUnlock()

	if !exists {
		// Auto-initialize if not exists
		a.InitializeRangeDetector(symbol, nil)
		detector, _ = a.rangeDetectors[symbol]
	}

	detector.AddPrice(high, low, close)

	// Check for breakout and handle
	if detector.IsBreakout() {
		a.handleBreakout(symbol, close)
	}
}

// handleBreakout handles breakout detection
func (a *AdaptiveGridManager) handleBreakout(symbol string, currentPrice float64) {
	a.logger.Warn("Breakout detected - Closing all positions immediately!",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice))

	// 1. Cancel all grid orders
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
		a.logger.Error("Failed to cancel orders on breakout", zap.Error(err))
	}

	// 2. Get current position and close it
	position := a.GetPosition(symbol)
	if position != nil && position.PositionAmt != 0 {
		a.emergencyClosePosition(ctx, symbol, position.PositionAmt)
	}

	// 3. Pause trading
	a.pauseTrading(symbol)

	// 4. Clear position tracking
	a.mu.Lock()
	delete(a.positions, symbol)
	delete(a.positionStopLoss, symbol)
	delete(a.trailingStopPrice, symbol)
	a.mu.Unlock()

	a.logger.Info("Breakout handling complete - Trading paused, waiting for stabilization",
		zap.String("symbol", symbol))
}

// GetRangeState returns current range state for a symbol
func (a *AdaptiveGridManager) GetRangeState(symbol string) RangeState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return RangeStateUnknown
	}
	return detector.GetState()
}

// GetRangeInfo returns range information for a symbol
func (a *AdaptiveGridManager) GetRangeInfo(symbol string) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return map[string]interface{}{"error": "no range detector"}
	}
	return detector.GetRangeInfo()
}

// ShouldTradeInRange returns true if trading is allowed in current range
func (a *AdaptiveGridManager) ShouldTradeInRange(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return true // Default allow if no detector
	}

	// Only trade when range is active
	return detector.ShouldTrade()
}

// GetRangeBasedGridParams returns grid parameters based on current range
func (a *AdaptiveGridManager) GetRangeBasedGridParams(symbol string) (spreadPct float64, levels int, valid bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return 0, 0, false
	}
	return detector.GetGridParameters()
}

// InitializeVolumeScaler creates a volume scaler for a symbol
func (a *AdaptiveGridManager) InitializeVolumeScaler(symbol string, config *VolumeScalingConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if config == nil {
		config = DefaultVolumeScalingConfig()
	}

	a.volumeScalers[symbol] = NewVolumeScaler(config, a.logger)
	a.logger.Info("VolumeScaler initialized for symbol",
		zap.String("symbol", symbol),
		zap.Float64("center_notional", config.CenterNotional),
		zap.Float64("edge_notional", config.EdgeNotional),
		zap.String("curve", config.CurveType))
}

// UpdateVolumeScalerRange updates range for volume scaler
func (a *AdaptiveGridManager) UpdateVolumeScalerRange(symbol string, rangeData *RangeData) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	scaler, exists := a.volumeScalers[symbol]
	if !exists {
		// Auto-initialize
		a.mu.RUnlock()
		a.InitializeVolumeScaler(symbol, nil)
		a.mu.RLock()
		scaler = a.volumeScalers[symbol]
	}

	scaler.UpdateRange(rangeData)
}

// GetTaperedOrderSize returns order size with pyramid/tapered scaling
func (a *AdaptiveGridManager) GetTaperedOrderSize(symbol string, price float64, isBuy bool) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	scaler, exists := a.volumeScalers[symbol]
	if !exists {
		// Fallback to base notional
		return 100.0
	}

	return scaler.CalculateOrderSize(price, isBuy)
}

// GetGridSizesWithTapering returns all grid level sizes with tapering
func (a *AdaptiveGridManager) GetGridSizesWithTapering(symbol string, currentPrice float64, numLevels int, spreadPct float64) []GridLevelSize {
	a.mu.RLock()
	defer a.mu.RUnlock()

	scaler, exists := a.volumeScalers[symbol]
	if !exists {
		return nil
	}

	return scaler.CalculateAllGridLevels(currentPrice, numLevels, spreadPct)
}

// GetVolumeScalerInfo returns volume scaler info for a symbol
func (a *AdaptiveGridManager) GetVolumeScalerInfo(symbol string, price float64) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	scaler, exists := a.volumeScalers[symbol]
	if !exists {
		return map[string]interface{}{"error": "no volume scaler"}
	}

	info := scaler.GetZoneInfo(price)
	info["tapered_size"] = scaler.CalculateOrderSize(price, true)
	return info
}
