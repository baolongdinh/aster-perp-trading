package tradingmode

import (
	"fmt"
	"sync"
	"time"

	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_grid"

	"go.uber.org/zap"
)

// ModeManager manages trading mode transitions and state
type ModeManager struct {
	mu          sync.RWMutex
	currentMode TradingMode
	modeSince   time.Time
	config      *config.TradingModesConfig
	logger      *zap.Logger

	// Mode history for tracking
	modeHistory []ModeTransition

	// Cooldown tracking
	cooldownEnd time.Time
}

// NewModeManager creates a new mode manager
func NewModeManager(cfg *config.TradingModesConfig, logger *zap.Logger) *ModeManager {
	if cfg == nil {
		cfg = &config.TradingModesConfig{}
	}

	return &ModeManager{
		currentMode: TradingModeUnknown,
		modeSince:   time.Now(),
		config:      cfg,
		logger:      logger.With(zap.String("component", "mode_manager")),
		modeHistory: make([]ModeTransition, 0),
	}
}

// GetCurrentMode returns the current trading mode
func (m *ModeManager) GetCurrentMode() TradingMode {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if cooldown has expired
	if m.currentMode == TradingModeCooldown && time.Now().After(m.cooldownEnd) {
		m.mu.RUnlock()
		m.mu.Lock()
		if m.currentMode == TradingModeCooldown && time.Now().After(m.cooldownEnd) {
			m.transitionTo(TradingModeMicro, "cooldown_expired")
		}
		m.mu.Unlock()
		m.mu.RLock()
	}

	return m.currentMode
}

// GetModeSince returns when current mode started
func (m *ModeManager) GetModeSince() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modeSince
}

// GetModeDuration returns duration in current mode
func (m *ModeManager) GetModeDuration() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Since(m.modeSince)
}

// GetModeHistory returns mode transition history
func (m *ModeManager) GetModeHistory() []ModeTransition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	history := make([]ModeTransition, len(m.modeHistory))
	copy(history, m.modeHistory)
	return history
}

// EvaluateMode evaluates market conditions and determines appropriate mode
func (m *ModeManager) EvaluateMode(
	rangeState adaptive_grid.RangeState,
	adx float64,
	isBreakout bool,
	isTrending bool,
	isVolatilitySpike bool,
) TradingMode {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check cooldown first
	if m.currentMode == TradingModeCooldown {
		if time.Now().Before(m.cooldownEnd) {
			return TradingModeCooldown
		}
		// Cooldown expired, will transition to Micro below
	}

	// Determine target mode based on conditions
	targetMode := m.determineTargetMode(rangeState, adx, isBreakout, isTrending, isVolatilitySpike)

	// Check if we should transition
	if m.shouldTransition(targetMode) {
		m.transitionTo(targetMode, m.buildTransitionReason(rangeState, adx, isBreakout, isTrending, isVolatilitySpike))
	}

	return m.currentMode
}

// determineTargetMode determines what mode we should be in
func (m *ModeManager) determineTargetMode(
	rangeState adaptive_grid.RangeState,
	adx float64,
	isBreakout bool,
	isTrending bool,
	isVolatilitySpike bool,
) TradingMode {
	// Get thresholds from config
	sidewaysThreshold := m.config.Transitions.ADXThresholdSideways
	if sidewaysThreshold == 0 {
		sidewaysThreshold = 20.0
	}
	trendingThreshold := m.config.Transitions.ADXThresholdTrending
	if trendingThreshold == 0 {
		trendingThreshold = 25.0
	}

	// Priority 1: Breakout or volatility spike -> Cooldown
	if isBreakout || isVolatilitySpike {
		return TradingModeCooldown
	}

	// Priority 2: Strong trend -> TrendAdapted
	if isTrending || adx > trendingThreshold {
		if m.config.TrendAdaptedMode.Enabled {
			return TradingModeTrendAdapted
		}
		// Fallback to Micro if TrendAdapted disabled
		if m.config.MicroMode.Enabled {
			return TradingModeMicro
		}
	}

	// Priority 3: BB Range Active + Low ADX -> Standard
	if rangeState == adaptive_grid.RangeStateActive && adx < sidewaysThreshold {
		if m.config.StandardMode.Enabled {
			return TradingModeStandard
		}
	}

	// Priority 4: Default to Micro mode (always trade)
	if m.config.MicroMode.Enabled {
		return TradingModeMicro
	}

	// Fallback: Unknown (should not happen if Micro enabled)
	return TradingModeUnknown
}

