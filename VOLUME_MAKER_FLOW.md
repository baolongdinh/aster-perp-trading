# Volume Maker - Hướng Dẫn Chi Tiết

## Mục tiêu của Bot
- **Kiếm tiền từ spread nhỏ**: Mỗi lệnh chỉ kiếm 0.001% (micro profit)
- **Volume lớn**: Đặt nhiều lệnh nhỏ để tối đa volume
- **Không bị mất tiền lớn**: Có nhiều lớp bảo vệ rủi ro

---

## Các Tham Số Chính

| Tham số | Giá trị | Ý nghĩa |
|---------|---------|---------|
| Leverage | 150x | Đòn bẩy cao để tăng notional |
| Max Position/Symbol | $3,000 | Mỗi cặp tiền không quá $3,000 |
| Max Total | $15,000 | Tổng tất cả symbols không quá $15,000 |
| Grid Levels | 40-50 | Số lệnh mỗi bên (mua/bán) |
| Grid Spacing | 0.001% | Khoảng cách giữa các lệnh |

---

## Cấu Trúc Lệnh (Grid)

Bot đặt 2 bên lệnh: MUA và BÁN

```
GIÁ THỊ TRƯỜNG: $2331

LỆNH MUA (dưới thị trường):
Level 1: $2330.9  (-0.1 bps)
Level 2: $2330.7  (-0.2 bps)
Level 3: $2330.5  (-0.3 bps)
...
Level 50: $2326.9  (-5 bps)

LỆNH BÁN (trên thị trường):
Level 1: $2331.1  (+0.1 bps)
Level 2: $2331.3  (+0.2 bps)
Level 3: $2331.5  (+0.3 bps)
...
Level 50: $2335.1  (+5 bps)
```

**Tại sao đặt như vậy?**
- Khi giá giảm xuống → lệnh MUA được khớp → kiếm spread
- Khi giá tăng lên → lệnh BÁN được khớp → kiếm spread
- Luôn có lệnh 2 bên → delta neutral

---

## 3 Vòng Lặp Chính

### Vòng 1: Order Management (5 giây)
```
Mỗi 5 giây, bot làm:
1. Lấy giá thị trường (từ WebSocket - nhanh)
2. Lấy danh sách lệnh đang mở (từ API - có cache 1s)
3. Kiểm tra lệnh nào đã khớp → Cập nhật position
4. Kiểm tra momentum (giá di chuyển nhanh không?)
5. Kiểm tra grid drift (lệnh có lệch quá xa không?)
6. Hủy lệnh cũ (quá 120 giây hoặc giá lệch quá xa)
7. Đặt lệnh mới (nếu cần)
```

### Vòng 2: Risk Monitoring (5 giây)
```
Mỗi 5 giây, bot kiểm tra:
- Liquidation Guard: Còn cách liquidation bao xa?
- Max Position Guard: Tổng exposure có quá $15,000 không?
- Daily Loss Guard: Hôm nay lỗ bao nhiêu %?
- OT Ratio Guard: Tỷ lệ lệnh đặt / lệnh khớp có cao không?

→ Nếu bất kỳ cái nào vượt ngưỡng → DỪNG KHẨN CẤP
```

### Vòng 3: Position Sync (10 giây)
```
Mỗi 10 giây:
1. Lấy position từ exchange (có cache 1s)
2. Cập nhật tất cả guards
```

---

## Chi Tiết Từng Bước

### Bước 1: VÀO LỆNH (PlaceOrders)

```
1. Lấy giá từ WebSocket
   bestBid = 2330.9, bestAsk = 2331.1

2. Tính số lượng mỗi lệnh
   balance = $100 (từ WebSocket)
   maxOrderValue = $100 × 150 × 100% = $15,000
   perLevelQty = $15,000 / (2331 × 50) = 0.1287

3. Tính giá mỗi level
   Buy Level 1: 2330.9 × (1 - 0.00001) = 2330.8767
   Sell Level 1: 2331.1 × (1 + 0.00001) = 2331.1233

4. Đặt lệnh (song song - goroutines)
   → 40 lệnh MUA + 40 lệnh BÁN
```

### Bước 2: KIỂM TRA KHỚP LỆNH (Fill Detection)

