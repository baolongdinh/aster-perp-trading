package adaptive_grid

import (
	"fmt"
	"math"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"

	"go.uber.org/zap"
)

// MarketCondition represents evaluated market condition scores
type MarketCondition struct {
	VolatilityScore float64 // 0-1, higher = more volatile
	TrendScore      float64 // 0-1, higher = stronger trend
	PositionScore   float64 // 0-1, higher = larger position
	RiskScore       float64 // 0-1, higher = higher risk
	MarketScore     float64 // 0-1, higher = better market conditions
}

// StateRecommendation represents a recommended state with confidence
type StateRecommendation struct {
	State      GridState
	Confidence float64 // 0-1
	Reason     string
	Conditions MarketCondition
}

// MarketConditionEvaluator evaluates market conditions and recommends optimal state
type MarketConditionEvaluator struct {
	config *config.MarketConditionEvaluatorConfig
	logger *zap.Logger
	mu     sync.RWMutex

	// Data source references
	adaptiveGridManager interface{} // AdaptiveGridManager reference
	riskManager         interface{} // RiskManager reference
	wsClient            interface{} // WebSocketClient reference
	circuitBreaker      interface{} // CircuitBreaker reference

	// NEW: AdaptiveThresholdManager for adaptive thresholds
	adaptiveThresholdManager *AdaptiveThresholdManager

	// State tracking for stability duration
	lastStateChangeTime map[string]time.Time // symbol -> last state change time
}

// NewMarketConditionEvaluator creates a new market condition evaluator
func NewMarketConditionEvaluator(config *config.MarketConditionEvaluatorConfig, logger *zap.Logger) *MarketConditionEvaluator {
	return &MarketConditionEvaluator{
		config:              config,
		logger:              logger,
		lastStateChangeTime: make(map[string]time.Time),
	}
}

// SetAdaptiveGridManager sets the adaptive grid manager reference
func (e *MarketConditionEvaluator) SetAdaptiveGridManager(agrid interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.adaptiveGridManager = agrid
}

// SetRiskManager sets the risk manager reference
func (e *MarketConditionEvaluator) SetRiskManager(rm interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.riskManager = rm
}

// SetWebSocketClient sets the WebSocket client reference
func (e *MarketConditionEvaluator) SetWebSocketClient(ws interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.wsClient = ws
}

// SetCircuitBreaker sets the circuit breaker reference
func (e *MarketConditionEvaluator) SetCircuitBreaker(cb interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.circuitBreaker = cb
}

// SetAdaptiveThresholdManager sets the adaptive threshold manager
func (e *MarketConditionEvaluator) SetAdaptiveThresholdManager(atm *AdaptiveThresholdManager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.adaptiveThresholdManager = atm
	e.logger.Info("AdaptiveThresholdManager set on MarketConditionEvaluator")
}

// GetConfig returns the evaluator config
func (e *MarketConditionEvaluator) GetConfig() *config.MarketConditionEvaluatorConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// Evaluate evaluates market conditions for a symbol and recommends state
func (e *MarketConditionEvaluator) Evaluate(symbol string) (*StateRecommendation, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.config.Enabled {
		return &StateRecommendation{
			State:      GridStateTrading,
			Confidence: 1.0,
			Reason:     "Evaluator disabled, default to TRADING",
		}, nil
	}

	// Evaluate all factors
	conditions := MarketCondition{
		VolatilityScore: e.evaluateVolatility(symbol),
		TrendScore:      e.evaluateTrend(symbol),
		PositionScore:   e.evaluatePosition(symbol),
		RiskScore:       e.evaluateRisk(symbol),
		MarketScore:     e.evaluateMarket(symbol),
	}

	// Select state based on conditions
	recommendation := e.recommendState(conditions, symbol)

	e.logger.Info("Market condition evaluation",
		zap.String("symbol", symbol),
		zap.Float64("volatility_score", conditions.VolatilityScore),
		zap.Float64("trend_score", conditions.TrendScore),
		zap.Float64("position_score", conditions.PositionScore),
		zap.Float64("risk_score", conditions.RiskScore),
		zap.Float64("market_score", conditions.MarketScore),
		zap.String("recommended_state", recommendation.State.String()),
		zap.Float64("confidence", recommendation.Confidence),
		zap.String("reason", recommendation.Reason),
	)

	return recommendation, nil
}

