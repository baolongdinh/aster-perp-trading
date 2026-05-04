# Volume Farm Maker - Nghiệp Vụ Chi Tiết (Dành Cho Dev)

## 1. Tổng Quan Kiến Trúc

```mermaid
flowchart TB
    subgraph MAIN["main.go"]
        START["Start Bot"]
    end

    subgraph STRATEGY["MakerStrategyImpl"]
        OM["OrderManager<br/>PlaceOrder<br/>CancelOrder<br/>GetOpenOrders"]
        IM["InventoryManager<br/>GetPosition<br/>UpdatePosition"]
        SC["SpreadCalculator<br/>GetSpread"]
        RG["Risk Guards<br/>LiqGuard<br/>MaxPosGuard<br/>DailyLoss<br/>OTRatio"]
        RM["RiskManager<br/>10-Level Risk Check"]
        
        subgraph LOOPS["Background Loops"]
            OML["orderManagementLoop<br/>5s"]
            RML["riskMonitoringLoop<br/>5s"]
            PSL["positionSyncLoop<br/>10s"]
            TOL["positionTimeoutLoop<br/>5s"]
            TSL["trailingStopLoop<br/>1s"]
            DRL["dailyResetLoop<br/>1min"]
        end
    end

    subgraph API["External APIs"]
        FC["FuturesClient<br/>REST API<br/>PlaceOrder<br/>CancelOrder<br/>GetPositions<br/>GetAccountInfo"]
        WS["WebSocketClient<br/>Real-time Data<br/>GetTickerData<br/>SubscribeTicker"]
    end

    START --> STRATEGY
    OM --> FC
    IM --> FC
    RG --> FC
    RM --> FC
    SC --> STRATEGY
    WS --> STRATEGY
    WS --> LOOPS
```

---

## 2. Các Vòng Lặp Chính (Main Loops)

| Loop | Interval | Chức năng | FR |
|------|----------|-----------|-----|
| orderManagementLoop | 5s | Xử lý vòng đời order: kiểm tra fill, cancel, regrid | - |
| riskMonitoringLoop | 5s | Kiểm tra điều kiện risk: liquidation, max position, daily loss | FR13 |
| positionSyncLoop | 10s | Đồng bộ positions từ exchange về local cache | - |
| positionTimeoutLoop | 5s | **MỚI** - Check timeout 60s, force close | FR4 |
| trailingStopLoop | 1s | **MỚI** - Monitor trailing stop, activate at 0.1% | FR5 |
| dailyResetLoop | 1min | **MỚI** - Check 23:00 UTC, close all positions | FR8 |

---

## 3. Luồng Khởi Động (Start)

```mermaid
sequenceDiagram
    participant Main as main.go
    participant Strategy as MakerStrategyImpl
    participant WS as WebSocketClient
    participant FC as FuturesClient
    participant Loops as Background Loops

    Main->>Strategy: NewMakerStrategy()
    Strategy->>Strategy: Initialize all managers
    Strategy->>Strategy: Init RiskManager (10 levels)
    Strategy->>Strategy: Init maps (positionOpenTime, trailingStates, emaCache)

    Main->>Strategy: Start(ctx)
    Strategy->>FC: reconcileOnStartup() [FR10]
    FC-->>Strategy: Return positions & open orders
    Strategy->>Strategy: Log reconciliation
    
    Strategy->>WS: SubscribeToTicker(symbols)
    WS-->>Strategy: Subscribe success
    
    Strategy->>Loops: Start all background loops
    Loops-->>Strategy: Loops running
    
    Strategy-->>Main: Started successfully
```

---

## 4. Luồng Đặt Lệnh Grid (PlaceOrders) - FR1, FR2, FR7, FR9

