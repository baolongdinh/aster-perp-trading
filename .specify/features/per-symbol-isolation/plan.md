# Per-Symbol Isolation Refactoring Plan

## Overview

This plan addresses critical per-symbol isolation issues in the trading bot where global state causes one symbol's behavior to affect other symbols. Each symbol should have independent signals, chart data, volume tracking, loss tracking, and cooldown state.

## Critical Issues Identified

### P1: TradeTracker (CRITICAL)
**File**: `backend/internal/farming/adaptive_grid/risk_sizing.go`

**Problem**:
- Global `results []TradeResult` mixes all symbols together
- `GetWinRate()` calculates win rate across all symbols
- `GetConsecutiveLosses()` counts losses across all symbols from end of array

**Impact**:
- BTC loses 3 times → consecutiveLosses = 3
- ETH tries to trade → blocked due to consecutiveLosses >= threshold
- ETH's sizing reduced even though ETH never lost

**Example Scenario**:
```
Time 00:00 - BTC: loss (consecutiveLosses = 1)
Time 00:05 - BTC: loss (consecutiveLosses = 2)
Time 00:10 - ETH: win (consecutiveLosses = 2, not reset because BTC's loss is before ETH's win in array)
Time 00:15 - BTC: loss (consecutiveLosses = 3)
Time 00:20 - ETH: tries to trade → blocked by consecutiveLosses >= 3
```

### P2: ExposureManager (CRITICAL)
**File**: `backend/internal/farming/adaptive_grid/risk_sizing.go`

**Problem**:
- Global `consecutiveLosses int` - should be per-symbol
- Global `lastLossTime time.Time` - should be per-symbol
- Global `cooldownActive bool` - should be per-symbol

**Impact**:
- BTC hits 3 losses → cooldownActive = true for ALL symbols
- ETH cannot trade even though ETH is profitable

**Example Scenario**:
```
BTC: 3 losses → RecordLoss() → consecutiveLosses = 3, cooldownActive = true
ETH: tries to trade → CanEnter() → cooldownActive check → BLOCKED
```

### P3: ModeManager (HIGH)
**File**: `backend/internal/farming/tradingmode/mode_manager.go`

**Problem**:
- Has both global state (`currentMode`, `modeSince`) and per-symbol state (`symbolModes`)
- Global methods still exist and could be accidentally called

**Status**: Already marked as DEPRECATED. New code should use CircuitBreaker.

**Impact**: If old global methods are accidentally called, they override per-symbol modes.

## Implementation Plan

### Phase 1: Fix TradeTracker (P1 - CRITICAL)

#### Step 1.1: Refactor TradeTracker struct
```go
// BEFORE
type TradeTracker struct {
    results []TradeResult
    mu sync.RWMutex
    windowSize int
    maxConsecutiveLosses int
}

// AFTER
type TradeTracker struct {
    results map[string][]TradeResult  // symbol -> results
    mu sync.RWMutex
    windowSize int
    maxConsecutiveLosses int
}
```

#### Step 1.2: Update RecordTrade method
```go
func (t *TradeTracker) RecordTrade(symbol string, pnl float64) {
    t.mu.Lock()
    defer t.mu.Unlock()
    
    // Initialize symbol slice if needed
    if t.results == nil {
        t.results = make(map[string][]TradeResult)
    }
    if _, exists := t.results[symbol]; !exists {
        t.results[symbol] = make([]TradeResult, 0, t.windowSize)
    }
    
    result := TradeResult{
        Timestamp: time.Now(),
        Symbol: symbol,
        PnL: pnl,
        IsWin: pnl > 0,
    }
    
    t.results[symbol] = append(t.results[symbol], result)
    
    // Remove old results outside window for this symbol
    cutoff := time.Now().Add(-time.Duration(t.windowSize) * time.Minute)
    newResults := make([]TradeResult, 0, len(t.results[symbol]))
    for _, r := range t.results[symbol] {
        if r.Timestamp.After(cutoff) {
            newResults = append(newResults, r)
        }
    }
    t.results[symbol] = newResults
}
```

#### Step 1.3: Add per-symbol GetWinRate
```go
func (t *TradeTracker) GetWinRate(symbol string) float64 {
    t.mu.RLock()
    defer t.mu.RUnlock()
    
    symbolResults, exists := t.results[symbol]
    if !exists || len(symbolResults) == 0 {
        return 0.5 // Default 50% win rate if no data
    }
    
    wins := 0
    for _, r := range symbolResults {
        if r.IsWin {
            wins++
        }
    }
    return float64(wins) / float64(len(symbolResults))
}
```

