package adaptive_grid

import (
	"fmt"

	"aster-bot/internal/config"
	"aster-bot/internal/farming/market_regime"

	"go.uber.org/zap"
)

// ParameterApplier applies regime-specific parameters to grid manager
type ParameterApplier struct {
	configManager ConfigManagerInterface
	logger        *zap.Logger
}

// NewParameterApplier creates a new parameter applier
func NewParameterApplier(configManager ConfigManagerInterface, logger *zap.Logger) *ParameterApplier {
	return &ParameterApplier{
		configManager: configManager,
		logger:        logger,
	}
}

// ApplyRegimeParameters applies regime-specific parameters to grid manager
func (p *ParameterApplier) ApplyRegimeParameters(
	gridManager GridManagerInterface,
	symbol string,
	regime market_regime.MarketRegime,
) error {
	// Get regime configuration
	regimeConfig, exists := p.configManager.GetRegimeConfig(string(regime))
	if !exists {
		return fmt.Errorf("no configuration found for regime: %s", regime)
	}

	p.logger.Info("Applying regime parameters",
		zap.String("symbol", symbol),
		zap.String("regime", string(regime)),
		zap.Float64("order_size", regimeConfig.OrderSizeUSDT),
		zap.Float64("grid_spread", regimeConfig.GridSpreadPct))

	// Apply parameters to grid manager
	if err := p.applyGridParameters(gridManager, regimeConfig); err != nil {
		return fmt.Errorf("failed to apply grid parameters: %w", err)
	}

	p.logger.Info("Regime parameters applied successfully",
		zap.String("symbol", symbol),
		zap.String("regime", string(regime)))

	return nil
}

// applyGridParameters applies configuration to grid manager
func (p *ParameterApplier) applyGridParameters(gridManager GridManagerInterface, config *config.RegimeConfig) error {
	// Apply order size
	if config.OrderSizeUSDT > 0 {
		gridManager.SetOrderSize(config.OrderSizeUSDT)
	}

	// Apply grid spread
	if config.GridSpreadPct > 0 {
		gridManager.SetGridSpread(config.GridSpreadPct)
	}

	// Apply max orders per side
	if config.MaxOrdersPerSide > 0 {
		gridManager.SetMaxOrdersPerSide(config.MaxOrdersPerSide)
	}

	// Apply position timeout
	if config.PositionTimeoutMinutes > 0 {
		gridManager.SetPositionTimeout(config.PositionTimeoutMinutes)
	}

	return nil
}

// ValidateRegimeParameters validates regime parameters before application
func (p *ParameterApplier) ValidateRegimeParameters(config *config.RegimeConfig) error {
	if config == nil {
		return fmt.Errorf("regime config cannot be nil")
	}

	// Validate order size
	if config.OrderSizeUSDT < 1 || config.OrderSizeUSDT > 1000 {
		return fmt.Errorf("invalid order size: %.2f (must be between 1 and 1000)", config.OrderSizeUSDT)
	}

	// Validate grid spread
	if config.GridSpreadPct < 0.001 || config.GridSpreadPct > 1.0 {
		return fmt.Errorf("invalid grid spread: %.4f (must be between 0.001%% and 1.0%%)", config.GridSpreadPct)
	}

	// Validate max orders per side
	if config.MaxOrdersPerSide < 1 || config.MaxOrdersPerSide > 20 {
		return fmt.Errorf("invalid max orders per side: %d (must be between 1 and 20)", config.MaxOrdersPerSide)
	}

	// Validate position timeout
	if config.PositionTimeoutMinutes < 1 || config.PositionTimeoutMinutes > 1440 {
		return fmt.Errorf("invalid position timeout: %d (must be between 1 minute and 24 hours)", config.PositionTimeoutMinutes)
	}

	return nil
}
