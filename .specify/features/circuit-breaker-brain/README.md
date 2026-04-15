# Circuit Breaker as Brain

## Overview

This feature refactors the trading bot's decision-making architecture by merging `ModeManager` and `CircuitBreaker` into a single, unified "brain" within the `CircuitBreaker`. The new `CircuitBreaker` is responsible for making per-symbol trading decisions based on continuous market condition evaluation.

## Architecture

### Before
- **ModeManager**: Managed trading mode transitions (MICRO, STANDARD, TREND_ADAPTED, COOLDOWN)
- **CircuitBreaker**: Managed per-symbol breaker state (trip/reset logic)
- **AdaptiveGridManager**: Called both ModeManager and CircuitBreaker separately

### After
- **CircuitBreaker**: Unified decision engine that combines:
  - Circuit breaker state (isTripped, tripTime, reason)
  - Trading mode decision (MICRO, STANDARD, TREND_ADAPTED, COOLDOWN)
  - Market condition histories (ATR, BB width, price, volume, ADX)
- **AdaptiveGridManager**: Calls `CircuitBreaker.GetSymbolDecision(symbol)` for unified trading decisions

## Key Changes

### 1. CircuitBreaker SymbolDecisionState
```go
type SymbolDecisionState struct {
    // Circuit breaker state
    isTripped         bool
    tripTime          time.Time
    reason            string
    consecutiveLosses int
    
    // Trading mode decision (NEW)
    tradingMode string  // "MICRO", "STANDARD", "TREND_ADAPTED", "COOLDOWN"
    modeSince  time.Time
    
    // Market condition tracking
    atrHistory        []float64
    bbWidthHistory    []float64
    priceHistory      []float64
    volumeHistory     []float64
    adxHistory        []float64
}
```

### 2. Unified Decision API
```go
// GetSymbolDecision returns (canTrade, tradingMode)
func (cb *CircuitBreaker) GetSymbolDecision(symbol string) (bool, string)
```

### 3. Mode Decision Logic
The `determineTradingMode()` method in CircuitBreaker implements the same logic as ModeManager:
- Volatility spike → COOLDOWN
- Breakout with momentum → COOLDOWN
- Breakout without momentum → MICRO
- Strong trend → TREND_ADAPTED
- Range active + low ADX → STANDARD
- Default → MICRO

### 4. Integration Points
- **VolumeFarmEngine**: Initializes CircuitBreaker and passes to AdaptiveGridManager
- **AdaptiveGridManager.CanPlaceOrder()**: Calls `CircuitBreaker.GetSymbolDecision(symbol)` instead of ModeManager
- **ModeManager**: Deprecated but kept for backward compatibility

## Migration Status

### Completed (Phase 3-4)
- ✅ ModeManager refactored to per-symbol mode management
- ✅ CircuitBreaker.SymbolDecisionState with mode fields
- ✅ CircuitBreaker.determineTradingMode() method
- ✅ CircuitBreaker.evaluateSymbol() method
- ✅ CircuitBreaker.GetSymbolDecision() unified API
- ✅ AdaptiveGridManager updated to use CircuitBreaker
- ✅ VolumeFarmEngine wired to CircuitBreaker

### Skipped (Future Work)
- ⏸️ Phase 5: Evaluation worker to call evaluateSymbol() (requires market condition data integration)
- ⏸️ Phase 6: Placement logic to use mode from GetSymbolDecision() (optional enhancement)
- ⏸️ Phase 7: Documentation updates (README, ARCHITECTURE.md)

## Usage

### Basic Usage
```go
// Get trading decision for a symbol
canTrade, tradingMode := circuitBreaker.GetSymbolDecision("BTCUSD1")

if !canTrade {
    // Circuit breaker tripped or in COOLDOWN mode
    return
}

// Use tradingMode for placement logic
switch tradingMode {
case "MICRO":
    // Use micro grid sizing
case "STANDARD":
    // Use standard grid sizing
case "TREND_ADAPTED":
    // Use trend-adapted sizing
}
```

### Setting Callbacks
```go
// Trip callback - when circuit breaker trips
circuitBreaker.SetOnTripCallback(func(symbol string, reason string) {
    logger.Warn("Circuit breaker tripped", 
        zap.String("symbol", symbol), 
        zap.String("reason", reason))
})

// Reset callback - when circuit breaker resets
circuitBreaker.SetOnResetCallback(func(symbol string) {
    logger.Info("Circuit breaker reset", zap.String("symbol", symbol))
})

// Mode change callback - when trading mode changes
circuitBreaker.SetOnModeChangeCallback(func(symbol string, oldMode, newMode string) {
    logger.Info("Trading mode changed",
        zap.String("symbol", symbol),
        zap.String("oldMode", oldMode),
        zap.String("newMode", newMode))
})
```

## Backward Compatibility

ModeManager is deprecated but kept for backward compatibility:
- All global methods still work (`GetCurrentModeGlobal`, `EvaluateModeGlobal`, etc.)
- Per-symbol methods added (`GetCurrentMode(symbol)`, `EvaluateModeSymbol(symbol, ...)`)
- ModeManager can be removed in future after full migration

## Testing

- ✅ Per-symbol mode management tests (`mode_manager_test.go`)
- ✅ CircuitBreaker mode decision tests (`agentic_test.go`)
- ✅ Backward compatibility tests

## Configuration

CircuitBreaker uses `AgenticCircuitBreakerConfig` from config:
```yaml
agentic:
  circuit_breakers:
    volatility_spike:
      enabled: true
      atr_multiplier: 3.0
    consecutive_losses:
      enabled: true
      threshold: 3
      size_reduction: 0.5
```
