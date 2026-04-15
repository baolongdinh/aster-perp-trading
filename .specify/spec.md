# Config Optimization Specification

## Feature Overview
Optimize core trade logic and configuration to achieve "perfect state" with minimal trade-offs:
- Maximize leverage utilization for volume farming and micro profit
- Minimize risk through intelligent parameter tuning
- Flexible mode adjustment based on market conditions
- Optimize scalp grid timing and configuration

## User Stories

### [P1] US1: Optimize Grid Spread Parameters
**Priority:** P1 (Critical - Immediate Fix)

**Story:**
As a volume farmer, I want grid spread parameters to be optimized for high leverage (20x-100x) so that I can achieve consistent fills without excessive liquidation risk.

**Acceptance Criteria:**
- Ranging spread increased from 0.02% to 0.06% (3x wider)
- Trending spread increased from 0.15% to 0.2%
- Volatile spread increased from 0.1% to 0.15%
- Base spread increased from 0.15% to 0.6%
- All spreads validated to be appropriate for leverage levels
- Win rate improves from 55-60% to 65-70%

**Notes:**
- Current 0.02% spread with 100x leverage = 2% price movement = liquidation risk
- Wider spreads reduce whipsaw losses and stop hunts
- Trade-off: Slightly lower fill rate for higher win rate

---

### [P1] US2: Optimize Take-Profit and Stop-Loss Parameters
**Priority:** P1 (Critical - Immediate Fix)

**Story:**
As a volume farmer, I want TP/SL parameters to be optimized for volatility so that positions can hit targets realistically without accumulating as stuck orders.

**Acceptance Criteria:**
- TP increased from 0.3% to 0.6% (2x wider)
- SL increased from 0.5% to 1.0% (2x wider)
- TP/SL ratio maintained at 1:2 (risk:reward)
- Parameters validated for 100x leverage
- Average position duration remains < 10 minutes
- Stuck position rate reduced by 50%

**Notes:**
- Current 0.3% TP with 0.02% spread = 15 grid levels = unrealistic
- Wider TP allows more realistic targets
- Wider SL provides buffer against volatility spikes

---

### [P1] US3: Increase Trending Regime Order Capacity
**Priority:** P1 (Critical - Immediate Fix)

**Story:**
As a volume farmer, I want the trending regime to support more orders so that I don't miss volume farming opportunities during trends.

**Acceptance Criteria:**
- Trending orders increased from 2/side to 5/side
- Trending spread maintained at 0.2% (wider than before)
- Trending size maintained at $0.8-1.0
- Volume during trending periods increases by 50%
- Risk per trade remains within acceptable limits

**Notes:**
- Current 2 orders/side too conservative for volume farming
- More orders = more fill opportunities = more volume
- Wider spread compensates for increased order count

---

### [P2] US4: Implement Dynamic Leverage Adjustment
**Priority:** P2 (High - Medium Priority)

**Story:**
As a volume farmer, I want leverage to automatically adjust based on volatility so that I can maximize leverage during calm periods and reduce risk during volatile periods.

**Acceptance Criteria:**
- Leverage multiplier based on ATR volatility:
  - Low volatility (ATR < 0.3%): 100x
  - Normal volatility (ATR 0.3-0.8%): 50x
  - High volatility (ATR 0.8-1.5%): 20x
  - Extreme volatility (ATR > 1.5%): 10x or pause
- Leverage adjustment happens smoothly (no sudden jumps)
- Position sizes automatically recalculated when leverage changes
- Liquidation risk reduced by 40% during volatile periods
- Volume maintained during calm periods

**Notes:**
- Requires integration with existing ATR calculation
- Need to update position sizing logic
- Must ensure smooth transitions between leverage levels

---

### [P2] US5: Implement Equity Curve Position Sizing
**Priority:** P2 (High - Medium Priority)

**Story:**
As a volume farmer, I want order sizes to automatically adjust based on current equity and recent performance so that I can compound gains and reduce risk after losses.

**Acceptance Criteria:**
- Position size based on Kelly Criterion: Size = Kelly% × Equity / Leverage
- Kelly fraction configurable (default 0.25 for conservative)
- Size reduction after consecutive losses (20% per loss)
- Size increase after winning streak (10% per win)
- Minimum size floor: $5 per order
- Maximum size cap: $100 per order
- 24-hour lookback window for win rate calculation
- Drawdown automatically reduces position size

