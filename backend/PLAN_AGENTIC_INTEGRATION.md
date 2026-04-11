# Agentic + Volume Farm Integration Plan

## Architecture Overview

**Agentic = Decision Layer**  
**Volume Farm = Execution Layer**

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           AGENTIC DECISION LAYER                            │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  AgenticEngine                                                      │   │
│  │  ├── MultiPairRegimeDetector (per symbol regime detection)         │   │
│  │  ├── OpportunityScorer (score symbols by regime quality)           │   │
│  │  ├── SymbolPrioritizer (rank & select symbols to trade)            │   │
│  │  └── VolumeFarmController (điều khiển VF engine)                   │   │
│  └──────────────────────────────┬──────────────────────────────────────┘   │
└─────────────────────────────────┼───────────────────────────────────────────┘
                                  ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│                         VOLUME FARM EXECUTION LAYER                         │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  VolumeFarmEngine (existing - KHÔNG SỬA)                           │   │
│  │  ├── SymbolSelector (chọn symbols từ whitelist)                    │   │
│  │  ├── GridManager (đặt lệnh, quản lý vị thế)                        │   │
│  │  ├── RiskManager (giới hạn position, check balance)              │   │
│  │  └── AdaptiveGridManager (điều chỉnh grid theo thị trường)         │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Core Concept

**Volume Farm** đã có:
- ✅ Multi-symbol support (SymbolSelector)
- ✅ Risk management (RiskManager)
- ✅ Grid execution (GridManager)
- ✅ Balance checking (trong RiskManager)
- ✅ Config structure (VolumeFarmConfig)

**Agentic** thêm vào:
- 🔮 **Regime Detection** per symbol (trending/sideways/volatile)
- 🔮 **Scoring** - điểm cơ hội cho từng symbol
- 🔮 **Prioritization** - chọn symbols tốt nhất để trade
- 🔮 **Dynamic Whitelist** - thay đổi whitelist VF theo regime

## Luồng Hoạt Động

```
┌────────────────────────────────────────────────────────────────────┐
│  1. AGENTIC DETECTION LOOP (mỗi 30s)                               │
│     └── For each symbol trong universe:                            │
│         ├── Fetch candles                                          │
│         ├── Detect regime (ADX, BB, ATR)                            │
│         └── Calculate opportunity score                            │
└────────────────────────────────┬───────────────────────────────────┘
                                 ↓
┌────────────────────────────────────────────────────────────────────┐
│  2. SCORING & PRIORITIZATION                                       │
│     └── Rank symbols by score:                                     │
│         ├── Score >= 75: HIGH (priority deploy)                   │
│         ├── Score 60-75: MEDIUM (reduced size)                    │
│         ├── Score 40-60: LOW (monitor only)                         │
│         └── Score < 40: SKIP (unfavorable)                          │
└────────────────────────────────┬───────────────────────────────────┘
                                 ↓
┌────────────────────────────────────────────────────────────────────┐
│  3. WHITELIST MANAGEMENT                                           │
│     └── Update VolumeFarm whitelist:                               │
│         ├── Thêm HIGH/MEDIUM symbols vào whitelist                │
│         ├── Giữ symbols đang có position (dù score thấp)          │
│         └── Loại bỏ LOW/SKIP symbols khỏi whitelist                │
└────────────────────────────────┬───────────────────────────────────┘
                                 ↓
┌────────────────────────────────────────────────────────────────────┐
│  4. VOLUME FARM EXECUTION (existing logic)                         │
│     └── SymbolSelector đọc whitelist:                              │
│         ├── Chọn symbols từ whitelist                            │
│         ├── RiskManager check balance                              │
│         ├── GridManager đặt lệnh                                   │
│         └── RiskManager giám sát position                          │
└────────────────────────────────────────────────────────────────────┘
```

## Data Flow

```
┌──────────────┐     ┌──────────────────┐     ┌─────────────────────┐
│   Symbols    │────→│  AgenticEngine   │────→│ VolumeFarmEngine    │
│  Universe    │     │                  │     │                     │
│ (20-50 cặp)  │     │ • Detect regime  │     │ • SymbolSelector    │
│              │     │ • Score symbols  │     │ • GridManager       │
│              │     │ • Update whitelist│    │ • RiskManager       │
└──────────────┘     └──────────────────┘     └─────────────────────┘
                            │
                            ↓
                     ┌──────────────┐
                     │   Decision   │
                     │   Output     │
                     │              │
                     │ whitelist:   │
                     │ [BTC, ETH]   │
                     │              │
                     │ positionSize │
                     │ adjustments │
                     │ per symbol   │
                     └──────────────┘
```

