# Implementation Plan: Circuit Breaker as "Brain" - Merge ModeManager

## Overview

Merge `ModeManager` and `CircuitBreaker` into a unified "brain" component that:
1. Makes per-symbol decisions (can trade? what mode?)
2. Runs continuous evaluation worker (3s interval)
3. Provides single API for trade logic: `GetSymbolDecision(symbol) (canTrade bool, mode TradingMode)`

## Technical Context

### Current Architecture

| Component | Scope | Decision | Location |
|-----------|-------|----------|----------|
| **ModeManager** | Global (all symbols share mode) | Trading mode (MICRO, STANDARD, TREND_ADAPTED, COOLDOWN) | `backend/internal/farming/tradingmode/mode_manager.go` |
| **CircuitBreaker** | Per-symbol | Can trade? (tripped/not tripped) | `backend/internal/agentic/circuit_breaker.go` |
| **AgenticEngine** | Global | Whitelist symbols (30s detection cycle) | `backend/internal/agentic/engine.go` |
| **AdaptiveGridManager** | Per-symbol | CanPlaceOrder check | `backend/internal/farming/adaptive_grid/manager.go` |

### Problems

1. **ModeManager global** → Cannot have symbol A in MICRO, symbol B in STANDARD simultaneously
2. **Two separate "brains"** → ModeManager and CircuitBreaker make independent decisions
3. **Slow detection cycle** → 30s vs real-time market changes
4. **No unified API** → Trade logic must check both ModeManager and CircuitBreaker

### Target Architecture

**Unified CircuitBreaker (Brain):**

```go
type SymbolDecisionState struct {
    // Circuit breaker state
    isTripped bool
    tripTime  time.Time
    reason    string
    
    // Trading mode decision (NEW - from ModeManager)
    tradingMode TradingMode
    modeSince   time.Time
    
    // Market condition tracking
    atrHistory     []float64
    bbWidthHistory []float64
    priceHistory   []float64
    volumeHistory  []float64
    adxHistory     []float64
}

type CircuitBreaker struct {
    symbolStates       map[string]*SymbolDecisionState
    evaluationInterval time.Duration // 3s
    stopCh             chan struct{}
    
    onTripCallback   func(symbol, reason string)
    onResetCallback  func(symbol string)
    onModeChangeCallback func(symbol string, oldMode, newMode TradingMode)
}

// API for trade logic
func (cb *CircuitBreaker) GetSymbolDecision(symbol) (canTrade bool, mode TradingMode)
```

**Evaluation Worker (3s):**
```go
func (cb *CircuitBreaker) evaluationLoop() {
    for symbol, state := range cb.symbolStates {
        // Evaluate market conditions
        canTrade, mode := cb.evaluateSymbol(symbol, state)
        
        // Update state
        oldMode := state.tradingMode
        state.isTripped = !canTrade
        state.tradingMode = mode
        
        // Trigger callbacks
        if oldMode != mode {
            cb.onModeChangeCallback(symbol, oldMode, mode)
        }
    }
}
```

## Implementation Phases

### Phase 1: Refactor ModeManager to Per-Symbol

**Goal:** Change ModeManager from global to per-symbol mode management.

**Tasks:**
1. Add `symbolModes map[string]*SymbolModeState` to ModeManager struct
2. Create `SymbolModeState` struct (mode, modeSince, cooldownEnd, etc.)
3. Refactor `GetCurrentMode()` → `GetCurrentMode(symbol)`
4. Refactor `EvaluateMode()` → `EvaluateMode(symbol, ...)`
5. Update AdaptiveGridManager to pass symbol to ModeManager calls
6. Update all ModeManager method signatures to include symbol parameter

**Files to modify:**
- `backend/internal/farming/tradingmode/mode_manager.go`
- `backend/internal/farming/adaptive_grid/manager.go`

**Acceptance criteria:**
- Each symbol has independent mode state
- ModeManager tests updated for per-symbol behavior
- AdaptiveGridManager passes symbol to all ModeManager calls

---

### Phase 2: Merge ModeManager Logic into CircuitBreaker

**Goal:** Move mode decision logic from ModeManager to CircuitBreaker.

**Tasks:**
1. Add `tradingMode`, `modeSince` to `SymbolBreakerState` (rename to `SymbolDecisionState`)
2. Copy `EvaluateMode()` logic from ModeManager to CircuitBreaker
3. Add `evaluateSymbol()` method that combines:
   - Circuit breaker checks (ATR, BB width, price stability, volume, ADX)
   - Mode decision logic (from ModeManager.EvaluateMode)
4. Add `onModeChangeCallback` to CircuitBreaker
5. Update evaluation worker to call `evaluateSymbol()` and update mode

**Files to modify:**
- `backend/internal/agentic/circuit_breaker.go`

**Acceptance criteria:**
- CircuitBreaker decides both `isTripped` and `tradingMode`
- Evaluation worker updates both breaker and mode state
- Mode change callback triggers when mode changes

