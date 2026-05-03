package maker

import (
	"math"
	"sync"
)

type SpreadCalculator struct {
	config          *Config
	positionManager InventoryManager
	mu              sync.RWMutex
	symbolsSpread   map[string]struct {
		buySpread  float64
		sellSpread float64
	}
}

func NewSpreadCalculator(config *Config, positionManager InventoryManager) *SpreadCalculator {
	return &SpreadCalculator{
		config:          config,
		positionManager: positionManager,
		symbolsSpread: make(map[string]struct {
			buySpread  float64
			sellSpread float64
		}),
	}
}

func (s *SpreadCalculator) GetSpreadForSymbol(symbol string) (buySpread, sellSpread float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if spread, ok := s.symbolsSpread[symbol]; ok {
		return spread.buySpread, spread.sellSpread
	}

	baseSpread := s.config.DefaultSpreadBps
	return baseSpread, baseSpread
}

func (s *SpreadCalculator) CalculateDynamicSpread(symbol string, baseSpreadBps float64) (buySpread, sellSpread float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ultra-tight micro-profit farming: 1-2 bps
	// Clamp to ultra-tight range first
	baseSpreadBps = s.ClampSpread(baseSpreadBps)

	pos := s.positionManager.GetPosition(symbol)
	if pos == nil || math.Abs(pos.Amount) < 0.001 || pos.MarkPrice == 0 {
		// No position: Use ultra-tight base spread
		s.symbolsSpread[symbol] = struct {
			buySpread  float64
			sellSpread float64
		}{buySpread: baseSpreadBps, sellSpread: baseSpreadBps}
		return baseSpreadBps, baseSpreadBps
	}

	maxPosition := s.config.MaxPositionUSDT / pos.MarkPrice
	if maxPosition == 0 {
		maxPosition = 1
	}

	deviation := pos.Amount / maxPosition
	threshold := s.config.RebalanceThreshold

	var buySp, sellSp float64

	// Micro-profit optimized: Very tight spreads with minimal adjustment
	if deviation > threshold {
		// Long bias: Tighten buy spread (aggressive), loosen sell slightly
		buySp = math.Max(1, baseSpreadBps*0.8)   // Minimum 1 bps
		sellSp = math.Min(10, baseSpreadBps*1.2) // Maximum 10 bps
	} else if deviation < -threshold {
		// Short bias: Loosen buy, tighten sell
		buySp = math.Min(10, baseSpreadBps*1.2) // Maximum 10 bps
		sellSp = math.Max(1, baseSpreadBps*0.8) // Minimum 1 bps
	} else {
		// Balanced: Ultra-tight both sides for max fill rate
		buySp = math.Max(1, baseSpreadBps*0.9)  // Slightly tighter
		sellSp = math.Max(1, baseSpreadBps*0.9) // Slightly tighter
	}

	// Final clamp to ensure ultra-tight range
	buySp = math.Max(1, math.Min(10, buySp))
	sellSp = math.Max(1, math.Min(10, sellSp))

	s.symbolsSpread[symbol] = struct {
		buySpread  float64
		sellSpread float64
	}{buySpread: buySp, sellSpread: sellSp}

	return buySp, sellSp
}

func (s *SpreadCalculator) CalculateLimitPrices(midPrice, buySpread, sellSpread float64) (buyPrice, sellPrice float64) {
	buyPrice = midPrice * (1 - buySpread/10000)
	sellPrice = midPrice * (1 + sellSpread/10000)
	return buyPrice, sellPrice
}

func (s *SpreadCalculator) GetMinSpreadBps() float64 {
	return 1 // Ultra-tight: 0.01% minimum
}

func (s *SpreadCalculator) GetMaxSpreadBps() float64 {
	return 10 // Conservative: 0.1% maximum for micro-profit
}

func (s *SpreadCalculator) ClampSpread(spreadBps float64) float64 {
	return math.Max(s.GetMinSpreadBps(), math.Min(s.GetMaxSpreadBps(), spreadBps))
}
