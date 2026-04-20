# Config Optimization Implementation Plan

## Overview

This plan addresses critical configuration and logic gaps to optimize the bot for micro profit farming and volume farming while maintaining maximum leverage (20x-100x) as requested.

**Issues to Fix:**
- Issue 1: Grid Spread Too Tight for High Leverage
- Issue 2: TP/SL Too Tight for Volatility
- ~~Issue 3: No Dynamic Leverage Adjustment~~ **SKIPPED** (user wants max leverage)
- Issue 4: No Equity Curve Position Sizing
- Issue 5: Trending Regime Too Conservative
- Issue 6: Market Evaluation (evaluateMarket) placeholder
- Issue 7: State-Specific Actions not implemented

## Technical Context

### Current Configuration Analysis

**Capital & Leverage:**
- Capital: $10
- Leverage: 20x-100x (dynamic per symbol)
- Order Size: $20.5 base, adaptive $0.5-2.0
- Margin Required: $0.20-1.03 per order (100x)
- Buffer: 60% capital for margin ($6)

**Current Grid Config (PROBLEMATIC):**
- Base Spread: 0.15%
- Ranging Spread: 0.02% (TOO TIGHT for 100x leverage)
- Trending Spread: 0.15%
- Volatile Spread: 0.1%
- Max Orders: 10/side
- TP: 0.3% (TOO TIGHT)
- SL: 0.5% (TOO TIGHT)

**Current Adaptive Regimes (PROBLEMATIC):**
- Ranging (70%): $2 size, 0.02% spread, 10 orders
- Trending (20%): $1 size, 0.15% spread, 2 orders (TOO FEW)
- Volatile (10%): $0.5 size, 0.1% spread, 4 orders

### Dependencies
- Config files: `backend/config/agentic-vf-config.yaml`, `backend/config/adaptive_config.yaml`
- Config structs: `internal/config/config.go`
- Market condition evaluator: `internal/farming/adaptive_grid/market_condition_evaluator.go`
- Grid manager: `internal/farming/grid_manager.go`
- State machine: `internal/farming/adaptive_grid/state_machine.go`

## Constitution Check

**Constitution:** NEADS CLARIFICATION - No constitution.md found in .specify/memory

## Implementation Strategy

### Phase 0: Research & Analysis (1 day)

**Task 0.1: Analyze Current Spread Impact**
- Research: Current 0.02% spread with 100x leverage = 2% price movement
- Impact: High probability of stop hunts and whipsaw losses
- Recommendation: Increase to 0.05-0.08% for ranging

**Task 0.2: Analyze TP/SL Realism**
- Research: Current 0.3% TP with 0.02% spread = 15 grid levels
- Impact: Orders rarely hit TP, accumulate stuck positions
- Recommendation: Increase TP to 0.5-0.8%, SL to 0.8-1.0%

**Task 0.3: Analyze Equity Curve Sizing Best Practices**
- Research: Kelly Criterion sizing formula
- Research: Win rate-based position sizing
- Research: Drawdown-based position reduction
- Recommendation: Implement Kelly Criterion with configurable fraction

**Task 0.4: Analyze Market Evaluation Components**
- Research: Spread impact on grid performance
- Research: Volume impact on fill rate
- Research: Funding rate impact on profitability
- Recommendation: Implement weighted evaluation of spread/volume/funding

**Output:** `.specify/research-config-optimization.md`

### Phase 1: Configuration Fixes (1 day)

**Priority:** P1 - Critical (Immediate Fixes)

#### Task 1.1: Fix Grid Spread Parameters

**File:** `backend/config/adaptive_config.yaml`

**Changes:**
```yaml
ranging:
  order_size_usdt: 2.0 → 1.5  # Reduce for safety
  grid_spread_pct: 0.02 → 0.06  # Triple spread (0.02% → 0.06%)
  max_orders_per_side: 10 → 8  # Reduce slightly

trending:
  order_size_usdt: 1.0 → 0.8  # Reduce slightly
  grid_spread_pct: 0.15 → 0.2  # Increase spread
  max_orders_per_side: 2 → 5  # Increase for volume farming

volatile:
  order_size_usdt: 0.5 → 0.3  # Reduce further
  grid_spread_pct: 0.1 → 0.15  # Increase spread
  max_orders_per_side: 4 → 3  # Reduce
```

