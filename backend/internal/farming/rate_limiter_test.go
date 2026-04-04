package farming

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRateLimiter_Allow(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rl := NewRateLimiter(5, 1, logger) // 5 capacity, 1 token/sec

	// Should allow 5 requests immediately
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Errorf("Expected allow on request %d", i+1)
		}
	}

	// 6th should be denied
	if rl.Allow() {
		t.Error("Expected deny on 6th request")
	}
}

func TestRateLimiter_WaitForToken(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rl := NewRateLimiter(1, 2, logger) // 1 capacity, 2 tokens/sec

	// Consume the token
	if !rl.Allow() {
		t.Error("Expected allow for first request")
	}

	// Wait for token should succeed quickly
	if !rl.WaitForToken(1 * time.Second) {
		t.Error("Expected WaitForToken to succeed")
	}
}

func TestRateLimiter_ApplyPenalty(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rl := NewRateLimiter(5, 1, logger)

	rl.ApplyPenalty(1 * time.Second)

	// Should be penalized
	if !rl.IsPenalized() {
		t.Error("Expected to be penalized")
	}

	time.Sleep(1100 * time.Millisecond)

	// Should not be penalized anymore
	if rl.IsPenalized() {
		t.Error("Expected penalty to expire")
	}
}

func TestRateLimiter_GetTokens(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rl := NewRateLimiter(10, 1, logger)

	initial := rl.GetTokens()
	if initial != 10 {
		t.Errorf("Expected 10 tokens, got %f", initial)
	}

	rl.Allow()
	after := rl.GetTokens()
	if after >= 10 {
		t.Errorf("Expected less than 10 tokens after consume, got %f", after)
	}
}