// evaluateVolatility evaluates volatility based on ATR, BB width, price swing
func (e *MarketConditionEvaluator) evaluateVolatility(symbol string) float64 {
	if e.adaptiveGridManager == nil {
		return 0.5 // Default medium volatility
	}

	// Type assertion to AdaptiveGridManager
	agrid, ok := e.adaptiveGridManager.(interface{ GetRangeDetector(string) *RangeDetector })
	if !ok {
		return 0.5
	}

	// Get range detector for this symbol
	rangeDetector := agrid.GetRangeDetector(symbol)
	if rangeDetector == nil {
		return 0.5
	}

	// Get ATR from range detector
	atr := rangeDetector.GetATR()
	if atr == 0 {
		return 0.5
	}

	// Get current range for BB width
	currentRange := rangeDetector.GetCurrentRange()
	if currentRange == nil {
		return 0.5
	}

	// Calculate volatility score based on ATR and BB width
	// Higher ATR and wider BB = higher volatility

	// ATR Score: Normalize ATR relative to price
	// Typical ATR for crypto: 0.1% - 2% of price
	price := currentRange.MidPrice
	atrPct := (atr / price) * 100
	// Map ATR% to 0-1: 0.1% -> 0, 2% -> 1
	atrScore := (atrPct - 0.1) / 1.9
	if atrScore < 0 {
		atrScore = 0
	}
	if atrScore > 1 {
		atrScore = 1
	}

	// BB Width Score: Wider BB = higher volatility
	// Typical BB width: 0.5% - 5% of price
	bbWidthPct := currentRange.WidthPct
	// Map BB width to 0-1: 0.5% -> 0, 5% -> 1
	bbScore := (bbWidthPct - 0.5) / 4.5
	if bbScore < 0 {
		bbScore = 0
	}
	if bbScore > 1 {
		bbScore = 1
	}

	// Combine scores (weighted average)
	volatilityScore := (atrScore*0.6 + bbScore*0.4)
	return volatilityScore
}

// evaluateTrend evaluates trend strength based on ADX and trend direction
func (e *MarketConditionEvaluator) evaluateTrend(symbol string) float64 {
	if e.adaptiveGridManager == nil {
		return 0.5 // Default medium trend
	}

	// Type assertion to AdaptiveGridManager
	agrid, ok := e.adaptiveGridManager.(interface{ GetRangeDetector(string) *RangeDetector })
	if !ok {
		return 0.5
	}

	// Get range detector for this symbol
	rangeDetector := agrid.GetRangeDetector(symbol)
	if rangeDetector == nil {
		return 0.5
	}

	// Get ADX from range detector
	adx := rangeDetector.GetCurrentADX()
	if adx == 0 {
		return 0.5
	}

	// Calculate trend score based on ADX
	// Higher ADX = stronger trend
	// Normalize to 0-1 range (ADX typically 0-100)
	// ADX < 20 = weak trend, ADX 20-40 = moderate, ADX > 40 = strong
	trendScore := adx / 60.0 // 60 ADX = maximum score
	if trendScore > 1.0 {
		trendScore = 1.0
	}
	return trendScore
}

// evaluatePosition evaluates position size and PnL
func (e *MarketConditionEvaluator) evaluatePosition(symbol string) float64 {
	if e.wsClient == nil {
		return 0.0 // No position
	}

	// Type assertion to WebSocketClient
	ws, ok := e.wsClient.(interface {
		GetCachedPositions() map[string]client.Position
	})
	if !ok {
		return 0.0
	}

	positions := ws.GetCachedPositions()
	position, hasPosition := positions[symbol]
	if !hasPosition || position.PositionAmt == 0 {
		return 0.0 // No position
	}

	// Calculate position notional value
	positionNotional := math.Abs(position.PositionAmt * position.MarkPrice)

	// Get max position USDT (default to 100 for now)
	// TODO: Pass max position config via constructor or setter
	maxPositionUSDT := 100.0

	// Calculate position score based on size relative to max
	// 0% of max = 0, 100% of max = 1
	positionScore := positionNotional / maxPositionUSDT
	if positionScore > 1.0 {
		positionScore = 1.0
	}

	return positionScore
}

// evaluateRisk evaluates risk based on daily PnL, drawdown, consecutive losses
func (e *MarketConditionEvaluator) evaluateRisk(symbol string) float64 {
	if e.wsClient == nil {
		return 0.5 // Default medium risk
	}

	// Type assertion to WebSocketClient
	ws, ok := e.wsClient.(interface {
		GetCachedPositions() map[string]client.Position
	})
	if !ok {
		return 0.5
	}

	positions := ws.GetCachedPositions()
	position, hasPosition := positions[symbol]
	if !hasPosition || position.PositionAmt == 0 {
		return 0.0 // No position = no risk
	}

	// Calculate risk score based on unrealized PnL
	// Loss = higher risk, Profit = lower risk
	unrealizedPnL := position.UnrealizedProfit

	// Map PnL to risk score
	// -$10 loss = 1.0 (max risk), $0 = 0.5, $10 profit = 0.0 (min risk)
	riskScore := 0.5 - (unrealizedPnL / 20.0) // $20 range
	if riskScore < 0 {
		riskScore = 0.0
	}
	if riskScore > 1.0 {
		riskScore = 1.0
	}

	return riskScore
}

