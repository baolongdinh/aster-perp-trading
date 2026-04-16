# Adaptive State Machine Implementation Plan

## Overview

Implement adaptive state machine to make bot flexible and resilient in all market conditions. The bot should be able to "thiên biến vạn hóa, hóa nguy thành cơ" (transform from danger to opportunity).

**Key Principle:** States must dynamically evaluate market conditions in real-time and select the most appropriate state based on multiple factors, not just simple threshold triggers.

## Current State Machine

```
IDLE → ENTER_GRID → TRADING → EXIT_HALF → EXIT_ALL → WAIT_NEW_RANGE → ENTER_GRID
```

## Problem Statement

Current state machine is too rigid:
- Only reacts to PnL-based risk (EXIT_HALF, EXIT_ALL)
- Doesn't adapt to volatility changes
- Doesn't handle position size limits gracefully
- No recovery mechanism after losses
- No defensive mode for extreme conditions
- **No dynamic evaluation of market conditions**
- **States are trigger-based, not evaluation-based**

## Architecture Principle: Market Condition Evaluation

### Real-Time Market Data Sources (Existing Workers)
1. **globalKlineProcessor** - Receives klines every second
2. **RangeDetector** - Evaluates range/breakout (ATR, BB, ADX)
3. **WebSocketClient** - Position updates, order updates
4. **CircuitBreaker** - Volatility spike detection (already has evaluation logic)

### Market Condition Evaluation Layer (NEW)
**Purpose:** Continuously evaluate market conditions and recommend optimal state

**Factors to Evaluate:**
- **Volatility:** ATR, BB width, price swing
- **Trend:** ADX, trend strength, direction
- **Position:** Size, unrealized PnL, time in position
- **Risk:** Daily PnL, drawdown, consecutive losses
- **Market:** Spread, volume, funding rate

**Evaluation Frequency:** Every kline (1s) via globalKlineProcessor

**Output:** Recommended state + confidence score

## Proposed States

### Priority 1: OVER_SIZE (Critical)
**Trigger:** Position size > MaxPositionUSDT (but < 1.2x hard cap)

**Behavior:**
- Only allow reduce-only orders (close positions, TP, SL)
- Block all orders that would increase position size
- Auto-reduce position when profitable opportunities arise
- Log warnings for size limit breaches

**Transitions:**
- `TRADING` → `OVER_SIZE` (EventOverSizeLimit)
- `OVER_SIZE` → `TRADING` (EventSizeNormalized, when size <= 0.8x MaxPositionUSDT)
- `OVER_SIZE` → `EXIT_HALF` (EventPartialLoss, if still losing)
- `OVER_SIZE` → `EXIT_ALL` (EventFullLoss, if critical loss)

### Priority 2: DEFENSIVE (High)
**Trigger:** Extreme volatility (ATR > 3x average, BB width > 5%, flash crash detected)

**Behavior:**
- Spread multiplier: 2x (wider spreads for safety)
- Stop loss multiplier: 0.8 (tighter SL for quick exit)
- No new position openings
- Only close existing positions (TP, SL)
- Reduced order count (50% of normal)

**Transitions:**
- `TRADING` → `DEFENSIVE` (EventExtremeVolatility)
- `DEFENSIVE` → `TRADING` (EventVolatilityNormalized)
- `DEFENSIVE` → `EXIT_ALL` (EventEmergencyExit, if conditions worsen)

### Priority 3: RECOVERY (Medium)
**Trigger:** After EXIT_HALF/EXIT_ALL, when PnL starts recovering (PnL >= -0.5)

**Behavior:**
- Position size: 50% of normal
- Spread multiplier: 1.5x (wider for safety)
- Conservative TP/SL (TP 0.8x, SL 1.2x normal)
- Max orders: 50% of normal
- Minimum 30 minutes of stable PnL before full recovery

**Transitions:**
- `EXIT_HALF` → `RECOVERY` (EventRecoveryStart)
- `EXIT_ALL` → `RECOVERY` (EventRecoveryStart, after manual intervention)
- `RECOVERY` → `TRADING` (EventRecoveryComplete, after stable PnL)
- `RECOVERY` → `EXIT_HALF` (EventPartialLoss, if loss recurs)

## Implementation Phases

### Phase 0: Market Condition Evaluator (Foundational - 2-3 days)

