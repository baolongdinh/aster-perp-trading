# Feature Specification: Micro Profit + Volume Farming

## Overview

### Feature Description
Add micro profit taking capability to the trading bot. When a grid order is filled and a position is opened, immediately place a take profit order at a small spread (0.005%) to capture micro profit. Once the take profit order is filled, automatically rebalance the grid to continue volume farming. This enables the bot to generate consistent micro profits while maintaining volume farming operations.

### Business Value
- Generate consistent micro profits from each filled order instead of waiting for large price movements
- Reduce position holding time and liquidation risk
- Maintain volume farming metrics while adding profit generation
- Improve overall profitability without increasing risk exposure

## User Scenarios & Testing

### Scenario 1: Buy Order Filled - Micro Profit Taking
**Given** Bot has active grid orders with BUY orders below current price
**When** A BUY order is filled at $1000, opening a LONG position
**Then** System immediately places a SELL take profit order at $1000.05 (0.005% spread) with ReduceOnly flag
**And** When take profit order is filled, system records $0.05 profit
**And** System rebalances grid orders around current price
**And** Volume farming continues with new grid orders

### Scenario 2: Sell Order Filled - Micro Profit Taking
**Given** Bot has active grid orders with SELL orders above current price
**When** A SELL order is filled at $1000, opening a SHORT position
**Then** System immediately places a BUY take profit order at $999.95 (0.005% spread) with ReduceOnly flag
**And** When take profit order is filled, system records $0.05 profit
**And** System rebalances grid orders around current price
**And** Volume farming continues with new grid orders

### Scenario 3: Take Profit Timeout - Position Close
**Given** A position is opened and take profit order is placed
**When** Take profit order is not filled within 30 seconds
**Then** System closes position by market order
**And** System rebalances grid orders around current price
**And** Volume farming continues

### Scenario 4: Multiple Positions - Individual Take Profit
**Given** Bot has multiple open positions from filled grid orders
**When** Each position is opened
**Then** System places individual take profit order for each position
**And** Take profit orders are tracked independently
**And** Each filled take profit triggers individual grid rebalance

## Functional Requirements

### FR1: Automatic Take Profit Order Placement
**Acceptance Criteria:**
- When a grid order is filled and position is opened, system immediately places a take profit order
- Take profit order is placed at configurable spread percentage (default 0.005%)
- Take profit order has ReduceOnly flag set to true
- Take profit order size matches the filled position size
- Take profit order is placed on the opposite side of the filled order

### FR2: Take Profit Order Tracking
**Acceptance Criteria:**
- System tracks all active take profit orders with their corresponding positions
- System monitors take profit order fill status
- System logs take profit order placement and fill events
- System records profit amount when take profit order is filled

### FR3: Grid Rebalance After Take Profit
**Acceptance Criteria:**
- When take profit order is filled, system triggers immediate grid rebalance
- Grid rebalance cancels stale orders and places new orders around current price
- Grid rebalance respects all existing risk checks and state machine gates
- Volume farming metrics are updated after rebalance

### FR4: Take Profit Timeout Handling
**Acceptance Criteria:**
- If take profit order is not filled within configurable timeout (default 30s), system closes position by market order
- Position close triggers grid rebalance
- System logs timeout events
- Timeout is configurable per symbol

### FR5: Configuration Management
**Acceptance Criteria:**
- Micro profit feature can be enabled/disabled via configuration
- Take profit spread percentage is configurable (default 0.005%)
- Take profit timeout is configurable (default 30s)
- Minimum profit threshold is configurable (default $0.01)
- Configuration can be updated without bot restart

## Success Criteria

1. Micro profit generation rate: At least 60% of filled positions generate micro profit
2. Average position holding time: Reduced from current baseline by 50%
3. Profit per trade: Average micro profit of at least $0.01 per filled order
4. Volume farming continuity: Grid rebalancing continues seamlessly after take profit
5. System stability: No increase in order rejection or API errors

## Key Entities

- TakeProfitOrder: Represents a take profit order placed for a position
  - Fields: OrderID, Symbol, Side, Price, Size, ParentOrderID, Status, CreatedAt, FilledAt
- MicroProfitConfig: Configuration for micro profit feature
  - Fields: Enabled, SpreadPct, TimeoutSeconds, MinProfitUSDT
- PositionTakeProfitMapping: Maps positions to their take profit orders
  - Fields: PositionID, TakeProfitOrderID, Symbol, Status

## Assumptions & Dependencies

- Bot has existing grid order placement and fill tracking infrastructure
- Bot has existing position tracking and management
- Bot has existing grid rebalancing logic
- Exchange API supports ReduceOnly orders
- Exchange API allows order placement within milliseconds

## Out of Scope

- Dynamic take profit spread adjustment based on market conditions
- Take profit order partial fills (assume full fill or timeout)
- Complex take profit strategies (trailing stop, scaling out)
- Cross-symbol take profit hedging
- Take profit order placement during high volatility periods

