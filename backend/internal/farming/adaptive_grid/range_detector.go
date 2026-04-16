package adaptive_grid

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RangeState represents the current state of price range
type RangeState int

const (
	RangeStateUnknown      RangeState = iota
	RangeStateEstablishing            // Đang xác lập range
	RangeStateActive                  // Range đã active, đặt grid
	RangeStateBreakout                // Breakout khỏi range, out tất cả
	RangeStateStabilizing             // Chờ giá ổn định lại
)

// RangeConfig holds configuration for range detection
type RangeConfig struct {
	Method                   string        // "bollinger", "atr", hoặc "combined"
	Periods                  int           // Số period cho calculation (20 cho BB, 14 cho ATR)
	BBMultiplier             float64       // Bollinger Bands multiplier (thường 2.0)
	ATRMultiplier            float64       // ATR multiplier cho range
	BreakoutThreshold        float64       // % vượt khỏi range để coi là breakout (e.g., 0.01 = 1%)
	StabilizationPeriod      time.Duration // Thời gian chờ ổn định sau breakout
	MinRangeWidthPct         float64       // Minimum range width as % of price (e.g., 0.005 = 0.5%)
	ADXPeriod                int           // Period for ADX confirmation
	MaterialShiftPct         float64       // Min center shift required before re-entry
	WidthChangePct           float64       // Min width change required before re-entry
	ReentryConfirmations     int           // Consecutive confirmations before range becomes active again
	EntryConfirmations       int           // Consecutive confirmations before initial entry
	OutsideBandConfirmations int           // Consecutive closes outside the bands before breakout
	BBExpansionFactor        float64       // Width expansion factor versus rolling average
}

// EnhancedRangeConfig extends RangeConfig with ADX and fast detection support
type EnhancedRangeConfig struct {
	RangeConfig `yaml:",inline" mapstructure:",squash"`

	// Fast detection settings (Phase 2 optimization)
	BBPeriod         int     `yaml:"bb_period" mapstructure:"bb_period"`                 // 10 for fast, 20 for standard
	ADXPeriod        int     `yaml:"adx_period" mapstructure:"adx_period"`               // 14 periods for ADX
	SidewaysADXMax   float64 `yaml:"sideways_adx_max" mapstructure:"sideways_adx_max"`   // Max ADX for sideways (20.0)
	StabilizationMin int     `yaml:"stabilization_min" mapstructure:"stabilization_min"` // Min periods after breakout

	// ADX tracking
	EnableADXFilter bool    `yaml:"enable_adx_filter" mapstructure:"enable_adx_filter"` // Enable ADX < 20 filter
	CurrentADX      float64 `yaml:"-" mapstructure:"-"`                                 // Current ADX value (runtime)
}

// FastRangeConfig returns optimized config for fast range detection (5 periods)
func FastRangeConfig() *EnhancedRangeConfig {
	return &EnhancedRangeConfig{
		RangeConfig: RangeConfig{
			Method:                   "combined",
			Periods:                  5, // Ultra-fast: 5 periods instead of 10
			BBMultiplier:             2.0,
			ATRMultiplier:            1.5,
			BreakoutThreshold:        0.01,
			StabilizationPeriod:      10 * time.Second, // Ultra-fast: 10s instead of 30s
			MinRangeWidthPct:         0.0001,           // Giảm để dễ tạo range hơn
			MaterialShiftPct:         0.005,
			WidthChangePct:           0.0015,
			ReentryConfirmations:     2, // Giảm từ 3 xuống 2
			EntryConfirmations:       1,
			OutsideBandConfirmations: 2,
			BBExpansionFactor:        1.5,
		},
		BBPeriod:         5, // Giảm từ 10 xuống 5
		ADXPeriod:        7, // Giảm từ 14 xuống 7
		SidewaysADXMax:   20.0,
		StabilizationMin: 2,     // Giảm từ 3 xuống 2
		EnableADXFilter:  false, // Disabled by default
	}
}

