# Strategy Integration into Agentic Volume Farming - Implementation Plan

## Overview

This plan implements the integration of the existing Strategy ecosystem (liquidity_sweep, fvg_fill, bb_bounce, etc.) into the Agentic Volume Farming bot. The goal is to enhance the "fluid like water" trading behavior by incorporating structural market analysis signals into the volume farming decision flow.

**Key Insight**: The Strategy layer (12+ sub-strategies) already exists and is battle-tested in the regular bot (`cmd/bot`), but the Agentic bot (`cmd/agentic`) operates without these signals, missing opportunities for:
- Better entry timing at structural levels
- Directional bias adjustment based on setup quality
- Enhanced flow intensity modulation

## Current State Analysis

### Existing Strategy Ecosystem (Ready to Use)
```
backend/internal/strategy/
├── interface.go                    # Strategy interface (Signal, OnKline, etc.)
├── router.go                       # Route by regime (TRENDING/RANGING/BREAKOUT)
├── farming/
│   └── volume_farm.go              # VolumeFarmStrategy wrapper
├── structure/
│   ├── liquidity_sweep.go         # Detects liquidity grabs
│   ├── fvg_fill.go                # Fair Value Gap detection
│   └── structure_bos.go           # Break of Structure
├── meanrev/
│   ├── bb_bounce.go               # Bollinger Bands mean reversion
│   ├── rsi_divergence.go          # RSI divergence
│   ├── vwap_reversion.go          # VWAP deviation
│   └── sr_bounce.go               # Support/Resistance bounce
├── momentum/
│   ├── volume_spike.go            # Volume breakout
│   ├── orb.go                     # Opening Range Breakout
│   └── momentum_roc.go            # Rate of Change
└── trend/
    ├── ema_cross.go               # EMA crossover
    ├── breakout_retest.go         # Breakout & retest
    ├── flag_pennant.go            # Flag patterns
    └── trailing_sh.go             # Trailing stop hunt
```

### Agentic Bot Current Flow (Without Strategy)
```
AgenticEngine
├── RegimeDetector (ADX/ATR based) ──► RegimeSnapshot
├── OpportunityScorer ───────────────► Score (0-100)
│   ├── trendScore (ADX based)
│   ├── volScore (ATR/BB based)
│   ├── volumeScore (24h volume)
│   └── structureScore (price change %)
└── WhitelistManager ────────────────► Active symbols

VolumeFarmEngine
├── FluidFlowEngine ────────────────► FlowParameters
│   ├── flowIntensity (0-1)
│   ├── flowDirection (-1 to 1)
│   └── sizeMultiplier
├── AdaptiveGridGeometry ───────────► Spread/OrderCount
└── GridManager ────────────────────► Order placement
```

## Implementation Strategy

### Architecture: Strategy Signal Layer

```
┌─────────────────────────────────────────────────────────────┐
│                    STRATEGY SIGNAL LAYER                    │
├─────────────────────────────────────────────────────────────┤
│  StrategySignalAggregator                                   │
│  ├── Collects signals from all sub-strategies               │
│  ├── Maps regime → active strategies                      │
│  │   RANGING: bb_bounce, rsi_divergence, fvg_fill          │
│  │   TRENDING: ema_cross, breakout_retest, flag_pennant    │
│  │   BREAKOUT: volume_spike, orb, momentum_roc              │
│  └── Outputs: SignalBundle per symbol                       │
│      ├── PrimarySignal (highest conviction)                 │
│      ├── SignalStrength (0-1)                               │
│      ├── DirectionalBias (-1=long, 1=short)               │
│      └── StructuralLevels (SL/TP candidates)                │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                  ENHANCED AGENTIC FLOW                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  OpportunityScorer (Enhanced)                               │
│  ├── Existing: Trend/Vol/Volume/Structure scores           │
│  └── NEW: SetupQualityScore (from Strategy)                │
│      ├── +20 pts: Strong signal at structural level      │
│      ├── +10 pts: Moderate signal                          │
│      └── -10 pts: Counter-trend signal                     │
│                                                             │
│  FluidFlowEngine (Enhanced)                                 │
│  ├── Existing: flowIntensity base calculation              │
│  └── NEW: Intensity modulation by signal                   │
│      ├── signalStrength > 0.7: intensity *= 1.3            │
│      ├── signalStrength < 0.3: intensity *= 0.6            │
│      └── directionalBias affects flowDirection             │
│                                                             │
│  AdaptiveGridGeometry (Enhanced)                          │
│  ├── Existing: spread by volatility                        │
│  └── NEW: Asymmetric spread by signal                      │
│      ├── Bullish FVG: tighten buy spread 30%              │
│      ├── Bearish Sweep: widen sell spread                 │
│      └── Directional bias affects order distribution        │
│                                                             │
│  GridManager (Enhanced Entry)                               │
│  ├── Existing: time-based entry                            │
│  └── NEW: Signal-triggered entry                           │
│      ├── Wait for signal before entry                      │
│      └── Use structural levels for SL/TP                   │
└─────────────────────────────────────────────────────────────┘
```

