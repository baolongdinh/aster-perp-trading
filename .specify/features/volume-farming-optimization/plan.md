# Volume Farming Optimization - Implementation Plan

## Technical Context

### System Architecture
- **Language**: Go 1.21+
- **Framework**: Custom trading bot with grid-based volume farming
- **Key Components**:
  - Grid Manager: Order placement and grid management
  - Risk Monitor: Position and risk tracking
  - Config Manager: YAML-based configuration
  - Exchange Client: API wrapper for exchange interactions
  - Logger: Structured logging with Zap

### Current State
- Grid Manager places limit orders on both sides
- No tick-size awareness
- No toxic flow detection
- No post-only order handling
- No inventory hedging

### Dependencies
- Exchange API for tick-size, order book, order placement
- Existing Grid Manager for order operations
- Existing Risk Monitor for position data
- Existing Config Manager for parameter loading

### Technology Choices
- **Tick-size Management**: In-memory cache with periodic refresh
- **VPIN Calculation**: Sliding window algorithm
- **Post-Only**: Exchange API flag support
- **Smart Cancellation**: Timer-based spread monitoring
- **Inventory Hedging**: Configurable strategy pattern

### Integration Points
- Grid Manager: Order placement/cancellation hooks
- Risk Monitor: Position data access
- Config Manager: Load optimization config
- Exchange Client: API calls for tick-size, order book, orders

---

## Constitution Check

### Code Quality
- ✅ Follow existing Go conventions
- ✅ Use structured logging (Zap)
- ✅ Implement proper error handling
- ✅ Add mutex protection for concurrent access
- ✅ Write unit tests for all new components

### Performance
- ✅ Ensure VPIN calculation <10ms
- ✅ Ensure tick-size rounding <1ms
- ✅ Minimize allocations in hot paths
- ✅ Use RWMutex for read-heavy operations

### Security
- ✅ Validate all configuration values
- ✅ Sanitize exchange API responses
- ✅ Implement rate limiting for API calls
- ✅ Log sensitive operations appropriately

### Testing
- ✅ Unit tests for each component
- ✅ Integration tests for end-to-end flows
- ✅ Mock exchange client for testing
- ✅ Test edge cases (invalid data, API failures)

### Documentation
- ✅ Document all public APIs
- ✅ Add inline comments for complex logic
- ✅ Update README with new features
- ✅ Provide configuration examples

---

## Phase 0: Research & Design

### Research Tasks
1. **Research exchange tick-size API**
   - Task: Investigate exchange API for tick-size endpoint
   - Deliverable: API documentation and caching strategy

2. **Research VPIN algorithm variations**
   - Task: Evaluate different VPIN calculation methods
   - Deliverable: Chosen algorithm with rationale

3. **Research post-only support across exchanges**
   - Task: Verify post-only flag support on target exchanges
   - Deliverable: Support matrix and fallback strategy

4. **Research hedging strategies**
   - Task: Evaluate internal vs cross-pair hedging effectiveness
   - Deliverable: Recommended strategy with pros/cons

### Design Decisions
- **Tick-size Cache**: Refresh every 24h or on error
- **VPIN Window**: 50 buckets of 1000 USDT each
- **Post-Only Fallback**: Retry 3 times then skip order
- **Smart Cancellation**: Check every 5 seconds
- **Hedging Mode**: Internal mode first, cross-pair later

---

## Phase 1: Implementation (High Priority)

### Task 1.1: Tick-size Awareness
**File**: `backend/internal/farming/volume_optimization/tick_size_manager.go`

**Implementation**:
```go
type TickSizeManager struct {
    tickSizes map[string]float64
    mu        sync.RWMutex
    client    ExchangeClient
    logger    *zap.Logger
}

func NewTickSizeManager(client ExchangeClient, logger *zap.Logger) *TickSizeManager
func (t *TickSizeManager) GetTickSize(symbol string) float64
func (t *TickSizeManager) RoundToTick(price, tickSize float64) float64
func (t *TickSizeManager) RefreshTickSizes() error
```

**Integration**:
- Initialize in VolumeFarmEngine
- Add to Grid Manager dependency
- Use in grid level calculation

**Tests**:
- Test tick-size fetching
- Test rounding to tick
- Test cache refresh

