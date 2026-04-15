package agentic

import (
	"sync"
	"time"

	"aster-bot/internal/config"

	"go.uber.org/zap"
)

// SymbolDecisionState holds circuit breaker and mode decision state for a single symbol
type SymbolDecisionState struct {
	isTripped         bool
	tripTime          time.Time
	reason            string
	consecutiveLosses int
	lastTradeOutcome  string // "win" or "loss"
	atrHistory        []float64

	// Market condition tracking for sophisticated reset checks
	bbWidthHistory []float64
	priceHistory   []float64
	volumeHistory  []float64
	adxHistory     []float64

	// Trading mode decision (NEW - from ModeManager)
	tradingMode string
	modeSince   time.Time
}

// CircuitBreaker monitors market conditions and trading performance per-symbol
type CircuitBreaker struct {
	config config.AgenticCircuitBreakerConfig
	logger *zap.Logger

	mu                 sync.RWMutex
	symbolStates       map[string]*SymbolDecisionState
	maxATRHistory      int
	evaluationInterval time.Duration // How often to evaluate market conditions (default 3s)
	stopCh             chan struct{}

	// Callback when circuit breaker trips for a symbol
	// Used to trigger emergency exit in farming engine
	onTripCallback func(symbol, reason string)

	// Callback when circuit breaker resets for a symbol
	// Used to trigger force placement in farming engine
	onResetCallback func(symbol string)

	// Callback when trading mode changes for a symbol
	// Used to notify mode transitions
	onModeChangeCallback func(symbol string, oldMode, newMode string)
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(cfg config.AgenticCircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		config:             cfg,
		logger:             logger.With(zap.String("component", "circuit_breaker")),
		symbolStates:       make(map[string]*SymbolDecisionState),
		maxATRHistory:      20,
		evaluationInterval: 3 * time.Second,
		stopCh:             make(chan struct{}),
	}
}

// Start starts the evaluation worker that checks market conditions periodically
func (cb *CircuitBreaker) Start() {
	cb.logger.Info("Starting circuit breaker evaluation worker",
		zap.Duration("interval", cb.evaluationInterval))

	go cb.evaluationLoop()
}

// Stop stops the evaluation worker
func (cb *CircuitBreaker) Stop() {
	cb.logger.Info("Stopping circuit breaker evaluation worker")
	close(cb.stopCh)
}

// evaluationLoop periodically evaluates market conditions for all symbols
func (cb *CircuitBreaker) evaluationLoop() {
	ticker := time.NewTicker(cb.evaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cb.evaluateAllSymbols()
		case <-cb.stopCh:
			cb.logger.Info("Circuit breaker evaluation worker stopped")
			return
		}
	}
}

// evaluateAllSymbols checks market conditions for all symbols and resets if safe
func (cb *CircuitBreaker) evaluateAllSymbols() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	for symbol, state := range cb.symbolStates {
		if !state.isTripped {
			continue
		}

		// Check if market conditions have stabilized
		if cb.shouldResetSymbol(symbol, state) {
			cb.logger.Info("Market conditions stabilized, resetting circuit breaker",
				zap.String("symbol", symbol),
				zap.Duration("trip_duration", time.Since(state.tripTime)))

			cb.resetSymbol(symbol, state)

			// Call callback to trigger force placement
			if cb.onResetCallback != nil {
				cb.onResetCallback(symbol)
			}
		}
	}
}

