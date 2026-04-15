package metrics

import (
	"sync"
	"time"

	"aster-bot/internal/farming/tradingmode"
	"go.uber.org/zap"
)

// TradingMetrics tracks trading performance metrics
type TradingMetrics struct {
	mu sync.RWMutex

	Symbol      string
	CurrentMode tradingmode.TradingMode
	ModeSince   time.Time

	// Fill metrics
	FillsLastHour      int
	TotalFills24h      int
	AvgProfitPerFill   float64
	TotalVolume24h     float64

	// Exit metrics
	LastExitDuration  time.Duration
	ExitCount24h      int
	MaxExitDuration   time.Duration

	// Sync metrics
	SyncMismatches24h int
	LastMismatchAt    time.Time

	// Mode transition metrics
	ModeTransitions24h int

	// Internal tracking
	fillHistory       []FillRecord
	exitHistory       []ExitRecord
	modeHistory       []ModeRecord
	logger            *zap.Logger
}

// FillRecord tracks a single fill
type FillRecord struct {
	Timestamp time.Time
	Symbol    string
	Side      string
	Size      float64
	Price     float64
	Profit    float64
}

// ExitRecord tracks a single exit
type ExitRecord struct {
	Timestamp time.Time
	Symbol    string
	Duration  time.Duration
	Reason    string
}

// ModeRecord tracks mode duration
type ModeRecord struct {
	Mode      tradingmode.TradingMode
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}

// NewTradingMetrics creates new trading metrics collector
func NewTradingMetrics(symbol string, logger *zap.Logger) *TradingMetrics {
	return &TradingMetrics{
		Symbol:      symbol,
		CurrentMode: tradingmode.TradingModeUnknown,
		ModeSince:   time.Now(),
		fillHistory: make([]FillRecord, 0),
		exitHistory: make([]ExitRecord, 0),
		modeHistory: make([]ModeRecord, 0),
		logger:      logger.With(zap.String("component", "trading_metrics"), zap.String("symbol", symbol)),
	}
}

// RecordFill records a new fill
func (m *TradingMetrics) RecordFill(side string, size, price, profit float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := FillRecord{
		Timestamp: time.Now(),
		Symbol:    m.Symbol,
		Side:      side,
		Size:      size,
		Price:     price,
		Profit:    profit,
	}

	m.fillHistory = append(m.fillHistory, record)
	m.TotalFills24h++

	// Update fills in last hour
	m.recalculateFillsLastHour()

	// Update average profit
	m.recalculateAvgProfit()

	// Update volume
	m.TotalVolume24h += size * price

	// Clean old records
	m.cleanOldRecords()

	m.logger.Debug("Fill recorded",
		zap.String("side", side),
		zap.Float64("size", size),
		zap.Float64("price", price),
		zap.Float64("profit", profit))
}

// RecordExit records an exit operation
func (m *TradingMetrics) RecordExit(duration time.Duration, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := ExitRecord{
		Timestamp: time.Now(),
		Symbol:    m.Symbol,
		Duration:  duration,
		Reason:    reason,
	}

	m.exitHistory = append(m.exitHistory, record)
	m.ExitCount24h++
	m.LastExitDuration = duration

	if duration > m.MaxExitDuration {
		m.MaxExitDuration = duration
	}

	// Alert if exit took too long
	if duration > 5*time.Second {
		m.logger.Warn("Exit duration exceeded 5 seconds",
			zap.Duration("duration", duration),
			zap.String("reason", reason))
	}

	m.cleanOldRecords()
}

// RecordModeTransition records a mode transition
func (m *TradingMetrics) RecordModeTransition(from, to tradingmode.TradingMode) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// Record previous mode duration
	if m.CurrentMode != tradingmode.TradingModeUnknown {
		record := ModeRecord{
			Mode:      m.CurrentMode,
			StartTime: m.ModeSince,
			EndTime:   now,
			Duration:  now.Sub(m.ModeSince),
		}
		m.modeHistory = append(m.modeHistory, record)
	}

	m.CurrentMode = to
	m.ModeSince = now
	m.ModeTransitions24h++

	m.logger.Info("Mode transition recorded",
		zap.String("from", from.String()),
		zap.String("to", to.String()))

	m.cleanOldRecords()
}

