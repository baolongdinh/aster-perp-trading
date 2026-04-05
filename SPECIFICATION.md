# Grid Trading Bot Optimization Specification
## Advanced Risk Management & Profit Maximization

**Version:** 1.0  
**Date:** April 6, 2026  
**Status:** Draft Specification  
**Author:** Trading Bot Engineering Team

---

## Executive Summary

This specification outlines advanced optimization features for the grid trading bot to address critical issues including:
- **Liquidation Risk**: Bot accumulating positions during trending markets
- **Inefficient Spacing**: Fixed grid spreads not adapting to volatility
- **Position Management**: No inventory skew adjustment based on net exposure
- **Stop-Loss Gaps**: Single-account stop-loss vulnerable to wicks
- **Technical Safeguards**: Missing backend protections for high-volatility scenarios

---

## 1. Dynamic Grid Logic (Lưới Động)

### 1.1 Problem Statement
- **Current**: Fixed grid spread (e.g., 0.5%) regardless of market conditions
- **Issue**: Too tight in volatile markets = grid gets "swept" in one candle; Too wide in calm markets = misses opportunities

### 1.2 Solution: ATR-Based Adaptive Spreading

#### Functional Requirements

**FR-1.1.1 Volatility Measurement**
- System shall calculate ATR(14) on 15m or 1h timeframe
- ATR shall be expressed as percentage of current price
- Update frequency: Every 5 minutes or on significant price movement (>0.5%)

**FR-1.1.2 Dynamic Spread Calculation**
```
BaseSpread = User configured default (e.g., 0.5%)
ATR_Percent = ATR / CurrentPrice

If ATR_Percent < 0.3%:  // Low volatility
    SpreadMultiplier = 0.6  // Tighter grid
    
Else If ATR_Percent < 0.8%:  // Normal volatility  
    SpreadMultiplier = 1.0  // Normal spread
    
Else If ATR_Percent < 1.5%:  // High volatility
    SpreadMultiplier = 1.8  // Wider spread
    
Else:  // Extreme volatility
    SpreadMultiplier = 2.5  // Very wide + reduce order count
    
DynamicSpread = BaseSpread × SpreadMultiplier
```

**FR-1.1.3 Grid Level Adjustment**
- When SpreadMultiplier > 2.0: Reduce max grid levels by 30%
- When SpreadMultiplier < 0.7: Increase max grid levels by 20% (cap at 10 levels per side)

**FR-1.1.4 Volatility Regime Logging**
- Log current ATR, spread multiplier, and grid configuration every 15 minutes
- Alert when entering/exiting "Extreme Volatility" regime

---

## 2. Inventory Skew Logic (Lệch Vị Thế)

### 2.1 Problem Statement
- **Current**: Symmetric grid (equal buy/sell levels)
- **Issue**: During trend, bot accumulates positions one side, increasing liquidation risk

### 2.2 Solution: Position-Based Skew Adjustment

#### Functional Requirements

**FR-2.1.1 Inventory Tracking**
- System shall track net position (total long - total short) for each symbol
- Calculate as: NetExposure = Σ(BuyOrders) - Σ(SellOrders)
- Express in both base currency quantity and USD notional value

**FR-2.1.2 Skew Coefficient Calculation**
```
MaxInventory = AccountEquity × MaxInventoryPct (e.g., 30%)
CurrentInventory = abs(NetExposure)
SkewRatio = CurrentInventory / MaxInventory  // 0.0 to 1.0+

If SkewRatio < 0.3:  // Minimal skew
    Action = "NORMAL"
    
Else If SkewRatio < 0.6:  // Moderate skew
    Action = "REDUCE_SKEW_SIDE"
    Reduce size of orders on skewed side by 30%
    
Else If SkewRatio < 0.8:  // Significant skew  
    Action = "PAUSE_SKEW_SIDE"
    Stop placing new orders on skewed side
    Reduce opposite side take-profit targets by 20%
    
Else:  // Critical skew
    Action = "EMERGENCY_SKEW"
    Stop all new orders on skewed side
    Close furthest losing positions via market order
    Reduce all take-profit targets to breakeven + 0.1%
```

**FR-2.1.3 Dynamic Take-Profit Adjustment**
- When SkewRatio > 0.5: Reduce take-profit distance for winning side by:
  - SkewRatio 0.5-0.7: Reduce 15%
  - SkewRatio 0.7-0.9: Reduce 30%
  - SkewRatio > 0.9: Reduce 50% (exit ASAP)

**FR-2.1.4 Inventory Rebalancing Priority**
- Priority 1: Close furthest underwater positions first
- Priority 2: Reduce take-profit targets for winning positions
- Priority 3: Pause new skew-amplifying orders

