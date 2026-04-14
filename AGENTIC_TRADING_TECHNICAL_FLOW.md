# AGENTIC TRADING - Technical Flow & Architecture

## 1. Tổng Quan Kiến Trúc (Unified State Machine)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    UNIFIED AGENTIC TRADING SYSTEM                       │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │              UNIFIED STATE MACHINE (5 States)                    │   │
│  │                                                                  │   │
│  │   IDLE → ENTER_GRID → TRADING → EXIT_ALL → WAIT_NEW_RANGE       │   │
│  │                     ↑___________________________________________│   │
│  └──────────────────────────────────────────────────────────────────┘   │
│                              │                                          │
│         ┌────────────────────┼────────────────────┐                     │
│         ↓                    ↓                    ↓                     │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│  │ Data Layer   │    │ Core Engine  │    │ Execution    │              │
│  │ (Market Data)│    │ (Decision)   │    │ (Grid/Trade) │              │
│  └──────────────┘    └──────────────┘    └──────────────┘              │
│         │                   │                   │                        │
│         ↓                   ↓                   ↓                        │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│  │ Real-time    │    │ 4 Factors    │    │ Real-time    │              │
│  │ Exit Monitor │    │ Scoring      │    │ Exit Signals │              │
│  │ (100ms tick) │    │ + State Mgmt │    │ (ADX/BB)     │              │
│  └──────────────┘    └──────────────┘    └──────────────┘              │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘

State Machine Controls:
- RangeDetector: Unknown → Establishing → Active → Breakout → Stabilizing
- GridStateMachine: IDLE → ENTER_GRID → TRADING → EXIT_ALL → WAIT_NEW_RANGE
```

---

## 2. Luồng Dữ Liệu Thị Trường

### 2.1 Warm-up Flow (Khởi Động)

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Start Bot  │────→│ REST API    │────→│ 1000 Candle │────→│ Indicator   │
│             │     │ /klines     │     │ Pre-load    │     │ Calculate   │
└─────────────┘     └─────────────┘     └─────────────┘     └──────┬──────┘
                                                                    │
                                                                    ↓
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Ready to   │←────│ Regime      │←────│ ADX/BB/ATR  │
│  Trade      │     │ Detect      │     │ EMAs        │
└─────────────┘     └─────────────┘     └─────────────┘
```

**Mô tả:**
1. Bot khởi động, gọi REST API lấy 1000 nến lịch sử
2. Tính toán chỉ báo kỹ thuật (ADX, Bollinger, ATR, các EMA)
3. Xác định regime hiện tại từ chỉ báo
4. Chuyển sang trạng thái sẵn sàng giao dịch (không cần hysteresis)

### 2.2 Real-time Flow (Vận Hành)

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│ WebSocket   │────→│ New Candle  │────→│ Slide       │────→│ Update      │
│ @kline_1m   │     │ Arrival     │     │ Window      │     │ Indicators  │
└─────────────┘     └─────────────┘     │ (1000 max)  │     └──────┬──────┘
                                        └─────────────┘            │
                                                                   ↓
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│ 4 Factors   │←────│ Regime      │←────│ Detect      │←────│ Recalculate │
│ Scoring     │     │ Change?     │     │ Regime      │     │ All         │
└──────┬──────┘     └─────────────┘     └─────────────┘     └─────────────┘
       │
       ↓
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│ Score 0-100 │────→│ Pattern     │────→│ Position    │────→│ Grid        │
│             │     │ Matching    │     │ Sizing      │     │ Adapter     │
└─────────────┘     └─────────────┘     └─────────────┘     └──────┬──────┘
                                                                    │
                                                                    ↓
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│ Decision    │←────│ Circuit     │←────│ Execute/    │←────│ Grid Params │
│ Log         │     │ Breakers?   │     │ Wait        │     │ Applied     │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

**Mô tả (V2 - Unified State Machine):**
- WebSocket đẩy nến mới mỗi phút → Update RangeDetector + StateMachine
- **T001**: `shouldSchedulePlacement()` kiểm tra **cả** `RangeState == Active` **và** `GridState ∈ {ENTER_GRID, TRADING}`
- **T003**: Micro grid (0.05% spread, 5 orders/side) là **primary geometry**, BB chỉ dùng để gate permission
- Recalculate chỉ báo → Detect regime → Scoring 4 factors
- **T002**: Dynamic leverage dựa trên BB width (inverse proportion)
- **T009**: Real-time exit goroutine monitor ADX/BB mỗi **100ms**
- **T011**: Multi-layer liquidation protection (4-tier: warn→reduce50%→close→hedge)
- State machine điều khiển placement gating, không chỉ là advisory

