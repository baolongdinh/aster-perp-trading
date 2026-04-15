# Data Model: Circuit Breaker as "Brain"

## Overview

This document describes the data model for the unified CircuitBreaker component that serves as the "brain" for per-symbol trading decisions.

## Core Entities

### SymbolDecisionState

Represents the complete decision state for a single symbol.

```go
type SymbolDecisionState struct {
    // Circuit breaker state
    isTripped        bool          // Whether trading is blocked for this symbol
    tripTime         time.Time     // When the breaker was last tripped
    reason           string        // Reason for trip (e.g., "volatility_spike", "consecutive_losses")
    
    // Trading mode decision
    tradingMode      TradingMode   // Current trading mode (MICRO, STANDARD, TREND_ADAPTED, COOLDOWN)
    modeSince        time.Time     // When current mode was set
    
    // Market condition tracking (for evaluation)
    atrHistory       []float64     // ATR values for volatility analysis (last 20)
    bbWidthHistory   []float64     // BB width values for range analysis (last 20)
    priceHistory     []float64     // Price values for stability analysis (last 20)
    volumeHistory    []float64     // Volume values for spike analysis (last 20)
    adxHistory       []float64     // ADX values for trend analysis (last 20)
    
    // Consecutive loss tracking
    consecutiveLosses int          // Number of consecutive losses
    lastTradeOutcome  string        // Last trade outcome ("win" or "loss")
}
```

**Validation Rules:**
- `isTripped` defaults to `false`
- `tradingMode` defaults to `TradingModeUnknown`
- All history arrays limited to 20 elements
- `consecutiveLosses` must be >= 0

**State Transitions:**
```
isTripped: false → true (when market conditions trigger trip)
isTripped: true → false (when market conditions stabilize)

tradingMode: Unknown → MICRO (initial mode)
tradingMode: MICRO → STANDARD (range active, ADX < 25)
tradingMode: MICRO → TREND_ADAPTED (trending, ADX > 25)
tradingMode: Any → COOLDOWN (volatility spike or consecutive losses)
tradingMode: COOLDOWN → MICRO (cooldown expired)
```

---

### CircuitBreaker

Container for all symbol decision states and evaluation logic.

```go
type CircuitBreaker struct {
    // Configuration
    config             config.AgenticCircuitBreakerConfig
    logger             *zap.Logger
    
    // State management
    mu                 sync.RWMutex
    symbolStates       map[string]*SymbolDecisionState
    maxHistorySize     int           // Max history size (default 20)
    
    // Evaluation worker
    evaluationInterval time.Duration // How often to evaluate (default 3s)
    stopCh             chan struct{}
    
    // Callbacks
    onTripCallback      func(symbol, reason string)      // Called when breaker trips
    onResetCallback     func(symbol string)              // Called when breaker resets
    onModeChangeCallback func(symbol string, oldMode, newMode TradingMode) // Called when mode changes
}
```

**Validation Rules:**
- `symbolStates` map must be initialized
- `evaluationInterval` must be >= 1 second
- `maxHistorySize` must be >= 5

**Lifecycle:**
```
NewCircuitBreaker() → Start() → [evaluationLoop running] → Stop()
```

---

### TradingMode (Enum)

Trading mode for a symbol.

```go
type TradingMode string

const (
    TradingModeUnknown      TradingMode = "UNKNOWN"
    TradingModeMicro        TradingMode = "MICRO"
    TradingModeStandard     TradingMode = "STANDARD"
    TradingModeTrendAdapted TradingMode = "TREND_ADAPTED"
    TradingModeCooldown     TradingMode = "COOLDOWN"
)
```

**Mode Characteristics:**

| Mode | Description | Grid Spacing | Risk Level |
|------|-------------|--------------|------------|
| MICRO | Small grid, ATR bands | 0.05% | Low |
| STANDARD | Normal grid, BB active | 0.1% | Medium |
| TREND_ADAPTED | Wide grid, trending | 0.2% | High |
| COOLDOWN | No trading | N/A | None |

---

## Relationships

```
CircuitBreaker
    └── map[string]*SymbolDecisionState (1:N)
        ├── Symbol A
        ├── Symbol B
        └── Symbol C
```

**Cardinality:**
- One CircuitBreaker → Many SymbolDecisionState (1:N)
- Each symbol → One SymbolDecisionState (1:1)

---

## Data Flow

### Evaluation Flow

```
1. Evaluation Loop (every 3s)
   └── For each symbol:
       ├── Get market conditions (ATR, BB, price, volume, ADX)
       ├── Call evaluateSymbol(symbol, state)
       │   ├── Check breaker conditions (ATR, BB, price, volume, ADX)
       │   ├── Check mode conditions (range, trend, volatility)
       │   └── Return (canTrade, tradingMode)
       ├── Update state.isTripped = !canTrade
       ├── Update state.tradingMode = mode
       └── Trigger callbacks if state changed
```

### Trade Decision Flow

```
1. AdaptiveGridManager.CanPlaceOrder(symbol)
   └── Call circuitBreaker.GetSymbolDecision(symbol)
       └── Return (canTrade, tradingMode)
   └── If !canTrade → return false
   └── If canTrade → use tradingMode for placement
```

---

## Persistence

**In-memory only** - No persistence required. State is reconstructed on restart.

---

## Performance Considerations

### Memory Usage
- O(n) where n = number of symbols
- Each symbol: ~1KB (20 history arrays × 5 metrics × 8 bytes)
- Example: 100 symbols = ~100KB

### Concurrency
- All state updates protected by mutex
- Evaluation worker reads/writes state
- Trade logic reads state (read lock)
- Callbacks triggered under write lock

---

## Migration Path

### From Current Architecture

**Before:**
```
ModeManager (global) + CircuitBreaker (per-symbol)
```

**After:**
```
CircuitBreaker (per-symbol, includes mode decision)
ModeManager (deprecated)
```

**Migration Steps:**
1. Add mode fields to SymbolDecisionState
2. Copy mode logic to CircuitBreaker
3. Update consumers to use CircuitBreaker.GetSymbolDecision()
4. Deprecate ModeManager

---

## Example State

```go
// Symbol: BTCUSD1
state := &SymbolDecisionState{
    isTripped: false,
    tripTime: time.Time{},
    reason: "",
    tradingMode: TradingModeMicro,
    modeSince: time.Now().Add(-5 * time.Minute),
    
    atrHistory: []float64{0.003, 0.0032, 0.0031, ...},
    bbWidthHistory: []float64{0.008, 0.0085, 0.0082, ...},
    priceHistory: []float64{50000, 50050, 50030, ...},
    volumeHistory: []float64{1000, 1200, 1100, ...},
    adxHistory: []float64{18, 19, 18.5, ...},
    
    consecutiveLosses: 0,
    lastTradeOutcome: "win",
}
```

**Interpretation:**
- Not tripped → can trade
- In MICRO mode for 5 minutes
- Low volatility (ATR 0.3%)
- Tight range (BB width 0.8%)
- No consecutive losses
