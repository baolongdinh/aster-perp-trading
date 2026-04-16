# Volume Farming Optimization - Advanced Risk Mitigation

## Overview
Advanced optimization strategies for volume farming with minimal risk exposure.

---

## 1. Order Priority (Ưu tiên thứ tự lệnh)

### Problem Analysis
Trong Farming Volume, vị trí lệnh trong Order Book quyết định 80% thành công:
- Lệnh ở "Top of Book" (giá tốt nhất) được ưu tiên khớp trước
- HFT bots có thể nhảy lên 1 tick giá, khiến lệnh của bạn bị "treo"
- Lệnh không khớp = không tạo volume = lãng phí vốn

### Solution: Tick-size Awareness

**Implementation:**
```go
type TickSizeManager struct {
    tickSizes map[string]float64 // symbol -> tick size
    mu        sync.RWMutex
}

func (t *TickSizeManager) GetTickSize(symbol string) float64 {
    // BTC: 0.1, ETH: 0.01, SOL: 0.001
    // Fetch from exchange API or cache
}

func (t *TickSizeManager) RoundToTick(price, tickSize float64) float64 {
    return math.Round(price/tickSize) * tickSize
}
```

**Integration:**
- Thêm vào Grid Manager
- Sử dụng khi tính toán grid levels
- Đảm bảo tất cả lệnh đều nằm trên tick-size hợp lệ

### Solution: Penny Jumping Strategy

**Implementation:**
```go
type PennyJumpingStrategy struct {
    enabled         bool
    jumpThreshold   float64 // % of spread to jump
    maxJump         float64 // max ticks to jump
    competition     map[string]int // track competitor orders
}

func (p *PennyJumpingStrategy) CalculateOptimalPrice(
    currentPrice, bestBid, bestAsk, spread float64,
    isBuy bool,
) float64 {
    if !p.enabled {
        return currentPrice
    }

    tickSize := GetTickSize(symbol)

    if isBuy {
        // Buy order: jump 1 tick above best bid
        optimalPrice := bestBid + tickSize
        // Ensure still within spread limit
        if optimalPrice > bestAsk - p.jumpThreshold*spread {
            optimalPrice = bestAsk - p.jumpThreshold*spread
        }
        return optimalPrice
    } else {
        // Sell order: jump 1 tick below best ask
        optimalPrice := bestAsk - tickSize
        // Ensure still within spread limit
        if optimalPrice < bestBid + p.jumpThreshold*spread {
            optimalPrice = bestBid + p.jumpThreshold*spread
        }
        return optimalPrice
    }
}
```

**Benefits:**
- Tăng tốc độ khớp lệnh (Asset Turnover)
- Tăng volume mà không cần tăng size
- Đánh bại HFT bots ở mức độ nhất định

**Risks:**
- Có thể làm giảm spread thu được
- Cần giám sát để không over-optimize

---

## 2. Toxic Flow Detection (VPIN)

### Problem Analysis
Market Making sợ nhất là bị "cá mập" xả hàng khi biết trước giá sập:
- Bot khớp nhiều lệnh ở giá xấu ngay trước khi sập
- Volume lớn nhưng PnL cực xấu
- Bot trở thành "người thanh khoản" cho đợt bán tháo

### Solution: VPIN Indicator

**VPIN (Volume Probability of Informed Trading):**
- Theo dõi sự mất cân đối giữa buy/sell volume
- Chỉ số từ 0 (an toàn) đến 1 (rất độc hại)

