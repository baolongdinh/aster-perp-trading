# Implementation Tasks: Continuous Volume Farming with Micro-Profit & Adaptive Risk Control

## Overview

- **Feature**: Continuous Volume Farming Architecture
- **Branch**: feature/continuous-vf-architecture
- **Spec**: @/media/aiozlong/data3/CODE/TOOLS/aster-perp-trading/.specify/features/continuous-vf-architecture/spec.md
- **Plan**: @/media/aiozlong/data3/CODE/TOOLS/aster-perp-trading/.specify/features/continuous-vf-architecture/plan.md
- **Total Tasks**: 50
- **Estimated Duration**: 14-17 days

---

## Dependencies

### Execution Order
```
Phase 1 (Setup) → Phase 2 (Mode Framework) → Phase 3 (Bypass Range Gate - CRITICAL)
       ↓
Phase 4 (Exit Handling) → Phase 5 (Regime Adjustment) → Phase 6 (WebSocket-Only)
       ↓
Phase 7 (Sync Workers) → Phase 8 (Metrics/Polish)
```

### Critical Path
- T002 → T003 → T005 → T006 → T007 (Mode Framework)
- T008 → T009 → T010 → T011 → T012 → T013 → T014 → T015 (Bypass Range Gate - CRITICAL)
- T020 → T021 → T022 → T023 → T024 (Emergency Exit)

### Parallel Opportunities
- T001, T002 can be done in parallel
- T016, T017, T018 (config) parallel with T008-T015
- T033-T039 (Sync Workers) mostly parallel
- T043-T050 (Metrics) parallel after core phases

---

## Phase 1: Setup & Prerequisites (1 day)

**Goal**: Prepare project structure and dependencies

**Acceptance Criteria**:
- [ ] All new file paths created
- [ ] Config structures defined
- [ ] No compilation errors

### Tasks

- [ ] T001 [P] Create directory structure for new components
  - Create `internal/farming/tradingmode/` directory
  - Create `internal/farming/sync/` directory
  - Create `internal/farming/metrics/` directory

- [ ] T002 [P] Add mode configuration to config structs
  - Add `TradingModesConfig` struct to `internal/config/volume_farm_config.go`
  - Add mode parameters: micro_mode, standard_mode, trend_adapted_mode, cooldown_mode

- [ ] T003 Add mode thresholds configuration
  - Add `ModeTransitions` struct with ADX thresholds (sideways: 20, trending: 25)
  - Add volatility spike multiplier (3.0) and breakout confirmations (2)

---

## Phase 2: Core Trading Mode Framework (2-3 days)

**Story**: [US1] As a trader, I want the bot to automatically switch trading modes based on market conditions so that it always trades optimally

**Acceptance Criteria**:
- [ ] ModeManager can evaluate and switch modes
- [ ] Mode transitions are thread-safe
- [ ] Mode history is tracked

### Tasks

- [ ] T004 [P] [US1] Create TradingMode enum
  - Create `internal/farming/tradingmode/trading_mode.go`
  - Define enum: Unknown, Micro, Standard, TrendAdapted, Cooldown
  - Implement String() method

- [ ] T005 [P] [US1] Create DynamicParameters struct
  - Create `internal/farming/tradingmode/dynamic_params.go`
  - Define fields: SizeMultiplier, SpreadMultiplier, LevelCount, UseBBBands, etc.
  - Implement GetParametersForMode(TradingMode) function

- [ ] T006 [US1] Create ModeManager struct
  - Create `internal/farming/tradingmode/mode_manager.go`
  - Implement ModeManager with currentMode, modeSince, config fields
  - Add mutex for thread-safety

- [ ] T007 [US1] Implement mode evaluation logic
  - Implement EvaluateMode(rangeState, adx, isBreakout, isTrending) TradingMode
  - Logic: ADX>30→TrendAdapted, BB active + ADX<20→Standard, else Micro
  - Implement ShouldTransition() with MinModeDuration check (anti-oscillation)

