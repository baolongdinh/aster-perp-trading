package adaptive_grid

import (
	"fmt"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SkewAction represents the action to take based on inventory skew
type SkewAction int

const (
	SkewActionNormal SkewAction = iota
	SkewActionReduceSkewSide
	SkewActionPauseSkewSide
	SkewActionEmergencySkew
)

func (s SkewAction) String() string {
	switch s {
	case SkewActionNormal:
		return "NORMAL"
	case SkewActionReduceSkewSide:
		return "REDUCE_SKEW_SIDE"
	case SkewActionPauseSkewSide:
		return "PAUSE_SKEW_SIDE"
	case SkewActionEmergencySkew:
		return "EMERGENCY_SKEW"
	default:
		return "UNKNOWN"
	}
}

// PositionInfo tracks information about a position
type PositionInfo struct {
	Symbol        string
	Side          string  // "LONG" or "SHORT"
	Size          float64 // Quantity
	EntryPrice    float64
	CurrentPrice  float64
	NotionalValue float64
	UnrealizedPnL float64
	EntryTime     time.Time
	GridLevel     int
}

// InventoryManager manages inventory skew and position tracking
type InventoryManager struct {
	positions       map[string][]PositionInfo // symbol -> positions
	netExposure     map[string]float64        // symbol -> net exposure (positive = long, negative = short)
	maxInventoryPct float64                   // Max inventory as % of equity
	equity          float64
	config          *InventoryConfig // Full config reference
	logger          *zap.Logger
	mu              sync.RWMutex
}

// SkewThreshold holds configuration for a skew level
type SkewThreshold struct {
	Threshold           float64 `yaml:"threshold"`
	SizeReduction       float64 `yaml:"size_reduction"`
	PauseSide           bool    `yaml:"pause_side"`
	TakeProfitReduction float64 `yaml:"take_profit_reduction,omitempty"`
}

// InventoryConfig holds configuration for inventory management
type InventoryConfig struct {
	MaxInventoryPct   float64       `yaml:"max_inventory_pct"` // Default 0.30 (30%)
	LowThreshold      SkewThreshold `yaml:"low_threshold"`
	ModerateThreshold SkewThreshold `yaml:"moderate_threshold"`
	HighThreshold     SkewThreshold `yaml:"high_threshold"`
	CriticalThreshold SkewThreshold `yaml:"critical_threshold"`
}

// DefaultInventoryConfig returns default configuration
func DefaultInventoryConfig() *InventoryConfig {
	return &InventoryConfig{
		MaxInventoryPct: 0.30,
		LowThreshold: SkewThreshold{
			Threshold:     0.40,
			SizeReduction: 0.0,
			PauseSide:     false,
		},
		ModerateThreshold: SkewThreshold{
			Threshold:     0.80,
			SizeReduction: 0.20,
			PauseSide:     false,
		},
		HighThreshold: SkewThreshold{
			Threshold:           1.0,
			SizeReduction:       0.30,
			PauseSide:           false,
			TakeProfitReduction: 0.20,
		},
		CriticalThreshold: SkewThreshold{
			Threshold:           1.2,
			SizeReduction:       0.50,
			PauseSide:           true,
			TakeProfitReduction: 0.30,
		},
	}
}

// NewInventoryManager creates new inventory manager
func NewInventoryManager(config *InventoryConfig, logger *zap.Logger) *InventoryManager {
	if config == nil {
		config = DefaultInventoryConfig()
	}

	return &InventoryManager{
		positions:       make(map[string][]PositionInfo),
		netExposure:     make(map[string]float64),
		maxInventoryPct: config.MaxInventoryPct,
		config:          config,
		logger:          logger,
	}
}

// TrackPosition tracks a new position
func (im *InventoryManager) TrackPosition(symbol, side string, size, entryPrice float64, gridLevel int) {
	im.mu.Lock()
	defer im.mu.Unlock()

	position := PositionInfo{
		Symbol:        symbol,
		Side:          side,
		Size:          size,
		EntryPrice:    entryPrice,
		CurrentPrice:  entryPrice,
		NotionalValue: size * entryPrice,
		EntryTime:     time.Now(),
		GridLevel:     gridLevel,
	}

	if _, exists := im.positions[symbol]; !exists {
		im.positions[symbol] = make([]PositionInfo, 0)
	}
	im.positions[symbol] = append(im.positions[symbol], position)

	// Update net exposure
	if side == "LONG" {
		im.netExposure[symbol] += size * entryPrice
	} else {
		im.netExposure[symbol] -= size * entryPrice
	}

	im.logger.Debug("Position tracked",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.Float64("size", size),
		zap.Int("grid_level", gridLevel))
}

// UpdatePositionPrice updates current price for all positions of a symbol
func (im *InventoryManager) UpdatePositionPrice(symbol string, currentPrice float64) {
	im.mu.Lock()
	defer im.mu.Unlock()

	positions, exists := im.positions[symbol]
	if !exists {
		return
	}

	for i := range positions {
		positions[i].CurrentPrice = currentPrice
		notional := positions[i].Size * currentPrice
		positions[i].NotionalValue = notional

		// Calculate unrealized PnL
		if positions[i].Side == "LONG" {
			positions[i].UnrealizedPnL = (currentPrice - positions[i].EntryPrice) * positions[i].Size
		} else {
			positions[i].UnrealizedPnL = (positions[i].EntryPrice - currentPrice) * positions[i].Size
		}
	}
}

// GetNetExposure returns net exposure for a symbol (positive = long, negative = short)
func (im *InventoryManager) GetNetExposure(symbol string) float64 {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return im.netExposure[symbol]
}

// GetNetExposureNotional returns net exposure in notional value
func (im *InventoryManager) GetNetExposureNotional(symbol string) float64 {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return im.netExposure[symbol]
}

// CalculateSkewRatio calculates the skew ratio (0.0 to 1.0+)
func (im *InventoryManager) CalculateSkewRatio(symbol string) float64 {
	im.mu.RLock()
	defer im.mu.RUnlock()

	if im.equity == 0 {
		return 0
	}

	netExposure := math.Abs(im.netExposure[symbol])
	maxInventory := im.equity * im.maxInventoryPct

	if maxInventory == 0 {
		return 0
	}

	return netExposure / maxInventory
}

// GetSkewAction determines the action to take based on skew ratio
func (im *InventoryManager) GetSkewAction(skewRatio float64) SkewAction {
	// Use config thresholds if available, otherwise use defaults
	var lowThreshold, moderateThreshold, highThreshold float64 = 0.40, 0.80, 1.0
	if im.config != nil {
		lowThreshold = im.config.LowThreshold.Threshold
		moderateThreshold = im.config.ModerateThreshold.Threshold
		highThreshold = im.config.HighThreshold.Threshold
	}

	switch {
	case skewRatio < lowThreshold:
		return SkewActionNormal
	case skewRatio < moderateThreshold:
		return SkewActionReduceSkewSide
	case skewRatio < highThreshold:
		return SkewActionPauseSkewSide
	default:
		return SkewActionEmergencySkew
	}
}

// GetAdjustedOrderSize returns size adjusted for skew
func (im *InventoryManager) GetAdjustedOrderSize(symbol string, side string, baseSize float64) float64 {
	im.mu.RLock()
	defer im.mu.RUnlock()

	skewRatio := im.CalculateSkewRatio(symbol)
	action := im.GetSkewAction(skewRatio)

	// Determine if this is the skewed side
	netExposure := im.netExposure[symbol]
	isSkewedSide := (netExposure > 0 && side == "LONG") || (netExposure < 0 && side == "SHORT")

	var reduction float64
	switch action {
	case SkewActionNormal:
		return baseSize
	case SkewActionReduceSkewSide:
		if isSkewedSide {
			reduction = 0.30 // Reduce by 30%
		}
	case SkewActionPauseSkewSide:
		if isSkewedSide {
			reduction = 0.50 // Reduce by 50%
		}
	case SkewActionEmergencySkew:
		if isSkewedSide {
			reduction = 0.90 // Giảm 90% thay vì block hoàn toàn - vẫn có lệnh để farm volume
		}
	}

	return baseSize * (1 - reduction)
}

// ShouldPauseSide returns true if side should be paused
func (im *InventoryManager) ShouldPauseSide(symbol string, side string) bool {
	im.mu.RLock()
	defer im.mu.RUnlock()

	skewRatio := im.CalculateSkewRatio(symbol)
	action := im.GetSkewAction(skewRatio)

	// Volume farming: Không pause hoàn toàn dù skew cao - vẫn đặt lệnh với size giảm
	// Chỉ return true cho EMERGENCY_SKEW và cần force stop
	if action != SkewActionEmergencySkew {
		return false
	}

	// Ngay cả với EmergencySkew, vẫn cho phép đặt lệnh nhỏ để farm volume
	// Trả về false để không block, size sẽ được giảm 90% trong GetAdjustedOrderSize
	return false
}

// SetBias sets a funding rate bias for a symbol
// side: "LONG" or "SHORT" - the side to favor
// strength: 0.0-1.0 - how much to reduce the opposite side (e.g., 0.7 = 70% reduction)
type FundingBias struct {
	Side     string
	Strength float64 // 0.0-1.0
	Expires  time.Time
}

var fundingBiases = make(map[string]*FundingBias)
var fundingBiasMu sync.RWMutex

// SetBias sets funding rate bias for a symbol
func (im *InventoryManager) SetBias(symbol string, side string, strength float64) {
	fundingBiasMu.Lock()
	defer fundingBiasMu.Unlock()

	fundingBiases[symbol] = &FundingBias{
		Side:     side,
		Strength: strength,
		Expires:  time.Now().Add(8 * time.Hour), // Funding period is typically 8 hours
	}

	im.logger.Info("Funding bias set",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.Float64("strength", strength))
}

// GetFundingBias returns current funding bias for symbol
func (im *InventoryManager) GetFundingBias(symbol string) (side string, strength float64, active bool) {
	fundingBiasMu.RLock()
	defer fundingBiasMu.RUnlock()

	bias, exists := fundingBiases[symbol]
	if !exists {
		return "", 0, false
	}

	// Check if expired
	if time.Now().After(bias.Expires) {
		return "", 0, false
	}

	return bias.Side, bias.Strength, true
}

// ClearFundingBias clears the funding bias for a symbol
func (im *InventoryManager) ClearFundingBias(symbol string) {
	fundingBiasMu.Lock()
	defer fundingBiasMu.Unlock()
	delete(fundingBiases, symbol)
}

// GetAdjustedOrderSizeWithFunding considers both skew and funding bias
func (im *InventoryManager) GetAdjustedOrderSizeWithFunding(symbol string, side string, baseSize float64) float64 {
	// First apply inventory skew adjustment
	size := im.GetAdjustedOrderSize(symbol, side, baseSize)

	// Then apply funding bias
	if biasSide, strength, active := im.GetFundingBias(symbol); active {
		// If this side is against the bias, reduce size
		if (biasSide == "LONG" && side == "SHORT") || (biasSide == "SHORT" && side == "LONG") {
			reduction := strength
			size = size * (1 - reduction)
			im.logger.Debug("Size reduced due to funding bias",
				zap.String("symbol", symbol),
				zap.String("side", side),
				zap.Float64("reduction", reduction),
				zap.Float64("new_size", size))
		}
	}

	return size
}

// GetAdjustedTakeProfitDistance returns adjusted take-profit distance
func (im *InventoryManager) GetAdjustedTakeProfitDistance(symbol string, side string, baseDistance float64) float64 {
	skewRatio := im.CalculateSkewRatio(symbol)

	var reduction float64
	switch {
	case skewRatio < 0.5:
		return baseDistance
	case skewRatio < 0.7:
		reduction = 0.15
	case skewRatio < 0.9:
		reduction = 0.30
	default:
		reduction = 0.50
	}

	return baseDistance * (1 - reduction)
}

// GetFurthestPositions returns the furthest underwater positions
func (im *InventoryManager) GetFurthestPositions(symbol string, count int) []PositionInfo {
	im.mu.RLock()
	defer im.mu.RUnlock()

	positions, exists := im.positions[symbol]
	if !exists || len(positions) == 0 {
		return nil
	}

	// Sort by unrealized PnL (worst first)
	sorted := make([]PositionInfo, len(positions))
	copy(sorted, positions)

	// Simple bubble sort by PnL (worst first)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].UnrealizedPnL < sorted[i].UnrealizedPnL {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Return count worst positions
	if count > len(sorted) {
		count = len(sorted)
	}
	return sorted[:count]
}

// ClosePosition removes a position from tracking
func (im *InventoryManager) ClosePosition(symbol string, gridLevel int) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	positions, exists := im.positions[symbol]
	if !exists {
		return fmt.Errorf("no positions for symbol %s", symbol)
	}

	// Find and remove position
	for i, pos := range positions {
		if pos.GridLevel == gridLevel {
			// Update net exposure
			if pos.Side == "LONG" {
				im.netExposure[symbol] -= pos.NotionalValue
			} else {
				im.netExposure[symbol] += pos.NotionalValue
			}

			// Remove from slice
			im.positions[symbol] = append(positions[:i], positions[i+1:]...)

			im.logger.Debug("Position closed",
				zap.String("symbol", symbol),
				zap.Int("grid_level", gridLevel))
			return nil
		}
	}

	return fmt.Errorf("position not found for grid level %d", gridLevel)
}