// DefaultRangeConfig returns default range configuration
func DefaultRangeConfig() *RangeConfig {
	return &RangeConfig{
		Method:                   "combined",       // Dùng cả BB và ATR
		Periods:                  5,                // 5 periods (fast warm-up: 5 minutes for 1m klines)
		BBMultiplier:             2.0,              // 2 sigma cho BB
		ATRMultiplier:            1.5,              // 1.5x ATR cho range
		BreakoutThreshold:        0.02,             // 2% vượt range = breakout (tăng từ 1% để ít nhạy cảm hơn)
		StabilizationPeriod:      10 * time.Second, // Chờ 10s sau breakout để resume siêu nhanh
		MinRangeWidthPct:         0.0001,           // Tối thiểu 0.01% range width (giảm để dễ tạo range hơn)
		ADXPeriod:                14,
		MaterialShiftPct:         0.005,
		WidthChangePct:           0.0015,
		ReentryConfirmations:     2, // Giảm từ 3 xuống 2
		EntryConfirmations:       1, // Giữ nguyên
		OutsideBandConfirmations: 3, // Tăng từ 2 lên 3 để ít nhạy cảm hơn
		BBExpansionFactor:        1.5,
	}
}

// RangeData holds the calculated price range
type RangeData struct {
	UpperBound    float64   // Giá trần của range
	LowerBound    float64   // Giá sàn của range
	MidPrice      float64   // Giá giữa range
	Width         float64   // Độ rộng range
	WidthPct      float64   // Độ rộng % so với giá
	EstablishedAt time.Time // Thời điểm range được xác lập
	ExpiresAt     time.Time // Thời điểm range hết hạn (recalculate)
	ATR           float64   // Current ATR value
	Volatility    float64   // Đo lường volatility
}

// IsPriceInRange checks if price is within the range
func (r *RangeData) IsPriceInRange(price float64) bool {
	return price >= r.LowerBound && price <= r.UpperBound
}

// IsBreakout checks if price has broken out of range
func (r *RangeData) IsBreakout(price float64, threshold float64) bool {
	// Breakout trên
	if price > r.UpperBound*(1+threshold) {
		return true
	}
	// Breakout dưới
	if price < r.LowerBound*(1-threshold) {
		return true
	}
	return false
}

// RangeDetector detects and manages price ranges
type RangeDetector struct {
	config *RangeConfig
	logger *zap.Logger
	mu     sync.RWMutex

	// Price history
	prices     []float64
	highs      []float64
	lows       []float64
	maxHistory int

	// Current state
	currentRange       *RangeData
	lastAcceptedRange  *RangeData
	state              RangeState
	breakoutTime       time.Time
	lastPrice          float64
	stabilizationStart time.Time
	reentryCount       int
	entryCount         int
	outsideBandCount   int
	widthHistory       []float64

	// Grid parameters derived from range
	gridSpreadPct float64
	gridLevels    int

	// NEW: ADX tracking for sideways detection
	currentADX      float64   // Current ADX value
	adxHistory      []float64 // ADX history for smoothing
	enableADXFilter bool      // Enable ADX < 20 filter
	sidewaysADXMax  float64   // Max ADX for sideways (default 20.0)
}

// NewRangeDetector creates new range detector
func NewRangeDetector(config *RangeConfig, logger *zap.Logger) *RangeDetector {
	if config == nil {
		config = DefaultRangeConfig()
	}

	return &RangeDetector{
		config:     config,
		logger:     logger,
		prices:     make([]float64, 0, config.Periods*2),
		highs:      make([]float64, 0, config.Periods*2),
		lows:       make([]float64, 0, config.Periods*2),
		maxHistory: config.Periods * 2,
		state:      RangeStateUnknown,
	}
}