**File:** `backend/config/agentic-vf-config.yaml`

**Changes:**
```yaml
volume_farming:
  order_size_usdt: 20.5 → 15.0  # Reduce base size for safety
  grid_spread_pct: 0.0015 → 0.006  # Increase to 0.6%
  max_orders_per_side: 10 → 8  # Reduce slightly
```

**Rationale:**
- Ranging spread 0.02% with 100x leverage = 2% price movement = liquidation risk
- 0.06% spread with 100x leverage = 6% price movement = safer buffer
- Wider spreads reduce whipsaw losses
- Trade-off: Slightly lower fill rate for higher win rate

**Acceptance Criteria:**
- Ranging spread increased to 0.06%
- Trending spread increased to 0.2%
- Volatile spread increased to 0.15%
- Base spread increased to 0.6%
- Config loads without errors

#### Task 1.2: Fix TP/SL Parameters

**File:** `backend/config/agentic-vf-config.yaml`

**Changes:**
```yaml
volume_farming:
  risk:
    per_trade_take_profit_pct: 0.3 → 0.6  # Double TP
    per_trade_stop_loss_pct: 0.5 → 1.0  # Double SL
    position_timeout_minutes: 5 → 7  # Increase slightly
```

**Rationale:**
- Current 0.3% TP with 0.02% spread = 15 grid levels = unrealistic
- 0.6% TP with 0.06% spread = 10 grid levels = more realistic
- Wider SL provides buffer against volatility spikes
- TP/SL ratio maintained at 1:2 (risk:reward)

**Acceptance Criteria:**
- TP increased to 0.6%
- SL increased to 1.0%
- Position timeout increased to 7 minutes
- Config loads without errors

#### Task 1.3: Increase Trending Orders

**File:** `backend/config/adaptive_config.yaml`

**Changes:**
```yaml
trending:
  max_orders_per_side: 2 → 5  # Increase for volume farming
```

**Rationale:**
- Current 2 orders/side too conservative for volume farming
- More orders = more fill opportunities = more volume
- Wider spread (0.2%) compensates for increased order count

**Acceptance Criteria:**
- Trending orders increased to 5/side
- Config loads without errors

**Risk:** Low - Configuration changes only
**Testing:** Dry-run mode for 24 hours

### Phase 2: Equity Curve Position Sizing (2 days)

**Priority:** P2 - High (Medium Priority)

#### Task 2.1: Implement Equity Tracking

**File:** `backend/internal/farming/grid_manager.go`

**Changes:**
```go
// Add fields to GridManager
type GridManager struct {
    // ... existing fields
    initialEquity     float64
    currentEquity     float64
    equityHistory     []EquitySnapshot
    consecutiveWins   int
    consecutiveLosses int
    equityMu          sync.RWMutex
}

type EquitySnapshot struct {
    Timestamp time.Time
    Equity    float64
    PnL       float64
}

// Add methods
func (g *GridManager) UpdateEquity(equity, pnl float64) {
    g.equityMu.Lock()
    defer g.equityMu.Unlock()
    
    g.currentEquity = equity
    
    // Track consecutive wins/losses
    if pnl > 0 {
        g.consecutiveWins++
        g.consecutiveLosses = 0
    } else if pnl < 0 {
        g.consecutiveLosses++
        g.consecutiveWins = 0
    }
    
    // Add snapshot
    g.equityHistory = append(g.equityHistory, EquitySnapshot{
        Timestamp: time.Now(),
        Equity:    equity,
        PnL:       pnl,
    })
    
    // Keep last 1000 snapshots
    if len(g.equityHistory) > 1000 {
        g.equityHistory = g.equityHistory[1:]
    }
}

func (g *GridManager) GetWinRate24h() float64 {
    g.equityMu.RLock()
    defer g.equityMu.RUnlock()
    
    cutoff := time.Now().Add(-24 * time.Hour)
    wins := 0
    total := 0
    
    for _, snap := range g.equityHistory {
        if snap.Timestamp.After(cutoff) {
            total++
            if snap.PnL > 0 {
                wins++
            }
        }
    }
    
    if total == 0 {
        return 0.5 // Default 50%
    }
    
    return float64(wins) / float64(total)
}
```

#### Task 2.2: Implement Kelly Criterion Sizing

**File:** `backend/internal/farming/grid_manager.go`

