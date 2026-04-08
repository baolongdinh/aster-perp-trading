package adaptive_grid

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/farming/market_regime"

	"go.uber.org/zap"
)

// DynamicSizeCalculator calculates order size based on multiple factors
type DynamicSizeCalculator struct {
	baseNotional        float64 // Base order size in USD (e.g., $100)
	minNotional         float64 // Minimum order size (e.g., $20)
	maxNotional         float64 // Maximum order size (e.g., $500)
	atrMultiplier       float64 // How much ATR affects size (0.5 = reduce size when volatile)
	maxTotalExposurePct float64 // Max exposure as % of equity (e.g., 0.3 = 30%)
	equity              float64 // Current account equity
	atrCalc             *ATRCalculator
	logger              *zap.Logger
	mu                  sync.RWMutex
}

// NewDynamicSizeCalculator creates new size calculator
func NewDynamicSizeCalculator(
	baseNotional, minNotional, maxNotional float64,
	atrMultiplier, maxExposurePct float64,
	logger *zap.Logger,
) *DynamicSizeCalculator {
	return &DynamicSizeCalculator{
		baseNotional:        baseNotional,
		minNotional:         minNotional,
		maxNotional:         maxNotional,
		atrMultiplier:       atrMultiplier,
		maxTotalExposurePct: maxExposurePct,
		atrCalc:             NewATRCalculator(14), // 14-period ATR
		logger:              logger,
	}
}

// UpdateEquity updates current account equity
func (d *DynamicSizeCalculator) UpdateEquity(equity float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.equity = equity
}

// AddPriceData adds price data for ATR calculation
func (d *DynamicSizeCalculator) AddPriceData(high, low, close float64) {
	d.atrCalc.AddPrice(high, low, close)
}

// CalculateOrderSize calculates appropriate order size
func (d *DynamicSizeCalculator) CalculateOrderSize(
	currentPrice float64,
	currentExposure float64,
	isLong bool,
	marketRegime market_regime.MarketRegime,
) (float64, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 1. Calculate max allowed exposure based on equity
	maxExposure := d.equity * d.maxTotalExposurePct
	availableExposure := maxExposure - currentExposure

	if availableExposure <= 0 {
		return 0, fmt.Errorf("max exposure reached: %.2f/%.2f", currentExposure, maxExposure)
	}

	// 2. Get ATR-based volatility adjustment
	atrPct := d.atrCalc.GetATRPct(currentPrice)

	// 3. Calculate volatility factor (reduce size when volatile)
	// Base: 1.0, High volatility: < 1.0
	volatilityFactor := 1.0
	if atrPct > 0 {
		// If ATR > 1%, reduce size proportionally
		if atrPct > 0.01 {
			volatilityFactor = math.Max(0.3, 1.0-(atrPct-0.01)*d.atrMultiplier)
		}
	}

	// 4. Regime adjustment
	regimeFactor := 1.0
	switch marketRegime {
	case market_regime.RegimeTrending:
		regimeFactor = 0.5 // Reduce 50% in trending (risk of reversal)
	case market_regime.RegimeVolatile:
		regimeFactor = 0.3 // Reduce 70% in volatile markets
	case market_regime.RegimeRanging:
		regimeFactor = 1.0 // Full size in ranging
	}

	// 5. Calculate final notional size
	notionalSize := d.baseNotional * volatilityFactor * regimeFactor

	// Apply bounds
	if notionalSize < d.minNotional {
		notionalSize = d.minNotional
	}
	if notionalSize > d.maxNotional {
		notionalSize = d.maxNotional
	}
	if notionalSize > availableExposure {
		notionalSize = availableExposure
	}

	// 6. Convert to quantity
	quantity := notionalSize / currentPrice

	d.logger.Info("Order size calculated",
		zap.Float64("current_price", currentPrice),
		zap.Float64("atr_pct", atrPct*100),
		zap.Float64("volatility_factor", volatilityFactor),
		zap.Float64("regime_factor", regimeFactor),
		zap.Float64("notional_usd", notionalSize),
		zap.Float64("quantity", quantity),
		zap.String("regime", string(marketRegime)),
	)

	return quantity, nil
}

