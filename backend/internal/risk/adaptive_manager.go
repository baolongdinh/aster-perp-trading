package risk

import (
	"fmt"
	"sync"

	"aster-bot/internal/farming/market_regime"
)

// AdaptiveRiskManager extends risk manager with regime-aware risk limits
type AdaptiveRiskManager struct {
	baseManager   *Manager
	regimeConfigs map[string]*AdaptiveRiskConfig
	currentRegime map[string]market_regime.MarketRegime
	mu            sync.RWMutex
}

// AdaptiveRiskConfig contains regime-specific risk parameters
type AdaptiveRiskConfig struct {
	MaxPositionUSDT        float64
	MaxOrdersPerSide       int
	DailyLossLimitUSDT     float64
	PositionTimeoutMinutes int
	RiskPerTradeUSDT       float64
}

// NewAdaptiveRiskManager creates a new adaptive risk manager
func NewAdaptiveRiskManager(baseManager *Manager) *AdaptiveRiskManager {
	return &AdaptiveRiskManager{
		baseManager:   baseManager,
		regimeConfigs: make(map[string]*AdaptiveRiskConfig),
		currentRegime: make(map[string]market_regime.MarketRegime),
		mu:            sync.RWMutex{},
	}
}

// SetRegimeConfig sets risk configuration for specific regime
func (a *AdaptiveRiskManager) SetRegimeConfig(
	regime market_regime.MarketRegime,
	config *AdaptiveRiskConfig,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if config == nil {
		return fmt.Errorf("risk config cannot be nil")
	}

	a.regimeConfigs[string(regime)] = config
	return nil
}

// GetRegimeConfig returns risk configuration for specific regime
func (a *AdaptiveRiskManager) GetRegimeConfig(
	regime market_regime.MarketRegime,
) (*AdaptiveRiskConfig, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	config, exists := a.regimeConfigs[string(regime)]
	return config, exists
}

// CanEnterWithRegime checks if entry is allowed for specific regime
func (a *AdaptiveRiskManager) CanEnterWithRegime(
	symbol string,
	notional float64,
	leverage float64,
	regime market_regime.MarketRegime,
) error {
	// Get regime-specific config
	regimeConfig, exists := a.GetRegimeConfig(regime)
	if !exists {
		// Fall back to base manager if no regime config
		return a.baseManager.CanEnter(symbol, notional, leverage)
	}

	// Check regime-specific position limit
	if notional > regimeConfig.MaxPositionUSDT {
		return fmt.Errorf("risk: position exceeds regime-specific limit (%.2f > %.2f)",
			notional, regimeConfig.MaxPositionUSDT)
	}

	// Check regime-specific daily loss
	if a.baseManager.dailyPnL <= -regimeConfig.DailyLossLimitUSDT {
		return fmt.Errorf("risk: regime-specific daily loss limit reached")
	}

	// Use base manager for other checks
	return a.baseManager.CanEnter(symbol, notional, leverage)
}

// UpdateRegime updates current regime for symbol
func (a *AdaptiveRiskManager) UpdateRegime(symbol string, regime market_regime.MarketRegime) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.currentRegime[symbol] = regime
}

// GetCurrentRegime returns current regime for symbol
func (a *AdaptiveRiskManager) GetCurrentRegime(symbol string) market_regime.MarketRegime {
	a.mu.RLock()
	defer a.mu.RUnlock()

	regime, exists := a.currentRegime[symbol]
	if !exists {
		return market_regime.RegimeUnknown
	}
	return regime
}
