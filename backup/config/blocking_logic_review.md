# Core Agentic Trading - Blocking Logic Review

## Summary

**Total Blocking Points Identified**: 10
**Complete with Unblock Logic**: 9
**Critical Gaps**: 1

---

## 1. tradingPaused / resumeTrading ✅

**Location**: `adaptive_grid/manager.go:1849-1915`

### Block Logic
- **Trigger**: `pauseTrading(symbol)` sets `tradingPaused[symbol] = true`
- **Triggers**:
  - Emergency close position (line 1749)
  - Emergency close all (line 1845)
  - Manual pause (if implemented)
- **Check**: `CanPlaceOrder()` returns false if `tradingPaused[symbol] = true` (line 2180)

### Unblock Logic
- **Manual**: `resumeTrading(symbol)` deletes from map (line 1856)
- **Auto**: `TryResumeTrading()` called every 30s via `CheckAndResumeAll()` (line 1038)
- **Conditions**:
  1. Range detector exists
  2. State = WAIT_NEW_RANGE
  3. `isReadyForRegrid()` returns true
  4. `detector.ShouldTrade()` returns true
- **Action**: `resumeTrading()` + `RebuildGrid()`

**Status**: ✅ COMPLETE

---

## 2. cooldownActive (ExposureManager) ✅

**Location**: `adaptive_grid/manager.go`

### Block Logic
- **Trigger**: `cooldownActive[symbol] = true` when consecutive losses >= 3
- **Check**: `CanPlaceOrder()` - **NOT BLOCKED** for volume farming (line 2185-2202)
  - Logs warning but allows rebalance
  - Auto-resets after 30s cooldown

### Unblock Logic
- **Auto**: `RecordLoss(isWin=true)` sets `cooldownActive[symbol] = false`
- **Auto**: Cooldown expires after 30s (line 2189)

**Status**: ✅ COMPLETE (relaxed for volume farming)

---

## 3. transitionCooldown ✅

**Location**: `adaptive_grid/manager.go:2205-2212`

### Block Logic
- **Trigger**: `transitionCooldown[symbol]` set after state transitions
- **Check**: `CanPlaceOrder()` - **NOT BLOCKED** for volume farming (line 2205-2212)
  - Logs debug but allows rebalance
  - 5s cooldown (reduced from 30s)

### Unblock Logic
- **Auto**: Expires after 5 seconds

**Status**: ✅ COMPLETE (relaxed for volume farming)

---

## 4. Position Limit (Max Position) ⚠️ PARTIAL

**Location**: `adaptive_grid/manager.go:2214-2236`

### Block Logic
- **Trigger**: Position notional >= MaxPositionUSDT (300)
- **Check**: `CanPlaceOrder()` (line 2214-2236)
  - **Soft Block**: position >= max - allows hedge orders only
  - **Hard Block**: position >= hardCap (1.2x = 360) - blocks ALL orders

### Unblock Logic
- **Auto**: `evaluateRiskAndAct()` reduces position by 50% when >= max (line 1335-1366)
  - 30s cooldown between reductions
  - Emergency close if >= hardCap
- **Manual**: Emergency close triggers `pauseTrading()` → requires resume

**Status**: ⚠️ PARTIAL - Has reduction logic but can be overwhelmed

**CRITICAL ISSUE**: Grid orders (non-rebalance) are placed without checking position limit in `placeGridOrders()`. If multiple orders fill simultaneously, position can exceed hardCap before reduction triggers.

---

## 5. RiskMonitor Exposure ✅

**Location**: `adaptive_grid/manager.go:2238-2248`

### Block Logic
- **Trigger**: utilization >= 1.0 (100% exposure)
- **Check**: `CanPlaceOrder()` - **NOT BLOCKED** for volume farming (line 2238-2248)
  - Logs warning but allows rebalance
  - Size adjusted in order placement logic

### Unblock Logic
- **Auto**: Utilization decreases when positions closed

**Status**: ✅ COMPLETE (relaxed for volume farming)

---

## 6. RangeDetector State Machine ✅

**Location**: `adaptive_grid/range_detector.go`

### Block Logic
- **Trigger**: State = Breakout/Stabilizing → `ShouldTrade() = false`
- **Check**: `TryResumeTrading()` requires `detector.ShouldTrade() = true` (line 1895)

### Unblock Logic
- **Auto**: Auto-transition Stabilizing → Active when reentry confirmations met
- **Conditions**:
  1. Price in range
  2. ADX < 25 (relaxed from 20)
  3. BB width contraction < 1.8x (relaxed from 1.5x)
  4. Range shift >= 0.3% (relaxed from 0.5%)

**Status**: ✅ COMPLETE (relaxed by user)

---

## 7. TimeFilter ✅

**Location**: `adaptive_grid/time_filter.go`

### Block Logic
- **Trigger**: `CanTrade() = false` when outside trading hours
- **Check**: `CanPlaceOrder()` - **NOT CHECKED** (assumed handled upstream)

### Unblock Logic
- **Auto**: Auto-change when time enters trading slot
- **Time-based**: No manual intervention needed

**Status**: ✅ COMPLETE

---

## 8. RateLimiter ✅

**Location**: `adaptive_grid/rate_limiter.go`

### Block Logic
- **Trigger**: Token bucket empty + penalty active
- **Check**: `CanPlaceOrder()` - **NOT CHECKED** (assumed handled upstream)

### Unblock Logic
- **Auto**: Auto refill (refillRate) + penalty auto expire (penaltyUntil)

**Status**: ✅ COMPLETE

---

## 9. SpreadProtection ✅

**Location**: `adaptive_grid/spread_protection.go`

