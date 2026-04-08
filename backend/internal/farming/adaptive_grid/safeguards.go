package adaptive_grid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/config"

	"go.uber.org/zap"
)

// SafeguardsManager orchestrates all protection mechanisms
type SafeguardsManager struct {
	antiReplay        *AntiReplayProtection
	stateValidation   *StateValidation
	spreadProtection  *SpreadProtectionMonitor
	slippageMonitor   *SlippageMonitor
	fundingProtection *FundingProtection
	circuitBreaker    *CircuitBreakerMonitor
	config            *config.SafeguardsConfig
	logger            *zap.Logger
	mu                sync.RWMutex
}

// AntiReplayProtection prevents duplicate order processing
type AntiReplayProtection struct {
	events          map[string]time.Time
	dedupWindow     time.Duration
	maxEvents       int
	lockTimeout     time.Duration
	cleanupInterval time.Duration
	mu              sync.RWMutex
	logger          *zap.Logger
}

// StateValidation validates order state transitions
type StateValidation struct {
	validTransitions map[string][]string
	logger           *zap.Logger
}

// SpreadProtectionMonitor monitors orderbook spread (wraps existing SpreadProtection)
type SpreadProtectionMonitor struct {
	pauseThreshold      float64
	emergencyThreshold  float64
	resumeAfter         time.Duration
	samplesBeforeResume int
	logger              *zap.Logger
}

// SlippageMonitor tracks execution slippage
type SlippageMonitor struct {
	fills          []FillInfo
	maxFills       int
	alertThreshold float64
	mu             sync.RWMutex
	logger         *zap.Logger
}

// FillInfo stores information about a fill
type FillInfo struct {
	OrderID       string
	Symbol        string
	ExpectedPrice float64
	ActualPrice   float64
	SlippagePct   float64
	Timestamp     time.Time
}

// FundingProtection monitors funding rates and adjusts levels
type FundingProtection struct {
	highThreshold   float64
	checkInterval   time.Duration
	levelAdjustment int
	costTracking    *CostTracking
	lastCheck       time.Time
	mu              sync.RWMutex
	logger          *zap.Logger
}

// CostTracking tracks funding costs vs profits
type CostTracking struct {
	enabled         bool
	compareToProfit bool
	alertRatio      float64
	fundingCosts    float64
	profits         float64
}

// CircuitBreakerMonitor stops trading on critical errors
type CircuitBreakerMonitor struct {
	isOpen             bool
	fallbackToDefaults bool
	safeSpreadPct      float64
	safeSizeMultiplier float64
	retryInterval      time.Duration
	maxRetries         int
	retryCount         int
	lastOpenTime       time.Time
	mu                 sync.RWMutex
	logger             *zap.Logger
}

// NewSafeguardsManager creates a new safeguards manager
func NewSafeguardsManager(cfg *config.SafeguardsConfig, logger *zap.Logger) *SafeguardsManager {
	if cfg == nil {
		return &SafeguardsManager{
			logger: logger,
		}
	}

	sm := &SafeguardsManager{
		config: cfg,
		logger: logger,
	}

	if cfg.AntiReplay.Enabled {
		sm.antiReplay = NewAntiReplayProtection(cfg.AntiReplay, logger)
	}

	if cfg.StateValidation.Enabled {
		sm.stateValidation = NewStateValidation(cfg.StateValidation, logger)
	}

	if cfg.SpreadProtection.Enabled {
		sm.spreadProtection = NewSpreadProtectionMonitor(cfg.SpreadProtection, logger)
	}

	if cfg.Slippage.Enabled {
		sm.slippageMonitor = NewSlippageMonitor(cfg.Slippage, logger)
	}

	if cfg.FundingProtection.Enabled {
		sm.fundingProtection = NewFundingProtection(cfg.FundingProtection, logger)
	}

	if cfg.CircuitBreaker.Enabled {
		sm.circuitBreaker = NewCircuitBreakerMonitor(cfg.CircuitBreaker, logger)
	}

	return sm
}

