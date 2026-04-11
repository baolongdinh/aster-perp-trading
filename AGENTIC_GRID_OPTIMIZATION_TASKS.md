# Agentic Grid Trading Optimization Tasks

## Feature: Tối ưu Volume Farming - Micro Profit & Risk Management

**Goal**: Tối đa volume, tối đa micro profit, tối thiểu rủi ro liquidation qua adaptive grid với BB+ADX detection

---

## Phase 1: Setup & Configuration

**Goal**: Chuẩn bị config structures cho tất cả optimizations

- [ ] T001 Add `MicroGridConfig` struct to `backend/internal/farming/adaptive_grid/micro_grid.go`
- [ ] T002 [P] Add `EnhancedRangeConfig` struct to `backend/internal/farming/adaptive_grid/range_detector.go`
- [ ] T003 [P] Add `DynamicLeverageConfig` struct to `backend/internal/farming/adaptive_grid/risk_sizing.go`
- [ ] T004 [P] Add `MultiLayerLiquidationConfig` struct to `backend/internal/farming/adaptive_grid/manager.go`
- [ ] T005 Update YAML config loader to support new config types in `backend/internal/config/config.go`

---

## Phase 2: Foundational - Range Detection Enhancement (P0)

**Goal**: BB period 20→10 để detect sideway nhanh hơn

**Independent Test Criteria**: RangeDetector cập nhật range trong ≤10 periods thay vì 20

- [ ] T006 Add `FastRangeConfig` with BBPeriod=10 to `backend/internal/farming/adaptive_grid/range_detector.go`
- [ ] T007 Update `DefaultRangeConfig()` to use BBPeriod=10 as default
- [ ] T008 Modify `calculateBollingerBands()` to work efficiently with 10 periods
- [ ] T009 Add unit test `TestFastRangeDetection_ShorterPeriod` in `backend/internal/farming/adaptive_grid/range_detector_test.go`
- [ ] T010 Update existing range detector tests to use new default

---

## Phase 3: Micro Grid Configuration (P0)

**Goal**: Giảm spread 0.15%→0.05%, tăng orders 3→5, giảm size $5→$3

**Independent Test Criteria**: Grid places 5 orders mỗi side với 0.05% spread, $3 size

- [ ] T011 [P] Create `MicroGridConfig` struct with spread 0.0005, ordersPerSide 5, orderSize 3.0
- [ ] T012 [P] Implement `MicroGridCalculator` in `backend/internal/farming/adaptive_grid/micro_grid.go`
- [ ] T013 Add `SetMicroGridMode(enabled bool)` method to `AdaptiveGridManager`
- [ ] T014 Modify `placeGridOrders()` to use micro grid when enabled
- [ ] T015 Update `calculateOrderSize()` with micro grid fallback ($3 minimum)
- [ ] T016 Add config flag `micro_grid.enabled` to YAML config
- [ ] T017 Add unit test `TestMicroGrid_OrderSpacing` verifying 0.05% spread
- [ ] T018 Add integration test `TestMicroGrid_PlacesFiveOrdersPerSide`

---

## Phase 4: ADX Sideways Filter (P1)

**Goal**: Chỉ trade khi ADX < 20 (confirmed sideways)

**Independent Test Criteria**: No orders placed when ADX ≥ 20

- [ ] T019 [P] Add `SidewaysADXMax` field to `RangeConfig` (default 20.0)
- [ ] T020 [P] Create `IsSidewaysConfirmed()` method in `RangeDetector`
- [ ] T021 Integrate ADX check into `ShouldTrade()` method
- [ ] T022 Update `CanPlaceOrder()` in `AdaptiveGridManager` to check ADX filter
- [ ] T023 Add `currentADX` tracking to `UpdatePriceData()` flow
- [ ] T024 Add unit test `TestADXFilter_BlocksHighADX` in `manager_test.go`
- [ ] T025 Add unit test `TestADXFilter_AllowsLowADX`

---

## Phase 5: Range State Validation Before Entry (P1)

**Goal**: Chờ `RangeStateActive` trước khi place orders lần đầu

**Independent Test Criteria**: Grid không place orders cho đến khi range active

- [ ] T026 [P] Add `WaitForRangeActive` flag to config (default true)
- [ ] T027 Modify `GridManager.UpdateSymbols()` to not enqueue placement immediately
- [ ] T028 Add `OnRangeActive(symbol string)` callback in `GridManager`
- [ ] T029 Update `RangeDetector` to trigger callback when state → Active
- [ ] T030 Add `TryEnqueuePlacement(symbol string)` method chỉ enqueue khi range active
- [ ] T031 Add unit test `TestGridManager_WaitsForRangeActive`
- [ ] T032 Add integration test `TestFirstPlacement_AfterRangeActive`

---

## Phase 6: Dynamic Leverage by Volatility (P2)

**Goal**: Leverage thấp khi ATR cao/trending, cao khi range chặt

**Independent Test Criteria**: Leverage tự động giảm khi ATR > 1.5x average

- [ ] T033 [P] Add `CalculateOptimalLeverage(atr, bbWidth, adx)` function
- [ ] T034 [P] Create `DynamicLeverageCalculator` struct
- [ ] T035 Integrate leverage calculation vào position sizing flow
- [ ] T036 Add `UpdateLeverage(symbol string, leverage float64)` method
- [ ] T037 Call leverage update khi regime change hoặc ATR spike
- [ ] T038 Add unit test `TestDynamicLeverage_HighATR_ReducesLeverage`
- [ ] T039 Add unit test `TestDynamicLeverage_TightRange_IncreasesLeverage`

