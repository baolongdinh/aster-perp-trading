# Agentic + Volume Farm Integration Tasks

**Generated from**: PLAN_AGENTIC_INTEGRATION.md  
**Total Phases**: 5  
**Estimated Implementation Time**: 3-4 weeks

---

## Phase 1: Setup & Infrastructure

### Goal: Prepare codebase for Agentic integration

- [ ] T001 [P] Create agentic package directory `internal/agentic/`
- [ ] T002 Create base config types `internal/agentic/config.go` với AgenticConfig struct
- [ ] T003 Create types and constants `internal/agentic/types.go` (RegimeType, Recommendation, SymbolScore)
- [ ] T004 Add agentic config section to `internal/config/config.go`
- [ ] T005 [P] Create `config/agentic-vf-config.yaml` template với full agentic section

---

## Phase 2: Foundational (Blocking Prerequisites)

### Goal: Build core detection and scoring infrastructure

### Story: Regime Detection Engine
- [ ] T006 [P] Implement candle fetcher `internal/agentic/candle_fetcher.go` - fetch candles for multiple symbols
- [ ] T007 Implement indicator calculator `internal/agentic/indicators.go` - ADX, BB, ATR calculations
- [ ] T008 Create `internal/agentic/regime_detector.go` - per-symbol regime detection logic
- [ ] T009 [P] Add regime detection tests `internal/agentic/regime_detector_test.go`

### Story: Scoring System
- [ ] T010 Implement opportunity scorer `internal/agentic/scorer.go` - score calculation with weights
- [ ] T011 Add scoring rules for each regime type (sideways bonus, volatile penalty)
- [ ] T012 [P] Create `internal/agentic/scorer_test.go` - test scoring logic

---

## Phase 3: User Story 1 - Whitelist Management (MVP)

### Goal: Agentic can control VF whitelist

**Acceptance Criteria**:
- Agentic detects regime for N symbols
- Calculates scores and ranks symbols
- Updates VF whitelist dynamically
- VF trades only whitelisted symbols

### Tasks:
- [ ] T013 [P] Implement `internal/agentic/whitelist_manager.go` - quản lý whitelist logic
- [ ] T014 Add `UpdateWhitelist(symbols []string)` method to `internal/farming/volume_farm_engine.go`
- [ ] T015 Add `SetWhitelist(symbols []string)` method to `internal/farming/symbol_selector.go`
- [ ] T016 Add `GetActivePositions() []Position` method to `internal/farming/grid_manager.go`
- [ ] T017 [P] Wire whitelist manager to VF engine in `internal/agentic/whitelist_manager.go`
- [ ] T018 Create integration test `internal/agentic/whitelist_integration_test.go`

---

## Phase 4: User Story 2 - Agentic Engine Core

### Goal: Full AgenticEngine orchestration

**Acceptance Criteria**:
- AgenticEngine runs detection loop (30s interval)
- Manages multiple RegimeDetectors (1 per symbol)
- Updates whitelist based on scores
- Graceful start/stop lifecycle

### Tasks:
- [ ] T019 [P] Implement `internal/agentic/engine.go` - AgenticEngine struct và lifecycle methods
- [ ] T020 Add `Start(ctx context.Context)` - khởi động detection loop
- [ ] T021 Add `Stop()` - graceful shutdown
- [ ] T022 Implement detection coordinator - chạy regime detection cho tất cả symbols song song
- [ ] T023 [P] Add engine tests `internal/agentic/engine_test.go`
- [ ] T024 Create `cmd/agentic/main.go` - unified entry point khởi động cả Agentic + VF
- [ ] T025 [P] Add main integration test `cmd/agentic/main_test.go` - test full startup/shutdown

---

## Phase 5: User Story 3 - Position Sizing & Circuit Breakers

### Goal: Dynamic position sizing based on scores

**Acceptance Criteria**:
- HIGH score symbols: 100% size
- MEDIUM score symbols: 60% size
- LOW score: 30% size
- SKIP: không vào lệnh mới
- Circuit breakers override decisions

### Tasks:
- [ ] T026 [P] Implement position size calculator `internal/agentic/position_sizer.go`
- [ ] T027 Add score-based multipliers (high=1.0, medium=0.6, low=0.3)
- [ ] T028 Add regime-based multipliers (sideways=1.0, trending=0.7, volatile=0.5)
- [ ] T029 Modify `internal/farming/grid_manager.go` to accept custom order sizes
- [ ] T030 [P] Create `internal/agentic/circuit_breaker.go` - volatility spike, consecutive loss detection
- [ ] T031 Wire circuit breakers to whitelist manager
- [ ] T032 [P] Add position sizing tests `internal/agentic/position_sizer_test.go`

---

## Phase 6: Polish & Cross-Cutting Concerns

### Goal: Production readiness

