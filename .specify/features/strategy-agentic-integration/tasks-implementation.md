# Implementation Tasks: Adaptive State-Based Trading System

> Phân rã chi tiết từng task theo phase để implement hệ thống state-based trading

---

## Phase 1: Setup & Infrastructure (0.5 day)

**Goal**: Chuẩn bị môi trường, kiểm tra compatibility

### Setup Tasks

- [X] T001 Verify existing state machine structure in `backend/internal/farming/adaptive_grid/state_machine.go`
- [X] T002 Check Strategy interface compatibility in `backend/internal/strategy/interface.go`
- [X] T003 Verify RegimeDetector integration points in `backend/internal/agentic/regime_detector.go`
- [X] T004 Check WebSocket kline stream availability in `backend/internal/stream/websocket.go`
- [X] T005 Verify config schema can accommodate state manager config in `backend/internal/config/config.go`
- [X] T006 Create feature branch `feature/adaptive-state-trading`

---

## Phase 2: Foundational - Core Infrastructure (2 days)

**Goal**: Xây dựng infrastructure cốt lõi mà tất cả states phụ thuộc

### Foundational Tasks

- [X] T007 Define `TradingMode` enum (GRID, TRENDING, ACCUMULATION, DEFENSIVE, RECOVERY) in `backend/internal/agentic/types.go`
- [X] T008 Define `TradingModeScore` struct with score components in `backend/internal/agentic/types.go`
- [X] T009 Define `StateTransition` struct với smoothing duration in `backend/internal/agentic/types.go`
- [X] T010 Define `AdaptiveStateManager` interface in `backend/internal/agentic/state_manager.go`
- [X] T011 Define `ScoreCalculationEngine` interface in `backend/internal/agentic/score_engine.go`
- [X] T012 Define `DecisionEngine` interface for centralized decisions in `backend/internal/agentic/decision_engine.go`
- [X] T013 Implement `ScoreCalculationEngine` với Grid Score formula in `backend/internal/agentic/score_engine.go`
- [X] T014 Implement `ScoreCalculationEngine` với Trend Score formula in `backend/internal/agentic/score_engine.go`
- [X] T015 Implement `ScoreCalculationEngine` với Hybrid Trend Score in `backend/internal/agentic/score_engine.go`
- [X] T016 Implement `DecisionEngine` với transition decision logic in `backend/internal/agentic/decision_engine.go`
- [X] T017 Add state transition hysteresis (buffer +/- 0.1) in `backend/internal/agentic/decision_engine.go`
- [X] T018 Implement lock-free state coordination (version + CAS) in `backend/internal/agentic/decision_engine.go`
- [X] T019 Create event publisher for state changes in `backend/internal/agentic/event_publisher.go`
- [X] T020 Add config section cho state manager in `backend/internal/config/agentic_config.go`
- [X] T021 Create unit tests cho ScoreCalculationEngine in `backend/internal/agentic/score_engine_test.go`
- [X] T022 Create unit tests cho DecisionEngine in `backend/internal/agentic/decision_engine_test.go`

---

## Phase 3: User Story 1 - IDLE State (P0) - 1 day

**Story**: As a trader, I want the bot to wait in IDLE until clear opportunity emerges  
**Test Criteria**: `TestIdleTransitions` passes - IDLE → WAIT_NEW_RANGE khi Grid Score > 0.6

### US1 Tasks

