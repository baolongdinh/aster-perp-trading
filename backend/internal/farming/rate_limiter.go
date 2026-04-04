package farming

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// RateLimiter implements a token bucket rate limiter for API requests.
type RateLimiter struct {
	mu           sync.Mutex
	tokens       float64
	capacity     float64
	refillRate   float64 // tokens per second
	lastRefill   time.Time
	penaltyUntil time.Time
	logger       *zap.Logger
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(capacity float64, refillRate float64, logger *zap.Logger) *RateLimiter {
	return &RateLimiter{
		tokens:     capacity,
		capacity:   capacity,
		refillRate: refillRate,
		lastRefill: time.Now(),
		logger:     logger,
	}
}

// Allow checks if a request can proceed, consuming a token if available.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.tokens += elapsed * rl.refillRate
	if rl.tokens > rl.capacity {
		rl.tokens = rl.capacity
	}
	rl.lastRefill = now

	if rl.penaltyUntil.After(now) {
		return false
	}

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// WaitForToken waits until a token is available or timeout.
func (rl *RateLimiter) WaitForToken(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rl.Allow() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// ApplyPenalty applies a penalty period after rate limit violation.
func (rl *RateLimiter) ApplyPenalty(duration time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.penaltyUntil = time.Now().Add(duration)
	rl.logger.Warn("Rate limit penalty applied", zap.Duration("duration", duration))
}

// IsPenalized checks if currently under penalty.
func (rl *RateLimiter) IsPenalized() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	return rl.penaltyUntil.After(time.Now())
}

// GetTokens returns current token count (for monitoring).
func (rl *RateLimiter) GetTokens() float64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	currentTokens := rl.tokens + elapsed*rl.refillRate
	if currentTokens > rl.capacity {
		currentTokens = rl.capacity
	}
	return currentTokens
}