**Changes:**
```go
// Add config
type EquitySizingConfig struct {
    KellyFraction    float64 `yaml:"kelly_fraction"`    // Default 0.25
    MinSize         float64 `yaml:"min_size"`          // Minimum $5
    MaxSize         float64 `yaml:"max_size"`          // Maximum $100
    WinRateWindow   int     `yaml:"win_rate_window"`   // 24 hours
    LossReduction   float64 `yaml:"loss_reduction"`     // 20% per loss
    WinIncrease     float64 `yaml:"win_increase"`       // 10% per win
    DrawdownReduction float64 `yaml:"drawdown_reduction"` // 10% per 5% drawdown
}

// Add sizing logic
func (g *GridManager) CalculateEquityBasedSize(baseSize float64, leverage float64) float64 {
    g.equityMu.RLock()
    defer g.equityMu.RUnlock()
    
    // Kelly Criterion: Size = Kelly% × Equity / Leverage
    kellySize := g.config.EquitySizing.KellyFraction * g.currentEquity / leverage
    
    // Adjust for consecutive losses
    lossMultiplier := 1.0 - (float64(g.consecutiveLosses) * g.config.EquitySizing.LossReduction)
    kellySize *= lossMultiplier
    
    // Adjust for consecutive wins
    winMultiplier := 1.0 + (float64(g.consecutiveWins) * g.config.EquitySizing.WinIncrease)
    kellySize *= winMultiplier
    
    // Adjust for drawdown
    drawdown := (g.initialEquity - g.currentEquity) / g.initialEquity
    if drawdown > 0 {
        drawdownMultiplier := 1.0 - (drawdown / g.config.EquitySizing.DrawdownReduction)
        kellySize *= drawdownMultiplier
    }
    
    // Clamp to bounds
    if kellySize < g.config.EquitySizing.MinSize {
        kellySize = g.config.EquitySizing.MinSize
    }
    if kellySize > g.config.EquitySizing.MaxSize {
        kellySize = g.config.EquitySizing.MaxSize
    }
    
    return kellySize
}
```

#### Task 2.3: Wire Equity Sizing to Order Placement

**File:** `backend/internal/farming/grid_manager.go`

**Changes:**
```go
// In PlaceGridOrder(), replace baseNotionalUSD with equity-based size
func (g *GridManager) PlaceGridOrder(...) error {
    // ... existing logic
    
    // Calculate equity-based size
    orderSize := g.baseNotionalUSD
    if g.config.EquitySizing != nil && g.config.EquitySizing.Enabled {
        leverage := g.getLeverageForSymbol(symbol)
        orderSize = g.CalculateEquityBasedSize(g.baseNotionalUSD, leverage)
    }
    
    // ... rest of order placement
}
```

**File:** `backend/config/agentic-vf-config.yaml`

**Changes:**
```yaml
volume_farming:
  equity_sizing:
    enabled: true
    kelly_fraction: 0.25  # Conservative Kelly
    min_size: 5.0  # Minimum $5 per order
    max_size: 100.0  # Maximum $100 per order
    win_rate_window: 24  # 24-hour lookback
    loss_reduction: 0.2  # 20% reduction per consecutive loss
    win_increase: 0.1  # 10% increase per consecutive win
    drawdown_reduction: 0.05  # 10% reduction per 5% drawdown
```

**Rationale:**
- Existing smart_sizing config partially implements this
- Need to enhance with proper equity tracking
- Must handle edge cases (equity near zero, consecutive losses)

**Acceptance Criteria:**
- Equity tracking initialized with initial balance
- Equity updated on every position close
- Kelly Criterion sizing calculated correctly
- Size adjustments applied in order placement
- Config loads without errors

**Risk:** Medium - Code changes require testing
**Testing:** Unit tests + dry-run mode for 24 hours

### Phase 3: Market Evaluation Implementation (2 days)

**Priority:** P2 - High (Medium Priority)

#### Task 3.1: Implement Spread Evaluation

**File:** `backend/internal/farming/adaptive_grid/market_condition_evaluator.go`

