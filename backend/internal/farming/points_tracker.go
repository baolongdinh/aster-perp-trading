package farming

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/config"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// PointsTracker tracks points accumulation and farming efficiency
type PointsTracker struct {
	config *config.VolumeFarmConfig
	logger *logrus.Entry

	// Points tracking
	totalPoints int64
	totalVolume float64
	totalFees   float64

	// Performance metrics
	pointsPerDollar float64
	fillRate        float64
	efficiencyScore float64

	// State management
	isRunning   bool
	isRunningMu sync.RWMutex
	stopCh      chan struct{}
	wg          sync.WaitGroup

	// Metrics storage
	orderMetrics map[string]*OrderMetrics
	metricsMu    sync.RWMutex

	// Historical data
	hourlyStats []*HourlyStats
	statsMu     sync.RWMutex
}

// OrderMetrics tracks metrics for individual orders
type OrderMetrics struct {
	OrderID      string        `json:"order_id"`
	Symbol       string        `json:"symbol"`
	Side         string        `json:"side"`
	Size         float64       `json:"size"`
	Price        float64       `json:"price"`
	FeePaid      float64       `json:"fee_paid"`
	PointsEarned int64         `json:"points_earned"`
	FilledAt     time.Time     `json:"filled_at"`
	OrderType    string        `json:"order_type"`
	TimeToFill   time.Duration `json:"time_to_fill"`
}

// HourlyStats tracks hourly performance statistics
type HourlyStats struct {
	Hour            time.Time `json:"hour"`
	Volume          float64   `json:"volume"`
	Points          int64     `json:"points"`
	Fees            float64   `json:"fees"`
	FillRate        float64   `json:"fill_rate"`
	PointsPerDollar float64   `json:"points_per_dollar"`
	EfficiencyScore float64   `json:"efficiency_score"`
	OrderCount      int       `json:"order_count"`
}

// NewPointsTracker creates a new points tracker
func NewPointsTracker(cfg *config.VolumeFarmConfig, logger *logrus.Entry) *PointsTracker {
	tracker := &PointsTracker{
		config:       cfg,
		logger:       logger.WithField("component", "points_tracker"),
		orderMetrics: make(map[string]*OrderMetrics),
		hourlyStats:  make([]*HourlyStats, 0),
		stopCh:       make(chan struct{}),
	}

	return tracker
}

// Start starts the points tracker
func (p *PointsTracker) Start(ctx context.Context) error {
	p.isRunningMu.Lock()
	if p.isRunning {
		p.isRunningMu.Unlock()
		return fmt.Errorf("points tracker is already running")
	}
	p.isRunning = true
	p.isRunningMu.Unlock()

	p.logger.Info("📊 Starting Points Tracker")

	// Start metrics calculation loop
	p.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.logger.Error("Metrics loop goroutine panic recovered",
					zap.Any("panic", r))
			}
		}()
		defer p.wg.Done()
		p.metricsLoop(ctx)
	}()

	// Start hourly stats aggregation
	p.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.logger.Error("Hourly stats loop goroutine panic recovered",
					zap.Any("panic", r))
			}
		}()
		defer p.wg.Done()
		p.hourlyStatsLoop(ctx)
	}()

	p.logger.Info("✅ Points Tracker started successfully")
	return nil
}

// Stop stops the points tracker
func (p *PointsTracker) Stop(ctx context.Context) error {
	p.isRunningMu.Lock()
	if !p.isRunning {
		p.isRunningMu.Unlock()
		return nil
	}
	p.isRunning = false
	p.isRunningMu.Unlock()

	p.logger.Info("🛑 Stopping Points Tracker")

	// Safely close stopCh
	select {
	case <-p.stopCh:
		// already closed
	default:
		close(p.stopCh)
	}

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.logger.Error("WaitGroup goroutine panic recovered during stop",
					zap.Any("panic", r))
				close(done)
			}
		}()
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("✅ Points Tracker stopped gracefully")
		return nil
	case <-ctx.Done():
		p.logger.Warn("⚠️  Points Tracker stop timeout")
		return ctx.Err()
	}
}

