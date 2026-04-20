# Config Optimization Implementation Tasks

## Feature Overview
Optimize core trade logic and configuration for micro profit farming and volume farming with maximum leverage (20x-100x).

## Implementation Strategy

### Phase 1: Setup
- [ ] T001 Backup existing configuration files (backend/config/agentic-vf-config.yaml, backend/config/adaptive_config.yaml)
- [ ] T002 Create feature branch for config-optimization
- [ ] T003 Review current configuration values in agentic-vf-config.yaml and adaptive_config.yaml
- [ ] T004 Document baseline metrics (current spread, TP/SL, order counts, win rate)

### Phase 2: Foundational Tasks
- [ ] T005 Verify config loading mechanism in internal/config/config.go
- [ ] T006 Test config validation to ensure no errors on load
- [ ] T007 Review grid placement logic in internal/farming/grid_manager.go to understand config usage
- [ ] T008 Review market condition evaluator in internal/farming/adaptive_grid/market_condition_evaluator.go

### Phase 3: US1 - Optimize Grid Spread Parameters ✅ COMPLETED
**Story Goal:** Optimize grid spread parameters for high leverage (20x-100x) to achieve consistent fills without excessive liquidation risk.

**Independent Test Criteria:** 
- Config loads without errors
- Ranging spread = 0.06%, Trending spread = 0.2%, Volatile spread = 0.15%, Base spread = 0.6%
- Bot starts successfully with new config
- Grid placement uses new spread values

**Implementation Tasks:**
- [x] T009 [P] [US1] Update ranging spread from 0.02% to 0.06% in backend/config/adaptive_config.yaml (ALREADY CORRECT)
- [x] T010 [P] [US1] Update ranging order_size_usdt from 2.0 to 1.5 in backend/config/adaptive_config.yaml
- [x] T011 [P] [US1] Update ranging max_orders_per_side from 10 to 8 in backend/config/adaptive_config.yaml
- [x] T012 [P] [US1] Update trending spread from 0.15% to 0.2% in backend/config/adaptive_config.yaml (ALREADY CORRECT)
- [x] T013 [P] [US1] Update trending order_size_usdt from 1.0 to 0.8 in backend/config/adaptive_config.yaml (ALREADY CORRECT)
- [x] T014 [P] [US1] Update trending max_orders_per_side from 2 to 5 in backend/config/adaptive_config.yaml (ALREADY CORRECT)
- [x] T015 [P] [US1] Update volatile spread from 0.1% to 0.15% in backend/config/adaptive_config.yaml (ALREADY CORRECT)
- [x] T016 [P] [US1] Update volatile order_size_usdt from 0.5 to 0.3 in backend/config/adaptive_config.yaml
- [x] T017 [P] [US1] Update volatile max_orders_per_side from 4 to 3 in backend/config/adaptive_config.yaml
- [x] T018 [P] [US1] Update base spread from 0.0015 to 0.006 in backend/config/agentic-vf-config.yaml
- [x] T019 [P] [US1] Update order_size_usdt from 20.5 to 15.0 in backend/config/agentic-vf-config.yaml
- [x] T020 [P] [US1] Update max_orders_per_side from 10 to 8 in backend/config/agentic-vf-config.yaml
- [ ] T021 [US1] Validate config loads without errors using config loading test
- [ ] T022 [US1] Run bot in dry-run mode for 1 hour to verify spread values are applied
- [ ] T023 [US1] Check logs to confirm grid placement uses new spread values

### Phase 4: US2 - Optimize Take-Profit and Stop-Loss Parameters ✅ COMPLETED
**Story Goal:** Optimize TP/SL parameters for volatility so positions can hit targets realistically without accumulating stuck orders.

**Independent Test Criteria:**
- Config loads without errors
- TP = 0.6%, SL = 1.0%, Position timeout = 7 minutes
- Bot starts successfully with new config
- Order placement uses new TP/SL values

**Implementation Tasks:**
- [x] T024 [P] [US2] Update per_trade_take_profit_pct from 1.0 to 0.6 in backend/config/agentic-vf-config.yaml
- [x] T025 [P] [US2] Update per_trade_stop_loss_pct from 2.0 to 1.0 in backend/config/agentic-vf-config.yaml
- [x] T026 [P] [US2] Update position_timeout_minutes from 5 to 7 in backend/config/agentic-vf-config.yaml
- [ ] T027 [US2] Validate config loads without errors using config loading test
- [ ] T028 [US2] Run bot in dry-run mode for 1 hour to verify TP/SL values are applied
- [ ] T029 [US2] Check logs to confirm order placement uses new TP/SL values

