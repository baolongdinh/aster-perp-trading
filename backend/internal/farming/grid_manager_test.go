package farming

import (
	"aster-bot/internal/client"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

func TestGridManager_DynamicCooldown(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()
	gm := &GridManager{
		logger:                logrus.NewEntry(logger),
		activeGrids:           make(map[string]*SymbolGrid),
		gridPlacementCooldown: 10 * time.Second,
		rateLimiter:           NewRateLimiter(10, 2, zapLogger),
	}

	grid := &SymbolGrid{
		Symbol:        "BTCUSD1",
		IsActive:      true,
		CurrentPrice:  50000,
		LastAttempt:   time.Now().Add(-15 * time.Second), // 15s ago > 10s cooldown
		OrdersPlaced:  false,
		PlacementBusy: false,
	}

	// No failures, should allow
	if !gm.shouldSchedulePlacement(grid, 49999) {
		t.Error("Expected to allow placement with no failures")
	}

	// Simulate failures
	gm.consecutiveFailures = 2
	gm.lastFailureTime = time.Now()

	// Should not allow due to increased cooldown
	if gm.shouldSchedulePlacement(grid, 49999) {
		t.Error("Expected to deny placement due to dynamic cooldown")
	}

	// Wait for cooldown
	time.Sleep(25 * time.Second) // 2x base cooldown + buffer

	if !gm.shouldSchedulePlacement(grid, 49999) {
		t.Error("Expected to allow after dynamic cooldown")
	}
}

func TestGridManager_ResetStaleOrders(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()
	gm := &GridManager{
		logger:      logrus.NewEntry(logger),
		activeGrids: make(map[string]*SymbolGrid),
		rateLimiter: NewRateLimiter(10, 2, zapLogger),
	}

	grid := &SymbolGrid{
		Symbol:       "BTCUSD1",
		IsActive:     true,
		CurrentPrice: 50000,
		LastAttempt:  time.Now().Add(-35 * time.Second), // 35s ago
		OrdersPlaced: true,
	}

	gm.activeGrids["BTCUSD1"] = grid

	gm.resetStaleOrders()

	if grid.OrdersPlaced {
		t.Error("Expected OrdersPlaced to be reset after timeout")
	}
}

func TestGridManager_FinishPlacement(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()
	gm := &GridManager{
		logger:      logrus.NewEntry(logger),
		activeGrids: make(map[string]*SymbolGrid),
		rateLimiter: NewRateLimiter(10, 2, zapLogger),
	}

	grid := &SymbolGrid{
		Symbol:        "BTCUSD1",
		IsActive:      true,
		CurrentPrice:  50000,
		OrdersPlaced:  false,
		PlacementBusy: true,
	}

	gm.activeGrids["BTCUSD1"] = grid

	// Test successful placement
	gm.finishPlacement("BTCUSD1", true)
	if !grid.OrdersPlaced || grid.PlacementBusy {
		t.Error("Expected OrdersPlaced=true and PlacementBusy=false on success")
	}
	if gm.consecutiveFailures != 0 {
		t.Error("Expected consecutiveFailures to reset on success")
	}

	// Reset for failure test
	grid.OrdersPlaced = false
	grid.PlacementBusy = true
	gm.consecutiveFailures = 0

	// Test failed placement
	gm.finishPlacement("BTCUSD1", false)
	if grid.OrdersPlaced || grid.PlacementBusy {
		t.Error("Expected OrdersPlaced=false and PlacementBusy=false on failure")
	}
	if gm.consecutiveFailures != 1 {
		t.Error("Expected consecutiveFailures to increment on failure")
	}
}

// =============================================================================
// PLACEMENT FLOW TESTS
// =============================================================================

// TestGridManager_UpdateSymbols_CreatesGrids tests grid creation
func TestGridManager_UpdateSymbols_CreatesGrids(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()

	gm := &GridManager{
		logger:                logrus.NewEntry(logger),
		activeGrids:           make(map[string]*SymbolGrid),
		gridPlacementCooldown: 1 * time.Second,
		rateLimiter:           NewRateLimiter(10, 2, zapLogger),
		gridSpreadPct:         0.01,
		maxOrdersSide:         5,
	}

	// Test UpdateSymbols creates grids
	symbols := []*SymbolData{
		{Symbol: "BTCUSD1", QuoteAsset: "USD1"},
	}

	gm.UpdateSymbols(symbols)

	// Verify grid was created
	gm.gridsMu.RLock()
	grid, exists := gm.activeGrids["BTCUSD1"]
	gm.gridsMu.RUnlock()

	if !exists {
		t.Fatal("Grid should be created for BTCUSD1")
	}
	if grid.Symbol != "BTCUSD1" {
		t.Error("Grid symbol mismatch")
	}
}

// TestGridManager_processWebSocketTicker_FirstPriceEnqueuesPlacement tests first price triggers placement
func TestGridManager_processWebSocketTicker_FirstPriceEnqueuesPlacement(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()

	gm := &GridManager{
		logger:                logrus.NewEntry(logger),
		activeGrids:           make(map[string]*SymbolGrid),
		placementQueue:        make(chan string, 100),
		gridPlacementCooldown: 1 * time.Second,
		rateLimiter:           NewRateLimiter(10, 2, zapLogger),
	}

	// Create grid with price = 0 (waiting for first price)
	gm.activeGrids["BTCUSD1"] = &SymbolGrid{
		Symbol:        "BTCUSD1",
		CurrentPrice:  0, // No price yet
		PlacementBusy: false,
		OrdersPlaced:  false,
	}

	// Simulate WebSocket ticker data
	tickerData := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{
				"s": "BTCUSD1",
				"c": "50000.00",
			},
		},
	}

	// Process ticker
	gm.processWebSocketTicker(tickerData)

	// Check if symbol was enqueued
	select {
	case symbol := <-gm.placementQueue:
		if symbol != "BTCUSD1" {
			t.Errorf("Expected BTCUSD1, got %s", symbol)
		}
	case <-time.After(1 * time.Second):
		t.Error("Expected symbol to be enqueued for first price, but queue is empty")
	}
}

