# Agentic Core Logic Refactoring - Tasks

## Phase 0: Foundation (US1-3)

### T001: Create ConditionBlocker Component
**Story**: US1 - Replace State Blocking with Conditional Blocking
**Priority**: P0
**Estimated**: 2 days

**Description**: 
Create a new component that calculates blocking factor based on multi-dimensional conditions instead of binary state blocking.

**Tasks**:
- Create `backend/internal/farming/adaptive_grid/condition_blocker.go`
- Define `ConditionBlocker` struct with score weights
- Implement `CalculateBlockingFactor()` method:
  - Input: position size, volatility, risk, trend, skew scores
  - Output: blocking factor (0-1, where 0 = full block, 1 = no block)
- Implement score normalization functions:
  - `NormalizePositionSize(positionNotional, maxPosition) float64`
  - `NormalizeVolatility(atr, bbWidth) float64`
  - `NormalizeRisk(pnl, drawdown) float64`
  - `NormalizeTrend(adx) float64`
  - `NormalizeSkew(inventory) float64`
- Add configuration struct for weights and thresholds
- Write unit tests for blocking factor calculation
- Write integration tests with mock market conditions

**Acceptance Criteria**:
- ConditionBlocker can calculate blocking factor from 5 input scores
- Blocking factor returns value between 0 and 1
- Unit tests cover edge cases (all zeros, all ones, mixed values)
- Configuration allows weight customization

**Files**:
- `backend/internal/farming/adaptive_grid/condition_blocker.go` (new)
- `backend/internal/farming/adaptive_grid/condition_blocker_test.go` (new)

---

### T002: Modify CanPlaceOrder to Use Conditional Blocking
**Story**: US1 - Replace State Blocking with Conditional Blocking
**Priority**: P0
**Estimated**: 2 days

**Description**: 
Replace hardcoded state blocking in CanPlaceOrder with condition-based blocking factor and always allow MICRO mode.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Remove hardcoded state checks (OVER_SIZE, DEFENSIVE, RECOVERY, EXIT_HALF, EXIT_ALL)
  - Add ConditionBlocker integration
  - Calculate blocking factor from current conditions
  - Apply blocking factor to order size (size *= blockingFactor)
  - Implement MICRO mode check (always allow if blockingFactor > 0.1)
  - Add logging for blocking factor and size reduction
- Add MICRO mode configuration (size multiplier: 0.1)
- Update `backend/config/agentic-vf-config.yaml`:
  - Add condition blocking section
  - Add score weights configuration
  - Add MICRO mode configuration
- Write integration tests for CanPlaceOrder with conditions
- Test MICRO mode always available
- Test size reduction based on blocking factor

**Acceptance Criteria**:
- CanPlaceOrder no longer blocks based on state name
- Blocking factor calculated from real-time conditions
- MICRO mode (10% size) always allowed
- Size reduced by blocking factor when conditions adverse
- Logs show blocking factor and size calculations

**Files**:
- `backend/internal/farming/adaptive_grid/manager.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

**Dependencies**: T001

---

### T003: Create ContinuousState Component
**Story**: US2 - Implement Continuous State Space
**Priority**: P0
**Estimated**: 2 days

**Description**: 
Create a continuous multi-dimensional state representation to replace or augment discrete states.

**Tasks**:
- Create `backend/internal/farming/adaptive_grid/continuous_state.go`
- Define `ContinuousState` struct:
  - PositionSize float64 (0-1)
  - Volatility float64 (0-1)
  - Risk float64 (0-1)
  - Trend float64 (0-1)
  - Skew float64 (-1 to 1)
  - Timestamp time.Time
- Implement `UpdateContinuousState()` method:
  - Calculate each dimension from current market data
  - Apply exponential moving average for smoothing
  - Update timestamp
- Implement dimension calculation functions:
  - `CalculatePositionSize(position, maxPosition) float64`
  - `CalculateVolatility(atr, bbWidth) float64`
  - `CalculateRisk(pnl, drawdown) float64`
  - `CalculateTrend(adx) float64`
  - `CalculateSkew(inventory) float64`
- Add smoothing parameter (alpha for EMA)
- Write unit tests for state calculations
- Write unit tests for smoothing behavior

**Acceptance Criteria**:
- ContinuousState tracks 5 dimensions
- Each dimension normalized to appropriate range
- Smoothing applied to prevent sudden jumps
- Unit tests cover all dimension calculations

**Files**:
- `backend/internal/farming/adaptive_grid/continuous_state.go` (new)
- `backend/internal/farming/adaptive_grid/continuous_state_test.go` (new)

---

### T004: Integrate ContinuousState with State Machine
**Story**: US2 - Implement Continuous State Space
**Priority**: P0
**Estimated**: 2 days

**Description**: 
Integrate continuous state into existing state machine for enhanced decision making.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/state_machine.go`:
  - Add ContinuousState field to SymbolState
  - Add `GetContinuousState(symbol) *ContinuousState` method
  - Add `UpdateContinuousState(symbol, state) *ContinuousState` method
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Update continuous state every kline in UpdatePriceForRange
  - Use continuous state for decision making alongside discrete state
  - Add continuous state logging