```mermaid
flowchart TB
    START["PlaceOrders(symbol)"] --> RUN{"s.running?"}
    RUN -->|No| RETURN1["return nil"]
    RUN -->|Yes| EMERGENCY{"emergencyStop.Check()"}
    EMERGENCY -->|Yes| RETURN2["return nil, log warning"]
    EMERGENCY -->|No| TICKER["GetTickerData(symbol)"]
    
    TICKER --> PRICE{"bestBid/bestAsk > 0?"}
    PRICE -->|No| RETURN3["return nil"]
    PRICE -->|Yes| MID["midPrice = (bid+ask)/2"]
    
    %% FR9: Spread Protection
    MID --> SPREAD["checkSpreadProtection(bid, ask)"]:::new
    SPREAD --> BLOCKED{"shouldBlock?"}
    BLOCKED -->|Yes| RETURN4["return nil"]
    BLOCKED -->|No| ZONE["calculateActiveZoneGrid()"]:::new
    
    %% FR1: Active Zone Grid
    ZONE --> BALANCE["getAvailableBalance()"]
    BALANCE --> BAL_CHECK{"balance > 0?"}
    BAL_CHECK -->|No| RETURN5["return nil"]
    BAL_CHECK -->|Yes| POSITION["GetPosition(symbol)"]
    
    POSITION --> EMA["updateEMACache(symbol, midPrice)"]:::new
    EMA --> ZONE_MULT["calculateZoneMultiplier()"]:::new
    ZONE_MULT --> GRID_VARS["gridLevels = activeZone.LevelCount<br/>gridSpacing = activeZone.GridSpacing"]:::new
    
    %% FR2/FR15: Order Sizing
    GRID_VARS --> ORDER_SIZE["calculateOrderSize(balance)"]:::new
    ORDER_SIZE --> ORDER_COUNT["calculateOrderCount(balance)"]:::new
    ORDER_COUNT --> MIN_NOTIONAL["minNotional = $5"]
    
    MIN_NOTIONAL --> ADJUST["Adjust gridLevels if needed"]
    ADJUST --> BUY_QTY["Calculate perLevelBuyQty"]
    ADJUST --> SELL_QTY["Calculate perLevelSellQty"]
    
    BUY_QTY --> CONCURRENT["Concurrent Order Placement"]
    SELL_QTY --> CONCURRENT
    
    CONCURRENT --> BUY_LOOP["for i=0 to gridLevels"]
    BUY_LOOP --> BUY_PRICE["bestBid * (1 - gridSpacing*i)"]
    BUY_PRICE --> PLACE_BUY["placeLimitOrder(BUY)"]
    
    CONCURRENT --> SELL_LOOP["for i=0 to gridLevels"]
    SELL_LOOP --> SELL_PRICE["bestAsk * (1 + gridSpacing*i)"]
    SELL_PRICE --> PLACE_SELL["placeLimitOrder(SELL)"]
    
    PLACE_BUY --> UPDATE_TIME["updatePositionOpenTime()"]:::new
    PLACE_SELL --> UPDATE_TIME
    
    UPDATE_TIME --> COMPLETE["return nil"]
    
    classDef new fill:#90EE90,stroke:#333,stroke-width:2px
```

### Chi Tiết FR1: Active Zone Grid

```mermaid
flowchart LR
    A["midPrice = 1850"] --> B["Active Zone Range = 0.1%"]
    B --> C["Min = 1850 * 0.999 = 1848.15"]
    B --> D["Max = 1850 * 1.001 = 1851.85"]
    
    C --> E["8 Levels"]
    D --> E
    
    E --> E1["Level 1: 1848.15"]
    E --> E2["Level 2: 1848.64"]
    E --> E3["Level 3: 1850.00 (bestBid)"]
    E --> E4["..."]
    E --> E8["Level 8: 1851.85"]
    
    note1["Chỉ đặt lệnh trong vùng này"]
    note1 -.-> E
```

### Chi Tiết FR7: Zone-Based Sizing (EMA)

```mermaid
flowchart TB
    START["calculateZoneMultiplier(symbol, price)"] --> GET_EMA["Get EMA from cache"]
    GET_EMA --> DIFF["diff = (price - EMA) / EMA * 100"]
    
    DIFF --> ZONE{"diff > 0?"}
    ZONE -->|Yes| ABOVE["ZoneAboveEMA"]
    ZONE -->|No| CHECK_NEG{"diff > -1%"}
    
    CHECK_NEG -->|Yes| NORMAL["ZoneNormalDip<br/>multiplier = 1.0"]
    CHECK_NEG -->|No| CHECK_STRONG{"diff > -2%"}
    
    CHECK_STRONG -->|Yes| STRONG["ZoneStrongDip<br/>multiplier = 1.5"]
    CHECK_STRONG -->|No| HARD["ZoneHardDip<br/>multiplier = 0"]
    
    ABOVE --> RETURN["Return (multiplier, zoneType)"]
    NORMAL --> RETURN
    STRONG --> RETURN
    HARD --> RETURN
    
    style ABOVE fill:#FFE4B5
    style NORMAL fill:#90EE90
    style STRONG fill:#87CEEB
    style HARD fill:#FFB6C1
```

---

