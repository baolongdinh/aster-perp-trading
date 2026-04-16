# Detailed Implementation Plan: Adaptive State-Based Trading System

> Chi tiết từng state và chiến lược trade cho hệ thống "mềm dẻo như nước"

---

## Tổng Quan Kiến Trúc State Machine

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         ADAPTIVE STATE MANAGER                               │
├─────────────────────────────────────────────────────────────────────────────┤
│  Single Decision Engine (Centralized)                                       │
│  ├── Collects tín hiệu từ: GRID Worker, TREND Worker, RISK Worker          │
│  ├── Tính điểm: GRID Score vs TREND Score                                     │
│  ├── Quyết định state transition với confidence score                       │
│  └── Publish state change events (lock-free)                                │
└─────────────────────────────────────────────────────────────────────────────┘
                                    ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│                         STATE LIFECYCLE FLOW                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│   ┌──────────┐    Grid Score > 0.6      ┌──────────┐                         │
│   │   IDLE   │ ───────────────────────→ │ WAIT_NEW │ ←─────────────────┐    │
│   └──────────┘                          │ _RANGE    │                   │    │
│        ↑                                └──────────┘                   │    │
│        │ Trend Score > 0.75                      │                    │    │
│        └───────────────────────────────────────────┘                    │    │
│                                                     │                   │    │
│           ┌─────────────────────────────────────────┼───────────────────┘    │
│           │                                         ↓                        │
│           │                                ┌──────────┐                     │
│           │    Trend Score > 0.75           │  ENTER   │ Grid Score > 0.6     │
│           └───────────────────────────────→ │  _GRID   │ ─────────────────→   │
│                                           └──────────┘                     │
│                                                 │                          │
│           ┌─────────────────────────────────────┼──────────────────────┐   │
│           │                                     ↓                      │   │
│           │    Grid dominating             ┌──────────┐                │   │
│           ├───────────────────────────────→│ TRADING  │←───────────────┤   │
│           │    (Re-entry)                 │  (GRID)  │                │   │
│           │                                └──────────┘                │   │
│           │                                     │                    │   │
│           │    Trend dominating          ┌──────────┐                  │   │
│           └─────────────────────────────→│ TRENDING │←────────────────┘   │
│                (Re-entry)                │  (TREND) │                       │
│                                          └──────────┘                       │
│                                               │                             │
│   ┌──────────┐    Extreme Risk        ┌──────────┐    Gradual Exit    ┌──────────┐
│   │ RECOVERY │ ←──────────────────────│ EXIT_HALF│ ←────────────────│ EXIT_ALL │
│   └──────────┘                        └──────────┘                    └──────────┘
│        │                                                                    │
│        └────────────────────────────────────────────────────────────────────┘
│                                    (Back to WAIT_NEW_RANGE)
│
│   ┌──────────┐    Position too large    ┌──────────┐    Volatility spike
│   │ OVER_SIZE│ ←────────────────────────│ TRADING  │←──────────────────┐
│   └──────────┘                        │ TRENDING │                      │
│        │ Size normalized              └──────────┘                      │
│        └───────────────────────────────────────────────┐              │
│                                                         ↓              │
│                                                   ┌──────────┐         │
│                                                   │ DEFENSIVE│←────────┘
│                                                   └──────────┘
│                                                        │
│                                                        └─→ Volatility normalized
│
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Chi Tiết Từng State

### State 1: IDLE

**Mục đích**: Khởi động, chờ điều kiện thị trường rõ ràng

**Entry Conditions**:
- Bot vừa khởi động
- Vừa hoàn thành một chu kỳ trade, đang chờ cơ hội mới
- Không có vị thế mở

**Chiến lược Trade**:
```go
// Không trade trong state này
// Chỉ monitoring và tính toán scores
func (s *AdaptiveStateManager) idleStrategy(symbol string) {
    // 1. Calculate GRID score
    gridScore := s.calculateGridScore(symbol)
    
    // 2. Calculate TREND score  
    trendScore := s.calculateTrendScore(symbol)
    
    // 3. Decision
    if trendScore > 0.75 && trendScore > gridScore {
        s.transitionTo(symbol, StateTrending, trendScore)
    } else if gridScore > 0.6 {
        s.transitionTo(symbol, StateWaitNewRange, gridScore)
    }
    // Else: stay in IDLE, continue monitoring
}
```

**Exit Conditions**:
| Condition | Target State | Confidence |
|-----------|--------------|------------|
| Grid Score > 0.6 | WAIT_NEW_RANGE | 0.6-0.8 |
| Trend Score > 0.75 | TRENDING | 0.75-0.9 |
| Cả 2 scores < 0.4 | Stay IDLE | - |

**Risk Management**:
- Không có vị thế → Không risk
- Max time in IDLE: 300s (force evaluation)

