# Agentic Trading Bot Core Flow Fixes

## Status: ✅ ALL 15 TASKS COMPLETED

**Completion Date**: April 12, 2026

| Phase | Tasks | Status |
|-------|-------|--------|
| Phase 1 (Critical) | T001, T002 | ✅ Complete |
| Phase 2 (High) | T003, T004, T005, T006 | ✅ Complete |
| Phase 3 (Medium) | T007, T008, T009, T010, T011, T012 | ✅ Complete |
| Phase 4 (Low) | T013, T014, T015 | ✅ Complete |

## Overview

Comprehensive fixes for the agentic trading bot's core flow to align with the desired state machine architecture, entry/exit conditions, re-grid logic, micro grid parameters, and volume maximization requirements.

## Dependencies

```
T001, T002 (Critical) → T003, T004, T005, T006 (High) → T007-T012 (Medium) → T013-T015 (Low)
```

## Phase 1: Critical Safety Fixes (MUST complete first)

### T001: Add State Machine Gate to Placement Enqueue
- **File**: `backend/internal/farming/grid_manager.go`
- **Location**: `shouldSchedulePlacement()` function (~line 520)
- **Action**: Add `GetRangeState(symbol) == RangeStateActive` check before allowing enqueue
- **Current**: Enqueue triggers on 0.01% price delta
- **Expected**: Enqueue only when `RangeStateActive` AND price delta
- **Safety Impact**: PREVENTS trading during breakout/stabilizing states

### T002: Wire Dynamic Leverage to Actual Leverage Setting
- **File**: `backend/internal/farming/volume_farm_engine.go`
- **Location**: `setLeverageForSymbols()` function (~line 480)
- **Action**: 
  1. Call `adaptiveGridManager.GetOptimalLeverage()` before setting leverage
  2. Replace `targetLeverage := int(maxLeverage * 0.8)` with dynamic calculation
  3. Update leverage when range changes (tight BB → higher leverage)
- **Current**: Static 80% of max leverage
- **Expected**: `leverage = min(maxLeverage, baseLeverage / bbWidthNormalized)`
- **Profit Impact**: Maximizes leverage efficiency based on market conditions

## Phase 2: High Priority Architecture Fixes

### T003: Fix Micro Grid Precedence
- **File**: `backend/internal/farming/grid_manager.go`
- **Location**: `placeGridOrders()` function (~line 843)
- **Action**: Swap precedence - check micro grid FIRST, BB bands only if micro disabled
- **Current**: BB bands take precedence, micro is fallback
- **Expected**: Micro grid is primary geometry, BB/ADX only gates permission

### T004: Implement GridStateMachine Struct
- **File**: `backend/internal/farming/adaptive_grid/state_machine.go` (NEW)
- **Action**: Create state machine with 5 states:
  ```go
  type GridState int
  const (
      GridStateIdle GridState = iota
      GridStateEnterGrid
      GridStateTrading
      GridStateExitAll
      GridStateWaitNewRange
  )
  ```
- **States**: IDLE → ENTER_GRID → TRADING → EXIT_ALL → WAIT_NEW_RANGE

### T005: Add Event-Based Transitions
- **File**: `backend/internal/farming/adaptive_grid/state_machine.go`
- **Action**: Define transition events:
  ```go
  type GridEvent int
  const (
      EventRangeConfirmed GridEvent = iota  // WAIT_NEW_RANGE → ENTER_GRID
      EventEntryPlaced                     // ENTER_GRID → TRADING
      EventTrendExit                       // TRADING → EXIT_ALL
      EventNewRangeReady                   // EXIT_ALL → WAIT_NEW_RANGE
  )
  ```
- **Integrate**: Connect events to `RangeDetector` state changes and exit triggers

### T006: Integrate State Machine with GridManager
- **File**: `backend/internal/farming/grid_manager.go`
- **Action**: 
  1. Add `GridStateMachine` field to `GridManager`
  2. Modify `shouldSchedulePlacement()` to use state machine instead of price delta
  3. Trigger enqueue on `EventRangeConfirmed` transition only
  4. Remove/enhance 0.01% price delta trigger
