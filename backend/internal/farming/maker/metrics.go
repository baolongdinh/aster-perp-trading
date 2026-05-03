package maker

import (
	"sync"
	"time"
)

type MetricsCollector struct {
	mu           sync.RWMutex
	startTime    time.Time
	totalVolume  float64
	netPnL       float64
	makerRebate  float64
	totalOrders  int64
	totalFills   int64
	feesPaid     float64
	lastUpdate   time.Time
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime: time.Now(),
		lastUpdate: time.Now(),
	}
}

func (m *MetricsCollector) RecordTrade(volume float64, pnl float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalVolume += volume
	m.netPnL += pnl
	m.totalFills++
	m.lastUpdate = time.Now()
}

func (m *MetricsCollector) RecordOrder() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalOrders++
}

func (m *MetricsCollector) RecordFee(fee float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.feesPaid += fee
}

func (m *MetricsCollector) RecordRebate(rebate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.makerRebate += rebate
}

func (m *MetricsCollector) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	uptime := time.Since(m.startTime)
	var fillRate float64
	if m.totalOrders > 0 {
		fillRate = float64(m.totalFills) / float64(m.totalOrders)
	}

	var effectiveFeeRate float64
	if m.totalVolume > 0 {
		effectiveFeeRate = m.feesPaid / m.totalVolume
	}

	return map[string]interface{}{
		"uptime_seconds":    uptime.Seconds(),
		"total_volume":      m.totalVolume,
		"net_pnl":           m.netPnL,
		"maker_rebate":      m.makerRebate,
		"total_orders":      m.totalOrders,
		"total_fills":       m.totalFills,
		"fill_rate":         fillRate * 100,
		"fees_paid":         m.feesPaid,
		"effective_fee_pct": effectiveFeeRate * 100,
		"last_update":       m.lastUpdate,
	}
}

func (m *MetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalVolume = 0
	m.netPnL = 0
	m.makerRebate = 0
	m.totalOrders = 0
	m.totalFills = 0
	m.feesPaid = 0
	m.startTime = time.Now()
	m.lastUpdate = time.Now()
}

func (m *MetricsCollector) GetTotalVolume() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalVolume
}

func (m *MetricsCollector) GetNetPnL() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.netPnL
}

func (m *MetricsCollector) GetFillRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.totalOrders == 0 {
		return 0
	}
	return float64(m.totalFills) / float64(m.totalOrders)
}
