# Strategy-Agentic Integration - Detailed Task Breakdown

> Generated from plan.md and spec.md using speckit-tasks format

---

## Phase 1: Setup (Infrastructure) - 0.5 day

**Goal**: Setup and validation before implementation

### Setup Tasks

- [ ] T001 Verify Strategy interface compatibility with Agentic types in `backend/internal/strategy/interface.go`
- [ ] T002 Check existing WebSocket kline stream availability in `backend/internal/stream/`
- [ ] T003 Verify FluidFlowEngine accepts external signal input in `backend/internal/farming/adaptive_grid/fluid_flow.go`
- [ ] T004 Validate config schema can accommodate new `continuous_adaptation` section in `backend/internal/config/config.go`
- [ ] T005 Create feature branch `feature/strategy-agentic-integration`

---

## Phase 2: Foundational (Core Infrastructure) - 1.5 days

**Goal**: Build core infrastructure that all user stories depend on

### Foundational Tasks

- [ ] T006 [P] Define `SignalBundle` struct in `backend/internal/agentic/types.go`
- [ ] T007 [P] Define `StructuralLevels` and `FVGZone` types in `backend/internal/agentic/types.go`
- [ ] T008 [P] Define `BlendedSignal` struct with entropy field in `backend/internal/agentic/types.go`
- [ ] T009 [P] Define `MicroState` type with 7 states in `backend/internal/agentic/micro_regime.go`
- [ ] T010 Create `StrategySignalAggregator` skeleton in `backend/internal/agentic/strategy_bridge.go`
- [ ] T011 Implement strategy initialization by regime in `backend/internal/agentic/strategy_bridge.go:InitializeStrategies()`
- [ ] T012 Wire WebSocket klines to strategy router in `backend/internal/agentic/strategy_bridge.go:OnKline()`
- [ ] T013 Implement `GetSignalBundle()` method in `backend/internal/agentic/strategy_bridge.go`
- [ ] T014 Add signal cache with 5s TTL in `backend/internal/agentic/strategy_bridge.go`
- [ ] T015 Create unit tests for StrategySignalAggregator in `backend/internal/agentic/strategy_bridge_test.go`

---

## Phase 3: User Story 1 - Basic Strategy Integration (P0) - 2 days

**Story**: As a trader, I want the bot to use strategy signals for basic opportunity scoring and flow modulation  
**Test Criteria**: `TestFVGFlow` passes - FVG signal 0.8 → intensity 1.3x

### US1 Tasks

- [ ] T016 [US1] Enhance `OpportunityScorer.CalculateScore()` to accept `SignalBundle` in `backend/internal/agentic/scorer.go`
- [ ] T017 [US1] Implement `scoreSetupQuality()` method in `backend/internal/agentic/scorer.go`
- [ ] T018 [US1] Add regime alignment bonus logic in `backend/internal/agentic/scorer.go`
- [ ] T019 [US1] Wire SignalAggregator into AgenticEngine in `backend/internal/agentic/engine.go:NewAgenticEngine()`
- [ ] T020 [US1] Modify `runDetectionCycle()` to collect strategy signals in `backend/internal/agentic/engine.go`
- [ ] T021 [US1] Create `calculateScoresWithSignals()` method in `backend/internal/agentic/engine.go`
- [ ] T022 [US1] Add strategy config to AgenticConfig in `backend/internal/config/agentic_config.go`
- [ ] T023 [US1] Implement `CalculateFlowWithSignals()` in `backend/internal/farming/adaptive_grid/fluid_flow.go`
- [ ] T024 [US1] Add intensity modulation: 0.6x to 1.3x in `backend/internal/farming/adaptive_grid/fluid_flow.go`
- [ ] T025 [US1] Add directional bias integration in `backend/internal/farming/adaptive_grid/fluid_flow.go`
- [ ] T026 [US1] Add size multiplier adjustment in `backend/internal/farming/adaptive_grid/fluid_flow.go`
- [ ] T027 [US1] Create unit test `TestScoreSetupQuality` in `backend/internal/agentic/scorer_test.go`
- [ ] T028 [US1] Create integration test `TestFVGFlow` in `backend/internal/agentic/engine_integration_test.go`

