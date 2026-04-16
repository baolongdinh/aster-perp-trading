# Volume Farming Optimization - Implementation Tasks

## Feature Name
Volume Farming Optimization - Agentic Bot Integration

## Overview
This task breakdown addresses the critical gaps in the current volume farming optimization implementation. The bot currently has volume optimization components created but not integrated into the core trading flow. These tasks ensure all components are truly wired and used in the agentic bot's core logic, not just logging or unused code.

---

## Phase 1: Setup - Configuration

### Goal
Add volume_optimization configuration section to enable all optimization features.

### Independent Test Criteria
- Config file loads without errors
- All volume_optimization parameters are parsed correctly
- Config validation passes for all fields

### Implementation Tasks
- [ ] T001 Add volume_optimization section to backend/config/agentic-vf-config.yaml with all Phase 1 and Phase 2 parameters
- [ ] T002 Verify volume_optimization config section is loaded in VolumeFarmEngine initialization
- [ ] T003 Add config validation to ensure all required fields are present when volume_optimization.enabled = true

---

## Phase 2: Foundational - Volume Optimization Config Loading

### Goal
Ensure VolumeFarmEngine properly loads and validates volume_optimization configuration.

### Independent Test Criteria
- VolumeFarmEngine initializes volume_optimization components when config enabled
- VolumeFarmEngine skips volume_optimization components when config disabled
- No nil pointer errors when accessing volume_optimization config

### Implementation Tasks
- [ ] T004 Modify VolumeFarmEngine.NewVolumeFarmEngine() to check volumeConfig.VolumeOptimization != nil before initialization
- [ ] T005 Add logging to confirm volume_optimization config is loaded or skipped
- [ ] T006 Add unit test for VolumeFarmEngine initialization with volume_optimization enabled
- [ ] T007 Add unit test for VolumeFarmEngine initialization with volume_optimization disabled

---

## Phase 3: User Story 1 - VPIN Monitor Integration

### Story Goal
Integrate VPIN Monitor into core trading flow to actually pause trading when toxic flow is detected.

### Independent Test Criteria
- VPIN Monitor pauses trading when VPIN threshold is breached
- VPIN Monitor auto-resumes trading after delay when VPIN normalizes
- CanPlaceOrder() returns false during toxic flow period
- CanPlaceOrder() returns true after auto-resume
- Trading pause flag is set and cleared correctly
- Pause reason is logged correctly

### Verification Tasks
- [ ] T008 Verify VPIN Monitor.IsToxic() is called in AdaptiveGridManager.CanPlaceOrder()
- [ ] T009 Verify vpinMonitor.TriggerPause() is called when IsToxic() returns true
- [ ] T010 Verify tradingPaused flag is set in AdaptiveGridManager when toxic
- [ ] T011 Verify pauseReason is set to "toxic_vpin" when toxic
- [ ] T012 Verify CanPlaceOrder() returns false when tradingPaused = true
- [ ] T013 Verify vpinMonitor.Resume() is called after auto_resume_delay
- [ ] T014 Verify tradingPaused flag is cleared after auto-resume
- [ ] T015 Verify pauseReason is cleared after auto-resume
- [ ] T016 Verify GetPauseStartTime() method exists in VPINMonitor
- [ ] T017 Verify GetAutoResumeDelay() method exists in VPINMonitor

### Implementation Tasks
- [ ] T018 Add tradingPaused bool field to AdaptiveGridManager struct in backend/internal/farming/adaptive_grid/manager.go
- [ ] T019 Add pauseReason string field to AdaptiveGridManager struct
- [ ] T020 Implement GetPauseStartTime() method in VPINMonitor in backend/internal/farming/volume_optimization/vpin_monitor.go
- [ ] T021 Implement GetAutoResumeDelay() method in VPINMonitor
- [ ] T022 Modify AdaptiveGridManager.CanPlaceOrder() around line 3184 to add actual pause trigger logic
- [ ] T023 Add type assertion to convert vpinMonitor interface to *volume_optimization.VPINMonitor
- [ ] T024 Add vpinMonitor.TriggerPause() call when IsToxic() returns true
- [ ] T025 Add tradingPaused = true and pauseReason = "toxic_vpin" assignment
- [ ] T026 Add auto-resume logic to check if vpinMonitor.IsPaused()
- [ ] T027 Add auto-resume delay check using time.Since(vpinMonitor.GetPauseStartTime())
- [ ] T028 Add vpinMonitor.Resume() call when delay passed
- [ ] T029 Add tradingPaused = false and pauseReason = "" assignment after resume
- [ ] T030 Add logging for toxic flow detection with VPIN value
- [ ] T031 Add logging for auto-resume with delay duration
- [ ] T032 Add unit test for VPIN pause trigger in CanPlaceOrder
- [ ] T033 Add unit test for VPIN auto-resume in CanPlaceOrder
- [ ] T034 Add integration test for end-to-end VPIN pause/resume flow

---

## Phase 4: User Story 2 - TickSize Manager Integration

### Story Goal
Integrate TickSize Manager into GridManager to round all grid levels to valid tick sizes.

