package adaptive_grid

import (
	"math"
	"sync"

	"go.uber.org/zap"
)

// VolumeScalingConfig holds configuration for dynamic volume scaling
type VolumeScalingConfig struct {
	// Base notional size at center of range
	CenterNotional float64 // e.g., $150 at middle

	// Minimum notional size at edges
	EdgeNotional float64 // e.g., $20 at edges

	// Scaling curve type: "linear", "exponential", "sigmoid"
	CurveType string

	// Range division zones
	NumZones int // Số vùng chia từ center đến edge (mặc định 3)

	// ATR adjustment - giảm thêm khi volatility cao
	ATRAdjustmentEnabled bool
	ATRAdjustmentFactor  float64 // e.g., 0.5 = giảm 50% khi ATR cao
}

// DefaultVolumeScalingConfig returns default config
func DefaultVolumeScalingConfig() *VolumeScalingConfig {
	return &VolumeScalingConfig{
		CenterNotional:       150.0,         // $150 ở center
		EdgeNotional:         20.0,          // $20 ở edge
		CurveType:            "exponential", // Giảm nhanh ra biên
		NumZones:             3,             // 3 zones mỗi bên
		ATRAdjustmentEnabled: true,
		ATRAdjustmentFactor:  0.5,
	}
}

// ZonePosition represents position within the range
type ZonePosition int

const (
	ZoneCenter ZonePosition = iota
	ZoneInner
	ZoneMiddle
	ZoneOuter
	ZoneEdge
)

// VolumeScaler implements pyramid/tapered volume scaling
type VolumeScaler struct {
	config      *VolumeScalingConfig
	rangeConfig *RangeConfig
	logger      *zap.Logger
	mu          sync.RWMutex

	// Current range reference
	currentRange *RangeData
	atrValue     float64
}

// NewVolumeScaler creates new volume scaler
func NewVolumeScaler(config *VolumeScalingConfig, logger *zap.Logger) *VolumeScaler {
	if config == nil {
		config = DefaultVolumeScalingConfig()
	}
	return &VolumeScaler{
		config: config,
		logger: logger,
	}
}

// UpdateRange updates current range reference
func (v *VolumeScaler) UpdateRange(rangeData *RangeData) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.currentRange = rangeData
}

// UpdateATR updates current ATR for volatility adjustment
func (v *VolumeScaler) UpdateATR(atr float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.atrValue = atr
}

// CalculateOrderSize calculates size based on price position in range
func (v *VolumeScaler) CalculateOrderSize(price float64, isBuy bool) float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.currentRange == nil {
		// Fallback to center size if no range
		return v.config.CenterNotional
	}

	// Calculate position ratio within range (0 = lower edge, 1 = upper edge)
	ratio := v.calculatePositionRatio(price)

	// Calculate distance from center (0.5 is center)
	distanceFromCenter := math.Abs(ratio-0.5) * 2 // 0 at center, 1 at edge

	// Apply scaling curve
	scaledNotional := v.applyScalingCurve(distanceFromCenter)

	// Apply ATR adjustment if enabled
	if v.config.ATRAdjustmentEnabled && v.atrValue > 0 {
		atrPct := v.atrValue / price
		if atrPct > 0.01 { // ATR > 1%
			adjustment := math.Max(0.3, 1.0-(atrPct-0.01)*v.config.ATRAdjustmentFactor)
			scaledNotional *= adjustment
		}
	}

	// Ensure bounds
	if scaledNotional < v.config.EdgeNotional {
		scaledNotional = v.config.EdgeNotional
	}
	if scaledNotional > v.config.CenterNotional {
		scaledNotional = v.config.CenterNotional
	}

	v.logger.Debug("Volume scaled",
		zap.Float64("price", price),
		zap.Float64("ratio", ratio),
		zap.Float64("distance_from_center", distanceFromCenter),
		zap.Float64("notional", scaledNotional),
		zap.Bool("is_buy", isBuy))

	return scaledNotional
}

// calculatePositionRatio calculates where price is within range [0, 1]
func (v *VolumeScaler) calculatePositionRatio(price float64) float64 {
	rangeWidth := v.currentRange.UpperBound - v.currentRange.LowerBound
	if rangeWidth == 0 {
		return 0.5
	}

	ratio := (price - v.currentRange.LowerBound) / rangeWidth

	// Clamp to [0, 1]
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	return ratio
}

// applyScalingCurve applies the configured scaling curve
func (v *VolumeScaler) applyScalingCurve(distanceFromCenter float64) float64 {
	switch v.config.CurveType {
	case "linear":
		// Linear decay: size = center - (center - edge) * distance
		return v.config.CenterNotional - (v.config.CenterNotional-v.config.EdgeNotional)*distanceFromCenter

	case "exponential":
		// Exponential decay: giảm nhanh hơn ở biên
		// factor = e^(-k * distance), k = 2 để giảm nhanh
		factor := math.Exp(-2.0 * distanceFromCenter)
		return v.config.EdgeNotional + (v.config.CenterNotional-v.config.EdgeNotional)*factor

	case "sigmoid":
		// Sigmoid: giữ nguyên ở center, giảm nhanh ở giữa, chậm ở edge
		// S-shape curve
		x := (distanceFromCenter - 0.5) * 6 // Scale to sigmoid range
		sigmoid := 1 / (1 + math.Exp(x))
		return v.config.EdgeNotional + (v.config.CenterNotional-v.config.EdgeNotional)*sigmoid

	case "step":
		// Step function: giảm theo zone rõ ràng
		return v.applyStepScaling(distanceFromCenter)

	default:
		// Default linear
		return v.config.CenterNotional - (v.config.CenterNotional-v.config.EdgeNotional)*distanceFromCenter
	}
}

