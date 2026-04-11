# FE Refactor & Monitoring Dashboard - Task Breakdown

## Overview
Phân rã implementation plan thành các task nhỏ, có thể thực thi được. Mỗi task có file path cụ thể và rõ ràng.

---

## Phase 1: Setup & Foundation

### 1.1 Design System Foundation

- [ ] T001 Tạo file CSS variables: `frontend/src/styles/variables.css`
  - Định nghĩa color palette (bg-primary, bg-secondary, accent colors)
  - Typography variables (font-sans, font-mono)
  - Border, spacing, shadow variables

- [ ] T002 [P] Tạo global CSS: `frontend/src/styles/global.css`
  - Import variables.css
  - Base styles cho html, body
  - Custom scrollbar styles (từ App.vue)
  - Page transition animations

- [ ] T003 [P] Cập nhật `frontend/src/main.ts`
  - Import global.css sau index.css
  - Giữ nguyên existing imports

### 1.2 Base UI Components

- [ ] T004 Tạo Card component: `frontend/src/components/ui/Card.vue`
  - Props: title?, class?, padding?
  - Slot cho content
  - Consistent styling với design system
  - Header slot cho title/actions

- [ ] T005 [P] Tạo Badge component: `frontend/src/components/ui/Badge.vue`
  - Props: variant ('success' | 'warning' | 'danger' | 'info' | 'neutral'), size?
  - Slot cho content
  - Status indicator dot option

- [ ] T006 [P] Tạo Button component: `frontend/src/components/ui/Button.vue`
  - Props: variant, size, disabled, loading
  - Support icon prefix
  - Consistent hover/active states

- [ ] T007 [P] Tạo StatValue component: `frontend/src/components/ui/StatValue.vue`
  - Props: label, value, prefix?, suffix?, change?, format?
  - Auto-format numbers (K, M, B)
  - Show trend indicator nếu có change prop

- [ ] T008 [P] Tạo PnLDisplay component: `frontend/src/components/ui/PnLDisplay.vue`
  - Props: value, size?, showSign?
  - Auto-color: green (positive), red (negative), gray (zero)
  - Format: +$X.XX / -$X.XX

---

## Phase 2: Layout Components (Tách từ App.vue)

- [ ] T009 Tạo Sidebar component: `frontend/src/components/layout/Sidebar.vue`
  - Tách navigation logic từ App.vue
  - Props: items (nav items array)
  - Collapsible với transition
  - Tooltip khi collapsed
  - Active state highlighting

- [ ] T010 [P] Tạo Header component: `frontend/src/components/layout/Header.vue`
  - Tách header section từ App.vue
  - Props: status, apiEndpoint, equity
  - Bot status indicator (Online/Offline)
  - Account equity display
  - User avatar/initial

- [ ] T011 [P] Tạo PageContainer component: `frontend/src/components/layout/PageContainer.vue`
  - Consistent page padding
  - Max-width container
  - Optional title slot
  - Breadcrumb support (future)

- [ ] T012 Refactor App.vue: `frontend/src/App.vue`
  - Import Sidebar, Header từ layout/
  - Xóa inline sidebar/header code
  - Giữ WebSocket/App-level logic
  - Simplified template sử dụng layout components

---

## Phase 3: Composables

- [ ] T013 Tạo useFormat composable: `frontend/src/composables/useFormat.ts`
  - formatCurrency(value, decimals?)
  - formatNumber(value, decimals?, compact?)
  - formatPercent(value, decimals?)
  - formatPrice(value, symbol?)
  - formatDate(date, format?)

- [ ] T014 [P] Tạo useAutoRefresh composable: `frontend/src/composables/useAutoRefresh.ts`
  - Input: fetchFn, interval (default 3000)
  - Output: data, loading, error, refresh
  - Auto-cleanup on unmount
  - Pause/resume capability

- [ ] T015 [P] Tạo useWebSocket composable: `frontend/src/composables/useWebSocket.ts`
  - Input: url, autoReconnect?
  - Output: ws, isConnected, send, onMessage, disconnect
  - Auto-reconnect với exponential backoff
  - Subscribe/unsubscribe methods

