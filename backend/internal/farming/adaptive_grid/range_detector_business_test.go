package adaptive_grid

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func newBusinessRangeDetector() *RangeDetector {
	cfg := DefaultRangeConfig()
	cfg.Periods = 3
	cfg.ADXPeriod = 3
	cfg.StabilizationPeriod = 0
	cfg.EntryConfirmations = 2
	cfg.ReentryConfirmations = 3
	cfg.OutsideBandConfirmations = 2
	cfg.BBExpansionFactor = 1.5
	cfg.MinRangeWidthPct = 0.0001
	return NewRangeDetector(cfg, zap.NewNop())
}

func TestRangeDetector_InitialEntryRequiresTwoConfirmations(t *testing.T) {
	detector := newBusinessRangeDetector()
	detector.SetADXFilter(true, 20)
	detector.UpdateADX(12)
	detector.UpdateADX(11)
	detector.UpdateADX(10)
	detector.mu.Lock()
	detector.state = RangeStateEstablishing
	detector.currentRange = &RangeData{
		UpperBound: 101,
		LowerBound: 99,
		MidPrice:   100,
		WidthPct:   0.02,
	}
	detector.lastPrice = 100
	detector.mu.Unlock()

	detector.checkStateTransition()
	assert.NotEqual(t, RangeStateActive, detector.GetState(), "first valid range should not activate immediately")

	detector.checkStateTransition()

	assert.Equal(t, RangeStateActive, detector.GetState(), "second confirmation should activate range")
	assert.True(t, detector.ShouldTrade(), "detector should allow trading after confirmations")
}

func TestRangeDetector_BreakoutAfterTwoClosesOutsideBand(t *testing.T) {
	detector := newBusinessRangeDetector()
	detector.mu.Lock()
	detector.state = RangeStateActive
	detector.currentRange = &RangeData{
		UpperBound: 101,
		LowerBound: 99,
		MidPrice:   100,
		WidthPct:   0.02,
	}
	detector.mu.Unlock()

	detector.mu.Lock()
	detector.lastPrice = 101.0005
	detector.mu.Unlock()
	detector.checkStateTransition()
	assert.NotEqual(t, RangeStateBreakout, detector.GetState(), "single close outside band should not breakout immediately")

	detector.mu.Lock()
	detector.lastPrice = 101.0008
	detector.mu.Unlock()
	detector.checkStateTransition()
	assert.Equal(t, RangeStateBreakout, detector.GetState(), "two consecutive closes outside band should breakout")
	assert.True(t, detector.IsBreakout(), "breakout state should be visible")
}

func TestRangeDetector_ShouldExitForVolatilityExpansion(t *testing.T) {
	detector := newBusinessRangeDetector()
	detector.SetADXFilter(true, 20)
	detector.UpdateADX(10)
	detector.UpdateADX(10)
	detector.UpdateADX(10)

	// Inflate the recent width far above history without needing a full OHLC simulation.
	detector.mu.Lock()
	detector.state = RangeStateActive
	detector.currentRange = &RangeData{
		UpperBound: 101.5,
		LowerBound: 98.5,
		MidPrice:   100,
		WidthPct:   0.03,
	}
	detector.currentRange.WidthPct = 0.03
	detector.widthHistory = []float64{0.005, 0.006, 0.0055, 0.03}
	detector.mu.Unlock()

	assert.True(t, detector.ShouldExitForTrend(), "large BB width expansion should trigger exit")
}

func TestRangeDetector_ReentryRequiresMaterialShiftAndThreeConfirmations(t *testing.T) {
	detector := newBusinessRangeDetector()
	detector.SetADXFilter(true, 20)
	detector.UpdateADX(10)
	detector.UpdateADX(10)
	detector.UpdateADX(10)

	detector.mu.Lock()
	detector.lastAcceptedRange = &RangeData{
		UpperBound: 101,
		LowerBound: 99,
		MidPrice:   100,
		WidthPct:   0.02,
	}
	detector.currentRange = &RangeData{
		UpperBound: 101.8,
		LowerBound: 99.2,
		MidPrice:   100.5,
		WidthPct:   0.026,
	}
	detector.state = RangeStateBreakout
	detector.breakoutTime = time.Now().Add(-time.Second)
	detector.lastPrice = 100.5
	detector.mu.Unlock()

	detector.checkStateTransition()
	assert.Equal(t, RangeStateStabilizing, detector.GetState())

	detector.checkStateTransition()
	assert.Equal(t, RangeStateStabilizing, detector.GetState())

	detector.checkStateTransition()
	assert.Equal(t, RangeStateStabilizing, detector.GetState(), "two confirmations should still wait")

	detector.checkStateTransition()
	assert.Equal(t, RangeStateActive, detector.GetState(), "third confirmation should reactivate trading")
}
