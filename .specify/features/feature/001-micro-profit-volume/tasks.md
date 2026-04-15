# Tasks: Micro Profit + Volume Farming

## Overview
Total Tasks: 28
Stories: 5
Parallel Opportunities: 8

## Phase 1: Setup

- [X] T001 Create config/micro_profit.yaml with default configuration in backend/config/micro_profit.yaml
- [X] T002 [P] Create internal/farming/adaptive_grid/micro_profit_config.go with MicroProfitConfig struct
- [X] T003 [P] Create internal/farming/adaptive_grid/take_profit_order.go with TakeProfitOrder struct and TakeProfitStatus enum
- [X] T004 [P] Create internal/farming/adaptive_grid/take_profit_manager.go with TakeProfitManager struct skeleton

## Phase 2: Foundational

- [X] T005 Implement MicroProfitConfig validation methods in internal/farming/adaptive_grid/micro_profit_config.go
- [X] T006 [P] Implement TakeProfitOrder profit calculation methods in internal/farming/adaptive_grid/take_profit_order.go
- [X] T007 [P] Implement TakeProfitOrder expiration check methods in internal/farming/adaptive_grid/take_profit_order.go
- [X] T008 Initialize TakeProfitManager in GridManager constructor in internal/farming/grid_manager.go
- [X] T009 Add TakeProfitManager field to GridManager struct in internal/farming/grid_manager.go
- [X] T010 [P] Create PositionTakeProfitMapping struct in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T011 [P] Create MappingStatus enum in internal/farming/adaptive_grid/take_profit_manager.go

## Phase 3: US1 - Automatic Take Profit Order Placement (FR1)

**Story Goal**: When a grid order is filled and position is opened, system immediately places a take profit order at configurable spread with ReduceOnly flag.

**Independent Test Criteria**: 
- Grid order filled → take profit order placed within 100ms
- Take profit order has correct side (opposite to filled order)
- Take profit order has ReduceOnly flag set to true
- Take profit order price is at configured spread from fill price
- Take profit order size matches filled position size

**Implementation Tasks**:
- [X] T012 [US1] Implement PlaceTakeProfitOrder method in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T013 [US1] Add TakeProfitOrderID field to GridOrder struct in internal/farming/grid_manager.go
- [X] T014 [US1] Modify handleOrderFill to call TakeProfitManager.PlaceTakeProfitOrder in internal/farming/grid_manager.go
- [X] T015 [US1] Add take profit order placement logging in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T016 [US1] Add error handling for take profit placement failures in internal/farming/adaptive_grid/take_profit_manager.go

## Phase 4: US2 - Take Profit Order Tracking (FR2)

**Story Goal**: System tracks all active take profit orders with their corresponding positions, monitors fill status, logs events, and records profit.

**Independent Test Criteria**:
- Take profit orders stored in memory map with O(1) lookup
- Take profit order status updates correctly on fills
- All take profit events are logged with context
- Profit amount calculated and recorded correctly

**Implementation Tasks**:
- [X] T017 [US2] Implement take profit order tracking map in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T018 [US2] Implement HandleTakeProfitFill method in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T019 [US2] Add profit recording logic in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T020 [US2] Modify OnOrderUpdate to detect take profit fills in internal/farming/grid_manager.go
- [X] T021 [US2] Add take profit fill event logging in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T022 [US2] Add mutex for thread-safe take profit tracking in internal/farming/adaptive_grid/take_profit_manager.go

## Phase 5: US3 - Grid Rebalance After Take Profit (FR3)

**Story Goal**: When take profit order is filled, system triggers immediate grid rebalance that respects risk checks and state machine gates.

**Independent Test Criteria**:
- Take profit fill triggers grid rebalance within 50ms
- Rebalance respects CircuitBreaker checks
- Rebalance respects state machine gates
- Volume farming metrics updated after rebalance

**Implementation Tasks**:
- [X] T023 [US3] Add rebalance trigger call in HandleTakeProfitFill in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T024 [US3] Add rebalance trigger logging in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T025 [US3] Add error handling for rebalance failures in internal/farming/adaptive_grid/take_profit_manager.go

## Phase 6: US4 - Take Profit Timeout Handling (FR4)

**Story Goal**: If take profit order not filled within configurable timeout, system closes position by market order and triggers rebalance.

**Independent Test Criteria**:
- Timeout check runs every 5 seconds
- Expired take profit orders detected correctly
- Position closed by market order on timeout
- Rebalance triggered after position close
- Timeout events logged