**Purpose:** Create the evaluation layer that continuously assesses market conditions and recommends optimal state. This is the foundation for all adaptive states.

**Tasks:**

1. **Create Market Condition Evaluator** (`adaptive_grid/market_condition_evaluator.go` - new file)
   - Evaluate volatility (ATR, BB width, price swing)
   - Evaluate trend (ADX, trend strength, direction)
   - Evaluate position (size, unrealized PnL, time in position)
   - Evaluate risk (daily PnL, drawdown, consecutive losses)
   - Evaluate market (spread, volume, funding rate)
   - Calculate confidence score for each state
   - Return recommended state with confidence

2. **Integrate with Existing Workers**
   - Call from `globalKlineProcessor` every kline
   - Reuse RangeDetector data (ATR, BB, ADX)
   - Reuse WebSocketClient data (positions, orders)
   - Reuse CircuitBreaker data (volatility detection)
   - Reuse RiskManager data (daily PnL, drawdown)

3. **Add Config** (`agentic-vf-config.yaml`)
   ```yaml
   market_condition_evaluator:
     enabled: true
     evaluation_interval_sec: 1  # Every kline
     min_confidence_threshold: 0.7  # Minimum confidence to switch state
     state_stability_duration: 30  # Seconds to stay in state before considering switch
   ```

4. **Add Config Struct** (`config.go`)
   - Add `MarketConditionEvaluatorConfig` struct
   - Add to main config structure

**Evaluation Logic:**
```go
type MarketCondition struct {
    VolatilityScore    float64  // 0-1, higher = more volatile
    TrendScore         float64  // 0-1, higher = stronger trend
    PositionScore      float64  // 0-1, higher = larger position
    RiskScore          float64  // 0-1, higher = higher risk
    MarketScore        float64  // 0-1, higher = better market conditions
}

type StateRecommendation struct {
    State          GridState
    Confidence     float64  // 0-1
    Reason         string
    Conditions     MarketCondition
}
```

**State Selection Logic:**
- OVER_SIZE: PositionScore > 0.8
- DEFENSIVE: VolatilityScore > 0.8 OR RiskScore > 0.8
- RECOVERY: RiskScore > 0.6 AND PositionScore < 0.5
- TRADING: Default state when no special conditions
- EXIT_HALF: RiskScore > 0.7 AND PositionScore > 0.5
- EXIT_ALL: RiskScore > 0.9 OR PositionScore > 0.95

**Testing:**
- Unit tests for each evaluation factor
- Integration test with real market data
- Test state selection with simulated conditions
- Verify confidence score calculation

**Deliverable:** Market condition evaluation layer that provides dynamic state recommendations based on real-time market data.

### Phase 1: OVER_SIZE State (1-2 days)

**Tasks:**

1. **Add State Constants** (`state_machine.go`)
   - Add `GridStateOverSize` constant
   - Add `EventOverSizeLimit`, `EventSizeNormalized` events
   - Update String() methods

2. **Update Transition Logic** (`state_machine.go`)
   - Add case for `GridStateTrading` → `GridStateOverSize`
   - Add case for `GridStateOverSize` → `GridStateTrading`
   - Add case for `GridStateOverSize` → `GridStateExitHalf/ExitAll`
   - Update `CanTransition()` method

3. **Add Position Size Check** (`grid_manager.go`)
   - Create `checkPositionSize()` method
   - Call from `globalKlineProcessor` after `checkPnLRisk()`
   - Check if position notional > MaxPositionUSDT
   - Trigger transitions based on thresholds

4. **Update Order Placement Logic** (`manager.go`)
   - Modify `CanPlaceOrder()` to check state
   - If `OVER_SIZE`: only allow reduce-only orders
   - Block orders that would increase position size
   - Add logging for blocked orders

5. **Add Config** (`agentic-vf-config.yaml`)
   ```yaml
   risk:
     over_size:
       enabled: true
       threshold_pct: 1.0  # 100% = MaxPositionUSDT
       recovery_pct: 0.8   # 80% to normalize
       auto_reduction_enabled: true
   ```

6. **Add Config Struct** (`config.go`)
   - Add `OverSizeConfig` struct
   - Add to `RiskConfig`

**Testing:**
- Unit tests for transition logic
- Integration test with position size limit
- Verify reduce-only order restriction
- Test auto-reduction behavior

### Phase 2: DEFENSIVE State (2-3 days)

**Tasks:**

