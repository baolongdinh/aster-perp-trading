#!/bin/bash
# Quick setup script for Aster API Integration Tests
# This script helps you set up environment variables for running integration tests

set -e

echo "========================================"
echo "Aster API Integration Test Setup"
echo "========================================"
echo ""

# Check if .env file exists
if [ -f .env ]; then
    echo "✓ Found .env file"
    read -p "Use existing .env? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Loading .env..."
        source .env
    fi
else
    echo "⚠ No .env file found. Creating template..."
    cp .env.example .env
    echo "✓ Created .env - please edit with your credentials"
fi

echo ""
echo "Select Authentication Method:"
echo "1) V1 Authentication (HMAC-SHA256)"
echo "2) V3 Authentication (Wallet-based)"
read -p "Choice (1 or 2): " auth_choice

if [ "$auth_choice" = "1" ]; then
    echo ""
    echo "=== V1 Authentication Setup ==="
    read -p "Enter ASTER_API_KEY: " api_key
    read -sp "Enter ASTER_API_SECRET: " api_secret
    echo ""
    
    # Update .env
    if grep -q "^ASTER_API_KEY=" .env; then
        sed -i.bak "s/^ASTER_API_KEY=.*/ASTER_API_KEY=$api_key/" .env
    else
        echo "ASTER_API_KEY=$api_key" >> .env
    fi
    
    if grep -q "^ASTER_API_SECRET=" .env; then
        sed -i.bak "s/^ASTER_API_SECRET=.*/ASTER_API_SECRET=$api_secret/" .env
    else
        echo "ASTER_API_SECRET=$api_secret" >> .env
    fi
    
    echo "✓ V1 credentials saved to .env"

elif [ "$auth_choice" = "2" ]; then
    echo ""
    echo "=== V3 Authentication Setup ==="
    read -p "Enter ASTER_USER_WALLET: " user_wallet
    read -p "Enter ASTER_API_SIGNER: " api_signer
    read -sp "Enter ASTER_API_SIGNER_KEY: " api_signer_key
    echo ""
    
    # Update .env
    if grep -q "^ASTER_USER_WALLET=" .env; then
        sed -i.bak "s|^ASTER_USER_WALLET=.*|ASTER_USER_WALLET=$user_wallet|" .env
    else
        echo "ASTER_USER_WALLET=$user_wallet" >> .env
    fi
    
    if grep -q "^ASTER_API_SIGNER=" .env; then
        sed -i.bak "s|^ASTER_API_SIGNER=.*|ASTER_API_SIGNER=$api_signer|" .env
    else
        echo "ASTER_API_SIGNER=$api_signer" >> .env
    fi
    
    if grep -q "^ASTER_API_SIGNER_KEY=" .env; then
        sed -i.bak "s|^ASTER_API_SIGNER_KEY=.*|ASTER_API_SIGNER_KEY=$api_signer_key|" .env
    else
        echo "ASTER_API_SIGNER_KEY=$api_signer_key" >> .env
    fi
    
    echo "✓ V3 credentials saved to .env"
else
    echo "Invalid choice"
    exit 1
fi

echo ""
echo "========================================"
echo "Setup Complete!"
echo "========================================"
echo ""
echo "⚠️  IMPORTANT:"
echo "  1. Keep your .env file SECRET - NEVER commit it to git"
echo "  2. Add .env to .gitignore"
echo "  3. Credentials in .env override config.yaml"
echo ""
echo "Next Steps:"
echo "  1. cd backend"
echo "  2. Run tests:"
echo "       # All tests"
echo "       go test -v ./internal/client/... -run '^Test'"
echo ""
echo "       # Market data only (no auth needed)"
echo "       go test -v ./internal/client/... -run '^TestMarket'"
echo ""
echo "       # With timeout"
echo "       go test -v -timeout 10m ./internal/client/... -run '^Test'"
echo ""
