@echo off
REM Aster Client PlaceOrder Test Script (Windows)
REM This script tests the placeOrder functionality with detailed logging
REM Usage: test-place-order.bat

setlocal enabledelayedexpansion

echo.
echo ────────────────────────────────────────────────────────────────────
echo      Aster Client - PlaceOrder Function Tests (Windows)
echo ────────────────────────────────────────────────────────────────────
echo.

REM Check if credentials are set
if "!ASTER_API_KEY!"=="" (
    echo ERROR: API credentials not set
    echo.
    echo Please set credentials first:
    echo   set ASTER_API_KEY=your_key
    echo   set ASTER_API_SECRET=your_secret
    echo.
    echo Or create and source .env file:
    echo   copy .env.example .env
    echo   type .env
    exit /b 1
)

echo OK: API credentials detected
echo.

REM Check if we're in the backend directory
if not exist "go.mod" (
    echo ERROR: go.mod not found
    echo Please run this script from the backend\ directory
    exit /b 1
)

echo Starting PlaceOrder tests...
echo.

REM Test 1: PlaceOrder LIMIT
echo ────────────────────────────────────────────────────────────────────
echo TEST 1: PlaceOrder ^(LIMIT^) - Creates and cancels a limit order
echo ────────────────────────────────────────────────────────────────────
echo.

go test -v -timeout 15m ./internal/client/... -run "^TestOrderPlaceLimitOrder$"
if errorlevel 1 (
    echo.
    echo FAIL: PlaceOrder ^(LIMIT^) test failed
    exit /b 1
)

echo.
echo OK: PlaceOrder ^(LIMIT^) test passed
echo.

REM Test 2: PlaceOrder MARKET
echo ────────────────────────────────────────────────────────────────────
echo TEST 2: PlaceOrder ^(MARKET^) - Creates and cancels a market order
echo ────────────────────────────────────────────────────────────────────
echo.

go test -v -timeout 15m ./internal/client/... -run "^TestOrderPlaceMarketOrder$"
if errorlevel 1 (
    echo.
    echo FAIL: PlaceOrder ^(MARKET^) test failed
    exit /b 1
)

echo.
echo OK: PlaceOrder ^(MARKET^) test passed
echo.

REM Test 3: Cancel Order
echo ────────────────────────────────────────────────────────────────────
echo TEST 3: CancelOrder - Creates, then cancels by OrderID
echo ────────────────────────────────────────────────────────────────────
echo.

go test -v -timeout 15m ./internal/client/... -run "^TestOrderCancelOrder$"
if errorlevel 1 (
    echo.
    echo FAIL: CancelOrder test failed
    exit /b 1
)

REM Summary
echo.
echo ────────────────────────────────────────────────────────────────────
echo                      ALL TESTS PASSED!
echo ────────────────────────────────────────────────────────────────────
echo.
echo Summary:
echo   OK PlaceOrder ^(LIMIT^) - Working correctly
echo   OK PlaceOrder ^(MARKET^) - Working correctly
echo   OK CancelOrder - Working correctly
echo.
echo All orders were automatically cleaned up.
echo.
echo Next steps:
echo   - Run complete workflow: make test-workflow-complete
echo   - Run all tests: make test-aster-all
echo   - See: TEST_ASTER_CLIENT.md for more commands
echo.

endlocal
