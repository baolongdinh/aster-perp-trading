# Config Fixes Implementation Tasks

## Overview
Fix config loading and implementation issues in aster-perp-trading backend. 7 config files have mismatches between YAML definitions and Go implementations.

---

## Phase 1: Critical Fixes (HIGH SEVERITY) - BLOCKING

### Story: Fix Trading Hours Config Loading
**Goal:** Fix struct mismatch that prevents trading_hours.yaml from loading correctly
**Test Criteria:** `LoadOptimizationConfig()` can load trading_hours.yaml without errors

- [ ] T001 Add TimeWindow struct definition in `backend/internal/config/config.go`
- [ ] T002 Update TimeSlot struct with Window wrapper in `backend/internal/config/config.go`
- [ ] T003 Update TimeSlot struct with Window wrapper in `backend/config/config.go`
- [ ] T004 Create config converter from TimeSlot to TimeSlotConfig in `backend/internal/farming/adaptive_grid/config_converter.go`
- [ ] T005 [P] Update manager.go Initialize() to use loaded TimeFilter config
- [ ] T006 Write unit test for trading_hours.yaml loading

### Story: Fix Trend Detection Config Loading
**Goal:** Fix root key mismatch in trend_detection.yaml
**Test Criteria:** `LoadOptimizationConfig()` can load trend_detection.yaml without errors

- [ ] T007 Create TrendDetectionYAML wrapper struct in `backend/internal/config/config.go`
- [ ] T008 Update LoadOptimizationConfig() to use wrapper for trend_detection.yaml
- [ ] T009 Create config converter for TrendDetectionConfig in `backend/internal/farming/adaptive_grid/config_converter.go`
- [ ] T010 [P] Update manager.go to use loaded TrendDetection config instead of default
- [ ] T011 Write unit test for trend_detection.yaml loading

### Story: Create Safeguards Implementation
**Goal:** Implement missing safeguards logic from safeguards.yaml
**Test Criteria:** All safeguards features are functional and configurable

- [ ] T012 Create AntiReplayProtection struct in `backend/internal/farming/adaptive_grid/safeguards.go`
- [ ] T013 Implement deduplication logic with time window
- [ ] T014 Create StateValidation struct with transition validation
- [ ] T015 Implement valid transition checking (PENDING→FILLED/CANCELLED/REJECTED)
- [ ] T016 Create SpreadProtection struct in `backend/internal/farming/adaptive_grid/safeguards.go`
- [ ] T017 Implement spread monitoring and pause/resume logic
- [ ] T018 Create SlippageMonitor struct for tracking fill slippage
- [ ] T019 Create FundingProtection struct for funding rate adjustments
- [ ] T020 Create CircuitBreaker struct with fallback defaults
- [ ] T021 Create SafeguardsManager to orchestrate all protections
- [ ] T022 [P] Integrate SafeguardsManager into AdaptiveGridManager
- [ ] T023 Write unit tests for AntiReplayProtection
- [ ] T024 Write unit tests for SpreadProtection
- [ ] T025 Write unit tests for CircuitBreaker

---

## Phase 2: Enhancement (MEDIUM SEVERITY)

### Story: Enhance DynamicSpreadCalculator
**Goal:** Use full dynamic_grid.yaml config instead of hardcoded defaults
**Test Criteria:** All config fields from dynamic_grid.yaml affect calculator behavior

- [ ] T026 Add LevelAdjustments struct to DynamicSpreadCalculator
- [ ] T027 Implement dynamic level calculation based on spread multiplier
- [ ] T028 Add ATRThresholds struct matching YAML config
- [ ] T029 Implement configurable volatility thresholds
- [ ] T030 Add update_interval parsing and usage
- [ ] T031 Add log_volatility_changes flag support
- [ ] T032 Create config converter from DynamicGridConfig to DynamicSpreadConfig
- [ ] T033 [P] Update manager.go to pass full config to DynamicSpreadCalculator
- [ ] T034 Write unit test for level adjustments
- [ ] T035 Write unit test for ATR threshold configuration

### Story: Enhance InventoryManager
**Goal:** Implement full inventory_skew.yaml config support
**Test Criteria:** All inventory skew thresholds and actions work as configured

- [ ] T036 Create Thresholds struct (Low, Moderate, High, Critical)
- [ ] T037 Implement ThresholdConfig with all fields (threshold, size_reduction, pause_side, action, take_profit_reduction, emergency_close)
- [ ] T038 Add TakeProfitAdjustments struct
- [ ] T039 Implement take profit reduction by skew level
- [ ] T040 Add RebalancingConfig struct
- [ ] T041 Implement close furthest first logic
- [ ] T042 Implement breakeven exit logic
- [ ] T043 Update GetSkewAction to use configured thresholds
- [ ] T044 Update GetAdjustedOrderSize to use configured reductions
- [ ] T045 Update GetAdjustedTakeProfitDistance to use config
- [ ] T046 Add logging interval and alert configuration
- [ ] T047 [P] Update manager.go to pass full InventorySkew config
- [ ] T048 Write unit test for threshold-based actions
- [ ] T049 Write unit test for take profit adjustments

### Story: Enhance ClusterManager
**Goal:** Implement missing cluster_stoploss.yaml features
**Test Criteria:** Cluster definition, heat map, and alerts work as configured

- [ ] T050 Add ClusterDefinition struct (max_levels_per_cluster, max_distance_between_levels)
- [ ] T051 Implement cluster grouping by distance
- [ ] T052 Add HeatMapConfig struct support
- [ ] T053 Implement heat map generation with recommendations
- [ ] T054 Add ClusterAlerts struct (on_emergency_close, on_stale_close, on_breakeven_exit)
- [ ] T055 Implement alert triggering for cluster events
- [ ] T056 Add stale position detection and auto-close
- [ ] T057 [P] Update manager.go to pass full ClusterStopLoss config
- [ ] T058 Write unit test for cluster definition logic
- [ ] T059 Write unit test for heat map generation

