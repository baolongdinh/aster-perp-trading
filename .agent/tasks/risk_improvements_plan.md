# Risk Management Improvements - Implementation Plan

## Overview
Implementation plan for critical risk management improvements to the Aster Perp Trading Bot.

**Created**: 2026-04-07
**Priority**: Critical & High
**Estimated Duration**: 2-3 days

---

## Phase 1: CRITICAL - Take-Profit Logic (TP001)

### 1.1 Grid Level Take-Profit System
**Files to modify**:
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/farming/adaptive_grid/risk_sizing.go`
- `backend/config/adaptive_config.yaml`

**Implementation Steps**:

1. **Add TP Configuration to EnhancedRiskConfig** (`risk_sizing.go:237-261`):
```go
// New fields to add:
TakeProfitRRatio       float64  // Target R:R ratio (e.g., 1.5 = 1.5:1)
MinTakeProfitPct       float64  // Minimum TP as % (e.g., 0.01 = 1%)
MaxTakeProfitPct       float64  // Maximum TP as % (e.g., 0.05 = 5%)
UseBreakevenTP         bool     // Enable breakeven-based TP
BreakevenBufferPct     float64  // Buffer above breakeven (e.g., 0.005 = 0.5%)
```

2. **Create GridLevelTP struct** (new file or in `manager.go`):
```go
type GridLevelTP struct {
    Level           int
    EntryPrice      float64
    StopLossPrice   float64
    TakeProfitPrice float64
    RiskAmount      float64 // USD risked at this level
    RewardAmount    float64 // USD reward target
    RR              float64 // Risk:Reward ratio
    Status          TPStatus // PENDING, HIT, CANCELLED
}

type TPStatus int
const (
    TPPending GridLevelTPStatus = iota
    TPHit
    TPCancelled
)
```

3. **Calculate TP Price** (new method in `manager.go`):
```go
func (a *AdaptiveGridManager) CalculateTakeProfitPrice(
    entryPrice float64,
    stopLossPrice float64,
    side string,
    skewRatio float64,
) float64 {
    // Calculate risk distance
    riskDistance := math.Abs(entryPrice - stopLossPrice)
    
    // Get target R:R from config
    targetRR := a.riskConfig.TakeProfitRRatio
    if targetRR <= 0 {
        targetRR = 1.5 // Default 1.5:1
    }
    
    // Calculate reward distance
    rewardDistance := riskDistance * targetRR
    
    // Apply inventory adjustment (reduce TP when skewed)
    tpAdjustment := a.inventoryMgr.GetAdjustedTakeProfitDistance(symbol, side, 0)
    // tpAdjustment is reduction %, so actual multiplier = 1 - reduction
    rewardDistance = rewardDistance * (1 - tpAdjustment)
    
    // Calculate TP price
    if side == "LONG" {
        return entryPrice + rewardDistance
    }
    return entryPrice - rewardDistance
}
```

4. **Add TP Tracking to Position** (`manager.go:639-654`):
```go
// Add to SymbolPosition struct:
type SymbolPosition struct {
    // ... existing fields ...
    TakeProfitPrice   float64
    RiskDistance      float64
    RewardDistance    float64
    TargetRR          float64
}
```

5. **Check TP in evaluateRiskAndAct** (`manager.go:720-787`):
```go
// Add after stop loss check (line 738):
// 1.5. Check take profit
if a.isTakeProfitHit(symbol, markPrice, pos.PositionAmt) {
    a.logger.Info("TAKE PROFIT HIT - Closing position",
        zap.String("symbol", symbol),
        zap.Float64("mark_price", markPrice),
        zap.Float64("unrealized_pnl", unrealizedPnL))
    a.closePositionWithProfit(ctx, symbol, pos.PositionAmt)
    return
}
```

6. **YAML Config Updates** (`adaptive_config.yaml`):
```yaml
risk_management:
  take_profit:
    rr_ratio: 1.5           # Target 1.5:1 R:R
    min_tp_pct: 0.01        # Minimum 1% TP
    max_tp_pct: 0.05        # Maximum 5% TP
    use_breakeven: true     # Enable breakeven exits
    breakeven_buffer_pct: 0.005  # 0.5% buffer above breakeven