- [X] T023 [US1] Implement `IdleStateHandler` struct in `backend/internal/agentic/idle_state.go`
- [X] T024 [US1] Implement `HandleState()` method - calculate scores in `backend/internal/agentic/idle_state.go`
- [X] T025 [US1] Add IDLE → WAIT_NEW_RANGE transition logic in `backend/internal/agentic/idle_state.go`
- [X] T026 [US1] Add IDLE → TRENDING transition logic (bypass wait) in `backend/internal/agentic/idle_state.go`
- [X] T027 [US1] Add timeout mechanism (300s max in IDLE) in `backend/internal/agentic/idle_state.go`
- [X] T028 [US1] Implement score trend monitoring (5 min window) in `backend/internal/agentic/idle_state.go`
- [X] T029 [US1] Add volatility check before transition in `backend/internal/agentic/idle_state.go`
- [X] T030 [US1] Wire IdleStateHandler vào AgenticEngine in `backend/internal/agentic/engine.go`
- [X] T031 [US1] Create unit test `TestIdleToWaitNewRange` in `backend/internal/agentic/idle_state_test.go`
- [X] T032 [US1] Create unit test `TestIdleToTrending` in `backend/internal/agentic/idle_state_test.go`
- [X] T033 [US1] Create integration test `TestIdleTransitions` in `backend/internal/agentic/idle_state_test.go`

---

## Phase 4: User Story 2 - WAIT_NEW_RANGE State (P0) - 1.5 days

**Story**: As a trader, I want the bot to detect range and decide between grid or trend  
**Test Criteria**: `TestWaitNewRangeDecisions` passes - detect range, compression, hoặc trend

### US2 Tasks

- [X] T034 [US2] Implement `WaitRangeStateHandler` in `backend/internal/agentic/wait_range_state.go`
- [X] T035 [US2] Implement `detectRange()` với high/low boundaries in `backend/internal/agentic/wait_range_state.go`
- [X] T036 [US2] Implement `calculateRangeQuality()` score in `backend/internal/agentic/wait_range_state.go`
- [X] T037 [US2] Implement `isCompressionDetected()` (BB width < 0.02, ATR < 0.003) in `backend/internal/agentic/wait_range_state.go`
- [X] T038 [US2] Add WAIT_NEW_RANGE → ENTER_GRID transition in `backend/internal/agentic/wait_range_state.go`
- [X] T039 [US2] Add WAIT_NEW_RANGE → ACCUMULATION transition (compression) in `backend/internal/agentic/wait_range_state.go`
- [X] T040 [US2] Add WAIT_NEW_RANGE → TRENDING transition (trend detected) in `backend/internal/agentic/wait_range_state.go`
- [X] T041 [US2] Add WAIT_NEW_RANGE → DEFENSIVE (volatility spike) in `backend/internal/agentic/wait_range_state.go`
- [X] T042 [US2] Add timeout mechanism (120s max) in `backend/internal/agentic/wait_range_state.go`
- [X] T043 [US2] Implement range boundary storage in `backend/internal/agentic/wait_range_state.go`
- [X] T044 [US2] Wire handler vào AgenticEngine in `backend/internal/agentic/engine.go`
- [X] T045 [US2] Create unit test `TestDetectRange` in `backend/internal/agentic/wait_range_state_test.go`
- [X] T046 [US2] Create unit test `TestCompressionDetection` in `backend/internal/agentic/wait_range_state_test.go`
- [X] T047 [US2] Create integration test `TestWaitNewRangeDecisions` in `backend/internal/agentic/wait_range_state_test.go`

---

## Phase 5: User Story 3 - ENTER_GRID State (P0) - 1.5 days

**Story**: As a trader, I want signal-triggered grid entry với asymmetric spreads  
**Test Criteria**: `TestEnterGridFlow` passes - signal > 0.5 → place grid với adjusted spreads

### US3 Tasks

