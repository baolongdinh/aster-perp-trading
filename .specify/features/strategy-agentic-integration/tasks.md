# Strategy-Agentic Integration - Task Breakdown

## Phase 1: Core Integration (P0)

### Task 1.1: Create StrategySignalAggregator
**Priority**: P0 | **Estimate**: 1 day

**Files to Create/Modify**:
- `backend/internal/agentic/strategy_bridge.go` (NEW)
- `backend/internal/agentic/types.go` (append)

**Implementation Checklist**:
- [ ] Define `StrategySignalAggregator` struct
- [ ] Define `SignalBundle` and `StructuralLevels` types
- [ ] Implement `NewStrategySignalAggregator()` constructor
- [ ] Wire existing `strategy.Router` into aggregator
- [ ] Implement `GetSignalBundle(symbol, regime)` method
- [ ] Add signal caching with TTL (5 seconds)
- [ ] Implement signal strength aggregation formula
- [ ] Add logging for signal collection

**Test Requirements**:
- [ ] Unit test: Initialize with strategies
- [ ] Unit test: Collect signals from mock strategies
- [ ] Unit test: Calculate signal strength correctly
- [ ] Unit test: Cache expiration works

---

### Task 1.2: Enhance OpportunityScorer with SetupQuality
**Priority**: P0 | **Estimate**: 0.5 day

**Files to Modify**:
- `backend/internal/agentic/scorer.go`

**Implementation Checklist**:
- [ ] Add `SetupQuality` weight to `ScoringConfig`
- [ ] Modify `CalculateScore()` signature to accept `SignalBundle`
- [ ] Implement `scoreSetupQuality(bundle, regime)` method
- [ ] Add regime alignment bonus (sideways + mean reversion)
- [ ] Add signal strength scoring (0.7+ = +30pts, 0.4+ = +15pts)
- [ ] Update `GetFactorBreakdown()` to include setup score
- [ ] Ensure backward compatibility (nil bundle = 50 neutral)

**Test Requirements**:
- [ ] Unit test: Nil bundle returns 50
- [ ] Unit test: Strong signal adds 30 points
- [ ] Unit test: Regime alignment bonus works
- [ ] Unit test: Final score calculation correct

---

### Task 1.3: Wire SignalAggregator into AgenticEngine
**Priority**: P0 | **Estimate**: 0.5 day

**Files to Modify**:
- `backend/internal/agentic/engine.go`

**Implementation Checklist**:
- [ ] Add `signalAggregator` field to `AgenticEngine`
- [ ] Initialize aggregator in `NewAgenticEngine()`
- [ ] Add `collectStrategySignals()` method
- [ ] Modify `runDetectionCycle()` to collect signals
- [ ] Create `calculateScoresWithSignals()` method
- [ ] Wire WebSocket klines to strategy router
- [ ] Add config check to enable/disable integration

**Integration Points**:
- [ ] Connect to existing WebSocket kline stream
- [ ] Ensure thread-safe signal access
- [ ] Add metrics: signal_collection_latency_ms

---

## Phase 2: Flow & Geometry Enhancement (P1)

### Task 2.1: Strategy-Aware FluidFlowEngine
**Priority**: P1 | **Estimate**: 0.5 day

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/fluid_flow.go`

**Implementation Checklist**:
- [ ] Create `CalculateFlowWithSignals()` method
- [ ] Implement intensity modulation (0.6x - 1.3x)
- [ ] Apply directional bias from signal
- [ ] Adjust size multiplier by conviction
- [ ] Add logging for modulation factors
- [ ] Update AdaptiveGridManager to use new method

**Config Changes**:
- [ ] Add `intensity_multiplier_max` config
- [ ] Add `intensity_multiplier_min` config

---

### Task 2.2: Strategy-Aware AdaptiveGridGeometry
**Priority**: P1 | **Estimate**: 0.5 day

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go`

**Implementation Checklist**:
- [ ] Create `GridGeometry` result struct with multipliers
- [ ] Implement `CalculateSpreadWithSignals()` method
- [ ] Add FVG asymmetric spread logic
- [ ] Add Liquidity Sweep asymmetric spread logic
- [ ] Add Mean Reversion balanced spread logic
- [ ] Update GridManager to read multipliers

**Test Requirements**:
- [ ] Unit test: FVG bullish tightens buy spread
- [ ] Unit test: Sweep high widens sell spread
- [ ] Unit test: BB bounce maintains balance

---

## Phase 3: Smart Entry & Risk Management (P2)

### Task 3.1: Signal-Triggered Grid Entry
**Priority**: P2 | **Estimate**: 0.5 day