**Monitoring Metrics**:
- Grid Score trend (5 phút)
- Trend Score trend (5 phút)
- Market volatility (ATR)

---

### State 2: WAIT_NEW_RANGE

**Mục đích**: Chờ xác nhận range sideway để vào lệnh grid

**Entry Conditions**:
- Từ IDLE: Grid Score > 0.6
- Từ EXIT_ALL: Sau khi đóng vị thế, thị trường vẫn sideways

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) waitNewRangeStrategy(symbol string) {
    // 1. Detect range boundaries
    rangeHigh, rangeLow := s.detectRange(symbol)
    
    // 2. Calculate range quality score
    rangeScore := s.calculateRangeQuality(rangeHigh, rangeLow, symbol)
    
    // 3. Check for compression (preparing for breakout)
    if s.isCompressionDetected(symbol) {
        // Transition to ACCUMULATION state (pre-breakout)
        s.transitionTo(symbol, StateAccumulation, 0.7)
        return
    }
    
    // 4. Range confirmed → Enter Grid
    if rangeScore > 0.7 {
        s.setRangeBoundaries(symbol, rangeHigh, rangeLow)
        s.transitionTo(symbol, StateEnterGrid, rangeScore)
    }
    
    // 5. Trend detected while waiting → Switch to TRENDING
    trendScore := s.calculateTrendScore(symbol)
    if trendScore > 0.75 && trendScore > rangeScore {
        s.transitionTo(symbol, StateTrending, trendScore)
    }
}
```

**Trading Logic**:
- **Không vào lệnh** trong state này
- **Monitoring**: Range detection, compression patterns
- **Preparation**: Tính toán grid levels, position sizing

**Exit Conditions**:
| Condition | Target State | Trigger | Confidence |
|-----------|--------------|---------|------------|
| Range confirmed | ENTER_GRID | EventRangeConfirmed | 0.7+ |
| Compression detected | ACCUMULATION | EventCompressionDetected | 0.7+ |
| Trend mạnh xuất hiện | TRENDING | EventTrendDetected | 0.75+ |
| Timeout (120s) | IDLE | EventTimeout | 0.5 |

**Risk Management**:
- Max wait time: 120s (tránh chờ vô hạn)
- Nếu volatility tăng >200% → chuyển DEFENSIVE

---

### State 3: ENTER_GRID

**Mục đích**: Chuẩn bị và đặt lệnh grid

**Entry Conditions**:
- Từ WAIT_NEW_RANGE: Range confirmed
- Từ RECOVERY: Sau khi recover, thị trường sideways

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) enterGridStrategy(symbol string) {
    // 1. Get strategy signals
    signals := s.signalAggregator.GetSignals(symbol)
    
    // 2. Calculate optimal grid parameters
    gridParams := s.calculateGridParameters(symbol, signals)
    
    // 3. Asymmetric spread based on signals
    if fvgSignal := signals.GetFVG(); fvgSignal != nil {
        // Tighten spread on FVG side
        if fvgSignal.Side == SideBuy {
            gridParams.BuySpread *= 0.7  // Tighten 30%
            gridParams.SellSpread *= 1.2 // Widen 20%
        } else {
            gridParams.BuySpread *= 1.2
            gridParams.SellSpread *= 0.7
        }
    }
    
    // 4. Signal-triggered entry (hybrid mode)
    if s.shouldWaitForSignal(symbol) {
        bestSignal := s.getBestEntrySignal(signals)
        if bestSignal.Strength < s.config.EntryMinSignalStrength {
            // Wait for better signal
            if s.entryTimeoutExpired(symbol) {
                // Timeout → enter anyway with reduced size
                gridParams.SizeMultiplier *= 0.6
            } else {
                return // Keep waiting
            }
        }
    }
    
    // 5. Place grid orders
    s.placeGridOrders(symbol, gridParams)
    s.transitionTo(symbol, StateTrading, 0.8)
}
```

**Grid Parameters**:
| Parameter | Formula | Range |
|-----------|---------|-------|
| Grid Levels | Based on ATR | 5-15 levels |
| Spread | ATR * multiplier | 0.5% - 2% |
| Position Size | Base * signal strength | 0.6x - 1.3x |
| Total Exposure | Risk / (Grid levels * Spread) | Max 5% account |

**Trading Logic**:
- **Entry**: Place limit orders at grid levels
- **Signal Enhancement**: Tighten spread ở vùng có signal mạnh
- **Hybrid Mode**: Chờ signal hoặc timeout

**Exit Conditions**:
| Condition | Target State | Trigger |
|-----------|--------------|---------|
| Grid placed | TRADING | EventEntryPlaced | 
| Trend mạnh xuất hiện | TRENDING | EventTrendDetected |
| Cancel/Error | WAIT_NEW_RANGE | EventEntryFailed |

