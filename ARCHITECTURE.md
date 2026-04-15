# Architecture Documentation

## Per-Symbol Isolation Design

### Overview

The trading bot is designed to handle multiple symbols independently, ensuring that each symbol's trading decisions, order placement, and state updates are handled on a per-symbol basis. This is critical because each symbol has unique signals, charts, and volume characteristics.

### Key Design Principles

1. **Independent Symbol Trading**: Each symbol trades independently with its own signal, chart data, and volume tracking.
2. **Isolated State Management**: Each symbol maintains its own state for trading decisions, loss tracking, and cooldowns.
3. **No Cross-Symbol Contamination**: One symbol's poor performance or cooldown state does NOT affect other symbols.

### Components with Per-Symbol Isolation

#### TradeTracker

**Location**: `backend/internal/farming/adaptive_grid/risk_sizing.go`

**Purpose**: Tracks trade history for Kelly Criterion calculation and smart order sizing.

**Per-Symbol Implementation**:
- Uses `map[string][]TradeResult` to store trade history per symbol
- `GetWinRate(symbol)` returns win rate for specific symbol only
- `GetConsecutiveLosses(symbol)` returns consecutive losses for specific symbol only
- One symbol's losses do NOT affect another symbol's consecutive loss count

**Example**:
```go
// BTC has 3 losses, ETH has 0 losses
tracker.RecordTrade("BTC", -1.0)
tracker.RecordTrade("BTC", -1.0)
tracker.RecordTrade("BTC", -1.0)
tracker.RecordTrade("ETH", 1.0)

// BTC's metrics
tracker.GetWinRate("BTC")      // 0.0 (0% win rate)
tracker.GetConsecutiveLosses("BTC")  // 3

// ETH's metrics (unaffected by BTC)
tracker.GetWinRate("ETH")      // 1.0 (100% win rate)
tracker.GetConsecutiveLosses("ETH")  // 0
```

#### ExposureManager

**Location**: `backend/internal/farming/adaptive_grid/risk_sizing.go`

**Purpose**: Manages total exposure and implements cooldowns based on consecutive losses.

**Per-Symbol Implementation**:
- Uses `map[string]int` for `consecutiveLosses` per symbol
- Uses `map[string]time.Time` for `lastLossTime` per symbol
- Uses `map[string]bool` for `cooldownActive` per symbol
- `RecordLoss(symbol)` updates only that symbol's state
- `IsCooldownActive(symbol)` returns cooldown state for specific symbol only
- One symbol entering cooldown does NOT block other symbols

**Example**:
```go
// BTC hits 3 losses and enters cooldown
exposureMgr.RecordLoss("BTC")
exposureMgr.RecordLoss("BTC")
exposureMgr.RecordLoss("BTC")

// BTC is in cooldown
exposureMgr.IsCooldownActive("BTC")  // true

// ETH can still trade (not affected by BTC's cooldown)
exposureMgr.IsCooldownActive("ETH")  // false
```

#### AdaptiveGridManager

**Location**: `backend/internal/farming/adaptive_grid/manager.go`

**Purpose**: Higher-level adaptive logic for grid placement and risk management.

**Per-Symbol Implementation**:
- Already uses per-symbol maps for `consecutiveLosses`, `lastLossTime`, `cooldownActive`
- Each symbol has independent state machine
- Grid placement and risk decisions are per-symbol

#### GridManager

**Location**: `backend/internal/farming/grid_manager.go`

**Purpose**: Manages active grids, orders, fills, and position caches.

**Per-Symbol Implementation**:
- Each symbol has its own grid state
- Order and position caches are per-symbol
- Grid placement logic is per-symbol

### ModeManager

**Location**: `backend/internal/farming/tradingmode/mode_manager.go`

**Status**: Deprecated in favor of `CircuitBreaker`

**Note**: ModeManager has both global and per-symbol state, but it's being phased out. The new `CircuitBreaker` component provides unified per-symbol trading mode and breaker state management.

### Testing

Unit tests verify per-symbol isolation:

1. **TestTradeTracker_PerSymbolIsolation**: Verifies that BTC losses don't affect ETH metrics
2. **TestExposureManager_PerSymbolIsolation**: Verifies that BTC cooldown doesn't block ETH trading

### Migration Checklist

When adding new trading logic, ensure:

- [ ] State is stored per-symbol (use `map[string]T` pattern)
- [ ] Methods accept `symbol` parameter
- [ ] One symbol's state changes don't affect other symbols
- [ ] Unit tests verify per-symbol isolation
- [ ] Documentation clearly states per-symbol design

### Benefits of Per-Symbol Isolation

1. **Independent Risk Management**: Each symbol's risk is managed independently
2. **No Cascading Failures**: One symbol's poor performance doesn't cascade to others
3. **Accurate Metrics**: Win rates and loss counts are accurate per symbol
4. **Flexible Trading**: Bot can continue trading healthy symbols while one is in cooldown
5. **Scalability**: Easy to add new symbols without affecting existing ones

### Common Pitfalls to Avoid

1. **Global State**: Never use global variables for symbol-specific state
2. **Mixed Aggregation**: Don't aggregate metrics across symbols when making per-symbol decisions
3. **Shared Cooldowns**: Ensure cooldowns are per-symbol, not global
4. **Cross-Symbol Dependencies**: Avoid logic where one symbol's state affects another symbol's decisions
