# Feature Specification: Continuous Volume Farming with Micro-Profit & Adaptive Risk Control

## Overview

### Feature Description
Refactor the current Agentic + Volume Farm architecture from a "wait-for-range-then-trade" model to a **continuous farming model** that:
1. **Always trades** to maximize volume farming opportunities
2. **Creates micro-profits** through high-frequency small trades
3. **Dynamically adjusts risk parameters** based on market regime (sideways/trending/volatile)
4. **Gracefully exits** when price breaks range or strong trend detected

### Problem Statement (Current Flow Issues)
The current implementation is blocked by:
- **Range Detector Gate**: Orders are blocked when `RangeState != Active`
- **Warm-up Deadlock**: Bot waits for BB range establishment before placing any orders
- **Missed Opportunities**: No trading during range establishment phase = lost volume
- **All-or-Nothing**: Either fully in grid or fully out, no middle ground

### Business Value
- **Increased Trading Volume**: Trade continuously instead of waiting for perfect conditions
- **Better Capital Efficiency**: Capital always working, not idle during "establishing" phase
- **Smoother PnL Curve**: Micro-profits from many small trades vs. waiting for big grid profits
- **Adaptive Risk**: Reduce size/spread in volatile conditions instead of stopping entirely

---

## User Scenarios & Testing

### Scenario 1: Continuous Trading in Sideways Market
**Given** Market is moving sideways with low volatility
**When** Bot starts and detects regime as "sideways"
**Then**
- Bot enters "Standard Grid Mode" with normal size/spread
- Places grid orders immediately without waiting for BB confirmation
- Maintains full grid (e.g., 5 levels each side)
- Expected: 50-100 fills per hour, micro-profit per fill ~0.02%

### Scenario 2: Micro-Trading During Range Establishment
**Given** Range detector is still in "Establishing" state (insufficient candles)
**When** Price is within reasonable bounds and no strong trend detected
**Then**
- Bot enters "Micro-Trade Mode" with reduced parameters
- Places smaller grid (e.g., 2-3 levels each side, wider spread)
- Uses default ATR-based bands instead of BB bands
- Expected: 20-30 fills per hour, smaller size (30-50% of normal)
- Seamlessly transitions to Standard Mode when range establishes

### Scenario 3: Trending Market - Reduce and Hedge
**Given** ADX > 25 or strong directional movement detected
**When** Bot detects trending regime
**Then**
- Bot enters "Trend-Adapted Mode"
- Reduces grid size to 1-2 levels each side
- Increases spread to 2x normal (wider safety margin)
- Reduces order size to 30% of normal
- Adds trend-biased positioning (more orders on trend side)
- Expected: Fewer fills but safer, maintains some volume

### Scenario 4: Volatile/Breakout - Emergency Exit
**Given** Price breaks BB bands with volume spike
**When** Breakout confirmed (2+ closes outside band)
**Then**
- Bot immediately cancels all pending orders
- Closes any open positions (market orders)
- Enters "Cool-down Mode" for 60 seconds
- Re-enters Micro-Trade Mode when price stabilizes
- Expected: Max loss per event capped at ~0.1% of position

### Scenario 5: Whitelist Rotation
**Given** Agentic layer identifies better opportunity on another symbol
**When** Score difference > 20 points and current symbol not in active position
**Then**
- Bot gracefully exits current symbol (cancel orders, close if needed)
- Transitions to new symbol within 30 seconds
- Applies appropriate mode based on new symbol's regime
- Expected: Seamless rotation without manual intervention

---

## Functional Requirements

### FR1: Trading Mode State Machine
**Acceptance Criteria:**
- System supports 4 distinct trading modes: `MICRO`, `STANDARD`, `TREND_ADAPTED`, `COOLDOWN`
- Mode transitions happen automatically based on market conditions
- No mode blocks trading entirely - only adjusts parameters
- Mode transitions logged with reason (ADX value, BB state, trend score, etc.)
- Can override mode manually via API/config for testing

### FR2: Bypass Range Gate for Micro Mode
**Acceptance Criteria:**
- When `RangeState != Active` AND no breakout detected → enter MICRO mode
- MICRO mode uses ATR-based dynamic bands (not BB) for grid levels
- Orders placed with 60% reduced size vs STANDARD
- Grid levels reduced to 2-3 per side vs 5 per side
- When `RangeState` becomes Active → auto-transition to STANDARD

