#!/bin/bash
# Aster Client PlaceOrder Test Script
# This script tests the placeOrder functionality with detailed logging
# Usage: ./test-place-order.sh

set -e

echo "╔════════════════════════════════════════════════════════════════╗"
echo "║     Aster Client - PlaceOrder Function Tests                   ║"
echo "╚════════════════════════════════════════════════════════════════╝"
echo ""

# Check if credentials are set
if [ -z "$ASTER_API_KEY" ] || [ -z "$ASTER_API_SECRET" ]; then
    echo "❌ Error: API credentials not set"
    echo ""
    echo "Please set credentials first:"
    echo "  export ASTER_API_KEY='your_key'"
    echo "  export ASTER_API_SECRET='your_secret'"
    echo ""
    echo "Or create and source .env file:"
    echo "  cp .env.example .env"
    echo "  source .env"
    exit 1
fi

echo "✓ API credentials detected"
echo ""

# Check if we're in the backend directory
if [ ! -f "go.mod" ]; then
    echo "❌ Error: go.mod not found"
    echo "Please run this script from the backend/ directory"
    exit 1
fi

echo "Starting PlaceOrder tests..."
echo ""

# Test 1: PlaceOrder LIMIT
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST 1: PlaceOrder (LIMIT) - Creates and cancels a limit order"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if go test -v -timeout 15m ./internal/client/... -run '^TestOrderPlaceLimitOrder$'; then
    echo ""
    echo "✓ PASS: PlaceOrder (LIMIT) test passed"
    echo ""
else
    echo ""
    echo "❌ FAIL: PlaceOrder (LIMIT) test failed"
    exit 1
fi

# Test 2: PlaceOrder MARKET
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST 2: PlaceOrder (MARKET) - Creates and cancels a market order"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if go test -v -timeout 15m ./internal/client/... -run '^TestOrderPlaceMarketOrder$'; then
    echo ""
    echo "✓ PASS: PlaceOrder (MARKET) test passed"
    echo ""
else
    echo ""
    echo "❌ FAIL: PlaceOrder (MARKET) test failed"
    exit 1
fi

# Test 3: Cancel Order
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST 3: CancelOrder - Creates, then cancels by OrderID"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if go test -v -timeout 15m ./internal/client/... -run '^TestOrderCancelOrder$'; then
    echo ""
    echo "✓ PASS: CancelOrder test passed"
    echo ""
else
    echo ""
    echo "❌ FAIL: CancelOrder test failed"
    exit 1
fi

# Summary
echo "╔════════════════════════════════════════════════════════════════╗"
echo "║                   🎉 ALL TESTS PASSED! 🎉                      ║"
echo "╚════════════════════════════════════════════════════════════════╝"
echo ""
echo "Summary:"
echo "  ✓ PlaceOrder (LIMIT) - Working correctly"
echo "  ✓ PlaceOrder (MARKET) - Working correctly"
echo "  ✓ CancelOrder - Working correctly"
echo ""
echo "All orders were automatically cleaned up."
echo ""
echo "Next steps:"
echo "  • Run complete workflow: make test-workflow-complete"
echo "  • Run all tests: make test-aster-all"
echo "  • See: TEST_ASTER_CLIENT.md for more commands"
echo ""
