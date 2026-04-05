# Grid Trading Bot Optimization - Task Breakdown
## Implementation Phases & Tasks

**Specification:** SPECIFICATION.md  
**Total Estimated Effort:** 4 weeks (20 working days)  
**Generated:** April 6, 2026

---

## Phase Overview

| Phase | Focus | Duration | Priority |
|-------|-------|----------|----------|
| Phase 1 | Core Infrastructure & Safety | Week 1 | P0 - Critical |
| Phase 2 | Dynamic Grid & ATR | Week 2 | P1 - High |
| Phase 3 | Inventory Skew & Position Mgmt | Week 2-3 | P1 - High |
| Phase 4 | Advanced Stop-Loss & Trend | Week 3 | P2 - Medium |
| Phase 5 | Integration, Testing & Polish | Week 4 | P1 - High |

---

## Phase 1: Core Infrastructure & Safety (Week 1)
### Goal: Implement backend safeguards and foundational risk management

#### Setup Tasks
- [ ] T001 [P] Create `ATRCalculator` struct with 14-period ATR calculation in `adaptive_grid/atr_calculator.go`
- [ ] T002 [P] Create `RSICalculator` struct with 14-period RSI calculation in `adaptive_grid/rsi_calculator.go`
- [ ] T003 Create `OrderLockManager` for per-symbol processing mutex in `adaptive_grid/order_lock.go`
- [ ] T004 Create `FillEventDeduplicator` with Redis/memory backing in `adaptive_grid/dedup.go`

#### Anti-Replay & Order Safety
- [ ] T005 Implement `LockOrderProcessing(symbol)` with 5-second timeout in `adaptive_grid/order_lock.go:45-80`
- [ ] T006 Implement `UnlockOrderProcessing(symbol)` with safe cleanup in `adaptive_grid/order_lock.go:82-110`
- [ ] T007 Implement `IsDuplicateFill(orderID, timestamp)` checking last 100 events in `adaptive_grid/dedup.go:50-90`
- [ ] T008 Implement state transition validation: PENDING→FILLED only in `adaptive_grid/validation.go`
- [ ] T009 Add state machine: Reject FILLED→PENDING transitions with alert logging

#### Spread Protection
- [ ] T010 Create `OrderbookMonitor` struct in `adaptive_grid/spread_protection.go`
- [ ] T011 Implement `GetSpreadPct()` calculation: (Ask-Bid)/MidPrice in `adaptive_grid/spread_protection.go:60-85`
- [ ] T012 Implement `IsSpreadTooWide(threshold)` returning boolean in `adaptive_grid/spread_protection.go:87-110`
- [ ] T013 Add `PauseTradingIfSpreadExceeds(0.1%)` with 3-second check in `adaptive_grid/manager.go:250-280`
- [ ] T014 Add `ResumeTradingIfSpreadNormal()` after 30s of normal spread

#### Testing Phase 1
- [ ] T015 [P] Write unit tests for ATR calculator with known values
- [ ] T016 [P] Write unit tests for RSI calculator
- [ ] T017 Write integration test: Order lock prevents concurrent processing
- [ ] T018 Write test: Duplicate fill detection works correctly

---

## Phase 2: Dynamic Grid & ATR (Week 2)
### Goal: Implement adaptive spread based on market volatility

#### Dynamic Grid Core
- [ ] T019 [US1] Create `DynamicSpreadCalculator` struct in `adaptive_grid/dynamic_spread.go`
- [ ] T020 [US1] [P] Implement `GetATRPercent(atr, price)` calculation in `adaptive_grid/dynamic_spread.go:45-60`
- [ ] T021 [US1] Implement spread multiplier logic:
  - ATR<0.3%: 0.6x
  - ATR 0.3-0.8%: 1.0x
  - ATR 0.8-1.5%: 1.8x
  - ATR>1.5%: 2.5x
  in `adaptive_grid/dynamic_spread.go:62-120`
- [ ] T022 [US1] Implement `CalculateDynamicSpread(baseSpread, atrPercent)` in `adaptive_grid/dynamic_spread.go:122-150`