---

### Phase 3: Update Trade Logic to Use Unified API

**Goal:** Replace ModeManager calls with CircuitBreaker.GetSymbolDecision().

**Tasks:**
1. Add `GetSymbolDecision(symbol) (canTrade bool, mode TradingMode)` to CircuitBreaker
2. Update AdaptiveGridManager.CanPlaceOrder():
   - Remove ModeManager calls
   - Call `circuitBreaker.GetSymbolDecision(symbol)`
   - Check `canTrade` and use `mode` for placement logic
3. Update VolumeFarmEngine to pass CircuitBreaker to AdaptiveGridManager
4. Remove ModeManager dependency from AdaptiveGridManager

**Files to modify:**
- `backend/internal/agentic/circuit_breaker.go`
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/farming/volume_farm_engine.go`

**Acceptance criteria:**
- AdaptiveGridManager uses only CircuitBreaker for trade decisions
- ModeManager no longer used in trade logic path

---

### Phase 4: Cleanup and Deprecation

**Goal:** Remove old ModeManager usage and clean up code.

**Tasks:**
1. Mark ModeManager as deprecated (add comment)
2. Remove ModeManager from AdaptiveGridManager struct
3. Update tests to use CircuitBreaker instead of ModeManager
4. Update documentation (AGENTIC_TRADING_NGHIEP_VU.md)
5. Add integration test for unified CircuitBreaker

**Files to modify:**
- `backend/internal/farming/tradingmode/mode_manager.go`
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/agentic/agentic_test.go`
- `AGENTIC_TRADING_NGHIEP_VU.md`

**Acceptance criteria:**
- ModeManager deprecated but kept for backward compatibility
- All trade logic uses CircuitBreaker
- Documentation updated

---

## Constitution Check

### Code Quality
- ✅ Per-symbol state management follows single responsibility principle
- ✅ Unified API reduces coupling between components
- ⚠️ Need to ensure thread safety with concurrent symbol state updates

### Architecture
- ✅ CircuitBreaker as "brain" aligns with centralized decision making
- ✅ Per-symbol granularity allows independent symbol management
- ✅ Evaluation worker (3s) provides real-time market response

### Performance
- ⚠️ Per-symbol state increases memory usage (O(n) where n = number of symbols)
- ✅ Evaluation interval (3s) is reasonable for trading decisions
- ✅ No blocking operations in evaluation loop

### Testing
- ⚠️ Need integration tests for per-symbol mode transitions
- ⚠️ Need tests for evaluation worker concurrent updates
- ⚠️ Need tests for mode change callback triggering

---

## Gate Conditions

### Phase 1 Gate
- [ ] ModeManager tests pass for per-symbol behavior
- [ ] AdaptiveGridManager integration tests pass
- [ ] No regression in existing functionality

### Phase 2 Gate
- [ ] CircuitBreaker tests include mode decision logic
- [ ] Evaluation worker tests cover mode updates
- [ ] Mode change callback tests pass

### Phase 3 Gate
- [ ] AdaptiveGridManager uses only CircuitBreaker
- [ ] Integration tests for trade decisions pass
- [ ] No ModeManager calls in trade logic path

### Phase 4 Gate
- [ ] All tests pass
- [ ] Documentation updated
- [ ] No deprecated code warnings

---

## Dependencies

### External Dependencies
- None

### Internal Dependencies
- `backend/internal/config` - TradingModesConfig, AgenticCircuitBreakerConfig
- `backend/internal/farming/adaptive_grid` - RangeState, RangeDetector integration
- `backend/internal/farming/tradingmode` - TradingMode enum, ModeTransition

### Integration Points
- AdaptiveGridManager → CircuitBreaker (trade decisions)
- VolumeFarmEngine → CircuitBreaker (initialization)
- AgenticEngine → CircuitBreaker (callbacks for emergency exit/force placement)

---

## Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Per-symbol state memory leak | High | Add state cleanup for inactive symbols |
| Concurrent state corruption | High | Use mutex locks for all state updates |
| Mode decision logic regression | Medium | Comprehensive test coverage |
| Performance degradation with many symbols | Medium | Monitor memory usage, add state limit |

---

## Success Criteria

1. ✅ CircuitBreaker provides unified `GetSymbolDecision(symbol)` API
2. ✅ Each symbol has independent breaker + mode state
3. ✅ Evaluation worker (3s) updates both breaker and mode
4. ✅ Trade logic uses only CircuitBreaker (no ModeManager)
5. ✅ ModeManager deprecated but kept for compatibility
6. ✅ All tests pass
7. ✅ Documentation updated

---

## Timeline Estimate

- Phase 1: 2-3 hours (ModeManager per-symbol refactor)
- Phase 2: 2-3 hours (Merge logic into CircuitBreaker)
- Phase 3: 2-3 hours (Update trade logic)
- Phase 4: 1-2 hours (Cleanup and documentation)

**Total: 7-11 hours**
