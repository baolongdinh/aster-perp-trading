# AGENTIC TRADING - Nghiệp Vụ Vận Hành

## 1. Tổng Quan Hệ Thống

### 1.1 Định Nghĩa
Agentic Trading là hệ thống giao dịch thông minh tự động điều chỉnh chiến lược dựa trên phân tích thị trường theo thời gian thực. Hệ thống tự động nhận biết chế độ thị trường, tính toán điểm số đa yếu tố, và điều chỉnh kích thước lệnh/grid spacing phù hợp.

### 1.2 Các Chế Độ Thị Trường (Regime)

| Chế Độ | Đặc Điểm | Chiến Lược Grid |
|--------|----------|-----------------|
| **Sideways** | Giá dao động trong biên độ hẹp, ADX thấp | Grid spacing nhỏ (0.3%), lướt sóng ngắn |
| **Trending** | Xu hướng rõ ràng, EMA xếp chồng | Grid spacing lớn (1-2%), position size giảm |
| **Volatile** | Biến động cao, ATR tăng đột biến | **Ngừng giao dịch** hoặc position size cực nhỏ |
| **Recovery** | Hồi phục sau volatility spike | Chờ confirm trend, spacing vừa phải |

### 1.3 Điểm Số Triển Khai (0-100)

```
≥75 điểm: Triển khai full size
60-74 điểm: Triển khai reduced size (50%)
<60 điểm: Chờ đợi, không triển khai
```

---

## 2. Quy Trình Khởi Động (Cold Start)

### 2.1 Warm-up Phase
1. **Load dữ liệu lịch sử**: Hệ thống tự động tải 1000 nến gần nhất từ API
2. **Tính toán chỉ báo**: ADX, Bollinger Band, ATR, EMA (9, 21, 50, 200)
3. **Xác định chế độ**: Phân tích chỉ báo để xác định regime hiện tại
4. **Sẵn sàng giao dịch**: Chờ 2 lần đọc regime liên tiếp giống nhau (hysteresis)

### 2.2 Pattern Learning Phase
- **Giai đoạn 1 (0-200 trades)**: Chỉ thu thập dữ liệu, chưa dùng pattern
- **Giai đoạn 2 (≥200 trades + accuracy ≥60%)**: Pattern bắt đầu ảnh hưởng ±5 điểm vào score
- **Decay công thức**: Pattern cũ có trọng số giảm theo thời gian `exp(-days/14)`

---

## 3. Circuit Breakers - Cầu Chì An Toàn

### 3.1 5 Cầu Chì Tự Động

| Cầu Chì | Điều Kiện Kích Hoạt | Hành Động | Ưu Tiên |
|---------|---------------------|-----------|---------|
| **Drawdown Limit** | Drawdown portfolio > 10% | Đóng toàn bộ vị thế, dừng bot | 1 (Cao nhất) |
| **Volatility Spike** | ATR tăng 3x trong 5 phút | Đóng khẩn cấp, chờ ổn định | 2 |
| **Liquidity Crisis** | Spread bid-ask > 5x bình thường | Ngừng đặt lệnh mới | 3 |
| **Consecutive Losses** | 3 chu kỳ grid liên tiếp lỗ | Giảm 50% kích thước lệnh | 4 |
| **Connection Failure** | Mất kết nối API 3 lần liên tiếp | Ngừng hoạt động, cảnh báo | 5 |

### 3.2 Reset Cầu Chì
- **Tự động**: Sau thời gian chờ (30s - 5 phút tùy cầu chì)
- **Thủ công**: Operator có thể reset qua API/command

---

## 4. Yếu Tố Tính Toán Điểm Số (4 Factors)

### 4.1 Trọng Số Các Yếu Tố

| Yếu Tố | Trọng Số | Ý Nghĩa |
|--------|----------|---------|
| **Trend** | 30% | Xu hướng thị trường (EMA alignment, ADX) |
| **Volatility** | 25% | Mức độ biến động (ATR, BB width) |
| **Volume** | 25% | Khối lượng giao dịch (vs MA20) |
| **Structure** | 20% | Cấu trúc giá (support/resistance) |

