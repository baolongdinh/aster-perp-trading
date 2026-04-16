# Volume Farming Optimization - Feature Specification

## Overview
Advanced optimization strategies for volume farming with minimal risk exposure. This feature adds 4 critical improvements to the existing volume farming bot to maximize volume generation while minimizing risk.

## Business Context

### Current State
- Bot farms volume using grid trading strategy
- Places limit orders on both sides of the order book
- Generates volume through maker orders (low fees)
- No protection against adverse selection
- No optimization for order priority
- No inventory management for long-term farming

### Problem Statement
1. **Low Fill Rate**: Orders may not reach top of book, resulting in low fill rates
2. **Adverse Selection Risk**: Bot may fill orders right before price crashes (toxic flow)
3. **Taker Fees**: Limit orders can become market orders during fast price movement, incurring high fees
4. **Inventory Imbalance**: Long-term farming leads to inventory skew, requiring manual intervention

### Success Criteria
- Increase fill rate by 20-30%
- Reduce adverse selection losses by 50%
- Eliminate taker fees in 95%+ of orders
- Maintain inventory within ±30% of neutral position

---

## Feature Requirements

### 1. Order Priority Optimization

#### 1.1 Tick-size Awareness
**Description**: Bot must be aware of exchange tick-sizes and ensure all orders are placed on valid ticks.

**Requirements**:
- Fetch and cache tick-sizes for all trading symbols
- Round all order prices to valid tick increments
- Log warnings when tick-size is unknown or invalid

**Acceptance Criteria**:
- All grid levels are on valid tick sizes
- No order rejections due to invalid tick size
- Tick-size cache updates every 24 hours or on error

#### 1.2 Penny Jumping Strategy (Optional - Phase 3)
**Description**: Place orders 1 tick above best bid/below best ask to prioritize fills.

**Requirements**:
- Monitor order book for best bid/ask
- Calculate optimal price: best bid + 1 tick (buy) or best ask - 1 tick (sell)
- Ensure jumped price stays within spread limit (e.g., 10% of spread)
- Limit maximum jump to prevent over-optimization

**Acceptance Criteria**:
- Fill rate increases by 20-30%
- Average spread received doesn't decrease by more than 5%
- No order rejections due to price outside valid range

---

### 2. Toxic Flow Detection (VPIN)

#### 2.1 VPIN Indicator
**Description**: Implement Volume Probability of Informed Trading indicator to detect toxic flow.

**Requirements**:
- Track buy/sell volume in time buckets (e.g., 1000 USDT per bucket)
- Calculate VPIN = |Buy - Sell| / (Buy + Sell) over N buckets
- VPIN ranges from 0 (balanced) to 1 (highly toxic)
- Update VPIN in real-time as orders fill

**Acceptance Criteria**:
- VPIN calculation is accurate and consistent
- VPIN updates within 100ms of order fill
- Historical VPIN data is retained for analysis

#### 2.2 Toxic Flow Response
**Description**: Take protective action when toxic flow is detected.

**Requirements**:
- Define VPIN threshold (default: 0.3)
- Configurable action on toxic flow: pause, widen_spread, reduce_size
- Log toxic flow events with context (symbol, VPIN value, action taken)
- Auto-resume when VPIN returns to normal

**Acceptance Criteria**:
- Bot pauses within 200ms of VPIN threshold breach
- No orders placed during toxic flow period
- Auto-resume within 5 seconds of VPIN normalization

---

### 3. Maker/Taker Logic Optimization

#### 3.1 Post-Only Orders
**Description**: Always use post-only flag to ensure orders only fill as maker orders.

**Requirements**:
- Set post-only flag on all grid limit orders
- Handle post-only rejections (order cannot be maker):
  - Cancel and retry with adjusted price
  - Or skip order placement for this cycle
- Log post-only rejections with reason

**Acceptance Criteria**:
- 95%+ of filled orders are maker orders
- Post-only rejections are handled gracefully
- No order fills as taker when post-only is enabled

#### 3.2 Smart Cancellation
**Description**: Automatically cancel and replace orders when spread changes rapidly.

**Requirements**:
- Monitor current spread vs last spread
- Trigger cancellation if spread changes by > X% (default: 20%)
- Cancel all pending orders and rebuild grid with new spread
- Check spread at regular intervals (default: 5 seconds)

**Acceptance Criteria**:
- Grid rebuilds within 1 second of spread change trigger
- No orders remain at stale prices after spread change
- Spread change detection is accurate

---

### 4. Self-Healing Inventory Management

#### 4.1 Inventory Monitoring
**Description**: Track inventory skew and trigger hedging when threshold exceeded.

**Requirements**:
- Calculate inventory percentage: inventory / max_position
- Define hedge threshold (default: 30%)
- Log inventory skew warnings when approaching threshold

**Acceptance Criteria**:
- Inventory percentage is calculated accurately
- Warnings logged at 25%, 30%, 35% of max position
- No false positives/negatives in inventory calculation

#### 4.2 Internal Hedging
**Description**: Execute hedge orders to reduce inventory skew.

