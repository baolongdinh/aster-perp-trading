# Worker Panic Recovery Implementation Plan

## Objective
Ensure ALL workers in the bot have panic recovery mechanisms. Workers should NEVER crash silently - they must log the panic and either recover or restart automatically.

## Root Cause Analysis
Based on code review, the following workers lack panic recovery:

### Critical Workers (NO Panic Recovery)
1. **UserStream** (`internal/stream/user_stream.go`)
   - `Run()` method - reconnect loop but no panic recovery
   - `connect()` method - no panic recovery
   - keepalive goroutine - no panic recovery
   - **Impact**: Order/position/balance updates stop permanently (causes stale cache)

2. **WebSocket Client** (`internal/client/websocket.go`)
   - `realWebSocketHandler()` - no panic recovery
   - `pingHandler()` - no panic recovery
   - **Impact**: Ticker/kline streams stop permanently

3. **Volume Farm Engine Goroutines** (`internal/farming/volume_farm_engine.go`)
   - All goroutines in `Start()` method lack panic recovery
   - **Impact**: Any component can die silently

### Workers With Panic Recovery ✅
1. **Sync Workers** (`internal/farming/sync/manager.go`) - Already protected
2. **Grid Manager Workers** (`internal/farming/grid_manager.go`) - Already protected

## Implementation Plan

### Phase 1: UserStream Panic Recovery (HIGHEST PRIORITY)

**File**: `internal/stream/user_stream.go`

#### 1.1 Add panic recovery to `Run()` method
```go
func (us *UserStream) Run(ctx context.Context) {
    defer func() {
        if r := recover(); r != nil {
            us.log.Error("UserStream goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    
    delay := reconnectDelay
    for {
        // ... existing code
    }
}
```

#### 1.2 Add panic recovery to `connect()` method
```go
func (us *UserStream) connect(ctx context.Context) error {
    defer func() {
        if r := recover(); r != nil {
            us.log.Error("UserStream connect panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    // ... existing code
}
```

#### 1.3 Add panic recovery to keepalive goroutine
```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            us.log.Error("UserStream keepalive panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    // ... existing code
}()
```

#### 1.4 Add import for debug stack
```go
import (
    "runtime/debug"
    // ... existing imports
)
```

### Phase 2: WebSocket Client Panic Recovery (HIGH PRIORITY)

**File**: `internal/client/websocket.go`

#### 2.1 Add panic recovery to `realWebSocketHandler()`
```go
func (ws *WebSocketClient) realWebSocketHandler(ctx context.Context) {
    defer func() {
        if r := recover(); r != nil {
            ws.logger.Error("WebSocket handler goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer ws.wg.Done()
    // ... existing code
}
```

#### 2.2 Add panic recovery to `pingHandler()`
```go
func (ws *WebSocketClient) pingHandler(ctx context.Context) {
    defer func() {
        if r := recover(); r != nil {
            ws.logger.Error("WebSocket ping handler panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    // ... existing code
}
```

#### 2.3 Add import for debug stack
```go
import (
    "runtime/debug"
    // ... existing imports
)
```

### Phase 3: Volume Farm Engine Goroutines Panic Recovery (HIGH PRIORITY)

**File**: `internal/farming/volume_farm_engine.go`

#### 3.1 Add panic recovery to SymbolSelector goroutine (line 900-906)
```go
e.wg.Add(1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            e.logger.Error("SymbolSelector goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer e.wg.Done()
    if err := e.symbolSelector.Start(ctx); err != nil {
        e.logger.Error("Symbol selector error", zap.Error(err))
    }
}()
```

#### 3.2 Add panic recovery to Grid sync ticker goroutine (line 914-930)
```go
e.wg.Add(1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            e.logger.Error("Grid sync ticker goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer e.wg.Done()
    ticker := time.NewTicker(2 * time.Minute)
    defer ticker.Stop()
    // ... existing code
}()
```

#### 3.3 Add panic recovery to GridManager goroutine (line 932-938)
```go
e.wg.Add(1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            e.logger.Error("GridManager goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer e.wg.Done()
    if err := e.gridManager.Start(ctx); err != nil {
        e.logger.Error("Grid manager error", zap.Error(err))
    }
}()
```

#### 3.4 Add panic recovery to AdaptiveGridManager goroutine (line 940-1005)
```go
e.wg.Add(1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            e.logger.Error("AdaptiveGridManager goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer e.wg.Done()
    // ... existing code
}()
```

#### 3.5 Add panic recovery to PointsTracker goroutine (line 1007-1013)
```go
e.wg.Add(1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            e.logger.Error("PointsTracker goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer e.wg.Done()
    if err := e.pointsTracker.Start(ctx); err != nil {
        e.logger.Error("Points tracker error", zap.Error(err))
    }
}()
```

#### 3.6 Add panic recovery to Risk monitor goroutine (line 1015-1019)
```go
e.wg.Add(1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            e.logger.Error("Risk monitor goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer e.wg.Done()
    e.monitorRisk(ctx)
}()
```

#### 3.7 Add panic recovery to UserStream goroutine (line 1119-1125) - CRITICAL
```go
e.wg.Add(1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            e.logger.Error("UserStream goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer e.wg.Done()
    e.logger.Info("Starting UserStream for real-time order updates")
    userStream.Run(ctx)
    e.logger.Info("UserStream stopped")
}()
```

#### 3.8 Add import for debug stack
```go
import (
    "runtime/debug"
    // ... existing imports
)
```

