# Multi-Pair Parallel Trading Refactor Plan

## Overview

Chuyển đổi từ **single-pair trading** sang **multi-pair parallel trading** với balance check trước khi mở vị thế.

---

## 1. Current State Analysis

### 1.1 Problems Identified

| Component | Issue | Location |
|-----------|-------|----------|
| `main.go` | Hardcode single symbol, single DataProvider, single trading loop | Line 66-152 |
| `adapter.go:106` | `currentPair string` - single string instead of map | Line 106 |
| `adapter.go:220` | Handler hardcoded to "BTCUSD1" | Line 220 |
| `detector.go` | Single candleBuffer cho tất cả symbols | Line 16 |
| **Thiếu** | Balance/Margin check trước khi deploy | N/A |

### 1.2 Current Flow (Single-Pair)
```
main.go → LoadConfig → NewCompleteAgent → Start(symbol)
                                   ↓
                        Single Detector (1 candleBuffer)
                                   ↓
                        Single DecisionHandler (1 currentPair)
                                   ↓
                        Single Trading Loop
```

---

## 2. Target Architecture (Multi-Pair)

### 2.1 New Component Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                        main.go                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Parse symbols (comma-separated)                         │   │
│  │  e.g., "BTCUSDT,ETHUSDT,SOLUSDT,LINKUSDT"               │   │
│  └──────────────────────┬───────────────────────────────────┘   │
└─────────────────────────┼───────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────┐
│                    MultiPairAgent                                 │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  PortfolioManager                                          │ │
│  │  ├── BalanceChecker (margin, available, exposure)          │ │
│  │  ├── PairLimiter (max concurrent pairs)                    │ │
│  │  └── ExposureTracker (total position size)                 │ │
│  └──────────────────────┬─────────────────────────────────────┘ │
│                         ↓                                       │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  MultiPairDetector                                         │ │
│  │  └── map[string]*Detector  (1 per symbol)                 │ │
│  └──────────────────────┬─────────────────────────────────────┘ │
│                         ↓                                       │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  PairExecutors (map[string]*PairExecutor)                  │ │
│  │  ├── Each has: DataProvider, Handler, TradingLoop        │ │
│  │  └── Independent decision making per pair                │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 Decision Flow (Multi-Pair)

```
Every tick (30s):
    ┌────────────────────────────────────────────────────────┐
    │  1. Check Portfolio Balance                            │
    │     ├── Get available margin                           │
    │     ├── Get used margin                                │
    │     └── Calculate remaining capacity                   │
    └────────────────────┬───────────────────────────────────┘
                         ↓
    ┌────────────────────────────────────────────────────────┐
    │  2. For each configured symbol:                        │
    │     a. Fetch candles (concurrent)                      │
    │     b. Detect regime                                   │
    │     c. Calculate decision score                        │
    └────────────────────┬───────────────────────────────────┘
                         ↓
    ┌────────────────────────────────────────────────────────┐
    │  3. Rank symbols by score (opportunity quality)        │
    │     └── Sort: highest score first                      │
    └────────────────────┬───────────────────────────────────┘
                         ↓
    ┌────────────────────────────────────────────────────────┐
    │  4. Deploy decision:                                     │
    │     For each symbol in ranked order:                   │
    │     ├── If already trading → check regime change       │
    │     │   ├── Regime xấu → reduce/close                 │
    │     │   └── Regime tốt → continue/maintain            │
    │     └── If NOT trading → check balance                 │
    │         ├── Đủ balance + score đủ cao → deploy grid   │
    │         └── Không đủ balance → skip                   │
    └────────────────────────────────────────────────────────┘
```

---

## 3. Data Model

### 3.1 New Types

```go
// PairState tracks trading state for each symbol
type PairState struct {
    Symbol          string
    IsActive        bool              // Đang có vị thế mở
    Detector        *Detector         // Regime detector riêng
    Handler         *DecisionHandler  // Decision handler riêng
    DataProvider    *DataProvider     // Data fetcher riêng
    CurrentRegime   RegimeSnapshot
    CurrentDecision *TradingDecision
    PositionInfo    *PositionInfo     // Thông tin vị thế hiện tại
    LastUpdated     time.Time
}

// PositionInfo tracks position for a pair
type PositionInfo struct {
    HasPosition     bool
    PositionSize    float64
    EntryPrice      float64
    UnrealizedPnL   float64
    MarginUsed      float64
    GridOrders      []OrderInfo       // Grid orders đang mở
}

// BalanceInfo from exchange
type BalanceInfo struct {
    TotalBalance    float64
    AvailableMargin float64
    UsedMargin      float64
    UnrealizedPnL   float64
    MaintenanceMargin float64
}

// PortfolioConfig
type PortfolioConfig struct {
    MaxConcurrentPairs  int     // Tối đa số cặp song song (default: 5)
    MinMarginPerPair    float64 // Margin tối thiểu để mở cặp mới
    MaxTotalExposure    float64 // % của balance tối đa dùng cho tất cả vị thế
    MinScoreToDeploy    float64 // Score tối thiểu để mở cặp mới
    ScoreThresholdClose float64 // Score dưới ngưỡng này thì đóng vị thế
}
```

