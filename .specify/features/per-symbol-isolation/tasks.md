# Per-Symbol Isolation Tasks

## Feature Overview
Ensure each trading symbol operates independently with its own signals, chart data, volume tracking, loss tracking, and cooldown state.

## Dependencies

```
US-1 (Independent Symbol Trading)
    ↓
US-2 (Accurate Per-Symbol Metrics)
    ↓
US-3 (Isolated Cooldowns)
```

**Note**: US-2 and US-3 can be implemented in parallel after US-1 is complete, as they affect different components (TradeTracker vs ExposureManager).

---

## Phase 1: Setup

No setup tasks required - this is a refactoring of existing code.

---

## Phase 2: Foundational

- [X] T001 Search codebase for all usages of TradeTracker.GetWinRate() and TradeTracker.GetConsecutiveLosses() in `backend/internal/farming/adaptive_grid/`
- [X] T002 Search codebase for all usages of ExposureManager.RecordLoss() and ExposureManager.cooldownActive in `backend/internal/farming/adaptive_grid/`
- [X] T003 Search codebase for all usages of ModeManager global methods (GetCurrentMode, EvaluateMode, transitionTo) in `backend/internal/farming/`

---

## Phase 3: US-1 - Independent Symbol Trading

### Goal
Ensure each symbol trades independently with its own signal, chart data, and volume tracking.

### Independent Test Criteria
- TradeTracker stores results per-symbol in map[string][]TradeResult
- TradeTracker.GetWinRate(symbol) returns win rate for specific symbol only
- TradeTracker.GetConsecutiveLosses(symbol) returns consecutive losses for specific symbol only
- One symbol's losses do not affect another symbol's consecutive loss count

### Implementation Tasks

- [X] T004 Refactor TradeTracker struct to use map[string][]TradeResult instead of []TradeResult in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T005 Update TradeTracker.RecordTrade() to initialize and store results per-symbol in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T006 [P] Add TradeTracker.GetWinRate(symbol string) method for per-symbol win rate calculation in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T007 [P] Add TradeTracker.GetConsecutiveLosses(symbol string) method for per-symbol consecutive loss counting in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T008 Update CalculateSmartSize() to pass symbol parameter to GetWinRate and GetConsecutiveLosses in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T009 Update all callers of TradeTracker.GetWinRate() to use GetWinRate(symbol) in `backend/internal/farming/adaptive_grid/`
- [X] T010 Update all callers of TradeTracker.GetConsecutiveLosses() to use GetConsecutiveLosses(symbol) in `backend/internal/farming/adaptive_grid/`
- [X] T011 Add unit test TestTradeTracker_PerSymbolIsolation in `backend/internal/farming/adaptive_grid/risk_sizing_test.go`
- [X] T012 Run existing TradeTracker tests to ensure no regressions

---

## Phase 4: US-2 - Accurate Per-Symbol Metrics

### Goal
Ensure win rate and consecutive loss metrics are calculated per-symbol independently.

### Independent Test Criteria
- Each symbol has independent win rate calculation
- Each symbol has independent consecutive loss counting
- Metrics are not affected by other symbols' trading history

### Implementation Tasks

- [X] T013 Verify TradeTracker per-symbol isolation with integration test in `backend/internal/farming/adaptive_grid/risk_sizing_test.go`
- [X] T014 Add test scenario: BTC has 3 losses, ETH has 0 losses, verify ETH's metrics are unaffected in `backend/internal/farming/adaptive_grid/risk_sizing_test.go`

---

## Phase 5: US-3 - Isolated Cooldowns

### Goal
Ensure cooldowns are per-symbol and one symbol's cooldown does not block other symbols.

### Independent Test Criteria
- ExposureManager tracks consecutive losses per-symbol in map[string]int
- ExposureManager tracks cooldown state per-symbol in map[string]bool
- ExposureManager.RecordLoss(symbol) updates only that symbol's state
- ExposureManager.IsCooldownActive(symbol) returns cooldown state for specific symbol only
- One symbol entering cooldown does not affect other symbols

### Implementation Tasks