```

---

## Phase 2: CRITICAL - Fix Drawdown Calculation (DD001)

### 2.1 Correct Drawdown Formula
**Files to modify**:
- `backend/internal/risk/risk_manager.go`

**Current (Wrong)**:
```go
// Line 226-234
drawdown := (m.dailyPnL / m.dailyStartingEquity) * 100  // ❌ Wrong!
```

**Correct Implementation**:
```go
// 2. PERCENTAGE LIMIT (Drawdown) - Corrected
if m.dailyStartingEquity > 0 {
    currentEquity := m.dailyStartingEquity + m.dailyPnL + totalUnrealizedPnL
    drawdownPct := ((m.dailyStartingEquity - currentEquity) / m.dailyStartingEquity) * 100
    
    if drawdownPct >= m.cfg.DailyDrawdownPct {
        m.paused = true
        m.log.Warn("risk: daily drawdown percentage hit, bot paused",
            zap.Float64("drawdown_pct", drawdownPct),
            zap.Float64("limit_pct", m.cfg.DailyDrawdownPct),
            zap.Float64("starting_equity", m.dailyStartingEquity),
            zap.Float64("current_equity", currentEquity),
            zap.Float64("realized_pnl", m.dailyPnL),
            zap.Float64("unrealized_pnl", totalUnrealizedPnL),
        )
    }
}
```

**Required Changes**:
1. Add field to track total unrealized PnL across all symbols
2. Add method to update unrealized PnL periodically
3. Use correct drawdown formula: `(StartEquity - CurrentEquity) / StartEquity`

---

## Phase 3: HIGH - Integrate EnhancedRiskConfig (RC001)

### 3.1 Wire Up EnhancedRiskConfig
**Files to modify**:
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/farming/adaptive_grid/risk_sizing.go`
- `backend/internal/farming/volume_farm_engine.go`

**Implementation**:

1. **Replace RiskConfig with EnhancedRiskConfig** in `manager.go`:
```go
// Change from:
riskConfig *RiskConfig

// To:
riskConfig *EnhancedRiskConfig
```

2. **Add Consecutive Loss Tracking** (new fields in `manager.go`):
```go
type AdaptiveGridManager struct {
    // ... existing fields ...
    consecutiveLosses   map[string]int      // symbol -> count
    lastLossTime        map[string]time.Time
    cooldownActive      map[string]bool
    totalLossesToday    int
}
```

3. **Record Trade Result** (in `manager.go`):
```go
func (a *AdaptiveGridManager) RecordTradeResult(symbol string, realizedPnL float64) {
    a.mu.Lock()
    defer a.mu.Unlock()
    
    if realizedPnL < 0 {
        a.consecutiveLosses[symbol]++
        a.lastLossTime[symbol] = time.Now()
        a.totalLossesToday++
        
        // Check max consecutive losses
        if a.riskConfig.MaxConsecutiveLosses > 0 && 
           a.consecutiveLosses[symbol] >= a.riskConfig.MaxConsecutiveLosses {
            a.pauseTrading(symbol)
            a.cooldownActive[symbol] = true
            a.logger.Warn("Max consecutive losses reached - Entering cooldown",
                zap.String("symbol", symbol),
                zap.Int("losses", a.consecutiveLosses[symbol]),
                zap.Duration("cooldown", a.riskConfig.CooldownAfterLosses))
        }
    } else {
        // Reset on win
        a.consecutiveLosses[symbol] = 0
        a.cooldownActive[symbol] = false
    }
}
```

4. **Check Cooldown in CanPlaceOrder** (`manager.go:1279`):
```go
func (a *AdaptiveGridManager) CanPlaceOrder(symbol string) bool {
    // ... existing checks ...
    
    // Check cooldown
    if a.cooldownActive[symbol] {
        if lastLoss, exists := a.lastLossTime[symbol]; exists {
            if time.Since(lastLoss) < a.riskConfig.CooldownAfterLosses {
                a.logger.Debug("Order blocked - cooldown active",
                    zap.String("symbol", symbol),
                    zap.Duration("remaining", a.riskConfig.CooldownAfterLosses - time.Since(lastLoss)))
                return false
            }
            // Cooldown expired
            a.cooldownActive[symbol] = false
            a.consecutiveLosses[symbol] = 0
        }
    }
    
    return true
}
```

