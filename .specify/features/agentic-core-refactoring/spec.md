# Agentic Core Logic Refactoring Specification

## Feature Overview
Transform the bot from state-based logic to condition-based logic to achieve true "agentic" behavior - able to adapt to any market condition, turn danger into opportunity, and continuously hunt micro-profits in all market regimes.

**Current Problem**: Bot uses hardcoded state-based logic with fixed thresholds, making it rigid and unable to adapt to rapid market changes.

**Target State**: Bot uses continuous condition-based logic with adaptive thresholds, dynamic parameters, and graduated trading modes to maximize profitability in all conditions.

## Vision Statement
> "Bot phải thiên biến vạn hóa, biến nguy thành cơ trong mọi loại điều kiện thị trường, luôn săn được micro profit liên tục"

## User Stories

### [P0] US1: Replace State Blocking with Conditional Blocking
**Priority:** P0 (Critical - Foundation)

**Story:**
As an agentic bot, I want order placement to be blocked based on real-time conditions rather than fixed state names so that I can continue hunting micro-profits even during adverse conditions.

**Acceptance Criteria:**
- Remove hardcoded state blocking in CanPlaceOrder (OVER_SIZE, DEFENSIVE, RECOVERY, EXIT_HALF, EXIT_ALL)
- Implement conditional blocking based on combined metrics:
  - Position size percentage (0-100% of max)
  - Volatility score (0-1 normalized)
  - Risk score (0-1 normalized)
  - Trend strength (0-1 normalized)
  - Inventory skew (-1 to 1)
- Calculate dynamic blocking factor (0-100%) instead of binary block
- Allow partial trading (reduce size by blocking factor) instead of full block
- Micro-trading mode (1-10% size) always allowed regardless of conditions
- Win rate increases by 15-20% through continuous trading

**Notes:**
- Current: if state == OVER_SIZE → return false (100% block)
- Target: if positionSize > 80% AND volatility > 0.7 AND risk > 0.8 → blockFactor = 90%
- Bot can still place 10% size trades to hunt micro-profits
- Blocking factor adjusts dynamically as conditions change

---

### [P0] US2: Implement Continuous State Space
**Priority:** P0 (Critical - Foundation)

**Story:**
As an agentic bot, I want to represent my state as continuous multi-dimensional space rather than discrete states so that I can make nuanced decisions based on exact conditions.

**Acceptance Criteria:**
- Replace discrete states (IDLE, TRADING, OVER_SIZE, etc.) with continuous state space:
  - Position size: 0-100% of max
  - Volatility: 0-1 (normalized ATR + BB width)
  - Risk: 0-1 (normalized PnL + drawdown)
  - Trend: 0-1 (normalized ADX)
  - Skew: -1 to 1 (inventory imbalance)
- Bot behavior = f(position_size, volatility, risk, trend, skew)
- Implement smooth transitions between behavioral modes
- No sudden jumps in behavior
- State space visualization in dashboard
- Real-time state space tracking and logging

**Notes:**
- Discrete states cause rigid behavior (e.g., stuck in OVER_SIZE at 81%)
- Continuous space allows nuanced decisions (e.g., reduce size by 81% at 81% position)
- Enables true "agentic" behavior based on exact conditions

---

### [P0] US3: Add Adaptive Thresholds
**Priority:** P0 (Critical - Foundation)

**Story:**
As an agentic bot, I want my decision thresholds to adapt based on symbol characteristics, market regime, and recent performance so that I can optimize for each unique situation.

**Acceptance Criteria:**
- Replace fixed thresholds (0.8, 0.6) with adaptive thresholds:
  - Symbol-specific: Different thresholds for BTC vs SOL based on historical volatility
  - Regime-specific: Different thresholds for ranging vs trending vs volatile
  - Performance-based: Adjust thresholds based on recent win rate
  - Time-based: Different thresholds for Asian vs European vs US sessions
  - Funding-based: Adjust thresholds based on funding rate environment
- Implement threshold learning mechanism:
  - Track which threshold values work best in which conditions
  - Adapt thresholds over time based on performance
  - A/B test different threshold combinations
- Configurable threshold adaptation rate
- Threshold change logging and alerts
- Dashboard visualization of threshold evolution

**Notes:**
- Current: PositionScore > 0.8 → OVER_SIZE (fixed for all symbols)
- Target: PositionScore > adaptive_threshold(symbol, regime, time, performance) → OVER_SIZE
- BTC might have threshold 0.85, SOL might have 0.75 due to different volatility
- Thresholds evolve as bot learns

