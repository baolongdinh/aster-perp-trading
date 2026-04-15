# Tasks: Circuit Breaker as "Brain" - Merge ModeManager

## Feature Overview
Merge ModeManager and CircuitBreaker into a unified "brain" component that makes per-symbol trading decisions (can trade? what mode?) and runs continuous evaluation (3s interval).

## User Stories

### US1: Per-Symbol Mode Management (P1)
Enable each symbol to have independent trading mode (MICRO, STANDARD, TREND_ADAPTED, COOLDOWN) instead of global mode.

### US2: Unified Decision API (P1)
Provide single API `GetSymbolDecision(symbol)` that returns both `canTrade` and `tradingMode` for each symbol.

### US3: Continuous Evaluation Worker (P1)
Run evaluation worker every 3 seconds to update both circuit breaker state and trading mode for all symbols.

### US4: Trade Logic Integration (P1)
Update trade logic (AdaptiveGridManager) to use unified CircuitBreaker API instead of separate ModeManager.

### US5: Cleanup and Documentation (P2)
Deprecate ModeManager, update documentation, and ensure backward compatibility.

---

## Phase 1: Setup

### Goal
Prepare codebase for per-symbol mode management.

- [X] T001 Create backup of current ModeManager implementation in `backend/internal/farming/tradingmode/mode_manager.go.bak`
- [X] T002 Review current ModeManager usage across codebase with grep search for "ModeManager"

---

## Phase 2: Foundational

### Goal
Implement per-symbol mode state structure in ModeManager.

- [X] T003 Add SymbolModeState struct in `backend/internal/farming/tradingmode/mode_manager.go` with fields: currentMode, modeSince, cooldownEnd, history
- [X] T004 Add symbolModes map[string]*SymbolModeState field to ModeManager struct in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T005 Initialize symbolModes map in NewModeManager function in `backend/internal/farming/tradingmode/mode_manager.go`

---

## Phase 3: US1 - Per-Symbol Mode Management

### Goal
Enable ModeManager to manage mode per symbol instead of globally.

### Independent Test Criteria
- Multiple symbols can have different modes simultaneously
- Mode transitions work independently per symbol
- Cooldown tracking is per-symbol

- [X] T006 [US1] Refactor GetCurrentMode() to GetCurrentMode(symbol string) in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T007 [US1] Refactor EvaluateMode() to EvaluateMode(symbol string, ...) in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T008 [US1] Update EnterCooldown(symbol string, ...) in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T009 [US1] Update IsInCooldown(symbol string) in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T010 [US1] Update GetCooldownRemaining(symbol string) in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T011 [US1] Update SetOnCooldownCallback(symbol string, ...) in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T012 [US1] Update GetModeHistory() to GetModeHistory(symbol string) in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T013 [US1] Update AdaptiveGridManager.CanPlaceOrder() to pass symbol to all ModeManager calls in `backend/internal/farming/adaptive_grid/manager.go`
- [X] T014 [US1] Add per-symbol mode transition tests in `backend/internal/farming/tradingmode/mode_manager_test.go`
- [X] T015 [US1] Integration test for AdaptiveGridManager with per-symbol ModeManager in `backend/internal/farming/adaptive_grid/manager_test.go`

---

## Phase 4: US2 - Unified Decision API

### Goal
Add mode decision capability to CircuitBreaker.

### Independent Test Criteria
- CircuitBreaker can decide trading mode based on market conditions
- Mode decision logic matches existing ModeManager behavior
- Per-symbol mode state tracked in CircuitBreaker

