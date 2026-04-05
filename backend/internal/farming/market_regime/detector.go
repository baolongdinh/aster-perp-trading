package market_regime

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// RegimeDetector analyzes price history to determine market conditions
type RegimeDetector struct {
	priceHistory  map[string][]float64    // Symbol -> historical prices
	currentRegime map[string]MarketRegime // Symbol -> current regime
	confidence    map[string]float64      // Symbol -> detection confidence
	lastUpdate    map[string]time.Time    // Symbol -> last detection time
	mu            sync.RWMutex            // Thread safety
	logger        *zap.Logger             // Logger for regime transitions
}

// NewRegimeDetector creates a new regime detector instance
func NewRegimeDetector(logger interface{}) *RegimeDetector {
	return &RegimeDetector{
		priceHistory:  make(map[string][]float64),
		currentRegime: make(map[string]MarketRegime),
		confidence:    make(map[string]float64),
		lastUpdate:    make(map[string]time.Time),
		mu:            sync.RWMutex{},
	}
}

// DetectRegime analyzes current market conditions for a symbol
func (d *RegimeDetector) DetectRegime(symbol string, currentPrice float64) MarketRegime {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Add current price to history
	prices := d.priceHistory[symbol]
	prices = append(prices[1:], currentPrice) // Keep last N prices
	if len(prices) > 100 {
		prices = prices[len(prices)-100:] // Keep only last 100
	}
	d.priceHistory[symbol] = prices

	d.detectAndUpdateRegime(symbol)

	return RegimeUnknown
}

// detectAndUpdateRegime detects market regime and updates current state
func (d *RegimeDetector) detectAndUpdateRegime(symbol string) {
	// TODO: Implement hybrid detection algorithm
	// For now, return unknown until full implementation
	newRegime := RegimeUnknown

	// Update current regime if changed
	if currentRegime, exists := d.currentRegime[symbol]; exists && currentRegime != newRegime {
		d.currentRegime[symbol] = newRegime
		d.lastUpdate[symbol] = time.Now()

		// TODO: Log regime transition
		// d.logger.Info("Regime transition detected",
		//	zap.String("symbol", symbol),
		//	zap.String("from", string(currentRegime)),
		//	zap.String("to", string(newRegime)),
		//	zap.Time("timestamp", d.lastUpdate[symbol]))
	}
}

// GetCurrentRegime returns the current regime for a symbol
func (d *RegimeDetector) GetCurrentRegime(symbol string) MarketRegime {
	d.mu.RLock()
	defer d.mu.RUnlock()

	regime, exists := d.currentRegime[symbol]
	if !exists {
		regime = RegimeUnknown
	}

	return regime
}

// UpdatePriceHistory updates price history and triggers regime detection
func (d *RegimeDetector) UpdatePriceHistory(symbol string, newPrice float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.priceHistory[symbol] = append(d.priceHistory[symbol], newPrice)
	d.detectAndUpdateRegime(symbol)
	// TODO: Trigger regime detection update
	// d.detectAndUpdateRegime(symbol)
}