---

### [P1] US4: Implement Graduated Trading Modes
**Priority:** P1 (High - Significant Impact)

**Story:**
As an agentic bot, I want to have graduated trading modes (FULL/REDUCED/MICRO) instead of binary can/cannot trade so that I can continue trading at reduced capacity during degraded conditions.

**Acceptance Criteria:**
- Replace binary CircuitBreaker decision with graduated modes:
  - FULL mode: 100% size, all strategies enabled
  - REDUCED mode: 50% size, conservative strategies only
  - MICRO mode: 10% size, micro-profit strategies only
  - PAUSED mode: 0% size, no trading
- Mode transitions based on conditions:
  - Mode = f(risk, volatility, drawdown, consecutive_losses, funding_rate)
- Always allow MICRO mode for volume farming even in degraded states
- Mode-specific parameter sets:
  - FULL: Normal spread, normal order count, normal size
  - REDUCED: Wider spread, fewer orders, reduced size
  - MICRO: Ultra-tight spread, many orders, tiny size
- Mode change cooldown (minimum 5 minutes in each mode)
- Dashboard mode indicator
- Mode transition logging

**Notes:**
- Current: CircuitBreaker tripped → cannot trade (100% block)
- Target: CircuitBreaker tripped → switch to MICRO mode (10% size trading)
- Bot continues hunting micro-profits even during adverse conditions
- Gradual mode changes reduce shock to system

---

### [P1] US5: Implement Dynamic Position Sizing
**Priority:** P1 (High - Significant Impact)

**Story:**
As an agentic bot, I want my order sizes to dynamically adjust based on real-time risk, opportunity, and liquidity so that I can maximize profit when conditions are favorable and minimize risk when conditions are adverse.

**Acceptance Criteria:**
- Replace fixed order sizes with dynamic sizing:
  - Base size = f(equity, leverage, max_position)
  - Risk adjustment = f(drawdown, consecutive_losses, unrealized_PnL)
  - Opportunity adjustment = f(spread_tightness, market_depth, funding_arbitrage)
  - Liquidity adjustment = f(order_book_depth, recent_volume)
- Implement Kelly Criterion sizing:
  - Kelly% = (win_rate × avg_win - loss_rate × avg_loss) / avg_win
  - Size = Kelly% × Equity / Leverage
  - Configurable Kelly fraction (default 0.25 conservative)
- Consecutive loss decay:
  - Size reduction: 20% per consecutive loss
  - Size recovery: 10% per consecutive win
  - Maximum reduction: 90% (minimum 10% size)
- Market depth consideration:
  - Tight spread + high depth → increase size
  - Wide spread + low depth → decrease size
- Real-time size adjustment (every kline)
- Size logging and dashboard visualization

**Notes:**
- Current: Order size = $5 (fixed per regime)
- Target: Order size = f(equity, risk, opportunity, liquidity) (dynamic)
- Bot adapts size to current conditions in real-time
- Maximizes profit when favorable, minimizes risk when adverse

---

### [P1] US6: Implement Adaptive Grid Geometry
**Priority:** P1 (High - Significant Impact)

**Story:**
As an agentic bot, I want my grid geometry (spread, order count, spacing) to dynamically adjust based on volatility, skew, and market depth so that I can optimize for current market conditions.

**Acceptance Criteria:**
- Replace fixed grid geometry with adaptive geometry:
  - Spread = f(volatility, skew, funding_rate, time_of_day)
  - Order count = f(market_depth, risk_tolerance, regime)
  - Spacing = f(volatility, trend_strength)
- Volatility-based spread:
  - Low volatility (ATR < 0.3%): tight spread 0.02-0.05%
  - Normal volatility (ATR 0.3-0.8%): normal spread 0.05-0.1%
  - High volatility (ATR 0.8-1.5%): wide spread 0.1-0.2%
  - Extreme volatility (ATR > 1.5%): very wide spread 0.2-0.3%
- Skew-based asymmetric grids:
  - Long skew → more sell orders, tighter buy spread
  - Short skew → more buy orders, tighter sell spread
  - Skew strength determines asymmetry degree
- Market depth-based order count:
  - High depth → more orders (up to 15/side)
  - Low depth → fewer orders (as low as 3/side)
- Real-time grid rebalancing:
  - Rebuild grid when conditions change significantly
  - Smart rebuild: only replace orders at changed levels
- Grid geometry logging and dashboard visualization

