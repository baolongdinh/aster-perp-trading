# Volume Farming Bot - Giải Thích Logic Code Chi Tiết

> Tài liệu này giải thích chi tiết cách code hoạt động để thực hiện các nghiệp vụ grid trading được mô tả trong GRID_TRADING_LOGIC.md.

---

## � Mục Lục

1. [Kiến Trúc Tổng Quan](#1-kiến-trúc-tổng-quan)
2. [Luồng Xử Lý Chính](#2-luồng-xử-lý-chính)
3. [Entry Logic - Code Implementation](#3-entry-logic---code-implementation)
4. [Exit Logic - Code Implementation](#4-exit-logic---code-implementation)
5. [Risk Management - Code Implementation](#5-risk-management---code-implementation)
6. [Adaptive Grid - Code Implementation](#6-adaptive-grid---code-implementation)
7. [Các Module Quan Trọng](#7-các-module-quan-trọng)
8. [Data Flow](#8-data-flow)

---

## 1. Kiến Trúc Tổng Quan

### 1.1 Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    VOLUME FARM ENGINE                         │
│  (cmd/bot/main.go → volume_farm_engine.go)                    │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐        │
│  │   Grid       │  │   Adaptive   │  │    Risk      │        │
│  │   Manager    │  │   Grid Mgr   │  │   Manager    │        │
│  │              │  │              │  │              │        │
│  │ • Places     │  │ • Detects    │  │ • Enforces   │        │
│  │   orders     │  │   regime     │  │   limits     │        │
│  │ • Manages    │  │ • Adjusts    │  │ • Tracks PnL │        │
│  │   levels     │  │   spread     │  │ • Emergency  │        │
│  └──────────────┘  └──────────────┘  └──────────────┘        │
│         │                │                │                   │
│         └────────────────┼────────────────┘                   │
│                          │                                    │
│              ┌───────────▼────────────┐                       │
│              │   Futures Client       │                       │
│              │   (Binance API)        │                       │
│              │                        │                       │
│              │ • REST API             │                       │
│              │ • WebSocket            │                       │
│              │ • Order mgmt            │                       │
│              └────────────────────────┘                       │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### 1.2 Package Structure

```
backend/
├── cmd/bot/
│   └── main.go                    # Entry point
│
├── internal/
│   ├── farming/
│   │   ├── volume_farm_engine.go  # Main orchestrator
│   │   ├── adaptive_grid/
│   │   │   ├── manager.go         # Core grid logic
│   │   │   ├── grid_calculator.go # Price calculations
│   │   │   ├── order_placer.go    # Order execution
│   │   │   ├── inventory_manager.go # Inventory skew mgmt
│   │   │   ├── cluster_manager.go   # Cluster stop loss
│   │   │   └── slot_manager.go      # Time slot management
│   │   └── market_regime/
│   │       └── detector.go        # Trend detection
│   │
│   ├── risk/
│   │   └── risk_manager.go        # Risk enforcement
│   │
│   └── client/
│       └── futures_client.go      # Binance API wrapper
│
└── config/
    └── volume-farm-config.yaml    # Configuration
```

---

## 2. Luồng Xử Lý Chính

### 2.1 Main Loop Flow

```go
// From: volume_farm_engine.go - Run() method

func (e *VolumeFarmEngine) Run(ctx context.Context) error {
    // 1. Initialize all components
    e.initializeComponents()
    
    // 2. Start WebSocket for real-time data
    e.startWebSocket(ctx)
    
    // 3. Main trading loop
    for {
        select {
        case <-ticker.C:  // Every 1 second
            // A. Check risk limits
            if !e.riskManager.CanTrade() {
                continue  // Skip if risk limits hit
            }
            
            // B. Update market regime
            regime := e.detectMarketRegime()
            
            // C. Process each symbol
            for _, symbol := range e.activeSymbols {
                // 1. Fetch positions
                positions := e.fetchPositions(symbol)
                
                // 2. Check existing orders
                orders := e.fetchOpenOrders(symbol)
                
                // 3. Calculate grid levels
                grid := e.calculateGrid(symbol, regime)
                
                // 4. Place missing orders
                e.placeMissingOrders(symbol, grid, orders)
                
                // 5. Manage risk for existing positions
                e.checkRiskAndAct(symbol, positions)
            }
            
        case orderUpdate := <-e.orderUpdateCh:
            // Handle order fill events
            e.handleOrderFill(orderUpdate)
            
        case <-ctx.Done():
            return e.shutdown()
        }
    }
}
```

### 2.2 Order Fill Event Flow

```
[WebSocket] → [FuturesClient] → [VolumeFarmEngine] → [AdaptiveGridManager]
     ↓              ↓                    ↓                      ↓
  Receive      Parse JSON        handleOrderFill()      processFill()
  Raw Data     → OrderUpdate     → Update position      → Place opposite
                                                    → Record PnL
                                                    → Update stats
```

---

## 3. Entry Logic - Code Implementation

### 3.1 Grid Level Calculation

```go
// From: adaptive_grid/grid_calculator.go

type GridCalculator struct {
    spread         float64  // Grid spread percentage
    maxOrders      int      // Max orders per side
    orderSizeUSDT  float64  // Order size in USDT
}

// CalculateGridLevels computes buy and sell grid levels
func (gc *GridCalculator) CalculateGridLevels(
    symbol string,
    currentPrice float64,
    inventorySkew float64,
) (*GridLevels, error) {
    
    levels := &GridLevels{
        Symbol: symbol,
        BuyLevels:  make([]GridLevel, 0, gc.maxOrders),
        SellLevels: make([]GridLevel, 0, gc.maxOrders),
    }
    
    // Calculate spread amount
    spreadAmount := currentPrice * gc.spread
    
    // Generate BUY levels (below current price)
    for i := 1; i <= gc.maxOrders; i++ {
        price := currentPrice - (float64(i) * spreadAmount)
        
        // Adjust for inventory skew
        if inventorySkew > 0.5 {  // Too much LONG
            price *= 1.02  // Move buy orders further down
        }
        
        levels.BuyLevels = append(levels.BuyLevels, GridLevel{
            Price:  price,
            Amount: gc.orderSizeUSDT / price,
            Side:   "BUY",
        })
    }
    
    // Generate SELL levels (above current price)
    for i := 1; i <= gc.maxOrders; i++ {
        price := currentPrice + (float64(i) * spreadAmount)
        
        // Adjust for inventory skew
        if inventorySkew < -0.5 {  // Too much SHORT
            price *= 0.98  // Move sell orders further up
        }
        
        levels.SellLevels = append(levels.SellLevels, GridLevel{
            Price:  price,
            Amount: gc.orderSizeUSDT / price,
            Side:   "SELL",
        })
    }
    
    return levels, nil
}
```

### 3.2 Order Placement Logic

```go
// From: adaptive_grid/order_placer.go

func (op *OrderPlacer) PlaceGridOrders(
    ctx context.Context,
    symbol string,
    levels *GridLevels,
    existingOrders []Order,
) error {
    
    // 1. Determine which orders need to be placed
    neededBuys := op.findMissingOrders(levels.BuyLevels, existingOrders, "BUY")
    neededSells := op.findMissingOrders(levels.SellLevels, existingOrders, "SELL")
    
    // 2. Check risk limits before placing
    for _, level := range neededBuys {
        if !op.riskManager.CanEnter(symbol, level.Amount, level.Price, "BUY") {
            op.logger.Warn("Risk limit prevents BUY order",
                zap.String("symbol", symbol),
                zap.Float64("price", level.Price))
            continue
        }
        
        // 3. Place the order
        order, err := op.client.PlaceLimitOrder(ctx, PlaceOrderRequest{
            Symbol:   symbol,
            Side:     "BUY",
            Price:    level.Price,
            Quantity: level.Amount,
            TimeInForce: "GTC",
        })
        
        if err != nil {
            op.metrics.RecordOrderFailure(symbol, "BUY", err)
            continue
        }
        
        op.metrics.RecordOrderPlaced(symbol, "BUY", order.OrderID)
    }
    
    // Same for SELL orders...
    return nil
}

// findMissingOrders checks which grid levels don't have orders
func (op *OrderPlacer) findMissingOrders(
    required []GridLevel,
    existing []Order,
    side string,
) []GridLevel {
    missing := make([]GridLevel, 0)
    
    for _, req := range required {
        found := false
        for _, ex := range existing {
            // Check if existing order is at this price level
            if ex.Side == side && math.Abs(ex.Price-req.Price) < 0.01 {
                found = true
                break
            }
        }
        if !found {
            missing = append(missing, req)
        }
    }
    
    return missing
}
```

### 3.3 CanPlaceOrder - Entry Validation

```go
// From: adaptive_grid/manager.go

func (agm *AdaptiveGridManager) CanPlaceOrder(
    symbol string,
    side string,
    price float64,
) bool {
    agm.mu.RLock()
    defer agm.mu.RUnlock()
    
    // 1. Check if grid is paused
    if agm.pausedSymbols[symbol] {
        return false
    }
    
    // 2. Check cooldown after losses
    if agm.IsInCooldown(symbol) {
        return false
    }
    
    // 3. Check max orders limit
    currentOrders := agm.countOrdersForSymbol(symbol)
    if currentOrders >= agm.config.MaxOrdersPerSide*2 {
        return false
    }
    
    // 4. Check position limits
    if position, exists := agm.positions[symbol]; exists {
        if position.NotionalValue >= agm.config.MaxPositionUSDT {
            return false
        }
    }
    
    // 5. Check inventory skew
    if agm.inventoryMgr != nil {
        skew := agm.inventoryMgr.CalculateSkewRatio(symbol)
        action := agm.inventoryMgr.GetSkewAction(skew)
        
        // Don't add to the skewed side
        if (action == SkewActionReduceSkewSide || action == SkewActionPauseSkewSide) {
            if (side == "BUY" && skew > 0) || (side == "SELL" && skew < 0) {
                agm.logger.Warn("Skipping order due to inventory skew",
                    zap.String("symbol", symbol),
                    zap.String("side", side),
                    zap.Float64("skew", skew))
                return false
            }
        }
    }
    
    // 6. Check trend direction (directional bias)
    if agm.config.UseDirectionalBias && agm.trendDetector != nil {
        state := agm.trendDetector.GetTrendState()
        score := agm.trendDetector.GetTrendScore()
        
        // Strong trend - pause trading
        if score >= 4 {
            return false
        }
        
        // Block orders against trend
        if state == TrendStateStrongUp && side == "SELL" {
            return false
        }
        if state == TrendStateStrongDown && side == "BUY" {
            return false
        }
    }
    
    return true
}
```
---

## 4. Exit Logic - Code Implementation

### 4.1 Order Fill Handler

```go
// From: adaptive_grid/manager.go

func (agm *AdaptiveGridManager) handleOrderFill(fill OrderFill) {
    agm.mu.Lock()
    defer agm.mu.Unlock()
    
    symbol := fill.Symbol
    side := fill.Side
    filledQty := fill.Quantity
    filledPrice := fill.Price
    
    agm.logger.Info("Order filled",
        zap.String("symbol", symbol),
        zap.String("side", side),
        zap.Float64("qty", filledQty),
        zap.Float64("price", filledPrice))
    
    // 1. Update position tracking
    agm.updatePositionTracking(symbol, side, filledQty, filledPrice)
    
    // 2. Update metrics
    agm.metrics.RecordFill(symbol, side, filledQty*filledPrice)
    
    // 3. Calculate and place opposite order (auto-rebalance)
    oppositeSide := "SELL"
    if side == "SELL" {
        oppositeSide = "BUY"
    }
    
    // Calculate opposite price with spread
    spreadAmount := filledPrice * agm.config.GridSpreadPct
    var oppositePrice float64
    if oppositeSide == "SELL" {
        oppositePrice = filledPrice + spreadAmount
    } else {
        oppositePrice = filledPrice - spreadAmount
    }
    
    // 4. Queue the opposite order
    agm.orderQueue = append(agm.orderQueue, OrderRequest{
        Symbol:   symbol,
        Side:     oppositeSide,
        Price:    oppositePrice,
        Quantity: filledQty,
    })
    
    // 5. Check for realized PnL
    if agm.canCalculatePnL(symbol, side, filledPrice) {
        pnl := agm.calculateRealizedPnL(symbol, filledPrice, filledQty)
        agm.metrics.RecordPnL(symbol, pnl)
        
        // Update consecutive loss tracking
        isWin := pnl > 0
        agm.RecordTradeResult(symbol, isWin)
    }
}
```

### 4.2 PnL Calculation

```go
// From: adaptive_grid/pnl_tracker.go

type PnLTracker struct {
    realizedPnL     map[string]float64  // Symbol -> realized PnL
    unrealizedPnL   map[string]float64  // Symbol -> unrealized PnL
    avgEntryPrices  map[string]float64  // Symbol -> weighted avg entry
    positions       map[string]float64    // Symbol -> position size
}

// CalculateRealizedPnL computes PnL when a position is closed
func (pt *PnLTracker) CalculateRealizedPnL(
    symbol string,
    exitPrice float64,
    exitQty float64,
) float64 {
    avgEntry := pt.avgEntryPrices[symbol]
    
    if pt.positions[symbol] > 0 {  // Long position
        pnl := (exitPrice - avgEntry) * exitQty
        pt.realizedPnL[symbol] += pnl
        return pnl
    } else {  // Short position
        pnl := (avgEntry - exitPrice) * exitQty
        pt.realizedPnL[symbol] += pnl
        return pnl
    }
}

// CalculateUnrealizedPnL computes current open position PnL
func (pt *PnLTracker) CalculateUnrealizedPnL(
    symbol string,
    markPrice float64,
) float64 {
    position := pt.positions[symbol]
    avgEntry := pt.avgEntryPrices[symbol]
    
    if position == 0 {
        return 0
    }
    
    if position > 0 {  // Long
        return (markPrice - avgEntry) * position
    } else {  // Short
        return (avgEntry - markPrice) * math.Abs(position)
    }
}
---

## 5. Risk Management - Code Implementation

### 5.1 Risk Manager Core

```go
// From: risk/risk_manager.go

type Manager struct {
    cfg                 config.RiskConfig
    log                 *zap.Logger
    
    // Daily tracking
    dailyPnL            float64            // Realized PnL today
    dailyUnrealizedPnL  float64            // Current unrealized across all
    dailyStartingEquity float64            // Equity at start of day
    
    // Symbol tracking
    symPositions        map[string]int     // Symbol -> position count
    symNotional         map[string]float64 // Symbol -> position value
    symUnrealizedPnL    map[string]float64 // Symbol -> unrealized PnL
    
    // Risk state
    paused              bool               // Bot paused flag
    lastCumulativePnL   map[string]float64 // For loss tracking
}

// CanEnter checks if new position can be opened
func (m *Manager) CanEnter(symbol, side string, qty, price float64) bool {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // 1. Check if bot is paused
    if m.paused {
        return false
    }
    
    // 2. Check daily loss limit
    if m.dailyPnL < -m.cfg.DailyLossLimitUSDT {
        m.log.Warn("Daily loss limit reached")
        m.paused = true
        return false
    }
    
    // 3. Check drawdown limit (includes unrealized)
    if m.dailyStartingEquity > 0 {
        totalEquity := m.dailyStartingEquity + m.dailyPnL + m.dailyUnrealizedPnL
        drawdown := ((m.dailyStartingEquity - totalEquity) / m.dailyStartingEquity) * 100
        
        if drawdown >= m.cfg.DailyDrawdownPct {
            m.log.Warn("Drawdown limit reached", zap.Float64("drawdown", drawdown))
            m.paused = true
            return false
        }
    }
    
    // 4. Check max positions per symbol
    if m.symPositions[symbol] >= m.cfg.MaxTradesPerSymbol {
        return false
    }
    
    // 5. Check position size limit
    notional := qty * price
    if m.symNotional[symbol]+notional > m.cfg.MaxPositionUSDTPerSymbol {
        return false
    }
    
    // 6. Check total exposure
    totalExposure := m.getTotalExposure()
    if totalExposure+notional > m.cfg.MaxTotalPositionsUSDT {
        return false
    }
    
    return true
}

// UpdateUnrealizedPnL updates current unrealized PnL
func (m *Manager) UpdateUnrealizedPnL(symbol string, markPrice, avgEntry, positionAmt float64) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if positionAmt == 0 {
        m.symUnrealizedPnL[symbol] = 0
    } else if positionAmt > 0 {  // Long
        m.symUnrealizedPnL[symbol] = (markPrice - avgEntry) * positionAmt
    } else {  // Short
        m.symUnrealizedPnL[symbol] = (avgEntry - markPrice) * math.Abs(positionAmt)
    }
    
    // Recalculate total unrealized
    m.dailyUnrealizedPnL = 0
    for _, pnl := range m.symUnrealizedPnL {
        m.dailyUnrealizedPnL += pnl
    }
}
```

### 5.2 Consecutive Loss Tracking

```go
// From: adaptive_grid/manager.go

// RecordTradeResult tracks wins/losses for cooldown logic
func (agm *AdaptiveGridManager) RecordTradeResult(symbol string, isWin bool) {
    agm.mu.Lock()
    defer agm.mu.Unlock()
    
    // Also update risk monitor if available
    if agm.riskMonitor != nil {
        agm.riskMonitor.RecordTradeResult(isWin)
    }
    
    if isWin {
        // Reset consecutive losses on win
        if agm.consecutiveLosses[symbol] > 0 {
            agm.logger.Info("Win recorded - resetting consecutive losses",
                zap.String("symbol", symbol),
                zap.Int("previous_streak", agm.consecutiveLosses[symbol]))
        }
        agm.consecutiveLosses[symbol] = 0
        agm.cooldownActive[symbol] = false
    } else {
        // Increment consecutive losses
        agm.consecutiveLosses[symbol]++
        agm.lastLossTime[symbol] = time.Now()
        agm.totalLossesToday++
        
        agm.logger.Warn("Loss recorded",
            zap.String("symbol", symbol),
            zap.Int("consecutive_losses", agm.consecutiveLosses[symbol]),
            zap.Int("total_losses_today", agm.totalLossesToday))
        
        // Check if max consecutive losses reached
        maxLosses := agm.config.MaxConsecutiveLosses
        if maxLosses <= 0 {
            maxLosses = 3  // Default
        }
        
        if agm.consecutiveLosses[symbol] >= maxLosses {
            agm.cooldownActive[symbol] = true
            agm.pauseTrading(symbol)
            agm.logger.Error("MAX CONSECUTIVE LOSSES REACHED",
                zap.String("symbol", symbol),
                zap.Int("losses", agm.consecutiveLosses[symbol]))
        }
    }
}

// IsInCooldown checks if symbol is in cooldown period
func (agm *AdaptiveGridManager) IsInCooldown(symbol string) bool {
    agm.mu.RLock()
    defer agm.mu.RUnlock()
    
    if !agm.cooldownActive[symbol] {
        return false
    }
    
    cooldownDuration := agm.config.CooldownAfterLosses
    if cooldownDuration <= 0 {
        cooldownDuration = 5 * time.Minute  // Default
    }
    
    lastLoss := agm.lastLossTime[symbol]
    elapsed := time.Since(lastLoss)
    
    if elapsed >= cooldownDuration {
        // Cooldown expired - reset
        agm.mu.RUnlock()
        agm.mu.Lock()
        agm.cooldownActive[symbol] = false
        agm.consecutiveLosses[symbol] = 0
        agm.mu.Unlock()
        agm.mu.RLock()
        
        agm.logger.Info("Cooldown expired - resuming trading",
            zap.String("symbol", symbol),
            zap.Duration("elapsed", elapsed))
        return false
    }
    
    return true
}
---

## 6. Adaptive Grid - Code Implementation

### 6.1 Market Regime Detection

```go
// From: farming/market_regime/detector.go

type RegimeDetector struct {
    config   *RegimeDetectorConfig
    prices   []PricePoint  // Historical prices
    regime   MarketRegime  // Current regime
    mu       sync.RWMutex
}

type PricePoint struct {
    Price     float64
    High      float64
    Low       float64
    Volume    float64
    Timestamp time.Time
}

// DetectRegime analyzes market conditions
func (rd *RegimeDetector) DetectRegime() MarketRegime {
    rd.mu.Lock()
    defer rd.mu.Unlock()
    
    if len(rd.prices) < rd.config.LookbackPeriods {
        return RegimeUnknown
    }
    
    // Get recent prices
    recent := rd.prices[len(rd.prices)-rd.config.LookbackPeriods:]
    
    // Calculate ATR (Average True Range)
    atr := rd.calculateATR(recent)
    
    // Calculate directional movement
    adx, plusDI, minusDI := rd.calculateADX(recent)
    
    // Determine regime
    if adx > rd.config.TrendingThreshold {
        if plusDI > minusDI {
            rd.regime = RegimeTrendingUp
        } else {
            rd.regime = RegimeTrendingDown
        }
    } else if adx < rd.config.RangingThreshold {
        rd.regime = RegimeRanging
    } else {
        rd.regime = RegimeTransition
    }
    
    return rd.regime
}

// calculateATR computes Average True Range
func (rd *RegimeDetector) calculateATR(prices []PricePoint) float64 {
    if len(prices) < 2 {
        return 0
    }
    
    var sumTR float64
    for i := 1; i < len(prices); i++ {
        tr1 := prices[i].High - prices[i].Low
        tr2 := math.Abs(prices[i].High - prices[i-1].Price)
        tr3 := math.Abs(prices[i].Low - prices[i-1].Price)
        
        tr := tr1
        if tr2 > tr {
            tr = tr2
        }
        if tr3 > tr {
            tr = tr3
        }
        sumTR += tr
    }
    
    return sumTR / float64(len(prices)-1)
}
```

### 6.2 Grid Adjustment Based on Regime

```go
// From: adaptive_grid/manager.go - applyNewRegimeParameters

func (agm *AdaptiveGridManager) applyNewRegimeParameters(
    symbol string,
    regime MarketRegime,
) {
    agm.mu.Lock()
    defer agm.mu.Unlock()
    
    var newSpread float64
    var newMaxOrders int
    
    switch regime {
    case RegimeTrendingUp:
        // Reduce buy orders, increase spread
        newSpread = agm.config.GridSpreadPct * 1.5
        newMaxOrders = agm.config.MaxOrdersPerSide / 2
        agm.logger.Info("Adjusting for trending up",
            zap.String("symbol", symbol),
            zap.Float64("new_spread", newSpread),
            zap.Int("max_orders", newMaxOrders))
        
    case RegimeTrendingDown:
        // Reduce sell orders, increase spread
        newSpread = agm.config.GridSpreadPct * 1.5
        newMaxOrders = agm.config.MaxOrdersPerSide / 2
        agm.logger.Info("Adjusting for trending down",
            zap.String("symbol", symbol),
            zap.Float64("new_spread", newSpread),
            zap.Int("max_orders", newMaxOrders))
        
    case RegimeRanging:
        // Tighten spread for more fills
        newSpread = agm.config.GridSpreadPct * 0.8
        newMaxOrders = agm.config.MaxOrdersPerSide + 2
        agm.logger.Info("Adjusting for ranging market",
            zap.String("symbol", symbol),
            zap.Float64("new_spread", newSpread),
            zap.Int("max_orders", newMaxOrders))
        
    case RegimeVolatile:
        // Increase spread to avoid slippage
        newSpread = agm.config.GridSpreadPct * 2.0
        newMaxOrders = agm.config.MaxOrdersPerSide
        agm.logger.Info("Adjusting for volatile market",
            zap.String("symbol", symbol),
            zap.Float64("new_spread", newSpread))
    }
    
    // Apply new parameters
    agm.currentSpread = newSpread
    agm.currentMaxOrders = newMaxOrders
    
    // Trigger grid recalculation
    agm.needsRecalculation[symbol] = true
}
```

---

## 7. Các Module Quan Trọng

### 7.1 Inventory Manager

```go
// From: adaptive_grid/inventory_manager.go

// InventoryManager tracks and manages inventory skew
type InventoryManager struct {
    config         *InventoryConfig
    longPositions  map[string]float64  // Symbol -> long value
    shortPositions map[string]float64  // Symbol -> short value
    mu             sync.RWMutex
    logger         *zap.Logger
}

// CalculateSkewRatio computes inventory imbalance
func (im *InventoryManager) CalculateSkewRatio(symbol string) float64 {
    im.mu.RLock()
    defer im.mu.RUnlock()
    
    long := im.longPositions[symbol]
    short := im.shortPositions[symbol]
    total := long + short
    
    if total == 0 {
        return 0
    }
    
    // +1 = 100% long, -1 = 100% short, 0 = balanced
    return (long - short) / total
}

// GetSkewAction determines action based on skew
func (im *InventoryManager) GetSkewAction(skew float64) SkewAction {
    cfg := im.config.SkewThresholds
    
    absSkew := math.Abs(skew)
    
    if absSkew >= cfg.Critical {
        return SkewActionEmergencySkew
    } else if absSkew >= cfg.High {
        return SkewActionEmergencySkew
    } else if absSkew >= cfg.Moderate {
        return SkewActionPauseSkewSide
    } else if absSkew >= cfg.Low {
        return SkewActionReduceSkewSide
    }
    
    return SkewActionNormal
}
```

### 7.2 Cluster Stop Loss

```go
// From: adaptive_grid/cluster_manager.go

// ClusterStopLoss tracks position clusters for bulk exit
type ClusterStopLoss struct {
    config           *ClusterStopLossConfig
    positionClusters map[string]*PositionCluster
    mu               sync.RWMutex
    logger           *zap.Logger
}

type PositionCluster struct {
    Symbol        string
    EntryPrice    float64
    TotalAmount   float64
    EntryTime     time.Time
    Breakeven50   float64  // 50% breakeven price
    Breakeven100  float64  // 100% breakeven price
}

// CheckClusterExit checks if cluster should exit
func (csl *ClusterStopLoss) CheckClusterExit(
    symbol string,
    markPrice float64,
    totalUnrealizedPnL float64,
) (exitAction ExitAction, exitPrice float64) {
    csl.mu.RLock()
    defer csl.mu.RUnlock()
    
    cluster, exists := csl.positionClusters[symbol]
    if !exists {
        return ExitActionNone, 0
    }
    
    // Calculate drawdown from breakeven
    if cluster.EntryPrice > 0 {
        drawdown := (cluster.EntryPrice - markPrice) / cluster.EntryPrice * 100
        
        // Time-based exit (if held too long)
        heldDuration := time.Since(cluster.EntryTime)
        if heldDuration > csl.config.MaxStaleDuration {
            // Check for breakeven exit
            if math.Abs(totalUnrealizedPnL) < 1.0 {  // Near breakeven
                if markPrice >= cluster.Breakeven50 {
                    return ExitActionBreakeven50, markPrice
                }
            }
        }
        
        // Drawdown-based exit
        if drawdown >= csl.config.MaxClusterDrawdown {
            return ExitActionStopLoss, markPrice
        }
    }
    
    return ExitActionNone, 0
}
```

---

## 8. Data Flow

### 8.1 Order Lifecycle

```
┌─────────────────────────────────────────────────────────────┐
│                    ORDER LIFECYCLE                            │
└─────────────────────────────────────────────────────────────┘

1. GRID CALCULATION
   ├─ CalculateGridLevels() → GridLevels
   └─ Input: Current price, inventory skew, regime

2. RISK CHECK
   ├─ CanPlaceOrder() → bool
   ├─ Check: Position limits, drawdown, cooldown
   └─ Input: Symbol, side, price, quantity

3. ORDER PLACEMENT
   ├─ PlaceLimitOrder() → Order
   ├─ Via: FuturesClient (REST API)
   └─ Stored: In open orders map

4. WAIT FOR FILL
   ├─ WebSocket listens for updates
   └─ On PARTIAL_FILL: Continue waiting

5. FILL HANDLING
   ├─ handleOrderFill() processes fill
   ├─ Update position tracking
   ├─ Calculate PnL
   └─ Queue opposite order (auto-rebalance)

6. OPPOSITE ORDER PLACEMENT
   ├─ Take opposite side
   ├─ Price = Fill price ± spread
   └─ Return to step 2 (risk check)
```

### 8.2 Risk Check Flow

```
CanEnter() Flow:
═══════════════════════════════════════════════

Input: symbol, side, price, quantity

1. Bot Paused? ────────NO──────► 2. Daily Loss? ────────NO──────► 3. Drawdown?
   │YES                              │YES                            │YES
   ▼                                 ▼                              ▼
RETURN FALSE              PAUSE BOT         PAUSE BOT
                                         RETURN FALSE              ▼
                                                              4. Max Positions? ──NO──► 5. Position Size?
                                                                 │YES                       │YES
                                                                 ▼                          ▼
                                                           RETURN FALSE          RETURN FALSE
                                                                                     ▼
                                                                               6. Total Exposure?
                                                                                  │YES
                                                                                  ▼
                                                                           RETURN FALSE
                                                                                     ▼
                                                                               7. Inventory Skew?
                                                                                  │BLOCK SIDE
                                                                                  ▼
                                                                           RETURN FALSE
                                                                                     ▼
                                                                               8. Trend Direction?
                                                                                  │BLOCK COUNTER-TREND
                                                                                  ▼
                                                                           RETURN FALSE
                                                                                     ▼
                                                                           RETURN TRUE ✓
```

---

## � Tóm Tắt Logic Code

### Entry Flow
```
Ticker → Fetch Price → Detect Regime → Calc Grid → Risk Check → Place Order
```

### Exit Flow
```
WebSocket → Order Fill → Update Position → Calc PnL → Queue Opposite → Place Order
```

### Risk Flow
```
Position Check → Calc Drawdown (realized + unrealized) → Check Limits → Act if Breached
```

### Key Files
| File | Purpose |
|------|---------|
| `manager.go` | Core orchestration |
| `risk_manager.go` | Risk enforcement |
| `grid_calculator.go` | Price level calculation |
| `detector.go` | Market regime detection |
| `inventory_manager.go` | Skew management |
| `cluster_manager.go` | Bulk exit logic |

---

*Logic code được thiết kế modular để dễ bảo trì và mở rộng. Mỗi nghiệp vụ trong GRID_TRADING_LOGIC.md đều có code implementation tương ứng.*
