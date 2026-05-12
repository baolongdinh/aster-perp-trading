package volume_optimization

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OBISnapshot represents a single orderbook depth snapshot
type OBISnapshot struct {
	Timestamp time.Time
	BidQty    float64 // Total quantity at best bid level
	AskQty    float64 // Total quantity at best ask level
	MidPrice  float64
}

// OBISignal represents the derived market microstructure signal
type OBISignal struct {
	OBIScore          float64 // Range: [-1, 1]: -1 (all sellers), 0 (balanced), 1 (all buyers)
	BiasDirection     string  // "BUY", "SELL", or "NEUTRAL"
	Strength          string  // "WEAK", "MODERATE", or "STRONG"
	RecommendedAction string  // "TIGHTEN_BUY", "TIGHTEN_SELL", or "BALANCED"
	Confidence        float64 // 0-1, based on sustained imbalance
	UpdatedAt         time.Time
}

// OrderBookImbalanceDetector tracks order book imbalance over time
type OrderBookImbalanceDetector struct {
	windowSize         int            // Number of snapshots to track
	snapshots          []*OBISnapshot // Circular buffer of snapshots
	currentIdx         int            // Current index in circular buffer
	imbalanceThreshold float64        // Threshold for "strong" imbalance (default 0.5 = 50%)
	filledCount        int            // Track how many snapshots are filled
	mu                 sync.RWMutex
	logger             *zap.Logger

	// Metrics tracking
	lastOBI        float64
	obi24hHistory  []float64
	lastUpdateTime time.Time
}

// NewOrderBookImbalanceDetector creates a new OBI detector
// windowSize: number of snapshots to track (e.g., 10 = 10 seconds @ 1Hz)
// threshold: what constitutes "strong" imbalance (e.g., 0.5 = 50% one-sided)
func NewOrderBookImbalanceDetector(windowSize int, threshold float64, logger *zap.Logger) *OrderBookImbalanceDetector {
	if windowSize <= 0 {
		windowSize = 10
	}
	if threshold <= 0 || threshold > 1 {
		threshold = 0.5
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	return &OrderBookImbalanceDetector{
		windowSize:         windowSize,
		snapshots:          make([]*OBISnapshot, windowSize),
		imbalanceThreshold: threshold,
		logger:             logger,
		obi24hHistory:      make([]float64, 0, 1440), // 24h * 60min
	}
}

// UpdateOrderBook records a new orderbook snapshot
// bidQty: total quantity at best bid level
// askQty: total quantity at best ask level
// midPrice: mid price for reference
func (d *OrderBookImbalanceDetector) UpdateOrderBook(bidQty, askQty, midPrice float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.snapshots[d.currentIdx] = &OBISnapshot{
		Timestamp: time.Now(),
		BidQty:    bidQty,
		AskQty:    askQty,
		MidPrice:  midPrice,
	}
	d.currentIdx = (d.currentIdx + 1) % d.windowSize

	if d.filledCount < d.windowSize {
		d.filledCount++
	}

	d.lastUpdateTime = time.Now()
}

// CalculateOBI computes the Order Book Imbalance score
// OBI = (BidQty - AskQty) / (BidQty + AskQty)
// Range: [-1, 1]
// Positive (> 0) = more buyers (bullish imbalance)
// Negative (< 0) = more sellers (bearish imbalance)
// Near 0 = balanced orderbook
func (d *OrderBookImbalanceDetector) CalculateOBI() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.filledCount == 0 {
		return 0
	}

	totalBid := 0.0
	totalAsk := 0.0

	for i := 0; i < d.filledCount; i++ {
		if d.snapshots[i] != nil {
			totalBid += d.snapshots[i].BidQty
			totalAsk += d.snapshots[i].AskQty
		}
	}

	if (totalBid + totalAsk) == 0 {
		return 0
	}

	obi := (totalBid - totalAsk) / (totalBid + totalAsk)
	// Clamp to [-1, 1] range
	return math.Max(-1, math.Min(1, obi))
}

// GetSignal generates a trading signal from current OBI state
func (d *OrderBookImbalanceDetector) GetSignal() OBISignal {
	// Calculate OBI first to avoid deadlock (write lock + read lock)
	obi := d.CalculateOBI()

	d.mu.Lock()
	defer d.mu.Unlock()

	d.lastOBI = obi

	signal := OBISignal{
		OBIScore:  obi,
		UpdatedAt: time.Now(),
	}

	absOBI := math.Abs(obi)

	// Determine bias direction
	if obi > 0.1 {
		signal.BiasDirection = "BUY"
	} else if obi < -0.1 {
		signal.BiasDirection = "SELL"
	} else {
		signal.BiasDirection = "NEUTRAL"
	}

	// Determine strength and confidence
	if absOBI > d.imbalanceThreshold {
		signal.Strength = "STRONG"
		signal.Confidence = math.Min(absOBI/d.imbalanceThreshold, 1.0)
	} else if absOBI > d.imbalanceThreshold*0.5 {
		signal.Strength = "MODERATE"
		signal.Confidence = absOBI / d.imbalanceThreshold
	} else {
		signal.Strength = "WEAK"
		signal.Confidence = absOBI / (d.imbalanceThreshold * 0.5)
	}

	// Determine recommended trading action
	if signal.BiasDirection == "BUY" && signal.Strength == "STRONG" {
		// Many buyers, few sellers → tighten sell spread to attract sellers
		signal.RecommendedAction = "TIGHTEN_SELL"
	} else if signal.BiasDirection == "SELL" && signal.Strength == "STRONG" {
		// Many sellers, few buyers → tighten buy spread to attract buyers
		signal.RecommendedAction = "TIGHTEN_BUY"
	} else if signal.Strength == "MODERATE" {
		// Moderate imbalance → adjust slightly
		if signal.BiasDirection == "BUY" {
			signal.RecommendedAction = "MODERATE_SELL"
		} else if signal.BiasDirection == "SELL" {
			signal.RecommendedAction = "MODERATE_BUY"
		} else {
			signal.RecommendedAction = "BALANCED"
		}
	} else {
		// Balanced orderbook
		signal.RecommendedAction = "BALANCED"
	}

	d.logger.Debug("OBI Signal",
		zap.Float64("obi_score", signal.OBIScore),
		zap.String("bias_direction", signal.BiasDirection),
		zap.String("strength", signal.Strength),
		zap.Float64("confidence", signal.Confidence),
		zap.String("action", signal.RecommendedAction))

	return signal
}