**Risk Management**:
- Max grid exposure: 5% account
- Stop-loss: Range boundary +/- buffer
- Emergency: Cancel all nếu volatility >300%

---

### State 4: TRADING (GRID Mode)

**Mục đích**: Quản lý grid positions, farm volume trong sideways

**Entry Conditions**:
- Từ ENTER_GRID: Grid đã được đặt
- Từ OVER_SIZE: Size normalized
- Từ DEFENSIVE: Volatility normalized
- Từ RECOVERY: Recovery complete

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) tradingGridStrategy(symbol string) {
    // 1. Monitor fills và PnL
    pnl := s.getUnrealizedPnL(symbol)
    filledLevels := s.getFilledGridLevels(symbol)
    
    // 2. Continuous signal blending
    blendedSignal := s.blendEngine.CalculateBlend(symbol)
    
    // 3. Adjust grid parameters based on signals
    if blendedSignal.Entropy < 0.3 {
        // Signals agree → confident mode
        s.adjustGridIntensity(symbol, 1.2)
    } else if blendedSignal.Entropy > 0.7 {
        // Signals conflict → defensive
        s.adjustGridIntensity(symbol, 0.7)
    }
    
    // 4. Rebalancing khi cần
    if s.shouldRebalance(symbol) {
        s.rebalanceGrid(symbol)
    }
    
    // 5. Check for trend emergence
    trendScore := s.calculateTrendScore(symbol)
    if trendScore > 0.8 && trendScore > s.getGridScore(symbol)*1.2 {
        // Strong trend emerging → transition
        s.initiateGracefulExit(symbol, StateTrending)
        return
    }
    
    // 6. Risk checks
    if pnl < -s.config.MaxGridLoss {
        s.transitionTo(symbol, StateExitHalf, 0.9)
        return
    }
    
    // 7. Check position size
    if s.getPositionSize(symbol) > s.config.MaxPositionSize {
        s.transitionTo(symbol, StateOverSize, 0.8)
        return
    }
    
    // 8. Extreme volatility
    if s.getVolatility(symbol) > s.config.ExtremeVolatilityThreshold {
        s.transitionTo(symbol, StateDefensive, 0.9)
        return
    }
}
```

**Grid Management**:
| Action | Condition | Execution |
|--------|-----------|-----------|
| Rebalance | 50% levels filled | Add opposite side |
| Intensity +20% | Strong signal (entropy < 0.3) | Increase order size |
| Intensity -30% | Weak signal (entropy > 0.7) | Decrease order size |
| Tighten spread | FVG detected | Adjust 20-30% |

**PnL Management**:
- **Micro-profit**: Take profit at grid levels
- **Reinvest**: Dùng profit để tăng grid size
- **Compound**: Scale up khi winning streak

**Exit Conditions**:
| Condition | Target State | Reason |
|-----------|--------------|--------|
| Trend mạnh | TRENDING | Better opportunity |
| Loss > max | EXIT_HALF | Risk management |
| Position too large | OVER_SIZE | Size control |
| Volatility spike | DEFENSIVE | Protection |
| Range broken | EXIT_ALL | Invalid setup |

**Risk Management**:
- **Stop Loss**: Range boundary +/- 0.5% buffer
- **Position Limit**: Max 5% per symbol
- **Drawdown Limit**: -3% → EXIT_HALF, -5% → EXIT_ALL

---

### State 5: TRENDING (Trend Following Mode)

**Mục đích**: Ride strong trend với breakout + momentum strategy

**Entry Conditions**:
- Từ IDLE: Trend Score > 0.75
- Từ WAIT_NEW_RANGE: Trend detected while waiting
- Từ TRADING: Strong trend emerges
- Từ ACCUMULATION: Breakout confirmed

**Chiến lược Trade (Hybrid)**:
```go
func (s *AdaptiveStateManager) trendingStrategy(symbol string) {
    // 1. Hybrid Trend Detection
    breakoutSignal := s.detectBreakout(symbol)
    momentumSignal := s.calculateMomentum(symbol)
    
    // 2. Hybrid Score Calculation
    hybridScore := s.calculateHybridTrendScore(breakoutSignal, momentumSignal)
    // Formula: max(breakout, momentum) * 0.6 + min(breakout, momentum) * 0.4
    
    // 3. Entry Decision
    if hybridScore > 0.7 {
        direction := s.determineTrendDirection(breakoutSignal, momentumSignal)
        
        // 4. Position Sizing
        positionSize := s.calculateTrendPositionSize(symbol, hybridScore)
        // Formula: BaseSize * hybridScore * volatilityAdjustment
        
        // 5. Entry Execution
        entryPrice := s.getOptimalEntryPrice(symbol, direction)
        s.enterTrendPosition(symbol, direction, positionSize, entryPrice)
        
        // 6. Stop Loss Placement
        if direction == Long {
            stopLoss := s.getBreakoutLevel(symbol) - s.calculateATR(symbol)*2
        } else {
            stopLoss := s.getBreakoutLevel(symbol) + s.calculateATR(symbol)*2
        }
        s.setStopLoss(symbol, stopLoss)
        
        // 7. Trailing Stop Setup
        trailingStop := s.initializeTrailingStop(symbol, direction)
        s.setTrailingStop(symbol, trailingStop)
    }
    
    // 8. Trend Management
    currentPnL := s.getUnrealizedPnL(symbol)
    
    // Micro-profit taking at FVG zones
    if fvgZones := s.detectFVGZones(symbol); len(fvgZones) > 0 {
        for _, zone := range fvgZones {
            if zone.InProfitZone(currentPrice) {
                s.takeMicroProfit(symbol, zone, 0.25) // 25% position
            }
        }
    }
    
    // 9. Trend Exhaustion Detection
    if s.isTrendExhausted(symbol) {
        // Momentum divergence + volume decrease
        s.initiateGracefulExit(symbol, StateExitAll)
        return
    }
    
    // 10. Update Trailing Stop
    s.updateTrailingStop(symbol, currentPrice)
    
    // 11. Check for Grid Opportunity
    gridScore := s.calculateGridScore(symbol)
    if gridScore > 0.7 && hybridScore < 0.5 {
        // Sideways returning, exit trend
        s.initiateGracefulExit(symbol, StateEnterGrid)
        return
    }
    
    // 12. Risk Checks
    if currentPnL < -s.config.MaxTrendLoss {
        s.transitionTo(symbol, StateExitAll, 0.95)
        return
    }
}
```

**Hybrid Trend Strategy**:
| Component | Weight | Signal |
|-----------|--------|--------|
| Breakout | 60% | Giá vượt range + volume confirm |
| Momentum | 40% | ROC + velocity + volume profile |
| **Combined** | 100% | Entry khi > 0.7 |

**Position Sizing**:
```go
positionSize = baseSize * hybridScore * (1 / volatilityFactor)
// Max: 3% account per trend trade
// Min: 1% account (khi volatility cao)
```

**Entry Points**:
1. **Breakout Entry**: Vượt range high/low với volume > 150% avg
2. **Pullback Entry**: Retest breakout level với momentum confirm
3. **Momentum Entry**: ROC > threshold + volume velocity tăng

**Stop Loss Strategy**:
- **Initial SL**: Breakout level +/- 2x ATR
- **Trailing SL**: ATR-based (2-3x), tighten khi trend mạnh
- **Breakeven**: Move to entry + 1% khi profit > 2%

**Take Profit Strategy**:
- **Micro-profit**: 25% position tại FVG zones
- **Trend ride**: 50% với trailing stop
- **Final TP**: Next major resistance/support

**Exit Conditions**:
| Condition | Target State | Reason |
|-----------|--------------|--------|
| Trend exhausted | EXIT_ALL | Momentum divergence |
| Grid better | ENTER_GRID | Sideways returning |
| Loss > max | EXIT_ALL | Risk management |
| Trailing hit | EXIT_ALL | Normal exit |

**Risk Management**:
- **Max Loss**: -3% per trend trade
- **Position Limit**: Max 3% account
- **Time Limit**: Max 4 hours in trend (avoid chop)

---

### State 6: ACCUMULATION (Pre-Breakout)

**Mục đích**: Tích lũy trước khi breakout xảy ra

**Entry Conditions**:
- Từ WAIT_NEW_RANGE: Compression detected
- Từ TRADING: Range compression trong grid

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) accumulationStrategy(symbol string) {
    // 1. Detect compression
    bbWidth := s.getBollingerBandWidth(symbol)
    atr := s.getATR(symbol)
    
    if bbWidth < 0.02 && atr < s.getAverageATR(symbol)*0.5 {
        // Strong compression detected
        s.setCompressionLevel(symbol, High)
    }
    
    // 2. Wyckoff Accumulation Detection
    if s.detectWyckoffAccumulation(symbol) {
        // Accumulate gradually
        s.accumulateGradually(symbol, 0.7)
    }
    
    // 3. Prepare for breakout
    expectedDirection := s.predictBreakoutDirection(symbol)
    // Based on: volume profile, order flow, liquidity levels
    
    // 4. Position Building
    if expectedDirection == Long {
        // Buy dips trong compression range
        s.buyDips(symbol, gridParams{
            Levels: 3,
            Spread: atr * 0.5,
            Size: baseSize * 0.3, // 30% normal size
        })
    } else {
        // Sell rallies
        s.sellRallies(symbol, gridParams{
            Levels: 3,
            Spread: atr * 0.5,
            Size: baseSize * 0.3,
        })
    }
    
    // 5. Monitor for breakout
    breakoutImminent := s.detectBreakoutImminent(symbol)
    if breakoutImminent {
        // Tighten stops, prepare to add size
        s.prepareForBreakout(symbol)
        
        // Transition to TRENDING when breakout confirmed
        if s.confirmBreakout(symbol, expectedDirection) {
            s.addToPosition(symbol, baseSize * 1.5) // Add 150% size
            s.transitionTo(symbol, StateTrending, 0.85)
            return
        }
    }
    
    // 6. False breakout / Compression continues
    if s.timeInAccumulation(symbol) > 300 { // 5 minutes
        // Back to GRID mode nếu không breakout
        s.transitionTo(symbol, StateEnterGrid, 0.6)
        return
    }
    
    // 7. Risk: Compression resolves to opposite direction
    if s.detectOppositeBreakout(symbol, expectedDirection) {
        s.exitAll(symbol)
        s.transitionTo(symbol, StateExitAll, 0.9)
        return
    }
}
```