- **Breaking Change**: Placement now strictly gated by state machine

## Phase 3: Medium Priority Implementation Tasks

### T007: Implement calculateDynamicLeverage()
- **File**: `backend/internal/farming/adaptive_grid/risk_sizing.go`
- **Action**: Create function:
  ```go
  func calculateDynamicLeverage(bbWidth float64, maxLeverage float64) float64 {
      // Inverse proportion: tight range = higher leverage
      baseLeverage := maxLeverage * 0.5
      if bbWidth < 0.005 {  // Tight range
          return maxLeverage
      }
      normalized := math.Min(bbWidth/0.02, 1.0)  // Cap at 2% width
      return math.Max(baseLeverage, maxLeverage*(1.0-normalized))
  }
  ```

### T008: Implement isReadyForRegrid()
- **File**: `backend/internal/farming/adaptive_grid/manager.go`
- **Action**: Create function with strict conditions:
  ```go
  func (a *AdaptiveGridManager) isReadyForRegrid(symbol string) bool {
      // 1. Zero open orders AND zero position
      // 2. Range shift ≥ 0.5% from last accepted range
      // 3. BB width contraction (< 1.5x average)
      // 4. ADX < 20 for ≥ 3 consecutive candles
      // 5. Current state is WAIT_NEW_RANGE
  }
  ```

### T009: Create Real-Time Exit Goroutine
- **File**: `backend/internal/farming/adaptive_grid/manager.go`
- **Action**: 
  1. Create dedicated goroutine in `Initialize()`
  2. Monitor ADX/BB every 100ms (not WebSocket cadence)
  3. Call `handleBreakout()` on exit conditions:
     - ADX > 25
     - BB expand > 1.5x average
     - Price outside BB for ≥ 2 candles
  4. Thread-safe with existing mutex

### T010: Update Consecutive Losses Configuration
- **File**: 
  - `backend/internal/farming/adaptive_grid/manager.go` (~line 2281)
  - `backend/internal/farming/adaptive_grid/risk_sizing.go` (~line 293)
- **Action**: Change `MaxConsecutiveLosses` default from 5 to 4
- **Spec**: `> 3` losses = exit, so threshold should be 4

### T011: Enable Multi-Layer Liquidation Protection
- **File**: `backend/internal/farming/adaptive_grid/manager.go`
- **Action**:
  1. Set `MultiLayerLiquidationConfig.Enabled = true` in initialization
  2. Wire to position monitor in `monitorPositions()`
  3. Implement 4-tier actions:
     - Tier 1 (50%): Reduce position 20%
     - Tier 2 (30%): Reduce position 50%
     - Tier 3 (15%): Emergency close all
     - Tier 4 (10%): Hedge + close

### T012: Unify BB Period Configuration
- **File**: `config/agentic-vf-config.yaml` (or equivalent)
- **Action**: Change agentic layer BB period from 20 to 10
- **Current Split**: Agentic uses 20, execution uses 10
- **Expected**: Both layers use 10 for fast detection

## Phase 4: Low Priority Polish

### T013: Thread-Safe State Machine Access
- **File**: `backend/internal/farming/adaptive_grid/state_machine.go`
- **Action**: Add `sync.RWMutex` to `GridStateMachine` for thread-safe state reads/writes

### T014: Idempotent Exit Actions
- **File**: `backend/internal/farming/adaptive_grid/manager.go`
- **Action**: Add duplicate call protection in `handleBreakout()` and `handleStrongTrend()`
- **Pattern**: Check `exitInProgress` flag before executing

### T015: JSONL Logging for State Transitions
- **File**: `backend/internal/farming/adaptive_grid/state_machine.go`
- **Action**: Log all state transitions with JSONL format:
  ```go
  logger.Info("state_transition", 
      zap.String("symbol", symbol),
      zap.String("from", oldState.String()),
      zap.String("to", newState.String()),
      zap.String("event", event.String()),
      zap.Time("timestamp", time.Now()),
  )
  ```

## Execution Order

**Week 1 (Critical)**:
1. T001 - State machine gate (safety)
2. T002 - Dynamic leverage wiring (profit)