// shouldResetSymbol determines if a symbol's circuit breaker should be reset
func (cb *CircuitBreaker) shouldResetSymbol(symbol string, state *SymbolDecisionState) bool {
	// INTENTIONAL DESIGN: Trip at ADX > 25, reset when ADX < 20 (buffer zone 20-25)
	// This prevents rapid mode switching during marginal volatility

	// Check 1: ATR-based volatility check
	if len(state.atrHistory) > 0 {
		avgATR := cb.averageATR(state.atrHistory)
		if avgATR < 0.005 { // ATR < 0.5% - low volatility
			cb.logger.Debug("ATR indicates low volatility, considering reset",
				zap.String("symbol", symbol),
				zap.Float64("avgATR", avgATR))
			// Don't return true yet, need to check other conditions too
		} else {
			cb.logger.Debug("ATR still high, not resetting",
				zap.String("symbol", symbol),
				zap.Float64("avgATR", avgATR))
			return false
		}
	}

	// Check 2: BB width normalization (width < threshold → stable)
	if len(state.bbWidthHistory) > 0 {
		avgBBWidth := cb.averageATR(state.bbWidthHistory)
		if avgBBWidth < 0.01 { // BB width < 1% - tight range, stable
			cb.logger.Debug("BB width indicates stable range",
				zap.String("symbol", symbol),
				zap.Float64("avgBBWidth", avgBBWidth))
		} else {
			cb.logger.Debug("BB width still wide, not resetting",
				zap.String("symbol", symbol),
				zap.Float64("avgBBWidth", avgBBWidth))
			return false
		}
	}

	// Check 3: Price stability (no large swings in recent candles)
	if len(state.priceHistory) >= 3 {
		priceSwing := cb.calculatePriceSwing(state.priceHistory)
		if priceSwing < 0.003 { // Price swing < 0.3% - stable
			cb.logger.Debug("Price stable, considering reset",
				zap.String("symbol", symbol),
				zap.Float64("priceSwing", priceSwing))
		} else {
			cb.logger.Debug("Price still swinging, not resetting",
				zap.String("symbol", symbol),
				zap.Float64("priceSwing", priceSwing))
			return false
		}
	}

	// Check 4: Volume normalization (volume spike subsided)
	if len(state.volumeHistory) >= 5 {
		volumeSpike := cb.calculateVolumeSpike(state.volumeHistory)
		if volumeSpike < 2.0 { // Volume < 2x average - normal
			cb.logger.Debug("Volume normal, considering reset",
				zap.String("symbol", symbol),
				zap.Float64("volumeSpike", volumeSpike))
		} else {
			cb.logger.Debug("Volume still spiked, not resetting",
				zap.String("symbol", symbol),
				zap.Float64("volumeSpike", volumeSpike))
			return false
		}
	}

	// Check 5: ADX check (ADX < 20 to exit buffer zone)
	if len(state.adxHistory) > 0 {
		latestADX := state.adxHistory[len(state.adxHistory)-1]
		if latestADX < 20.0 { // ADX < 20 - exited buffer zone
			cb.logger.Debug("ADX below threshold, market stabilized",
				zap.String("symbol", symbol),
				zap.Float64("ADX", latestADX))
		} else {
			cb.logger.Debug("ADX still in buffer zone, not resetting",
				zap.String("symbol", symbol),
				zap.Float64("ADX", latestADX))
			return false
		}
	}

	// All checks passed - safe to reset
	cb.logger.Info("All market conditions stabilized, resetting circuit breaker",
		zap.String("symbol", symbol))
	return true
}

// calculatePriceSwing calculates max price swing percentage from price history
func (cb *CircuitBreaker) calculatePriceSwing(history []float64) float64 {
	if len(history) < 2 {
		return 0
	}

	minPrice := history[0]
	maxPrice := history[0]

	for _, price := range history {
		if price < minPrice {
			minPrice = price
		}
		if price > maxPrice {
			maxPrice = price
		}
	}

	if minPrice == 0 {
		return 0
	}

	return (maxPrice - minPrice) / minPrice
}

// calculateVolumeSpike calculates volume spike ratio (current / average)
func (cb *CircuitBreaker) calculateVolumeSpike(history []float64) float64 {
	if len(history) < 2 {
		return 0
	}

	latest := history[len(history)-1]
	sum := 0.0
	for i := 0; i < len(history)-1; i++ {
		sum += history[i]
	}
	avg := sum / float64(len(history)-1)

	if avg == 0 {
		return 0
	}

	return latest / avg
}