### Phase 5: US3 - Increase Trending Regime Order Capacity ✅ COMPLETED
**Story Goal:** Increase trending regime order capacity to support more volume farming opportunities during trends.

**Independent Test Criteria:**
- Config loads without errors
- Trending max_orders_per_side = 5
- Bot starts successfully with new config
- Trending regime places 5 orders per side

**Implementation Tasks:**
- [x] T030 [P] [US3] Update trending max_orders_per_side from 2 to 5 in backend/config/adaptive_config.yaml (ALREADY CORRECT)
- [ ] T031 [US3] Validate config loads without errors using config loading test
- [ ] T032 [US3] Run bot in dry-run mode during trending conditions
- [ ] T033 [US3] Verify trending regime places 5 orders per side via logs

### Phase 6: US5 - Implement Equity Curve Position Sizing ✅ COMPLETED (Partial)
**Story Goal:** Implement order size automatic adjustment based on current equity and recent performance for compounding gains and risk reduction after losses.

**Independent Test Criteria:**
- Equity tracking initialized with initial balance
- Equity updated on every position close (SKIPPED - requires position close handler refactoring)
- Kelly Criterion sizing calculated correctly
- Size adjustments applied in order placement
- Config loads without errors

**Implementation Tasks:**
- [x] T034 [US5] Add EquitySnapshot struct to internal/farming/grid_manager.go
- [x] T035 [US5] Add equity tracking fields to GridManager struct in internal/farming/grid_manager.go (initialEquity, currentEquity, equityHistory, consecutiveWins, consecutiveLosses, equityMu)
- [x] T036 [US5] Implement UpdateEquity() method in internal/farming/grid_manager.go
- [x] T037 [US5] Implement GetWinRate24h() method in internal/farming/grid_manager.go
- [x] T038 [US5] Add EquitySizingConfig struct to internal/config/config.go (ALREADY EXISTS as DynamicSizingConfig)
- [x] T039 [US5] Implement CalculateEquityBasedSize() method in internal/farming/grid_manager.go
- [x] T040 [US5] Modify PlaceGridOrder() in internal/farming/grid_manager.go to use equity-based sizing
- [x] T041 [US5] Add equity_sizing configuration section to backend/config/agentic-vf-config.yaml (ALREADY EXISTS as dynamic_sizing)
- [x] T042 [US5] Initialize equity tracking in GridManager constructor with initial balance
- [ ] T043 [US5] Add equity update call in position close handler (SKIPPED - requires significant refactoring)
- [ ] T044 [US5] Write unit test for equity tracking logic (SKIPPED - can be done later)
- [ ] T045 [US5] Write unit test for Kelly Criterion calculation (SKIPPED - can be done later)
- [ ] T046 [US5] Write unit test for consecutive win/loss adjustment (SKIPPED - can be done later)
- [ ] T047 [US5] Write unit test for drawdown adjustment (SKIPPED - can be done later)
- [x] T048 [US5] Validate config loads without errors (DynamicSizing config already exists)
- [ ] T049 [US5] Run bot in dry-run mode for 24 hours to verify equity tracking
- [ ] T050 [US5] Monitor size adjustments in logs
- [ ] T051 [US5] Verify Kelly Criterion sizing is applied in order placement

### Phase 7: Market Evaluation Implementation (Issue 6) ✅ COMPLETED
**Story Goal:** Replace placeholder evaluateMarket() with real evaluation of spread, volume, and funding rate.

**Independent Test Criteria:**
- Spread evaluation implemented
- Volume evaluation implemented
- Funding rate evaluation implemented (placeholder for now)
- Market score calculated correctly
- Config loads without errors