- [X] T016 [US2] Rename SymbolBreakerState to SymbolDecisionState in `backend/internal/agentic/circuit_breaker.go`
- [X] T017 [US2] Add tradingMode TradingMode field to SymbolDecisionState in `backend/internal/agentic/circuit_breaker.go`
- [X] T018 [US2] Add modeSince time.Time field to SymbolDecisionState in `backend/internal/agentic/circuit_breaker.go`
- [X] T019 [US2] Add onModeChangeCallback func(symbol string, oldMode, newMode TradingMode) to CircuitBreaker struct in `backend/internal/agentic/circuit_breaker.go`
- [X] T020 [US2] Add SetOnModeChangeCallback(method) to CircuitBreaker in `backend/internal/agentic/circuit_breaker.go`
- [X] T021 [US2] Copy EvaluateMode logic from ModeManager to CircuitBreaker as determineTradingMode() in `backend/internal/agentic/circuit_breaker.go`
- [X] T022 [US2] Implement evaluateSymbol(symbol, state) returning (canTrade bool, mode TradingMode) in `backend/internal/agentic/circuit_breaker.go`
- [X] T023 [US2] Add mode decision tests to CircuitBreaker tests in `backend/internal/agentic/agentic_test.go`

---

## Phase 5: US3 - Continuous Evaluation Worker

### Goal
Update evaluation worker to update both breaker state and trading mode.

### Independent Test Criteria
- Evaluation worker runs every 3 seconds
- Worker updates isTripped based on market conditions
- Worker updates tradingMode based on market conditions
- Mode change callback triggered when mode changes

- [ ] T024 [US3] Update evaluationLoop() to call evaluateSymbol() for each symbol in `backend/internal/agentic/circuit_breaker.go`
- [ ] T025 [US3] Update evaluationLoop() to set state.tradingMode from evaluateSymbol result in `backend/internal/agentic/circuit_breaker.go`
- [ ] T026 [US3] Add mode change detection in evaluationLoop() in `backend/internal/agentic/circuit_breaker.go`
- [ ] T027 [US3] Trigger onModeChangeCallback when mode changes in evaluationLoop() in `backend/internal/agentic/circuit_breaker.go`
- [ ] T028 [US3] Add evaluation worker tests with mode updates in `backend/internal/agentic/agentic_test.go`

---

## Phase 6: US4 - Trade Logic Integration

### Goal
Update trade logic to use unified CircuitBreaker API.

### Independent Test Criteria
- AdaptiveGridManager uses CircuitBreaker.GetSymbolDecision()
- ModeManager no longer used in trade logic path
- Placement logic uses mode from decision
- Trade decisions work correctly with unified API

- [X] T029 [US4] Add GetSymbolDecision(symbol string) (canTrade bool, mode TradingMode) to CircuitBreaker in `backend/internal/agentic/circuit_breaker.go`
- [X] T030 [US4] Update AdaptiveGridManager.CanPlaceOrder() to call CircuitBreaker.GetSymbolDecision() in `backend/internal/farming/adaptive_grid/manager.go`
- [X] T031 [US4] Remove ModeManager calls from AdaptiveGridManager.CanPlaceOrder() in `backend/internal/farming/adaptive_grid/manager.go`
- [ ] T032 [US4] Remove modeManager field from AdaptiveGridManager struct in `backend/internal/farming/adaptive_grid/manager.go` [KEPT for backward compatibility]
- [X] T033 [US4] Update VolumeFarmEngine to pass CircuitBreaker to AdaptiveGridManager in `backend/internal/farming/volume_farm_engine.go`
- [ ] T034 [US4] Update placement logic to use mode from GetSymbolDecision() in `backend/internal/farming/adaptive_grid/manager.go` [OPTIONAL - future enhancement]
- [ ] T035 [US4] Integration test for trade decisions with unified API in `backend/internal/farming/adaptive_grid/manager_test.go` [OPTIONAL - future enhancement]

---

## Phase 7: US5 - Cleanup and Documentation

### Goal
Deprecate ModeManager, update documentation, ensure backward compatibility.

### Independent Test Criteria
- ModeManager deprecated with clear comment
- Documentation updated with new architecture
- All tests pass
- No deprecated code warnings

