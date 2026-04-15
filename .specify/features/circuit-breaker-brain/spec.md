# Feature Spec: Circuit Breaker as "Brain" - Unified Decision Engine

## Problem Statement

Currently, the trading bot has two separate decision-making components:
1. **ModeManager** (global): Decides trading mode (MICRO, STANDARD, TREND_ADAPTED, COOLDOWN) for ALL symbols
2. **CircuitBreaker** (per-symbol): Decides if a symbol can trade (tripped/not tripped)

This creates several problems:
- **No per-symbol mode granularity**: Cannot have symbol A in MICRO while symbol B in STANDARD
- **Two separate "brains"**: Trade logic must check both components independently
- **Slow detection cycle**: AgenticEngine runs every 30s, not real-time
- **Complex integration**: Trade logic scattered across multiple components

## Solution

Merge ModeManager and CircuitBreaker into a unified "brain" component that:
1. Makes per-symbol decisions (can trade? what mode?)
2. Runs continuous evaluation worker (3s interval)
3. Provides single API: `GetSymbolDecision(symbol) (canTrade bool, mode TradingMode)`

## Requirements

### Functional Requirements

#### FR1: Per-Symbol Decision State
- Each symbol MUST have independent decision state
- State includes: `isTripped`, `tradingMode`, `tripTime`, `modeSince`, `reason`
- State MUST be thread-safe (mutex protected)

#### FR2: Unified Decision API
- CircuitBreaker MUST provide `GetSymbolDecision(symbol) (canTrade bool, mode TradingMode)`
- Trade logic MUST use only this API (no ModeManager calls)
- API MUST be thread-safe for concurrent access

#### FR3: Continuous Evaluation Worker
- Evaluation worker MUST run every 3 seconds
- Worker MUST evaluate all tracked symbols
- Worker MUST update both breaker state and trading mode
- Worker MUST trigger callbacks on state changes

#### FR4: Market Condition Evaluation
- Worker MUST evaluate: ATR, BB width, price stability, volume, ADX
- Worker MUST decide `canTrade` based on market conditions
- Worker MUST decide `tradingMode` based on market conditions
- Evaluation logic MUST match existing ModeManager.EvaluateMode() behavior

#### FR5: Callback System
- CircuitBreaker MUST support `onTripCallback(symbol, reason)` for emergency exit
- CircuitBreaker MUST support `onResetCallback(symbol)` for force placement
- CircuitBreaker MUST support `onModeChangeCallback(symbol, oldMode, newMode)` for mode transitions

#### FR6: Backward Compatibility
- ModeManager MUST be kept but deprecated
- ModeManager methods MUST still work for existing integrations
- Migration path MUST be clear for consumers

### Non-Functional Requirements

#### NFR1: Performance
- Evaluation worker MUST complete within 1 second for all symbols
- Memory usage MUST be O(n) where n = number of symbols
- State cleanup MUST occur for inactive symbols

#### NFR2: Reliability
- State updates MUST be atomic (mutex protected)
- Worker MUST recover from panics
- No state corruption under concurrent access

#### NFR3: Maintainability
- Clear separation between breaker logic and mode logic
- Comprehensive test coverage
- Documentation for all public APIs

## Architecture

### Current Architecture

```
┌─────────────────┐
│  ModeManager    │ (Global)
│  - MICRO        │
│  - STANDARD     │
│  - COOLDOWN     │
└────────┬────────┘
         │
         ├─ AdaptiveGridManager (checks mode)
         │
┌────────▼────────┐
│ CircuitBreaker  │ (Per-symbol)
│  - isTripped    │
└────────┬────────┘
         │
         └─ AgenticEngine (checks breaker)
```

### Target Architecture

```
┌─────────────────────────────────┐
│   CircuitBreaker (Brain)         │
│   ┌─────────────────────────┐   │
│   │ SymbolDecisionState    │   │
│   │ - isTripped: bool      │   │
│   │ - tradingMode: Mode    │   │
│   │ - tripTime: time       │   │
│   │ - modeSince: time      │   │
│   │ - market conditions    │   │
│   └─────────────────────────┘   │
│                                  │
│   Evaluation Worker (3s)         │
│   - Evaluate all symbols         │
│   - Update breaker + mode        │
│   - Trigger callbacks            │
└──────────────┬───────────────────┘
               │
               ├─ AdaptiveGridManager
               │  GetSymbolDecision(symbol)
               │
               └─ AgenticEngine
                  (callbacks)
```

### Data Model

#### SymbolDecisionState
```go
type SymbolDecisionState struct {
    // Circuit breaker state
    isTripped        bool
    tripTime         time.Time
    reason           string
    
    // Trading mode decision
    tradingMode      TradingMode
    modeSince        time.Time
    
    // Market condition tracking
    atrHistory       []float64
    bbWidthHistory   []float64
    priceHistory     []float64
    volumeHistory    []float64
    adxHistory       []float64
    
    // Consecutive loss tracking
    consecutiveLosses int
    lastTradeOutcome  string
}
```

#### CircuitBreaker
```go
type CircuitBreaker struct {
    mu                 sync.RWMutex
    symbolStates       map[string]*SymbolDecisionState
    evaluationInterval time.Duration // 3s
    stopCh             chan struct{}
    
    // Callbacks
    onTripCallback      func(symbol, reason string)
    onResetCallback     func(symbol string)
    onModeChangeCallback func(symbol string, oldMode, newMode TradingMode)
}
```

### API

#### GetSymbolDecision
```go
// GetSymbolDecision returns trade decision for a symbol
// Returns: (canTrade bool, tradingMode TradingMode)
func (cb *CircuitBreaker) GetSymbolDecision(symbol string) (bool, TradingMode)
```

#### EvaluateSymbol (internal)
```go
// evaluateSymbol evaluates market conditions and returns decision
func (cb *CircuitBreaker) evaluateSymbol(symbol string, state *SymbolDecisionState) (canTrade bool, mode TradingMode)
```

## Implementation Plan

See `plan.md` for detailed implementation phases.

## Testing

### Unit Tests
- Test per-symbol state management
- Test evaluation worker logic
- Test market condition evaluation
- Test callback triggering
- Test concurrent access safety

### Integration Tests
- Test AdaptiveGridManager integration
- Test VolumeFarmEngine integration
- Test AgenticEngine callbacks
- Test end-to-end trade decision flow

### Performance Tests
- Test evaluation worker with 100 symbols
- Test memory usage with many symbols
- Test concurrent access under load

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| State memory leak | High | Add state cleanup for inactive symbols |
| Concurrent corruption | High | Mutex locks for all state updates |
| Logic regression | Medium | Comprehensive test coverage |
| Performance degradation | Medium | Monitor memory, add state limit |

## Success Criteria

1. CircuitBreaker provides unified `GetSymbolDecision(symbol)` API
2. Each symbol has independent breaker + mode state
3. Evaluation worker (3s) updates both breaker and mode
4. Trade logic uses only CircuitBreaker (no ModeManager)
5. ModeManager deprecated but kept for compatibility
6. All tests pass
7. Documentation updated