#### Step 1.4: Add per-symbol GetConsecutiveLosses
```go
func (t *TradeTracker) GetConsecutiveLosses(symbol string) int {
    t.mu.RLock()
    defer t.mu.RUnlock()
    
    symbolResults, exists := t.results[symbol]
    if !exists || len(symbolResults) == 0 {
        return 0
    }
    
    count := 0
    // Count from the end for this symbol only
    for i := len(symbolResults) - 1; i >= 0; i-- {
        if !symbolResults[i].IsWin {
            count++
        } else {
            break
        }
    }
    return count
}
```

#### Step 1.5: Update all callers
Search for `GetWinRate()` and `GetConsecutiveLosses()` calls and add symbol parameter:

Files to update:
- `backend/internal/farming/adaptive_grid/risk_sizing.go` - CalculateSmartSize()
- `backend/internal/farming/adaptive_grid/manager.go` - RecordTradeResult()
- Any other files using TradeTracker

#### Step 1.6: Add tests
```go
func TestTradeTracker_PerSymbolIsolation(t *testing.T) {
    tracker := NewTradeTracker(24)
    
    // BTC has 3 losses
    tracker.RecordTrade("BTC", -1.0)
    tracker.RecordTrade("BTC", -1.0)
    tracker.RecordTrade("BTC", -1.0)
    
    // ETH has 0 losses (has wins)
    tracker.RecordTrade("ETH", 1.0)
    tracker.RecordTrade("ETH", 1.0)
    
    // Verify isolation
    assert.Equal(t, 3, tracker.GetConsecutiveLosses("BTC"))
    assert.Equal(t, 0, tracker.GetConsecutiveLosses("ETH"))
    
    assert.Equal(t, 0.0, tracker.GetWinRate("BTC"))
    assert.Equal(t, 1.0, tracker.GetWinRate("ETH"))
}
```

### Phase 2: Fix ExposureManager (P2 - CRITICAL)

#### Step 2.1: Refactor ExposureManager struct
```go
// BEFORE
type ExposureManager struct {
    equity float64
    totalExposure float64
    maxExposurePct float64
    symbolExposures map[string]float64
    consecutiveLosses int  // ❌ GLOBAL
    lastLossTime time.Time  // ❌ GLOBAL
    cooldownActive bool  // ❌ GLOBAL
    mu sync.RWMutex
    logger *zap.Logger
}

// AFTER
type ExposureManager struct {
    equity float64
    totalExposure float64
    maxExposurePct float64
    symbolExposures map[string]float64
    consecutiveLosses map[string]int  // ✅ Per-symbol
    lastLossTime map[string]time.Time  // ✅ Per-symbol
    cooldownActive map[string]bool  // ✅ Per-symbol
    mu sync.RWMutex
    logger *zap.Logger
}
```

#### Step 2.2: Update NewExposureManager
```go
func NewExposureManager(maxExposurePct float64, logger *zap.Logger) *ExposureManager {
    return &ExposureManager{
        maxExposurePct: maxExposurePct,
        symbolExposures: make(map[string]float64),
        consecutiveLosses: make(map[string]int),
        lastLossTime: make(map[string]time.Time),
        cooldownActive: make(map[string]bool),
        logger: logger,
    }
}
```

#### Step 2.3: Update RecordLoss to accept symbol
```go
func (e *ExposureManager) RecordLoss(symbol string) {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    e.consecutiveLosses[symbol]++
    e.lastLossTime[symbol] = time.Now()
    
    if e.consecutiveLosses[symbol] >= 3 {
        e.cooldownActive[symbol] = true
        e.logger.Warn("Cooldown activated after consecutive losses",
            zap.String("symbol", symbol),
            zap.Int("consecutive_losses", e.consecutiveLosses[symbol]))
    }
}
```

#### Step 2.4: Add ResetLosses for symbol
```go
func (e *ExposureManager) ResetLosses(symbol string) {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    e.consecutiveLosses[symbol] = 0
    e.cooldownActive[symbol] = false
}
```

#### Step 2.5: Add GetConsecutiveLosses for symbol
```go
func (e *ExposureManager) GetConsecutiveLosses(symbol string) int {
    e.mu.RLock()
    defer e.mu.RUnlock()
    return e.consecutiveLosses[symbol]
}
```

#### Step 2.6: Add IsCooldownActive for symbol
```go
func (e *ExposureManager) IsCooldownActive(symbol string) bool {
    e.mu.RLock()
    defer e.mu.RUnlock()
    return e.cooldownActive[symbol]
}
```

