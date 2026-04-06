# Aster Bot - Grid Trading System

## Tổng Quan Hệ Thống

Bot giao dịch dựa trên chiến lược **Grid Trading** tối ưu cho **Volume Farming** - tạo nhiều lệnh limit nhỏ xung quanh giá hiện tại để tận dụng biến động giá nhỏ và tích lũy volume giao dịch.

---

## Kiến Trúc Hệ Thống

```
┌─────────────────────────────────────────────────────────────────┐
│                    Volume Farm Engine                           │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ Symbol       │  │ Grid         │  │ Adaptive     │          │
│  │ Selector     │  │ Manager      │  │ Grid Manager │          │
│  │ (Chọn coin)  │  │ (Quản lý     │  │ (Tối ưu      │          │
│  │              │  │  lưới lệnh)  │  │  thông số)   │          │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘          │
│         │                 │                 │                   │
│         ▼                 ▼                 ▼                   │
│  ┌──────────────────────────────────────────────────────┐     │
│  │              WebSocket Price Feed                     │     │
│  │         (Nhận giá real-time từ sàn)                  │     │
│  └──────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌──────────────────┐
                    │   Futures API    │
                    │  (Đặt lệnh thực) │
                    └──────────────────┘
```

---

## Cơ Chế Vào Lệnh (Order Entry)

### 1. Tạo Grid Orders

Khi có giá mới từ WebSocket, hệ thống tạo các lệnh limit theo công thức:

```
Với ETH @ $2062, spread = 0.002% (0.00002), max_orders_per_side = 30

BUY Orders (dưới giá hiện tại):
  Level 1: $2062 - ($2062 × 0.00002 × 1) = $2061.96
  Level 2: $2062 - ($2062 × 0.00002 × 2) = $2061.92
  Level 3: $2062 - ($2062 × 0.00002 × 3) = $2061.88
  ... cho đến Level 30

SELL Orders (trên giá hiện tại):
  Level 1: $2062 + ($2062 × 0.00002 × 1) = $2062.04
  Level 2: $2062 + ($2062 × 0.00002 × 2) = $2062.08
  Level 3: $2062 + ($2062 × 0.00002 × 3) = $2062.12
  ... cho đến Level 30
```

**Tổng: 60 lệnh/symbol** (30 BUY + 30 SELL)

### 2. Kích Thước Lệnh

```
Order Size = Order Size USDT / Giá
Ví dụ: $6 / $2062 = 0.00291 ETH
```

**Ràng buộc:**
- Minimum notional: $5 (theo yêu cầu sàn)
- Nếu tính toán < $5, tự động điều chỉnh lên $5.1

### 3. Placement Workers

- **20 concurrent workers** đặt lệnh đồng thời
- Mỗi lệnh được xử lý async qua goroutine riêng
- Rate limiter: 100 tokens, refill 20 tokens/giây

---

## Cơ Chế Ra Lệnh (Order Execution)

### 1. Khi Giá Chạm Lệnh Limit

```
Ví dụ: Giá ETH giảm từ $2062 → $2061.96
→ Lệnh BUY Level 1 được fill
→ Hệ thống phát hiện fill qua WebSocket
```

### 2. Xử Lý Fill Event

```
1. Deduplicator kiểm tra duplicate fill
2. StateValidator kiểm tra state transition hợp lệ
3. Cập nhật inventory trong AdaptiveGridManager
4. Trigger rebalancing → Tạo lệnh mới ngay lập tức
```

### 3. Rebalancing (Tái Cân Bằng)

Khi 1 lệnh được fill:

```
Ví dụ: Lệnh BUY @ $2061.96 filled
→ Grid cần được "rebalance"
→ Tạo thêm lệnh BUY mới ở level thấp hơn
→ HOẶC đẩy các lệnh SELL xuống gần giá mới hơn
```

---

## Cơ Chế Out Lệnh (Order Exit/Cancellation)

### 1. Tự Động Hủy Khi Fill