---

## Phase 7: Multi-Layer Liquidation Protection (P2)

**Goal**: 4 layers protection trước khi hit liquidation

**Independent Test Criteria**: Position giảm dần theo tiers khi gần liquidation

- [ ] T040 [P] Add `LiquidationTiers` struct với 4 layers (Warn 50%, Reduce 30%, Close 15%, Hedge 10%)
- [ ] T041 [P] Implement `checkLiquidationTiers()` method
- [ ] T042 Implement `reducePositionByPct(symbol string, pct float64)`
- [ ] T043 Implement `emergencyHedgeAndClose(symbol string)` với counter order
- [ ] T044 Update `StartPositionMonitoring()` loop để check tiers thay vì chỉ check nearLiquidation
- [ ] T045 Add unit test `TestLiquidationTiers_ReduceAt30Percent`
- [ ] T046 Add unit test `TestLiquidationTiers_HedgeAt10Percent`

---

## Phase 8: Micro Partial Close (P3)

**Goal**: 4 TP levels với micro profit (8-40 bps)

**Independent Test Criteria**: Position partially closes at each TP level

- [ ] T047 [P] Create `MicroPartialCloseConfig` với 4 TP levels
- [ ] T048 [P] Update `PositionSlice` struct hỗ trợ 4 TP levels
- [ ] T049 Add `CheckMicroPartialClose(symbol, currentPrice)` method
- [ ] T050 Modify `handlePartialClose()` to use micro config when enabled
- [ ] T051 Add trailing stop activation sau TP3
- [ ] T052 Add unit test `TestMicroPartialClose_TP1At8Bps`
- [ ] T053 Add unit test `TestMicroPartialClose_AllFourLevels`

---

## Phase 9: Integration & Polish

**Goal**: Tất cả components hoạt động cùng nhau, config-driven

- [ ] T054 [P] Add feature flags config section: `optimization.micro_grid`, `optimization.fast_range`, `optimization.adx_filter`
- [ ] T055 [P] Create `OptimizationManager` để coordinate các features
- [ ] T056 Add metrics collection cho volume, fills, profit per fill
- [ ] T057 Update dashboard.py hiển thị micro grid metrics
- [ ] T058 Add integration test `TestFullOptimizationFlow` với tất cả features enabled
- [ ] T059 Performance test: verify < 1ms overhead per price update
- [ ] T060 Update README với optimization guides

---

## Dependencies & Execution Order

```
Phase 1 (Setup) ──────────────────────────────────────────────┐
                                                              │
Phase 2 (Fast Range) ───────────────────────────────────────┤
                              ↓                               │
Phase 3 (Micro Grid) ────────────────────────────────────────┤
                              ↓                               │
Phase 4 (ADX Filter) ────────┐ ↓                              │
                              ↓                               │
Phase 5 (Range State) ───────┴─────────┐                      │
                                        ↓                     │
Phase 6 (Dynamic Leverage) ────────────┤                      │
                              ↓                               │
Phase 7 (Liquidation Tiers) ──────────┤                      │
                              ↓                               │
Phase 8 (Micro Partial Close) ─────────┘                      │
                                        ↓                     │
Phase 9 (Integration) ◄──────────────────────────────────────┘
```

**Parallel Opportunities**:
- T002, T003, T004 (config structs) can run in parallel
- T011, T012 (micro grid) can parallel with T019, T020 (ADX)
- T033, T034 (leverage) can parallel with T040, T041 (liquidation)

---

## Implementation Strategy

### MVP (Week 1)
- Hoàn thành Phase 2 (Fast Range) + Phase 3 (Micro Grid)
- Test với 1 symbol trên testnet
- Metrics: Volume tăng 2x, spread profit giảm nhưng frequency tăng

### Iteration 1 (Week 2)
- Hoàn thành Phase 4 (ADX Filter) + Phase 5 (Range State)
- Backtest với historical data
- Metrics: Win rate > 60%, giảm false entries

### Iteration 2 (Week 3)
- Hoàn thành Phase 6 (Dynamic Leverage) + Phase 7 (Liquidation)
- Risk stress test
- Metrics: 0 liquidation events, optimal leverage utilization

### Final (Week 4)
- Hoàn thành Phase 8 (Micro Partial Close) + Phase 9 (Integration)
- Full integration test
- Metrics: Micro profit 8-40 bps realized, total PnL positive

---

## File Changes Summary

| File | Changes |
|------|---------|
| `backend/internal/farming/adaptive_grid/range_detector.go` | Fast BB period, ADX filter |
| `backend/internal/farming/adaptive_grid/micro_grid.go` | **NEW** Micro grid calculator |
| `backend/internal/farming/adaptive_grid/manager.go` | Multi-layer liquidation, range state checks |
| `backend/internal/farming/adaptive_grid/risk_sizing.go` | Dynamic leverage |
| `backend/internal/farming/grid_manager.go` | Wait for range active, micro grid mode |
| `backend/internal/farming/adaptive_grid/partial_close.go` | Micro TP levels |
| `backend/internal/config/config.go` | New config structures |
| `backend/config/agentic-vf-config.yaml` | Feature flags |

---

**Total Tasks**: 60
**Estimated Effort**: 4 weeks (2 senior developers)
**Key Metrics**: 
- Volume: +200-300%
- Profit per fill: 8-40 bps
- Max drawdown: < 5%
- Liquidation risk: Near zero