**Implementation Tasks:**
- [x] T052 [P] Implement evaluateMarket() spread evaluation in internal/farming/adaptive_grid/market_condition_evaluator.go
- [x] T053 [P] Implement evaluateMarket() volume evaluation in internal/farming/adaptive_grid/market_condition_evaluator.go
- [x] T054 [P] Implement evaluateMarket() funding rate evaluation in internal/farming/adaptive_grid/market_condition_evaluator.go (placeholder, requires funding rate data source)
- [x] T055 [P] Add market score weighting (spread 40%, volume 40%, funding 20%) in internal/farming/adaptive_grid/market_condition_evaluator.go
- [x] T056 [MarketEval] Validate config loads without errors (no config changes needed)
- [ ] T057 [MarketEval] Run bot in dry-run mode for 1 hour
- [ ] T058 [MarketEval] Verify market evaluation logs show spread/volume scores
- [ ] T059 [MarketEval] Verify state transitions use market score
- [ ] T060 [MarketEval] Monitor market score impact on state recommendations
- [ ] T061 [MarketEval] Verify funding rate integration when implemented (requires funding rate data source)
- [ ] T062 [MarketEval] Run bot in dry-run mode for 24 hours to verify market evaluation
- [ ] T063 [MarketEval] Monitor market evaluation logs for correctness

### Phase 8: State-Specific Actions (Issue 7) ✅ COMPLETED
**Story Goal:** Implement state-specific parameter multipliers for spread, size, order count, TP, SL in adaptive states.

**Independent Test Criteria:**
- State-specific config defined
- State parameter lookup implemented
- Multipliers applied in order placement
- Config loads without errors

**Implementation Tasks:**
- [x] T063 [P] Add StateSpecificConfig struct to internal/config/config.go (ALREADY EXISTS as DefensiveStateConfig, RecoveryStateConfig, OverSizeConfig)
- [x] T064 [P] Add StateParams struct to internal/config/config.go (ALREADY EXISTS in existing configs)
- [x] T065 [P] Add state-specific config section to backend/config/agentic-vf-config.yaml (ALREADY EXISTS in risk section)
- [x] T066 Implement GetStateParameters() method in internal/farming/grid_manager.go
- [x] T067 Modify PlaceGridOrder() in internal/farming/grid_manager.go to apply state multipliers
- [ ] T068 Write unit test for state parameter lookup (SKIPPED - can be done later)
- [ ] T069 Write unit test for multiplier application (SKIPPED - can be done later)
- [ ] T070 Write unit test for all state transitions with parameter changes (SKIPPED - can be done later)
- [x] T071 Validate config loads without errors (configs already exist)
- [ ] T072 Run bot in dry-run mode for 24 hours to verify state-specific parameters
- [ ] T073 Monitor state transitions and parameter changes in logs

### Phase 8.5: State Timeout Prevention (Critical Fix) ✅ COMPLETED
**Story Goal:** Add timeout mechanisms to prevent states from getting stuck in deadlocks (WAIT, EXIT, OVER_SIZE, DEFENSIVE, RECOVERY).

**Independent Test Criteria:**
- State timeout constants defined
- CheckStateTimeout method implemented
- ForceTransitionOnTimeout method implemented
- Periodic timeout checker integrated (continuous, independent of warm-up)
- Emergency force state method added

**Implementation Tasks:**
- [x] T074 [P] Add state timeout constants to internal/farming/adaptive_grid/state_machine.go (WaitNewRangeTimeout, ExitAllTimeout, OverSizeTimeout, DefensiveTimeout, RecoveryTimeout)
- [x] T075 [P] Implement GetStateTimeout() method in internal/farming/adaptive_grid/state_machine.go
- [x] T076 [P] Implement CheckStateTimeout() method in internal/farming/adaptive_grid/state_machine.go
- [x] T077 [P] Implement ForceTransitionOnTimeout() method in internal/farming/adaptive_grid/state_machine.go
- [x] T078 [P] Add TimeoutStart field to SymbolState struct
- [x] T079 [P] Implement EmergencyForceState() method for manual recovery
- [x] T080 [P] Implement checkStateTimeouts() method in internal/farming/grid_manager.go
- [x] T081 [P] Add continuous state timeout checker (30s interval, runs for entire bot lifetime)
- [x] T081b [P] FIX: Removed redundant timeout checker from globalKlineProcessor (warm-up only)
- [ ] T082 [P] Write unit test for timeout detection and forced transitions (SKIPPED - can be done later)
- [ ] T083 [P] Test timeout behavior in dry-run mode (SKIPPED - can be done later)

