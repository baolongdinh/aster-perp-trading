package agentic

import (
	"sync"
	"time"

	"aster-bot/internal/config"

	"go.uber.org/zap"
)

// CircuitBreaker monitors market conditions and trading performance
type CircuitBreaker struct {
	config       config.AgenticCircuitBreakerConfig
	logger       *zap.Logger

	mu           sync.RWMutex
	isTripped    bool
	lastTripTime time.Time
	reason       string

	// Consecutive loss tracking
	consecutiveLosses int
	lastTradeOutcome  string // "win" or "loss"

	// Volatility tracking
	atrHistory []float64
	maxATRHistory int
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(cfg config.AgenticCircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		config:          cfg,
		logger:          logger.With(zap.String("component", "circuit_breaker")),
		atrHistory:      make([]float64, 0, 20),
		maxATRHistory:   20,
	}
}

// Check checks if circuit breaker should trip
func (cb *CircuitBreaker) Check(scores map[string]SymbolScore) (bool, string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Already tripped, check if we should reset
	if cb.isTripped {
		// Auto-reset after 5 minutes
		if time.Since(cb.lastTripTime) > 5*time.Minute {
			cb.logger.Info("Circuit breaker auto-reset after timeout")
			cb.reset()
		} else {
			return true, cb.reason
		}
	}

	// Check volatility spike
	if cb.config.VolatilitySpike.Enabled {
		if cb.checkVolatilitySpike(scores) {
			cb.trip("volatility_spike")
			return true, "Volatility spike detected"
		}
	}

	// Check consecutive losses
	if cb.config.ConsecutiveLosses.Enabled {
		if cb.checkConsecutiveLosses() {
			cb.trip("consecutive_losses")
			return true, "Too many consecutive losses"
		}
	}

	return false, ""
}

// checkVolatilitySpike detects if market volatility is too high
func (cb *CircuitBreaker) checkVolatilitySpike(scores map[string]SymbolScore) bool {
	spikeCount := 0
	spikeThreshold := len(scores) / 3 // At least 1/3 of symbols showing spike

	for _, score := range scores {
		// Track ATR
		cb.atrHistory = append(cb.atrHistory, score.RawATR14)
		if len(cb.atrHistory) > cb.maxATRHistory {
			cb.atrHistory = cb.atrHistory[1:]
		}

		// Check for spike
		if len(cb.atrHistory) >= 5 {
			avgATR := cb.calculateAvgATR()
			if avgATR > 0 && score.RawATR14 > avgATR*cb.config.VolatilitySpike.ATRMultiplier {
				spikeCount++
			}
		}
	}

	return spikeCount >= spikeThreshold
}

// checkConsecutiveLosses checks if we've had too many losses
func (cb *CircuitBreaker) checkConsecutiveLosses() bool {
	return cb.consecutiveLosses >= cb.config.ConsecutiveLosses.Threshold
}

// RecordTradeOutcome records the outcome of a trade
func (cb *CircuitBreaker) RecordTradeOutcome(pnl float64) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	outcome := "loss"
	if pnl > 0 {
		outcome = "win"
	}

	if outcome == "loss" {
		if cb.lastTradeOutcome == "loss" {
			cb.consecutiveLosses++
		} else {
			cb.consecutiveLosses = 1
		}
	} else {
		cb.consecutiveLosses = 0
	}

	cb.lastTradeOutcome = outcome

	cb.logger.Debug("Trade outcome recorded",
		zap.String("outcome", outcome),
		zap.Float64("pnl", pnl),
		zap.Int("consecutive_losses", cb.consecutiveLosses),
	)
}

// trip trips the circuit breaker
func (cb *CircuitBreaker) trip(reason string) {
	cb.isTripped = true
	cb.lastTripTime = time.Now()
	cb.reason = reason

	cb.logger.Warn("Circuit breaker TRIPPED",
		zap.String("reason", reason),
		zap.Time("time", cb.lastTripTime),
	)
}

// reset resets the circuit breaker
func (cb *CircuitBreaker) reset() {
	cb.isTripped = false
	cb.reason = ""
	cb.consecutiveLosses = 0
}

// IsTripped returns whether the circuit breaker is currently tripped
func (cb *CircuitBreaker) IsTripped() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.isTripped
}

// GetStatus returns the current circuit breaker status
func (cb *CircuitBreaker) GetStatus() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"is_tripped":         cb.isTripped,
		"last_trip_time":     cb.lastTripTime,
		"reason":             cb.reason,
		"consecutive_losses": cb.consecutiveLosses,
		"last_trade_outcome": cb.lastTradeOutcome,
	}
}

// calculateAvgATR calculates average ATR from history
func (cb *CircuitBreaker) calculateAvgATR() float64 {
	if len(cb.atrHistory) == 0 {
		return 0
	}

	sum := 0.0
	for _, atr := range cb.atrHistory {
		sum += atr
	}
	return sum / float64(len(cb.atrHistory))
}

// Reset manually resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.reset()
	cb.logger.Info("Circuit breaker manually reset")
}