**Changes:**
```go
// Add spread evaluation
func (e *MarketConditionEvaluator) evaluateSpread(symbol string) float64 {
    // Get current spread from order book or use configured spread
    spread := e.config.DefaultSpread // Default 0.15%
    
    // Get real spread if available
    if e.orderBookClient != nil {
        bestBid, bestAsk := e.orderBookClient.GetBestBidAsk(symbol)
        if bestBid > 0 && bestAsk > 0 {
            spread = (bestAsk - bestBid) / bestBid
        }
    }
    
    // Normalize: 0.01% → 0, 1% → 1
    spreadScore := spread / 0.01
    if spreadScore > 1 {
        spreadScore = 1
    }
    
    return spreadScore
}
```

#### Task 3.2: Implement Volume Evaluation

**File:** `backend/internal/farming/adaptive_grid/market_condition_evaluator.go`

**Changes:**
```go
// Add volume evaluation
func (e *MarketConditionEvaluator) evaluateVolume(symbol string) float64 {
    // Get 24h volume
    volume24h := e.get24hVolume(symbol)
    
    // Normalize: $1M → 0, $100M → 1 (log scale)
    volumeScore := math.Log10(volume24h / 1_000_000) / 2.0
    if volumeScore < 0 {
        volumeScore = 0
    }
    if volumeScore > 1 {
        volumeScore = 1
    }
    
    return volumeScore
}
```

#### Task 3.3: Implement Funding Rate Evaluation

**File:** `backend/internal/farming/adaptive_grid/market_condition_evaluator.go`

**Changes:**
```go
// Add funding rate evaluation
func (e *MarketConditionEvaluator) evaluateFunding(symbol string) float64 {
    // Get current funding rate
    funding := e.getFundingRate(symbol)
    
    // Normalize: -0.1% → 0, 0.1% → 1 (absolute value)
    fundingScore := math.Abs(funding) / 0.001
    if fundingScore > 1 {
        fundingScore = 1
    }
    
    return fundingScore
}
```

#### Task 3.4: Integrate into evaluateMarket()

**File:** `backend/internal/farming/adaptive_grid/market_condition_evaluator.go`

**Changes:**
```go
// Replace placeholder evaluateMarket
func (e *MarketConditionEvaluator) evaluateMarket(symbol string) float64 {
    spreadScore := e.evaluateSpread(symbol)
    volumeScore := e.evaluateVolume(symbol)
    fundingScore := e.evaluateFunding(symbol)
    
    // Weighted average: 40% spread, 40% volume, 20% funding
    marketScore := (spreadScore * 0.4) + (volumeScore * 0.4) + (fundingScore * 0.2)
    
    e.logger.Debug("Market evaluation",
        zap.String("symbol", symbol),
        zap.Float64("spread_score", spreadScore),
        zap.Float64("volume_score", volumeScore),
        zap.Float64("funding_score", fundingScore),
        zap.Float64("market_score", marketScore))
    
    return marketScore
}
```

**Rationale:**
- Current placeholder returns 0.5 (neutral)
- Need real evaluation of market conditions
- Spread impacts fill rate and slippage
- Volume impacts liquidity
- Funding rate impacts profitability

**Acceptance Criteria:**
- Spread evaluation implemented
- Volume evaluation implemented
- Funding rate evaluation implemented
- Weighted average calculation correct
- Logging added for debugging

**Risk:** Medium - Requires order book/funding rate data
**Testing:** Unit tests + dry-run mode

### Phase 4: State-Specific Actions (2 days)

**Priority:** P2 - High (Medium Priority)

#### Task 4.1: Define State-Specific Parameters

**File:** `backend/internal/config/config.go`

**Changes:**
```go
// Add state-specific parameter configs
type StateSpecificConfig struct {
    TradingState   StateParams `yaml:"trading"`
    OverSizeState  StateParams `yaml:"over_size"`
    DefensiveState StateParams `yaml:"defensive"`
    RecoveryState  StateParams `yaml:"recovery"`
    ExitHalfState  StateParams `yaml:"exit_half"`
    ExitAllState   StateParams `yaml:"exit_all"`
}

type StateParams struct {
    SpreadMultiplier  float64 `yaml:"spread_multiplier"`
    SizeMultiplier    float64 `yaml:"size_multiplier"`
    OrderMultiplier   float64 `yaml:"order_multiplier"`
    TpMultiplier     float64 `yaml:"tp_multiplier"`
    SlMultiplier     float64 `yaml:"sl_multiplier"`
}
```

#### Task 4.2: Add State Parameter Application Logic

**File:** `backend/internal/farming/grid_manager.go`

