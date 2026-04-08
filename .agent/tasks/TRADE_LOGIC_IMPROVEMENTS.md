# Trade Logic Improvements - Implementation Tasks

> Phân rã task implementation cho 5 cải tiến logic trading
> Created: 2026-04-08

---

## Overview

| Feature | Priority | Effort | Dependencies |
|---------|----------|--------|--------------|
| 1. Dynamic Liquidation Buffer | P0 | Low | None |
| 2. Funding Rate Awareness | P0 | Low | None |
| 3. Dynamic Spread Adjustment | P1 | Medium | ATR Calculator |
| 4. Smart Position Sizing | P1 | Medium | Risk Monitor |
| 5. Partial Close Strategy | P2 | High | Position Manager |

---

## Phase 1: P0 Features (Quick Wins)

### Feature 1: Dynamic Liquidation Buffer Enhancement

**Goal**: Tự động điều chỉnh liquidation buffer theo leverage thay vì fixed 35%

**Files to modify**:
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/farming/adaptive_grid/risk_config.go` (nếu chưa có)

#### Tasks:

- [ ] T001 [P] [F1] Thêm hàm CalculateLiquidationBuffer vào manager.go
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  // Thêm sau line ~1110 (trong evaluateRiskAndAct hoặc làm helper)
  func (a *AdaptiveGridManager) CalculateLiquidationBuffer(leverage float64) float64 {
      if leverage >= 100 {
          return 0.50  // 50% buffer cho 100x
      } else if leverage >= 50 {
          return 0.35  // 35% cho 50x
      } else if leverage >= 20 {
          return 0.25  // 25% cho 20x
      }
      return 0.20  // 20% cho thấp hơn
  }
  ```

- [ ] T002 [F1] Cập nhật hàm isNearLiquidation để dùng dynamic buffer
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  // Sửa trong hàm isNearLiquidation (line ~1112)
  // Thay vì: return distancePct < a.riskConfig.LiquidationBufferPct
  // Thành dynamic calculation
  ```

- [ ] T003 [F1] Thêm leverage vào SymbolPosition struct tracking
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  // SymbolPosition đã có Leverage field, đảm bảo được set từ exchange
  ```

- [ ] T004 [F1] Viết unit test cho CalculateLiquidationBuffer
  ```go
  // File: backend/internal/farming/adaptive_grid/manager_test.go
  // Test các mức leverage: 100x, 50x, 20x, 10x
  ```

---

### Feature 2: Funding Rate Awareness

**Goal**: Điều chỉnh bias và size theo funding rate để tối ưu funding cost

**Files to modify**:
- `backend/internal/farming/adaptive_grid/funding_monitor.go` (đã có)
- `backend/internal/farming/adaptive_grid/inventory_manager.go`
- `backend/internal/farming/adaptive_grid/manager.go`

#### Tasks:

- [ ] T005 [P] [F2] Thêm FundingBiasConfig struct
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go hoặc funding_monitor.go
  type FundingBiasConfig struct {
      HighThreshold     float64  // 0.001 = 0.1%
      ExtremeThreshold  float64  // 0.005 = 0.5%
      BiasStrength      float64  // 0.7 = giảm 70% size ngược hướng
      SkipThreshold     float64  // Bỏ qua mở position nếu funding > threshold
  }
  ```

- [ ] T006 [P] [F2] Thêm GetCurrentRate method vào FundingRateMonitor
  ```go
  // File: backend/internal/farming/adaptive_grid/funding_monitor.go
  // Nếu chưa có, thêm method để lấy current funding rate
  func (f *FundingRateMonitor) GetCurrentRate(symbol string) float64
  ```

- [ ] T007 [F2] Thêm SetBias method vào InventoryManager
  ```go
  // File: backend/internal/farming/adaptive_grid/inventory_manager.go
  func (im *InventoryManager) SetBias(side string, strength float64)
  // strength: 0.0-1.0, điều chỉnh size theo hướng bias
  ```

- [ ] T008 [F2] Implement ApplyFundingBias trong AdaptiveGridManager
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  func (a *AdaptiveGridManager) ApplyFundingBias(symbol string) {
      rate := a.fundingMonitor.GetCurrentRate(symbol)
      
      if rate > 0.001 {  // > 0.1%
          a.inventoryMgr.SetBias("SHORT", 0.7)  // Ưu tiên short
      } else if rate < -0.001 {
          a.inventoryMgr.SetBias("LONG", 0.7)   // Ưu tiên long
      }
  }
  ```

