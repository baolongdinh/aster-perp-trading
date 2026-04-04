package client

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"aster-bot/internal/auth"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestIntegration is the main test suite for all Aster API V3 client calls.
// This test uses REAL API credentials from environment variables in .env file.
// Set the following before running:
//   - ASTER_USER_WALLET: Your wallet address (0x...)
//   - ASTER_API_SIGNER: API signer address (0x...)
//   - ASTER_API_SIGNER_KEY: Private key for API signer
//
// Test covers:
//  1. Market data (public)
//  2. Account info (authenticated)
//  3. Order placement (LIMIT, MARKET)
//  4. Order retrieval and cancellation
//  5. Position queries
//
// Tests will create real orders on the API and clean them up.
type TestIntegration struct {
	log            *zap.Logger
	httpClient     *HTTPClient
	futuresClient  *FuturesClient
	marketClient   *MarketClient
	testSymbol     string
	orderIDs       []int64
	clientOrderIDs []string
}

// Set up test environment with real API credentials
func setupIntegrationTest(t *testing.T) *TestIntegration {
	// Load .env file
	_ = godotenv.Load("../../.env")

	// Create logger
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	log, err := cfg.Build()
	require.NoError(t, err, "failed to create logger")

	// Get V3 credentials from env
	userWallet := os.Getenv("ASTER_USER_WALLET")
	apiSigner := os.Getenv("ASTER_API_SIGNER")
	apiSignerKey := os.Getenv("ASTER_API_SIGNER_KEY")

	// If V3 creds not available, skip test
	if userWallet == "" || apiSigner == "" || apiSignerKey == "" {
		t.Skipf("ASTER_USER_WALLET, ASTER_API_SIGNER, or ASTER_API_SIGNER_KEY not set - skipping live API test")
	}

	// Create V3 signer
	signer, err := auth.NewV3Signer(userWallet, apiSigner, apiSignerKey, 5000)
	require.NoError(t, err, "failed to create V3 signer")

	// Create HTTP clients
	futuresHTTPClient := NewHTTPClientV3("https://fapi.asterdex.com", signer, log, 1)
	marketHTTPClient := NewHTTPClient("https://fapi.asterdex.com", nil, log, 1)

	// Create futures and market clients
	futuresClient := NewFuturesClient(futuresHTTPClient, false, log, 1) // dryRun = false
	marketClient := NewMarketClient(marketHTTPClient)

	return &TestIntegration{
		log:            log,
		httpClient:     futuresHTTPClient,
		futuresClient:  futuresClient,
		marketClient:   marketClient,
		testSymbol:     "ETHUSDT", // Use Ethereum for testing
		orderIDs:       []int64{},
		clientOrderIDs: []string{},
	}
}

// cleanup cancels all orders created during the test
func (ti *TestIntegration) cleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Cancel all orders by order ID
	for _, orderId := range ti.orderIDs {
		_, err := ti.futuresClient.CancelOrder(ctx, CancelOrderRequest{
			Symbol:  ti.testSymbol,
			OrderID: orderId,
		})
		if err != nil {
			t.Logf("warning: failed to cancel order %d: %v", orderId, err)
		} else {
			t.Logf("✓ cancelled order ID %d", orderId)
		}
	}

	// Cancel by client order ID if needed
	for _, clientOrderId := range ti.clientOrderIDs {
		_, err := ti.futuresClient.CancelOrder(ctx, CancelOrderRequest{
			Symbol:        ti.testSymbol,
			ClientOrderID: clientOrderId,
		})
		if err != nil {
			t.Logf("warning: failed to cancel client order %s: %v", clientOrderId, err)
		} else {
			t.Logf("✓ cancelled client order %s", clientOrderId)
		}
	}

	ti.orderIDs = []int64{}
	ti.clientOrderIDs = []string{}
}

// ============================================================================
// Market Data Tests (Public / Unsigned)
// ============================================================================

// TestMarketPing verifies server connectivity
func TestMarketPing(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := ti.marketClient.Ping(ctx)
	require.NoError(t, err, "Ping should succeed")
	t.Log("✓ Ping successful")
}

