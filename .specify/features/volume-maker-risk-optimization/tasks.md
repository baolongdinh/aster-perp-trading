# Tasks: Volume Maker Risk Optimization & Fill Rate Maximization

## Implementation Strategy

**MVP Scope**: User Story 1 (Active Zone Grid + Position Timeout) - Core risk prevention  
**Delivery**: Incremental by user story, each independently testable

---

## Phase 1: Setup

- [ ] T001 Review existing code in `internal/farming/maker/` to understand current structure
- [ ] T002 Verify WebSocket client provides real-time price data for EMA calculation

---

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T003 [P] Add new config parameters to `internal/farming/maker/config.go`
- [ ] T004 [P] Create new types in `internal/farming/maker/types.go` (PositionWithTimeout, TrailingState, ZoneType, GridActiveZone, RiskState, etc.)

---

## Phase 3: User Story 1 - Core Risk Prevention (Position Timeout + Active Zone)

**Goal**: Prevent liquidation by implementing position timeout and active zone grid  
**Independent Test Criteria**: Positions automatically close after 60s, orders only placed near market

### Implementation

- [ ] T005 [US1] Implement `calculateActiveZoneGrid()` in `internal/farming/maker/strategy.go` - calculate 5-10 price levels within 0.1% of current price
- [ ] T006 [US1] Modify order placement logic to only place orders within active zone (skip orders outside 0.1% range)
- [ ] T007 [US1] Add position tracking with timestamps in `internal/farming/maker/strategy.go`
- [ ] T008 [US1] Implement `positionTimeoutLoop()` - check every 5s, force close positions > 60s
- [ ] T009 [US1] Add logging for position timeout events with reason

### Test Criteria

- [ ] T010 Verify positions close automatically after 60s
- [ ] T011 Verify orders only placed within 0.1% of market price

---

## Phase 4: User Story 2 - Profit Locking (Trailing Stop)

**Goal**: Lock in profits and prevent giving back gains  
**Independent Test Criteria**: Trailing stop activates at 0.1% profit, sells when profit drops 0.03% from peak

### Implementation

- [ ] T012 [US2] Add TrailingState struct to track peak profit per position
- [ ] T013 [US2] Implement `manageTrailingStop()` - track peak profit, activate trailing at +0.1%, trail by 0.03%
- [ ] T014 [US2] Modify position close logic to check trailing stop before regular TP/SL

### Test Criteria

- [ ] T015 Verify trailing stop activates at correct profit level
- [ ] T016 Verify positions close when profit drops 0.03% from peak

---

## Phase 5: User Story 3 - Fill Rate Optimization

**Goal**: Increase fill rate by using smaller orders and post-only  
**Independent Test Criteria**: More fills per day, maker rebate captured

### Implementation

- [ ] T017 [US3] Implement `calculateOrderSize()` - split total value into $2-10 chunks
- [ ] T018 [US3] Implement `calculateOrderCount()` - increase order count based on balance (1 order per $5)
- [ ] T019 [US3] Modify order placement to use post-only flag (GTX + postOnly)
- [ ] T020 [US3] Adjust grid spacing to 0.05-0.06% (closer to liquidation buffer)

### Test Criteria

- [ ] T021 Verify order sizes are between $2-10
- [ ] T022 Verify more orders placed as balance increases

---

## Phase 6: User Story 4 - Position Limits + FIFO

**Goal**: Prevent over-exposure through position limits  
**Independent Test Criteria**: Max 5 positions per side, oldest closes first when exceeded

### Implementation

- [ ] T023 [US4] Implement `checkPositionLimits()` - count positions per side
- [ ] T024 [US4] Implement FIFO close logic - close oldest position first when > 5 per side
- [ ] T025 [US4] Add position open time tracking for FIFO ordering

### Test Criteria

- [ ] T026 Verify no more than 5 positions per side
- [ ] T027 Verify oldest position closes first when limit exceeded

---

## Phase 7: User Story 5 - Zone-Based Sizing

**Goal**: Adjust order size based on EMA zones  
**Independent Test Criteria**: Different multipliers applied based on price distance from EMA

### Implementation

