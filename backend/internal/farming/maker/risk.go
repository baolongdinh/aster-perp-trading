package maker

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

type LiquidationGuard struct {
	config        *Config
	logger        *zap.Logger
	positions     map[string]*PositionState
	mu            sync.RWMutex
	checkInterval time.Duration
}

func NewLiquidationGuard(config *Config, logger *zap.Logger) *LiquidationGuard {
	return &LiquidationGuard{
		config:        config,
		logger:        logger,
		positions:     make(map[string]*PositionState),
		checkInterval: config.CheckInterval,
	}
}

func (g *LiquidationGuard) Name() string {
	return "LiquidationGuard"
}

func (g *LiquidationGuard) UpdatePosition(symbol string, amount, entryPrice, markPrice float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if math.Abs(amount) < 0.001 {
		delete(g.positions, symbol)
		return
	}
	g.positions[symbol] = &PositionState{
		Symbol:        symbol,
		Amount:        amount,
		EntryPrice:    entryPrice,
		MarkPrice:     markPrice,
		UnrealizedPNL: 0,
		UpdatedAt:     time.Now(),
	}
}

func (g *LiquidationGuard) Check(ctx context.Context) (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for symbol, pos := range g.positions {
		if pos.Amount == 0 || pos.MarkPrice == 0 {
			continue
		}

		liqPrice := g.calculateLiqPrice(pos.EntryPrice, pos.Amount > 0, g.config.MaxLeverage)
		distanceToLiq := math.Abs(pos.MarkPrice-liqPrice) / liqPrice

		if distanceToLiq < g.config.LiquidationBuffer {
			g.logger.Warn("Liquidation risk high, position should close",
				zap.String("symbol", symbol),
				zap.Float64("mark_price", pos.MarkPrice),
				zap.Float64("liq_price", liqPrice),
				zap.Float64("distance_pct", distanceToLiq*100),
				zap.Float64("buffer_pct", g.config.LiquidationBuffer*100))
			return true, "liquidation_buffer_breached"
		}
	}
	return false, ""
}

func (g *LiquidationGuard) calculateLiqPrice(entryPrice float64, isLong bool, leverage int) float64 {
	liqPrice := entryPrice
	if isLong {
		liqPrice = entryPrice * (1 - float64(leverage-1)/float64(leverage))
	} else {
		liqPrice = entryPrice * (1 + float64(leverage-1)/float64(leverage))
	}
	return liqPrice
}

type MaxPositionGuard struct {
	config        *Config
	logger        *zap.Logger
	totalExposure float64
	positions     map[string]float64 // Per-symbol exposure tracking
	mu            sync.RWMutex
}

func NewMaxPositionGuard(config *Config, logger *zap.Logger) *MaxPositionGuard {
	return &MaxPositionGuard{
		config:    config,
		logger:    logger,
		positions: make(map[string]float64),
	}
}

func (g *MaxPositionGuard) Name() string {
	return "MaxPositionGuard"
}

func (g *MaxPositionGuard) UpdateExposure(symbol string, amount, price float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	exposure := math.Abs(amount * price)
	// Store per-symbol exposure for accurate tracking
	if g.positions == nil {
		g.positions = make(map[string]float64)
	}
	g.positions[symbol] = exposure
	// Calculate total exposure across all symbols
	g.totalExposure = 0
	for _, exp := range g.positions {
		g.totalExposure += exp
	}
}

func (g *MaxPositionGuard) Check(ctx context.Context) (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.totalExposure > g.config.MaxTotalExposureUSDT {
		g.logger.Warn("Total exposure exceeds limit",
			zap.Float64("exposure", g.totalExposure),
			zap.Float64("limit", g.config.MaxTotalExposureUSDT))
		return true, "max_total_exposure_exceeded"
	}
	return false, ""
}

func (g *MaxPositionGuard) GetTotalExposure() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.totalExposure
}

type DailyLossGuard struct {
	config          *Config
	logger          *zap.Logger
	startingBalance float64
	dailyPnL        float64
	mu              sync.RWMutex
	lastReset       time.Time
}

