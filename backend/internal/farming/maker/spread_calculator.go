package maker

import (
	"aster-bot/internal/farming/volume_optimization"
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

	// NEW: VPIN integration for toxic flow detection
	vpinMonitor *volume_optimization.VPINMonitor

	// NEW: OBI integration for orderbook imbalance detection
	obiDetector *volume_optimization.OrderBookImbalanceDetector

	// NEW: Market microstructure analyzer for combined signals
	microAnalyzer *volume_optimization.MarketMicrostructureAnalyzer
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

	// ============================================================
	// NEW: VPIN-AWARE SPREAD ADJUSTMENT
	// ============================================================
	if s.vpinMonitor != nil {
		vpin := s.vpinMonitor.CalculateVPIN()

		// VPIN-based spread adjustment
		if vpin > 0.7 {
			// Extremely toxic - widen spreads significantly
			buySp = math.Max(5, buySp*1.5)   // Minimum 5 bps
			sellSp = math.Max(5, sellSp*1.5) // Minimum 5 bps
		} else if vpin > 0.5 {
			// Moderately toxic - moderate widening
			buySp = math.Max(2, buySp*1.2)   // Minimum 2 bps
			sellSp = math.Max(2, sellSp*1.2) // Minimum 2 bps
		} else if vpin < 0.3 {
			// Healthy flow - tighten for max fills
			buySp = math.Max(0.5, buySp*0.8)   // Minimum 0.5 bps
			sellSp = math.Max(0.5, sellSp*0.8) // Minimum 0.5 bps
		}
	}

	// ============================================================
	// NEW: OBI-AWARE SPREAD ADJUSTMENT
	// ============================================================
	if s.obiDetector != nil {
		obiSignal := s.obiDetector.GetSignal()

		// OBI-based asymmetric spread adjustment
		if obiSignal.BiasDirection == "BUY" && obiSignal.Strength == "STRONG" {
			// More buyers than sellers - tighten sell spread to attract sellers
			sellSp = math.Max(0.5, sellSp*0.7) // Tighten sell by 30%
		} else if obiSignal.BiasDirection == "SELL" && obiSignal.Strength == "STRONG" {
			// More sellers than buyers - tighten buy spread to attract buyers
			buySp = math.Max(0.5, buySp*0.7) // Tighten buy by 30%
		}
	}

	// ============================================================
	// NEW: MICROSTRUCTURE-AWARE SPREAD ADJUSTMENT
	// ============================================================
	if s.microAnalyzer != nil {
		signal := s.microAnalyzer.AnalyzeMarket()

		// Apply microstructure-based spread multiplier
		buySp *= signal.SpreadMultiplier
		sellSp *= signal.SpreadMultiplier

		// Ensure minimum viable spreads
		buySp = math.Max(0.5, buySp)
		sellSp = math.Max(0.5, sellSp)
	}

	// Final clamp to ensure ultra-tight range
	buySp = math.Max(0.5, math.Min(15, buySp))   // Expanded range for toxic conditions
	sellSp = math.Max(0.5, math.Min(15, sellSp)) // Expanded range for toxic conditions

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

// ============================================================
// NEW: VPIN, OBI, and Microstructure Integration Methods
// ============================================================

// SetVPINMonitor wires VPIN monitor for toxic flow detection
func (s *SpreadCalculator) SetVPINMonitor(vpin *volume_optimization.VPINMonitor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vpinMonitor = vpin
}

// SetOBIDetector wires order book imbalance detector
func (s *SpreadCalculator) SetOBIDetector(obi *volume_optimization.OrderBookImbalanceDetector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.obiDetector = obi
}

// SetMicrostructureAnalyzer wires market microstructure analyzer
func (s *SpreadCalculator) SetMicrostructureAnalyzer(analyzer *volume_optimization.MarketMicrostructureAnalyzer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.microAnalyzer = analyzer
}

// GetVPINAwareSpread returns VPIN-adjusted spread for logging/monitoring
func (s *SpreadCalculator) GetVPINAwareSpread(symbol string) (buySpread, sellSpread float64, vpin float64, isToxic bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vpin = 0.0
	isToxic = false

	if s.vpinMonitor != nil {
		vpin = s.vpinMonitor.CalculateVPIN()
		isToxic = vpin > 0.7
	}

	buySpread, sellSpread = s.GetSpreadForSymbol(symbol)
	return buySpread, sellSpread, vpin, isToxic
}

// GetOBIAdjustedSpread returns OBI-adjusted spread for logging/monitoring
func (s *SpreadCalculator) GetOBIAdjustedSpread(symbol string) (buySpread, sellSpread float64, obiSignal volume_optimization.OBISignal) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	obiSignal = volume_optimization.OBISignal{}

	if s.obiDetector != nil {
		obiSignal = s.obiDetector.GetSignal()
	}

	buySpread, sellSpread = s.GetSpreadForSymbol(symbol)
	return buySpread, sellSpread, obiSignal
}