### 3.2 Modified Types

```go
// MultiPairDetector - replaces single Detector
type MultiPairDetector struct {
    detectors map[string]*Detector
    config    *RegimeDetectionConfig
    mu        sync.RWMutex
}

// PortfolioManager - NEW COMPONENT
type PortfolioManager struct {
    config      PortfolioConfig
    pairStates  map[string]*PairState
    balanceInfo *BalanceInfo
    breakerMgr  CircuitBreakerManager
    mu          sync.RWMutex
}

// MultiPairAgent - replaces CompleteAgent
type MultiPairAgent struct {
    Config          *AgentConfig
    PortfolioMgr    *PortfolioManager
    Detectors       *MultiPairDetector
    Breakers        CircuitBreakerManager
    Logger          *DefaultDecisionLogger
    PatternStore    *PatternStore
    AlertManager    *AlertManager
    
    symbols         []string
    isRunning       bool
    stopCh          chan struct{}
}
```

---

## 4. Configuration Changes

### 4.1 New Config Section (agentic-config.yaml)

```yaml
agent:
  enabled: true
  
  # ... existing config ...
  
  # NEW: Portfolio Management
  portfolio:
    max_concurrent_pairs: 5          # Tối đa 5 cặp song song
    min_margin_per_pair: 100.0       # USDT - margin tối thiểu mỗi cặp
    max_total_exposure: 0.8          # 80% balance tối đa
    min_score_to_deploy: 65          # Score tối thiểu mở cặp mới
    score_threshold_close: 40        # Score dưới này thì đóng
    
    # Pair priority (được ưu tiên trước)
    priority_pairs:                   # Luôn check trước
      - BTCUSDT
      - ETHUSDT
    
    # All tradable pairs
    symbols:                        # Tất cả cặp có thể trade
      - BTCUSDT
      - ETHUSDT
      - SOLUSDT
      - LINKUSDT
      - DOGEUSDT
      - XRPUSDT
      
    # Dynamic pair selection
    dynamic_selection:
      enabled: true
      top_volume_count: 20          # Chọn từ top 20 volume
      min_24h_volume: 10000000      # $10M volume tối thiểu
```

### 4.2 CLI Flag Changes

```go
// Từ:
var symbol = flag.String("symbol", "BTCUSDT", "Trading pair symbol")

// Thành:
var symbols = flag.String("symbols", "BTCUSDT,ETHUSDT", "Comma-separated trading pairs")
var maxPairs = flag.Int("max-pairs", 5, "Maximum concurrent trading pairs")
```

---

## 5. Interface Contracts

### 5.1 PortfolioManager Interface

```go
type PortfolioManagerInterface interface {
    // Balance Management
    UpdateBalance(info *BalanceInfo) error
    GetBalance() *BalanceInfo
    GetAvailableCapacity() float64
    
    // Pair State Management
    GetPairState(symbol string) *PairState
    SetPairActive(symbol string, active bool) error
    GetActivePairs() []string
    
    // Deployment Decisions
    CanDeployNewPair(symbol string, score float64) (bool, string)
    ShouldClosePair(symbol string, score float64) (bool, string)
    GetDeployablePairs(scores map[string]float64) []string
    
    // Lifecycle
    Start() error
    Stop() error
}
```

### 5.2 BalanceChecker Interface

```go
type BalanceCheckerInterface interface {
    // Fetch from exchange
    FetchBalance() (*BalanceInfo, error)
    
    // Calculations
    CalculateMarginForPair(symbol string, positionSize float64) float64
    GetPositionInfo(symbol string) (*PositionInfo, error)
    
    // Validation
    HasSufficientBalance(symbol string, requiredMargin float64) bool
    IsWithinExposureLimit(newPosition float64) bool
}
```

### 5.3 MultiPairDetector Interface