#### Grid Level Adjustment
- [ ] T023 [US1] Implement `AdjustGridLevels(currentLevels, spreadMultiplier)` in `adaptive_grid/dynamic_spread.go:152-180`
- [ ] T024 [US1] [P] Add logic: If multiplier>2.0, reduce levels by 30% (min 3 levels)
- [ ] T025 [US1] [P] Add logic: If multiplier<0.7, increase levels by 20% (max 10 levels)

#### Integration with GridManager
- [ ] T026 [US1] Add `dynamicSpreadCalc` field to `AdaptiveGridManager` struct
- [ ] T027 [US1] Implement `UpdateATR(high, low, close)` called every 5 minutes in `adaptive_grid/manager.go:380-410`
- [ ] T028 [US1] Modify `GetGridSpread()` to use dynamic calculation in `adaptive_grid/manager.go:540-570`
- [ ] T029 [US1] Add volatility regime logging every 15 minutes

#### Configuration
- [ ] T030 [US1] Create `DynamicGridConfig` struct with ATR period, thresholds, multipliers
- [ ] T031 [US1] Add config loading from YAML in `config/dynamic_grid.yaml`
- [ ] T032 [US1] Implement hot-reload for ATR configuration

#### Testing Phase 2
- [ ] T033 [US1] [P] Write test: ATR 0.2% → spread multiplier 0.6x
- [ ] T034 [US1] [P] Write test: ATR 1.0% → spread multiplier 1.8x
- [ ] T035 [US1] Write test: Extreme ATR 2.0% → levels reduced correctly
- [ ] T036 [US1] Write integration test: Spread updates after ATR calculation

---

## Phase 3: Inventory Skew & Position Management (Week 2-3)
### Goal: Implement position-based skew adjustment to prevent liquidation

#### Inventory Tracking
- [ ] T037 [US2] Create `InventoryManager` struct in `adaptive_grid/inventory_manager.go`
- [ ] T038 [US2] Implement `TrackPosition(symbol, side, size, price)` in `adaptive_grid/inventory_manager.go:50-85`
- [ ] T039 [US2] [P] Implement `GetNetExposure(symbol)` returning quantity and notional in `adaptive_grid/inventory_manager.go:87-120`
- [ ] T040 [US2] Implement `CalculateSkewRatio(netExposure, maxInventory)` in `adaptive_grid/inventory_manager.go:122-145`

#### Skew Actions
- [ ] T041 [US2] Implement `GetSkewAction(skewRatio)` returning action type:
  - <0.3: NORMAL
  - 0.3-0.6: REDUCE_SKEW_SIDE
  - 0.6-0.8: PAUSE_SKEW_SIDE
  - >0.8: EMERGENCY_SKEW
  in `adaptive_grid/inventory_manager.go:147-200`
- [ ] T042 [US2] Implement `ReduceOrderSize(side, reductionPct)` in `adaptive_grid/inventory_manager.go:202-230`
- [ ] T043 [US2] Implement `PauseSideOrders(symbol, side)` in `adaptive_grid/inventory_manager.go:232-260`
- [ ] T044 [US2] Implement `GetAdjustedTakeProfitDistance(side, skewRatio)` in `adaptive_grid/inventory_manager.go:262-300`

#### Emergency Skew Handling
- [ ] T045 [US2] Implement `CloseFurthestPositions(symbol, count)` using market orders in `adaptive_grid/inventory_manager.go:302-340`
- [ ] T046 [US2] Implement `EmergencyReduceAllTakeProfits(symbol, targetPct)` in `adaptive_grid/inventory_manager.go:342-380`
- [ ] T047 [US2] Add emergency skew trigger to `CanPlaceOrder()` check

#### Integration with Order Flow
- [ ] T048 [US2] Modify `GetTaperedOrderSize()` to consider inventory skew
- [ ] T049 [US2] Add skew check before placing each order in `placeGridOrders()`
- [ ] T050 [US2] Implement skew-adjusted take-profit calculation in order placement
- [ ] T051 [US2] Add inventory state logging every 5 minutes