**Requirements**:
- Support multiple hedging modes:
  - Internal: Hedge on same symbol with opposite side
  - Cross-pair: Hedge on correlated pair (e.g., BTC → ETH)
  - Scalping: Quick scalping orders to reduce inventory
- Calculate hedge size: |inventory| * hedge_ratio (default: 30%)
- Limit max hedge size (default: 100 USDT)
- Log all hedge executions

**Acceptance Criteria**:
- Hedge orders execute within 500ms of threshold breach
- Inventory returns to within 20% of neutral after hedge
- Hedge orders don't interfere with grid operations

---

## Non-Functional Requirements

### Performance
- VPIN calculation: <10ms per update
- Tick-size rounding: <1ms per order
- Post-only order handling: <5ms per rejection
- Smart cancellation check: <5ms per cycle
- Inventory monitoring: <1ms per check

### Reliability
- VPIN indicator must not crash on invalid data
- Tick-size cache must handle API failures gracefully
- Post-only mode must have fallback to regular limit orders
- Hedging must have manual override capability

### Security
- No unauthorized API calls for hedging
- Inventory thresholds cannot be bypassed without authorization
- VPIN threshold changes require configuration update

### Maintainability
- All new components must have unit tests
- Integration tests for end-to-end flows
- Configuration for all tunable parameters
- Clear logging for debugging

---

## Dependencies

### External
- Exchange API for tick-size fetching
- Exchange API for order book data (best bid/ask)
- Exchange API for post-only order placement

### Internal
- Grid Manager (for order placement/cancellation)
- Risk Monitor (for position tracking)
- Config Manager (for parameter loading)
- Logger (for event logging)

---

## Risks & Mitigations

### Risk 1: Penny Jumping Over-Optimization
**Description**: Aggressive penny jumping may reduce spread too much.

**Mitigation**:
- Start with conservative jump threshold (10% of spread)
- Monitor spread received vs baseline
- Disable if spread decreases by >5%

### Risk 2: VPIN False Positives
**Description**: Normal market volatility may trigger toxic flow detection.

**Mitigation**:
- Use appropriate threshold (0.3)
- Require sustained VPIN breach (e.g., 2 consecutive buckets)
- Manual override for emergency situations

### Risk 3: Post-Only Rejection Spam
**Description**: Fast-moving markets may cause many post-only rejections.

**Mitigation**:
- Implement exponential backoff for retries
- Skip order placement if rejections > 5 per minute
- Fallback to regular limit orders after N rejections

### Risk 4: Hedging Losses
**Description**: Hedge orders may incur losses if price moves unfavorably.

**Mitigation**:
- Use conservative hedge ratio (30%)
- Limit max hedge size
- Monitor hedge PnL and disable if losses exceed threshold

---

## Configuration

### Example Configuration
```yaml
volume_farming_optimization:
  enabled: true

  # Order Priority
  order_priority:
    tick_size_awareness:
      enabled: true
      tick_sizes:
        BTC: 0.1
        ETH: 0.01
        SOL: 0.001
        default: 0.01
    penny_jumping:
      enabled: false  # Phase 3
      jump_threshold: 0.1
      max_jump: 3

  # Toxic Flow Detection
  toxic_flow_detection:
    enabled: true
    window_size: 50
    bucket_size: 1000.0
    vpin_threshold: 0.3
    sustained_breaches: 2
    action: "pause"  # pause, widen_spread, reduce_size
    auto_resume_delay: 5s

  # Maker/Taker Optimization
  maker_taker_optimization:
    post_only_enabled: true
    post_only_fallback: true
    smart_cancellation:
      enabled: true
      spread_change_threshold: 0.2
      check_interval: 5s

  # Self-Healing Inventory
  inventory_hedging:
    enabled: true
    hedge_threshold: 0.3
    hedge_ratio: 0.3
    max_hedge_size: 100.0
    hedging_mode: "internal"  # internal, cross_pair, scalping
    hedge_pair: "ETH"  # For cross-pair hedging
```

---

## Success Metrics

### Quantitative
- Fill rate: +20-30%
- Taker fee ratio: <5%
- Adverse selection losses: -50%
- Inventory skew: <30% deviation
- VPIN detection accuracy: >90%

### Qualitative
- Reduced manual intervention
- Better protection against market crashes
- More consistent volume generation
- Improved risk-adjusted returns

---

## Implementation Phases

### Phase 1 (High Priority)
1. Post-Only Orders
2. VPIN Detection

### Phase 2 (Medium Priority)
3. Tick-size Awareness
4. Smart Cancellation

### Phase 3 (Advanced - Requires Backtesting)
5. Penny Jumping
6. Self-Healing Inventory

---

## Open Questions

1. Should penny jumping be enabled by default or require backtesting first?
2. What is the optimal VPIN threshold for this market?
3. Should hedging support cross-pair or internal mode first?
4. What is the maximum acceptable spread reduction from penny jumping?

---

## References
- Volume Farming Optimization Analysis: `backend/VOLUME_FARMING_OPTIMIZATION.md`
- Existing Grid Manager: `backend/internal/farming/grid_manager.go`
- Existing Risk Monitor: `backend/internal/farming/risk_monitor.go`