- Modify `backend/dashboard/metrics_streamer.go`:
  - Stream continuous state dimensions
  - Add to dashboard metrics
- Write integration tests for state machine with continuous state
- Test continuous state updates every kline

**Acceptance Criteria**:
- State machine tracks continuous state alongside discrete state
- Continuous state updated every kline
- Continuous state available for decision making
- Dashboard streams continuous state dimensions

**Files**:
- `backend/internal/farming/adaptive_grid/state_machine.go` (modify)
- `backend/internal/farming/adaptive_grid/manager.go` (modify)
- `backend/dashboard/metrics_streamer.go` (modify)

**Dependencies**: T003

---

### T005: Create AdaptiveThresholdManager
**Story**: US3 - Add Adaptive Thresholds
**Priority**: P0
**Estimated**: 3 days

**Description**: 
Create a manager for adaptive thresholds that adjust based on symbol, regime, performance, time, and funding.

**Tasks**:
- Create `backend/internal/farming/adaptive_grid/adaptive_thresholds.go`
- Define `AdaptiveThresholdManager` struct:
  - Symbol thresholds map
  - Regime thresholds map
  - Performance history
  - Adaptation parameters
- Implement threshold calculation methods:
  - `GetThreshold(symbol, regime, dimension) float64`
  - `CalculateSymbolThreshold(symbol, baseThreshold) float64`
  - `CalculateRegimeThreshold(regime, baseThreshold) float64`
  - `CalculatePerformanceThreshold(symbol, baseThreshold) float64`
  - `CalculateTimeThreshold(time, baseThreshold) float64`
  - `CalculateFundingThreshold(funding, baseThreshold) float64`
- Implement threshold learning:
  - `UpdateThreshold(symbol, dimension, value, performance)`
  - `AdaptThresholdsBasedOnPerformance()`
- Add configuration for initial thresholds and adaptation rate
- Write unit tests for threshold calculations
- Write unit tests for threshold adaptation

**Acceptance Criteria**:
- AdaptiveThresholdManager calculates thresholds from 5 dimensions
- Thresholds adapt based on performance
- Configuration allows initial values and adaptation rate
- Unit tests cover all threshold calculations

**Files**:
- `backend/internal/farming/adaptive_grid/adaptive_thresholds.go` (new)
- `backend/internal/farming/adaptive_grid/adaptive_thresholds_test.go` (new)

---

### T006: Integrate Adaptive Thresholds with MarketConditionEvaluator
**Story**: US3 - Add Adaptive Thresholds
**Priority**: P0
**Estimated**: 2 days

**Description**: 
Replace fixed thresholds in MarketConditionEvaluator with adaptive thresholds.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/market_condition_evaluator.go`:
  - Add AdaptiveThresholdManager field
  - Replace fixed thresholds (0.8, 0.6) with adaptive thresholds
  - Call `GetThreshold()` for each dimension check
  - Log threshold values used
  - Add setter for AdaptiveThresholdManager
- Modify `backend/internal/farming/volume_farm_engine.go`:
  - Initialize AdaptiveThresholdManager
  - Set on MarketConditionEvaluator
- Add configuration to `backend/config/agentic-vf-config.yaml`:
  - Add adaptive thresholds section
  - Add initial threshold values
  - Add adaptation parameters
- Write integration tests for evaluator with adaptive thresholds
- Test threshold adaptation over time

**Acceptance Criteria**:
- MarketConditionEvaluator uses adaptive thresholds
- Thresholds vary by symbol, regime, time, funding
- Thresholds adapt based on performance
- Logs show threshold values used

**Files**:
- `backend/internal/farming/adaptive_grid/market_condition_evaluator.go` (modify)
- `backend/internal/farming/volume_farm_engine.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

**Dependencies**: T005

---

## Phase 1: High-Impact Improvements (US5-8)