```
Mỗi cycle, kiểm tra từng lệnh:

Nếu lệnh đã khớp (Filled):
   1. Cập nhật position NGAY LẬP TỨC
      - inventoryMgr.UpdatePosition()
      - liqGuard.UpdatePosition()
      - maxPosGuard.UpdateExposure()
   
   2. Kiểm tra liquidation risk NGAY
      - Tính buffer = |giá hiện tại - giá liquidation| / giá hiện tại
      - Nếu buffer < 5% → DỪNG KHẨN CẤP
   
   3. Record fill cho OT ratio
```

### Bước 3: KIỂM TRA MOMENTUM

```
Momentum = độ biến động giá trong 30 giây

Ví dụ:
- Giá lúc 00:00: $1000
- Giá lúc 00:30: $1035
- Biến động: 3.5% > 3% (ngưỡng)

→ Nếu phát hiện momentum cao:
   - Hủy TẤT CẢ lệnh đang mở
   - Tạm dừng đặt lệnh mới
   - Đợi giá ổn định rồi mới đặt tiếp

Tại sao? Tránh bị "adverse selection" - tức là đặt lệnh mà bị sweep ngược.
```

### Bước 4: KIỂM TRA GRID DRIFT

```
Grid drift = độ lệch giữa giá trung tâm của lệnh và giá thị trường

Ví dụ:
- Giá trung tâm lệnh: $2330
- Giá thị trường: $2340
- Drift: 10 / 2340 = 0.43%

Ngưỡng = spacing × levels × 0.5 = 0.001% × 50 × 0.5 = 2.5%

→ Nếu drift > 2.5%:
   - Hủy tất cả lệnh
   - Đặt lệnh mới ở giá thị trường hiện tại
```

### Bước 5: HỦY LỆNH CŨ

```
Hủy lệnh nếu:
1. Grid shift cần thiết (drift > 2.5%)
2. Lệnh quá cũ (> 120 giây)
3. Giá lệch khỏi thị trường > 0.1%
4. Momentum cao được phát hiện
```

### Bước 6: ĐẶT LỆNH MỚI (có kiểm tra risk)

```
Trước khi đặt lệnh, kiểm tra:

1. Position Bias (lệch position)
   - Tính: |position hiện tại| / max position
   - Ví dụ: $1200 / $3000 = 40% > 30%
   
   → Nếu > 30%:
     * Hủy tất cả lệnh đang có
     * Giảm lệnh mới 85% (buyAdjustment = 0.15)
     * Cho phép bên đối diện unwind
   
   → Nếu > 80%: DỪNG KHẨN CẤP

2. Toxic Flow (dòng tiền 1 chiều)
   - Tính: buy volume / (buy + sell) trong 60s
   - Ví dụ: $8000 buy / $10000 total = 80% > 60%
   
   → Nếu > 60% hoặc < 40%:
     * Giảm tất cả lệnh 50%
```

---

## Các Lớp Bảo Vệ (Risk Guards)

| # | Tên | Khi nào kích hoạt | Hành động |
|---|-----|-------------------|-----------|
| 1 | **Liquidation Guard** | Buffer < 5% | Dừng khẩn cấp |
| 2 | **Extreme Bias** | Position > 80% max | Dừng khẩn cấp |
| 3 | **Max Position** | Total > $15,000 | Dừng khẩn cấp |
| 4 | **Daily Loss** | Loss > 2% | Dừng khẩn cấp |
| 5 | **Momentum** | Giá > 3% trong 30s | Hủy lệnh + Tạm dừng |
| 6 | **Position Bias** | Position > 30% max | Hủy + Giảm 85% |
| 7 | **Toxic Flow** | Một bên > 60% | Giảm 50% lệnh |
| 8 | **Order Age** | Lệnh > 120s | Hủy |
| 9 | **OT Ratio** | Ratio > 10 | Tạm dừng 30s |

### Chi Tiết Từng Guard

#### 1. Liquidation Guard - Tránh bị liquidation
```
Ví dụ:
- Vào lệnh LONG ở $2300, leverage 150x
- Giá liquidation = $2300 × (1 - 149/150) = $2333.33
- Giá hiện tại: $2330
- Buffer = |2330 - 2333.33| / 2330 = 0.14% < 5%

→ Kích hoạt DỪNG KHẨN CẤP
```

#### 2. Max Position Guard - Giới hạn tổng exposure
```
Track per-symbol (dùng map, không overwrite):

ETHUSD1: +$2000
BTCUSD1: +$500
Total: $2500 < $15000 ✓

Nếu vượt $15,000 → DỪNG KHẨN CẤP
```

