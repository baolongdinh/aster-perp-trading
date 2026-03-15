package indicators

import (
	"math"
	"testing"
)

func TestSMA(t *testing.T) {
	data := []float64{1, 2, 3, 4, 5}
	sma := SMA(data, 3)
	if sma != 4.0 {
		t.Errorf("Expected 4.0, got %f", sma)
	}
}

func TestEMAState(t *testing.T) {
	ema := NewEMAState(3)
	vals := []float64{2, 4, 6, 8, 10}

	for _, v := range vals {
		ema.Add(v)
	}

	result := ema.Value()
	expected := 7.5 // approximate depends on seed

	if math.Abs(result-expected) > 0.5 {
		t.Errorf("Expected near %f, got %f", expected, result)
	}
}

func TestRSIState(t *testing.T) {
	rsi := NewRSIState(14)
	
	// Uptrending data
	for i := 1.0; i <= 20.0; i++ {
		rsi.Add(i * 10)
	}

	val := rsi.Value()
	if val < 50.0 {
		t.Errorf("Expected RSI > 50 in heavy uptrend, got %f", val)
	}
}