// AddPrice adds new price data
func (r *RangeDetector) AddPrice(high, low, close float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.highs = append(r.highs, high)
	r.lows = append(r.lows, low)
	r.prices = append(r.prices, close)
	r.lastPrice = close

	// Keep history within limit
	if len(r.prices) > r.maxHistory {
		r.highs = r.highs[1:]
		r.lows = r.lows[1:]
		r.prices = r.prices[1:]
	}

	// Auto-update range if enough data
	if len(r.prices) >= r.config.Periods {
		r.updateADXLocked()
		r.updateRange()
		r.checkStateTransition()
	}
}

// updateRange calculates new range based on price history
func (r *RangeDetector) updateRange() {
	var upper, lower, mid, atr, volatility float64

	currentPrice := r.prices[len(r.prices)-1]

	switch r.config.Method {
	case "bollinger":
		upper, lower, mid = r.calculateBollingerBands()
	case "atr":
		atr = r.calculateATR()
		upper = currentPrice + atr*r.config.ATRMultiplier
		lower = currentPrice - atr*r.config.ATRMultiplier
		mid = currentPrice
	case "combined":
		// Dùng cả BB và ATR, chọn cái rộng hơn để an toàn
		bbUpper, bbLower, _ := r.calculateBollingerBands()
		atr = r.calculateATR()
		atrUpper := currentPrice + atr*r.config.ATRMultiplier
		atrLower := currentPrice - atr*r.config.ATRMultiplier

		// Chọn range rộng hơn
		upper = math.Max(bbUpper, atrUpper)
		lower = math.Min(bbLower, atrLower)
		mid = (upper + lower) / 2
	}

	width := upper - lower
	widthPct := width / currentPrice
	volatility = r.calculateVolatility()

	// Check if range is valid (đủ rộng)
	if widthPct < r.config.MinRangeWidthPct {
		r.logger.Info("Range too narrow, skipping",
			zap.Float64("width_pct", widthPct*100),
			zap.Float64("min_required", r.config.MinRangeWidthPct*100))
		return
	}

	r.logger.Info("Range calculated",
		zap.Float64("upper", upper),
		zap.Float64("lower", lower),
		zap.Float64("width_pct", widthPct*100))

	// Create new range
	r.currentRange = &RangeData{
		UpperBound:    upper,
		LowerBound:    lower,
		MidPrice:      mid,
		Width:         width,
		WidthPct:      widthPct,
		EstablishedAt: time.Now(),
		ExpiresAt:     time.Now().Add(time.Duration(r.config.Periods) * time.Minute),
		ATR:           atr,
		Volatility:    volatility,
	}

	r.widthHistory = append(r.widthHistory, widthPct)
	if len(r.widthHistory) > maxInt(r.config.Periods, 5) {
		r.widthHistory = r.widthHistory[1:]
	}

	// Calculate grid parameters
	r.calculateGridParameters()
}

