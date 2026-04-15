# Implementation: Micro Profit + Volume Farming

## Technical Context

**Existing Infrastructure:**
- Grid order placement system in `grid_manager.go`
- Order fill tracking in `handleOrderFill()`
- Position tracking in `AdaptiveGridManager`
- Grid rebalancing logic in `enqueuePlacement()`
- Risk management and state machine gates

**New Components Needed:**
- Take profit order placement logic
- Take profit order tracking system
- Take profit timeout handling
- Micro profit configuration management
- Profit recording and metrics

**Integration Points:**
- `handleOrderFill()` - trigger take profit order placement
- `OnOrderUpdate()` - monitor take profit order fills
- Grid rebalancing - trigger after take profit fill
- Risk manager - ensure take profit orders respect risk limits
- Configuration system - load micro profit config

**Unknowns/Research Needed:**
- Exchange API support for ReduceOnly orders (assumed supported)
- Optimal take profit spread for different symbols (research needed)
- Take profit timeout duration based on market conditions (research needed)

## Constitution Check

**Project Principles:**
- CircuitBreaker is the single source of truth for trading decisions
- Bot must have ability to generate micro profit, not just volume farming
- System must handle edge cases gracefully (timeouts, failures)
- Configuration must be hot-reloadable without restart

**Gate Evaluation:**
- ✅ Feature aligns with profit generation principle
- ✅ Take profit orders respect CircuitBreaker via existing CanPlaceOrder checks
- ✅ Timeout handling ensures system doesn't get stuck
- ✅ Configuration management allows hot-reload
- ⚠️ Need to ensure take profit orders don't interfere with CircuitBreaker mode decisions

## Phase 0: Research

### Task 0.1: Exchange API ReduceOnly Support
**Research Question:** Does the exchange API support ReduceOnly orders for take profit?
**Deliverable:** Document API capabilities and limitations
**Method:** Test with small order or review API documentation

### Task 0.2: Optimal Take Profit Spread
**Research Question:** What is the optimal spread percentage for take profit orders across different symbols?
**Deliverable:** Recommended spread values per symbol/volatility regime
**Method:** Analyze historical fill data, test different spreads in simulation

### Task 0.3: Take Profit Timeout Duration
**Research Question:** What is the appropriate timeout duration for take profit orders?
**Deliverable:** Recommended timeout values based on market conditions
**Method:** Analyze average time to fill for limit orders at small spreads

**Output:** `.specify/features/feature/001-micro-profit-volume/tech-specs/research.md`

## Phase 1: Design & Contracts

### Task 1.1: Data Model Design
**Deliverable:** `data-model.md` with entities:
- TakeProfitOrder structure
- MicroProfitConfig structure
- PositionTakeProfitMapping structure
- Relationships to existing GridOrder and Position entities

### Task 1.2: Configuration Contract
**Deliverable:** `contracts/micro-profit-config.yml` defining:
- Configuration schema
- Default values
- Validation rules
- Hot-reload mechanism

### Task 1.3: Quick Start Guide
**Deliverable:** `quickstart.md` with:
- Feature enable/disable instructions
- Configuration examples
- Testing procedures
- Monitoring guidelines

### Task 1.4: Agent Context Update
**Deliverable:** Update agent-specific context files with new components

**Output:** `data-model.md`, `contracts/micro-profit-config.yml`, `quickstart.md`

## Phase 2: Implementation Plan

### Phase 2.1: Core Take Profit Logic

#### Task 2.1.1: Create MicroProfitConfig Structure
**File:** `internal/farming/adaptive_grid/micro_profit_config.go`
**Description:** Define configuration structure for micro profit feature
**Acceptance Criteria:**
- Config struct with Enabled, SpreadPct, TimeoutSeconds, MinProfitUSDT
- Default values: Enabled=false, SpreadPct=0.005, TimeoutSeconds=30, MinProfitUSDT=0.01
- Validation methods to ensure config values are within acceptable ranges
- Load from YAML configuration file

#### Task 2.1.2: Create TakeProfitOrder Structure
**File:** `internal/farming/adaptive_grid/take_profit_order.go`
**Description:** Define structure to track take profit orders
**Acceptance Criteria:**
- Fields: OrderID, Symbol, Side, Price, Size, ParentOrderID, Status, CreatedAt, FilledAt
- Status enum: PENDING, FILLED, CANCELLED, TIMEOUT
- Methods to calculate profit amount
- Methods to check if order is expired