**Implementation:**
```go
type VPINMonitor struct {
    windowSize      int           // Số buckets để tính VPIN
    bucketSize      float64       // Volume mỗi bucket (e.g., 1000 USDT)
    buyVolume       []float64     // Buy volume trong mỗi bucket
    sellVolume      []float64     // Sell volume trong mỗi bucket
    currentBucket   int
    currentVol      float64
    threshold       float64       // VPIN threshold (e.g., 0.3)
    mu              sync.RWMutex
}

func (v *VPINMonitor) UpdateVolume(buyVol, sellVol float64) {
    v.mu.Lock()
    defer v.mu.Unlock()

    v.currentVol += buyVol + sellVol

    if v.currentVol >= v.bucketSize {
        // Bucket đầy, lưu và chuyển sang bucket mới
        v.buyVolume[v.currentBucket] = buyVol
        v.sellVolume[v.currentBucket] = sellVol
        v.currentBucket = (v.currentBucket + 1) % v.windowSize
        v.currentVol = 0
    }
}

func (v *VPINMonitor) CalculateVPIN() float64 {
    v.mu.RLock()
    defer v.mu.RUnlock()

    if v.currentBucket < v.windowSize {
        return 0 // Chưa đủ dữ liệu
    }

    totalBuy := 0.0
    totalSell := 0.0
    for i := 0; i < v.windowSize; i++ {
        totalBuy += v.buyVolume[i]
        totalSell += v.sellVolume[i]
    }

    // VPIN = |Buy - Sell| / (Buy + Sell)
    vpin := math.Abs(totalBuy - totalSell) / (totalBuy + totalSell)
    return vpin
}

func (v *VPINMonitor) IsToxic() bool {
    vpin := v.CalculateVPIN()
    return vpin > v.threshold
}
```

**Integration:**
```go
// Trong Grid Manager
func (g *GridManager) CheckToxicFlow(symbol string) bool {
    if g.vpinMonitor == nil {
        return false
    }

    if g.vpinMonitor.IsToxic() {
        g.logger.Warn("Toxic flow detected - pausing orders",
            zap.String("symbol", symbol),
            zap.Float64("vpin", g.vpinMonitor.CalculateVPIN()))
        return true
    }
    return false
}

// Trong CanPlaceOrder
func (g *GridManager) CanPlaceOrder(symbol string) bool {
    // ... existing checks ...

    // NEW: Check toxic flow
    if g.CheckToxicFlow(symbol) {
        return false
    }

    return true
}
```

**Benefits:**
- Bảo vệ khỏi adverse selection
- Tránh trở thành thanh khoản cho bán tháo
- Giảm rủi ro PnL xấu

**Configuration:**
```yaml
toxic_flow_detection:
  enabled: true
  window_size: 50          # 50 buckets
  bucket_size: 1000.0      # 1000 USDT per bucket
  vpin_threshold: 0.3      # Trigger khi VPIN > 0.3
  action: "pause"          # pause, widen_spread, or reduce_size
```

---

## 3. Maker/Taker Logic Optimization

### Problem Analysis
Farming Volume mà dính fee Taker là "lợi bất cập hại":
- Lệnh Limit có thể thành Market khi giá chạy nhanh
- Fee Taker thường cao hơn Maker 2-3x
- Làm giảm lợi nhuận đáng kể

### Solution: Post-Only Orders

**Implementation:**
```go
type OrderType string

const (
    OrderTypeLimit    OrderType = "LIMIT"
    OrderTypePostOnly OrderType = "POST_ONLY" // Chỉ làm Maker
)

func (g *GridManager) PlaceGridOrder(symbol, side string, price, quantity float64) error {
    // Sử dụng Post-Only flag
    orderType := OrderTypePostOnly

    order := &Order{
        Symbol:   symbol,
        Side:     side,
        Type:     orderType,
        Price:    price,
        Quantity: quantity,
        // Post-Only: Nếu không thể làm Maker, sàn sẽ hủy lệnh
    }

    // Nếu lệnh bị reject (không thể Post-Only), xử lý:
    // 1. Hủy lệnh
    // 2. Đặt lại với giá mới
    // 3. Hoặc skip lệnh này
}
```

### Solution: Smart Cancellation

