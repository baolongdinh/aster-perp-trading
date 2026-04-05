#!/bin/bash

echo "🚀 Testing Volume Farming with Stop Loss & Volume Control"
echo "=================================================="

# Build the project
echo "📦 Building project..."
go build -o volume-farm ./cmd/volume-farm
if [ $? -ne 0 ]; then
    echo "❌ Build failed!"
    exit 1
fi

echo "✅ Build successful!"

# Run with new config
echo "🔄 Starting volume farming with enhanced config..."
echo "Config: volume-farming-config.yaml"
echo ""
echo "🛡️  Safety Features Enabled:"
echo "   ✅ Stop Loss: 2% per trade"
echo "   ✅ Volume Control: Max $500 per symbol"
echo "   ✅ Order Limits: Max 8 active orders per symbol"
echo "   ✅ Fast Cooldown: 10 seconds"
echo ""

# Run the application
./volume-farm -config volume-farming-config.yaml