### Independent Test Criteria
- TickSizeManager is passed from VolumeFarmEngine to GridManager
- GridManager stores TickSizeManager reference
- calculateGridLevels() uses TickSizeManager to round prices
- All grid levels are on valid tick sizes for the symbol
- No order rejections due to invalid tick size

### Verification Tasks
- [ ] T035 Verify engine.gridManager.SetTickSizeManager(tickSizeMgr) is called in VolumeFarmEngine
- [ ] T036 Verify GridManager has tickSizeManager field
- [ ] T037 Verify SetTickSizeManager() method exists in GridManager
- [ ] T038 Verify tickSizeManager is not nil in GridManager after initialization
- [ ] T039 Verify calculateGridLevels() calls tickSizeManager.RoundToTickForSymbol()
- [ ] T040 Verify grid levels are rounded to valid ticks
- [ ] T041 Verify rounding happens for all grid levels (buy and sell sides)

### Implementation Tasks
- [ ] T042 Add tickSizeManager *volume_optimization.TickSizeManager field to GridManager struct in backend/internal/farming/grid_manager.go
- [ ] T043 Implement SetTickSizeManager(tickSizeMgr *volume_optimization.TickSizeManager) method in GridManager
- [ ] T044 Add logging in SetTickSizeManager() to confirm wiring
- [ ] T045 Modify VolumeFarmEngine.NewVolumeFarmEngine() after line 360 to call engine.gridManager.SetTickSizeManager(tickSizeMgr)
- [ ] T046 Locate calculateGridLevels() method in GridManager
- [ ] T047 Add tickSizeManager nil check in calculateGridLevels()
- [ ] T048 Add tickSizeManager.RoundToTickForSymbol(symbol, price) call for each grid level
- [ ] T049 Replace original price with rounded price in levels array
- [ ] T050 Add logging for tick-size rounding with symbol, original price, rounded price
- [ ] T051 Add unit test for calculateGridLevels() with TickSizeManager
- [ ] T052 Add unit test for calculateGridLevels() without TickSizeManager
- [ ] T053 Add unit test for tick-size rounding with various tick sizes
- [ ] T054 Add integration test for grid placement with tick-size rounding

---

## Phase 5: User Story 3 - PostOnly Handler Integration

### Story Goal
Integrate PostOnly Handler into GridManager to set post-only flag on all grid orders with proper retry logic.

### Independent Test Criteria
- PostOnlyHandler is stored in VolumeFarmEngine (not discarded)
- PostOnlyHandler is passed from VolumeFarmEngine to GridManager
- GridManager stores PostOnlyHandler reference
- PlaceGridOrder() uses PostOnlyHandler when enabled
- Post-only flag is set on all grid orders
- Post-only rejections are handled with retry logic
- Fallback to regular limit orders after max retries

### Verification Tasks
- [ ] T055 Verify engine.postOnlyHandler is assigned (not discarded) in VolumeFarmEngine
- [ ] T056 Verify engine.postOnlyHandler field exists in VolumeFarmEngine struct
- [ ] T057 Verify engine.gridManager.SetPostOnlyHandler(engine.postOnlyHandler) is called
- [ ] T058 Verify GridManager has postOnlyHandler field
- [ ] T059 Verify SetPostOnlyHandler() method exists in GridManager
- [ ] T060 Verify postOnlyHandler is not nil in GridManager after initialization
- [ ] T061 Verify PlaceGridOrder() checks postOnlyHandler.IsEnabled()
- [ ] T062 Verify PlaceGridOrder() calls postOnlyHandler.PlaceOrderWithPostOnly()
- [ ] T063 Verify post-only flag is passed to order placement
- [ ] T064 Verify fallback to regular order placement exists

### Implementation Tasks
- [ ] T065 Add postOnlyHandler *volume_optimization.PostOnlyHandler field to VolumeFarmEngine struct in backend/internal/farming/volume_farm_engine.go
- [ ] T066 Modify VolumeFarmEngine.NewVolumeFarmEngine() line 392 to remove `_` and assign to engine.postOnlyHandler
- [ ] T067 Add engine.gridManager.SetPostOnlyHandler(engine.postOnlyHandler) call after handler initialization
- [ ] T068 Add logging to confirm PostOnlyHandler is wired to GridManager
- [ ] T069 Add postOnlyHandler *volume_optimization.PostOnlyHandler field to GridManager struct in backend/internal/farming/grid_manager.go
- [ ] T070 Implement SetPostOnlyHandler(handler *volume_optimization.PostOnlyHandler) method in GridManager
- [ ] T071 Add logging in SetPostOnlyHandler() to confirm wiring
- [ ] T072 Locate PlaceGridOrder() method in GridManager
- [ ] T073 Add postOnlyHandler nil check in PlaceGridOrder()
- [ ] T074 Add postOnly := false initialization
- [ ] T075 Add postOnly = true if postOnlyHandler.IsEnabled() returns true
- [ ] T076 Add postOnlyHandler.PlaceOrderWithPostOnly() call with order placement function
- [ ] T077 Implement order placement function that calls futuresClient.PlaceOrder() with postOnly flag
- [ ] T078 Add error handling for post-only order placement
- [ ] T079 Add fallback to regular order placement if postOnlyHandler is nil or disabled
- [ ] T080 Add logging for post-only order attempts with postOnly flag value
- [ ] T081 Add unit test for PlaceGridOrder() with PostOnlyHandler enabled
- [ ] T082 Add unit test for PlaceGridOrder() with PostOnlyHandler disabled
- [ ] T083 Add unit test for PostOnlyHandler retry logic
- [ ] T084 Add integration test for order placement with post-only flag

