package regime

import (
	"math"
	"sync"
)

// CorrelationTracker calculates Pearson correlation between symbol pairs.
type CorrelationTracker struct {
	mu          sync.RWMutex
	classifiers map[string]*Classifier
	threshold   float64 // Correlation above this is considered "highly correlated"
}

func NewCorrelationTracker(classifiers map[string]*Classifier, threshold float64) *CorrelationTracker {
	return &CorrelationTracker{
		classifiers: classifiers,
		threshold:   threshold,
	}
}

// Calculate returns the Pearson correlation coefficient between two symbols.
// It uses the last N periods of log-returns from the 1h timeframe by default.
func (t *CorrelationTracker) Calculate(s1, s2 string, timeframe string, periods int) float64 {
	c1, ok1 := t.classifiers[s1]
	c2, ok2 := t.classifiers[s2]
	if !ok1 || !ok2 {
		return 0
	}

	r1 := c1.GetLogReturns(timeframe, periods)
	r2 := c2.GetLogReturns(timeframe, periods)

	if len(r1) < 2 || len(r2) < 2 || len(r1) != len(r2) {
		// If lengths differ, trim to shortest
		minLen := len(r1)
		if len(r2) < minLen {
			minLen = len(r2)
		}
		if minLen < 2 {
			return 0
		}
		r1 = r1[:minLen]
		r2 = r2[:minLen]
	}

	return pearson(r1, r2)
}

// GetHighlyCorrelated returns symbols that are highly correlated with the target symbol.
func (t *CorrelationTracker) GetHighlyCorrelated(target string, activeSymbols []string) []string {
	var results []string
	for _, sym := range activeSymbols {
		if sym == target {
			continue
		}
		corr := t.Calculate(target, sym, "1h", 24) // Daily correlation (24h)
		if math.Abs(corr) >= t.threshold {
			results = append(results, sym)
		}
	}
	return results
}

func pearson(x, y []float64) float64 {
	n := float64(len(x))
	var sumX, sumY, sumXY, sumX2, sumY2 float64

	for i := 0; i < len(x); i++ {
		sumX += x[i]
		sumY += y[i]
		sumXY += x[i] * y[i]
		sumX2 += x[i] * x[i]
		sumY2 += y[i] * y[i]
	}

	numerator := (n * sumXY) - (sumX * sumY)
	denominator := math.Sqrt((n*sumX2 - sumX*sumX) * (n*sumY2 - sumY*sumY))

	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}