## Phase 1: Core Integration (P0 - 2-3 days)

### Task 1.1: Create StrategySignalAggregator

**File**: `backend/internal/agentic/strategy_bridge.go`

**Components**:
```go
// StrategySignalAggregator collects and aggregates signals from sub-strategies
type StrategySignalAggregator struct {
    router         *strategy.Router           // Existing router
    classifiers    map[string]*regime.Classifier
    signalCache    map[string]*SignalBundle  // symbol -> latest signals
    mu             sync.RWMutex
    logger         *zap.Logger
}

// SignalBundle represents aggregated signals for a symbol
type SignalBundle struct {
    Symbol           string
    PrimarySignal    *strategy.Signal          // Highest conviction signal
    SignalStrength   float64                   // 0-1 aggregate strength
    DirectionalBias  float64                   // -1 (long) to 1 (short)
    StructuralLevels *StructuralLevels         // SL/TP candidates
    Confidence       float64                   // Overall confidence
    Timestamp        time.Time
}

// StructuralLevels contains key price levels from structure analysis
type StructuralLevels struct {
    SupportLevels    []float64                 // Key support zones
    ResistanceLevels []float64                 // Key resistance zones
    LiquidityHighs   []float64                 // Equal highs (liquidity)
    LiquidityLows    []float64                 // Equal lows (liquidity)
    FVGZones         []FVGZone                 // Fair Value Gaps
}
```

**Implementation Steps**:
1. Initialize Strategy Router with sub-strategies based on Agentic regime
2. Wire WebSocket klines to router.OnKline() for real-time updates
3. Collect Signals() output from each strategy per symbol
4. Aggregate signals into SignalBundle with confidence weighting
5. Expose GetSignalBundle(symbol) for other components

**Acceptance Criteria**:
- [ ] Can instantiate with subset of strategies based on regime
- [ ] Receives real-time klines and updates strategy states
- [ ] Returns SignalBundle with PrimarySignal and SignalStrength
- [ ] Updates at least every 5 seconds per symbol

### Task 1.2: Enhance OpportunityScorer with SetupQuality

**File**: `backend/internal/agentic/scorer.go`

**Changes**:
```go
// Add to OpportunityScorer
func (os *OpportunityScorer) CalculateScore(
    regime RegimeSnapshot,
    values *IndicatorValues,
    signalBundle *SignalBundle,  // NEW parameter
) float64 {
    // Existing scores
    trendScore := os.scoreTrend(values, regime)
    volScore := os.scoreVolatility(values, regime)
    volumeScore := os.scoreVolume(values)
    structureScore := os.scoreStructure(values, regime)
    
    // NEW: Setup Quality Score from strategy signals
    setupScore := os.scoreSetupQuality(signalBundle, regime)
    
    // Add setup quality as new dimension
    finalScore :=
        trendScore*weights.Trend +
        volScore*weights.Volatility +
        volumeScore*weights.Volume +
        structureScore*weights.Structure*0.7 + // Reduce weight
        setupScore*weights.SetupQuality         // NEW weight (0.2-0.3)
    
    return finalScore
}

// NEW: Score setup quality from strategy signals
func (os *OpportunityScorer) scoreSetupQuality(bundle *SignalBundle, regime RegimeSnapshot) float64 {
    if bundle == nil || bundle.PrimarySignal == nil {
        return 50.0 // Neutral
    }
    
    baseScore := 50.0
    
    // Strong signal at structural level
    if bundle.SignalStrength > 0.7 {
        baseScore += 30.0
    } else if bundle.SignalStrength > 0.4 {
        baseScore += 15.0
    }
    
    // Bonus for regime alignment
    if regime.Regime == RegimeSideways && isMeanReversionSignal(bundle.PrimarySignal) {
        baseScore += 10.0 // Bonus for mean reversion in sideways
    }
    if regime.Regime == RegimeTrending && isTrendFollowingSignal(bundle.PrimarySignal) {
        baseScore += 10.0 // Bonus for trend following
    }
    
    return min(100.0, baseScore)
}
```

**Acceptance Criteria**:
- [ ] SetupQualityScore influences final score
- [ ] Strong structural signals (>0.7) add ~20-30 points
- [ ] Score calculation includes signal bundle
- [ ] Backward compatible (nil bundle = neutral score)

### Task 1.3: Wire SignalAggregator into AgenticEngine

**File**: `backend/internal/agentic/engine.go`