// GetMaxExposure returns max allowed exposure
func (d *DynamicSizeCalculator) GetMaxExposure() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.equity * d.maxTotalExposurePct
}

// GetCurrentUtilization returns current exposure utilization
func (d *DynamicSizeCalculator) GetCurrentUtilization(currentExposure float64) float64 {
	max := d.GetMaxExposure()
	if max == 0 {
		return 0
	}
	return currentExposure / max
}

// DirectionalBiasChecker checks if we should trade against the trend
type DirectionalBiasChecker struct {
	shortTermMA []float64 // Short-term moving average (e.g., 5-period)
	longTermMA  []float64 // Long-term moving average (e.g., 20-period)
	maxHistory  int
	mu          sync.RWMutex
}

// NewDirectionalBiasChecker creates new bias checker
func NewDirectionalBiasChecker(shortPeriod, longPeriod int) *DirectionalBiasChecker {
	return &DirectionalBiasChecker{
		shortTermMA: make([]float64, 0, shortPeriod),
		longTermMA:  make([]float64, 0, longPeriod),
		maxHistory:  longPeriod,
	}
}

// AddPrice adds price for MA calculation
func (d *DirectionalBiasChecker) AddPrice(price float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.shortTermMA = append(d.shortTermMA, price)
	d.longTermMA = append(d.longTermMA, price)

	if len(d.shortTermMA) > cap(d.shortTermMA) {
		d.shortTermMA = d.shortTermMA[1:]
	}
	if len(d.longTermMA) > cap(d.longTermMA) {
		d.longTermMA = d.longTermMA[1:]
	}
}

// BiasType represents market bias
type BiasType int

const (
	BiasNeutral BiasType = iota
	BiasLong
	BiasShort
)

// GetBias returns current market bias
func (d *DirectionalBiasChecker) GetBias() BiasType {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.shortTermMA) < 5 || len(d.longTermMA) < 20 {
		return BiasNeutral
	}

	shortMA := calculateMA(d.shortTermMA)
	longMA := calculateMA(d.longTermMA)

	// Price above both MAs = bullish
	currentPrice := d.shortTermMA[len(d.shortTermMA)-1]

	if currentPrice > shortMA && shortMA > longMA {
		return BiasLong
	}
	if currentPrice < shortMA && shortMA < longMA {
		return BiasShort
	}
	return BiasNeutral
}

// ShouldAllowLong checks if long position is allowed
func (d *DirectionalBiasChecker) ShouldAllowLong() bool {
	bias := d.GetBias()
	// Allow long in neutral or bullish, block in strong bearish
	return bias != BiasShort
}

// ShouldAllowShort checks if short position is allowed
func (d *DirectionalBiasChecker) ShouldAllowShort() bool {
	bias := d.GetBias()
	// Allow short in neutral or bearish, block in strong bullish
	return bias != BiasLong
}

func calculateMA(prices []float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	var sum float64
	for _, p := range prices {
		sum += p
	}
	return sum / float64(len(prices))
}

// EnhancedRiskConfig extends RiskConfig with new parameters
type EnhancedRiskConfig struct {
	// Original fields
	MaxPositionUSDT         float64
	MaxUnhedgedExposureUSDT float64
	MaxUnrealizedLossUSDT   float64
	StopLossPct             float64
	TrailingStopPct         float64
	TrailingStopDistancePct float64
	LiquidationBufferPct    float64
	PositionCheckInterval   time.Duration
	TrendingThreshold       float64

	// New fields for dynamic sizing
	BaseOrderNotional    float64       // Base order size in USD (e.g., $100)
	MinOrderNotional     float64       // Minimum order size (e.g., $20)
	MaxOrderNotional     float64       // Maximum order size (e.g., $500)
	MaxTotalExposurePct  float64       // Max total exposure as % of equity (e.g., 0.3 = 30%)
	ATRMultiplier        float64       // ATR impact on sizing (e.g., 0.5)
	VolatilityThreshold  float64       // ATR % to consider high volatility (e.g., 0.01 = 1%)
	UseDirectionalBias   bool          // Whether to check trend before entering
	TrendFollowingOnly   bool          // Only trade with trend (no counter-trend)
	MaxConsecutiveLosses int           // Pause after N consecutive losses
	CooldownAfterLosses  time.Duration // Cooldown duration after max losses

	// New fields for take-profit management
	TakeProfitRRatio   float64 // Target R:R ratio (e.g., 1.5 = 1.5:1)
	MinTakeProfitPct   float64 // Minimum TP as % (e.g., 0.01 = 1%)
	MaxTakeProfitPct   float64 // Maximum TP as % (e.g., 0.05 = 5%)
	UseBreakevenTP     bool    // Enable breakeven-based TP
	BreakevenBufferPct float64 // Buffer above breakeven (e.g., 0.005 = 0.5%)
}

