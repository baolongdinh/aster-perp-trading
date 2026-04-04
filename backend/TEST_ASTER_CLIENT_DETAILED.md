# Aster Client Testing Guide

## ⚡ Quick Test PlaceOrder in 30 Seconds

### Step 1: Set Your Credentials
```bash
cd backend
export ASTER_API_KEY="your_key_here"
export ASTER_API_SECRET="your_secret_here"
```

### Step 2: Run PlaceOrder Test
```bash
# Test LIMIT orders
make test-place-limit

# Test MARKET orders  
make test-place-market

# Test both
make test-order-complete
```

### Step 3: See the Results
You'll see output like:
```
✓ PlaceLimitOrder succeeded:
  OrderID: 1234567890
  Status: NEW
  Price: 2450.50, Qty: 0.01
  ExecutedQty: 0.000000
✓ cancelled order ID 1234567890
```

---

## 🔧 All Available Commands

### Test PlaceOrder Commands

```bash
cd backend

# Quick test (just LIMIT order)
make test-place-limit

# Test MARKET order
make test-place-market

# Test order lifecycle (place + retrieve + cancel)
make test-order-lifecycle

# Test cancellation
make test-cancel-order

# All order operations
make test-order-complete

# End-to-end workflow
make test-workflow-complete

# All tests at once
make test-aster-all
```

### Using Scripts Instead of Make

```bash
cd backend

# Linux/Mac
bash test-place-order.sh

# Windows
test-place-order.bat
```

---

## 📊 What Each Test Does

| Command | What it Tests | Time | Logs Shown |
|---------|---------------|------|-----------|
| `test-place-limit` | Place LIMIT order | ~2s | PlaceOrder (LIMIT), Cancel |
| `test-place-market` | Place MARKET order | ~1s | PlaceOrder (MARKET) |
| `test-order-lifecycle` | Place + Get + Cancel | ~5s | All operations |
| `test-cancel-order` | Cancel by OrderID | ~2s | Cancel confirmation |
| `test-order-complete` | All order ops | ~30s | Everything |
| `test-workflow-complete` | Full account flow | ~15s | Account → Orders → Positions |
| `test-aster-all` | Entire test suite | ~60s | All above |

---

## ✅ Expected Log Output

### Test: PlaceOrder (LIMIT)
```
=== RUN   TestOrderPlaceLimitOrder
    client_integration_test.go:234: ✓ PlaceLimitOrder succeeded:
    client_integration_test.go:235:   OrderID: 1234567890
    client_integration_test.go:236:   Status: NEW
    client_integration_test.go:237:   Price: 2450.50, Qty: 0.01
    client_integration_test.go:238:   ExecutedQty: 0.000000
    client_integration_test.go:239: ✓ cancelled order ID 1234567890
--- PASS: TestOrderPlaceLimitOrder (2.50s)
```

### Test: PlaceOrder (MARKET)
```
=== RUN   TestOrderPlaceMarketOrder
    client_integration_test.go:250: ✓ PlaceMarketOrder succeeded:
    client_integration_test.go:251:   OrderID: 1234567891
    client_integration_test.go:252:   Status: FILLED
    client_integration_test.go:253:   ExecutedQty: 0.005000
    client_integration_test.go:254:   AvgPrice: 2500.00
--- PASS: TestOrderPlaceMarketOrder (1.50s)
```

### Test: CancelOrder
```
=== RUN   TestOrderCancelOrder
    client_integration_test.go:315: ✓ CancelOrder succeeded:
    client_integration_test.go:316:   OrderID: 1234567890
    client_integration_test.go:317:   Status: CANCELED
    client_integration_test.go:318:   ExecutedQty: 0.000000
--- PASS: TestOrderCancelOrder (2.00s)
```

---

## 🔐 Setting Up Credentials

### Method 1: Environment Variables (Recommended)
```bash
# In terminal
export ASTER_API_KEY="your_api_key_here"
export ASTER_API_SECRET="your_api_secret_here"

# Then run tests
make test-place-limit
```

### Method 2: .env File
```bash
cd backend
cp .env.example .env

# Edit .env with your credentials
# ASTER_API_KEY=your_key
# ASTER_API_SECRET=your_secret

# Source it
source .env

# Then run tests
make test-place-limit
```

### Method 3: Interactive Setup
```bash
cd backend
bash setup-integration-tests.sh    # Linux/Mac
setup-integration-tests.bat        # Windows
```

---

## 🐛 Troubleshooting

### "ASTER_API_KEY or ASTER_API_SECRET not set"
**Problem:** Credentials not found  
**Solution:**
```bash
export ASTER_API_KEY="your_key"
export ASTER_API_SECRET="your_secret"
go test -v ./internal/client/... -run TestOrderPlaceLimitOrder
```

### "invalid signature, recvWindow too long"
**Problem:** System time is out of sync  
**Solution:** The test auto-corrects. If it persists:
```bash
# Sync system time
sudo ntpdate ntp.ubuntu.com  # Linux/Mac
# Windows: Settings → Time & Language → Date & time → Sync now
```

