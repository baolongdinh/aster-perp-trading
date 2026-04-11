package agentic

import (
	"time"
)

// RegimeType represents the market regime classification
type RegimeType string

const (
	RegimeSideways RegimeType = "SIDEWAYS"
	RegimeTrending RegimeType = "TRENDING"
	RegimeVolatile RegimeType = "VOLATILE"
	RegimeRecovery RegimeType = "RECOVERY"
)

// Recommendation represents the trading recommendation based on score
type Recommendation string

const (
	RecHigh   Recommendation = "HIGH"    // Score >= 75 - Deploy full
	RecMedium Recommendation = "MEDIUM"  // Score 60-75 - Deploy reduced
	RecLow    Recommendation = "LOW"     // Score 40-60 - Monitor only
	RecSkip   Recommendation = "SKIP"    // Score < 40 - Skip
)

// SymbolScore contains the evaluation result for a symbol
type SymbolScore struct {
	Symbol           string
	Score            float64            // 0-100 overall score
	Regime           RegimeType         // Current regime
	Confidence       float64            // Regime detection confidence (0-1)
	Factors          map[string]float64 // Individual factor scores
	LastUpdated      time.Time
	Recommendation   Recommendation
	RawADX           float64
	RawATR14         float64
	RawBBWidth       float64
}

// RegimeSnapshot captures the current regime state
type RegimeSnapshot struct {
	Regime      RegimeType
	ADX         float64
	ATR14       float64
	BBWidth     float64
	Volume24h   float64
	Timestamp   time.Time
	Confidence  float64
}

// IndicatorValues holds calculated indicator values
type IndicatorValues struct {
	ADX          float64
	ATR14        float64
	BBUpper      float64
	BBLower      float64
	BBMiddle     float64
	BBWidth      float64
	Volume24h    float64
	PriceChange  float64
}

// Candle represents a price candle for technical analysis
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp time.Time
}

// WhitelistDecision represents the whitelist management decision
type WhitelistDecision struct {
	AddSymbols    []string
	RemoveSymbols []string
	KeepSymbols   []string // Symbols to keep despite low scores (due to open positions)
	Reasoning     map[string]string // Why each symbol was added/removed/kept
}

// DetectionResult holds the complete detection output for all symbols
type DetectionResult struct {
	Scores      map[string]SymbolScore
	UpdatedAt   time.Time
	BestSymbols []string // Sorted by score descending
}

// PositionStatus represents the current position state for a symbol
type PositionStatus struct {
	Symbol        string
	HasPosition   bool
	Size          float64
	UnrealizedPnL float64
	Side          string // LONG or SHORT
}
