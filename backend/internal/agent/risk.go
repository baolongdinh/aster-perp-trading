package agent

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CircuitBreaker defines the interface for all circuit breakers
type CircuitBreaker interface {
	Check(ctx context.Context) (*CircuitBreakerEvent, bool)
	Reset()
	IsEnabled() bool
	GetType() BreakerType
}

// DefaultCircuitBreakerManager is the default implementation of CircuitBreakerManager
type DefaultCircuitBreakerManager struct {
	breakers map[BreakerType]CircuitBreaker
	mu       sync.RWMutex
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager(config *CircuitBreakersConfig) CircuitBreakerManager {
	m := &DefaultCircuitBreakerManager{
		breakers: make(map[BreakerType]CircuitBreaker),
	}

	if config.VolatilitySpike.Enabled {
		m.breakers[BreakerVolatility] = NewVolatilityBreaker(&config.VolatilitySpike)
	}
	if config.LiquidityCrisis.Enabled {
		m.breakers[BreakerLiquidity] = NewLiquidityBreaker(&config.LiquidityCrisis)
	}
	if config.ConsecutiveLosses.Enabled {
		m.breakers[BreakerLosses] = NewLossesBreaker(&config.ConsecutiveLosses)
	}
	if config.DrawdownLimit.Enabled {
		m.breakers[BreakerDrawdown] = NewDrawdownBreaker(&config.DrawdownLimit)
	}
	if config.ConnectionFailure.Enabled {
		m.breakers[BreakerConnection] = NewConnectionBreaker(&config.ConnectionFailure)
	}

	return m
}

// Check runs all circuit breakers and returns the first triggered event
func (m *DefaultCircuitBreakerManager) Check(ctx context.Context) (*CircuitBreakerEvent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Priority order: Drawdown > Volatility > Liquidity > Losses > Connection
	priority := []BreakerType{
		BreakerDrawdown,
		BreakerVolatility,
		BreakerLiquidity,
		BreakerLosses,
		BreakerConnection,
	}

	for _, bt := range priority {
		if breaker, ok := m.breakers[bt]; ok {
			if event, triggered := breaker.Check(ctx); triggered {
				return event, true
			}
		}
	}

	return nil, false
}

// Reset resets a specific circuit breaker
func (m *DefaultCircuitBreakerManager) Reset(breakerType BreakerType) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if breaker, ok := m.breakers[breakerType]; ok {
		breaker.Reset()
	}
}

// GetStatus returns the status of all breakers
func (m *DefaultCircuitBreakerManager) GetStatus() map[BreakerType]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[BreakerType]bool)
	for bt, breaker := range m.breakers {
		status[bt] = breaker.IsEnabled()
	}
	return status
}

// VolatilityBreaker triggers on ATR spikes
type VolatilityBreaker struct {
	config      *VolatilityBreakerConfig
	atrHistory  []float64
	lastTrigger time.Time
	mu          sync.Mutex
}

// NewVolatilityBreaker creates a new volatility circuit breaker
func NewVolatilityBreaker(config *VolatilityBreakerConfig) *VolatilityBreaker {
	return &VolatilityBreaker{
		config:     config,
		atrHistory: make([]float64, 0, 100),
	}
}

// Check implements CircuitBreaker
func (b *VolatilityBreaker) Check(ctx context.Context) (*CircuitBreakerEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.atrHistory) < 20 {
		return nil, false
	}

	currentATR := b.atrHistory[len(b.atrHistory)-1]

	// Calculate average ATR (excluding recent)
	var avgATR float64
	for i := 0; i < len(b.atrHistory)-5; i++ {
		avgATR += b.atrHistory[i]
	}
	avgATR /= float64(len(b.atrHistory) - 5)

	if avgATR == 0 {
		return nil, false
	}

	if currentATR/avgATR >= b.config.ATRMultiplier {
		return &CircuitBreakerEvent{
			ID:           uuid.New(),
			BreakerType:  BreakerVolatility,
			TriggerValue: currentATR,
			Threshold:    avgATR * b.config.ATRMultiplier,
			TriggeredAt:  time.Now(),
			ActionTaken:  "Emergency close all positions",
		}, true
	}

	return nil, false
}

// Reset implements CircuitBreaker
func (b *VolatilityBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.atrHistory = b.atrHistory[:0]
}

// IsEnabled implements CircuitBreaker
func (b *VolatilityBreaker) IsEnabled() bool {
	return b.config.Enabled
}

// GetType implements CircuitBreaker
func (b *VolatilityBreaker) GetType() BreakerType {
	return BreakerVolatility
}

// UpdateATR adds a new ATR value
func (b *VolatilityBreaker) UpdateATR(atr float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.atrHistory = append(b.atrHistory, atr)
	if len(b.atrHistory) > 100 {
		b.atrHistory = b.atrHistory[1:]
	}
}

// LiquidityBreaker triggers on spread widening
type LiquidityBreaker struct {
	config  *LiquidityBreakerConfig
	spreads []float64
	mu      sync.Mutex
}

// NewLiquidityBreaker creates a new liquidity circuit breaker
func NewLiquidityBreaker(config *LiquidityBreakerConfig) *LiquidityBreaker {
	return &LiquidityBreaker{
		config:  config,
		spreads: make([]float64, 0, 50),
	}
}