// DefaultEnhancedRiskConfig returns default enhanced config
func DefaultEnhancedRiskConfig() *EnhancedRiskConfig {
	return &EnhancedRiskConfig{
		// Original defaults
		MaxPositionUSDT:         1000.0,
		MaxUnhedgedExposureUSDT: 500.0,
		MaxUnrealizedLossUSDT:   50.0,
		StopLossPct:             0.015,
		TrailingStopPct:         0.01,
		TrailingStopDistancePct: 0.005,
		LiquidationBufferPct:    0.3,
		PositionCheckInterval:   5 * time.Second,
		TrendingThreshold:       0.7,

		// New defaults
		BaseOrderNotional:    100.0, // $100 per order
		MinOrderNotional:     20.0,  // Minimum $20
		MaxOrderNotional:     500.0, // Maximum $500 per order
		MaxTotalExposurePct:  0.3,   // Max 30% of equity
		ATRMultiplier:        0.5,   // Reduce size when volatile
		VolatilityThreshold:  0.01,  // 1% ATR = high volatility
		UseDirectionalBias:   true,
		TrendFollowingOnly:   false, // Allow counter-trend but with reduced size
		MaxConsecutiveLosses: 3,
		CooldownAfterLosses:  5 * time.Minute,

		// Take-profit defaults
		TakeProfitRRatio:   1.5,   // 1.5:1 R:R
		MinTakeProfitPct:   0.01,  // Minimum 1% TP
		MaxTakeProfitPct:   0.05,  // Maximum 5% TP
		UseBreakevenTP:     true,  // Enable breakeven TP
		BreakevenBufferPct: 0.005, // 0.5% buffer above breakeven
	}
}

// ExposureManager manages total exposure across all symbols
type ExposureManager struct {
	equity            float64
	totalExposure     float64 // Sum of all notional positions
	maxExposurePct    float64
	symbolExposures   map[string]float64
	consecutiveLosses int
	lastLossTime      time.Time
	cooldownActive    bool
	mu                sync.RWMutex
	logger            *zap.Logger
}

// NewExposureManager creates new exposure manager
func NewExposureManager(maxExposurePct float64, logger *zap.Logger) *ExposureManager {
	return &ExposureManager{
		maxExposurePct:  maxExposurePct,
		symbolExposures: make(map[string]float64),
		logger:          logger,
	}
}

// UpdateEquity updates account equity
func (e *ExposureManager) UpdateEquity(equity float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.equity = equity
}

// UpdateExposure updates exposure for a symbol
func (e *ExposureManager) UpdateExposure(symbol string, notionalValue float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	oldExposure := e.symbolExposures[symbol]
	e.totalExposure = e.totalExposure - oldExposure + notionalValue
	e.symbolExposures[symbol] = notionalValue
}

// RecordLoss records a losing trade
func (e *ExposureManager) RecordLoss() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.consecutiveLosses++
	e.lastLossTime = time.Now()

	if e.consecutiveLosses >= 3 {
		e.cooldownActive = true
		e.logger.Warn("Cooldown activated after consecutive losses",
			zap.Int("consecutive_losses", e.consecutiveLosses))
	}
}

// RecordWin resets consecutive losses
func (e *ExposureManager) RecordWin() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecutiveLosses = 0
	e.cooldownActive = false
}