- [ ] T008 [US1] Implement mode transition tracking
  - Add ModeTransition struct (From, To, Timestamp, Reason)
  - Add modeHistory slice to ModeManager
  - Implement GetModeHistory() method

- [ ] T009 [US1] Add mode manager to VolumeFarmEngine
  - Add ModeManager field to `internal/farming/volume_farm_engine.go`
  - Initialize in NewVolumeFarmEngine()
  - Expose GetModeManager() method

---

## Phase 3: Bypass Range Gate for MICRO Mode (3-4 days) ⭐ CRITICAL

**Story**: [US2] As a trader, I want orders placed immediately on startup using ATR bands, so I don't miss volume during warm-up

**Acceptance Criteria**:
- [ ] Orders placed within 60s of startup
- [ ] Grid uses ATR bands when BB not ready (40% size, 2-3 levels)
- [ ] Seamless transition to STANDARD when BB active
- [ ] No duplicate orders during transition

### Tasks

- [ ] T010 [P] [US2] Expose ATR calculation in RangeDetector
  - Modify `internal/farming/adaptive_grid/range_detector.go`
  - Add GetATR() method returning current ATR value
  - Cache ATR calculation to avoid recomputation

- [ ] T011 [US2] Add ATR bands calculation
  - Add GetATRBands(symbol string) (upper, lower float64) to RangeDetector
  - Calculate: upper = close + ATR*multiplier, lower = close - ATR*multiplier
  - Default multiplier: 1.5

- [ ] T012 [US2] Modify CanPlaceOrder for MICRO mode
  - Modify `internal/farming/adaptive_grid/manager.go`
  - Change CanPlaceOrder() to accept TradingMode parameter
  - Allow orders if mode==Micro AND ATR bands valid (even if RangeState!=Active)
  - Add GetTradingMode() method to access current mode

- [ ] T013 [US2] Modify grid building for dynamic parameters
  - Modify `internal/farming/grid_manager.go` or grid builder
  - Accept DynamicParameters in BuildGridOrders()
  - Adjust level count: 3 for Micro, 5 for Standard, 2 for TrendAdapted
  - Adjust spread: 2x for Micro, 1x for Standard, 2x for TrendAdapted

- [ ] T014 [US2] Integrate ModeManager with GridManager
  - Modify `internal/farming/grid_manager.go`
  - Add ModeManager reference to GridManager
  - Call modeManager.EvaluateMode() before placement check
  - Pass mode to CanPlaceForSymbol() and buildGridOrders()

- [ ] T015 [US2] Modify canPlaceForSymbol for MICRO mode bypass
  - Modify `internal/farming/grid_manager.go` canPlaceForSymbol()
  - Add logic: if mode==Micro && ATR bands exist → allow placement
  - Keep existing checks: position limits, exposure limits
  - Log mode and parameters at placement time

- [ ] T016 [P] [US2] Add MICRO mode configuration
  - Add to config: size_multiplier=0.4, level_count=3, spread_multiplier=2.0
  - Add min_mode_duration_seconds=30 (anti-oscillation)

- [ ] T017 [P] [US2] Add STANDARD mode configuration
  - Add to config: size_multiplier=1.0, level_count=5, spread_multiplier=1.0
  - Add min_mode_duration_seconds=60

- [ ] T018 [P] [US2] Add TREND_ADAPTED mode configuration
  - Add to config: size_multiplier=0.3, level_count=2, spread_multiplier=2.0
  - Add trend_bias_enabled=true, trend_bias_ratio=0.6

---

## Phase 4: Emergency Exit & Breakout Handling (3 days) ⭐ CRITICAL

**Story**: [US3] As a trader, I want the bot to exit all positions within 5 seconds when price breaks range, to minimize losses

**Acceptance Criteria**:
- [ ] Breakout detected in <1s from WebSocket message
- [ ] All orders cancelled in <1s (async batch)
- [ ] All positions closed in <5s (market orders)
- [ ] Cooldown entered automatically after exit