- [ ] T009 [F2] Tích hợp ApplyFundingBias vào CanPlaceOrder
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  // Trong CanPlaceOrder hoặc logic đặt lệnh, gọi ApplyFundingBias
  ```

- [ ] T010 [F2] Thêm config funding_rate vào volume-farm-config.yaml
  ```yaml
  # File: backend/config/volume-farm-config.yaml
  funding_rate:
    high_threshold: 0.001
    extreme_threshold: 0.005
    bias_strength: 0.7
    enabled: true
  ```

---

## Phase 2: P1 Features (Medium Complexity)

### Feature 3: Dynamic Spread Adjustment

**Goal**: Tự động điều chỉnh grid spread theo ATR/volatility

**Files to modify**:
- `backend/internal/farming/adaptive_grid/dynamic_spread.go` (đã có)
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/farming/grid_manager.go`

#### Tasks:

- [ ] T011 [P] [F3] Thêm ATR-based spread calculation vào DynamicSpreadCalculator
  ```go
  // File: backend/internal/farming/adaptive_grid/dynamic_spread.go
  func (d *DynamicSpreadCalculator) CalculateATRSpread(symbol string, atr float64) float64
  // Sử dụng ATR để tính spread phù hợp
  ```

- [ ] T012 [F3] Thêm volatility thresholds vào DynamicSpreadConfig
  ```go
  // File: backend/internal/farming/adaptive_grid/dynamic_spread.go
  type VolatilityThresholds struct {
      LowATR     float64  // ATR < 0.5% = low vol
      HighATR    float64  // ATR > 2% = high vol
      Multiplier float64  // spread multiplier
  }
  ```

- [ ] T013 [F3] Implement CalculateDynamicSpread trong manager
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  func (a *AdaptiveGridManager) CalculateDynamicSpread(symbol string) float64 {
      atr := a.atrCalc.GetATR(symbol)
      baseSpread := a.riskConfig.BaseGridSpreadPct  // 0.15%
      
      if atr > highVolatilityThreshold {
          return baseSpread * 2.0  // 0.30%
      } else if atr < lowVolatilityThreshold {
          return baseSpread * 0.7  // 0.10%
      }
      return baseSpread
  }
  ```

- [ ] T014 [F3] Cập nhật GridManager để dùng dynamic spread
  ```go
  // File: backend/internal/farming/grid_manager.go
  // Trong placeGridOrders, lấy spread từ AdaptiveGridManager
  // thay vì dùng g.gridSpreadPct fixed
  ```

- [ ] T015 [F3] Thêm config dynamic_spread vào volume-farm-config.yaml
  ```yaml
  # File: backend/config/volume-farm-config.yaml
  dynamic_grid:
    enabled: true
    base_spread_pct: 0.0015
    low_atr_threshold: 0.005  # 0.5%
    high_atr_threshold: 0.02  # 2%
    low_multiplier: 0.7
    high_multiplier: 2.0
  ```

---

### Feature 4: Smart Position Sizing

**Goal**: Kelly Criterion + consecutive loss decay thay vì fixed $30

**Files to modify**:
- `backend/internal/farming/adaptive_grid/risk_monitor.go` (đã có)
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/farming/grid_manager.go`

#### Tasks:

- [ ] T016 [P] [F4] Thêm SmartSizingConfig struct
  ```go
  // File: backend/internal/farming/adaptive_grid/risk_monitor.go
  type SmartSizingConfig struct {
      BaseNotional         float64  // $30
      MaxRiskPerTrade      float64  // 2% balance
      KellyFraction        float64  // 0.25
      ConsecutiveLossDecay float64  // 0.8 = 20% reduction per loss
      MinSize              float64  // $5
      MaxSize              float64  // $100
  }
  ```

