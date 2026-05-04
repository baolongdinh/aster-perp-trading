# Implementation Plan: Volume Maker Risk Optimization & Fill Rate Maximization

## Feature Overview

**Feature**: Volume Maker Risk Optimization & Fill Rate Maximization  
**Objective**: Farm volume + micro profit với vốn nhỏ + leverage 150x, ngăn ngừa liquidation, tăng fill rate gấp 2-3 lần

---

## Technical Context

### Architecture
- **Project**: `aster-perp-trading/backend`
- **Language**: Go
- **Main Package**: `internal/farming/maker/`
- **Entry Point**: `cmd/volume-farm-maker/main.go`

### Key Components to Modify

| Component | File | Changes |
|-----------|------|---------|
| Strategy | `internal/farming/maker/strategy.go` | Add active zone, position timeout, trailing stop |
| Order Manager | `internal/farming/maker/order_manager.go` | Add post-only, smaller orders |
| Config | `internal/farming/maker/config.go` | Add new config params |
| Risk Guards | New file or extend existing | 10-level risk management |

### Dependencies
- WebSocket client for real-time price/EMA
- Futures client for order placement
- Existing risk guards (to be extended)

### Unknowns / Needs Clarification
- **Post-only support**: Exchange có hỗ trợ post-only order type không?
- **Maker rebate**: Exchange có cung cấp maker rebate không?
- **EMA calculation**: Dùng EMA period nào? (đề xuất: 30)

---

## Constitution Check

### Gate 1: Minimal Scope
- [x] Single feature: Risk optimization + fill rate
- [x] Single symbol: ETHUSD1/BTCUSD1
- [x] No external services required

### Gate 2: Reversibility
- [x] All changes in `internal/farming/maker/`
- [x] Config-based, not hard-coded
- [x] Can be disabled via config

### Gate 3: Testability
- [x] Unit tests for each FR
- [x] Integration test with dry-run mode
- [x] Success criteria measurable

### Gate 4: Security
- [x] No new API keys required
- [x] Uses existing exchange API
- [x] Risk controls prevent over-exposure

---

## Phase 0: Research

### Research Tasks

| Task | Description | Status |
|------|-------------|--------|
| R1 | Verify post-only order support on Aster exchange | Pending |
| R2 | Verify maker rebate availability | Pending |
| R3 | Research EMA calculation implementation in Go | Pending |
| R4 | Research trailing stop algorithm | Pending |

---

## Phase 1: Implementation

### Step 1.1: Config Updates
```
File: internal/farming/maker/config.go
Changes:
- Add ActiveZoneRange float64 (default: 0.001 = 0.1%)
- Add GridSpacingMin float64 (default: 0.0005 = 0.05%)
- Add PositionTimeoutSeconds int (default: 60)
- Add TrailingActivationPct float64 (default: 0.001)
- Add TrailingCallbackPct float64 (default: 0.0003)
- Add MaxPositionsPerSide int (default: 5)
- Add DailyResetHour int (default: 23)
- Add MarginEquityRatio float64 (default: 0.75)
- Add MinOrderSizeUSD float64 (default: 2.0)
- Add MaxOrderSizeUSD float64 (default: 10.0)
- Add SpreadThreshold float64 (default: 0.001)
- Add EMAPeriod int (default: 30)
- Add ZoneMultipliers (above, normal, strong, hard)
```

### Step 1.2: New Data Structures
```
File: internal/farming/maker/types.go (new or extend)
New Types:
- PositionWithTimeout: Position + OpenTime + TrailingPeak
- TrailingState: PeakProfit + ActivationLevel + CallbackLevel
- ZoneType: enum (AboveEMA, NormalDip, StrongDip, HardDip)
- GridActiveZone: MinPrice + MaxPrice + Levels[]
```

### Step 1.3: Active Zone Grid (FR1)
```
File: internal/farming/maker/strategy.go
Function: calculateActiveZoneGrid()
- Get current market price
- Calculate active zone range (0.1% from price)
- Place only 5-10 orders within this range
- Skip orders outside active zone
```

### Step 1.4: Smaller Order Size (FR2)
```
File: internal/farming/maker/strategy.go
Function: calculateOrderSize()
- Split total order value into smaller chunks
- Min: $2, Max: $10 per order
- Increase order count based on balance
```