### Tasks

- [ ] T019 [US3] Create ExitExecutor struct
  - Create `internal/farming/exit_executor.go`
  - Define struct with futuresClient, wsClient, timeout (5s)
  - Define ExitSequence struct for tracking timing

- [ ] T020 [US3] Implement async order cancellation
  - Implement cancelAllOrdersAsync(ctx, symbol) method
  - Use goroutine for parallel cancellation
  - Track cancelled count and duration

- [ ] T021 [US3] Implement position close logic
  - Implement closePositionsMarket(ctx, symbol) method
  - Get positions from WebSocket cache
  - Place market orders to close each position
  - Verify close via position stream

- [ ] T022 [US3] Implement ExecuteFastExit orchestration
  - Implement ExecuteFastExit(ctx, symbol) *ExitSequence
  - Timeline: T+0 detect, T+100ms cancel, T+800ms close, T+5s verify
  - Log all timing metrics
  - Return ExitSequence with duration and error

- [ ] T023 [US3] Integrate exit executor with GridManager
  - Add EmergencyExit(symbol string) method to GridManager
  - Call exitExecutor.ExecuteFastExit()
  - Set cooldown mode after exit
  - Block new orders during cooldown

- [ ] T024 [US3] Enhance breakout detection in RangeDetector
  - Modify `internal/farming/adaptive_grid/range_detector.go`
  - Add IsBreakoutConfirmed() - requires 2 consecutive closes outside BB
  - Add VolatilitySpikeDetected() - ATR > 3x average
  - Trigger exit immediately on confirmed breakout

- [ ] T025 [US3] Add cooldown mode management
  - Add EnterCooldown(duration time.Duration) to ModeManager
  - Add IsInCooldown() check
  - Auto-transition to Micro after cooldown expires

- [ ] T026 [P] [US3] Add exit metrics tracking
  - Track exit count, avg exit duration, last exit timestamp
  - Alert if exit duration > 5s

---

## Phase 5: Regime-Based Parameter Adjustment (2 days)

**Story**: [US4] As a trader, I want the bot to reduce size/spread when ADX shows trending market, to reduce risk

**Acceptance Criteria**:
- [ ] ADX evaluated every 30s
- [ ] Mode switches to TrendAdapted when ADX > 25
- [ ] Size reduced to 30%, spread widened to 2x
- [ ] Trend bias adds more orders on trend side

### Tasks

- [ ] T027 [US4] Expose ADX from Agentic RegimeDetector
  - Modify `internal/agentic/regime_detector.go`
  - Add GetADX() method returning current ADX value
  - Cache ADX from last calculation

- [ ] T028 [US4] Add ADX-based mode evaluation
  - Modify ModeManager.EvaluateMode()
  - Add logic: ADX > 25 → TrendAdapted, ADX < 20 → Standard/Micro
  - Add hysteresis: require 3 consecutive readings above threshold

- [ ] T029 [US4] Implement trend bias in grid building
  - Modify grid builder for TrendAdapted mode
  - If TrendBiasEnabled: place 60% of orders on trend side
  - Calculate trend direction from price movement

- [ ] T030 [US4] Add regime update loop
  - Add 30s ticker in VolumeFarmEngine
  - Call regimeDetector.Update() and modeManager.EvaluateMode()
  - Log mode changes with ADX value and reason

- [ ] T031 [US4] Add parameter smoothing (anti-oscillation)
  - Add MinModeDuration check before transition
  - Default: 30s for Micro/TrendAdapted, 60s for Standard
  - Log when transition blocked by duration

---

## Phase 6: WebSocket-Only Data Flow (2 days)

**Story**: [US5] As a trader, I want the bot to use only WebSocket data (no REST API) during trading, to avoid rate limits

**Acceptance Criteria**:
- [ ] No REST API calls for order/position/balance state
- [ ] WebSocket cache with 1-10s TTL
- [ ] Reconnect with exponential backoff
- [ ] Automatic cooldown on 30s+ disconnect