```go
type MultiPairDetectorInterface interface {
    // Per-symbol operations
    Detect(symbol string) (RegimeSnapshot, error)
    UpdateCandles(symbol string, candles []Candle) error
    GetCurrent(symbol string) RegimeSnapshot
    
    // Batch operations
    DetectAll() map[string]RegimeSnapshot
    GetAllCurrent() map[string]RegimeSnapshot
    
    // Lifecycle
    AddSymbol(symbol string) error
    RemoveSymbol(symbol string) error
}
```

---

## 6. Implementation Phases

### Phase 1: Foundation (Core Types)

**Files to create:**
- `internal/agent/portfolio.go` - PortfolioManager implementation
- `internal/agent/balance.go` - BalanceChecker implementation
- `internal/agent/multipair_detector.go` - MultiPairDetector implementation
- `internal/agent/pair_executor.go` - Per-pair trading loop

**Files to modify:**
- `internal/agent/types.go` - Add new types
- `internal/agent/config.go` - Add PortfolioConfig parsing

### Phase 2: Refactor Core Components

**Changes:**
1. **MultiPairDetector**: Wrap multiple Detectors in map
2. **PortfolioManager**: Implement balance checks, pair limiting
3. **DecisionHandler**: Make it per-pair instance
4. **CompleteAgent** → **MultiPairAgent**: Orchestrate multiple pairs

### Phase 3: Update Entry Point

**Files to modify:**
- `cmd/agentic/main.go`:
  - Parse comma-separated symbols
  - Initialize MultiPairAgent
  - Start multiple trading loops (concurrent)

### Phase 4: Integration & Testing

**Tasks:**
- Wire BalanceChecker with exchange API
- Test concurrent pair detection
- Test balance constraints
- Test pair limit enforcement

### Phase 5: Configuration & Scripts

**Files to modify:**
- `config/agentic-config.yaml` - Add portfolio section
- `Makefile` - Update agentic targets for multi-symbol support
- `run-agentic-termux.sh` - Support multiple symbols

---

## 7. Key Algorithms

### 7.1 Pair Selection Algorithm

```go
func (pm *PortfolioManager) SelectPairsToTrade(opportunities map[string]float64) []string {
    var selected []string
    
    // 1. Sort by score (descending)
    type pairScore struct {
        symbol string
        score  float64
    }
    var scores []pairScore
    for sym, score := range opportunities {
        scores = append(scores, pairScore{sym, score})
    }
    sort.Slice(scores, func(i, j int) bool {
        return scores[i].score > scores[j].score
    })
    
    // 2. Check each in order
    for _, ps := range scores {
        // Already trading?
        if pm.IsPairActive(ps.symbol) {
            selected = append(selected, ps.symbol)
            continue
        }
        
        // Can we open new?
        if can, _ := pm.CanDeployNewPair(ps.symbol, ps.score); can {
            selected = append(selected, ps.symbol)
        }
        
        // Hit max pairs?
        if len(selected) >= pm.config.MaxConcurrentPairs {
            break
        }
    }
    
    return selected
}
```

### 7.2 Balance Check Logic

```go
func (pm *PortfolioManager) CanDeployNewPair(symbol string, score float64) (bool, string) {
    // Check 1: Score đủ cao?
    if score < pm.config.MinScoreToDeploy {
        return false, fmt.Sprintf("Score %.1f < threshold %.1f", score, pm.config.MinScoreToDeploy)
    }
    
    // Check 2: Chưa vượt số cặp tối đa?
    activeCount := len(pm.GetActivePairs())
    if activeCount >= pm.config.MaxConcurrentPairs {
        return false, fmt.Sprintf("Already at max pairs: %d/%d", activeCount, pm.config.MaxConcurrentPairs)
    }
    
    // Check 3: Đủ margin?
    balance := pm.GetBalance()
    requiredMargin := pm.config.MinMarginPerPair
    if balance.AvailableMargin < requiredMargin {
        return false, fmt.Sprintf("Insufficient margin: %.2f < %.2f", 
            balance.AvailableMargin, requiredMargin)
    }
    
    // Check 4: Không vượt exposure limit?
    estimatedExposure := pm.CalculateTotalExposure() + requiredMargin
    maxExposure := balance.TotalBalance * pm.config.MaxTotalExposure
    if estimatedExposure > maxExposure {
        return false, fmt.Sprintf("Would exceed exposure limit: %.2f > %.2f",
            estimatedExposure, maxExposure)
    }
    
    return true, "OK"
}
```

### 7.3 Concurrent Trading Loop

