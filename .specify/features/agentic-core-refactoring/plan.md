# Agentic Core Logic Refactoring - Implementation Plan

## Technical Context

### Current Architecture
- **State Machine**: Discrete states (IDLE, ENTER_GRID, TRADING, OVER_SIZE, DEFENSIVE, RECOVERY, EXIT_HALF, EXIT_ALL, WAIT_NEW_RANGE)
- **Decision Logic**: Fixed state-based blocking in CanPlaceOrder
- **Thresholds**: Hardcoded values (0.8, 0.6, 15s timeout)
- **Position Sizing**: Fixed per regime (ranging $5, trending $2, volatile $3)
- **Grid Geometry**: Fixed spread and order count per regime
- **Market Evaluation**: MarketConditionEvaluator with fixed thresholds
- **CircuitBreaker**: Binary can/cannot trade decision
- **Regrid Logic**: Only from IDLE/WAIT_NEW_RANGE states

### Technology Stack
- **Language**: Go 1.21+
- **Trading**: Binance Futures API
- **State Management**: Custom state machine (state_machine.go)
- **Market Data**: WebSocket for real-time klines, positions, orders
- **Logging**: Zap structured logging
- **Configuration**: YAML-based config (agentic-vf-config.yaml)

### Dependencies
- **Internal**:
  - `internal/farming/grid_manager.go` - Grid order placement
  - `internal/farming/adaptive_grid/manager.go` - Adaptive state management
  - `internal/farming/adaptive_grid/state_machine.go` - State machine logic
  - `internal/farming/adaptive_grid/market_condition_evaluator.go` - Market evaluation
  - `internal/agentic/circuit_breaker.go` - Circuit breaker logic
- **External**:
  - Binance Futures API for trading
  - WebSocket for real-time data

### Integration Points
- **GridManager** ↔ **AdaptiveGridManager**: Order placement decisions
- **MarketConditionEvaluator** ↔ **RangeDetector**: Market data
- **CircuitBreaker** ↔ **AdaptiveGridManager**: Trading mode decisions
- **State Machine** ↔ **All components**: State coordination

## Constitution Check

### Applicable Principles
1. **Risk First**: All changes must maintain or improve risk management
2. **No Hardcoded Values**: Replace hardcoded thresholds with configurable/adaptive values
3. **Continuous Trading**: Bot should never be fully blocked (MICRO mode always available)
4. **Dynamic Adaptation**: Parameters must adapt to real-time conditions
5. **Fail-Safe**: Always have fallback mechanisms

### Constitution Compliance
- ✅ All changes maintain risk management (graduated modes reduce risk)
- ✅ No hardcoded values (adaptive thresholds, dynamic parameters)
- ✅ Continuous trading (MICRO mode always available)
- ✅ Dynamic adaptation (real-time optimization)
- ✅ Fail-safe mechanisms (timeout, fallback logic)

## Phase 0: Foundation (US1-3)

### US1: Replace State Blocking with Conditional Blocking

**Implementation Approach**:
1. Create `ConditionBlocker` struct in `adaptive_grid/condition_blocker.go`
2. Implement multi-dimensional scoring:
   - Position size score (0-1)
   - Volatility score (0-1)
   - Risk score (0-1)
   - Trend score (0-1)
   - Skew score (-1 to 1)
3. Calculate blocking factor:
   - `blockFactor = weighted_sum(scores)`
   - Returns 0-1 (0 = full block, 1 = no block)
4. Modify `CanPlaceOrder`:
   - Replace state checks with condition checks
   - Apply blocking factor to order size
   - Always allow MICRO mode (10% size)
5. Add configuration for:
   - Score weights
   - Blocking thresholds
   - MICRO mode size

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/condition_blocker.go` (new)
- `backend/internal/farming/adaptive_grid/manager.go` (CanPlaceOrder)
- `backend/config/agentic-vf-config.yaml` (add condition blocking config)

**Testing Strategy**:
- Unit tests for ConditionBlocker scoring
- Integration tests for CanPlaceOrder with conditions
- Simulation tests for various market conditions

---

### US2: Implement Continuous State Space

**Implementation Approach**:
1. Create `ContinuousState` struct in `adaptive_grid/continuous_state.go`
2. Define state dimensions:
   - PositionSize: float64 (0-1)
   - Volatility: float64 (0-1)
   - Risk: float64 (0-1)
   - Trend: float64 (0-1)
   - Skew: float64 (-1 to 1)
3. Implement state space tracking:
   - Update every kline (1s)
   - Smooth transitions (exponential moving average)
4. Modify state machine:
   - Keep discrete states for compatibility
   - Add continuous state as additional context
   - Use continuous state for decision making
5. Add visualization:
   - Dashboard state space plot
   - Real-time state tracking UI

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/continuous_state.go` (new)
- `backend/internal/farming/adaptive_grid/state_machine.go` (add continuous state)
- `backend/internal/farming/adaptive_grid/manager.go` (use continuous state)
- `backend/dashboard/metrics_streamer.go` (stream continuous state)

