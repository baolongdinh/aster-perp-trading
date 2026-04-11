# FE Refactor & Monitoring Dashboard Implementation Plan

## Tổng quan

Refactor toàn bộ frontend để:
1. **Tinh gọn codebase**: Unified design system, shared components, consistent styling
2. **Giám sát bot toàn diện**: Hiển thị tất cả thông tin quan trọng từ BE lên FE
3. **Real-time monitoring**: WebSocket cho dữ liệu live

---

## Current State Analysis

### Frontend hiện tại
- **App.vue**: Layout chính với sidebar (dark theme, Tailwind classes)
- **Dashboard.vue**: Bot status, Daily P&L, Unrealized P&L, Open positions (light theme, scoped CSS - INCONSISTENT)
- **Strategies.vue**: List strategies với toggle enable/disable (light theme, scoped CSS - INCONSISTENT)
- **Activity.vue**: Activity logs với WebSocket real-time (mixed styling)
- **API Layer**: axios client, composables cho status/positions/strategies

### Backend APIs hiện có
| Endpoint | Data |
|----------|------|
| GET /api/v1/status | running, paused, daily_pnl, open_pos, server_time |
| GET /api/v1/positions | open positions |
| GET /api/v1/orders | open orders (TODO) |
| GET /api/v1/strategies | list strategies |
| GET /api/v1/metrics | daily_pnl, open_positions, is_paused |
| GET /api/v1/activity | activity logs |
| /ws | WebSocket real-time |

### Data available in BE nhưng chưa expose
- **Risk Manager**: `availableBalance`, `dailyStartingEquity`, `pendingMargin`, `symNotional`, `paused`
- **Positions**: `mark_price`, `unrealized_pnl`, `leverage`
- **Account**: Balance, Equity, Margin info (từ exchange)
- **Volume Farming**: Grid metrics, funding rates, volume stats

---

## Phase 1: Backend API Extensions

### 1.1 New API Endpoints

```go
// GET /api/v1/account - Account overview
{
  "balance": 50000.00,           // Available balance
  "equity": 50245.82,            // Total equity
  "margin_used": 1234.56,        // Used margin
  "margin_ratio": 2.46,          // Margin ratio %
  "unrealized_pnl": 245.82,
  "realized_pnl_today": 150.50
}

// GET /api/v1/risk - Risk metrics
{
  "daily_pnl": 150.50,
  "daily_pnl_pct": 0.30,         // % của starting equity
  "daily_starting_equity": 50000.00,
  "available_balance": 48765.44,
  "pending_margin": 500.00,
  "total_notional": 15000.00,    // Tổng position notional
  "max_total_notional": 50000.00, // Giới hạn
  "is_paused": false,
  "pause_reason": null,
  "open_positions": 3,
  "max_open_positions": 10,
  "positions_by_symbol": {
    "BTCUSDT": { "count": 1, "notional": 5000 },
    "ETHUSDT": { "count": 2, "notional": 10000 }
  }
}

// GET /api/v1/orders - Open orders (fill existing TODO)
{
  "orders": [
    {
      "order_id": "...",
      "symbol": "BTCUSDT",
      "side": "BUY",
      "type": "LIMIT",
      "price": 45000.00,
      "quantity": 0.1,
      "filled_qty": 0.05,
      "status": "PARTIALLY_FILLED",
      "created_at": "..."
    }
  ],
  "total_orders": 5,
  "pending_notional": 2500.00
}

// GET /api/v1/farming - Volume farming metrics
{
  "active_grids": 3,
  "total_grids": 10,
  "volume_24h": 150000.00,
  "volume_7d": 1200000.00,
  "estimated_funding_24h": -25.50,
  "grid_configs": [
    {
      "symbol": "BTCUSDT",
      "status": "active",
      "levels": 20,
      "spread_pct": 0.5,
      "position_size": 100.00,
      "unrealized_pnl": 12.50,
      "volume_24h": 50000.00
    }
  ]
}

// GET /api/v1/summary - Daily summary (consolidated)
{
  "date": "2026-04-06",
  "starting_equity": 50000.00,
  "current_equity": 50245.82,
  "total_pnl": 245.82,
  "total_pnl_pct": 0.49,
  "trades": {
    "total": 25,
    "winning": 15,
    "losing": 10,
    "win_rate": 60.0
  },
  "orders": {
    "placed": 30,
    "filled": 25,
    "cancelled": 3,
    "rejected": 2
  },
  "volume": 150000.00,
  "strategies_active": 5
}
```