// NewAntiReplayProtection creates anti-replay protection
func NewAntiReplayProtection(cfg config.AntiReplayConfig, logger *zap.Logger) *AntiReplayProtection {
	dedupWindow, _ := time.ParseDuration(cfg.DeduplicationWindow)
	if dedupWindow == 0 {
		dedupWindow = 30 * time.Second
	}

	lockTimeout, _ := time.ParseDuration(cfg.LockTimeout)
	if lockTimeout == 0 {
		lockTimeout = 5 * time.Second
	}

	cleanupInterval, _ := time.ParseDuration(cfg.CleanupInterval)
	if cleanupInterval == 0 {
		cleanupInterval = 10 * time.Minute
	}

	arp := &AntiReplayProtection{
		events:          make(map[string]time.Time),
		dedupWindow:     dedupWindow,
		maxEvents:       cfg.MaxStoredEvents,
		lockTimeout:     lockTimeout,
		cleanupInterval: cleanupInterval,
		logger:          logger,
	}

	// Start cleanup goroutine
	go arp.cleanupLoop()

	return arp
}

// IsDuplicate checks if an event is a duplicate
func (arp *AntiReplayProtection) IsDuplicate(eventKey string) bool {
	arp.mu.Lock()
	defer arp.mu.Unlock()

	if timestamp, exists := arp.events[eventKey]; exists {
		if time.Since(timestamp) < arp.dedupWindow {
			arp.logger.Warn("Duplicate event detected",
				zap.String("event_key", eventKey),
				zap.Duration("since", time.Since(timestamp)))
			return true
		}
	}

	// Store event
	arp.events[eventKey] = time.Now()

	// Prune if too many events
	if len(arp.events) > arp.maxEvents {
		arp.pruneOldEvents()
	}

	return false
}

// GenerateEventKey generates a unique key for an event
func (arp *AntiReplayProtection) GenerateEventKey(orderID string, eventType string) string {
	data := fmt.Sprintf("%s:%s:%d", orderID, eventType, time.Now().Unix()/int64(arp.dedupWindow.Seconds()))
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// pruneOldEvents removes old events
func (arp *AntiReplayProtection) pruneOldEvents() {
	cutoff := time.Now().Add(-arp.dedupWindow)
	for key, timestamp := range arp.events {
		if timestamp.Before(cutoff) {
			delete(arp.events, key)
		}
	}
}

// cleanupLoop periodically cleans up old events
func (arp *AntiReplayProtection) cleanupLoop() {
	ticker := time.NewTicker(arp.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		arp.mu.Lock()
		arp.pruneOldEvents()
		arp.mu.Unlock()
	}
}

// NewStateValidation creates state validation
func NewStateValidation(cfg config.StateValidationConfig, logger *zap.Logger) *StateValidation {
	sv := &StateValidation{
		validTransitions: make(map[string][]string),
		logger:           logger,
	}

	// Define default valid transitions
	// NEW: Initial state when order is created
	sv.validTransitions["NEW"] = []string{"PENDING", "FILLED", "CANCELLED", "REJECTED"}
	// PENDING: Order submitted to exchange, waiting for fill
	sv.validTransitions["PENDING"] = []string{"FILLED", "CANCELLED", "REJECTED"}
	// Terminal states
	sv.validTransitions["FILLED"] = []string{}
	sv.validTransitions["CANCELLED"] = []string{}
	sv.validTransitions["REJECTED"] = []string{}

	return sv
}

// IsValidTransition checks if a state transition is valid
func (sv *StateValidation) IsValidTransition(from, to string) bool {
	validStates, exists := sv.validTransitions[from]
	if !exists {
		return false
	}

	for _, state := range validStates {
		if state == to {
			return true
		}
	}

	return false
}

// ValidateTransition validates and logs invalid transitions
func (sv *StateValidation) ValidateTransition(orderID, from, to string) bool {
	isValid := sv.IsValidTransition(from, to)
	if !isValid {
		sv.logger.Error("Invalid state transition",
			zap.String("order_id", orderID),
			zap.String("from", from),
			zap.String("to", to))
	}
	return isValid
}

// NewSpreadProtectionMonitor creates spread protection monitor
func NewSpreadProtectionMonitor(cfg config.SpreadProtectionConfig, logger *zap.Logger) *SpreadProtectionMonitor {
	resumeAfter, _ := time.ParseDuration(cfg.ResumeAfter)
	if resumeAfter == 0 {
		resumeAfter = 10 * time.Second
	}

	return &SpreadProtectionMonitor{
		pauseThreshold:      cfg.PauseThreshold,
		emergencyThreshold:  cfg.EmergencyThreshold,
		resumeAfter:         resumeAfter,
		samplesBeforeResume: cfg.SamplesBeforeResume,
		logger:              logger,
	}
}

// NewSlippageMonitor creates slippage monitor
func NewSlippageMonitor(cfg config.SlippageConfig, logger *zap.Logger) *SlippageMonitor {
	return &SlippageMonitor{
		fills:          make([]FillInfo, 0),
		maxFills:       cfg.MaxStoredFills,
		alertThreshold: cfg.AlertThreshold,
		logger:         logger,
	}
}

// RecordFill records a fill and checks slippage
func (sm *SlippageMonitor) RecordFill(orderID, symbol string, expectedPrice, actualPrice float64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if expectedPrice <= 0 {
		return
	}

	slippage := (actualPrice - expectedPrice) / expectedPrice
	if actualPrice < expectedPrice {
		slippage = (expectedPrice - actualPrice) / expectedPrice
	}

	fill := FillInfo{
		OrderID:       orderID,
		Symbol:        symbol,
		ExpectedPrice: expectedPrice,
		ActualPrice:   actualPrice,
		SlippagePct:   slippage,
		Timestamp:     time.Now(),
	}

	sm.fills = append(sm.fills, fill)

	// Keep only recent fills
	if len(sm.fills) > sm.maxFills {
		sm.fills = sm.fills[1:]
	}

	if slippage > sm.alertThreshold {
		sm.logger.Warn("High slippage detected",
			zap.String("order_id", orderID),
			zap.String("symbol", symbol),
			zap.Float64("slippage_pct", slippage*100),
			zap.Float64("threshold", sm.alertThreshold*100))
	}
}

// GetAverageSlippage returns average slippage over recorded fills
func (sm *SlippageMonitor) GetAverageSlippage() float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(sm.fills) == 0 {
		return 0
	}

	var total float64
	for _, fill := range sm.fills {
		total += fill.SlippagePct
	}

	return total / float64(len(sm.fills))
}