---

## 3. Luồng Phân Tích Chế Độ & State Machine (T001-T015)

```
┌────────────────────────────────────────────────────────────────────────┐
│              RANGE DETECTION + GRID STATE MACHINE                      │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   ┌─────────────────────────────────────────────────────────────┐     │
│   │  RANGE DETECTOR (Market Condition)                            │     │
│   │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │     │
│   │  │   ADX    │  │ BB Width │  │   ATR    │  │  EMAs    │  │     │
│   │  │  Period  │  │  Period  │  │  Period  │  │9,21,50,  │  │     │
│   │  │    14    │  │    10    │  │    14    │  │   200    │  │     │
│   │  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘  │     │
│   │       │             │             │             │         │     │
│   │       └─────────────┴──────┬──────┴─────────────┘         │     │
│   │                            │                              │     │
│   │                            ↓                              │     │
│   │  ┌──────────────────────────────────────────────────────┐│     │
│   │  │  RangeState: Unknown → Establishing → Active        ││     │
│   │  │                      ↓              ↓                ││     │
│   │  │              Stabilizing ←── Breakout               ││     │
│   │  └──────────────────────┬───────────────────────────────┘│     │
│   └──────────────────────────┼────────────────────────────────┘     │
│                              │                                      │
│                              ↓ Triggers State Transition           │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │  GRID STATE MACHINE (Trading Lifecycle)                    │   │
│   │                                                             │   │
│   │   IDLE ──EventRangeConfirmed──→ ENTER_GRID                │   │
│   │                                     │                       │   │
│   │                            EventEntryPlaced               │   │
│   │                                     ↓                       │   │
│   │   WAIT_NEW_RANGE ←──EventNewRangeReady── TRADING           │   │
│   │        ↑                              │                     │   │
│   │        └──EventPositionsClosed── EXIT_ALL                   │   │
│   │                              ↑                              │   │
│   │                    EventTrendExit/Emergency                 │   │
│   │                                                             │   │
│   │  T001 Gate: Only ENTER_GRID/TRADING → Place Orders        │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘

Classification Logic:
- Sideways:  ADX < 25, BB width < 0.5%, RangeState = Active → Grid Trading
- Trending:  ADX > 25 → EventTrendExit → EXIT_ALL
- Breakout:  Price outside BB → EventEmergencyExit → EXIT_ALL
- Stabilizing: After breakout, wait for new range → WAIT_NEW_RANGE

---

## 4. Luồng Tính Toán Điểm Số (4 Factors Engine)

```
┌────────────────────────────────────────────────────────────────────────┐
│                     MULTI-FACTOR SCORING FLOW                          │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │
│  │   TREND     │  │  VOLATILITY │  │   VOLUME    │  │  STRUCTURE  │   │
│  │   30%       │  │    25%      │  │    25%      │  │    20%      │   │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘   │
│         │                │                │                │          │
│         ↓                ↓                ↓                ↓          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │
│  │EMA Align    │  │ATR Norm     │  │Vol/MA20    │  │Support/Res  │   │
│  │ADX Strength │  │BB Expansion │  │Trend Dir   │  │Range Detect │   │
│  │Direction    │  │Regime Mult  │  │            │  │             │   │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘   │
│         │                │                │                │          │
│         └────────────────┴────────────────┴────────────────┘          │
│                                    │                                   │
│                                    ↓                                   │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │           WEIGHTED SUM CALCULATION                             │  │
│  │  Score = (Trend × 0.3 + Vol × 0.25 + Volume × 0.25 + Struct × 0.2) │  │
│  │            × RegimeMultiplier                                    │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                    │                                   │
│                                    ↓                                   │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │              CACHE LAYER (5s TTL)                              │  │
│  │  Same indicators within 5s → Return cached score             │  │
│  │  Reduces CPU usage for frequent detections                      │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 5. Luồng Circuit Breakers & Real-time Exit Monitor (T009)

### 5.1 Real-time Exit Monitor (Goroutine riêng)