---

## Phase 4: User Story 2 - Asymmetric Grid Geometry (P0) - 1.5 days

**Story**: As a trader, I want grid spreads to adjust asymmetrically based on signal type  
**Test Criteria**: `TestSweepResponse` passes - Sweep high → widen sell spread

### US2 Tasks

- [ ] T029 [US2] Define `GridGeometry` struct with multipliers in `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go`
- [ ] T030 [US2] Implement `CalculateSpreadWithSignals()` in `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go`
- [ ] T031 [US2] Add FVG asymmetric spread logic (tighten fill side) in `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go`
- [ ] T032 [US2] Add Liquidity Sweep spread logic (widen rejection side) in `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go`
- [ ] T033 [US2] Add Mean Reversion balanced spread logic in `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go`
- [ ] T034 [US2] Wire GridGeometry to GridManager in `backend/internal/farming/grid_manager.go:SetGridGeometry()`
- [ ] T035 [US2] Update GridManager to use asymmetric multipliers in `backend/internal/farming/grid_manager.go:calculateGridSpread()`
- [ ] T036 [US2] Add geometry config to YAML schema in `backend/config/agentic-vf-config.yaml`
- [ ] T037 [US2] Create unit test `TestFVGAsymmetricSpread` in `backend/internal/farming/adaptive_grid/adaptive_grid_geometry_test.go`
- [ ] T038 [US2] Create integration test `TestSweepResponse` in `backend/internal/farming/adaptive_grid/integration_test.go`

---

## Phase 5: User Story 3 - Signal-Triggered Entry & Structural Risk (P1) - 1.5 days

**Story**: As a trader, I want signal-triggered entry and SL/TP at structural levels  
**Test Criteria**: `TestStructuralSL` passes - SL at liquidity sweep level

### US3 Tasks

- [ ] T039 [US3] Define `GridEntryStrategy` config struct in `backend/internal/config/config.go`
- [ ] T040 [US3] Add entry mode enum: time_based/signal_triggered/hybrid in `backend/internal/farming/grid_manager.go`
- [ ] T041 [US3] Implement `shouldEnterGrid()` with signal check in `backend/internal/farming/grid_manager.go`
- [ ] T042 [US3] Implement hybrid mode with timeout in `backend/internal/farming/grid_manager.go`
- [ ] T043 [US3] Implement `SetRiskLevelsFromStructure()` in `backend/internal/farming/adaptive_grid/manager.go`
- [ ] T044 [US3] Extract liquidity levels for SL placement in `backend/internal/farming/adaptive_grid/manager.go`
- [ ] T045 [US3] Extract support/resistance for TP placement in `backend/internal/farming/adaptive_grid/manager.go`
- [ ] T046 [US3] Add SL/TP buffer validation in `backend/internal/farming/adaptive_grid/manager.go`
- [ ] T047 [US3] Implement ATR fallback when no structure in `backend/internal/farming/adaptive_grid/manager.go`
- [ ] T048 [US3] Add entry config to YAML in `backend/config/agentic-vf-config.yaml`
- [ ] T049 [US3] Add risk config to YAML in `backend/config/agentic-vf-config.yaml`
- [ ] T050 [US3] Create unit test `TestShouldEnterGrid` in `backend/internal/farming/grid_manager_test.go`
- [ ] T051 [US3] Create unit test `TestStructuralSLPlacement` in `backend/internal/farming/adaptive_grid/manager_test.go`
- [ ] T052 [US3] Create integration test `TestStructuralSL` in `backend/internal/farming/adaptive_grid/integration_test.go`

---

## Phase 6: User Story 4 - Continuous Strategy Blending (P0) - 1.5 days

**Story**: As a trader, I want multiple strategies to blend continuously rather than discrete switching  
**Test Criteria**: `TestBlendSmoothness` passes - weight transitions no jumps >0.2