#### Configuration
- [ ] T052 [US2] Create `InventorySkewConfig` struct with thresholds and actions
- [ ] T053 [US2] Add config to `config/inventory_skew.yaml`

#### Testing Phase 3
- [ ] T054 [US2] [P] Write test: Skew ratio 0.5 → action REDUCE_SKEW_SIDE
- [ ] T055 [US2] [P] Write test: Skew ratio 0.9 → action EMERGENCY_SKEW
- [ ] T056 [US2] Write test: Take-profit distance reduced correctly at 0.7 skew
- [ ] T057 [US2] Write integration test: Emergency close executes within 10 seconds

---

## Phase 4: Advanced Stop-Loss & Trend Detection (Week 3)
### Goal: Implement cluster-based stop-loss and RSI trend detection

#### Cluster Stop-Loss - Time Based
- [ ] T058 [US3] Create `ClusterManager` struct in `adaptive_grid/cluster_manager.go`
- [ ] T059 [US3] Implement `TrackClusterEntry(symbol, level, entryTime)` in `adaptive_grid/cluster_manager.go:45-80`
- [ ] T060 [US3] [P] Implement `GetClusterAge(symbol, level)` in `adaptive_grid/cluster_manager.go:82-110`
- [ ] T061 [US3] Implement `CheckTimeBasedStopLoss(symbol)` with thresholds:
  - 2h + <-0.5%: MONITOR_CLOSE
  - 4h + <-1.0%: EMERGENCY_CLOSE
  - 8h: STALE_CLOSE
  in `adaptive_grid/cluster_manager.go:112-160`

#### Breakeven Exit Logic
- [ ] T062 [US3] Implement `CheckBreakevenExit(symbol, currentPrice)` in `adaptive_grid/cluster_manager.go:162-210`
- [ ] T063 [US3] [P] Implement `Close50PercentAtRecovery(symbol, recoveryLevel)` in `adaptive_grid/cluster_manager.go:212-250`
- [ ] T064 [US3] Implement `Close100PercentAtBreakeven(symbol)` in `adaptive_grid/cluster_manager.go:252-290`
- [ ] T065 [US3] Add breakeven logging: "Breakeven exit after X% drawdown"

#### Cluster Heat Map
- [ ] T066 [US3] Implement `GenerateClusterHeatMap(symbol)` in `adaptive_grid/cluster_manager.go:292-340`
- [ ] T067 [US3] Add heat map logging every 15 minutes during active positions

#### RSI Trend Detection
- [ ] T068 [US4] Create `TrendDetector` struct with RSI calculator in `adaptive_grid/trend_detector.go`
- [ ] T069 [US4] [P] Implement `CalculateRSI(prices, period=14)` in `adaptive_grid/trend_detector.go:45-85`
- [ ] T070 [US4] Implement `GetTrendState(rsi)` returning:
  - RSI>70: STRONG_UP
  - RSI>60: UP
  - RSI 40-60: NEUTRAL
  - RSI<40: DOWN
  - RSI<30: STRONG_DOWN
  in `adaptive_grid/trend_detector.go:87-130`
- [ ] T071 [US4] Implement `ShouldPauseCounterTrend(trendState, side)` in `adaptive_grid/trend_detector.go:132-170`

#### Trend Persistence
- [ ] T072 [US4] Implement `CalculateTrendScore(rsi, price, ema, volume)` in `adaptive_grid/trend_detector.go:172-220`
- [ ] T073 [US4] Add trend persistence check: Require 15-minute RSI confirmation
- [ ] T074 [US4] Implement trend exhaustion detection via RSI divergence

#### Integration
- [ ] T075 [US3/US4] Add time-based stop-loss check to position monitor (every 5 min)
- [ ] T076 [US3/US4] Add breakeven check to price update handler
- [ ] T077 [US4] Add trend detection check before placing orders in `CanPlaceOrder()`
- [ ] T078 [US4] Implement `GetTrendAdjustedSize(size, trendState, side)`