---

## 3. Cluster Stop-Loss Logic

### 3.1 Problem Statement
- **Current**: Single stop-loss for entire account or none at all
- **Issue**: One wick can liquidate entire position; No time-based decay for stuck positions

### 3.2 Solution: Multi-Layer Cluster Management

#### 3.2.1 Time-Based Cluster Stop-Loss

**FR-3.1.1 Position Age Tracking**
- Track entry time for each grid cluster (group of orders at similar levels)
- Cluster definition: Orders within 2×ATR of each other

**FR-3.1.2 Time Decay Thresholds**
```
PositionAge = CurrentTime - EntryTime

If PositionAge > 2 hours AND UnrealizedPnL < -0.5%:
    Status = "MONITOR_CLOSE"
    
If PositionAge > 4 hours AND UnrealizedPnL < -1.0%:
    Status = "EMERGENCY_CLOSE"
    Close cluster at market price
    
If PositionAge > 8 hours (any PnL):
    Status = "STALE_CLOSE"
    Close at breakeven or small loss
    Log: "Position stale, trend established against position"
```

**FR-3.1.3 Breakeven Exit Logic**
- When price returns toward entry after significant drawdown:
  - If drawdown was > 2% and now recovered to -0.2%:
    - Close 50% of cluster immediately (lock in reduced loss)
  - If drawdown was > 3% and now at breakeven:
    - Close 100% of cluster (grateful exit)
    - Log: "Breakeven exit after significant drawdown"

#### 3.2.2 Cluster Heat Map

**FR-3.2.1 Visual/Log Representation**
- Create cluster heat map showing:
  - Cluster entry price
  - Current distance from entry
  - Age of cluster
  - Recommended action (HOLD/MONITOR/CLOSE)

---

## 4. Backend Technical Safeguards

### 4.1 Anti-Replay & Order Overlap Protection

**FR-4.1.1 Order Processing Lock**
- Implement per-symbol processing mutex
- Lock must be acquired before:
  - Processing fill event
  - Placing replacement order
  - Updating position tracking
- Lock timeout: 5 seconds (fail-safe unlock)

**FR-4.1.2 Fill Event Deduplication**
- Track last 100 fill event IDs in Redis/memory
- Reject duplicate fill events within 30-second window
- Log warning on duplicate detection

**FR-4.1.3 State Transition Validation**
```
Valid transitions:
  PENDING → FILLED ✓
  PENDING → CANCELLED ✓
  FILLED → PENDING ✗ (INVALID - replay attack or bug)
  
On invalid transition: Log error, skip processing, alert admin
```

### 4.2 Spread Protection (Chống Trượt Giá)

**FR-4.2.1 Orderbook Spread Monitoring**
- Fetch best bid/ask every 3 seconds during trading
- Calculate: SpreadPct = (Ask - Bid) / MidPrice

**FR-4.2.2 Trading Pause Thresholds**
```
If SpreadPct > 0.1%:  // Wide spread
    PauseNewOrders = true
    Log: "Wide spread detected, pausing new orders"
    
If SpreadPct > 0.3%:  // Extreme spread
    PauseAllOrders = true  
    Log: "Extreme spread, emergency pause"
    Alert: "Market liquidity issue detected"
    
If SpreadPct returns < 0.05% for 30 seconds:
    ResumeTrading = true
```

**FR-4.2.3 Slippage Calculation**
- For each filled order, calculate actual slippage:
  - Market Buy: Slippage = (FillPrice - BestAsk) / BestAsk
  - Market Sell: Slippage = (BestBid - FillPrice) / BestBid
- If average slippage > 0.05% over last 10 fills: Alert

### 4.3 Funding Rate Arbitrage Protection

**FR-4.3.1 Funding Rate Monitoring**
- Fetch funding rate every 8 hours (or API callback)
- Track 24h funding cost: FundingCost = PositionSize × FundingRate × 3

**FR-4.3.2 Funding-Based Position Adjustment**
```
If FundingRate > 0.03% (Longs pay high):
    If NetLong > 0:
        ReduceLongLevels = 2  // Remove 2 furthest buy levels
        AddShortLevels = 1    // Add 1 sell level closer
        Log: "High funding, reducing long exposure"
        
If FundingRate < -0.03% (Shorts pay high):
    If NetShort > 0:
        ReduceShortLevels = 2
        AddLongLevels = 1
```

**FR-4.3.3 Funding Cost Accumulation**
- Track cumulative funding cost vs grid profit
- If FundingCost > 50% of DailyGridProfit: Alert "Funding eating profits"

