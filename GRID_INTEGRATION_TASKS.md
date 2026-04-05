# Grid Optimization Integration Tasks

## Overview

Tích hợp toàn bộ các module optimization đã tạo vào core trading logic. Mỗi task implement xong phải test build.

## Dependencies

- Phase 1 → Phase 2 → Phase 3 → Phase 4 (sequential)
- Trong mỗi phase: các task [P] có thể song song

---

## Phase 1: Backend Safeguards (Foundation)

**Goal**: Tích hợp các safeguard cơ bản vào GridManager

- [ ] T001 Add OrderLockManager to GridManager struct in `internal/farming/grid_manager.go`
- [ ] T002 [P] Add FillEventDeduplicator to GridManager struct in `internal/farming/grid_manager.go`
- [ ] T003 [P] Add StateValidator to GridManager struct in `internal/farming/grid_manager.go`
- [ ] T004 Initialize safeguard components in GridManager constructor
- [ ] T005 Implement order locking in placeOrder() method
- [ ] T006 Implement fill deduplication in handleOrderFill() method
- [ ] T007 Implement state validation before order transitions
- [ ] T008 Add safeguard cleanup in Stop() method
- [ ] T009 **BUILD TEST** - Verify Phase 1 compiles

---

## Phase 2: Spread Protection & Dynamic Sizing

**Goal**: Tích hợp spread protection và dynamic spread calculation

- [ ] T010 Add SpreadProtection to AdaptiveGridManager struct in `internal/farming/adaptive_grid/manager.go`
- [ ] T011 [P] Add DynamicSpreadCalculator to AdaptiveGridManager struct
- [ ] T012 Initialize SpreadProtection in Initialize() method
- [ ] T013 Initialize DynamicSpreadCalculator in Initialize() method
- [ ] T014 Integrate spread check into CanPlaceOrder() method
- [ ] T015 Replace hardcoded spread with dynamic spread calculation
- [ ] T016 Implement spread monitoring goroutine
- [ ] T017 Update applyNewRegimeParameters() to use dynamic spread
- [ ] T018 **BUILD TEST** - Verify Phase 2 compiles

---

## Phase 3: Inventory Skew & Position Management

**Goal**: Tích hợp inventory tracking và skew management

- [ ] T019 Add InventoryManager to AdaptiveGridManager struct
- [ ] T020 [P] Add ClusterManager to AdaptiveGridManager struct
- [ ] T021 Initialize InventoryManager in Initialize() method
- [ ] T022 Initialize ClusterManager in Initialize() method
- [ ] T023 Update position tracking to use InventoryManager
- [ ] T024 Integrate inventory skew check into CanPlaceOrder()
- [ ] T025 Implement skew-based order size adjustment in GetOrderSize()
- [ ] T026 Add cluster stop-loss check in positionMonitor()
- [ ] T027 Implement breakeven exit logic
- [ ] T028 [P] Add cluster heat map logging
- [ ] T029 **BUILD TEST** - Verify Phase 3 compiles

---

## Phase 4: Trend Detection & Counter-Trend Protection

**Goal**: Tích hợp trend detection và counter-trend order management

- [ ] T030 Add TrendDetector to AdaptiveGridManager struct
- [ ] T031 [P] Add FundingRateMonitor to AdaptiveGridManager struct
- [ ] T032 [P] Add ATRCalculator to AdaptiveGridManager struct
- [ ] T033 [P] Add RSICalculator to AdaptiveGridManager struct
- [ ] T034 Initialize all trend/ATR/RSI components in Initialize()
- [ ] T035 Implement price data feeding to ATR calculator
- [ ] T036 Implement trend detection in positionMonitor()
- [ ] T037 Integrate trend check into CanPlaceOrder() - pause counter-trend
- [ ] T038 Implement trend-adjusted order sizing
- [ ] T039 Add trend exhaustion detection and handling
- [ ] T040 Implement funding rate monitoring goroutine
- [ ] T041 Integrate funding rate adjustment into order sizing
- [ ] T042 [P] Add trend state logging
- [ ] T043 **BUILD TEST** - Verify Phase 4 compiles

---

## Phase 5: Volume Farm Engine Integration

**Goal**: Tích hợp vào VolumeFarmEngine để kích hoạt toàn bộ hệ thống

- [ ] T044 Add all optimization managers to VolumeFarmEngine struct in `internal/farming/volume_farm_engine.go`
- [ ] T045 Initialize all optimization components in engine Start()
- [ ] T046 Implement price data pipeline (WebSocket → ATR/Trend calculators)
- [ ] T047 Integrate CanPlaceOrder checks into order placement flow
- [ ] T048 Implement dynamic spread updates on regime change
- [ ] T049 Add comprehensive metrics export for all components
- [ ] T050 Implement graceful shutdown with all component cleanup
- [ ] T051 **FINAL BUILD TEST** - Verify complete integration compiles

---

## Phase 6: Configuration & Testing

**Goal**: Config loading và validation

- [ ] T052 Load all YAML configs in engine initialization
- [ ] T053 Implement config hot-reload for all components
- [ ] T054 Add integration tests for each component
- [ ] T055 Add end-to-end flow test
- [ ] T056 **FINAL INTEGRATION TEST**

---

## Summary

| Phase | Tasks | Description |
|-------|-------|-------------|
| 1 | 9 | Backend Safeguards |
| 2 | 9 | Spread Protection & Dynamic |
| 3 | 11 | Inventory & Cluster |
| 4 | 14 | Trend & Funding |
| 5 | 8 | Engine Integration |
| 6 | 5 | Config & Testing |
| **Total** | **56** | |

## File Paths

Core files to modify:
- `internal/farming/grid_manager.go`
- `internal/farming/adaptive_grid/manager.go`
- `internal/farming/volume_farm_engine.go`

Module files (already created):
- `internal/farming/adaptive_grid/atr_calculator.go`
- `internal/farming/adaptive_grid/rsi_calculator.go`
- `internal/farming/adaptive_grid/order_lock.go`
- `internal/farming/adaptive_grid/dedup.go`
- `internal/farming/adaptive_grid/spread_protection.go`
- `internal/farming/adaptive_grid/validation.go`
- `internal/farming/adaptive_grid/dynamic_spread.go`
- `internal/farming/adaptive_grid/inventory_manager.go`
- `internal/farming/adaptive_grid/cluster_manager.go`
- `internal/farming/adaptive_grid/trend_detector.go`
- `internal/farming/adaptive_grid/funding_monitor.go`