func NewDailyLossGuard(config *Config, logger *zap.Logger) *DailyLossGuard {
	return &DailyLossGuard{
		config:    config,
		logger:    logger,
		lastReset: time.Now(),
	}
}

func (g *DailyLossGuard) Name() string {
	return "DailyLossGuard"
}

func (g *DailyLossGuard) SetStartingBalance(balance float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.startingBalance = balance
}

func (g *DailyLossGuard) RecordPnL(pnl float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dailyPnL += pnl
}

func (g *DailyLossGuard) Check(ctx context.Context) (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.startingBalance == 0 {
		return false, ""
	}

	lossPct := -g.dailyPnL / g.startingBalance
	if lossPct > g.config.DailyLossLimitPct {
		g.logger.Warn("Daily loss limit exceeded",
			zap.Float64("daily_pnl", g.dailyPnL),
			zap.Float64("loss_pct", lossPct*100),
			zap.Float64("limit_pct", g.config.DailyLossLimitPct*100))
		return true, "daily_loss_limit_exceeded"
	}
	return false, ""
}

func (g *DailyLossGuard) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dailyPnL = 0
	g.lastReset = time.Now()
}

func (g *DailyLossGuard) GetDailyPnL() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.dailyPnL
}

type OrderToTradeGuard struct {
	config      *Config
	logger      *zap.Logger
	totalOrders int
	totalFills  int
	mu          sync.RWMutex
}

func NewOrderToTradeGuard(config *Config, logger *zap.Logger) *OrderToTradeGuard {
	return &OrderToTradeGuard{
		config: config,
		logger: logger,
	}
}

func (g *OrderToTradeGuard) Name() string {
	return "OrderToTradeGuard"
}

func (g *OrderToTradeGuard) RecordOrder() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.totalOrders++
}

func (g *OrderToTradeGuard) RecordFill() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.totalFills++
}

func (g *OrderToTradeGuard) Check(ctx context.Context) (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.totalFills == 0 && g.totalOrders > g.config.OrderToTradeLimit*10 {
		g.logger.Warn("Order-to-Trade ratio too high",
			zap.Int("total_orders", g.totalOrders),
			zap.Int("total_fills", g.totalFills),
			zap.Int("limit", g.config.OrderToTradeLimit))
		return true, "order_to_trade_ratio_high"
	}
	return false, ""
}

func (g *OrderToTradeGuard) GetRatio() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.totalFills == 0 {
		return float64(g.totalOrders)
	}
	return float64(g.totalOrders) / float64(g.totalFills)
}

func (g *OrderToTradeGuard) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.totalOrders = 0
	g.totalFills = 0
}

type EmergencyStop struct {
	logger     *zap.Logger
	shouldStop bool
	stopReason string
	mu         sync.RWMutex
}

func NewEmergencyStop(logger *zap.Logger) *EmergencyStop {
	return &EmergencyStop{
		logger: logger,
	}
}

func (e *EmergencyStop) Trigger(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shouldStop = true
	e.stopReason = reason
	e.logger.Error("Emergency stop triggered", zap.String("reason", reason))
}

func (e *EmergencyStop) Check(ctx context.Context) (bool, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.shouldStop, e.stopReason
}

func (e *EmergencyStop) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shouldStop = false
	e.stopReason = ""
}

func (e *EmergencyStop) IsStopped() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.shouldStop
}

// FlowDirectionTracker tracks buy/sell volume ratio to detect toxic flow
type FlowDirectionTracker struct {
	config         *Config
	logger         *zap.Logger
	buyVolume      float64
	sellVolume     float64
	mu             sync.RWMutex
	windowDuration time.Duration
	windowStart    time.Time
}

func NewFlowDirectionTracker(config *Config, logger *zap.Logger) *FlowDirectionTracker {
	return &FlowDirectionTracker{
		config:         config,
		logger:         logger,
		buyVolume:      0,
		sellVolume:     0,
		windowDuration: 60 * time.Second, // 60 second window
		windowStart:    time.Now(),
	}
}