### Tasks:
- [ ] T033 [P] Add comprehensive logging to AgenticEngine (zap logger)
- [ ] T034 Implement metrics collection (score history, regime transitions, whitelist changes)
- [ ] T035 Add health check endpoint cho Agentic status
- [ ] T036 Update `run-agentic-termux.sh` để hỗ trợ unified config
- [ ] T037 [P] Add Makefile targets cho agentic-vf integration
- [ ] T038 Create migration guide từ `agentic-config.yaml` sang `agentic-vf-config.yaml`
- [ ] T039 [P] Write integration tests e2e `e2e/agentic_vf_test.go`
- [ ] T040 Update README.md với architecture diagram và setup instructions

---

## Dependencies & Execution Order

```
Phase 1 (Setup)
    ↓
Phase 2 (Foundational)
    ├── Regime Detection (T006-T009)
    └── Scoring System (T010-T012)
    ↓
Phase 3 (US1: Whitelist)
    ├── Core logic (T013)
    ├── VF modifications (T014-T016)
    └── Integration (T017-T018)
    ↓
Phase 4 (US2: Engine)
    ├── Engine core (T019-T023)
    └── Entry point (T024-T025)
    ↓
Phase 5 (US3: Position Sizing)
    ├── Sizer (T026-T028)
    ├── VF modifications (T029)
    └── Breakers (T030-T032)
    ↓
Phase 6 (Polish)
```

---

## Parallel Execution Opportunities

| Tasks | Can Run Parallel | Notes |
|-------|-----------------|-------|
| T001-T005 | ✅ Yes | Different files, no dependencies |
| T006-T007 | ✅ Yes | Independent components |
| T013, T014-T016 | ✅ Yes | Whitelist logic và VF methods independent |
| T026-T028 | ✅ Yes | Different sizing aspects |
| T033-T040 | ✅ Yes | Polish tasks independent |

---

## MVP Scope (Phase 1-3 only)

**Minimum Viable Product** = Phases 1-3:
1. ✅ Setup infrastructure
2. ✅ Regime detection + scoring
3. ✅ Whitelist management + VF integration

**What works after MVP:**
- Agentic detects regime cho 5-10 symbols
- Calculates scores và ranks
- Updates VF whitelist (max 3 symbols)
- VF trades the whitelisted symbols

**What's NOT in MVP:**
- Position sizing adjustments
- Circuit breakers
- Metrics/monitoring
- Full parallel detection

---

## File Inventory

### New Files (16)
- `internal/agentic/config.go`
- `internal/agentic/types.go`
- `internal/agentic/candle_fetcher.go`
- `internal/agentic/indicators.go`
- `internal/agentic/regime_detector.go`
- `internal/agentic/regime_detector_test.go`
- `internal/agentic/scorer.go`
- `internal/agentic/scorer_test.go`
- `internal/agentic/whitelist_manager.go`
- `internal/agentic/whitelist_integration_test.go`
- `internal/agentic/engine.go`
- `internal/agentic/engine_test.go`
- `internal/agentic/position_sizer.go`
- `internal/agentic/position_sizer_test.go`
- `internal/agentic/circuit_breaker.go`
- `config/agentic-vf-config.yaml`

### Modified Files (6)
- `internal/config/config.go`
- `internal/farming/volume_farm_engine.go`
- `internal/farming/symbol_selector.go`
- `internal/farming/grid_manager.go`
- `cmd/agentic/main.go`
- `Makefile`

### Test Files (6)
- `internal/agentic/regime_detector_test.go`
- `internal/agentic/scorer_test.go`
- `internal/agentic/whitelist_integration_test.go`
- `internal/agentic/engine_test.go`
- `internal/agentic/position_sizer_test.go`
- `e2e/agentic_vf_test.go`

---

## Test Criteria Per Phase

| Phase | Test Criteria |
|-------|--------------|
| Phase 1 | Config types compile, YAML parses correctly |
| Phase 2 | Regime detection returns correct regime, Scoring math verified |
| Phase 3 | Whitelist updates reflect in VF, VF only trades whitelisted |
| Phase 4 | Engine starts/stops gracefully, Detection loop runs at interval |
| Phase 5 | Position sizes match score/regime multipliers, Breakers trigger correctly |
| Phase 6 | All tests pass, Termux scripts work, e2e test passes |

---

## Implementation Strategy

1. **MVP First**: Complete Phases 1-3 để có working integration
2. **Incremental**: Each phase independently testable
3. **Parallel Tasks**: Tận dụng [P] tasks để speed up
4. **Test Early**: Viết test ngay sau mỗi implementation task
5. **Integration Last**: Chỉ wire components khi individual parts work

**Estimated Timeline**:
- Phase 1: 1 day
- Phase 2: 3-4 days (parallelizable)
- Phase 3: 2-3 days
- Phase 4: 3-4 days
- Phase 5: 2-3 days
- Phase 6: 2 days

**Total MVP**: ~6-8 days
**Total Complete**: ~15-18 days