### T007: Enhance Dynamic Position Sizing
**Story**: US5 - Implement Dynamic Position Sizing
**Priority**: P1
**Estimated**: 3 days

**Description**: 
Enhance RiskMonitor to calculate dynamic position size based on risk, opportunity, and liquidity factors.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/risk_sizing.go`:
  - Add sizing factor methods:
    - `CalculateBaseSize(equity, leverage, maxPosition) float64`
    - `CalculateRiskAdjustment(drawdown, consecutiveLosses, pnl) float64`
    - `CalculateOpportunityAdjustment(spread, depth, funding) float64`
    - `CalculateLiquidityAdjustment(orderBookDepth, volume) float64`
  - Implement Kelly Criterion:
    - `CalculateKelly(winRate, avgWin, avgLoss) float64`
    - `CalculateKellySize(kellyFraction, equity, leverage) float64`
  - Implement consecutive loss decay:
    - Track consecutive losses
    - `ApplyLossDecay(baseSize, consecutiveLosses) float64`
    - `ApplyWinRecovery(baseSize, consecutiveWins) float64`
  - Enhance `CalculateOrderSize()` to use all factors
  - Add logging for size calculation breakdown
- Add configuration:
  - Kelly fraction
  - Loss decay rate
  - Win recovery rate
  - Size limits (min/max)
- Write unit tests for sizing calculations
- Write backtest comparing dynamic vs fixed sizing

**Acceptance Criteria**:
- Order size calculated from 5 factors (base, risk, opportunity, liquidity, Kelly)
- Kelly Criterion implemented with configurable fraction
- Consecutive loss decay reduces size
- Win recovery increases size
- Size respects min/max limits
- Logs show size calculation breakdown

**Files**:
- `backend/internal/farming/adaptive_grid/risk_sizing.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

---

### T008: Integrate Dynamic Sizing with GridManager
**Story**: US5 - Implement Dynamic Position Sizing
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Integrate dynamic sizing into GridManager order placement.

**Tasks**:
- Modify `backend/internal/farming/grid_manager.go`:
  - Call `GetSmartOrderSize()` or enhanced `CalculateOrderSize()` before placing orders
  - Apply dynamic size in `placeGridOrders()`
  - Apply dynamic size in `placeMicroGridOrders()`
  - Log size used for each order
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Ensure RiskMonitor has access to performance data for Kelly
  - Track win rate, avg win, avg loss
- Write integration tests for dynamic sizing
- Test size reduction after consecutive losses
- Test size recovery after consecutive wins

**Acceptance Criteria**:
- GridManager uses dynamic size for all orders
- Size varies based on conditions
- Size reduces after losses
- Size recovers after wins
- Logs show dynamic size values

**Files**:
- `backend/internal/farming/grid_manager.go` (modify)
- `backend/internal/farming/adaptive_grid/manager.go` (modify)

**Dependencies**: T007

---

### T009: Remove State Restriction from isReadyForRegrid
**Story**: US7 - Make Regrid Logic State-Agnostic
**Priority**: P1
**Estimated**: 1 day

**Description**: 
Remove the restriction that only IDLE and WAIT_NEW_RANGE states can regrid.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Remove `if state != GridStateIdle && state != GridStateWaitNewRange` check in `isReadyForRegrid()`
  - Add state-specific regrid criteria:
    - OVER_SIZE: position ≤ 60% max + ADX < threshold + BB < threshold
    - DEFENSIVE: BB < threshold + ADX < threshold
    - RECOVERY: PnL ≥ 0 + stable X minutes
    - EXIT_HALF: PnL ≥ 0
    - EXIT_ALL: position = 0 OR dynamic timeout
- Add configuration for state-specific thresholds
- Write unit tests for regrid from each state
- Test regrid from OVER_SIZE when conditions met
- Test regrid from DEFENSIVE when conditions met

**Acceptance Criteria**:
- isReadyForRegrid allows regrid from any state
- Each state has specific exit conditions
- Configuration allows threshold customization
- Unit tests cover regrid from all states

**Files**:
- `backend/internal/farming/adaptive_grid/manager.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

---

### T010: Implement Dynamic EXIT_ALL Timeout
**Story**: US7 - Make Regrid Logic State-Agnostic
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Replace fixed 15s EXIT_ALL timeout with dynamic timeout based on PnL, volatility, and time.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Add `CalculateDynamicTimeout(position, volatility, time, regime) time.Duration` method
  - Implement timeout logic:
    - Profitable position: longer timeout (up to 60s)
    - Loss position: shorter timeout (as low as 5s)
    - Calm market: longer timeout
    - Volatile market: shorter timeout
  - Replace fixed 15s timeout with dynamic timeout
  - Add logging for timeout calculation
- Add configuration for timeout parameters
- Write unit tests for timeout calculation
- Test timeout varies with conditions

**Acceptance Criteria**:
- EXIT_ALL timeout varies based on conditions
- Profitable positions get longer timeout
- Loss positions get shorter timeout
- Configuration allows parameter tuning
- Logs show timeout calculation

**Files**:
- `backend/internal/farming/adaptive_grid/manager.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