#### 3. Daily Loss Guard - Giới hạn lỗ hàng ngày
```
PHẢI gọi SetStartingBalance() khi start!

Ví dụ:
- Balance lúc start: $1000
- Balance hiện tại: $980
- Loss: $20 = 2%

→ Kích hoạt DỪNG KHẨN CẤP
```

#### 4. Momentum Guard - Tránh trending market
```
Ví dụ:
- 00:00: Giá $1000
- 00:30: Giá $1035
- Move: 3.5% > 3%

→ HỦY TẤT CẢ LỆNH + TẠM DỪNG
```

#### 5. Position Bias - Tránh lệch position
```
Ví dụ:
- Max position: $3000
- Current: $1200 (long)
- Bias: 40% > 30%

→ Hủy tất cả lệnh
→ Giảm lệnh MUA 85%
→ Cho phép lệnh BÁN unwind

Nếu bias > 80% → DỪNG KHẨN CẤP
```

#### 6. Toxic Flow - Tránh one-sided flow
```
Track volume trong 60s:
- Buy: $8000
- Sell: $2000
- Buy ratio: 80% > 60%

→ Giảm tất cả lệnh 50%
```

---

## Ví Dụ Thực Tế

### Trường hợp 1: Thị trường sideways (không có xu hướng)
```
Giá dao động: $2330 - $2332

1. Bot đặt 80 lệnh (40 mua + 40 bán)
2. Giá chạm $2330.9 → lệnh MUA level 1 khớp
3. Bot cập nhật position REAL-TIME
4. Kiểm tra liquidation → OK (buffer > 5%)
5. Đặt lệnh MUA mới thế chỗ
6. Tiếp tục...

→ Kiếm micro profit từ mỗi lệnh khớp
```

### Trường hợp 2: Giá tăng mạnh (trending up)
```
00:00: Giá $1000
00:05: Giá $1010 (+1%)
00:10: Giá $1025 (+2.5%)
00:15: Giá $1035 (+3.5%) → MOMENTUM TRIGGERED!

1. Momentum Guard phát hiện 3.5% > 3%
2. Hủy TẤT CẢ lệnh đang mở
3. Tạm dừng đặt lệnh mới
4. Đợi giá ổn định...

→ Tránh bị adverse selection
```

### Trường hợp 3: Position bị lệch nhiều
```
1. Bot liên tục mua, position = +$2500 (83% của $3000)
2. Position Bias Guard phát hiện 83% > 30%
3. Hủy tất cả lệnh MUA
4. Giảm lệnh MUA mới 85%
5. Cho phép lệnh BÁN nhiều hơn để unwind

→ Tránh tập trung position một bên
```

---

## Cấu Hình (CLI Flags)

```bash
-leverage 150              # Đòn bẩy tối đa
-micro-spacing 0.2         # Khoảng cách grid (bps)
-micro-levels 40           # Số level mỗi bên
-bias-threshold 0.5        # Ngưỡng position bias (0.5 = 50%)
-momentum true             # Bật momentum guard
-momentum-threshold 0.03   # Ngưỡng momentum (3%)
-toxic-flow true           # Bật toxic flow guard
```

---

## Tóm Tắt

```
┌─────────────────────────────────────────────────────────────┐
│                    BOT CHẠY 3 VÒNG LẶP                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ORDER LOOP (5s)         RISK LOOP (5s)      SYNC (10s)    │
│  ├─ Get ticker           ├─ Check liq        ├─ Get pos    │
│  ├─ Check fills          ├─ Check max pos    └─ Update     │
│  ├─ Check momentum       ├─ Check daily loss │   guards    │
│  ├─ Check grid drift     └─ Check OT ratio   │             │
│  ├─ Cancel old orders    │                    │             │
│  └─ Place new orders     │                    │             │
│                          │                    │             │
│         ↓                ↓                    ↓             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  NẾU BẤT KỲ RISK NÀO VƯỢT NGƯỠNG → DỪNG KHẨN CẤP   │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Điểm mấu chốt:
1. **Vào lệnh**: Grid 2 bên, spacing nhỏ (0.001%), parallel placement
2. **Giám sát**: 5s cycle, real-time fill detection
3. **Thoát lệnh**: Fill → Grid shift → Order age
4. **Bảo vệ**: 9 lớp protection, emergency stop
5. **Tối ưu**: Caching 1s, parallel processing, dynamic thresholds
