# Implementation Plan: Continuous Volume Farming with Micro-Profit & Adaptive Risk Control

## Technical Context

### Current Architecture (Existing)
- **Language**: Go 1.21+
- **Key Components**:
  - `cmd/agentic/main.go` - Entry point (Agentic + VF unified)
  - `internal/farming/volume_farm_engine.go` - VF execution layer
  - `internal/farming/grid_manager.go` - Grid order placement
  - `internal/farming/adaptive_grid/manager.go` - Adaptive grid logic
  - `internal/farming/adaptive_grid/range_detector.go` - BB/ATR range detection
  - `internal/agentic/` - Agentic decision layer (scoring, regime detection)
  - `internal/client/websocket.go` - WebSocket client
  - `internal/config/` - Configuration types

### Existing Data Flow
```
WebSocket Klines → RangeDetector → GridManager → Order Placement
                        ↓
                   RangeState (Active/Establishing/Breakout)
                        ↓
              Gate: Only Active → Allow Orders
```

### Problem Identified
- **Blocking Gate**: `CanPlaceOrder()` returns false when `RangeState != Active`
- **Dead Time**: No trading during warm-up (10-20 candles needed)
- **Missed Volume**: Opportunities lost while waiting for BB establishment

---

## Architecture Overview (New)

### Trading Mode State Machine
```
┌─────────────┐     ADX<20 &        ┌─────────────┐
│   START     │ ──Price stable────> │    MICRO    │
│   (Init)    │                     │    MODE     │
└─────────────┘                     │  (40% size) │
       │                            └──────┬──────┘
       │                                   │
       │                            BB Active
       │                                   ↓
       │                            ┌─────────────┐
       │         ┌─────────────────│  STANDARD   │
       │         │   BB Inactive   │    MODE     │
       │         │   (Cooldown)    │  (100% size)│
       │         └────────────────>└──────┬──────┘
       │                                  │
       │                          ADX>25 (Trend)
       │                                  ↓
       │                            ┌─────────────┐
       │         ┌─────────────────│   TREND     │
       │         │    ADX<20       │   ADAPTED   │
       └─────────┤   (Sideways)    │  (30% size) │
                 │                 └──────┬──────┘
                 │                        │
           Breakout/Volatility            │
                 │                        │
                 └────────────────>┌─────────────┐
                                  │  COOLDOWN   │
                                  │   (60s)     │
                                  └─────────────┘
```

### Component Changes

| Component | Current Behavior | New Behavior |
|-----------|------------------|--------------|
| `RangeDetector` | Blocks on `!= Active` | Provides ATR bands for MICRO mode |
| `AdaptiveGridManager.CanPlaceOrder()` | Returns false if not Active | Returns true with adjusted params per mode |
| `GridManager` | Single grid params | Dynamic params based on TradingMode |
| `AgenticEngine` | Whitelist only | Regime detection + mode signaling |

---

## Data Model

### New Types

```go
// TradingMode represents the current trading mode
type TradingMode int

const (
    TradingModeUnknown TradingMode = iota
    TradingModeMicro        // 40% size, ATR bands, 2-3 levels
    TradingModeStandard     // 100% size, BB bands, 5 levels
    TradingModeTrendAdapted // 30% size, BB bands, 1-2 levels, trend bias
    TradingModeCooldown     // No orders, 60s wait
)

// DynamicParameters holds mode-specific settings
type DynamicParameters struct {
    SizeMultiplier     float64 // 0.3, 0.4, 1.0
    SpreadMultiplier   float64 // 1.0, 2.0, 3.0
    LevelCount         int     // 1-2, 2-3, 5
    UseBBBands         bool    // false for MICRO, true for others
    TrendBiasEnabled   bool    // true for TREND_ADAPTED
    CooldownDuration   time.Duration
}

// ModeConfig from YAML
type ModeConfig struct {
    MicroMode        MicroModeConfig        `yaml:"micro_mode"`
    StandardMode     StandardModeConfig     `yaml:"standard_mode"`
    TrendAdaptedMode TrendAdaptedModeConfig `yaml:"trend_adapted_mode"`
    CooldownMode     CooldownModeConfig     `yaml:"cooldown_mode"`
}

type MicroModeConfig struct {
    Enabled           bool    `yaml:"enabled"`
    SizeMultiplier    float64 `yaml:"size_multiplier"`    // default 0.4
    LevelCount        int     `yaml:"level_count"`        // default 3
    SpreadMultiplier  float64 `yaml:"spread_multiplier"`  // default 2.0
    MinATRMultiplier  float64 `yaml:"min_atr_multiplier"` // default 1.5
}
```

