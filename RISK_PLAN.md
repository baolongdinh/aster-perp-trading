# Grid Trading Risk Management - Implementation Plan

## Vấn đề phân tích từ hình ảnh
- **Max held 0.99 ETH** vs **0.02 ETH bình thường** = 49x lớn hơn
- Bot tích lũy position khi giá đi 1 chiều dẫn đến liquidation liên tục
- Sizing hiện tại không đủ linh hoạt để thích ứng với biến động

## Giải pháp đã implement

### 1. Notional Value Sizing (Thay thế fixed size)
```go
// Trước: orderSizeUSDT = 25 (fixed)
// Sau: baseNotionalUSD = $100/lệnh (dựa trên giá trị USD)

orderSize := g.baseNotionalUSD / currentPrice
// ETH $2000 → size = 100/2000 = 0.05 ETH
// ETH $3000 → size = 100/3000 = 0.033 ETH
```

### 2. ATR-Based Volatility Sizing
```go
// Giảm size khi biến động cao
atrPct := GetATR() / currentPrice
if atrPct > 1% {
    volatilityFactor = 1.0 - (atrPct-0.01)*0.5
    // ATR 2% → factor = 0.5 (giảm 50% size)
}
```

### 3. Hard-Cap Exposure (Max 30% Equity)
```go
MaxTotalExposurePct: 0.3  // Max 30% của equity

func CanOpenPosition(notionalValue) bool {
    maxExposure := equity * 0.3
    return totalExposure + notionalValue <= maxExposure
}
```

### 4. Trend Detection (Không vào ngược trend)
```go
type DirectionalBiasChecker struct {
    shortTermMA  []float64  // 5-period
    longTermMA   []float64  // 20-period
}

func ShouldAllowLong() bool {
    return bias != BiasShort  // Không Long khi đang bearish
}

func ShouldAllowShort() bool {
    return bias != BiasLong   // Không Short khi đang bullish
}
```

### 5. Regime-Based Adjustment
```go
switch marketRegime {
case RegimeTrending:
    regimeFactor = 0.5   // Giảm 50%
    pauseTrading()      // Pause nếu trending mạnh
case RegimeVolatile:
    regimeFactor = 0.3   // Giảm 70%
case RegimeRanging:
    regimeFactor = 1.0   // Full size
}
```

### 6. Consecutive Loss Cooldown
```go
MaxConsecutiveLosses: 3
CooldownAfterLosses: 5 minutes

func RecordLoss() {
    consecutiveLosses++
    if consecutiveLosses >= 3 {
        cooldownActive = true  // Pause 5 phút
    }
}
```

## Cấu trúc files mới

```
backend/internal/farming/adaptive_grid/
├── manager.go          # AdaptiveGridManager (đã có)
├── risk_sizing.go      # NEW: Risk sizing & exposure management
└── order_manager.go    # Order operations
```

## Risk Config mặc định

```go
DefaultEnhancedRiskConfig := &EnhancedRiskConfig{
    // Risk cơ bản
    MaxPositionUSDT:         300.0,    // Max per symbol
    MaxUnrealizedLossUSDT:   3.0,      // Cut loss
    StopLossPct:             0.01,     // 1% stop
    LiquidationBufferPct:    0.2,      // 20% buffer
    
    // Dynamic sizing
    BaseOrderNotional:    100.0,   // $100/lệnh
    MinOrderNotional:     20.0,    // Min $20
    MaxOrderNotional:     500.0,   // Max $500
    MaxTotalExposurePct:  0.3,     // 30% equity
    ATRMultiplier:        0.5,     // ATR impact
    
    // Trend control
    UseDirectionalBias:   true,
    MaxConsecutiveLosses: 3,
    CooldownAfterLosses:  5 * time.Minute,
}
```

## Flow hoạt động mới

### Khi đặt lệnh:
1. Check exposure limit (max 30% equity)
2. Check trend direction (không ngược trend)
3. Check cooldown status (sau 3 losses)
4. Calculate ATR-based size
5. Check regime adjustment
6. Convert notional → quantity
7. Place order

### Khi lệnh filled:
1. Check risk trước khi rebalance
2. Update exposure tracking
3. Check stop loss / trailing stop
4. Check liquidation proximity
5. Nếu risk OK → mới rebalance

### Risk Monitor (mỗi 5 giây):
1. Fetch positions từ exchange
2. Update equity & exposure
3. Check stop loss hit
4. Check near liquidation
5. Check max unrealized loss
6. Emergency close nếu cần

## Ưu điểm của giải pháp

| Vấn đề cũ | Giải pháp mới |
|-----------|---------------|
| Fixed size không linh hoạt | Notional sizing theo giá |
| Không biết biến động | ATR-based reduction |
| Tích lũy vô hạn | Hard-cap 30% equity |
| Vào ngược trend | Trend detection filter |
| Không pause sau loss | Cooldown 3 losses |
| Liquidation 96x | 20% liquidation buffer |

## Next Steps để deploy

1. **Build test**:
```bash
cd backend
go build ./cmd/bot
```

2. **Config update**:
```yaml
volume_farming:
  order_size_usdt: 100  # Notional USD per order
  max_exposure_pct: 0.3  # 30% equity max
  use_dynamic_sizing: true
  atr_multiplier: 0.5
```

3. **Monitor logs**:
- "Order size calculated" - xem size thay đổi
- "Exposure stats" - theo dõi utilization
- "Trend bias" - check trend detection
- "Cooldown activated" - pause sau losses

## Kỳ vọng

- **Giảm max held** từ 0.99 ETH xuống ~0.1-0.2 ETH
- **Tăng frequency** nhưng giảm size mỗi lệnh
- **Tự động pause** khi trending mạnh
- **Không liquidation** vì hard-cap exposure
- **Survive** biến động nhỏ, **exit** khi breakout
