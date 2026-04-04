# Aster Client Test Commands - Quick Guide

## Prerequisites

Before running any authenticated tests, set up your API credentials:

```bash
# Option 1: Set environment variables
export ASTER_API_KEY="your_api_key_here"
export ASTER_API_SECRET="your_api_secret_here"

# Option 2: Create & source .env file
cd backend
cp .env.example .env
# Edit .env with your credentials
source .env
```

---

## Test PlaceOrder Commands

### Test LIMIT Orders
```bash
cd backend
make test-place-limit
```
**What it tests:**
- Gets current market price
- Places LIMIT BUY order at -1% from bid
- Verifies order creation (OrderID, Status, Price)
- Cancels order cleanup

**Expected output:**
```
✓ PlaceLimitOrder succeeded:
  OrderID: 1234567890
  Status: NEW
  Price: 2450.50, Qty: 0.01
  ExecutedQty: 0.000000
✓ cancelled order ID 1234567890
```

### Test MARKET Orders
```bash
cd backend
make test-place-market
```
**What it tests:**
- Places MARKET BUY order (immediate execution)
- Verifies execution
- Cleanup

**Expected output:**
```
✓ PlaceMarketOrder succeeded:
  OrderID: 1234567890
  Status: FILLED (or NEW)
  ExecutedQty: 0.005000
  AvgPrice: 2500.00
```

---

## Test Order Lifecycle

### Complete Order Flow
```bash
cd backend
make test-order-lifecycle
```
**Tests:**
1. Place LIMIT BUY order
2. Get open orders (verify it appears)
3. Cancel order
4. Verify cancellation

**Expected logs:**
```
✓ Order placed
✓ Found order in open orders
✓ Order cancelled successfully
```

### Cancel Order
```bash
cd backend
make test-cancel-order
```
**Tests:**
- Place order
- Cancel by OrderID
- Verify status is CANCELED

---

## Complete Order Tests

### All Order Operations at Once
```bash
cd backend
make test-order-complete
```
**Runs:**
- ✓ PlaceLimitOrder
- ✓ PlaceMarketOrder
- ✓ GetOpenOrders
- ✓ CancelOrder
- ✓ CancelByClientOrderID
- ✓ Get24hrTicker

**Duration:** ~30-45 seconds

---

## Complete Workflow Test

### End-to-End: Account → Orders → Positions
```bash
cd backend
make test-workflow-complete
```
**Steps:**
1. Fetch account balance
2. Get current market price
3. Place LIMIT BUY + SELL orders
4. Retrieve open orders
5. Fetch positions
6. Cancel orders
7. Verify cleanup

**Expected output:**
```
=== Starting Comprehensive Order Workflow Test ===

Step 1: Fetching account information...
  AvailableBalance: 1250.50 USDT

Step 2: Fetching current market price...
  ETHUSDT Bid/Ask: 2450.00 / 2500.00

Step 3: Placing LIMIT BUY order...
  OrderID: 123456, Status: NEW

Step 4: Placing LIMIT SELL order...
  OrderID: 123457, Status: NEW

Step 5: Retrieving open orders...
  Total open orders for ETHUSDT: 2

Step 6: Fetching positions...
  Total symbols: 1000, open positions: 0

Step 7: Cancelling orders...
  ✓ cancelled order ID 123456
  ✓ cancelled order ID 123457

✓ Workflow test completed successfully!
```

---

## Public Market Data Tests (No Auth Required)

### All Public APIs
```bash
cd backend
make test-public-market
```
**Tests:**
- ✓ Ping (server connectivity)
- ✓ ServerTime (time sync)
- ✓ Klines (candlestick data)
- ✓ MarkPrice (funding rate)
- ✓ AllMarkPrices (all symbols)
- ✓ BookTicker (best bid/ask)
- ✓ FundingRates (rate history)
- ✓ ExchangeInfo (exchange configuration)

**Duration:** ~10-15 seconds
**No credentials needed** ✓

---

## Run Everything at Once

### Complete Test Suite
```bash
cd backend
make test-aster-all
```
**Runs:**
- All public market data tests
- Account tests (with auth)
- All order operations
- Complete workflow test
- Saves results to `test-results.log`

**Duration:** ~60-90 seconds

**Output saved to**:
```
test-results.log
```

---

## Single Test Commands

### Run Specific Market Test
```bash
cd backend
make test-run TEST=TestMarketPing
make test-run TEST=TestMarketKlines
make test-run TEST=TestMarketBookTicker
```

### Run Specific Order Test
```bash
cd backend
make test-run TEST=TestOrderPlaceLimitOrder
make test-run TEST=TestOrderCancelOrder
make test-run TEST=TestOrderWorkflow
```

---

## Show All Available Tests
```bash
cd backend
make test-help
```

---

## Troubleshooting

### Test skipped - "ASTER_API_KEY or ASTER_API_SECRET not set"
**Solution:**
```bash
export ASTER_API_KEY="your_key"
export ASTER_API_SECRET="your_secret"
```

### "invalid signature, recvWindow too long"
**Solution:** System clock is out of sync. Tests auto-correct, but if error persists:
```bash
# Sync system time
sudo ntpdate ntp.ubuntu.com  # Linux/Mac
```

### "order would trigger immediately"
This is normal for limit orders placed close to market price. Tests use orders far from market (-1% or +3%) to prevent fills.

### Insufficient balance
Your account balance is too low to place test orders. Increase deposit or reduce test order sizes.

### Tests timeout
Increase timeout:
```bash
go test -v -timeout 30m ./internal/client/... -run '^TestOrderWorkflow'
```

---

## Test Output Legend

| Symbol | Meaning |
|--------|---------|
| ✓ | Success / Order cancelled |
| OrderID | Exchange-assigned order ID |
| Status | Order status (NEW, FILLED, CANCELED) |
| ExecutedQty | Amount that was filled |
| AvgPrice | Average execution price |

---

## Example Full Session

```bash
# Step 1: Navigate to backend
cd backend

# Step 2: Set credentials
export ASTER_API_KEY="abc123..."
export ASTER_API_SECRET="xyz789..."

# Step 3: Test public APIs first (no auth needed)
make test-public-market
# Output: ✓ All tests pass

# Step 4: Test PlaceOrder
make test-place-limit
# Output: ✓ Order placed and cancelled successfully

# Step 5: Test complete order flow
make test-order-complete
# Output: ✓ All order operations successful

# Step 6: Run complete workflow
make test-workflow-complete
# Output: ✓ Workflow test completed successfully

# Done! ✨
```

---

## Notes

✅ **All created orders are automatically cancelled** - No orphaned orders  
✅ **Clear logs show every step** - Easy to verify what happened  
✅ **Works with real API** - Tests actual exchange connectivity  
⚠️ **Creates real orders** - But cleans them up automatically  
⚠️ **Requires API credentials** - Except public market tests  

For more details, see: `INTEGRATION_TEST_README.md`