---

## Phase 4: Backend API Extensions (BE Tasks)

### 4.1 Risk Manager Methods

- [ ] T016 Thêm GetAvailableBalance(): `backend/internal/risk/risk_manager.go`
  - Expose availableBalance field
  - Thread-safe với mu.Lock()

- [ ] T017 [P] Thêm GetDailyStartingEquity(): `backend/internal/risk/risk_manager.go`
  - Expose dailyStartingEquity field

- [ ] T018 [P] Thêm GetPendingMargin(): `backend/internal/risk/risk_manager.go`
  - Expose pendingMargin field

- [ ] T019 [P] Thêm GetSymNotional(): `backend/internal/risk/risk_manager.go`
  - Return copy của symNotional map
  - Return total notional sum

### 4.2 API Handlers

- [ ] T020 Tạo handleAccount: `backend/internal/api/handlers/account.go`
  - GET /api/v1/account
  - Return: balance, equity, margin_used, margin_ratio, unrealized_pnl, realized_pnl_today
  - Lấy data từ risk manager + exchange client

- [ ] T021 [P] Tạo handleRisk: `backend/internal/api/handlers/risk.go`
  - GET /api/v1/risk
  - Return: daily_pnl, daily_pnl_pct, daily_starting_equity, available_balance, pending_margin, total_notional, max_total_notional, is_paused, pause_reason, open_positions, max_open_positions, positions_by_symbol

- [ ] T022 [P] Tạo handleOrders: `backend/internal/api/handlers/orders.go`
  - GET /api/v1/orders
  - Return: orders[], total_orders, pending_notional
  - Wire với order manager

- [ ] T023 [P] Tạo handleFarming: `backend/internal/api/handlers/farming.go`
  - GET /api/v1/farming
  - Return: active_grids, total_grids, volume_24h, volume_7d, estimated_funding_24h, grid_configs[]
  - Wire với farming manager

- [ ] T024 [P] Tạo handleSummary: `backend/internal/api/handlers/summary.go`
  - GET /api/v1/summary
  - Return: date, starting_equity, current_equity, total_pnl, total_pnl_pct, trades{}, orders{}, volume, strategies_active
  - Aggregate từ nhiều sources

- [ ] T025 Cập nhật server routes: `backend/internal/api/server.go`
  - Register handlers cho /api/v1/account, /api/v1/risk, /api/v1/orders, /api/v1/farming, /api/v1/summary

- [ ] T026 [P] Enhance WebSocket: `backend/internal/api/ws_hub.go`
  - Thêm message types: 'metrics', 'position', 'account'
  - Broadcast real-time updates
  - Subscription channels

---

## Phase 5: Frontend API Layer

- [ ] T027 Cập nhật types.ts: `frontend/src/api/types.ts`
  - Thêm Account, RiskMetrics, Order, FarmingMetrics, DailySummary interfaces
  - Cập nhật WebSocket message types

- [ ] T028 [P] Tạo useAccount composable: `frontend/src/api/account.ts`
  - useAccount() hook
  - Auto-refresh 3s
  - Return: account, loading, error, refresh

- [ ] T029 [P] Tạo useRisk composable: `frontend/src/api/risk.ts`
  - useRisk() hook
  - Auto-refresh 3s
  - Return: risk, loading, error, refresh

- [ ] T030 [P] Tạo useOrders composable: `frontend/src/api/orders.ts`
  - useOrders() hook
  - Auto-refresh 3s
  - cancelOrder(orderId) method
  - Return: orders, loading, error, refresh, cancelOrder

- [ ] T031 [P] Tạo useFarming composable: `frontend/src/api/farming.ts`
  - useFarming() hook
  - Auto-refresh 5s
  - Return: farming, loading, error, refresh

- [ ] T032 [P] Tạo useSummary composable: `frontend/src/api/summary.ts`
  - useSummary() hook
  - Auto-refresh 10s
  - Return: summary, loading, error, refresh