---

## Phase 6: User Story 4 - Smart Cancellation

### Story Goal
Create and integrate Smart Cancellation component to monitor spread changes and trigger grid rebuilds.

### Independent Test Criteria
- SmartCancellation component is created
- SmartCancellation monitors spread changes at configured interval
- SmartCancellation triggers grid rebuild when spread changes > threshold
- Grid rebuild completes within 1 second
- No race conditions during rebuild
- Spread change is logged with old and new values

### Verification Tasks
- [ ] T085 Verify smart_cancellation.go file exists
- [ ] T086 Verify SmartCancellation struct exists with all required fields
- [ ] T087 Verify NewSmartCancellation() function exists
- [ ] T088 Verify Start() method exists and starts monitoring goroutine
- [ ] T089 Verify Stop() method exists and stops monitoring goroutine
- [ ] T090 Verify UpdateSpread() method exists and detects changes
- [ ] T091 Verify SetOnSpreadChangeCallback() method exists
- [ ] T092 Verify engine.smartCancellation field exists in VolumeFarmEngine
- [ ] T093 Verify engine.smartCancellation is initialized in VolumeFarmEngine
- [ ] T094 Verify callback triggers grid rebuild on spread change
- [ ] T095 Verify grid rebuild is called with correct symbol

### Implementation Tasks
- [ ] T096 Create backend/internal/farming/volume_optimization/smart_cancellation.go file
- [ ] T097 Define SmartCancellation struct with enabled, spreadChangeThreshold, checkInterval, lastSpread, onSpreadChange, stopCh, mu, logger fields
- [ ] T098 Define SmartCancelConfig struct with Enabled, SpreadChangeThreshold, CheckInterval fields
- [ ] T099 Implement NewSmartCancellation(config SmartCancelConfig, logger *zap.Logger) function
- [ ] T100 Initialize lastSpread map in constructor
- [ ] T101 Initialize stopCh channel in constructor
- [ ] T102 Implement SetOnSpreadChangeCallback(fn func(symbol string)) method with mutex lock
- [ ] T103 Implement Start() method with ticker loop
- [ ] T104 Add enabled check in Start() method
- [ ] T105 Add ticker.NewTicker(checkInterval) in Start()
- [ ] T106 Add select loop for ticker and stopCh in Start()
- [ ] T107 Add call to checkSpreadChanges() in ticker case
- [ ] T108 Add "Smart cancellation stopped" log in stopCh case
- [ ] T109 Implement Stop() method that closes stopCh
- [ ] T110 Implement UpdateSpread(symbol, spread float64) method with mutex lock
- [ ] T111 Add lastSpread lookup in UpdateSpread()
- [ ] T112 Add changePct calculation using math.Abs(spread-lastSpread) / lastSpread
- [ ] T113 Add threshold check using changePct > spreadChangeThreshold
- [ ] T114 Add logging for spread change detection
- [ ] T115 Add callback invocation when threshold exceeded
- [ ] T116 Update lastSpread[symbol] = spread
- [ ] T117 Implement checkSpreadChanges() method (placeholder for order book fetch)
- [ ] T118 Add smartCancellation *volume_optimization.SmartCancellation field to VolumeFarmEngine struct
- [ ] T119 Modify VolumeFarmEngine.NewVolumeFarmEngine() to initialize SmartCancellation
- [ ] T120 Read config from volumeConfig.VolumeOptimization.MakerTaker.SmartCancellation
- [ ] T121 Create smartCancelConfig struct from config values
- [ ] T122 Call volume_optimization.NewSmartCancellation(smartCancelConfig, logger)
- [ ] T123 Set callback to trigger grid rebuild with engine.gridManager.RebuildGrid(ctx, symbol)
- [ ] T124 Add error handling for grid rebuild failure with logging
- [ ] T125 Assign to engine.smartCancellation
- [ ] T126 Start monitoring goroutine with go smartCancellation.Start()
- [ ] T127 Add logging for SmartCancellation initialization and start
- [ ] T128 Add unit test for SmartCancellation spread change detection
- [ ] T129 Add unit test for SmartCancellation callback invocation
- [ ] T130 Add unit test for SmartCancellation Start() and Stop()
- [ ] T131 Add integration test for SmartCancellation with GridManager rebuild
- [ ] T132 Create backend/internal/farming/volume_optimization/smart_cancellation_test.go

---

## Phase 7: User Story 5 - Advanced Features (Optional)

### Story Goal
Implement Penny Jumping and Inventory Hedging features (requires backtesting before production use).