#### Task 2.1.3: Create TakeProfitManager
**File:** `internal/farming/adaptive_grid/take_profit_manager.go`
**Description:** Manager class to handle take profit order lifecycle
**Acceptance Criteria:**
- Place take profit order when position is opened
- Track take profit order status
- Handle take profit order fills
- Handle take profit order timeouts
- Record profit metrics
- Thread-safe operations with mutex

#### Task 2.1.4: Integrate TakeProfitManager into GridManager
**File:** `internal/farming/grid_manager.go`
**Description:** Add TakeProfitManager to GridManager
**Acceptance Criteria:**
- Initialize TakeProfitManager in GridManager constructor
- Pass configuration to TakeProfitManager
- Wire up order fill events to TakeProfitManager
- Wire up order update events to TakeProfitManager

### Phase 2.2: Order Fill Integration

#### Task 2.2.1: Modify handleOrderFill to Trigger Take Profit
**File:** `internal/farming/grid_manager.go` - `handleOrderFill()`
**Description:** When grid order is filled, trigger take profit order placement
**Acceptance Criteria:**
- After order fill tracking, call TakeProfitManager.PlaceTakeProfitOrder()
- Pass filled order details (symbol, side, price, size)
- Log take profit order placement
- Handle errors gracefully (continue without take profit if placement fails)

#### Task 2.2.2: Modify OnOrderUpdate to Monitor Take Profit Fills
**File:** `internal/farming/grid_manager.go` - `OnOrderUpdate()`
**Description:** Monitor take profit order fill status
**Acceptance Criteria:**
- Check if updated order is a take profit order
- If filled, trigger TakeProfitManager.HandleTakeProfitFill()
- Record profit amount
- Trigger grid rebalance
- Log take profit fill event

### Phase 2.3: Grid Rebalance Integration

#### Task 2.3.1: Add Rebalance Trigger After Take Profit Fill
**File:** `internal/farming/adaptive_grid/take_profit_manager.go`
**Description:** When take profit is filled, trigger grid rebalance
**Acceptance Criteria:**
- After recording profit, call GridManager.enqueuePlacement()
- Ensure rebalance respects all existing risk checks
- Log rebalance trigger
- Handle errors gracefully

### Phase 2.4: Timeout Handling

#### Task 2.4.1: Implement Take Profit Timeout Check
**File:** `internal/farming/adaptive_grid/take_profit_manager.go`
**Description:** Periodic check for take profit order timeouts
**Acceptance Criteria:**
- Run ticker every 5 seconds to check for expired take profit orders
- If take profit order is expired (timeout exceeded), close position by market order
- Trigger grid rebalance after position close
- Log timeout events
- Handle errors gracefully

#### Task 2.4.2: Add Timeout Check to Main Loop
**File:** `internal/farming/adaptive_grid/manager.go` - main event loop
**Description:** Add timeout check ticker to main loop
**Acceptance Criteria:**
- Add ticker for take profit timeout checks
- Call TakeProfitManager.CheckTimeouts() on each tick
- Ensure ticker is stopped on shutdown

### Phase 2.5: Configuration Management

#### Task 2.5.1: Load MicroProfitConfig from YAML
**File:** `internal/config/micro_profit_config.go`
**Description:** Load micro profit configuration from YAML file
**Acceptance Criteria:**
- Read from `config/micro_profit.yaml`
- Parse and validate configuration
- Provide default values if file doesn't exist
- Support hot-reload (watch file for changes)

#### Task 2.5.2: Wire Config to TakeProfitManager
**File:** `internal/farming/adaptive_grid/manager.go` - constructor
**Description:** Pass configuration to TakeProfitManager
**Acceptance Criteria:**
- Load MicroProfitConfig during initialization
- Pass to TakeProfitManager constructor
- Support config updates without restart

### Phase 2.6: Metrics & Monitoring

#### Task 2.6.1: Add Micro Profit Metrics
**File:** `internal/farming/adaptive_grid/take_profit_manager.go`
**Description:** Track micro profit metrics
**Acceptance Criteria:**
- Track total micro profit generated
- Track take profit success rate
- Track average position holding time
- Track timeout rate
- Provide metrics via GetMicroProfitMetrics() method

