# Aster API Integration Tests - Quick Reference

## ⚡ Quick Start (30 seconds)

```bash
# 1. Navigate to backend
cd backend

# 2. Set credentials (choose one)
# Option A: Edit .env file
export ASTER_API_KEY="your_key"
export ASTER_API_SECRET="your_secret"

# Option B: Use setup script
bash setup-integration-tests.sh    # Linux/Mac
setup-integration-tests.bat        # Windows

# 3. Run tests
# Market data (no auth required)
go test -v ./internal/client/... -run '^TestMarket'

# All tests (requires auth)
go test -v -timeout 15m ./internal/client/... -run '^Test'

# Specific test
go test -v ./internal/client/... -run TestOrderPlaceLimitOrder
```

## 📋 Test Categories

| Category | Tests | Auth Required | Creates Orders |
|----------|-------|---------------|-----------------|
| **Market Data** | Ping, ServerTime, Klines, MarkPrice, BookTicker, FundingRates, ExchangeInfo | ❌ | ❌ |
| **Account** | GetAccountInfo, GetPositions, Get24hrTicker | ✅ | ❌ |
| **Orders** | PlaceOrder, GetOpenOrders, CancelOrder | ✅ | ✅ |
| **Workflow** | Complete end-to-end test | ✅ | ✅ |

## 🔑 Environment Setup

### V1 Authentication (HMAC)
```bash
export ASTER_API_KEY=your_api_key
export ASTER_API_SECRET=your_api_secret
```

### V3 Authentication (Wallet)
```bash
export ASTER_USER_WALLET=0x...
export ASTER_API_SIGNER=0x...
export ASTER_API_SIGNER_KEY=0x...
```

### Create .env file
```bash
cp .env.example .env
# Edit .env with your credentials
source .env  # Or on Windows: type .env
```

## 📝 Common Commands

```bash
# Run all tests with 15-minute timeout
make test-integration

# Run market tests only
make test-market

# Run account tests
make test-account

# Run order tests (⚠️ creates real orders)
make test-order

# Run workflow test (end-to-end)
make test-workflow

# Run specific test
make test-run TEST=TestMarketPing

# Show all test targets
make test-help
```

## 🔍 Example Test Output

```
=== RUN   TestMarketPing
    client_integration_test.go:123: ✓ Ping successful
--- PASS: TestMarketPing (0.45s)

=== RUN   TestOrderPlaceLimitOrder
    client_integration_test.go:234: ✓ PlaceLimitOrder succeeded:
    client_integration_test.go:235:   OrderID: 1234567890
    client_integration_test.go:236:   Status: NEW
    client_integration_test.go:237:   ✓ cancelled order ID 1234567890
--- PASS: TestOrderPlaceLimitOrder (2.50s)
```

## ✅ What Each Test Does

### Market Data Tests (Public)
- **TestMarketPing** - Check server is reachable
- **TestMarketServerTime** - Get server time and sync offset
- **TestMarketKlines** - Fetch 10 hourly candles
- **TestMarketMarkPrice** - Get mark price + funding rate
- **TestMarketAllMarkPrices** - Get prices for all symbols
- **TestMarketBookTicker** - Get best bid/ask
- **TestMarketFundingRates** - Get last 5 funding rates
- **TestMarketExchangeInfo** - Download exchange configuration

### Account Tests (Authenticated)
- **TestAccountGetAccountInfo** - Wallet balance, margin, positions
- **TestAccountGetPositions** - All open positions

### Order Tests (Authenticated + Real Orders)
- **TestOrderPlaceLimitOrder** - Place LIMIT BUY at -1% from bid → **AUTO-CANCEL**
- **TestOrderPlaceMarketOrder** - Place MARKET BUY 0.005 ETH → **AUTO-CANCEL**
- **TestOrderGetOpenOrders** - Fetch open orders, verify consistency
- **TestOrderCancelOrder** - Place order, cancel by OrderID
- **TestOrderCancelByClientOrderID** - Place order, cancel by ClientOrderID
- **TestOrderGet24hrTicker** - High-level market overview

### Workflow Test (Complete Flow)
- Places LIMIT BUY + LIMIT SELL orders
- Retrieves open orders
- Cancels orders by ID
- Queries account balance and positions
- **All orders automatically cleaned up** ✨

## ⚠️ Important Notes

### Security
- ❌ **NEVER** commit `.env` file to git
- ❌ **NEVER** share API keys or private keys
- ✅ Add `.env` to `.gitignore`
- ✅ Use minimal permissions when creating API keys

### Rate Limiting
- Tests use 1 request/second between API calls
- Built-in retry logic with exponential backoff
- Avoid running multiple test suites in parallel

### Real API Usage
- Tests use **REAL** API with your credentials
- Tests **CREATE REAL ORDERS** on the exchange
- Use **testnet** credentials if available
- Monitor [Aster API Status](https://status.asterdex.com) before testing

### Order Cleanup
- ✅ All orders are **automatically cancelled** after tests complete
- ✅ Uses `defer` pattern to ensure cleanup even if test fails
- ✅ Worst-case: find orphan orders via `TestOrderGetOpenOrders`

## 🚀 Run Full Test Suite

```bash
# Backend directory
cd backend

# Set credentials
export ASTER_API_KEY=your_key
export ASTER_API_SECRET=your_secret

# Run all tests with verbose output + 15-minute timeout
go test -v -timeout 15m ./internal/client/... -run '^Test'

# Expected output:
# ✓ Market data tests (10s)
# ✓ Account tests (5s)
# ✓ Order tests (30s)
# ✓ Workflow test (20s)
# Total: ~65 seconds
```

## 🐛 Troubleshooting

| Issue | Solution |
|-------|----------|
| `ASTER_API_KEY or ASTER_API_SECRET not set` | Set env vars or create `.env` file |
| `invalid signature, recvWindow too long` | System clock out of sync, tests auto-correct |
| `order would trigger immediately` | Limit price too close to market, test uses far prices |
| `insufficient balance` | Account balance too low, increase deposit |
| `unknown symbol` | Change `testSymbol` in test code (ETHUSDT → BTCUSDT) |
| `not enough margin` | Reduce test order sizes in code |

## 📚 See Also

- [INTEGRATION_TEST_README.md](INTEGRATION_TEST_README.md) - Full documentation
- [.env.example](.env.example) - Environment variable reference  
- [Aster API Docs](https://fapi.asterdex.com/docs) - Official API documentation
- [Makefile](Makefile) - All available test targets

## 📞 Getting Help

1. Check logs: `tail -f logs/bot.log`
2. Read API docs: https://fapi.asterdex.com/docs
3. Verify timestamp sync
4. Check Aster status page
5. Verify API key permissions
6. Reduce test order size and try again
