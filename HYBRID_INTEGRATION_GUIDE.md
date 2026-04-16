# Hướng Dẫn Tích Hợp Hybrid (Option 3)

## 🎯 Kiến Trúc Hybrid

```
┌─────────────────────────────────────────────────────────────────────┐
│                    AGENTIC ENGINE (Decision)                        │
│                     ─────────────────────                           │
├─────────────────────────────────────────────────────────────────────┤
│  ScoreCalculationEngine    →  Tính Grid/Trend/Accumulation scores   │
│  DecisionEngine           →  Quyết định state transitions           │
│  State Handlers (10)      →  Logic cho mỗi state                   │
│  StateEventBus            →  Publish state transition events       │
│  AgenticVFBridge          →  Bridge đến VF                          │
└────────────────────────────────────────────────────────────────────┘
                              │
                              │ StateTransitionEvent (async)
                              │ {symbol, from, to, trigger, params}
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│              VOLUME FARM ENGINE (Execution)                         │
│               ───────────────────────                               │
├─────────────────────────────────────────────────────────────────────┤
│  AgenticEventHandler        →  Subscribe & handle events            │
│  GridManager               →  Quyết định: grid levels, spacing      │
│  OrderManager             →  Quyết định: order size, timing        │
│  PositionManager          →  Quyết định: position limits            │
└────────────────────────────────────────────────────────────────────┘
```

**Nguyên tắc:** Agentic quyết định **WHAT** (state nào), VF quyết định **HOW** (cách thực hiện)

---

## 📦 Files Đã Tạo

### 1. `state_events.go` - Event System
- `StateTransitionEvent`: Struct chứa thông tin transition
- `ExecutionParams`: Parameters cho execution (grid levels, stop loss, etc.)
- `StateEventPublisher`: Publish events đến subscribers
- `StateEventBus`: Bidirectional communication

### 2. `vf_bridge.go` - Bridge
- `AgenticVFBridge`: Kết nối Agentic ↔ VF
- `RequestStateTransition()`: Gửi request đến VF
- `buildExecutionParams()`: Tạo params dựa trên state
- `HandleExecutionResult()`: Nhận kết quả từ VF

### 3. Cập nhật `engine.go`
- Thêm `stateEventBus` và `vfBridge` fields
- Khởi tạo trong `NewAgenticEngine()`
- Logs: "Hybrid state-execution integration initialized"

---

## 🔌 Cách Tích Hợp Với VolumeFarmEngine

### Bước 1: VolumeFarmEngine subscribe đến events

```go
// Trong cmd/agentic/main.go sau khi tạo cả 2 engine

// 1. Create VF Event Handler
vfEventHandler := farming.NewAgenticEventHandler(vfEngine, logger)

// 2. Subscribe đến Agentic event bus
agenticEngine.GetStateEventBus().GetPublisher().Subscribe(vfEventHandler)

// 3. VF Engine publish results back
vfEngine.SetEventBus(agenticEngine.GetStateEventBus())
```

### Bước 2: Tạo AgenticEventHandler trong farming package

```go
// farming/agentic_event_handler.go

type AgenticEventHandler struct {
    engine *VolumeFarmEngine
    logger *zap.Logger
}

func (h *AgenticEventHandler) HandleStateTransition(ctx, event) error {
    switch event.ToState {
    case agentic.TradingModeGrid:
        return h.enterGridTrading(event)
    case agentic.TradingModeTrending:
        return h.switchToTrending(event)
    case agentic.TradingModeDefensive:
        return h.defensiveExit(event)
    // ...
    }
}
```

### Bước 3: Cập nhật runStateManagement để dùng bridge

```go
// Trong engine.go: runStateManagement()

if transition != nil {
    // OLD: Chỉ log
    ae.logger.Info("State transition", ...)
    
    // NEW: Gửi request đến VF qua bridge
    if ae.vfBridge != nil {
        ae.vfBridge.RequestStateTransition(ctx, symbol, 
            transition.FromState, transition.ToState,
            transition.Trigger, transition.Score, regime)
    }
}
```