### Tasks

- [ ] T032 [US5] Add WebSocket cache methods
  - Modify `internal/client/websocket.go`
  - Add GetCachedOrders(symbol) []Order method
  - Add GetCachedPositions() map[string]Position method
  - Add GetCachedBalance() Balance method
  - Implement TTL eviction (1s for orders, 5s for positions, 10s for balance)

- [ ] T033 [US5] Remove REST API calls from GridManager
  - Modify `internal/farming/grid_manager.go`
  - Replace GetOpenOrders REST call with WebSocket cache
  - Replace GetPositions REST call with WebSocket cache
  - Keep REST only for initial load (not trading loop)

- [ ] T034 [US5] Add WebSocket health monitor
  - Create `internal/client/websocket_health.go`
  - Track lastPong, latency, isConnected
  - Ping/pong every 10s
  - Mark stale if msg delay > 3s

- [ ] T035 [US5] Implement reconnect with backoff
  - Add reconnect logic: 1s, 2s, 4s, 8s, max 30s
  - Buffer messages during reconnect (max 100)
  - Log reconnect attempts

- [ ] T036 [US5] Add disconnect fallback logic
  - 30s disconnect → trigger COOLDOWN mode
  - 60s disconnect → execute EmergencyExit for all symbols
  - REST API only for emergency position check

---

## Phase 7: State Sync Workers (3 days)

**Story**: [US6] As a trader, I want internal state synced with exchange every 5 seconds, so no state mismatches cause errors

**Acceptance Criteria**:
- [ ] 3 workers run every 5s (Order, Position, Balance)
- [ ] Mismatch detected within 10s
- [ ] Exchange state trusted as ground truth
- [ ] Critical mismatches trigger alerts

### Tasks

- [ ] T037 [P] [US6] Create OrderSyncWorker
  - Create `internal/farming/sync/order_sync_worker.go`
  - Run every 5s ticker
  - Get orders from WebSocket cache
  - Compare with internal state
  - Handle: missing orders, unknown orders, status mismatch

- [ ] T038 [P] [US6] Create PositionSyncWorker
  - Create `internal/farming/sync/position_sync_worker.go`
  - Sync position size, side, PnL
  - Alert on side mismatch (critical)
  - Sync from exchange immediately on mismatch

- [ ] T039 [P] [US6] Create BalanceSyncWorker
  - Create `internal/farming/sync/balance_sync_worker.go`
  - Check available margin vs required
  - Alert on low margin
  - Update internal balance for risk calc

- [ ] T040 [US6] Create SyncManager coordinator
  - Create `internal/farming/sync/manager.go`
  - Coordinate all sync workers
  - Start/stop lifecycle management
  - Expose sync status and metrics

- [ ] T041 [US6] Implement mismatch reconciliation
  - Define Mismatch struct (Type, OrderID, InternalVal, ExchangeVal, Severity)
  - Rule: exchange state is ground truth
  - Handle missed fill events (internal shows filled, exchange shows open)
  - Log all mismatches with severity

- [ ] T042 [US6] Add sync alerting
  - Alert if mismatch duration > 10s
  - Critical alert for side mismatch
  - Warning alert for size mismatch > 1%
  - Include symbol and order details in alerts

---

## Phase 8: Metrics & Observability (2 days)

**Story**: [US7] As a trader, I want metrics on fill rate, exit timing, and mode duration, so I can optimize performance

**Acceptance Criteria**:
- [ ] Fill rate tracked per hour per symbol
- [ ] Exit duration < 5s metric and alert
- [ ] Prometheus metrics exposed
- [ ] Mode transitions logged with context

### Tasks

- [ ] T043 [P] [US7] Create TradingMetrics collector
  - Create `internal/farming/metrics/trading_metrics.go`
  - Track: FillsLastHour, AvgProfitPerFill, TotalVolume24h
  - Track: CurrentMode, ModeSince, LastExitDuration
  - Thread-safe metric updates