---

## Phase 4: HIGH - Wire Up Directional Bias (RC002)

### 4.1 Integrate TrendDetector with Order Placement
**Files to modify**:
- `backend/internal/farming/adaptive_grid/manager.go`
- `backend/internal/farming/adaptive_grid/trend_detector.go`

**Implementation**:

1. **Add Trend Check to CanPlaceOrder**:
```go
func (a *AdaptiveGridManager) CanPlaceOrder(symbol string, side string) bool {
    // ... existing checks ...
    
    // Check directional bias
    if a.riskConfig.UseDirectionalBias && a.trendDetector != nil {
        trendState := a.trendDetector.GetTrendState()
        trendScore := a.trendDetector.GetTrendScore()
        
        // Strong trend detected
        if trendScore >= 4 {
            // Check if order is against trend
            isAgainstTrend := false
            if trendState == TrendStateStrongUp && side == "SHORT" {
                isAgainstTrend = true
            } else if trendState == TrendStateStrongDown && side == "LONG" {
                isAgainstTrend = true
            }
            
            if isAgainstTrend {
                if a.riskConfig.TrendFollowingOnly {
                    a.logger.Warn("Order blocked - Against strong trend",
                        zap.String("symbol", symbol),
                        zap.String("side", side),
                        zap.String("trend", trendState.String()))
                    return false
                }
                // Allow but with reduced size (handled elsewhere)
            }
        }
    }
    
    return true
}
```

2. **Add Trend-Adjusted Sizing**:
```go
func (a *AdaptiveGridManager) GetTrendAdjustedSize(
    baseSize float64,
    side string,
) float64 {
    if !a.riskConfig.UseDirectionalBias || a.trendDetector == nil {
        return baseSize
    }
    
    trendState := a.trendDetector.GetTrendState()
    trendScore := a.trendDetector.GetTrendScore()
    
    // With trend: increase size
    // Against trend: decrease size
    multiplier := 1.0
    
    switch trendState {
    case TrendStateStrongUp:
        if side == "LONG" {
            multiplier = 1.0 + (float64(trendScore) * 0.05) // +5% per score point
        } else {
            multiplier = 1.0 - (float64(trendScore) * 0.1) // -10% per score point
        }
    case TrendStateStrongDown:
        if side == "SHORT" {
            multiplier = 1.0 + (float64(trendScore) * 0.05)
        } else {
            multiplier = 1.0 - (float64(trendScore) * 0.1)
        }
    }
    
    // Ensure minimum size
    if multiplier < 0.2 {
        multiplier = 0.2 // Still place some orders
    }
    
    return baseSize * multiplier
}
```

---

## Phase 5: HIGH - Enable Correlation Check (COR001)

### 5.1 Wire Up CorrelationTracker
**Files to modify**:
- `backend/internal/farming/volume_farm_engine.go`
- `backend/internal/risk/risk_manager.go`
- `backend/internal/strategy/regime/correlation_tracker.go` (verify exists)

**Implementation**:

1. **Initialize CorrelationTracker** (in `volume_farm_engine.go`):
```go
// After creating risk manager
corrTracker := regime.NewCorrelationTracker(
    engine.riskManager.cfg.CorrelationThreshold,
    logger,
)
engine.riskManager.SetCorrelationTracker(corrTracker)

// Start correlation update loop
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            prices := engine.fetchAllSymbolPrices()
            corrTracker.UpdatePrices(prices)
        case <-engine.ctx.Done():
            return
        }
    }
}()
```

2. **Verify Correlation Check in CanEnter** (`risk_manager.go:67-131`):
```go
// Lines 102-115 already have correlation check
// Ensure CorrelationTracker is properly initialized and threshold is configurable
```

3. **Add Config Field** (`config/config.go:46-65`):
```go
// Verify exists:
CorrelationThreshold float64 `mapstructure:"correlation_threshold"` // Default 0.8
```

---

## Phase 6: MEDIUM - Per-Level TP Prices (TP002)

