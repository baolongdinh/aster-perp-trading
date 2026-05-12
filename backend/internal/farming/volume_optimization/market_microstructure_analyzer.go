package volume_optimization

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MicrostructureSignal combines VPIN and OBI for comprehensive market analysis
type MicrostructureSignal struct {
	CompositeScore      float64 // [-1, 1]: -1=extreme danger, 0=normal, 1=extreme opportunity
	TradingMode         string  // "AGGRESSIVE", "NEUTRAL", "DEFENSIVE", or "PAUSED"
	OrderSizeMultiplier float64 // 0.1-1.5x applied to base size
	SpreadMultiplier    float64 // 0.7-2.0x applied to base spread
	LeverageMultiplier  float64 // 0.25-1.0x applied to max leverage
	Confidence          float64 // 0-1, higher = more reliable signal
	Rationale           string  // Explanation of the signal
	VPINContribution    float64 // How much VPIN influenced the score
	OBIContribution     float64 // How much OBI influenced the score
	UpdatedAt           time.Time
}

// MarketMicrostructureAnalyzer combines VPIN and OBI for intelligent trading decisions
type MarketMicrostructureAnalyzer struct {
	vpin                *VPINMonitor
	obi                 *OrderBookImbalanceDetector
	logger              *zap.Logger
	mu                  sync.RWMutex
	lastSignal          *MicrostructureSignal
	signalHistory       []*MicrostructureSignal
	maxHistorySize      int
	aggressivenessLevel int // 1-5 scale for how aggressive to be
}

// NewMarketMicrostructureAnalyzer creates a new analyzer
func NewMarketMicrostructureAnalyzer(
	vpin *VPINMonitor,
	obi *OrderBookImbalanceDetector,
	logger *zap.Logger,
) *MarketMicrostructureAnalyzer {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &MarketMicrostructureAnalyzer{
		vpin:                vpin,
		obi:                 obi,
		logger:              logger,
		signalHistory:       make([]*MicrostructureSignal, 0, 100),
		maxHistorySize:      100,
		aggressivenessLevel: 3, // Default: medium (1=conservative, 5=aggressive)
	}
}

// SetAggressivenessLevel sets trading aggressiveness (1=conservative, 5=aggressive)
func (m *MarketMicrostructureAnalyzer) SetAggressivenessLevel(level int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if level < 1 {
		level = 1
	} else if level > 5 {
		level = 5
	}
	m.aggressivenessLevel = level
	m.logger.Info("Aggressiveness level updated", zap.Int("level", level))
}

// AnalyzeMarket generates a comprehensive trading signal
func (m *MarketMicrostructureAnalyzer) AnalyzeMarket() MicrostructureSignal {
	m.mu.Lock()
	defer m.mu.Unlock()

	signal := MicrostructureSignal{
		UpdatedAt: time.Now(),
	}

	// Get component signals
	vpin := m.vpin.CalculateVPIN()
	obiSignal := m.obi.GetSignal()

	// Normalize VPIN to [-1, 1] scale
	// Low VPIN (healthy) = positive score
	// High VPIN (toxic) = negative score
	vpinScore := (0.5 - vpin) * 2.0 // Map [0, 0.5] → [1, 0], [0.5, 1] → [0, -1]
	vpinScore = math.Max(-1, math.Min(1, vpinScore))

	// OBI signal already in [-1, 1]
	obiScore := obiSignal.OBIScore

	// Weighted average: VPIN more important for risk (60%), OBI for opportunity (40%)
	signal.CompositeScore = vpinScore*0.6 + obiScore*0.4
	signal.VPINContribution = vpinScore * 0.6
	signal.OBIContribution = obiScore * 0.4

	// Calculate confidence as average of both signals' confidences
	vpinConfidence := 1.0 - math.Abs(vpin-0.5)*2.0 // Confidence = how far from center
	obiConfidence := obiSignal.Confidence
	signal.Confidence = (vpinConfidence + obiConfidence) / 2.0

	// Determine trading mode and multipliers
	m.calculateMultipliers(&signal, vpin, obiSignal)

	// Store in history
	m.signalHistory = append(m.signalHistory, &signal)
	if len(m.signalHistory) > m.maxHistorySize {
		m.signalHistory = m.signalHistory[1:]
	}
	m.lastSignal = &signal

	m.logger.Debug("Market Microstructure Signal",
		zap.Float64("composite_score", signal.CompositeScore),
		zap.String("trading_mode", signal.TradingMode),
		zap.Float64("order_size_mult", signal.OrderSizeMultiplier),
		zap.Float64("spread_mult", signal.SpreadMultiplier),
		zap.Float64("leverage_mult", signal.LeverageMultiplier),
		zap.String("rationale", signal.Rationale))

	return signal
}

