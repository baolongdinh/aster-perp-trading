# Blocking Logic Fix Plan

## Overview
Fix unreasonable blocking logic in trading core flow to prevent bot from dying/getting stuck.

## Issues Identified

### HIGH PRIORITY

### 1. Max Notional Limit - Reduce Order Size Instead of Reject
**File:** `grid_manager.go:1970-1979`
**Current Logic:** Reject order if notional > max_notional
**Issue:** Hard limit, no fallback
**Solution:** Reduce order size to fit within limit (similar to exposure fix)

### 2. Trading Paused - Auto-Resume After Stabilization
**File:** `adaptive_grid/manager.go:1956, 2276`
**Current Logic:** Pause trading indefinitely after emergency close/regime change
**Issue:** Can get stuck forever
**Solution:** Auto-resume after stabilization period (configurable, default 5 min)

### 3. Cooldown - Dynamic Cooldown Based on Performance
**File:** `adaptive_grid/manager.go:55-68`, `grid_manager.go:595-598`
**Current Logic:** Fixed cooldown period after losses
**Issue:** Cooldown can be too long even if bot is performing well
**Solution:** Dynamic cooldown based on recent win rate (reduce cooldown if win rate > 60%)

### MEDIUM PRIORITY

### 4. Circuit Breaker - Auto-Resume Timeout
**File:** `adaptive_grid/manager.go:2422-2428`
**Current Logic:** Block if circuit breaker returns false
**Issue:** Can block indefinitely
**Solution:** Auto-resume after timeout (configurable, default 10 min)

### 5. Grid State Machine - Auto-Transition Timeout
**File:** `grid_manager.go:602-607`
**Current Logic:** Block if state doesn't allow placement
**Issue:** Can get stuck in unexpected state
**Solution:** Auto-transition to valid state after timeout (configurable, default 5 min)

### 6. Spread Protection - Reduce Aggressiveness
**File:** `adaptive_grid/spread_protection.go:82-100`
**Current Logic:** Emergency pause if spread too wide
**Issue:** Too aggressive, can pause unnecessarily
**Solution:** Reduce emergency threshold from 2% to 5%, add auto-resume after spread normalizes

### LOW PRIORITY

### 7. Time Filter - Emergency Exception
**File:** `grid_manager.go:2034-2040`, `adaptive_grid/manager.go:2468-2473`
**Current Logic:** Hard block outside trading hours
**Issue:** No exception for emergency closes
**Solution:** Allow reduce-only orders outside hours for risk management

### 8. Funding Rate Bias - Hedge Option
**File:** `adaptive_grid/manager.go:2515-2521`
**Current Logic:** Block if funding rate too high
**Issue:** Hard block, no alternative
**Solution:** Allow hedge positions instead of blocking

### 9. Market Bias - Reduce Size Instead of Block
**File:** `adaptive_grid/risk_sizing.go:941-951`
**Current Logic:** Block long if bearish, short if bullish
**Issue:** Can miss opportunities
**Solution:** Reduce order size by 50% instead of blocking

## Implementation Order

### Phase 1: HIGH PRIORITY (Critical Fixes)
1. Fix Max Notional Limit (similar to exposure fix)
2. Implement Auto-Resume for Trading Paused
3. Implement Dynamic Cooldown

### Phase 2: MEDIUM PRIORITY (Stability Improvements)
4. Add Circuit Breaker Auto-Resume
5. Add Grid State Machine Auto-Transition
6. Reduce Spread Protection Aggressiveness

### Phase 3: LOW PRIORITY (Enhancements)
7. Add Time Filter Emergency Exception
8. Add Funding Rate Hedge Option
9. Change Market Bias to Size Reduction

## Testing Strategy
- Test each fix with dry-run mode
- Verify bot doesn't get stuck after fixes
- Monitor logs for "ORDER SIZE REDUCED" vs "ORDER REJECTED"
- Ensure risk limits still respected

## Configuration Changes
Add to config:
```yaml
blocking_logic:
  max_notional_reduce_size: true
  trading_paused_auto_resume_seconds: 300
  dynamic_cooldown_enabled: true
  circuit_breaker_timeout_seconds: 600
  state_machine_timeout_seconds: 300
  spread_emergency_threshold_pct: 5.0
```