---

## Phase 6: Views Implementation

### 6.1 Dashboard.vue (Refactor)

- [ ] T033 Refactor Dashboard.vue: `frontend/src/views/Dashboard.vue`
  - Sử dụng Card, StatValue components
  - Import useStatus, usePositions, useSummary
  - Stats grid: Equity, Daily P&L, Unrealized P&L, Open Positions
  - Quick actions: Pause/Resume, Close All, Export
  - Recent activity (last 5 events)
  - Xóa tất cả scoped CSS cũ
  - Styling theo design system (dark theme)

### 6.2 Positions.vue (New)

- [ ] T034 Tạo Positions.vue: `frontend/src/views/Positions.vue`
  - Import usePositions
  - Table columns: Symbol, Side, Size, Entry, Mark, Unrealized, Leverage, Actions
  - Real-time PnL updates
  - Sort/filter capability
  - Close position button
  - PnLDisplay component cho unrealized column

### 6.3 Orders.vue (New)

- [ ] T035 Tạo Orders.vue: `frontend/src/views/Orders.vue`
  - Import useOrders
  - Table columns: Symbol, Side, Type, Price, Qty, Filled %, Status, Time, Actions
  - Cancel order button
  - Status badges
  - Filled % progress bar

### 6.4 Account.vue (New)

- [ ] T036 Tạo Account.vue: `frontend/src/views/Account.vue`
  - Import useAccount, useSummary
  - Stats cards: Balance, Equity, Margin Used, Margin Ratio, Unrealized PnL
  - Allocation section (Available/In Positions/Pending)
  - Simple bar chart cho allocation
  - Equity history (placeholder cho chart)

### 6.5 Risk.vue (New)

- [ ] T037 Tạo Risk.vue: `frontend/src/views/Risk.vue`
  - Import useRisk
  - Progress bars cho: Daily P&L vs Limit, Positions vs Max, Notional vs Max
  - Risk metrics grid
  - Pause status + reason display
  - Risk events log (recent alerts)

### 6.6 Farming.vue (New)

- [ ] T038 Tạo Farming.vue: `frontend/src/views/Farming.vue`
  - Import useFarming
  - Summary cards: Active Grids, 24h Volume, 7d Volume, Est. Funding
  - Grid configs table
  - Per-symbol volume breakdown

### 6.7 Strategies.vue (Refactor)

- [ ] T039 Refactor Strategies.vue: `frontend/src/views/Strategies.vue`
  - Sử dụng Card, Badge, Button components
  - Giữ chức năng enable/disable
  - Thêm strategy metrics (nếu có sẵn)
  - Xóa scoped CSS cũ, apply design system

### 6.8 Activity.vue (Refactor)

- [ ] T040 Refactor Activity.vue: `frontend/src/views/Activity.vue`
  - Sử dụng Card, Badge components
  - Filter presets: Today, Last Hour, Last 10 min
  - Real-time badge animation
  - Export JSON/CSV buttons
  - Xóa scoped CSS cũ, apply design system

---

## Phase 7: Router & Navigation

- [ ] T041 Cập nhật router: `frontend/src/router/index.ts`
  - Thêm routes: /positions, /orders, /account, /risk, /farming
  - Lazy load tất cả views
  - Giữ existing routes

- [ ] T042 Cập nhật nav items: `frontend/src/App.vue` (hoặc Sidebar.vue nếu đã tách)
  - Thêm: Positions, Orders, Account, Risk, Farming
  - Icons tương ứng từ lucide-vue-next

---

## Phase 8: Charts (Optional Enhancement)

- [ ] T043 [P] Cài đặt chart library: `frontend/package.json`
  - Thêm: `vue-chartjs` hoặc `lightweight-charts`
  - Run npm install

- [ ] T044 [P] Tạo MiniSparkline component: `frontend/src/components/charts/MiniSparkline.vue`
  - Props: data (number[]), color, height?
  - SVG-based lightweight sparkline
  - Không cần heavy chart lib