1. **Add State Constants** (`state_machine.go`)
   - Add `GridStateDefensive` constant
   - Add `EventExtremeVolatility`, `EventVolatilityNormalized` events
   - Update String() methods

2. **Update Transition Logic** (`state_machine.go`)
   - Add transition cases for DEFENSIVE state
   - Update `CanTransition()` method

3. **Add Volatility Monitor** (`adaptive_grid/volatility_monitor.go` - new file)
   - Monitor ATR, BB width, price swings
   - Detect extreme volatility conditions
   - Trigger DEFENSIVE state transitions
   - Call from `globalKlineProcessor`

4. **Update Grid Parameters for DEFENSIVE** (`manager.go`)
   - Apply spread multiplier when in DEFENSIVE
   - Apply SL multiplier when in DEFENSIVE
   - Reduce max orders when in DEFENSIVE
   - Block new position openings

5. **Add Config** (`agentic-vf-config.yaml`)
   ```yaml
   adaptive_states:
     defensive:
       enabled: true
       atr_multiplier_threshold: 3.0
       bb_width_threshold: 0.05
       spread_multiplier: 2.0
       sl_multiplier: 0.8
       max_orders_multiplier: 0.5
   ```

6. **Add Config Struct** (`config.go`)
   - Add `DefensiveStateConfig` struct
   - Add to config structure

**Testing:**
- Unit tests for volatility detection
- Integration test with simulated volatility spike
- Verify parameter adjustments in DEFENSIVE
- Test transition back to TRADING

### Phase 3: RECOVERY State (2-3 days)

**Tasks:**

1. **Add State Constants** (`state_machine.go`)
   - Add `GridStateRecovery` constant
   - Add `EventRecoveryStart`, `EventRecoveryComplete` events
   - Update String() methods

2. **Update Transition Logic** (`state_machine.go`)
   - Add transition cases for RECOVERY state
   - Update `CanTransition()` method

3. **Add Recovery Monitor** (`adaptive_grid/recovery_monitor.go` - new file)
   - Monitor PnL after EXIT_HALF/EXIT_ALL
   - Detect when PnL starts recovering
   - Track stable PnL duration
   - Trigger RECOVERY state transitions

4. **Update Grid Parameters for RECOVERY** (`manager.go`)
   - Apply size multiplier (50%)
   - Apply spread multiplier (1.5x)
   - Conservative TP/SL settings
   - Reduce max orders
   - Require stable PnL before full recovery

5. **Add Config** (`agentic-vf-config.yaml`)
   ```yaml
   adaptive_states:
     recovery:
       enabled: true
       recovery_threshold_usdt: -0.5
       size_multiplier: 0.5
       spread_multiplier: 1.5
       tp_multiplier: 0.8
       sl_multiplier: 1.2
       max_orders_multiplier: 0.5
       min_stable_minutes: 30
   ```

6. **Add Config Struct** (`config.go`)
   - Add `RecoveryStateConfig` struct
   - Add to config structure

**Testing:**
- Unit tests for recovery detection
- Integration test after EXIT_HALF
- Verify parameter adjustments in RECOVERY
- Test transition back to TRADING

## Tech Stack

### Existing Components
- **State Machine:** `internal/farming/adaptive_grid/state_machine.go`
- **Grid Manager:** `internal/farming/grid_manager.go`
- **Adaptive Manager:** `internal/farming/adaptive_grid/manager.go`
- **Config:** `internal/config/config.go`
- **YAML Config:** `backend/config/agentic-vf-config.yaml`

### New Components
- `internal/farming/adaptive_grid/volatility_monitor.go`
- `internal/farming/adaptive_grid/recovery_monitor.go`

## File Changes

### Modified Files
1. `internal/farming/adaptive_grid/state_machine.go`
   - Add state constants
   - Add event constants
   - Update transition logic
   - Update CanTransition()

2. `internal/farming/grid_manager.go`
   - Add checkPositionSize()
   - Update globalKlineProcessor to call new checks
   - Add state-aware order placement logic

3. `internal/farming/adaptive_grid/manager.go`
   - Add parameter adjustment logic per state
   - Update CanPlaceOrder() for state restrictions
   - Add config loading for new states

4. `internal/config/config.go`
   - Add OverSizeConfig struct
   - Add DefensiveStateConfig struct
   - Add RecoveryStateConfig struct
   - Add to main config structure