### US4 Tasks

- [ ] T053 [US4] Create `StrategyBlendEngine` struct in `backend/internal/agentic/strategy_blend.go`
- [ ] T054 [US4] Define `BlendSnapshot` with entropy in `backend/internal/agentic/strategy_blend.go`
- [ ] T055 [US4] Implement `CalculateContinuousBlend()` in `backend/internal/agentic/strategy_blend.go`
- [ ] T056 [US4] Add smooth weight transition (convergence rate 0.1-0.3) in `backend/internal/agentic/strategy_blend.go`
- [ ] T057 [US4] Implement `normalizeWeights()` in `backend/internal/agentic/strategy_blend.go`
- [ ] T058 [US4] Implement `weightedBias()` calculation in `backend/internal/agentic/strategy_blend.go`
- [ ] T059 [US4] Implement `weightedConviction()` calculation in `backend/internal/agentic/strategy_blend.go`
- [ ] T060 [US4] Implement entropy calculation in `backend/internal/agentic/strategy_blend.go`
- [ ] T061 [US4] Add conflict detection (entropy > 0.7) in `backend/internal/agentic/strategy_blend.go`
- [ ] T062 [US4] Integrate StrategyBlendEngine into AgenticEngine in `backend/internal/agentic/engine.go`
- [ ] T063 [US4] Replace single signal with blended signal in scoring in `backend/internal/agentic/scorer.go`
- [ ] T064 [US4] Add blending config to YAML in `backend/config/agentic-vf-config.yaml`
- [ ] T065 [US4] Create unit test `TestContinuousBlend` in `backend/internal/agentic/strategy_blend_test.go`
- [ ] T066 [US4] Create unit test `TestEntropyCalculation` in `backend/internal/agentic/strategy_blend_test.go`
- [ ] T067 [US4] Create integration test `TestBlendSmoothness` in `backend/internal/agentic/engine_integration_test.go`

---

## Phase 7: User Story 5 - Signal Confidence Decay (P1) - 1 day

**Story**: As a trader, I want signal confidence to fade smoothly over time  
**Test Criteria**: `TestDecayCurve` passes - actual decay matches formula within 5%

### US5 Tasks

- [ ] T068 [US5] Create `SignalDecayManager` struct in `backend/internal/agentic/signal_decay.go`
- [ ] T069 [US5] Define `DecayCurve` struct in `backend/internal/agentic/signal_decay.go`
- [ ] T070 [US5] Implement exponential decay: `N(t) = N0 * 0.5^(t/half_life)` in `backend/internal/agentic/signal_decay.go`
- [ ] T071 [US5] Implement `GetDecayedConfidence()` in `backend/internal/agentic/signal_decay.go`
- [ ] T072 [US5] Implement `SmoothRenewal()` with blend ratio 0.7/0.3 in `backend/internal/agentic/signal_decay.go`
- [ ] T073 [US5] Add min confidence floor (0.1) in `backend/internal/agentic/signal_decay.go`
- [ ] T074 [US5] Add decay curves map per symbol per strategy in `backend/internal/agentic/signal_decay.go`
- [ ] T075 [US5] Integrate SignalDecayManager into StrategyBlendEngine in `backend/internal/agentic/strategy_blend.go`
- [ ] T076 [US5] Add decay config to YAML in `backend/config/agentic-vf-config.yaml`
- [ ] T077 [US5] Create unit test `TestExponentialDecay` in `backend/internal/agentic/signal_decay_test.go`
- [ ] T078 [US5] Create unit test `TestSmoothRenewal` in `backend/internal/agentic/signal_decay_test.go`
- [ ] T079 [US5] Create integration test `TestDecayCurve` in `backend/internal/agentic/engine_integration_test.go`

---

## Phase 8: User Story 6 - Predictive Flow Adjustment (P1) - 1 day

**Story**: As a trader, I want the bot to predict and adjust flow before signal peaks  
**Test Criteria**: `TestPredictionAccuracy` passes - >60% correct predictions