**Notes:**
- Current: Spread = 0.02% (fixed), Orders = 10/side (fixed)
- Target: Spread = f(volatility, ...), Orders = f(depth, ...) (dynamic)
- Bot adapts grid to current conditions
- Optimizes for fill rate vs risk in real-time

---

### [P1] US7: Make Regrid Logic State-Agnostic
**Priority:** P1 (High - Significant Impact)

**Story:**
As an agentic bot, I want to be able to regrid from any state when conditions are favorable so that I don't get stuck in states longer than necessary.

**Acceptance Criteria:**
- Remove state restriction from isReadyForRegrid
- Allow regrid from all states based on conditions:
  - OVER_SIZE: position ≤ 60% max + ADX < threshold + BB width < threshold
  - DEFENSIVE: BB width < threshold + ADX < threshold
  - RECOVERY: PnL ≥ 0 + stable for X minutes
  - EXIT_HALF: PnL ≥ 0
  - EXIT_ALL: position = 0 OR timeout (dynamic based on PnL/volatility)
- State-specific regrid criteria:
  - Each state has its own exit conditions
  - Conditions checked every kline
  - Auto-transition when conditions met
- Configurable thresholds per symbol
- Regrid logging with reason
- Dashboard regrid status indicator

**Notes:**
- Current: Only IDLE/WAIT_NEW_RANGE can regrid
- Target: Any state can regrid when conditions met
- Bot doesn't get stuck in states
- Faster adaptation to market changes

---

### [P1] US8: Implement Conditional State Transitions
**Priority:** P1 (High - Significant Impact)

**Story:**
As an agentic bot, I want to be able to transition between states based on combined conditions rather than fixed paths so that I can handle rapid market changes and multiple simultaneous conditions.

**Acceptance Criteria:**
- Replace fixed transition paths with conditional transitions
- Allow transitions based on combined conditions:
  - OVER_SIZE → DEFENSIVE if position large AND volatility spikes
  - DEFENSIVE → RECOVERY if loss occurs during defensive period
  - RECOVERY → OVER_SIZE if position grows during recovery
  - Any state → EXIT_ALL if emergency condition (extreme drawdown, liquidation risk)
- Implement emergency transition paths for rapid changes:
  - Skip intermediate states when conditions change rapidly
  - Direct transition to safest state when multiple risks high
- State merging capability:
  - OVER_SIZE + DEFENSIVE can coexist
  - Apply both state behaviors simultaneously
- Transition confidence scoring:
  - Only transition if confidence > threshold
  - Confidence = f(condition_strength, stability_duration)
- Transition logging with full context
- Dashboard state transition visualization

**Notes:**
- Current: Fixed paths (OVER_SIZE → TRADING only via SizeNormalized)
- Target: Conditional paths (OVER_SIZE → DEFENSIVE if conditions warrant)
- Bot handles complex, rapidly changing conditions
- No stuck states due to rigid paths

---

### [P2] US9: Implement Dynamic Exit Logic
**Priority:** P2 (Medium - Important)

**Story:**
As an agentic bot, I want my exit logic to be dynamic based on position PnL, market conditions, and time so that I don't force suboptimal exits.

**Acceptance Criteria:**
- Replace fixed 15s EXIT_ALL timeout with dynamic timeout:
  - Timeout = f(position_PnL, volatility, time_of_day, market_regime)
  - Profitable position: longer timeout (up to 60s)
  - Loss position: shorter timeout (as low as 5s)
  - Calm market: longer timeout
  - Volatile market: shorter timeout
- Add graduated exit options:
  - 25% exit: reduce position by 25%
  - 50% exit: reduce position by 50%
  - 75% exit: reduce position by 75%
  - 100% exit: close entire position
- Recovery probability consideration:
  - Estimate recovery probability based on conditions
  - Only force exit if recovery probability < threshold
  - Allow position to continue if recovery probability high
- Exit decision logging with full rationale
- Dashboard exit status indicator

**Notes:**
- Current: Fixed 15s timeout regardless of conditions
- Target: Dynamic timeout based on PnL, volatility, time
- Bot doesn't force suboptimal exits
- Maximizes profit, minimizes loss

---

### [P2] US10: Implement Real-Time Parameter Optimization
**Priority:** P2 (Medium - Important)

**Story:**
As an agentic bot, I want to continuously optimize my parameters in real-time based on current conditions so that I can always operate at peak efficiency.

**Acceptance Criteria:**
- Real-time parameter recalculation (every kline, 1s):
  - Optimal spread based on volatility
  - Optimal order count based on depth
  - Optimal size based on risk/opportunity
  - Optimal mode based on conditions