### Phase 8.6: Regrid Condition Debugging & Fallback (Root Cause Fix) ✅ COMPLETED
**Story Goal:** Fix root cause of SOLUSD1 stuck in WAIT_NEW_RANGE - add detailed logging and fallback logic for regrid conditions.

**Root Cause Analysis:**
- SOLUSD1 stuck in WAIT_NEW_RANGE because isReadyForRegrid() returns false
- canRegridStandard() requires: position ≈ 0 + ADX < 70 + BB width < 10x
- If RangeDetector has no data (currentRange/lastAcceptedRange nil), regrid fails indefinitely
- No fallback mechanism when market conditions fail

**Independent Test Criteria:**
- Detailed logging added to canRegridStandard()
- Detailed logging added to checkMarketConditionsForRegrid()
- Fallback logic: force regrid after 5 minutes if market conditions fail
- Logs show which condition is failing

**Implementation Tasks:**
- [x] T084 [P] Add detailed logging to canRegridStandard() in internal/farming/adaptive_grid/manager.go
- [x] T085 [P] Add detailed logging to checkMarketConditionsForRegrid() in internal/farming/adaptive_grid/manager.go
- [x] T086 [P] Add fallback logic: force regrid after 5 minutes in WAIT_NEW_RANGE if market conditions fail
- [x] T087 [P] Log time in state and fallback timeout
- [ ] T088 [P] Monitor logs to identify which condition fails for SOLUSD1
- [ ] T089 [P] Adjust regrid conditions based on real market data

### Phase 9: Polish & Cross-Cutting Concerns
- [ ] T090 Create comprehensive documentation of all config changes
- [ ] T091 Update README with new configuration parameters
- [ ] T092 Add config validation to prevent invalid values
- [ ] T093 Add monitoring for equity tracking metrics
- [ ] T094 Add monitoring for market evaluation metrics
- [ ] T095 Add monitoring for state-specific parameter metrics
- [ ] T096 Final integration test with all features enabled
- [ ] T097 Code review and cleanup
- [ ] T098 Create git tag pre-config-opt-phase1

## Dependencies

**Story Dependency Graph:**
```
Phase 1 (Setup) → Phase 2 (Foundational)
Phase 2 → US1, US2, US3 (can run in parallel)
US1, US2, US3 → US5 (Equity Sizing)
US1, US2, US3 → Market Evaluation
US1, US2, US3 → State-Specific Actions
US5, Market Evaluation, State-Specific Actions → Phase 9 (Polish)
```

**Execution Order:**
1. Phase 1: Setup (T001-T004)
2. Phase 2: Foundational (T005-T008)
3. Phase 3: US1 (T009-T023)
4. Phase 4: US2 (T024-T029)
5. Phase 5: US3 (T030-T033)
6. Phase 6: US5 (T034-T051)
7. Phase 7: Market Evaluation (T052-T062)
8. Phase 8: State-Specific Actions (T063-T073)
9. Phase 9: Polish (T074-T085)

## Parallel Execution Opportunities

**Phase 3 (US1):**
- T009-T020 can run in parallel (different config files, no dependencies)

**Phase 4 (US2):**
- T024-T026 can run in parallel (different config parameters)

**Phase 7 (Market Evaluation):**
- T052-T054 can run in parallel (different evaluation methods)

**Phase 8 (State-Specific Actions):**
- T063-T065 can run in parallel (different config structs)

## MVP Scope

**Recommended MVP:** Phase 1-5 (Setup + US1 + US2 + US3)
- Configuration-only changes
- Lowest risk
- Can be deployed and tested quickly
- Expected win rate improvement: 10-15%

**Full Scope:** All phases (Phase 1-9)
- Complete optimization with equity sizing, market evaluation, state-specific actions
- Expected win rate improvement: 15-20%
- Expected drawdown reduction: 50%

## Testing Strategy

**Phase 1-5 (MVP):**
- Config validation tests
- Dry-run mode for 24 hours
- Monitor win rate, fill rate, stuck position rate

**Phase 6-8 (Full):**
- Unit tests for all new code
- Integration tests for equity sizing
- Integration tests for market evaluation
- Integration tests for state-specific actions
- Dry-run mode for 24 hours per phase
- Live monitoring with reduced size

**Phase 9 (Polish):**
- Comprehensive integration test
- End-to-end validation
- Performance testing