- [ ] T017 [P] [F4] Thêm trade result tracking
  ```go
  // File: backend/internal/farming/adaptive_grid/risk_monitor.go
  type TradeResult struct {
      Timestamp time.Time
      PnL       float64
      IsWin     bool
  }
  
  func (r *RiskMonitor) RecordTradeResult(symbol string, result TradeResult)
  func (r *RiskMonitor) GetRecentWinRate(window time.Duration) float64
  func (r *RiskMonitor) GetConsecutiveLosses(symbol string) int
  ```

- [ ] T018 [F4] Implement CalculateSmartSize theo Kelly
  ```go
  // File: backend/internal/farming/adaptive_grid/risk_monitor.go
  func (r *RiskMonitor) CalculateSmartSize(symbol string) float64 {
      // 1. Lấy consecutive losses
      losses := r.GetConsecutiveLosses(symbol)
      decay := math.Pow(r.config.ConsecutiveLossDecay, float64(losses))
      
      // 2. Lấy win rate
      winRate := r.GetRecentWinRate(24 * time.Hour)
      
      // 3. Kelly calculation (R:R = 1.5)
      kelly := (winRate*1.5 - (1-winRate)) / 1.5
      if kelly < 0 {
          kelly = 0.1  // Minimum 10% kelly
      }
      
      // 4. Conservative Kelly (fractional)
      size := r.config.BaseNotional * decay * kelly * r.config.KellyFraction
      
      // 5. Clamp to min/max
      return math.Max(r.config.MinSize, math.Min(size, r.config.MaxSize))
  }
  ```

- [ ] T019 [F4] Tích hợp CalculateSmartSize vào GridManager
  ```go
  // File: backend/internal/farming/grid_manager.go
  // Trong placeGridOrders, lấy size từ riskMonitor.CalculateSmartSize()
  // thay vì dùng g.baseNotionalUSD fixed
  ```

- [ ] T020 [F4] Cập nhật RecordTradeResult để track vào riskMonitor
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  // Trong closePositionWithProfit và emergencyClosePosition,
  // gọi a.riskMonitor.RecordTradeResult() với kết quả
  ```

- [ ] T021 [F4] Thêm config smart_sizing vào volume-farm-config.yaml
  ```yaml
  # File: backend/config/volume-farm-config.yaml
  smart_sizing:
    enabled: true
    base_notional: 30
    max_risk_per_trade: 0.02  # 2%
    kelly_fraction: 0.25
    consecutive_loss_decay: 0.8  # 20% reduction per loss
    min_size: 5
    max_size: 100
  ```

---

## Phase 3: P2 Features (Complex)

### Feature 5: Partial Close Strategy

**Goal**: Chốt lãi từng phần thay vì close 100% (TP1 30%, TP2 40%, TP3 30%)

**Files to modify**:
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/farming/adaptive_grid/position_tracker.go` (cần tạo)

#### Tasks:

- [ ] T022 [P] [F5] Thêm PartialCloseConfig struct
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  type PartialCloseConfig struct {
      TP1_Pct       float64  // 0.30 = 30%
      TP1_Profit    float64  // 0.005 = 0.5%
      TP2_Pct       float64  // 0.40 = 40%
      TP2_Profit    float64  // 0.01 = 1.0%
      TP3_Pct       float64  // 0.30 = 30%
      TP3_Profit    float64  // 0.015 = 1.5%
      TrailingAfter string   // "TP2" = trailing sau TP2
  }
  ```

- [ ] T023 [F5] Thêm PositionSlice tracking
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go hoặc position_tracker.go
  type PositionSlice struct {
      OriginalSize    float64
      RemainingSize   float64
      ClosedPct       float64
      TPLevels        []TPLevel
  }
  
  type TPLevel struct {
      TargetPct   float64  // % profit target
      ClosePct    float64  // % position to close
      IsHit       bool
      ExecutedQty float64
  }
  ```

- [ ] T024 [F5] Implement InitializePartialClose trong manager
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  func (a *AdaptiveGridManager) InitializePartialClose(symbol string, positionAmt float64) {
      // Khởi tạo position slices với 3 TP levels
      // TP1: 30% at 0.5%, TP2: 40% at 1.0%, TP3: 30% at 1.5%
  }
  ```

- [ ] T025 [F5] Implement CheckPartialTakeProfits
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  func (a *AdaptiveGridManager) CheckPartialTakeProfits(symbol string, markPrice float64) []PartialCloseOrder {
      // Check từng TP level, trả về danh sách orders cần close
  }
  ```