### 1.2 Enhanced WebSocket Events

```typescript
// Thêm vào WS subscription types
interface WsMessage {
  type: 'activity' | 'alert' | 'metrics' | 'position' | 'order' | 'account'
  payload: any
}

// Real-time metrics update
{
  type: 'metrics',
  payload: {
    daily_pnl: 150.50,
    unrealized_pnl: 245.82,
    equity: 50245.82,
    timestamp: "..."
  }
}

// Position update
{
  type: 'position',
  payload: {
    symbol: "BTCUSDT",
    side: "LONG",
    size: 0.1,
    entry_price: 45000,
    mark_price: 45100,
    unrealized_pnl: 10.00
  }
}
```

---

## Phase 2: Frontend Refactor

### 2.1 Design System (Unified)

**Color Palette (Dark Theme - Giữ nguyên từ App.vue)**
```css
--bg-primary: #0b0e11;      /* Main background */
--bg-secondary: #151a1e;     /* Card/sidebar background */
--bg-tertiary: #1e2329;    /* Elevated elements */
--border: #2b3139;         /* Borders */
--text-primary: #ffffff;
--text-secondary: #9ca3af;
--accent-primary: #40baf7; /* Blue accent */
--accent-success: #0ecb81; /* Green */
--accent-danger: #f6465d;  /* Red */
--accent-warning: #f0b90b; /* Yellow */
```

**Typography**
```css
--font-sans: 'Inter', system-ui, sans-serif;
--font-mono: 'JetBrains Mono', 'Fira Code', monospace;
```

### 2.2 Component Structure mới

```
frontend/src/
├── components/
│   ├── ui/                    # Base UI components
│   │   ├── Card.vue
│   │   ├── Badge.vue
│   │   ├── Button.vue
│   │   ├── Table.vue
│   │   ├── StatValue.vue        # Hiển thị số liệu với formatting
│   │   └── PnLDisplay.vue       # P&L với màu +/-/0
│   ├── layout/
│   │   ├── Sidebar.vue          # Tách từ App.vue
│   │   ├── Header.vue           # Tách từ App.vue
│   │   └── PageContainer.vue
│   └── charts/
│       ├── MiniSparkline.vue    # Biểu đồ nhỏ cho trend
│       └── EquityChart.vue      # Biểu đồ equity
├── composables/
│   ├── useWebSocket.ts          # Unified WS handler
│   ├── useAutoRefresh.ts        # Auto-refresh pattern
│   └── useFormat.ts             # Number/formatting helpers
├── views/
│   ├── Dashboard.vue            # Overview + key metrics
│   ├── Positions.vue            # Positions chi tiết
│   ├── Orders.vue               # Open orders
│   ├── Account.vue              # Account/Wallet
│   ├── Risk.vue                 # Risk metrics
│   ├── Farming.vue              # Volume farming
│   ├── Strategies.vue           # Refactored
│   └── Activity.vue             # Refactored
├── stores/
│   └── dashboard.ts             # Pinia store cho shared state
├── utils/
│   └── formatters.ts            # Currency, number, date formatting
└── styles/
    ├── variables.css            # CSS variables
    └── global.css               # Global styles
```

### 2.3 Shared Components chi tiết

**StatValue.vue** - Hiển thị metric
```vue
<template>
  <div class="stat-card">
    <div class="stat-label">{{ label }}</div>
    <div class="stat-value" :class="valueClass">
      {{ formattedValue }}
    </div>
    <div v-if="change !== undefined" class="stat-change" :class="changeClass">
      {{ formattedChange }}
    </div>
  </div>
</template>
```

**PnLDisplay.vue** - P&L với màu
```vue
<template>
  <span class="pnl" :class="{ 'positive': value > 0, 'negative': value < 0 }">
    {{ sign }}${{ Math.abs(value).toFixed(2) }}
  </span>
</template>
```

### 2.4 Composables mới

