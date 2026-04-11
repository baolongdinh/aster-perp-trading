# AGENTIC TRADING - Technical Flow & Architecture

## 1. Tổng Quan Kiến Trúc

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         AGENTIC TRADING SYSTEM                           │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│  │ Data Layer   │───→│ Core Engine  │───→│ Execution    │              │
│  │ (Market Data)│    │ (Decision)   │    │ (Grid/Trade) │              │
│  └──────────────┘    └──────────────┘    └──────────────┘              │
│         │                   │                   │                        │
│         ↓                   ↓                   ↓                        │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│  │ 1000 Candle  │    │ 4 Factors    │    │ Circuit      │              │
│  │ Buffer       │    │ Scoring      │    │ Breakers     │              │
│  └──────────────┘    └──────────────┘    └──────────────┘              │
│                              │                                         │
│                              ↓                                         │
│                       ┌──────────────┐                                  │
│                       │ Pattern      │                                  │
│                       │ Learning     │                                  │
│                       └──────────────┘                                  │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
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
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Ready to   │←────│ Regime      │←────│ Hysteresis  │←────│ ADX/BB/ATR  │
│  Trade      │     │ Detect      │     │ (2 reads)   │     │ EMAs        │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

**Mô tả:**
1. Bot khởi động, gọi REST API lấy 1000 nến lịch sử
2. Tính toán chỉ báo kỹ thuật (ADX, Bollinger, ATR, các EMA)
3. Áp dụng Hysteresis: cần 2 lần đọc regime giống nhau mới xác nhận
4. Chuyển sang trạng thái sẵn sàng giao dịch

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

**Mô tả:**
- WebSocket đẩy nến mới mỗi phút
- Sliding window giữ 1000 nến gần nhất (FIFO)
- Recalculate chỉ báo → Detect regime → Scoring 4 factors
- Pattern matching (nếu active) điều chỉnh ±5 điểm
- Position sizing theo score + volatility
- Circuit breakers kiểm tra trước khi execute
- Grid adapter cập nhật parameters hoặc dừng nếu cầu chì kích hoạt

---

## 3. Luồng Phân Tích Chế Độ Thị Trường (Regime Detection)

```
┌────────────────────────────────────────────────────────────────────────┐
│                     REGIME DETECTION PIPELINE                           │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐       │
│   │   ADX    │    │ BB Width │    │   ATR    │    │  EMAs    │       │
│   │  Period  │    │  Period  │    │  Period  │    │9,21,50,  │       │
│   │    14    │    │    20    │    │    14    │    │   200    │       │
│   └────┬─────┘    └────┬─────┘    └────┬─────┘    └────┬─────┘       │
│        │               │               │               │              │
│        └───────────────┴───────┬───────┴───────────────┘              │
│                                │                                      │
│                                ↓                                      │
│   ┌──────────────────────────────────────────────────────────┐       │
│   │              CLASSIFICATION LOGIC                         │       │
│   ├──────────────────────────────────────────────────────────┤       │
│   │  1. Volatile?  → ATR spike > 3σ trong 5 phút            │       │
│   │        ↓ YES → Regime = VOLATILE                       │       │
│   │  2. Trending?  → ADX > 25 AND EMA 9>21>50>200         │       │
│   │        ↓ YES → Regime = TRENDING                       │       │
│   │  3. Recovery?  → Vừa volatility, ATR normalize        │       │
│   │        ↓ YES → Regime = RECOVERY                      │       │
│   │  4. Sideways?  → ADX < 25 AND BB width < threshold    │       │
│   │        ↓ YES → Regime = SIDEWAYS                      │       │
│   └──────────────────────────┬───────────────────────────────┘       │
│                              │                                        │
│                              ↓                                        │
│   ┌──────────────────────────────────────────────────────────┐       │
│   │              HYSTERESIS (Anti-Flicker)                  │       │
│   ├──────────────────────────────────────────────────────────┤       │
│   │  Reading 1: TRENDING                                     │       │
│   │  Reading 2: TRENDING  ──→ CONFIRM TRENDING              │       │
│   │                                                          │       │
│   │  Reading 1: TRENDING                                     │       │
│   │  Reading 2: SIDEWAYS ──→ KEEP OLD REGIME                │       │
│   └──────────────────────────────────────────────────────────┘       │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

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

## 5. Luồng Circuit Breakers (Cầu Chì An Toàn)

```
┌────────────────────────────────────────────────────────────────────────┐
│                    CIRCUIT BREAKER CHECK FLOW                         │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│                        ┌──────────────┐                               │
│                        │  Pre-Trade   │                               │
│                        │   Check      │                               │
│                        └───────┬──────┘                               │
│                                │                                      │
│              ┌──────────────────┼──────────────────┐                  │
│              ↓                  ↓                  ↓                  │
│       ┌──────────┐      ┌──────────┐      ┌──────────┐              │
│       │Drawdown  │      │Volatility│      │Liquidity │              │
│       │> 10% ?   │      │Spike ?   │      │Crisis ?  │              │
│       └────┬─────┘      └────┬─────┘      └────┬─────┘              │
│            │                │                │                      │
│            ↓ YES            ↓ YES            ↓ YES                   │
│       ┌──────────┐      ┌──────────┐      ┌──────────┐              │
│       │  STOP    │      │ EMERGENCY│      │  PAUSE   │              │
│       │  ALL     │      │  CLOSE   │      │  NEW     │              │
│       └──────────┘      └──────────┘      └──────────┘              │
│            │                │                │                      │
│            └────────────────┴────────────────┘                      │
│                          │                                           │
│                          ↓                                           │
│                   ┌──────────────┐                                  │
│                   │  Priority 1-3  │                                  │
│                   │  Processed   │                                  │
│                   └───────┬──────┘                                  │
│                           │                                         │
│              ┌────────────┼────────────┐                           │
│              ↓            ↓            ↓                           │
│       ┌──────────┐  ┌──────────┐  ┌──────────┐                    │
│       │Consecutive│  │Connection│  │   ALL    │                    │
│       │ Losses   │  │ Failure  │  │  CLEAR   │                    │
│       │ 3x ?     │  │ 3x ?     │  │          │                    │
│       └────┬─────┘  └────┬─────┘  └────┬─────┘                    │
│            │            │            │                            │
│            ↓ YES        ↓ YES        ↓                             │
│       ┌──────────┐  ┌──────────┐  ┌──────────┐                    │
│       │ REDUCE   │  │  PAUSE   │  │  EXECUTE │                    │
│       │  SIZE    │  │  & ALERT │  │  TRADE   │                    │
│       └──────────┘  └──────────┘  └──────────┘                    │
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
## 8. Luồng Position Sizing & Grid Spacing

