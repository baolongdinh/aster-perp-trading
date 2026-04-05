package adaptive_config

import (
	"sync"
	"time"

	"aster-bot/internal/config"

	"go.uber.org/zap"
)

// AdaptiveConfigManager manages adaptive configuration with hot-reload capability
type AdaptiveConfigManager struct {
	config     *config.AdaptiveConfig
	configPath string
	logger     *zap.Logger
	mu         sync.RWMutex
	lastReload time.Time
}

// NewAdaptiveConfigManager creates a new adaptive config manager
func NewAdaptiveConfigManager(configPath string, logger *zap.Logger) *AdaptiveConfigManager {
	return &AdaptiveConfigManager{
		configPath: configPath,
		logger:     logger,
		mu:         sync.RWMutex{},
	}
}

// LoadConfig loads adaptive configuration from file
func (m *AdaptiveConfigManager) LoadConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	config, err := config.LoadAdaptiveConfig(m.configPath)
	if err != nil {
		m.logger.Error("Failed to load adaptive config", zap.Error(err))
		return err
	}

	m.config = config
	m.lastReload = time.Now()

	m.logger.Info("Adaptive config loaded successfully",
		zap.String("config_path", m.configPath),
		zap.Bool("enabled", m.config.Enabled),
		zap.String("detection_method", m.config.Detection.Method))

	return nil
}

// GetConfig returns current adaptive configuration (thread-safe)
func (m *AdaptiveConfigManager) GetConfig() *config.AdaptiveConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// ReloadConfig reloads configuration from file
func (m *AdaptiveConfigManager) ReloadConfig() error {
	m.logger.Info("Reloading adaptive configuration")
	return m.LoadConfig()
}

// GetRegimeConfig returns configuration for specific regime
func (m *AdaptiveConfigManager) GetRegimeConfig(regime string) (*config.RegimeConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.config.Enabled {
		return nil, false
	}

	regimeConfig, exists := m.config.Regimes[regime]
	if !exists {
		m.logger.Warn("Regime config not found", zap.String("regime", regime))
		return nil, false
	}

	return &regimeConfig, true
}

// IsEnabled returns whether adaptive configuration is enabled
func (m *AdaptiveConfigManager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config != nil && m.config.Enabled
}

// GetLastReload returns the time of last configuration reload
func (m *AdaptiveConfigManager) GetLastReload() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastReload
}