```
┌────────────────────────────────────────────────────────────────────────┐
│              REAL-TIME EXIT MONITOR (100ms Tick)                      │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐             │
│   │  100ms      │    │  Check All  │    │  Trigger    │             │
│   │  Ticker     │───→│  Symbols    │───→│  Exit?      │             │
│   └─────────────┘    └─────────────┘    └──────┬──────┘             │
│                                                 │                     │
│                      ┌──────────────────────────┼─────────────────┐  │
│                      ↓                          ↓                 ↓  │
│               ┌──────────┐              ┌──────────┐        ┌──────────┐│
│               │ ADX > 25 │              │ BB Width │        │Consecutive││
│               │          │              │ > 1.5%   │        │Losses > 3 ││
│               └────┬─────┘              └────┬─────┘        └────┬─────┘│
│                    │                         │                  │     │
│                    └────────────┬──────────────┘                  │     │
│                               ↓ YES                            ↓ YES│
│                    ┌──────────────────────┐        ┌──────────────────┐ │
│                    │ handleTrendExit()    │        │ handleBreakout() │ │
│                    │                      │        │                  │ │
│                    │ 1. Cancel orders     │        │ 1. Cancel orders │ │
│                    │ 2. Close positions   │        │ 2. Close pos     │ │
│                    │ 3. Clear grid        │        │ 3. Clear grid    │ │
│                    │ 4. pauseTrading()    │        │ 4. pauseTrading()│ │
│                    │ 5. ForceRecalculate()│        │ 5. ForceRecalc() │ │
│                    │ 6. Transition to     │        │ 6. Transition    │ │
│                    │    EXIT_ALL          │        │    EXIT_ALL      │ │
│                    └──────────────────────┘        └──────────────────┘ │
│                                                                        │
│   **T014 - Idempotent**: exitInProgress flag prevents duplicate exits │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Multi-Layer Liquidation Protection (T011)

```
┌────────────────────────────────────────────────────────────────────────┐
│              MULTI-LAYER LIQUIDATION (4-Tier System)                   │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  Tier 1 (50% distance)    Tier 2 (30%)    Tier 3 (15%)   Tier 4 (10%)│
│  ┌─────────────────┐     ┌─────────────┐   ┌──────────┐  ┌──────────┐│
│  │     WARN        │     │ REDUCE 50%  │   │  CLOSE   │  │ HEDGE +  ││
│  │   (Log only)    │────→│   Position  │──→│   ALL    │─→│  CLOSE   ││
│  └─────────────────┘     └─────────────┘   └──────────┘  └──────────┘│
│         ↑                                              │              │
│         └──────────────────────────────────────────────┘              │
│                    positionMonitor() kiểm tra mỗi 30s                   │
│                                                                        │
│   T011: Enabled by default, wired vào positionMonitor                 │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Luồng Pattern Learning (Học Máy)