## Config Structure (Unified)

Dùng **volume-farm-config.yaml** làm base, thêm agentic section:

```yaml
# =============================================================================
# VOLUME FARMING + AGENTIC CONFIG
# =============================================================================

# ... (giữ nguyên toàn bộ volume-farm config) ...

# =============================================================================
# AGENTIC DECISION LAYER (NEW)
# =============================================================================
agentic:
  enabled: true
  
  # Symbol universe - các cặp agentic sẽ theo dõi và đánh giá
  universe:
    symbols:
      - BTCUSD1
      - ETHUSD1
      - SOLUSD1
      - LINKUSD1
      - DOGEUSD1
      - XRPUSD1
      - ADAUSD1
      - AVAXUSD1
      - MATICUSD1
      - DOTUSD1
    
    # Hoặc auto-discover từ top volume
    auto_discover: false
    top_volume_count: 20
    min_24h_volume_usd: 10000000  # $10M
  
  # Regime Detection Settings
  regime_detection:
    update_interval: 30s
    adx_period: 14
    bb_period: 20
    atr_period: 14
    
    thresholds:
      sideways_adx_max: 25
      trending_adx_min: 25
      volatile_atr_spike: 3.0
  
  # Scoring Weights
  scoring:
    weights:
      trend: 0.30
      volatility: 0.25
      volume: 0.25
      structure: 0.20
    
    thresholds:
      high_score: 75      # Deploy full
      medium_score: 60    # Deploy reduced
      low_score: 40       # Monitor only
      skip_score: 0       # Skip
  
  # Position Sizing per Regime
  position_sizing:
    score_multipliers:
      high: 1.0
      medium: 0.6
      low: 0.3
    
    regime_multipliers:
      sideways: 1.0     # Grid hoạt động tốt
      trending: 0.7     # Giảm size vì rủi ro
      volatile: 0.5     # Giảm nhiều hơn
      recovery: 0.8     # Cẩn trọng
  
  # Whitelist Management
  whitelist_management:
    max_symbols: 5              # Tối đa symbols trong whitelist
    min_score_to_add: 60        # Score tối thiểu để thêm vào whitelist
    score_to_remove: 35         # Score dưới này thì remove
    keep_if_position_open: true # Giữ symbol nếu đang có position
    replace_immediately: true  # Thay thế ngay khi có symbol tốt hơn
  
  # Circuit Breakers (override VF breakers nếu cần)
  circuit_breakers:
    volatility_spike:
      enabled: true
      atr_multiplier: 3.0
    
    consecutive_losses:
      enabled: true
      threshold: 3
      size_reduction: 0.5

# ... (giữ nguyên volume_farming, risk, funding_rate, etc.) ...
```

## Component Design

### 1. AgenticEngine (New)

```go
package agentic

import (
    "aster-bot/internal/config"
    "aster-bot/internal/farming"
)

// AgenticEngine là decision layer
type AgenticEngine struct {
    config           *config.AgenticConfig
    volumeFarmEngine *farming.VolumeFarmEngine
    
    // Components
    regimeDetectors  map[string]*RegimeDetector  // 1 per symbol
    opportunityScorer *OpportunityScorer
    whitelistManager  *WhitelistManager
    
    // State
    symbolScores     map[string]SymbolScore
    currentWhitelist []string
    isRunning        bool
    stopCh           chan struct{}
}

// SymbolScore chứa điểm và metadata
type SymbolScore struct {
    Symbol          string
    Score           float64          // 0-100
    Regime          RegimeType       // TRENDING, SIDEWAYS, VOLATILE
    Confidence      float64          // Confidence của regime detection
    Factors         map[string]float64 // Chi tiết từng yếu tố
    LastUpdated     time.Time
    Recommendation  Recommendation // HIGH, MEDIUM, LOW, SKIP
}

type Recommendation string
const (
    RecHigh   Recommendation = "HIGH"    // Score >= 75
    RecMedium Recommendation = "MEDIUM"  // Score 60-75
    RecLow    Recommendation = "LOW"      // Score 40-60
    RecSkip   Recommendation = "SKIP"     // Score < 40
)
```

### 2. RegimeDetector (per symbol)

