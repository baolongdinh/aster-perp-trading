package adaptive_grid

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OptimizationWeights defines weights for multi-objective optimization
type OptimizationWeights struct {
	ProfitWeight   float64 // Weight for profit objective
	RiskWeight     float64 // Weight for risk objective
	VolumeWeight   float64 // Weight for volume objective
	DrawdownWeight float64 // Weight for drawdown objective
}

// SmoothingParameters defines parameters for parameter smoothing
type SmoothingParameters struct {
	Alpha           float64 // Smoothing factor (0-1, lower = more smoothing)
	MinChangePct    float64 // Minimum percentage change to apply
	MaxChangePct    float64 // Maximum percentage change allowed
	MaxChangePerSec float64 // Maximum rate of change per second
}

// ParetoPoint represents a point on the Pareto frontier
type ParetoPoint struct {
	Spread     float64
	OrderCount int
	Size       float64
	Mode       string
	Scores     map[string]float64 // profit, risk, volume, drawdown
	Dominates  []int              // Indices of points this dominates
}

// RealTimeOptimizer optimizes parameters in real-time based on current conditions
type RealTimeOptimizer struct {
	mu sync.RWMutex // CRITICAL: Protects all fields from race conditions

	logger *zap.Logger

	// Optimization weights
	weights OptimizationWeights

	// Smoothing parameters
	smoothing SmoothingParameters

	// Current parameter values (for smoothing)
	currentSpread     float64
	currentOrderCount int
	currentSize       float64
	currentMode       string
	lastUpdateTime    time.Time

	// Historical performance tracking
	performanceHistory map[string][]float64 // parameter -> performance history

	// Emergency detection (fast path for over-smoothing)
	emergencyDetected bool
	lastEmergencyTime time.Time
}

// NewRealTimeOptimizer creates a new real-time optimizer
func NewRealTimeOptimizer(logger *zap.Logger) *RealTimeOptimizer {
	return &RealTimeOptimizer{
		logger: logger,
		weights: OptimizationWeights{
			ProfitWeight:   0.4,
			RiskWeight:     0.3,
			VolumeWeight:   0.2,
			DrawdownWeight: 0.1,
		},
		smoothing: SmoothingParameters{
			Alpha:           0.3,  // 30% new value, 70% old value
			MinChangePct:    0.01, // 1% minimum change
			MaxChangePct:    0.50, // 50% maximum change
			MaxChangePerSec: 0.10, // 10% max change per second
		},
		currentSpread:      0.0015, // Default 0.15%
		currentOrderCount:  10,
		currentSize:        100.0, // Default $100
		currentMode:        "FULL",
		lastUpdateTime:     time.Now(),
		performanceHistory: make(map[string][]float64),
	}
}

// OptimizeSpread calculates optimal spread based on conditions
func (r *RealTimeOptimizer) OptimizeSpread(volatility, skew, funding float64, currentTime time.Time) float64 {
	// Base spread from volatility
	baseSpread := 0.0015 // 0.15%
	if volatility > 0.015 {
		baseSpread *= 2.0 // High volatility
	} else if volatility > 0.008 {
		baseSpread *= 1.5 // Normal volatility
	} else if volatility < 0.003 {
		baseSpread *= 0.5 // Low volatility
	}

	// Adjust for skew
	if math.Abs(skew) > 0.3 {
		baseSpread *= 1.2
	}

	// Adjust for funding
	if math.Abs(funding) > 0.0001 {
		baseSpread *= 1.1
	}

	// Apply smoothing
	return r.smoothSpread(baseSpread, currentTime)
}

// OptimizeOrderCount calculates optimal order count based on conditions
func (r *RealTimeOptimizer) OptimizeOrderCount(depth, risk float64, regime string, currentTime time.Time) int {
	// Base order count
	baseCount := 10

	// Adjust for depth
	if depth < 0.3 {
		baseCount = 5 // Shallow depth
	} else if depth > 0.7 {
		baseCount = 15 // Deep depth
	}

	// Adjust for risk
	if risk > 0.8 {
		baseCount = int(float64(baseCount) * 0.6) // High risk
	} else if risk < 0.3 {
		baseCount = int(float64(baseCount) * 1.2) // Low risk
	}

	// Adjust for regime
	switch regime {
	case "RANGING":
		baseCount = int(float64(baseCount) * 1.2)
	case "TRENDING":
		baseCount = int(float64(baseCount) * 0.8)
	case "VOLATILE":
		baseCount = int(float64(baseCount) * 0.6)
	}

	// Clamp to reasonable bounds
	if baseCount < 2 {
		baseCount = 2
	}
	if baseCount > 50 {
		baseCount = 50
	}

	// Apply smoothing
	return r.smoothOrderCount(baseCount, currentTime)
}

