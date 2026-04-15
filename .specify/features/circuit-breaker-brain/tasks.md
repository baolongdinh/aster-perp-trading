# Tasks: Circuit Breaker as "Brain" - Merge ModeManager

## Phase 1: Refactor ModeManager to Per-Symbol

### Task 1.1: Add SymbolModeState struct
**File:** `backend/internal/farming/tradingmode/mode_manager.go`
**Description:** Create struct to hold per-symbol mode state
```go
type SymbolModeState struct {
    currentMode  TradingMode
    modeSince    time.Time
    cooldownEnd  time.Time
    history      []ModeTransition
}
```
**Acceptance:** Struct defined with all required fields

---

### Task 1.2: Add symbolModes map to ModeManager
**File:** `backend/internal/farming/tradingmode/mode_manager.go`
**Description:** Add map to track per-symbol mode state
```go
type ModeManager struct {
    // ... existing fields ...
    symbolModes map[string]*SymbolModeState
}
```
**Acceptance:** Map added, initialized in NewModeManager

---

### Task 1.3: Refactor GetCurrentMode to GetCurrentMode(symbol)
**File:** `backend/internal/farming/tradingmode/mode_manager.go`
**Description:** Add symbol parameter, return mode for specific symbol
**Acceptance:** Method signature updated, returns symbol's mode

---

### Task 1.4: Refactor EvaluateMode to EvaluateMode(symbol, ...)
**File:** `backend/internal/farming/tradingmode/mode_manager.go`
**Description:** Add symbol parameter, update mode for specific symbol
**Acceptance:** Method signature updated, updates specific symbol's mode

---

### Task 1.5: Update all ModeManager method signatures
**File:** `backend/internal/farming/tradingmode/mode_manager.go`
**Description:** Add symbol parameter to: EnterCooldown, IsInCooldown, GetCooldownRemaining, SetOnCooldownCallback
**Acceptance:** All methods updated with symbol parameter

---

### Task 1.6: Update AdaptiveGridManager to pass symbol
**File:** `backend/internal/farming/adaptive_grid/manager.go`
**Description:** Update CanPlaceOrder to pass symbol to all ModeManager calls
**Acceptance:** All ModeManager calls include symbol parameter

---

### Task 1.7: Update ModeManager tests
**File:** `backend/internal/farming/tradingmode/mode_manager_test.go` (create if not exists)
**Description:** Add tests for per-symbol mode management
**Acceptance:** Tests cover per-symbol mode transitions

---

### Task 1.8: Integration test for AdaptiveGridManager
**File:** `backend/internal/farming/adaptive_grid/manager_test.go`
**Description:** Test AdaptiveGridManager with per-symbol ModeManager
**Acceptance:** Integration tests pass

---

## Phase 2: Merge ModeManager Logic into CircuitBreaker

### Task 2.1: Rename SymbolBreakerState to SymbolDecisionState
**File:** `backend/internal/agentic/circuit_breaker.go`
**Description:** Rename struct to reflect dual purpose
**Acceptance:** Struct renamed, all references updated

---

### Task 2.2: Add tradingMode and modeSince to SymbolDecisionState
**File:** `backend/internal/agentic/circuit_breaker.go`
**Description:** Add fields from ModeManager
```go
type SymbolDecisionState struct {
    // ... existing fields ...
    tradingMode TradingMode
    modeSince   time.Time
}
```
**Acceptance:** Fields added, initialized in NewCircuitBreaker

---

### Task 2.3: Add onModeChangeCallback to CircuitBreaker
**File:** `backend/internal/agentic/circuit_breaker.go`
**Description:** Add callback for mode changes
**Acceptance:** Callback field added, SetOnModeChangeCallback method added

---

### Task 2.4: Copy EvaluateMode logic from ModeManager
**File:** `backend/internal/agentic/circuit_breaker.go`
**Description:** Copy mode decision logic, adapt for per-symbol
**Acceptance:** Logic copied, adapted for per-symbol use

---

### Task 2.5: Implement evaluateSymbol method
**File:** `backend/internal/agentic/circuit_breaker.go`
**Description:** Combine breaker checks + mode decision
```go
func (cb *CircuitBreaker) evaluateSymbol(symbol string, state *SymbolDecisionState) (canTrade bool, mode TradingMode)
```
**Acceptance:** Method implemented, returns both canTrade and mode

---

### Task 2.6: Update evaluation worker to call evaluateSymbol
**File:** `backend/internal/agentic/circuit_breaker.go`
**Description:** Update evaluationLoop to call evaluateSymbol and update mode
**Acceptance:** Worker updates both isTripped and tradingMode

---