```go
// RegimeDetector phát hiện chế độ thị trường cho 1 symbol
type RegimeDetector struct {
    symbol         string
    config         *RegimeDetectionConfig
    candles        []Candle
    calculator     *IndicatorCalculator
    currentRegime  RegimeSnapshot
}

func (rd *RegimeDetector) Detect() (RegimeSnapshot, error) {
    // Tính ADX, BB Width, ATR
    values := rd.calculator.CalculateAll(rd.candles)
    
    // Classify regime
    if values.ADX > rd.config.Thresholds.TrendingADXMin {
        return RegimeTrending, values
    } else if values.ATR14 > rd.config.Thresholds.VolatileATRSpike * avgATR {
        return RegimeVolatile, values
    } else {
        return RegimeSideways, values
    }
}
```

### 3. OpportunityScorer

```go
// OpportunityScorer tính điểm cơ hội
type OpportunityScorer struct {
    config *ScoringConfig
}

func (os *OpportunityScorer) CalculateScore(
    regime RegimeSnapshot,
    values *IndicatorValues,
) float64 {
    // Trend score (0-100)
    trendScore := os.scoreTrend(values)
    
    // Volatility score (0-100)
    volScore := os.scoreVolatility(values)
    
    // Volume score (0-100)
    volumeScore := os.scoreVolume(values)
    
    // Structure score (0-100)
    structScore := os.scoreStructure(values)
    
    // Weighted final score
    finalScore := 
        trendScore * os.config.Weights.Trend +
        volScore * os.config.Weights.Volatility +
        volumeScore * os.config.Weights.Volume +
        structScore * os.config.Weights.Structure
    
    // Adjust based on regime
    if regime.Regime == RegimeSideways {
        finalScore *= 1.1 // Bonus cho grid trading
    } else if regime.Regime == RegimeVolatile {
        finalScore *= 0.7 // Penalty cho volatility cao
    }
    
    return finalScore
}
```

### 4. WhitelistManager

```go
// WhitelistManager quản lý whitelist cho VolumeFarm
type WhitelistManager struct {
    config          *WhitelistConfig
    currentScores   map[string]SymbolScore
    activeWhitelist []string
    vfEngine        *farming.VolumeFarmEngine
}

func (wm *WhitelistManager) UpdateWhitelist(scores map[string]SymbolScore) {
    var newWhitelist []string
    
    // 1. Thêm HIGH/MEDIUM scores
    for symbol, score := range scores {
        if score.Recommendation == RecHigh || score.Recommendation == RecMedium {
            if len(newWhitelist) < wm.config.MaxSymbols {
                newWhitelist = append(newWhitelist, symbol)
            }
        }
    }
    
    // 2. Giữ symbols đang có position (nếu config cho phép)
    if wm.config.KeepIfPositionOpen {
        activePositions := wm.vfEngine.GetActivePositions()
        for _, pos := range activePositions {
            if !contains(newWhitelist, pos.Symbol) {
                newWhitelist = append(newWhitelist, pos.Symbol)
            }
        }
    }
    
    // 3. Sắp xếp theo score (cao nhất đầu tiên)
    sort.Slice(newWhitelist, func(i, j int) bool {
        return scores[newWhitelist[i]].Score > scores[newWhitelist[j]].Score
    })
    
    // 4. Cắt bớt nếu vượt max
    if len(newWhitelist) > wm.config.MaxSymbols {
        newWhitelist = newWhitelist[:wm.config.MaxSymbols]
    }
    
    // 5. Update VolumeFarm whitelist
    wm.vfEngine.UpdateWhitelist(newWhitelist)
    wm.activeWhitelist = newWhitelist
}
```

### 5. Integration với VolumeFarmEngine

```go
// Trong VolumeFarmEngine, thêm method:

func (vfe *VolumeFarmEngine) UpdateWhitelist(symbols []string) {
    vfe.symbolSelector.SetWhitelist(symbols)
    vfe.logger.Info("Whitelist updated by Agentic",
        zap.Strings("symbols", symbols))
}

func (vfe *VolumeFarmEngine) GetActivePositions() []Position {
    return vfe.gridManager.GetActivePositions()
}
```

## Main Entry Point (cmd/agentic/main.go)

```go
package main

import (
    "aster-bot/internal/agentic"
    "aster-bot/internal/config"
    "aster-bot/internal/farming"
)

func main() {
    // 1. Load unified config (volume-farm + agentic)
    cfg, err := config.Load("config/agentic-vf-config.yaml")
    if err != nil {
        log.Fatal(err)
    }
    
    // 2. Initialize VolumeFarmEngine (execution layer)
    vfEngine, err := farming.NewVolumeFarmEngine(cfg, logger)
    if err != nil {
        log.Fatal(err)
    }
    
    // 3. Initialize AgenticEngine (decision layer)
    agenticEngine, err := agentic.NewAgenticEngine(cfg.Agentic, vfEngine)
    if err != nil {
        log.Fatal(err)
    }
    
    // 4. Start both engines
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Start Volume Farm first
    go vfEngine.Start(ctx)
    
    // Then start Agentic (điều khiển VF qua whitelist)
    go agenticEngine.Start(ctx)
    
    // Wait for shutdown
    <-sigChan
    
    // Graceful shutdown
    agenticEngine.Stop()
    vfEngine.Stop(shutdownCtx)
}
```

