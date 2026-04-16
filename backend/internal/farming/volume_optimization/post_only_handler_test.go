package volume_optimization

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewPostOnlyHandler(t *testing.T) {
	logger := zap.NewNop()
	config := PostOnlyConfig{
		Enabled:    true,
		Fallback:   true,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	}

	handler := NewPostOnlyHandler(config, logger)

	if !handler.enabled {
		t.Error("Expected enabled to be true")
	}
	if !handler.fallback {
		t.Error("Expected fallback to be true")
	}
	if handler.maxRetries != 3 {
		t.Errorf("Expected max retries 3, got %d", handler.maxRetries)
	}
}

func TestPlaceOrderWithPostOnly_Disabled(t *testing.T) {
	logger := zap.NewNop()
	config := PostOnlyConfig{
		Enabled:    false,
		Fallback:   true,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	}

	handler := NewPostOnlyHandler(config, logger)

	placeOrderCalled := false
	placeOrder := func(ctx context.Context, symbol, side string, price, quantity float64, postOnly bool) error {
		placeOrderCalled = true
		if postOnly {
			t.Error("Expected post-only to be false when disabled")
		}
		return nil
	}

	err := handler.PlaceOrderWithPostOnly(context.Background(), "BTC", "BUY", 50000.0, 0.1, placeOrder)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !placeOrderCalled {
		t.Error("Expected placeOrder to be called")
	}
}

func TestPlaceOrderWithPostOnly_Success(t *testing.T) {
	logger := zap.NewNop()
	config := PostOnlyConfig{
		Enabled:    true,
		Fallback:   true,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	}

	handler := NewPostOnlyHandler(config, logger)

	placeOrder := func(ctx context.Context, symbol, side string, price, quantity float64, postOnly bool) error {
		if !postOnly {
			t.Error("Expected post-only to be true")
		}
		return nil
	}

	err := handler.PlaceOrderWithPostOnly(context.Background(), "BTC", "BUY", 50000.0, 0.1, placeOrder)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestPlaceOrderWithPostOnly_RejectionWithFallback(t *testing.T) {
	logger := zap.NewNop()
	config := PostOnlyConfig{
		Enabled:    true,
		Fallback:   true,
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
	}

	handler := NewPostOnlyHandler(config, logger)

	attempts := 0
	placeOrder := func(ctx context.Context, symbol, side string, price, quantity float64, postOnly bool) error {
		attempts++
		if postOnly {
			return errors.New("order would be a taker")
		}
		return nil
	}

	err := handler.PlaceOrderWithPostOnly(context.Background(), "BTC", "BUY", 50000.0, 0.1, placeOrder)
	if err != nil {
		t.Errorf("Unexpected error after fallback: %v", err)
	}
	if attempts != 4 { // 3 retries + 1 fallback
		t.Errorf("Expected 4 attempts (3 retries + 1 fallback), got %d", attempts)
	}
}

func TestPlaceOrderWithPostOnly_RejectionNoFallback(t *testing.T) {
	logger := zap.NewNop()
	config := PostOnlyConfig{
		Enabled:    true,
		Fallback:   false,
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
	}

	handler := NewPostOnlyHandler(config, logger)

	placeOrder := func(ctx context.Context, symbol, side string, price, quantity float64, postOnly bool) error {
		if postOnly {
			return errors.New("order would be a taker")
		}
		return nil
	}

	err := handler.PlaceOrderWithPostOnly(context.Background(), "BTC", "BUY", 50000.0, 0.1, placeOrder)
	if err == nil {
		t.Error("Expected error when fallback disabled")
	}
}

func TestIsPostOnlyRejection(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Post-only rejection",
			err:      errors.New("order would be a taker"),
			expected: true,
		},
		{
			name:     "Post-only keyword",
			err:      errors.New("post-only order rejected"),
			expected: true,
		},
		{
			name:     "Immediate keyword",
			err:      errors.New("order would execute immediately"),
			expected: true,
		},
		{
			name:     "Other error",
			err:      errors.New("insufficient balance"),
			expected: false,
		},
		{
			name:     "Nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPostOnlyRejection(tt.err)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSetEnabled(t *testing.T) {
	logger := zap.NewNop()
	config := PostOnlyConfig{
		Enabled:    true,
		Fallback:   true,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	}

	handler := NewPostOnlyHandler(config, logger)

	handler.SetEnabled(false)
	if handler.IsEnabled() {
		t.Error("Expected enabled to be false after SetEnabled(false)")
	}

	handler.SetEnabled(true)
	if !handler.IsEnabled() {
		t.Error("Expected enabled to be true after SetEnabled(true)")
	}
}

func TestPostOnlyGetStats(t *testing.T) {
	logger := zap.NewNop()
	config := PostOnlyConfig{
		Enabled:    true,
		Fallback:   true,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	}

	handler := NewPostOnlyHandler(config, logger)

	stats := handler.GetStats()

	if stats["enabled"] != true {
		t.Error("Expected enabled to be true in stats")
	}
	if stats["fallback"] != true {
		t.Error("Expected fallback to be true in stats")
	}
	if stats["max_retries"] != 3 {
		t.Errorf("Expected max_retries 3 in stats, got %v", stats["max_retries"])
	}
}

func TestDefaultConfig(t *testing.T) {
	logger := zap.NewNop()
	config := PostOnlyConfig{
		Enabled:  true,
		Fallback: true,
		// MaxRetries and RetryDelay should get defaults
	}

	handler := NewPostOnlyHandler(config, logger)

	if handler.maxRetries != 3 {
		t.Errorf("Expected default max retries 3, got %d", handler.maxRetries)
	}
	if handler.retryDelay != 100*time.Millisecond {
		t.Errorf("Expected default retry delay 100ms, got %v", handler.retryDelay)
	}
}
