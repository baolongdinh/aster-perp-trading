# Aster Perp Trading Bot 🤖

Bot giao dịch tự động trên Aster Finance Futures API - Grid Trading + Volume Farming.

## 📚 Tài Liệu Quan Trọng

| Tài Liệu | Mô Tả | Dành Cho |
|----------|-------|----------|
| **[GRID_TRADING_LOGIC.md](GRID_TRADING_LOGIC.md)** | Giải thích chi tiết nghiệp vụ bot - cách vào lệnh, ra lệnh, quản lý rủi ro | Người muốn hiểu bot **trade như thế nào** |
| **[VOLUME_FARMING_README.md](VOLUME_FARMING_README.md)** | Giải thích logic code - cách code thực hiện các nghiệp vụ | Người muốn hiểu **code chạy như thế nào** |

> 💡 **Gợi ý**: Đọc `GRID_TRADING_LOGIC.md` trước để hiểu nghiệp vụ, sau đó đọc `VOLUME_FARMING_README.md` nếu cần tìm hiểu sâu về implementation.

---

## 🎯 Bot Làm Gì?

Bot tự động đặt lệnh mua (BUY) và bán (SELL) xung quanh giá hiện tại để:
- **Grid Trading**: Kiếm lãi từ biên độ dao động giá (mua thấp - bán cao)
- **Volume Farming**: Tạo volume giao dịch liên tục để tích lũy điểm thưởng

### Cơ Chế Đơn Giản
```
Giá hiện tại: $100
├─ Đặt SELL ở $101, $102, $103... (trên giá)
└─ Đặt BUY ở $99, $98, $97... (dưới giá)

Khi giá dao động:
→ Chạm BUY $99 → Khớp lệnh → Tự động đặt SELL $101
→ Chạm SELL $101 → Khớp lệnh → Lãi $2
→ Lặp lại nhiều lần → Tích lũy lãi
```

---

## 🚀 Cách Chạy Bot

### 1. Cài Đặt

```bash
# Vào thư mục backend
cd backend

# Build bot
go build -v ./cmd/bot
```

### 2. Cấu Hình

```bash
# Copy file config mẫu
cp config/volume-farm-config.example.yaml config/volume-farm-config.yaml

# Sửa config theo nhu cầu
nano config/volume-farm-config.yaml
```

**Cấu hình quan trọng:**
```yaml
volume_farming:
  order_size_usdt: 100      # $100 mỗi lệnh
  grid_spread_pct: 0.001    # 0.1% spread
  max_orders_per_side: 5    # 5 lệnh mỗi bên

risk_management:
  max_position_usdt: 300    # Tối đa $300/symbol
  daily_drawdown_pct: 20    # Dừng nếu lỗ 20%
```

### 3. Chạy Bot

```bash
# Cách 1: Chạy trực tiếp
./bot

# Cách 2: Chạy với Termux (mobile)
make termux-start

# Cách 3: Dùng script
./run-volume-farm.sh start
```

### 4. Theo Dõi

```bash
# Xem logs
./run-volume-farm.sh logs

# Kiểm tra status
./run-volume-farm.sh status

# Dừng bot
./run-volume-farm.sh stop
```

---

## 📂 Cấu Trúc Project

```
aster-perp-trading/
├── backend/                      # Code Go chính
│   ├── cmd/bot/                  # Entry point
│   ├── internal/
│   │   ├── farming/              # Core trading logic
│   │   │   ├── adaptive_grid/    # Grid manager
│   │   │   └── market_regime/    # Trend detection
│   │   ├── risk/                 # Risk management
│   │   └── client/               # Binance API client
│   └── config/                   # YAML configs
├── skills/                       # OpenClaw skills (nếu dùng)
├── GRID_TRADING_LOGIC.md         # 📖 Nghiệp vụ chi tiết
├── VOLUME_FARMING_README.md      # 📖 Logic code chi tiết
└── README.md                     # 📖 File này
```

---

## ⚠️ Lưu Ý Quan Trọng

### Rủi Ro
- **Trending Market**: Nếu giá chạy 1 chiều mạnh, bot có thể tích lũy vị thế lớn
- **Volatility**: Biến động cao có thể gây slippage
- **Funding Fee**: Giữ vị thế qua đêm có thể mất phí funding

### Khuyến Nghị
- **Test trên testnet** trước khi chạy real
- **Bắt đầu với vốn nhỏ** ($100-500)
- **Theo dõi liên tục** trong những ngày đầu
- **Đọc kỹ** [GRID_TRADING_LOGIC.md](GRID_TRADING_LOGIC.md) để hiểu rủi ro

---

## 🔧 Skills (Nếu Dùng OpenClaw)

Thư mục `skills/` chứa các skills để tích hợp với OpenClaw:

| Skill | Mục Đích |
|-------|----------|
| `aster-api-auth-v3` | Xác thực EIP-712 |
| `aster-api-trading-v3` | Đặt/hủy lệnh |
| `aster-api-market-data-v3` | Lấy dữ liệu thị trường |
| `aster-api-account-v3` | Quản lý tài khoản |

---

## 🆘 Hỗ Trợ

### Debug
```bash
# Xem logs chi tiết
tail -f backend/logs/volume-farm.log

# Kiểm tra lỗi
grep "ERROR" backend/logs/volume-farm.log
```

### Thông Tin Thêm
- Đọc [GRID_TRADING_LOGIC.md](GRID_TRADING_LOGIC.md) để hiểu **cách bot trade**
- Đọc [VOLUME_FARMING_README.md](VOLUME_FARMING_README.md) để hiểu **cách code hoạt động**

---

## 📊 API Endpoints

| API | Loại | URL |
|-----|------|-----|
| Futures | REST | `https://fapi.asterdex.com` |
| Futures | WebSocket | `wss://fstream.asterdex.com` |

---

*Bot được xây dựng bằng Go + được tài liệu hóa chi tiết bằng tiếng Việt 🇻🇳*