// calculateMultipliers derives trading parameters from the composite score
func (m *MarketMicrostructureAnalyzer) calculateMultipliers(
	signal *MicrostructureSignal,
	vpin float64,
	obiSignal OBISignal,
) {
	compositeScore := signal.CompositeScore

	// Adjust based on aggressiveness level (1=conservative, 5=aggressive)
	aggrFactor := float64(m.aggressivenessLevel) / 3.0 // Maps 1→0.33, 3→1.0, 5→1.67

	// ===========================================
	// CASE 1: EXTREME DANGER (compositeScore < -0.7)
	// ===========================================
	if compositeScore < -0.7 {
		signal.TradingMode = "PAUSED"
		signal.OrderSizeMultiplier = 0.0 // No orders
		signal.SpreadMultiplier = 2.5    // Very wide spread (if resuming)
		signal.LeverageMultiplier = 0.1  // Minimal leverage
		signal.Rationale = "Toxic flow detected (VPIN > 0.85) + extreme imbalance"
		return
	}

	// ===========================================
	// CASE 2: HIGH RISK (compositeScore -0.7 to -0.4)
	// ===========================================
	if compositeScore < -0.4 {
		signal.TradingMode = "DEFENSIVE"
		signal.OrderSizeMultiplier = 0.3 * aggrFactor // 30% of normal
		signal.SpreadMultiplier = 2.0 / aggrFactor    // Widen spread significantly
		signal.LeverageMultiplier = 0.25 * aggrFactor // Reduce leverage to 25%
		signal.Rationale = "Toxic flow (VPIN > 0.7) OR severe imbalance detected"
		return
	}

	// ===========================================
	// CASE 3: CAUTION (compositeScore -0.4 to 0.0)
	// ===========================================
	if compositeScore < 0 {
		signal.TradingMode = "NEUTRAL"
		signal.OrderSizeMultiplier = 0.7 * aggrFactor // 70% of normal
		signal.SpreadMultiplier = 1.3 / aggrFactor    // Moderate spread widening
		signal.LeverageMultiplier = 0.5 * aggrFactor  // 50% leverage
		signal.Rationale = "Moderately toxic flow (VPIN 0.5-0.7) OR moderate imbalance"
		return
	}

	// ===========================================
	// CASE 4: NEUTRAL/BALANCED (compositeScore 0.0 to 0.4)
	// ===========================================
	if compositeScore < 0.4 {
		signal.TradingMode = "NEUTRAL"
		signal.OrderSizeMultiplier = 1.0 * aggrFactor // Normal size
		signal.SpreadMultiplier = 1.0 / aggrFactor    // Normal spread
		signal.LeverageMultiplier = 0.75 * aggrFactor // 75% leverage
		signal.Rationale = "Healthy market conditions (VPIN < 0.5, balanced OBI)"
		return
	}

	// ===========================================
	// CASE 5: OPPORTUNITY (compositeScore > 0.4)
	// ===========================================
	if compositeScore > 0.4 {
		signal.TradingMode = "AGGRESSIVE"

		// Scale aggressiveness based on how strong the opportunity is
		opportunityStrength := math.Min(compositeScore, 1.0)

		signal.OrderSizeMultiplier = (1.0 + opportunityStrength*0.5) * aggrFactor // 1.0-1.5x
		signal.SpreadMultiplier = (1.0 - opportunityStrength*0.3) / aggrFactor    // 0.7-1.0x (tighten)
		signal.LeverageMultiplier = (1.0 - opportunityStrength*0.3) * aggrFactor  // 0.7-1.0x (full leverage)

		// Add OBI-specific guidance
		if obiSignal.BiasDirection == "BUY" {
			signal.Rationale = "Excellent conditions: Low VPIN + buy pressure (bullish imbalance)"
		} else if obiSignal.BiasDirection == "SELL" {
			signal.Rationale = "Excellent conditions: Low VPIN + sell pressure (bearish imbalance)"
		} else {
			signal.Rationale = "Excellent conditions: Low VPIN + balanced OBI (ideal for maker)"
		}

		return
	}

	// Fallback (shouldn't reach here)
	signal.TradingMode = "NEUTRAL"
	signal.OrderSizeMultiplier = 1.0
	signal.SpreadMultiplier = 1.0
	signal.LeverageMultiplier = 0.75
	signal.Rationale = "Unknown market conditions"
}