## 5. Background Loops Chi Tiết

### 5.1 Position Timeout Loop (FR4)

```mermaid
flowchart TB
    START["positionTimeoutLoop"] --> TICKER["ticker: 5s"]
    
    TICKER --> SYMBOLS["for each symbol"]
    SYMBOLS --> CHECK["checkPositionTimeout(symbol)"]
    
    CHECK --> GET_TIME["Get positionOpenTime[symbol]"]
    GET_TIME --> EXISTS{"exists?"}
    EXISTS -->|No| SKIP["continue"]
    EXISTS -->|Yes| ELAPSED["elapsed = now - openTime"]
    
    ELAPSED --> TIMEOUT{"elapsed > 60s?"}
    TIMEOUT -->|No| SKIP
    TIMEOUT -->|Yes| FORCE_CLOSE["Log: Force closing due to timeout"]
    FORCE_CLOSE --> DELETE["delete(positionOpenTime, symbol)"]
    
    DELETE --> NEXT["next symbol"]
    SKIP --> NEXT
```

### 5.2 Trailing Stop Loop (FR5)

```mermaid
flowchart TB
    START["trailingStopLoop"] --> TICKER["ticker: 1s"]
    
    TICKER --> POSITIONS["GetAllPositions()"]
    POSITIONS --> LOOP["for each symbol, position"]
    
    LOOP --> AMT_CHECK{"position.Amount != 0?"}
    AMT_CHECK -->|No| NEXT1["continue"]
    AMT_CHECK -->|Yes| SIDE["side = long/short"]
    SIDE --> PRICE["GetTickerData(symbol)"]
    
    PRICE --> CURRENT["currentPrice = (bid+ask)/2"]
    CURRENT --> TRAIL["manageTrailingStop(symbol, currentPrice, entryPrice, side)"]
    
    TRAIL --> CHECK_PROFIT["Calculate profit %"]
    CHECK_PROFIT --> FIRST_CHECK{"first time > 0.1%?"}
    
    FIRST_CHECK -->|Yes| ACTIVATE["Activate trailing<br/>Set peakPrice = currentPrice"]
    FIRST_CHECK -->|No| CHECK_TRAIL{"profit < peakPrice - 0.03%?"}
    
    ACTIVATE --> NEXT1
    CHECK_TRAIL -->|Yes| CLOSE["Force close position<br/>Log: trailing stop triggered"]
    CHECK_TRAIL -->|No| UPDATE["Update peakPrice if higher"]
    
    CLOSE --> NEXT1
    UPDATE --> NEXT1
```

### 5.3 Daily Reset Loop (FR8)

```mermaid
flowchart TB
    START["dailyResetLoop"] --> TICKER["ticker: 1min"]
    
    TICKER --> CHECK["shouldDailyReset()"]
    CHECK --> HOUR["currentHour = now.UTC().Hour()"]
    HOUR --> RESET_HR{"hour == DailyResetHour (23)?"}
    
    RESET_HR -->|No| SKIP["continue"]
    RESET_HR -->|Yes| LOG["Log: Daily reset triggered"]
    
    LOG --> CLOSE_ALL["Close ALL positions via futuresClient"]
    CLOSE_ALL --> RESET_STATS["Reset daily stats:<br/>TotalVolume = 0<br/>TotalProfit = 0<br/>LastResetDate = now"]
    
    RESET_STATS --> DONE
```

---

## 6. Risk Manager (FR13) - 10 Levels