**Changes**:
```go
type AgenticEngine struct {
    // ... existing fields ...
    
    // NEW: Strategy signal integration
    signalAggregator *StrategySignalAggregator
}

func (ae *AgenticEngine) runDetectionCycle(ctx context.Context) error {
    // 1. Detect regime for all symbols (existing)
    detectionResults := ae.detectAllSymbols(ctx)
    
    // 2. Collect strategy signals (NEW)
    signalBundles := ae.collectStrategySignals(detectionResults)
    
    // 3. Calculate scores with signal data (enhanced)
    scores := ae.calculateScoresWithSignals(detectionResults, signalBundles)
    
    // ... rest of cycle ...
}

func (ae *AgenticEngine) collectStrategySignals(
    detections map[string]RegimeSnapshot,
) map[string]*SignalBundle {
    bundles := make(map[string]*SignalBundle)
    
    for symbol, regime := range detections {
        // Get aggregated signals for this symbol
        bundle := ae.signalAggregator.GetSignalBundle(symbol, regime.Regime)
        bundles[symbol] = bundle
    }
    
    return bundles
}
```

**Acceptance Criteria**:
- [ ] SignalAggregator initialized in NewAgenticEngine
- [ ] WebSocket klines forwarded to strategies
- [ ] SignalBundles passed to scoring
- [ ] No performance degradation (<100ms added latency)

## Phase 2: Flow & Geometry Enhancement (P1 - 2 days)

### Task 2.1: Strategy-Aware FluidFlowEngine

**File**: `backend/internal/farming/adaptive_grid/fluid_flow.go`

**Enhancement**:
```go
// CalculateFlowWithSignals enhances flow calculation with strategy input
func (f *FluidFlowEngine) CalculateFlowWithSignals(
    symbol string,
    positionSize, volatility, risk, trend, skew, liquidity float64,
    signalBundle *SignalBundle,  // NEW
) FlowParameters {
    
    // Base calculation (existing)
    baseParams := f.CalculateFlow(positionSize, volatility, risk, trend, skew, liquidity)
    
    // NEW: Apply strategy-based modulation
    if signalBundle != nil && signalBundle.PrimarySignal != nil {
        // Modulate intensity by signal strength
        intensityMultiplier := 0.7 + (signalBundle.SignalStrength * 0.6) // 0.7-1.3x
        baseParams.Intensity *= intensityMultiplier
        
        // Apply directional bias from signal
        baseParams.Direction = signalBundle.DirectionalBias
        
        // Adjust size multiplier by signal conviction
        if signalBundle.SignalStrength > 0.8 {
            baseParams.SizeMultiplier *= 1.25 // Boost for high conviction
        } else if signalBundle.SignalStrength < 0.3 {
            baseParams.SizeMultiplier *= 0.6  // Reduce for low conviction
        }
        
        f.logger.Debug("Strategy modulation applied",
            zap.String("symbol", symbol),
            zap.Float64("intensity_mult", intensityMultiplier),
            zap.Float64("direction", baseParams.Direction),
            zap.Float64("size_mult", baseParams.SizeMultiplier))
    }
    
    return baseParams
}
```

**Acceptance Criteria**:
- [ ] Flow intensity scales with signal strength (0.7-1.3x)
- [ ] Direction follows signal bias
- [ ] Size multiplier adjusts by conviction
- [ ] Logging shows modulation factors

### Task 2.2: Strategy-Aware AdaptiveGridGeometry

**File**: `backend/internal/farming/adaptive_grid/adaptive_grid_geometry.go`

**Enhancement**:
```go
// CalculateSpreadWithSignals creates asymmetric spreads based on structure
func (g *AdaptiveGridGeometry) CalculateSpreadWithSignals(
    volatility, skew, funding float64,
    currentTime time.Time,
    signalBundle *SignalBundle,  // NEW
) GridGeometry {
    
    // Base geometry
    baseGeometry := g.CalculateBaseGeometry(volatility, skew, funding, currentTime)
    
    if signalBundle == nil || signalBundle.PrimarySignal == nil {
        return baseGeometry
    }
    
    // Asymmetric spread adjustment based on signal type
    switch signalBundle.PrimarySignal.StrategyName {
    case "fvg_fill":
        if signalBundle.PrimarySignal.Side == strategy.SideBuy {
            // Bullish FVG: tighten buy side to fill imbalance
            baseGeometry.BuySpreadMultiplier = 0.7
            baseGeometry.SellSpreadMultiplier = 1.2
        } else {
            // Bearish FVG: tighten sell side
            baseGeometry.BuySpreadMultiplier = 1.2
            baseGeometry.SellSpreadMultiplier = 0.7
        }
        
    case "liquidity_sweep":
        if signalBundle.PrimarySignal.Side == strategy.SideBuy {
            // Swept lows: expect bounce, tighten buy
            baseGeometry.BuySpreadMultiplier = 0.8
            baseGeometry.SellSpreadMultiplier = 1.3 // Wait for rejection
        } else {
            // Swept highs: tighten sell
            baseGeometry.BuySpreadMultiplier = 1.3
            baseGeometry.SellSpreadMultiplier = 0.8
        }
        
    case "bb_bounce", "vwap_reversion":
        // Mean reversion: balanced but slightly favor mean direction
        baseGeometry.BuySpreadMultiplier = 1.0
        baseGeometry.SellSpreadMultiplier = 1.0
    }
    
    return baseGeometry
}

type GridGeometry struct {
    Spread             float64
    OrderCount         int
    Spacing            float64
    BuySpreadMultiplier  float64  // NEW
    SellSpreadMultiplier float64  // NEW
}
```