- [X] T048 [US3] Implement `EnterGridStateHandler` in `backend/internal/agentic/enter_grid_state.go`
- [X] T049 [US3] Implement signal aggregation from strategies in `backend/internal/agentic/enter_grid_state.go`
- [X] T050 [US3] Implement `calculateGridParameters()` với ATR-based levels in `backend/internal/agentic/enter_grid_state.go`
- [X] T051 [US3] Implement asymmetric spread adjustment (FVG side tighten 30%) in `backend/internal/agentic/enter_grid_state.go`
- [X] T052 [US3] Implement signal-triggered entry (hybrid mode) in `backend/internal/agentic/enter_grid_state.go`
- [X] T053 [US3] Add timeout handling (60s) với reduced size in `backend/internal/agentic/enter_grid_state.go`
- [X] T054 [US3] Implement `placeGridOrders()` integration in `backend/internal/agentic/enter_grid_state.go`
- [X] T055 [US3] Add ENTER_GRID → TRADING transition in `backend/internal/agentic/enter_grid_state.go`
- [X] T056 [US3] Add ENTER_GRID → TRENDING (trend emerges) in `backend/internal/agentic/enter_grid_state.go`
- [X] T057 [US3] Add error handling → WAIT_NEW_RANGE in `backend/internal/agentic/enter_grid_state.go`
- [X] T058 [US3] Wire handler vào AgenticEngine in `backend/internal/agentic/engine.go`
- [X] T059 [US3] Create unit test `TestAsymmetricSpreadCalculation` in `backend/internal/agentic/enter_grid_state_test.go`
- [X] T060 [US3] Create unit test `TestSignalTriggeredEntry` in `backend/internal/agentic/enter_grid_state_test.go`
- [X] T061 [US3] Create integration test `TestEnterGridFlow` in `backend/internal/agentic/enter_grid_state_test.go`

---

## Phase 6: User Story 4 - TRADING (GRID) State (P0) - 2 days

**Story**: As a trader, I want continuous grid management với trend detection  
**Test Criteria**: `TestTradingGridManagement` passes - rebalancing, blending, trend switch

### US4 Tasks

- [X] T062 [US4] Implement `TradingGridStateHandler` in `backend/internal/agentic/trading_grid_state.go`
- [X] T063 [US4] Implement signal blending (entropy-weighted) in `backend/internal/agentic/trading_grid_state.go`
- [X] T064 [US4] Implement continuous rebalancing (50% levels filled) in `backend/internal/agentic/trading_grid_state.go`
- [X] T065 [US4] Add trend emergence detection → TRENDING in `backend/internal/agentic/trading_grid_state.go`
- [X] T066 [US4] Add risk checks: max loss (-3%), position size (5%), volatility in `backend/internal/agentic/trading_grid_state.go`
- [X] T067 [US4] Add range broken detection → EXIT_ALL in `backend/internal/agentic/trading_grid_state.go`
- [X] T068 [US4] Implement grid intensity adjustment based on signals in `backend/internal/agentic/trading_grid_state.go`
- [X] T069 [US4] Wire handler vào AgenticEngine in `backend/internal/agentic/engine.go`
- [X] T070 [US4] Create unit test `TestSignalBlending` in `backend/internal/agentic/trading_grid_state_test.go`
- [X] T071 [US4] Create unit test `TestRebalancingTrigger` in `backend/internal/agentic/trading_grid_state_test.go`
- [X] T072 [US4] Create integration test `TestTradingGridManagement` in `backend/internal/agentic/trading_grid_state_test.go`

---

## Phase 7: User Story 5 - TRENDING State (P0) - 2 days

**Story**: As a trader, I want hybrid trend following với breakout + momentum  
**Test Criteria**: `TestTrendingHybridStrategy` passes - hybrid entry, trailing stop, micro-profit

### US5 Tasks

- [X] T073 [US5] Implement `TrendingStateHandler` in `backend/internal/agentic/trending_state.go`
- [X] T074 [US5] Implement hybrid trend detection (breakout + momentum) in `backend/internal/agentic/trending_state.go`
- [X] T075 [US5] Implement micro-profit taking at FVG zones (25%) in `backend/internal/agentic/trending_state.go`
- [X] T076 [US5] Implement trailing stop (2-3x ATR) in `backend/internal/agentic/trending_state.go`
- [X] T077 [US5] Add trend exhaustion detection → EXIT_ALL in `backend/internal/agentic/trending_state.go`
- [X] T078 [US5] Add grid opportunity detection (sideways returning) in `backend/internal/agentic/trending_state.go`
- [X] T079 [US5] Add risk checks: stop loss, trailing stop, max loss (-3%), time limit in `backend/internal/agentic/trending_state.go`
- [X] T080 [US5] Wire handler vào AgenticEngine in `backend/internal/agentic/engine.go`
- [X] T081 [US5] Create unit test `TestHybridTrendScore` in `backend/internal/agentic/trending_state_test.go`
- [X] T082 [US5] Create unit test `TestTrailingStopLogic` in `backend/internal/agentic/trending_state_test.go`
- [X] T083 [US5] Create integration test `TestTrendingHybridStrategy` in `backend/internal/agentic/trending_state_test.go`

