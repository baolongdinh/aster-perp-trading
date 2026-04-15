# Data Model: Continuous Volume Farming

## Core Entities

### TradingMode
```go
type TradingMode int

const (
    TradingModeUnknown TradingMode = iota
    TradingModeMicro        // Entry mode: ATR bands, 40% size
    TradingModeStandard     // Normal mode: BB bands, 100% size
    TradingModeTrendAdapted // Trend mode: BB bands, 30% size, bias
    TradingModeCooldown     // Pause mode: no orders, 60s wait
)

func (m TradingMode) String() string
func (m TradingMode) Parameters() DynamicParameters
```

### DynamicParameters
```go
type DynamicParameters struct {
    // Sizing
    SizeMultiplier   float64 // 0.3, 0.4, 1.0
    MinOrderSizeUSDT float64
    MaxOrderSizeUSDT float64
    
    // Grid structure
    SpreadMultiplier float64 // 1.0, 2.0, 3.0
    LevelCount       int      // 1-2, 2-3, 5
    
    // Bands
    UseBBBands       bool     // true = BB, false = ATR
    ATRMultiplier    float64  // for ATR-based bands
    
    // Trend
    TrendBiasEnabled bool     // add orders on trend side
    TrendBiasRatio   float64  // e.g., 0.6 (60% on trend side)
    
    // Timing
    CooldownDuration time.Duration
    MinModeDuration  time.Duration // anti-oscillation
}
```

### ModeManager
```go
type ModeManager struct {
    mu            sync.RWMutex
    currentMode   TradingMode
    modeSince     time.Time
    config        ModeConfig
    
    // Inputs
    rangeState    RangeState
    adx           float64
    isBreakout    bool
    isTrending    bool
    
    // History
    modeHistory   []ModeTransition
}

type ModeTransition struct {
    From      TradingMode
    To        TradingMode
    Timestamp time.Time
    Reason    string // "adx_spike", "range_active", "breakout"
}

func (m *ModeManager) EvaluateMode(
    rangeState RangeState,
    adx float64,
    isBreakout bool,
    isTrending bool,
) TradingMode

func (m *ModeManager) ShouldTransition(
    newMode TradingMode,
) bool // checks MinModeDuration, hysteresis
```

### SyncWorkers
```go
type OrderSyncWorker struct {
    wsClient      *client.WebSocketClient
    internalState *InternalOrderState
    interval      time.Duration
    mismatchAlertThreshold time.Duration // 10s
}

type PositionSyncWorker struct {
    wsClient      *client.WebSocketClient
    internalState *InternalPositionState
    interval      time.Duration
}

type BalanceSyncWorker struct {
    wsClient      *client.WebSocketClient
    internalState *InternalBalanceState
    interval      time.Duration
    lowMarginThreshold float64
}

type SyncResult struct {
    Symbol      string
    Mismatches  []Mismatch
    SyncedAt    time.Time
    ExchangeTruthApplied bool
}

type Mismatch struct {
    Type        string // "missing_order", "unknown_order", "status_diff", "size_diff", "side_diff"
    OrderID     string
    InternalVal interface{}
    ExchangeVal interface{}
    Severity    string // "warning", "critical"
}
```

### ExitExecutor
```go
type ExitExecutor struct {
    futuresClient *client.FuturesClient
    wsClient      *client.WebSocketClient
    timeout       time.Duration // 5s
}

type ExitSequence struct {
    TriggeredAt     time.Time
    OrdersCancelled int
    PositionsClosed int
    CompletedAt     time.Time
    Duration        time.Duration
    Error           error
}

func (e *ExitExecutor) ExecuteFastExit(ctx context.Context, symbol string) *ExitSequence
```

## Configuration

### ModeConfig (YAML)
```yaml
volume_farming:
  trading_modes:
    micro_mode:
      enabled: true
      size_multiplier: 0.4
      level_count: 3
      spread_multiplier: 2.0
      min_atr_multiplier: 1.5
      min_mode_duration_seconds: 30
      
    standard_mode:
      enabled: true
      size_multiplier: 1.0
      level_count: 5
      spread_multiplier: 1.0
      min_mode_duration_seconds: 60
      
    trend_adapted_mode:
      enabled: true
      size_multiplier: 0.3
      level_count: 2
      spread_multiplier: 2.0
      trend_bias_enabled: true
      trend_bias_ratio: 0.6
      min_mode_duration_seconds: 30
      
    cooldown_mode:
      duration_seconds: 60
      
  mode_transitions:
    adx_threshold_sideways: 20.0  # below = sideways/micro
    adx_threshold_trending: 25.0   # above = trend
    volatility_spike_multiplier: 3.0
    breakout_confirmations: 2
```

## State Transitions

### Mode State Machine
```
Unknown → Micro (on startup)
Micro → Standard (BB active + ADX < 20)
Micro → TrendAdapted (ADX > 25)
Micro → Cooldown (breakout detected)

Standard → Micro (BB inactive)
Standard → TrendAdapted (ADX > 25)
Standard → Cooldown (breakout)

TrendAdapted → Standard (ADX < 20 + BB active)
TrendAdapted → Micro (ADX < 20 + BB inactive)
TrendAdapted → Cooldown (breakout)

Cooldown → Micro (after duration)
```

## Metrics

### TradingMetrics
```go
type TradingMetrics struct {
    Symbol          string
    CurrentMode     TradingMode
    ModeSince       time.Time
    
    // Fill metrics
    FillsLastHour   int
    AvgProfitPerFill float64
    TotalVolume24h  float64
    
    // Exit metrics
    LastExitDuration time.Duration
    ExitCount24h    int
    
    // Sync metrics
    SyncMismatches24h int
    LastMismatchAt    time.Time
    
    // WebSocket
    WsConnected     bool
    WsLatencyMs     int
    WsReconnects24h int
}
```

## Relationships

```
[VolumeFarmEngine] → uses → [ModeManager]
[ModeManager] → evaluates → [RangeState, ADX, Breakout]
[ModeManager] → provides → [TradingMode]

[GridManager] → queries → [ModeManager.GetMode()]
[GridManager] → gets params → [DynamicParameters]
[GridManager] → places orders → [AdaptiveGridManager]

[AdaptiveGridManager] → checks → [CanPlaceOrder(mode)]
[AdaptiveGridManager] → builds grid → [with DynamicParameters]

[SyncWorkers] → monitor → [InternalState vs WebSocket]
[SyncWorkers] → reconcile → [Trust Exchange State]

[ExitExecutor] → triggered by → [Breakout / Trend]
[ExitExecutor] → cancels → [Orders]
[ExitExecutor] → closes → [Positions]
```
