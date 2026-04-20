# WebSocket-First Architecture - Single Source of Truth

## 🎯 Objective

**Eliminate all local cache layers. Single source of truth = exchange realtime data via WebSocket.**

## 📊 Current Architecture (Multi-Layer Cache)

```
WebSocket
  ├── wsClient.positionCache (single source of truth for positions)
  ├── wsClient.balanceCache (single source of truth for balance)
  └── wsClient.orderCache (single source of truth for orders)
       ↓
  ├── GridManager.cachedPositions (DUPLICATE cache)
  ├── GridManager.exchangeOrderCache (DUPLICATE cache)
  ├── SyncManager.PositionSyncWorker.internalPositions (DUPLICATE cache)
  ├── SyncManager.OrderSyncWorker.internalOrders (DUPLICATE cache)
  └── AdaptiveGridManager position tracking (DUPLICATE cache)
```

**Problem:** Too many cache layers → data mismatch, stale data, sync issues.

---

## 🎯 Target Architecture (Single Source of Truth)

```
WebSocket
  ├── wsClient.positionCache ← SINGLE SOURCE OF TRUTH
  ├── wsClient.balanceCache ← SINGLE SOURCE OF TRUTH
  └── wsClient.orderCache ← SINGLE SOURCE OF TRUTH
       ↓
  ├── GridManager → reads from wsClient cache
  ├── SyncManager → reads from wsClient cache (no local state)
  ├── AdaptiveGridManager → reads from wsClient cache
  └── All components → read from wsClient cache (single source)
```

**Benefits:**
- No data mismatch
- No stale data
- Simpler architecture
- Easier debugging
- Single source of truth

---

## 🛠️ Implementation Plan

### Phase 1: Eliminate GridManager.cachedPositions

**Current:**
```go
// GridManager has own cache
cachedPositions    map[string]*client.Position
cachedPositionsMu  sync.RWMutex
lastPositionUpdate time.Time

// OnAccountUpdate updates GridManager cache
func (g *GridManager) OnAccountUpdate(accountUpdate stream.WsAccountUpdate) {
    g.cachedPositionsMu.Lock()
    // Update g.cachedPositions
    g.cachedPositionsMu.Unlock()
}

// GetCachedPositions reads from GridManager cache
func (g *GridManager) GetCachedPositions(ctx context.Context) ([]client.Position, error) {
    g.cachedPositionsMu.RLock()
    // Read from g.cachedPositions
    g.cachedPositionsMu.RUnlock()
}
```

**Target:**
```go
// GridManager removes own cache
// cachedPositions REMOVED
// cachedPositionsMu REMOVED
// lastPositionUpdate REMOVED

// OnAccountUpdate only updates wsClient cache (already done)
func (g *GridManager) OnAccountUpdate(accountUpdate stream.WsAccountUpdate) {
    // Only update AdaptiveGridManager position tracking
    if g.adaptiveMgr != nil {
        g.adaptiveMgr.UpdatePositionTracking(pos.Symbol, cachedPos)
    }
    // wsClient cache already updated in volume_farm_engine.go
}

// GetCachedPositions reads from wsClient cache
func (g *GridManager) GetCachedPositions(ctx context.Context) ([]client.Position, error) {
    // Read from wsClient cache (single source of truth)
    positions := g.wsClient.GetCachedPositions()
    
    // Fallback to API if cache stale (> 5s)
    if g.wsClient.IsCacheStale("position") {
        g.logger.Warn("wsClient cache stale, falling back to API")
        return g.futuresClient.GetPositions(ctx)
    }
    
    // Convert map to slice
    result := make([]client.Position, 0, len(positions))
    for _, pos := range positions {
        result = append(result, pos)
    }
    return result, nil
}
```

---

### Phase 2: Eliminate GridManager.exchangeOrderCache

**Current:**
```go
// GridManager has own order cache
exchangeOrderCache    map[string]*ExchangeOrderCacheEntry
exchangeOrderCacheMu  sync.RWMutex
exchangeOrderCacheTTL time.Duration

// getCachedExchangeOrders reads from GridManager cache
func (g *GridManager) getCachedExchangeOrders(symbol string) ([]client.Order, bool) {
    g.exchangeOrderCacheMu.RLock()
    // Read from g.exchangeOrderCache
    g.exchangeOrderCacheMu.RUnlock()
}
```

**Target:**
```go
// GridManager removes own order cache
// exchangeOrderCache REMOVED
// exchangeOrderCacheMu REMOVED
// exchangeOrderCacheTTL REMOVED

// Use wsClient order cache or direct API call
func (g *GridManager) getExchangeOrders(ctx context.Context, symbol string) ([]client.Order, error) {
    // Read from wsClient cache (single source of truth)
    orders := g.wsClient.GetCachedOrders(symbol)
    
    // Fallback to API if cache stale (> 5s) or empty
    if g.wsClient.IsCacheStale("order") || len(orders) == 0 {
        g.logger.Debug("wsClient order cache stale or empty, calling API")
        return g.futuresClient.GetOpenOrders(ctx, symbol)
    }
    
    return orders, nil
}
```

---

### Phase 3: Eliminate SyncManager Local State