### Phase 4: Add Worker Health Monitoring (MEDIUM PRIORITY)

**File**: `internal/farming/volume_farm_engine.go`

#### 4.1 Add health check struct
```go
type WorkerHealth struct {
    mu                sync.RWMutex
    userStreamAlive    bool
    userStreamLastSeen time.Time
    wsAlive            bool
    wsLastSeen         time.Time
    syncWorkersAlive   map[string]bool
}
```

#### 4.2 Add health check ticker
```go
// Add to Start() method
e.wg.Add(1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            e.logger.Error("Health check goroutine panic recovered",
                zap.Any("panic", r),
                zap.Stack("stack", debug.Stack()))
        }
    }()
    defer e.wg.Done()
    
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-e.stopCh:
            return
        case <-ticker.C:
            e.checkWorkerHealth()
        }
    }
}()
```

#### 4.3 Implement health check method
```go
func (e *VolumeFarmEngine) checkWorkerHealth() {
    // Check UserStream health
    if e.userStream != nil {
        // Check if last account update was recent
        lastPosUpdate := e.wsClient.lastPosUpdate
        if time.Since(lastPosUpdate) > 5*time.Minute {
            e.logger.Error("UserStream health check FAILED - position cache stale",
                zap.Duration("stale_duration", time.Since(lastPosUpdate)))
            // Could trigger restart here
        }
    }
    
    // Check WebSocket health
    if !e.wsClient.IsRunning() {
        e.logger.Error("WebSocket health check FAILED - not running")
        // Could trigger restart here
    }
    
    // Check sync workers
    if e.syncManager != nil && !e.syncManager.IsRunning() {
        e.logger.Error("SyncManager health check FAILED - not running")
        // Could trigger restart here
    }
}
```

### Phase 5: Add Worker Restart Logic (LOW PRIORITY - FUTURE ENHANCEMENT)

For critical workers like UserStream, consider adding auto-restart logic:

```go
// Example for UserStream with auto-restart
go func() {
    for {
        defer func() {
            if r := recover(); r != nil {
                e.logger.Error("UserStream panic, restarting in 5s",
                    zap.Any("panic", r),
                    zap.Stack("stack", debug.Stack()))
                time.Sleep(5 * time.Second)
            }
        }()
        
        e.logger.Info("Starting UserStream")
        userStream.Run(ctx)
        e.logger.Warn("UserStream exited, restarting...")
        time.Sleep(5 * time.Second)
    }
}()
```

## Implementation Order

1. **Phase 1**: UserStream panic recovery (CRITICAL - fixes stale position cache) ✅ COMPLETED
2. **Phase 2**: WebSocket client panic recovery (HIGH - fixes data streams) ✅ COMPLETED
3. **Phase 3**: Volume Farm Engine goroutines panic recovery (HIGH - general stability) ✅ COMPLETED
4. **Phase 4**: Additional goroutines panic recovery ✅ COMPLETED
   - symbol_selector.go websocketProcessor
   - grid_manager.go WaitGroup goroutine
   - adaptive_grid/manager.go WaitGroup goroutine
   - market_stream.go ping goroutine
   - volume_farm_engine.go COOLDOWN callback
   - volume_farm_engine.go context bridge
   - volume_farm_engine.go WaitGroup goroutine
   - volume_farm_engine.go emergency exit goroutine
   - cmd/bot/main.go API server goroutine
   - cmd/volume-farm/main.go engine goroutine
   - cmd/agentic/main.go VF engine goroutine
   - cmd/agentic/main.go agentic engine goroutine
5. **Phase 5**: Agentic package complete review ✅ COMPLETED
   - internal/agentic/status.go server goroutine
   - internal/agentic/engine.go detection loop
   - internal/agentic/engine.go detection loop wrapper
   - internal/agentic/engine.go WaitGroup goroutine
   - internal/agentic/engine.go regime detector goroutines
6. **Phase 6**: Worker health monitoring ✅ COMPLETED
   - Created internal/health/monitor.go
   - Created internal/health/wrapper.go
   - Integrated with Volume Farm Engine
   - Monitoring 4 critical workers: SymbolSelector, GridManager, PointsTracker, UserStream
7. **Phase 7**: Auto-restart logic ✅ COMPLETED
   - Auto-restart enabled for all monitored workers
   - Automatic restart every 10 seconds for dead workers
   - Restart tracking and logging
8. **Phase 8**: Agentic core trading health monitoring ✅ COMPLETED
   - Integrated health monitor into Agentic Engine
   - Monitoring agentic detection loop (core decision logic)
   - Added heartbeat reporting to detection loop
   - Auto-restart enabled for detection loop

## Testing

After each phase:
1. Build the bot: `go build ./cmd/volume-farm`
2. Run the bot and verify no compilation errors
3. Monitor logs to ensure workers start successfully
4. Check that panic recovery logs appear if a panic occurs

## Verification

After all phases complete:
1. Review all goroutine starts to ensure they have `defer recover()`
2. Add a test panic to verify recovery works (optional)
3. Run bot for extended period to verify stability
4. Monitor logs for any unhandled panics

## Files to Modify

1. `internal/stream/user_stream.go`
2. `internal/client/websocket.go`
3. `internal/farming/volume_farm_engine.go`

## Estimated Time

- Phase 1: 15 minutes
- Phase 2: 10 minutes
- Phase 3: 20 minutes
- Phase 4: 30 minutes
- Phase 5: 20 minutes (future enhancement)

**Total**: ~1.5 hours for Phases 1-4