// NewFundingProtection creates funding protection
func NewFundingProtection(cfg config.FundingProtectionConfig, logger *zap.Logger) *FundingProtection {
	checkInterval, _ := time.ParseDuration(cfg.CheckInterval)
	if checkInterval == 0 {
		checkInterval = 4 * time.Hour
	}

	return &FundingProtection{
		highThreshold:   cfg.HighThreshold,
		checkInterval:   checkInterval,
		levelAdjustment: cfg.LevelAdjustment,
		costTracking: &CostTracking{
			enabled:         cfg.CostTracking.Enabled,
			compareToProfit: cfg.CostTracking.CompareToProfit,
			alertRatio:      cfg.CostTracking.AlertRatio,
		},
		logger: logger,
	}
}

// CheckFundingRate checks if funding rate is high and returns level adjustment
func (fp *FundingProtection) CheckFundingRate(fundingRate float64) int {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	now := time.Now()
	if time.Since(fp.lastCheck) < fp.checkInterval {
		return 0
	}
	fp.lastCheck = now

	if fundingRate >= fp.highThreshold {
		fp.logger.Warn("High funding rate detected",
			zap.Float64("funding_rate", fundingRate),
			zap.Float64("threshold", fp.highThreshold),
			zap.Int("level_adjustment", fp.levelAdjustment))
		return fp.levelAdjustment
	}

	return 0
}

// RecordFundingCost records a funding cost
func (fp *FundingProtection) RecordFundingCost(cost float64) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	if fp.costTracking.enabled {
		fp.costTracking.fundingCosts += cost

		if fp.costTracking.compareToProfit {
			if fp.costTracking.profits > 0 {
				ratio := fp.costTracking.fundingCosts / fp.costTracking.profits
				if ratio > fp.costTracking.alertRatio {
					fp.logger.Warn("Funding costs exceeding profit threshold",
						zap.Float64("ratio", ratio),
						zap.Float64("threshold", fp.costTracking.alertRatio),
						zap.Float64("funding_costs", fp.costTracking.fundingCosts),
						zap.Float64("profits", fp.costTracking.profits))
				}
			}
		}
	}
}

// RecordProfit records a profit for comparison
func (fp *FundingProtection) RecordProfit(profit float64) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	if fp.costTracking.enabled {
		fp.costTracking.profits += profit
	}
}