### Independent Test Criteria
- Penny Jumping component is created (disabled by default)
- Inventory Hedging component is created (disabled by default)
- Both features can be enabled via config
- Both features have proper error handling
- Both features have extensive logging

### Verification Tasks
- [ ] T133 Verify penny_jumping.go file exists
- [ ] T134 Verify inventory_hedging.go file exists
- [ ] T135 Verify both features are disabled by default in config
- [ ] T136 Verify both features can be enabled via config

### Implementation Tasks
- [ ] T137 Create backend/internal/farming/volume_optimization/penny_jumping.go file
- [ ] T138 Define PennyJumping struct with monitoring logic
- [ ] T139 Add order book monitoring for best bid/ask
- [ ] T140 Add price calculation: best bid + 1 tick (buy) or best ask - 1 tick (sell)
- [ ] T141 Add spread limit check (10% of spread)
- [ ] T142 Add max jump limit check
- [ ] T143 Add logging for penny jumping decisions
- [ ] T144 Create backend/internal/farming/volume_optimization/inventory_hedging.go file
- [ ] T145 Define InventoryHedging struct with monitoring logic
- [ ] T146 Add inventory percentage calculation
- [ ] T147 Add hedge threshold check (default 30%)
- [ ] T148 Add hedge size calculation: |inventory| * hedge_ratio
- [ ] T149 Add max hedge size limit
- [ ] T150 Add internal hedging mode (same symbol, opposite side)
- [ ] T151 Add logging for inventory skew warnings
- [ ] T152 Add logging for hedge order executions
- [ ] T153 Add unit tests for Penny Jumping
- [ ] T154 Add unit tests for Inventory Hedging
- [ ] T155 Add backtesting documentation for both features
- [ ] T156 Add config documentation for both features

---

## Phase 8: Polish & Cross-Cutting Concerns

### Goal
Add comprehensive testing, documentation, and monitoring for all volume optimization features.

### Independent Test Criteria
- All components have unit tests
- All integrations have integration tests
- All features have documentation
- All features have monitoring metrics
- All features have alerting rules

### Implementation Tasks
- [ ] T157 Create backend/internal/farming/volume_optimization/integration_test.go
- [ ] T158 Add integration test for VPIN Monitor end-to-end flow
- [ ] T159 Add integration test for TickSize Manager end-to-end flow
- [ ] T160 Add integration test for PostOnly Handler end-to-end flow
- [ ] T161 Add integration test for Smart Cancellation end-to-end flow
- [ ] T162 Add dry-run mode test for all Phase 1 features
- [ ] T163 Add monitoring metrics for VPIN Monitor (VPIN values, toxic flow count, pause duration)
- [ ] T164 Add monitoring metrics for TickSize Manager (rounding frequency, invalid tick warnings)
- [ ] T165 Add monitoring metrics for PostOnly Handler (order count, rejection rate, retry success)
- [ ] T166 Add monitoring metrics for Smart Cancellation (spread change count, rebuild count, rebuild time)
- [ ] T167 Add critical alert for VPIN toxic flow (high frequency)
- [ ] T168 Add critical alert for trading pause duration > 30 minutes
- [ ] T169 Add critical alert for post-only rejection rate > 50%
- [ ] T170 Add critical alert for grid rebuild failure rate > 10%
- [ ] T171 Add warning alert for VPIN approaching threshold
- [ ] T172 Add warning alert for tick-size warnings for unknown symbols
- [ ] T173 Add warning alert for smart cancellation rebuild frequency > 5/hour
- [ ] T174 Add enhanced logging for VPIN (every calculation, threshold breach, pause/resume)
- [ ] T175 Add enhanced logging for TickSize (every rounding, tick-size lookup)
- [ ] T176 Add enhanced logging for PostOnly (every attempt, rejection, retry, fallback)
- [ ] T177 Add enhanced logging for SmartCancellation (every spread change, rebuild trigger, rebuild result)
- [ ] T178 Update README.md with volume optimization features documentation
- [ ] T179 Update agentic-vf-config.yaml with volume_optimization section comments
- [ ] T180 Create VOLUME_OPTIMIZATION_IMPLEMENTATION.md with implementation details

---

## Dependencies

### Story Completion Order
1. **Phase 1 & 2** (Setup & Foundational) - MUST complete first
2. **Phase 3** (VPIN Monitor) - Independent of other stories
3. **Phase 4** (TickSize Manager) - Independent of other stories
4. **Phase 5** (PostOnly Handler) - Independent of other stories
5. **Phase 6** (Smart Cancellation) - Depends on GridManager rebuild capability
6. **Phase 7** (Advanced Features) - Independent, optional
7. **Phase 8** (Polish) - Depends on all previous phases

### Parallel Execution Opportunities

**Within Phase 3 (VPIN Monitor):**
- T018-T021 can be parallel (different files)
- T032-T034 can be parallel (different test files)

**Within Phase 4 (TickSize Manager):**
- T042-T044 can be parallel (different methods in same file)
- T051-T054 can be parallel (different test files)

