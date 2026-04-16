# Tasks: Adaptive State Machine

## Overview
Total Tasks: 45
Stories: 3 (OVER_SIZE, DEFENSIVE, RECOVERY)
Parallel Opportunities: 12

## Phase 1: Setup

- [ ] T001 Review existing state machine implementation in internal/farming/adaptive_grid/state_machine.go
- [ ] T002 Review existing grid manager implementation in internal/farming/grid_manager.go
- [ ] T003 Review existing adaptive manager implementation in internal/farming/adaptive_grid/manager.go
- [ ] T004 Review existing config structure in internal/config/config.go
- [ ] T005 Review existing agentic-vf-config.yaml structure
- [ ] T006 Create backup of current state machine code
- [ ] T007 Create feature branch for adaptive-state-machine implementation

## Phase 2: Foundational - Market Condition Evaluator

- [ ] T008 [P] Create MarketCondition struct with volatility, trend, position, risk, market scores in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T009 [P] Create StateRecommendation struct with state, confidence, reason, conditions in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T010 [P] Create MarketConditionEvaluatorConfig struct in internal/config/config.go
- [ ] T011 [P] Add MarketConditionEvaluatorConfig field to main config struct in internal/config/config.go
- [ ] T012 Create MarketConditionEvaluator struct with logger, config, and data source references in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T013 Implement EvaluateVolatility() method using RangeDetector data (ATR, BB width, price swing) in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T014 Implement EvaluateTrend() method using RangeDetector data (ADX, trend strength, direction) in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T015 Implement EvaluatePosition() method using WebSocketClient data (size, unrealized PnL, time in position) in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T016 Implement EvaluateRisk() method using RiskManager data (daily PnL, drawdown, consecutive losses) in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T017 Implement EvaluateMarket() method using WebSocketClient data (spread, volume, funding rate) in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T018 Implement CalculateConfidence() method for state recommendation scoring in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T019 Implement RecommendState() method with state selection logic (OVER_SIZE, DEFENSIVE, RECOVERY, TRADING, EXIT_HALF, EXIT_ALL) in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T020 Add market_condition_evaluator config section to backend/config/agentic-vf-config.yaml
- [ ] T021 Create NewMarketConditionEvaluator() constructor function in internal/farming/adaptive_grid/market_condition_evaluator.go
- [ ] T022 Add marketConditionEvaluator field to GridManager struct in internal/farming/grid_manager.go
- [ ] T023 Add SetMarketConditionEvaluator() method to GridManager in internal/farming/grid_manager.go
- [ ] T024 Integrate MarketConditionEvaluator call in globalKlineProcessor after checkPnLRisk() in internal/farming/grid_manager.go
- [ ] T025 Load market condition evaluator config in VolumeFarmEngine and set to GridManager in internal/farming/volume_farm_engine.go
- [ ] T026 Add unit tests for EvaluateVolatility() in internal/farming/adaptive_grid/market_condition_evaluator_test.go
- [ ] T027 Add unit tests for EvaluateTrend() in internal/farming/adaptive_grid/market_condition_evaluator_test.go
- [ ] T028 Add unit tests for EvaluatePosition() in internal/farming/adaptive_grid/market_condition_evaluator_test.go
- [ ] T029 Add unit tests for RecommendState() in internal/farming/adaptive_grid/market_condition_evaluator_test.go
- [ ] T030 Add integration test for market condition evaluator with real data in internal/farming/adaptive_grid/market_condition_evaluator_integration_test.go

## Phase 3: OVER_SIZE State Implementation