**Week 2 (Architecture)**:
3. T004 - State machine struct
4. T005 - Event transitions
5. T003 - Micro grid precedence
6. T006 - State machine integration

**Week 3 (Features)**:
7. T007 - Dynamic leverage function
8. T008 - Regrid ready check
9. T009 - Real-time exit goroutine
10. T010 - Consecutive losses config

**Week 4 (Risk & Polish)**:
11. T011 - Liquidation tiers
12. T012 - BB period unification
13. T013-T015 - Polish tasks

## Success Criteria

- ✅ `shouldSchedulePlacement()` only returns true on `RangeStateActive`
- ✅ Leverage changes dynamically with BB width
- ✅ Micro grid takes precedence over BB bands
- ✅ State machine enforces IDLE→ENTER_GRID→TRADING→EXIT_ALL→WAIT_NEW_RANGE flow
- ✅ Real-time exit goroutine runs independently of WebSocket flow
- ✅ 4-tier liquidation protection active
- ✅ BB period 10 used consistently across all layers

---

# TradingDecisionWorker Integration

## Status: 🔄 IN PROGRESS

**Created**: April 15, 2026

## Overview

Integrate TradingDecisionWorker as the single source of truth for trading decisions, centralizing all decision logic while keeping existing checks.

## Phase 1: Integration Tasks

### T016: Create TradingDecisionWorker Instance in VolumeFarmEngine
- **File**: `backend/internal/farming/volume_farm_engine.go`
- **Location**: `NewVolumeFarmEngine()` function (~line 150)
- **Action**:
  1. Add field `tradingDecisionWorker *TradingDecisionWorker` to VolumeFarmEngine struct
  2. Create instance after adaptiveGridManager initialization:
     ```go
     engine.tradingDecisionWorker = farming.NewTradingDecisionWorker(adaptiveGridManager, logger)
     ```
  3. Set symbols: `engine.tradingDecisionWorker.SetSymbols([]string{"BTCUSD1", "ETHUSD1", "SOLUSD1"})`

### T017: Set TradingDecisionWorker to GridManager
- **File**: `backend/internal/farming/volume_farm_engine.go`
- **Location**: After gridManager initialization (~line 200)
- **Action**:
  1. Call `engine.gridManager.SetTradingDecisionWorker(engine.tradingDecisionWorker)`
  2. Log confirmation: "TradingDecisionWorker set to GridManager"

### T018: Start TradingDecisionWorker
- **File**: `backend/internal/farming/volume_farm_engine.go`
- **Location**: `Start()` method (~line 300)
- **Action**:
  1. Add goroutine: `go engine.tradingDecisionWorker.Run(ctx)`
  2. Ensure it starts after adaptiveGridManager starts
  3. Add error handling for worker startup

### T019: Stop TradingDecisionWorker on Shutdown
- **File**: `backend/internal/farming/volume_farm_engine.go`
- **Location**: `Stop()` method (~line 800)
- **Action**:
  1. Call `engine.tradingDecisionWorker.Stop()` before stopping other components
  2. Add logging for shutdown confirmation
  3. Ensure graceful shutdown with timeout

### T020: Add CircuitBreaker Event Handlers
- **File**: `backend/internal/farming/volume_farm_engine.go`
- **Location**: CircuitBreaker callback setup (~line 225)
- **Action**:
  1. In SetOnTripCallback: Call `engine.tradingDecisionWorker.ForceUpdate(symbol, false, "circuit breaker trip: "+reason)`
  2. In SetOnResetCallback: Call `engine.tradingDecisionWorker.ForceUpdate(symbol, true, "circuit breaker reset")`
  3. Ensure ForceUpdate happens before exit/rebuild actions

### T021: Add RangeDetector Event Handlers
- **File**: `backend/internal/farming/adaptive_grid/manager.go`
- **Location**: Range breakout handling (~line 3070)
- **Action**:
  1. Add field `tradingDecisionWorker *TradingDecisionWorker` to AdaptiveGridManager
  2. Add setter method: `SetTradingDecisionWorker(worker *TradingDecisionWorker)`
  3. In `handleBreakout()`: Call `tradingDecisionWorker.ForceUpdate(symbol, false, "range breakout")`
  4. In ready-to-reenter: Call `tradingDecisionWorker.evaluateSymbol(symbol)` for immediate update