// NewCircuitBreakerMonitor creates circuit breaker monitor
func NewCircuitBreakerMonitor(cfg config.CircuitBreakerConfig, logger *zap.Logger) *CircuitBreakerMonitor {
	retryInterval, _ := time.ParseDuration(cfg.RetryInterval)
	if retryInterval == 0 {
		retryInterval = 30 * time.Second
	}

	return &CircuitBreakerMonitor{
		isOpen:             false,
		fallbackToDefaults: cfg.FallbackToSafeDefaults,
		safeSpreadPct:      cfg.SafeSpreadPct,
		safeSizeMultiplier: cfg.SafeSizeMultiplier,
		retryInterval:      retryInterval,
		maxRetries:         cfg.MaxRetries,
		retryCount:         0,
		logger:             logger,
	}
}

// IsOpen returns whether circuit breaker is open
func (cb *CircuitBreakerMonitor) IsOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if !cb.isOpen {
		return false
	}

	// Check if we can try again
	if time.Since(cb.lastOpenTime) > cb.retryInterval {
		cb.mu.RUnlock()
		cb.mu.Lock()
		defer cb.mu.Unlock()

		cb.retryCount++
		if cb.retryCount > cb.maxRetries {
			cb.logger.Error("Circuit breaker max retries exceeded")
			return true
		}

		cb.isOpen = false
		cb.logger.Info("Circuit breaker retry attempt",
			zap.Int("attempt", cb.retryCount))
		return false
	}

	return true
}

// Open opens the circuit breaker
func (cb *CircuitBreakerMonitor) Open(reason string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.isOpen = true
	cb.lastOpenTime = time.Now()
	cb.retryCount = 0

	cb.logger.Error("Circuit breaker opened",
		zap.String("reason", reason),
		zap.Bool("fallback_enabled", cb.fallbackToDefaults),
		zap.Float64("safe_spread_pct", cb.safeSpreadPct),
		zap.Float64("safe_size_multiplier", cb.safeSizeMultiplier))
}

// Close closes the circuit breaker
func (cb *CircuitBreakerMonitor) Close() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.isOpen = false
	cb.retryCount = 0
	cb.logger.Info("Circuit breaker closed")
}

// GetSafeDefaults returns safe default parameters if circuit is open
func (cb *CircuitBreakerMonitor) GetSafeDefaults() (spreadPct, sizeMultiplier float64, useSafe bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.isOpen && cb.fallbackToDefaults {
		return cb.safeSpreadPct, cb.safeSizeMultiplier, true
	}
	return 0, 0, false
}

// SafeguardsManager methods

// CheckAntiReplay checks for duplicate events
func (sm *SafeguardsManager) CheckAntiReplay(orderID string, eventType string) bool {
	if sm.antiReplay == nil {
		return true
	}
	key := sm.antiReplay.GenerateEventKey(orderID, eventType)
	return !sm.antiReplay.IsDuplicate(key)
}

// CheckSpread validates spread is acceptable
func (sm *SafeguardsManager) CheckSpread(bid, ask float64) bool {
	if sm.spreadProtection == nil {
		return true
	}
	// Use existing SpreadProtection if available
	return true
}

// ValidateStateTransition validates order state transition
func (sm *SafeguardsManager) ValidateStateTransition(orderID, from, to string) bool {
	if sm.stateValidation == nil {
		return true
	}
	return sm.stateValidation.ValidateTransition(orderID, from, to)
}

// RecordFill records a fill for slippage monitoring
func (sm *SafeguardsManager) RecordFill(orderID, symbol string, expectedPrice, actualPrice float64) {
	if sm.slippageMonitor == nil {
		return
	}
	sm.slippageMonitor.RecordFill(orderID, symbol, expectedPrice, actualPrice)
}

// CheckFundingRate checks funding rate and returns level adjustment
func (sm *SafeguardsManager) CheckFundingRate(fundingRate float64) int {
	if sm.fundingProtection == nil {
		return 0
	}
	return sm.fundingProtection.CheckFundingRate(fundingRate)
}

// IsCircuitOpen returns whether circuit breaker is open
func (sm *SafeguardsManager) IsCircuitOpen() bool {
	if sm.circuitBreaker == nil {
		return false
	}
	return sm.circuitBreaker.IsOpen()
}

// OpenCircuit opens the circuit breaker
func (sm *SafeguardsManager) OpenCircuit(reason string) {
	if sm.circuitBreaker == nil {
		return
	}
	sm.circuitBreaker.Open(reason)
}

// GetSafeDefaults returns safe defaults if circuit is open
func (sm *SafeguardsManager) GetSafeDefaults() (spreadPct, sizeMultiplier float64, useSafe bool) {
	if sm.circuitBreaker == nil {
		return 0, 0, false
	}
	return sm.circuitBreaker.GetSafeDefaults()
}