### US6 Tasks

- [ ] T080 [US6] Create `PredictiveFlowEngine` struct in `backend/internal/agentic/predictive_flow.go`
- [ ] T081 [US6] Define `SignalPoint` for history tracking in `backend/internal/agentic/predictive_flow.go`
- [ ] T082 [US6] Implement signal history storage per symbol in `backend/internal/agentic/predictive_flow.go`
- [ ] T083 [US6] Implement strength momentum calculation in `backend/internal/agentic/predictive_flow.go`
- [ ] T084 [US6] Implement bias momentum calculation in `backend/internal/agentic/predictive_flow.go`
- [ ] T085 [US6] Implement prediction with lead time (30s) in `backend/internal/agentic/predictive_flow.go`
- [ ] T086 [US6] Implement `CalculatePredictiveFlow()` in `backend/internal/agentic/predictive_flow.go`
- [ ] T087 [US6] Add intensity boost: momentum * 0.5 in `backend/internal/agentic/predictive_flow.go`
- [ ] T088 [US6] Add direction lead: momentum * 0.3 in `backend/internal/agentic/predictive_flow.go`
- [ ] T089 [US6] Implement prediction confidence calculation in `backend/internal/agentic/predictive_flow.go`
- [ ] T090 [US6] Integrate PredictiveFlowEngine into FluidFlowEngine in `backend/internal/farming/adaptive_grid/fluid_flow.go`
- [ ] T091 [US6] Add predictive config to YAML in `backend/config/agentic-vf-config.yaml`
- [ ] T092 [US6] Create unit test `TestMomentumCalculation` in `backend/internal/agentic/predictive_flow_test.go`
- [ ] T093 [US6] Create integration test `TestPredictionAccuracy` in `backend/internal/agentic/engine_integration_test.go`

---

## Phase 9: User Story 7 - Micro-Regime Detection (P1) - 1 day

**Story**: As a trader, I want the bot to detect fine-grained market states within base regimes  
**Test Criteria**: `TestMicroRegimeAccuracy` passes - >70% accuracy on historical data

### US7 Tasks

- [ ] T094 [US7] Create `MicroRegimeDetector` struct in `backend/internal/agentic/micro_regime.go`
- [ ] T095 [US7] Implement `DetectMicroRegime()` for Sideways sub-states in `backend/internal/agentic/micro_regime.go`
- [ ] T096 [US7] Detect MicroAccumulation (sweep + bounce + volume) in `backend/internal/agentic/micro_regime.go`
- [ ] T097 [US7] Detect MicroCompression (BB width < 0.02) in `backend/internal/agentic/micro_regime.go`
- [ ] T098 [US7] Detect MicroDistribution in `backend/internal/agentic/micro_regime.go`
- [ ] T099 [US7] Implement `DetectMicroRegime()` for Trending sub-states in `backend/internal/agentic/micro_regime.go`
- [ ] T100 [US7] Detect MicroImpulse in `backend/internal/agentic/micro_regime.go`
- [ ] T101 [US7] Detect MicroPullback (FVG opposite to trend) in `backend/internal/agentic/micro_regime.go`
- [ ] T102 [US7] Detect MicroConsolidation (flag/pennant) in `backend/internal/agentic/micro_regime.go`
- [ ] T103 [US7] Add smooth transition logic between micro-states in `backend/internal/agentic/micro_regime.go`
- [ ] T104 [US7] Map each micro-state to flow parameters in `backend/internal/agentic/micro_regime.go`
- [ ] T105 [US7] Integrate MicroRegimeDetector into AdaptiveGridManager in `backend/internal/farming/adaptive_grid/manager.go`
- [ ] T106 [US7] Add micro-regime config to YAML in `backend/config/agentic-vf-config.yaml`
- [ ] T107 [US7] Create unit test `TestMicroAccumulation` in `backend/internal/agentic/micro_regime_test.go`
- [ ] T108 [US7] Create unit test `TestMicroCompression` in `backend/internal/agentic/micro_regime_test.go`
- [ ] T109 [US7] Create integration test `TestMicroRegimeAccuracy` in `backend/internal/agentic/engine_integration_test.go`