```go
func (mpa *MultiPairAgent) Start(ctx context.Context) error {
    // Start balance monitor
    go mpa.balanceMonitorLoop(ctx)
    
    // Start regime detection for all pairs
    for _, symbol := range mpa.symbols {
        go mpa.pairDetectionLoop(ctx, symbol)
    }
    
    // Start trading coordinator
    go mpa.tradingCoordinatorLoop(ctx)
    
    return nil
}

func (mpa *MultiPairAgent) tradingCoordinatorLoop(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-mpa.stopCh:
            return
        case <-ticker.C:
            // 1. Collect all opportunities
            opportunities := make(map[string]float64)
            for _, sym := range mpa.symbols {
                if state := mpa.PortfolioMgr.GetPairState(sym); state != nil {
                    opportunities[sym] = state.CurrentDecision.FinalScore
                }
            }
            
            // 2. Select pairs to trade
            toTrade := mpa.PortfolioMgr.SelectPairsToTrade(opportunities)
            
            // 3. Execute decisions (concurrent)
            var wg sync.WaitGroup
            for _, sym := range toTrade {
                wg.Add(1)
                go func(symbol string) {
                    defer wg.Done()
                    mpa.executePairDecision(symbol)
                }(sym)
            }
            wg.Wait()
        }
    }
}
```

---

## 8. Risk & Safety Considerations

### 8.1 Circuit Breakers (Portfolio Level)

```go
// Thêm vào PortfolioManager
type PortfolioCircuitBreakers struct {
    TotalDrawdown     float64  // Drawdown toàn portfolio
    MarginRatio       float64  // Used / Total margin
    ConcentrationRisk float64  // % exposure ở 1 pair
}
```

### 8.2 Concurrency Safety

```go
// All map accesses must be RLock/RUnlock
type PortfolioManager struct {
    pairStates map[string]*PairState
    mu         sync.RWMutex  // Bảo vệ pairStates
    
    balanceInfo *BalanceInfo
    balanceMu   sync.RWMutex // Bảo vệ balanceInfo
}
```

---

## 9. Testing Strategy

### 9.1 Unit Tests

```go
// Test balance constraints
func TestCanDeployNewPair_BalanceCheck(t *testing.T)
func TestCanDeployNewPair_MaxPairsLimit(t *testing.T)
func TestCanDeployNewPair_ScoreThreshold(t *testing.T)

// Test pair selection
func TestSelectPairsToTrade_Ranking(t *testing.T)
func TestSelectPairsToTrade_MaxLimit(t *testing.T)

// Test concurrency
func TestMultiPairDetector_ConcurrentDetect(t *testing.T)
func TestPortfolioManager_ConcurrentBalanceUpdate(t *testing.T)
```

### 9.2 Integration Tests

```go
// Test với mock exchange
func TestMultiPairAgent_TwoPairs(t *testing.T)
func TestMultiPairAgent_BalanceExhaustion(t *testing.T)
func TestMultiPairAgent_PairRotation(t *testing.T)
```

---

## 10. Migration Path

### Backward Compatibility

```go
// Giữ CompleteAgent cho backward compatibility
// Thêm MultiPairAgent mới

// Factory function chọn implementation
type AgentMode string
const (
    ModeSingle  AgentMode = "single"
    ModeMulti   AgentMode = "multi"
)

func NewAgent(config *AgentConfig, mode AgentMode) (Agent, error) {
    switch mode {
    case ModeMulti:
        return NewMultiPairAgent(config)
    default:
        return NewCompleteAgent(config) // legacy
    }
}
```

---

## 11. Deliverables

| Item | Path | Status |
|------|------|--------|
| PortfolioManager | `internal/agent/portfolio.go` | 🔲 TODO |
| BalanceChecker | `internal/agent/balance.go` | 🔲 TODO |
| MultiPairDetector | `internal/agent/multipair_detector.go` | 🔲 TODO |
| MultiPairAgent | `internal/agent/multipair_agent.go` | 🔲 TODO |
| Updated Config | `config/agentic-config.yaml` | 🔲 TODO |
| Updated Main | `cmd/agentic/main.go` | 🔲 TODO |
| Termux Script | `run-agentic-termux.sh` | 🔲 TODO |

---

## 12. Time Estimate

| Phase | Duration | Complexity |
|-------|----------|------------|
| Phase 1: Foundation | 2-3 hours | Medium |
| Phase 2: Core Refactor | 4-5 hours | High |
| Phase 3: Entry Point | 1-2 hours | Low |
| Phase 4: Integration | 3-4 hours | High |
| Phase 5: Config & Scripts | 1 hour | Low |
| **Total** | **11-15 hours** | **High** |
