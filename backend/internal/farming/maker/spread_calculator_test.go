package maker

import (
	"testing"
	"time"

	"aster-bot/internal/farming/volume_optimization"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestSpreadCalculator_VPINIntegration(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		DefaultSpreadBps: 2.0,
	}

	// Create mock position manager
	positionMgr := &MockInventoryManager{}
	spreadCalc := NewSpreadCalculator(config, positionMgr)

	// Create VPIN monitor
	vpinConfig := volume_optimization.VPINConfig{
		WindowSize:        10,
		BucketSize:        100,
		VPINThreshold:     0.6,
		SustainedBreaches: 3,
		AutoResumeDelay:   30 * time.Second,
	}
	vpin := volume_optimization.NewVPINMonitor(vpinConfig, logger)
	spreadCalc.SetVPINMonitor(vpin)

	// Test 1: Normal VPIN (healthy flow)
	vpin.UpdateVolume(50, 50) // Balanced volume
	buySpread, sellSpread := spreadCalc.GetSpreadForSymbol("BTCUSD1")
	assert.Greater(t, buySpread, 0.5, "Buy spread should be > 0.5 bps for healthy flow")
	assert.Greater(t, sellSpread, 0.5, "Sell spread should be > 0.5 bps for healthy flow")

	// Test 2: High VPIN (toxic flow)
	vpin.UpdateVolume(100, 10) // Highly imbalanced volume
	buySpread, sellSpread = spreadCalc.GetSpreadForSymbol("BTCUSD1")
	assert.GreaterOrEqual(t, buySpread, 2.0, "Buy spread should widen to >= 2 bps for toxic flow")
	assert.GreaterOrEqual(t, sellSpread, 2.0, "Sell spread should widen to >= 2 bps for toxic flow")
}

func TestSpreadCalculator_OBIIntegration(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		DefaultSpreadBps: 2.0,
	}

	positionMgr := &MockInventoryManager{}
	spreadCalc := NewSpreadCalculator(config, positionMgr)

	// Create OBI detector
	obi := volume_optimization.NewOrderBookImbalanceDetector(10, 0.5, logger)
	spreadCalc.SetOBIDetector(obi)

	// Test 1: Balanced orderbook
	obi.UpdateOrderBook(1000, 1000, 50000) // Balanced
	buySpread, sellSpread := spreadCalc.GetSpreadForSymbol("BTCUSD1")
	assert.Greater(t, buySpread, 0.5, "Buy spread should be > 0.5 bps for balanced book")
	assert.Greater(t, sellSpread, 0.5, "Sell spread should be > 0.5 bps for balanced book")

	// Test 2: Strong buy imbalance
	obi.UpdateOrderBook(2000, 500, 50000) // More buyers
	buySpread, sellSpread = spreadCalc.GetSpreadForSymbol("BTCUSD1")
	// Sell spread should tighten to attract sellers
	assert.Less(t, sellSpread, 2.0, "Sell spread should tighten < 2 bps for buy imbalance")

	// Test 3: Strong sell imbalance
	obi.UpdateOrderBook(500, 2000, 50000) // More sellers
	buySpread, sellSpread = spreadCalc.GetSpreadForSymbol("BTCUSD1")
	// Buy spread should tighten to attract buyers
	assert.Less(t, buySpread, 2.0, "Buy spread should tighten < 2 bps for sell imbalance")
}