### 6.1 Grid Level TP Management
**Files to modify**:
- `backend/internal/farming/adaptive_grid/grid_manager.go`
- `backend/internal/farming/adaptive_grid/manager.go`

**Implementation**:

1. **Add TP to GridOrder**:
```go
type GridOrder struct {
    // ... existing fields ...
    TakeProfitPrice float64
    StopLossPrice   float64
    RiskAmount      float64
    RR              float64
}
```

2. **Calculate Grid with TP**:
```go
func (g *GridManager) BuildGridWithTP(
    ctx context.Context,
    symbol string,
    centerPrice float64,
    levels int,
    spreadPct float64,
    riskConfig *EnhancedRiskConfig,
) error {
    // ... build grid levels ...
    
    for i := 1; i <= levels; i++ {
        buyPrice := centerPrice * (1 - spreadPct*float64(i))
        sellPrice := centerPrice * (1 + spreadPct*float64(i))
        
        // Calculate SL and TP for each level
        buySL := buyPrice * (1 - riskConfig.StopLossPct)
        buyTP := g.calculateTPPrice(buyPrice, buySL, "LONG", riskConfig)
        
        sellSL := sellPrice * (1 + riskConfig.StopLossPct)
        sellTP := g.calculateTPPrice(sellPrice, sellSL, "SHORT", riskConfig)
        
        // Store in grid orders
        g.orders[symbol] = append(g.orders[symbol], GridOrder{
            Price:           buyPrice,
            Side:            "BUY",
            StopLossPrice:   buySL,
            TakeProfitPrice: buyTP,
            RR:              riskConfig.TakeProfitRRatio,
        })
        
        g.orders[symbol] = append(g.orders[symbol], GridOrder{
            Price:           sellPrice,
            Side:            "SELL",
            StopLossPrice:   sellSL,
            TakeProfitPrice: sellTP,
            RR:              riskConfig.TakeProfitRRatio,
        })
    }
}
```

---

## Phase 7: MEDIUM - Inventory-Adjusted TP (TP003)

### 7.1 Implement in InventoryManager
**Files to modify**:
- `backend/internal/farming/adaptive_grid/inventory_manager.go`

**Current Issue**: `GetAdjustedTakeProfitDistance` uses hardcoded thresholds

**Fix**: Use config thresholds
```go
func (im *InventoryManager) GetAdjustedTakeProfitDistance(
    symbol string,
    side string,
    baseDistance float64,
) float64 {
    skewRatio := im.CalculateSkewRatio(symbol)
    
    // Get thresholds from config
    var reduction float64
    switch {
    case skewRatio < im.config.LowThreshold.Threshold:
        return baseDistance
    case skewRatio < im.config.ModerateThreshold.Threshold:
        reduction = im.config.ModerateThreshold.TakeProfitReduction
    case skewRatio < im.config.HighThreshold.Threshold:
        reduction = im.config.HighThreshold.TakeProfitReduction
    default:
        reduction = im.config.CriticalThreshold.TakeProfitReduction
    }
    
    return baseDistance * (1 - reduction)
}
```

---

## Testing Strategy

### Unit Tests
1. `TestTakeProfitCalculation` - Verify R:R math
2. `TestDrawdownCalculation` - Verify includes unrealized
3. `TestConsecutiveLossCooldown` - Verify cooldown logic
4. `TestTrendAdjustedSizing` - Verify trend multiplier
5. `TestCorrelationBlock` - Verify correlated pairs blocked

### Integration Tests
1. `TestGridWithTP` - Full grid with TP placement
2. `TestRiskManagerIntegration` - All risk layers working together
3. `TestEmergencyCloseAll` - Verify TP/SL + pending orders cancelled

---

## Dependencies & Blockers

- **None** - All changes are internal to existing codebase
- **Config changes** require updating YAML files
- **No external API changes**

---

## Success Criteria

1. ✅ TP hits close positions with profit
2. ✅ Drawdown calculated correctly (includes unrealized)
3. ✅ Bot pauses after N consecutive losses
4. ✅ Cooldown expires and trading resumes
5. ✅ Against-trend orders reduced/blocked
6. ✅ Correlated symbols blocked
7. ✅ All tests pass
8. ✅ No runtime errors in 24h paper trading