---

## Phase 8: User Story 6 - ACCUMULATION State (P1) - 1.5 days

**Story**: As a trader, I want pre-breakout accumulation với Wyckoff pattern  
**Test Criteria**: `TestAccumulationStrategy` passes - detect compression, position building, breakout

### US6 Tasks

- [X] T084 [US6] Implement `AccumulationStateHandler` in `backend/internal/agentic/accumulation_state.go`
- [X] T085 [US6] Implement Wyckoff phase detection in `backend/internal/agentic/accumulation_state.go`
- [X] T086 [US6] Implement compression detection (BB width < 0.015) in `backend/internal/agentic/accumulation_state.go`
- [X] T087 [US6] Implement position building (gradual sizing) in `backend/internal/agentic/accumulation_state.go`
- [X] T088 [US6] Add breakout detection → TRENDING in `backend/internal/agentic/accumulation_state.go`
- [X] T089 [US6] Add Sign of Strength (SOS) detection → TRENDING in `backend/internal/agentic/accumulation_state.go`
- [X] T090 [US6] Add risk checks: time limit (8hr), position size (3%), volatility in `backend/internal/agentic/accumulation_state.go`
- [X] T091 [US6] Add failed accumulation detection → DEFENSIVE in `backend/internal/agentic/accumulation_state.go`
- [X] T092 [US6] Wire handler vào AgenticEngine in `backend/internal/agentic/engine.go`
- [X] T093 [US6] Create unit test `TestWyckoffPhaseDetection` in `backend/internal/agentic/accumulation_state_test.go`
- [X] T094 [US6] Create integration test `TestAccumulationStrategy` in `backend/internal/agentic/accumulation_state_test.go`
- [X] T095 [US6] Create integration test `TestAccumulationStrategy` in `backend/internal/agentic/integration_test.go`
- [X] T096 [US6] Implement `predictBreakoutDirection()` (volume profile, order flow) in `backend/internal/agentic/states/accumulation_handler.go`
- [X] T097 [US6] Implement `buyDips()` / `sellRallies()` grid in `backend/internal/agentic/states/accumulation_handler.go`
- [X] T098 [US6] Implement `detectBreakoutImminent()` in `backend/internal/agentic/states/accumulation_handler.go`
- [X] T099 [US6] Implement `confirmBreakout()` với direction validation in `backend/internal/agentic/states/accumulation_handler.go`
- [X] T100 [US6] Add position add on breakout (150% size) in `backend/internal/agentic/states/accumulation_handler.go`
- [X] T101 [US6] Add ACCUMULATION → TRENDING (breakout confirmed) in `backend/internal/agentic/states/accumulation_handler.go`
- [X] T102 [US6] Add ACCUMULATION → ENTER_GRID (timeout 5min) in `backend/internal/agentic/states/accumulation_handler.go`
- [X] T103 [US6] Add ACCUMULATION → EXIT_ALL (false breakout) in `backend/internal/agentic/states/accumulation_handler.go`
- [X] T104 [US6] Wire handler vào state manager in `backend/internal/agentic/state_manager.go`
- [X] T105 [US6] Create unit test `TestWyckoffAccumulationDetection` in `backend/internal/agentic/states/accumulation_handler_test.go`
- [X] T106 [US6] Create unit test `TestBreakoutPrediction` in `backend/internal/agentic/states/accumulation_handler_test.go`
- [X] T107 [US6] Create integration test `TestAccumulationStrategy` in `backend/internal/agentic/integration_test.go`

---

## Phase 9: User Story 7 - DEFENSIVE State (P1) - 1.5 days

**Story**: As a trader, I want graduated exit với risk protection  
**Test Criteria**: `TestDefensiveStrategies` passes - EXIT_HALF với breakeven, EXIT_ALL graduated