**Accumulation Tactics**:
| Phase | Action | Size |
|-------|--------|------|
| Early compression | Small grid | 30% normal |
| Wyckoff LPS | Buy dips | 50% normal |
| Pre-breakout | Tight grid | 70% normal |
| Breakout confirm | Add 150% | Full size |

**Exit Conditions**:
| Condition | Target State | Reason |
|-----------|--------------|--------|
| Breakout confirmed | TRENDING | Trend following |
| False breakout | EXIT_ALL | Wrong direction |
| Timeout (5min) | ENTER_GRID | Back to grid |

---

### State 7: EXIT_HALF (Partial Exit)

**Mục đích**: Cắt 50% vị thế để giảm risk khi drawdown

**Entry Conditions**:
- Từ TRADING: Partial loss threshold hit (-3%)
- Từ TRENDING: Trend weakening nhưng chưa dead

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) exitHalfStrategy(symbol string) {
    // 1. Calculate optimal exit levels
    currentPositions := s.getPositions(symbol)
    halfQuantity := currentPositions.TotalQuantity / 2
    
    // 2. Exit 50% at market or limit
    exitPrice := s.calculateOptimalExitPrice(symbol)
    s.exitPosition(symbol, halfQuantity, exitPrice)
    
    // 3. Adjust stops for remaining 50%
    remainingPnL := s.getUnrealizedPnL(symbol)
    if remainingPnL < -s.config.MaxRemainingLoss {
        // Still losing → exit all
        s.transitionTo(symbol, StateExitAll, 0.9)
        return
    }
    
    // 4. Move stop to breakeven for remaining
    s.moveStopToBreakeven(symbol)
    
    // 5. Wait for recovery or further loss
    recoveryDetected := s.detectRecovery(symbol)
    if recoveryDetected {
        // Market recovering
        s.transitionTo(symbol, StateTrading, 0.7)
        return
    }
    
    // 6. Further loss
    if remainingPnL < -s.config.FullLossThreshold {
        s.transitionTo(symbol, StateExitAll, 0.95)
        return
    }
    
    // 7. Time limit
    if s.timeInExitHalf(symbol) > 180 { // 3 minutes
        s.transitionTo(symbol, StateRecovery, 0.6)
        return
    }
}
```

**Exit Execution**:
| Priority | Method | Quantity |
|----------|--------|----------|
| 1 | Market order (urgent) | 50% |
| 2 | Limit order (better price) | Remaining 50% |

**Risk Management**:
- Remaining 50% có stop loss chặt chẽ
- Max time: 3 minutes trong state này
- Nếu không recover → EXIT_ALL

**Exit Conditions**:
| Condition | Target State | Reason |
|-----------|--------------|--------|
| Recovery detected | TRADING | Back to normal |
| Further loss | EXIT_ALL | Risk limit |
| Timeout | RECOVERY | Force recovery |

---

### State 8: EXIT_ALL (Full Exit)

**Mục đích**: Đóng toàn bộ vị thế, bảo vệ capital

**Entry Conditions**:
- Từ TRADING: Full loss threshold (-5%)
- Từ TRENDING: Trend exit hoặc stop loss
- Từ EXIT_HALF: Further loss
- Từ OVER_SIZE: Emergency exit
- Từ DEFENSIVE: Volatility persists

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) exitAllStrategy(symbol string) {
    // 1. Calculate total position
    totalPosition := s.getTotalPosition(symbol)
    
    // 2. Graduated exit (tránh impact giá)
    exitPercentage := s.state.ExitPercentage // 0-1
    if exitPercentage == 0 {
        exitPercentage = 0.25 // Start with 25%
    }
    
    // 3. Exit in chunks
    chunkSize := totalPosition * exitPercentage
    s.exitPosition(symbol, chunkSize, OrderTypeMarket)
    
    // 4. Update exit percentage
    s.state.ExitPercentage += 0.25
    
    // 5. Check completion
    if s.state.ExitPercentage >= 1.0 {
        // All positions closed
        s.logExit(symbol, "COMPLETE")
        s.transitionTo(symbol, StateWaitNewRange, 1.0)
        return
    }
    
    // 6. Wait and continue exiting
    time.Sleep(2 * time.Second) // Delay between chunks
    
    // 7. Emergency: If volatility spikes during exit
    if s.getVolatility(symbol) > s.config.EmergencyThreshold {
        s.exitAllImmediately(symbol)
        s.transitionTo(symbol, StateWaitNewRange, 1.0)
        return
    }
}
```