**Acceptance Criteria**:
- [ ] FVG signals create asymmetric spreads (tighten fill side)
- [ ] Liquidity sweep adjusts for expected reversal
- [ ] Mean reversion strategies maintain balance
- [ ] Multipliers configurable via config

## Phase 3: Smart Entry & Risk Management (P2 - 2 days)

### Task 3.1: Signal-Triggered Grid Entry

**File**: `backend/internal/farming/grid_manager.go`

**Enhancement**:
```go
// GridEntryStrategy defines when to enter grid
type GridEntryStrategy struct {
    Mode              string  // "time_based" | "signal_triggered" | "hybrid"
    MinSignalStrength float64 // Min strength to trigger entry
    EntryTimeoutSec   int     // Max wait for signal
}

// shouldEnterGrid decides if grid should be placed now
func (gm *GridManager) shouldEnterGrid(symbol string, signalBundle *SignalBundle) bool {
    if gm.entryStrategy.Mode == "time_based" {
        return true // Existing behavior
    }
    
    if gm.entryStrategy.Mode == "signal_triggered" {
        // Wait for qualifying signal
        if signalBundle == nil || signalBundle.PrimarySignal == nil {
            return false
        }
        return signalBundle.SignalStrength >= gm.entryStrategy.MinSignalStrength
    }
    
    if gm.entryStrategy.Mode == "hybrid" {
        // Enter on signal OR timeout
        if signalBundle != nil && signalBundle.SignalStrength >= gm.entryStrategy.MinSignalStrength {
            return true
        }
        // Check if we've waited too long
        return gm.hasEntryTimeoutExpired(symbol)
    }
    
    return true
}
```

**Acceptance Criteria**:
- [ ] Configurable entry mode (time/signal/hybrid)
- [ ] Signal mode waits for qualifying signal
- [ ] Hybrid mode balances patience with opportunity cost
- [ ] Timeout prevents indefinite waiting

### Task 3.2: Structural SL/TP Integration

**File**: `backend/internal/farming/adaptive_grid/manager.go`

**Enhancement**:
```go
// SetRiskLevelsFromStructure uses strategy-derived levels for SL/TP
func (a *AdaptiveGridManager) SetRiskLevelsFromStructure(
    symbol string,
    signalBundle *SignalBundle,
) {
    if signalBundle == nil || signalBundle.StructuralLevels == nil {
        return // Use default ATR-based levels
    }
    
    levels := signalBundle.StructuralLevels
    
    // Find closest structural level for SL
    if len(levels.LiquidityLows) > 0 && signalBundle.DirectionalBias < 0 {
        // Short position: SL above liquidity high
        a.positionStopLoss[symbol] = levels.LiquidityHighs[0]
    }
    if len(levels.LiquidityHighs) > 0 && signalBundle.DirectionalBias > 0 {
        // Long position: SL below liquidity low
        a.positionStopLoss[symbol] = levels.LiquidityLows[0]
    }
    
    // TP at next structural level (support/resistance)
    if signalBundle.DirectionalBias < 0 && len(levels.SupportLevels) > 0 {
        a.positionTakeProfit[symbol] = levels.SupportLevels[0]
    }
    if signalBundle.DirectionalBias > 0 && len(levels.ResistanceLevels) > 0 {
        a.positionTakeProfit[symbol] = levels.ResistanceLevels[0]
    }
    
    a.logger.Info("Structural levels set",
        zap.String("symbol", symbol),
        zap.Float64("sl", a.positionStopLoss[symbol]),
        zap.Float64("tp", a.positionTakeProfit[symbol]))
}
```

**Acceptance Criteria**:
- [ ] SL placed at structural level (liquidity sweep high/low)
- [ ] TP placed at next support/resistance
- [ ] Fallback to ATR-based if no structure available
- [ ] Validates levels are reasonable (not too tight/wide)

## Phase 3.5: Advanced Continuous Fluid Adaptation (P0 - 2 days)

### Mục tiêu: Làm bot thực sự "Mềm mại như nước - Thiên biến vạn hóa"

Các cơ chế **continuous adaptation** thay vì discrete switches - đảm bảo bot chuyển đổi mượt mà giữa các trạng thái, không có jump discontinuities.

### Task 3.3: Continuous Strategy Blending (Multi-Strategy Fusion)

**Vấn đề hiện tại**: Chỉ chọn 1 primary signal → discrete switching giữa strategies → hard edges  
**Giải pháp**: Blend tất cả active strategies với weights liên tục biến đổi

**File**: `backend/internal/agentic/strategy_blend.go` (NEW)