**Dependencies**: T009

---

### T011: Implement Conditional State Transitions
**Story**: US8 - Implement Conditional State Transitions
**Priority**: P1
**Estimated**: 3 days

**Description**: 
Enhance state machine to allow conditional transitions based on combined conditions.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/state_machine.go`:
  - Add `CanConditionalTransition(symbol, event, conditions) bool` method
  - Implement conditional transition logic:
    - OVER_SIZE → DEFENSIVE if position large AND volatility high
    - DEFENSIVE → RECOVERY if loss during defensive
    - RECOVERY → OVER_SIZE if position grows
  - Implement emergency transitions:
    - Direct to EXIT_ALL if multiple risks high
    - Skip intermediate states for rapid changes
  - Implement state merging:
    - Allow OVER_SIZE + DEFENSIVE coexistence
    - Add `MergedStates` field to SymbolState
  - Add transition confidence calculation:
    - `CalculateTransitionConfidence(event, conditions) float64`
    - Only transition if confidence > threshold
- Modify `Transition()` to use conditional transitions
- Add configuration for transition thresholds and confidence
- Write unit tests for conditional transitions
- Test emergency transitions
- Test state merging

**Acceptance Criteria**:
- State machine allows conditional transitions
- Emergency transitions skip intermediate states
- State merging allows OVER_SIZE + DEFENSIVE
- Transition confidence calculated and applied
- Configuration allows threshold tuning

**Files**:
- `backend/internal/farming/adaptive_grid/state_machine.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

---

### T012: Integrate Conditional Transitions with Manager
**Story**: US8 - Implement Conditional State Transitions
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Integrate conditional transitions into AdaptiveGridManager's state transition logic.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Update `handleStateTransitions()` to use conditional transitions
  - Add condition checking before transitions
  - Add confidence checking before transitions
  - Log transition confidence
  - Handle merged states in decision making
- Write integration tests for conditional transitions
- Test transitions with combined conditions
- Test emergency transitions

**Acceptance Criteria**:
- Manager uses conditional transitions
- Conditions checked before transitions
- Confidence checked before transitions
- Merged states handled correctly
- Logs show transition details

**Files**:
- `backend/internal/farming/adaptive_grid/manager.go` (modify)

**Dependencies**: T011

---

## Phase 2: Significant Improvements (US4, US6, US9, US10)

### T013: Implement Graduated Trading Modes in CircuitBreaker
**Story**: US4 - Implement Graduated Trading Modes
**Priority**: P1
**Estimated**: 3 days

**Description**: 
Replace binary can/cannot trade with graduated modes (FULL/REDUCED/MICRO/PAUSED).

**Tasks**:
- Modify `backend/internal/agentic/circuit_breaker.go`:
  - Define mode constants: FULL, REDUCED, MICRO, PAUSED
  - Add mode to SymbolState
  - Implement `CalculateTradingMode(risk, volatility, drawdown, losses, funding) string` method
  - Implement mode transition logic:
    - Mode = f(conditions)
    - Minimum 5 minutes per mode
  - Add mode-specific parameter sets:
    - FULL: normal spread, normal orders, normal size
    - REDUCED: wider spread, fewer orders, reduced size
    - MICRO: ultra-tight spread, many orders, tiny size
  - Modify `GetSymbolDecision()` to return mode
- Add configuration for mode thresholds and parameters
- Write unit tests for mode calculation
- Test mode transitions
- Test mode-specific parameters

**Acceptance Criteria**:
- CircuitBreaker has 4 modes instead of binary
- Mode calculated from conditions
- Mode transitions have cooldown
- Each mode has specific parameters
- Configuration allows customization