### Test is slow or times out
**Solution:** Increase timeout
```bash
go test -v -timeout 30m ./internal/client/... -run TestOrderPlaceLimitOrder
```

### "order would trigger immediately"
**Problem:** Limit price is too close to market  
**Solution:** This is expected. Tests place orders far from market to prevent fills. Normal behavior.

### "insufficient balance"
**Problem:** Account balance is too low  
**Solution:** Deposit more funds to your account or use smaller test amounts

### "unknown symbol"
**Problem:** ETHUSDT not available  
**Solution:** Edit test file to use different symbol (BTCUSDT, BNBUSDT, etc.)

---

## 📋 Test Breakdown

### Public Market Data Tests (No Auth Required)
These tests don't need API credentials:
```bash
make test-public-market
```
Tests:
- Ping (server reachable)
- ServerTime (time sync)
- Klines (candlestick data)
- MarkPrice (funding rate)
- BookTicker (best bid/ask)
- FundingRates (rate history)
- ExchangeInfo (exchange config)

### Authenticated Tests (Requires Credentials)
These tests need your API key/secret:
```bash
make test-place-limit
make test-place-market
make test-order-complete
```

### Real Order Tests
These tests create REAL orders on the exchange:
```bash
make test-order-complete
```
✅ All orders are automatically cleaned up after tests  
✅ No orphan orders left behind  
✅ Even if test fails, cleanup still happens

---

## 💡 Pro Tips

### 1. Test Public APIs First
```bash
# These require no credentials
make test-public-market

# Only if successful, then test authenticated APIs
make test-place-limit
```

### 2. Use testnet credentials
If available, use testnet API credentials to avoid trading on mainnet during testing.

### 3. Run with logging saved
```bash
# Save all output to file
make test-order-complete 2>&1 | tee test-output.log

# Review logs later
cat test-output.log
```

### 4. Test one thing at a time
```bash
# Just test LIMIT orders
make test-place-limit

# Just test MARKET orders
make test-place-market

# Just test cancellation
make test-cancel-order
```

### 5. Use test scripts
```bash
# Cleaner interface with pre-built tests
bash test-place-order.sh    # Linux/Mac
test-place-order.bat        # Windows
```

---

## 📝 Commands Reference

### Basic Testing
```bash
make test-help              # Show all available tests
make test-market            # Public market data (no auth)
make test-place-limit       # Test LIMIT orders
make test-place-market      # Test MARKET orders
make test-order-complete    # All order operations
make test-aster-all         # Full test suite
```

### Advanced Testing
```bash
make test-run TEST=TestOrderPlaceLimitOrder    # Run specific test
go test -v -timeout 30m ./internal/client/...  # Custom timeout
go test -v ./internal/client/... -run TestOrder  # All order tests
```

### Setup & Utilities
```bash
make setup-tests            # Interactive credential setup
bash test-place-order.sh    # Run place/cancel tests (Linux/Mac)
test-place-order.bat        # Run place/cancel tests (Windows)
```

---

## 🎯 Full Testing Workflow

```bash
# 1. Navigate to project
cd backend

# 2. Set credentials
export ASTER_API_KEY="your_key"
export ASTER_API_SECRET="your_secret"

# 3. Verify credentials work (public API test - no auth needed)
make test-public-market
# Expected: ✓ Ping, ServerTime, Klines, etc all pass

# 4. Test PlaceOrder (LIMIT)
make test-place-limit
# Expected: ✓ Order placed and cancelled

# 5. Test PlaceOrder (MARKET)
make test-place-market
# Expected: ✓ Order executed and cleaned up

# 6. Test complete order flow
make test-order-complete
# Expected: ✓ Place, retrieve, cancel all work

# 7. Test end-to-end workflow
make test-workflow-complete
# Expected: ✓ Account → Orders → Positions all work

# 8. Run everything at once
make test-aster-all
# Expected: All ~17 tests pass in ~60 seconds

# Done! ✨
```

---

## 📚 Documentation

- **This file:** `TEST_ASTER_CLIENT_DETAILED.md` - Detailed testing guide
- **Quick ref:** `TEST_ASTER_CLIENT.md` - Quick start and command reference
- **Integration tests:** `INTEGRATION_TEST_README.md` - Full integration test docs
- **Makefile:** `Makefile` - All available make targets

---

## ✨ Summary

✅ Easy to run: `make test-place-limit`  
✅ Clear logs: Shows ✓ confirmations and order details  
✅ Automatic cleanup: No orphaned orders  
✅ Works with real API: Tests actual connectivity  
✅ Multiple language support: Works on Windows, Linux, Mac  

Get started:
```bash
cd backend
export ASTER_API_KEY="your_key"
export ASTER_API_SECRET="your_secret"
make test-place-limit
```

Done! 🚀