```mermaid
flowchart TB
    START["RiskManager.CheckAllLevels(ctx)"] --> RESET["Reset all levels to inactive"]
    
    RESET --> L1["Level 1: IM Rate >= 90%"]
    L1 --> L1_CHECK{"IMRate >= 0.9?"}
    L1_CHECK -->|Yes| L1_ACTION["Action: BlockWorkflow"]
    L1_CHECK -->|No| L2
    
    L2["Level 2: IM >= 80% && profit > 0"] --> L2_CHECK{"IMRate >= 0.8 && profit > 0?"}
    L2_CHECK -->|Yes| L2_ACTION["Action: ClosePosition"]
    L2_CHECK -->|No| L3
    
    L3["Level 3: Profit >= 0.1%"] --> L3_CHECK{"profit >= 0.001?"}
    L3_CHECK -->|Yes| L3_ACTION["Action: ActivateTrailing"]
    L3_CHECK -->|No| L4
    
    L4["Level 4: Position > 40% max"] --> L4_CHECK{"positionPct > 0.4?"}
    L4_CHECK -->|Yes| L4_ACTION["Action: BlockNewOrders"]
    L4_CHECK -->|No| L5
    
    L5["Level 5: Position > 60s"] --> L5_CHECK{"positionAge > 60s?"}
    L5_CHECK -->|Yes| L5_ACTION["Action: ForceClose"]
    L5_CHECK -->|No| L6
    
    L6["Level 6: Both long && short profit > 0"] --> L6_CHECK{"longProfit > 0 && shortProfit > 0?"}
    L6_CHECK -->|Yes| L6_ACTION["Action: CloseBoth"]
    L6_CHECK -->|No| L7
    
    L7["Level 7: Insufficient margin for TP"] --> L7_CHECK{"margin < TP_requirement?"}
    L7_CHECK -->|Yes| L7_ACTION["Action: PauseTP"]
    L7_CHECK -->|No| L8
    
    L8["Level 8: Margin < required for protective"] --> L8_CHECK{"margin < protective_margin?"}
    L8_CHECK -->|Yes| L8_ACTION["Action: BlockOrders"]
    L8_CHECK -->|No| L9
    
    L9["Level 9: > N regrid in 48h"] --> L9_CHECK{"regridCount > limit?"}
    L9_CHECK -->|Yes| L9_ACTION["Action: BlockRegrid"]
    L9_CHECK -->|No| L10
    
    L10["Level 10: Multiple triggers"] --> L10_CHECK{"activeLevels >= 3?"}
    L10_CHECK -->|Yes| L10_ACTION["Action: FullStop"]
    L10_CHECK -->|No| RETURN["Return all actions"]
    
    L1_ACTION --> L2
    L2_ACTION --> L3
    L3_ACTION --> L4
    L4_ACTION --> L5
    L5_ACTION --> L6
    L6_ACTION --> L7
    L7_ACTION --> L8
    L8_ACTION --> L9
    L9_ACTION --> L10
    
    style L1_ACTION fill:#FFB6C1
    style L2_ACTION fill:#FFB6C1
    style L5_ACTION fill:#FFB6C1
    style L10_ACTION fill:#FF0000,color:#fff
```

---

## 7. Luồng Quản Lý Vòng Đời Order (processOrderLifecycle)

```mermaid
flowchart TB
    START["processOrderLifecycle(symbol)"] --> GET_ORDERS["GetOpenOrders(symbol)"]
    
    GET_ORDERS --> SYNC["Sync with Exchange<br/>GetOpenOrders API"]
    SYNC --> CLEAN["Remove stale local orders"]
    
    CLEAN --> CHECK_FILLED["Check Filled Orders"]
    CHECK_FILLED --> LOOP1["for each order"]
    LOOP1 --> STATUS{"Status == FILLED?"}
    
    STATUS -->|Yes| RECORD["RecordFill<br/>Update inventory<br/>Log profit"]
    STATUS -->|No| CANCEL_CHECK{"Status == CANCELED?"}
    
    CANCEL_CHECK -->|Yes| REMOVE["Remove from cache"]
    CANCEL_CHECK -->|No| CONTINUE["Continue"]
    
    RECORD --> CANCEL_LOGIC
    REMOVE --> CANCEL_LOGIC
    
    CANCEL_LOGIC --> GRID_CHECK["Check Grid Drift"]
    GRID_CHECK --> DRIFT{"Drift > 0.2%?"}
    DRIFT -->|Yes| CANCEL_ALL["Cancel ALL orders<br/>needsGridShift = true"]
    DRIFT -->|No| INDIVIDUAL
    
    INDIVIDUAL --> AGE_CHECK["Check Order Age"]
    AGE_CHECK --> AGE{"Age > 120s?"}
    AGE -->|Yes| CANCEL_AGE["Cancel: too old"]
    AGE -->|No| PRICE_DRIFT{"Price Drift > 0.1%?"}
    
    PRICE_DRIFT -->|Yes| CANCEL_DRIFT["Cancel: price moved"]
    PRICE_DRIFT -->|No| KEEP["Keep order"]
    
    CANCEL_ALL --> PLACE_DECISION
    CANCEL_AGE --> PLACE_DECISION
    CANCEL_DRIFT --> PLACE_DECISION
    KEEP --> PLACE_DECISION
    
    PLACE_DECISION --> DECIDE{"Need to place?"}
    DECIDE -->|"filled > 0"| REPLACE["fill replacement"]
    DECIDE -->|"needsGridShift"| SHIFT["grid shift"]
    DECIDE -->|"!activeBuy"| MISSING_BUY["missing buy side"]
    DECIDE -->|"!activeSell"| MISSING_SELL["missing sell side"]
    DECIDE -->|"none"| ALIVE["Keep orders alive"]
    
    REPLACE --> CALL_PLACE["Call PlaceOrders()"]
    SHIFT --> CALL_PLACE
    MISSING_BUY --> CALL_PLACE
    MISSING_SELL --> CALL_PLACE
    ALIVE --> RETURN["return"]
    
    CALL_PLACE --> RETURN
```

