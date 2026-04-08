.PHONY: all build run test test-unit test-integration test-market test-account test-order test-workflow test-run test-place-limit test-place-market test-order-lifecycle test-cancel-order test-order-complete test-workflow-complete test-public-market test-aster-all setup-tests test-help clean deps termux-start termux-stop termux-status termux-logs termux-restart termux-daemon dashboard dashboard-script

# The name of our output binary
APP_NAME := aster-bot

# Variables
CMD_DIR := ./cmd/bot
CONFIG_FILE := config.yaml

all: build

# Download local dependencies
deps:
	go mod download
	go mod tidy

# Build the bot binary
build: deps
	go build -o bin/$(APP_NAME) $(CMD_DIR)

# Run the bot directly
run:
	go run $(CMD_DIR)/main.go $(CONFIG_FILE)

# ============================================================================
# Test Targets
# ============================================================================

# Run all tests (unit + integration)
test:
	go test -v -timeout 10m ./...

# Run unit tests only (no integration tests)
test-unit:
	go test -v -short ./...

# Run all integration tests (requires API credentials in .env or env vars)
test-integration:
	@echo "Running all integration tests..."
	@echo "Make sure API credentials are set in .env or environment variables"
	go test -v -timeout 15m ./internal/client/... -run '^Test'

# Run market data tests only (public APIs, no credentials needed)
test-market:
	@echo "Running market data tests (public APIs)..."
	go test -v -timeout 10m ./internal/client/... -run '^TestMarket'

# Run account tests (authenticated APIs, requires credentials)
test-account:
	@echo "Running account tests (requires API credentials)..."
	go test -v -timeout 10m ./internal/client/... -run '^TestAccount'

# Run order operation tests (creates real orders, requires credentials)
test-order:
	@echo "Running order tests (creates real orders, requires credentials)..."
	@echo "⚠️  WARNING: This will create real orders on the exchange!"
	go test -v -timeout 15m ./internal/client/... -run '^TestOrder'

# Run end-to-end workflow test
test-workflow:
	@echo "Running end-to-end workflow test..."
	@echo "⚠️  WARNING: This will create and cancel real orders!"
	go test -v -timeout 10m ./internal/client/... -run '^TestOrderWorkflow'

# Run a specific test by name
test-run:
	@echo "Usage: make test-run TEST=TestNameHere"
	@echo "Example: make test-run TEST=TestMarketPing"
	go test -v -timeout 10m ./internal/client/... -run '^$(TEST)$$'

# ============================================================================
# Aster Client Specific Tests (With Detailed Logging)
# ============================================================================

# Test placeOrder functionality (LIMIT orders)
test-place-limit:
	@echo "============================================"
	@echo "Testing PlaceOrder (LIMIT) - Aster Client"
	@echo "============================================"
	@echo ""
	@echo "This test will:"
	@echo "  1. Get current market price (BookTicker)"
	@echo "  2. Place a LIMIT BUY order at -1% from bid"
	@echo "  3. Verify order was created"
	@echo "  4. Cancel the order (cleanup)"
	@echo ""
	@echo "Expected logs:"
	@echo "  ✓ PlaceLimitOrder succeeded: (OrderID, Status, Price, Qty)"
	@echo "  ✓ cancelled order ID xxxx"
	@echo ""
	go test -v -timeout 15m ./internal/client/... -run TestOrderPlaceLimitOrder 2>&1

# Test placeOrder functionality (MARKET orders)
test-place-market:
	@echo "============================================"
	@echo "Testing PlaceOrder (MARKET) - Aster Client"
	@echo "============================================"
	@echo ""
	@echo "This test will:"
	@echo "  1. Place a MARKET BUY order (immediate execution)"
	@echo "  2. Verify order execution"
	@echo "  3. Cancel/cleanup"
	@echo ""
	@echo "Expected logs:"
	@echo "  ✓ PlaceMarketOrder succeeded: (OrderID, Status, ExecutedQty)"
	@echo ""
	go test -v -timeout 15m ./internal/client/... -run TestOrderPlaceMarketOrder 2>&1