**Within Phase 5 (PostOnly Handler):**
- T065-T068 can be parallel (VolumeFarmEngine file)
- T069-T071 can be parallel (GridManager file)
- T081-T084 can be parallel (different test files)

**Within Phase 6 (Smart Cancellation):**
- T096-T117 can be parallel (component implementation)
- T118-T127 can be parallel (integration)
- T128-T132 can be parallel (different test files)

**Within Phase 7 (Advanced Features):**
- T137-T143 can be parallel (Penny Jumping)
- T144-T152 can be parallel (Inventory Hedging)
- T153-T156 can be parallel (tests and docs)

---

## Implementation Strategy

### MVP Scope
**MVP includes Phases 1, 2, 3, 4, 5** - Fix all Phase 1 integration issues to make bot truly agentic.

This ensures:
- VPIN Monitor actually pauses trading (not just checks)
- TickSize Manager rounds grid levels (not just exists)
- PostOnly Handler sets post-only flag (not just created)
- Config is properly loaded and validated
- All components are wired and used in core logic

### Incremental Delivery
1. **First Increment:** Phases 1 & 2 (Setup & Foundational) - 1 day
2. **Second Increment:** Phase 3 (VPIN Monitor) - 1 day
3. **Third Increment:** Phase 4 (TickSize Manager) - 1 day
4. **Fourth Increment:** Phase 5 (PostOnly Handler) - 1 day
5. **Fifth Increment:** Phase 6 (Smart Cancellation) - 1-2 days
6. **Sixth Increment:** Phase 7 (Advanced Features) - 3-5 days (optional)
7. **Final Increment:** Phase 8 (Polish) - 1-2 days

### Risk Mitigation
- Each phase has independent test criteria
- Each phase can be deployed independently
- Rollback plan exists for each phase
- Dry-run mode testing before live deployment
- Extensive logging for debugging
- Feature flags for easy enable/disable

---

## Summary

- **Total Task Count:** 180 tasks
- **Phase 1 (Setup):** 3 tasks
- **Phase 2 (Foundational):** 4 tasks
- **Phase 3 (VPIN Monitor):** 27 tasks (8 verification + 19 implementation)
- **Phase 4 (TickSize Manager):** 20 tasks (7 verification + 13 implementation)
- **Phase 5 (PostOnly Handler):** 20 tasks (10 verification + 10 implementation)
- **Phase 6 (Smart Cancellation):** 37 tasks (10 verification + 27 implementation)
- **Phase 7 (Advanced Features):** 24 tasks (4 verification + 20 implementation)
- **Phase 8 (Polish):** 24 tasks

**Parallel Opportunities Identified:** 7 groups of parallelizable tasks across phases

**Independent Test Criteria:** Each user story phase has clear independent test criteria to verify true integration

**MVP Scope:** Phases 1-5 (74 tasks) - Fix all Phase 1 integration issues to make bot truly agentic

**Suggested First Phase:** Start with Phase 1 (Setup) to add config, then Phase 2 (Foundational) to ensure config loading, then proceed with Phase 3 (VPIN Monitor) as the first user story integration.
- [ ] Implement NewVPINMonitor constructor
- [ ] Implement UpdateVolume(buyVol, sellVol) method
- [ ] Implement CalculateVPIN() method
- [ ] Implement IsToxic() method with threshold check
- [ ] Add sustained breach detection (N consecutive breaches)
- [ ] Add auto-resume logic with configurable delay

**Acceptance Criteria**:
- VPIN calculation is accurate (|Buy - Sell| / (Buy + Sell))
- Sliding window works correctly
- Toxic detection triggers at correct threshold
- Auto-resume works after VPIN normalizes

**Dependencies**:
- Config for window size, bucket size, threshold

**Tests**:
- Unit test: VPIN calculation accuracy
- Unit test: sliding window behavior
- Unit test: toxic detection
- Unit test: sustained breach logic
- Unit test: auto-resume logic

---

### T003: Integrate VPIN Monitor with Grid Manager
**Priority**: High
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Integrate VPINMonitor into Grid Manager to pause orders during toxic flow conditions.

**Tasks**:
- [ ] Add VPINMonitor field to GridManager struct
- [ ] Add SetVPINMonitor() setter method
- [ ] Update VPIN on order fills in OnOrderUpdate()
- [ ] Add VPIN check in CanPlaceOrder()
- [ ] Log VPIN breach events
- [ ] Add config loading for VPIN parameters

**Acceptance Criteria**:
- VPIN updates on every order fill
- CanPlaceOrder returns false when toxic
- VPIN breaches are logged with context
- Auto-resume works correctly

**Dependencies**:
- T002 (VPIN Monitor)
- Grid Manager modifications

**Tests**:
- Integration test: VPIN update on order fill
- Integration test: CanPlaceOrder blocks on toxic flow
- Integration test: Auto-resume behavior

---

### T004: Implement Post-Only Order Support
**Priority**: High
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Add post-only flag support to order placement to ensure orders only fill as maker orders.

