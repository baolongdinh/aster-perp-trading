package maker

import (
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

type InventoryManagerImpl struct {
	config   *Config
	logger   *zap.Logger
	positions map[string]*PositionState
	mu       sync.RWMutex
}

func NewInventoryManager(config *Config, logger *zap.Logger) *InventoryManagerImpl {
	return &InventoryManagerImpl{
		config:    config,
		logger:    logger,
		positions: make(map[string]*PositionState),
	}
}

func (m *InventoryManagerImpl) UpdatePosition(symbol string, amount float64, entryPrice, markPrice float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if math.Abs(amount) < 0.001 {
		delete(m.positions, symbol)
		return
	}

	if existing, ok := m.positions[symbol]; ok {
		totalQty := existing.Amount + amount
		if math.Abs(totalQty) < 0.001 {
			delete(m.positions, symbol)
			return
		}
		existing.EntryPrice = (existing.EntryPrice*existing.Amount + entryPrice*amount) / totalQty
		existing.Amount = totalQty
		existing.MarkPrice = markPrice
		existing.UnrealizedPNL = (markPrice - existing.EntryPrice) * existing.Amount
		existing.UpdatedAt = time.Now()
	} else {
		m.positions[symbol] = &PositionState{
			Symbol:        symbol,
			Amount:        amount,
			EntryPrice:    entryPrice,
			MarkPrice:     markPrice,
			UnrealizedPNL: 0,
			UpdatedAt:     time.Now(),
		}
	}
}

func (m *InventoryManagerImpl) GetPosition(symbol string) *PositionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if pos, ok := m.positions[symbol]; ok {
		return pos
	}
	return nil
}

func (m *InventoryManagerImpl) GetAllPositions() map[string]*PositionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*PositionState, len(m.positions))
	for k, v := range m.positions {
		result[k] = v
	}
	return result
}

func (m *InventoryManagerImpl) GetNetExposure() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var netExposure float64
	for _, pos := range m.positions {
		if pos.MarkPrice > 0 {
			netExposure += pos.Amount * pos.MarkPrice
		}
	}
	return math.Abs(netExposure)
}

func (m *InventoryManagerImpl) GetLongExposure() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var longExposure float64
	for _, pos := range m.positions {
		if pos.Amount > 0 && pos.MarkPrice > 0 {
			longExposure += pos.Amount * pos.MarkPrice
		}
	}
	return longExposure
}

func (m *InventoryManagerImpl) GetShortExposure() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var shortExposure float64
	for _, pos := range m.positions {
		if pos.Amount < 0 && pos.MarkPrice > 0 {
			shortExposure += math.Abs(pos.Amount) * pos.MarkPrice
		}
	}
	return shortExposure
}

func (m *InventoryManagerImpl) GetInventoryState() InventoryState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state := InventoryState{
		MaxExposure: m.config.MaxTotalExposureUSDT,
	}

	for _, pos := range m.positions {
		if pos.MarkPrice > 0 {
			exposure := pos.Amount * pos.MarkPrice
			if pos.Amount > 0 {
				state.LongExposure += exposure
			} else {
				state.ShortExposure += math.Abs(exposure)
			}
		}
	}

	state.NetExposure = state.LongExposure - state.ShortExposure
	return state
}

func (m *InventoryManagerImpl) ShouldRebalance(symbol string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pos, ok := m.positions[symbol]
	if !ok || math.Abs(pos.Amount) < 0.001 || pos.MarkPrice == 0 {
		return false
	}

	maxPosition := m.config.MaxPositionUSDT / pos.MarkPrice
	if maxPosition == 0 {
		return false
	}

	deviation := math.Abs(pos.Amount) / maxPosition
	return deviation > m.config.RebalanceThreshold
}

func (m *InventoryManagerImpl) CalculateTargetSpread(symbol string, baseSpreadBps float64) (buySpread, sellSpread float64) {
	m.mu.RLock()
	pos, ok := m.positions[symbol]
	m.mu.RUnlock()

	if !ok || math.Abs(pos.Amount) < 0.001 || pos.MarkPrice == 0 {
		return baseSpreadBps, baseSpreadBps
	}

	maxPosition := m.config.MaxPositionUSDT / pos.MarkPrice
	if maxPosition == 0 {
		return baseSpreadBps, baseSpreadBps
	}

	deviation := pos.Amount / maxPosition
	threshold := m.config.RebalanceThreshold

	if deviation > threshold {
		buySpread = baseSpreadBps * 1.5
		sellSpread = baseSpreadBps * 0.5
		m.logger.Debug("Inventory long bias - widening buy, narrowing sell",
			zap.String("symbol", symbol),
			zap.Float64("deviation", deviation),
			zap.Float64("buy_spread", buySpread),
			zap.Float64("sell_spread", sellSpread))
	} else if deviation < -threshold {
		buySpread = baseSpreadBps * 0.5
		sellSpread = baseSpreadBps * 1.5
		m.logger.Debug("Inventory short bias - narrowing buy, widening sell",
			zap.String("symbol", symbol),
			zap.Float64("deviation", deviation),
			zap.Float64("buy_spread", buySpread),
			zap.Float64("sell_spread", sellSpread))
	} else {
		buySpread = baseSpreadBps
		sellSpread = baseSpreadBps
	}

	return buySpread, sellSpread
}

func (m *InventoryManagerImpl) GetTotalUnrealizedPNL() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var totalPNL float64
	for _, pos := range m.positions {
		totalPNL += pos.UnrealizedPNL
	}
	return totalPNL
}

func (m *InventoryManagerImpl) ClearPositions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.positions = make(map[string]*PositionState)
}