**Exit Execution (Graduated)**:
| Step | Percentage | Delay | Method |
|------|------------|-------|--------|
| 1 | 25% | 0s | Market |
| 2 | 25% | 2s | Market/Limit |
| 3 | 25% | 2s | Limit |
| 4 | 25% | 2s | Limit |

**Emergency Exit**:
- Nếu volatility > 500% ATR
- Exit 100% immediately (market orders)
- Skip graduated approach

**Exit Conditions**:
| Condition | Target State | Reason |
|-----------|--------------|--------|
| All closed | WAIT_NEW_RANGE | Ready for next |
| Emergency | WAIT_NEW_RANGE | Volatility spike |

---

### State 9: OVER_SIZE (Position Size Limit)

**Mục đích**: Giảm position size khi vượt quá limit

**Entry Conditions**:
- Từ TRADING: Position > max limit (5% account)

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) overSizeStrategy(symbol string) {
    currentSize := s.getPositionSize(symbol)
    maxSize := s.config.MaxPositionSize
    
    // 1. Calculate excess
    excess := currentSize - maxSize
    reductionRatio := excess / currentSize // e.g., 0.3 = reduce 30%
    
    // 2. Gradual reduction
    targetSize := currentSize * (1 - reductionRatio*0.5) // Reduce 50% of excess
    
    // 3. Close excess positions
    s.reducePosition(symbol, targetSize)
    
    // 4. Check if normalized
    newSize := s.getPositionSize(symbol)
    if newSize <= maxSize * 1.05 { // 5% tolerance
        s.transitionTo(symbol, StateTrading, 0.8)
        return
    }
    
    // 5. Risk: Can't reduce (no liquidity)
    if s.timeInOverSize(symbol) > 60 { // 1 minute
        s.transitionTo(symbol, StateExitAll, 0.9)
        return
    }
}
```

**Size Reduction**:
| Current | Target | Method | Time |
|---------|--------|--------|------|
| 6% | 5% | Close 17% | Gradual |
| 7% | 5% | Close 29% | Gradual |
| 8%+ | 0% | Emergency | Immediate |

**Exit Conditions**:
| Condition | Target State | Reason |
|-----------|--------------|--------|
| Size normalized | TRADING | Back to normal |
| Can't reduce | EXIT_ALL | Emergency |

---

### State 10: DEFENSIVE (Extreme Volatility)

**Mục đích**: Bảo vệ capital khi volatility cực cao

**Entry Conditions**:
- Từ TRADING: Volatility > 300% ATR
- Từ TRENDING: Flash crash/spike detected

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) defensiveStrategy(symbol string) {
    volatility := s.getVolatility(symbol)
    
    // 1. Immediate position reduction
    if s.hasOpenPositions(symbol) {
        // Close 70% immediately
        s.exitPosition(symbol, s.getPositionSize(symbol)*0.7, OrderTypeMarket)
    }
    
    // 2. Stop all new orders
    s.pauseNewOrders(symbol, 60*time.Second)
    
    // 3. Widen stops for remaining positions
    s.widenStops(symbol, 3.0) // 3x normal ATR
    
    // 4. Monitor for normalization
    if volatility < s.getAverageVolatility(symbol)*1.5 {
        // Volatility normalizing
        if s.hasOpenPositions(symbol) {
            s.transitionTo(symbol, StateTrading, 0.7)
        } else {
            s.transitionTo(symbol, StateWaitNewRange, 0.7)
        }
        return
    }
    
    // 5. Extreme persists
    if s.timeInDefensive(symbol) > 120 { // 2 minutes
        // Exit all remaining
        if s.hasOpenPositions(symbol) {
            s.transitionTo(symbol, StateExitAll, 0.9)
        } else {
            s.transitionTo(symbol, StateWaitNewRange, 0.8)
        }
        return
    }
    
    // 6. Flash crash protection
    if s.detectFlashCrash(symbol) {
        s.exitAllImmediately(symbol)
        s.transitionTo(symbol, StateWaitNewRange, 1.0)
        return
    }
}
```