### US7 Tasks

- [X] T108 [US7] Implement `DefensiveStateHandler` in `backend/internal/agentic/defensive_state.go`
- [X] T109 [US7] Implement breakeven detection (0.5% threshold) in `backend/internal/agentic/defensive_state.go`
- [X] T110 [US7] Implement EXIT_HALF (1% profit threshold) in `backend/internal/agentic/defensive_state.go`
- [X] T111 [US7] Implement recovery detection → TRENDING in `backend/internal/agentic/defensive_state.go`
- [X] T112 [US7] Implement emergency exit on max loss (-2%) in `backend/internal/agentic/defensive_state.go`
- [X] T113 [US7] Add timeout (30min) → IDLE in `backend/internal/agentic/defensive_state.go`
- [X] T114 [US7] Implement EXIT_ALL (2% profit threshold) in `backend/internal/agentic/defensive_state.go`
- [X] T115 [US7] Implement graduated exit stage tracking in `backend/internal/agentic/defensive_state.go`
- [X] T116 [US7] Wire handler vào AgenticEngine in `backend/internal/agentic/engine.go`
- [X] T117 [US7] Create unit test `TestDefensiveTransitions` in `backend/internal/agentic/defensive_state_test.go`
- [X] T118 [US7] Create unit test `TestDefensiveStateProgression` in `backend/internal/agentic/defensive_state_test.go`
- [X] T119 [US7] Create integration test `TestDefensiveStrategies` in `backend/internal/agentic/defensive_state_test.go`

---

## Phase 10: User Story 8 - Risk States (P1) - 1.5 days

**Story**: As a trader, I want protection states cho extreme conditions  
**Test Criteria**: `TestRiskProtection` passes - OVER_SIZE reduction, RECOVERY adjustment

### US8 Tasks

- [X] T123 [US8] Implement `OverSizeStateHandler` in `backend/internal/agentic/over_size_state.go`
- [X] T124 [US8] Implement position size reduction calculation in `backend/internal/agentic/over_size_state.go`
- [X] T125 [US8] Implement gradual reduction (50% of excess) in `backend/internal/agentic/over_size_state.go`
- [X] T126 [US8] Add OVER_SIZE → TRADING (normalized) in `backend/internal/agentic/over_size_state.go`
- [X] T127 [US8] Add OVER_SIZE → IDLE (emergency >1min) in `backend/internal/agentic/over_size_state.go`
- [X] T128 [US8] Implement `RecoveryStateHandler` in `backend/internal/agentic/recovery_state.go`
- [X] T129 [US8] Implement loss analysis (minor/moderate/severe) in `backend/internal/agentic/recovery_state.go`
- [X] T130 [US8] Implement cooldown period (2min) in `backend/internal/agentic/recovery_state.go`
- [X] T131 [US8] Implement parameter adjustment based on loss in `backend/internal/agentic/recovery_state.go`
- [X] T132 [US8] Add RECOVERY → GRID/IDLE based on severity in `backend/internal/agentic/recovery_state.go`
- [X] T133 [US8] Add market readiness check for re-entry in `backend/internal/agentic/recovery_state.go`
- [X] T134 [US8] Add max recovery time (5min) in `backend/internal/agentic/recovery_state.go`
- [X] T135 [US8] Wire handlers vào AgenticEngine in `backend/internal/agentic/engine.go`
- [X] T136 [US8] Create unit test `TestOverSizeTransitions` in `backend/internal/agentic/over_size_state_test.go`
- [X] T137 [US8] Create unit test `TestRecoveryStrategies` in `backend/internal/agentic/recovery_state_test.go`
- [X] T138 [US8] Create integration test `TestRiskProtection` in `backend/internal/agentic/over_size_state_test.go`

---

## Phase 11: Polish & Smooth Transitions (P2) - 2 days

**Goal**: Smooth transitions, testing, documentation

### Polish Tasks

