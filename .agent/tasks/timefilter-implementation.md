# TimeFilter Implementation Tasks

> Phân rã implementation cho tính năng Time-Based Trading (trading_hours.yaml)
> Mục tiêu: Tự động điều chỉnh lệnh grid khi chuyển khung giờ

---

## Tổng quan

| Thành phần | File chính | Mô tả |
|------------|------------|-------|
| TimeFilter | `backend/internal/farming/adaptive_grid/time_filter.go` | Đã có, cần bổ sung tracking slot change |
| GridManager | `backend/internal/farming/grid_manager.go` | Cần check CanTrade() khi đặt lệnh |
| AdaptiveGridManager | `backend/internal/farming/adaptive_grid/manager.go` | Cần goroutine theo dõi slot transition |
| OrderManager | `backend/internal/farming/adaptive_grid/order_manager.go` | Có sẵn CancelAndRebuild, cần tích hợp |

---

## Phase 1: Foundation (Cơ sở)

**Mục tiêu**: Bổ sung infrastructure để track và handle slot changes

- [ ] T001 Thêm `previousSlot` tracking vào TimeFilter
  - File: `backend/internal/farming/adaptive_grid/time_filter.go`
  - Thêm field `previousSlot *TimeSlotConfig` vào struct TimeFilter
  - Thêm method `HasSlotChanged() bool` để detect chuyển khung giờ
  - Thêm method `GetSlotTransition() (old, new *TimeSlotConfig)`

- [ ] T002 [P] Thêm `OnSlotChange` callback registration vào TimeFilter
  - File: `backend/internal/farming/adaptive_grid/time_filter.go`
  - Thêm field `slotChangeCallbacks []func(old, new *TimeSlotConfig)`
  - Thêm method `RegisterSlotChangeCallback(fn func(old, new *TimeSlotConfig))`
  - Gọi callbacks khi phát hiện slot thay đổi trong `GetCurrentSlot()`

- [ ] T003 Tạo `TimeSlotTransitionHandler` struct
  - File: `backend/internal/farming/adaptive_grid/slot_transition_handler.go` (mới)
  - Struct handle logic: cancel orders → clear grid → rebuild với params mới
  - Inject GridManagerInterface và AdaptiveGridManager
  - Method `HandleTransition(ctx, symbol, oldSlot, newSlot) error`

---

## Phase 2: Core Integration (Tích hợp chính)

**Mục tiêu**: Tích hợp TimeFilter vào flow đặt lệnh và monitoring

- [ ] T004 [P] Thêm `CanTrade()` check trong `placeOrder()`
  - File: `backend/internal/farming/grid_manager.go:856-985`
  - Trước khi đặt lệnh, check `adaptiveMgr.CanTrade()` nếu có
  - Nếu `CanTrade() == false`, skip đặt lệnh và log warning
  - Trả về error `ErrTradingNotAllowed` để caller biết

- [ ] T005 [P] Thêm `GetCurrentSlot()` method vào AdaptiveGridManager
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Proxy method: `return a.timeFilter.GetCurrentSlot()`
  - Handle case `timeFilter == nil` (return nil)

- [ ] T006 Thêm `CanTrade()` method vào AdaptiveGridManager
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Proxy method: `return a.timeFilter.CanTrade()`
  - Nếu `timeFilter == nil`, return `true` (fallback cho backward compat)

- [ ] T007 [P] Thêm slot monitoring goroutine trong AdaptiveGridManager
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Trong `Initialize()`, spawn goroutine `slotMonitor()`
  - Check slot mỗi 30 giây
  - Nếu slot thay đổi: gọi `handleSlotTransition()`

- [ ] T008 Implement `handleSlotTransition()` trong AdaptiveGridManager
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Lấy oldSlot, newSlot từ timeFilter
  - Log: "Slot transition: {old} -> {new}"
  - Nếu newSlot.Enabled == false: chỉ hủy lệnh, không rebuild
  - Nếu newSlot.Enabled == true: Cancel → Clear → Rebuild với params mới

---

## Phase 3: Grid Parameter Update (Cập nhật tham số grid)

**Mục tiêu**: Áp dụng `size_multiplier` và `spread_multiplier` từ slot config

- [ ] T009 [P] Thêm `GetSizeMultiplier()` vào AdaptiveGridManager
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Proxy: `return a.timeFilter.GetSizeMultiplier()`
  - Nếu timeFilter == nil, return 1.0

- [ ] T010 [P] Thêm `GetSpreadMultiplier()` vào AdaptiveGridManager
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Proxy: `return a.timeFilter.GetSpreadMultiplier()`
  - Nếu timeFilter == nil, return 1.0

- [ ] T011 Cập nhật `GetAdaptiveOrderSize()` sử dụng size_multiplier
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Sau khi tính size từ riskMonitor, nhân với `GetSizeMultiplier()`
  - Đảm bảo: `finalSize = baseSize * sizeMultiplier`
  - Log: "Size adjusted by time filter: {base} -> {final}"

- [ ] T012 Cập nhật grid spread calculation sử dụng spread_multiplier
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Tìm nơi tính grid spread (trong `GetDynamicSpread()` hoặc tương tự)
  - Nhân spread với `GetSpreadMultiplier()`
  - Đảm bảo spread cũng được áp dụng khi rebuild grid sau slot change