**Defensive Actions**:
| Action | Trigger | Execution |
|--------|---------|-----------|
| Reduce 70% | Vol > 300% ATR | Immediate |
| Pause orders | Flash crash | 60s timeout |
| Widen stops | High volatility | 3x ATR |
| Full exit | Persist > 2min | Emergency |

**Exit Conditions**:
| Condition | Target State | Reason |
|-----------|--------------|--------|
| Volatility normal | TRADING | Back to normal |
| Timeout | EXIT_ALL/WAIT | Force exit |
| Flash crash | EXIT_ALL | Emergency |

---

### State 11: RECOVERY (After Losses)

**Mục đích**: Phục hồi sau drawdown, rebuild confidence

**Entry Conditions**:
- Từ EXIT_HALF: Timeout
- Từ EXIT_ALL: Multiple consecutive losses

**Chiến lược Trade**:
```go
func (s *AdaptiveStateManager) recoveryStrategy(symbol string) {
    // 1. Analyze what went wrong
    lossAnalysis := s.analyzeLoss(symbol)
    
    // 2. Adjust parameters based on analysis
    switch lossAnalysis.Reason {
    case "OversizedPosition":
        s.config.MaxPositionSize *= 0.8 // Reduce 20%
    case "WrongTrendDirection":
        s.trendDetectionThreshold += 0.1 // Stricter
    case "IgnoredStopLoss":
        s.enforceHardStops = true
    }
    
    // 3. Paper trade / Small size testing
    testSize := s.config.BaseSize * 0.2 // 20% normal
    
    // 4. Run small test trades
    if s.consecutiveLosses < 3 {
        // Light recovery
        s.transitionTo(symbol, StateEnterGrid, 0.5)
    } else {
        // Deep recovery
        s.transitionTo(symbol, StateIdle, 0.5)
        s.incrementRecoveryLevel(symbol)
    }
    
    // 5. Recovery complete check
    if s.getRecentPnL(symbol, 5) > 0 { // Profitable last 5 trades
        s.resetRecoveryLevel(symbol)
        s.transitionTo(symbol, StateTrading, 0.8)
    }
}
```