// Check implements CircuitBreaker
func (b *LiquidityBreaker) Check(ctx context.Context) (*CircuitBreakerEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.spreads) < 10 {
		return nil, false
	}

	currentSpread := b.spreads[len(b.spreads)-1]

	// Calculate average spread
	var avgSpread float64
	for _, s := range b.spreads[:len(b.spreads)-5] {
		avgSpread += s
	}
	avgSpread /= float64(len(b.spreads) - 5)

	if avgSpread == 0 {
		return nil, false
	}

	if currentSpread/avgSpread >= b.config.SpreadMultiplier {
		return &CircuitBreakerEvent{
			ID:           uuid.New(),
			BreakerType:  BreakerLiquidity,
			TriggerValue: currentSpread,
			Threshold:    avgSpread * b.config.SpreadMultiplier,
			TriggeredAt:  time.Now(),
			ActionTaken:  "Pause new orders",
		}, true
	}

	return nil, false
}

// Reset implements CircuitBreaker
func (b *LiquidityBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spreads = b.spreads[:0]
}

// IsEnabled implements CircuitBreaker
func (b *LiquidityBreaker) IsEnabled() bool {
	return b.config.Enabled
}

// GetType implements CircuitBreaker
func (b *LiquidityBreaker) GetType() BreakerType {
	return BreakerLiquidity
}

// UpdateSpread adds a new spread value
func (b *LiquidityBreaker) UpdateSpread(spread float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spreads = append(b.spreads, spread)
	if len(b.spreads) > 50 {
		b.spreads = b.spreads[1:]
	}
}

// LossesBreaker triggers on consecutive losses
type LossesBreaker struct {
	config       *LossesBreakerConfig
	lossCount    int
	lastTradePnL float64
	mu           sync.Mutex
}

// NewLossesBreaker creates a new losses circuit breaker
func NewLossesBreaker(config *LossesBreakerConfig) *LossesBreaker {
	return &LossesBreaker{
		config: config,
	}
}

// Check implements CircuitBreaker
func (b *LossesBreaker) Check(ctx context.Context) (*CircuitBreakerEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.lossCount >= b.config.Threshold {
		return &CircuitBreakerEvent{
			ID:           uuid.New(),
			BreakerType:  BreakerLosses,
			TriggerValue: float64(b.lossCount),
			Threshold:    float64(b.config.Threshold),
			TriggeredAt:  time.Now(),
			ActionTaken:  "Reduce position size by 50%",
		}, true
	}

	return nil, false
}

// Reset implements CircuitBreaker
func (b *LossesBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lossCount = 0
}

// IsEnabled implements CircuitBreaker
func (b *LossesBreaker) IsEnabled() bool {
	return b.config.Enabled
}

// GetType implements CircuitBreaker
func (b *LossesBreaker) GetType() BreakerType {
	return BreakerLosses
}

// RecordTrade records a trade outcome
func (b *LossesBreaker) RecordTrade(pnl float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if pnl < 0 {
		b.lossCount++
	} else {
		b.lossCount = 0
	}
}

// DrawdownBreaker triggers on portfolio drawdown
type DrawdownBreaker struct {
	config       *DrawdownBreakerConfig
	peakValue    float64
	currentValue float64
	mu           sync.Mutex
}

// NewDrawdownBreaker creates a new drawdown circuit breaker
func NewDrawdownBreaker(config *DrawdownBreakerConfig) *DrawdownBreaker {
	return &DrawdownBreaker{
		config: config,
	}
}

// Check implements CircuitBreaker
func (b *DrawdownBreaker) Check(ctx context.Context) (*CircuitBreakerEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.peakValue == 0 {
		return nil, false
	}

	drawdown := (b.peakValue - b.currentValue) / b.peakValue
	if drawdown >= b.config.MaxDrawdown {
		return &CircuitBreakerEvent{
			ID:           uuid.New(),
			BreakerType:  BreakerDrawdown,
			TriggerValue: drawdown,
			Threshold:    b.config.MaxDrawdown,
			TriggeredAt:  time.Now(),
			ActionTaken:  "Stop all strategies",
		}, true
	}

	return nil, false
}

// Reset implements CircuitBreaker
func (b *DrawdownBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.peakValue = 0
	b.currentValue = 0
}

// IsEnabled implements CircuitBreaker
func (b *DrawdownBreaker) IsEnabled() bool {
	return b.config.Enabled
}

// GetType implements CircuitBreaker
func (b *DrawdownBreaker) GetType() BreakerType {
	return BreakerDrawdown
}

// UpdatePortfolio updates portfolio value
func (b *DrawdownBreaker) UpdatePortfolio(value float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if value > b.peakValue {
		b.peakValue = value
	}
	b.currentValue = value
}

// ConnectionBreaker triggers on connection failures
type ConnectionBreaker struct {
	config      *ConnectionBreakerConfig
	failures    int
	lastFailure time.Time
	mu          sync.Mutex
}

// NewConnectionBreaker creates a new connection circuit breaker
func NewConnectionBreaker(config *ConnectionBreakerConfig) *ConnectionBreaker {
	return &ConnectionBreaker{
		config: config,
	}
}

// Check implements CircuitBreaker
func (b *ConnectionBreaker) Check(ctx context.Context) (*CircuitBreakerEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.failures >= b.config.MaxFailures {
		return &CircuitBreakerEvent{
			ID:           uuid.New(),
			BreakerType:  BreakerConnection,
			TriggerValue: float64(b.failures),
			Threshold:    float64(b.config.MaxFailures),
			TriggeredAt:  time.Now(),
			ActionTaken:  "Pause and alert",
		}, true
	}

	return nil, false
}

// Reset implements CircuitBreaker
func (b *ConnectionBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
}

// IsEnabled implements CircuitBreaker
func (b *ConnectionBreaker) IsEnabled() bool {
	return b.config.Enabled
}

// GetType implements CircuitBreaker
func (b *ConnectionBreaker) GetType() BreakerType {
	return BreakerConnection
}

// RecordFailure records a connection failure
func (b *ConnectionBreaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	b.lastFailure = time.Now()
}

// RecordSuccess resets failure count on successful connection
func (b *ConnectionBreaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
}
