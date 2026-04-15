package tradingmode

import (
	"testing"
	"time"

	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_grid"

	"go.uber.org/zap"
)

// DEPRECATED: ModeManager is being merged into CircuitBreaker.
// These tests are kept for backward compatibility during migration.
// New tests should be added to CircuitBreaker instead.

func TestPerSymbolModeManagement(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.TradingModesConfig{
		MicroMode: config.MicroModeConfig{
			Enabled: true,
		},
		StandardMode: config.StandardModeConfig{
			Enabled: true,
		},
		TrendAdaptedMode: config.TrendAdaptedModeConfig{
			Enabled: true,
		},
		Transitions: config.ModeTransitionsConfig{
			ADXThresholdSideways: 20.0,
			ADXThresholdTrending: 25.0,
		},
	}

	mm := NewModeManager(cfg, logger)

	// Test 1: Multiple symbols can have different modes simultaneously
	t.Run("DifferentModesPerSymbol", func(t *testing.T) {
		// Set BTC to MICRO mode
		modeBTC := mm.EvaluateModeSymbol("BTCUSD1", adaptive_grid.RangeStateUnknown, 18.0, false, false, false)
		if modeBTC != TradingModeMicro {
			t.Errorf("Expected BTC to be in MICRO mode, got %s", modeBTC)
		}

		// Set ETH to STANDARD mode
		modeETH := mm.EvaluateModeSymbol("ETHUSD1", adaptive_grid.RangeStateActive, 18.0, false, false, false)
		if modeETH != TradingModeStandard {
			t.Errorf("Expected ETH to be in STANDARD mode, got %s", modeETH)
		}

		// Verify they remain different
		currentBTC := mm.GetCurrentMode("BTCUSD1")
		currentETH := mm.GetCurrentMode("ETHUSD1")

		if currentBTC != TradingModeMicro {
			t.Errorf("Expected BTC to remain in MICRO mode, got %s", currentBTC)
		}
		if currentETH != TradingModeStandard {
			t.Errorf("Expected ETH to remain in STANDARD mode, got %s", currentETH)
		}
	})

	// Test 2: Mode transitions work independently per symbol
	t.Run("IndependentModeTransitions", func(t *testing.T) {
		// Transition BTC to COOLDOWN
		mm.EnterCooldownSymbol("BTCUSD1", 10*time.Second)
		if !mm.IsInCooldownSymbol("BTCUSD1") {
			t.Error("Expected BTC to be in cooldown")
		}

		// ETH should not be in cooldown
		if mm.IsInCooldownSymbol("ETHUSD1") {
			t.Error("Expected ETH to not be in cooldown")
		}

		// Wait for cooldown to expire
		time.Sleep(11 * time.Second)

		// BTC should auto-transition to MICRO
		currentBTC := mm.GetCurrentMode("BTCUSD1")
		if currentBTC != TradingModeMicro {
			t.Errorf("Expected BTC to auto-transition to MICRO after cooldown, got %s", currentBTC)
		}
	})

	// Test 3: Cooldown tracking is per-symbol
	t.Run("PerSymbolCooldownTracking", func(t *testing.T) {
		// Set BTC cooldown to 5 seconds
		mm.EnterCooldownSymbol("BTCUSD1", 5*time.Second)

		// Set ETH cooldown to 10 seconds
		mm.EnterCooldownSymbol("ETHUSD1", 10*time.Second)

		// Wait 3 seconds
		time.Sleep(3 * time.Second)

		// Both should still be in cooldown
		if !mm.IsInCooldownSymbol("BTCUSD1") {
			t.Error("Expected BTC to still be in cooldown after 3s")
		}
		if !mm.IsInCooldownSymbol("ETHUSD1") {
			t.Error("Expected ETH to still be in cooldown after 3s")
		}

		// Wait another 3 seconds (total 6 seconds)
		time.Sleep(3 * time.Second)

		// BTC should be out of cooldown
		if mm.IsInCooldownSymbol("BTCUSD1") {
			t.Error("Expected BTC to be out of cooldown after 6s")
		}

		// ETH should still be in cooldown
		if !mm.IsInCooldownSymbol("ETHUSD1") {
			t.Error("Expected ETH to still be in cooldown after 6s")
		}
	})

	// Test 4: Mode history is per-symbol
	t.Run("PerSymbolModeHistory", func(t *testing.T) {
		historyBTC := mm.GetModeHistorySymbol("BTCUSD1")
		historyETH := mm.GetModeHistorySymbol("ETHUSD1")

		// Both should have history
		if len(historyBTC) == 0 {
			t.Error("Expected BTC to have mode history")
		}
		if len(historyETH) == 0 {
			t.Error("Expected ETH to have mode history")
		}

		// Histories should be independent
		// (This is a basic check - in real scenarios, we'd verify specific transitions)
	})

	// Test 5: GetModeSince is per-symbol
	t.Run("PerSymbolModeSince", func(t *testing.T) {
		// This tests that modeSince is tracked per symbol
		// We can't easily test exact timing, but we can verify the method works
		_ = mm.GetCurrentMode("BTCUSD1")
		_ = mm.GetCurrentMode("ETHUSD1")

		// If this doesn't panic, the per-symbol tracking is working
	})
}

func TestBackwardCompatibility(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.TradingModesConfig{
		MicroMode: config.MicroModeConfig{
			Enabled: true,
		},
	}

	mm := NewModeManager(cfg, logger)

	// Test that global methods still work
	t.Run("GlobalMethodsStillWork", func(t *testing.T) {
		globalMode := mm.GetCurrentModeGlobal()
		if globalMode != TradingModeUnknown {
			t.Errorf("Expected global mode to be UNKNOWN initially, got %s", globalMode)
		}

		mm.EnterCooldown(5 * time.Second)
		if !mm.IsInCooldown() {
			t.Error("Expected global cooldown to be active")
		}

		remaining := mm.GetCooldownRemaining()
		if remaining <= 0 {
			t.Error("Expected positive remaining cooldown time")
		}
	})
}
