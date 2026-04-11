# Agentic Trading Flow Analysis

## Core Architecture

### 1. Entry Point (cmd/agentic/main.go)
- Loads config
- Initializes VolumeFarmEngine 
- Initializes AgenticEngine with VF controller
- Starts both engines
- Waits for shutdown signal

### 2. Agentic Engine Flow (internal/agentic/engine.go)
```
Start() 
  → runDetectionCycle() [initial]
  → detectionLoop() [goroutine]
    
detectionLoop()
  → runDetectionCycle() every 30s
    → detectAllSymbols() parallel regime detection
    → calculateScores() opportunity scoring
    → circuitBreaker.Check() risk check
    → whitelistManager.UpdateWhitelist()
```

### 3. Whitelist Management (internal/agentic/whitelist_manager.go)
```
UpdateWhitelist(scores)
  → vfController.GetActivePositions()
  → buildWhitelist() rank symbols by score
  → vfController.UpdateWhitelist(newSymbols)
```

### 4. Volume Farm Engine (internal/farming/volume_farm_engine.go)
```
UpdateWhitelist(symbols) [implements VFWhitelistController]
  → symbolSelector.SetWhitelist(symbols)
  → syncGridSymbols()
    → gridManager.UpdateSymbols(symbols)
      → Create SymbolGrid for each symbol
      → InitializeRangeDetector() [NEW]
      → enqueuePlacement() if has price

### 5. Grid Manager Flow (internal/farming/grid_manager.go)
```
Start()
  → Connect WebSocket
  → websocketProcessor() [goroutine]
    → processWebSocketTicker()
      → UpdatePriceData() for each ticker
      → If oldPrice == 0: enqueuePlacement()
      → If shouldSchedulePlacement(): enqueuePlacement()

placementWorker()
  → processPlacement(symbol)
    → placeGridOrders()
      → placeBuyOrder() / placeSellOrder()
```

## Current Issues Identified

### 1. **DEADLOCK in UpdatePriceForRange** [FIXED]
- `UpdatePriceData` held `a.mu.Lock()` 
- Called `UpdatePriceForRange` → `InitializeRangeDetector` → `a.mu.Lock()` again
- **Fix**: Removed auto-initialize, pre-initialize in `UpdateSymbols`

### 2. **Missing Range Detector Initialization** [FIXED]
- RangeDetector wasn't initialized before price data arrived
- **Fix**: Added `InitializeRangeDetector` call in `UpdateSymbols` when creating grid

### 3. **WebSocket Subscription Issues** [FIXED]
- `SubscribeToTicker` wasn't called after WebSocket connect
- **Fix**: Added subscription call in both GridManager and SymbolSelector

### 4. **Symbol Casing Mismatch** [FIXED]
- WebSocket sends lowercase ("solusd1"), config uses uppercase ("SOLUSD1")
- **Fix**: Normalize to uppercase in `processWebSocketTicker`

### 5. **Config Parsing Errors** [FIXED]
- YAML parsing failed due to missing mapstructure tags
- **Fix**: Added mapstructure tags to DetectionConfig struct

## Unit Test Plan

### Phase 1: Core Component Tests

#### Test 1: AgenticEngine Detection Flow
```go
func TestAgenticEngine_DetectionCycle(t *testing.T)
- Mock market client responses
- Test regime detection for multiple symbols
- Verify score calculation
- Test whitelist update trigger
```

#### Test 2: WhitelistManager Logic
```go
func TestWhitelistManager_BuildWhitelist(t *testing.T)
- Test score-based ranking
- Test max symbols limit
- Test position-based retention
- Test whitelist diff calculation
```

#### Test 3: VolumeFarmEngine Integration
```go
func TestVolumeFarmEngine_UpdateWhitelist(t *testing.T)
- Test symbol selector whitelist update
- Test grid manager symbol sync
- Test range detector initialization
```

### Phase 2: Grid Manager Tests

#### Test 4: Grid Creation & Price Update
```go
func TestGridManager_UpdateSymbols(t *testing.T)
- Test grid creation for new symbols
- Test range detector initialization
- Test price-based enqueue
```

#### Test 5: WebSocket Ticker Processing
```go
func TestGridManager_processWebSocketTicker(t *testing.T)
- Test symbol casing normalization
- Test first price enqueue
- Test price change detection
- Test UpdatePriceData call
```

#### Test 6: Placement Flow
```go
func TestGridManager_processPlacement(t *testing.T)
- Test exchange order count check
- Test grid state management
- Test placeGridOrders call
```

### Phase 3: Adaptive Grid Tests

#### Test 7: UpdatePriceData Flow
```go
func TestAdaptiveGridManager_UpdatePriceData(t *testing.T)
- Test no deadlock with concurrent calls
- Test ATR/RSI calculator updates
- Test TrendDetector updates
- Test range detector updates
```

#### Test 8: Range Detector
```go
func TestRangeDetector_AddPrice(t *testing.T)
- Test price history management
- Test range calculation after 20 periods
- Test breakout detection
```

### Phase 4: Integration Tests

#### Test 9: End-to-End Flow
```go
func TestEndToEnd_AgenticToGrid(t *testing.T)
- Start AgenticEngine with mock data
- Verify whitelist update
- Verify grid creation
- Verify order placement trigger
```

#### Test 10: WebSocket Integration
```go
func TestWebSocket_TickerFlow(t *testing.T)
- Mock WebSocket server
- Send ticker data
- Verify price updates
- Verify placement enqueue
```

## Implementation Checklist

- [ ] Create `internal/agentic/engine_test.go`
- [ ] Create `internal/agentic/whitelist_manager_test.go`
- [ ] Create `internal/farming/volume_farm_engine_test.go`
- [ ] Create `internal/farming/grid_manager_test.go`
- [ ] Create `internal/farming/adaptive_grid/manager_test.go`
- [ ] Create `internal/farming/adaptive_grid/range_detector_test.go`
- [ ] Add mocks for MarketClient, VFController
- [ ] Add integration test with testcontainers (optional)

## Next Steps

1. Generate unit tests for each component
2. Run tests to verify fixes
3. Add CI/CD pipeline for automated testing
4. Document expected behaviors
