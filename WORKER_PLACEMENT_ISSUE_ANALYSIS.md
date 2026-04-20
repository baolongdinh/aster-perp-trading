# Worker Placement Issue Analysis - Bot không đặt lệnh

## 🔍 Phân tích Log

### Vấn đề chính tìm được:

#### 1. **EXPOSURE LIMIT SAO** ✅ ĐÃ FIX

**Log:**
```
ORDER SKIPPED: Exposure already at limit, cannot place any order
current_exposure=446.646 exposure_limit=360 max_config=300
exchange_positions="[SOLUSD1: 68.36 USDT, BTCUSD1: 446.646 USDT]"
exchange_total_notional=721.4602
```

**Vấn đề:**
- Exposure limit được tính từ `maxNotionalUSD * 1.5 = 300 * 1.5 = 360`
- Nhưng `maxNotionalUSD` là `MaxPositionUSDTPerSymbol` (300), không phải `MaxTotalPositionsUSDT` (500)
- Exposure limit nên được tính từ `MaxTotalPositionsUSDT` (500) cho tổng exposure

**Giải pháp:**
1. Thêm field `maxTotalNotionalUSD` vào GridManager
2. Set nó từ `MaxTotalPositionsUSDT` config (500)
3. Sử dụng `maxTotalNotionalUSD` cho exposure limit thay vì `maxNotionalUSD * 1.5`
4. Fallback to `maxNotionalUSD * 2.0` nếu `maxTotalNotionalUSD` không set

**Files changed:**
- `internal/farming/grid_manager.go`:
  - Added `maxTotalNotionalUSD` field (line 91)
  - Set from config (lines 788-792)
  - Updated exposure limit calculation (lines 3531-3536)
  - Updated log messages (lines 3541-3547)

**Kết quả:**
- Exposure limit mới: 500 USDT (từ MaxTotalPositionsUSDT)
- Total exposure hiện tại: 721.46 USDT → vẫn vượt limit
- Bot vẫn sẽ skip orders cho đến khi positions được close

---

#### 2. **POSITION CACHE STALE** ✅ ĐÃ FIX

**Log:**
```
Position cache empty or stale, falling back to API call
cache_size:0, stale_seconds: 14-30
last_update:2026-04-20 11:36:52.458337706 +0700
```

**Vấn đề:**
- Position cache không được update từ WebSocket account updates
- Cache size = 0 → cache rỗng
- Last update từ 11:36:52 → cũ hơn 30 giây
- Bot luôn fallback API call → chậm và không real-time

**Nguyên nhân gốc rễ:**
- GridManager và SyncManager có position cache riêng
- GridManager.OnAccountUpdate cập nhật GridManager.cachedPositions
- SyncManager.PositionSyncWorker periodic sync với wsClient.GetCachedPositions()
- Hai cache này KHÔNG sync với nhau!

**Giải pháp:**
1. Wire GridManager với SyncManager trong volume_farm_engine.go
2. Khi OnAccountUpdate được called, cũng cập nhật SyncManager
3. Thêm RemovePosition method vào SyncManager để xử lý position closed

**Files changed:**
- `internal/farming/sync/manager.go`:
  - Added RemovePosition method (lines 174-179)
- `internal/farming/volume_farm_engine.go`:
  - OnAccountUpdate handler: gọi SyncManager.UpdatePosition (line 1233)
  - OnAccountUpdate handler: gọi SyncManager.RemovePosition khi position closed (line 1241)

**Kết quả:**
- GridManager và SyncManager giờ được sync qua WebSocket
- Position cache sẽ được update real-time
- Không còn fallback API call khi WebSocket hoạt động

---

#### 2.5. **ORDER SYNC MISSING** ✅ ĐÃ FIX

**Vấn đề:**
- GridManager.OnOrderUpdate xử lý order status updates
- SyncManager.OrderSyncWorker periodic sync với wsClient
- Hai cache này KHÔNG sync với nhau!

**Giải pháp:**
1. Wire GridManager với SyncManager trong volume_farm_engine.go
2. Khi OnOrderUpdate được called, cũng cập nhật SyncManager

**Files changed:**
- `internal/farming/volume_farm_engine.go`:
  - OnOrderUpdate handler: gọi SyncManager.UpdateOrder (line 1221)

**Kết quả:**
- GridManager và SyncManager giờ được sync qua WebSocket
- Order cache sẽ được update real-time

---

#### 3. **STATE MACHINE STUCK** ⚠️ CẦN FIX

