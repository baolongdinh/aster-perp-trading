@echo off
REM Quick setup script for Aster API Integration Tests (Windows)
REM This script helps you set up environment variables for running integration tests

setlocal enabledelayedexpansion

echo ========================================
echo Aster API Integration Test Setup
echo ========================================
echo.

if exist .env (
    echo Found .env file
    set /p use_env="Use existing .env? (y/n): "
    if /i "!use_env!"=="y" (
        echo Loading .env...
        for /f "tokens=*" %%a in (.env) do (
            if not "%%a"=="" (
                set "%%a"
            )
        )
    )
) else (
    echo No .env file found. Creating template...
    copy .env.example .env
    echo Created .env - please edit with your credentials
)

echo.
echo Select Authentication Method:
echo 1) V1 Authentication (HMAC-SHA256)
echo 2) V3 Authentication (Wallet-based)
set /p auth_choice="Choice (1 or 2): "

if "%auth_choice%"=="1" (
    echo.
    echo === V1 Authentication Setup ===
    set /p api_key="Enter ASTER_API_KEY: "
    set /p api_secret="Enter ASTER_API_SECRET: "
    
    echo Setting env variables...
    setx ASTER_API_KEY "%api_key%"
    setx ASTER_API_SECRET "%api_secret%"
    
    echo V1 credentials saved
    
) else if "%auth_choice%"=="2" (
    echo.
    echo === V3 Authentication Setup ===
    set /p user_wallet="Enter ASTER_USER_WALLET: "
    set /p api_signer="Enter ASTER_API_SIGNER: "
    set /p api_signer_key="Enter ASTER_API_SIGNER_KEY: "
    
    echo Setting env variables...
    setx ASTER_USER_WALLET "%user_wallet%"
    setx ASTER_API_SIGNER "%api_signer%"
    setx ASTER_API_SIGNER_KEY "%api_signer_key%"
    
    echo V3 credentials saved
    
) else (
    echo Invalid choice
    exit /b 1
)

echo.
echo ========================================
echo Setup Complete!
echo ========================================
echo.
echo ^! IMPORTANT:
echo   1. Keep credentials SECRET - NEVER commit to git
echo   2. Add .env to .gitignore
echo   3. You may need to restart your terminal for env vars to take effect
echo.
echo Next Steps:
echo   1. cd backend
echo   2. Run tests:
echo        REM All tests
echo        go test -v ./internal/client/... -run "^Test"
echo.
echo        REM Market data only (no auth needed)
echo        go test -v ./internal/client/... -run "^TestMarket"
echo.
echo        REM With timeout
echo        go test -v -timeout 10m ./internal/client/... -run "^Test"
echo.

endlocal