// GetLastSignal returns the most recent signal
func (m *MarketMicrostructureAnalyzer) GetLastSignal() *MicrostructureSignal {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lastSignal == nil {
		return &MicrostructureSignal{
			CompositeScore:      0,
			TradingMode:         "NEUTRAL",
			OrderSizeMultiplier: 1.0,
			SpreadMultiplier:    1.0,
			LeverageMultiplier:  0.75,
		}
	}

	return m.lastSignal
}

// GetSignalTrend analyzes how signals have evolved over time
// Returns: "IMPROVING", "DETERIORATING", or "STABLE"
func (m *MarketMicrostructureAnalyzer) GetSignalTrend() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.signalHistory) < 3 {
		return "INSUFFICIENT_DATA"
	}

	// Compare last 3 signals
	current := m.signalHistory[len(m.signalHistory)-1]
	prev1 := m.signalHistory[len(m.signalHistory)-2]
	prev2 := m.signalHistory[len(m.signalHistory)-3]

	currentScore := current.CompositeScore
	prev1Score := prev1.CompositeScore
	prev2Score := prev2.CompositeScore

	// Trend detection
	improving := currentScore > prev1Score && prev1Score > prev2Score
	deteriorating := currentScore < prev1Score && prev1Score < prev2Score

	if improving && (current.CompositeScore-prev2Score) > 0.1 {
		return "IMPROVING"
	} else if deteriorating && (prev2Score-current.CompositeScore) > 0.1 {
		return "DETERIORATING"
	}

	return "STABLE"
}

// GetRiskLevel returns a simple risk assessment
func (m *MarketMicrostructureAnalyzer) GetRiskLevel() string {
	m.mu.RLock()
	signal := m.lastSignal
	m.mu.RUnlock()

	if signal == nil {
		return "UNKNOWN"
	}

	switch signal.TradingMode {
	case "PAUSED":
		return "CRITICAL"
	case "DEFENSIVE":
		return "HIGH"
	case "NEUTRAL":
		if signal.CompositeScore < -0.2 {
			return "MEDIUM"
		}
		return "LOW"
	case "AGGRESSIVE":
		return "LOW"
	default:
		return "UNKNOWN"
	}
}

// ShouldPauseTrading returns true if market conditions warrant a trading pause
func (m *MarketMicrostructureAnalyzer) ShouldPauseTrading() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lastSignal == nil {
		return false
	}

	return m.lastSignal.TradingMode == "PAUSED"
}

// GetMetrics returns all analyzer metrics for monitoring/logging
func (m *MarketMicrostructureAnalyzer) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	signal := m.GetLastSignal()

	return map[string]interface{}{
		"composite_score":       signal.CompositeScore,
		"trading_mode":          signal.TradingMode,
		"order_size_multiplier": signal.OrderSizeMultiplier,
		"spread_multiplier":     signal.SpreadMultiplier,
		"leverage_multiplier":   signal.LeverageMultiplier,
		"confidence":            signal.Confidence,
		"rationale":             signal.Rationale,
		"risk_level":            m.GetRiskLevel(),
		"signal_trend":          m.GetSignalTrend(),
		"vpin_contribution":     signal.VPINContribution,
		"obi_contribution":      signal.OBIContribution,
		"should_pause":          m.ShouldPauseTrading(),
	}
}