// shouldTransition checks if we can transition to target mode
func (m *ModeManager) shouldTransition(targetMode TradingMode) bool {
	// Don't transition if already in target mode
	if m.currentMode == targetMode {
		return false
	}

	// Get minimum duration for current mode
	minDuration := m.getMinModeDuration(m.currentMode)

	// Check if we've been in current mode long enough
	if time.Since(m.modeSince) < minDuration {
		return false
	}

	return true
}

// getMinModeDuration returns minimum duration for a mode
func (m *ModeManager) getMinModeDuration(mode TradingMode) time.Duration {
	switch mode {
	case TradingModeMicro:
		sec := m.config.MicroMode.MinModeDurationSec
		if sec == 0 {
			sec = 30
		}
		return time.Duration(sec) * time.Second
	case TradingModeStandard:
		sec := m.config.StandardMode.MinModeDurationSec
		if sec == 0 {
			sec = 60
		}
		return time.Duration(sec) * time.Second
	case TradingModeTrendAdapted:
		sec := m.config.TrendAdaptedMode.MinModeDurationSec
		if sec == 0 {
			sec = 30
		}
		return time.Duration(sec) * time.Second
	case TradingModeCooldown:
		sec := m.config.CooldownMode.DurationSec
		if sec == 0 {
			sec = 60
		}
		return time.Duration(sec) * time.Second
	default:
		return 0
	}
}

// transitionTo performs the mode transition
func (m *ModeManager) transitionTo(newMode TradingMode, reason string) {
	if m.currentMode == newMode {
		return
	}

	oldMode := m.currentMode
	m.currentMode = newMode
	m.modeSince = time.Now()

	// Record transition
	transition := ModeTransition{
		From:      oldMode,
		To:        newMode,
		Timestamp: time.Now(),
		Reason:    reason,
	}
	m.modeHistory = append(m.modeHistory, transition)

	// Limit history size
	if len(m.modeHistory) > 100 {
		m.modeHistory = m.modeHistory[len(m.modeHistory)-100:]
	}

	// Log the transition
	m.logger.Info("Mode transition",
		zap.String("from", oldMode.String()),
		zap.String("to", newMode.String()),
		zap.String("reason", reason),
		zap.Duration("duration_in_prev_mode", time.Since(m.modeSince)),
	)
}

// buildTransitionReason builds a human-readable reason string
func (m *ModeManager) buildTransitionReason(
	rangeState adaptive_grid.RangeState,
	adx float64,
	isBreakout bool,
	isTrending bool,
	isVolatilitySpike bool,
) string {
	switch {
	case isBreakout:
		return "breakout_detected"
	case isVolatilitySpike:
		return "volatility_spike"
	case isTrending:
		return "trend_detected"
	case adx > m.config.Transitions.ADXThresholdTrending:
		return fmt.Sprintf("adx_high_%d", int(adx))
	case rangeState == adaptive_grid.RangeStateActive:
		return "range_active"
	case adx < m.config.Transitions.ADXThresholdSideways:
		return fmt.Sprintf("adx_low_%d", int(adx))
	default:
		return "default_micro"
	}
}

// EnterCooldown manually enters cooldown mode
func (m *ModeManager) EnterCooldown(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if duration == 0 {
		duration = m.getMinModeDuration(TradingModeCooldown)
	}

	m.cooldownEnd = time.Now().Add(duration)
	m.transitionTo(TradingModeCooldown, "manual_cooldown")
}

// IsInCooldown returns true if currently in cooldown
func (m *ModeManager) IsInCooldown() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentMode == TradingModeCooldown && time.Now().Before(m.cooldownEnd)
}

// GetCooldownRemaining returns remaining cooldown duration
func (m *ModeManager) GetCooldownRemaining() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentMode != TradingModeCooldown {
		return 0
	}

	remaining := time.Until(m.cooldownEnd)
	if remaining < 0 {
		return 0
	}
	return remaining
}