### Task 2.7: Trigger onModeChangeCallback in evaluation worker
**File:** `backend/internal/agentic/circuit_breaker.go`
**Description:** Call callback when mode changes
**Acceptance:** Callback triggered on mode transitions

---

### Task 2.8: Update CircuitBreaker tests
**File:** `backend/internal/agentic/agentic_test.go`
**Description:** Add tests for mode decision logic
**Acceptance:** Tests cover mode decision evaluation

---

### Task 2.9: Test evaluation worker with mode updates
**File:** `backend/internal/agentic/agentic_test.go`
**Description:** Test worker updates mode correctly
**Acceptance:** Worker tests pass

---

## Phase 3: Update Trade Logic to Use Unified API

### Task 3.1: Add GetSymbolDecision method to CircuitBreaker
**File:** `backend/internal/agentic/circuit_breaker.go`
**Description:** Implement unified API
```go
func (cb *CircuitBreaker) GetSymbolDecision(symbol string) (canTrade bool, mode TradingMode)
```
**Acceptance:** Method implemented, returns decision

---

### Task 3.2: Update AdaptiveGridManager.CanPlaceOrder
**File:** `backend/internal/farming/adaptive_grid/manager.go`
**Description:** Remove ModeManager calls, use CircuitBreaker.GetSymbolDecision
**Acceptance:** Method uses only CircuitBreaker

---

### Task 3.3: Remove ModeManager from AdaptiveGridManager struct
**File:** `backend/internal/farming/adaptive_grid/manager.go`
**Description:** Remove modeManager field
**Acceptance:** Field removed, no compilation errors

---

### Task 3.4: Update VolumeFarmEngine initialization
**File:** `backend/internal/farming/volume_farm_engine.go`
**Description:** Pass CircuitBreaker to AdaptiveGridManager
**Acceptance:** CircuitBreaker passed correctly

---

### Task 3.5: Update placement logic to use mode from decision
**File:** `backend/internal/farming/adaptive_grid/manager.go`
**Description:** Use mode from GetSymbolDecision for placement
**Acceptance:** Placement uses correct mode

---

### Task 3.6: Integration test for trade decisions
**File:** `backend/internal/farming/adaptive_grid/manager_test.go`
**Description:** Test trade decisions with unified API
**Acceptance:** Integration tests pass

---

## Phase 4: Cleanup and Deprecation

### Task 4.1: Deprecate ModeManager
**File:** `backend/internal/farming/tradingmode/mode_manager.go`
**Description:** Add deprecation comment
**Acceptance:** Deprecation comment added

---

### Task 4.2: Remove ModeManager from AdaptiveGridManager
**File:** `backend/internal/farming/adaptive_grid/manager.go`
**Description:** Complete removal if not used elsewhere
**Acceptance:** ModeManager completely removed

---

### Task 4.3: Update ModeManager tests (deprecation)
**File:** `backend/internal/farming/tradingmode/mode_manager_test.go`
**Description:** Mark tests as deprecated or remove
**Acceptance:** Tests marked deprecated

---

### Task 4.4: Update CircuitBreaker tests
**File:** `backend/internal/agentic/agentic_test.go`
**Description:** Ensure all CircuitBreaker tests pass
**Acceptance:** All tests pass

---

### Task 4.5: Integration test for unified CircuitBreaker
**File:** `backend/internal/agentic/agentic_test.go`
**Description:** End-to-end test of unified CircuitBreaker
**Acceptance:** Integration test passes

---

### Task 4.6: Update documentation
**File:** `AGENTIC_TRADING_NGHIEP_VU.md`
**Description:** Document new architecture
**Acceptance:** Documentation updated

---

### Task 4.7: Update technical flow documentation
**File:** `AGENTIC_TRADING_TECHNICAL_FLOW.md`
**Description:** Update flow diagrams
**Acceptance:** Diagrams updated

---

### Task 4.8: Add feature documentation
**File:** `.specify/features/circuit-breaker-brain/README.md`
**Description:** Document feature usage
**Acceptance:** Feature documentation created

---

## Task Summary

| Phase | Tasks | Estimated Time |
|-------|-------|----------------|
| Phase 1 | 8 tasks | 2-3 hours |
| Phase 2 | 9 tasks | 2-3 hours |
| Phase 3 | 6 tasks | 2-3 hours |
| Phase 4 | 8 tasks | 1-2 hours |
| **Total** | **31 tasks** | **7-11 hours** |

## Dependencies

### Phase 1
- None (can start immediately)

### Phase 2
- Depends on Phase 1 completion

### Phase 3
- Depends on Phase 2 completion

### Phase 4
- Depends on Phase 3 completion