// TestMarketServerTime fetches and validates server time
func TestMarketServerTime(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverTime, err := ti.marketClient.ServerTime(ctx)
	require.NoError(t, err, "ServerTime should succeed")
	require.True(t, serverTime > 0, "ServerTime should return positive value")

	// Verify the time is reasonable (within 24 hours of now)
	now := time.Now().UnixMilli()
	diff := abs(now - serverTime)
	require.Less(t, diff, int64(24*60*60*1000), "ServerTime should be close to now")

	t.Logf("✓ ServerTime: %d (diff from now: %d ms)", serverTime, diff)
}

// TestMarketKlines fetches candlestick data
func TestMarketKlines(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	klines, err := ti.marketClient.Klines(ctx, ti.testSymbol, "1h", 10)
	require.NoError(t, err, "Klines should succeed")
	require.NotEmpty(t, klines, "Should return klines data")
	require.Len(t, klines, 10, "Should return requested number of klines")

	// Validate kline data
	for i, k := range klines {
		require.True(t, k.Open > 0, "kline %d: Open should be positive", i)
		require.True(t, k.Close > 0, "kline %d: Close should be positive", i)
		require.True(t, k.High >= k.Low, "kline %d: High should be >= Low", i)
		require.True(t, k.Volume >= 0, "kline %d: Volume should be non-negative", i)
	}

	t.Logf("✓ Klines: loaded %d candles for %s", len(klines), ti.testSymbol)
	t.Logf("  Latest: Close=%.2f, Volume=%.2f", klines[len(klines)-1].Close, klines[len(klines)-1].Volume)
}

// TestMarketMarkPrice fetches mark price for a symbol
func TestMarketMarkPrice(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mp, err := ti.marketClient.MarkPrice(ctx, ti.testSymbol)
	require.NoError(t, err, "MarkPrice should succeed")
	require.NotNil(t, mp, "MarkPrice should return data")
	require.Equal(t, ti.testSymbol, mp.Symbol, "Symbol should match")
	require.True(t, mp.MarkPrice > 0, "MarkPrice should be positive")
	require.True(t, mp.IndexPrice > 0, "IndexPrice should be positive")

	t.Logf("✓ MarkPrice for %s: Mark=%.2f, Index=%.2f, FundingRate=%.6f",
		mp.Symbol, mp.MarkPrice, mp.IndexPrice, mp.LastFundingRate)
}

// TestMarketAllMarkPrices fetches mark prices for all symbols
func TestMarketAllMarkPrices(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mps, err := ti.marketClient.AllMarkPrices(ctx)
	require.NoError(t, err, "AllMarkPrices should succeed")
	require.NotEmpty(t, mps, "Should return at least one mark price")

	// Validate data
	for _, mp := range mps {
		require.True(t, mp.MarkPrice > 0, "MarkPrice should be positive for %s", mp.Symbol)
		require.True(t, mp.IndexPrice > 0, "IndexPrice should be positive for %s", mp.Symbol)
	}

	t.Logf("✓ AllMarkPrices: loaded %d symbols", len(mps))
	if len(mps) > 0 {
		t.Logf("  Sample: %s mark=%.2f", mps[0].Symbol, mps[0].MarkPrice)
	}
}

// TestMarketBookTicker fetches best bid/ask for a symbol
func TestMarketBookTicker(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bt, err := ti.marketClient.BookTicker(ctx, ti.testSymbol)
	require.NoError(t, err, "BookTicker should succeed")
	require.NotNil(t, bt, "BookTicker should return data")
	require.Equal(t, ti.testSymbol, bt.Symbol, "Symbol should match")
	require.True(t, bt.BidPrice > 0, "BidPrice should be positive")
	require.True(t, bt.AskPrice > 0, "AskPrice should be positive")
	require.True(t, bt.BidPrice < bt.AskPrice, "BidPrice should be less than AskPrice")

	t.Logf("✓ BookTicker for %s: Bid=%.2f (qty=%.4f), Ask=%.2f (qty=%.4f)",
		bt.Symbol, bt.BidPrice, bt.BidQty, bt.AskPrice, bt.AskQty)
}

