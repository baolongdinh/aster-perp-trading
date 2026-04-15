# Data Model: Micro Profit + Volume Farming

## Overview

This document defines the data structures and entities required for the micro profit feature, which adds take profit order placement and tracking capabilities to the existing grid trading system.

## Entities

### TakeProfitOrder

Represents a take profit order placed for a specific position.

**Fields:**
- `OrderID` (string): Unique identifier for the take profit order
- `Symbol` (string): Trading symbol (e.g., "BTCUSD1")
- `Side` (string): Order side ("BUY" or "SELL")
- `Price` (float64): Take profit price
- `Size` (float64): Order size (quantity)
- `ParentOrderID` (string): ID of the filled grid order that triggered this take profit
- `Status` (TakeProfitStatus): Current status of the take profit order
- `CreatedAt` (time.Time): Timestamp when take profit order was created
- `FilledAt` (time.Time): Timestamp when take profit order was filled (nil if not filled)
- `TimeoutAt` (time.Time): Timestamp when take profit order will timeout (nil if no timeout)

**Relationships:**
- Belongs to: `GridOrder` (via ParentOrderID)
- Associated with: `Position` (via Symbol and timing)

**Validation Rules:**
- OrderID must be unique
- Price must be > 0
- Size must be > 0
- Side must be opposite to parent order side
- Status transitions must follow valid state machine

**State Transitions:**
```
PENDING → FILLED (order filled successfully)
PENDING → CANCELLED (order cancelled manually)
PENDING → TIMEOUT (order expired without fill)
```

### MicroProfitConfig

Configuration for the micro profit feature.

**Fields:**
- `Enabled` (bool): Whether micro profit feature is enabled
- `SpreadPct` (float64): Spread percentage for take profit orders (e.g., 0.005 = 0.005%)
- `TimeoutSeconds` (int): Timeout duration in seconds for take profit orders
- `MinProfitUSDT` (float64): Minimum profit threshold in USDT to place take profit order

**Validation Rules:**
- SpreadPct must be between 0.001 and 0.01 (0.001% to 0.01%)
- TimeoutSeconds must be between 10 and 300 (10s to 5min)
- MinProfitUSDT must be between 0.001 and 1.0 ($0.001 to $1)

**Default Values:**
- Enabled: false
- SpreadPct: 0.005
- TimeoutSeconds: 30
- MinProfitUSDT: 0.01

### PositionTakeProfitMapping

Maps a position to its corresponding take profit order.

**Fields:**
- `PositionID` (string): Identifier for the position (symbol + timestamp)
- `TakeProfitOrderID` (string): ID of the take profit order
- `Symbol` (string): Trading symbol
- `Status` (MappingStatus): Status of the mapping

**Relationships:**
- Links: `Position` ↔ `TakeProfitOrder`

**Validation Rules:**
- PositionID must be unique
- TakeProfitOrderID must reference existing TakeProfitOrder
- Symbol must match both position and take profit order

**State Transitions:**
```
ACTIVE → COMPLETED (take profit filled)
ACTIVE → CANCELLED (take profit cancelled)
ACTIVE → TIMEOUT (take profit timed out)
```

## Enums

### TakeProfitStatus

Status of a take profit order.

**Values:**
- `PENDING`: Order placed, waiting for fill
- `FILLED`: Order filled successfully
- `CANCELLED`: Order cancelled manually
- `TIMEOUT`: Order expired without fill

### MappingStatus

Status of the position-to-take-profit mapping.

**Values:**
- `ACTIVE`: Mapping is active, take profit order pending
- `COMPLETED`: Take profit order filled, mapping complete
- `CANCELLED`: Take profit order cancelled, mapping cancelled
- `TIMEOUT`: Take profit order timed out, mapping expired

## Relationships with Existing Entities

### GridOrder (Existing)

**New Relationship:**
- GridOrder can have 0 or 1 associated TakeProfitOrder (via ParentOrderID)

**Impact:**
- Add field `TakeProfitOrderID *string` to GridOrder structure
- Update GridOrder fill handling to trigger take profit placement

### Position (Existing)

**New Relationship:**
- Position can have 0 or 1 associated TakeProfitOrder (via PositionTakeProfitMapping)

**Impact:**
- Add field `TakeProfitOrderID *string` to Position structure
- Update position tracking to include take profit status

### SymbolGrid (Existing)

**New Relationship:**
- SymbolGrid has 0 or 1 associated MicroProfitConfig

**Impact:**
- Add field `MicroProfitConfig *MicroProfitConfig` to SymbolGrid structure
- Load config during grid initialization

## Data Flow

### Order Fill Flow

```
GridOrder filled
  ↓
Create Position
  ↓
Create PositionTakeProfitMapping (ACTIVE)
  ↓
Place TakeProfitOrder (PENDING)
  ↓
Link TakeProfitOrder to Position
  ↓
Monitor TakeProfitOrder status
  ↓
If FILLED → Update Mapping to COMPLETED → Record Profit → Rebalance Grid
If TIMEOUT → Update Mapping to TIMEOUT → Close Position → Rebalance Grid
If CANCELLED → Update Mapping to CANCELLED → Rebalance Grid
```

### Configuration Load Flow

```
Load micro_profit.yaml
  ↓
Parse MicroProfitConfig
  ↓
Validate config values
  ↓
Apply to TakeProfitManager
  ↓
Watch file for changes (hot-reload)
```

## Storage Considerations

**In-Memory Storage:**
- TakeProfitOrder: Stored in memory map for fast access
- PositionTakeProfitMapping: Stored in memory map
- MicroProfitConfig: Loaded into memory on startup

**Persistence:**
- Configuration: Persisted in YAML file (micro_profit.yaml)
- Orders: Not persisted (recreated on restart)
- Mappings: Not persisted (recreated on restart)

**Cleanup:**
- Remove completed/cancelled/timed out mappings after 1 hour
- Remove completed take profit orders after 1 hour