// CanOpenPosition checks if new position can be opened
func (e *ExposureManager) CanOpenPosition(symbol string, notionalValue float64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check cooldown
	if e.cooldownActive {
		if time.Since(e.lastLossTime) < 5*time.Minute {
			return false
		}
		e.cooldownActive = false
	}

	// Check max exposure
	maxExposure := e.equity * e.maxExposurePct
	if e.totalExposure+notionalValue > maxExposure {
		e.logger.Warn("Cannot open position: max exposure would be exceeded",
			zap.Float64("current_exposure", e.totalExposure),
			zap.Float64("new_position", notionalValue),
			zap.Float64("max_allowed", maxExposure))
		return false
	}

	return true
}

// GetExposureStats returns current exposure statistics
func (e *ExposureManager) GetExposureStats() (totalExposure, maxExposure, utilization float64) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	maxExposure = e.equity * e.maxExposurePct
	if maxExposure > 0 {
		utilization = e.totalExposure / maxExposure
	}
	return e.totalExposure, maxExposure, utilization
}

// AccountInfoProvider interface for getting account info
type AccountInfoProvider interface {
	GetAccountInfo(ctx context.Context) (*client.AccountInfo, error)
	GetPositions(ctx context.Context) ([]client.Position, error)
}

// RiskMonitor monitors and manages risk in real-time
type RiskMonitor struct {
	exposureMgr     *ExposureManager
	sizeCalc        *DynamicSizeCalculator
	biasChecker     *DirectionalBiasChecker
	accountProvider AccountInfoProvider
	logger          *zap.Logger
	config          *EnhancedRiskConfig
	stopCh          chan struct{}
	wg              sync.WaitGroup

	// NEW: Smart position sizing
	tradeTracker      *TradeTracker
	smartSizingConfig *SmartSizingConfig
}

// NewRiskMonitor creates new risk monitor
func NewRiskMonitor(
	accountProvider AccountInfoProvider,
	config *EnhancedRiskConfig,
	logger *zap.Logger,
) *RiskMonitor {
	smartConfig := DefaultSmartSizingConfig()
	return &RiskMonitor{
		exposureMgr:       NewExposureManager(config.MaxTotalExposurePct, logger),
		sizeCalc:          NewDynamicSizeCalculator(config.BaseOrderNotional, config.MinOrderNotional, config.MaxOrderNotional, config.ATRMultiplier, config.MaxTotalExposurePct, logger),
		biasChecker:       NewDirectionalBiasChecker(5, 20),
		accountProvider:   accountProvider,
		logger:            logger,
		config:            config,
		stopCh:            make(chan struct{}),
		tradeTracker:      NewTradeTracker(smartConfig.WindowHours),
		smartSizingConfig: smartConfig,
	}
}

// SetSmartSizingConfig updates smart sizing configuration
func (r *RiskMonitor) SetSmartSizingConfig(config *SmartSizingConfig) {
	r.smartSizingConfig = config
}

// Start starts the risk monitor
func (r *RiskMonitor) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.monitorLoop(ctx)
}

// Stop stops the risk monitor
func (r *RiskMonitor) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

// monitorLoop continuously monitors risk
func (r *RiskMonitor) monitorLoop(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.config.PositionCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.updateAccountInfo(ctx)
		}
	}
}

// updateAccountInfo fetches and updates account information
func (r *RiskMonitor) updateAccountInfo(ctx context.Context) {
	account, err := r.accountProvider.GetAccountInfo(ctx)
	if err != nil {
		r.logger.Warn("Failed to get account info", zap.Error(err))
		return
	}

	// Update equity
	r.exposureMgr.UpdateEquity(account.TotalWalletBalance)
	r.sizeCalc.UpdateEquity(account.TotalWalletBalance)

	// Update positions
	positions, err := r.accountProvider.GetPositions(ctx)
	if err != nil {
		r.logger.Warn("Failed to get positions", zap.Error(err))
		return
	}

	for _, pos := range positions {
		if pos.PositionAmt != 0 {
			notional := math.Abs(pos.PositionAmt) * pos.MarkPrice
			r.exposureMgr.UpdateExposure(pos.Symbol, notional)
		}
	}
}