### FR3: Regime-Based Dynamic Parameters
**Acceptance Criteria:**
- ADX-based regime detection: `SIDeways` (ADX<20), `TRENDING` (ADX>25), `VOLATILE` (ATR spike)
- Each regime has predefined parameter multipliers:
  - SIDeways: Size=100%, Spread=100%, Levels=5, Biased=false
  - TRENDING: Size=30%, Spread=200%, Levels=2, Biased=true (follow trend)
  - VOLATILE: Size=20%, Spread=300%, Levels=1, Cooldown=30s
- Parameters updated every 30 seconds based on latest regime
- Sudden regime change (ADX +10 in 1 minute) triggers immediate re-evaluation

### FR4: Continuous Volume Tracking & Micro-Profit Optimization
**Acceptance Criteria:**
- Track fills per hour per symbol (target: >30 fills/hour in any mode)
- Calculate average profit per fill (target: >0.01% after fees)
- Adjust spread dynamically to achieve fill rate target:
  - If fills < 20/hour → reduce spread by 10% (max 0.05%)
  - If fills > 100/hour → increase spread by 10% (prevent over-trading)
- Daily volume target: configurable (default 10x position size)

### FR5: Adaptive Exit on Breakout/Trend
**Acceptance Criteria:**
- Breakout detection: Price outside BB band for 2 consecutive 1m candles
- Trend detection: ADX > 25 for 3 consecutive readings
- On exit trigger:
  1. Cancel all pending orders (async, <1s)
  2. Close open positions at market (if size > 0)
  3. Enter COOLDOWN mode for 60s (no new orders)
  4. After cooldown, re-enter based on new conditions
- Max time from trigger to full exit: 5 seconds

### FR6: Agentic Layer Integration - Smart Whitelist
**Acceptance Criteria:**
- Agentic engine scores symbols every 30s (regime + opportunity)
- Auto-rotate to highest-scoring symbol when:
  - Current symbol score < 60 AND new symbol score > 80
  - No active position on current symbol
  - Rotation cooldown (60s) has passed since last rotation
- Symbol whitelist limited to top 3 by score
- Graceful rotation: exit current before entering new (no overlap)

### FR7: Risk Monitoring - Position & Exposure
**Acceptance Criteria:**
- Max position per symbol: configurable (default $1000 USDT)
- Max total exposure: configurable (default $3000 USDT across all symbols)
- If position limit reached → only place reducing-side orders (hedge mode)
- If exposure limit reached → stop new symbol entry, maintain existing
- Emergency stop: Daily loss > 5% → pause all trading for 1 hour

---

## Success Criteria

1. **Fill Rate**: Bot achieves >30 fills per hour per symbol in any market condition
2. **Uptime**: Bot places orders within 60 seconds of startup (no waiting for BB establishment)
3. **Regime Adaptation**: ADX-based parameter changes applied within 30 seconds of detection
4. **Breakout Response**: Full exit (orders + positions) completed within 5 seconds of breakout
5. **Micro-Profit**: Average profit per fill >0.01% (after exchange fees)
6. **Volume Target**: Daily trading volume >10x average position size
7. **Max Drawdown**: Single-event loss capped at 0.2% of capital (due to quick exit)

---

## Key Entities

- **TradingMode**: Enum {MICRO, STANDARD, TREND_ADAPTED, COOLDOWN}
- **RegimeType**: Enum {SIDEWAYS, TRENDING_UP, TRENDING_DOWN, VOLATILE, UNKNOWN}
- **DynamicParameters**: {SizeMultiplier, SpreadMultiplier, LevelCount, TrendBias, CooldownDuration}
- **SymbolScore**: {Symbol, Score, Regime, TrendStrength, Volatility, Recommendation}
- **FillMetrics**: {FillsPerHour, AvgProfitPerFill, TotalVolume24h, CurrentPositionPnL}

---

## Assumptions & Dependencies