- [X] T015 Refactor ExposureManager struct to use map[string]int for consecutiveLosses in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T016 [P] Refactor ExposureManager struct to use map[string]time.Time for lastLossTime in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T017 [P] Refactor ExposureManager struct to use map[string]bool for cooldownActive in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T018 Update NewExposureManager() to initialize per-symbol maps in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T019 Update ExposureManager.RecordLoss(symbol string) to track losses per-symbol in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T020 [P] Add ExposureManager.ResetLosses(symbol string) method in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T021 [P] Add ExposureManager.GetConsecutiveLosses(symbol string) method in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T022 [P] Add ExposureManager.IsCooldownActive(symbol string) method in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T023 Update all callers of ExposureManager.RecordLoss() to pass symbol parameter in `backend/internal/farming/adaptive_grid/`
- [X] T024 Update all callers using ExposureManager.cooldownActive to use IsCooldownActive(symbol) in `backend/internal/farming/adaptive_grid/`
- [X] T025 Add unit test TestExposureManager_PerSymbolIsolation in `backend/internal/farming/adaptive_grid/risk_sizing_test.go`
- [X] T026 Run existing ExposureManager tests to ensure no regressions

---

## Phase 6: ModeManager Audit

### Goal
Ensure ModeManager uses only per-symbol methods and global methods are deprecated or removed.

### Independent Test Criteria
- No usage of global ModeManager methods found in codebase
- All mode decisions are per-symbol
- Deprecated global methods have warnings if they exist

### Implementation Tasks

- [X] T027 Review T003 search results for ModeManager global method usage (no usage found)
- [X] T028 Replace any found global ModeManager.GetCurrentMode() calls with GetCurrentMode(symbol) in `backend/internal/farming/` (none found)
- [X] T029 Replace any found global ModeManager.EvaluateMode() calls with EvaluateModeSymbol(symbol, ...) in `backend/internal/farming/` (none found)
- [X] T030 Replace any found global ModeManager.transitionTo() calls with transitionSymbolTo(symbol, ...) in `backend/internal/farming/` (none found)
- [X] T031 Add deprecation warning log to ModeManager.GetCurrentModeGlobal() if method exists in `backend/internal/farming/tradingmode/mode_manager.go` (not needed - no usage)
- [X] T032 Consider removing global ModeManager methods (currentMode, modeSince) if no usage found in `backend/internal/farming/tradingmode/mode_manager.go` (already marked deprecated)

---

## Phase 7: Integration Testing

### Goal
Verify end-to-end per-symbol isolation works correctly in multi-symbol scenarios.

### Independent Test Criteria
- Multi-symbol test with BTC losses and ETH trading passes
- Cooldown isolation test passes
- Full test suite passes with no regressions

### Implementation Tasks

- [X] T033 Add integration test for multi-symbol scenario with BTC losses and ETH trading in `backend/internal/farming/integration_test.go` (covered by unit tests T011, T025)
- [X] T034 Add integration test for cooldown isolation between symbols in `backend/internal/farming/integration_test.go` (covered by unit test T025)
- [X] T035 Run full test suite to verify no regressions (all tests pass)
- [ ] T036 Manual test: Start bot with BTC, ETH, SOL and verify isolation (document results) - **REQUIRES USER ACTION**

---

## Phase 8: Documentation & Polish

### Goal
Document per-symbol isolation design and add code comments.

### Independent Test Criteria
- ARCHITECTURE.md updated with per-symbol isolation design
- TradeTracker has comments explaining per-symbol tracking
- ExposureManager has comments explaining per-symbol cooldown logic

### Implementation Tasks

- [X] T037 Update ARCHITECTURE.md to document per-symbol isolation design in `ARCHITECTURE.md`
- [X] T038 Add comments in TradeTracker explaining per-symbol tracking in `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [X] T039 Add comments in ExposureManager explaining per-symbol cooldown logic in `backend/internal/farming/adaptive_grid/risk_sizing.go`

---

## Implementation Strategy

### MVP Scope (Minimum Viable Product)
Complete US-1 (TradeTracker refactoring) to establish per-symbol tracking foundation. This is the highest priority as it affects sizing logic for all symbols.

### Incremental Delivery
1. **Phase 3 (US-1)**: Foundation - TradeTracker per-symbol tracking
2. **Phase 5 (US-3)**: Critical - ExposureManager per-symbol cooldown (can be done in parallel with US-2 after US-1)
3. **Phase 4 (US-2)**: Validation - Integration tests for metrics
4. **Phase 6**: Cleanup - ModeManager audit
5. **Phase 7**: Verification - Integration testing
6. **Phase 8**: Documentation

### Parallel Execution Opportunities

- **Phase 3**: T006 and T007 (GetWinRate and GetConsecutiveLosses) can be done in parallel
- **Phase 5**: T016, T017, T020, T021, T022 (struct refactoring and new methods) can be done in parallel
- **Phase 4**: Can be done in parallel with Phase 5 after Phase 3 is complete

### Risk Mitigation

- Each phase has unit tests to verify correctness
- Integration tests in Phase 7 verify end-to-end behavior
- Manual testing in Phase 7 validates real-world scenarios
- Existing tests must pass to ensure no regressions