# Test complete order lifecycle (place + retrieve + cancel)
test-order-lifecycle:
	@echo "============================================"
	@echo "Testing Order Lifecycle - Aster Client"
	@echo "============================================"
	@echo ""
	@echo "This test will:"
	@echo "  1. Place LIMIT BUY order"
	@echo "  2. Retrieve open orders"
	@echo "  3. Verify order appears in list"
	@echo "  4. Cancel order"
	@echo "  5. Verify order is cancelled"
	@echo ""
	@echo "Expected logs:"
	@echo "  ✓ Order placed"
	@echo "  ✓ Found order in open orders"
	@echo "  ✓ Order cancelled successfully"
	@echo ""
	go test -v -timeout 15m ./internal/client/... -run TestOrderGetOpenOrders 2>&1

# Test cancel order functionality
test-cancel-order:
	@echo "============================================"
	@echo "Testing CancelOrder - Aster Client"
	@echo "============================================"
	@echo ""
	@echo "This test will:"
	@echo "  1. Place a LIMIT order"
	@echo "  2. Cancel by OrderID"
	@echo "  3. Verify status is CANCELED"
	@echo ""
	@echo "Expected logs:"
	@echo "  ✓ CancelOrder succeeded:"
	@echo "    OrderID: xxxx"
	@echo "    Status: CANCELED"
	@echo ""
	go test -v -timeout 15m ./internal/client/... -run TestOrderCancelOrder 2>&1

# Test all order operations in sequence
test-order-complete:
	@echo "============================================"
	@echo "Testing All Order Operations - Aster Client"
	@echo "============================================"
	@echo ""
	@echo "This comprehensive test will:"
	@echo "  1. ✓ Get market price"
	@echo "  2. ✓ Place LIMIT order"
	@echo "  3. ✓ Place MARKET order"
	@echo "  4. ✓ Retrieve open orders"
	@echo "  5. ✓ Cancel orders"
	@echo "  6. ✓ Verify all operations succeeded"
	@echo ""
	@echo "All orders will be automatically cleaned up"
	@echo ""
	go test -v -timeout 30m ./internal/client/... -run TestOrder 2>&1

# Test complete workflow (account -> orders -> positions)
test-workflow-complete:
	@echo "============================================"
	@echo "Testing Complete Workflow - Aster Client"
	@echo "============================================"
	@echo ""
	@echo "This test will:"
	@echo "  1. Fetch account information"
	@echo "  2. Fetch current market price"
	@echo "  3. Place BUY and SELL limit orders"
	@echo "  4. Retrieve open orders"
	@echo "  5. Fetch positions"
	@echo "  6. Cancel orders"
	@echo "  7. Verify cleanup"
	@echo ""
	@echo "===== STARTING WORKFLOW TEST ====="
	@echo ""
	go test -v -timeout 30m ./internal/client/... -run '^TestOrderWorkflow$$' 2>&1
	@echo ""
	@echo "===== WORKFLOW TEST COMPLETE ====="

# Test all public market data endpoints (no credentials needed)
test-public-market:
	@echo "============================================"
	@echo "Testing Public Market Data - Aster Client"
	@echo "============================================"
	@echo ""
	@echo "These tests do NOT require API credentials"
	@echo ""
	@echo "Testing:"
	@echo "  ✓ Ping (server connectivity)"
	@echo "  ✓ ServerTime (time sync)"
	@echo "  ✓ Klines (candlestick data)"
	@echo "  ✓ MarkPrice (funding rate)"
	@echo "  ✓ BookTicker (best bid/ask)"
	@echo "  ✓ FundingRates (rate history)"
	@echo "  ✓ ExchangeInfo (configuration)"
	@echo ""
	go test -v -timeout 20m ./internal/client/... -run TestMarket 2>&1