**Tasks**:
- [ ] Add postOnly parameter to PlaceGridOrder() method
- [ ] Add post-only flag to Order struct
- [ ] Implement handlePostOnlyRejection() method
- [ ] Add retry logic for post-only rejections (max 3 retries)
- [ ] Add fallback to regular limit order after retries
- [ ] Log post-only rejections with reason
- [ ] Add config for post-only enabled/disabled

**Acceptance Criteria**:
- Post-only flag is set on all grid orders when enabled
- Rejections are handled gracefully
- Retry logic works correctly
- Fallback to regular limit after max retries
- <5% of orders fill as taker

**Dependencies**:
- Exchange Client API support for post-only flag

**Tests**:
- Unit test: post-only order placement
- Unit test: rejection handling
- Unit test: retry logic
- Unit test: fallback behavior

---

### T005: Add Volume Optimization Config
**Priority**: High
**Estimated**: 1 hour
**Status**: Pending

**Description**:
Create configuration structures and YAML config for volume optimization features.

**Tasks**:
- [ ] Create file: `backend/config/volume_optimization_config.go`
- [ ] Implement VolumeOptimizationConfig struct
- [ ] Implement OrderPriorityConfig struct
- [ ] Implement ToxicFlowConfig struct
- [ ] Implement MakerTakerConfig struct
- [ ] Implement InventoryHedgeConfig struct
- [ ] Add config section to agentic-vf-config.yaml
- [ ] Add config loading in VolumeFarmEngine

**Acceptance Criteria**:
- All config structs are defined
- YAML config is valid
- Config loads correctly
- All features can be enabled/disabled via config

**Dependencies**:
- Existing Config Manager

**Tests**:
- Unit test: config struct parsing
- Unit test: config loading
- Unit test: default values

---

### T006: Initialize Volume Optimization Components in VolumeFarmEngine
**Priority**: High
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Initialize all volume optimization components in VolumeFarmEngine and wire them to Grid Manager.

**Tasks**:
- [ ] Initialize TickSizeManager in VolumeFarmEngine
- [ ] Initialize VPINMonitor in VolumeFarmEngine
- [ ] Pass TickSizeManager to Grid Manager
- [ ] Pass VPINMonitor to Grid Manager
- [ ] Load volume optimization config
- [ ] Set post-only config on Grid Manager
- [ ] Add logging for component initialization

**Acceptance Criteria**:
- All components initialize correctly
- Components are passed to Grid Manager
- Config loads correctly
- Initialization is logged

**Dependencies**:
- T001 (TickSize Manager)
- T002 (VPIN Monitor)
- T005 (Config)

**Tests**:
- Integration test: component initialization
- Integration test: component wiring

---

## Phase 2: Medium Priority Implementation

### T007: Create Smart Cancellation Component
**Priority**: Medium
**Estimated**: 3 hours
**Status**: Pending

**Description**:
Create SmartCancellation component to monitor spread changes and trigger grid rebuilds.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/smart_cancellation.go`
- [ ] Implement SmartCancellation struct
- [ ] Implement NewSmartCancellation constructor
- [ ] Implement Start() goroutine for periodic checks
- [ ] Implement ShouldCancel(symbol) method
- [ ] Implement CancelAndReplace(symbol) method
- [ ] Add spread change detection logic
- [ ] Add grid rebuild trigger

**Acceptance Criteria**:
- Spread monitoring runs at configured interval
- Cancellation triggers at correct threshold
- Grid rebuilds within 1 second of trigger
- Goroutine stops on context cancellation

**Dependencies**:
- Grid Manager interface
- Config for check interval, threshold

**Tests**:
- Unit test: spread change detection
- Unit test: cancellation trigger
- Unit test: grid rebuild
- Unit test: goroutine lifecycle

---

### T008: Integrate Smart Cancellation with VolumeFarmEngine
**Priority**: Medium
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Integrate SmartCancellation into VolumeFarmEngine to monitor spreads and rebuild grids.

**Tasks**:
- [ ] Initialize SmartCancellation in VolumeFarmEngine
- [ ] Start SmartCancellation goroutine
- [ ] Pass Grid Manager to SmartCancellation
- [ ] Load smart cancellation config
- [ ] Add logging for cancellation events
- [ ] Handle goroutine shutdown on engine stop

**Acceptance Criteria**:
- SmartCancellation starts correctly
- Monitors spreads at configured interval
- Rebuilds grid on spread change
- Stops cleanly on engine shutdown

**Dependencies**:
- T007 (Smart Cancellation)
- VolumeFarmEngine modifications

**Tests**:
- Integration test: smart cancellation lifecycle
- Integration test: spread change trigger
- Integration test: grid rebuild

---

### T009: Integrate Tick-size with Grid Level Calculation
**Priority**: Medium
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Modify Grid Manager to use TickSizeManager when calculating grid levels.

**Tasks**:
- [ ] Add tickSizeManager field to GridManager
- [ ] Add SetTickSizeManager() setter method
- [ ] Modify calculateGridLevels() to use tick-size rounding
- [ ] Round all grid levels to valid ticks
- [ ] Add logging for tick-size usage
- [ ] Handle unknown tick-size gracefully

**Acceptance Criteria**:
- All grid levels are on valid ticks
- Rounding is accurate
- Unknown tick-size doesn't crash system
- Tick-size warnings are logged

**Dependencies**:
- T001 (TickSize Manager)
- Grid Manager modifications

**Tests**:
- Unit test: grid level rounding
- Unit test: tick-size fallback
- Unit test: invalid tick handling

---

## Phase 3: Advanced Implementation (Optional)

### T010: Create Penny Jumping Strategy
**Priority**: Low (Optional)
**Estimated**: 4 hours
**Status**: Pending

**Description**:
Create PennyJumpingStrategy to place orders 1 tick above/below best bid/ask for priority fills.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/penny_jumping.go`
- [ ] Implement PennyJumpingStrategy struct
- [ ] Implement NewPennyJumpingStrategy constructor
- [ ] Implement CalculateOptimalPrice() method
- [ ] Add jump threshold enforcement
- [ ] Add max jump constraint
- [ ] Add order book monitoring
- [ ] Add spread limit enforcement