// calculateBollingerBands calculates Bollinger Bands
func (r *RangeDetector) calculateBollingerBands() (upper, lower, mid float64) {
	period := r.config.Periods
	if len(r.prices) < period {
		return 0, 0, 0
	}

	// Calculate SMA (mid)
	recentPrices := r.prices[len(r.prices)-period:]
	sum := 0.0
	for _, p := range recentPrices {
		sum += p
	}
	mid = sum / float64(period)

	// Calculate standard deviation
	variance := 0.0
	for _, p := range recentPrices {
		diff := p - mid
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	// Calculate bands
	upper = mid + stdDev*r.config.BBMultiplier
	lower = mid - stdDev*r.config.BBMultiplier

	return upper, lower, mid
}

// calculateATR calculates Average True Range
func (r *RangeDetector) calculateATR() float64 {
	period := r.config.Periods
	if len(r.prices) < period+1 {
		return 0
	}

	trSum := 0.0
	startIdx := len(r.prices) - period

	for i := startIdx; i < len(r.prices); i++ {
		// True Range = max(high-low, |high-close_prev|, |low-close_prev|)
		highLow := r.highs[i] - r.lows[i]
		highClose := math.Abs(r.highs[i] - r.prices[i-1])
		lowClose := math.Abs(r.lows[i] - r.prices[i-1])

		tr := highLow
		if highClose > tr {
			tr = highClose
		}
		if lowClose > tr {
			tr = lowClose
		}
		trSum += tr
	}

	return trSum / float64(period)
}

// calculateVolatility calculates current volatility
func (r *RangeDetector) calculateVolatility() float64 {
	if len(r.prices) < 2 {
		return 0
	}

	// Tính volatility dựa trên % change giữa các periods
	sumChange := 0.0
	for i := 1; i < len(r.prices); i++ {
		change := math.Abs(r.prices[i]-r.prices[i-1]) / r.prices[i-1]
		sumChange += change
	}

	return sumChange / float64(len(r.prices)-1)
}

// calculateGridParameters calculates optimal grid parameters from range
func (r *RangeDetector) calculateGridParameters() {
	if r.currentRange == nil {
		return
	}

	// Grid spread = range width / số levels
	// Ví dụ: range 2% → chia 4 levels mỗi bên = 0.5% spread
	r.gridLevels = 3 // Mỗi bên 3 levels
	r.gridSpreadPct = r.currentRange.WidthPct / float64(r.gridLevels) * 100

	// Giới hạn spread tối thiểu và tối đa
	if r.gridSpreadPct < 0.05 {
		r.gridSpreadPct = 0.05 // Tối thiểu 0.05%
	}
	if r.gridSpreadPct > 0.5 {
		r.gridSpreadPct = 0.5 // Tối đa 0.5%
	}

	r.logger.Info("Grid parameters calculated",
		zap.Float64("range_width_pct", r.currentRange.WidthPct*100),
		zap.Float64("grid_spread_pct", r.gridSpreadPct),
		zap.Int("levels", r.gridLevels))
}

// checkStateTransition handles state machine transitions
func (r *RangeDetector) checkStateTransition() {
	if r.currentRange == nil {
		if r.state != RangeStateEstablishing {
			r.state = RangeStateEstablishing
			r.logger.Info("State: Establishing range")
		}
		return
	}

	switch r.state {
	case RangeStateUnknown, RangeStateEstablishing:
		// Chuyển sang Active khi có range hợp lệ và sideway đã được xác nhận.
		if r.currentRange == nil {
			r.logger.Debug("Cannot activate: currentRange is nil")
			return
		}
		if !r.currentRange.IsPriceInRange(r.lastPrice) {
			r.logger.Debug("Cannot activate: price not in range",
				zap.Float64("price", r.lastPrice),
				zap.Float64("upper", r.currentRange.UpperBound),
				zap.Float64("lower", r.currentRange.LowerBound))
			r.entryCount = 0
			return
		}
		if r.enableADXFilter && !r.isSidewaysConfirmedLocked() {
			r.entryCount = 0
			r.reentryCount = 0
			return
		}
		r.entryCount++
		if r.entryCount < maxInt(1, r.config.EntryConfirmations) {
			return
		}
		r.state = RangeStateActive
		r.lastAcceptedRange = cloneRangeData(r.currentRange)
		r.reentryCount = 0
		r.entryCount = 0
		r.outsideBandCount = 0
		r.logger.Info("State: Range Active - Ready for grid",
			zap.Float64("upper", r.currentRange.UpperBound),
			zap.Float64("lower", r.currentRange.LowerBound),
			zap.Float64("width_pct", r.currentRange.WidthPct*100))

	case RangeStateActive:
		// Kiểm tra breakout
		if !r.currentRange.IsPriceInRange(r.lastPrice) {
			r.outsideBandCount++
		} else {
			r.outsideBandCount = 0
		}
		if r.currentRange.IsBreakout(r.lastPrice, r.config.BreakoutThreshold) ||
			r.outsideBandCount >= maxInt(1, r.config.OutsideBandConfirmations) ||
			r.isVolatilityExpandedLocked() {
			r.state = RangeStateBreakout
			r.breakoutTime = time.Now()
			r.reentryCount = 0
			r.entryCount = 0
			r.logger.Warn("State: BREAKOUT DETECTED - Close all positions!",
				zap.Float64("price", r.lastPrice),
				zap.Float64("upper_bound", r.currentRange.UpperBound),
				zap.Float64("lower_bound", r.currentRange.LowerBound))
		}

	case RangeStateBreakout:
		// Chờ stabilization period
		if time.Since(r.breakoutTime) >= r.config.StabilizationPeriod {
			r.state = RangeStateStabilizing
			r.stabilizationStart = time.Now()
			r.reentryCount = 0
			r.outsideBandCount = 0
			r.logger.Info("State: Stabilizing - Waiting for new range")
		}

	case RangeStateStabilizing:
		if r.currentRange == nil {
			r.reentryCount = 0
			return
		}
		if time.Since(r.stabilizationStart) < r.config.StabilizationPeriod {
			return
		}
		if !r.currentRange.IsPriceInRange(r.lastPrice) {
			r.reentryCount = 0
			return
		}
		if r.enableADXFilter && !r.isSidewaysConfirmedLocked() {
			r.reentryCount = 0
			return
		}
		if !r.hasMaterialRangeShiftLocked() {
			r.reentryCount = 0
			return
		}

		r.reentryCount++
		if r.reentryCount >= maxInt(1, r.config.ReentryConfirmations) {
			r.state = RangeStateActive
			r.lastAcceptedRange = cloneRangeData(r.currentRange)
			r.reentryCount = 0
			r.outsideBandCount = 0
			r.logger.Info("State: New Range Active - Resume trading",
				zap.Float64("upper", r.currentRange.UpperBound),
				zap.Float64("lower", r.currentRange.LowerBound),
				zap.Float64("avg_adx", r.averageADXLocked()))
		}
	}
}

// GetCurrentRange returns current range data
func (r *RangeDetector) GetCurrentRange() *RangeData {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentRange
}

// GetState returns current state
func (r *RangeDetector) GetState() RangeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// GetGridParameters returns calculated grid parameters
func (r *RangeDetector) GetGridParameters() (spreadPct float64, levels int, valid bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.state != RangeStateActive || r.currentRange == nil {
		return 0, 0, false
	}

	return r.gridSpreadPct, r.gridLevels, true
}

// ShouldTrade returns true if trading is allowed (range active + ADX sideways if enabled)
func (r *RangeDetector) ShouldTrade() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Must have active range
	if r.state != RangeStateActive {
		return false
	}

	// Must pass ADX filter if enabled
	if r.enableADXFilter {
		return r.IsSidewaysConfirmed()
	}

	return true
}

