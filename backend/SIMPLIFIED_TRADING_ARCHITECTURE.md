# Simplified Trading Architecture Proposal

**Date**: 2026-04-15
**Purpose**: Simplify trading logic with single source of truth

---

## Current Architecture (Complex)

```
Order placement flow:
enqueuePlacement → canPlaceForSymbol?
  → AdaptiveGridManager.CanPlaceOrder? (7 checks)
    → tradingPaused?
    → transitionCooldown?
    → Position limit?
    → RiskMonitor exposure?
    → CircuitBreaker?
    → RangeDetector?
    → TimeFilter?
    → SpreadProtection?
    → InventoryManager?
    → TrendDetector?
    → FundingRate?
  → StateMachine.ShouldEnqueuePlacement?
  → processPlacement
  → placeOrder (exposure check again)
```

**Problems**:
- Too many blocking gates (10+ checks)
- No single source of truth
- Decision logic scattered across multiple components
- Hard to debug why bot stopped trading
- Redundant checks (exposure checked 2 times)

---

## Proposed Architecture (Simple)

```
┌─────────────────────────────────────────┐
│  TradingDecisionWorker (Đầu não)         │
│  - Run every 5 seconds                   │
│  - For each symbol:                       │
│    1. Check market conditions            │
│    2. Check circuit breaker              │
│    3. Check range state                  │
│    4. Check trend state                  │
│    5. Check trading hours                │
│    6. Update canTrade flag               │
│    7. Update tradingMode                 │
└────────────────┬────────────────────────┘
                 │
                 │ Write to shared state
                 ↓
┌─────────────────────────────────────────┐
│  TradingState (Single Source of Truth)  │
│  canTrade[symbol] = bool                │
│  tradingMode[symbol] = string            │
│  lastDecisionTime[symbol] = time.Time   │
│  decisionReason[symbol] = string         │
└────────────────┬────────────────────────┘
                 │
                 │ Read from shared state
                 ↓
┌─────────────────────────────────────────┐
│  Placement Workers                      │
│  - Check canTrade[symbol]               │
│  - Get tradingMode[symbol]              │
│  - Place orders if canTrade = true      │
│  - NO decision logic, just execution    │
└─────────────────────────────────────────┘
```

---

## Component: TradingDecisionWorker

```go
type TradingDecisionWorker struct {
    tradingState map[string]*SymbolTradingState
    circuitBreaker *agentic.CircuitBreaker
    rangeDetectors map[string]*RangeDetector
    trendDetectors map[string]*TrendDetector
    timeFilter *TimeFilter
    logger *zap.Logger
    mu sync.RWMutex
    stopCh chan struct{}
}

type SymbolTradingState struct {
    CanTrade bool
    TradingMode string  // MICRO, STANDARD, TREND_ADAPTED, COOLDOWN
    LastDecisionTime time.Time
    DecisionReason string  // Why canTrade is true/false
}

func (w *TradingDecisionWorker) Run(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
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
    symbols := w.getSymbols()

    for _, symbol := range symbols {
        decision := w.evaluateSymbol(symbol)
        w.updateTradingState(symbol, decision)
    }
}

func (w *TradingDecisionWorker) evaluateSymbol(symbol string) TradingDecision {
    // Check 1: CircuitBreaker
    canTrade, mode := w.circuitBreaker.GetSymbolDecision(symbol)
    if !canTrade {
        return TradingDecision{
            CanTrade: false,
            TradingMode: mode,
            Reason: "CircuitBreaker tripped or in COOLDOWN",
        }
    }

    // Check 2: RangeDetector
    if detector, exists := w.rangeDetectors[symbol]; exists {
        if !detector.ShouldTrade() {
            return TradingDecision{
                CanTrade: false,
                TradingMode: mode,
                Reason: "Range not active",
            }
        }
    }

    // Check 3: TrendDetector
    if detector, exists := w.trendDetectors[symbol]; exists {
        score := detector.GetTrendScore()
        if score >= 4 {
            return TradingDecision{
                CanTrade: false,
                TradingMode: mode,
                Reason: "Strong trend detected",
            }
        }
    }

    // Check 4: TimeFilter
    if w.timeFilter != nil && !w.timeFilter.CanTrade() {
        return TradingDecision{
            CanTrade: false,
            TradingMode: mode,
            Reason: "Outside trading hours",
        }
    }

    // All checks passed
    return TradingDecision{
        CanTrade: true,
        TradingMode: mode,
        Reason: "All checks passed",
    }
}

func (w *TradingDecisionWorker) updateTradingState(symbol string, decision TradingDecision) {
    w.mu.Lock()
    defer w.mu.Unlock()

    state, exists := w.tradingState[symbol]
    if !exists {
        state = &SymbolTradingState{}
        w.tradingState[symbol] = state
    }

    // Only log if state changed
    if state.CanTrade != decision.CanTrade || state.TradingMode != decision.TradingMode {
        w.logger.Info("Trading decision updated",
            zap.String("symbol", symbol),
            zap.Bool("can_trade", decision.CanTrade),
            zap.String("mode", decision.TradingMode),
            zap.String("reason", decision.Reason))
    }

    state.CanTrade = decision.CanTrade
    state.TradingMode = decision.TradingMode
    state.LastDecisionTime = time.Now()
    state.DecisionReason = decision.Reason
}

func (w *TradingDecisionWorker) GetTradingState(symbol string) *SymbolTradingState {
    w.mu.RLock()
    defer w.mu.RUnlock()
    return w.tradingState[symbol]
}
```

---

## Simplified CanPlaceOrder

```go
func (a *AdaptiveGridManager) CanPlaceOrder(symbol string) bool {
    // Read from single source of truth
    state := a.tradingDecisionWorker.GetTradingState(symbol)
    if state == nil {
        return false  // No decision yet
    }

    return state.CanTrade
}
```

---

## Benefits

1. **Single source of truth** - All decisions in one place
2. **Easy to debug** - Check TradingState to see why blocked
3. **Clear separation** - Decision vs Execution
4. **Simplified flow** - Placement workers just read flag
5. **Consistent updates** - Decision worker runs every 5s
6. **No redundant checks** - Decision logic centralized

---

## Implementation Steps

1. Create `TradingDecisionWorker` component
2. Add `TradingState` struct
3. Move all decision logic to TradingDecisionWorker
4. Simplify `CanPlaceOrder` to just read state
5. Remove redundant checks from other components
6. Add logging for state changes
7. Add API endpoint to query trading state

---

## Migration Path

**Phase 1**: Create new component
- Implement TradingDecisionWorker
- Add to VolumeFarmEngine
- Run in parallel with old logic

**Phase 2**: Test and verify
- Compare decisions between old and new
- Verify state changes logged correctly
- Check API endpoint

**Phase 3**: Switch to new logic
- Replace CanPlaceOrder with new implementation
- Remove old decision logic
- Monitor for issues

---

## API Endpoint for Debugging

```
GET /api/trading-state
Response:
{
  "symbols": {
    "BTCUSD1": {
      "can_trade": true,
      "trading_mode": "STANDARD",
      "last_decision_time": "2026-04-15T17:50:00Z",
      "decision_reason": "All checks passed"
    },
    "ETHUSD1": {
      "can_trade": false,
      "trading_mode": "COOLDOWN",
      "last_decision_time": "2026-04-15T17:48:00Z",
      "decision_reason": "CircuitBreaker tripped"
    }
  }
}
```

---

## Conclusion

This architecture provides:
- Single source of truth for trading decisions
- Clear separation of concerns
- Easy debugging and monitoring
- Simplified placement logic
- Consistent decision updates

**Status**: Ready for implementation