### State Transitions

```go
// ModeTransition defines when to switch modes
type ModeTransition struct {
    From TradingMode
    To   TradingMode
    Condition ModeTransitionCondition
}

type ModeTransitionCondition struct {
    ADXMin              float64 // e.g., 25 for trend
    ADXMax              float64 // e.g., 20 for sideways
    RangeState          RangeState
    IsBreakout          bool
    IsVolatilitySpike   bool
    CooldownExpired     bool
}
```

---

## Implementation Phases

### Phase 1: Core Trading Mode Framework (Foundation)
**Duration**: 2-3 days
**Files to Create/Modify**:

1. **Create** `internal/farming/trading_mode.go`
   - Define `TradingMode` enum
   - Define `DynamicParameters` struct
   - Implement `GetParametersForMode()` function
   - Implement mode transition logic

2. **Create** `internal/farming/mode_manager.go`
   - `ModeManager` struct to track current mode
   - `EvaluateMode()` - determine mode from market conditions
   - `ShouldTransition()` - check transition conditions
   - Thread-safe mode switching

3. **Modify** `internal/config/config.go`
   - Add `ModeConfig` to `VolumeFarmConfig`
   - Add mode transition thresholds

**Acceptance Criteria**:
- [ ] Can create ModeManager with config
- [ ] Can evaluate mode from ADX + RangeState
- [ ] Mode transitions logged with reason
- [ ] Thread-safe mode access

---

### Phase 2: Bypass Range Gate for MICRO Mode (Critical Fix)
**Duration**: 2-3 days
**Files to Modify**:

1. **Modify** `internal/farming/adaptive_grid/manager.go`
   - Change `CanPlaceOrder()` to allow MICRO mode
   - Add `GetATRBands()` method for MICRO mode grid calc
   - Modify grid building to use ATR when BB not ready

2. **Modify** `internal/farming/adaptive_grid/range_detector.go`
   - Add `GetATRBands()` method
   - Expose ATR-based upper/lower bounds
   - Cache ATR calculation

3. **Modify** `internal/farming/grid_manager.go`
   - Change `canPlaceForSymbol()` logic:
     ```go
     if rangeState == RangeStateActive {
         // Use BB bands, standard params
     } else if mode == TradingModeMicro {
         // Use ATR bands, reduced params
     } else {
         // Block
     }
     ```
   - Pass TradingMode to order building

4. **Modify** `internal/farming/grid_builder.go` (or equivalent)
   - Support dynamic grid params based on mode
   - Adjust level count, spread, size per mode

**Acceptance Criteria**:
- [ ] Orders placed immediately on startup (MICRO mode)
- [ ] Grid uses ATR bands when BB not ready
- [ ] Size reduced to 40% in MICRO mode
- [ ] Seamless transition to STANDARD when BB active

---

### Phase 3: Regime-Based Parameter Adjustment
**Duration**: 2 days
**Files to Modify**:

1. **Modify** `internal/agentic/regime_detector.go`
   - Add `GetADX()` method (expose current ADX)
   - Add trend strength classification

2. **Modify** `internal/farming/mode_manager.go`
   - Add regime evaluation:
     ```go
     if adx > 30 {
         mode = TradingModeTrendAdapted
     } else if adx < 20 && rangeActive {
         mode = TradingModeStandard
     } else {
         mode = TradingModeMicro
     }
     ```