**Files**:
- `backend/internal/agentic/circuit_breaker.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

---

### T014: Integrate Graduated Modes with CanPlaceOrder
**Story**: US4 - Implement Graduated Trading Modes
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Integrate graduated modes into CanPlaceOrder to apply mode-specific size multipliers.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Add `GetTradingMode(symbol) string` method
  - Add mode-specific size multipliers:
    - FULL: 1.0x
    - REDUCED: 0.5x
    - MICRO: 0.1x
    - PAUSED: 0x (block)
  - Modify `CanPlaceOrder()` to apply mode multiplier
  - MICRO mode always returns true (never blocks)
  - Log mode and multiplier applied
- Write integration tests for mode-specific behavior
- Test MICRO mode always allows trading
- Test REDUCED mode reduces size

**Acceptance Criteria**:
- CanPlaceOrder applies mode-specific multiplier
- MICRO mode always allows trading
- REDUCED mode reduces size by 50%
- FULL mode uses normal size
- PAUSED mode blocks trading
- Logs show mode and multiplier

**Files**:
- `backend/internal/farming/adaptive_grid/manager.go` (modify)

**Dependencies**: T013

---

### T015: Create AdaptiveGridGeometry Component
**Story**: US6 - Implement Adaptive Grid Geometry
**Priority**: P1
**Estimated**: 3 days

**Description**: 
Create component to calculate adaptive grid geometry (spread, order count, spacing) based on conditions.

**Tasks**:
- Create `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go`
- Define `AdaptiveGridGeometry` struct:
  - Volatility bands (low/normal/high/extreme)
  - Depth thresholds
  - Skew sensitivity
- Implement geometry calculation methods:
  - `CalculateSpread(volatility, skew, funding, time) float64`
  - `CalculateOrderCount(depth, risk, regime) int`
  - `CalculateSpacing(volatility, trend) float64`
  - `CalculateAsymmetry(skew) (buySpread, sellSpread, buyCount, sellCount)`
- Implement volatility band classification:
  - Low: ATR < 0.3%
  - Normal: ATR 0.3-0.8%
  - High: ATR 0.8-1.5%
  - Extreme: ATR > 1.5%
- Add configuration for geometry parameters
- Write unit tests for geometry calculations
- Test spread varies with volatility
- Test order count varies with depth

**Acceptance Criteria**:
- AdaptiveGridGeometry calculates spread, order count, spacing
- Spread varies with volatility bands
- Order count varies with depth
- Asymmetry applied based on skew
- Configuration allows parameter tuning

**Files**:
- `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go` (new)
- `backend/internal/farming/adaptive_grid/adaptive_grid_geometry_test.go` (new)

---

### T016: Integrate Adaptive Grid Geometry with GridManager
**Story**: US6 - Implement Adaptive Grid Geometry
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Integrate adaptive grid geometry into GridManager order placement.

**Tasks**:
- Modify `backend/internal/farming/grid_manager.go`:
  - Add AdaptiveGridGeometry field
  - Modify `placeGridOrders()` to use adaptive spread
  - Modify `placeGridOrders()` to use adaptive order count
  - Apply asymmetry if skew present
  - Implement smart rebuild:
    - Rebuild when conditions change significantly
    - Only replace orders at changed levels
  - Log geometry calculations
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Initialize AdaptiveGridGeometry
  - Provide access to market data for geometry calculations
- Write integration tests for adaptive geometry
- Test grid rebuilds when conditions change
- Test asymmetry applied

**Acceptance Criteria**:
- GridManager uses adaptive spread and order count
- Grid rebuilds when conditions change significantly
- Asymmetry applied based on skew
- Smart rebuild only replaces changed levels
- Logs show geometry calculations

**Files**:
- `backend/internal/farming/grid_manager.go` (modify)
- `backend/internal/farming/adaptive_grid/manager.go` (modify)

**Dependencies**: T015

---

### T017: Implement Graduated Exit Options
**Story**: US9 - Implement Dynamic Exit Logic
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Add graduated exit options (25%, 50%, 75%, 100%) instead of only full exit.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Add `CalculateExitPercentage(position, volatility, time, regime) float64` method
  - Implement exit percentage logic:
    - Small loss: 25% exit
    - Medium loss: 50% exit
    - Large loss: 75% exit
    - Extreme loss: 100% exit
  - Modify EXIT_ALL logic to use graduated exit
  - Add `exitPercentage` field to state
  - Log exit percentage used
- Add configuration for exit thresholds
- Write unit tests for exit percentage calculation
- Test graduated exits

**Acceptance Criteria**:
- EXIT_ALL uses graduated exit percentages
- Exit percentage varies with conditions
- Configuration allows threshold tuning
- Logs show exit percentage

**Files**:
- `backend/internal/farming/adaptive_grid/manager.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

---