// OnOrderUpdate handles order update events
func (p *PointsTracker) OnOrderUpdate(orderUpdate *OrderUpdate) {
	if !p.IsRunning() {
		return
	}

	// Calculate points earned
	pointsEarned := p.calculatePointsForOrder(orderUpdate)

	// Create order metrics
	metrics := &OrderMetrics{
		OrderID:      orderUpdate.OrderID,
		Symbol:       orderUpdate.Symbol,
		Side:         orderUpdate.Side,
		Size:         orderUpdate.Quantity,
		Price:        orderUpdate.Price,
		FeePaid:      orderUpdate.Fee,
		PointsEarned: pointsEarned,
		FilledAt:     time.Now(),
		OrderType:    orderUpdate.Type,
		TimeToFill:   time.Since(time.Unix(0, orderUpdate.Timestamp*1000000)),
	}

	// Store metrics
	p.metricsMu.Lock()
	p.orderMetrics[orderUpdate.OrderID] = metrics
	p.metricsMu.Unlock()

	// Update totals
	p.updateTotals(orderUpdate.Quantity, orderUpdate.Fee, pointsEarned)

	p.logger.WithFields(logrus.Fields{
		"symbol":            orderUpdate.Symbol,
		"side":              orderUpdate.Side,
		"size":              orderUpdate.Quantity,
		"price":             orderUpdate.Price,
		"fee_paid":          orderUpdate.Fee,
		"points":            pointsEarned,
		"points_per_dollar": float64(pointsEarned) / orderUpdate.Quantity,
	}).Info("Order filled - points earned")
}

// calculatePointsForOrder calculates points earned for an order
func (p *PointsTracker) calculatePointsForOrder(orderUpdate *OrderUpdate) int64 {
	// Base points calculation
	// Maker orders get 2x points: fee contribution + maker liquidity contribution
	// Taker orders get 1x points: fee contribution only

	feeUSDT := orderUpdate.Fee
	volumeUSDT := orderUpdate.Quantity * orderUpdate.Price

	var basePoints int64

	if orderUpdate.Type == "LIMIT" {
		// Maker order: 2 points per $0.001 fee contribution
		basePoints = int64(feeUSDT * 2000)

		// Additional maker liquidity contribution: 1 point per $1 volume
		liquidityPoints := int64(volumeUSDT)
		basePoints += liquidityPoints

		p.logger.WithFields(logrus.Fields{
			"fee_points":        int64(feeUSDT * 2000),
			"liquidity_points":  liquidityPoints,
			"total_base_points": basePoints,
		}).Debug("Maker order points calculation")
	} else {
		// Taker order: 1 point per $0.001 fee contribution
		basePoints = int64(feeUSDT * 1000)

		p.logger.WithFields(logrus.Fields{
			"fee_points": basePoints,
		}).Debug("Taker order points calculation")
	}

	// Apply efficiency bonuses
	efficiencyBonus := p.calculateEfficiencyBonus(orderUpdate)
	finalPoints := basePoints + efficiencyBonus

	return finalPoints
}

// calculateEfficiencyBonus calculates efficiency bonus for orders
func (p *PointsTracker) calculateEfficiencyBonus(orderUpdate *OrderUpdate) int64 {
	var bonus int64

	// Fast fill bonus (< 30 seconds)
	if time.Since(time.Unix(0, orderUpdate.Timestamp*1000000)) < 30*time.Second {
		bonus += 10
	}

	// Large order bonus (> $1000)
	if (orderUpdate.Quantity * orderUpdate.Price) > 1000 {
		bonus += 20
	}

	// Tight spread bonus (this would require spread data)
	// For now, give small bonus to all limit orders
	if orderUpdate.Type == "LIMIT" {
		bonus += 5
	}

	return bonus
}

// updateTotals updates cumulative totals
func (p *PointsTracker) updateTotals(volume, fees float64, points int64) {
	p.totalVolume += volume
	p.totalFees += fees
	p.totalPoints += points

	// Calculate points per dollar
	if p.totalVolume > 0 {
		p.pointsPerDollar = float64(p.totalPoints) / p.totalVolume
	}
}

// metricsLoop calculates and updates performance metrics
func (p *PointsTracker) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.calculateMetrics()
		}
	}
}

// calculateMetrics calculates current performance metrics
func (p *PointsTracker) calculateMetrics() {
	p.metricsMu.RLock()
	defer p.metricsMu.RUnlock()

	// Calculate fill rate
	totalOrders := len(p.orderMetrics)
	if totalOrders == 0 {
		p.fillRate = 0
	} else {
		filledOrders := 0
		for _, metrics := range p.orderMetrics {
			if metrics.FilledAt.IsZero() == false {
				filledOrders++
			}
		}
		p.fillRate = float64(filledOrders) / float64(totalOrders)
	}

	// Calculate efficiency score (0-100)
	// Based on fill rate, points per dollar, and fee efficiency
	fillRateScore := p.fillRate * 40                           // 40% weight
	pointsScore := min(p.pointsPerDollar/10, 1) * 30           // 30% weight, max at 10 points/$
	feeScore := max(0, 1-(p.totalFees/p.totalVolume)*100) * 30 // 30% weight

	p.efficiencyScore = fillRateScore + pointsScore + feeScore
}

// hourlyStatsLoop aggregates hourly statistics
func (p *PointsTracker) hourlyStatsLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.aggregateHourlyStats()
		}
	}
}