// IsBreakout returns true if breakout detected
func (r *RangeDetector) IsBreakout() bool {
	return r.GetState() == RangeStateBreakout
}

// GetATR returns the current ATR value
func (r *RangeDetector) GetATR() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.currentRange != nil {
		return r.currentRange.ATR
	}

	// Calculate ATR from history if range not established
	if len(r.prices) > 0 {
		return r.calculateATRLocked()
	}

	return 0
}

// GetATRBands returns ATR-based bands for MICRO mode
func (r *RangeDetector) GetATRBands(multiplier float64) (upper, lower float64, valid bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.prices) == 0 {
		return 0, 0, false
	}

	currentPrice := r.lastPrice
	if currentPrice == 0 && len(r.prices) > 0 {
		currentPrice = r.prices[len(r.prices)-1]
	}

	// Use existing ATR or calculate from history
	var atr float64
	if r.currentRange != nil && r.currentRange.ATR > 0 {
		atr = r.currentRange.ATR
	} else {
		atr = r.calculateATRLocked()
	}

	if atr == 0 || multiplier <= 0 {
		return 0, 0, false
	}

	upper = currentPrice + atr*multiplier
	lower = currentPrice - atr*multiplier
	valid = upper > lower && upper > 0 && lower > 0

	return upper, lower, valid
}