### T018: Implement Recovery Probability for Exit Decision
**Story**: US9 - Implement Dynamic Exit Logic
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Add recovery probability estimation to avoid forcing suboptimal exits.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Add `EstimateRecoveryProbability(position, volatility, trend, funding) float64` method
  - Implement recovery probability logic:
    - Based on historical recovery patterns
    - Based on current market conditions
    - Based on position PnL trajectory
  - Modify exit decision:
    - Only force exit if recovery probability < threshold
    - Allow position to continue if recovery probability high
  - Log recovery probability
- Add configuration for recovery probability model
- Write unit tests for recovery probability estimation
- Test exit decision with recovery probability

**Acceptance Criteria**:
- Recovery probability estimated from conditions
- Exit decision considers recovery probability
- High recovery probability avoids forced exit
- Configuration allows model tuning
- Logs show recovery probability

**Files**:
- `backend/internal/farming/adaptive_grid/manager.go` (modify)
- `backend/config/agentic-vf-config.yaml` (modify)

**Dependencies**: T017

---

### T019: Create RealTimeOptimizer Component
**Story**: US10 - Implement Real-Time Parameter Optimization
**Priority**: P1
**Estimated**: 4 days

**Description**: 
Create component to optimize parameters in real-time based on current conditions.

**Tasks**:
- Create `backend/internal/farming/adaptive_grid/realtime_optimizer.go`
- Define `RealTimeOptimizer` struct:
  - Optimization weights (profit, risk, volume, drawdown)
  - Pareto frontier
  - Smoothing parameters
- Implement parameter optimization methods:
  - `OptimizeSpread(volatility, skew, funding, time) float64`
  - `OptimizeOrderCount(depth, risk, regime) int`
  - `OptimizeSize(equity, risk, opportunity, liquidity) float64`
  - `OptimizeMode(risk, volatility, drawdown, losses) string`
- Implement multi-objective optimization:
  - Calculate objective scores
  - Find Pareto optimal points
  - Select operating point based on weights
- Implement parameter smoothing:
  - No sudden jumps
  - Gradual adjustment
- Add configuration for optimization weights and smoothing
- Write unit tests for optimization
- Test parameter smoothing

**Acceptance Criteria**:
- RealTimeOptimizer optimizes 4 parameters (spread, orders, size, mode)
- Multi-objective optimization implemented
- Pareto frontier explored
- Parameters smoothed to avoid jumps
- Configuration allows weight tuning

**Files**:
- `backend/internal/farming/adaptive_grid/realtime_optimizer.go` (new)
- `backend/internal/farming/adaptive_grid/realtime_optimizer_test.go` (new)

---