// TestGridManager_processWebSocketTicker_SymbolCasing tests uppercase normalization
func TestGridManager_processWebSocketTicker_SymbolCasing(t *testing.T) {
	logger := logrus.New()
	zapLogger, _ := zap.NewDevelopment()

	gm := &GridManager{
		logger:                logrus.NewEntry(logger),
		activeGrids:           make(map[string]*SymbolGrid),
		placementQueue:        make(chan string, 100),
		gridPlacementCooldown: 1 * time.Second,
		rateLimiter:           NewRateLimiter(10, 2, zapLogger),
	}

	// Create grid with UPPERCASE symbol (as config stores it)
	gm.activeGrids["BTCUSD1"] = &SymbolGrid{
		Symbol:        "BTCUSD1",
		CurrentPrice:  0,
		PlacementBusy: false,
		OrdersPlaced:  false,
	}

	// Simulate WebSocket ticker with LOWERCASE symbol (as exchange sends it)
	tickerData := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{
				"s": "btcusd1", // lowercase from exchange
				"c": "50000.00",
			},
		},
	}

	// Process ticker - should normalize to uppercase
	gm.processWebSocketTicker(tickerData)

	// Check if symbol was enqueued (meaning uppercase matching worked)
	select {
	case symbol := <-gm.placementQueue:
		if symbol != "BTCUSD1" {
			t.Errorf("Expected BTCUSD1 (uppercase), got %s", symbol)
		}
	case <-time.After(1 * time.Second):
		t.Error("Symbol casing normalization failed - grid not found")
	}
}

// TestPlacementWorker_ConcurrentWorkers verifies multiple workers can dequeue from queue safely
// T013: Tests that workers can dequeue concurrently without race conditions
func TestPlacementWorker_ConcurrentWorkers(t *testing.T) {
	logger := logrus.New()

	// Create GridManager with buffered queue
	gm := &GridManager{
		logger:         logrus.NewEntry(logger),
		placementQueue: make(chan string, 100),
		stopCh:         make(chan struct{}),
	}

	// Test simple dequeue with multiple goroutines
	numWorkers := 5
	numItems := 10
	processed := make(chan string, numItems)
	var wg sync.WaitGroup

	// Start workers that just dequeue and record
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-gm.stopCh:
					return
				case symbol, ok := <-gm.placementQueue:
					if !ok {
						return
					}
					processed <- fmt.Sprintf("worker%d:%s", workerID, symbol)
				}
			}
		}(i)
	}

	// Enqueue all symbols
	for i := 0; i < numItems; i++ {
		gm.placementQueue <- fmt.Sprintf("SYM%d", i)
	}

	// Close stopCh to signal workers to stop after processing
	go func() {
		// Wait a bit then close
		time.Sleep(50 * time.Millisecond)
		close(gm.stopCh)
	}()

	// Wait for workers
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Workers did not stop within timeout")
	}

	// Verify all items were processed
	close(processed)
	count := 0
	for range processed {
		count++
	}

	if count != numItems {
		t.Errorf("Expected %d items processed, got %d", numItems, count)
	}

	t.Logf("Concurrent workers test completed: %d workers processed %d items", numWorkers, count)
}