**Testing Strategy**:
- Unit tests for state space calculations
- Integration tests for state transitions
- Visualization tests

---

### US3: Add Adaptive Thresholds

**Implementation Approach**:
1. Create `AdaptiveThresholdManager` in `adaptive_grid/adaptive_thresholds.go`
2. Implement threshold dimensions:
   - Symbol-specific thresholds
   - Regime-specific thresholds
   - Performance-based thresholds
   - Time-based thresholds
   - Funding-based thresholds
3. Implement threshold learning:
   - Track performance vs threshold values
   - Adjust thresholds based on performance
   - A/B test different thresholds
4. Modify MarketConditionEvaluator:
   - Use adaptive thresholds instead of fixed
   - Log threshold changes
5. Add configuration:
   - Initial threshold values
   - Adaptation rate
   - Learning parameters

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/adaptive_thresholds.go` (new)
- `backend/internal/farming/adaptive_grid/market_condition_evaluator.go` (use adaptive thresholds)
- `backend/config/agentic-vf-config.yaml` (add adaptive threshold config)

**Testing Strategy**:
- Unit tests for threshold adaptation
- Backtesting with different threshold strategies
- Performance comparison (fixed vs adaptive)

## Phase 1: High-Impact Improvements (US5-8)

### US5: Implement Dynamic Position Sizing

**Implementation Approach**:
1. Enhance `RiskMonitor` in `adaptive_grid/risk_sizing.go`
2. Implement sizing factors:
   - Base size (equity, leverage, max position)
   - Risk adjustment (drawdown, consecutive losses, PnL)
   - Opportunity adjustment (spread, depth, funding)
   - Liquidity adjustment (order book depth, volume)
3. Implement Kelly Criterion:
   - Calculate win rate, avg win, avg loss
   - Calculate Kelly percentage
   - Apply Kelly fraction
4. Implement consecutive loss decay:
   - Track consecutive losses
   - Reduce size per loss
   - Recover size per win
5. Modify order placement:
   - Calculate dynamic size before each order
   - Log size calculations
6. Add configuration:
   - Kelly fraction
   - Loss decay rate
   - Win recovery rate
   - Size limits (min/max)

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/risk_sizing.go` (enhance)
- `backend/internal/farming/grid_manager.go` (use dynamic sizing)
- `backend/config/agentic-vf-config.yaml` (add dynamic sizing config)

**Testing Strategy**:
- Unit tests for sizing calculations
- Backtesting with dynamic vs fixed sizing
- Risk analysis (drawdown, volatility of returns)

---

### US7: Make Regrid Logic State-Agnostic

**Implementation Approach**:
1. Modify `isReadyForRegrid` in `adaptive_grid/manager.go`
2. Remove state restriction:
   - Delete `if state != GridStateIdle && state != GridStateWaitNewRange`
3. Add state-specific criteria:
   - OVER_SIZE: position ≤ 60% + ADX < threshold + BB < threshold
   - DEFENSIVE: BB < threshold + ADX < threshold
   - RECOVERY: PnL ≥ 0 + stable X minutes
   - EXIT_HALF: PnL ≥ 0
   - EXIT_ALL: position = 0 OR dynamic timeout
4. Implement dynamic timeout:
   - Timeout = f(PnL, volatility, time, regime)
   - Profitable: longer timeout
   - Loss: shorter timeout
5. Add configuration:
   - State-specific thresholds
   - Timeout parameters

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/manager.go` (isReadyForRegrid)
- `backend/config/agentic-vf-config.yaml` (add regrid config)

**Testing Strategy**:
- Unit tests for regrid criteria
- Integration tests for regrid from different states
- Simulation tests for timeout behavior

---

### US8: Implement Conditional State Transitions

**Implementation Approach**:
1. Enhance `CanTransition` in `adaptive_grid/state_machine.go`
2. Add conditional transitions:
   - Check combined conditions for transition validity
   - Allow OVER_SIZE → DEFENSIVE if position large AND volatility high
   - Allow DEFENSIVE → RECOVERY if loss during defensive
   - Allow RECOVERY → OVER_SIZE if position grows
3. Implement emergency transitions:
   - Direct to EXIT_ALL if multiple risks high
   - Skip intermediate states for rapid changes
4. Implement state merging:
   - Allow OVER_SIZE + DEFENSIVE coexistence
   - Apply both state behaviors
5. Add transition confidence:
   - Calculate confidence based on condition strength
   - Only transition if confidence > threshold
6. Add configuration:
   - Transition thresholds
   - Confidence thresholds
   - Emergency conditions

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/state_machine.go` (CanTransition, Transition)
- `backend/config/agentic-vf-config.yaml` (add transition config)