### T022: Create Trading State API Endpoint
- **File**: `backend/internal/api/trading_state_api.go` (NEW)
- **Action**:
  1. Create handler: `func GetTradingState(w http.ResponseWriter, r *http.Request)`
  2. Query TradingDecisionWorker: `worker.GetAllTradingStates()`
  3. Return JSON response with all symbol states
  4. Add route: `GET /api/trading-state`
  5. Add query param: `?symbol=BTCUSD1` for single symbol

### T023: Wire API to HTTP Server
- **File**: `backend/cmd/agentic/main.go` or equivalent
- **Location**: HTTP server setup
- **Action**:
  1. Import trading state API package
  2. Add route: `router.HandleFunc("/api/trading-state", api.GetTradingState)`
  3. Ensure it's registered before server starts

## Phase 2: Testing & Validation

### T024: Test TradingDecisionWorker Evaluation
- **File**: `backend/internal/farming/trading_decision_worker_test.go` (NEW)
- **Action**:
  1. Create unit test for `evaluateSymbol()`
  2. Mock AdaptiveGridManager.CanPlaceOrder() responses
  3. Verify state updates correctly
  4. Test ForceUpdate() functionality

### T025: Test Integration with GridManager
- **File**: `backend/internal/farming/grid_manager_test.go`
- **Action**:
  1. Test `canPlaceForSymbol()` with TradingDecisionWorker set
  2. Verify it reads from worker state
  3. Test fallback when worker state not available
  4. Verify logging outputs

### T026: Test API Endpoint
- **File**: `backend/internal/api/trading_state_api_test.go` (NEW)
- **Action**:
  1. Test GET /api/trading-state returns all states
  2. Test GET /api/trading-state?symbol=BTCUSD1 returns single state
  3. Verify JSON format matches expected schema
  4. Test error handling for invalid symbol

### T027: Manual Integration Test
- **File**: Test plan document
- **Action**:
  1. Start bot with TradingDecisionWorker integrated
  2. Monitor logs for "Trading decision updated" messages
  3. Trigger CircuitBreaker trip - verify ForceUpdate called
  4. Query /api/trading-state - verify state reflects decision
  5. Verify canPlaceForSymbol respects worker decision

## Phase 3: Monitoring & Polish

### T028: Add Metrics for Trading Decisions
- **File**: `backend/internal/farming/trading_decision_worker.go`
- **Action**:
  1. Add counter for total evaluations
  2. Add counter for state changes
  3. Add gauge for current canTrade count
  4. Export metrics for Prometheus scraping

### T029: Add Health Check
- **File**: `backend/internal/farming/volume_farm_engine.go`
- **Location**: Health check endpoint
- **Action**:
  1. Add TradingDecisionWorker status to health check
  2. Report last evaluation time
  3. Report number of symbols being evaluated
  4. Alert if worker not running

### T030: Documentation Update
- **File**: `backend/REFACTOR_TRADING_DECISION.md`
- **Action**:
  1. Update with actual implementation details
  2. Add API endpoint documentation
  3. Add troubleshooting section
  4. Add example API responses

## Execution Order

**Immediate (Critical)**:
1. T016 - Create instance
2. T017 - Set to GridManager
3. T018 - Start worker
4. T019 - Stop worker

**Short-term (High)**:
5. T020 - CircuitBreaker handlers
6. T021 - RangeDetector handlers
7. T022 - API endpoint
8. T023 - Wire API

**Medium-term**:
9. T024 - T026 - Tests
10. T027 - Manual test

**Long-term**:
11. T028 - T030 - Polish

## Success Criteria

- ✅ TradingDecisionWorker starts and evaluates symbols every 5s
- ✅ GridManager.canPlaceForSymbol reads from worker state
- ✅ CircuitBreaker trip triggers ForceUpdate
- ✅ Range breakout triggers ForceUpdate
- ✅ API endpoint returns trading state
- ✅ All tests pass
- ✅ Manual integration test successful
- ✅ Metrics exported correctly
- ✅ Health check includes worker status