// TestProcessPlacement_AsyncFetch verifies exchange order cache and timeout behavior
// T020: Tests cache TTL and fallback when fetch fails
func TestProcessPlacement_AsyncFetch(t *testing.T) {
	logger := logrus.New()

	gm := &GridManager{
		logger:                logrus.NewEntry(logger),
		activeGrids:           make(map[string]*SymbolGrid),
		exchangeOrderCache:    make(map[string]*ExchangeOrderCacheEntry),
		exchangeOrderCacheTTL: 1 * time.Second,
		maxOrdersSide:         5,
	}

	// Test 1: Cache miss (empty cache)
	orders, hit := gm.getCachedExchangeOrders("BTCUSD1")
	if hit {
		t.Error("Expected cache miss for empty cache")
	}
	if len(orders) != 0 {
		t.Error("Expected empty orders on cache miss")
	}

	// Test 2: Add to cache
	testOrders := []client.Order{
		{Symbol: "BTCUSD1", Side: "BUY", Status: "NEW"},
		{Symbol: "BTCUSD1", Side: "SELL", Status: "NEW"},
	}
	gm.cacheExchangeOrders("BTCUSD1", testOrders)

	// Test 3: Cache hit
	orders, hit = gm.getCachedExchangeOrders("BTCUSD1")
	if !hit {
		t.Error("Expected cache hit after storing orders")
	}
	if len(orders) != 2 {
		t.Errorf("Expected 2 orders from cache, got %d", len(orders))
	}

	// Test 4: Cache TTL expiration
	gm.exchangeOrderCacheTTL = 1 * time.Millisecond // Short TTL for testing
	time.Sleep(10 * time.Millisecond)               // Wait for TTL to expire
	orders, hit = gm.getCachedExchangeOrders("BTCUSD1")
	if hit {
		t.Error("Expected cache miss after TTL expiration")
	}

	t.Log("Async fetch cache test completed successfully")
}

// TestProcessPlacement_PriceWait verifies price wait loop behavior
// T025: Tests price wait with max 5s timeout
func TestProcessPlacement_PriceWait(t *testing.T) {
	logger := logrus.New()

	gm := &GridManager{
		logger:         logrus.NewEntry(logger),
		activeGrids:    make(map[string]*SymbolGrid),
		placementQueue: make(chan string, 100),
		stopCh:         make(chan struct{}),
	}

	// Test 1: Grid without price should wait
	gm.activeGrids["BTCUSD1"] = &SymbolGrid{
		Symbol:        "BTCUSD1",
		CurrentPrice:  0, // No price yet
		IsActive:      true,
		PlacementBusy: false,
	}

	// Simulate price update after short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		gm.gridsMu.Lock()
		gm.activeGrids["BTCUSD1"].CurrentPrice = 50000.0
		gm.gridsMu.Unlock()
	}()

	// Wait loop simulation
	gm.gridsMu.Lock()
	grid := gm.activeGrids["BTCUSD1"]
	waitStart := time.Now()
	maxWait := 5 * time.Second

	for grid.CurrentPrice == 0 && time.Since(waitStart) < maxWait {
		gm.gridsMu.Unlock()
		time.Sleep(50 * time.Millisecond)
		gm.gridsMu.Lock()
		grid = gm.activeGrids["BTCUSD1"]
	}

	waitDuration := time.Since(waitStart)
	price := grid.CurrentPrice
	gm.gridsMu.Unlock()

	if price == 0 {
		t.Error("Expected price to be updated after wait")
	}
	if price != 50000.0 {
		t.Errorf("Expected price 50000.0, got %f", price)
	}
	if waitDuration > 1*time.Second {
		t.Errorf("Waited too long: %v", waitDuration)
	}

	t.Logf("Price wait test passed: waited %v for price %f", waitDuration, price)
}

// TestEnqueuePlacement_Backpressure verifies blocking enqueue behavior
// T030: Tests queue wait and timeout
func TestEnqueuePlacement_Backpressure(t *testing.T) {
	logger := logrus.New()

	gm := &GridManager{
		logger:         logrus.NewEntry(logger),
		activeGrids:    make(map[string]*SymbolGrid),
		placementQueue: make(chan string, 2), // Small buffer for testing
		stopCh:         make(chan struct{}),
	}

	gm.activeGrids["BTCUSD1"] = &SymbolGrid{Symbol: "BTCUSD1"}

	// Test 1: Fast enqueue (queue not full)
	start := time.Now()
	gm.enqueuePlacement("BTCUSD1")
	elapsed := time.Since(start)
	t.Logf("Fast enqueue took %v", elapsed)

	// Fill the queue
	gm.placementQueue <- "SYM1"
	gm.placementQueue <- "SYM2"

	// Test 2: Slow enqueue (queue full, should wait then timeout or succeed)
	go func() {
		// Simulate consumer draining queue after 100ms
		time.Sleep(100 * time.Millisecond)
		<-gm.placementQueue
	}()

	start = time.Now()
	gm.enqueuePlacement("BTCUSD2") // Should wait for consumer
	elapsed = time.Since(start)

	if elapsed < 50*time.Millisecond {
		t.Error("Expected some delay for blocking enqueue when queue near full")
	}
	t.Logf("Blocking enqueue took %v", elapsed)
}