**Notes:**
- Existing smart_sizing config partially implements this
- Need to enhance with proper equity tracking
- Must handle edge cases (equity near zero, consecutive losses)

---

### [P2] US6: Optimize Trading Hours Configuration
**Priority:** P2 (High - Medium Priority)

**Story:**
As a volume farmer, I want trading hours to be optimized for volume farming so that I focus on low-volatility periods and minimize exposure during high-risk periods.

**Acceptance Criteria:**
- Consider eliminating US session entirely (19:00-23:00)
- Or reduce US session to 0.15x size (from 0.3x)
- Add more Asian session hours if beneficial
- Add weekend pause option
- Total trading hours optimized for volume/risk balance
- Volume maintained or increased with reduced hours

**Notes:**
- US session currently 0.3x size but still high risk
- Asian session (07:00-12:00) is ideal for grid trading
- Need to analyze historical performance by session

---

### [P3] US7: Implement Micro-Grid Scalping Mode
**Priority:** P3 (Low - Advanced Feature)

**Story:**
As a volume farmer, I want an ultra-short-term scalping mode so that I can capture micro-profits during very calm market conditions.

**Acceptance Criteria:**
- Scalping mode with 10-30 second position durations
- TP targets at 0.1-0.2% (very tight)
- Grid spread at 0.01-0.02% (extremely tight)
- Only active during low volatility (ATR < 0.2%)
- Only active during Asian session
- Size multiplier: 0.5x normal
- Automatic switch to normal mode if volatility increases

**Notes:**
- High-frequency trading mode
- Requires very low latency execution
- Only for very calm markets
- Can generate significant volume in short time

---

### [P3] US8: Implement Funding Rate Optimization
**Priority:** P3 (Low - Advanced Feature)

**Story:**
As a volume farmer, I want position bias to automatically adjust based on funding rate so that I can minimize funding costs and capture funding arbitrage.

**Acceptance Criteria:**
- Bias positions against funding direction (e.g., long when funding negative)
- Bias strength configurable (default 70%)
- Reduce position size when funding > 0.05%
- Skip positions when funding > 0.1%
- Check funding rate every 5 minutes
- Track funding cost vs grid profit ratio
- Alert when funding cost > 50% of grid profit

**Notes:**
- Existing funding_rate config partially implements this
- Need to enhance with position bias logic
- Can turn funding from cost to profit

---

### [P3] US9: Implement Correlation Hedging
**Priority:** P3 (Low - Advanced Feature)

**Story:**
As a volume farmer, I want correlation monitoring and hedging so that I can reduce risk when trading multiple correlated symbols.

**Acceptance Criteria:**
- Monitor correlation between symbols (rolling 30-day window)
- Reduce total exposure when correlation > 0.8
- Add hedging positions when correlation high and positions opposite
- Correlation threshold configurable
- Hedge ratio based on beta
- Automatic hedge unwinding when correlation decreases

**Notes:**
- Currently only trading 1 symbol (ETHUSD1)
- Future-proof for multi-symbol trading
- Complex feature, requires careful testing

---

## Dependencies

**Story Dependency Graph:**
```
US1 (Grid Spread) ──────┐
US2 (TP/SL) ───────────┤
US3 (Trending Orders) ─┤
                       ├─── US4 (Dynamic Leverage)
US5 (Equity Sizing) ───┤       ├─── US6 (Trading Hours)
                       └───────┤
                                ├─── US7 (Scalping)
                                ├─── US8 (Funding)
                                └─── US9 (Correlation)
```

**Explanation:**
- US1, US2, US3 are independent and can be done in parallel (Phase 1)
- US4, US5, US6 depend on Phase 1 fixes (Phase 2)
- US7, US8, US9 are advanced features (Phase 3)

## MVP Scope

**Recommended MVP:** Phase 1 only (US1, US2, US3)
- Immediate fixes with highest impact
- Low complexity, low risk
- Can be implemented and tested quickly
- Expected win rate improvement: 10-15%

**Full Scope:** All 9 stories
- Complete optimization roadmap
- Expected win rate improvement: 15-20%
- Expected drawdown reduction: 50%