---

## 5. Trend Detection & Pause Logic

### 5.1 Problem Statement
- **Current**: Bot continues placing orders during strong trends
- **Issue**: RSI can show 70+ for hours during strong uptrend; Bot keeps shorting into strength

### 5.2 Solution: Multi-Factor Trend Detection

#### 5.2.1 RSI-Based Trend Detection

**FR-5.1.1 RSI Calculation**
- Calculate RSI(14) on 15m timeframe
- Update every 5 minutes

**FR-5.1.2 Trend State Classification**
```
RSI > 70:  // Overbought
    TrendState = "STRONG_UP"
    PauseNewShorts = true
    ReduceShortSizes = 50%
    
RSI > 60 AND RSI trending up:  // Mildly overbought
    TrendState = "UP"
    ReduceShortSizes = 30%
    
RSI < 30:  // Oversold
    TrendState = "STRONG_DOWN"
    PauseNewLongs = true
    ReduceLongSizes = 50%
    
RSI < 40 AND RSI trending down:
    TrendState = "DOWN"
    ReduceLongSizes = 30%
    
RSI 40-60:  // Neutral
    TrendState = "NEUTRAL"
    NormalTrading = true
```

#### 5.2.2 Trend Persistence Detection

**FR-5.2.1 Trend Strength Meter**
```
TrendScore = 0

If RSI > 70 for > 30 minutes: TrendScore += 2
If Price > 20-period EMA: TrendScore += 1
If Volume > 2× Average: TrendScore += 1
If Successive higher highs: TrendScore += 1

If TrendScore >= 4:
    PauseCounterTrend = true
    Log: "Strong uptrend confirmed, pausing shorts"
```

**FR-5.2.2 Trend Exhaustion Detection**
- If trend persists > 2 hours: Increase monitoring
- If RSI divergence appears (price higher, RSI lower): Prepare to resume normal trading

---

## 6. Integration Architecture

### 6.1 Component Interaction

```
┌─────────────────────────────────────────────────────────┐
│                 ADAPTIVE GRID MANAGER                    │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────┐  │
│  │  DYNAMIC     │    │  INVENTORY   │    │  TREND   │  │
│  │  SPREAD      │◄──►│   SKEW       │◄──►│  DETECT  │  │
│  │  (ATR)       │    │  (Position)  │    │  (RSI)   │  │
│  └──────┬───────┘    └──────┬───────┘    └────┬─────┘  │
│         │                   │                  │       │
│         └───────────────────┼──────────────────┘       │
│                             │                          │
│         ┌───────────────────▼──────────┐              │
│         │      ORDER DECISION ENGINE     │              │
│         │  - Can place? - What size?     │              │
│         │  - What price? - Take profit?  │              │
│         └───────────────┬──────────────┘              │
│                         │                              │
│  ┌──────────────────────┼──────────────────────────┐  │
│  │      SAFEGUARDS      │      CLUSTER MGMT          │  │
│  │  ┌──────────────┐   │   ┌──────────────────┐     │  │
│  │  │Anti-Replay   │   │   │Time-Based Stop   │     │  │
│  │  │Spread Protect│◄──┼──►│Breakeven Exit    │     │  │
│  │  │Funding Arb   │   │   │Cluster Heat Map  │     │  │
│  │  └──────────────┘   │   └──────────────────┘     │  │
│  └──────────────────────┼──────────────────────────┘  │
└─────────────────────────┼───────────────────────────────┘
                          │
                    ┌─────▼──────┐
                    │   EXCHANGE  │
                    │    API      │
                    └─────────────┘
```

### 6.2 Configuration Structure

