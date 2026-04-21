package agentic

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// AgenticRiskManager manages risk checks and controls
type AgenticRiskManager struct {
	logger *zap.Logger

	// Risk limits
	maxPositionUSDT      float64
	maxPortfolioExposure float64
	dailyLossLimitPct    float64
	maxLeverage          float64
	drawdownLimitPct     float64
	minMarginPct         float64

	// Risk tracking
	portfolioExposure float64
	dailyLossUSDT     float64
	peakEquity        float64
	currentEquity     float64
	lastDailyReset    time.Time

	// Symbol-specific tracking
	symbolPositions map[string]float64 // symbol -> position size USDT
	symbolDailyLoss map[string]float64 // symbol -> daily loss USDT

	// Circuit breaker integration
	circuitBreaker *CircuitBreaker

	// State
	mu sync.RWMutex
}

// NewAgenticRiskManager creates a new risk manager
func NewAgenticRiskManager(
	logger *zap.Logger,
) *AgenticRiskManager {
	return &AgenticRiskManager{
		logger:               logger.With(zap.String("component", "agentic_risk_manager")),
		maxPositionUSDT:      1000.0, // Default max position size
		maxPortfolioExposure: 5000.0, // Default max portfolio exposure
		dailyLossLimitPct:    0.05,   // 5% daily loss limit
		maxLeverage:          100.0,  // Default max leverage
		drawdownLimitPct:     0.10,   // 10% drawdown limit
		minMarginPct:         0.20,   // 20% minimum margin requirement
		portfolioExposure:    0.0,
		dailyLossUSDT:        0.0,
		peakEquity:           0.0,
		currentEquity:        0.0,
		lastDailyReset:       time.Now(),
		symbolPositions:      make(map[string]float64),
		symbolDailyLoss:      make(map[string]float64),
	}
}

// SetCircuitBreaker sets the circuit breaker for risk manager
func (arm *AgenticRiskManager) SetCircuitBreaker(cb *CircuitBreaker) {
	arm.mu.Lock()
	defer arm.mu.Unlock()
	arm.circuitBreaker = cb
	arm.logger.Info("Circuit breaker set for risk manager")
}

// UpdateEquity updates current equity and tracks peak/drawdown
func (arm *AgenticRiskManager) UpdateEquity(equity float64) {
	arm.mu.Lock()
	defer arm.mu.Unlock()

	arm.currentEquity = equity

	// Update peak equity
	if equity > arm.peakEquity {
		arm.peakEquity = equity
	}

	// Check if we need to reset daily stats
	if time.Since(arm.lastDailyReset) >= 24*time.Hour {
		arm.resetDailyStats()
	}

	// Check drawdown and log warning (circuit breaker will be triggered by monitoring)
	drawdown := arm.calculateDrawdown()
	if drawdown > arm.drawdownLimitPct {
		arm.logger.Error("Drawdown limit exceeded - manual intervention required",
			zap.Float64("drawdown_pct", drawdown*100),
			zap.Float64("limit_pct", arm.drawdownLimitPct*100),
		)
		// Note: Circuit breaker should be triggered by external monitoring
	}
}

// resetDailyStats resets daily tracking statistics
func (arm *AgenticRiskManager) resetDailyStats() {
	arm.dailyLossUSDT = 0
	arm.symbolDailyLoss = make(map[string]float64)
	arm.lastDailyReset = time.Now()
	arm.logger.Info("Daily risk stats reset")
}

// calculateDrawdown calculates current drawdown percentage
func (arm *AgenticRiskManager) calculateDrawdown() float64 {
	if arm.peakEquity <= 0 {
		return 0
	}
	return (arm.peakEquity - arm.currentEquity) / arm.peakEquity
}