// TestMarketFundingRates fetches recent funding rates
func TestMarketFundingRates(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rates, err := ti.marketClient.FundingRates(ctx, ti.testSymbol, 5)
	require.NoError(t, err, "FundingRates should succeed")
	require.NotEmpty(t, rates, "Should return at least one funding rate")
	require.Len(t, rates, 5, "Should return requested number of rates")

	for i, rate := range rates {
		require.Equal(t, ti.testSymbol, rate.Symbol, "rate %d: Symbol should match", i)
		require.True(t, rate.FundingTime > 0, "rate %d: FundingTime should be positive", i)
	}

	t.Logf("✓ FundingRates for %s: %d rates loaded", ti.testSymbol, len(rates))
	if len(rates) > 0 {
		t.Logf("  Latest: FundingRate=%.6f at %d", rates[0].FundingRate, rates[0].FundingTime)
	}
}

// TestMarketExchangeInfo fetches exchange info
func TestMarketExchangeInfo(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := ti.marketClient.ExchangeInfo(ctx)
	require.NoError(t, err, "ExchangeInfo should succeed")
	require.NotNil(t, info, "ExchangeInfo should return data")
	require.NotEmpty(t, info, "ExchangeInfo response should not be empty")

	t.Logf("✓ ExchangeInfo loaded: %d bytes", len(info))
}

// ============================================================================
// Account & Position Tests (Authenticated)
// ============================================================================

// TestAccountGetAccountInfo fetches account information
func TestAccountGetAccountInfo(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	account, err := ti.futuresClient.GetAccountInfo(ctx)
	require.NoError(t, err, "GetAccountInfo should succeed")
	require.NotNil(t, account, "GetAccountInfo should return data")
	require.True(t, account.TotalWalletBalance >= 0, "TotalWalletBalance should be non-negative")
	require.True(t, account.AvailableBalance >= 0, "AvailableBalance should be non-negative")

	t.Logf("✓ AccountInfo:")
	t.Logf("  TotalWalletBalance: %.2f", account.TotalWalletBalance)
	t.Logf("  TotalMarginBalance: %.2f", account.TotalMarginBalance)
	t.Logf("  AvailableBalance: %.2f", account.AvailableBalance)
	t.Logf("  UnrealizedProfit: %.2f", account.TotalUnrealizedProfit)
	t.Logf("  Positions: %d", len(account.Positions))
}

// TestAccountGetPositions fetches all open positions
func TestAccountGetPositions(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	positions, err := ti.futuresClient.GetPositions(ctx)
	require.NoError(t, err, "GetPositions should succeed")
	require.NotNil(t, positions, "GetPositions should return data")

	// Count non-zero positions
	openPositions := 0
	for _, p := range positions {
		if p.PositionAmt != 0 {
			openPositions++
		}
	}

	t.Logf("✓ Positions: %d total symbols, %d with open positions", len(positions), openPositions)

	// Show first few positions
	for i := 0; i < len(positions) && i < 3; i++ {
		p := positions[i]
		if p.PositionAmt != 0 {
			t.Logf("  %s: amt=%.4f, entry=%.2f, mark=%.2f, unrealized=%.2f",
				p.Symbol, p.PositionAmt, p.EntryPrice, p.MarkPrice, p.UnrealizedProfit)
		}
	}
}

// ============================================================================
// Order Tests (Placement, Retrieval, Cancellation)
// ============================================================================