```yaml
# config/grid_optimization.yaml

# 1. Dynamic Grid Settings
dynamic_grid:
  enabled: true
  atr_period: 14
  atr_timeframe: "15m"
  base_spread_pct: 0.5
  
  spread_multipliers:
    low_volatility: 0.6      # ATR < 0.3%
    normal: 1.0            # ATR 0.3-0.8%
    high_volatility: 1.8   # ATR 0.8-1.5%
    extreme: 2.5           # ATR > 1.5%
  
  level_adjustments:
    increase_threshold: 0.7  # Increase levels when spread < 0.7x
    decrease_threshold: 2.0  # Decrease levels when spread > 2.0x

# 2. Inventory Skew Settings  
inventory_skew:
  enabled: true
  max_inventory_pct: 0.30    # 30% of equity max
  
  actions:
    low:      { threshold: 0.3, size_reduction: 0, pause: false }
    moderate: { threshold: 0.6, size_reduction: 0.3, pause: false }
    high:     { threshold: 0.8, size_reduction: 0.5, pause_skewed_side: true }
    critical: { threshold: 1.0, close_positions: true, emergency_exit: true }

# 3. Cluster Stop-Loss Settings
cluster_stop_loss:
  enabled: true
  
  time_based:
    monitor_hours: 2
    emergency_hours: 4
    stale_hours: 8
    
  drawdown_thresholds:
    monitor: -0.5%
    emergency: -1.0%
    
  breakeven_exit:
    enabled: true
    close_50_pct_at: -0.2%  # After >2% drawdown
    close_100_pct_at: 0.0%  # At breakeven after >3% drawdown

# 4. Backend Safeguards
safeguards:
  anti_replay:
    enabled: true
    dedup_window: 30
    lock_timeout: 5
    
  spread_protection:
    enabled: true
    pause_threshold: 0.1%
    emergency_pause: 0.3%
    resume_after: 30
    
  funding_protection:
    enabled: true
    high_funding_threshold: 0.03%
    level_adjustment: 2

# 5. Trend Detection
trend_detection:
  enabled: true
  rsi_period: 14
  rsi_timeframe: "15m"
  update_interval: 5
  
  thresholds:
    strong_overbought: 70
    mild_overbought: 60
    neutral_high: 60
    neutral_low: 40
    mild_oversold: 40
    strong_oversold: 30
    
  pause_durations:
    strong_trend: 30      # Minutes
    mild_trend: 15
```

---

## 7. Acceptance Criteria

### 7.1 Dynamic Grid
- [ ] ATR calculates correctly within ±0.01% of TradingView
- [ ] Spread adjusts automatically when ATR changes >20%
- [ ] Grid levels increase/decrease based on volatility regime
- [ ] No orders placed within 1 minute of spread adjustment (settling period)

### 7.2 Inventory Skew
- [ ] Net position tracked accurately (±0.1% of actual)
- [ ] Size reduces on skewed side when threshold crossed
- [ ] Take-profit targets adjust within 30 seconds of threshold breach
- [ ] Emergency close executes within 10 seconds when critical threshold hit

### 7.3 Cluster Stop-Loss
- [ ] Position age tracked per cluster accurately
- [ ] Time-based close triggers within 60 seconds of threshold
- [ ] Breakeven exit triggers correctly (close at specified recovery level)
- [ ] Heat map logged every 15 minutes during active positions

### 7.4 Backend Safeguards
- [ ] No duplicate fill processing (verified via logs)
- [ ] Trading pauses within 3 seconds of spread >0.1%
- [ ] Funding rate adjustment applies within 1 hour of rate change
- [ ] No "stuck" locks after 10 seconds (automatic cleanup)

### 7.5 Trend Detection
- [ ] RSI matches TradingView within ±1 point
- [ ] Pause triggers within 5 minutes of RSI threshold breach
- [ ] Resume occurs after trend exhaustion detected
- [ ] Trend score calculates correctly (verified against manual calculation)

---

## 8. Success Metrics

| Metric | Baseline | Target | Measurement |
|--------|----------|--------|-------------|
| Liquidation Events | Current rate | Zero | Per 30 days |
| Avg Position Size | Equal sizing | -40% at edges | Per order |
| Grid Sweeps (1-candle) | Current | -70% | Per week |
| Stuck Positions (>8h) | Current | -90% | Per week |
| Breakeven Exits | N/A | 80% success | Of attempted |
| Daily Profit vs Funding | Current | Net positive | Daily P&L |
| Trend Losses | Current | -60% | During RSI>70/<30 |

---

## 9. Risks & Mitigations

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| False volatility spike | Medium | Missed trades | Use 15m smoothed ATR, not tick |
| RSI whipsaw | High | Frequent pausing | Require 15-min persistence |
| Inventory skew over-correction | Medium | Reduced profit | Gradual reduction, not instant |
| Funding rate lag | Low | 8h of high fees | Check rate every 4h, not just 8h |
| Spread flash crash | Low | Unnecessary pause | Require 3 samples > threshold |

---

## 10. Implementation Phases

### Phase 1: Core Safety (Week 1)
- Inventory Skew basic implementation
- Anti-replay protection
- Spread protection

### Phase 2: Dynamic Systems (Week 2)
- ATR-based spread adjustment
- Time-based cluster stop-loss
- Trend detection with RSI

### Phase 3: Advanced Features (Week 3)
- Breakeven exit logic
- Funding rate arbitrage protection
- Cluster heat map visualization

### Phase 4: Testing & Tuning (Week 4)
- Paper trading validation
- Parameter tuning based on results
- Documentation and monitoring

---

**END OF SPECIFICATION**
