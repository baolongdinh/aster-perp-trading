package volume_optimization

import (
	"math"
	"testing"

	"go.uber.org/zap"
)

func TestSetAndGetTickSize(t *testing.T) {
	logger := zap.NewNop()
	tsm := NewTickSizeManager(logger)

	tsm.SetTickSize("BTC", 0.1)
	tickSize := tsm.GetTickSize("BTC")

	if tickSize != 0.1 {
		t.Errorf("Expected tick-size 0.1, got %v", tickSize)
	}
}

func TestGetTickSizeDefault(t *testing.T) {
	logger := zap.NewNop()
	tsm := NewTickSizeManager(logger)

	tickSize := tsm.GetTickSize("UNKNOWN")

	if tickSize != 0.01 {
		t.Errorf("Expected default tick-size 0.01, got %v", tickSize)
	}
}

func TestRoundToTick(t *testing.T) {
	logger := zap.NewNop()
	tsm := NewTickSizeManager(logger)

	tests := []struct {
		name     string
		price    float64
		tickSize float64
		expected float64
	}{
		{
			name:     "Round to 0.1",
			price:    50000.123,
			tickSize: 0.1,
			expected: 50000.1,
		},
		{
			name:     "Round to 0.01",
			price:    3000.12345,
			tickSize: 0.01,
			expected: 3000.12,
		},
		{
			name:     "Round to 0.001",
			price:    150.123456,
			tickSize: 0.001,
			expected: 150.123,
		},
		{
			name:     "Zero tick-size",
			price:    50000.123,
			tickSize: 0,
			expected: 50000.123,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tsm.RoundToTick(tt.price, tt.tickSize)
			// Allow small floating point tolerance
			if math.Abs(result-tt.expected) > 1e-9 {
				t.Errorf("RoundToTick() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRoundToTickForSymbol(t *testing.T) {
	logger := zap.NewNop()
	tsm := NewTickSizeManager(logger)

	tsm.SetTickSize("BTC", 0.1)
	price := 50000.123
	result := tsm.RoundToTickForSymbol("BTC", price)

	// Allow small floating point tolerance
	if math.Abs(result-50000.1) > 1e-9 {
		t.Errorf("Expected 50000.1, got %v", result)
	}
}

func TestRefreshTickSizes(t *testing.T) {
	logger := zap.NewNop()
	tsm := NewTickSizeManager(logger)

	err := tsm.RefreshTickSizes()
	if err != nil {
		t.Errorf("RefreshTickSizes() returned error: %v", err)
	}

	// Verify common tick-sizes are set
	if tsm.GetTickSize("BTCUSD1") != 0.1 {
		t.Error("BTCUSD1 tick-size not set correctly")
	}
	if tsm.GetTickSize("ETHUSD1") != 0.01 {
		t.Error("ETHUSD1 tick-size not set correctly")
	}
}

func TestGetAllTickSizes(t *testing.T) {
	logger := zap.NewNop()
	tsm := NewTickSizeManager(logger)

	tsm.SetTickSize("BTC", 0.1)
	tsm.SetTickSize("ETH", 0.01)

	allTickSizes := tsm.GetAllTickSizes()

	if len(allTickSizes) != 2 {
		t.Errorf("Expected 2 tick-sizes, got %d", len(allTickSizes))
	}

	if allTickSizes["BTC"] != 0.1 {
		t.Errorf("Expected BTC tick-size 0.1, got %v", allTickSizes["BTC"])
	}
	if allTickSizes["ETH"] != 0.01 {
		t.Errorf("Expected ETH tick-size 0.01, got %v", allTickSizes["ETH"])
	}
}

func TestConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	tsm := NewTickSizeManager(logger)

	// Test concurrent access
	done := make(chan bool)

	// Goroutine 1: Read
	go func() {
		for i := 0; i < 100; i++ {
			tsm.GetTickSize("BTC")
		}
		done <- true
	}()

	// Goroutine 2: Write
	go func() {
		for i := 0; i < 100; i++ {
			tsm.SetTickSize("BTC", 0.1)
		}
		done <- true
	}()

	<-done
	<-done

	// If we get here without panic, concurrent access works
}