#### Funding Rate Protection
- [ ] T079 [P] Create `FundingRateMonitor` in `adaptive_grid/funding_monitor.go`
- [ ] T080 [P] Implement `GetCurrentFundingRate(symbol)` API call
- [ ] T081 [P] Implement `AdjustForHighFunding(symbol, rate)` reducing levels if |rate|>0.03%

#### Configuration
- [ ] T082 [US3] Create `ClusterStopLossConfig` in `config/cluster_stoploss.yaml`
- [ ] T083 [US4] Create `TrendDetectionConfig` in `config/trend_detection.yaml`
- [ ] T084 [P] Create `FundingProtectionConfig` in `config/funding_protection.yaml`

#### Testing Phase 4
- [ ] T085 [US3] [P] Write test: 4h old position with -1.1% PnL triggers emergency close
- [ ] T086 [US3] Write test: Breakeven exit triggers at correct recovery level
- [ ] T087 [US4] [P] Write test: RSI 72 triggers pause for short orders
- [ ] T088 [US4] Write test: Trend score calculation with mock data
- [ ] T089 [P] Write test: High funding rate triggers level reduction

---

## Phase 5: Integration, Testing & Polish (Week 4)
### Goal: Integrate all components, comprehensive testing, monitoring

#### Integration
- [ ] T090 Integrate `DynamicSpreadCalculator` with `RangeDetector` (use ATR from range)
- [ ] T091 Integrate `InventoryManager` with `VolumeScaler` (apply skew after tapering)
- [ ] T092 Integrate `TrendDetector` with `TimeFilter` (reduce size more in volatile + trend)
- [ ] T093 Implement unified `CanPlaceOrder()` with all checks:
  - TimeFilter
  - RangeDetector
  - TrendDetector
  - InventoryManager
  - SpreadProtection
  in priority order

#### Configuration Integration
- [ ] T094 Create master config loader `LoadOptimizationConfig()` in `config/loader.go`
- [ ] T095 Implement config validation across all modules
- [ ] T096 Implement hot-reload for all optimization configs
- [ ] T097 Add config example files with documentation

#### Monitoring & Logging
- [ ] T098 Create `OptimizationMetrics` collector in `metrics/optimization.go`
- [ ] T099 Implement metrics export: Spread multiplier, Skew ratio, Trend state
- [ ] T100 Add structured logging for all optimization decisions
- [ ] T101 Create dashboard data endpoint `/api/optimization/status`

#### Testing - System Level
- [ ] T102 [P] Write integration test: Full flow from price update to order placement
- [ ] T103 Write stress test: 1000 rapid price updates, verify no duplicate orders
- [ ] T104 Write scenario test: High volatility → spread widens → fewer orders
- [ ] T105 Write scenario test: Strong trend detected → shorts paused → longs reduced
- [ ] T106 Write scenario test: Inventory skew critical → emergency close executed

#### Documentation
- [ ] T107 [P] Update API documentation with new optimization endpoints
- [ ] T108 [P] Create operational runbook: "Tuning Optimization Parameters"
- [ ] T109 Create troubleshooting guide: "Common Optimization Issues"
- [ ] T110 Write migration guide from v1.0 to v1.1 (optimization features)

#### Final Polish
- [ ] T111 Performance optimization: Profile and optimize hot paths
- [ ] T112 Add circuit breaker for optimization calculations (fail-safe defaults)
- [ ] T113 Implement graceful degradation: If ATR calc fails, use last known value
- [ ] T114 Final code review: Check all TODOs, error handling, logging

---

## Dependency Graph