// RecordSyncMismatch records a sync mismatch
func (m *TradingMetrics) RecordSyncMismatch() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SyncMismatches24h++
	m.LastMismatchAt = time.Now()
}

// GetFillsPerHour returns fills per hour
func (m *TradingMetrics) GetFillsPerHour() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.recalculateFillsLastHour()
	return m.FillsLastHour
}

// GetMetricsSnapshot returns a snapshot of current metrics
func (m *TradingMetrics) GetMetricsSnapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MetricsSnapshot{
		Symbol:             m.Symbol,
		CurrentMode:        m.CurrentMode.String(),
		ModeDuration:       time.Since(m.ModeSince),
		FillsLastHour:      m.FillsLastHour,
		TotalFills24h:      m.TotalFills24h,
		AvgProfitPerFill:   m.AvgProfitPerFill,
		TotalVolume24h:     m.TotalVolume24h,
		LastExitDuration:   m.LastExitDuration,
		ExitCount24h:       m.ExitCount24h,
		SyncMismatches24h:  m.SyncMismatches24h,
		ModeTransitions24h: m.ModeTransitions24h,
	}
}

// recalculateFillsLastHour recalculates fills in last hour
func (m *TradingMetrics) recalculateFillsLastHour() {
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	count := 0

	for _, record := range m.fillHistory {
		if record.Timestamp.After(oneHourAgo) {
			count++
		}
	}

	m.FillsLastHour = count
}

// recalculateAvgProfit recalculates average profit per fill
func (m *TradingMetrics) recalculateAvgProfit() {
	if len(m.fillHistory) == 0 {
		m.AvgProfitPerFill = 0
		return
	}

	totalProfit := 0.0
	for _, record := range m.fillHistory {
		totalProfit += record.Profit
	}

	m.AvgProfitPerFill = totalProfit / float64(len(m.fillHistory))
}

// cleanOldRecords removes records older than 24 hours
func (m *TradingMetrics) cleanOldRecords() {
	oneDayAgo := time.Now().Add(-24 * time.Hour)

	// Clean fills
	newFills := make([]FillRecord, 0)
	for _, record := range m.fillHistory {
		if record.Timestamp.After(oneDayAgo) {
			newFills = append(newFills, record)
		}
	}
	m.fillHistory = newFills

	// Clean exits
	newExits := make([]ExitRecord, 0)
	for _, record := range m.exitHistory {
		if record.Timestamp.After(oneDayAgo) {
			newExits = append(newExits, record)
		}
	}
	m.exitHistory = newExits

	// Clean mode history
	newModes := make([]ModeRecord, 0)
	for _, record := range m.modeHistory {
		if record.StartTime.After(oneDayAgo) {
			newModes = append(newModes, record)
		}
	}
	m.modeHistory = newModes
}

// MetricsSnapshot is a snapshot of trading metrics
type MetricsSnapshot struct {
	Symbol             string        `json:"symbol"`
	CurrentMode        string        `json:"current_mode"`
	ModeDuration       time.Duration `json:"mode_duration"`
	FillsLastHour      int           `json:"fills_last_hour"`
	TotalFills24h      int           `json:"total_fills_24h"`
	AvgProfitPerFill   float64       `json:"avg_profit_per_fill"`
	TotalVolume24h     float64       `json:"total_volume_24h"`
	LastExitDuration   time.Duration `json:"last_exit_duration"`
	ExitCount24h       int           `json:"exit_count_24h"`
	SyncMismatches24h  int           `json:"sync_mismatches_24h"`
	ModeTransitions24h int         `json:"mode_transitions_24h"`
}