5. `backend/config/agentic-vf-config.yaml`
   - Add over_size config section
   - Add adaptive_states config section

### New Files
1. `internal/farming/adaptive_grid/market_condition_evaluator.go` (Phase 0)
   - Market condition evaluation logic
   - State recommendation engine
   - Confidence score calculation

2. `internal/farming/adaptive_grid/volatility_monitor.go` (Phase 2)
   - Volatility detection logic
   - DEFENSIVE state triggers

3. `internal/farming/adaptive_grid/recovery_monitor.go` (Phase 3)
   - Recovery detection logic
   - RECOVERY state triggers

## Testing Strategy

### Unit Tests
- State transition logic
- Volatility detection
- Position size checks
- Recovery detection
- Parameter adjustment calculations

### Integration Tests
- Full state transition flows
- State-aware order placement
- Config loading and application
- Multiple state transitions in sequence

### Simulation Tests
- Simulate volatility spike → DEFENSIVE → TRADING
- Simulate position size breach → OVER_SIZE → TRADING
- Simulate loss → EXIT_HALF → RECOVERY → TRADING
- Simulate extreme conditions → DEFENSIVE → EXIT_ALL

### Production Testing
- Deploy with dry-run mode
- Monitor state transitions
- Verify parameter adjustments
- Check order placement restrictions
- Validate recovery behavior

## Deployment Strategy

### Phase 0: Market Condition Evaluator
1. Implement and test evaluation logic
2. Deploy with config disabled
3. Enable config and monitor evaluation accuracy
4. Verify state recommendations match expected conditions
5. Adjust scoring thresholds based on production data

### Phase 1: OVER_SIZE
1. Implement and test
2. Deploy with config disabled
3. Enable config and monitor
4. Gradually lower threshold to production value

### Phase 2: DEFENSIVE
1. Implement and test
2. Deploy with high thresholds (conservative)
3. Monitor volatility detection
4. Adjust thresholds based on production data

### Phase 3: RECOVERY
1. Implement and test
2. Deploy with longer stable PnL requirement
3. Monitor recovery behavior
4. Adjust parameters based on production data

## Rollback Plan

Each phase can be independently rolled back:
- Disable config section to disable state
- Git revert for code changes
- No database changes (safe rollback)

## Monitoring

### State Transition Metrics
- Count of transitions per state
- Time spent in each state
- Transition frequency
- State transition success rate

### Performance Metrics
- Win rate by state
- PnL by state
- Order fill rate by state
- Position duration by state

### Alerting
- State stuck in DEFENSIVE > 30 minutes
- State stuck in OVER_SIZE > 1 hour
- State stuck in RECOVERY > 2 hours
- Rapid state transitions (> 10/minute)

## Success Criteria

### Phase 0: Market Condition Evaluator
- Evaluation factors accurately reflect market conditions
- State recommendations match expected behavior
- Confidence scores prevent rapid switching
- Integration with existing workers successful
- Evaluation performance overhead minimal (<10ms)

### OVER_SIZE
- Position size never exceeds hard cap
- Auto-reduction works correctly
- Reduce-only restriction enforced
- No regression in normal trading

### DEFENSIVE
- Extreme volatility detected accurately
- Parameters adjusted correctly
- No new positions in DEFENSIVE
- Smooth transition back to TRADING

### RECOVERY
- Recovery detected after losses
- Conservative parameters applied
- Stable PnL requirement enforced
- Successful return to TRADING

## Timeline

- **Phase 0 (Market Condition Evaluator):** 2-3 days (foundational)
- **Phase 1 (OVER_SIZE):** 1-2 days (after Phase 0)
- **Phase 2 (DEFENSIVE):** 2-3 days (after Phase 1 validation)
- **Phase 3 (RECOVERY):** 2-3 days (after Phase 2 validation)

**Total:** 7-11 days for full implementation

## Notes

- All states are backward compatible
- Can be individually enabled/disabled via config
- No database changes required
- Dry-run mode available for safe testing
- Each phase can be deployed independently
- **Phase 0 is foundational - must be implemented first**
- **Market Condition Evaluator leverages existing workers (globalKlineProcessor, RangeDetector, CircuitBreaker, RiskManager)**
- **State selection is evaluation-based, not just trigger-based**
- **States dynamically adapt to real-time market conditions**
- **Confidence scores prevent rapid state switching**