// TestOrderPlaceLimitOrder places a LIMIT order and verifies response
func TestOrderPlaceLimitOrder(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()
	defer ti.cleanup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get current mark price to place a limit order below market
	bt, err := ti.marketClient.BookTicker(ctx, ti.testSymbol)
	require.NoError(t, err, "should fetch book ticker")

	pm := loadPrecisionManager(t, ctx, ti)
	limitPrice := validSymbolPrice(t, pm, ti.testSymbol, bt.BidPrice*0.99)
	quantity := validSymbolQty(t, pm, ti.testSymbol, 0.01)

	order, err := ti.futuresClient.PlaceOrder(ctx, PlaceOrderRequest{
		Symbol:        ti.testSymbol,
		Side:          "BUY",
		Type:          "LIMIT",
		TimeInForce:   "GTC",
		Price:         limitPrice,
		Quantity:      quantity,
		ClientOrderID: "test_limit_" + time.Now().Format("20060102150405"),
	})

	require.NoError(t, err, "PlaceOrder should succeed")
	require.NotNil(t, order, "PlaceOrder should return order")
	require.Equal(t, "BUY", order.Side, "Side should be BUY")
	require.Equal(t, "LIMIT", order.Type, "Type should be LIMIT")
	require.True(t, order.OrderID > 0, "OrderID should be positive")

	ti.orderIDs = append(ti.orderIDs, order.OrderID)

	t.Logf("✓ PlaceLimitOrder succeeded:")
	t.Logf("  OrderID: %d", order.OrderID)
	t.Logf("  Status: %s", order.Status)
	t.Logf("  Price: %s, Qty: %s", limitPrice, quantity)
	t.Logf("  ExecutedQty: %.6f", order.ExecutedQty)
}

// TestOrderPlaceMarketOrder places a MARKET order and verifies response
func TestOrderPlaceMarketOrder(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()
	defer ti.cleanup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pm := loadPrecisionManager(t, ctx, ti)
	quantity := validSymbolQty(t, pm, ti.testSymbol, 0.01)

	order, err := ti.futuresClient.PlaceOrder(ctx, PlaceOrderRequest{
		Symbol:        ti.testSymbol,
		Side:          "BUY",
		Type:          "MARKET",
		Quantity:      quantity,
		ClientOrderID: "test_market_" + time.Now().Format("20060102150405"),
	})

	require.NoError(t, err, "PlaceMarketOrder should succeed")
	require.NotNil(t, order, "PlaceMarketOrder should return order")
	require.Equal(t, "BUY", order.Side, "Side should be BUY")
	// MARKET orders may execute immediately or partially
	require.True(t, order.OrderID > 0 || order.Status == "FILLED",
		"OrderID > 0 or status should be FILLED")

	if order.OrderID > 0 {
		ti.orderIDs = append(ti.orderIDs, order.OrderID)
	}

	t.Logf("✓ PlaceMarketOrder succeeded:")
	t.Logf("  OrderID: %d", order.OrderID)
	t.Logf("  Status: %s", order.Status)
	t.Logf("  ExecutedQty: %.6f", order.ExecutedQty)
	t.Logf("  AvgPrice: %.2f", order.AvgPrice)
}

// TestOrderGetOpenOrders retrieves all open orders
func TestOrderGetOpenOrders(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First place a test order
	bt, err := ti.marketClient.BookTicker(ctx, ti.testSymbol)
	require.NoError(t, err, "should fetch book ticker")

	pm := loadPrecisionManager(t, ctx, ti)
	limitPrice := validSymbolPrice(t, pm, ti.testSymbol, bt.BidPrice*0.95)
	quantity := validSymbolQty(t, pm, ti.testSymbol, 0.01)

	order, err := ti.futuresClient.PlaceOrder(ctx, PlaceOrderRequest{
		Symbol:        ti.testSymbol,
		Side:          "BUY",
		Type:          "LIMIT",
		TimeInForce:   "GTC",
		Price:         limitPrice,
		Quantity:      quantity,
		ClientOrderID: "test_open_" + time.Now().Format("20060102150405"),
	})
	require.NoError(t, err, "should place order")
	require.NotNil(t, order)
	ti.orderIDs = append(ti.orderIDs, order.OrderID)

	defer ti.cleanup(t)

	// Now get open orders
	openOrders, err := ti.futuresClient.GetOpenOrders(ctx, ti.testSymbol)
	require.NoError(t, err, "GetOpenOrders should succeed")
	require.NotNil(t, openOrders, "GetOpenOrders should return array")

	// Should contain our test order
	found := false
	for _, o := range openOrders {
		if o.OrderID == order.OrderID {
			found = true
			require.Equal(t, "BUY", o.Side, "Order Side should match")
			require.Equal(t, "LIMIT", o.Type, "Order Type should match")
		}
	}
	require.True(t, found, "Should find the placed order in open orders")

	t.Logf("✓ GetOpenOrders succeeded: found %d open orders for %s", len(openOrders), ti.testSymbol)
}