// averageATR calculates average ATR from history
func (cb *CircuitBreaker) averageATR(history []float64) float64 {
	if len(history) == 0 {
		return 0
	}
	sum := 0.0
	for _, atr := range history {
		sum += atr
	}
	return sum / float64(len(history))
}

// resetSymbol resets circuit breaker for a specific symbol
func (cb *CircuitBreaker) resetSymbol(symbol string, state *SymbolDecisionState) {
	state.isTripped = false
	state.reason = ""
	state.consecutiveLosses = 0
}

// Check checks if circuit breaker should trip for a specific symbol
func (cb *CircuitBreaker) CheckSymbol(symbol string, score SymbolScore, currentATR float64) (bool, string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Get or create state for symbol
	state, exists := cb.symbolStates[symbol]
	if !exists {
		state = &SymbolDecisionState{
			atrHistory:  make([]float64, 0, cb.maxATRHistory),
			tradingMode: "MICRO",
			modeSince:   time.Now(),
		}
		cb.symbolStates[symbol] = state
	}

	// Update ATR history
	cb.updateATRHistory(state, currentATR)

	// If already tripped, check if we should reset
	if state.isTripped {
		return true, state.reason
	}

	// Check volatility spike
	if cb.config.VolatilitySpike.Enabled {
		if cb.checkVolatilitySpikeForSymbol(state, score) {
			cb.tripSymbol(symbol, state, "volatility_spike")
			return true, "Volatility spike detected"
		}
	}

	// Check consecutive losses
	if cb.config.ConsecutiveLosses.Enabled {
		if cb.checkConsecutiveLossesForSymbol(state) {
			cb.tripSymbol(symbol, state, "consecutive_losses")
			return true, "Consecutive losses detected"
		}
	}

	return false, ""
}

// Check checks if ANY symbol has tripped circuit breaker (legacy method for compatibility)
func (cb *CircuitBreaker) Check(scores map[string]SymbolScore) (bool, string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	for symbol := range scores {
		state, exists := cb.symbolStates[symbol]
		if exists && state.isTripped {
			return true, state.reason
		}
	}

	return false, ""
}

// updateATRHistory updates ATR history for a symbol
func (cb *CircuitBreaker) updateATRHistory(state *SymbolDecisionState, atr float64) {
	state.atrHistory = append(state.atrHistory, atr)
	if len(state.atrHistory) > cb.maxATRHistory {
		state.atrHistory = state.atrHistory[1:]
	}
}

// checkVolatilitySpikeForSymbol checks if volatility spike condition is met for a symbol
func (cb *CircuitBreaker) checkVolatilitySpikeForSymbol(state *SymbolDecisionState, score SymbolScore) bool {
	if len(state.atrHistory) < 5 {
		return false
	}

	// Calculate ATR spike
	avgATR := cb.averageATR(state.atrHistory)
	latestATR := state.atrHistory[len(state.atrHistory)-1]
	multiplier := cb.config.VolatilitySpike.ATRMultiplier
	if multiplier == 0 {
		multiplier = 3.0
	}

	return latestATR > avgATR*multiplier
}

// checkConsecutiveLossesForSymbol checks if consecutive losses threshold is met for a symbol
func (cb *CircuitBreaker) checkConsecutiveLossesForSymbol(state *SymbolDecisionState) bool {
	threshold := cb.config.ConsecutiveLosses.Threshold
	if threshold == 0 {
		threshold = 3
	}
	return state.consecutiveLosses >= threshold
}

// tripSymbol trips circuit breaker for a specific symbol
func (cb *CircuitBreaker) tripSymbol(symbol string, state *SymbolDecisionState, reason string) {
	wasAlreadyTripped := state.isTripped
	state.isTripped = true
	state.tripTime = time.Now()
	state.reason = reason

	cb.logger.Warn("Circuit breaker TRIPPED for symbol",
		zap.String("symbol", symbol),
		zap.String("reason", reason),
		zap.Time("time", state.tripTime))

	// Call callback on EVERY trip to trigger emergency exit
	// This ensures positions are closed even after circuit breaker resets and trips again
	if cb.onTripCallback != nil {
		cb.logger.Info("Calling onTripCallback to trigger emergency exit",
			zap.String("symbol", symbol),
			zap.Bool("was_already_tripped", wasAlreadyTripped))
		cb.onTripCallback(symbol, reason)
	}
}