### Task 1.2: VPIN Monitor
**File**: `backend/internal/farming/volume_optimization/vpin_monitor.go`

**Implementation**:
```go
type VPINMonitor struct {
    windowSize      int
    bucketSize      float64
    buyVolume       []float64
    sellVolume      []float64
    currentBucket   int
    currentVol      float64
    threshold       float64
    mu              sync.RWMutex
    logger          *zap.Logger
}

func NewVPINMonitor(config VPINConfig, logger *zap.Logger) *VPINMonitor
func (v *VPINMonitor) UpdateVolume(buyVol, sellVol float64)
func (v *VPINMonitor) CalculateVPIN() float64
func (v *VPINMonitor) IsToxic() bool
func (v *VPINMonitor) GetVPIN() float64
```

**Integration**:
- Initialize in VolumeFarmEngine
- Update on order fills
- Check in CanPlaceOrder

**Tests**:
- Test VPIN calculation accuracy
- Test toxic flow detection
- Test sliding window behavior

### Task 1.3: Post-Only Orders
**File**: `backend/internal/farming/grid_manager.go` (modify)

**Implementation**:
```go
// Add post-only flag to order placement
func (g *GridManager) PlaceGridOrder(
    symbol, side string,
    price, quantity float64,
    postOnly bool,
) error

// Handle post-only rejections
func (g *GridManager) handlePostOnlyRejection(
    symbol, side string,
    price, quantity float64,
    rejectionReason string,
) error
```

**Integration**:
- Add post-only config to Grid Manager
- Use post-only flag in all grid orders
- Implement retry logic for rejections

**Tests**:
- Test post-only order placement
- Test rejection handling
- Test fallback to regular limit

---

## Phase 2: Implementation (Medium Priority)

### Task 2.1: Smart Cancellation
**File**: `backend/internal/farming/volume_optimization/smart_cancellation.go`

**Implementation**:
```go
type SmartCancellation struct {
    enabled           bool
    spreadChangeThreshold float64
    checkInterval     time.Duration
    lastCheck         time.Time
    lastSpread        float64
    gridManager       GridManagerInterface
    mu                sync.RWMutex
    logger            *zap.Logger
}

func NewSmartCancellation(config SmartCancelConfig, gm GridManagerInterface, logger *zap.Logger) *SmartCancellation
func (s *SmartCancellation) Start(ctx context.Context)
func (s *SmartCancellation) ShouldCancel(symbol string) bool
func (s *SmartCancellation) CancelAndReplace(symbol string) error
```

**Integration**:
- Initialize in VolumeFarmEngine
- Start as goroutine
- Call CancelAndReplace on trigger

**Tests**:
- Test spread change detection
- Test cancellation trigger
- Test grid rebuild

### Task 2.2: Tick-size Integration
**File**: `backend/internal/farming/grid_manager.go` (modify)

**Implementation**:
```go
// Add tick-size manager to Grid Manager
type GridManager struct {
    // ... existing fields ...
    tickSizeManager *TickSizeManager
}

// Use tick-size in grid calculation
func (g *GridManager) calculateGridLevels(
    symbol string,
    currentPrice float64,
    gridSpread float64,
    numLevels int,
) []GridLevel {
    tickSize := g.tickSizeManager.GetTickSize(symbol)
    // Round all levels to tick
}
```

**Integration**:
- Pass tick-size manager to Grid Manager
- Use in grid level calculation
- Log tick-size warnings

**Tests**:
- Test grid level rounding
- Test tick-size fallback
- Test invalid tick handling

---

## Phase 3: Implementation (Advanced)

### Task 3.1: Penny Jumping (Optional)
**File**: `backend/internal/farming/volume_optimization/penny_jumping.go`

**Implementation**:
```go
type PennyJumpingStrategy struct {
    enabled         bool
    jumpThreshold   float64
    maxJump         int
    tickSizeManager *TickSizeManager
    logger          *zap.Logger
}

func NewPennyJumpingStrategy(config PennyConfig, tsm *TickSizeManager, logger *zap.Logger) *PennyJumpingStrategy
func (p *PennyJumpingStrategy) CalculateOptimalPrice(
    currentPrice, bestBid, bestAsk, spread float64,
    isBuy bool,
) float64
```

