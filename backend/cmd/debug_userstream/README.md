# Asterdex UserStream Debug Tool

Tool để test Asterdex UserStream WebSocket connection và verify có nhận được account/order updates không.

## Cách Dùng

### Cách 1: Auto-generate ListenKey (Khuyên Dùng)

**Step 1: Set API Credentials**

```bash
export ASTER_API_KEY=your_api_key
export ASTER_API_SECRET=your_api_secret
```

**Step 2: Chạy Test**

```bash
cd backend
./debug_userstream
```

Tool sẽ tự động:
1. Gọi Asterdex API để lấy listenKey
2. Connect WebSocket với listenKey đó
3. Listen cho account/order updates

### Cách 2: Dùng ListenKey Có Sẵn

Nếu bạn đã có listenKey:

```bash
./debug_userstream -listenKey=YOUR_LISTEN_KEY
```

### Custom Endpoints

```bash
./debug_userstream -restBase=https://fapi.asterdex.com -wsBase=wss://fstream.asterdex.com
```

## Output Examples

### Success Case (nhận được messages)

```
=== Asterdex UserStream Debug Test ===
REST Base: https://fapi.asterdex.com
WebSocket Base: wss://fstream.asterdex.com

[Step 1] Getting listenKey from Asterdex API...
✓ ListenKey obtained: ltpGCH1j7p2a06d18P5x3m22w17444k467587f4a6d7a3f7a7a7a

Connecting to WebSocket...
WebSocket URL: wss://fstream.asterdex.com/ws/ltpGCH1j7p2a06d18P5x3m22w17444k467587f4a6d7a3f7a7a7a
✓ WebSocket connected successfully

Listening for messages (press Ctrl+C to stop)...

💓 Heartbeat: Total messages=0, Account=0, Order=0, Unknown=0

📨 Message #1 received (size: 256 bytes)
  Event type: ACCOUNT_UPDATE
  ✅ ACCOUNT_UPDATE parsed successfully
  Event reason: ORDER
  Balances: 1
    [0] Asset: USDT, Wallet: 5000.00, Cross: 4500.00, Change: -100.00
  Positions: 1
    [0] Symbol: BTCUSD1, Amt: 0.1, Entry: 60000.00, PnL: 500.00, Side: BOTH

=== Test Summary ===
Total messages: 1
Account updates: 1
Order updates: 0
Unknown messages: 0

✅ TEST PASSED - UserStream is working!
```

### No Messages Case (không có active positions/orders)

```
💓 Heartbeat: Total messages=0, Account=0, Order=0, Unknown=0
💓 Heartbeat: Total messages=0, Account=0, Order=0, Unknown=0
...
(30 giây không có messages)

=== Test Summary ===
Total messages: 0
Account updates: 0
Order updates: 0
Unknown messages: 0

❌ TEST FAILED - No messages received

Possible issues:
  1. No active positions/orders on exchange
  2. ListenKey expired or invalid
  3. WebSocket endpoint is incorrect
  4. Network/firewall blocking connection
  5. Asterdex not sending updates for this account
```

## Troubleshooting

### 1. WebSocket Connection Failed

```
Failed to connect to WebSocket: dial tcp: lookup fstream.asterdex.com: no such host
```
→ Check internet connection hoặc DNS

### 2. No Messages Received

Nếu test thất bại với "No messages received", có thể:
- **Không có active positions/orders** → Hãy thử place một order hoặc mở position trên exchange
- **ListenKey expired** → Get new listenKey
- **Network/firewall** → Check firewall settings

### 3. ListenKey Expired

```
⚠️ Listen key expired!
```
→ Get new listenKey và restart test

## Build

```bash
cd backend
go build -o debug_userstream ./cmd/debug_userstream
```

## Notes

- Tool tự động get listenKey từ Asterdex API nếu không cung cấp
- ListenKey valid trong 60 phút
- Tool sẽ heartbeat mỗi 10 giây để show stats
- Press Ctrl+C để dừng test
- Test sẽ timeout sau 60 giây nếu không có messages (server timeout)
- Nếu không có active positions/orders, có thể sẽ không nhận messages