// RecordTradeOutcome records trade outcome for a symbol
func (cb *CircuitBreaker) RecordTradeOutcome(symbol string, isWin bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state, exists := cb.symbolStates[symbol]
	if !exists {
		state = &SymbolDecisionState{
			atrHistory:     make([]float64, 0, cb.maxATRHistory),
			bbWidthHistory: make([]float64, 0, cb.maxATRHistory),
			priceHistory:   make([]float64, 0, cb.maxATRHistory),
			volumeHistory:  make([]float64, 0, cb.maxATRHistory),
			adxHistory:     make([]float64, 0, cb.maxATRHistory),
			tradingMode:    "MICRO",
			modeSince:      time.Now(),
		}
		cb.symbolStates[symbol] = state
	}

	if isWin {
		state.consecutiveLosses = 0
		state.lastTradeOutcome = "win"
	} else {
		state.consecutiveLosses++
		state.lastTradeOutcome = "loss"
	}
}

// UpdateMarketConditions updates market condition data for a symbol
func (cb *CircuitBreaker) UpdateMarketConditions(symbol string, bbWidth, price, volume, adx float64) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state, exists := cb.symbolStates[symbol]
	if !exists {
		state = &SymbolDecisionState{
			atrHistory:     make([]float64, 0, cb.maxATRHistory),
			bbWidthHistory: make([]float64, 0, cb.maxATRHistory),
			priceHistory:   make([]float64, 0, cb.maxATRHistory),
			volumeHistory:  make([]float64, 0, cb.maxATRHistory),
			adxHistory:     make([]float64, 0, cb.maxATRHistory),
			tradingMode:    "MICRO",
			modeSince:      time.Now(),
		}
		cb.symbolStates[symbol] = state
	}

	// Update BB width history
	state.bbWidthHistory = append(state.bbWidthHistory, bbWidth)
	if len(state.bbWidthHistory) > cb.maxATRHistory {
		state.bbWidthHistory = state.bbWidthHistory[1:]
	}

	// Update price history
	state.priceHistory = append(state.priceHistory, price)
	if len(state.priceHistory) > cb.maxATRHistory {
		state.priceHistory = state.priceHistory[1:]
	}

	// Update volume history
	state.volumeHistory = append(state.volumeHistory, volume)
	if len(state.volumeHistory) > cb.maxATRHistory {
		state.volumeHistory = state.volumeHistory[1:]
	}

	// Update ADX history
	state.adxHistory = append(state.adxHistory, adx)
	if len(state.adxHistory) > cb.maxATRHistory {
		state.adxHistory = state.adxHistory[1:]
	}
}

// IsTripped returns whether circuit breaker is tripped for a specific symbol
func (cb *CircuitBreaker) IsTripped(symbol string) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state, exists := cb.symbolStates[symbol]
	if !exists {
		return false
	}
	return state.isTripped
}

// GetTrippedSymbols returns list of symbols with tripped circuit breakers
func (cb *CircuitBreaker) GetTrippedSymbols() []string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	symbols := make([]string, 0)
	for symbol, state := range cb.symbolStates {
		if state.isTripped {
			symbols = append(symbols, symbol)
		}
	}
	return symbols
}

// SetOnTripCallback sets the callback function to be called when circuit breaker trips
func (cb *CircuitBreaker) SetOnTripCallback(callback func(symbol, reason string)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onTripCallback = callback
}

// SetOnResetCallback sets the callback function to be called when circuit breaker resets
func (cb *CircuitBreaker) SetOnResetCallback(callback func(symbol string)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onResetCallback = callback
}