- Khi lệnh BUY filled → Tự động hủy các lệnh BUY khác ở xa
- Khi lệnh SELL filled → Tự động hủy các lệnh SELL khác ở xa

### 2. Reset Incomplete Grid

```
Mỗi 3 giây, hệ thống kiểm tra:
- Nếu grid đã placed nhưng thiếu lệnh
→ Đánh dấu OrdersPlaced = false
→ Trigger placement lại
```

### 3. Cooldown Logic

```
Base cooldown: 200ms (giữa các lần placement)
Nếu có lỗi liên tiếp > 2 lần:
→ Tăng cooldown lên 3 giây (tránh spam)
```

---

## Luồng Dữ Liệu Chi Tiết

### Bước 1: Khởi Động

```
1. Load config từ volume-farm-config.yaml
2. Khởi tạo GridManager với các thông số:
   - maxOrdersSide: 30
   - gridSpreadPct: 0.002%
   - baseNotionalUSD: $6
3. Kết nối WebSocket đến sàn giao dịch
4. Khởi động 20 placement workers
5. Khởi động symbol selector (tìm coin phù hợp)
```

### Bước 2: Nhận Giá Real-time

```
WebSocket Push:
{
  "s": "ETHUSD1",      // Symbol
  "c": "2062.50",      // Last price
  ...
}

→ Cập nhật grid.CurrentPrice = $2062.50
→ Kiểm tra shouldSchedulePlacement()
```

### Bước 3: Quyết Định Placement

```
shouldSchedulePlacement() trả về true nếu:
1. Grid đang active
2. Có giá hợp lệ (> 0)
3. Không đang busy (PlacementBusy = false)
4. Qua cooldown period (200ms)
5. HOẶC: Grid incomplete (thiếu lệnh)
```

### Bước 4: Tạo Orders

```
placeGridOrders(symbol, grid):
1. Tính spreadAmount = giá × 0.002%
2. Vòng lặp i từ 1 → 30:
   - Tạo BUY order @ giá - (spread × i)
   - Tạo SELL order @ giá + (spread × i)
3. Tính toán kích thước order ($6 / giá)
4. Trả về danh sách 60 orders
```

### Bước 5: Đặt Lệnh Concurrent

```
Với mỗi order trong danh sách:
  ├─ Tạo goroutine mới
  ├─ Gọi placeOrderAsync()
  │   ├─ Kiểm tra context (không bị cancel)
  │   ├─ Gọi placeOrder() (API call)
  │   │   ├─ Rate limit check (100 tokens)
  │   │   ├─ Round quantity & price
  │   │   ├─ POST /fapi/v1/order
  │   │   └─ Lưu orderID vào activeOrders
  │   └─ Trả kết quả về successChan
  └─ Đợi tất cả goroutines hoàn thành
```

### Bước 6: Xử Lý Fill

```
Khi nhận fill event từ WebSocket:
1. deduplicator.IsDuplicate()? → Skip nếu duplicate
2. deduplicator.RecordEvent() → Đánh dấu đã xử lý
3. stateValidator.IsValidTransition()? → Kiểm tra state
4. Cập nhật order.Status = "FILLED"
5. Di chuyển từ activeOrders → filledOrders
6. adaptiveMgr.TrackInventoryPosition() → Cập nhật inventory
7. canRebalance()? → Kiểm tra risk limits
8. enqueuePlacement(symbol) → Trigger rebuild grid
```

### Bước 7: Rebuild Grid

```
Sau khi fill:
1. Đánh dấu grid.OrdersPlaced = false
2. Enqueue symbol vào placement queue
3. Worker tiếp theo sẽ xử lý
4. Tạo grid mới với giá hiện tại
```

---

## Các Thông Số Quan Trọng