- [ ] T028 [US5] Add EMA calculation (30-period) to market data
- [ ] T029 [US5] Implement `calculateZoneMultiplier()` - determine zone (above/normal/strong/hard) and return multiplier
- [ ] T030 [US5] Apply zone multiplier to order size calculation
- [ ] T031 [US5] Add hard dip protection (0x multiplier when price < -2% from EMA)

### Test Criteria

- [ ] T032 Verify correct zone classification based on EMA distance
- [ ] T033 Verify correct multiplier applied for each zone

---

## Phase 8: User Story 6 - Daily Reset

**Goal**: Prevent overnight exposure  
**Independent Test Criteria**: All positions closed by configured hour (default 23:00 UTC)

### Implementation

- [ ] T032 [US6] Implement `dailyResetLoop()` - check current hour vs reset hour
- [ ] T033 [US6] Add `closeAllPositions()` function for daily reset
- [ ] T034 [US6] Add daily reset state tracking (last reset date, volume, profit)

### Test Criteria

- [ ] T035 Verify all positions closed at reset hour
- [ ] T036 Verify no overnight positions remain

---

## Phase 9: User Story 7 - Risk Guards (Spread, Startup, Emergency)

**Goal**: Comprehensive risk management  
**Independent Test Criteria**: No trading during wide spread, proper state reconciliation on startup

### Implementation

- [ ] T037 [US7] Implement `checkSpreadBeforeOrder()` - pause when spread > 0.1%
- [ ] T038 [US7] Implement `reconcileOnStartup()` - fetch positions/orders from exchange, verify against local state
- [ ] T039 [US7] Implement emergency trigger at 80% position limit
- [ ] T040 [US7] Add spread protection resume logic (auto resume when spread < 0.1%)

### Test Criteria

- [ ] T041 Verify no orders placed when spread > 0.1%
- [ ] T042 Verify startup reconciliation detects state mismatches

---

## Phase 10: User Story 8 - 10-Level Risk Management

**Goal**: Professional-grade risk system  
**Independent Test Criteria**: All 10 risk levels functional

### Implementation

- [ ] T041 [US8] Create `internal/farming/maker/risk_manager.go`
- [ ] T042 [US8] Implement RiskManager struct with 10 levels
- [ ] T043 [US8] Implement `CheckAllLevels()` - evaluate all triggers
- [ ] T044 [US8] Implement Level 5 (position timeout) trigger
- [ ] T045 [US8] Implement Level 10 (emergency stop) with manual reset

### Test Criteria

- [ ] T046 Verify each risk level triggers correct action
- [ ] T047 Verify emergency stop fully halts trading

---

## Phase 11: Polish & Cross-Cutting

- [ ] T048 Add comprehensive logging for all new features
- [ ] T049 Update NGHIEPVU.md with new flow diagrams
- [ ] T050 Run integration test with dry-run mode
- [ ] T051 Verify all success criteria from spec met

---

## Dependencies

```
T001, T002 (Setup)
    ↓
T003, T004 (Foundational)
    ↓
T005-T011 (US1: Core Risk Prevention) ← MVP
    ↓
T012-T016 (US2: Trailing Stop)
    ↓
T017-T022 (US3: Fill Rate)
    ↓
T023-T027 (US4: Position Limits)
    ↓
T028-T033 (US5: Zone-Based)
    ↓
T034-T036 (US6: Daily Reset)
    ↓
T037-T042 (US7: Risk Guards)
    ↓
T043-T047 (US8: 10-Level Risk)
    ↓
T048-T051 (Polish)
```

---

## Parallel Opportunities

| Tasks | Reason |
|-------|--------|
| T003, T004 | Independent - config vs types |
| T005, T007 | Can start together - different functions |
| T012, T013 | Trailing state and logic can be parallel |
| T017, T018 | Order size and count are independent |
| T028, T029 | EMA and zone calculation can be parallel |

---

## Summary

| Metric | Value |
|--------|-------|
| Total Tasks | 51 |
| User Stories | 8 |
| MVP Tasks (US1) | 7 |
| Parallelizable | ~15 tasks |
| Independent Test Criteria | 8 (one per user story) |

**Suggested MVP**: T005-T011 (US1) - Core risk prevention with position timeout and active zone