**Integration**:
- Initialize in VolumeFarmEngine
- Use in grid order placement
- Monitor spread impact

**Tests**:
- Test price calculation
- Test spread limit enforcement
- Test max jump constraint

### Task 3.2: Inventory Hedging
**File**: `backend/internal/farming/volume_optimization/inventory_hedging.go`

**Implementation**:
```go
type InventoryHedging struct {
    enabled        bool
    hedgeThreshold float64
    hedgeRatio     float64
    maxHedgeSize   float64
    hedgingMode    HedgingMode
    hedgePair      string
    riskMonitor    RiskMonitor
    exchangeClient ExchangeClient
    logger         *zap.Logger
}

type HedgingMode string
const (
    HedgingModeInternal   HedgingMode = "internal"
    HedgingModeCrossPair  HedgingMode = "cross_pair"
    HedgingModeScalping   HedgingMode = "scalping"
)

func NewInventoryHedging(config HedgeConfig, rm RiskMonitor, client ExchangeClient, logger *zap.Logger) *InventoryHedging
func (h *InventoryHedging) ShouldHedge(symbol string) bool
func (h *InventoryHedging) ExecuteHedge(symbol string) error
func (h *InventoryHedging) CalculateHedgeSize(symbol string) float64
```

**Integration**:
- Initialize in VolumeFarmEngine
- Monitor inventory periodically
- Execute hedge on threshold breach

**Tests**:
- Test inventory monitoring
- Test hedge calculation
- Test hedge execution
- Test cross-pair hedging
- Test scalping hedging

---

## Configuration Integration

### File: `backend/config/volume_optimization_config.go`

**Implementation**:
```go
type VolumeOptimizationConfig struct {
    Enabled bool `yaml:"enabled"`

    OrderPriority OrderPriorityConfig `yaml:"order_priority"`
    ToxicFlow     ToxicFlowConfig     `yaml:"toxic_flow_detection"`
    MakerTaker     MakerTakerConfig     `yaml:"maker_taker_optimization"`
    InventoryHedge InventoryHedgeConfig `yaml:"inventory_hedging"`
}

type OrderPriorityConfig struct {
    TickSizeAwareness TickSizeConfig `yaml:"tick_size_awareness"`
    PennyJumping      PennyConfig     `yaml:"penny_jumping"`
}

type ToxicFlowConfig struct {
    Enabled            bool          `yaml:"enabled"`
    WindowSize         int           `yaml:"window_size"`
    BucketSize         float64       `yaml:"bucket_size"`
    VPINThreshold      float64       `yaml:"vpin_threshold"`
    SustainedBreaches  int           `yaml:"sustained_breaches"`
    Action             string        `yaml:"action"`
    AutoResumeDelay    time.Duration `yaml:"auto_resume_delay"`
}

type MakerTakerConfig struct {
    PostOnlyEnabled     bool                `yaml:"post_only_enabled"`
    PostOnlyFallback    bool                `yaml:"post_only_fallback"`
    SmartCancellation   SmartCancelConfig    `yaml:"smart_cancellation"`
}

type SmartCancelConfig struct {
    Enabled               bool          `yaml:"enabled"`
    SpreadChangeThreshold float64       `yaml:"spread_change_threshold"`
    CheckInterval         time.Duration `yaml:"check_interval"`
}

type InventoryHedgeConfig struct {
    Enabled        bool         `yaml:"enabled"`
    HedgeThreshold float64      `yaml:"hedge_threshold"`
    HedgeRatio     float64      `yaml:"hedge_ratio"`
    MaxHedgeSize   float64      `yaml:"max_hedge_size"`
    HedgingMode    string       `yaml:"hedging_mode"`
    HedgePair      string       `yaml:"hedge_pair"`
}
```

### File: `backend/config/agentic-vf-config.yaml` (add section)