// IsHealthyBook checks if the orderbook is sufficiently balanced
// Returns true if |OBI| < threshold
func (d *OrderBookImbalanceDetector) IsHealthyBook() bool {
	obi := math.Abs(d.CalculateOBI())
	return obi < d.imbalanceThreshold
}

// GetOBITrend returns whether OBI has been improving or worsening
// Returns: "IMPROVING" (toward balanced), "WORSENING", or "STABLE"
func (d *OrderBookImbalanceDetector) GetOBITrend() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.filledCount < 3 {
		return "INSUFFICIENT_DATA"
	}

	// Get last 3 OBI snapshots
	now := d.currentIdx
	prev1Idx := (now - 1 + d.windowSize) % d.windowSize
	prev2Idx := (now - 2 + d.windowSize) % d.windowSize

	// Calculate OBI for each period
	current := d.calculateOBIForIndex(now)
	prev1 := d.calculateOBIForIndex(prev1Idx)
	prev2 := d.calculateOBIForIndex(prev2Idx)

	currentAbs := math.Abs(current)
	prev1Abs := math.Abs(prev1)
	prev2Abs := math.Abs(prev2)

	// Trend detection: comparing absolute values (distance from balanced)
	improving := currentAbs < prev1Abs && prev1Abs < prev2Abs
	worsening := currentAbs > prev1Abs && prev1Abs > prev2Abs

	if improving {
		return "IMPROVING"
	} else if worsening {
		return "WORSENING"
	}
	return "STABLE"
}

// calculateOBIForIndex is an internal helper
func (d *OrderBookImbalanceDetector) calculateOBIForIndex(idx int) float64 {
	if d.snapshots[idx] == nil {
		return 0
	}
	snap := d.snapshots[idx]
	if (snap.BidQty + snap.AskQty) == 0 {
		return 0
	}
	obi := (snap.BidQty - snap.AskQty) / (snap.BidQty + snap.AskQty)
	return math.Max(-1, math.Min(1, obi))
}

// GetSpreadAdjustmentFactor returns a multiplier for spread adjustment
// > 1.0 = widen spread (reduce competition)
// < 1.0 = tighten spread (increase competition)
// Useful for SpreadCalculator integration
func (d *OrderBookImbalanceDetector) GetSpreadAdjustmentFactor(side string) float64 {
	signal := d.GetSignal()

	// Strong imbalance detected
	if signal.Strength == "STRONG" {
		if side == "BUY" && signal.BiasDirection == "SELL" {
			// Many sellers, few buyers → tighten buy to attract
			return 0.8 - (signal.Confidence * 0.1) // 0.7-0.8x
		}
		if side == "SELL" && signal.BiasDirection == "BUY" {
			// Many buyers, few sellers → tighten sell to attract
			return 0.8 - (signal.Confidence * 0.1) // 0.7-0.8x
		}
	}

	// Moderate imbalance
	if signal.Strength == "MODERATE" {
		if side == "BUY" && signal.BiasDirection == "SELL" {
			return 0.95
		}
		if side == "SELL" && signal.BiasDirection == "BUY" {
			return 0.95
		}
	}

	// Balanced or bias favors this side → widen slightly
	return 1.05
}

// GetOrderSizingFactor returns a multiplier for order sizing
// Healthy book = 1.0, imbalanced = reduce size
func (d *OrderBookImbalanceDetector) GetOrderSizingFactor() float64 {
	signal := d.GetSignal()

	switch signal.Strength {
	case "STRONG":
		return 0.7 // Reduce orders by 30%
	case "MODERATE":
		return 0.85 // Reduce by 15%
	default:
		return 1.0 // Full size
	}
}

// Reset clears all history (useful for testing or restart)
func (d *OrderBookImbalanceDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.snapshots = make([]*OBISnapshot, d.windowSize)
	d.currentIdx = 0
	d.filledCount = 0
	d.lastOBI = 0
	d.obi24hHistory = make([]float64, 0, 1440)
}

// GetMetrics returns OBI detector metrics for monitoring
func (d *OrderBookImbalanceDetector) GetMetrics() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	signal := d.GetSignal()

	return map[string]interface{}{
		"obi_score":           d.lastOBI,
		"bias_direction":      signal.BiasDirection,
		"strength":            signal.Strength,
		"confidence":          signal.Confidence,
		"recommended_action":  signal.RecommendedAction,
		"is_healthy_book":     d.IsHealthyBook(),
		"spread_adjust_buy":   d.GetSpreadAdjustmentFactor("BUY"),
		"spread_adjust_sell":  d.GetSpreadAdjustmentFactor("SELL"),
		"order_sizing_factor": d.GetOrderSizingFactor(),
		"trend":               d.GetOBITrend(),
	}
}
