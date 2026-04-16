# Feature Specification: Strategy Integration into Agentic Volume Farming

## Overview

### Feature Description
Integrate the existing Strategy ecosystem (12+ battle-tested trading strategies) into the Agentic Volume Farming bot. This bridges the gap between structural market analysis and volume farming execution, enabling the bot to trade with awareness of liquidity levels, Fair Value Gaps, and mean reversion setups while maintaining the "fluid like water" adaptive behavior.

### Business Value
- **Better Timing**: Enter grids at structural levels rather than random intervals
- **Directional Awareness**: Adjust grid bias based on setup quality and market structure
- **Smarter Risk**: Place SL/TP at meaningful structural levels (liquidity highs/lows, FVG zones)
- **Reduced Over-trading**: Lower activity when no qualifying setups exist
- **Enhanced Fluid Behavior**: Flow intensity modulates by signal strength (stronger signal = stronger flow)

## User Scenarios

### Scenario 1: FVG-Based Grid Entry
**Given** BTCUSD1 is in sideways regime with bullish Fair Value Gap at $42,000-$42,100  
**When** `fvg_fill` strategy detects the imbalance with 0.8 signal strength  
**Then** the bot:
- Increases flow intensity to 1.3x (strong signal boost)
- Tightens buy-side grid spread by 30% to capture fills in FVG zone
- Sets take-profit at next resistance ($43,500)
- Waits for signal before placing grid (hybrid entry mode)

### Scenario 2: Liquidity Sweep Response
**Given** ETHUSD1 sweeps equal highs at $3,500 forming liquidity grab  
**When** `liquidity_sweep` strategy detects bearish sweep with 0.75 conviction  
**Then** the bot:
- Applies short directional bias to flow calculation
- Widens sell-side spread (waits for rejection confirmation)
- Tightens buy-side slightly (prepared for reversal)
- Sets stop-loss above sweep high ($3,520 with 0.5% buffer)

### Scenario 3: No Signal Defensive Mode
**Given** SOLUSD1 has no clear structural setup  
**When** all strategies return nil or low confidence (<0.3) signals  
**Then** the bot:
- Reduces flow intensity to 0.6x (defensive posture)
- Widens spreads to reduce fill frequency
- Maintains smaller position sizes
- Switches to time-based entry after timeout (60s)

### Scenario 4: Regime-Strategy Alignment
**Given** Market transitions from ranging to trending (ADX crosses 30)  
**When** Agentic RegimeDetector updates regime to TRENDING  
**Then** the bot:
- Switches active strategy set (bb_bounce→ema_cross, etc.)
- Maintains existing positions with trend-following bias
- Adjusts geometry for trend mode (wider spacing, fewer orders)

### Scenario 5: Continuous Strategy Blending
**Given** BTCUSD1 has FVG signal (strength 0.7) + BB Bounce signal (strength 0.5) simultaneously  
**When** StrategyBlendEngine processes both signals  
**Then** the bot:
- Blends weights smoothly: FVG (0.6) + BB Bounce (0.4) instead of picking one
- Calculates blended directional bias: (-0.7 * 0.6) + (-0.5 * 0.4) = -0.62
- Entropy is low (0.3) because signals agree → maintains high conviction
- Adjusts flow continuously, no discrete jumps between strategies

### Scenario 6: Signal Confidence Decay
**Given** FVG signal was detected 45 seconds ago with strength 0.8  
**When** SignalDecayManager calculates current confidence  
**Then** the bot:
- Applies exponential decay with 30s half-life: 0.8 → 0.57 (mượt mà)
- Does NOT suddenly drop to zero after timeout
- Gradually reduces flow intensity as confidence fades
- When new FVG refresh arrives (strength 0.75), smoothly blends: 0.57*0.3 + 0.75*0.7 = 0.69