- [ ] T044 [P] [US7] Implement fill rate tracking
  - Hook into order fill handler
  - Increment counter on each fill
  - Calculate fills per hour (sliding window)

- [ ] T045 [P] [US7] Implement exit duration tracking
  - Hook into ExitExecutor
  - Record start time on trigger, end time on complete
  - Alert if duration > 5s

- [ ] T046 [P] [US7] Implement mode duration tracking
  - Record mode entry timestamp
  - Calculate duration on mode exit
  - Track total time per mode (daily)

- [ ] T047 [US7] Add Prometheus metrics
  - Create Prometheus gauges/counters:
    - `trading_mode_current{symbol}` (0-4 enum)
    - `fills_per_hour{symbol}` (counter)
    - `avg_profit_per_fill{symbol}` (gauge)
    - `exit_duration_ms{symbol}` (histogram)
    - `sync_mismatches_total{symbol}` (counter)
  - Expose on /metrics endpoint

- [ ] T048 [US7] Enhance logging for mode transitions
  - Log mode transition with: from, to, reason, adx, rangeState
  - Log parameters: sizeMult, spreadMult, levelCount
  - Structured JSON logs for parsing

- [ ] T049 [US7] Add performance dashboards
  - Create `dashboards/trading_performance.json`
  - Grafana panels: fill rate, mode distribution, exit duration
  - Alert panels: exit duration > 5s, sync mismatches > 0

- [ ] T050 [US7] Create health check endpoint
  - Add `/health` endpoint to API
  - Return: mode, last fill time, sync status, ws connected
  - Use for monitoring uptime

---

## Implementation Strategy

### MVP (Minimum Viable Product)
Complete Phase 1-3 only:
- T001-T018 (Setup + Mode Framework + Bypass Range Gate)

**Result**: Bot trades immediately on startup with MICRO mode, transitions to STANDARD when BB ready. Basic risk management from existing code.

### Incremental Delivery
1. **Week 1**: Phase 1-3 (MVP) - Bot trading continuously
2. **Week 2**: Phase 4-5 - Emergency exit + regime adjustment
3. **Week 3**: Phase 6-8 - WebSocket-only + sync + metrics

### Risk Mitigation
- Keep existing RangeDetector logic intact (add to, don't replace)
- Add feature flags for MICRO mode (can disable if issues)
- Test in paper trading for 48h before production
- Monitor exit duration metric closely

---

## Testing Strategy

### Unit Tests
- T005, T006, T007: Mode evaluation logic
- T020, T021, T022: Exit executor timing
- T037, T038, T039: Sync worker reconciliation

### Integration Tests
- T014-T015: Full MICRO → STANDARD flow
- T023: Breakout → Exit → Cooldown flow
- T035-T036: WebSocket disconnect → fallback

### Live Testing (Paper Trading)
- 1 symbol, 24 hours
- Verify >30 fills/hour
- Verify <5s exit on breakout simulation
- Monitor sync mismatch rate < 0.1%

---

## Summary

| Phase | Tasks | Duration | Priority |
|-------|-------|----------|----------|
| 1: Setup | T001-T003 | 1 day | Medium |
| 2: Mode Framework | T004-T009 | 2-3 days | High |
| 3: Bypass Range Gate | T010-T018 | 3-4 days | ⭐ CRITICAL |
| 4: Exit Handling | T019-T026 | 3 days | ⭐ CRITICAL |
| 5: Regime Adjustment | T027-T031 | 2 days | Medium |
| 6: WebSocket-Only | T032-T036 | 2 days | High |
| 7: Sync Workers | T037-T042 | 3 days | High |
| 8: Metrics | T043-T050 | 2 days | Low |

**Total**: 50 tasks, 14-17 days
**MVP**: 18 tasks, 6-8 days (Phases 1-3)

---

Generated: 2026-04-15
Ready for implementation