3. **Modify** `internal/farming/adaptive_grid/manager.go`
   - Apply trend bias in TREND_ADAPTED mode
   - Reduce levels/spread in high volatility

**Acceptance Criteria**:
- [ ] ADX > 25 triggers TREND_ADAPTED mode
- [ ] Trend bias adds more orders on trend side
- [ ] Size reduced to 30% in trending markets
- [ ] Parameters update every 30 seconds

---

### Phase 4: Emergency Exit & Breakout Handling
**Duration**: 2-3 days
**Files to Modify**:

1. **Modify** `internal/farming/adaptive_grid/manager.go`
   - Enhance `handleBreakout()` for <5s exit:
     ```go
     // T+0ms: Detect
     if detector.IsBreakout() {
         // T+100ms: Cancel all
         go cancelAllOrdersAsync()
         
         // T+800ms: Close positions
         go closePositionsMarket()
         
         // Enter cooldown
         modeManager.EnterCooldown(60 * time.Second)
     }
     ```

2. **Create** `internal/farming/exit_executor.go`
   - `ExitExecutor` - handles fast exit sequence
   - Async order cancellation
   - Position close coordination
   - Completion verification

3. **Modify** `internal/farming/grid_manager.go`
   - Add `EmergencyExit(symbol string)` method
   - Track exit timing metrics

4. **Add** WebSocket fast path for exit:
   - Prioritize exit messages
   - Skip queue for cancel/close operations

**Acceptance Criteria**:
- [ ] Breakout detected within 1 candle (1 minute)
- [ ] All orders cancelled within 1 second
- [ ] Positions closed within 5 seconds
- [ ] Cooldown entered automatically
- [ ] Metrics: exit duration logged

---

### Phase 5: State Sync Workers (Internal vs Exchange)
**Duration**: 3 days
**Files to Create**:

1. **Create** `internal/farming/sync/order_sync_worker.go`
   ```go
   type OrderSyncWorker struct {
       wsClient      *client.WebSocketClient
       internalState *InternalState
       interval      time.Duration // 5s
       logger        *zap.Logger
   }
   
   func (w *OrderSyncWorker) Run() {
       ticker := time.NewTicker(5 * time.Second)
       for range ticker.C {
           w.sync()
       }
   }
   
   func (w *OrderSyncWorker) sync() {
       // Get WebSocket cached orders
       wsOrders := w.wsClient.GetCachedOrders()
       
       // Compare with internal
       for symbol, intOrders := range w.internalState.Orders {
           extOrders := wsOrders[symbol]
           
           // Check missing
           for id, order := range intOrders {
               if _, exists := extOrders[id]; !exists {
                   w.handleMissingOrder(order)
               }
           }
           
           // Check unknown
           for id, order := range extOrders {
               if _, exists := intOrders[id]; !exists {
                   w.handleUnknownOrder(order)
               }
           }
       }
   }
   ```

2. **Create** `internal/farming/sync/position_sync_worker.go`
   - Sync position size, side, PnL
   - Alert on side mismatch (critical)

3. **Create** `internal/farming/sync/balance_sync_worker.go`
   - Sync available margin
   - Alert on low margin

4. **Create** `internal/farming/sync/manager.go`
   - Coordinate all sync workers
   - Start/stop lifecycle

**Acceptance Criteria**:
- [ ] Workers run every 5 seconds
- [ ] Mismatch detected within 10 seconds
- [ ] Exchange state trusted over internal
- [ ] Alerts on critical mismatches (side, large size diff)

---

### Phase 6: WebSocket-Only Data Flow
**Duration**: 2 days
**Files to Modify**:

1. **Modify** `internal/farming/grid_manager.go`
   - Remove REST API calls for order/position state
   - Use WebSocket cache exclusively