**Recovery Levels**:
| Level | Consecutive Losses | Action |
|-------|-------------------|--------|
| 1 | 1-2 | Reduce size 20% |
| 2 | 3-4 | Reduce size 50%, stricter entry |
| 3 | 5+ | Back to IDLE, manual review |

**Exit Conditions**:
| Condition | Target State | Reason |
|-----------|--------------|--------|
| Light loss | ENTER_GRID | Resume trading |
| Deep loss | IDLE | Pause & review |
| Recovery success | TRADING | Normal mode |

---

## Transition Rules & Score Calculation

### Score Formulas

```go
// GRID Mode Score
func (s *AdaptiveStateManager) calculateGridScore(symbol string) float64 {
    sidewaysConfidence := s.regimeDetector.GetSidewaysConfidence(symbol)
    meanReversionSignal := s.getMeanReversionSignalStrength(symbol)
    fvgSignal := s.getFVGSignalStrength(symbol)
    liquiditySignal := s.getLiquiditySignalStrength(symbol)
    
    // Components
    regimeComponent := sidewaysConfidence * 0.40
    signalComponent := (meanReversionSignal*0.3 + fvgSignal*0.4 + liquiditySignal*0.3) * 0.60
    
    // Historical performance weighting
    historicalWeight := s.getHistoricalGridPerformance(symbol) // 0.8-1.2
    
    score := (regimeComponent + signalComponent) * historicalWeight
    return clamp(score, 0, 1)
}

// TREND Mode Score
func (s *AdaptiveStateManager) calculateTrendScore(symbol string) float64 {
    trendingConfidence := s.regimeDetector.GetTrendingConfidence(symbol)
    breakoutSignal := s.getBreakoutSignalStrength(symbol)
    momentumSignal := s.getMomentumSignalStrength(symbol)
    volumeConfirm := s.getVolumeConfirmation(symbol)
    
    // Hybrid formula
    trendComponent := trendingConfidence * 0.30
    signalComponent := (breakoutSignal + momentumSignal) * 0.35
    volumeComponent := volumeConfirm * 0.35
    
    score := trendComponent + signalComponent + volumeComponent
    return clamp(score, 0, 1)
}

// Hybrid Trend Score
func (s *AdaptiveStateManager) calculateHybridTrendScore(
    breakout, momentum float64,
) float64 {
    maxSignal := math.Max(breakout, momentum)
    minSignal := math.Min(breakout, momentum)
    
    // Strong agreement bonus
    agreementBonus := 1.0
    if math.Abs(breakout-momentum) < 0.2 {
        agreementBonus = 1.15 // 15% bonus khi 2 signals agree
    }
    
    hybrid := (maxSignal*0.6 + minSignal*0.4) * agreementBonus
    return clamp(hybrid, 0, 1)
}
```