#### Step 2.7: Update all callers
Search for `RecordLoss()` calls and add symbol parameter:

Files to update:
- `backend/internal/farming/adaptive_grid/manager.go` - RecordTradeResult()
- Any other files calling ExposureManager methods

#### Step 2.8: Add tests
```go
func TestExposureManager_PerSymbolIsolation(t *testing.T) {
    manager := NewExposureManager(1.0, zap.NewNop())
    
    // BTC hits 3 losses
    manager.RecordLoss("BTC")
    manager.RecordLoss("BTC")
    manager.RecordLoss("BTC")
    
    // ETH has 0 losses
    // (no RecordLoss calls for ETH)
    
    // Verify isolation
    assert.True(t, manager.IsCooldownActive("BTC"))
    assert.False(t, manager.IsCooldownActive("ETH"))
    
    assert.Equal(t, 3, manager.GetConsecutiveLosses("BTC"))
    assert.Equal(t, 0, manager.GetConsecutiveLosses("ETH"))
}
```

### Phase 3: Audit ModeManager (P3 - HIGH)

#### Step 3.1: Search for usage of global ModeManager methods
Search for these methods in codebase:
- `GetCurrentMode()` (without symbol parameter)
- `EvaluateMode()` (without symbol parameter)
- `transitionTo()` (without symbol parameter)

#### Step 3.2: Replace with per-symbol methods
Replace any found usage with per-symbol equivalents:
- `GetCurrentMode(symbol)`
- `EvaluateModeSymbol(symbol, ...)`
- `transitionSymbolTo(symbol, ...)`

#### Step 3.3: Add deprecation warnings
Add logging warnings when global methods are called:
```go
func (m *ModeManager) GetCurrentModeGlobal() TradingMode {
    m.logger.Warn("DEPRECATED: GetCurrentModeGlobal() called - use GetCurrentMode(symbol) instead")
    // ... existing code
}
```

#### Step 3.4: Consider removing global methods (optional)
If no usage found, consider removing global methods entirely:
- `currentMode`
- `modeSince`
- `modeHistory`
- `cooldownEnd`

## Testing Strategy

### Unit Tests
1. **TradeTracker tests**: Verify per-symbol win rate and consecutive loss isolation
2. **ExposureManager tests**: Verify per-symbol cooldown isolation
3. **Integration tests**: Verify that one symbol's losses don't affect other symbols

### Regression Tests
1. Run existing tests to ensure no breaking changes
2. Add test scenario: BTC loses 3 times, ETH should still trade normally
3. Add test scenario: BTC in cooldown, ETH should not be affected

### Manual Testing
1. Start bot with multiple symbols (BTC, ETH, SOL)
2. Force BTC to lose 3 times (simulate losses)
3. Verify BTC enters cooldown
4. Verify ETH and SOL continue trading normally
5. Verify ETH's sizing is not reduced by BTC's losses

## Rollout Plan

### Step 1: Implement Phase 1 (TradeTracker)
- Estimated time: 2-3 hours
- Risk: Medium - affects sizing logic
- Testing: High - comprehensive unit tests

### Step 2: Implement Phase 2 (ExposureManager)
- Estimated time: 2-3 hours
- Risk: High - affects trading ability
- Testing: Critical - must verify cooldown isolation

### Step 3: Implement Phase 3 (ModeManager audit)
- Estimated time: 1-2 hours
- Risk: Low - deprecated component
- Testing: Medium - search and replace

### Step 4: Integration Testing
- Estimated time: 2-3 hours
- Risk: High - end-to-end validation
- Testing: Critical - full bot simulation

### Step 5: Deployment
- Estimated time: 1 hour
- Risk: Medium - production deployment
- Testing: Monitor logs for cooldown and sizing behavior

## Success Criteria

1. ✅ TradeTracker calculates win rate and consecutive losses per-symbol
2. ✅ ExposureManager tracks consecutive losses and cooldown per-symbol
3. ✅ One symbol's losses do not affect other symbols' trading ability
4. ✅ One symbol's cooldown does not block other symbols
5. ✅ All existing tests pass
6. ✅ New per-symbol isolation tests pass
7. ✅ Manual testing confirms isolation works in practice

## Backward Compatibility

- TradeTracker: Breaking change - requires updating all callers
- ExposureManager: Breaking change - requires updating all callers
- ModeManager: No breaking change - only adding warnings

## Notes

- Total estimated time: 8-12 hours
- Critical path: Phase 1 → Phase 2 → Phase 3
- Can be done incrementally per phase
- Each phase should be tested independently before proceeding