// GetOrderSize calculates order size with all risk checks
func (r *RiskMonitor) GetOrderSize(
	symbol string,
	currentPrice float64,
	isLong bool,
	regime market_regime.MarketRegime,
) (float64, error) {
	// Update price data
	r.biasChecker.AddPrice(currentPrice)

	// Check directional bias if enabled
	if r.config.UseDirectionalBias {
		if isLong && !r.biasChecker.ShouldAllowLong() {
			return 0, fmt.Errorf("long position blocked: market in bearish trend")
		}
		if !isLong && !r.biasChecker.ShouldAllowShort() {
			return 0, fmt.Errorf("short position blocked: market in bullish trend")
		}
	}

	// Get current exposure for this symbol
	totalExposure, _, _ := r.exposureMgr.GetExposureStats()

	// Calculate size
	size, err := r.sizeCalc.CalculateOrderSize(currentPrice, totalExposure, isLong, regime)
	if err != nil {
		return 0, err
	}

	// Check if we can open this position
	notional := size * currentPrice
	if !r.exposureMgr.CanOpenPosition(symbol, notional) {
		return 0, fmt.Errorf("position blocked by exposure manager")
	}

	return size, nil
}

// RecordTradeResult records trade result for loss tracking and trade history
func (r *RiskMonitor) RecordTradeResult(symbol string, pnl float64) {
	isWin := pnl > 0
	if isWin {
		r.exposureMgr.RecordWin()
	} else {
		r.exposureMgr.RecordLoss()
	}

	// Record in trade tracker for Kelly calculation
	if r.tradeTracker != nil {
		r.tradeTracker.RecordTrade(symbol, pnl)
	}
}

// GetSmartOrderSize calculates order size using Kelly Criterion and consecutive loss decay
func (r *RiskMonitor) GetSmartOrderSize(baseSize float64) float64 {
	if r.smartSizingConfig == nil || !r.smartSizingConfig.Enabled {
		return baseSize
	}

	winRate := 0.5 // Default
	if r.tradeTracker != nil {
		winRate = r.tradeTracker.GetWinRate()
	}

	consecutiveLosses := 0
	if r.tradeTracker != nil {
		consecutiveLosses = r.tradeTracker.GetConsecutiveLosses()
	}

	size := CalculateSmartSize(baseSize, winRate, consecutiveLosses, r.smartSizingConfig)

	r.logger.Debug("Smart order size calculated",
		zap.Float64("base_size", baseSize),
		zap.Float64("win_rate", winRate),
		zap.Int("consecutive_losses", consecutiveLosses),
		zap.Float64("final_size", size))

	// Log Kelly metrics for dashboard (every 60 calls to avoid spam)
	if r.tradeTracker != nil && r.tradeTracker.GetTradeCount()%60 == 0 {
		r.logger.Info("Kelly Metrics",
			zap.Float64("win_rate", winRate),
			zap.Int("consecutive_losses", consecutiveLosses),
			zap.Float64("kelly_fraction", r.smartSizingConfig.KellyFraction),
			zap.Float64("decay_factor", r.smartSizingConfig.ConsecutiveLossDecay),
			zap.Int("total_trades", r.tradeTracker.GetTradeCount()))
	}

	return size
}