### 4.2 Hệ Số Điều Chỉnh Theo Chế Độ

```
Trending: Trend +20%, Volatility -10%
Sideways: Volatility +15%, Volume +10%
Volatile: Tất cả yếu tố bị giảm trọng số
Recovery: Dần dần trở về bình thường
```

---

## 5. Quản Lý Vị Thế

### 5.1 Công Thức Kích Thước Lệnh

```
final_size = base_size × score_multiplier × volatility_multiplier × pattern_multiplier

Trong đó:
- score_multiplier: 1.0 (≥75đ), 0.5 (60-74đ), 0.0 (<60đ)
- volatility_multiplier: 1.0 (normal), 0.5 (high), 0.0 (extreme)
- pattern_multiplier: ±5% từ pattern matching (nếu accuracy ≥60%)
```

### 5.2 Grid Spacing Theo Volatility

| Điều Kiện Volatility | Grid Spacing | Mô Tả |
|---------------------|--------------|-------|
| Low Vol (ATR nhỏ) | 0.3% | Biên độ hẹp, nhiều lưới |
| Normal Vol | 1.0% | Lưới tiêu chuẩn |
| High Vol (ATR lớn) | 2.0% | Biên độ rộng, ít lưới hơn |

---

## 6. Logging & Audit

### 6.1 Decision Log
Mỗi quyết định được ghi nhận:
- Timestamp
- Regime hiện tại + confidence
- 4 factors (giá trị + đóng góp)
- Final score + multipliers
- Grid parameters (spacing, size)
- Pattern matches (nếu có)
- Rationale (lý do quyết định)

### 6.2 Retention
- File log: `decisions_YYYY-MM-DD.jsonl`
- Thời gian lưu: 90 ngày
- Nén file cũ sau 30 ngày

---

## 7. Các Cặp Giao Dịch Hỗ Trợ

| Cặp | Pattern Storage File | Min Trades để Active |
|-----|---------------------|---------------------|
| BTC/USD1 | `btcusd1_patterns.json` | 200 |
| ETH/USD1 | `ethusd1_patterns.json` | 200 |
| SOL/USD1 | `solusd1_patterns.json` | 200 |

Mỗi cặp có pattern storage riêng, accuracy tracking riêng.

---

## 8. Monitoring & Alert

### 8.1 Tình Huống Cảnh Báo
- **Regime Change**: Thông báo ngay khi chế độ thị trường thay đổi
- **Circuit Breaker**: Cảnh báo khẩn cấp + SMS/email nếu cầu chì drawdown/volatility kích hoạt
- **High Drawdown**: Cảnh báo khi drawdown > 5% (trước khi chạm 10% cầu chì)

### 8.2 Rate Limiting Alert
- Tối đa 1 alert/5 phút cho mỗi loại
- Tránh spam khi thị trường biến động liên tục

---

## 9. Operational Commands

### 9.1 Khởi Động Bot
```
# Test mode (không giao dịch thật)
./agentic-bot --config=config.yaml --symbol=BTCUSDT --test

# Live mode (có giao dịch thật)
./agentic-bot --config=config.yaml --symbol=BTCUSDT
```

### 9.2 Các Thao Tác Quản Lý
- **Dừng**: Ctrl+C hoặc SIGTERM → Graceful shutdown, save patterns
- **Check status**: Log file hoặc API `/health`
- **Reset breaker**: API POST hoặc command

---

## 10. KPIs & Performance Targets

| Chỉ Số | Target | Đo Lường |
|--------|--------|----------|
| Decision Latency | < 500ms | Thời gian từ data → decision |
| Regime Detection | 30s | Khoảng cách giữa các lần detect |
| Cold Start | < 5s | Thời gian warm-up 1000 candles |
| Pattern Query | < 100ms | Thời gian tìm pattern phù hợp |
| Uptime | > 99% | Thời gian hoạt động liên tục |

---

*Document Version: 1.0*
*Last Updated: 2026-04-10*