- **Assumption**: WebSocket kline data available with <1s latency for real-time regime detection
- **Assumption**: Exchange API supports order placement/cancellation within 500ms
- **Dependency**: Agentic layer provides regime scoring every 30s
- **Dependency**: AdaptiveGridManager handles order execution and position tracking

---

## Out of Scope

- Cross-exchange arbitrage
- Machine learning for price prediction
- Social trading / copy trading features
- Mobile app UI (focus on backend trading logic)

---

## Clarifications

### Session 2026-04-15

#### Q1: Entry Logic - When to Enter vs Wait?
**→ A:** 

**MUST ENTER (Micro Mode):**
- ADX < 20 (sideways) + Price within ATR bands → Standard size
- ADX 20-25 (neutral) + No breakout detected → 60% size
- Range establishing (BB not ready) + Price stable → 40% size (ATR bands)

**MUST WAIT (No Entry):**
- ADX > 30 (strong trend) → Wait, don't trade against trend
- Breakout confirmed (2 consecutive closes outside BB) → Enter Cooldown
- Volatility spike (ATR > 3x average) → Wait 60s
- Price gap > 1% from last close → Wait for stabilization

**Entry Decision Flow:**
```
Check ADX > 30? → YES → WAIT
Check breakout? → YES → COOLDOWN
Check volatility spike? → YES → WAIT 60s
Check BB range active? → YES → STANDARD MODE
                          → NO → MICRO MODE (ATR bands)
```

#### Q2: Exit Logic - Close All When Break Range?
**→ A:**

**IMMEDIATE EXIT Triggers:**
1. **Breakout detected**: Price outside BB band for 2 consecutive 1m candles
2. **Strong trend**: ADX > 30 with directional movement > 0.5%
3. **Volatility spike**: ATR increases > 200% in 2 minutes
4. **Manual override**: API command or config change

**Exit Sequence (MUST complete within 5 seconds):**
```
T+0ms:   Detect exit trigger
T+100ms: Cancel ALL pending orders (async batch)
T+500ms: Get open positions via WebSocket cache
T+800ms: Place market orders to close positions (if any)
T+3s:    Verify all positions closed via WebSocket position stream
T+5s:    Enter COOLDOWN mode (no new orders for 60s)
```

**Position Sync Check:**
- If WebSocket shows position still open after 5s → Retry close with larger slippage
- If still open after 10s → Alert + manual intervention required
- Log all exit actions with timestamps for audit

#### Q3: State Sync Workers - Verify Internal vs Exchange?
**→ A:**

**Worker Architecture (3 workers run every 5 seconds):**

**1. Order Sync Worker:**
```go
func (w *OrderSyncWorker) Run() {
    // Get from WebSocket cache (not REST API)
    wsOrders := w.wsClient.GetCachedOrders(symbol)
    
    // Compare with internal state
    for symbol, internalOrders := range w.internalState.Orders {
        exchangeOrders := wsOrders[symbol]
        
        // Check: Missing orders (we think exists but exchange doesn't)
        for id, order := range internalOrders {
            if !existsIn(exchangeOrders, id) {
                // Order filled or cancelled externally
                w.handleMissingOrder(symbol, order)
            }
        }
        
        // Check: Unknown orders (exchange has but we don't)
        for id, order := range exchangeOrders {
            if !existsIn(internalOrders, id) {
                // External order detected
                w.handleUnknownOrder(symbol, order)
            }
        }
        
        // Check: Status mismatch
        for id, intOrder := range internalOrders {
            if extOrder, exists := exchangeOrders[id]; exists {
                if intOrder.Status != extOrder.Status {
                    w.handleStatusMismatch(symbol, intOrder, extOrder)
                }
            }
        }
    }
}
```

**2. Position Sync Worker:**
```go
func (w *PositionSyncWorker) Run() {
    // Get positions from WebSocket
    wsPositions := w.wsClient.GetCachedPositions()
    
    for symbol, internalPos := range w.internalState.Positions {
        exchangePos := wsPositions[symbol]
        
        // Size mismatch
        if abs(internalPos.Size - exchangePos.Size) > 0.001 {
            w.syncPositionSize(symbol, exchangePos.Size)
        }
        
        // Side mismatch (should never happen but critical)
        if internalPos.Side != exchangePos.Side {
            w.alertCriticalMismatch(symbol, "SIDE_MISMATCH")
            w.syncFromExchange(symbol) // Trust exchange
        }
        
        // PnL sync (for reporting)
        internalPos.UnrealizedPnL = exchangePos.UnrealizedPnL
    }
}
```