// applyStepScaling applies zone-based step scaling
func (v *VolumeScaler) applyStepScaling(distanceFromCenter float64) float64 {
	zones := float64(v.config.NumZones)
	zoneWidth := 1.0 / zones

	// Determine which zone we're in
	zone := int(distanceFromCenter / zoneWidth)
	if zone >= v.config.NumZones {
		zone = v.config.NumZones - 1
	}

	// Calculate size for this zone (linear interpolation between zones)
	zoneRatio := float64(zone) / float64(v.config.NumZones)
	return v.config.CenterNotional - (v.config.CenterNotional-v.config.EdgeNotional)*zoneRatio
}

// GetZoneInfo returns which zone a price is in (for logging/debugging)
func (v *VolumeScaler) GetZoneInfo(price float64) map[string]interface{} {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.currentRange == nil {
		return map[string]interface{}{"error": "no range set"}
	}

	ratio := v.calculatePositionRatio(price)
	distanceFromCenter := math.Abs(ratio-0.5) * 2

	// Determine zone
	zone := "center"
	if distanceFromCenter > 0.2 {
		zone = "inner"
	}
	if distanceFromCenter > 0.4 {
		zone = "middle"
	}
	if distanceFromCenter > 0.6 {
		zone = "outer"
	}
	if distanceFromCenter > 0.8 {
		zone = "edge"
	}

	return map[string]interface{}{
		"price":                price,
		"range_ratio":          ratio,
		"distance_from_center": distanceFromCenter,
		"zone":                 zone,
		"range_lower":          v.currentRange.LowerBound,
		"range_upper":          v.currentRange.UpperBound,
		"range_mid":            v.currentRange.MidPrice,
	}
}

// CalculateAllGridSizes calculates sizes for all grid levels
func (v *VolumeScaler) CalculateAllGridLevels(currentPrice float64, numLevels int, spreadPct float64) []GridLevelSize {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.currentRange == nil {
		return nil
	}

	levels := make([]GridLevelSize, 0, numLevels*2)

	// BUY levels (below current price)
	for i := 1; i <= numLevels; i++ {
		buyPrice := currentPrice * (1 - spreadPct*float64(i)/100)
		notional := v.CalculateOrderSize(buyPrice, true)

		// Convert notional to quantity
		qty := notional / buyPrice

		levels = append(levels, GridLevelSize{
			Level:    -i,
			Side:     "BUY",
			Price:    buyPrice,
			Notional: notional,
			Quantity: qty,
			Zone:     v.getZoneName(buyPrice),
		})
	}

	// SELL levels (above current price)
	for i := 1; i <= numLevels; i++ {
		sellPrice := currentPrice * (1 + spreadPct*float64(i)/100)
		notional := v.CalculateOrderSize(sellPrice, false)

		// Convert notional to quantity
		qty := notional / sellPrice

		levels = append(levels, GridLevelSize{
			Level:    i,
			Side:     "SELL",
			Price:    sellPrice,
			Notional: notional,
			Quantity: qty,
			Zone:     v.getZoneName(sellPrice),
		})
	}

	return levels
}

// getZoneName returns zone name for a price
func (v *VolumeScaler) getZoneName(price float64) string {
	ratio := v.calculatePositionRatio(price)
	distanceFromCenter := math.Abs(ratio-0.5) * 2

	if distanceFromCenter < 0.2 {
		return "CENTER"
	}
	if distanceFromCenter < 0.4 {
		return "INNER"
	}
	if distanceFromCenter < 0.6 {
		return "MIDDLE"
	}
	if distanceFromCenter < 0.8 {
		return "OUTER"
	}
	return "EDGE"
}

// GridLevelSize represents size for a specific grid level
type GridLevelSize struct {
	Level    int
	Side     string
	Price    float64
	Notional float64
	Quantity float64
	Zone     string
}

// GetTotalExposure calculates total exposure if all levels filled
func (v *VolumeScaler) GetTotalExposure(currentPrice float64, numLevels int, spreadPct float64) (buyExposure, sellExposure float64) {
	levels := v.CalculateAllGridLevels(currentPrice, numLevels, spreadPct)

	for _, level := range levels {
		if level.Side == "BUY" {
			buyExposure += level.Notional
		} else {
			sellExposure += level.Notional
		}
	}

	return buyExposure, sellExposure
}

// ValidateConfig checks if config is valid
func (v *VolumeScaler) ValidateConfig() bool {
	if v.config.CenterNotional <= v.config.EdgeNotional {
		v.logger.Error("CenterNotional must be greater than EdgeNotional")
		return false
	}
	if v.config.CenterNotional <= 0 || v.config.EdgeNotional <= 0 {
		v.logger.Error("Notional values must be positive")
		return false
	}
	if v.config.NumZones < 1 {
		v.logger.Error("NumZones must be at least 1")
		return false
	}
	return true
}