// SetEquity updates current account equity
func (im *InventoryManager) SetEquity(equity float64) {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.equity = equity
}

// GetStatus returns current inventory status for a symbol
func (im *InventoryManager) GetStatus(symbol string) map[string]interface{} {
	im.mu.RLock()
	defer im.mu.RUnlock()

	skewRatio := im.CalculateSkewRatio(symbol)
	action := im.GetSkewAction(skewRatio)
	netExposure := im.netExposure[symbol]

	result := map[string]interface{}{
		"symbol":            symbol,
		"net_exposure":      netExposure,
		"skew_ratio":        skewRatio,
		"skew_action":       action.String(),
		"position_count":    len(im.positions[symbol]),
		"equity":            im.equity,
		"max_inventory_pct": im.maxInventoryPct,
	}

	// Log inventory metrics for dashboard
	im.logger.Info("Inventory Metrics",
		zap.String("symbol", symbol),
		zap.Float64("net_exposure", netExposure),
		zap.Float64("skew_ratio", skewRatio),
		zap.String("skew_action", action.String()),
		zap.Int("position_count", len(im.positions[symbol])),
		zap.Float64("equity", im.equity))

	return result
}

// Reset clears all data
func (im *InventoryManager) Reset() {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.positions = make(map[string][]PositionInfo)
	im.netExposure = make(map[string]float64)
}