**Files to Modify**:
- `backend/internal/farming/grid_manager.go`
- `backend/internal/config/config.go` (append)

**Implementation Checklist**:
- [ ] Define `GridEntryStrategy` config struct
- [ ] Add `EntryStrategy` field to `GridManager`
- [ ] Implement `shouldEnterGrid()` method
- [ ] Add "time_based" mode (existing behavior)
- [ ] Add "signal_triggered" mode
- [ ] Add "hybrid" mode with timeout
- [ ] Implement `hasEntryTimeoutExpired()` helper
- [ ] Add entry mode to YAML config

**Config Schema**:
```yaml
grid_entry:
  mode: "hybrid"  # time_based | signal_triggered | hybrid
  min_signal_strength: 0.5
  entry_timeout_sec: 60
```

---

### Task 3.2: Structural SL/TP Integration
**Priority**: P2 | **Estimate**: 0.5 day

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/manager.go`

**Implementation Checklist**:
- [ ] Implement `SetRiskLevelsFromStructure()`
- [ ] Extract liquidity highs/lows from signal bundle
- [ ] Set SL at opposite liquidity level
- [ ] Set TP at next support/resistance
- [ ] Add buffer percentage to structural levels
- [ ] Fallback to ATR-based if no structure
- [ ] Validate levels are reasonable (not too tight)
- [ ] Add config: `use_structural_sl`, `sl_buffer_pct`

**Validation Logic**:
- [ ] Ensure SL is not within 0.5% of entry (too tight)
- [ ] Ensure SL is within 5% of entry (not too wide)
- [ ] Ensure TP gives positive R:R

---

## Phase 3.5: Advanced Continuous Fluid Adaptation (P0 - 2 days)

### Task 3.3: Continuous Strategy Blending

**Priority**: P0 | **Estimate**: 0.5 day

**File**: `backend/internal/agentic/strategy_blend.go` (NEW)

**Implementation Checklist**:
- [ ] Define `StrategyBlendEngine` struct with convergence rate
- [ ] Define `BlendSnapshot` with weights + entropy + blended output
- [ ] Implement `CalculateContinuousBlend()` with smooth weight transitions
- [ ] Implement entropy calculation for conflict detection
- [ ] Add `normalizeWeights()` to ensure sum = 1
- [ ] Implement `weightedBias()` and `weightedConviction()` methods
- [ ] Add blend history tracking for monitoring
- [ ] Integrate with `StrategySignalAggregator`

**Test Requirements**:
- [ ] Unit test: Weight transitions smooth (no jumps >0.2)
- [ ] Unit test: Entropy calculation correct for conflicting signals
- [ ] Unit test: Blend output continuous (no discrete switches)
- [ ] Unit test: Single signal = 100% weight (backward compat)

---

### Task 3.4: Signal Confidence Decay

**Priority**: P0 | **Estimate**: 0.5 day

**File**: `backend/internal/agentic/signal_decay.go` (NEW)

**Implementation Checklist**:
- [ ] Define `SignalDecayManager` with half-life and min confidence
- [ ] Define `DecayCurve` struct for tracking
- [ ] Implement exponential decay: `N(t) = N0 * 0.5^(t/half_life)`
- [ ] Implement `GetDecayedConfidence()` method
- [ ] Implement `SmoothRenewal()` with blend ratio (0.7 new + 0.3 old)
- [ ] Add decay curves map per symbol per strategy
- [ ] Integrate with StrategyBlendEngine

**Test Requirements**:
- [ ] Unit test: Decay matches exponential formula within 5%
- [ ] Unit test: Min confidence floor prevents zero
- [ ] Unit test: Renewal smooth (no jumps)
- [ ] Unit test: Independent decay per strategy

---

### Task 3.5: Predictive Flow Adjustment

**Priority**: P0 | **Estimate**: 0.5 day

**File**: `backend/internal/agentic/predictive_flow.go` (NEW)

**Implementation Checklist**:
- [ ] Define `PredictiveFlowEngine` with momentum window
- [ ] Define `SignalPoint` for history tracking
- [ ] Implement signal history storage (per symbol)
- [ ] Implement momentum calculation (strength + bias)
- [ ] Implement prediction with lead time (30s default)
- [ ] Add `CalculatePredictiveFlow()` with intensity boost + direction lead
- [ ] Implement prediction confidence calculation
- [ ] Integrate with FluidFlowEngine

**Test Requirements**:
- [ ] Unit test: Momentum calculation from 3+ points
- [ ] Unit test: Prediction accuracy >60% on test data
- [ ] Unit test: Lead time configurable
- [ ] Unit test: Fallback when prediction uncertain

---

### Task 3.6: Micro-Regime Detection

**Priority**: P0 | **Estimate**: 0.5 day

**File**: `backend/internal/agentic/micro_regime.go` (NEW)

**Implementation Checklist**:
- [ ] Define `MicroState` type with 6+ states
- [ ] Define `MicroRegimeDetector` struct
- [ ] Implement `DetectMicroRegime()` for Sideways sub-states
- [ ] Implement `DetectMicroRegime()` for Trending sub-states
- [ ] Add smooth transition logic (no discrete jumps)
- [ ] Map each micro-state to specific flow parameters
- [ ] Integrate with AdaptiveGridManager for geometry adjustment
- [ ] Add validation with historical data

**Test Requirements**:
- [ ] Unit test: Detect 6 micro-states correctly
- [ ] Unit test: MicroAccumulation detection (sweep + bounce + volume)
- [ ] Unit test: MicroCompression detection (BB width + ATR)
- [ ] Unit test: Accuracy >70% on historical data

---

## Phase 4: Testing & Validation (P1 - 2 days)

### Task 4.1: Unit Tests for Strategy Bridge

**File**: `backend/internal/agentic/strategy_bridge_test.go`

**Test Cases**:
- [ ] Signal aggregation from multiple strategies
- [ ] Regime-based strategy selection
- [ ] Signal strength calculation
- [ ] Bundle freshness/expiration

---

### Task 4.2: Integration Tests

**File**: `backend/internal/agentic/engine_integration_test.go`

**Test Scenarios**:
- [ ] **FVG Flow Test**: FVG detected → intensity 1.3x → asymmetric spread
- [ ] **Sweep Response Test**: Sweep high → short bias → wait for rejection
- [ ] **No Signal Test**: Low signal → intensity 0.6x → defensive mode
- [ ] **Hybrid Entry Test**: Wait 60s then enter even without signal
- [ ] **Structural SL Test**: SL placed at liquidity sweep level
- [ ] **Blend Test**: FVG + BB Bounce blend with 0.6/0.4 weights
- [ ] **Decay Test**: 0.8 → 0.57 after 45s with 30s half-life
- [ ] **Prediction Test**: Momentum detected → early flow adjustment
- [ ] **Micro-Regime Test**: Accumulation detected → 0.7x intensity

---

### Task 4.3: Configuration & E2E Validation

**Tasks**:
- [ ] Add full config schema to `agentic-vf-config.yaml`
- [ ] Add continuous_adaptation config section
- [ ] Create example config with recommended values
- [ ] Add config validation in `config.go`
- [ ] Test with feature disabled (backward compat)
- [ ] Performance benchmark (latency check)
- [ ] Memory profiling during signal collection

---

## Phase 5: Documentation (P2)

### Task 5.1: Architecture Documentation

**Priority**: P2 | **Estimate**: 0.25 day

**Deliverables**:
- [ ] Update ARCHITECTURE.md with Strategy Bridge section
- [ ] Create diagram: Strategy → SignalAggregator → Enhanced Flow
- [ ] Document signal weighting formula
- [ ] Document continuous adaptation architecture

---

### Task 5.2: Configuration Guide

**Priority**: P2 | **Estimate**: 0.25 day

**Deliverables**:
- [ ] Document each config parameter
- [ ] Document continuous_adaptation settings
- [ ] Provide recommended settings per market type
- [ ] Add troubleshooting section
- [ ] Add tuning guide for convergence rates

---

## Summary

| Phase | Tasks | Days | Status |
|-------|-------|------|--------|
| Phase 1 | 3 | 2-3 | 🔵 Pending |
| Phase 2 | 2 | 1 | 🔵 Pending |
| Phase 3 | 2 | 1 | 🔵 Pending |
| Phase 3.5 | 4 | 2 | 🔵 NEW - Continuous Adaptation |
| Phase 4 | 3 | 1.5 | 🔵 Pending |
| Phase 5 | 2 | 0.5 | 🔵 Pending |
| **Total** | **16** | **8-9** | |

## Dependencies

1. ✅ Strategy ecosystem exists (`internal/strategy`)
2. ✅ WebSocket klines active
3. ✅ RegimeDetector functional
4. ✅ FluidFlowEngine operational
5. ⚠️ Need to verify Strategy interface compatibility

## Risk Flags

🟡 **Medium**: Strategy interface may need adapter for Agentic types
🟡 **Medium**: Signal latency could affect real-time performance
🟢 **Low**: Existing code structure supports enhancement pattern
