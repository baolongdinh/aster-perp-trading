package client

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

// SymbolPrecision tracks exchange rules for a symbol.
type SymbolPrecision struct {
	Symbol       string
	TickSize     float64
	StepSize     float64
	PricePrec    int
	QuantityPrec int
	MaxLeverage  float64
}

// PrecisionManager manages symbol-specific formatting rules.
type PrecisionManager struct {
	mu      sync.RWMutex
	symbols map[string]SymbolPrecision
}

// NewPrecisionManager creates an empty PrecisionManager.
func NewPrecisionManager() *PrecisionManager {
	return &PrecisionManager{
		symbols: make(map[string]SymbolPrecision),
	}
}

// UpdateFromExchangeInfo parses raw exchange info and updates local rules.
func (p *PrecisionManager) UpdateFromExchangeInfo(raw []byte) error {
	var info struct {
		Symbols []struct {
			Symbol  string `json:"symbol"`
			Filters []struct {
				FilterType string `json:"filterType"`
				TickSize   string `json:"tickSize"`
				StepSize   string `json:"stepSize"`
			} `json:"filters"`
			LeverageBrackets []struct {
				Bracket     int     `json:"bracket"`
				NotionalCap float64 `json:"notionalCap"`
				Leverage    float64 `json:"initialLeverage"`
			} `json:"leverageBrackets"`
		} `json:"symbols"`
	}

	if err := json.Unmarshal(raw, &info); err != nil {
		return fmt.Errorf("unmarshal exchange info: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, s := range info.Symbols {
		sp := SymbolPrecision{Symbol: s.Symbol}
		for _, f := range s.Filters {
			if f.FilterType == "PRICE_FILTER" {
				sp.TickSize, _ = strconv.ParseFloat(f.TickSize, 64)
				sp.PricePrec = precisionFromStep(f.TickSize)
			}
			if f.FilterType == "LOT_SIZE" {
				sp.StepSize, _ = strconv.ParseFloat(f.StepSize, 64)
				sp.QuantityPrec = precisionFromStep(f.StepSize)
			}
		}
		// NEW: Parse max leverage from brackets
		for _, b := range s.LeverageBrackets {
			if b.Leverage > sp.MaxLeverage {
				sp.MaxLeverage = b.Leverage
			}
		}
		p.symbols[s.Symbol] = sp
	}
	return nil
}

// RoundPrice rounds a float64 price to the correct tick size and returns a string.
func (p *PrecisionManager) RoundPrice(symbol string, price float64) string {
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return "0"
	}

	p.mu.RLock()
	sp, ok := p.symbols[symbol]
	p.mu.RUnlock()

	if !ok || sp.TickSize == 0 {
		return fmt.Sprintf("%.8f", price) // Fallback
	}

	rounded := math.Round(price/sp.TickSize) * sp.TickSize
	return strconv.FormatFloat(rounded, 'f', sp.PricePrec, 64)
}

// RoundQty rounds a float64 quantity to the correct step size and returns a string.
// Uses Ceil to ensure quantity never rounds down to 0 (critical for high-priced assets like BTC).
func (p *PrecisionManager) RoundQty(symbol string, qty float64) string {
	if math.IsNaN(qty) || math.IsInf(qty, 0) || qty <= 0 {
		return "0"
	}

	p.mu.RLock()
	sp, ok := p.symbols[symbol]
	p.mu.RUnlock()

	if !ok || sp.StepSize == 0 {
		return fmt.Sprintf("%.2f", qty) // Default to 2 decimals for quantity if unknown
	}

	// CRITICAL: Use Ceil to prevent quantity from rounding down to 0
	// For BTC with StepSize=0.001, a small qty like 0.00069 would Floor to 0
	// Ceil ensures we always have at least one step size unit
	rounded := math.Ceil(qty/sp.StepSize) * sp.StepSize
	return strconv.FormatFloat(rounded, 'f', sp.QuantityPrec, 64)
}

// GetMaxLeverage returns the maximum leverage for a symbol.
func (p *PrecisionManager) GetMaxLeverage(symbol string) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if sp, ok := p.symbols[symbol]; ok {
		return sp.MaxLeverage
	}
	return 0 // Unknown
}

// precisionFromStep converts "0.0001" to 4
func precisionFromStep(step string) int {
	if !strings.Contains(step, ".") {
		return 0
	}
	parts := strings.Split(step, ".")
	if len(parts) < 2 {
		return 0
	}
	// Trim trailing zeros
	s := strings.TrimRight(parts[1], "0")
	return len(s)
}