- Multi-objective optimization:
  - Maximize profit
  - Minimize risk
  - Maximize volume
  - Minimize drawdown
- Pareto frontier exploration:
  - Find optimal trade-off between objectives
  - Select operating point based on current priorities
- Parameter change smoothing:
  - No sudden jumps in parameters
  - Gradual adjustment over time
- Parameter optimization logging
- Dashboard parameter visualization

**Notes:**
- Current: Parameters fixed per regime
- Target: Parameters optimized in real-time
- Bot always operates at peak efficiency
- Adapts to changing conditions instantly

---

### [P3] US11: Implement Learning/Adaptation Mechanism
**Priority:** P3 (Low - Advanced)

**Story:**
As an agentic bot, I want to learn from my performance and adapt my strategy over time so that I continuously improve.

**Acceptance Criteria:**
- Track which strategies work in which conditions:
  - Condition → Strategy → Performance mapping
  - Historical performance database
- Adapt thresholds based on recent performance:
  - If threshold too aggressive → increase
  - If threshold too conservative → decrease
- Learn optimal parameters per symbol:
  - Each symbol has different optimal parameters
  - Adapt to symbol characteristics over time
- Implement reinforcement learning:
  - Agent learns optimal policy through trial and error
  - Reward = profit - risk_penalty
  - Policy evolves over time
- Performance tracking and reporting:
  - Track improvement over time
  - Compare to baseline
  - A/B test different strategies

**Notes:**
- Advanced feature requiring significant development
- Enables continuous improvement
- Bot gets smarter over time

---

### [P3] US12: Implement Micro-Profit Hunter Mode
**Priority:** P3 (Low - Advanced)

**Story:**
As an agentic bot, I want a dedicated micro-profit hunting mode that always runs in the background to capture small arbitrage opportunities regardless of my main state.

**Acceptance Criteria:**
- Micro-profit mode always enabled (1-5% size)
- Hunt micro-arbitrage opportunities:
  - Cross-exchange arbitrage (if applicable)
  - Funding rate arbitrage
  - Micro price inefficiencies
  - Order book imbalances
- Ultra-tight spreads (0.01-0.02%)
- Ultra-fast execution (10-30 second positions)
- Separate from main trading logic
- Runs in parallel with main strategy
- Micro-profit tracking and reporting
- Dashboard micro-profit indicator

**Notes:**
- Always-on background mode
- Generates small continuous profits
- Low risk due to small size
- Complements main strategy

---

## Dependencies

**Story Dependency Graph:**
```
US1 (Conditional Blocking) ──────┐
US2 (Continuous State Space) ────┤
US3 (Adaptive Thresholds) ──────┤
                               ├─── US4 (Graduated Modes)
US5 (Dynamic Sizing) ───────────┤       ├─── US6 (Adaptive Grid)
US7 (State-Agnostic Regrid) ────┤
US8 (Conditional Transitions) ───┤
                               └───────┤
                                        ├─── US9 (Dynamic Exit)
US10 (Real-Time Optimization) ─────────┤
                                        ├─── US11 (Learning)
                                        └─── US12 (Micro-Profit)
```

**Explanation:**
- US1, US2, US3 are foundational (Phase 0) - must be done first
- US5, US7, US8 build on foundation (Phase 1)
- US4, US6, US9, US10 are significant improvements (Phase 2)
- US11, US12 are advanced features (Phase 3)

## MVP Scope

**Recommended MVP:** Phase 0 + Phase 1 (US1-8)
- Foundation changes (US1-3)
- High-impact improvements (US5-8)
- Expected win rate improvement: 25-35%
- Expected drawdown reduction: 60-70%
- Bot becomes truly "agentic"

**Full Scope:** All 12 stories
- Complete agentic transformation
- Continuous learning and optimization
- Expected win rate improvement: 35-50%
- Expected drawdown reduction: 70-80%
- Bot continuously improves over time

## Success Criteria

- **Bot never stuck in any state** (max 15s in EXIT_ALL with dynamic timeout)
- **Continuous trading** (MICRO mode always available)
- **Dynamic parameters** (adjust every kline based on conditions)
- **Adaptive thresholds** (evolve based on performance)
- **Win rate improvement**: 25-35% (MVP), 35-50% (Full)
- **Drawdown reduction**: 60-70% (MVP), 70-80% (Full)
- **Volume maintained or increased** despite risk reduction
- **Bot survives any market condition** (crash, spike, ranging, trending)
- **Bot hunts micro-profit continuously** in all conditions