- [ ] T026 [F5] Sửa closePositionWithProfit thành partial
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  // Thay vì close 100%, chỉ close theo pct được chỉ định
  func (a *AdaptiveGridManager) closePositionPartial(ctx context.Context, symbol string, qty float64)
  ```

- [ ] T027 [F5] Cập nhật evaluateRiskAndAct để check partial TP
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  // Trong isTakeProfitHit, thay vì close 100%, gọi CheckPartialTakeProfits
  ```

- [ ] T028 [F5] Thêm trailing stop sau TP2
  ```go
  // File: backend/internal/farming/adaptive_grid/manager.go
  // Sau khi TP2 hit, activate trailing stop cho phần còn lại (30%)
  ```

- [ ] T029 [F5] Thêm config partial_close vào volume-farm-config.yaml
  ```yaml
  # File: backend/config/volume-farm-config.yaml
  partial_close:
    enabled: true
    tp1:
      close_pct: 0.30
      profit_pct: 0.005
    tp2:
      close_pct: 0.40
      profit_pct: 0.01
    tp3:
      close_pct: 0.30
      profit_pct: 0.015
    trailing_after: "TP2"
    trailing_distance: 0.005
  ```

---

## Dependencies Graph

```
Phase 1 (P0):
├── F1: Dynamic Liquidation Buffer
│   └── [Independent]
└── F2: Funding Rate Awareness
    └── [Independent]

Phase 2 (P1):
├── F3: Dynamic Spread Adjustment
│   └── Depends on: ATRCalculator (đã có)
└── F4: Smart Position Sizing
    └── Depends on: RiskMonitor (đã có)
    └── [Can parallel with F3]

Phase 3 (P2):
└── F5: Partial Close Strategy
    └── Depends on: F4 (position tracking)
    └── [Must be after F1-F4]
```

---

## Testing Strategy

### Unit Tests (cho mỗi feature):

- [ ] T030 [Test] Unit test cho CalculateLiquidationBuffer với các mức leverage
- [ ] T031 [Test] Unit test cho ApplyFundingBias với các funding rate scenarios
- [ ] T032 [Test] Unit test cho CalculateDynamicSpread với các ATR levels
- [ ] T033 [Test] Unit test cho CalculateSmartSize với Kelly formula
- [ ] T034 [Test] Unit test cho PartialClose với 3 TP levels

### Integration Tests:

- [ ] T035 [Test] Integration test: Dynamic spread + Position sizing cùng lúc
- [ ] T036 [Test] Integration test: Funding bias + Inventory skew
- [ ] T037 [Test] Integration test: Partial close với Emergency close

---

## Migration Guide

### Config Migration:

```yaml
# OLD config (current)
risk:
  liquidation_buffer_pct: 0.35  # Fixed
  
# NEW config (after implementation)
risk:
  liquidation_buffer_dynamic: true
  liquidation_buffer:
    leverage_100x: 0.50
    leverage_50x: 0.35
    leverage_20x: 0.25
    default: 0.20
  
  funding_rate_awareness: true
  
  dynamic_spread:
    enabled: true
    base: 0.0015
    
  smart_sizing:
    enabled: true
    base_notional: 30
    
  partial_close:
    enabled: false  # Tắt mặc định, bật khi cần
```

---

## Notes

1. **Backward Compatibility**: Tất cả features đều có `enabled` flag, default = false
2. **Feature Flags**: Có thể bật/tắt từng feature qua config
3. **Logging**: Mỗi feature cần log rõ ràng khi activate (để debug)
4. **Metrics**: Theo dõi hiệu quả của từng feature qua dashboard

---

## Estimated Timeline

| Phase | Features | Est. Time | Parallel?
|-------|----------|-----------|----------
| 1 | F1 + F2 | 2-3 days | Yes
| 2 | F3 + F4 | 4-5 days | Yes
| 3 | F5 | 3-4 days | No
| Test | All | 2-3 days | - 
| **Total** | | **11-15 days** | 