**Testing Strategy**:
- Unit tests for conditional transitions
- Integration tests for state merging
- Simulation tests for emergency transitions

## Phase 2: Significant Improvements (US4, US6, US9, US10)

### US4: Implement Graduated Trading Modes

**Implementation Approach**:
1. Modify `CircuitBreaker` in `agentic/circuit_breaker.go`
2. Define modes:
   - FULL: 100% size, all strategies
   - REDUCED: 50% size, conservative strategies
   - MICRO: 10% size, micro-profit strategies
   - PAUSED: 0% size, no trading
3. Implement mode transitions:
   - Mode = f(risk, volatility, drawdown, losses, funding)
   - Smooth transitions (min 5 min per mode)
4. Implement mode-specific parameters:
   - FULL: normal spread, normal orders, normal size
   - REDUCED: wider spread, fewer orders, reduced size
   - MICRO: ultra-tight spread, many orders, tiny size
5. Modify `CanPlaceOrder`:
   - Apply mode-specific size multiplier
   - MICRO mode always allowed
6. Add configuration:
   - Mode thresholds
   - Mode parameters
   - Transition cooldown

**Files to Modify**:
- `backend/internal/agentic/circuit_breaker.go` (add modes)
- `backend/internal/farming/adaptive_grid/manager.go` (CanPlaceOrder)
- `backend/config/agentic-vf-config.yaml` (add mode config)

**Testing Strategy**:
- Unit tests for mode transitions
- Integration tests for mode-specific behavior
- Performance analysis by mode

---

### US6: Implement Adaptive Grid Geometry

**Implementation Approach**:
1. Create `AdaptiveGridGeometry` in `adaptive_grid/adaptive_grid_geometry.go`
2. Implement spread calculation:
   - Spread = f(volatility, skew, funding, time)
   - Volatility bands: low/normal/high/extreme
3. Implement order count calculation:
   - Orders = f(depth, risk, regime)
   - Depth-based: high depth → more orders
4. Implement skew-based asymmetry:
   - Long skew → more sells, tighter buy spread
   - Short skew → more buys, tighter sell spread
5. Modify `placeGridOrders`:
   - Use adaptive spread
   - Use adaptive order count
   - Apply asymmetry
6. Implement real-time rebalancing:
   - Rebuild when conditions change significantly
   - Smart rebuild (only changed levels)
7. Add configuration:
   - Volatility bands
   - Depth thresholds
   - Skew sensitivity

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go` (new)
- `backend/internal/farming/grid_manager.go` (placeGridOrders)
- `backend/config/agentic-vf-config.yaml` (add geometry config)

**Testing Strategy**:
- Unit tests for geometry calculations
- Backtesting with adaptive vs fixed geometry
- Fill rate analysis

---

### US9: Implement Dynamic Exit Logic

**Implementation Approach**:
1. Modify EXIT_ALL logic in `adaptive_grid/manager.go`
2. Implement dynamic timeout:
   - Timeout = f(PnL, volatility, time, regime)
   - Profitable: up to 60s
   - Loss: as low as 5s
3. Implement graduated exits:
   - 25%, 50%, 75%, 100% exit options
   - Choose based on conditions
4. Implement recovery probability:
   - Estimate based on conditions
   - Only force exit if probability < threshold
5. Add configuration:
   - Timeout parameters
   - Exit thresholds
   - Recovery probability model

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/manager.go` (EXIT_ALL logic)
- `backend/config/agentic-vf-config.yaml` (add exit config)

**Testing Strategy**:
- Unit tests for timeout calculation
- Simulation tests for exit behavior
- PnL analysis (dynamic vs fixed exit)

---

### US10: Implement Real-Time Parameter Optimization

**Implementation Approach**:
1. Create `RealTimeOptimizer` in `adaptive_grid/realtime_optimizer.go`
2. Implement parameter recalculation:
   - Every kline (1s)
   - Optimal spread, orders, size, mode
3. Implement multi-objective optimization:
   - Maximize profit, minimize risk, maximize volume, minimize drawdown
   - Pareto frontier exploration
4. Implement parameter smoothing:
   - No sudden jumps
   - Gradual adjustment
5. Integrate with existing components:
   - Apply optimized parameters
   - Log parameter changes