---

## 📊 Luồng Dữ Liệu

```
1. Detection Cycle (30s)
   ↓
2. State Handler phát hiện transition cần thiết
   ↓ 
3. vfBridge.RequestStateTransition() 
   → Tạo StateTransitionEvent
   → Build ExecutionParams
   → Publish đến VF
   ↓
4. AgenticEventHandler nhận event
   → Quyết định HOW (grid levels, order size, etc.)
   → Thực thi (đặt/hủy lệnh)
   ↓
5. Publish ExecutionResult về Agentic
   ↓
6. Agentic cập nhật state tracking
```

---

## 🧪 Test Integration

### Test Case 1: Grid Entry
```
Agentic: IDLE → GRID (score 0.72 > 0.60)
  ↓
Event: {symbol: BTCUSD1, to: GRID, params: {grid_levels: 10}}
  ↓
VF: Tạo grid với 10 levels, spacing = ATR-based
  ↓
Result: {success: true, orders_placed: 10}
```

### Test Case 2: Trend Switch
```
Agentic: GRID → TRENDING (trend score 0.82 > grid 0.45)
  ↓
Event: {symbol: ETHUSD1, to: TRENDING, params: {direction: up, trailing: true}}
  ↓
VF: Hủy grid orders, vào long position với trailing stop
  ↓
Result: {success: true, orders_cancelled: 8, position: long}
```

### Test Case 3: Defensive Exit
```
Agentic: GRID → DEFENSIVE (max loss -3% hit)
  ↓
Event: {symbol: SOLUSD1, to: DEFENSIVE, priority: CRITICAL}
  ↓
VF: Exit all positions ngay lập tức
  ↓
Result: {success: true, exit_percentage: 1.0}
```

---

## ⚠️ Cần Implement Thêm

### 1. Trong `farming/` package:
- [ ] `AgenticEventHandler` struct
- [ ] `HandleStateTransition()` method
- [ ] Helper methods: `enterGrid()`, `enterTrend()`, `defensiveExit()`

### 2. Trong `VolumeFarmEngine`:
- [ ] `SetEventBus()` method
- [ ] `GetCurrentPrice()` method
- [ ] `EnableGridTrading()` method
- [ ] `EnableTrendTrading()` method
- [ ] `ExitAllPositions()` method

### 3. Trong `cmd/agentic/main.go`:
- [ ] Wire VF handler vào Agentic event bus
- [ ] Set event bus cho VF engine

---

## ✅ Status Hiện Tại

| Component | Status |
|-----------|--------|
| StateEventBus | ✅ Created |
| AgenticVFBridge | ✅ Created |
| AgenticEngine fields | ✅ Added |
| AgenticEngine init | ✅ Updated |
| VF Event Handler | ❌ Need create |
| VF Methods | ❌ Need add |
| Main wiring | ❌ Need add |

---

## 🚀 Next Steps

1. Tạo `farming/agentic_event_handler.go` với stub methods
2. Thêm methods cần thiết vào `VolumeFarmEngine`
3. Update `cmd/agentic/main.go` để wire everything
4. Test với dry-run
5. Monitor logs để verify events flowing

---

## 📈 Expected Logs (Sau khi tích hợp)

```
# AgenticEngine logs
"Requesting state transition" symbol=BTCUSD1 from=IDLE to=GRID trigger=score_threshold score=0.72

# EventBus logs  
"Publishing state transition event" symbol=BTCUSD1 to=GRID priority=1

# VF Handler logs
"Received state transition event" symbol=BTCUSD1 to=GRID
"Executing grid entry" symbol=BTCUSD1 range_low=49500 range_high=50500

# VF Execution logs
"Grid entry executed" symbol=BTCUSD1 orders_placed=10

# Result logs
"Publishing execution result" symbol=BTCUSD1 to_state=GRID success=true
"Received execution result" symbol=BTCUSD1 success=true
```

---

**Tóm lại:** Framework đã sẵn sàng. Cần implement VF-side handler và wiring trong main.go.