// evaluateMarket evaluates market conditions (spread, volume, funding rate)
func (e *MarketConditionEvaluator) evaluateMarket(symbol string) float64 {
	if e.wsClient == nil {
		return 0.5 // Default medium market conditions
	}

	// For now, return medium score as market conditions are complex
	// TODO: Implement spread, volume, funding rate evaluation
	// - Spread: Wider = lower score
	// - Volume: Higher = higher score
	// - Funding: Extreme = lower score
	_ = e.wsClient // Remove unused variable warning
	marketScore := 0.5
	return marketScore
}

// recommendState selects the optimal state based on conditions
func (e *MarketConditionEvaluator) recommendState(conditions MarketCondition, symbol string) *StateRecommendation {
	// State selection logic (from plan)

	// Get adaptive thresholds if available, otherwise use fixed defaults
	positionThreshold := 0.8
	volatilityThreshold := 0.8
	riskThreshold := 0.6
	riskThresholdHigh := 0.7
	riskThresholdCritical := 0.9
	positionThresholdLow := 0.5
	positionThresholdHigh := 0.95

	if e.adaptiveThresholdManager != nil {
		positionThreshold = e.adaptiveThresholdManager.GetThreshold(symbol, "position")
		volatilityThreshold = e.adaptiveThresholdManager.GetThreshold(symbol, "volatility")
		riskThreshold = e.adaptiveThresholdManager.GetThreshold(symbol, "risk")
		riskThresholdHigh = e.adaptiveThresholdManager.GetThreshold(symbol, "risk")         // Could use different threshold
		riskThresholdCritical = e.adaptiveThresholdManager.GetThreshold(symbol, "risk")     // Could use different threshold
		positionThresholdLow = e.adaptiveThresholdManager.GetThreshold(symbol, "position")  // Could use different threshold
		positionThresholdHigh = e.adaptiveThresholdManager.GetThreshold(symbol, "position") // Could use different threshold

		e.logger.Debug("Using adaptive thresholds",
			zap.String("symbol", symbol),
			zap.Float64("position_threshold", positionThreshold),
			zap.Float64("volatility_threshold", volatilityThreshold),
			zap.Float64("risk_threshold", riskThreshold))
	}

	// OVER_SIZE: PositionScore > threshold
	if conditions.PositionScore > positionThreshold {
		return &StateRecommendation{
			State:      GridStateOverSize,
			Confidence: 0.8,
			Reason:     fmt.Sprintf("Position size too large (score: %.2f, threshold: %.2f)", conditions.PositionScore, positionThreshold),
			Conditions: conditions,
		}
	}

	// DEFENSIVE: VolatilityScore > threshold OR RiskScore > threshold
	if conditions.VolatilityScore > volatilityThreshold || conditions.RiskScore > riskThreshold {
		return &StateRecommendation{
			State:      GridStateDefensive,
			Confidence: 0.8,
			Reason:     fmt.Sprintf("Extreme volatility or risk (vol: %.2f, risk: %.2f, vol_threshold: %.2f, risk_threshold: %.2f)", conditions.VolatilityScore, conditions.RiskScore, volatilityThreshold, riskThreshold),
			Conditions: conditions,
		}
	}

	// RECOVERY: RiskScore > threshold AND PositionScore < low threshold
	if conditions.RiskScore > riskThreshold && conditions.PositionScore < positionThresholdLow {
		return &StateRecommendation{
			State:      GridStateRecovery,
			Confidence: 0.7,
			Reason:     fmt.Sprintf("Recovery mode (risk: %.2f, position: %.2f, risk_threshold: %.2f, pos_threshold_low: %.2f)", conditions.RiskScore, conditions.PositionScore, riskThreshold, positionThresholdLow),
			Conditions: conditions,
		}
	}

	// EXIT_HALF: RiskScore > high threshold AND PositionScore > low threshold
	if conditions.RiskScore > riskThresholdHigh && conditions.PositionScore > positionThresholdLow {
		return &StateRecommendation{
			State:      GridStateExitHalf,
			Confidence: 0.7,
			Reason:     fmt.Sprintf("Partial loss (risk: %.2f, position: %.2f, risk_threshold_high: %.2f, pos_threshold_low: %.2f)", conditions.RiskScore, conditions.PositionScore, riskThresholdHigh, positionThresholdLow),
			Conditions: conditions,
		}
	}

	// EXIT_ALL: RiskScore > critical threshold OR PositionScore > high threshold
	if conditions.RiskScore > riskThresholdCritical || conditions.PositionScore > positionThresholdHigh {
		return &StateRecommendation{
			State:      GridStateExitAll,
			Confidence: 0.9,
			Reason:     fmt.Sprintf("Critical risk (risk: %.2f, position: %.2f, risk_threshold_critical: %.2f, pos_threshold_high: %.2f)", conditions.RiskScore, conditions.PositionScore, riskThresholdCritical, positionThresholdHigh),
			Conditions: conditions,
		}
	}

	// Default: TRADING
	return &StateRecommendation{
		State:      GridStateTrading,
		Confidence: 0.6,
		Reason:     "Normal trading conditions",
		Conditions: conditions,
	}
}