**Implementation Tasks**:
- [X] T026 [US4] Implement CheckTimeouts method with ticker in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T027 [US4] Add market order close for timeout in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T028 [US4] Add timeout check ticker to main loop in internal/farming/adaptive_grid/manager.go
- [X] T029 [US4] Add timeout event logging in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T030 [US4] Add ticker shutdown logic in internal/farming/adaptive_grid/manager.go

## Phase 7: US5 - Configuration Management (FR5)

**Story Goal**: Micro profit feature can be enabled/disabled via configuration, with configurable spread, timeout, and minimum profit. Configuration can be updated without restart.

**Independent Test Criteria**:
- Configuration loads from YAML file on startup
- Configuration values validated against ranges
- Default values used if file missing
- Hot-reload works on file change
- Configuration changes apply to new take profit orders

**Implementation Tasks**:
- [X] T031 [US5] Implement LoadMicroProfitConfig function in internal/config/micro_profit_config.go
- [X] T032 [US5] Add YAML parsing and validation in internal/config/micro_profit_config.go
- [X] T033 [US5] Add default values fallback in internal/config/micro_profit_config.go
- [X] T034 [US5] Implement file watcher for hot-reload in internal/config/micro_profit_config.go
- [X] T035 [US5] Wire config loading to TakeProfitManager in internal/farming/adaptive_grid/manager.go
- [X] T036 [US5] Add config update handler in internal/farming/adaptive_grid/take_profit_manager.go

## Phase 8: Polish & Cross-Cutting

- [X] T037 [P] Add GetMicroProfitMetrics method in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T038 [P] Add metrics tracking fields in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T039 [P] Integrate metrics display in dashboard in internal/farming/volume_farm_engine.go
- [X] T040 [P] Add comprehensive error logging across all take profit methods in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T041 Add performance optimization for take profit order placement in internal/farming/adaptive_grid/take_profit_manager.go
- [X] T042 Update README with micro profit feature documentation in README.md
- [X] T043 Add configuration guide to docs in docs/configuration/micro-profit.md

## Dependencies

**Story Completion Order:**
1. US1 (Automatic Take Profit Order Placement) - Foundation for all other stories
2. US2 (Take Profit Order Tracking) - Depends on US1 (needs orders to track)
3. US3 (Grid Rebalance After Take Profit) - Depends on US2 (needs fill detection)
4. US4 (Take Profit Timeout Handling) - Depends on US1 (needs orders to timeout)
5. US5 (Configuration Management) - Independent, can run parallel with US2-US4

**Phase Dependencies:**
- Phase 1 (Setup) must complete before Phase 2 (Foundational)
- Phase 2 (Foundational) must complete before Phase 3 (US1)
- US1 must complete before US2 and US4
- US2 must complete before US3
- US5 can run in parallel with US2-US4

## Parallel Execution Examples

**Within Phase 1 (Setup):**
- T002, T003, T004 can run in parallel (different files, no dependencies)

**Within Phase 2 (Foundational):**
- T006, T007, T010, T011 can run in parallel (different files)

**Within Phase 3 (US1):**
- T013, T015, T016 can run in parallel (different concerns)

**Within Phase 4 (US2):**
- T019, T021 can run in parallel (different concerns)

**Within Phase 7 (US5):**
- T032, T033 can run in parallel (different validation logic)

**Within Phase 8 (Polish):**
- T037, T038, T039, T040 can run in parallel (different concerns)

**Cross-Phase Parallelism:**
- US5 (Phase 7) can run in parallel with US2-US4 (Phases 4-6)
- T037-T040 (Phase 8 polish tasks) can run in parallel with US4-US5 implementation

## Implementation Strategy

**MVP Scope (Phase 1-3):**
- Setup configuration structures
- Implement core take profit order placement
- Integrate with order fill handling
- Basic error handling and logging

**Incremental Delivery:**
1. **Week 1**: Complete Phase 1-3 (MVP) - Basic take profit placement
2. **Week 2**: Complete Phase 4-5 - Order tracking and rebalance
3. **Week 3**: Complete Phase 6-7 - Timeout handling and configuration
4. **Week 4**: Complete Phase 8 - Polish, metrics, documentation

**Risk Mitigation:**
- Feature flag (enabled=false by default) allows safe rollout
- Comprehensive error handling prevents system crashes
- Each user story independently testable
- Can disable feature at any time if issues arise

**Testing Strategy:**
- Unit tests for each component (TDD approach recommended)
- Integration test for full flow (US1 → US2 → US3)
- Simulation test for performance validation
- Manual testing in staging before production
