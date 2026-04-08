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
	Method              string        // "bollinger", "atr", hoặc "combined"
	Periods             int           // Số period cho calculation (20 cho BB, 14 cho ATR)
	BBMultiplier        float64       // Bollinger Bands multiplier (thường 2.0)
	ATRMultiplier       float64       // ATR multiplier cho range
	BreakoutThreshold   float64       // % vượt khỏi range để coi là breakout (e.g., 0.01 = 1%)
	StabilizationPeriod time.Duration // Thời gian chờ ổn định sau breakout
	MinRangeWidthPct    float64       // Minimum range width as % of price (e.g., 0.005 = 0.5%)
}

// DefaultRangeConfig returns default range configuration
func DefaultRangeConfig() *RangeConfig {
	return &RangeConfig{
		Method:              "combined",       // Dùng cả BB và ATR
		Periods:             20,               // 20 periods
		BBMultiplier:        2.0,              // 2 sigma cho BB
		ATRMultiplier:       1.5,              // 1.5x ATR cho range
		BreakoutThreshold:   0.01,             // 1% vượt range = breakout
		StabilizationPeriod: 30 * time.Second, // Chờ 30s sau breakout để resume nhanh
		MinRangeWidthPct:    0.003,            // Tối thiểu 0.3% range width
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
	state              RangeState
	breakoutTime       time.Time
	lastPrice          float64
	stabilizationStart time.Time

	// Grid parameters derived from range
	gridSpreadPct float64
	gridLevels    int
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
		r.logger.Debug("Range too narrow, skipping",
			zap.Float64("width_pct", widthPct*100),
			zap.Float64("min_required", r.config.MinRangeWidthPct*100))
		return
	}

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
		// Chuyển sang Active khi có range hợp lệ
		r.state = RangeStateActive
		r.logger.Info("State: Range Active - Ready for grid",
			zap.Float64("upper", r.currentRange.UpperBound),
			zap.Float64("lower", r.currentRange.LowerBound),
			zap.Float64("width_pct", r.currentRange.WidthPct*100))

	case RangeStateActive:
		// Kiểm tra breakout
		if r.currentRange.IsBreakout(r.lastPrice, r.config.BreakoutThreshold) {
			r.state = RangeStateBreakout
			r.breakoutTime = time.Now()
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
			r.logger.Info("State: Stabilizing - Waiting for new range")
		}

	case RangeStateStabilizing:
		// Kiểm tra xem đã có range mới ổn định chưa (nhanh - 30s)
		if r.currentRange != nil && time.Since(r.stabilizationStart) >= 30*time.Second {
			// Kiểm tra giá đang nằm trong range mới
			if r.currentRange.IsPriceInRange(r.lastPrice) {
				r.state = RangeStateActive
				r.logger.Info("State: New Range Active - Resume trading",
					zap.Float64("upper", r.currentRange.UpperBound),
					zap.Float64("lower", r.currentRange.LowerBound))
			}
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

// ShouldTrade returns true if trading is allowed (range active)
func (r *RangeDetector) ShouldTrade() bool {
	state := r.GetState()
	return state == RangeStateActive
}

// IsBreakout returns true if breakout detected
func (r *RangeDetector) IsBreakout() bool {
	return r.GetState() == RangeStateBreakout
}

// ForceRecalculate forces range recalculation
func (r *RangeDetector) ForceRecalculate() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.currentRange = nil
	r.state = RangeStateEstablishing
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