// aggregateHourlyStats aggregates statistics for the past hour
func (p *PointsTracker) aggregateHourlyStats() {
	now := time.Now()
	hourStart := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())

	// Find orders in the past hour
	var hourOrders []*OrderMetrics
	p.metricsMu.RLock()
	for _, metrics := range p.orderMetrics {
		if metrics.FilledAt.After(hourStart) && metrics.FilledAt.Before(now) {
			hourOrders = append(hourOrders, metrics)
		}
	}
	p.metricsMu.RUnlock()

	if len(hourOrders) == 0 {
		return
	}

	// Calculate hourly stats
	var hourVolume, hourFees float64
	var hourPoints int64
	var hourFillRate float64

	for _, order := range hourOrders {
		hourVolume += order.Size * order.Price
		hourFees += order.FeePaid
		hourPoints += order.PointsEarned
	}

	hourFillRate = float64(len(hourOrders)) / float64(len(hourOrders))
	hourPointsPerDollar := float64(hourPoints) / hourVolume

	// Calculate efficiency score for the hour
	fillRateScore := hourFillRate * 40
	pointsScore := min(hourPointsPerDollar/10, 1) * 30
	feeScore := max(0, 1-(hourFees/hourVolume)*100) * 30
	hourEfficiencyScore := fillRateScore + pointsScore + feeScore

	// Create hourly stats
	stats := &HourlyStats{
		Hour:            hourStart,
		Volume:          hourVolume,
		Points:          hourPoints,
		Fees:            hourFees,
		FillRate:        hourFillRate,
		PointsPerDollar: hourPointsPerDollar,
		EfficiencyScore: hourEfficiencyScore,
		OrderCount:      len(hourOrders),
	}

	// Store stats
	p.statsMu.Lock()
	p.hourlyStats = append(p.hourlyStats, stats)

	// Keep only last 24 hours of stats
	if len(p.hourlyStats) > 24 {
		p.hourlyStats = p.hourlyStats[1:]
	}
	p.statsMu.Unlock()

	p.logger.WithFields(logrus.Fields{
		"hour":              hourStart.Format("2006-01-02 15:04"),
		"volume":            hourVolume,
		"points":            hourPoints,
		"points_per_dollar": hourPointsPerDollar,
		"efficiency_score":  fmt.Sprintf("%.1f", hourEfficiencyScore),
		"order_count":       len(hourOrders),
	}).Info("Hourly stats aggregated")
}

// GetCurrentPoints returns current total points
func (p *PointsTracker) GetCurrentPoints() int64 {
	return p.totalPoints
}

// GetCurrentVolume returns current total volume
func (p *PointsTracker) GetCurrentVolume() float64 {
	return p.totalVolume
}

// GetPointsPerDollar returns current points per dollar ratio
func (p *PointsTracker) GetPointsPerDollar() float64 {
	return p.pointsPerDollar
}

// GetEfficiencyScore returns current efficiency score
func (p *PointsTracker) GetEfficiencyScore() float64 {
	return p.efficiencyScore
}

// GetFillRate returns current fill rate
func (p *PointsTracker) GetFillRate() float64 {
	return p.fillRate
}

// GetHourlyStats returns hourly statistics
func (p *PointsTracker) GetHourlyStats() []*HourlyStats {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()

	result := make([]*HourlyStats, len(p.hourlyStats))
	copy(result, p.hourlyStats)
	return result
}

// GetOrderMetrics returns order metrics
func (p *PointsTracker) GetOrderMetrics() map[string]*OrderMetrics {
	p.metricsMu.RLock()
	defer p.metricsMu.RUnlock()

	result := make(map[string]*OrderMetrics)
	for k, v := range p.orderMetrics {
		result[k] = v
	}
	return result
}

// GetPerformanceSummary returns a performance summary
func (p *PointsTracker) GetPerformanceSummary() *PerformanceSummary {
	return &PerformanceSummary{
		TotalPoints:     p.totalPoints,
		TotalVolume:     p.totalVolume,
		TotalFees:       p.totalFees,
		PointsPerDollar: p.pointsPerDollar,
		FillRate:        p.fillRate,
		EfficiencyScore: p.efficiencyScore,
		OrderCount:      len(p.orderMetrics),
		LastUpdate:      time.Now(),
	}
}

// PerformanceSummary represents a summary of farming performance
type PerformanceSummary struct {
	TotalPoints     int64     `json:"total_points"`
	TotalVolume     float64   `json:"total_volume"`
	TotalFees       float64   `json:"total_fees"`
	PointsPerDollar float64   `json:"points_per_dollar"`
	FillRate        float64   `json:"fill_rate"`
	EfficiencyScore float64   `json:"efficiency_score"`
	OrderCount      int       `json:"order_count"`
	LastUpdate      time.Time `json:"last_update"`
}

// IsRunning returns whether the points tracker is running
func (p *PointsTracker) IsRunning() bool {
	p.isRunningMu.RLock()
	defer p.isRunningMu.RUnlock()
	return p.isRunning
}

// Helper functions
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
