# Chiến Lược Grid Trading - Giải Thích Nghiệp Vụ Chi Tiết

> Tài liệu này giải thích chi tiết chiến lược giao dịch lưới (Grid Trading) từ góc độ nghiệp vụ - Bot đang trade như thế nào, khi nào vào lệnh, khi nào ra lệnh, quản lý rủi ra sao.

---

## 📑 Mục Lục

1. [Tổng Quan Chiến Lược](#1-tổng-quan-chiến-lược)
2. [Cơ Chế Vào Lệnh (Entry Logic)](#2-cơ-chế-vào-lệnh-entry-logic)
3. [Cơ Chế Ra Lệnh (Exit Logic)](#3-cơ-chế-ra-lệnh-exit-logic)
4. [Quản Lý Rủi Ro (Risk Management)](#4-quản-lý-rủi-ro-risk-management)
5. [Chốt Lãi (Take Profit)](#5-chốt-lãi-take-profit)
6. [Cắt Lỗ (Stop Loss)](#6-cắt-lỗ-stop-loss)
7. [Các Kịch Bản Thị Trường](#7-các-kịch-bản-thị-trường)
8. [Cấu Hình và Tham Số](#8-cấu-hình-và-tham-số)
9. [KPI và Đánh Giá Hiệu Quả](#9-kpi-và-đánh-giá-hiệu-quả)

---

## 1. Tổng Quan Chiến Lược

### 1.1 Grid Trading Là Gì?

**Grid Trading** là chiến lược giao dịch tự động đặt nhiều lệnh mua (BUY) và bán (SELL) ở các mức giá cách đều nhau tạo thành một "lưới" (grid) xung quanh giá hiện tại.

### 1.2 Nguyên Lý Cơ Bản

```
                    Grid Trading Visualization
                    ========================

Giá Hiện Tại: $100

Lệnh BÁN (Sell Orders)          Giá            Lệnh MUA (Buy Orders)
═══════════════════════════════╦═════════════════════════════════════════════
    S5 (Sell $10) ◄────────────║── $105        ──────────────► B5 (Buy $10)
    S4 (Sell $10) ◄────────────║── $104        ──────────────► B4 (Buy $10)
    S3 (Sell $10) ◄────────────║── $103        ──────────────► B3 (Buy $10)
    S2 (Sell $10) ◄────────────║── $102        ──────────────► B2 (Buy $10)
    S1 (Sell $10) ◄────────────║── $101        ──────────────► B1 (Buy $10)
                               ║
                      $100 ◄──═╩══ GIÁ HIỆN TẠI (Mark Price)
                               ║
                               ║
    ═══════════════════════════════════════════════════════════════════
    PHẠM VI LƯỚI: Khoảng 5% mỗi bên (từ $95 đến $105)
    TỔNG EXPOSURE: $50 LONG + $50 SHORT = $100 tối đa
```

### 1.3 Cách Bot Kiếm Lãi

Bot **không dự đoán hướng giá**, mà kiếm lãi từ **biên độ dao động** của thị trường:

| Kịch Bản | Cơ Chế Kiếm Lãi |
|----------|----------------|
| **Sideway** (Đi ngang) | Mua thấp → Bán cao, lặp lại nhiều lần |
| **Trending Up** (Tăng) | Tích lũy LONG → Bán dần khi giá lên |
| **Trending Down** (Giảm) | Tích lũy SHORT → Mua lại khi giá xuống |

---

## 2. Cơ Chế Vào Lệnh (Entry Logic)

### 2.1 Khi Nào Bot Vào Lệnh?

Bot vào lệnh dựa trên **triggers** sau:

#### A. **Price Grid Trigger** (Chính)
```
Điều kiện: Giá thị trường chạm mức lưới

Ví dụ:
- Giá đang $100, có lệnh BUY ở $99
- Giá giảm xuống $99 → Lệnh BUY được khớp
- Bot tự động đặt lệnh SELL mới ở $101 (cách 2% nếu spread=1%)
```

#### B. **Auto-Rebalance Trigger**
```
Điều kiện: Lệnh cũ được khớp (filled)

Flow:
1. Lệnh BUY $99 khớp
2. Bot phát hiện qua WebSocket
3. Ngay lập tức đặt lệnh thay thế:
   - Nếu vừa mua → Đặt SELL ở $101
   - Duy trì số lệnh trong lưới không đổi
```

#### C. **Time-Based Trigger**
```
Điều kiện: Đến khung giờ được phép trade (nếu có cấu hình)

Ví dụ cấu hình:
- Chỉ trade 9h-11h sáng và 2h-4h chiều
- Ngoài khung giờ: Bot dừng đặt lệnh mới
```

### 2.2 Logic Đặt Giá Lưới

```
Công thức tính giá lưới:

Spread Amount = Current Price × Grid Spread %

Ví dụ:
- Current Price: $100
- Grid Spread: 0.5%
- Spread Amount: $100 × 0.5% = $0.50

→ Lệnh BUY ở: $100 - $0.50 = $99.50
→ Lệnh SELL ở: $100 + $0.50 = $100.50
```

### 2.3 Bảng Tham Số Vào Lệnh

| Tham Số | Ý Nghĩa | Ví Dụ | Ảnh Hưởng |
|---------|---------|-------|-----------|
| **Grid Spread** | Khoảng cách giữa 2 lệnh | 0.5% | Spread nhỏ = Khớp nhiều, lãi ít/lệnh |
| **Max Orders/Side** | Số lệnh tối đa mỗi bên | 5 | Càng nhiều = Phạm vi bảo vệ càng rộng |
| **Order Size** | Số tiền mỗi lệnh | $10 USDT | Quyết định exposure tối đa |
| **Min Notional** | Giá trị tối thiểu | $5.1 | Do sàn quy định, bot thêm margin an toàn |

### 2.4 Ví Dụ Thực Tế - Vào Lệnh

```
Scenario: Giá BTC đang $50,000
Cấu hình: Spread 0.1%, 3 lệnh/side, $100/lệnh

Bước 1: Bot tính toán lưới
- Spread Amount: $50,000 × 0.1% = $50

Bước 2: Bot đặt lệnh
SELL Side (trên giá hiện tại):
  - S3: $50,150 (giá + 3×spread)
  - S2: $50,100 (giá + 2×spread)
  - S1: $50,050 (giá + 1×spread)

BUY Side (dưới giá hiện tại):
  - B1: $49,950 (giá - 1×spread)
  - B2: $49,900 (giá - 2×spread)
  - B3: $49,850 (giá - 3×spread)

Bước 3: Chờ giá chạm lưới...
→ Nếu giá giảm xuống $49,950: Lệnh B1 khớp
→ Bot tự động đặt SELL ở $50,050 (B1 + 2×spread)
```

---

## 3. Cơ Chế Ra Lệnh (Exit Logic)

### 3.1 Các Trường Hợp Ra Lệnh

| Loại | Điều Kiện | Hành Động |
|------|-----------|-----------|
| **Fill** | Lệnh được khớp hoàn toàn | Tạo lệnh đối xứng mới |
| **Partial Fill** | Khớp một phần | Tiếp tục chờ, không làm gì |
| **Cancel** | Hết hiệu lực hoặc thủ công | Xóa khỏi hệ thống |
| **Emergency Close** | Rủi ro cao | Đóng tất cả vị thế |

### 3.2 Logic Khi Lệnh Được Khớp (Filled)

```
Flow khi lệnh BUY $99 được khớp:

1. WebSocket nhận update: "Order #12345 FILLED"
2. Bot xác nhận: Lệnh BUY $99 khớp $10
3. Bot tính toán:
   - Vừa mua thêm $10 LONG exposure
   - Cần đặt lệnh SELL để chốt lãi
4. Bot đặt lệnh mới:
   - SELL ở $101 (nếu spread = 1%)
   - Size: $10 (bằng lệnh vừa khớp)
5. Cập nhật metrics:
   - Tổng volume += $10
   - Số lệnh khớp += 1
   - PnL thực = 0 (chưa chốt)
```

### 3.3 Ví Dụ Ra Lệnh Thực Tế

```
Scenario: Thị trường sideway

Timeline:
────────────────────────────────────────────────
T0: Giá $100
    → Bot có lệnh BUY $99, SELL $101

T1: Giá $98:  Khớp BUY $98
         → Đặt SELL $100

T2: Giá $102: Khớp SELL $102 (lệnh cũ), BUY $100 (lệnh mới)
          → Lãi: $2 (bán $102, mua $100)
          
T3: Giá $99:  Khớp BUY $99
         → Đặt SELL $101

T4: Giá $101: Khớp SELL $101
          → Lãi: $2 (bán $101, mua $99)
────────────────────────────────────────────────

Kết quả sau 1 ngày:
- Số lệnh khớp: 50 lệnh
- Tổng lãi: $100 (trên $1000 vốn = 10%)
- Rủi ro: Thấp (luôn hedged)
```

---

## 4. Quản Lý Rủi Ro (Risk Management)

### 4.1 Các Lớp Bảo Vệ Rủi Ro

```
Layer 1: Position Limits (Giới hạn vị thế)
├── Max Position per Symbol: $300
├── Max Total Exposure: $500
└── Max Orders per Side: 5

Layer 2: Loss Limits (Giới hạn lỗ)
├── Per-Position Loss: $1 (đóng ngay nếu lỗ >$1)
├── Max Unrealized Loss: $3/position
└── Total Net Loss: $5 (đóng tất cả nếu tổng lỗ >$5)

Layer 3: Drawdown Protection
├── Daily Drawdown: 20%
├── Emergency Stop: Dừng bot nếu drawdown >20%
└── Cooldown: Nghỉ 5 phút sau 3 lỗ liên tiếp

Layer 4: Market Regime Detection
├── Trending Market: Thu hẹp lưới, giảm số lệnh
├── Ranging Market: Mở rộng lưới, tăng số lệnh
└── Volatile Market: Tăng spread, tạm dừng nếu cần
```

### 4.2 Cơ Chế Adaptive Grid

Bot tự động điều chỉnh theo điều kiện thị trường:

| Chế Độ Thị Trường | Nhận Diện | Hành Động | Mục Đích |
|------------------|-----------|-----------|----------|
| **Trending Up** | ADX > 25, +DI > -DI | Giảm số lệnh BUY, tăng SELL | Giảm tích lũy LONG |
| **Trending Down** | ADX > 25, -DI > +DI | Giảm số lệnh SELL, tăng BUY | Giảm tích lũy SHORT |
| **Ranging** | ADX < 20, giá dao động | Mở rộng lưới, tăng số lệnh | Tối đa hóa lãi |
| **Volatile** | ATR tăng đột biến | Tăng spread, giảm size | Tránh slippage |

### 4.3 Ví Dụ Risk Management Thực Tế

```
Scenario: Thị trường trending mạnh

T0: Giá $100, Bot cấu hình 5 lệnh/side
T1: Giá tăng liên tục $100 → $110

Không có Adaptive Grid:
→ Tất cả lệnh BUY ($99, $98, $97, $96, $95) khớp hết
→ Bot giữ vị thế LONG $50
→ Nếu giá quay đầu: Lỗ lớn

Có Adaptive Grid:
→ Bot phát hiện trending (tăng)
→ Tự động giảm xuống 2 lệnh BUY/side
→ Exposure tối đa chỉ $20
→ Nếu giá quay đầu: Lỗ nhỏ, dễ phục hồi
```

---

## 5. Chốt Lãi (Take Profit)

### 5.1 Cơ Chế Chốt Lãi Trong Grid Trading

Khác với trading truyền thống, Grid Trading **không có TP cố định**. Thay vào đó:

#### A. **Hedging Tự Động**
```
Cơ chế: Mỗi lệnh mua được "chốt lãi" bởi lệnh bán đối xứng

Ví dụ:
1. Mua $99 (BUY)
2. Giá tăng lên $101
3. Bán $101 (SELL)
4. Kết quả: Lãi $2 (không cần đặt TP)
```

#### B. **R:R Ratio TP (Mới)**
```
Bot mới cập nhật thêm TP dựa trên Risk:Reward ratio:

Công thức:
TP Price = Entry Price ± (Risk Distance × R:R Ratio)

Ví dụ:
- Entry: $100
- Stop Loss: $98 (risk $2)
- R:R = 1.5
→ TP = $100 + ($2 × 1.5) = $103

Nếu giá chạm $103 → Bot tự động đóng lệnh với lãi $3
```

### 5.2 Bảng So Sánh Cơ Chế Chốt Lãi

| Cơ Chế | Cách Hoạt Động | Ưu Điểm | Nhược Điểm |
|--------|---------------|---------|------------|
| **Hedging** | Buy-Sell đối xứng | Tự động, không cần can thiệp | Lãi nhỏ/lệnh |
| **R:R TP** | Đặt TP theo ratio | Chốt lãi khi đủ R:R | Có thể miss lãi lớn |
| **Breakeven** | Đưa SL về entry | Không lỗ | Có thể bị đánh ra sớm |

---

## 6. Cắt Lỗ (Stop Loss)

### 6.1 Các Loại Stop Loss

#### A. **Fixed Stop Loss** (SL Cố Định)
```
Cấu hình: Stop Loss = Entry Price × (1 - SL%)

Ví dụ:
- Entry: $100
- SL%: 2%
→ SL Price: $98

Nếu giá chạm $98 → Bot đóng lệnh, lỗ $2
```

#### B. **Trailing Stop Loss** (SL Theo Giá)
```
Cơ chế: SL di chuyển theo giá khi có lãi

Ví dụ:
- Entry: $100
- Trailing: 1%
- Kích hoạt khi: Lãi 1% (giá $101)

Timeline:
T0: Giá $100, SL $98 (trailing chưa kích hoạt)
T1: Giá $101 (lãi 1%), SL di chuyển lên $100 (breakeven)
T2: Giá $102 (lãi 2%), SL di chuyển lên $101
T3: Giá giảm về $101 → Chạm SL → Đóng lệnh, lãi $1
```

#### C. **Liquidation Protection** (Bảo Vệ Thanh Lý)
```
Cấu hình: Đóng vị thế khi gần liquidation

Ví dụ (Futures):
- Entry: $100
- Liquidation: $50
- Buffer: 20%
→ Đóng ở: $50 + 20% × ($100-$50) = $60

Nếu giá chạm $60 → Bot đóng hết để tránh thanh lý
```

### 6.2 Bảng Cấu Hình Stop Loss

| Loại SL | Tham Số | Giá Trị Mặc Định | Khi Nào Dùng |
|---------|---------|------------------|-------------|
| **Fixed SL** | `StopLossPct` | 2% | Thị trường ổn định |
| **Trailing SL** | `TrailingStopPct` | 1% | Trending market |
| **Trailing Distance** | `TrailingStopDistancePct` | 0.5% | - |
| **Liquidation Buffer** | `LiquidationBufferPct` | 20% | Luôn dùng cho futures |

---

## 7. Các Kịch Bản Thị Trường

### 7.1 Kịch Bản 1: Sideway (Đi Ngang) - Lý Tưởng

```
Diễn biến giá: $98 → $102 → $99 → $101 → $100

Timeline:
────────────────────────────────────────────────
Giá $100: Bot setup lưới
         
Giá $98:  Khớp BUY $98
         → Đặt SELL $100

Giá $102: Khớp SELL $102 (lệnh cũ), BUY $100 (lệnh mới)
          → Lãi: $2 (bán $102, mua $100)
          
Giá $99:  Khớp BUY $99
         → Đặt SELL $101

Giá $101: Khớp SELL $101
          → Lãi: $2 (bán $101, mua $99)
────────────────────────────────────────────────

Kết quả sau 1 ngày:
- Số lệnh khớp: 50 lệnh
- Tổng lãi: $100 (trên $1000 vốn = 10%)
- Rủi ro: Thấp (luôn hedged)
```

### 7.2 Kịch Bản 2: Trending Up (Tăng Mạnh)

```
Diễn biến giá: $100 → $105 → $110 → $115

Timeline:
────────────────────────────────────────────────
Giá $100: Bot setup lưới 5 lệnh BUY ($99, $98, $97, $96, $95)

Giá $105: Tất cả lệnh BUY khớp hết
          → Bot giữ vị thế LONG $50
          → Các lệnh SELL ($101-$105) dần khớp

Giá $110: Tiếp tục khớp SELL
          → Vị thế LONG giảm dần
          → Lãi tích lũy từ chênh lệch

Giá $115: Có thể hết lệnh SELL để khớp
          → Bot giữ LONG lớn
          → Rủi ro nếu giá quay đầu
────────────────────────────────────────────────

Kết quả:
- Nếu giá tiếp tục tăng: Lãi từ LONG position
- Nếu giá quay đầu: Lỗ từ LONG chưa hedge
→ Adaptive Grid giúp giảm số lệnh BUY khi trending
```

### 7.3 Kịch Bản 3: Trending Down (Giảm Mạnh)

```
Tương tự Trending Up nhưng ngược lại:
- Tích lũy SHORT position
- Các lệnh BUY dần khớp để chốt lãi
- Rủi ro nếu giá bật tăng
```

### 7.4 Kịch Bản 4: Volatile (Biến Động Dữ Dội)

```
Diễn biến giá: $100 → $105 → $95 → $110 → $90 (trong 1 giờ)

Rủi ro:
- Slippage cao (trượt giá)
- Lệnh không kịp đặt lại
- Giá khớp xa so với dự kiến
- Spread thực tế > spread cấu hình → Lỗ

Giải pháp của bot:
1. Tăng spread lên 2-3% (thay vì 0.5%)
2. Giảm order size xuống 50%
3. Tạm dừng nếu biến động >5% trong 5 phút
```

---

## 8. Cấu Hình và Tham Số

### 8.1 File Cấu Hình Chính

```yaml
# volume-farm-config.yaml

volume_farming:
  order_size_usdt: 100          # $100 mỗi lệnh
  grid_spread_pct: 0.001        # 0.1% spread
  max_orders_per_side: 5        # 5 lệnh/side
  placement_cooldown_ms: 100    # 100ms giữa các lệnh
  
risk_management:
  max_position_usdt: 300        # $300 tối đa/symbol
  max_unrealized_loss_usdt: 3   # Đóng nếu lỗ >$3/position
  per_position_loss_limit: 1    # Đóng nếu lỗ >$1
  total_net_loss_limit: 5       # Đóng tất cả nếu lỗ >$5
  daily_drawdown_pct: 20        # Dừng bot nếu drawdown 20%
  
  # Stop Loss
  stop_loss_pct: 0.02           # 2% SL
  trailing_stop_pct: 0.01       # 1% trailing activation
  trailing_stop_distance: 0.005 # 0.5% trailing distance
  liquidation_buffer_pct: 0.2     # 20% buffer
  
  # Take Profit (mới)
  take_profit_rr_ratio: 1.5     # 1.5:1 R:R
  min_take_profit_pct: 0.01     # Tối thiểu 1% TP
  max_take_profit_pct: 0.05     # Tối đa 5% TP
  use_breakeven_tp: true        # Cho phép TP ở breakeven
  
  # Consecutive Loss (mới)
  max_consecutive_losses: 3     # Dừng sau 3 lỗ liên tiếp
  cooldown_after_losses: 5m     # Nghỉ 5 phút
  
  # Directional Bias (mới)
  use_directional_bias: true    # Chỉ trade theo trend
  
  # Correlation (mới)
  correlation_threshold: 0.8    # Không mở symbol tương quan >80%

adaptive_grid:
  trending_threshold: 0.7       # Ngưỡng nhận diện trending
  ranging_spread_multiplier: 1.0 # Giữ nguyên khi ranging
  trending_spread_multiplier: 1.5 # Tăng 50% khi trending
```

### 8.2 Bảng Giá Trị Khuyến Nghị

| Profile | Spread | Orders/Side | Order Size | Risk Level | Expected Return |
|---------|--------|-------------|------------|------------|-----------------|
| **Conservative** | 0.5-1% | 3-5 | $5-10 | Thấp | 1-3%/tháng |
| **Balanced** | 0.2-0.5% | 5-10 | $10-20 | Trung bình | 3-7%/tháng |
| **Aggressive** | 0.1-0.2% | 10-20 | $20-50 | Cao | 7-15%/tháng |

---

## 9. KPI và Đánh Giá Hiệu Quả

### 9.1 Các Chỉ Số Quan Trọng

| KPI | Công Thức | Target | Ý Nghĩa |
|-----|-----------|--------|---------|
| **Fill Rate** | Filled / Placed × 100% | >80% | Tỷ lệ lệnh được khớp |
| **Order Success** | Success / Total × 100% | 100% | Không có lỗi API |
| **Volume/Hour** | Total Volume / Hours | >$10k | Tốc độ sinh volume |
| **PnL/Day** | Daily Profit/Loss | >$0 | Có lãi mỗi ngày |
| **Max Drawdown** | (Peak - Trough) / Peak | <20% | Mức giảm tối đa |
| **Sharpe Ratio** | Return / Volatility | >1 | Hiệu quả rủi ro |

### 9.2 Dashboard Theo Dõi

```
┌─────────────────────────────────────────────────────────────┐
│                   GRID TRADING DASHBOARD                    │
├─────────────────────────────────────────────────────────────┤
│  Metrics (24h)          │  Positions                       │
│  ─────────────────      │  ───────────                     │
│  Volume:    $150,000   │  BTC:  LONG $50, PnL +$2.5       │
│  Orders:    150 placed  │  ETH:  SHORT $30, PnL -$0.5      │
│  Filled:    127 (85%)   │  SOL:  LONG $20, PnL +$1.2       │
│  PnL:       +$125.50    │                                  │
│  Drawdown:  5.2%        │  Total Exposure: $100             │
│                         │  Net PnL:       +$3.2            │
├─────────────────────────────────────────────────────────────┤
│  Active Grids (3)       │  Risk Status: GREEN ✓            │
│  ───────────────        │  Daily Limit: $500/$1000 (50%)   │
│  BTC: ████████████░     │  Drawdown:    5.2%/20% (safe)    │
│  ETH: ████████░░░░░     │  Cooldown:    None               │
│  SOL: ██████░░░░░░░     │                                  │
└─────────────────────────────────────────────────────────────┘
```

### 9.3 Log Mẫu Khi Bot Chạy

```
[INFO] 09:00:01 - Grid initialized for BTC: $50,000
[INFO] 09:00:02 - Placed 5 BUY orders: $49,950 to $49,750
[INFO] 09:00:02 - Placed 5 SELL orders: $50,050 to $50,250
[INFO] 09:05:23 - Order filled: BUY $49,950 (Qty: 0.002 BTC)
[INFO] 09:05:23 - Auto-rebalance: Placed SELL $50,050
[INFO] 09:12:45 - Order filled: SELL $50,050 (Qty: 0.002 BTC)
[INFO] 09:12:45 - Profit: $0.20 (round completed)
[INFO] 09:15:00 - Metrics report: 12 orders filled, PnL: +$1.20
[WARN] 09:30:00 - Trending detected (ADX: 28), reducing BUY orders to 3
[INFO] 10:00:00 - Hourly summary: Volume $25k, PnL +$5.50, Fill rate 82%
```

### Ưu Điểm
| Ưu Điểm | Giải Thích |
|---------|-----------|
| **Passive Income** | Bot chạy 24/7, không cần theo dõi |
| **Không cần dự đoán** | Hoạt động tốt trong thị trường sideway |
| **Tích lũy nhỏ** | Nhiều giao dịch nhỏ = Lãi ổn định |
| **Hedging tự động** | Mua-bán đối xứng tự bảo vệ |

### Nhược Điểm & Rủi Ro
| Rủi Ro | Mô Tả | Giải Pháp |
|--------|-------|-----------|
| **Trend Risk** | Giá chạy 1 chiều vượt lưới | Giới hạn max orders, dùng adaptive grid |
| **Liquidity Risk** | Thị trường kém thanh khoản | Chọn coin có volume cao |
| **Funding Fee** | Giữ vị thế lâu → Phí funding | Đóng lệnh cuối ngày, tránh qua đêm |
| **Slippage** | Giá khớp khác giá đặt | Tăng spread khi biến động cao |

---

## 🎮 Cấu Hình Khuyến Nghị Theo Kinh Nghiệm

### Cho Người Mới (Conservative)
```
Order Size: $5-$10
Grid Spread: 0.5% - 1.0%
Max Orders/Side: 3-5
Symbols: BTC, ETH (thanh khoản cao)
Expected Return: 1-3%/tháng
Max Drawdown: 10%
```

### Cho Người Có Kinh Nghiệm (Aggressive)
```
Order Size: $20-$50
Grid Spread: 0.1% - 0.3%
Max Orders/Side: 10-20
Symbols: Altcoin có biên độ cao
Expected Return: 5-15%/tháng
Max Drawdown: 30%
```

---

## � Tóm Tắt Nghiệp Vụ

### Vào Lệnh
- **Khi**: Giá chạm mức lưới + Đủ điều kiện risk
- **Gì**: Đặt lệnh BUY dưới giá, SELL trên giá
- **Mục tiêu**: Tích lũy vị thế đối xứng

### Ra Lệnh  
- **Tự động**: Lệnh khớp → Tạo lệnh đối xứng mới
- **SL**: Đóng khi lỗ > ngưỡng hoặc gần liquidation
- **TP**: Chốt lãi từ chênh lệch BUY-SELL hoặc R:R TP

### Quản Lý Rủi Ro
- **Position Limits**: Giới hạn exposure
- **Loss Limits**: Đóng khi lỗ vượt ngưỡng  
- **Adaptive**: Điều chỉnh theo chế độ thị trường
- **Emergency**: Dừng bot khi drawdown cao

### Kết Quả Mong Đợi
- **Sideway**: 2-5%/tháng (lý tưởng)
- **Trending**: 0-2%/tháng (hoặc hòa vốn)
- **Volatile**: -5% đến +5% (rủi ro cao)

---

*Grid Trading phù hợp với người muốn thu nhập thụ động, không cần phân tích kỹ thuật phức tạp, chấp nhận lãi nhỏ nhưng ổn định.*