**Implementation:**
```go
type SmartCancellation struct {
    enabled           bool
    spreadChangeThreshold float64 // % spread change để trigger
    checkInterval     time.Duration
    lastCheck         time.Time
}

func (s *SmartCancellation) ShouldCancelOrders(
    symbol string,
    currentSpread, lastSpread float64,
) bool {
    if !s.enabled {
        return false
    }

    // Nếu spread thay đổi quá nhanh
    spreadChange := math.Abs(currentSpread - lastSpread) / lastSpread
    if spreadChange > s.spreadChangeThreshold {
        return true
    }

    return false
}

func (g *GridManager) SmartCancelAndReplace(symbol string) {
    currentSpread := g.GetCurrentSpread(symbol)

    if g.smartCancel.ShouldCancelOrders(symbol, currentSpread, g.lastSpread) {
        g.logger.Info("Spread changed rapidly - cancelling and replacing orders",
            zap.String("symbol", symbol),
            zap.Float64("old_spread", g.lastSpread),
            zap.Float64("new_spread", currentSpread))

        // Hủy tất cả lệnh pending
        g.CancelAllOrders(context.Background(), symbol)

        // Đặt lại lưới với spread mới
        g.RebuildGrid(context.Background(), symbol)
    }

    g.lastSpread = currentSpread
}
```

**Benefits:**
- Đảm bảo luôn là Maker (fee thấp)
- Tránh Taker fees khi thị trường biến động
- Tối ưu hóa lợi nhuận

**Configuration:**
```yaml
maker_taker_optimization:
  post_only_enabled: true
  smart_cancellation:
    enabled: true
    spread_change_threshold: 0.2  # 20% spread change
    check_interval: 5s
```

---

## 4. Self-Healing Inventory Management

### Problem Analysis
Farming lâu sẽ bị lệch kho hàng:
- Cầm quá nhiều Long hoặc Short
- Đợi giá quay lại khớp lưới thì quá lâu
- Rủi ro nếu giá đi ngược hướng

### Solution: Internal Hedging

**Implementation:**
```go
type InventoryHedging struct {
    enabled           bool
    hedgeThreshold    float64 // % inventory để trigger hedge
    hedgePair         string  // Cặp tiền tương quan (BTC -> ETH)
    hedgeRatio        float64 // Tỷ lệ hedge (e.g., 0.3 = hedge 30%)
    maxHedgeSize      float64 // Max size cho hedge order
}

func (h *InventoryHedging) ShouldHedge(inventory, maxPosition float64) bool {
    inventoryPct := inventory / maxPosition
    return math.Abs(inventoryPct) > h.hedgeThreshold
}

func (h *InventoryHedging) CalculateHedgeSize(
    inventory, maxPosition float64,
    currentPrice float64,
) float64 {
    inventoryPct := inventory / maxPosition

    // Tính toán size hedge
    hedgeSize := math.Abs(inventory) * h.hedgeRatio

    // Giới hạn max
    if hedgeSize > h.maxHedgeSize {
        hedgeSize = h.maxHedgeSize
    }

    return hedgeSize
}

func (g *GridManager) ExecuteHedge(symbol string) {
    if g.inventoryHedging == nil || !g.inventoryHedging.enabled {
        return
    }

    position := g.GetPosition(symbol)
    if position == nil {
        return
    }

    if g.inventoryHedging.ShouldHedge(position.NotionalValue, g.maxPosition) {
        hedgeSize := g.inventoryHedging.CalculateHedgeSize(
            position.NotionalValue,
            g.maxPosition,
            g.GetCurrentPrice(symbol),
        )

        // Thực hiện hedge order (Taker để khớp ngay)
        hedgeSide := "SELL"
        if position.NotionalValue < 0 {
            hedgeSide = "BUY"
        }

        g.logger.Info("Executing inventory hedge",
            zap.String("symbol", symbol),
            zap.String("hedge_side", hedgeSide),
            zap.Float64("hedge_size", hedgeSize))

        // Place hedge order trên cặp tương quan hoặc scalping nhanh
        g.placeHedgeOrder(symbol, hedgeSide, hedgeSize)
    }
}
```

**Alternative: Scalping for Inventory Reduction**