// TestOrderCancelOrder cancels a placed order
func TestOrderCancelOrder(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()
	defer ti.cleanup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Place a test order first
	bt, err := ti.marketClient.BookTicker(ctx, ti.testSymbol)
	require.NoError(t, err, "should fetch book ticker")

	pm := loadPrecisionManager(t, ctx, ti)
	limitPrice := validSymbolPrice(t, pm, ti.testSymbol, bt.BidPrice*0.90)
	quantity := validSymbolQty(t, pm, ti.testSymbol, 0.01)

	order, err := ti.futuresClient.PlaceOrder(ctx, PlaceOrderRequest{
		Symbol:        ti.testSymbol,
		Side:          "BUY",
		Type:          "LIMIT",
		TimeInForce:   "GTC",
		Price:         limitPrice,
		Quantity:      quantity,
		ClientOrderID: "test_cancel_" + time.Now().Format("20060102150405"),
	})
	require.NoError(t, err, "should place order")
	require.NotNil(t, order)
	require.True(t, order.OrderID > 0, "OrderID should be positive")

	// Now cancel it
	cancelledOrder, err := ti.futuresClient.CancelOrder(ctx, CancelOrderRequest{
		Symbol:  ti.testSymbol,
		OrderID: order.OrderID,
	})

	require.NoError(t, err, "CancelOrder should succeed")
	require.NotNil(t, cancelledOrder, "CancelOrder should return order")
	require.Equal(t, order.OrderID, cancelledOrder.OrderID, "OrderID should match")
	require.Equal(t, "CANCELED", cancelledOrder.Status, "Status should be CANCELED")

	t.Logf("✓ CancelOrder succeeded:")
	t.Logf("  OrderID: %d", cancelledOrder.OrderID)
	t.Logf("  Status: %s", cancelledOrder.Status)
	t.Logf("  ExecutedQty: %.6f", cancelledOrder.ExecutedQty)
}

// TestOrderCancelByClientOrderID cancels order by client order ID
func TestOrderCancelByClientOrderID(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()
	defer ti.cleanup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Place a test order
	bt, err := ti.marketClient.BookTicker(ctx, ti.testSymbol)
	require.NoError(t, err, "should fetch book ticker")

	pm := loadPrecisionManager(t, ctx, ti)
	clientOrderID := "test_client_" + time.Now().Format("20060102150405")
	limitPrice := validSymbolPrice(t, pm, ti.testSymbol, bt.BidPrice*0.88)
	quantity := validSymbolQty(t, pm, ti.testSymbol, 0.01)
	order, err := ti.futuresClient.PlaceOrder(ctx, PlaceOrderRequest{
		Symbol:        ti.testSymbol,
		Side:          "BUY",
		Type:          "LIMIT",
		TimeInForce:   "GTC",
		Price:         limitPrice,
		Quantity:      quantity,
		ClientOrderID: clientOrderID,
	})
	require.NoError(t, err, "should place order")
	require.NotNil(t, order)
	ti.orderIDs = append(ti.orderIDs, order.OrderID)

	// Cancel by client order ID
	cancelledOrder, err := ti.futuresClient.CancelOrder(ctx, CancelOrderRequest{
		Symbol:        ti.testSymbol,
		ClientOrderID: clientOrderID,
	})

	require.NoError(t, err, "CancelOrder by ClientOrderID should succeed")
	require.NotNil(t, cancelledOrder, "Should return cancelled order")
	require.Equal(t, "CANCELED", cancelledOrder.Status, "Status should be CANCELED")

	t.Logf("✓ CancelOrder by ClientOrderID succeeded:")
	t.Logf("  OrderID: %d", cancelledOrder.OrderID)
	t.Logf("  ClientOrderID: %s", cancelledOrder.ClientOrderID)
	t.Logf("  Status: %s", cancelledOrder.Status)
}