### Transition Decision Matrix

| Current State | Grid Score | Trend Score | Decision | Target State |
|---------------|------------|-------------|----------|--------------|
| IDLE | > 0.6 | < 0.6 | Grid better | WAIT_NEW_RANGE |
| IDLE | < 0.6 | > 0.75 | Trend strong | TRENDING |
| IDLE | > 0.6 | > 0.75 | Compare | Higher score wins |
| TRADING | > 0.5 | > 0.8 | Trend emerges | TRENDING (graceful) |
| TRENDING | > 0.7 | < 0.5 | Sideways returns | ENTER_GRID |
| Any | Any | Any | Risk trigger | EXIT_* states |

### Smoothing & Hysteresis

```go
// Hysteresis để tránh flip-flop
func (s *AdaptiveStateManager) shouldTransition(
    currentState, targetState GridState,
    currentScore, newScore float64,
) bool {
    // Same state → no transition
    if currentState == targetState {
        return false
    }
    
    // Hysteresis buffer
    var threshold float64
    switch targetState {
    case StateTrading:
        threshold = 0.6
    case StateTrending:
        threshold = 0.75
    case StateExitHalf:
        threshold = 0.9 // High confidence for exit
    case StateExitAll:
        threshold = 0.95 // Very high for full exit
    }
    
    // Check if score exceeds threshold + hysteresis
    if currentState == StateTrading && targetState == StateTrending {
        // Need 0.1 buffer to switch from grid to trend
        return newScore > threshold+0.1
    }
    
    if currentState == StateTrending && targetState == StateTrading {
        // Need 0.15 buffer to switch back (trend has inertia)
        return newScore > threshold+0.15
    }
    
    return newScore > threshold
}

// Smooth transition (blend weights)
func (s *AdaptiveStateManager) executeSmoothTransition(
    symbol string,
    fromState, toState GridState,
    duration time.Duration,
) {
    steps := 10
    stepDuration := duration / time.Duration(steps)
    
    for i := 0; i <= steps; i++ {
        weight := float64(i) / float64(steps)
        s.setStateBlend(symbol, fromState, toState, weight)
        time.Sleep(stepDuration)
    }
    
    s.setFinalState(symbol, toState)
}
```

---

## Implementation Order

### Phase 1: Core State Machine (3 days)
1. Refactor state_machine.go với flexible transitions
2. Implement AdaptiveStateManager
3. Implement Score Calculation Engine
4. Implement DecisionEngine (single point of decision)

### Phase 2: GRID States (2 days)
1. WAIT_NEW_RANGE với range detection
2. ENTER_GRID với signal-triggered entry
3. TRADING với continuous blending
4. EXIT_HALF & EXIT_ALL với graduated exit

### Phase 3: TREND States (2 days)
1. TRENDING với hybrid strategy
2. ACCUMULATION với pre-breakout logic
3. Integration với trailing stops

### Phase 4: Risk States (2 days)
1. OVER_SIZE với size management
2. DEFENSIVE với volatility protection
3. RECOVERY với drawdown analysis

### Phase 5: Polish (2 days)
1. Smooth transitions (5-10s blend)
2. Comprehensive testing
3. Performance optimization

---

## Risk Management Summary

| State | Max Loss | Max Size | Max Time | Emergency Action |
|-------|----------|----------|----------|------------------|
| IDLE | 0% | 0% | 300s | N/A |
| WAIT_NEW_RANGE | 0% | 0% | 120s | → DEFENSIVE |
| ENTER_GRID | 0% | 0% | 60s | Cancel all |
| TRADING | -3% → EXIT_HALF | 5% | ∞ | → DEFENSIVE |
| TRENDING | -3% → EXIT_ALL | 3% | 4h | Trailing stop |
| ACCUMULATION | -2% | 2% | 5min | → EXIT_ALL |
| EXIT_HALF | -2% (remaining) | 50% of original | 3min | → EXIT_ALL |
| EXIT_ALL | N/A | 0% | Graduated 10s | Immediate if needed |
| OVER_SIZE | -1% | Reduce to 5% | 1min | → EXIT_ALL |
| DEFENSIVE | -1% | 30% of original | 2min | → EXIT_ALL |
| RECOVERY | -0.5% | 20% of normal | ∞ | → IDLE |

---

## Success Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| State Transition Latency | < 100ms | Time from signal to state change |
| Correct State Selection | > 80% | Backtest accuracy |
| Flip-flop Rate | < 3/hour | Số lần đổi state không cần thiết |
| Recovery Speed | < 5 trades | Số trade để recover từ drawdown |
| Emergency Exit Success | 100% | Thoát kịp trong extreme events |