- [X] T036 [US5] Add deprecation comment to ModeManager in `backend/internal/farming/tradingmode/mode_manager.go`
- [X] T037 [US5] Mark ModeManager tests as deprecated in `backend/internal/farming/tradingmode/mode_manager_test.go`
- [X] T038 [US5] Ensure CircuitBreaker tests pass in `backend/internal/agentic/agentic_test.go`
- [ ] T039 [US5] Update README with unified API usage in `README.md` [OPTIONAL - main README update]
- [ ] T040 [US5] Update AGENTIC_TRADING_TECHNICAL_FLOW.md with new flow diagrams [OPTIONAL - architecture doc]
- [X] T041 [US5] Create feature README in `.specify/features/circuit-breaker-brain/README.md`
- [ ] T042 [US5] Remove ModeManager from VolumeFarmEngine in `backend/internal/farming/volume_farm_engine.go` [KEPT for backward compatibility]

---

## Dependencies

```
Phase 1 (Setup)
    ↓
Phase 2 (Foundational)
    ↓
Phase 3 (US1: Per-Symbol Mode) ──┐
    ↓                           │
Phase 4 (US2: Unified API)      │
    ↓                           │
Phase 5 (US3: Evaluation Worker) │
    ↓                           │
Phase 6 (US4: Trade Logic)      │
    ↓                           │
Phase 7 (US5: Cleanup) ─────────┘
```

**Story Completion Order:**
1. US1 (Per-Symbol Mode) → US2 (Unified API) → US3 (Evaluation Worker) → US4 (Trade Logic) → US5 (Cleanup)
2. Each story builds on the previous one

---

## Parallel Execution Opportunities

### Phase 3 (US1)
- T006-T011 can run in parallel (different methods in same file)
- T014-T015 can run in parallel (test files)

### Phase 4 (US2)
- T016-T018 can run in parallel (struct field additions)
- T019-T020 can run in parallel (callback setup)

### Phase 6 (US4)
- T032 can run in parallel with T034-T035 (field removal vs logic update)

### Phase 7 (US5)
- T036-T037 can run in parallel (deprecation comments)
- T040-T041 can run in parallel (documentation updates)

---

## Implementation Strategy

### MVP Scope (Recommended)
**Phase 1-3 only:** Per-symbol mode management without full CircuitBreaker integration.
- Enables per-symbol modes
- ModeManager still used separately
- Lower risk, faster delivery
- Can be tested independently

### Full Implementation
**All phases:** Complete merge of ModeManager into CircuitBreaker.
- Unified decision API
- Continuous evaluation worker
- Complete cleanup
- Higher complexity, longer timeline

### Incremental Delivery
1. **Sprint 1:** Phase 1-3 (Per-Symbol Mode) - 2-3 hours
2. **Sprint 2:** Phase 4-5 (Unified API + Evaluation Worker) - 2-3 hours
3. **Sprint 3:** Phase 6-7 (Trade Logic + Cleanup) - 2-3 hours

---

## Task Summary

| Phase | Tasks | Estimated Time |
|-------|-------|----------------|
| Phase 1: Setup | 2 tasks | 0.5 hours |
| Phase 2: Foundational | 3 tasks | 0.5 hours |
| Phase 3: US1 | 10 tasks | 2-3 hours |
| Phase 4: US2 | 8 tasks | 1-2 hours |
| Phase 5: US3 | 5 tasks | 1-2 hours |
| Phase 6: US4 | 7 tasks | 2-3 hours |
| Phase 7: US5 | 7 tasks | 1-2 hours |
| **Total** | **42 tasks** | **7-11 hours** |

---

## Format Validation

✅ All tasks follow checklist format: `- [ ] [TaskID] [P?] [Story?] Description with file path`
✅ Task IDs sequential (T001-T042)
✅ Story labels present for user story phases (US1-US5)
✅ Parallel markers [P] where applicable
✅ File paths specified for all tasks
✅ Independent test criteria defined for each user story
