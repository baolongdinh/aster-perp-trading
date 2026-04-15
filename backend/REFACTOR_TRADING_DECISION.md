# Refactor Trading Decision - Keep Logic, Centralize Decision

**Date**: 2026-04-15
**Purpose**: Centralize trading decision logic while keeping existing checks

---

## Current Problem

Logic check hiện tại rải rác khắp nơi, không có single source of truth:
- AdaptiveGridManager.CanPlaceOrder (7 checks)
- GridManager.canPlaceForSymbol (2 checks)
- StateMachine.ShouldEnqueuePlacement (1 check)
- Không biết bot bị block bởi gate nào

---

## Solution: Centralize Decision, Keep Logic

Tạo `TradingDecisionWorker` làm "đầu não", nhưng **giữ nguyên tất cả logic check hiện tại**.

```
┌─────────────────────────────────────────┐
│  TradingDecisionWorker (Đầu não)         │
│  - Run every 5 seconds                   │
│  - GIỮ NGUYÊN logic check hiện tại:       │
│    1. tradingPaused                      │
│    2. Position limit (1.2x hard cap)     │
│    3. CircuitBreaker                     │
│    4. RangeDetector                      │
│    5. TrendDetector                      │
│    6. TimeFilter                         │
│    7. FundingRate                        │
│  - Set canTrade flag                    │
│  - Set tradingMode                       │
└────────────────┬────────────────────────┘
                 │
                 │ Update canTrade flag
                 ↓
┌─────────────────────────────────────────┐
│  TradingState (Single Source of Truth)  │
│  canTrade[symbol] = bool                │
│  tradingMode[symbol] = string            │
│  lastDecisionTime[symbol] = time.Time   │
│  decisionReason[symbol] = string         │
└────────────────┬────────────────────────┘
                 │
                 │ Read flag (simplified)
                 ↓
┌─────────────────────────────────────────┐
│  AdaptiveGridManager.CanPlaceOrder       │
│  - CHỈ đọc canTrade flag                │
│  - KHÔNG check logic nữa                 │
│  - Return canTrade flag                 │
└─────────────────────────────────────────┘
```

---

## Implementation

### 1. TradingDecisionWorker Component

```go
type TradingDecisionWorker struct {
    tradingState map[string]*SymbolTradingState
    adaptiveMgr *AdaptiveGridManager  // Use existing logic
    logger *zap.Logger
    mu sync.RWMutex
    stopCh chan struct{}
    interval time.Duration
}

type SymbolTradingState struct {
    CanTrade bool
    TradingMode string
    LastDecisionTime time.Time
    DecisionReason string
}

func NewTradingDecisionWorker(adaptiveMgr *AdaptiveGridManager, logger *zap.Logger) *TradingDecisionWorker {
    return &TradingDecisionWorker{
        tradingState: make(map[string]*SymbolTradingState),
        adaptiveMgr: adaptiveMgr,
        logger: logger,
        interval: 5 * time.Second,  // Check every 5s
    }
}

func (w *TradingDecisionWorker) Run(ctx context.Context) {
    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.evaluateAllSymbols()
        }
    }
}

func (w *TradingDecisionWorker) evaluateAllSymbols() {
    symbols := w.getSymbols()  // Get active symbols from adaptiveMgr

    for _, symbol := range symbols {
        // GIỮ NGUYÊN logic check hiện tại
        canTrade, mode, reason := w.evaluateSymbol(symbol)
        w.updateTradingState(symbol, canTrade, mode, reason)
    }
}

func (w *TradingDecisionWorker) evaluateSymbol(symbol string) (bool, string, string) {
    // GIỮ NGUYÊN logic từ AdaptiveGridManager.CanPlaceOrder
    // Copy existing checks exactly as-is

    // Check 1: tradingPaused
    if w.adaptiveMgr.IsTradingPaused(symbol) {
        return false, "", "trading paused"
    }

    // Check 2: Position limit (1.2x hard cap)
    position, exists := w.adaptiveMgr.positions[symbol]
    if exists && position.NotionalValue >= w.adaptiveMgr.riskConfig.MaxPositionUSDT*1.2 {
        return false, "", "hard position cap exceeded"
    }

    // Check 3: CircuitBreaker
    if w.adaptiveMgr.circuitBreaker != nil {
        if cb, ok := w.adaptiveMgr.circuitBreaker.(interface {
            GetSymbolDecision(symbol string) (canTrade bool, tradingMode string)
        }); ok {
            canTrade, mode := cb.GetSymbolDecision(symbol)
            if !canTrade {
                return false, mode, "circuit breaker tripped"
            }
            return true, mode, "all checks passed"
        }
    }

    // Check 4: RangeDetector
    if detector, exists := w.adaptiveMgr.rangeDetectors[symbol]; exists {
        if !detector.ShouldTrade() {
            return false, "", "range not active"
        }
    }

    // Check 5: TrendDetector
    if w.adaptiveMgr.trendDetector != nil {
        score := w.adaptiveMgr.trendDetector.GetTrendScore()
        if score >= 4 {
            return false, "", "strong trend detected"
        }
    }

    // Check 6: TimeFilter
    if w.adaptiveMgr.timeFilter != nil && !w.adaptiveMgr.timeFilter.CanTrade() {
        return false, "", "outside trading hours"
    }

    // Check 7: FundingRate
    if w.adaptiveMgr.fundingMonitor != nil {
        if _, _, shouldSkip := w.adaptiveMgr.fundingMonitor.GetFundingBias(symbol); shouldSkip {
            return false, "", "extreme funding rate"
        }
    }

    return true, "", "all checks passed"
}

func (w *TradingDecisionWorker) updateTradingState(symbol string, canTrade bool, mode string, reason string) {
    w.mu.Lock()
    defer w.mu.Unlock()

    state, exists := w.tradingState[symbol]
    if !exists {
        state = &SymbolTradingState{}
        w.tradingState[symbol] = state
    }

    // Log if state changed
    if state.CanTrade != canTrade {
        w.logger.Info("Trading decision updated",
            zap.String("symbol", symbol),
            zap.Bool("can_trade", canTrade),
            zap.String("mode", mode),
            zap.String("reason", reason))
    }

    state.CanTrade = canTrade
    state.TradingMode = mode
    state.LastDecisionTime = time.Now()
    state.DecisionReason = reason
}

func (w *TradingDecisionWorker) GetTradingState(symbol string) *SymbolTradingState {
    w.mu.RLock()
    defer w.mu.RUnlock()
    return w.tradingState[symbol]
}

func (w *TradingDecisionWorker) ForceUpdate(symbol string, canTrade bool, reason string) {
    w.mu.Lock()
    defer w.mu.Unlock()

    state, exists := w.tradingState[symbol]
    if !exists {
        state = &SymbolTradingState{}
        w.tradingState[symbol] = state
    }

    state.CanTrade = canTrade
    state.LastDecisionTime = time.Now()
    state.DecisionReason = reason

    w.logger.Info("Trading decision force updated",
        zap.String("symbol", symbol),
        zap.Bool("can_trade", canTrade),
        zap.String("reason", reason))
}
```

