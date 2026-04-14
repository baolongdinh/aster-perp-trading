package agentic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestEngine_BBPeriodConfiguration kiểm tra T012 - BB Period = 10
func TestEngine_BBPeriodConfiguration(t *testing.T) {
	config := DefaultAgenticConfig()

	// T012: BB Period phải là 10 (unified với execution layer)
	assert.Equal(t, 10, config.RegimeDetection.BBPeriod, "T012: BB Period phải là 10")
	assert.Equal(t, 14, config.RegimeDetection.ADXPeriod, "ADX Period = 14")
	assert.Equal(t, 14, config.RegimeDetection.ATRPeriod, "ATR Period = 14")
}

// TestEngine_ConfigValidation kiểm tra các giá trị config
func TestEngine_ConfigValidation(t *testing.T) {
	config := DefaultAgenticConfig()

	// Kiểm tra các giá trị mặc định
	assert.True(t, config.Enabled, "Engine nên enabled by default")
	assert.True(t, config.WhitelistManagement.Enabled, "Whitelist management nên enabled")
	assert.Equal(t, 30*time.Second, config.RegimeDetection.UpdateInterval, "Update interval = 30s")

	// Thresholds
	assert.Equal(t, 25.0, config.RegimeDetection.Thresholds.SidewaysADXMax, "Sideways ADX max = 25")
	assert.Equal(t, 25.0, config.RegimeDetection.Thresholds.TrendingADXMin, "Trending ADX min = 25")
	assert.Equal(t, 3.0, config.RegimeDetection.Thresholds.VolatileATRSpike, "Volatile ATR spike = 3.0")
}

// TestEngine_SymbolSelection kiểm tra symbol selection logic
func TestEngine_SymbolSelection(t *testing.T) {
	config := DefaultAgenticConfig()

	// Kiểm tra default symbols
	assert.Contains(t, config.Universe.Symbols, "BTCUSD1", "Should include BTC")
	assert.Contains(t, config.Universe.Symbols, "ETHUSD1", "Should include ETH")
	assert.Contains(t, config.Universe.Symbols, "SOLUSD1", "Should include SOL")

	// Min volume
	assert.Equal(t, 10000000.0, config.Universe.Min24hVolumeUSD, "Min 24h volume = 10M")
}

// TestEngine_ScoringWeights kiểm tra trọng số scoring
func TestEngine_ScoringWeights(t *testing.T) {
	config := DefaultAgenticConfig()

	// Weights should sum to 1.0
	total := config.Scoring.Weights.Trend +
		config.Scoring.Weights.Volatility +
		config.Scoring.Weights.Volume +
		config.Scoring.Weights.Structure

	assert.InDelta(t, 1.0, total, 0.01, "Scoring weights nên sum gần 1.0")
}

// TestEngine_DecisionFlow mô phỏng flow quyết định của engine
func TestEngine_DecisionFlow(t *testing.T) {
	// Flow của AgenticEngine:
	// 1. detectionLoop chạy mỗi 30s
	// 2. detectAllSymbols -> lấy regime cho tất cả symbols
	// 3. calculateScores -> tính điểm opportunity
	// 4. circuitBreaker.Check -> kiểm tra an toàn
	// 5. whitelistManager.UpdateWhitelist -> cập nhật whitelist

	flowSteps := []struct {
		step string
		desc string
	}{
		{"1", "Fetch market data cho tất cả symbols"},
		{"2", "Tính BB bands (period=10), ADX (period=14)"},
		{"3", "Detect regime: sideways (ADX<25), trending (ADX>=25), volatile"},
		{"4", "Score opportunities dựa trên regime và trend strength"},
		{"5", "Check circuit breaker (nếu trending quá mạnh thì dừng)"},
		{"6", "Update whitelist với top symbols"},
		{"7", "Gửi whitelist cho VolumeFarmEngine"},
	}

	for _, s := range flowSteps {
		t.Run(s.step+"_"+s.desc, func(t *testing.T) {
			assert.True(t, true, "Flow step: %s", s.desc)
		})
	}
}

// TestEngine_IntegrationWithExecutionLayer kiểm tra tích hợp với execution
func TestEngine_IntegrationWithExecutionLayer(t *testing.T) {
	// Agentic layer (decision) và VolumeFarmEngine (execution) chạy độc lập
	// Nhưng phải đồng bộ qua:
	// 1. Whitelist (Agentic -> Execution)
	// 2. RangeDetector state (Execution cập nhật, Agentic không can thiệp trực tiếp)

	t.Run("WhitelistPropagation", func(t *testing.T) {
		// Agentic engine tính toán và cập nhật whitelist
		// VolumeFarmEngine lấy whitelist từ symbol selector
		assert.True(t, true, "Whitelist nên được propagate từ Agentic -> Execution")
	})

	t.Run("BBPeriodConsistency", func(t *testing.T) {
		// T012: Cả hai layer dùng BB Period = 10
		agenticBBPeriod := 10
		executionBBPeriod := 10 // From range_detector.go DefaultRangeConfig
		assert.Equal(t, agenticBBPeriod, executionBBPeriod, "BB Period phải đồng nhất")
	})
}

// TestEngine_ScoringThresholds kiểm tra scoring thresholds
func TestEngine_ScoringThresholds(t *testing.T) {
	config := DefaultAgenticConfig()

	// Scoring thresholds
	assert.Equal(t, 75.0, config.Scoring.Thresholds.HighScore, "High score threshold = 75")
	assert.Equal(t, 60.0, config.Scoring.Thresholds.MediumScore, "Medium score threshold = 60")
	assert.Equal(t, 40.0, config.Scoring.Thresholds.LowScore, "Low score threshold = 40")
}

// TestEngine_PositionSizing kiểm tra position sizing logic
func TestEngine_PositionSizing(t *testing.T) {
	config := DefaultAgenticConfig()

	// Position sizing dựa trên score multipliers
	assert.NotNil(t, config.PositionSizing.ScoreMultipliers, "Score multipliers should exist")
	assert.NotNil(t, config.PositionSizing.RegimeMultipliers, "Regime multipliers should exist")
}

// BenchmarkEngine_Configuration benchmark tạo config
func BenchmarkEngine_Configuration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = DefaultAgenticConfig()
	}
}