---

## 8. Spread Protection (FR9)

```mermaid
flowchart TB
    START["checkSpreadProtection(bestBid, bestAsk)"] --> VALID{"bid > 0 && ask > 0?"}
    VALID -->|No| NO_PRICE["return (true, 'no_price')"]
    VALID -->|Yes| SPREAD["spread = ask - bid"]
    
    SPREAD --> SPREAD_PCT["spreadPct = spread / ((bid+ask)/2)"]
    SPREAD_PCT --> THRESHOLD{"spreadPct > SpreadThreshold?"}
    
    THRESHOLD -->|Yes| BLOCK["return (true, 'spread_too_wide')"]
    THRESHOLD -->|No| OK["return (false, '')"]
    
    note["SpreadThreshold = 0.1% = 0.001"]
    note -.-> THRESHOLD
```

---

## 9. Tóm Tắt Mapping FR → Code

| FR | Tên | Function | Vị Trí Gọi |
|----|-----|----------|------------|
| FR1 | Active Zone Grid | `calculateActiveZoneGrid()` | PlaceOrders() |
| FR2 | Smaller Order Size | `calculateOrderSize()` | PlaceOrders() |
| FR3 | Post-Only | `TimeInForce = "GTX"` | placeLimitOrder() |
| FR4 | Position Timeout | `checkPositionTimeout()` | positionTimeoutLoop() |
| FR5 | Trailing Stop | `manageTrailingStop()` | trailingStopLoop() |
| FR6 | Max Position + FIFO | `checkPositionLimits()` | PlaceOrders() |
| FR7 | Zone-Based Sizing | `calculateZoneMultiplier()` | PlaceOrders() |
| FR8 | Daily Reset | `shouldDailyReset()` | dailyResetLoop() |
| FR9 | Spread Protection | `checkSpreadProtection()` | PlaceOrders() |
| FR10 | Startup Reconciliation | `reconcileOnStartup()` | Start() |
| FR13 | 10-Level Risk Manager | `RiskManager.CheckAllLevels()` | riskMonitoringLoop() |

---

## 10. Các Tham Số Cấu Hình

```go
type Config struct {
    // === Active Zone Grid (FR1) ===
    ActiveZoneRange float64  // 0.001 (0.1%)
    GridSpacingMin  float64  // 0.0005 (0.05%)
    GridLevels      int      // 8

    // === Position Timeout (FR4) ===
    PositionTimeoutSeconds int // 60

    // === Trailing Stop (FR5) ===
    TrailingActivationPct float64 // 0.001 (0.1%)
    TrailingTrailPct      float64 // 0.0003 (0.03%)

    // === Max Positions (FR6) ===
    MaxPositionsPerSide int // 5

    // === Zone-Based Sizing (FR7) ===
    EMAPeriod int // 20

    // === Daily Reset (FR8) ===
    DailyResetHour int // 23 (UTC)

    // === Spread Protection (FR9) ===
    SpreadThreshold float64 // 0.001 (0.1%)

    // === Order Sizing (FR2/FR15) ===
    MarginEquityRatio float64 // 0.75
    MinOrderSizeUSD   float64 // 2
    MaxOrderSizeUSD   float64 // 10

    // === Legacy ===
    MaxLeverage          int
    MaxPositionUSDT      float64
    MaxTotalExposureUSDT float64
    DailyLossLimitPct    float64
}
```

---

## 11. Data Structures Mới

```go
// FR1: Active Zone Grid
type GridActiveZone struct {
    MinPrice    float64
    MaxPrice    float64
    GridSpacing float64
    LevelCount  int
    Levels      []float64
}

// FR5: Trailing Stop State
type TrailingState struct {
    IsActive     bool
    PeakPrice    float64
    ActivationPct float64
}

// FR7: Zone Types
type ZoneType string
const (
    ZoneAboveEMA   ZoneType = "above_ema"
    ZoneNormalDip  ZoneType = "normal_dip"
    ZoneStrongDip  ZoneType = "strong_dip"
    ZoneHardDip    ZoneType = "hard_dip"
)

// FR8: Daily Reset State
type DailyResetState struct {
    ResetHour      int
    TotalVolume    float64
    TotalProfit    float64
    LastResetDate  time.Time
}
```