```
┌────────────────────────────────────────────────────────────────────────┐
│                    POSITION SIZING PIPELINE                           │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐         │
│  │  Score   │    │ Volatility│    │  Pattern │    │  Final   │         │
│  │   0-100  │───→│  Multi   │───→│  Impact  │───→│  Size    │         │
│  └────┬─────┘    └────┬─────┘    └────┬─────┘    └────┬─────┘         │
│       │              │              │              │                  │
│       ↓              ↓              ↓              ↓                  │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐         │
│  │≥75: 1.0  │    │Normal:   │    │±5 pts   │    │Calculate │         │
│  │60-74: 0.5│    │1.0       │    │max      │    │Min/Max  │         │
│  │<60: 0.0  │    │High: 0.5 │    │(if acc  │    │Bounds   │         │
│  │          │    │Extreme:0 │    │≥60%)    │    │Applied  │         │
│  └──────────┘    └──────────┘    └──────────┘    └──────────┘         │
│                                                                        │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │                    GRID SPACING ADJUSTMENT                      │   │
│  ├────────────────────────────────────────────────────────────────┤   │
│  │  Low Vol  (ATR small)  → 0.3% spacing                          │   │
│  │  Normal Vol            → 1.0% spacing (default)                │   │
│  │  High Vol (ATR large)  → 2.0% spacing                          │   │
│  │                                                                │   │
│  │  Stop Loss = Entry Price ± (ATR × Multiplier)                 │   │
│  └────────────────────────────────────────────────────────────────┘   │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 9. Luồng Logging & Decision Audit

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
│   │  Factors: [                                                  │    │
│   │    {type: "TREND", raw: 0.75, normalized: 0.8, weight: 0.3}  │    │
│   │    {type: "VOLATILITY", ...}                                  │    │
│   │    {type: "VOLUME", ...}                                     │    │
│   │    {type: "STRUCTURE", ...}                                  │    │
│   │  ]                                                           │    │
│   │  Score: {base: 72, pattern_impact: +3, final: 75}           │    │
│   │  Multipliers: {score: 1.0, volatility: 1.0, pattern: 1.03}│    │
│   │  GridParams: {spacing: 0.01, size: 100.0, stop_loss:...}   │    │
│   │  PatternMatches: [id1, id2, ...] (nếu có)                    │    │
│   │  CircuitBreakers: [] (nếu kích hoạt)                         │    │
│   │  Rationale: "Deploying full size in sideways regime"        │    │
│   │  Executed: true/false                                       │    │
│   └─────────────────────────────────────────────────────────────┘    │
│                                    │                                   │
│                                    ↓                                   │
│   ┌─────────────────────────────────────────────────────────────┐    │
│   │                    STORAGE LAYER                             │    │
│   ├─────────────────────────────────────────────────────────────┤    │
│   │  Daily File: decisions_YYYY-MM-DD.jsonl                     │    │
│   │  Path: data/logs/decisions/                                  │    │
│   │  Retention: 90 days                                          │    │
│   │  Compression: Sau 30 ngày                                   │    │
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

## 11. Component Interaction Summary

| Component | Input | Output | Triggers |
|-----------|-------|--------|----------|
| **DataProvider** | WebSocket @kline | Candle | Every 1 minute |
| **Detector** | 1000 Candles | RegimeSnapshot | Every 30s |
| **FactorEngine** | IndicatorSnapshot | Score 0-100 | Regime change or 5s (cache) |
| **PatternStore** | Current indicators | Matches + Impact | Score calculation |
| **CircuitBreaker** | Portfolio state | Action/Block | Real-time check |
| **GridAdapter** | Decision | GridParams | Approved deployment |
| **Logger** | Decision context | JSONL file | Every decision |

---

*Document Version: 1.0*  
*Last Updated: 2026-04-10*  
*Format: Technical Architecture Flow (No Code)*