// calculateATRLocked calculates ATR (must be called with lock held)
func (r *RangeDetector) calculateATRLocked() float64 {
	if len(r.highs) < 2 || len(r.lows) < 2 {
		return 0
	}

	period := r.config.Periods
	if period > len(r.highs) {
		period = len(r.highs)
	}

	sumTR := 0.0
	for i := len(r.highs) - period; i < len(r.highs); i++ {
		if i > 0 {
			tr := r.highs[i] - r.lows[i]
			if tr < 0 {
				tr = -tr
			}
			sumTR += tr
		}
	}

	if period > 0 {
		return sumTR / float64(period)
	}
	return 0
}

// HasEnoughDataForMICRO checks if we have enough data for MICRO mode trading
func (r *RangeDetector) HasEnoughDataForMICRO() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Need at least 3 candles for basic ATR calculation
	minCandles := 3
	if r.config.Periods < minCandles {
		minCandles = r.config.Periods
	}

	return len(r.prices) >= minCandles && r.lastPrice > 0
}

// ForceRecalculate forces range recalculation
func (r *RangeDetector) ForceRecalculate() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.currentRange = nil
	r.state = RangeStateEstablishing
	r.reentryCount = 0
	r.updateRange()
}

// GetStateString returns state as string for logging
func (r *RangeDetector) GetStateString() string {
	switch r.GetState() {
	case RangeStateUnknown:
		return "Unknown"
	case RangeStateEstablishing:
		return "Establishing"
	case RangeStateActive:
		return "Active"
	case RangeStateBreakout:
		return "Breakout"
	case RangeStateStabilizing:
		return "Stabilizing"
	default:
		return "Invalid"
	}
}

// GetRangeInfo returns comprehensive range info for logging/display
func (r *RangeDetector) GetRangeInfo() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info := map[string]interface{}{
		"state":       r.GetStateString(),
		"last_price":  r.lastPrice,
		"grid_spread": r.gridSpreadPct,
		"grid_levels": r.gridLevels,
	}

	if r.currentRange != nil {
		info["range_upper"] = r.currentRange.UpperBound
		info["range_lower"] = r.currentRange.LowerBound
		info["range_mid"] = r.currentRange.MidPrice
		info["range_width_pct"] = r.currentRange.WidthPct * 100
		info["atr"] = r.currentRange.ATR
		info["volatility"] = r.currentRange.Volatility
		info["price_in_range"] = r.currentRange.IsPriceInRange(r.lastPrice)
	}

	return info
}

// SetADXFilter enables/disables ADX filter for sideways detection
func (r *RangeDetector) SetADXFilter(enabled bool, maxADX float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enableADXFilter = enabled
	if maxADX > 0 {
		r.sidewaysADXMax = maxADX
	} else {
		r.sidewaysADXMax = 20.0 // Default
	}
}

// UpdateADX updates current ADX value
func (r *RangeDetector) UpdateADX(adx float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.currentADX = adx
	r.adxHistory = append(r.adxHistory, adx)

	// Keep only last 14 values
	if len(r.adxHistory) > 14 {
		r.adxHistory = r.adxHistory[1:]
	}
}

// GetCurrentADX returns current ADX value
func (r *RangeDetector) GetCurrentADX() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentADX
}

// IsSidewaysConfirmed returns true if ADX indicates sideways market (ADX < sidewaysADXMax)
func (r *RangeDetector) IsSidewaysConfirmed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.isSidewaysConfirmedLocked()
}

