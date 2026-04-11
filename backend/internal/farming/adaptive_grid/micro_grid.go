package adaptive_grid

// MicroGridConfig holds configuration for micro grid trading
// Optimized for maximum volume with micro profits (8-40 bps)
type MicroGridConfig struct {
	Enabled          bool    `yaml:"enabled" mapstructure:"enabled"`
	SpreadPct        float64 `yaml:"spread_pct" mapstructure:"spread_pct"`             // e.g., 0.0005 = 0.05%
	OrdersPerSide    int     `yaml:"orders_per_side" mapstructure:"orders_per_side"`   // e.g., 5-7
	OrderSizeUSDT    float64 `yaml:"order_size_usdt" mapstructure:"order_size_usdt"`   // e.g., 3.0-5.0
	MinProfitPerFill float64 `yaml:"min_profit_per_fill" mapstructure:"min_profit_per_fill"` // e.g., 0.0002 = 2 bps
}

// DefaultMicroGridConfig returns default micro grid configuration
// Optimized for high-frequency, low-margin trading
func DefaultMicroGridConfig() *MicroGridConfig {
	return &MicroGridConfig{
		Enabled:          false, // Disabled by default, enable via config
		SpreadPct:        0.0005,  // 0.05% spread (ultra-tight)
		OrdersPerSide:    5,       // 5 orders per side
		OrderSizeUSDT:    3.0,     // $3 per order (reduced risk per fill)
		MinProfitPerFill: 0.0002,  // 2 bps minimum profit to cover fees
	}
}

// MicroGridCalculator calculates grid parameters for micro trading
type MicroGridCalculator struct {
	config *MicroGridConfig
}

// NewMicroGridCalculator creates a new micro grid calculator
func NewMicroGridCalculator(config *MicroGridConfig) *MicroGridCalculator {
	if config == nil {
		config = DefaultMicroGridConfig()
	}
	return &MicroGridCalculator{config: config}
}

// CalculateGridPrices returns buy and sell prices for all grid levels
func (m *MicroGridCalculator) CalculateGridPrices(currentPrice float64) (buyPrices []float64, sellPrices []float64) {
	if !m.config.Enabled {
		return nil, nil
	}

	spreadMultiplier := 1.0 + m.config.SpreadPct

	// Calculate buy prices (below current price)
	for i := 0; i < m.config.OrdersPerSide; i++ {
		level := float64(i + 1)
		buyPrice := currentPrice / (spreadMultiplier * level)
		if buyPrice > 0 {
			buyPrices = append(buyPrices, buyPrice)
		}
	}

	// Calculate sell prices (above current price)
	for i := 0; i < m.config.OrdersPerSide; i++ {
		level := float64(i + 1)
		sellPrice := currentPrice * (spreadMultiplier * level)
		if sellPrice > currentPrice {
			sellPrices = append(sellPrices, sellPrice)
		}
	}

	return buyPrices, sellPrices
}

// CalculateOrderSize returns the base order size in quote currency
func (m *MicroGridCalculator) CalculateOrderSize(price float64) float64 {
	if price <= 0 || m.config.OrderSizeUSDT <= 0 {
		return 0
	}
	return m.config.OrderSizeUSDT / price
}

// IsEnabled returns whether micro grid mode is enabled
func (m *MicroGridCalculator) IsEnabled() bool {
	return m.config != nil && m.config.Enabled
}

// GetConfig returns the current micro grid configuration
func (m *MicroGridCalculator) GetConfig() *MicroGridConfig {
	return m.config
}