# Test everything with full verbose output
test-aster-all:
	@echo "============================================"
	@echo "Testing ALL Aster API Client Functions"
	@echo "============================================"
	@echo ""
	@echo "This will run:"
	@echo "  • Market data tests (public, no auth)"
	@echo "  • Account tests (with auth)"
	@echo "  • Order operations (with auth)"
	@echo "    - PlaceOrder (LIMIT & MARKET)"
	@echo "    - GetOpenOrders"
	@echo "    - CancelOrder"
	@echo "  • End-to-end workflow test"
	@echo ""
	@echo "Make sure API credentials are set:"
	@echo "  export ASTER_API_KEY=..."
	@echo "  export ASTER_API_SECRET=..."
	@echo ""
	@echo "Starting comprehensive test suite..."
	@echo ""
	go test -v -timeout 60m ./internal/client/... 2>&1
	@echo ""
	@echo "✓ Test results saved to test-results.log"

# ============================================================================
# Setup & Utilities
# ============================================================================

# Setup integration test environment (interactive)
setup-tests:
	@echo "Setting up integration tests..."
	@if [ -f setup-integration-tests.sh ]; then \
		chmod +x setup-integration-tests.sh; \
		./setup-integration-tests.sh; \
	else \
		echo "setup-integration-tests.sh not found"; \
	fi

# Show help for test targets
test-help:
	@echo "╔════════════════════════════════════════════════════════════════╗"
	@echo "║           Aster API Client Integration Tests                   ║"
	@echo "╚════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "📋 QUICK START:"
	@echo "  make test-place-limit      - Test PlaceOrder (LIMIT orders)"
	@echo "  make test-place-market     - Test PlaceOrder (MARKET orders)"
	@echo "  make test-order-complete   - Test all order operations"
	@echo "  make test-aster-all        - Run complete test suite"
	@echo ""
	@echo "🔍 SPECIFIC TESTS:"
	@echo "  make test-public-market    - Market data tests (no auth needed)"
	@echo "  make test-market           - All market data tests"
	@echo "  make test-account          - Account info tests (requires auth)"
	@echo "  make test-order            - All order tests (creates real orders)"
	@echo "  make test-order-lifecycle  - Test order place/retrieve/cancel"
	@echo "  make test-cancel-order     - Test cancellation"
	@echo "  make test-workflow-complete - End-to-end workflow test"
	@echo ""
	@echo "🎯 BASIC TESTS:"
	@echo "  make test-integration      - Run all integration tests"
	@echo "  make test-unit             - Run unit tests only"
	@echo "  make test                  - Run all tests"
	@echo ""
	@echo "⚙️  SETUP & UTILITIES:"
	@echo "  make setup-tests           - Interactive setup (creates .env)"
	@echo "  make test-run TEST=...     - Run specific test"
	@echo ""
	@echo "📝 ENVIRONMENT:"
	@echo "  Required env vars for authenticated tests:"
	@echo "    export ASTER_API_KEY=your_key"
	@echo "    export ASTER_API_SECRET=your_secret"
	@echo ""
	@echo "⚠️  NOTE:"
	@echo "  Order tests (*-order, *-place, *-cancel) create REAL orders"
	@echo "  Orders are automatically cleaned up after tests complete"
	@echo "  All tests show detailed logs with ✓ confirmations"
	@echo ""
	@echo "📚 For detailed documentation: see INTEGRATION_TEST_README.md"

# Clean build artifacts
clean:
	rm -rf bin/
	go clean

# ============================================================================
# Termux (Android) Targets - Run with wake lock (non-sleep mode)
# ============================================================================

# Start volume farming bot on Termux with wake lock (keeps running when screen off)
termux-start:
	@echo "============================================"
	@echo "Starting Volume Farming Bot on Termux"
	@echo "============================================"
	@echo ""
	@echo "Features:"
	@echo "  • Wake lock enabled (runs when screen off)"
	@echo "  • Auto-discover trading pairs"
	@echo "  • Grid trading with volume farming"
	@echo "  • Logs to volume-farm.log"
	@echo ""
	@chmod +x run-volume-farm-termux.sh 2>/dev/null || true
	@./run-volume-farm-termux.sh start ./config/volume-farm-config.yaml