// OptimizeSize calculates optimal order size based on conditions
func (r *RealTimeOptimizer) OptimizeSize(equity, risk, opportunity, liquidity float64, currentTime time.Time) float64 {
	// Base size
	baseSize := 100.0 // $100

	// Adjust for equity
	if equity > 1000.0 {
		baseSize *= 1.5 // Higher equity
	} else if equity < 500.0 {
		baseSize *= 0.7 // Lower equity
	}

	// Adjust for risk
	if risk > 0.8 {
		baseSize *= 0.5 // High risk
	} else if risk < 0.3 {
		baseSize *= 1.3 // Low risk
	}

	// Adjust for opportunity
	if opportunity > 0.8 {
		baseSize *= 1.2 // High opportunity
	} else if opportunity < 0.3 {
		baseSize *= 0.8 // Low opportunity
	}

	// Adjust for liquidity
	if liquidity > 0.8 {
		baseSize *= 1.1 // High liquidity
	} else if liquidity < 0.3 {
		baseSize *= 0.7 // Low liquidity
	}

	// Apply smoothing
	return r.smoothSize(baseSize, currentTime)
}

// OptimizeMode calculates optimal trading mode based on conditions
func (r *RealTimeOptimizer) OptimizeMode(risk, volatility, drawdown float64, losses int, currentTime time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	// FAST PATH: Emergency detection - skip smoothing for extreme conditions
	if drawdown > 0.15 || losses >= 4 || volatility > 0.8 {
		r.emergencyDetected = true
		r.lastEmergencyTime = currentTime
		r.logger.Warn("Emergency condition detected - fast path activated",
			zap.Float64("drawdown", drawdown),
			zap.Int("losses", losses),
			zap.Float64("volatility", volatility))
		return "PAUSED"
	}

	// Calculate mode based on conditions (similar to CircuitBreaker logic)
	if drawdown > 0.1 || losses >= 3 || risk > 0.8 {
		return "REDUCED"
	}
	if risk < 0.5 && volatility < 0.4 && drawdown < 0.05 {
		return "FULL"
	}
	return "MICRO"
}

// CalculateObjectiveScores calculates scores for each objective
func (r *RealTimeOptimizer) CalculateObjectiveScores(spread, size float64, orderCount int, mode string, conditions map[string]float64) map[string]float64 {
	scores := make(map[string]float64)

	// Profit score: higher for tighter spread, moderate size, more orders
	scores["profit"] = (1.0 / spread) * math.Min(size/100.0, 1.0) * math.Min(float64(orderCount)/20.0, 1.0)

	// Risk score: higher for smaller size, fewer orders, conservative mode
	riskMultiplier := 1.0
	if mode == "PAUSED" {
		riskMultiplier = 2.0
	} else if mode == "REDUCED" {
		riskMultiplier = 1.5
	} else if mode == "MICRO" {
		riskMultiplier = 1.2
	}
	scores["risk"] = riskMultiplier * (100.0 / size) * (20.0 / float64(orderCount))

	// Volume score: higher for more orders, moderate spread
	scores["volume"] = float64(orderCount) * (0.0015 / spread)

	// Drawdown score: higher for conservative mode, smaller size
	scores["drawdown"] = riskMultiplier * (100.0 / size)

	return scores
}

// FindParetoOptimal finds Pareto optimal points from candidate solutions
func (r *RealTimeOptimizer) FindParetoOptimal(candidates []ParetoPoint) []ParetoPoint {
	// For each point, check if it's dominated by any other point
	for i := range candidates {
		for j := range candidates {
			if i == j {
				continue
			}
			if r.dominates(&candidates[j], &candidates[i]) {
				candidates[i].Dominates = append(candidates[i].Dominates, j)
			}
		}
	}

	// Return non-dominated points
	paretoOptimal := make([]ParetoPoint, 0)
	for _, point := range candidates {
		if len(point.Dominates) == 0 {
			paretoOptimal = append(paretoOptimal, point)
		}
	}

	return paretoOptimal
}