```go
// StrategyBlendEngine continuously blends multiple strategy outputs
type StrategyBlendEngine struct {
    // Không chỉ 1 PrimarySignal, mà blend tất cả
    signalWeights   map[string]map[string]float64  // symbol -> strategy -> weight (0-1)
    blendHistory     map[string][]BlendSnapshot     // Track blend evolution
    decayFactor      float64                        // Exponential decay for old signals
    convergenceRate  float64                        // How fast weights adjust (0.1 = smooth)
}

type BlendSnapshot struct {
    Timestamp      time.Time
    Weights        map[string]float64  // Strategy weights
    BlendedBias    float64             // -1 to 1, continuous
    BlendedConviction float64          // 0-1, aggregate
    SignalEntropy  float64             // Measure of disagreement (high = uncertain)
}

// CalculateContinuousBlend smoothly blends all strategy signals
func (be *StrategyBlendEngine) CalculateContinuousBlend(
    symbol string,
    rawSignals map[string]*strategy.Signal,  // All strategy signals
    rawStrengths map[string]float64,           // Each signal's strength
) *BlendedSignal {
    
    // 1. Calculate raw weights by signal quality
    for strategyName, signal := range rawSignals {
        targetWeight := be.calculateTargetWeight(signal, rawStrengths[strategyName])
        
        // 2. Smooth weight transition (không jump từ 0 → 1)
        currentWeight := be.signalWeights[symbol][strategyName]
        newWeight := currentWeight + (targetWeight-currentWeight)*be.convergenceRate
        be.signalWeights[symbol][strategyName] = newWeight
    }
    
    // 3. Normalize weights tổng = 1
    be.normalizeWeights(symbol)
    
    // 4. Calculate blended output (weighted average)
    blended := &BlendedSignal{
        DirectionalBias: be.weightedBias(symbol, rawSignals),
        Conviction:      be.weightedConviction(symbol, rawStrengths),
        Entropy:         be.calculateEntropy(symbol),  // Đo mức độ đồng thuận
    }
    
    // 5. Nếu entropy cao (các strategy không đồng ý), giảm conviction
    if blended.Entropy > 0.7 {
        blended.Conviction *= 0.5  // Defensive khi không chắc chắn
        blended.IsConflicting = true
    }
    
    return blended
}
```

**Ý nghĩa thực tế**:
- **FVG** (weight 0.6) + **BB Bounce** (weight 0.3) + **VWAP** (weight 0.1) = Blend chính xác hơn
- Khi FVG yếu đi, weight giảm mượt mà từ 0.6 → 0.3 → 0.1 (không cut off đột ngột)
- Entropy cao = các strategy conflict → bot tự động giảm size, đứng ngoài

**Acceptance Criteria**:
- [ ] Weight transitions smooth (convergence rate 0.1-0.3, không jump)
- [ ] Blend output liên tục, không discrete switches
- [ ] Entropy detection hoạt động (conflict → reduce activity)
- [ ] Backward compatible (1 signal = 100% weight)

---

### Task 3.4: Signal Confidence Decay (Exponential Fade)

**Vấn đề**: Signal "suddenly" biến mất sau timeout → discrete change  
**Giải pháp**: Confidence decay theo exponential curve - mượt mà như nước chảy

**File**: `backend/internal/agentic/signal_decay.go`

```go
// SignalDecayManager manages exponential decay of signal confidence
type SignalDecayManager struct {
    halfLife       time.Duration  // Time for confidence to halve (e.g., 30s)
    minConfidence  float64        // Floor (e.g., 0.1, không decay về 0)
    decayCurves    map[string]DecayCurve
}

type DecayCurve struct {
    OriginalStrength float64
    Timestamp        time.Time
    CurrentValue     float64  // Continuously updated
}

// GetDecayedConfidence returns smoothly decayed confidence
func (dm *SignalDecayManager) GetDecayedConfidence(
    symbol string,
    strategy string,
    originalStrength float64,
    detectedAt time.Time,
) float64 {
    elapsed := time.Since(detectedAt)
    
    // Exponential decay: N(t) = N0 * (1/2)^(t/t_half)
    halfLives := float64(elapsed) / float64(dm.halfLife)
    decayed := originalStrength * math.Pow(0.5, halfLives)
    
    // Apply floor - không bao giờ fully decay về 0 (tránh sudden cut)
    if decayed < dm.minConfidence {
        return dm.minConfidence
    }
    
    return decayed
}

// SmoothRenewal khi có signal mới, không jump về 1.0 đột ngột
func (dm *SignalDecayManager) SmoothRenewal(
    symbol string,
    strategy string,
    newStrength float64,
) float64 {
    current := dm.GetCurrentConfidence(symbol, strategy)
    
    // Smooth blend giữa old và new (0.7 new + 0.3 old)
    blended := newStrength*0.7 + current*0.3
    
    return blended
}
```