- [ ] T142 Implement state weight blending (5-10s transition) in `backend/internal/agentic/state_manager.go`
- [ ] T143 Add transition confidence scoring (0-1) in `backend/internal/agentic/state_manager.go`
- [ ] T144 Implement transition history tracking in `backend/internal/agentic/state_manager.go`
- [ ] T145 Add flip-flop detection và prevention in `backend/internal/agentic/state_manager.go`
- [ ] T146 Create comprehensive state machine diagram in docs/
- [ ] T147 Document từng state với entry/exit conditions in docs/state-reference.md
- [ ] T148 Create tuning guide cho score thresholds in docs/tuning-guide.md
- [ ] T149 Add metrics collection (transition latency, success rate) in `backend/internal/agentic/metrics.go`
- [ ] T150 Create dashboard cho state monitoring in `backend/internal/agentic/dashboard.go`
- [ ] T151 Run full integration test suite (all states)
- [ ] T152 Performance benchmark: < 100ms transition latency
- [ ] T153 Stress test: 1000 state transitions without deadlock
- [ ] T154 Backward compatibility test (disable feature)
- [ ] T155 Final code review và cleanup
- [ ] T156 Merge feature branch to main

---

## Summary

| Phase | Story | Tasks | Days | Parallel Opportunities |
|-------|-------|-------|------|----------------------|
| 1 | Setup | 6 | 0.5 | T001-T006 |
| 2 | Foundational | 16 | 2 | T007-T012, T021-T022 |
| 3 | US1: IDLE | 11 | 1 | T023-T030 |
| 4 | US2: WAIT_NEW_RANGE | 14 | 1.5 | T034-T043 |
| 5 | US3: ENTER_GRID | 14 | 1.5 | T048-T057 |
| 6 | US4: TRADING | 14 | 2 | T062-T071 |
| 7 | US5: TRENDING | 17 | 2 | T076-T088 |
| 8 | US6: ACCUMULATION | 15 | 1.5 | T093-T102 |
| 9 | US7: EXIT | 15 | 1.5 | T108-T117 |
| 10 | US8: RISK | 19 | 1.5 | T123-T137 |
| 11 | Polish | 15 | 2 | T142-T150 |
| **Total** | 8 Stories | **156** | **14-15** | |

---

## Dependency Graph

```
Phase 1 (Setup)
    ↓
Phase 2 (Foundational) - ScoreEngine, DecisionEngine
    ↓
Phase 3 (US1: IDLE) ← Depends on Phase 2
    ↓
Phase 4 (US2: WAIT_NEW_RANGE) ← Depends on US1
    ↓
Phase 5 (US3: ENTER_GRID) ← Depends on US2
    ↓
Phase 6 (US4: TRADING) ← Depends on US3
    ↓
Phase 7 (US5: TRENDING) ← Can start after US4 (parallel with US6)
    ↓
Phase 8 (US6: ACCUMULATION) ← Depends on US2
    ↓
Phase 9 (US7: EXIT) ← Depends on US4, US5
    ↓
Phase 10 (US8: RISK) ← Depends on US4, US5
    ↓
Phase 11 (Polish)
```

**Parallel Execution Opportunities**:
- US5 (TRENDING) can start after T067 (trend detection in TRADING is done)
- US6 (ACCUMULATION) can be developed in parallel with US4-US5
- US7 & US8 can be developed in parallel

---

## Suggested MVP Scope (54 tasks, ~7 days)

**MVP = US1 + US2 + US3 + US4 (IDLE → WAIT_NEW_RANGE → ENTER_GRID → TRADING)**

Delivers:
- ✅ IDLE → WAIT_NEW_RANGE transition
- ✅ Range detection và compression
- ✅ ENTER_GRID với signal-triggered entry
- ✅ Asymmetric spreads (FVG-based)
- ✅ TRADING state với continuous blending
- ✅ Trend emergence detection
- ⚠️ No TRENDING mode (just detection)
- ⚠️ Basic EXIT_ALL only
- ⚠️ No ACCUMULATION, RISK states

**Full implementation adds US5-US8 cho complete "mềm dẻo như nước" behavior.**