**useWebSocket.ts**
```typescript
export function useWebSocket() {
  const ws = ref<WebSocket | null>(null)
  const isConnected = ref(false)
  const lastMessage = ref<any>(null)
  
  const connect = (url: string) => { /* ... */ }
  const subscribe = (channel: string) => { /* ... */ }
  const onMessage = (handler: (msg: any) => void) => { /* ... */ }
  
  return { ws, isConnected, connect, subscribe, onMessage, lastMessage }
}
```

**useAutoRefresh.ts**
```typescript
export function useAutoRefresh<T>(
  fetchFn: () => Promise<T>,
  interval: number = 3000
) {
  const data = ref<T | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  
  // Auto fetch + interval
  // Cleanup on unmount
  
  return { data, loading, error, refresh: fetchFn }
}
```

---

## Phase 3: Views Detail

### 3.1 Dashboard.vue (Refactored)

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│  [Stats Grid - 4 cards]                                     │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐            │
│  │ Equity  │ │ Daily   │ │ Unreal  │ │ Open    │            │
│  │ $50,245 │ │ +$150   │ │ +$245   │ │ 3       │            │
│  └─────────┘ └─────────┘ └─────────┘ └─────────┘            │
├─────────────────────────────────────────────────────────────┤
│  [Mini Chart]    │  [Position Summary]                      │
│  Equity 24h trend │  BTC +0.1  ETH -0.5                      │
├─────────────────────────────────────────────────────────────┤
│  [Quick Actions]                                            │
│  [Pause Bot] [Close All] [Export Report]                    │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Stats grid với real-time update
- Mini sparkline cho equity trend
- Position summary cards
- Quick action buttons
- Recent activity (last 5 events)

### 3.2 Positions.vue (New)

**Features:**
- Full positions table với sort/filter
- Mark price + Unrealized PnL real-time
- Close position button
- Symbol grouping
- PnL breakdown by symbol

**Columns:**
| Symbol | Side | Size | Entry | Mark | Unrealized | Leverage | Actions |

### 3.3 Orders.vue (New)

**Features:**
- Open orders list
- Cancel order button
- Order status badges
- Filled % progress bar
- Time since created