---

## Phase 10: Polish & Cross-Cutting (P2) - 1 day

**Goal**: Documentation, validation, and final polish

### Polish Tasks

- [ ] T110 Update ARCHITECTURE.md with Strategy Bridge section
- [ ] T111 Create architecture diagram: Strategy → Blend → Decay → Predict → Micro → Flow
- [ ] T112 Document signal weighting formula in ARCHITECTURE.md
- [ ] T113 Document continuous adaptation architecture in ARCHITECTURE.md
- [ ] T114 Create configuration guide with all parameters
- [ ] T115 Add tuning guide for convergence rates
- [ ] T116 Add troubleshooting section for common issues
- [ ] T117 Run full integration test suite
- [ ] T118 Performance benchmark: verify <50ms added latency
- [ ] T119 Memory profiling check for leaks
- [ ] T120 Backward compatibility test (feature disabled)
- [ ] T121 Final code review and cleanup
- [ ] T122 Merge feature branch to main

---

## Summary

| Phase | Story | Tasks | Days | Parallel Opportunities |
|-------|-------|-------|------|----------------------|
| 1 | Setup | 5 | 0.5 | T001-T005 |
| 2 | Foundational | 10 | 1.5 | T006-T009 |
| 3 | US1: Basic Integration | 13 | 2 | T016-T018, T027-T028 |
| 4 | US2: Asymmetric Geometry | 10 | 1.5 | T029-T033, T037-T038 |
| 5 | US3: Entry & Risk | 14 | 1.5 | T039-T042, T050-T052 |
| 6 | US4: Continuous Blend | 15 | 1.5 | T053-T061, T065-T067 |
| 7 | US5: Signal Decay | 12 | 1 | T068-T071, T077-T079 |
| 8 | US6: Predictive Flow | 14 | 1 | T080-T086, T092-T093 |
| 9 | US7: Micro-Regime | 16 | 1 | T094-T098, T107-T109 |
| 10 | Polish | 13 | 1 | T110-T113, T117-T119 |
| **Total** | 7 Stories | **122** | **11-12** | |

---

## Dependency Graph

```
Phase 1 (Setup)
    ↓
Phase 2 (Foundational) - StrategySignalAggregator
    ↓
Phase 3 (US1: Basic Integration) ← Depends on Phase 2
    ↓
Phase 4 (US2: Geometry) ← Can run parallel with US1 after T019
    ↓
Phase 5 (US3: Entry & Risk) ← Depends on US1, US2
    ↓
Phase 6 (US4: Blend) ← Depends on US1
    ↓
Phase 7 (US5: Decay) ← Depends on US4
    ↓
Phase 8 (US6: Predictive) ← Depends on US1, US5
    ↓
Phase 9 (US7: Micro-Regime) ← Depends on US1, US2
    ↓
Phase 10 (Polish)
```

**Parallel Execution Opportunities**:
- US2 (Geometry) can start after T019 (Aggregator wired)
- US4 (Blend), US5 (Decay), US6 (Predictive) can be developed in parallel after US1
- US7 (Micro-Regime) can be developed in parallel with US4-US6

---

## Suggested MVP Scope

**MVP = US1 + US2** (Basic integration + Asymmetric geometry)
- 5 tasks from Setup
- 10 tasks from Foundational  
- 13 tasks from US1
- 10 tasks from US2
- **Total: 38 tasks, ~5.5 days**

This delivers:
- ✅ Strategy signals collected and scored
- ✅ Flow intensity modulation (0.6x - 1.3x)
- ✅ Asymmetric grid spreads (FVG/Sweep specific)
- ⚠️ No continuous blending (discrete selection)
- ⚠️ No signal decay (sudden cut-off)
- ⚠️ No predictive adjustment
- ⚠️ No micro-regime detection

Full implementation adds US3-US7 for truly "fluid like water" behavior.
