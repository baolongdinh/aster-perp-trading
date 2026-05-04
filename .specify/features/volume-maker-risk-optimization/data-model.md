# Data Model: Volume Maker Risk Optimization

## Entities

### 1. PositionWithTimeout
Extended position với timeout tracking và trailing.

```go
type PositionWithTimeout struct {
    // Core position fields
    Symbol         string
    Side           string      // "long" or "short"
    EntryPrice     float64
    Size           float64
    NotionalValue  float64
    
    // Timeout tracking
    OpenTime       time.Time   // When position was opened
    LastUpdateTime time.Time   // Last time position was updated
    
    // Trailing stop state
    TrailingPeak   float64     // Peak profit achieved
    IsTrailing     bool        // Whether trailing is active
}
```

**Validation:**
- Size > 0
- EntryPrice > 0
- OpenTime not zero

---

### 2. TrailingState
State cho trailing stop mechanism.

```go
type TrailingState struct {
    PositionID     string
    PeakProfitPct  float64     // Peak profit as percentage
    ActivationPct  float64     // When trailing activates (default: 0.1%)
    CallbackPct    float64     // Trail distance (default: 0.03%)
    IsActive       bool        // Whether trailing is currently active
}
```

**Validation:**
- ActivationPct > CallbackPct
- CallbackPct > 0

---

### 3. ZoneType
EMA zone classification.

```go
type ZoneType string

const (
    ZoneAboveEMA   ZoneType = "above_ema"    // > 0% from EMA
    ZoneNormalDip  ZoneType = "normal_dip"   // 0% to -1% from EMA
    ZoneStrongDip  ZoneType = "strong_dip"   // -1% to -2% from EMA
    ZoneHardDip    ZoneType = "hard_dip"     // < -2% from EMA (no buy)
)
```

---

### 4. ZoneMultiplier
Configuration cho zone-based sizing.

```go
type ZoneMultiplier struct {
    AboveEMAMultiplier  float64   // Default: 0.5
    NormalDipMultiplier float64   // Default: 1.0
    StrongDipMultiplier float64   // Default: 1.5
    HardDipMultiplier   float64   // Default: 0.0 (no buy)
}
```

---

### 5. GridActiveZone
Active zone cho grid orders.

```go
type GridActiveZone struct {
    MinPrice       float64     // Lower bound of active zone
    MaxPrice       float64     // Upper bound of active zone
    GridSpacing    float64     // Spacing between levels (0.05-0.06%)
    LevelCount     int         // Number of levels (5-10)
    Levels         []float64   // Calculated price levels
}
```

**Validation:**
- MinPrice < MaxPrice
- LevelCount >= 5 && LevelCount <= 10
- GridSpacing >= 0.0005 && GridSpacing <= 0.0006

---

### 6. RiskLevel
10-level risk management.

```go
type RiskLevel struct {
    Level       int
    Name        string
    Trigger     string      // Condition that triggers this level
    Action      string      // What happens when triggered
    IsActive    bool
}
```

| Level | Name | Trigger | Action |
|-------|------|---------|--------|
| 1 | IM Rate High | IM >= 90% | Block workflow |
| 2 | High IM + Profit | IM >= 80% && profit > 0 | Close position |
| 3 | Trailing Stop | Profit >= 0.1% | Activate trailing |
| 4 | Position Limit | Position > 40% max | Block new orders |
| 5 | Position Timeout | Position > 60s | Force close |
| 6 | Both Profitable | Long && Short profit > 0 | Close both |
| 7 | TP Safety | Insufficient margin for TP | Pause TP |
| 8 | TP Pre-Check | Margin < required for protective | Block orders |
| 9 | Rate Limit | > N regrid in 48h | Block regrid |
| 10 | Emergency | Multiple triggers | Full stop |

---

### 7. RiskState
Current risk state của system.

```go
type RiskState struct {
    IMRate             float64
    PositionSizeUSD    float64
    MaxPositionUSD     float64
    DailyPnL           float64
    EmergencyTriggered bool
    ActiveRiskLevels   []int
    PositionCount      int
}
```

**Validation:**
- IMRate >= 0 && IMRate <= 1
- PositionCount >= 0

---

### 8. MarketStateWithEMA
Extended market state với EMA.

```go
type MarketStateWithEMA struct {
    LastPrice      float64
    BidPrice       float64
    AskPrice       float64
    Spread         float64
    Volume         float64
    Volatility     float64
    
    // EMA
    EMA30          float64
    EMADistancePct float64   // (price - EMA) / EMA * 100
    CurrentZone    ZoneType
}
```

---

### 9. OrderConfig
Configuration cho order placement.

```go
type OrderConfig struct {
    MinOrderSizeUSD  float64   // Default: 2.0
    MaxOrderSizeUSD  float64   // Default: 10.0
    GridSpacing      float64   // Default: 0.0005 (0.05%)
    ActiveZoneRange  float64   // Default: 0.001 (0.1%)
    MaxOrdersPerSide int       // Default: 5
    UsePostOnly      bool      // Default: true
}
```

---

### 10. DailyResetState
State cho daily reset.

```go
type DailyResetState struct {
    LastResetDate    time.Time
    ResetHour        int       // Default: 23 (UTC)
    PositionsClosed  int
    TotalVolume      float64
    TotalProfit      float64
}
```

---

## State Transitions

### Position Lifecycle
```
NEW → OPEN (when filled) → TRAILING (when profit >= 0.1%) → CLOSED
                     ↓
               TIMEOUT (60s) → CLOSED
                     ↓
            TRAILING_CALLBACK → CLOSED
```

### Zone Transition
```
AboveEMA → NormalDip → StrongDip → HardDip
   ↑_________↓_________↓_________|
   (reverse direction)
```

### Risk Level Transition
```
NORMAL → L1 (IM>=90%) → L4 (Pos>40%) → L5 (Timeout) → L10 (Emergency)
         ↓              ↓              ↓
       NORMAL        NORMAL        NORMAL
```

---

## Relationships

```
MakerStrategy
├── RiskManager (1:1)
│   └── RiskState (contains)
├── OrderManager (1:1)
├── MarketDataProvider (uses)
│   └── MarketStateWithEMA
└── PositionManager (manages)
    └── PositionWithTimeout[]
        └── TrailingState (each position)
```

---

## Configuration Summary

| Config | Type | Default | Description |
|--------|------|---------|-------------|
| ActiveZoneRange | float64 | 0.001 | 0.1% range from price |
| GridSpacing | float64 | 0.0005 | 0.05% between levels |
| PositionTimeoutSeconds | int | 60 | Max hold time |
| TrailingActivationPct | float64 | 0.001 | 0.1% profit to activate |
| TrailingCallbackPct | float64 | 0.0003 | 0.03% trail distance |
| MaxPositionsPerSide | int | 5 | Max positions per side |
| DailyResetHour | int | 23 | Hour to reset (UTC) |
| MarginEquityRatio | float64 | 0.75 | 75% equity for trading |
| MinOrderSizeUSD | float64 | 2.0 | Minimum order size |
| MaxOrderSizeUSD | float64 | 10.0 | Maximum order size |
| SpreadThreshold | float64 | 0.001 | 0.1% max spread |
| EMAPeriod | int | 30 | EMA calculation period |