// SetOnModeChangeCallback sets the callback function to be called when trading mode changes
func (cb *CircuitBreaker) SetOnModeChangeCallback(callback func(symbol string, oldMode, newMode string)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onModeChangeCallback = callback
}

// determineTradingMode determines what trading mode we should be in based on market conditions
// This logic is copied from ModeManager but adapted for CircuitBreaker
func (cb *CircuitBreaker) determineTradingMode(
	rangeState int, // adaptive_grid.RangeState as int
	adx float64,
	isBreakout bool,
	isTrending bool,
	isVolatilitySpike bool,
) string {
	// Hardcoded thresholds (matching ModeManager defaults)
	sidewaysThreshold := 20.0
	trendingThreshold := 25.0

	// Priority 1: Volatility spike -> COOLDOWN (always)
	if isVolatilitySpike {
		return "COOLDOWN"
	}

	// Priority 1b: Breakout with momentum (high ADX or trending) -> COOLDOWN
	// Breakout without momentum (sideways) -> MICRO mode (continue trading)
	if isBreakout {
		if isTrending || adx > trendingThreshold {
			// Strong breakout with trend momentum -> COOLDOWN
			return "COOLDOWN"
		}
		// Weak breakout without momentum -> MICRO mode (continue trading)
		return "MICRO"
	}

	// Priority 2: Strong trend -> TREND_ADAPTED
	if isTrending || adx > trendingThreshold {
		return "TREND_ADAPTED"
	}

	// Priority 3: BB Range Active (state=2) + Low ADX -> STANDARD
	if rangeState == 2 && adx < sidewaysThreshold {
		return "STANDARD"
	}

	// Priority 4: Default to MICRO mode (always trade)
	return "MICRO"
}

// evaluateSymbol evaluates both circuit breaker state and trading mode for a symbol
// Returns (canTrade, tradingMode)
func (cb *CircuitBreaker) evaluateSymbol(
	symbol string,
	state *SymbolDecisionState,
	rangeState int,
	adx float64,
	isBreakout bool,
	isTrending bool,
	isVolatilitySpike bool,
) (bool, string) {
	// Determine trading mode based on market conditions
	targetMode := cb.determineTradingMode(rangeState, adx, isBreakout, isTrending, isVolatilitySpike)

	// Update trading mode if changed
	if state.tradingMode != targetMode {
		oldMode := state.tradingMode
		state.tradingMode = targetMode
		state.modeSince = time.Now()

		cb.logger.Info("Trading mode changed",
			zap.String("symbol", symbol),
			zap.String("oldMode", oldMode),
			zap.String("newMode", targetMode))

		// Trigger callback if set
		if cb.onModeChangeCallback != nil {
			cb.onModeChangeCallback(symbol, oldMode, targetMode)
		}
	}

	// If circuit breaker is tripped, cannot trade
	if state.isTripped {
		return false, state.tradingMode
	}

	// If in COOLDOWN mode, cannot trade
	if state.tradingMode == "COOLDOWN" {
		return false, state.tradingMode
	}

	// Can trade
	return true, state.tradingMode
}

// GetStatus returns the current circuit breaker status for all symbols
func (cb *CircuitBreaker) GetStatus() map[string]bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	status := make(map[string]bool)
	for symbol, state := range cb.symbolStates {
		status[symbol] = state.isTripped
	}
	return status
}

// GetSymbolDecision returns the trading decision for a symbol
// Returns (canTrade, tradingMode)
func (cb *CircuitBreaker) GetSymbolDecision(symbol string) (bool, string) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state, exists := cb.symbolStates[symbol]
	if !exists {
		// New symbol - can trade with default MICRO mode
		return true, "MICRO"
	}

	// If circuit breaker is tripped, cannot trade
	if state.isTripped {
		return false, state.tradingMode
	}

	// If in COOLDOWN mode, cannot trade
	if state.tradingMode == "COOLDOWN" {
		return false, state.tradingMode
	}

	// Can trade
	return true, state.tradingMode
}
