# Aster API Client Integration Tests

Comprehensive integration test suite for all Aster Futures API client methods. These tests execute against the **real Aster API** and verify all implemented functionality.

## What Tests Are Included

### Market Data Tests (Public / Unsigned)
- **TestMarketPing** - Server connectivity check
- **TestMarketServerTime** - Server time synchronization
- **TestMarketKlines** - OHLCV candlestick data (1h candles)
- **TestMarketMarkPrice** - Mark price and funding rate for a symbol
- **TestMarketAllMarkPrices** - Mark prices for all symbols
- **TestMarketBookTicker** - Best bid/ask snapshot
- **TestMarketFundingRates** - Recent funding rate history
- **TestMarketExchangeInfo** - Exchange configuration and limits

### Account & Position Tests (Authenticated)
- **TestAccountGetAccountInfo** - Wallet balance, margin info, and total positions
- **TestAccountGetPositions** - All open positions across symbols

### Order Operations Tests (Authenticated)
- **TestOrderPlaceLimitOrder** - Place LIMIT order with custom price
- **TestOrderPlaceMarketOrder** - Place MARKET order (immediate execution)
- **TestOrderGetOpenOrders** - Retrieve all open orders for a symbol
- **TestOrderCancelOrder** - Cancel order by OrderID
- **TestOrderCancelByClientOrderID** - Cancel order by ClientOrderID
- **TestOrderGet24hrTicker** - 24-hour price change statistics

### Workflow Test (End-to-End)
- **TestOrderWorkflow** - Complete workflow: account info → place orders → retrieve → cancel

## Setup: Environment Variables

The tests require real API credentials. Set up one of the two authentication methods:

### Option 1: V1 Authentication (HMAC-SHA256)
```bash
export ASTER_API_KEY="your_api_key_here"
export ASTER_API_SECRET="your_api_secret_here"
```

### Option 2: V3 Authentication (Wallet-based)
```bash
export ASTER_USER_WALLET="your_wallet_address"
export ASTER_API_SIGNER="your_api_signer_address"
export ASTER_API_SIGNER_KEY="your_api_signer_private_key"
```

### Create a `.env` file (recommended)
```bash
# .env file in the backend/ directory
ASTER_API_KEY=your_api_key_here
ASTER_API_SECRET=your_api_secret_here
```

The config loader automatically reads `.env` using `godotenv`.

## Running the Tests

### Run All Tests
```bash
cd backend
go test -v ./internal/client/... -run "^TestMarket|^TestAccount|^TestOrder"
```

### Run Specific Test Category

Market data (public, no credentials needed):
```bash
go test -v ./internal/client/... -run "^TestMarket"
```

Account operations (requires credentials):
```bash
go test -v ./internal/client/... -run "^TestAccount"
```

Order operations (requires credentials, creates live orders):
```bash
go test -v ./internal/client/... -run "^TestOrder"
```

Complete workflow test:
```bash
go test -v ./internal/client/... -run "^TestOrderWorkflow"
```

### Run Single Test
```bash
go test -v ./internal/client/... -run TestOrderPlaceLimitOrder
```

### Run with Timeout
```bash
go test -v -timeout 5m ./internal/client/... -run "^TestOrder"
```

## Test Flow

Each test:
1. **Setup** - Reads credentials from env, creates logger and HTTP client
2. **Execute** - Calls the API method being tested
3. **Assert** - Validates response and data types
4. **Cleanup** - Cancels any orders created (automatic via `defer`)

### Order Cleanup
Tests that create orders automatically cancel them via `defer ti.cleanup(t)`. This ensures no orphan orders remain even if a test fails.

## Important Notes

### ⚠️ Real API Usage
- Tests use **REAL API** with your actual credentials
- Tests create **REAL orders** on the exchange
- Use **testnet** credentials if available to avoid trading on mainnet
- Monitor your orders during test execution

### Rate Limiting
- Tests are conservative with rate limiting (1 request/second between tests)
- The HTTP client includes built-in rate limiters
- Avoid running multiple test suites in parallel

### Test Symbol
- Default test symbol: `ETHUSDT` (Ethereum)
- To change, edit line in `setupIntegrationTest`:
  ```go
  testSymbol: "ETHUSDT", // Change to desired symbol
  ```

### Order Sizes
- Limit orders: 0.01 ETH (~$40)
- Market orders: 0.005 ETH (~$20)
- Prices are set far from market price to avoid immediate fills

### Debugging
Enable more verbose logging:
```bash
go test -v ./internal/client/... -run TestOrderPlaceLimitOrder 2>&1 | grep -E "✓|error"
```

## Test Output Example

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

## Troubleshooting

### "ASTER_API_KEY or ASTER_API_SECRET not set"
The test skips if credentials are missing. Set environment variables as shown above.

### "invalid signature, recvWindow too long"
The tests include automatic timestamp offset calculation. If errors persist:
```bash
# Verify system clock is synced
ntpdate -u time.nist.gov  # On Linux
```

### "order would trigger immediately"
Limit orders are placed far from market price (LIMIT) to avoid fills.  If symbol price moves unexpectedly, a limit order might execute.

### "position would be liquidated by order execution"
Your account doesn't have enough margin for the test order. Reduce test order size in the code.

### "unknown symbol"
Update `testSymbol` in `setupIntegrationTest()` to an active symbol like:
- `BTCUSDT` (Bitcoin)
- `ETHUSDT` (Ethereum)
- `BNBUSDT` (Binance Coin)

Check `TestMarketExchangeInfo` or Aster API docs for available symbols.

## API Methods Tested

### FuturesClient
| Method | Status | Test |
|--------|--------|------|
| PlaceOrder | ✓ | TestOrderPlaceLimitOrder, TestOrderPlaceMarketOrder |
| GetOpenOrders | ✓ | TestOrderGetOpenOrders |
| CancelOrder | ✓ | TestOrderCancelOrder, TestOrderCancelByClientOrderID |
| GetAccountInfo | ✓ | TestAccountGetAccountInfo |
| GetPositions | ✓ | TestAccountGetPositions |
| Get24hrTicker | ✓ | TestOrderGet24hrTicker |

### MarketClient
| Method | Status | Test |
|--------|--------|------|
| Ping | ✓ | TestMarketPing |
| ServerTime | ✓ | TestMarketServerTime |
| Klines | ✓ | TestMarketKlines |
| MarkPrice | ✓ | TestMarketMarkPrice |
| AllMarkPrices | ✓ | TestMarketAllMarkPrices |
| BookTicker | ✓ | TestMarketBookTicker |
| FundingRates | ✓ | TestMarketFundingRates |
| ExchangeInfo | ✓ | TestMarketExchangeInfo |

## CI/CD Integration

For continuous integration, run tests with environment variables:
```yaml
# Example GitHub Actions
- name: Run API Integration Tests
  env:
    ASTER_API_KEY: ${{ secrets.ASTER_API_KEY }}
    ASTER_API_SECRET: ${{ secrets.ASTER_API_SECRET }}
  run: |
    cd backend
    go test -v -timeout 10m ./internal/client/... -run "^Test"
```

## Contact & Support

If tests fail or API behavior changes:
1. Check Aster API documentation: https://fapi.asterdex.com/docs
2. Verify credentials and rate limits
3. Review error messages in test output
4. Check server status and maintenance windows