- [ ] T045 [P] Tạo EquityChart component: `frontend/src/components/charts/EquityChart.vue`
  - Props: data (timeseries)
  - Time range selector: 24h, 7d, 30d
  - Line chart với gradient fill

---

## Phase 9: Polish & Testing

- [ ] T046 Thêm loading states: All views
  - Skeleton loaders cho mỗi view
  - Shimmer effect consistent

- [ ] T047 Thêm error states: All views
  - Error boundary component
  - Retry buttons
  - Error message display

- [ ] T048 Responsive design: All views
  - Mobile breakpoint (< 768px)
  - Sidebar collapse on mobile
  - Table scroll horizontal
  - Stats grid 1 column on mobile

- [ ] T049 WebSocket integration: Dashboard.vue
  - Subscribe to 'metrics' channel
  - Real-time equity/PnL updates
  - Connection status indicator

- [ ] T050 Performance optimization
  - Vue DevTools profiling
  - Memoize computed properties
  - Virtual scroll cho long lists (nếu cần)

---

## Dependencies Graph

```
Phase 1 (Setup)
├── T001 → T002 → T003 (CSS variables → Global → Main.ts)
└── T004 → T005 → T006 → T007 → T008 (UI components song song)

Phase 2 (Layout)
├── T009 (Sidebar) → T012 (App.vue refactor)
├── T010 (Header) → T012
n└── T011 (PageContainer) → T012

Phase 3 (Composables)
├── T013 (useFormat) → T014, T015, T027-032
├── T014 (useAutoRefresh) → T027-032
└── T015 (useWebSocket) → T040, T049

Phase 4 (BE APIs)
├── T016-T019 (Risk methods) → T020-T024 (handlers)
├── T020-T024 → T025 (server routes)
└── T026 (WS enhance) → T049 (FE WS integration)

Phase 5 (FE API)
├── T027 (types) → T028-T032
└── T028-T032 → T033-T038 (views)

Phase 6 (Views)
├── T033 (Dashboard) → T049 (WS integration)
├── T034-T038 (New views) → T041 (router)
└── T039-T040 (Refactor) → Done

Phase 7 (Router)
└── T041 → T042 (navigation)

Phase 8 (Charts)
└── T043 → T044 → T045 (optional)

Phase 9 (Polish)
└── T046-T050 (song song)
```

---

## Execution Order Recommendation

### Sprint 1: Foundation (Week 1)
Tasks: T001-T015, T027
- Setup design system
- Tạo base components
- Tách layout components
- Tạo composables

### Sprint 2: Backend APIs (Week 1-2)
Tasks: T016-T026
- Risk manager methods
- API handlers
- WebSocket enhancement

### Sprint 3: Views - Part 1 (Week 2)
Tasks: T028-T033, T041-T042
- FE API layer
- Dashboard refactor
- Navigation updates

### Sprint 4: Views - Part 2 (Week 3)
Tasks: T034-T040
- Positions, Orders, Account views
- Risk, Farming views
- Strategies, Activity refactor

### Sprint 5: Polish (Week 4)
Tasks: T043-T050
- Charts (optional)
- Loading/error states
- Responsive
- WebSocket real-time
- Performance

---

## Total: 50 Tasks

| Phase | Task Count |
|-------|------------|
| 1. Setup & Foundation | 8 |
| 2. Layout Components | 4 |
| 3. Composables | 3 |
| 4. Backend APIs | 11 |
| 5. Frontend API Layer | 6 |
| 6. Views Implementation | 8 |
| 7. Router & Navigation | 2 |
| 8. Charts (Optional) | 3 |
| 9. Polish & Testing | 5 |
| **Total** | **50** |

---

## MVP Scope (Sprint 1-3)

Nếu cần deliver nhanh, tập trung:
1. **T001-T015**: Design system + components
2. **T016-T025**: Backend APIs (bỏ qua WS enhancement)
3. **T027-T033**: Dashboard refactor + API layer
4. **T041-T042**: Navigation

Sau MVP, thêm các views còn lại và polish.