```
┌────────────────────────────────────────────────────────────────────────┐
│                    PATTERN LEARNING LIFECYCLE                          │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │                   PHASE 1: OBSERVE ONLY (0-200 trades)       │   │
│  │  • Collect: Regime, Indicators, Grid Params, Trade Outcome    │   │
│  │  • Store: JSON file per pair (btcusd1_patterns.json)          │   │
│  │  • Calculate: Decay weight exp(-days/14)                        │   │
│  │  • Status: INACTIVE (does not affect scoring)                  │   │
│  └────────────────────────────────────────────────────────────────┘   │
│                                  │                                     │
│                                  ↓ 200+ trades AND accuracy ≥60%     │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │                   PHASE 2: ACTIVE (≥200 trades)                 │   │
│  │  • Pattern matching: k-NN with similarity threshold 0.8        │   │
│  │  • Impact: ±5 points max on final score                       │   │
│  │  • Only if: Accuracy ≥60% per regime                          │   │
│  └────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │              PATTERN MATCHING FLOW                            │   │
│  ├────────────────────────────────────────────────────────────────┤   │
│  │                                                                │   │
│  │  Current State        Historical Patterns                        │   │
│  │  ┌──────────┐         ┌──────────┐ ┌──────────┐ ┌──────────┐   │   │
│  │  │Indicator │         │ Pattern 1│ │ Pattern 2│ │ Pattern N│   │   │
│  │  │Snapshot │         │  Week ago│ │Yesterday │ │ Today    │   │   │
│  │  └────┬────┘         └────┬─────┘ └────┬─────┘ └────┬─────┘   │   │
│  │       │                    │            │            │          │   │
│  │       └────────────────────┴────────────┴────────────┘          │   │
│  │                    │                                             │   │
│  │                    ↓ Context Vector Similarity                   │   │
│  │       ┌──────────────────────────┐                             │   │
│  │       │   Similarity Score         │                             │   │
│  │       │   (Weighted by Decay)      │                             │   │
│  │       └─────────────┬──────────────┘                             │   │
│  │                     │                                            │   │
│  │                     ↓ Top 5 Matches                             │   │
│  │       ┌──────────────────────────┐                             │   │
│  │       │   Historical PnL         │                             │   │
│  │       │   → Score Impact         │                             │   │
│  │       │   (±5 points max)         │   │
│  │       └──────────────────────────┘                             │   │
│  │                                                                │   │
│  └────────────────────────────────────────────────────────────────┘   │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 7. Luồng Multi-Pair Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                    MULTI-PAIR PATTERN STORAGE                           │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   ┌────────────────────────────────────────────────────────────────┐   │
│   │                    PATTERN STORE MANAGER                        │   │
│   └────────────────────────────────────────────────────────────────┘   │
│                                │                                       │
│         ┌──────────────────────┼──────────────────────┐               │
│         ↓                      ↓                      ↓               │
│   ┌──────────┐           ┌──────────┐           ┌──────────┐         │
│   │ BTCUSD1  │           │ ETHUSD1  │           │ SOLUSD1  │         │
│   │  Store   │           │  Store   │           │  Store   │         │
│   └────┬─────┘           └────┬─────┘           └────┬─────┘         │
│        │                      │                      │                │
│        ↓                      ↓                      ↓                │
│   ┌──────────┐           ┌──────────┐           ┌──────────┐         │
│   │btcusd1_  │           │ethusd1_  │           │solusd1_  │         │
│   │patterns. │           │patterns. │           │patterns. │         │
│   │  json    │           │  json    │           │  json    │         │
│   └──────────┘           └──────────┘           └──────────┘         │
│                                                                        │
│   Mỗi cặp có:                                                          │
│   • Pattern storage riêng                                             │
│   • Accuracy tracking riêng per regime                                 │
│   • Activation threshold riêng (200 trades)                          │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---
## 8. Luồng Position Sizing & Grid Configuration (T002, T003, T012)

### 8.1 Dynamic Leverage Calculator (T002)

```
┌────────────────────────────────────────────────────────────────────────┐
│              DYNAMIC LEVERAGE CALCULATOR (BB Width Based)             │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  Formula: leverage = min(maxLeverage, baseLeverage / bbWidthNormalized)│
│                                                                        │
│  BB Width    │  Calculation         │  Leverage    │  Market Condition  │
│  ────────────┼──────────────────────┼──────────────┼────────────────────│
│  0.2%        │  50 × (0.02/0.002)   │  100x        │  Tight range      │
│  0.5%        │  50 × (0.02/0.005)   │  80x         │  Normal           │
│  1.0%        │  50 × (0.02/0.01)    │  40x         │  Wide             │
│  2.0%        │  50 × (0.02/0.02)    │  20x (capped)│  Volatile         │
│  >2.0%       │  Capped at 2%        │  20x (min)   │  Extreme          │
│                                                                        │
│  Implementation: adaptive_grid/risk_sizing.go:calculateDynamicLeverage()│
│  Wiring: volume_farm_engine.go:setLeverageForSymbols()               │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

### 8.2 Micro Grid Priority Configuration (T003)

```
┌────────────────────────────────────────────────────────────────────────┐
│              MICRO GRID GEOMETRY (Primary - T003)                      │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  placeGridOrders() Logic:                                              │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  1. CHECK: IsMicroGridEnabled() ?                                │   │
│  │       ↓ YES                                                       │   │
│  │  ┌─────────────────────────────────────────────┐                   │   │
│  │  │  placeMicroGridOrders()                     │                   │   │
│  │  │  • Spread: 0.05% (0.0005)                  │                   │   │
│  │  │  • Orders/Side: 5 (total 10)              │                   │   │
│  │  │  • Min Order: $3 USDT                       │                   │   │
│  │  │  • Geometry: Fixed around current price     │                   │   │
│  │  └─────────────────────────────────────────────┘                   │   │
│  │       ↓ NO (fallback)                                             │   │
│  │  ┌─────────────────────────────────────────────┐                   │   │
│  │  │  placeBBGridOrders()                          │                   │   │
│  │  │  • Geometry: BB upper/lower/mid               │                   │   │
│  │  │  • Only if BB range valid                    │                   │   │
│  │  └─────────────────────────────────────────────┘                   │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  T003 Change: Micro grid takes PRECEDENCE over BB bands              │
│  BB/ADX: Used for gate permission (RangeState), NOT geometry           │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

### 8.3 Position Sizing Pipeline

```
┌────────────────────────────────────────────────────────────────────────┐
│                    POSITION SIZING PIPELINE                           │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐         │
│  │  Score   │    │ Volatility│    │ Leverage │    │  Final   │         │
│  │   0-100  │───→│  Multi   │───→│  Dynamic │───→│  Size    │         │
│  └────┬─────┘    └────┬─────┘    └────┬─────┘    └────┬─────┘         │
│       │              │              │              │                  │
│       ↓              ↓              ↓              ↓                  │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐         │
│  │≥75: 1.0  │    │Normal:   │    │Tight:    │    │Calculate │         │
│  │60-74: 0.6│    │1.0       │    │100x      │    │Min/Max  │         │
│  │35-59: 0.3│    │High: 0.5 │    │Wide: 20x │    │Bounds   │         │
│  │<35: 0.0  │    │Extreme:0 │    │(BB based)│    │Applied  │         │
│  └──────────┘    └──────────┘    └──────────┘    └──────────┘         │
│                                                                        │
│  **T012: BB Period = 10** (Unified Agentic + Execution)              │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 9. Luồng Logging & Decision Audit