# Stop the volume farming bot
termux-stop:
	@echo "Stopping Volume Farming Bot..."
	@chmod +x run-volume-farm-termux.sh 2>/dev/null || true
	@./run-volume-farm-termux.sh stop

# Check bot status
termux-status:
	@chmod +x run-volume-farm-termux.sh 2>/dev/null || true
	@./run-volume-farm-termux.sh status

# View live logs
termux-logs:
	@echo "Viewing logs (Ctrl+C to exit)..."
	@chmod +x run-volume-farm-termux.sh 2>/dev/null || true
	@./run-volume-farm-termux.sh logs

# Restart the bot
termux-restart:
	@echo "Restarting Volume Farming Bot..."
	@chmod +x run-volume-farm-termux.sh 2>/dev/null || true
	@./run-volume-farm-termux.sh restart ./config/volume-farm-config.yaml

# Run in daemon mode with auto-restart
termux-daemon:
	@echo "Starting in daemon mode (auto-restart on crash)..."
	@echo "Press Ctrl+C to stop daemon watcher (bot will keep running)"
	@echo "Use 'make termux-stop' to stop the bot completely"
	@echo ""
	@chmod +x run-volume-farm-termux.sh 2>/dev/null || true
	@./run-volume-farm-termux.sh daemon ./config/volume-farm-config.yaml

# Quick setup for Termux (install termux-api if needed)
termux-setup:
	@echo "Setting up Termux environment..."
	@echo ""
	@echo "1. Installing termux-api (for wake lock)..."
	@pkg install termux-api -y 2>/dev/null || echo "termux-api already installed or manual install needed"
	@echo ""
	@echo "2. Building binary..."
	@go build -o bin/volume-farm ./cmd/volume-farm
	@echo ""
	@echo "Setup complete! Use 'make termux-start' to run"

# ============================================================================
# Dashboard (Real-time Metrics)
# ============================================================================

# Launch real-time dashboard (refresh 1s)
dashboard:
	@echo "============================================"
	@echo "Starting Grid Trading Dashboard"
	@echo "============================================"
	@echo ""
	@echo "Features:"
	@echo "  • Real-time metrics (refresh 1s)"
	@echo "  • Volume, orders, fill rate"
	@echo "  • PnL, positions, risk status"
	@echo "  • Press Ctrl+C to exit"
	@echo ""
	@if command -v python3 >/dev/null 2>&1; then \
		python3 ./scripts/dashboard.py; \
	elif command -v python >/dev/null 2>&1; then \
		python ./scripts/dashboard.py; \
	else \
		echo "ERROR: Python not found. Install with: pkg install python"; \
		exit 1; \
	fi

# Dashboard from run script (alternative)
dashboard-script:
	@chmod +x run-volume-farm-termux.sh 2>/dev/null || true
	@./run-volume-farm-termux.sh dashboard

# Simple dashboard for Termux/small terminals (no curses)
dashboard-simple:
	@echo "============================================"
	@echo "Starting Simple Dashboard (Termux-friendly)"
	@echo "============================================"
	@echo ""
	@echo "Features:"
	@echo "  • Real-time metrics (refresh 2s)"
	@echo "  • Works on any terminal size"
	@echo "  • Press Q+Enter or Ctrl+C to exit"
	@echo ""
	@if command -v python3 >/dev/null 2>&1; then \
		python3 ./scripts/dashboard_simple.py; \
	elif command -v python >/dev/null 2>&1; then \
		python ./scripts/dashboard_simple.py; \
	else \
		echo "ERROR: Python not found. Install with: pkg install python"; \
		exit 1; \
	fi