### Scenario 7: Predictive Flow Adjustment
**Given** FVG signal strength trend: 0.5 → 0.65 → 0.78 over 60s (momentum +0.28)  
**When** PredictiveFlowEngine detects upward momentum  
**Then** the bot:
- Predicts signal will reach ~0.92 in next 30s (extrapolation)
- Increases flow intensity EARLY by 0.14 (0.28 * 0.5) before signal peaks
- Leads the market instead of chasing it
- Adjusts direction bias proactively as momentum shifts

### Scenario 8: Micro-Regime Detection
**Given** Market is in Sideways regime (ADX 18) but showing specific patterns  
**When** MicroRegimeDetector analyzes strategy signals + price action  
**Then** the bot detects:
- **MicroAccumulation**: Liquidity sweep + BB bounce + ascending volume
- Flow: Accumulate gradually with 0.7x intensity, wider stops (2.5%)
- **MicroCompression**: BB width < 0.02 + ATR < 0.003
- Flow: Reduce to 0.5x intensity, prepare for breakout
- **MicroPullback** (in Trending): FVG fill opposite to trend
- Flow: Increase to 1.2x intensity, add to trend position

## Functional Requirements

### FR1: Strategy Signal Collection
**Acceptance Criteria:**
- AgenticEngine can collect real-time signals from sub-strategies
- Signal aggregation happens within detection cycle (< 30s latency)
- Supports multiple concurrent strategies per symbol
- Thread-safe signal access during collection

### FR2: Setup Quality Scoring
**Acceptance Criteria:**
- Strong signals (>0.7 strength) add 20-30 points to opportunity score
- Moderate signals (0.4-0.7) add 10-15 points
- Regime alignment bonus applies (+10 for matching setup type)
- Backward compatible (nil signals = neutral 50 score)

### FR3: Flow Intensity Modulation
**Acceptance Criteria:**
- Flow intensity scales 0.6x to 1.3x based on signal strength
- Directional bias (-1 to 1) influences flow direction
- Size multiplier adjusts by conviction (0.6x to 1.25x)
- Changes applied within one detection cycle

### FR4: Asymmetric Grid Geometry
**Acceptance Criteria:**
- FVG signals tighten spread on fill side by configurable %
- Liquidity sweep adjusts spreads for expected reversal
- Mean reversion maintains symmetric spreads
- Multipliers configurable via YAML

### FR5: Signal-Triggered Entry
**Acceptance Criteria:**
- Three entry modes supported: time_based, signal_triggered, hybrid
- Hybrid mode waits for signal OR timeout (configurable)
- Minimum signal strength threshold enforced
- Timeout prevents indefinite waiting

### FR6: Structural Risk Management
**Acceptance Criteria:**
- SL placed at structural levels (liquidity sweep high/low)
- TP placed at next support/resistance level
- Buffer percentage added to structural levels
- Fallback to ATR-based if no structure available
- Validation ensures reasonable distances

### FR7: Continuous Strategy Blending
**Acceptance Criteria:**
- Multiple strategy signals blended continuously (not discrete selection)
- Weight transitions smooth (convergence rate 0.1-0.3, no jumps)
- Entropy calculated to detect signal conflicts
- High entropy (>0.7) reduces conviction by 50% (defensive mode)
- Blend output: continuous Direction (-1 to 1) + Conviction (0-1)

### FR8: Signal Confidence Decay
**Acceptance Criteria:**
- Exponential decay curve for signal confidence (N(t) = N0 * 0.5^(t/half_life))
- Configurable half-life (10s - 120s, default 30s)
- Min confidence floor (0.1) prevents sudden drop to zero
- Smooth renewal: blend old*0.3 + new*0.7 (no jumps on refresh)
- Decay applied per strategy independently

### FR9: Predictive Flow Adjustment
**Acceptance Criteria:**
- Signal momentum calculated from recent history (3+ points)
- Predict lead time configurable (10s - 60s, default 30s)
- Prediction confidence affects adjustment magnitude
- Intensity boost = max(0, strength_momentum) * 0.5
- Direction lead = bias_momentum * 0.3
- Fallback to current signal when prediction uncertain