2. **Modify** `internal/client/websocket.go`
   - Add `GetCachedOrders(symbol)` method
   - Add `GetCachedPositions()` method
   - Add TTL management for cache

3. **Create** WebSocket health monitor:
   ```go
   type WebSocketHealthMonitor struct {
       lastPong      time.Time
       latency       time.Duration
       isConnected   bool
       reconnectCount int
   }
   ```

4. **Add** Fallback logic:
   - 30s disconnect → COOLDOWN
   - 60s disconnect → Exit all positions
   - REST API only for emergency position check

**Acceptance Criteria**:
- [ ] No REST API calls during normal trading
- [ ] All data from WebSocket streams
- [ ] Reconnect with exponential backoff
- [ ] Automatic cooldown on extended disconnect

---

### Phase 7: Metrics & Observability
**Duration**: 1-2 days
**Files to Create**:

1. **Create** `internal/farming/metrics/trading_metrics.go`
   - Fill rate per hour
   - Average profit per fill
   - Mode duration tracking
   - Exit timing metrics

2. **Add** Prometheus metrics:
   - `trading_mode_current` (gauge)
   - `fills_per_hour` (counter)
   - `avg_profit_per_fill` (gauge)
   - `exit_duration_ms` (histogram)

3. **Enhance** logging:
   - Mode transitions with context
   - Sync mismatch details
   - Exit sequence timing

**Acceptance Criteria**:
- [ ] Fill rate tracked per symbol
- [ ] Mode transitions logged
- [ ] Exit duration < 5s (metric + alert)
- [ ] Prometheus metrics exposed

---

## Testing Strategy

### Unit Tests (Each Phase)
- Mode transition logic
- Parameter calculations
- Sync worker reconciliation

### Integration Tests
- Full startup → MICRO → STANDARD flow
- Breakout → Exit → Cooldown flow
- WebSocket disconnect → fallback flow

### Live Testing (Paper Trading)
- 1 symbol for 24 hours
- Verify >30 fills/hour
- Verify <5s exit time
- Monitor for state mismatches

---

## Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| State mismatch causes double orders | High | Sync workers + exchange-as-truth rule |
| WebSocket disconnect during exit | High | Fallback to REST for emergency, 5s timeout |
| Mode oscillation (rapid switches) | Medium | Hysteresis: 30s min in mode before switch |
| ATR bands too wide in MICRO mode | Medium | Min/max spread limits, 0.05% floor |
| Trend detection lag | Medium | ADX smoothing (14 periods), early exit on momentum |

---

## Success Metrics (Post-Implementation)

| Metric | Target | Measurement |
|--------|--------|-------------|
| Time to first order | <60s from startup | Log timestamp diff |
| Fill rate | >30 fills/hour | Counter / hour window |
| Exit latency | <5s from trigger | Timer start→finish |
| State mismatch rate | <0.1% | Sync worker alerts / total orders |
| WebSocket uptime | >99.5% | Connected time / total time |

---

## Dependencies

- **WebSocket client** (`internal/client/websocket.go`) - must support caching
- **Range detector** - must expose ATR bands
- **Agentic engine** - must provide ADX/trend signals
- **Exchange API** - must support WebSocket streams (orders, positions, klines)

---

## Implementation Order (Recommended)

1. **Phase 2** (Bypass Range Gate) - **CRITICAL** - Unblocks trading immediately
2. **Phase 1** (Mode Framework) - Foundation for other phases
3. **Phase 4** (Exit Handling) - Risk management
4. **Phase 3** (Regime Adjustment) - Optimization
5. **Phase 6** (WebSocket-Only) - Reliability
6. **Phase 5** (Sync Workers) - Safety
7. **Phase 7** (Metrics) - Observability

---

Generated: 2026-04-15
Feature: continuous-vf-architecture
Spec: @/media/aiozlong/data3/CODE/TOOLS/aster-perp-trading/.specify/features/continuous-vf-architecture/spec.md