### 9.1 State Machine JSONL Logging (T015)

```
┌────────────────────────────────────────────────────────────────────────┐
│              STATE TRANSITION LOGGING (JSONL Format)                   │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   Log Entry Example:                                                   │
│   {"timestamp":"2026-04-12T07:45:00Z","symbol":"BTCUSD1",               │
│    "from_state":"TRADING","to_state":"EXIT_ALL","event":"TREND_EXIT",  │
│    "reason":"adx_spike","adx_value":28.5,"bb_width_pct":1.2}           │
│                                                                        │
│   Implementation: adaptive_grid/state_machine.go:Transition()          │
│   Logger: zap.Logger with Info("state_transition", fields...)          │
│                                                                        │
│   Rotation: decisions_YYYY-MM-DD.jsonl                                │
│   Retention: 90 days, compress after 30 days                          │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

### 9.2 Decision Audit Log

```
┌────────────────────────────────────────────────────────────────────────┐
│                    DECISION LOGGING FLOW                                │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   ┌─────────────────────────────────────────────────────────────┐    │
│   │                    DECISION EVENT                            │    │
│   ├─────────────────────────────────────────────────────────────┤    │
│   │  Timestamp: ISO8601                                          │    │
│   │  Regime: {type, confidence, indicators_snapshot}             │    │
│   │  GridState: {current, can_place_orders, is_trading}          │    │
│   │  Factors: [                                                  │    │
│   │    {type: "TREND", raw: 0.75, normalized: 0.8, weight: 0.3}  │    │
│   │    {type: "VOLATILITY", ...}                                  │    │
│   │    {type: "VOLUME", ...}                                     │    │
│   │    {type: "STRUCTURE", ...}                                  │    │
│   │  ]                                                           │    │
│   │  Score: {base: 72, final: 75}                                │    │
│   │  Multipliers: {score: 1.0, volatility: 1.0, leverage: 80}    │    │
│   │  GridParams: {spread: 0.05%, orders: 10, size: 100.0}        │    │
│   │  CircuitBreakers: []                                         │    │
│   │  Rationale: "Micro grid placement with dynamic leverage"    │    │
│   │  Executed: true/false                                        │    │
│   └─────────────────────────────────────────────────────────────┘    │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 10. Shutdown & Graceful Degradation

