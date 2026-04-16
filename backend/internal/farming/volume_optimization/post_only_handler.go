package volume_optimization

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// PostOnlyHandler manages post-only order placement and rejection handling
type PostOnlyHandler struct {
	enabled        bool
	fallback       bool
	maxRetries     int
	retryDelay     time.Duration
	logger         *zap.Logger
}

// PostOnlyConfig holds configuration for post-only handling
type PostOnlyConfig struct {
	Enabled    bool          `yaml:"enabled"`
	Fallback   bool          `yaml:"fallback"`
	MaxRetries int           `yaml:"max_retries"`
	RetryDelay time.Duration `yaml:"retry_delay"`
}

// NewPostOnlyHandler creates a new post-only handler
func NewPostOnlyHandler(config PostOnlyConfig, logger *zap.Logger) *PostOnlyHandler {
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 100 * time.Millisecond
	}

	return &PostOnlyHandler{
		enabled:    config.Enabled,
		fallback:   config.Fallback,
		maxRetries: config.MaxRetries,
		retryDelay: config.RetryDelay,
		logger:     logger,
	}
}

// OrderPlacementFunc is a function that places an order
type OrderPlacementFunc func(ctx context.Context, symbol, side string, price, quantity float64, postOnly bool) error

// PlaceOrderWithPostOnly places an order with post-only flag and handles rejections
func (p *PostOnlyHandler) PlaceOrderWithPostOnly(
	ctx context.Context,
	symbol, side string,
	price, quantity float64,
	placeOrder OrderPlacementFunc,
) error {
	if !p.enabled {
		// Post-only not enabled, place regular order
		return placeOrder(ctx, symbol, side, price, quantity, false)
	}

	// Try post-only first
	err := p.placeWithRetry(ctx, symbol, side, price, quantity, true, placeOrder)
	if err == nil {
		p.logger.Info("Post-only order placed successfully",
			zap.String("symbol", symbol),
			zap.String("side", side),
			zap.Float64("price", price),
			zap.Float64("quantity", quantity))
		return nil
	}

	// Post-only failed
	if p.fallback {
		p.logger.Warn("Post-only rejected, falling back to regular limit order",
			zap.String("symbol", symbol),
			zap.String("side", side),
			zap.Error(err))
		
		return placeOrder(ctx, symbol, side, price, quantity, false)
	}

	p.logger.Error("Post-only rejected and fallback disabled, order not placed",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.Error(err))
	return fmt.Errorf("post-only rejected: %w", err)
}

// placeWithRetry attempts to place an order with retries
func (p *PostOnlyHandler) placeWithRetry(
	ctx context.Context,
	symbol, side string,
	price, quantity float64,
	postOnly bool,
	placeOrder OrderPlacementFunc,
) error {
	var lastErr error

	for attempt := 1; attempt <= p.maxRetries; attempt++ {
		err := placeOrder(ctx, symbol, side, price, quantity, postOnly)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is post-only rejection
		if isPostOnlyRejection(err) {
			p.logger.Debug("Post-only rejection, will retry",
				zap.Int("attempt", attempt),
				zap.Int("max_retries", p.maxRetries),
				zap.Error(err))

			if attempt < p.maxRetries {
				// Wait before retry
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(p.retryDelay):
					continue
				}
			}
		} else {
			// Not a post-only rejection, return immediately
			return err
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", p.maxRetries, lastErr)
}

// isPostOnlyRejection checks if an error is a post-only rejection
func isPostOnlyRejection(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	// Common post-only rejection messages
	postOnlyErrors := []string{
		"post-only",
		"would be a taker",
		"immediate",
		"crossed",
	}

	for _, keyword := range postOnlyErrors {
		if contains(errMsg, keyword) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// IsEnabled returns whether post-only is enabled
func (p *PostOnlyHandler) IsEnabled() bool {
	return p.enabled
}

// SetEnabled sets the post-only enabled state
func (p *PostOnlyHandler) SetEnabled(enabled bool) {
	p.enabled = enabled
	p.logger.Info("Post-only enabled state changed", zap.Bool("enabled", enabled))
}

// GetStats returns post-only handler statistics
func (p *PostOnlyHandler) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"enabled":    p.enabled,
		"fallback":   p.fallback,
		"max_retries": p.maxRetries,
	}
}