// ShouldExitForTrend returns true when ADX confirms that the active range has likely transitioned to a trend.
func (r *RangeDetector) ShouldExitForTrend() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.enableADXFilter || r.currentRange == nil || r.state != RangeStateActive {
		return false
	}

	avgADX := r.averageADXLocked()
	return avgADX >= r.sidewaysADXMax*1.25 || r.isVolatilityExpandedLocked()
}

func (r *RangeDetector) isSidewaysConfirmedLocked() bool {
	// If ADX filter is not enabled, always return true (allow trading)
	if !r.enableADXFilter {
		return true
	}

	// Check if we have enough ADX data
	if r.currentADX == 0 && len(r.adxHistory) == 0 {
		return true // Default to allowing trading if no ADX data
	}

	// Sideways market when ADX < sidewaysADXMax (default 20)
	return r.averageADXLocked() < r.sidewaysADXMax
}

func (r *RangeDetector) averageADXLocked() float64 {
	if len(r.adxHistory) == 0 {
		return r.currentADX
	}

	sum := 0.0
	for _, v := range r.adxHistory {
		sum += v
	}
	return sum / float64(len(r.adxHistory))
}

func (r *RangeDetector) isVolatilityExpandedLocked() bool {
	if r.currentRange == nil {
		return false
	}
	if r.config.BBExpansionFactor <= 0 {
		return false
	}
	if len(r.widthHistory) < maxInt(r.config.Periods/2, 3) {
		return false
	}

	sum := 0.0
	for i := 0; i < len(r.widthHistory)-1; i++ {
		sum += r.widthHistory[i]
	}
	avgWidth := sum / float64(len(r.widthHistory)-1)
	if avgWidth <= 0 {
		return false
	}

	return r.currentRange.WidthPct >= avgWidth*r.config.BBExpansionFactor
}

func (r *RangeDetector) updateADXLocked() {
	if len(r.prices) < r.config.ADXPeriod+1 || r.config.ADXPeriod <= 1 {
		return
	}

	period := r.config.ADXPeriod
	start := len(r.prices) - period
	if start < 1 {
		start = 1
	}

	trSum := 0.0
	plusDMSum := 0.0
	minusDMSum := 0.0

	for i := start; i < len(r.prices); i++ {
		upMove := r.highs[i] - r.highs[i-1]
		downMove := r.lows[i-1] - r.lows[i]

		plusDM := 0.0
		minusDM := 0.0
		if upMove > downMove && upMove > 0 {
			plusDM = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM = downMove
		}

		highLow := r.highs[i] - r.lows[i]
		highClose := math.Abs(r.highs[i] - r.prices[i-1])
		lowClose := math.Abs(r.lows[i] - r.prices[i-1])
		tr := math.Max(highLow, math.Max(highClose, lowClose))

		trSum += tr
		plusDMSum += plusDM
		minusDMSum += minusDM
	}

	if trSum == 0 {
		return
	}

	plusDI := (plusDMSum / trSum) * 100
	minusDI := (minusDMSum / trSum) * 100
	denominator := plusDI + minusDI
	if denominator == 0 {
		return
	}

	adx := math.Abs(plusDI-minusDI) / denominator * 100
	r.currentADX = adx
	r.adxHistory = append(r.adxHistory, adx)
	if len(r.adxHistory) > period {
		r.adxHistory = r.adxHistory[1:]
	}
}

func (r *RangeDetector) hasMaterialRangeShiftLocked() bool {
	if r.currentRange == nil || r.lastAcceptedRange == nil {
		return true
	}

	centerShiftPct := math.Abs(r.currentRange.MidPrice-r.lastAcceptedRange.MidPrice) / r.lastAcceptedRange.MidPrice
	widthChangePct := math.Abs(r.currentRange.WidthPct - r.lastAcceptedRange.WidthPct)

	return centerShiftPct >= r.config.MaterialShiftPct || widthChangePct >= r.config.WidthChangePct
}

func cloneRangeData(in *RangeData) *RangeData {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