```go
func (g *GridManager) ScalpingReduction(symbol string) {
    position := g.GetPosition(symbol)
    if position == nil {
        return
    }

    // Nếu Long quá nhiều, thực hiện scalping sell nhanh
    if position.NotionalValue > g.maxPosition * 0.3 {
        currentPrice := g.GetCurrentPrice(symbol)
        scalpingPrice := currentPrice * 0.999 // Sell 0.1% dưới giá thị trường
        scalpingSize := position.NotionalValue * 0.1 // Sell 10% của position

        // Place Taker order để khớp ngay
        g.placeScalpingOrder(symbol, "SELL", scalpingPrice, scalpingSize)
    }
}
```

**Benefits:**
- Giữ inventory trong range an toàn
- Không bị kẹt vị thế quá lớn
- Tiếp tục farming volume hiệu quả

**Configuration:**
```yaml
inventory_hedging:
  enabled: true
  hedge_threshold: 0.3    # 30% inventory trigger
  hedge_pair: "ETH"       # Hedge BTC với ETH
  hedge_ratio: 0.3        # Hedge 30% của position
  max_hedge_size: 100.0    # Max 100 USDT per hedge
  hedging_mode: "internal" # internal, cross_pair, or scalping
```

---

## Implementation Priority

### Phase 1 (High Priority - Immediate)
1. **Maker/Taker Logic**: Post-Only orders (T041)
   - Impact: Giảm fee ngay lập tức
   - Complexity: Thấp
   - Risk: Thấp

2. **Toxic Flow Detection**: VPIN (T040)
   - Impact: Bảo vệ khỏi adverse selection
   - Complexity: Trung bình
   - Risk: Thấp

### Phase 2 (Medium Priority)
3. **Order Priority**: Tick-size Awareness (T038)
   - Impact: Tăng tốc độ khớp
   - Complexity: Thấp
   - Risk: Thấp

4. **Smart Cancellation** (T042)
   - Impact: Tối ưu fee
   - Complexity: Trung bình
   - Risk: Trung bình

### Phase 3 (Advanced)
5. **Penny Jumping Strategy** (T039)
   - Impact: Tăng volume đáng kể
   - Complexity: Cao
   - Risk: Cao (cần backtesting kỹ)

6. **Self-Healing Inventory** (T043)
   - Impact: Quản lý rủi ro dài hạn
   - Complexity: Cao
   - Risk: Trung bình

---

## Configuration Template

```yaml
# agentic-vf-config.yaml
volume_farming_optimization:
  # Order Priority
  order_priority:
    tick_size_awareness:
      enabled: true
      tick_sizes:
        BTC: 0.1
        ETH: 0.01
        SOL: 0.001
    penny_jumping:
      enabled: false  # Enable sau khi backtest
      jump_threshold: 0.1  # 10% of spread
      max_jump: 3      # Max 3 ticks

  # Toxic Flow Detection
  toxic_flow_detection:
    enabled: true
    window_size: 50
    bucket_size: 1000.0
    vpin_threshold: 0.3
    action: "pause"  # pause, widen_spread, reduce_size

  # Maker/Taker Optimization
  maker_taker_optimization:
    post_only_enabled: true
    smart_cancellation:
      enabled: true
      spread_change_threshold: 0.2
      check_interval: 5s

  # Self-Healing Inventory
  inventory_hedging:
    enabled: true
    hedge_threshold: 0.3
    hedge_pair: "ETH"
    hedge_ratio: 0.3
    max_hedge_size: 100.0
    hedging_mode: "internal"
```

---

## Testing & Validation

### Unit Tests
- VPIN calculation accuracy
- Tick-size rounding
- Post-Only order handling

### Integration Tests
- Toxic flow triggers pause
- Smart cancellation behavior
- Inventory hedge execution

### Backtesting
- Penny jumping effectiveness
- VPIN threshold tuning
- Hedge ratio optimization

---

## Conclusion

Các cải tiến này sẽ giúp bot farming volume:
- Tăng hiệu quả khớp lệnh (Order Priority)
- Bảo vệ khỏi adverse selection (VPIN)
- Tối ưu fee (Maker/Taker Logic)
- Quản lý inventory hiệu quả (Self-Healing)

**Recommendation**: Implement Phase 1 trước, monitor kết quả, sau đó triển khai Phase 2 và 3.