func TestSpreadCalculator_MicrostructureIntegration(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		DefaultSpreadBps: 2.0,
	}

	positionMgr := &MockInventoryManager{}
	spreadCalc := NewSpreadCalculator(config, positionMgr)

	// Create VPIN and OBI
	vpin := volume_optimization.NewVPINMonitor(volume_optimization.VPINConfig{
		WindowSize:        10,
		BucketSize:        100,
		VPINThreshold:     0.6,
		SustainedBreaches: 3,
		AutoResumeDelay:   30 * time.Second,
	}, logger)

	obi := volume_optimization.NewOrderBookImbalanceDetector(10, 0.5, logger)

	// Create microstructure analyzer
	analyzer := volume_optimization.NewMarketMicrostructureAnalyzer(vpin, obi, logger)
	analyzer.SetAggressivenessLevel(3) // Medium aggressiveness

	spreadCalc.SetVPINMonitor(vpin)
	spreadCalc.SetOBIDetector(obi)
	spreadCalc.SetMicrostructureAnalyzer(analyzer)

	// Test 1: Excellent conditions (low VPIN + balanced OBI)
	vpin.UpdateVolume(50, 50)              // Balanced
	obi.UpdateOrderBook(1000, 1000, 50000) // Balanced

	buySpread, sellSpread := spreadCalc.GetSpreadForSymbol("BTCUSD1")
	// Should be tightened for aggressive mode
	assert.Less(t, buySpread, 2.0, "Buy spread should tighten for excellent conditions")
	assert.Less(t, sellSpread, 2.0, "Sell spread should tighten for excellent conditions")

	// Test 2: Dangerous conditions (high VPIN + extreme OBI)
	vpin.UpdateVolume(150, 20)            // Toxic flow
	obi.UpdateOrderBook(2000, 200, 50000) // Extreme buy imbalance

	buySpread, sellSpread = spreadCalc.GetSpreadForSymbol("BTCUSD1")
	// Should be widened significantly
	assert.GreaterOrEqual(t, buySpread, 5.0, "Buy spread should widen significantly for dangerous conditions")
	assert.GreaterOrEqual(t, sellSpread, 5.0, "Sell spread should widen significantly for dangerous conditions")
}

func TestSpreadCalculator_VPINAwareSpread(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		DefaultSpreadBps: 2.0,
	}

	positionMgr := &MockInventoryManager{}
	spreadCalc := NewSpreadCalculator(config, positionMgr)

	// Create VPIN monitor
	vpin := volume_optimization.NewVPINMonitor(volume_optimization.VPINConfig{
		WindowSize:        10,
		BucketSize:        100,
		VPINThreshold:     0.6,
		SustainedBreaches: 3,
		AutoResumeDelay:   30 * time.Second,
	}, logger)
	spreadCalc.SetVPINMonitor(vpin)

	// Test healthy flow
	vpin.UpdateVolume(50, 50)
	_, _, vpinValue, isToxic := spreadCalc.GetVPINAwareSpread("BTCUSD1")
	assert.LessOrEqual(t, vpinValue, 0.5, "VPIN should be <= 0.5 for healthy flow")
	assert.False(t, isToxic, "Should not be toxic for healthy flow")

	// Test toxic flow
	vpin.UpdateVolume(150, 20)
	_, _, vpinValue, isToxic = spreadCalc.GetVPINAwareSpread("BTCUSD1")
	assert.Greater(t, vpinValue, 0.7, "VPIN should be > 0.7 for toxic flow")
	assert.True(t, isToxic, "Should be toxic for high VPIN")
}

func TestSpreadCalculator_OBIAdjustedSpread(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		DefaultSpreadBps: 2.0,
	}

	positionMgr := &MockInventoryManager{}
	spreadCalc := NewSpreadCalculator(config, positionMgr)

	// Create OBI detector
	obi := volume_optimization.NewOrderBookImbalanceDetector(10, 0.5, logger)
	spreadCalc.SetOBIDetector(obi)

	// Test balanced orderbook
	obi.UpdateOrderBook(1000, 1000, 50000)
	_, _, obiSignal := spreadCalc.GetOBIAdjustedSpread("BTCUSD1")
	assert.Equal(t, obiSignal.BiasDirection, "NEUTRAL", "Should detect neutral bias")
	assert.Equal(t, obiSignal.Strength, "WEAK", "Should detect weak strength")

	// Test strong buy imbalance
	obi.UpdateOrderBook(2000, 500, 50000)
	_, _, obiSignal = spreadCalc.GetOBIAdjustedSpread("BTCUSD1")
	assert.Equal(t, obiSignal.BiasDirection, "BUY", "Should detect buy bias")
	assert.Equal(t, obiSignal.Strength, "STRONG", "Should detect strong strength")
}

// MockInventoryManager for testing
type MockInventoryManager struct{}

func (m *MockInventoryManager) GetPosition(symbol string) *PositionState {
	return nil
}

func (m *MockInventoryManager) UpdatePosition(symbol string, amount, price, markPrice float64) {
	// Mock implementation
}

func (m *MockInventoryManager) CalculateTargetSpread(symbol string, baseSpreadBps float64) (float64, float64) {
	return 2.0, 2.0 // Mock implementation
}

func (m *MockInventoryManager) GetNetExposure() float64 {
	return 0.0 // Mock implementation
}

func (m *MockInventoryManager) ShouldRebalance(symbol string) bool {
	return false // Mock implementation
}