---

### 2. Simplified CanPlaceOrder

```go
func (a *AdaptiveGridManager) CanPlaceOrder(symbol string) bool {
    // CHỈ đọc từ TradingDecisionWorker
    // KHÔNG check logic nữa
    if a.tradingDecisionWorker != nil {
        state := a.tradingDecisionWorker.GetTradingState(symbol)
        if state == nil {
            return false  // No decision yet
        }
        return state.CanTrade
    }

    // Fallback to old logic if worker not available
    a.logger.Warn("TradingDecisionWorker not available, using fallback logic")
    return a.canPlaceOrderFallback(symbol)
}
```

---

### 3. Integration with VolumeFarmEngine

```go
// In NewVolumeFarmEngine
tradingDecisionWorker := NewTradingDecisionWorker(adaptiveGridManager, logger)
adaptiveGridManager.SetTradingDecisionWorker(tradingDecisionWorker)

// Start worker
go tradingDecisionWorker.Run(ctx)
```

---

### 4. Event Handlers (CircuitBreaker, RangeDetector)

```go
// When CircuitBreaker trips
func (a *AdaptiveGridManager) OnCircuitBreakerTrip(symbol string, reason string) {
    if a.tradingDecisionWorker != nil {
        a.tradingDecisionWorker.ForceUpdate(symbol, false, "circuit breaker trip: "+reason)
        // Then trigger exit
        a.ExitAll(context.Background(), symbol, EventEmergencyExit, reason)
    }
}

// When RangeDetector detects breakout
func (a *AdaptiveGridManager) OnRangeBreakout(symbol string) {
    if a.tradingDecisionWorker != nil {
        a.tradingDecisionWorker.ForceUpdate(symbol, false, "range breakout")
        // Then trigger exit
        a.ExitAll(context.Background(), symbol, EventTrendExit, "range breakout")
    }
}

// When positions closed and ready to re-enter
func (a *AdaptiveGridManager) OnReadyToReenter(symbol string) {
    if a.tradingDecisionWorker != nil {
        // Worker will auto-evaluate and set canTrade = true on next cycle
        // Or force immediate evaluation
        a.tradingDecisionWorker.evaluateSymbol(symbol)
    }
}
```

---

## Benefits

1. **Single source of truth** - TradingDecisionWorker duy nhất quyết định
2. **Giữ nguyên logic** - Không thay đổi cách check, chỉ tổ chức lại
3. **Easy to debug** - Check TradingState để xem tại sao bị block
4. **Clear separation** - Decision vs Execution
5. **Consistent updates** - Worker chạy mỗi 5s, auto-recover
6. **API endpoint** - Có thể query state để debug

---

## Migration Path

**Phase 1**: Add TradingDecisionWorker
- Implement worker with existing logic
- Add to VolumeFarmEngine
- Run in parallel with old logic
- Log both decisions for comparison

**Phase 2**: Switch to new logic
- Change CanPlaceOrder to read from worker
- Keep old logic as fallback
- Monitor for issues

**Phase 3**: Remove old logic
- Remove checks from CanPlaceOrder
- Clean up fallback
- Verify all tests pass

---

## API Endpoint for Debugging

```
GET /api/trading-state?symbol=BTCUSD1
Response:
{
  "symbol": "BTCUSD1",
  "can_trade": false,
  "trading_mode": "COOLDOWN",
  "last_decision_time": "2026-04-15T17:50:00Z",
  "decision_reason": "circuit breaker trip: volatility spike"
}
```

---

## Summary

**Giữ nguyên**: Tất cả logic check hiện tại
**Thay đổi**: Tập trung vào TradingDecisionWorker duy nhất
**Kết quả**: Single source of truth, dễ debug, auto-recover

**Status**: Ready for implementation