```yaml
volume_farming_optimization:
  enabled: true

  order_priority:
    tick_size_awareness:
      enabled: true
      tick_sizes:
        BTC: 0.1
        ETH: 0.01
        SOL: 0.001
        default: 0.01
    penny_jumping:
      enabled: false
      jump_threshold: 0.1
      max_jump: 3

  toxic_flow_detection:
    enabled: true
    window_size: 50
    bucket_size: 1000.0
    vpin_threshold: 0.3
    sustained_breaches: 2
    action: "pause"
    auto_resume_delay: 5s

  maker_taker_optimization:
    post_only_enabled: true
    post_only_fallback: true
    smart_cancellation:
      enabled: true
      spread_change_threshold: 0.2
      check_interval: 5s

  inventory_hedging:
    enabled: true
    hedge_threshold: 0.3
    hedge_ratio: 0.3
    max_hedge_size: 100.0
    hedging_mode: "internal"
    hedge_pair: "ETH"
```

---

## Testing Strategy

### Unit Tests
- `tick_size_manager_test.go`: Tick-size fetching, rounding, caching
- `vpin_monitor_test.go`: VPIN calculation, toxic detection
- `smart_cancellation_test.go`: Spread change detection, cancellation
- `inventory_hedging_test.go`: Inventory monitoring, hedge calculation

### Integration Tests
- `volume_optimization_integration_test.go`: End-to-end flows
  - Test post-only order placement
  - Test VPIN trigger and pause
  - Test smart cancellation and rebuild
  - Test inventory hedge execution

### Mock Exchange Client
```go
type MockExchangeClient struct {
    tickSizes map[string]float64
    orderBook map[string]*OrderBook
    orders    map[string]*Order
}

func (m *MockExchangeClient) GetTickSize(symbol string) (float64, error)
func (m *MockExchangeClient) GetOrderBook(symbol string) (*OrderBook, error)
func (m *MockExchangeClient) PlaceOrder(order *Order) error
```

---

## Deployment Plan

### Phase 1 Deployment
1. Deploy tick-size awareness (low risk)
2. Deploy post-only orders (low risk)
3. Monitor for 24 hours
4. Deploy VPIN detection (medium risk)
5. Monitor for 48 hours

### Phase 2 Deployment
1. Deploy smart cancellation (medium risk)
2. Monitor for 24 hours
3. Deploy tick-size integration (low risk)
4. Monitor for 24 hours

### Phase 3 Deployment (Optional)
1. Backtest penny jumping strategy
2. Deploy penny jumping if backtest successful
3. Deploy inventory hedging (requires careful monitoring)
4. Monitor for 1 week

---

## Rollback Plan

### Rollback Triggers
- Fill rate decreases by >10%
- Taker fee ratio >10%
- VPIN false positives >5/hour
- Hedge losses >$100/day
- System errors >1/hour

### Rollback Procedure
1. Disable specific feature in config
2. Reload config (hot reload or restart)
3. Monitor system stability
4. Investigate root cause
5. Fix and redeploy

---

## Monitoring & Observability

### Metrics to Track
- Fill rate (orders filled / orders placed)
- Taker fee ratio (taker orders / total orders)
- VPIN value and breach count
- Post-only rejection count
- Smart cancellation trigger count
- Inventory skew percentage
- Hedge execution count and PnL

### Alerts
- VPIN breach alert
- Post-only rejection rate >5%
- Taker fee ratio >10%
- Inventory skew >35%
- Hedge loss >$50

---

## Success Criteria

### Phase 1
- ✅ Tick-size awareness: All orders on valid ticks
- ✅ Post-only: <5% taker orders
- ✅ VPIN: <5 false positives/hour

### Phase 2
- ✅ Smart cancellation: Grid rebuilds within 1s
- ✅ Fill rate: +15% improvement
- ✅ No system errors

### Phase 3 (Optional)
- ✅ Penny jumping: +20% fill rate
- ✅ Inventory hedging: Skew <30%
- ✅ No significant hedge losses

---

## Open Questions

1. **VPIN Threshold**: Should threshold be dynamic based on market conditions?
2. **Penny Jumping**: What is the optimal jump threshold for this market?
3. **Hedging Mode**: Should we support multiple hedging modes simultaneously?
4. **Smart Cancellation**: Should cancellation trigger on volume changes too?

---

## Next Steps

1. Review and approve this plan
2. Create tasks.md with detailed breakdown
3. Start Phase 1 implementation
4. Write unit tests for each component
5. Integration testing
6. Deploy Phase 1 to staging
7. Monitor and iterate
8. Proceed to Phase 2