### FR10: Micro-Regime Detection
**Acceptance Criteria:**
- At least 6 micro-states detected within base regimes
- Sideways micro: Accumulation, Distribution, Range, Compression
- Trending micro: Impulse, Pullback, Consolidation
- Micro-state transitions smooth (no discrete jumps)
- Each micro-state maps to specific flow parameters
- Validated with historical data for accuracy > 70%

## Success Criteria

### Quantitative Metrics
1. **Signal Responsiveness**: Signals influence flow within 1 detection cycle (< 30s)
2. **Setup Quality Impact**: Strong signals increase symbol scores by 15-30 points
3. **Flow Modulation Range**: Intensity varies 0.6x to 1.3x as designed
4. **Entry Precision**: Signal-triggered entries show 20% better initial PnL
5. **Backward Compatibility**: Zero breaking changes when feature disabled
6. **Performance**: Added latency < 50ms per detection cycle

### Continuous Adaptation Metrics (NEW)
7. **Strategy Blend Smoothness**: Weight transitions with convergence rate 0.1-0.3 (no jumps >0.2)
8. **Entropy Detection**: Correctly identifies conflicting signals (test coverage >80%)
9. **Decay Curve Accuracy**: Actual decay matches exponential formula within 5%
10. **Prediction Accuracy**: Momentum-based predictions correct >60% of the time
11. **Micro-Regime Accuracy**: Micro-state detection validated >70% on historical data
12. **Continuous Parameters**: All outputs are continuous gradients (no binary on/off)

### Qualitative Indicators
1. Bot demonstrates awareness of key structural levels
2. Grid spreads visibly adjust to setup type in logs
3. SL/TP placed at meaningful market structure (not arbitrary %)
4. Reduced activity in choppy, no-setup conditions
5. Enhanced directional bias during clear trends

### Fluid Behavior Indicators (NEW)
6. **No Discrete Jumps**: Parameter changes are smooth (visible in metrics)
7. **Multi-Strategy Awareness**: Logs show multiple strategies contributing simultaneously
8. **Predictive Adjustments**: Flow changes BEFORE signal peaks (lead compensation)
9. **Graceful Degradation**: Signals fade smoothly, not suddenly cut
10. **Micro-Adaptation**: Bot distinguishes between Accumulation/Compression within Sideways

## Key Entities

### SignalBundle
Container for aggregated strategy signals per symbol:
- `PrimarySignal`: Highest conviction signal
- `SignalStrength`: Aggregate 0-1 confidence
- `DirectionalBias`: -1 (long) to 1 (short)
- `StructuralLevels`: SL/TP candidates (liquidity zones, FVG areas)

### BlendedSignal (NEW)
Continuous blend output from multiple strategies:
- `DirectionalBias`: Weighted average (-1 to 1, continuous)
- `Conviction`: Blended confidence (0-1, continuous)
- `Entropy`: Measure of strategy disagreement (0-1)
- `Weights`: Map of strategy weights (smoothly varying)
- `IsConflicting`: True when entropy > 0.7

### StrategySignalAggregator
Bridge between Strategy ecosystem and AgenticEngine:
- Routes klines to appropriate strategies by regime
- Collects and weights signals from multiple strategies
- Maintains signal cache with TTL
- Exposes GetSignalBundle() for other components

### StrategyBlendEngine (NEW)
Continuous multi-strategy fusion:
- Blends all active strategies with smooth weight transitions
- Calculates entropy for conflict detection
- Maintains blend history for tracking
- Convergence rate controls weight adjustment speed

### SignalDecayManager (NEW)
Exponential confidence decay:
- Half-life based decay curves per strategy
- Smooth renewal blending
- Min confidence floor (0.1)
- Independent decay per signal type