#### Task 2.6.2: Add Metrics to Dashboard
**File:** `internal/farming/volume_farm_engine.go`
**Description:** Display micro profit metrics in dashboard
**Acceptance Criteria:**
- Call TakeProfitManager.GetMicroProfitMetrics() periodically
- Log metrics to dashboard
- Include in health check

### Phase 2.7: Testing

#### Task 2.7.1: Unit Tests for TakeProfitManager
**File:** `internal/farming/adaptive_grid/take_profit_manager_test.go`
**Description:** Test take profit manager logic
**Acceptance Criteria:**
- Test take profit order placement
- Test take profit order fill handling
- Test timeout handling
- Test profit calculation
- Test error handling

#### Task 2.7.2: Integration Test for Full Flow
**File:** `internal/farming/adaptive_grid/micro_profit_integration_test.go`
**Description:** Test full micro profit flow
**Acceptance Criteria:**
- Test grid order fill → take profit placement → take profit fill → rebalance
- Test timeout flow
- Test multiple positions
- Test configuration changes

#### Task 2.7.3: Simulation Test
**File:** `internal/farming/adaptive_grid/micro_profit_simulation_test.go`
**Description:** Test with simulated market data
**Acceptance Criteria:**
- Test with realistic price movements
- Measure profit generation rate
- Measure position holding time reduction
- Validate success criteria

## Phase 3: Polish & Cross-Cutting

### Task 3.1: Error Handling & Logging
**Description:** Ensure comprehensive error handling and logging
**Acceptance Criteria:**
- All errors are logged with context
- System continues gracefully on failures
- Debug logging for troubleshooting
- Performance logging for optimization

### Task 3.2: Performance Optimization
**Description:** Optimize take profit order placement speed
**Acceptance Criteria:**
- Take profit order placed within 100ms of fill
- No blocking operations in main loop
- Efficient data structures for tracking

### Task 3.3: Documentation
**Description:** Update project documentation
**Acceptance Criteria:**
- Update README with micro profit feature
- Add configuration guide
- Add troubleshooting guide
- Update API documentation if applicable

## Dependencies

**Phase Dependencies:**
- Phase 2.1 must complete before Phase 2.2 (core logic before integration)
- Phase 2.2 must complete before Phase 2.3 (fill handling before rebalance)
- Phase 2.5 can run in parallel with Phase 2.2-2.4 (config is independent)

**External Dependencies:**
- Exchange API (ReduceOnly support)
- Configuration files (micro_profit.yaml)
- Existing grid infrastructure

## Parallel Execution Examples

**Parallel Opportunities:**
- Tasks 2.1.1, 2.1.2, 2.1.3 can run in parallel (different files)
- Tasks 2.2.1 and 2.2.2 can run in parallel (different event handlers)
- Tasks 2.5.1 and 2.7.1 can run in parallel (config vs testing)

**Sequential Requirements:**
- Task 2.1.4 must wait for 2.1.1-2.1.3 to complete
- Task 2.3.1 must wait for 2.2.2 to complete
- Task 2.4.2 must wait for 2.4.1 to complete

## Implementation Strategy

**Incremental Approach:**
1. Start with core take profit logic (Phase 2.1)
2. Integrate with existing order fill handling (Phase 2.2)
3. Add timeout handling (Phase 2.4)
4. Add configuration and metrics (Phase 2.5-2.6)
5. Test and polish (Phase 2.7-3.3)

**Risk Mitigation:**
- Feature flag to disable micro profit if issues arise
- Comprehensive error handling to prevent system crashes
- Extensive testing before production deployment
- Gradual rollout with monitoring

**Success Validation:**
- Unit tests pass
- Integration tests pass
- Simulation tests meet success criteria
- Manual testing in staging environment
- Production monitoring for 1 week before full rollout

## Progress

- [ ] Phase 0: Research
- [ ] Phase 1: Design & Contracts
- [ ] Phase 2.1: Core Take Profit Logic
- [ ] Phase 2.2: Order Fill Integration
- [ ] Phase 2.3: Grid Rebalance Integration
- [ ] Phase 2.4: Timeout Handling
- [ ] Phase 2.5: Configuration Management
- [ ] Phase 2.6: Metrics & Monitoring
- [ ] Phase 2.7: Testing
- [ ] Phase 3: Polish & Cross-Cutting

