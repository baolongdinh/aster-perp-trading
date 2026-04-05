package adaptive_grid

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OrderbookSnapshot represents current orderbook state
type OrderbookSnapshot struct {
	BestBid    float64
	BestAsk    float64
	MidPrice   float64
	Spread     float64
	SpreadPct  float64
	Timestamp  time.Time
}

// SpreadProtection monitors orderbook spread and pauses trading when too wide
type SpreadProtection struct {
	lastSnapshot   *OrderbookSnapshot
	pauseThreshold float64
	emergencyThreshold float64
	resumeDuration time.Duration
	isPaused       bool
	pausedAt       time.Time
	mu             sync.RWMutex
	logger         *zap.Logger
}

// NewSpreadProtection creates spread protection monitor
func NewSpreadProtection(logger *zap.Logger) *SpreadProtection {
	return &SpreadProtection{
		pauseThreshold:       0.001, // 0.1%
		emergencyThreshold:   0.003, // 0.3%
		resumeDuration:       30 * time.Second,
		logger:               logger,
	}
}

// UpdateOrderbook updates current orderbook prices
func (s *SpreadProtection) UpdateOrderbook(bestBid, bestAsk float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if bestBid <= 0 || bestAsk <= 0 || bestAsk <= bestBid {
		s.logger.Warn("Invalid orderbook prices",
			zap.Float64("bid", bestBid),
			zap.Float64("ask", bestAsk))
		return
	}

	midPrice := (bestBid + bestAsk) / 2
	spread := bestAsk - bestBid
	spreadPct := spread / midPrice

	s.lastSnapshot = &OrderbookSnapshot{
		BestBid:   bestBid,
		BestAsk:   bestAsk,
		MidPrice:  midPrice,
		Spread:    spread,
		SpreadPct: spreadPct,
		Timestamp: time.Now(),
	}

	// Check thresholds and update pause state
	s.checkThresholds()
}

// checkThresholds determines if trading should be paused
func (s *SpreadProtection) checkThresholds() {
	if s.lastSnapshot == nil {
		return
	}

	spreadPct := s.lastSnapshot.SpreadPct

	// Emergency pause
	if spreadPct > s.emergencyThreshold {
		if !s.isPaused {
			s.isPaused = true
			s.pausedAt = time.Now()
			s.logger.Error("EMERGENCY: Trading paused due to extreme spread",
				zap.Float64("spread_pct", spreadPct*100),
				zap.Float64("threshold", s.emergencyThreshold*100))
		}
		return
	}

	// Normal pause
	if spreadPct > s.pauseThreshold {
		if !s.isPaused {
			s.isPaused = true
			s.pausedAt = time.Now()
			s.logger.Warn("Trading paused due to wide spread",
				zap.Float64("spread_pct", spreadPct*100),
				zap.Float64("threshold", s.pauseThreshold*100))
		}
		return
	}

	// Check if we can resume
	if s.isPaused {
		if time.Since(s.pausedAt) >= s.resumeDuration {
			s.isPaused = false
			s.logger.Info("Trading resumed - spread normalized",
				zap.Float64("spread_pct", spreadPct*100))
		}
	}
}

// GetSpreadPct returns current spread percentage
func (s *SpreadProtection) GetSpreadPct() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.lastSnapshot == nil {
		return 0
	}
	return s.lastSnapshot.SpreadPct
}

// IsSpreadTooWide returns true if spread exceeds threshold
func (s *SpreadProtection) IsSpreadTooWide(threshold float64) bool {
	return s.GetSpreadPct() > threshold
}

// ShouldPauseTrading returns true if trading should be paused
func (s *SpreadProtection) ShouldPauseTrading() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isPaused
}

// IsTradingPaused returns current pause state
func (s *SpreadProtection) IsTradingPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isPaused
}

// ForceResume manually resumes trading
func (s *SpreadProtection) ForceResume() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isPaused {
		s.isPaused = false
		s.logger.Info("Trading manually resumed")
	}
}

// GetSnapshot returns current orderbook snapshot
func (s *SpreadProtection) GetSnapshot() *OrderbookSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSnapshot
}

// CalculateSlippage calculates slippage for a filled order
func CalculateSlippage(fillPrice, bestPrice float64, isBuy bool) float64 {
	if bestPrice == 0 {
		return 0
	}

	if isBuy {
		// For buys, slippage is positive if fill > best ask
		return math.Abs(fillPrice-bestPrice) / bestPrice
	}
	// For sells, slippage is positive if fill < best bid
	return math.Abs(bestPrice-fillPrice) / bestPrice
}

// CheckAverageSlippage checks if average slippage is too high
func CheckAverageSlippage(slippages []float64, threshold float64) bool {
	if len(slippages) == 0 {
		return false
	}

	var sum float64
	for _, s := range slippages {
		sum += s
	}
	avg := sum / float64(len(slippages))
	return avg > threshold
}