// GetSmartSizingStatus returns current smart sizing metrics
func (r *RiskMonitor) GetSmartSizingStatus() map[string]interface{} {
	if r.smartSizingConfig == nil || !r.smartSizingConfig.Enabled {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	return map[string]interface{}{
		"enabled":            r.smartSizingConfig.Enabled,
		"win_rate":           r.tradeTracker.GetWinRate(),
		"consecutive_losses": r.tradeTracker.GetConsecutiveLosses(),
		"kelly_fraction":     r.smartSizingConfig.KellyFraction,
		"decay_factor":       r.smartSizingConfig.ConsecutiveLossDecay,
	}
}

// SmartSizingConfig holds Kelly Criterion and consecutive loss decay config
type SmartSizingConfig struct {
	Enabled              bool    // Enable smart sizing
	KellyFraction        float64 // Conservative Kelly (e.g., 0.25)
	ConsecutiveLossDecay float64 // Size reduction per loss (e.g., 0.8 = 20% reduction)
	MinSize              float64 // Minimum order size
	MaxSize              float64 // Maximum order size
	WindowHours          float64 // Lookback window for win rate calculation
}

// DefaultSmartSizingConfig returns default smart sizing config
func DefaultSmartSizingConfig() *SmartSizingConfig {
	return &SmartSizingConfig{
		Enabled:              true,
		KellyFraction:        0.25,  // Conservative Kelly
		ConsecutiveLossDecay: 0.8,   // 20% reduction per consecutive loss
		MinSize:              5.0,   // $5 minimum
		MaxSize:              100.0, // $100 maximum
		WindowHours:          24.0,  // 24 hour lookback
	}
}

// TradeResult tracks individual trade PnL
type TradeResult struct {
	Timestamp time.Time
	Symbol    string
	PnL       float64
	IsWin     bool
}

// TradeTracker tracks trade history for Kelly calculation
type TradeTracker struct {
	results              []TradeResult
	mu                   sync.RWMutex
	windowSize           int
	maxConsecutiveLosses int
}

// NewTradeTracker creates new trade tracker
func NewTradeTracker(windowHours float64) *TradeTracker {
	// Estimate max trades in window (1 trade per minute)
	windowSize := int(windowHours * 60)
	return &TradeTracker{
		results:    make([]TradeResult, 0, windowSize),
		windowSize: windowSize,
	}
}

// RecordTrade records a trade result
func (t *TradeTracker) RecordTrade(symbol string, pnl float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := TradeResult{
		Timestamp: time.Now(),
		Symbol:    symbol,
		PnL:       pnl,
		IsWin:     pnl > 0,
	}

	t.results = append(t.results, result)

	// Remove old results outside window
	cutoff := time.Now().Add(-time.Duration(t.windowSize) * time.Minute)
	newResults := make([]TradeResult, 0, len(t.results))
	for _, r := range t.results {
		if r.Timestamp.After(cutoff) {
			newResults = append(newResults, r)
		}
	}
	t.results = newResults
}

// GetWinRate calculates win rate over the window
func (t *TradeTracker) GetWinRate() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.results) == 0 {
		return 0.5 // Default 50% win rate if no data
	}

	wins := 0
	for _, r := range t.results {
		if r.IsWin {
			wins++
		}
	}

	return float64(wins) / float64(len(t.results))
}

// GetConsecutiveLosses returns current consecutive loss count
func (t *TradeTracker) GetConsecutiveLosses() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	// Count from the end
	for i := len(t.results) - 1; i >= 0; i-- {
		if !t.results[i].IsWin {
			count++
		} else {
			break
		}
	}
	return count
}

// GetTradeCount returns total number of tracked trades
func (t *TradeTracker) GetTradeCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.results)
}

// CalculateSmartSize calculates order size using Kelly Criterion and consecutive loss decay
func CalculateSmartSize(
	baseSize float64,
	winRate float64,
	consecutiveLosses int,
	config *SmartSizingConfig,
) float64 {
	if !config.Enabled {
		return baseSize
	}

	// 1. Apply consecutive loss decay
	decay := math.Pow(config.ConsecutiveLossDecay, float64(consecutiveLosses))
	size := baseSize * decay

	// 2. Kelly Criterion adjustment (assuming 1.5:1 R:R ratio)
	// Kelly = (WinRate * 1.5 - (1 - WinRate)) / 1.5
	rewardRatio := 1.5
	kelly := (winRate*rewardRatio - (1 - winRate)) / rewardRatio

	// Clamp Kelly to avoid extreme values
	if kelly < 0.1 {
		kelly = 0.1 // Minimum 10% Kelly
	}
	if kelly > 1.0 {
		kelly = 1.0 // Maximum 100% Kelly
	}

	// 3. Conservative Kelly (fractional)
	size = size * kelly * config.KellyFraction

	// 4. Clamp to min/max
	if size < config.MinSize {
		size = config.MinSize
	}
	if size > config.MaxSize {
		size = config.MaxSize
	}

	return size
}

// AddPriceData adds price data for ATR calculation
func (r *RiskMonitor) AddPriceData(high, low, close float64) {
	r.sizeCalc.AddPriceData(high, low, close)
}

// GetExposureStats returns current exposure stats
func (r *RiskMonitor) GetExposureStats() (float64, float64, float64) {
	return r.exposureMgr.GetExposureStats()
}