6. Add configuration:
   - Optimization weights
   - Smoothing parameters
   - Update frequency

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/realtime_optimizer.go` (new)
- `backend/internal/farming/adaptive_grid/manager.go` (apply optimized params)
- `backend/config/agentic-vf-config.yaml` (add optimizer config)

**Testing Strategy**:
- Unit tests for optimization
- Backtesting with real-time optimization
- Performance comparison (optimized vs fixed)

## Phase 3: Advanced Features (US11-12)

### US11: Implement Learning/Adaptation Mechanism

**Implementation Approach**:
1. Create `LearningEngine` in `adaptive_grid/learning_engine.go`
2. Implement performance tracking:
   - Condition → Strategy → Performance mapping
   - Historical database
3. Implement threshold adaptation:
   - Adjust based on recent performance
   - A/B testing
4. Implement symbol-specific learning:
   - Different optimal parameters per symbol
   - Adapt over time
5. Implement reinforcement learning (optional):
   - Agent learns optimal policy
   - Reward = profit - risk_penalty
6. Add configuration:
   - Learning parameters
   - Adaptation rate
   - Performance tracking window

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/learning_engine.go` (new)
- `backend/internal/farming/adaptive_grid/adaptive_thresholds.go` (integrate learning)
- `backend/config/agentic-vf-config.yaml` (add learning config)

**Testing Strategy**:
- Unit tests for learning algorithms
- Backtesting with learning enabled
- Performance tracking over time

---

### US12: Implement Micro-Profit Hunter Mode

**Implementation Approach**:
1. Create `MicroProfitHunter` in `adaptive_grid/micro_profit_hunter.go`
2. Implement micro-arbitrage detection:
   - Cross-exchange (if applicable)
   - Funding rate arbitrage
   - Price inefficiencies
   - Order book imbalances
3. Implement ultra-fast execution:
   - 10-30 second positions
   - Ultra-tight spreads (0.01-0.02%)
   - 1-5% size
4. Run in parallel with main strategy:
   - Separate execution path
   - Independent risk management
5. Add configuration:
   - Micro-profit parameters
   - Arbitrage thresholds
   - Size limits

**Files to Modify**:
- `backend/internal/farming/adaptive_grid/micro_profit_hunter.go` (new)
- `backend/internal/farming/grid_manager.go` (integrate micro hunter)
- `backend/config/agentic-vf-config.yaml` (add micro-profit config)

**Testing Strategy**:
- Unit tests for arbitrage detection
- Simulation tests for micro-profit hunting
- Profit analysis (micro-profit vs main strategy)

## Testing Strategy

### Unit Testing
- All new components: ConditionBlocker, ContinuousState, AdaptiveThresholds, etc.
- Threshold calculations
- State space transitions
- Dynamic sizing calculations
- Grid geometry calculations

### Integration Testing
- CanPlaceOrder with conditional blocking
- State machine with continuous state
- Regrid from all states
- Graduated mode transitions
- Real-time parameter application

### Backtesting
- Compare fixed vs adaptive thresholds
- Compare fixed vs dynamic sizing
- Compare fixed vs adaptive grid geometry
- Compare binary vs graduated modes
- Compare fixed vs dynamic exit

### Simulation Testing
- Various market conditions (crash, spike, ranging, trending)
- Rapid state changes
- Emergency transitions
- Timeout behavior

### Performance Testing
- Win rate improvement
- Drawdown reduction
- Volume maintenance
- Latency of real-time optimization

## Risk Mitigation

### Rollback Plan
- Keep existing state machine logic as fallback
- Feature flags to enable/disable new features
- Gradual rollout (symbol by symbol)
- Monitoring and alerting

### Safety Mechanisms
- Maximum parameter change rate
- Minimum size floor (never go below 1%)
- Maximum size cap (never exceed configured max)
- Emergency stop button
- Circuit breaker override

### Monitoring
- Real-time state space visualization
- Parameter change logging
- Performance tracking
- Alert on abnormal behavior

## Success Metrics

### Quantitative
- Win rate: +25-35% (MVP), +35-50% (Full)
- Drawdown: -60-70% (MVP), -70-80% (Full)
- Volume: maintained or increased
- State stuck time: < 15s max
- Trading uptime: > 99% (MICRO mode always available)

### Qualitative
- Bot adapts to any market condition
- Bot hunts micro-profit continuously
- No hardcoded values
- All parameters dynamic
- Continuous learning and improvement

## Timeline

### Phase 0 (Foundation): 2-3 weeks
- US1: 1 week
- US2: 1 week
- US3: 1 week

### Phase 1 (High-Impact): 3-4 weeks
- US5: 1.5 weeks
- US7: 1 week
- US8: 1.5 weeks

### Phase 2 (Significant): 4-5 weeks
- US4: 1.5 weeks
- US6: 1.5 weeks
- US9: 1 week
- US10: 1 week

### Phase 3 (Advanced): 3-4 weeks
- US11: 2 weeks
- US12: 1-2 weeks

**Total**: 12-16 weeks for full implementation

## Next Steps

1. Review and approve this plan
2. Begin Phase 0 implementation (US1-3)
3. Test foundation changes thoroughly
4. Proceed to Phase 1 after foundation validated
5. Continue iteratively through all phases