// dominates checks if point A dominates point B (A is better in at least one objective and not worse in any)
func (r *RealTimeOptimizer) dominates(a, b *ParetoPoint) bool {
	aBetter := false
	for objective := range a.Scores {
		if a.Scores[objective] > b.Scores[objective] {
			aBetter = true
		} else if a.Scores[objective] < b.Scores[objective] {
			return false // A is worse in this objective
		}
	}
	return aBetter
}

// SelectOperatingPoint selects the best operating point from Pareto frontier based on weights
func (r *RealTimeOptimizer) SelectOperatingPoint(paretoFrontier []ParetoPoint) *ParetoPoint {
	if len(paretoFrontier) == 0 {
		return nil
	}

	// Calculate weighted score for each point
	bestPoint := paretoFrontier[0]
	bestScore := -1.0

	for _, point := range paretoFrontier {
		score := r.weights.ProfitWeight*point.Scores["profit"] +
			r.weights.RiskWeight*point.Scores["risk"] +
			r.weights.VolumeWeight*point.Scores["volume"] +
			r.weights.DrawdownWeight*point.Scores["drawdown"]

		if score > bestScore {
			bestScore = score
			bestPoint = point
		}
	}

	return &bestPoint
}

// smoothSpread applies exponential smoothing to spread value
func (r *RealTimeOptimizer) smoothSpread(newSpread float64, currentTime time.Time) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Calculate percentage change
	if r.currentSpread > 0 {
		changePct := math.Abs(newSpread-r.currentSpread) / r.currentSpread
		if changePct < r.smoothing.MinChangePct {
			// Change too small, keep current
			return r.currentSpread
		}
		if changePct > r.smoothing.MaxChangePct {
			// Change too large, limit it
			if newSpread > r.currentSpread {
				newSpread = r.currentSpread * (1.0 + r.smoothing.MaxChangePct)
			} else {
				newSpread = r.currentSpread * (1.0 - r.smoothing.MaxChangePct)
			}
		}
	}

	// Apply exponential smoothing
	smoothedSpread := r.smoothing.Alpha*newSpread + (1.0-r.smoothing.Alpha)*r.currentSpread
	r.currentSpread = smoothedSpread
	r.lastUpdateTime = currentTime

	r.logger.Debug("Smoothed spread",
		zap.Float64("new", newSpread),
		zap.Float64("smoothed", smoothedSpread),
		zap.Float64("alpha", r.smoothing.Alpha))

	return smoothedSpread
}

// smoothOrderCount applies exponential smoothing to order count
func (r *RealTimeOptimizer) smoothOrderCount(newCount int, currentTime time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	// For integer values, use weighted average with rounding
	smoothedCount := int(math.Round(float64(r.smoothing.Alpha)*float64(newCount) + (1.0-r.smoothing.Alpha)*float64(r.currentOrderCount)))
	r.currentOrderCount = smoothedCount
	return smoothedCount
}

// smoothSize applies exponential smoothing to size value
func (r *RealTimeOptimizer) smoothSize(newSize float64, currentTime time.Time) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	smoothedSize := r.smoothing.Alpha*newSize + (1.0-r.smoothing.Alpha)*r.currentSize
	r.currentSize = smoothedSize
	return smoothedSize
}

// SetWeights sets the optimization weights
func (r *RealTimeOptimizer) SetWeights(weights OptimizationWeights) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.weights = weights
	r.logger.Info("Optimization weights updated",
		zap.Float64("profit_weight", weights.ProfitWeight),
		zap.Float64("risk_weight", weights.RiskWeight),
		zap.Float64("volume_weight", weights.VolumeWeight),
		zap.Float64("drawdown_weight", weights.DrawdownWeight))
}

// SetSmoothing sets the smoothing parameters
func (r *RealTimeOptimizer) SetSmoothing(smoothing SmoothingParameters) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.smoothing = smoothing
	r.logger.Info("Smoothing parameters updated",
		zap.Float64("alpha", smoothing.Alpha),
		zap.Float64("min_change_pct", smoothing.MinChangePct),
		zap.Float64("max_change_pct", smoothing.MaxChangePct),
		zap.Float64("max_change_per_sec", smoothing.MaxChangePerSec))
}

// GetCurrentParameters returns the current smoothed parameters
func (r *RealTimeOptimizer) GetCurrentParameters() (spread float64, orderCount int, size float64, mode string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentSpread, r.currentOrderCount, r.currentSize, r.currentMode
}
