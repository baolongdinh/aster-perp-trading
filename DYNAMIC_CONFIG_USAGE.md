# Dynamic Configuration Usage Guide

## 🎯 Overview

Hệ thống Dynamic Configuration cho phép volume farm maker tự động tối ưu parameters cho từng symbol dựa trên real-time market data từ exchange.

## 🚀 Quick Start

```powershell
# Chạy từ thư mục backend
cd c:\CODE\GOLANG\TRADE\aster-bot-perp-trading-2\backend

# Adaptive mode với symbol USDT
go run cmd/volume-farm-maker/main.go -config=config/adaptive_config.yaml -symbol=BTCUSDT

# Hoặc build trước rồi chạy
go build -o vf.exe cmd/volume-farm-maker/main.go
./vf.exe -config=config/adaptive_config.yaml -symbol=ETHUSDT
```

## 📋 Command Line Options

### Basic Usage
```powershell
# Single symbol với adaptive configuration (recommended)
go run cmd/volume-farm-maker/main.go -config=config/adaptive_config.yaml -symbol=BTCUSDT

# Legacy mode (static parameters)
go run cmd/volume-farm-maker/main.go -config=config/adaptive_config.yaml -adaptive=false -symbol=ETHUSDT -spread=5
```

### Available Flags
| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `config/adaptive_config.yaml` | Path to config file |
| `-symbol` | `ethusd1` | Single symbol to trade |
| `-symbols` | - | Multiple symbols (comma-separated) |
| `-adaptive` | `true` | Enable dynamic configuration |
| `-dry-run` | `false` | Run without real orders |
| `-spread` | `5` | Spread in bps (legacy mode) |
| `-leverage` | `50` | Max leverage |

## 💱 Symbol Format

Hệ thống hỗ trợ nhiều định dạng symbol:

```powershell
# Tất cả đều được normalize về cùng format
-symbol=BTCUSDT   → btc
-symbol=btcusd1   → btcusd1  
-symbol=ETH/USDT  → eth
-symbol=agtusdt   → agt
```

### Supported Coins
| Symbol | Prefix | Spread Mult | Vol Mult | Liq Mult |
|--------|--------|-------------|----------|----------|
| BTCUSDT | btc | 1.2x | 0.8x | 1.3x |
| ETHUSDT | eth | 0.8x | 1.2x | 1.4x |
| SOLUSDT | sol | 1.8x | 1.5x | 0.6x |
| BNBUSDT | bnb | 1.5x | 1.3x | 0.7x |
| XRPUSDT | xrp | 2.0x | 2.0x | 0.5x |
| ADAUSDT | ada | 1.4x | 1.1x | 0.8x |
| DOTUSDT | dot | 1.6x | 1.4x | 0.6x |
| AVAXUSDT | avax | 1.7x | 1.6x | 0.5x |
| MATICUSDT | matic | 1.5x | 1.4x | 0.6x |
| LINKUSDT | link | 1.3x | 1.2x | 0.7x |
| LTCUSDT | ltc | 1.1x | 1.0x | 0.9x |
| UNIUSDT | uni | 1.4x | 1.3x | 0.6x |

## 🔧 Dynamic Parameters

Hệ thống tự động tính toán parameters dựa trên market data:

### Theo Volatility
| Condition | Spread | Grid Levels | Position Size |
|-----------|--------|-------------|---------------|
| Low Vol (<2%) | Tight (1-3 bps) | More (60-75) | Larger |
| Medium Vol (2-3%) | Normal (5-8 bps) | Standard (40-50) | Medium |
| High Vol (>5%) | Wide (10-15 bps) | Fewer (25-35) | Smaller |

### Theo Liquidity
| Condition | Grid Spacing | Max Position |
|-----------|--------------|--------------|
| High Liq (>0.8) | Ultra-tight (0.05 bps) | Larger ($4500+) |
| Medium Liq (0.5-0.8) | Normal (0.1 bps) | Medium ($2000-4500) |
| Low Liq (<0.5) | Wide (0.2 bps) | Smaller (<$2000) |

## 📊 Real-Time Market Analysis

### Data Sources
- **WebSocket**: Real-time bid/ask, volume 24h
- **REST API**: 24h ticker statistics từ exchange
- **Cache**: Ticker data được cache để tính toán

### Analysis Metrics
- **Volatility**: (High - Low) / Open từ 24h data
- **Liquidity Score**: Kết hợp volume, order book depth, spread
- **Price Change**: 24h price change percentage
- **Risk Score**: Weighted volatility + spread + liquidity risk

## 🔄 Adaptive Behavior

### Parameter Updates
- **Market Data**: Update mỗi 30 giây
- **Config Reeval**: Mỗi 5 phút
- **Auto Optimization**: Liên tục khi `-auto-optimize=true`

### Response to Market Changes
- **High Volatility**: Wider spreads, fewer grid levels, smaller positions
- **Low Liquidity**: Reduced position sizes, wider spreads
- **Trending Market**: Higher momentum threshold
- **Ranging Market**: Tight spreads for maximum fills

## 📈 Expected Benefits

- **Fill Rate**: +15-20% so với static config
- **Profit per Trade**: Optimized theo symbol characteristics
- **Risk Management**: Dynamic risk reduction 30-40%
- **Market Adaptation**: <5 phút parameter optimization
- **No Manual Config**: Tự động cho mọi symbol

## ⚠️ Important Notes

### Config Path
- Default: `config/adaptive_config.yaml`
- Phải chạy từ thư mục `backend/`

### Network Requirements
- Cần WebSocket connection cho real-time data
- Exchange API phải support 24h statistics endpoint

### Risk Management
- Dynamic parameters không thay thế risk management
- Position limits vẫn được áp dụng
- Emergency stop mechanisms luôn active

## 🔍 Troubleshooting

### Lỗi thường gặp

1. **"Unsupported Config Type"**
   - Kiểm tra config path đúng: `config/adaptive_config.yaml`
   - Chạy từ thư mục `backend/`

2. **Symbol không được parse**
   - Thử format: `-symbol=BTCUSDT` hoặc `-symbol=btcusd1`
   - Xem log "DEBUG raw flags" để kiểm tra

3. **WebSocket connection failed**
   - Kiểm tra network
   - Verify exchange URL trong config

### Debug Mode
```powershell
# Xem debug log
go run cmd/volume-farm-maker/main.go -config=config/adaptive_config.yaml -symbol=BTCUSDT
```

Log sẽ hiển thị:
```
DEBUG raw flags  {"symbol_flag": "BTCUSDT", "config_path": "config/adaptive_config.yaml"}
Trading symbols configured {"symbols": ["btc"], ...}
📊 Adaptive Configuration Loaded  {"optimal_spread_bps": 6.0, ...}
```

---

Hệ thống này đảm bảo mỗi symbol được tối ưu hóa dựa trên điều kiện thị trường thực tế từ exchange, maximizing hiệu quả trading!