### T020: Integrate RealTimeOptimizer with Manager
**Story**: US10 - Implement Real-Time Parameter Optimization
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Integrate real-time optimizer into AdaptiveGridManager to apply optimized parameters.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/manager.go`:
  - Add RealTimeOptimizer field
  - Call optimizer every kline in UpdatePriceForRange
  - Apply optimized parameters:
    - Update spread
    - Update order count
    - Update size
    - Update mode
  - Log parameter changes
  - Apply smoothing to parameter changes
- Write integration tests for real-time optimization
- Test parameters update every kline
- Test parameter smoothing

**Acceptance Criteria**:
- Manager calls optimizer every kline
- Optimized parameters applied
- Parameters smoothed to avoid jumps
- Logs show parameter changes
- Configuration allows update frequency tuning

**Files**:
- `backend/internal/farming/adaptive_grid/manager.go` (modify)

**Dependencies**: T019

---

## Phase 3: Advanced Features (US11-12)

### T021: Create LearningEngine Component
**Story**: US11 - Implement Learning/Adaptation Mechanism
**Priority**: P2
**Estimated**: 5 days

**Description**: 
Create learning engine to track performance and adapt thresholds/parameters over time.

**Tasks**:
- Create `backend/internal/farming/adaptive_grid/learning_engine.go`
- Define `LearningEngine` struct:
  - Performance database
  - Threshold history
  - Learning parameters
- Implement performance tracking:
  - `RecordPerformance(condition, strategy, performance)`
  - `GetPerformance(condition, strategy) float64`
- Implement threshold adaptation:
  - `AdaptThreshold(symbol, dimension, recentPerformance)`
  - A/B testing framework
- Implement symbol-specific learning:
  - `GetOptimalThresholds(symbol) map[string]float64`
  - `UpdateOptimalThresholds(symbol, thresholds)`
- Add configuration for learning parameters
- Write unit tests for learning algorithms
- Test threshold adaptation
- Test A/B testing

**Acceptance Criteria**:
- LearningEngine tracks performance by condition and strategy
- Thresholds adapt based on recent performance
- A/B testing framework implemented
- Symbol-specific optimal thresholds learned
- Configuration allows learning rate tuning

**Files**:
- `backend/internal/farming/adaptive_grid/learning_engine.go` (new)
- `backend/internal/farming/adaptive_grid/learning_engine_test.go` (new)

---

### T022: Integrate LearningEngine with AdaptiveThresholds
**Story**: US11 - Implement Learning/Adaptation Mechanism
**Priority**: P2
**Estimated**: 2 days

**Description**: 
Integrate learning engine with adaptive thresholds for automatic threshold optimization.

**Tasks**:
- Modify `backend/internal/farming/adaptive_grid/adaptive_thresholds.go`:
  - Add LearningEngine field
  - Call learning engine for threshold adaptation
  - Apply learned thresholds
  - Log threshold changes from learning
- Write integration tests for learning integration
- Test thresholds adapt based on performance
- Test symbol-specific thresholds learned

**Acceptance Criteria**:
- AdaptiveThresholds uses LearningEngine
- Thresholds adapt based on performance
- Symbol-specific thresholds learned
- Logs show learning-driven changes

**Files**:
- `backend/internal/farming/adaptive_grid/adaptive_thresholds.go` (modify)

**Dependencies**: T021

---

### T023: Create MicroProfitHunter Component
**Story**: US12 - Implement Micro-Profit Hunter Mode
**Priority**: P2
**Estimated**: 4 days

**Description**: 
Create micro-profit hunting mode that runs in background to capture small arbitrage opportunities.

**Tasks**:
- Create `backend/internal/farming/adaptive_grid/micro_profit_hunter.go`
- Define `MicroProfitHunter` struct:
  - Arbitrage detectors
  - Execution parameters
- Implement arbitrage detection:
  - `DetectFundingArbitrage(symbol) bool`
  - `DetectPriceInefficiency(symbol) bool`
  - `DetectOrderBookImbalance(symbol) bool`
- Implement ultra-fast execution:
  - 10-30 second positions
  - Ultra-tight spreads (0.01-0.02%)
  - 1-5% size
- Implement parallel execution:
  - Separate from main strategy
  - Independent risk management
- Add configuration for micro-profit parameters
- Write unit tests for arbitrage detection
- Test ultra-fast execution

**Acceptance Criteria**:
- MicroProfitHunter detects 3 types of arbitrage
- Ultra-fast execution (10-30s positions)
- Ultra-tight spreads (0.01-0.02%)
- 1-5% size for safety
- Runs in parallel with main strategy
- Configuration allows parameter tuning

**Files**:
- `backend/internal/farming/adaptive_grid/micro_profit_hunter.go` (new)
- `backend/internal/farming/adaptive_grid/micro_profit_hunter_test.go` (new)

---

### T024: Integrate MicroProfitHunter with GridManager
**Story**: US12 - Implement Micro-Profit Hunter Mode
**Priority**: P2
**Estimated**: 2 days

**Description**: 
Integrate micro-profit hunter into GridManager for parallel execution.

**Tasks**:
- Modify `backend/internal/farming/grid_manager.go`:
  - Add MicroProfitHunter field
  - Start micro-profit hunter in background goroutine
  - Execute micro-profit orders independently
  - Track micro-profit performance separately
- Add micro-profit metrics to dashboard
- Write integration tests for micro-profit integration
- Test parallel execution
- Test independent risk management

**Acceptance Criteria**:
- MicroProfitHunter runs in background
- Micro-profit orders independent of main strategy
- Separate performance tracking
- Dashboard shows micro-profit metrics
- Independent risk management

**Files**:
- `backend/internal/farming/grid_manager.go` (modify)
- `backend/dashboard/metrics_streamer.go` (modify)

**Dependencies**: T023

---

## Testing & Validation

### T025: Comprehensive Integration Testing
**Priority**: P0
**Estimated**: 3 days

**Description**: 
Write comprehensive integration tests for all new features.

**Tasks**:
- Write integration test suite:
  - Test conditional blocking with various conditions
  - Test continuous state updates
  - Test adaptive thresholds over time
  - Test dynamic sizing scenarios
  - Test regrid from all states
  - Test conditional transitions
  - Test graduated modes
  - Test adaptive grid geometry
  - Test dynamic exit logic
  - Test real-time optimization
- Test edge cases and error conditions
- Test performance under load

**Acceptance Criteria**:
- All features have integration tests
- Edge cases covered
- Error conditions tested
- Performance validated

**Files**:
- `backend/internal/farming/adaptive_grid/agentic_integration_test.go` (new)

---

### T026: Backtesting and Performance Comparison
**Priority**: P0
**Estimated**: 5 days

**Description**: 
Run backtests comparing old vs new logic to validate improvements.

**Tasks**:
- Set up backtesting framework
- Run backtest with old logic (baseline)
- Run backtest with new logic (MVP features)
- Compare metrics:
  - Win rate
  - Drawdown
  - Volume
  - Profit
  - Sharpe ratio
- Run backtest with full features
- Compare all three scenarios
- Generate performance report

**Acceptance Criteria**:
- Backtest framework set up
- Baseline metrics recorded
- MVP metrics show improvement
- Full features metrics show further improvement
- Performance report generated

**Files**:
- `backend/internal/farming/backtest/agentic_backtest.go` (new)

---

### T027: Dashboard Visualization Updates
**Priority**: P1
**Estimated**: 3 days

**Description**: 
Update dashboard to visualize new agentic features.

**Tasks**:
- Add continuous state visualization:
  - 5-dimensional state space plot
  - Real-time state tracking
- Add adaptive threshold visualization:
  - Threshold evolution over time
  - Threshold comparison by symbol
- Add graduated mode indicator:
  - Current mode display
  - Mode transition history
- Add dynamic parameter visualization:
  - Spread, order count, size over time
  - Optimization decisions
- Add micro-profit metrics:
  - Micro-profit PnL
  - Micro-profit volume
- Add learning visualization:
  - Threshold adaptation history
  - Performance improvement over time

**Acceptance Criteria**:
- Dashboard shows continuous state
- Dashboard shows adaptive thresholds
- Dashboard shows current mode
- Dashboard shows dynamic parameters
- Dashboard shows micro-profit metrics
- Dashboard shows learning progress

**Files**:
- `backend/dashboard/metrics_streamer.go` (modify)
- `dashboard.html` (modify)

---

## Documentation

### T028: Update Configuration Documentation
**Priority**: P1
**Estimated**: 1 day

**Description**: 
Update configuration documentation with new agentic parameters.

**Tasks**:
- Update README.md with new configuration sections
- Document condition blocking parameters
- Document adaptive threshold parameters
- Document graduated mode parameters
- Document dynamic sizing parameters
- Document adaptive grid geometry parameters
- Document dynamic exit parameters
- Document real-time optimization parameters
- Document learning parameters
- Document micro-profit parameters
- Add configuration examples

**Acceptance Criteria**:
- All new parameters documented
- Examples provided
- README updated

**Files**:
- `README.md` (modify)

---

### T029: Update Architecture Documentation
**Priority**: P1
**Estimated**: 2 days

**Description**: 
Update architecture documentation to reflect agentic changes.

**Tasks**:
- Update ARCHITECTURE.md:
  - Add ConditionBlocker component
  - Add ContinuousState component
  - Add AdaptiveThresholdManager component
  - Add AdaptiveGridGeometry component
  - Add RealTimeOptimizer component
  - Add LearningEngine component
  - Add MicroProfitHunter component
- Update component diagrams
- Update data flow diagrams
- Document new decision flows
- Document integration points

**Acceptance Criteria**:
- All new components documented
- Diagrams updated
- Decision flows documented
- Integration points documented

**Files**:
- `ARCHITECTURE.md` (modify)

---

### T030: Create Migration Guide
**Priority**: P1
**Estimated**: 1 day

**Description**: 
Create guide for migrating from old logic to new agentic logic.

**Tasks**:
- Document breaking changes
- Document configuration changes required
- Provide migration steps
- Provide rollback procedure
- Document testing recommendations
- Provide monitoring recommendations

**Acceptance Criteria**:
- Migration guide created
- Breaking changes documented
- Configuration changes documented
- Migration steps provided
- Rollback procedure provided

**Files**:
- `MIGRATION_GUIDE.md` (new)

---

## Task Summary

**Total Tasks**: 30
**Phase 0 (Foundation)**: 6 tasks (T001-T006)
**Phase 1 (High-Impact)**: 6 tasks (T007-T012)
**Phase 2 (Significant)**: 8 tasks (T013-T020)
**Phase 3 (Advanced)**: 4 tasks (T021-T024)
**Testing & Validation**: 3 tasks (T025-T027)
**Documentation**: 3 tasks (T028-T030)

**Estimated Total Time**: 12-16 weeks

**Critical Path**: T001 → T002 → T007 → T008 → T025 → T026