## Execution Flow Detail

```
TIME: 00:00
┌────────────────────────────────────────────────────────────┐
│ Agentic: Khởi tạo, đọc universe (20 symbols)              │
│ VolumeFarm: Khởi tạo, chờ whitelist từ Agentic            │
└────────────────────────────────────────────────────────────┘

TIME: 00:30
┌────────────────────────────────────────────────────────────┐
│ Agentic: Detect regime cho 20 symbols                       │
│          BTC: TRENDING, score 45 (SKIP)                    │
│          ETH: SIDEWAYS, score 78 (HIGH) ←                  │
│          SOL: SIDEWAYS, score 72 (HIGH) ←                │
│          LINK: TRENDING, score 55 (LOW)                   │
│          ...                                               │
└────────────────────────────────────────────────────────────┘
                            ↓
┌────────────────────────────────────────────────────────────┐
│ Agentic: Update whitelist = [ETH, SOL] (max 5, lấy top 2)   │
└────────────────────────────────────────────────────────────┘
                            ↓
┌────────────────────────────────────────────────────────────┐
│ VolumeFarm: SymbolSelector nhận whitelist mới              │
│             Bắt đầu trade ETH, SOL                        │
│             GridManager đặt lệnh                         │
└────────────────────────────────────────────────────────────┘

TIME: 01:00
┌────────────────────────────────────────────────────────────┐
│ Agentic: Re-evaluate                                       │
│          ETH: SIDEWAYS, score 75 (HIGH) - giữ            │
│          SOL: TRENDING, score 45 (SKIP) - xem xét đóng    │
│          LINK: SIDEWAYS, score 80 (HIGH) - thêm mới       │
└────────────────────────────────────────────────────────────┘
                            ↓
┌────────────────────────────────────────────────────────────┐
│ Agentic: Update whitelist = [ETH, LINK]                     │
│          (SOL bị remove vì score thấp + chưa có pos)     │
└────────────────────────────────────────────────────────────┘
                            ↓
┌────────────────────────────────────────────────────────────┐
│ VolumeFarm: Dừng mở position mới SOL                      │
│             Tiếp tục giữ position SOL hiện tại            │
│             Bắt đầu trade LINK                            │
└────────────────────────────────────────────────────────────┘
```

## Benefits

| Aspect | Before (VF only) | After (Agentic + VF) |
|--------|-----------------|---------------------|
| **Symbol Selection** | Static whitelist | Dynamic based on regime |
| **Risk Management** | Fixed per symbol | Adjusted by market condition |
| **Opportunity** | Trade all whitelist symbols | Focus on best opportunities |
| **Capital Efficiency** | Spread evenly | Concentrate on high-score |
| **Adaptability** | Manual config updates | Auto regime detection |

## Files to Create/Modify

### New Files
1. `internal/agentic/engine.go` - AgenticEngine
2. `internal/agentic/detector.go` - RegimeDetector per symbol
3. `internal/agentic/scorer.go` - OpportunityScorer
4. `internal/agentic/whitelist.go` - WhitelistManager
5. `internal/agentic/config.go` - AgenticConfig types

### Modified Files
1. `internal/farming/volume_farm_engine.go` - Add UpdateWhitelist(), GetActivePositions()
2. `internal/farming/symbol_selector.go` - Add SetWhitelist()
3. `internal/config/config.go` - Add AgenticConfig
4. `cmd/agentic/main.go` - Unified entry point

### Config
1. `config/agentic-vf-config.yaml` - Unified config

## Migration Path

```
Phase 1: Agentic Layer
├── Create detector, scorer, whitelist manager
└── Test standalone (no VF integration)

Phase 2: Integration
├── Modify VF engine to accept dynamic whitelist
├── Wire Agentic → VF
└── Test together

Phase 3: Unified Config
├── Merge agentic config into volume-farm-config
├── Update main.go
└── Full testing

Phase 4: Termux Scripts
├── Update run-agentic-termux.sh
└── Add agentic-specific make targets
```