```
Phase 1 (Infrastructure)
├── T001-T004: Core calculators & locks
├── T005-T009: Anti-replay
└── T010-T014: Spread protection
    ↓
Phase 2 (Dynamic Grid)
├── T019-T022: ATR-based spread ← depends on T001
├── T023-T025: Level adjustment
└── T026-T029: Integration ← depends on Phase 1 locks
    ↓
Phase 3 (Inventory Skew)
├── T037-T044: Inventory tracking ← depends on T038 (position tracking)
├── T045-T047: Emergency handling ← depends on T005 (order locks)
└── T048-T051: Integration ← depends on Phase 2 spread
    ↓
Phase 4 (Stop-Loss & Trend)
├── T058-T067: Cluster stop-loss ← depends on T038
├── T068-T074: Trend detection ← depends on T002 (RSI)
└── T079-T081: Funding protection ← independent
    ↓
Phase 5 (Integration)
├── T090-T093: Full integration ← depends on all previous
├── T102-T106: System tests ← depends on T090-T093
└── T107-T114: Polish ← depends on all
```

---

## Parallel Execution Opportunities

### Within Phase 1 (Days 1-2)
- T001, T002 (calculators) → Parallel
- T003, T004 (locks, dedup) → Parallel
- T005-T009 (anti-replay) → Sequential after T003-T004
- T010-T014 (spread) → Parallel with anti-replay

### Within Phase 2 (Days 3-5)
- T019, T020 (dynamic spread core) → Sequential
- T023-T025 (level adjustment) → Parallel with integration
- T030-T032 (config) → Parallel with core logic

### Within Phase 3 (Days 5-8)
- T037-T041 (inventory tracking) → Sequential
- T042-T047 (skew actions) → Parallel after T041
- T052-T053 (config) → Parallel

### Within Phase 4 (Days 8-11)
- T058-T061 (time stop-loss) → Sequential
- T062-T065 (breakeven) → Parallel with T058-T061
- T068-T071 (RSI trend) → Sequential
- T079-T081 (funding) → Parallel

### Within Phase 5 (Days 12-15)
- T090-T093 (integration) → Sequential, critical path
- T102-T106 (tests) → Parallel after integration
- T107-T110 (docs) → Parallel with tests

---

## MVP Scope Recommendation

**Minimum Viable Product (Week 1-2):**
1. Phase 1: Core safety infrastructure (T001-T018) - CRITICAL
2. Phase 2: Dynamic Grid with ATR (T019-T036) - HIGH VALUE
3. Phase 3: Basic Inventory Skew (T037-T057) - RISK MITIGATION

**Deferred to Post-MVP:**
- T058-T067: Cluster stop-loss (can use position monitor stop-loss as fallback)
- T068-T074: Full trend detection (can start with simple RSI threshold)
- T079-T081: Funding protection (monitor-only mode initially)

---

## Success Criteria per Phase

| Phase | Criteria | Measurement |
|-------|----------|-------------|
| Phase 1 | All safeguards functional | 0 duplicate fills in 1000 test events |
| Phase 2 | Spread adjusts to volatility | <3% deviation from target ATR multiplier |
| Phase 3 | Skew reduces size correctly | 50% size reduction at 0.6 skew ratio |
| Phase 4 | Stop-loss executes on time | <60s delay from threshold breach |
| Phase 5 | System integration solid | All tests pass, metrics exported |

---

## Risk Mitigation

| Risk | Tasks Affected | Mitigation |
|------|----------------|------------|
| ATR calculation bug | T019-T036 | Fallback to fixed spread if ATR fails |
| Lock contention | T003-T009 | 5-second timeout with automatic unlock |
| RSI whipsaw | T068-T074 | Require 15-minute persistence |
| Config load failure | All config tasks | Use safe defaults, alert but continue |
| Performance degradation | All | Profile at T111, optimize hot paths |

---

## Task Count Summary

| Phase | Total Tasks | Parallel Tasks | Estimated Hours |
|-------|-------------|----------------|---------------|
| Phase 1 | 18 | 8 | 40 hours |
| Phase 2 | 18 | 6 | 35 hours |
| Phase 3 | 21 | 8 | 45 hours |
| Phase 4 | 32 | 12 | 50 hours |
| Phase 5 | 25 | 10 | 40 hours |
| **Total** | **114** | **44** | **210 hours (~5.2 weeks)** |

**Adjusted for 1 developer at 80% capacity: 4 weeks**

---

**END OF TASK BREAKDOWN**