// TestOrderGet24hrTicker fetches 24hr ticker data
func TestOrderGet24hrTicker(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()

	tickers, err := ti.futuresClient.Get24hrTicker()
	require.NoError(t, err, "Get24hrTicker should succeed")
	require.NotEmpty(t, tickers, "Should return ticker data")

	// Find our test symbol
	found := false
	for _, ticker := range tickers {
		if ticker.Symbol == ti.testSymbol {
			found = true
			require.True(t, ticker.LastPrice > 0, "LastPrice should be positive")
			t.Logf("✓ Found %s in 24hr ticker: Last=%.2f, 24hChange=%.2f%%",
				ticker.Symbol, ticker.LastPrice, ticker.PriceChangePercent)
			break
		}
	}
	require.True(t, found, "Should find test symbol in ticker data")

	t.Logf("✓ Get24hrTicker succeeded: loaded %d symbols", len(tickers))
}

// ============================================================================
// Comprehensive Workflow Test
// ============================================================================

// TestOrderWorkflow is an end-to-end test that creates, retrieves, and cancels orders
func TestOrderWorkflow(t *testing.T) {
	ti := setupIntegrationTest(t)
	defer ti.log.Sync()
	defer ti.cleanup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Log("\n=== Starting Comprehensive Order Workflow Test ===\n")

	// Step 1: Get account info
	t.Log("Step 1: Fetching account information...")
	account, err := ti.futuresClient.GetAccountInfo(ctx)
	require.NoError(t, err, "should get account info")
	t.Logf("  AvailableBalance: %.2f USDT", account.AvailableBalance)

	// Step 2: Get book ticker for current price
	t.Log("\nStep 2: Fetching current market price...")
	bt, err := ti.marketClient.BookTicker(ctx, ti.testSymbol)
	require.NoError(t, err, "should get book ticker")
	t.Logf("  %s Bid/Ask: %.2f / %.2f", ti.testSymbol, bt.BidPrice, bt.AskPrice)

	// Step 3: Place LIMIT BUY order (below market)
	t.Log("\nStep 3: Placing LIMIT BUY order...")
	pm := loadPrecisionManager(t, ctx, ti)
	limitBuyPrice := validSymbolPrice(t, pm, ti.testSymbol, bt.BidPrice*0.97)
	quantity := validSymbolQty(t, pm, ti.testSymbol, 0.01)
	order1, err := ti.futuresClient.PlaceOrder(ctx, PlaceOrderRequest{
		Symbol:        ti.testSymbol,
		Side:          "BUY",
		Type:          "LIMIT",
		TimeInForce:   "GTC",
		Price:         limitBuyPrice,
		Quantity:      quantity,
		ClientOrderID: "workflow_buy_" + time.Now().Format("20060102150405"),
	})
	require.NoError(t, err, "should place limit buy order")
	ti.orderIDs = append(ti.orderIDs, order1.OrderID)
	t.Logf("  OrderID: %d, Status: %s", order1.OrderID, order1.Status)

	// Step 4: Place LIMIT SELL order (above market)
	t.Log("\nStep 4: Placing LIMIT SELL order...")
	limitSellPrice := validSymbolPrice(t, pm, ti.testSymbol, bt.AskPrice*1.03)
	quantity = validSymbolQty(t, pm, ti.testSymbol, 0.01)
	order2, err := ti.futuresClient.PlaceOrder(ctx, PlaceOrderRequest{
		Symbol:        ti.testSymbol,
		Side:          "SELL",
		Type:          "LIMIT",
		TimeInForce:   "GTC",
		Price:         limitSellPrice,
		Quantity:      quantity,
		ClientOrderID: "workflow_sell_" + time.Now().Format("20060102150405"),
	})
	require.NoError(t, err, "should place limit sell order")
	ti.orderIDs = append(ti.orderIDs, order2.OrderID)
	t.Logf("  OrderID: %d, Status: %s", order2.OrderID, order2.Status)

	// Step 5: Retrieve open orders
	t.Log("\nStep 5: Retrieving open orders...")
	openOrders, err := ti.futuresClient.GetOpenOrders(ctx, ti.testSymbol)
	require.NoError(t, err, "should get open orders")
	t.Logf("  Total open orders for %s: %d", ti.testSymbol, len(openOrders))

	orderCount := 0
	for _, o := range openOrders {
		if o.OrderID == order1.OrderID || o.OrderID == order2.OrderID {
			orderCount++
			t.Logf("  - %s %s @%s qty=%s (status: %s)",
				o.Side, o.Type, fmt.Sprintf("%.2f", o.Price),
				fmt.Sprintf("%.6f", o.OrigQty), o.Status)
		}
	}
	require.Equal(t, 2, orderCount, "should find both placed orders")

	// Step 6: Cancel one order
	t.Log("\nStep 6: Cancelling first order...")
	cancelled1, err := ti.futuresClient.CancelOrder(ctx, CancelOrderRequest{
		Symbol:  ti.testSymbol,
		OrderID: order1.OrderID,
	})
	require.NoError(t, err, "should cancel order")
	require.Equal(t, "CANCELED", cancelled1.Status, "order should be canceled")
	t.Logf("  OrderID: %d cancelled successfully", cancelled1.OrderID)

	// Step 7: Verify open orders updated
	t.Log("\nStep 7: Verifying open orders after cancellation...")
	openOrdersAfter, err := ti.futuresClient.GetOpenOrders(ctx, ti.testSymbol)
	require.NoError(t, err, "should get open orders")

	stillOpen := 0
	for _, o := range openOrdersAfter {
		if o.OrderID == order2.OrderID {
			stillOpen++
		}
	}
	require.Equal(t, 1, stillOpen, "should find remaining order")
	t.Logf("  Verified: 1 order still open")

	// Step 8: Get positions
	t.Log("\nStep 8: Fetching positions...")
	positions, err := ti.futuresClient.GetPositions(ctx)
	require.NoError(t, err, "should get positions")

	openPos := 0
	for _, p := range positions {
		if p.PositionAmt != 0 {
			openPos++
		}
	}
	t.Logf("  Total symbols: %d, open positions: %d", len(positions), openPos)

	// Step 9: Cancel remaining order
	t.Log("\nStep 9: Cancelling remaining order...")
	cancelled2, err := ti.futuresClient.CancelOrder(ctx, CancelOrderRequest{
		Symbol:  ti.testSymbol,
		OrderID: order2.OrderID,
	})
	require.NoError(t, err, "should cancel order")
	t.Logf("  OrderID: %d cancelled successfully", cancelled2.OrderID)

	t.Log("\n✓ Workflow test completed successfully!\n")
}

// ============================================================================
// Helper functions
// ============================================================================

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func loadPrecisionManager(t *testing.T, ctx context.Context, ti *TestIntegration) *PrecisionManager {
	data, err := ti.marketClient.ExchangeInfo(ctx)
	require.NoError(t, err, "ExchangeInfo should succeed for precision manager")
	pm := NewPrecisionManager()
	require.NoError(t, pm.UpdateFromExchangeInfo(data), "UpdateFromExchangeInfo should succeed")
	return pm
}

func validSymbolPrice(t *testing.T, pm *PrecisionManager, symbol string, price float64) string {
	p := pm.RoundPrice(symbol, price)
	require.NotEqual(t, "0", p, "unable to determine valid price for symbol %s", symbol)
	return p
}

func validSymbolQty(t *testing.T, pm *PrecisionManager, symbol string, qty float64) string {
	q := pm.RoundQty(symbol, qty)
	if q == "0" {
		q = pm.RoundQty(symbol, 1.0)
	}
	require.NotEqual(t, "0", q, "unable to determine valid quantity for symbol %s", symbol)
	return q
}