// UpdatePosition updates position size for a symbol
func (arm *AgenticRiskManager) UpdatePosition(symbol string, positionUSDT float64) {
	arm.mu.Lock()
	defer arm.mu.Unlock()

	// Update portfolio exposure
	oldPosition := arm.symbolPositions[symbol]
	arm.portfolioExposure = arm.portfolioExposure - oldPosition + positionUSDT
	arm.symbolPositions[symbol] = positionUSDT

	arm.logger.Debug("Position updated",
		zap.String("symbol", symbol),
		zap.Float64("old_position", oldPosition),
		zap.Float64("new_position", positionUSDT),
		zap.Float64("portfolio_exposure", arm.portfolioExposure),
	)
}

// RecordLoss records a loss for daily tracking
func (arm *AgenticRiskManager) RecordLoss(symbol string, lossUSDT float64) {
	arm.mu.Lock()
	defer arm.mu.Unlock()

	arm.dailyLossUSDT += lossUSDT
	arm.symbolDailyLoss[symbol] += lossUSDT

	// Check daily loss limit and log warning
	if arm.peakEquity > 0 {
		dailyLossPct := arm.dailyLossUSDT / arm.peakEquity
		if dailyLossPct > arm.dailyLossLimitPct {
			arm.logger.Error("Daily loss limit exceeded - consider stopping trading",
				zap.Float64("daily_loss_pct", dailyLossPct*100),
				zap.Float64("limit_pct", arm.dailyLossLimitPct*100),
				zap.Float64("total_loss_usdt", arm.dailyLossUSDT),
			)
			// Note: Circuit breaker should be triggered by external monitoring
		}
	}
}

// CheckPositionSize validates position size is within limits
func (arm *AgenticRiskManager) CheckPositionSize(symbol string, sizeUSDT float64) bool {
	arm.mu.RLock()
	defer arm.mu.RUnlock()

	if sizeUSDT > arm.maxPositionUSDT {
		arm.logger.Warn("Position size exceeds limit",
			zap.String("symbol", symbol),
			zap.Float64("size_usdt", sizeUSDT),
			zap.Float64("max_usdt", arm.maxPositionUSDT),
		)
		return false
	}

	return true
}

// CheckPortfolioExposure validates total portfolio exposure
func (arm *AgenticRiskManager) CheckPortfolioExposure(totalExposureUSDT float64) bool {
	arm.mu.RLock()
	defer arm.mu.RUnlock()

	// TODO: Implement portfolio exposure check
	// This will be enhanced in Phase 3
	return true
}

// CheckDailyLoss validates daily loss is within limits
func (arm *AgenticRiskManager) CheckDailyLoss(dailyLossPct float64) bool {
	arm.mu.RLock()
	defer arm.mu.RUnlock()

	if dailyLossPct > arm.dailyLossLimitPct {
		arm.logger.Warn("Daily loss exceeds limit",
			zap.Float64("daily_loss_pct", dailyLossPct),
			zap.Float64("limit_pct", arm.dailyLossLimitPct),
		)
		return false
	}

	return true
}

// CheckDrawdown validates drawdown is within limits
func (arm *AgenticRiskManager) CheckDrawdown(drawdownPct float64) bool {
	arm.mu.RLock()
	defer arm.mu.RUnlock()

	if drawdownPct > arm.drawdownLimitPct {
		arm.logger.Warn("Drawdown exceeds limit",
			zap.Float64("drawdown_pct", drawdownPct),
			zap.Float64("limit_pct", arm.drawdownLimitPct),
		)
		return false
	}

	return true
}

// CheckMargin validates margin availability
func (arm *AgenticRiskManager) CheckMargin(availableMargin float64, requiredMargin float64) bool {
	if requiredMargin > availableMargin {
		arm.logger.Warn("Insufficient margin",
			zap.Float64("available", availableMargin),
			zap.Float64("required", requiredMargin),
		)
		return false
	}

	return true
}

// CheckLeverage validates leverage is within limits
func (arm *AgenticRiskManager) CheckLeverage(leverage float64) bool {
	arm.mu.RLock()
	defer arm.mu.RUnlock()

	if leverage > arm.maxLeverage {
		arm.logger.Warn("Leverage exceeds limit",
			zap.Float64("leverage", leverage),
			zap.Float64("max_leverage", arm.maxLeverage),
		)
		return false
	}

	return true
}