**3. Balance Sync Worker:**
```go
func (w *BalanceSyncWorker) Run() {
    wsBalance := w.wsClient.GetCachedBalance()
    
    // Check available margin
    if wsBalance.Available < w.internalState.MarginRequired {
        w.alertLowMargin(wsBalance.Available, w.internalState.MarginRequired)
    }
    
    // Sync for risk calculations
    w.internalState.UpdateBalance(wsBalance)
}
```

**Sync Conflict Resolution:**
- **Default rule**: Exchange state is ground truth
- **When internal shows order filled but exchange shows open**: Reconcile as filled (missed fill event)
- **When internal shows no position but exchange has position**: Sync from exchange immediately
- **Mismatch duration > 10s**: Log error + alert

#### Q4: WebSocket-Only Data Flow?
**→ A:**

**FORBIDDEN (REST API calls during trading):**
- ❌ `GET /openOrders` for order state
- ❌ `GET /position` for position state
- ❌ `GET /klines` for price data during trading
- ❌ `GET /account` for balance during trading

**REQUIRED (WebSocket streams only):**
- ✅ `ws/order` stream → Order updates (placement, fill, cancel)
- ✅ `ws/position` stream → Position updates (size, PnL)
- ✅ `ws/kline` stream → OHLC data (1m, 5m intervals)
- ✅ `ws/ticker` stream → Last price, bid/ask
- ✅ `ws/account` stream → Balance updates

**WebSocket Data Flow Architecture:**

```
┌─────────────────────────────────────────────────────────────┐
│                     WebSocket Hub                           │
├─────────────┬─────────────┬───────────────┬─────────────────┤
│   Orders    │  Positions  │    Klines     │    Balance      │
│   Stream    │   Stream    │   Stream      │    Stream       │
└──────┬──────┴──────┬──────┴───────┬───────┴────────┬────────┘
       │             │              │                │
       ▼             ▼              ▼                ▼
┌─────────────┐ ┌──────────┐ ┌──────────────┐ ┌────────────┐
│ Order Cache │ │ Pos Cache│ │  Kline Cache │ │ Bal Cache  │
│  (1s TTL)   │ │ (1s TTL) │ │  (5s TTL)    │ │ (10s TTL)  │
└──────┬──────┘ └────┬─────┘ └──────┬───────┘ └─────┬──────┘
       │             │              │                │
       └─────────────┴──────────────┴────────────────┘
                         │
                         ▼
              ┌────────────────────┐
              │   State Sync Workers│
              │  (Every 5 seconds) │
              └────────────────────┘
```

**WebSocket Message Processing:**
```go
// Order update handler
func (h *OrderHandler) OnOrderUpdate(update OrderUpdate) {
    // Immediate cache update
    h.orderCache.Set(update.OrderID, update)
    
    // Check fill
    if update.Status == "FILLED" {
        h.handleFill(update)
    }
    
    // Update internal state
    h.internalState.UpdateOrder(update)
}

// Kline handler for regime detection
func (h *KlineHandler) OnKline(kline Kline) {
    // Update price cache
    h.priceCache.Add(kline.Symbol, kline.Close)
    
    // Feed to range detector
    h.rangeDetector.AddPrice(kline.High, kline.Low, kline.Close)
    
    // Feed to trend detector
    h.trendDetector.OnPrice(kline.Close)
}
```

**Fallback Strategy (WebSocket disconnect):**
1. Attempt reconnect with exponential backoff (1s, 2s, 4s, 8s, max 30s)
2. During reconnect: Pause NEW order placement, maintain existing
3. After 30s disconnect: Enter COOLDOWN mode (no new orders)
4. After 60s: Trigger exit sequence for all positions
5. Use REST API ONLY for emergency position check during extended disconnect

**WebSocket Health Monitoring:**
- Ping/pong every 10s
- Latency check: If msg delay > 3s → mark as stale
- Auto-reconnect on any error
- Buffer messages during reconnect (max 100 msgs)