### PredictiveFlowEngine (NEW)
Lead compensation based on momentum:
- Signal history tracking
- Momentum calculation (strength + bias)
- Predictive adjustments with lead time
- Confidence-weighted predictions

### MicroRegimeDetector (NEW)
Granular market state detection:
- Detects micro-states within base regimes
- Maps strategy signals to micro-states
- Smooth state transitions
- Specific parameters per micro-state

### DecayCurve (NEW)
Signal decay tracking:
- `OriginalStrength`: Initial signal strength
- `Timestamp`: Detection time
- `CurrentValue`: Decayed value (continuously updated)

### MicroState (NEW)
Fine-grained market states:
- `Accumulation`: Wyckoff accumulation pattern
- `Distribution`: Wyckoff distribution pattern
- `Compression`: Before breakout (low volatility)
- `RangeBound`: Simple ranging
- `Impulse`: Strong trend momentum
- `Pullback`: Trend continuation setup
- `Consolidation`: Flag/pennant patterns

### Enhanced Flow Parameters
Extension of FluidFlowEngine output:
- `Intensity`: 0-1 with strategy modulation (0.6x-1.3x)
- `Direction`: -1 to 1 with signal bias (continuous gradient)
- `SizeMultiplier`: Adjusted by signal conviction (0.5x-2.0x)
- `Geometry`: Asymmetric spread multipliers
- `MicroState`: Current micro-regime (granular)

## Technical Context

### Existing Components (Leverage)
| Component | Location | Role |
|-----------|----------|------|
| Strategy Router | `internal/strategy/router.go` | Route signals by regime |
| Sub-strategies | `internal/strategy/*` | Generate trading signals |
| RegimeDetector | `internal/agentic/regime_detector.go` | Classify market regime |
| OpportunityScorer | `internal/agentic/scorer.go` | Score opportunities |
| FluidFlowEngine | `internal/farming/adaptive_grid/fluid_flow.go` | Flow modulation |
| AdaptiveGridGeometry | `internal/farming/adaptive_grid/adaptive_grid_geometry.go` | Spread calculation |
| GridManager | `internal/farming/grid_manager.go` | Order placement |

### Integration Points
1. **Kline Stream**: WebSocket klines feed strategy router
2. **Detection Cycle**: AgenticEngine collects signals during regime detection
3. **Flow Calculation**: AdaptiveGridManager queries signals for flow modulation
4. **Entry Decision**: GridManager checks signals before placing orders
5. **Risk Levels**: AdaptiveGridManager uses structural levels for SL/TP

## Assumptions & Dependencies

### Assumptions
1. Existing Strategy ecosystem is stable and battle-tested
2. WebSocket kline stream has sufficient data for strategy calculations
3. Regime detection accuracy is acceptable for strategy selection
4. Signal latency of < 30s is acceptable for volume farming

### Dependencies
1. ✅ Strategy interface (`internal/strategy/interface.go`)
2. ✅ Regime classifier (`internal/strategy/regime/classifier.go`)
3. ✅ WebSocket kline infrastructure
4. ✅ Existing volume optimization components
5. ⚠️ Potential adapter needed for Agentic-specific types

## Configuration

### Minimal Configuration
```yaml
strategy_integration:
  enabled: true
  mode: "hybrid"
  
  scoring:
    setup_quality_weight: 0.25
    min_signal_strength: 0.4
    
  flow_modulation:
    enabled: true
    intensity_multiplier_max: 1.3
    intensity_multiplier_min: 0.6
    
  entry:
    mode: "hybrid"
    min_signal_strength: 0.5
    entry_timeout_sec: 60
```