- [ ] T031 [P] Add GridStateOverSize constant in internal/farming/adaptive_grid/state_machine.go
- [ ] T032 [P] Add EventOverSizeLimit and EventSizeNormalized events in internal/farming/adaptive_grid/state_machine.go
- [ ] T033 Update String() method to include OVER_SIZE state in internal/farming/adaptive_grid/state_machine.go
- [ ] T034 Update String() method to include new events in internal/farming/adaptive_grid/state_machine.go
- [ ] T035 Add transition case for TRADING → OVER_SIZE in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T036 Add transition case for OVER_SIZE → TRADING in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T037 Add transition case for OVER_SIZE → EXIT_HALF in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T038 Add transition case for OVER_SIZE → EXIT_ALL in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T039 Update CanTransition() method for OVER_SIZE state in internal/farming/adaptive_grid/state_machine.go
- [ ] T040 Implement checkPositionSize() method in GridManager in internal/farming/grid_manager.go
- [ ] T041 Add over_size config section to backend/config/agentic-vf-config.yaml
- [ ] T042 Add OverSizeConfig struct in internal/config/config.go
- [ ] T043 Add OverSizeConfig field to RiskConfig in internal/config/config.go
- [ ] T044 Update CanPlaceOrder() to check OVER_SIZE state and only allow reduce-only orders in internal/farming/adaptive_grid/manager.go
- [ ] T045 Add unit tests for OVER_SIZE state transitions in internal/farming/adaptive_grid/state_machine_test.go

## Phase 4: DEFENSIVE State Implementation

- [ ] T046 [P] Add GridStateDefensive constant in internal/farming/adaptive_grid/state_machine.go
- [ ] T047 [P] Add EventExtremeVolatility and EventVolatilityNormalized events in internal/farming/adaptive_grid/state_machine.go
- [ ] T048 Update String() method to include DEFENSIVE state in internal/farming/adaptive_grid/state_machine.go
- [ ] T049 Add transition case for TRADING → DEFENSIVE in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T050 Add transition case for DEFENSIVE → TRADING in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T051 Add transition case for DEFENSIVE → EXIT_ALL in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T052 Update CanTransition() method for DEFENSIVE state in internal/farming/adaptive_grid/state_machine.go
- [ ] T053 Create VolatilityMonitor struct in internal/farming/adaptive_grid/volatility_monitor.go
- [ ] T054 Implement MonitorVolatility() method using CircuitBreaker data in internal/farming/adaptive_grid/volatility_monitor.go
- [ ] T055 Add defensive state config section to backend/config/agentic-vf-config.yaml
- [ ] T056 Add DefensiveStateConfig struct in internal/config/config.go
- [ ] T057 Update grid parameter application logic for DEFENSIVE state (spread multiplier, SL multiplier, max orders) in internal/farming/adaptive_grid/manager.go
- [ ] T058 Add unit tests for DEFENSIVE state transitions in internal/farming/adaptive_grid/state_machine_test.go

## Phase 5: RECOVERY State Implementation

- [ ] T059 [P] Add GridStateRecovery constant in internal/farming/adaptive_grid/state_machine.go
- [ ] T060 [P] Add EventRecoveryStart and EventRecoveryComplete events in internal/farming/adaptive_grid/state_machine.go
- [ ] T061 Update String() method to include RECOVERY state in internal/farming/adaptive_grid/state_machine.go
- [ ] T062 Add transition case for EXIT_HALF → RECOVERY in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T063 Add transition case for EXIT_ALL → RECOVERY in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T064 Add transition case for RECOVERY → TRADING in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T065 Add transition case for RECOVERY → EXIT_HALF in Transition() method in internal/farming/adaptive_grid/state_machine.go
- [ ] T066 Update CanTransition() method for RECOVERY state in internal/farming/adaptive_grid/state_machine.go
- [ ] T067 Create RecoveryMonitor struct in internal/farming/adaptive_grid/recovery_monitor.go
- [ ] T068 Implement MonitorRecovery() method tracking PnL after EXIT_HALF/EXIT_ALL in internal/farming/adaptive_grid/recovery_monitor.go
- [ ] T069 Implement TrackStablePnL() method for minimum stable duration requirement in internal/farming/adaptive_grid/recovery_monitor.go
- [ ] T070 Add recovery state config section to backend/config/agentic-vf-config.yaml
- [ ] T071 Add RecoveryStateConfig struct in internal/config/config.go
- [ ] T072 Update grid parameter application logic for RECOVERY state (size multiplier, spread multiplier, TP/SL multipliers) in internal/farming/adaptive_grid/manager.go
- [ ] T073 Add unit tests for RECOVERY state transitions in internal/farming/adaptive_grid/state_machine_test.go