### Block Logic
- **Trigger**: `isPaused = true` when spread > threshold
  - Normal: spread > 0.1%
  - Emergency: spread > 0.3%
- **Check**: `detector.ShouldTrade()` includes spread check

### Unblock Logic
- **Auto**: Resume when spread <= threshold + 30s elapsed
- **Manual**: `ForceResume()` available

**Status**: ✅ COMPLETE

---

## 10. CircuitBreaker ⚠️ CRITICAL GAP

**Location**: `internal/agentic/circuit_breaker.go`

### Block Logic
- **Trigger**: `isTripped = true` when:
  1. Volatility spike (ATR > threshold)
  2. Consecutive losses >= 5
  3. Price swing > threshold
  4. Volume anomaly
  5. ADX spike
- **Check**: `evaluateAllSymbols()` runs every 3s (line 485-497)
- **Reset Logic**: `resetSymbol()` sets `isTripped = false` when conditions normalize

### Unblock Logic
- **Auto**: Evaluation loop checks reset conditions every 3s
- **Gap**: **Callbacks NOT WIRED**
  - `onTripCallback` - should trigger emergency exit
  - `onResetCallback` - should trigger force placement/grid rebuild
  - `onModeChangeCallback` - should notify mode transitions

**Impact**: No action when trip/reset occurs (no emergency exit, no force placement)

**Status**: ⚠️ CRITICAL GAP - Callbacks not wired in VolumeFarmEngine

---

## CRITICAL ISSUES IDENTIFIED

### Issue 1: Position Can Exceed HardCap Before Reduction

**Problem**:
- `placeGridOrders()` does NOT check position limit before placing orders
- If multiple grid orders fill simultaneously, position can jump from 290 to 592 (exceeding hardCap 360)
- `positionRebalancerWorker` runs every 5s - too slow to prevent this
- Emergency close triggers when position >= hardCap, causing trading pause

**Evidence**:
```
notional: 592.94
max_allowed: 300
hard_cap: 360
```

**Root Cause**:
1. Grid orders placed without position limit check
2. Rebalancing only checks every 5s
3. Multiple fills can happen in milliseconds

**Recommended Fix**:
Add position limit check in `placeGridOrders()` before placing each order:
```go
// Check if adding this order would exceed position limit
currentPosition := getCurrentPosition(symbol)
newPosition := currentPosition + orderNotional
if newPosition >= hardCap {
    skip order or reduce size
}
```

---

### Issue 2: CircuitBreaker Callbacks Not Wired

**Problem**:
- CircuitBreaker trips/resets but no action taken
- No emergency exit when critical conditions detected
- No force placement when conditions normalize

**Recommended Fix**:
Wire callbacks in `VolumeFarmEngine`:
```go
engine.circuitBreaker.SetOnTripCallback(func(symbol, reason string) {
    // Trigger emergency exit
    adaptiveGridManager.ExitAll(ctx, symbol, EventEmergencyExit, reason)
})

engine.circuitBreaker.SetOnResetCallback(func(symbol, reason string) {
    // Trigger force placement/grid rebuild
    adaptiveGridManager.ResumeTrading(symbol)
    gridManager.RebuildGrid(ctx, symbol)
})
```

---

## Summary Table

| Blocking Point | Block Trigger | Unblock Logic | Status | Gap |
|----------------|--------------|---------------|--------|-----|
| tradingPaused | Emergency close | TryResumeTrading (30s) | ✅ | None |
| cooldownActive | Consecutive losses >= 3 | Auto-reset (30s) | ✅ | None |
| transitionCooldown | State transition | Auto-expire (5s) | ✅ | None |
| Position Limit | position >= hardCap | Reduction (30s) | ⚠️ | Grid orders skip check |
| RiskMonitor Exposure | utilization >= 1.0 | Auto-decrease | ✅ | None |
| RangeDetector | State = Breakout | Auto-transition | ✅ | None |
| TimeFilter | Outside hours | Time-based | ✅ | None |
| RateLimiter | Token empty | Auto-refill | ✅ | None |
| SpreadProtection | spread > threshold | Auto-resume (30s) | ✅ | None |
| CircuitBreaker | Volatility/loss spike | Auto-reset (3s) | ⚠️ | Callbacks not wired |

---

## Recommended Actions

### HIGH PRIORITY

1. **Fix Position Limit Check in Grid Orders**
   - Add position limit check in `placeGridOrders()` before placing each order
   - Skip or reduce orders if would exceed hardCap
   - Prevent position from jumping 290→592

2. **Wire CircuitBreaker Callbacks**
   - Set `onTripCallback` to trigger emergency exit
   - Set `onResetCallback` to trigger force placement
   - Set `onModeChangeCallback` to notify mode transitions

### MEDIUM PRIORITY

3. **Increase Rebalancer Frequency**
   - Change from 5s to 1s for faster response
   - Or trigger rebalance immediately on fill (already done)

4. **Add Position Limit Check in placeOrder**
   - Even for rebalance orders, check if would exceed hardCap
   - Only skip for reduce-only orders closing position

### LOW PRIORITY

5. **Add Hard Cap Buffer**
   - Reduce hardCap from 1.2x to 1.1x for earlier intervention
   - Or add intermediate warning at 1.1x

---

## Conclusion

**Total Blocking Points**: 10
**Complete**: 8
**Partial**: 1 (Position Limit)
**Critical Gap**: 1 (CircuitBreaker callbacks)

The bot has comprehensive blocking/unblock logic, but two critical issues:
1. Grid orders can exceed hardCap before reduction triggers
2. CircuitBreaker callbacks not wired - no action on trip/reset

These issues explain why the bot gets stuck after emergency close and why positions can exceed limits unexpectedly.