### Full Configuration
```yaml
strategy_integration:
  enabled: true
  mode: "hybrid"  # time_based | signal_triggered | hybrid
  
  scoring:
    setup_quality_weight: 0.25      # % of total score
    min_signal_strength: 0.4       # Threshold for scoring bonus
    regime_alignment_bonus: 10.0     # Points for matching regime
    
  flow_modulation:
    enabled: true
    intensity_multiplier_max: 1.3   # Strong signal boost
    intensity_multiplier_min: 0.6    # Low signal reduction
    directional_bias_weight: 0.8   # How much signal affects direction
    
  geometry:
    asymmetric_spreads: true
    fvg_spread_tighten_pct: 30      # Tighten % on FVG side
    sweep_spread_widen_pct: 30      # Widen % on swept side
    
  entry:
    mode: "hybrid"
    min_signal_strength: 0.5
    entry_timeout_sec: 60
    
  risk:
    use_structural_sl: true
    sl_buffer_pct: 0.5               # Buffer around structural level
    tp_at_next_structure: true
    max_sl_distance_pct: 5.0         # Validate not too wide
    min_sl_distance_pct: 0.5         # Validate not too tight
    
  regime_strategies:
    sideways:
      - bb_bounce
      - rsi_divergence
      - fvg_fill
      - liquidity_sweep
      - vwap_reversion
      - sr_bounce
    trending:
      - ema_cross
      - breakout_retest
      - flag_pennant
      - trailing_sh
    breakout:
      - volume_spike
      - orb
      - momentum_roc
      
  # NEW: Continuous adaptation settings
  continuous_adaptation:
    enabled: true
    
    blending:
      convergence_rate: 0.2        # 0.1-0.3 (lower = smoother)
      entropy_threshold: 0.7       # Conflict detection threshold
      min_weight: 0.05             # Floor for strategy weights
      
    decay:
      half_life_sec: 30            # Time for confidence to halve
      min_confidence: 0.1          # Floor for decayed confidence
      renewal_blend_ratio: 0.7     # new_signal_weight * 0.7 + old * 0.3
      
    predictive:
      enabled: true
      lead_time_sec: 30            # Predict ahead by 30s
      momentum_window_sec: 60      # Calculate momentum over 60s
      min_history_points: 3        # Need 3+ points for prediction
      
    micro_regime:
      enabled: true
      transition_smoothing: 0.3     # Smooth transitions between micro-states
      validation_threshold: 0.7    # Min accuracy for micro-detection
```

## Out of Scope

1. **Full Strategy Router Migration** - We enhance, not replace, AgenticEngine
2. **Machine Learning Signal Weighting** - Manual config-based weights only
3. **Multi-Symbol Correlation** - Per-symbol analysis only
4. **Historical Backtesting** - Forward testing with live signals
5. **Complex Consensus Algorithms** - Simple primary signal selection
6. **Real-time Strategy Optimization** - Static strategy selection by regime

## Edge Cases

### EC1: Conflicting Signals
**Scenario**: `bb_bounce` says buy, `liquidity_sweep` says sell  
**Resolution**: Select by conviction (higher SignalStrength wins), if equal defer to regime alignment

### EC2: Stale Signals
**Scenario**: Last signal > 60s old  
**Resolution**: Treat as nil (neutral), log warning, proceed with base flow calculation

### EC3: Invalid Structural Levels
**Scenario**: Calculated SL is within 0.3% of entry (too tight)  
**Resolution**: Fallback to ATR-based SL with warning log

### EC4: Rapid Regime Changes
**Scenario**: Regime flips between TRENDING/RANGING rapidly  
**Resolution**: Use cooldown period (30s) before switching strategy set

### EC5: Strategy Initialization Failure
**Scenario**: One sub-strategy fails to initialize  
**Resolution**: Log error, continue with remaining strategies, graceful degradation

## Testing Strategy

### Unit Tests
- Signal aggregation formula correctness
- Setup quality score calculation
- Flow modulation math verification
- Structural level validation

### Integration Tests
- End-to-end signal flow from kline to grid placement
- Regime transition handling
- Configuration change hot-reload
- Backward compatibility with feature disabled

### Validation Criteria
- All existing tests pass
- New tests cover 80%+ of new code
- Latency benchmarks met (< 50ms added)
- No memory leaks in signal collection