**Current:**
```go
// SyncManager has own local state
type PositionSyncWorker struct {
    internalPositions map[string]client.Position
    mu               sync.RWMutex
}

type OrderSyncWorker struct {
    internalOrders map[string]client.Order
    mu            sync.RWMutex
}

// Periodic sync compares local state with exchange
func (w *PositionSyncWorker) sync() {
    // Get exchange positions
    exchangePositions := w.wsClient.GetCachedPositions()
    
    // Compare with internal state
    for symbol := range w.internalPositions {
        // Sync if mismatch
    }
}
```

**Target:**
```go
// SyncManager removes local state
// internalPositions REMOVED
// internalOrders REMOVED

// SyncManager becomes pure verification layer
type PositionSyncWorker struct {
    wsClient *client.WebSocketClient
    logger   *zap.Logger
    interval time.Duration
}

// Periodic verification checks for WebSocket failures
func (w *PositionSyncWorker) sync() {
    // Check if WebSocket is alive
    if w.wsClient.IsCacheStale("position") {
        w.logger.Warn("WebSocket position cache stale, possible connection issue")
        // Trigger alert or fallback
    }
    
    // Optional: Verify exchange state matches WebSocket cache
    // (call API occasionally to verify WebSocket integrity)
}
```

---

### Phase 4: Update WebSocket Handlers

**Current (volume_farm_engine.go):**
```go
OnAccountUpdate: func(u stream.WsAccountUpdate) {
    // Update GridManager cache
    e.gridManager.OnAccountUpdate(u)
    
    // Update wsClient cache
    for _, pos := range u.Update.Positions {
        e.wsClient.UpdatePositionCache(position)
    }
    
    // Update SyncManager
    e.syncManager.UpdatePosition(position)
}
```

**Target:**
```go
OnAccountUpdate: func(u stream.WsAccountUpdate) {
    // Update wsClient cache (single source of truth)
    for _, pos := range u.Update.Positions {
        e.wsClient.UpdatePositionCache(position)
    }
    
    // Update AdaptiveGridManager position tracking only
    if e.gridManager != nil && e.gridManager.adaptiveMgr != nil {
        for _, pos := range u.Update.Positions {
            e.gridManager.adaptiveMgr.UpdatePositionTracking(pos.Symbol, position)
        }
    }
    
    // SyncManager verification only (no local state update)
    // e.syncManager.VerifyWebSocketUpdate(position) - optional
}
```

---

## 📋 Implementation Steps

### Step 1: Update GridManager.GetCachedPositions
- Remove GridManager.cachedPositions usage
- Read from wsClient.GetCachedPositions()
- Fallback to API if wsClient cache stale

### Step 2: Remove GridManager.cachedPositions
- Remove cachedPositions field
- Remove cachedPositionsMu field
- Remove lastPositionUpdate field
- Simplify OnAccountUpdate (only update AdaptiveGridManager)

### Step 3: Update GridManager.getExchangeOrders
- Remove exchangeOrderCache usage
- Read from wsClient.GetCachedOrders()
- Fallback to API if wsClient cache stale

### Step 4: Remove GridManager.exchangeOrderCache
- Remove exchangeOrderCache field
- Remove exchangeOrderCacheMu field
- Remove exchangeOrderCacheTTL field
- Remove cacheExchangeOrders method

### Step 5: Simplify SyncManager
- Remove internalPositions from PositionSyncWorker
- Remove internalOrders from OrderSyncWorker
- Convert to pure verification layer
- Only check WebSocket health, not maintain local state

### Step 6: Update volume_farm_engine.go handlers
- Simplify OnAccountUpdate (only update wsClient + AdaptiveGridManager)
- Simplify OnOrderUpdate (only update wsClient)
- Remove SyncManager.UpdatePosition/UpdateOrder calls

### Step 7: Update wsClient cache methods
- Add RemovePositionCache method (for position closed)
- Add RemoveOrderCache method (for order filled/cancelled)
- Ensure thread-safe updates

---

## 🔍 Verification

After implementation, verify:
1. All components read from wsClient cache (single source of truth)
2. No local cache layers remain
3. WebSocket updates wsClient cache directly
4. Fallback to API only when wsClient cache stale
5. No data mismatch between components

---

## ⚠️ Risk Mitigation

1. **WebSocket failure**: Fallback to API when wsClient cache stale (> 5s)
2. **API rate limiting**: Cache API responses temporarily (TTL 1s)
3. **Data integrity**: Periodic verification (optional API call to verify WebSocket)
4. **Performance**: wsClient cache is in-memory, fast reads

---

## 📝 Files to Change

1. `internal/farming/grid_manager.go`:
   - Remove cachedPositions, cachedPositionsMu, lastPositionUpdate
   - Remove exchangeOrderCache, exchangeOrderCacheMu, exchangeOrderCacheTTL
   - Update GetCachedPositions to use wsClient cache
   - Update getExchangeOrders to use wsClient cache
   - Simplify OnAccountUpdate

2. `internal/farming/volume_farm_engine.go`:
   - Simplify OnAccountUpdate handler
   - Simplify OnOrderUpdate handler
   - Remove SyncManager.UpdatePosition/UpdateOrder calls

3. `internal/farming/sync/manager.go`:
   - Simplify SyncManager to verification layer
   - Remove local state management

4. `internal/farming/sync/position_sync_worker.go`:
   - Remove internalPositions
   - Convert to WebSocket health check

5. `internal/farming/sync/order_sync_worker.go`:
   - Remove internalOrders
   - Convert to WebSocket health check

6. `internal/client/websocket.go`:
   - Add RemovePositionCache method
   - Add RemoveOrderCache method