**Ý nghĩa thực tế**:
- Signal FVG phát hiện 0.8 → decay: 0.8 → 0.6 → 0.4 → 0.2 (mượt mà)
- Khi FVG refresh, không jump từ 0.2 → 0.8, mà smooth: 0.2 → 0.5 → 0.7 → 0.8
- Bot không "bối rối" khi signal biến mất đột ngột

**Acceptance Criteria**:
- [ ] Confidence decay theo exponential curve
- [ ] Renewal smooth (không jump)
- [ ] Configurable half-life (10s - 120s)
- [ ] Min confidence floor (tránh zero confidence)

---

### Task 3.5: Predictive Flow Adjustment (Lead Compensation)

**Vấn đề**: Flow chỉ react sau khi signal xuất hiện → lag  
**Giải pháp**: Dựa vào **signal momentum** để predict và điều chỉnh trước

**File**: `backend/internal/agentic/predictive_flow.go`

```go
// PredictiveFlowEngine adjusts flow based on signal trajectory
type PredictiveFlowEngine struct {
    signalHistory  map[string][]SignalPoint  // Track signal evolution
    momentumWindow time.Duration             // 60s default
}

type SignalPoint struct {
    Timestamp time.Time
    Strength  float64
    Bias      float64
}

// CalculatePredictiveFlow dự đoán và điều chỉnh trước
func (pf *PredictiveFlowEngine) CalculatePredictiveFlow(
    symbol string,
    currentSignal *BlendedSignal,
) *PredictedAdjustment {
    
    history := pf.signalHistory[symbol]
    if len(history) < 3 {
        return nil  // Không đủ data để predict
    }
    
    // 1. Calculate signal momentum (trend của signal itself)
    recent := history[len(history)-3:]
    strengthMomentum := (recent[2].Strength - recent[0].Strength) / 2
    biasMomentum := (recent[2].Bias - recent[0].Bias) / 2
    
    // 2. Predict future state (lead by 30s)
    predictedStrength := currentSignal.Conviction + strengthMomentum*0.5
    predictedBias := currentSignal.DirectionalBias + biasMomentum*0.5
    
    // 3. Clamp predictions
    predictedStrength = clamp(predictedStrength, 0, 1)
    predictedBias = clamp(predictedBias, -1, 1)
    
    // 4. Calculate adjustment để "lead" the market
    adjustment := &PredictedAdjustment{
        // Nếu signal đang tăng mạnh, increase flow SỚM hơn
        IntensityBoost: math.Max(0, strengthMomentum) * 0.5,
        
        // Nếu bias đang shift, adjust direction SỚM
        DirectionLead: biasMomentum * 0.3,
        
        // Confidence dựa trên prediction reliability
        PredictionConfidence: pf.calculatePredictionConfidence(history),
    }
    
    return adjustment
}
```

**Ý nghĩa thực tế**:
- Signal FVG đang tăng mạnh (0.5 → 0.7 → 0.85) → Predict sẽ đạt 0.9 → Tăng flow TRƯỚC khi đạt 0.9
- Bias đang shift từ neutral → long → Adjust direction sớm 30s
- Tránh "chasing" signal khi đã quá muộn

**Acceptance Criteria**:
- [ ] Momentum calculation accurate (verified with historical data)
- [ ] Lead time configurable (10s - 60s)
- [ ] Prediction confidence affects adjustment magnitude
- [ ] Fallback khi prediction uncertain (use current signal)

---

### Task 3.6: Micro-Regime Detection (Nested Market States)

**Vấn đề**: Regime chỉ có 4 states (Trending/Sideways/Volatile/Recovery) → too coarse  
**Giải pháp**: Detect micro-regimes bên trong regime lớn → granular adaptation

**File**: `backend/internal/agentic/micro_regime.go`

```go
// MicroRegimeDetector detects fine-grained market states
type MicroRegimeDetector struct {
    baseRegime     RegimeType     // Regime lớn từ RegimeDetector
    microState     MicroState     // Chi tiết hơn
    confidence     float64
}

// MicroState là granular hơn RegimeType
type MicroState string

const (
    // Trong Sideways:
    MicroAccumulation    MicroState = "ACCUMULATION"     // Wyckoff accumulation
    MicroDistribution    MicroState = "DISTRIBUTION"     // Wyckoff distribution
    MicroRangeBound      MicroState = "RANGE_BOUND"      // Simple range
    MicroCompression   MicroState = "COMPRESSION"      // Before breakout
    
    // Trong Trending:
    MicroImpulse         MicroState = "IMPULSE"          // Strong momentum
    MicroPullback        MicroState = "PULLBACK"         // Trend pullback
    MicroConsolidation   MicroState = "CONSOLIDATION"    // Flag/pennant
    
    // Trong Volatile:
    MicroSpike           MicroState = "SPIKE"            // Sudden spike
    MicroChurn           MicroState = "CHURN"            // High vol, no direction
)

// DetectMicroRegime sử dụng strategy signals để detect micro-state
func (mr *MicroRegimeDetector) DetectMicroRegime(
    baseRegime RegimeType,
    signals map[string]*strategy.Signal,
    priceAction PriceActionData,
) MicroState {
    
    switch baseRegime {
    case RegimeSideways:
        // Check cho tích lũy Wyckoff
        if signals["liquidity_sweep"] != nil && 
           signals["bb_bounce"] != nil &&
           priceAction.VolumeProfile == "Ascending" {
            return MicroAccumulation  // Chuẩn bị cho breakout
        }
        
        // Check cho compression
        if priceAction.BBWidth < 0.02 && priceAction.ATR < 0.003 {
            return MicroCompression  // Sắp breakout
        }
        
        return MicroRangeBound
        
    case RegimeTrending:
        // Check pullback
        if signals["fvg_fill"] != nil && 
           signals["fvg_fill"].Side == oppositeDirection(priceAction.Trend) {
            return MicroPullback  // Trend continuation setup
        }
        
        // Check consolidation
        if signals["flag_pennant"] != nil {
            return MicroConsolidation  // Breakout imminent
        }
        
        return MicroImpulse
    }
    
    return MicroRangeBound
}
```