### Step 1.5: Post-Only Orders (FR3)
```
File: internal/farming/maker/order_manager.go
Function: PlaceLimitOrder()
- Add isPostOnly parameter
- Use GTX + postOnly flag
- Verify maker rebate > taker fees
```

### Step 1.6: Position Timeout (FR4)
```
File: internal/farming/maker/strategy.go
New Loop: positionTimeoutLoop()
- Track position open time
- Check every 5 seconds
- Force close if > 60 seconds
- Log reason for force close
```

### Step 1.7: Trailing Stop (FR5)
```
File: internal/farming/maker/strategy.go
New Function: manageTrailingStop()
- Track peak profit per position
- Activate trailing at +0.1%
- Trail by 0.03% (sell if drops)
- Update on each price tick
```

### Step 1.8: Max Position + FIFO (FR6)
```
File: internal/farming/maker/strategy.go
Function: checkPositionLimits()
- Count positions per side
- If > 5: close oldest first (FIFO)
- Use position open time for ordering
```

### Step 1.9: Zone-Based Sizing (FR7)
```
File: internal/farming/maker/strategy.go
Function: calculateZoneMultiplier()
- Get EMA from market data
- Calculate distance from EMA
- Return multiplier: 0.5x / 1.0x / 1.5x / 0x
```

### Step 1.10: Daily Reset (FR8)
```
File: internal/farming/maker/strategy.go
New Loop: dailyResetLoop()
- Check current hour vs DailyResetHour
- If match: close ALL positions
- Log all closed positions
- Reset for next day
```

### Step 1.11: Spread Protection (FR9)
```
File: internal/farming/maker/strategy.go
Function: checkSpreadBeforeOrder()
- Get current spread
- If > 0.1%: pause new orders
- Resume when spread < 0.1%
```

### Step 1.12: Startup Reconciliation (FR10)
```
File: internal/farming/maker/strategy.go
Function: reconcileOnStartup()
- Fetch positions from exchange
- Fetch open orders from exchange
- Compare with local state
- Log discrepancies
- Block if state mismatch
```

### Step 1.13: 10-Level Risk Management (FR13)
```
File: internal/farming/maker/risk_manager.go (new)
Struct: RiskManager
- Levels map with triggers and actions
- CheckAllLevels() function
- Emergency stop handling
```

### Step 1.14: Balance-Based Order Count (FR15)
```
File: internal/farming/maker/strategy.go
Function: calculateOrderCount()
- Base: 5 orders
- Add 1 order per $5 balance
- Max: 20 orders
```

---

## Phase 2: Testing

### Unit Tests
| Test | Coverage |
|------|----------|
| TestActiveZoneGrid | FR1 |
| TestOrderSizeSplit | FR2 |
| TestPostOnlyOrder | FR3 |
| TestPositionTimeout | FR4 |
| TestTrailingStop | FR5 |
| TestPositionLimits | FR6 |
| TestZoneMultiplier | FR7 |
| TestDailyReset | FR8 |
| TestSpreadProtection | FR9 |
| TestStartupReconciliation | FR10 |
| TestRiskLevels | FR13 |

### Integration Test
- Run with dry-run mode
- Verify fills happen
- Verify positions close on timeout
- Verify trailing stop activates

---

## Dependencies & Sequence

```
Phase 1:
├── Step 1.1: Config (prerequisite for all)
├── Step 1.2: Types (prerequisite for all)
├── Step 1.3: Active Zone Grid
├── Step 1.4: Order Size
├── Step 1.5: Post-Only
├── Step 1.6: Position Timeout
├── Step 1.7: Trailing Stop
├── Step 1.8: Max Position + FIFO
├── Step 1.9: Zone-Based Sizing
├── Step 1.10: Daily Reset
├── Step 1.11: Spread Protection
├── Step 1.12: Startup Reconciliation
├── Step 1.13: Risk Management (depends on all above)
└── Step 1.14: Balance-Based Order Count

Phase 2:
└── Testing (after all Phase 1)
```

---

## Success Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Fill Rate | >2000/day | Log count |
| Liquidation | 0 | Monitor positions |
| Position Timeout | 100% <60s | Timestamp tracking |
| Spread Protection | 0 orders during wide spread | Log verification |
| Maker Rebate | >0 | Account balance check |

---

## Notes

- Leverage vẫn giữ 150x (không giảm)
- Tập trung vào Active Zone để tăng fill rate
- Position timeout là key để prevent liquidation
- Daily reset để tránh overnight risk
