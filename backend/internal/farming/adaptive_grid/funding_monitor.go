package adaptive_grid

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// FundingRateInfo holds funding rate information
type FundingRateInfo struct {
	Symbol      string
	Rate        float64
	NextFunding time.Time
	LastUpdate  time.Time
}

// FundingRateMonitor monitors and manages funding rate exposure
type FundingRateMonitor struct {
	rates          map[string]*FundingRateInfo
	highThreshold  float64
	checkInterval  time.Duration
	client         FundingRateClient
	logger         *zap.Logger
	mu             sync.RWMutex
	lastCheck      time.Time
}

// FundingRateClient interface for fetching funding rates
type FundingRateClient interface {
	GetFundingRate(ctx context.Context, symbol string) (*FundingRateInfo, error)
}

// FundingProtectionConfig holds configuration
type FundingProtectionConfig struct {
	HighThreshold  float64       `yaml:"high_threshold"`  // 0.03%
	CheckInterval  time.Duration `yaml:"check_interval"` // 4 hours
	LevelAdjustment int          `yaml:"level_adjustment"` // 2 levels
}

// DefaultFundingProtectionConfig returns default configuration
func DefaultFundingProtectionConfig() *FundingProtectionConfig {
	return &FundingProtectionConfig{
		HighThreshold:   0.0003, // 0.03%
		CheckInterval:   4 * time.Hour,
		LevelAdjustment: 2,
	}
}

// NewFundingRateMonitor creates new funding rate monitor
func NewFundingRateMonitor(config *FundingProtectionConfig, client FundingRateClient, logger *zap.Logger) *FundingRateMonitor {
	if config == nil {
		config = DefaultFundingProtectionConfig()
	}

	return &FundingRateMonitor{
		rates:         make(map[string]*FundingRateInfo),
		highThreshold: config.HighThreshold,
		checkInterval: config.CheckInterval,
		client:        client,
		logger:        logger,
		lastCheck:     time.Time{},
	}
}

// UpdateFundingRate updates funding rate for a symbol
func (fm *FundingRateMonitor) UpdateFundingRate(symbol string, rate float64, nextFunding time.Time) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.rates[symbol] = &FundingRateInfo{
		Symbol:      symbol,
		Rate:        rate,
		NextFunding: nextFunding,
		LastUpdate:  time.Now(),
	}

	fm.logger.Debug("Funding rate updated",
		zap.String("symbol", symbol),
		zap.Float64("rate", rate*100),
		zap.Time("next_funding", nextFunding))
}

// GetFundingRate returns current funding rate for symbol
func (fm *FundingRateMonitor) GetFundingRate(symbol string) *FundingRateInfo {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.rates[symbol]
}

// IsHighFunding returns true if funding rate is high (positive or negative)
func (fm *FundingRateMonitor) IsHighFunding(symbol string) bool {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	info, exists := fm.rates[symbol]
	if !exists {
		return false
	}

	return abs(info.Rate) > fm.highThreshold
}

// GetLevelAdjustment returns adjustment for grid levels based on funding
func (fm *FundingRateMonitor) GetLevelAdjustment(symbol string, netLong bool) (reduceLong, reduceShort int) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	info, exists := fm.rates[symbol]
	if !exists {
		return 0, 0
	}

	// If funding is high positive - longs pay shorts
	if info.Rate > fm.highThreshold {
		if netLong {
			// Reduce long exposure if we're long
			return fm.getConfig().LevelAdjustment, 0
		}
		// Add to shorts if we're short
		return 0, -1 // Add short level
	}

	// If funding is high negative - shorts pay longs
	if info.Rate < -fm.highThreshold {
		if !netLong {
			// Reduce short exposure if we're short
			return 0, fm.getConfig().LevelAdjustment
		}
		// Add to longs if we're long
		return -1, 0 // Add long level
	}

	return 0, 0
}

// CalculateFundingCost calculates cost of holding position through funding
func (fm *FundingRateMonitor) CalculateFundingCost(symbol string, positionNotional float64, hours float64) float64 {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	info, exists := fm.rates[symbol]
	if !exists {
		return 0
	}

	// Funding happens every 8 hours typically
	fundingPeriods := hours / 8.0
	return positionNotional * info.Rate * fundingPeriods
}

// CheckAndUpdate performs periodic check and update of funding rates
func (fm *FundingRateMonitor) CheckAndUpdate(ctx context.Context) error {
	// Check if enough time has passed
	if time.Since(fm.lastCheck) < fm.checkInterval {
		return nil
	}

	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Update all tracked symbols
	for symbol := range fm.rates {
		if fm.client != nil {
			info, err := fm.client.GetFundingRate(ctx, symbol)
			if err != nil {
				fm.logger.Warn("Failed to fetch funding rate",
					zap.String("symbol", symbol),
					zap.Error(err))
				continue
			}
			
			fm.rates[symbol] = info
			
			// Log if funding is high
			if abs(info.Rate) > fm.highThreshold {
				fm.logger.Warn("High funding rate detected",
					zap.String("symbol", symbol),
					zap.Float64("rate", info.Rate*100),
					zap.Float64("threshold", fm.highThreshold*100))
			}
		}
	}

	fm.lastCheck = time.Now()
	return nil
}

// GetStatus returns status for all tracked symbols
func (fm *FundingRateMonitor) GetStatus() map[string]interface{} {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	rates := make(map[string]interface{})
	for symbol, info := range fm.rates {
		rates[symbol] = map[string]interface{}{
			"rate":         info.Rate * 100,
			"next_funding": info.NextFunding,
			"last_update":  info.LastUpdate,
			"is_high":      abs(info.Rate) > fm.highThreshold,
		}
	}

	return map[string]interface{}{
		"rates":        rates,
		"high_threshold": fm.highThreshold * 100,
		"last_check":   fm.lastCheck,
	}
}

// Reset clears all funding rate data
func (fm *FundingRateMonitor) Reset() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.rates = make(map[string]*FundingRateInfo)
	fm.lastCheck = time.Time{}
}

// getConfig returns current config (placeholder for hot reload)
func (fm *FundingRateMonitor) getConfig() *FundingProtectionConfig {
	return DefaultFundingProtectionConfig()
}

// abs returns absolute value
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