**Ý nghĩa thực tế**:
- **Sideways + MicroAccumulation**: Grid nên accumulate dần dần, smaller size, wider stops
- **Sideways + MicroCompression**: Prepare for breakout, reduce exposure, tighten risk
- **Trending + MicroPullback**: Add to trend position, optimal entry timing
- **Trending + MicroConsolidation**: Wait for breakout, don't fight the flag

**Acceptance Criteria**:
- [ ] Detect được ít nhất 6 micro-states
- [ ] Micro-state transitions mượt mà (không discrete jump)
- [ ] Each micro-state có specific flow parameters
- [ ] Validated với historical data

---

## Tổng kết: Kiến trúc "Mềm Mại Như Nước"

```
┌─────────────────────────────────────────────────────────────────────┐
│                    STRATEGY ECOSYSTEM (12+ strategies)              │
├─────────────────────────────────────────────────────────────────────┤
│  liquidity_sweep │ fvg_fill │ bb_bounce │ rsi_div │ vwap │ ...     │
└────────────────────┬────────────────────────────────────────────────┘
                     │ Raw Signals
                     ▼
┌─────────────────────────────────────────────────────────────────────┐
│              CONTINUOUS STRATEGY BLEND ENGINE                         │
├─────────────────────────────────────────────────────────────────────┤
│  • Smooth weight transitions (convergence rate 0.1-0.3)          │
│  • Entropy detection (conflict → reduce activity)                  │
│  • Blended output: Direction (-1 to 1) + Conviction (0-1)          │
│  • NO discrete switches, everything is continuous                   │
└────────────────────┬────────────────────────────────────────────────┘
                     │ BlendedSignal
                     ▼
┌─────────────────────────────────────────────────────────────────────┐
│              SIGNAL CONFIDENCE DECAY (Exponential Fade)             │
├─────────────────────────────────────────────────────────────────────┤
│  • Signal strength: 0.8 → 0.6 → 0.4 → 0.2 (mượt mà)               │
│  • Smooth renewal: old*0.3 + new*0.7 (không jump)               │
│  • Min floor 0.1 (tránh sudden cut to zero)                         │
└────────────────────┬────────────────────────────────────────────────┘
                     │ DecayedSignal
                     ▼
┌─────────────────────────────────────────────────────────────────────┐
│              PREDICTIVE FLOW ADJUSTMENT (Lead Compensation)         │
├─────────────────────────────────────────────────────────────────────┤
│  • Signal momentum tracking                                         │
│  • Predict 30s ahead: strength_momentum * 0.5                        │
│  • Adjust flow TRƯỚC khi signal peak                               │
│  • Lead the market, don't chase it                                  │
└────────────────────┬────────────────────────────────────────────────┘
                     │ PredictedAdjustment
                     ▼
┌─────────────────────────────────────────────────────────────────────┐
│              MICRO-REGIME DETECTION (Granular States)               │
├─────────────────────────────────────────────────────────────────────┤
│  Base: Sideways → Micro: Accumulation / Compression / Range         │
│  Base: Trending → Micro: Impulse / Pullback / Consolidation         │
│  Each micro-state → specific flow geometry                          │
└────────────────────┬────────────────────────────────────────────────┘
                     │ MicroState
                     ▼
┌─────────────────────────────────────────────────────────────────────┐
│              FLUID FLOW ENGINE (Continuous Parameters)             │
├─────────────────────────────────────────────────────────────────────┤
│  • Intensity: 0.0 → 1.0 (continuous, không binary on/off)          │
│  • Direction: -1.0 → 1.0 (continuous gradient)                      │
│  • SizeMultiplier: 0.5x → 2.0x (smooth scaling)                     │
│  • Spread: Dynamic adjustment mượt mà theo micro-state            │
└─────────────────────────────────────────────────────────────────────┘
```