---

## 12. Ví Dụ Grid Placement

```
Market: ETHUSD1 = 1850.00 (bid) / 1850.10 (ask)
Active Zone: 1848.15 - 1851.85 (0.1% range)
Grid Spacing: 0.05%
Levels: 8

BUY GRID (below bid):
  Level 1: 1850.00 * (1 - 0.0005) = 1849.07
  Level 2: 1850.00 * (1 - 0.0010) = 1848.15  ← Min of zone
  Level 3: 1850.00 * (1 - 0.0015) = 1847.22
  ...

SELL GRID (above ask):
  Level 1: 1850.10 * (1 + 0.0005) = 1851.03
  Level 2: 1850.10 * (1 + 0.0010) = 1851.96
  Level 3: 1850.10 * (1 + 0.0015) = 1852.89  ← Max of zone
  ...
```

---

## 13. Risk Guards Có Sẵn

| Guard | Chức năng | Trigger |
|-------|-----------|---------|
| LiquidationGuard | Ngăn liquidation | positionSize * priceDiff / entryPrice > buffer |
| MaxPositionGuard | Giới hạn exposure | totalExposure > MaxTotalExposureUSDT |
| DailyLossGuard | Giới hạn daily loss | dailyLoss > DailyLossLimitPct * balance |
| OrderToTradeGuard | Spam detection | orders > OrderToTradeLimit * fills |
| EmergencyStop | Emergency stop | Manual trigger |

---

## 14. Flow Tổng Quan

```mermaid
flowchart TB
    START["START BOT"] --> INIT["NewMakerStrategy()"]
    
    INIT --> RECONCILE["reconcileOnStartup() [FR10]"]
    RECONCILE --> SUBSCRIBE["Subscribe to ticker"]
    
    SUBSCRIBE --> LOOPS
    
    subgraph LOOPS["6 Background Loops"]
        OML["orderManagementLoop<br/>5s"]
        RML["riskMonitoringLoop<br/>5s"]
        PSL["positionSyncLoop<br/>10s"]
        TOL["positionTimeoutLoop<br/>5s"]
        TSL["trailingStopLoop<br/>1s"]
        DRL["dailyResetLoop<br/>1min"]
    end
    
    OML --> OML_TASK["Check fills<br/>Cancel stale<br/>Regrid"]
    OML_TASK --> PLACE["PlaceOrders()"]
    PLACE --> SPREAD["checkSpreadProtection [FR9]"]
    SPREAD --> ZONE["calculateActiveZoneGrid [FR1]"]
    ZONE --> EMA["updateEMACache [FR7]"]
    EMA --> ZONE_MULT["calculateZoneMultiplier [FR7]"]
    ZONE_MULT --> SIZE["calculateOrderSize [FR2]"]
    SIZE --> COUNT["calculateOrderCount [FR15]"]
    COUNT --> GRID["Place grid orders"]
    GRID --> UPDATE_TIME["updatePositionOpenTime [FR4]"]
    
    RML_TASK["Check risk guards<br/>Check RiskManager [FR13]"]
    RML --> RML_TASK
    
    TOL_TASK["checkPositionTimeout [FR4]"]
    TOL --> TOL_TASK
    
    TSL_TASK["manageTrailingStop [FR5]"]
    TSL --> TSL_TASK
    
    DRL_TASK["shouldDailyReset [FR8]"]
    DRL --> DRL_TASK
    
    PSL_TASK["Sync positions"]
    PSL --> PSL_TASK
```

---

## 15. Mục Tiêu & Alignment

| Mục Tiêu | Implementation | Status |
|----------|---------------|--------|
| **Max Volume** | 8 levels trong 0.1% active zone, nhiều lệnh nhỏ | ✅ |
| **Micro Profit** | Post-only (GTX), grid spacing 0.05% | ✅ |
| **150x Leverage** | MaxLeverage = 150, MarginEquityRatio = 75% | ✅ |
| **Không Liquidation** | 60s timeout, trailing stop, daily reset | ✅ |
| **Risk Control** | 10-level RiskManager + 5 existing guards | ✅ |