**Log:**
```
SOLUSD1: grid_state=WAIT_NEW_RANGE should_enqueue=false
>>> GATE BLOCKED: Grid state machine not in placement state
Standard regrid check FAILED: position not zero
position_amt=-0.9, notional=75.95581221
```

**Vấn đề:**
- SOLUSD1 stuck trong WAIT_NEW_RANGE state
- Position không zero (-0.9) nên không thể regrid
- Bot không thể đặt orders khi ở WAIT_NEW_RANGE state

**Nguyên nhân:**
- Position được emergency close nhưng không thành công (margin insufficient)
- Position vẫn tồn tại trên exchange
- State machine đợi position = 0 để regrid

**Giải pháp đề xuất:**
1. Force close position khi stuck quá lâu (timeout)
2. Cho phép partial regrid khi position nhỏ
3. Manual intervention khi position stuck
4. Thêm alert khi position stuck quá lâu

---

#### 4. **WORKER HEALTH CHECK FAILED** ⚠️ CẦN KIỂM TRA

**Log:**
```
Worker health check failed, status:dead, last_seen:12.8s ago
worker: agentic_detection_loop
```

**Vấn đề:**
- Agentic detection loop worker đã die
- Không auto-restart được
- Bot không thể detect market conditions

**Nguyên nhân:**
- Panic trong detection loop
- Context bị cancel
- Resource exhaustion

**Giải pháp đề xuất:**
1. Kiểm tra panic stack trace trong log
2. Thêm panic recovery wrapper
3. Thêm auto-restart logic (đã có trong health monitor)
4. Monitor worker health

---

#### 5. **EMERGENCY CLOSE FAILED** ⚠️ CẦN FIX

**Log:**
```
Emergency close with ReduceOnly failed, retrying without ReduceOnly
Failed to emergency close position even without ReduceOnly
error: place order: aster api error -2019: Margin is insufficient.
```

**Vấn đề:**
- Emergency close failed do margin insufficient
- Position vẫn tồn tại trên exchange
- State machine stuck

**Giải pháp đề xuất:**
1. Thêm margin check trước emergency close
2. Thêm fallback: reduce position thay vì close all
3. Thêm alert khi emergency close failed
4. Manual intervention khi emergency close failed

---

## 📊 Tóm tắt

### Vấn đề đã fix:
1. ✅ **Exposure limit calculation** - Sử dụng MaxTotalPositionsUSDT thay vì MaxPositionUSDTPerSymbol
2. ✅ **Position sync missing** - Wire GridManager với SyncManager để sync position qua WebSocket
3. ✅ **Order sync missing** - Wire GridManager với SyncManager để sync order qua WebSocket

### Vấn đề còn lại:
1. ⚠️ **State machine stuck** - SOLUSD1 stuck trong WAIT_NEW_RANGE
2. ⚠️ **Worker health check failed** - Agentic detection loop dead
3. ⚠️ **Emergency close failed** - Margin insufficient

---

## 🛠️ Next Steps

### 1. Fix state machine stuck
- Thêm timeout cho WAIT_NEW_RANGE state
- Cho phép force regrid khi position stuck
- Thêm manual override

### 2. Fix worker health
- Kiểm tra panic stack trace
- Thêm panic recovery wrapper
- Verify auto-restart logic

### 3. Fix emergency close
- Thêm margin check trước emergency close
- Thêm fallback: reduce position
- Thêm alert khi failed

### 4. Monitor sync behavior
- Restart bot để apply sync fixes
- Monitor log để verify WebSocket updates
- Verify position/order cache real-time sync

---

## 📝 Files Changed

1. `internal/farming/grid_manager.go`:
   - Added `maxTotalNotionalUSD` field (line 91)
   - Set from `MaxTotalPositionsUSDT` config (lines 788-792)
   - Updated exposure limit calculation (lines 3531-3536)
   - Updated log messages (lines 3541-3547)

2. `internal/farming/sync/manager.go`:
   - Added RemovePosition method (lines 174-179)

3. `internal/farming/volume_farm_engine.go`:
   - OnOrderUpdate handler: gọi SyncManager.UpdateOrder (line 1221)
   - OnAccountUpdate handler: gọi SyncManager.UpdatePosition (line 1233)
   - OnAccountUpdate handler: gọi SyncManager.RemovePosition khi position closed (line 1241)

---

## 🔗 Related Documents

- `GRID_TRADING_STRATEGY_REVIEW.md` - Chiến lược grid trading
- `GRID_TRADING_STRATEGY_IMPLEMENTATION_SUMMARY.md` - Tóm tắt thay đổi