**Kết quả**: Bot thực sự "thiên biến vạn hóa":
- Không có discrete states - mọi thứ là continuous gradient
- Tự động blend nhiều signals thay vì chọn 1
- Dự đoán trước và điều chỉnh flow sớm
- Phát hiện micro-structures bên trong regime lớn
- Mượt mà như nước, không có hard edges

---

## Phase 4: Testing & Validation (P1 - 2 days)

### Task 4.1: Unit Tests for Strategy Bridge

**File**: `backend/internal/agentic/strategy_bridge_test.go`

**Test Cases**:
- Signal aggregation from multiple strategies
- Regime-based strategy selection
- Signal strength calculation
- Bundle freshness/expiration

### Task 4.2: Integration Tests

**File**: `backend/internal/agentic/engine_integration_test.go`

**Test Scenarios**:
1. **Strong FVG Signal Flow**:
   - FVG detected → SignalStrength 0.8 → Flow intensity 1.3x → Asymmetric spread
   
2. **Liquidity Sweep Response**:
   - Sweep high detected → Short bias → Tighten sell spread → Wait for rejection
   
3. **No Signal Mode**:
   - No qualifying signal → Reduced intensity 0.6x → Wider spreads (defensive)

### Task 4.3: Backward Compatibility Tests

- Ensure existing behavior preserved when feature disabled
- Performance benchmarks (latency < 50ms added)
- Memory usage monitoring

## Configuration Schema

**File**: `backend/config/agentic-vf-config.yaml`

```yaml
strategy_integration:
  enabled: true
  mode: "hybrid"  # time_based | signal_triggered | hybrid
  
  scoring:
    setup_quality_weight: 0.25  # 25% of total score
    min_signal_strength: 0.4    # Threshold for scoring bonus
    
  flow_modulation:
    enabled: true
    intensity_multiplier_max: 1.3
    intensity_multiplier_min: 0.6
    
  geometry:
    asymmetric_spreads: true
    fvg_spread_tighten_pct: 30   # Tighten 30% on FVG side
    sweep_spread_widen_pct: 30 # Widen 30% on swept side
    
  entry:
    mode: "hybrid"
    min_signal_strength: 0.5
    entry_timeout_sec: 60
    
  risk:
    use_structural_sl: true
    sl_buffer_pct: 0.5  # Add 0.5% buffer to structural level
    tp_at_next_structure: true
    
  # Strategy selection by regime
  regime_strategies:
    sideways:
      - bb_bounce
      - rsi_divergence
      - fvg_fill
      - liquidity_sweep
    trending:
      - ema_cross
      - breakout_retest
      - flag_pennant
    breakout:
      - volume_spike
      - orb
      - momentum_roc
```

## Success Criteria

### Quantitative Metrics
1. **Signal Responsiveness**: Strategy signals influence flow within 1 detection cycle (< 30s)
2. **Setup Quality Impact**: Strong signals (>0.7) increase symbol scores by 15-30 points
3. **Flow Modulation**: Intensity varies 0.6x to 1.3x based on signal strength
4. **Entry Precision**: Signal-triggered entries have 20% better initial position PnL
5. **Backward Compatibility**: Zero breaking changes when disabled

### Qualitative Metrics
1. Bot demonstrates "awareness" of structural levels
2. Grid spreads adjust intelligently to setup type
3. SL/TP placed at meaningful market structure
4. Reduced over-trading in low-conviction conditions

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Signal latency | Medium | Medium | Cache signals, async processing |
| Conflicting signals | High | Low | Weight by conviction, majority voting |
| Over-fitting to structure | Medium | High | ATR-based fallback always available |
| Performance degradation | Low | Medium | Benchmark tests, profiling |
| False signal spikes | Medium | High | Minimum strength threshold, cooldown |

## Out of Scope

1. **Full Strategy Router replacement** - We enhance, not replace, existing Agentic logic
2. **Complex multi-strategy consensus** - Simple primary signal selection
3. **Machine learning signal weighting** - Manual config-based weights
4. **Cross-symbol correlation signals** - Per-symbol only
5. **Historical backtesting engine** - Forward testing only

## Timeline Summary

| Phase | Duration | Deliverables |
|-------|----------|--------------|
| Phase 1: Core Integration | 2-3 days | StrategyBridge, Enhanced Scoring, Wired Engine |
| Phase 2: Flow & Geometry | 2 days | Modulated Flow, Asymmetric Spreads |
| Phase 3: Smart Entry & Risk | 2 days | Signal Entry, Structural SL/TP |
| Phase 4: Testing | 2 days | Unit/Integration tests, Validation |
| **Total** | **8-9 days** | |

## Dependencies

1. ✅ Strategy ecosystem already exists (`internal/strategy`)
2. ✅ WebSocket kline stream already active
3. ✅ RegimeDetector already classifies markets
4. ✅ FluidFlowEngine already modulates intensity
5. ⚠️ May need Strategy interface adapter for Agentic types