**Changes:**
```go
// Add method to get state-specific parameters
func (g *GridManager) GetStateParameters(symbol string) StateParams {
    if g.stateMachine == nil {
        return g.config.StateSpecific.TradingState // Default
    }
    
    state := g.stateMachine.GetState(symbol)
    
    switch state {
    case adaptive_grid.GridStateOverSize:
        return g.config.StateSpecific.OverSizeState
    case adaptive_grid.GridStateDefensive:
        return g.config.StateSpecific.DefensiveState
    case adaptive_grid.GridStateRecovery:
        return g.config.StateSpecific.RecoveryState
    case adaptive_grid.GridStateExitHalf:
        return g.config.StateSpecific.ExitHalfState
    case adaptive_grid.GridStateExitAll:
        return g.config.StateSpecific.ExitAllState
    default:
        return g.config.StateSpecific.TradingState
    }
}

// Apply state parameters in order placement
func (g *GridManager) PlaceGridOrder(...) error {
    // Get state-specific parameters
    stateParams := g.GetStateParameters(symbol)
    
    // Apply multipliers
    spread *= stateParams.SpreadMultiplier
    size *= stateParams.SizeMultiplier
    orderCount = int(float64(orderCount) * stateParams.OrderMultiplier)
    tp *= stateParams.TpMultiplier
    sl *= stateParams.SlMultiplier
    
    // ... rest of order placement
}
```

**File:** `backend/config/agentic-vf-config.yaml`

**Changes:**
```yaml
adaptive_states:
  trading:
    spread_multiplier: 1.0
    size_multiplier: 1.0
    order_multiplier: 1.0
    tp_multiplier: 1.0
    sl_multiplier: 1.0
  
  over_size:
    spread_multiplier: 1.5  # Wider spread
    size_multiplier: 0.5   # Reduce size
    order_multiplier: 0.5  # Fewer orders
    tp_multiplier: 1.2      # Wider TP
    sl_multiplier: 0.8      # Tighter SL (exit faster)
  
  defensive:
    spread_multiplier: 2.0  # Much wider spread
    size_multiplier: 0.3   # Much smaller size
    order_multiplier: 0.3  # Much fewer orders
    tp_multiplier: 1.5      # Wider TP
    sl_multiplier: 1.0      # Normal SL
  
  recovery:
    spread_multiplier: 1.2  # Slightly wider
    size_multiplier: 0.7   # Smaller size
    order_multiplier: 0.7  # Fewer orders
    tp_multiplier: 1.0      # Normal TP
    sl_multiplier: 1.2      # Wider SL (more room)
  
  exit_half:
    spread_multiplier: 1.0
    size_multiplier: 0.5   # Reducing position
    order_multiplier: 0.0  # No new orders
    tp_multiplier: 1.0
    sl_multiplier: 1.0
  
  exit_all:
    spread_multiplier: 1.0
    size_multiplier: 0.0   # No new orders
    order_multiplier: 0.0  # No new orders
    tp_multiplier: 1.0
    sl_multiplier: 1.0
```

**Rationale:**
- Current implementation only blocks new orders
- No parameter adjustments in adaptive states
- Need spread/SL/size multipliers per state
- Allows finer-grained risk control

**Acceptance Criteria:**
- State-specific config defined
- State parameter lookup implemented
- Multipliers applied in order placement
- Config loads without errors

**Risk:** Medium - Code changes require testing
**Testing:** Unit tests + dry-run mode for 24 hours

## Testing Strategy

### Unit Tests

**Phase 1 Tests:**
- Test config loading with new spread values
- Test config loading with new TP/SL values
- Test config loading with new trending orders

**Phase 2 Tests:**
- Test equity tracking logic
- Test Kelly Criterion calculation
- Test consecutive win/loss adjustment
- Test drawdown adjustment

**Phase 3 Tests:**
- Test spread evaluation
- Test volume evaluation
- Test funding rate evaluation
- Test weighted average calculation

**Phase 4 Tests:**
- Test state parameter lookup
- Test multiplier application
- Test all state transitions

### Integration Tests

**Phase 1 Integration Tests:**
- Test bot starts with new config
- Test grid placement with new spreads
- Test TP/SL with new values
- Test trending regime with 5 orders