func (f *FlowDirectionTracker) RecordBuy(volume float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkWindowReset()
	f.buyVolume += volume
}

func (f *FlowDirectionTracker) RecordSell(volume float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkWindowReset()
	f.sellVolume += volume
}

func (f *FlowDirectionTracker) checkWindowReset() {
	if time.Since(f.windowStart) > f.windowDuration {
		f.buyVolume = 0
		f.sellVolume = 0
		f.windowStart = time.Now()
	}
}

func (f *FlowDirectionTracker) GetBuyRatio() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	total := f.buyVolume + f.sellVolume
	if total == 0 {
		return 0.5 // Neutral if no data
	}
	return f.buyVolume / total
}

func (f *FlowDirectionTracker) IsToxicFlow() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	buyRatio := f.GetBuyRatio()
	// Toxic if > 60% buy or > 60% sell
	isToxic := buyRatio > f.config.ToxicFlowThreshold || buyRatio < (1-f.config.ToxicFlowThreshold)

	if isToxic {
		f.logger.Warn("⚠️ Toxic flow detected",
			zap.Float64("buy_ratio", buyRatio),
			zap.Float64("threshold", f.config.ToxicFlowThreshold),
			zap.Float64("buy_volume", f.buyVolume),
			zap.Float64("sell_volume", f.sellVolume))
	}
	return isToxic
}

func (f *FlowDirectionTracker) GetReductionFactor() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Inline logic to avoid deadlock (IsToxicFlow also tries to acquire RLock)
	total := f.buyVolume + f.sellVolume
	if total == 0 {
		return 1.0 // No data = no reduction
	}

	buyRatio := f.buyVolume / total
	isToxic := buyRatio > f.config.ToxicFlowThreshold || buyRatio < (1-f.config.ToxicFlowThreshold)

	if !isToxic {
		return 1.0
	}
	return f.config.ToxicFlowReducePct
}

func (f *FlowDirectionTracker) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buyVolume = 0
	f.sellVolume = 0
	f.windowStart = time.Now()
}

// MomentumGuard detects rapid price movements to prevent adverse selection
type MomentumGuard struct {
	config        *Config
	logger        *zap.Logger
	lastPrice     float64
	lastCheckTime time.Time
	mu            sync.RWMutex
}

func NewMomentumGuard(config *Config, logger *zap.Logger) *MomentumGuard {
	return &MomentumGuard{
		config:        config,
		logger:        logger,
		lastPrice:     0,
		lastCheckTime: time.Now(),
	}
}

func (m *MomentumGuard) Name() string {
	return "MomentumGuard"
}

func (m *MomentumGuard) Check(midPrice float64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.lastPrice == 0 || midPrice == 0 {
		m.lastPrice = midPrice
		m.lastCheckTime = time.Now()
		return false, ""
	}

	movePct := math.Abs(midPrice-m.lastPrice) / m.lastPrice
	timePassed := time.Since(m.lastCheckTime)

	// Use config values, with safe defaults
	threshold := m.config.MomentumThresholdPct
	if threshold <= 0 {
		threshold = 0.03 // Safe default: 3%
	}
	timeWindow := time.Duration(m.config.MomentumTimeWindow) * time.Second
	if timeWindow <= 0 {
		timeWindow = 30 * time.Second // Safe default: 30s
	}

	if timePassed < timeWindow && movePct > threshold {
		m.logger.Warn("⚡ High Momentum Detected",
			zap.Float64("last_price", m.lastPrice),
			zap.Float64("current_price", midPrice),
			zap.Float64("move_pct", movePct*100),
			zap.Duration("time_passed", timePassed),
			zap.Float64("threshold_pct", threshold*100))
		m.lastPrice = midPrice
		m.lastCheckTime = time.Now()
		return true, "high_momentum"
	}

	m.lastPrice = midPrice
	m.lastCheckTime = time.Now()
	return false, ""
}

func (m *MomentumGuard) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastPrice = 0
	m.lastCheckTime = time.Now()
}