**Acceptance Criteria**:
- Optimal price is 1 tick from best bid/ask
- Price stays within spread limit
- Max jump is enforced
- Order book monitoring works correctly

**Dependencies**:
- TickSizeManager
- Order book API

**Tests**:
- Unit test: price calculation
- Unit test: spread limit enforcement
- Unit test: max jump constraint
- Unit test: order book monitoring

---

### T011: Integrate Penny Jumping with Order Placement
**Priority**: Low (Optional)
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Integrate PennyJumpingStrategy into Grid Manager to use optimized prices.

**Tasks**:
- [ ] Add PennyJumpingStrategy field to GridManager
- [ ] Add SetPennyJumpingStrategy() setter method
- [ ] Modify PlaceGridOrder() to use penny jumping
- [ ] Monitor spread impact
- [ ] Add logging for penny jumping
- [ ] Add config for penny jumping enabled/disabled

**Acceptance Criteria**:
- Penny jumping is used when enabled
- Spread doesn't decrease by >5%
- Fill rate increases by 20-30%
- Penny jumping can be disabled via config

**Dependencies**:
- T010 (Penny Jumping)
- Grid Manager modifications

**Tests**:
- Integration test: penny jumping integration
- Integration test: spread impact monitoring
- Integration test: config toggle

---

### T012: Create Inventory Hedging Component
**Priority**: Low (Optional)
**Estimated**: 5 hours
**Status**: Pending

**Description**:
Create InventoryHedging component to monitor inventory and execute hedge orders.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/inventory_hedging.go`
- [ ] Implement InventoryHedging struct
- [ ] Implement NewInventoryHedging constructor
- [ ] Implement ShouldHedge(symbol) method
- [ ] Implement ExecuteHedge(symbol) method
- [ ] Implement CalculateHedgeSize(symbol) method
- [ ] Add internal hedging mode
- [ ] Add cross-pair hedging mode
- [ ] Add scalping hedging mode
- [ ] Add hedge PnL monitoring

**Acceptance Criteria**:
- Inventory monitoring is accurate
- Hedge triggers at correct threshold
- Hedge size is calculated correctly
- All hedging modes work
- Hedge PnL is tracked

**Dependencies**:
- Risk Monitor
- Exchange Client
- Config

**Tests**:
- Unit test: inventory monitoring
- Unit test: hedge calculation
- Unit test: internal hedging
- Unit test: cross-pair hedging
- Unit test: scalping hedging
- Unit test: PnL tracking

---

### T013: Integrate Inventory Hedging with VolumeFarmEngine
**Priority**: Low (Optional)
**Estimated**: 3 hours
**Status**: Pending

**Description**:
Integrate InventoryHedging into VolumeFarmEngine to monitor and hedge inventory.

**Tasks**:
- [ ] Initialize InventoryHedging in VolumeFarmEngine
- [ ] Start inventory monitoring goroutine
- [ ] Pass Risk Monitor to InventoryHedging
- [ ] Pass Exchange Client to InventoryHedging
- [ ] Load inventory hedging config
- [ ] Add logging for hedge events
- [ ] Add manual hedge trigger
- [ ] Add hedge disable capability

**Acceptance Criteria**:
- Inventory hedging starts correctly
- Monitors inventory at configured interval
- Executes hedge on threshold breach
- Can be triggered manually
- Can be disabled via config

**Dependencies**:
- T012 (Inventory Hedging)
- VolumeFarmEngine modifications

**Tests**:
- Integration test: inventory hedging lifecycle
- Integration test: hedge trigger
- Integration test: manual trigger
- Integration test: disable capability

---

## Testing Tasks

### T014: Write Unit Tests for TickSizeManager
**Priority**: High
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Write comprehensive unit tests for TickSizeManager component.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/tick_size_manager_test.go`
- [ ] Test tick-size fetching
- [ ] Test cache behavior
- [ ] Test rounding logic
- [ ] Test refresh goroutine
- [ ] Test error handling
- [ ] Test concurrent access (race detector)

---