---

## Phase 4: Slot Transition Logic (Logic chuyển khung giờ)

**Mục tiêu**: Xử lý đúng các scenario khi chuyển khung giờ

- [ ] T013 Implement transition: Enabled → Disabled
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Khi chuyển sang slot disabled: 
    - Gọi `gridManager.CancelAllOrders(ctx, symbol)`
    - Không đặt lệnh mới cho đến khi vào slot enabled
    - Log: "Trading paused - outside trading hours"

- [ ] T014 Implement transition: Disabled → Enabled
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Khi chuyển sang slot enabled:
    - Clear grid cũ
    - Rebuild với params của slot mới (size/spread multipliers)
    - Log: "Trading resumed - {slot.Description}"

- [ ] T015 Implement transition: Enabled → Enabled (khác params)
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Khi chuyển từ slot A sang slot B (đều enabled nhưng khác params):
    - Cancel all orders
    - Clear grid
    - Rebuild với params mới (sizeMultiplier, spreadMultiplier)
    - Log: "Grid rebuilt for new time slot: {newSlot.Description}"

- [ ] T016 [P] Thêm cooldown cho slot transitions
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Thêm field `lastSlotTransition map[string]time.Time`
  - Cooldown 2 phút giữa các transition để tránh spam
  - Check trước khi xử lý transition

---

## Phase 5: Testing & Validation

**Mục tiêu**: Đảm bảo implementation hoạt động đúng

- [ ] T017 [P] Unit test cho TimeFilter slot tracking
  - File: `backend/internal/farming/adaptive_grid/time_filter_test.go` (mới)
  - Test `HasSlotChanged()` với mock time
  - Test callback invocation khi slot đổi
  - Test overnight slots (23:00 → 01:00)

- [ ] T018 [P] Unit test cho slot transition handler
  - File: `backend/internal/farming/adaptive_grid/slot_transition_handler_test.go` (mới)
  - Test: Enabled → Disabled (chỉ cancel, không rebuild)
  - Test: Disabled → Enabled (cancel + clear + rebuild)
  - Test: Khác params (rebuild với multipliers mới)

- [ ] T019 Integration test cho end-to-end flow
  - File: `backend/internal/farming/adaptive_grid/time_filter_integration_test.go` (mới)
  - Test: Vào khung giờ mới → orders được cập nhật
  - Test: Ra khung giờ → orders bị hủy
  - Test: Multipliers được áp dụng đúng

- [ ] T020 [P] Test manual với config ngắn
  - Tạo test config với slot 2-3 phút để test nhanh
  - Verify: Lệnh được hủy khi ra khung giờ
  - Verify: Lệnh được tạo lại khi vào khung giờ mới

---

## Phase 6: Polish & Monitoring

**Mục tiêu**: Logging, metrics, và edge cases

- [ ] T021 [P] Thêm structured logging cho slot transitions
  - Log khi: phát hiện slot change, bắt đầu transition, hoàn thành
  - Include: oldSlot.Description, newSlot.Description, multipliers
  - File: `backend/internal/farming/adaptive_grid/manager.go`

- [ ] T022 [P] Thêm metrics endpoint cho time filter status
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Method `GetTimeFilterStatus() map[string]interface{}`
  - Include: current slot, canTrade, multipliers, next slot

- [ ] T023 Handle edge case: hot reload config
  - File: `backend/internal/farming/adaptive_grid/manager.go`
  - Khi `UpdateConfig()` được gọi, trigger immediate slot check
  - Nếu slot thay đổi sau reload, xử lý transition ngay

- [ ] T024 [P] Document behavior trong README
  - File: `backend/internal/farming/adaptive_grid/README_TIME_FILTER.md` (mới)
  - Mô tả: cách hoạt động, cách config, behavior khi chuyển slot

---

## Tóm tắt Dependencies

```
Phase 1 (T001-T003): Foundation
    ↓
Phase 2 (T004-T008): Core Integration  
    ↓
Phase 3 (T009-T012): Grid Parameters
    ↓
Phase 4 (T013-T016): Transition Logic
    ↓
Phase 5 (T017-T020): Testing
    ↓
Phase 6 (T021-T024): Polish
```

**Song song có thể**:
- T002, T003 (khác file, không dependency)
- T004, T005, T006 (khác methods, cùng file nhưng không phụ thuộc)
- T009, T010, T011 (tương tự)
- T017, T018 (khác test files)
- T021, T022, T023 (khác features)

---

## Estimate

| Phase | Tasks | Estimate |
|-------|-------|----------|
| 1 | 3 | 2h |
| 2 | 5 | 4h |
| 3 | 4 | 3h |
| 4 | 4 | 3h |
| 5 | 4 | 4h |
| 6 | 4 | 2h |
| **Total** | **24** | **~18h** |

---

## MVP Scope (Tối thiểu để chạy)

Chỉ cần Phase 1-3 + T013:
- T001, T002, T003: Foundation
- T004, T005, T006, T007, T008: Core
- T009, T010, T011, T012: Parameters
- T013: Transition Enabled → Disabled

**Total: 12 tasks (~10h)**