| Thông số | Giá trị | Ý nghĩa |
|----------|---------|---------|
| `grid_spread_pct` | 0.002% | Khoảng cách giữa các lệnh trong grid |
| `max_orders_per_side` | 30 | Số lệnh mỗi bên (BUY/SELL) |
| `order_size_usdt` | $6 | Giá trị mỗi lệnh |
| `grid_placement_cooldown` | 1 giây | Thời gian chờ giữa các lần đặt lệnh |
| `rate_limiter_capacity` | 100 | Số request tối đa trong bucket |
| `rate_limiter_refill_rate` | 20 | Số request refill mỗi giây |
| `num_workers` | 20 | Số workers xử lý đồng thời |

---

## Cơ Chế Bảo Vệ (Safeguards)

### 1. Deduplicator
- Tránh xử lý fill event trùng lặp
- Dựa trên orderID + timestamp

### 2. StateValidator
- Kiểm tra state transition hợp lệ
- Ví dụ: NEW → PENDING → FILLED (hợp lệ)
- NEW → FILLED (bỏ qua, không hợp lệ)

### 3. Rate Limiter
- Giới hạn số API call/giây
- Tránh bị ban IP từ sàn

### 4. Risk Checker
- Kiểm tra daily loss limit
- Kiểm tra max position size
- Kiểm tra drawdown

---

## Ví Dụ Thực Tế

### Scenario 1: Giá Dao Động Nhẹ

```
ETH @ $2062.00
Grid đặt sẵn:
  BUY: 2061.96, 2061.92, 2061.88... (30 levels)
  SELL: 2062.04, 2062.08, 2062.12... (30 levels)

Giá giảm → $2061.95:
  → Lệnh BUY @ 2061.96 FILLED
  → Grid rebuild với giá mới $2061.95
  → Tạo BUY mới @ 2061.91, 2061.87...
  → Tạo SELL @ 2061.99, 2062.03...

Volume farmed: 1 lệnh × $6 = $6
```

### Scenario 2: Giá Tăng Mạnh

```
ETH @ $2062.00 → Tăng lên $2080.00

Các lệnh SELL liên tục được fill:
  SELL @ 2062.04 → FILLED (Rebuild)
  SELL @ 2062.08 → FILLED (Rebuild)
  ...cho đến khi giá dừng

Kết quả: Nhiều lệnh SELL filled, thu lợi nhuận tích lũy
```

---

## Debug & Monitoring

### Log Levels

```
INFO: Grid created, orders placed, fills detected
WARN: Rate limit, order placement failed
ERROR: WebSocket disconnect, API errors
DEBUG: Detailed order flow (cần bật debug mode)
```

### Metrics

```
- total_orders_placed: Tổng số lệnh đã đặt
- total_orders_filled: Tổng số lệnh đã fill
- fill_rate: Tỷ lệ fill (filled/placed)
- total_volume_usdt: Tổng volume đã giao dịch
- active_orders: Số lệnh đang active
```

---

## Lưu Ý Quan Trọng

1. **Spread chặt (0.002%)** = Nhiều fill hơn nhưng lợi nhuận nhỏ hơn mỗi lệnh
2. **30 orders/side** = Nhiều coverage nhưng cần nhiều margin hơn
3. **Cooldown 1 giây** = Đủ nhanh để farm nhưng tránh spam
4. **Rebalance ngay lập tức** = Không bỏ lỡ cơ hội nhưng cần cẩn thận risk

---

## File Structure

```
backend/
├── cmd/
│   └── volume-farm/
│       └── main.go              # Entry point
├── internal/
│   ├── farming/
│   │   ├── grid_manager.go      # Core grid logic
│   │   ├── volume_farm_engine.go # Engine orchestration
│   │   └── adaptive_grid/       # Optimization modules
│   ├── client/
│   │   ├── futures.go           # API client
│   │   └── websocket.go         # WebSocket handler
│   └── config/
│       └── config.go            # Config management
└── config/
    └── volume-farm-config.yaml  # Trading parameters
```

---

**Build & Run:**
```bash
cd backend
go run ./cmd/volume-farm/main.go
```