### T015: Write Unit Tests for VPINMonitor
**Priority**: High
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Write comprehensive unit tests for VPINMonitor component.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/vpin_monitor_test.go`
- [ ] Test VPIN calculation accuracy
- [ ] Test sliding window behavior
- [ ] Test toxic detection
- [ ] Test sustained breach logic
- [ ] Test auto-resume logic
- [ ] Test concurrent access (race detector)

---

### T016: Write Unit Tests for SmartCancellation
**Priority**: Medium
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Write comprehensive unit tests for SmartCancellation component.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/smart_cancellation_test.go`
- [ ] Test spread change detection
- [ ] Test cancellation trigger
- [ ] Test grid rebuild
- [ ] Test goroutine lifecycle
- [ ] Test concurrent access (race detector)

---

### T017: Write Integration Tests for Volume Optimization
**Priority**: High
**Estimated**: 4 hours
**Status**: Pending

**Description**:
Write end-to-end integration tests for volume optimization features.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/integration_test.go`
- [ ] Create MockExchangeClient
- [ ] Test post-only order placement
- [ ] Test VPIN trigger and pause
- [ ] Test smart cancellation and rebuild
- [ ] Test tick-size integration
- [ ] Test full workflow with all features

---

### T018: Write Unit Tests for PennyJumping
**Priority**: Low (Optional)
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Write comprehensive unit tests for PennyJumpingStrategy component.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/penny_jumping_test.go`
- [ ] Test price calculation
- [ ] Test spread limit enforcement
- [ ] Test max jump constraint
- [ ] Test order book monitoring
- [ ] Test concurrent access (race detector)

---

### T019: Write Unit Tests for InventoryHedging
**Priority**: Low (Optional)
**Estimated**: 3 hours
**Status**: Pending

**Description**:
Write comprehensive unit tests for InventoryHedging component.

**Tasks**:
- [ ] Create file: `backend/internal/farming/volume_optimization/inventory_hedging_test.go`
- [ ] Test inventory monitoring
- [ ] Test hedge calculation
- [ ] Test internal hedging
- [ ] Test cross-pair hedging
- [ ] Test scalping hedging
- [ ] Test PnL tracking
- [ ] Test concurrent access (race detector)

---

## Documentation Tasks

### T020: Update README with Volume Optimization Features
**Priority**: Medium
**Estimated**: 1 hour
**Status**: Pending

**Description**:
Update project README to document new volume optimization features.

**Tasks**:
- [ ] Add volume optimization section
- [ ] Document each feature
- [ ] Provide configuration examples
- [ ] Add usage guidelines
- [ ] Add troubleshooting section

---

### T021: Create Volume Optimization Guide
**Priority**: Medium
**Estimated**: 2 hours
**Status**: Pending

**Description**:
Create detailed guide for using volume optimization features.

**Tasks**:
- [ ] Create file: `backend/docs/volume_optimization_guide.md`
- [ ] Document tick-size awareness
- [ ] Document VPIN detection
- [ ] Document post-only orders
- [ ] Document smart cancellation
- [ ] Document penny jumping
- [ ] Document inventory hedging
- [ ] Provide tuning guidelines
- [ ] Add FAQ section

---

## Deployment Tasks

### T022: Create Deployment Checklist
**Priority**: High
**Estimated**: 1 hour
**Status**: Pending

**Description**:
Create checklist for deploying volume optimization features.

**Tasks**:
- [ ] Create file: `backend/deploy/volume_optimization_checklist.md`
- [ ] Add pre-deployment checks
- [ ] Add deployment steps
- [ ] Add post-deployment verification
- [ ] Add rollback procedures
- [ ] Add monitoring checklist

---

### T023: Create Monitoring Dashboard
**Priority**: Medium
**Estimated**: 3 hours
**Status**: Pending

**Description**:
Create monitoring dashboard for volume optimization metrics.

**Tasks**:
- [ ] Add VPIN chart
- [ ] Add fill rate chart
- [ ] Add taker fee ratio chart
- [ ] Add inventory skew chart
- [ ] Add hedge execution chart
- [ ] Add alert configuration

---

## Task Dependencies

```
Phase 1:
T001 (TickSizeManager) → T006 (Initialize) → T009 (Integrate)
T002 (VPINMonitor) → T003 (Integrate) → T006 (Initialize)
T004 (Post-Only) → T006 (Initialize)
T005 (Config) → T006 (Initialize)

Phase 2:
T007 (SmartCancellation) → T008 (Integrate)
T009 (Tick-size Integration) → T008 (Integrate)

Phase 3:
T010 (PennyJumping) → T011 (Integrate)
T012 (InventoryHedging) → T013 (Integrate)

Testing:
T001 → T014
T002 → T015
T007 → T016
T010 → T018
T012 → T019
All Phase 1/2 → T017
```

---

## Total Estimated Time

- **Phase 1**: 12 hours
- **Phase 2**: 7 hours
- **Phase 3**: 14 hours (optional)
- **Testing**: 15 hours
- **Documentation**: 3 hours
- **Deployment**: 4 hours

**Total**: ~55 hours (without Phase 3: ~41 hours)