---

## Phase 3: Integration & Manager Updates

### Story: Update Manager.go Integration
**Goal:** Ensure all loaded configs are passed to components instead of defaults
**Test Criteria:** No component uses DefaultXXXConfig() when optConfig is available

- [ ] T060 Refactor manager.go Initialize() to use helper functions for config conversion
- [ ] T061 Update TimeFilter initialization to use optConfig.TimeFilter
- [ ] T062 Update TrendDetector initialization to use optConfig.TrendDetection
- [ ] T063 Update DynamicSpreadCalculator initialization with full config
- [ ] T064 Update InventoryManager initialization with full config
- [ ] T065 Update ClusterManager initialization with full config
- [ ] T066 Add nil checks and fallback to defaults for missing configs
- [ ] T067 Add logging for which configs are loaded vs using defaults
- [ ] T068 Write integration test for full config loading path

---

## Phase 4: Testing & Validation

### Story: Comprehensive Config Testing
**Goal:** Ensure all 7 config files load and apply correctly
**Test Criteria:** 100% config coverage in tests, no field is ignored

- [ ] T069 Create testdata/ directory with sample configs
- [ ] T070 Write test for LoadOptimizationConfig with all files present
- [ ] T071 Write test for LoadOptimizationConfig with missing files
- [ ] T072 Write test for LoadOptimizationConfig with invalid YAML
- [ ] T073 Write test verifying struct field mapping completeness
- [ ] T074 Create config field coverage report script
- [ ] T075 Write benchmark test for config loading performance
- [ ] T076 Add config validation (required fields, value ranges)
- [ ] T077 Write test for hot-reload config functionality
- [ ] T078 Create integration test: config → manager → behavior

### Story: Documentation & Validation
**Goal:** Document the config system and validate implementation
**Test Criteria:** All configs are documented, examples provided

- [ ] T079 Create CONFIG.md documenting all 7 config files
- [ ] T080 Add field descriptions and default values table
- [ ] T081 Create example configs for different strategies (conservative, balanced, aggressive)
- [ ] T082 Write migration guide for config changes
- [ ] T083 Add config validation errors guide
- [ ] T084 Create troubleshooting guide for common config issues
- [ ] T085 Run full test suite and verify 100% pass rate
- [ ] T086 Validate no hardcoded values override config values
- [ ] T087 Perform manual test: change config → verify behavior change

---

## Dependencies

```
Phase 1 (Critical)
├── Story 1: Trading Hours (T001-T006)
│   └── Blocks: T005, T061
├── Story 2: Trend Detection (T007-T011)
│   └── Blocks: T010, T062
└── Story 3: Safeguards (T012-T025)
    └── Blocks: T022, T066

Phase 2 (Enhancement)
├── Story 4: Dynamic Spread (T026-T035)
│   └── Blocks: T033, T063
├── Story 5: Inventory (T036-T049)
│   └── Blocks: T047, T064
└── Story 6: Cluster (T050-T059)
    └── Blocks: T057, T065

Phase 3 (Integration)
└── Story 7: Manager Updates (T060-T068)
    └── Depends on all Phase 1 and 2

Phase 4 (Testing)
└── Story 8 & 9 (T069-T087)
    └── Depends on all previous phases
```

---

## Parallel Execution Groups

**Group A (Independent - Can run in parallel):**
- T001, T007, T012 (Struct definitions)

**Group B (Independent after Group A):**
- T002, T003, T008, T013-T021 (Implementations)

**Group C (Independent after Group B):**
- T005, T010, T022, T026, T036, T050 (Component updates)

**Group D (Independent after Group C):**
- T033, T047, T057 (Manager integration)

**Group E (Must be sequential):**
- T060-T068 (Manager.go refactor)

---

## Files Modified Summary

### New Files:
- `backend/internal/farming/adaptive_grid/safeguards.go`
- `backend/internal/farming/adaptive_grid/config_converter.go`
- `backend/internal/farming/adaptive_grid/safeguards_test.go`
- `backend/internal/farming/adaptive_grid/config_converter_test.go`
- `backend/config/CONFIG.md`

### Modified Files:
- `backend/internal/config/config.go` (TimeSlot, TrendDetectionYAML, converters)
- `backend/config/config.go` (TimeSlot)
- `backend/internal/farming/adaptive_grid/manager.go` (Initialize with loaded configs)
- `backend/internal/farming/adaptive_grid/dynamic_spread.go` (LevelAdjustments, ATRThresholds)
- `backend/internal/farming/adaptive_grid/inventory_manager.go` (Thresholds, TakeProfitAdjustments)
- `backend/internal/farming/adaptive_grid/cluster_manager.go` (ClusterDefinition, HeatMap, Alerts)

---

## Risk Assessment

**HIGH RISK (Must test carefully):**
- T060-T068: Manager.go changes - affects all trading logic
- T022: Safeguards integration - can pause/stop trading unexpectedly

**MEDIUM RISK:**
- T033, T047, T057: Component config changes - verify defaults preserved

**LOW RISK:**
- Struct additions (T001-T003, T007, T012) - additive only
- Tests (all T0xx ending in test) - safe

---

## Estimated Effort

| Phase | Tasks | Est. Hours |
|-------|-------|------------|
| Phase 1: Critical | 25 | 8-10h |
| Phase 2: Enhancement | 34 | 10-12h |
| Phase 3: Integration | 9 | 4-6h |
| Phase 4: Testing | 19 | 6-8h |
| **Total** | **87** | **28-36h** |