## Final Phase: Polish & Cross-Cutting

- [ ] T074 Add logging for all state transitions with market condition context in internal/farming/adaptive_grid/state_machine.go
- [ ] T075 Add metrics for state transition frequency and duration in internal/farming/adaptive_grid/state_machine.go
- [ ] T076 Add alerting for states stuck too long (DEFENSIVE > 30min, OVER_SIZE > 1h, RECOVERY > 2h) in internal/farming/grid_manager.go
- [ ] T077 Add alerting for rapid state transitions (> 10/min) in internal/farming/grid_manager.go
- [ ] T078 Update CanPlaceOrders() to include new states (OVER_SIZE, DEFENSIVE, RECOVERY) in internal/farming/adaptive_grid/state_machine.go
- [ ] T079 Add integration test for full state machine with all new states in internal/farming/adaptive_grid/state_machine_integration_test.go
- [ ] T080 Add simulation test for volatility spike → DEFENSIVE → TRADING flow in internal/farming/adaptive_grid/state_machine_simulation_test.go
- [ ] T081 Add simulation test for position size breach → OVER_SIZE → TRADING flow in internal/farming/adaptive_grid/state_machine_simulation_test.go
- [ ] T082 Add simulation test for loss → EXIT_HALF → RECOVERY → TRADING flow in internal/farming/adaptive_grid/state_machine_simulation_test.go
- [ ] T083 Update documentation for new states in README.md or ARCHITECTURE.md
- [ ] T084 Performance test for market condition evaluator overhead (< 10ms target) in internal/farming/adaptive_grid/market_condition_evaluator_bench_test.go
- [ ] T085 Dry-run deployment test with all states enabled in config
- [ ] T086 Update config validation to include new state configs in internal/config/validation.go

## Dependencies

```
Phase 2 (Foundational) → Phase 3 (OVER_SIZE)
Phase 2 (Foundational) → Phase 4 (DEFENSIVE)
Phase 2 (Foundational) → Phase 5 (RECOVERY)
Phase 3 (OVER_SIZE) → Final Phase
Phase 4 (DEFENSIVE) → Final Phase
Phase 5 (RECOVERY) → Final Phase

Phase 3, 4, 5 can be implemented in parallel after Phase 2 completes
```

## Parallel Execution Examples

### Phase 2 - Foundational (Parallel Opportunities)
```
T008-T011: Create structs (4 parallel)
T013-T017: Implement evaluation methods (5 parallel)
T026-T029: Unit tests (4 parallel)
```

### Phase 3 - OVER_SIZE (Parallel Opportunities)
```
T031-T034: Add constants and string methods (4 parallel)
```

### Phase 4 - DEFENSIVE (Parallel Opportunities)
```
T046-T047: Add constants and events (2 parallel)
```

### Phase 5 - RECOVERY (Parallel Opportunities)
```
T059-T060: Add constants and events (2 parallel)
```

## Implementation Strategy

### MVP Approach (Phase 0 + Phase 1)
**Focus:** Market Condition Evaluator + OVER_SIZE state
- Implement foundational evaluation layer first
- Add OVER_SIZE state as first adaptive state
- Test evaluation accuracy with real data
- Validate reduce-only order restriction
- **Timeline:** 3-4 days

### Incremental Delivery
1. **Phase 2 (Market Condition Evaluator):** Deploy with config disabled, enable after validation
2. **Phase 3 (OVER_SIZE):** Deploy with high threshold (1.5x), gradually lower to 1.0x
3. **Phase 4 (DEFENSIVE):** Deploy with conservative thresholds, adjust based on production data
4. **Phase 5 (RECOVERY):** Deploy with longer stable PnL requirement (60min), reduce to 30min

### Risk Mitigation
- Each phase can be independently disabled via config
- No database changes required (safe rollback)
- Dry-run mode available for testing
- Git tags for each phase (easy rollback)
- Monitor state transition frequency to prevent oscillation

### Success Metrics
- Market condition evaluation accuracy: > 80%
- State transition confidence: > 0.7
- Evaluation performance: < 10ms
- Position size never exceeds hard cap
- No rapid state switching (< 10/min)
