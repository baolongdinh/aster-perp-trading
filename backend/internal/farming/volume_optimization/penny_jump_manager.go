package volume_optimization

import (
	"math"
	"sync"

	"go.uber.org/zap"
)

// PennyJumpManager manages penny jumping strategy - placing orders 1 tick from best bid/ask
type PennyJumpManager struct {
	enabled       bool
	jumpThreshold float64 // % of spread required to trigger jump
	maxJump       int     // Max ticks to jump
	
	// Track best bid/ask per symbol
	bestBids map[string]float64
	bestAsks map[string]float64
	
	// Tick size manager for price calculations
	tickSizeMgr interface {
		GetTickSize(symbol string) float64
		RoundToTick(price, tickSize float64) float64
	}
	
	mu     sync.RWMutex
	logger *zap.Logger
}

// PennyConfig holds configuration for penny jumping
type PennyConfig struct {
	Enabled       bool
	JumpThreshold float64 // % of spread (e.g., 0.1 = 10%)
	MaxJump       int     // Max ticks to jump (e.g., 3)
}

// NewPennyJumpManager creates a new penny jump manager
func NewPennyJumpManager(config PennyConfig, logger *zap.Logger) *PennyJumpManager {
	if config.JumpThreshold == 0 {
		config.JumpThreshold = 0.1 // 10% default
	}
	if config.MaxJump == 0 {
		config.MaxJump = 3 // Default 3 ticks
	}
	
	return &PennyJumpManager{
		enabled:       config.Enabled,
		jumpThreshold: config.JumpThreshold,
		maxJump:       config.MaxJump,
		bestBids:      make(map[string]float64),
		bestAsks:      make(map[string]float64),
		logger:        logger,
	}
}

// UpdateBestPrices updates the current best bid/ask for a symbol
func (p *PennyJumpManager) UpdateBestPrices(symbol string, bestBid, bestAsk float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.bestBids[symbol] = bestBid
	p.bestAsks[symbol] = bestAsk
	
	p.logger.Debug("Best prices updated",
		zap.String("symbol", symbol),
		zap.Float64("best_bid", bestBid),
		zap.Float64("best_ask", bestAsk),
		zap.Float64("spread", bestAsk-bestBid))
}

// GetPennyJumpedPrice returns the price with penny jump applied
// For BUY orders: place 1 tick above best bid (if threshold met)
// For SELL orders: place 1 tick below best ask (if threshold met)
func (p *PennyJumpManager) GetPennyJumpedPrice(symbol, side string, originalPrice float64) float64 {
	if !p.enabled {
		return originalPrice
	}
	
	p.mu.RLock()
	bestBid, hasBid := p.bestBids[symbol]
	bestAsk, hasAsk := p.bestAsks[symbol]
	p.mu.RUnlock()
	
	if !hasBid || !hasAsk {
		p.logger.Debug("No best prices available, skipping penny jump",
			zap.String("symbol", symbol))
		return originalPrice
	}
	
	spread := bestAsk - bestBid
	if spread <= 0 {
		return originalPrice
	}
	
	// Get tick size
	tickSize := 0.01 // Default
	if p.tickSizeMgr != nil {
		tickSize = p.tickSizeMgr.GetTickSize(symbol)
	}
	
	switch side {
	case "BUY":
		// Calculate distance from best bid
		distanceFromBid := math.Abs(originalPrice - bestBid)
		distancePct := distanceFromBid / spread
		
		// If original price is close to best bid, jump ahead
		if distancePct <= p.jumpThreshold {
			// Jump 1 tick above best bid (but not above original price + maxJump)
			jumpedPrice := bestBid + (tickSize * float64(p.maxJump))
			if jumpedPrice > originalPrice && jumpedPrice < bestAsk {
				p.logger.Debug("Penny jump applied for BUY",
					zap.String("symbol", symbol),
					zap.Float64("original", originalPrice),
					zap.Float64("jumped", jumpedPrice),
					zap.Float64("best_bid", bestBid))
				return jumpedPrice
			}
		}
		
	case "SELL":
		// Calculate distance from best ask
		distanceFromAsk := math.Abs(bestAsk - originalPrice)
		distancePct := distanceFromAsk / spread
		
		// If original price is close to best ask, jump ahead
		if distancePct <= p.jumpThreshold {
			// Jump 1 tick below best ask (but not below original price - maxJump)
			jumpedPrice := bestAsk - (tickSize * float64(p.maxJump))
			if jumpedPrice < originalPrice && jumpedPrice > bestBid {
				p.logger.Debug("Penny jump applied for SELL",
					zap.String("symbol", symbol),
					zap.Float64("original", originalPrice),
					zap.Float64("jumped", jumpedPrice),
					zap.Float64("best_ask", bestAsk))
				return jumpedPrice
			}
		}
	}
	
	return originalPrice
}

// SetTickSizeManager sets the tick size manager
func (p *PennyJumpManager) SetTickSizeManager(tickSizeMgr interface {
	GetTickSize(symbol string) float64
	RoundToTick(price, tickSize float64) float64
}) {
	p.tickSizeMgr = tickSizeMgr
	p.logger.Info("TickSizeManager set on PennyJumpManager")
}

// IsEnabled returns whether penny jumping is enabled
func (p *PennyJumpManager) IsEnabled() bool {
	return p.enabled
}

// SetEnabled enables/disables penny jumping
func (p *PennyJumpManager) SetEnabled(enabled bool) {
	p.enabled = enabled
	p.logger.Info("Penny jumping enabled state changed",
		zap.Bool("enabled", enabled))
}

// GetStats returns current statistics
func (p *PennyJumpManager) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return map[string]interface{}{
		"enabled":         p.enabled,
		"jump_threshold":  p.jumpThreshold,
		"max_jump":        p.maxJump,
		"tracked_symbols": len(p.bestBids),
	}
}