**Phase 2 Integration Tests:**
- Test equity update on position close
- Test size adjustment with Kelly Criterion
- Test size reduction after consecutive losses
- Test size increase after consecutive wins

**Phase 3 Integration Tests:**
- Test market evaluation in real-time
- Test spread changes trigger state changes
- Test volume changes trigger state changes

**Phase 4 Integration Tests:**
- Test state-specific parameter application
- Test all state transitions with parameter changes
- Test parameter rollback on state exit

### End-to-End Tests

**Dry-Run Mode Tests:**
- Run bot in dry-run mode for 24 hours
- Monitor equity tracking
- Monitor Kelly Criterion sizing
- Monitor market evaluation
- Monitor state-specific parameters
- Verify no actual orders placed

**Live Mode Tests (after all phases):**
- Deploy with reduced size
- Monitor for 1 week
- Track win rate improvement
- Track drawdown reduction
- Track fill rate impact

## Deployment Strategy

### Phase 1 Deployment (Configuration Only)

**Strategy:** Safe config change with monitoring

1. **Pre-Deployment:**
   - Backup existing config files
   - Create feature branch
   - Apply config changes
   - Validate config loads without errors

2. **Test Deployment:**
   - Deploy to test environment
   - Run in dry-run mode for 24 hours
   - Monitor logs for:
     - Grid placement with new spreads
     - TP/SL with new values
     - Trending regime order count
   - Fix any issues found

3. **Production Deployment:**
   - Deploy to production with dry-run mode enabled
   - Monitor for 24 hours
   - If stable, switch to live mode
   - Monitor for 1 week:
     - Win rate improvement
     - Fill rate impact
     - Stuck position rate

**Rollback:** Restore config backup

### Phase 2 Deployment (Equity Sizing)

**Strategy:** Staged deployment with monitoring

1. **Pre-Deployment:**
   - Implement equity tracking
   - Implement Kelly Criterion sizing
   - Add unit tests
   - Code review

2. **Test Deployment:**
   - Deploy to test environment
   - Run in dry-run mode for 24 hours
   - Monitor equity tracking
   - Monitor size adjustments
   - Fix any issues found

3. **Production Deployment:**
   - Deploy to production with dry-run mode enabled
   - Monitor for 24 hours
   - If stable, switch to live mode
   - Monitor for 1 week:
     - Equity tracking accuracy
     - Size adjustment frequency
     - Performance impact

**Rollback:** Git revert

### Phase 3 Deployment (Market Evaluation)

**Strategy:** Deploy after Phase 2 validation

1. **Pre-Deployment:**
   - Implement spread/volume/funding evaluation
   - Add unit tests
   - Code review

2. **Test Deployment:**
   - Deploy to test environment
   - Run in dry-run mode for 24 hours
   - Monitor market evaluation
   - Monitor state transition triggers
   - Fix any issues found

3. **Production Deployment:**
   - Deploy to production
   - Monitor for 48 hours
   - Track evaluation accuracy
   - Track state transition frequency

**Rollback:** Git revert

### Phase 4 Deployment (State-Specific Actions)

**Strategy:** Deploy after Phase 3 validation

1. **Pre-Deployment:**
   - Implement state-specific parameters
   - Add unit tests
   - Code review

2. **Test Deployment:**
   - Deploy to test environment
   - Run in dry-run mode for 24 hours
   - Monitor parameter application
   - Monitor all state transitions
   - Fix any issues found

3. **Production Deployment:**
   - Deploy to production
   - Monitor for 48 hours
   - Track parameter changes
   - Track state transition smoothness

**Rollback:** Git revert

## Rollback Plan

### Config Rollback
- Keep backup of config files before changes
- Simple file restore to rollback
- Config changes can be hot-reloaded

### Code Rollback
- Git tags for each phase:
  - `tag: pre-config-opt-phase1`
  - `tag: pre-config-opt-phase2`
  - `tag: pre-config-opt-phase3`
  - `tag: pre-config-opt-phase4`
- Simple git checkout to rollback
- No database schema changes

### Rollback Triggers
- Phase 1: Win rate drops > 10%, fill rate drops > 20%
- Phase 2: Size adjustments erratic, equity tracking errors
- Phase 3: State transitions too frequent (>10/hour), evaluation errors
- Phase 4: Parameter application errors, state transition failures

## Monitoring & Metrics

### Key Metrics to Track

