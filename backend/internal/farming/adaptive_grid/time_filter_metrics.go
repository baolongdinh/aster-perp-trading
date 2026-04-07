package adaptive_grid

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// TimeFilterMetrics tracks operational metrics for TimeFilter
type TimeFilterMetrics struct {
	slotTransitions    int64
	ordersBlocked      int64
	ordersAllowed      int64
	configReloads      int64
	lastTransition     time.Time
	lastConfigReload   time.Time
	currentSlotName    string
	currentSlotEnabled bool
	mu                 sync.RWMutex
}

// NewTimeFilterMetrics creates a new metrics collector
func NewTimeFilterMetrics() *TimeFilterMetrics {
	return &TimeFilterMetrics{}
}

// RecordSlotTransition records a slot transition
func (m *TimeFilterMetrics) RecordSlotTransition(from, to string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slotTransitions++
	m.lastTransition = time.Now()
	m.currentSlotName = to
}

// RecordOrderBlocked records when an order is blocked by time filter
func (m *TimeFilterMetrics) RecordOrderBlocked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ordersBlocked++
}

// RecordOrderAllowed records when an order is allowed by time filter
func (m *TimeFilterMetrics) RecordOrderAllowed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ordersAllowed++
}

// RecordConfigReload records a configuration reload
func (m *TimeFilterMetrics) RecordConfigReload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configReloads++
	m.lastConfigReload = time.Now()
}

// UpdateCurrentSlot updates current slot information
func (m *TimeFilterMetrics) UpdateCurrentSlot(name string, enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentSlotName = name
	m.currentSlotEnabled = enabled
}

// GetSnapshot returns a snapshot of current metrics
func (m *TimeFilterMetrics) GetSnapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalOrders := m.ordersAllowed + m.ordersBlocked
	blockRate := 0.0
	if totalOrders > 0 {
		blockRate = float64(m.ordersBlocked) / float64(totalOrders) * 100
	}

	timeSinceTransition := time.Since(m.lastTransition)
	timeSinceReload := time.Since(m.lastConfigReload)

	return map[string]interface{}{
		"slot_transitions":       m.slotTransitions,
		"orders_blocked":         m.ordersBlocked,
		"orders_allowed":         m.ordersAllowed,
		"total_orders_checked":   totalOrders,
		"block_rate_pct":         blockRate,
		"config_reloads":         m.configReloads,
		"last_transition_ago":    timeSinceTransition.String(),
		"last_reload_ago":        timeSinceReload.String(),
		"current_slot_name":      m.currentSlotName,
		"current_slot_enabled":   m.currentSlotEnabled,
	}
}

// TimeFilterLogger provides structured logging for time filter operations
type TimeFilterLogger struct {
	logger  *zap.Logger
	metrics *TimeFilterMetrics
}

// NewTimeFilterLogger creates a new structured logger
func NewTimeFilterLogger(logger *zap.Logger, metrics *TimeFilterMetrics) *TimeFilterLogger {
	return &TimeFilterLogger{
		logger:  logger,
		metrics: metrics,
	}
}

// LogSlotTransition logs a slot transition event
func (l *TimeFilterLogger) LogSlotTransition(from, to *TimeSlotConfig) {
	if l.metrics != nil {
		fromName := "none"
		if from != nil {
			fromName = from.Description
		}
		toName := "none"
		if to != nil {
			toName = to.Description
		}
		l.metrics.RecordSlotTransition(fromName, toName)
	}

	fields := []zap.Field{
		zap.String("event", "slot_transition"),
	}

	if from != nil {
		fields = append(fields,
			zap.String("from_slot", from.Description),
			zap.Bool("from_enabled", from.Enabled),
			zap.Float64("from_size_mult", from.SizeMultiplier),
			zap.Float64("from_spread_mult", from.SpreadMultiplier),
		)
	} else {
		fields = append(fields, zap.String("from_slot", "none"))
	}

	if to != nil {
		fields = append(fields,
			zap.String("to_slot", to.Description),
			zap.Bool("to_enabled", to.Enabled),
			zap.Float64("to_size_mult", to.SizeMultiplier),
			zap.Float64("to_spread_mult", to.SpreadMultiplier),
		)
	} else {
		fields = append(fields, zap.String("to_slot", "none"))
	}

	l.logger.Info("Time slot transition detected", fields...)
}

// LogOrderBlocked logs when an order is blocked
func (l *TimeFilterLogger) LogOrderBlocked(symbol, side string, slot *TimeSlotConfig) {
	if l.metrics != nil {
		l.metrics.RecordOrderBlocked()
	}

	fields := []zap.Field{
		zap.String("event", "order_blocked"),
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("reason", "outside_trading_hours"),
	}

	if slot != nil {
		fields = append(fields,
			zap.String("current_slot", slot.Description),
			zap.Bool("slot_enabled", slot.Enabled),
		)
	}

	l.logger.Warn("Order placement blocked by time filter", fields...)
}

// LogOrderAllowed logs when an order is allowed
func (l *TimeFilterLogger) LogOrderAllowed(symbol, side string, slot *TimeSlotConfig) {
	if l.metrics != nil {
		l.metrics.RecordOrderAllowed()
	}

	// Only log at debug level for allowed orders to avoid spam
	fields := []zap.Field{
		zap.String("event", "order_allowed"),
		zap.String("symbol", symbol),
		zap.String("side", side),
	}

	if slot != nil {
		fields = append(fields,
			zap.String("current_slot", slot.Description),
			zap.Float64("size_multiplier", slot.SizeMultiplier),
		)
	}

	l.logger.Debug("Order placement allowed by time filter", fields...)
}

// LogConfigReload logs a configuration reload
func (l *TimeFilterLogger) LogConfigReload(mode TimeFilterMode, numSlots int) {
	if l.metrics != nil {
		l.metrics.RecordConfigReload()
	}

	l.logger.Info("Time filter configuration reloaded",
		zap.String("event", "config_reload"),
		zap.String("mode", string(mode)),
		zap.Int("num_slots", numSlots),
	)
}

// LogSizeMultiplierApplied logs when size multiplier is applied
func (l *TimeFilterLogger) LogSizeMultiplierApplied(symbol string, baseSize, multiplier, adjustedSize float64) {
	l.logger.Info("Order size adjusted by time filter",
		zap.String("event", "size_adjusted"),
		zap.String("symbol", symbol),
		zap.Float64("base_size", baseSize),
		zap.Float64("multiplier", multiplier),
		zap.Float64("adjusted_size", adjustedSize),
	)
}

// LogSpreadMultiplierApplied logs when spread multiplier is applied
func (l *TimeFilterLogger) LogSpreadMultiplierApplied(baseSpread, multiplier, adjustedSpread float64) {
	l.logger.Debug("Spread adjusted by time filter",
		zap.String("event", "spread_adjusted"),
		zap.Float64("base_spread", baseSpread),
		zap.Float64("multiplier", multiplier),
		zap.Float64("adjusted_spread", adjustedSpread),
	)
}

// LogCooldownActive logs when a transition is skipped due to cooldown
func (l *TimeFilterLogger) LogCooldownActive(symbol string, remaining time.Duration) {
	l.logger.Info("Slot transition skipped - cooldown active",
		zap.String("event", "transition_cooldown"),
		zap.String("symbol", symbol),
		zap.Duration("remaining", remaining),
	)
}