```
┌────────────────────────────────────────────────────────────────────────┐
│                    SHUTDOWN SEQUENCE                                    │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   Signal: SIGINT/SIGTERM                                               │
│       │                                                                │
│       ↓                                                                │
│   ┌───────────────────────────────────────────────────────────────┐   │
│   │  1. Stop Main Loop                                            │   │
│   │     • Ngừng nhận dữ liệu WebSocket                            │   │
│   │     • Hoàn thành decision đang xử lý                           │   │
│   └───────────────────────────────────────────────────────────────┘   │
│       │                                                                │
│       ↓                                                                │
│   ┌───────────────────────────────────────────────────────────────┐   │
│   │  2. Save State                                                │   │
│   │     • Pattern store: Save tất cả active pairs                   │   │
│   │       - btcusd1_patterns.json                                  │   │
│   │       - ethusd1_patterns.json                                  │   │
│   │       - solusd1_patterns.json                                  │   │
│   │     • Flush decision logs                                       │   │
│   │     • Close file handles                                        │   │
│   └───────────────────────────────────────────────────────────────┘   │
│       │                                                                │
│       ↓                                                                │
│   ┌───────────────────────────────────────────────────────────────┐   │
│   │  3. Cleanup                                                   │   │
│   │     • Đóng kết nối API                                          │   │
│   │     • Release resources                                         │   │
│   │     • Exit 0                                                    │   │
│   └───────────────────────────────────────────────────────────────┘   │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 12. Regrid Logic Implementation (T008)

```
┌────────────────────────────────────────────────────────────────────────┐
│              STRICT REGRID CONDITIONS (T008 - isReadyForRegrid)        │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   Function: adaptive_grid/manager.go:isReadyForRegrid()               │
│                                                                        │
│   ┌─────────────────────────────────────────────────────────────────┐  │
│   │  CONDITIONS (ALL must be true):                                   │  │
│   │                                                                 │  │
│   │  1. ✓ Zero open orders     (GridManager.countActiveGridOrders) │  │
│   │  2. ✓ Zero position        (positions[symbol].PositionAmt == 0)│  │
│   │  3. ✓ Range shift ≥ 0.5%   (current vs lastAccepted center)    │  │
│   │  4. ✓ BB width < 1.5x avg  (currentRange.WidthPct / last)       │  │
│   │  5. ✓ ADX < 20             (detector.averageADXLocked)         │  │
│   │  6. ✓ State = WAIT_NEW_RANGE (GridStateMachine.GetState)         │  │
│   │                                                                 │  │
│   │  Flow: EXIT_ALL → PositionsClosed → WAIT_NEW_RANGE              │  │
│   │              ↓                                                  │  │
│   │         [Check all 6] → All true → EventNewRangeReady          │  │
│   │              ↓                                                  │  │
│   │         ENTER_GRID → Place Micro Grid                           │  │
│   └─────────────────────────────────────────────────────────────────┘  │
│                                                                        │
│   Thread-Safe: Uses RWMutex for concurrent access                      │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 11. Component Interaction Summary (T001-T015)

| Component | Input | Output | Triggers |
|-----------|-------|--------|----------|
| **DataProvider** | WebSocket @kline | Candle | Every 1 minute |
| **RangeDetector** | 1000 Candles + Price | RangeState | Every tick |
| **GridStateMachine** | Events | State + Gates | Transitions only |
| **FactorEngine** | IndicatorSnapshot | Score 0-100 | Regime change or 5s (cache) |
| **PatternStore** | Current indicators | Matches + Impact | Score calculation |
| **RealtimeExitMonitor** | ADX/BB every 100ms | Exit signal | ADX>25 / BB>1.5% |
| **MultiLayerLiquidation** | Position + MarkPrice | Tier actions | Every 30s |
| **GridManager** | StateMachine gates | Placement decision | RangeState==Active && GridState valid |
| **DynamicLeverage** | BB width | Leverage 20x-100x | On range change |
| **Logger** | Decision context | JSONL file | Every decision + state transition |

### 11.1 Task Implementation Map

| Task | File | Function | Status |
|------|------|----------|--------|
| T001 | grid_manager.go | shouldSchedulePlacement() | ✅ RangeState + GridState gates |
| T002 | volume_farm_engine.go | setLeverageForSymbols() | ✅ GetOptimalLeverage() wired |
| T003 | grid_manager.go | placeGridOrders() | ✅ Micro grid precedence |
| T004-T006 | state_machine.go | GridStateMachine struct | ✅ 5 states + transitions |
| T007 | risk_sizing.go | calculateDynamicLeverage() | ✅ Inverse BB width |
| T008 | manager.go | isReadyForRegrid() | ✅ 6 strict conditions |
| T009 | manager.go | realtimeExitMonitor() | ✅ 100ms goroutine |
| T010 | manager.go | RecordTradeResult() | ✅ MaxConsecutiveLosses = 3, triggers ExitAll |
| T011 | manager.go | checkMultiLayerLiquidation() | ✅ 4-tier enabled |
| T012 | config.go | BBPeriod | ✅ Unified to 10 |
| T013 | state_machine.go | RWMutex | ✅ Thread-safe |
| T014 | manager.go | exitInProgress flag | ✅ Idempotent |
| T015 | state_machine.go | Transition() | ✅ JSONL logging |

---

*Document Version: 2.1*  
*Last Updated: 2026-04-12*  
*Aligns with: Core Flow Implementation (T001-T015)*