**Configuration Metrics:**
- Grid spread by regime
- TP/SL values
- Order count by regime
- Fill rate by regime

**Equity Sizing Metrics:**
- Current equity vs initial equity
- Consecutive wins/losses count
- Win rate (24h)
- Drawdown percentage
- Size adjustment frequency
- Kelly Criterion size vs base size

**Market Evaluation Metrics:**
- Spread score over time
- Volume score over time
- Funding rate score over time
- Market score over time
- Evaluation accuracy

**State-Specific Metrics:**
- State transition frequency
- Parameter changes by state
- Time in each state
- Performance by state

### Alerting

**Critical Alerts:**
- Win rate drops > 10%
- Drawdown increases > 15%
- Equity tracking errors
- Market evaluation errors
- State transition failures

**Warning Alerts:**
- Fill rate drops > 20%
- Size adjustments erratic
- State transitions frequent (>5/hour)
- Parameter changes unexpected

### Logging

**Enhanced Logging:**
- Config: Log all config changes, validation
- Equity: Log equity updates, size adjustments
- Market: Log evaluation calculations, scores
- State: Log state transitions, parameter changes

## Success Criteria

### Phase 1 Success
**Functional Criteria:**
- Config loads without errors
- Grid spreads updated correctly
- TP/SL values updated correctly
- Trending orders increased to 5/side

**Performance Criteria:**
- No config loading errors
- No performance degradation

**Business Criteria:**
- Win rate improves from 55-60% to 65-70%
- Stuck position rate reduced by 50%

### Phase 2 Success
**Functional Criteria:**
- Equity tracking accurate
- Kelly Criterion sizing correct
- Size adjustments applied correctly
- Edge cases handled (equity near zero)

**Performance Criteria:**
- Equity tracking adds < 1ms overhead
- Size calculation adds < 1ms overhead
- No performance degradation

**Business Criteria:**
- Size reduces after consecutive losses
- Size increases after winning streak
- Drawdown reduces position size

### Phase 3 Success
**Functional Criteria:**
- Spread evaluation implemented
- Volume evaluation implemented
- Funding rate evaluation implemented
- Weighted average correct

**Performance Criteria:**
- Evaluation adds < 10ms overhead
- No performance degradation

**Business Criteria:**
- Market evaluation triggers appropriate state changes
- Evaluation accuracy > 80%

### Phase 4 Success
**Functional Criteria:**
- State-specific config defined
- Parameter lookup correct
- Multipliers applied correctly
- All states tested

**Performance Criteria:**
- Parameter application adds < 1ms overhead
- No performance degradation

**Business Criteria:**
- State transitions smoother
- Risk control improved in adaptive states

## Timeline

### Phase 0: 1 day
- Research and analysis
- Output: research.md

### Phase 1: 1 day
- Config changes (2 hours)
- Validation (2 hours)
- Dry-run testing (4 hours)
- Deployment (2 hours)

### Phase 2: 2 days
- Equity tracking implementation (4 hours)
- Kelly Criterion implementation (4 hours)
- Integration (2 hours)
- Testing (4 hours)
- Deployment (2 hours)

### Phase 3: 2 days
- Spread evaluation (2 hours)
- Volume evaluation (2 hours)
- Funding evaluation (2 hours)
- Integration (2 hours)
- Testing (4 hours)
- Deployment (2 hours)

### Phase 4: 2 days
- State-specific config (2 hours)
- Parameter application (4 hours)
- Integration (2 hours)
- Testing (4 hours)
- Deployment (2 hours)

**Total:** 8 days

## Notes

### Configuration
- All features can be enabled/disabled via config
- Config changes can be hot-reloaded
- Default config enables all Phase 1 features

### Backward Compatibility
- All changes are backward compatible
- Existing config files will work with defaults
- No database schema changes required
- No API changes

### Risk Mitigation
- Dry-run mode available for safe testing
- Git version control for easy rollback
- Staged deployment with monitoring
- Feature flags for easy enable/disable
- Extensive logging for debugging

### Dependencies
- Phase 1: No new dependencies
- Phase 2: No new dependencies
- Phase 3: May need order book/funding rate data
- Phase 4: No new dependencies

### Future Enhancements
- Add machine learning for Kelly optimization
- Add more sophisticated market evaluation
- Add more state-specific parameters
- Add performance analytics dashboard