### 3.4 Account.vue (New)

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│  [Account Overview]                                         │
│  Balance: $48,765  Equity: $50,246  Margin: 2.46%           │
├─────────────────────────────────────────────────────────────┤
│  [Allocation]                                               │
│  Pie chart: Available / In Positions / Pending Orders       │
├─────────────────────────────────────────────────────────────┤
│  [Equity History]                                           │
│  Line chart: 7d, 30d, 90d equity trend                      │
└─────────────────────────────────────────────────────────────┘
```

### 3.5 Risk.vue (New)

**Features:**
- Daily P&L vs Limit progress bar
- Position count vs Max
- Notional exposure vs Max
- Margin usage %
- Pause status + reason
- Risk events log

**Metrics:**
```
Daily P&L:     [$150 / $500 limit] ████████░░ 30%
Positions:     [3 / 10] █████░░░░░ 30%
Notional:      [$15k / $50k] ████████░░ 30%
Margin:        2.46%
Status:        ✅ Active
```

### 3.6 Farming.vue (New)

**Features:**
- Active grids list
- Volume stats (24h, 7d)
- Funding rate impact estimate
- Grid performance per symbol
- Turnover ratio

### 3.7 Activity.vue (Refactored)

**Changes:**
- Unified styling với design system
- Thêm filter presets (Today, Last Hour, etc.)
- Real-time badge animation
- Export to CSV/JSON

### 3.8 Strategies.vue (Refactored)

**Changes:**
- Unified styling
- Thêm strategy performance metrics
- Enable/disable với confirmation
- Strategy detail expansion

---

## Phase 4: Navigation Updates

### New Navigation Structure

```typescript
const navItems = [
  { name: 'Dashboard', icon: LayoutDashboard, path: '/' },
  { name: 'Positions', icon: BarChart3, path: '/positions' },
  { name: 'Orders', icon: ListOrdered, path: '/orders' },
  { name: 'Account', icon: Wallet, path: '/account' },
  { name: 'Risk', icon: Shield, path: '/risk' },
  { name: 'Farming', icon: Sprout, path: '/farming' },
  { name: 'Strategies', icon: Zap, path: '/strategies' },
  { name: 'Activity', icon: FileText, path: '/activity' },
]
```

---

## Phase 5: Implementation Order

### Sprint 1: Foundation
1. Tạo design system (CSS variables, base components)
2. Tách Sidebar + Header từ App.vue
3. Tạo shared components (Card, Badge, StatValue, PnLDisplay)
4. Tạo composables (useWebSocket, useAutoRefresh, useFormat)

### Sprint 2: Backend APIs
1. `GET /api/v1/account` - Account info
2. `GET /api/v1/risk` - Risk metrics  
3. `GET /api/v1/orders` - Open orders
4. `GET /api/v1/farming` - Farming stats
5. `GET /api/v1/summary` - Daily summary
6. Enhance WebSocket với metrics/position updates

### Sprint 3: Views
1. Refactor Dashboard.vue
2. Create Positions.vue
3. Create Orders.vue
4. Create Account.vue
5. Create Risk.vue
6. Create Farming.vue
7. Refactor Strategies.vue + Activity.vue

### Sprint 4: Polish
1. Add transitions/animations
2. Error states + loading skeletons
3. Mobile responsive
4. Testing

---

## API Implementation Details

### Backend Changes Required

File: `backend/internal/api/server.go`

Thêm handlers:
```go
s.mux.HandleFunc("GET /api/v1/account", s.handleAccount)
s.mux.HandleFunc("GET /api/v1/risk", s.handleRisk)
s.mux.HandleFunc("GET /api/v1/farming", s.handleFarming)
s.mux.HandleFunc("GET /api/v1/summary", s.handleSummary)
```

Risk Manager cần expose thêm methods:
```go
func (m *Manager) GetAvailableBalance() float64
func (m *Manager) GetDailyStartingEquity() float64
func (m *Manager) GetPendingMargin() float64
func (m *Manager) GetSymNotional() map[string]float64
```

Engine cần expose:
```go
func (e *Engine) GetAccountInfo() AccountInfo
func (e *Engine) GetOpenOrders() []Order
```

---

## Tech Stack

- **Framework**: Vue 3 + TypeScript (giữ nguyên)
- **Build**: Vite (giữ nguyên)
- **Routing**: Vue Router (giữ nguyên)
- **Styling**: Tailwind CSS (đã có, cần standardize)
- **Icons**: lucide-vue-next (giữ nguyên)
- **State**: Pinia (thêm mới cho complex state)
- **Charts**: Lightweight chart lib (ví dụ: vue-chartjs or lightweight-charts)
- **Date**: date-fns (đã có)

---

## Files to Create/Modify

### New Files (~25 files)
```
frontend/src/components/ui/Card.vue
frontend/src/components/ui/Badge.vue
frontend/src/components/ui/Button.vue
frontend/src/components/ui/StatValue.vue
frontend/src/components/ui/PnLDisplay.vue
frontend/src/components/layout/Sidebar.vue
frontend/src/components/layout/Header.vue
frontend/src/components/layout/PageContainer.vue
frontend/src/components/charts/MiniSparkline.vue
frontend/src/composables/useWebSocket.ts
frontend/src/composables/useAutoRefresh.ts
frontend/src/composables/useFormat.ts
frontend/src/stores/dashboard.ts
frontend/src/utils/formatters.ts
frontend/src/styles/variables.css
frontend/src/styles/global.css
frontend/src/views/Positions.vue
frontend/src/views/Orders.vue
frontend/src/views/Account.vue
frontend/src/views/Risk.vue
frontend/src/views/Farming.vue
```

### Modified Files (~8 files)
```
frontend/src/App.vue              # Tách sidebar/header
frontend/src/views/Dashboard.vue  # Refactor hoàn toàn
frontend/src/views/Strategies.vue # Refactor styling
frontend/src/views/Activity.vue   # Refactor styling
frontend/src/router/index.ts      # Thêm routes mới
frontend/src/api/types.ts         # Thêm types mới
frontend/src/api/client.ts        # Giữ nguyên
backend/internal/api/server.go    # Thêm handlers
```

---

## Success Metrics

- [ ] Tất cả views dùng chung design system
- [ ] Real-time data updates qua WebSocket
- [ ] Account/Risk/Farming data hiển thị đầy đủ
- [ ] Responsive trên mobile
- [ ] Load time < 2s
