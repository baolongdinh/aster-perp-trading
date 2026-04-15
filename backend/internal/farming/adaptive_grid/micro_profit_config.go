package adaptive_grid

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// MicroProfitConfig holds configuration for the micro profit feature
type MicroProfitConfig struct {
	Enabled        bool                      `yaml:"enabled"`
	SpreadPct      float64                   `yaml:"spread_pct"`
	TimeoutSeconds int                       `yaml:"timeout_seconds"`
	MinProfitUSDT  float64                   `yaml:"min_profit_usdt"`
	Symbols        map[string]SymbolOverride `yaml:"symbols,omitempty"`
}

// SymbolOverride allows per-symbol configuration overrides
type SymbolOverride struct {
	Enabled        *float64 `yaml:"enabled,omitempty"`
	SpreadPct      *float64 `yaml:"spread_pct,omitempty"`
	TimeoutSeconds *int     `yaml:"timeout_seconds,omitempty"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *MicroProfitConfig {
	return &MicroProfitConfig{
		Enabled:        false,
		SpreadPct:      0.005,
		TimeoutSeconds: 30,
		MinProfitUSDT:  0.01,
		Symbols:        make(map[string]SymbolOverride),
	}
}

// Validate checks if the configuration values are within acceptable ranges
func (c *MicroProfitConfig) Validate() error {
	// Validate spread_pct
	if c.SpreadPct < 0.001 || c.SpreadPct > 0.01 {
		return fmt.Errorf("spread_pct must be between 0.001 and 0.01, got %f", c.SpreadPct)
	}

	// Validate timeout_seconds
	if c.TimeoutSeconds < 10 || c.TimeoutSeconds > 300 {
		return fmt.Errorf("timeout_seconds must be between 10 and 300, got %d", c.TimeoutSeconds)
	}

	// Validate min_profit_usdt
	if c.MinProfitUSDT < 0.001 || c.MinProfitUSDT > 1.0 {
		return fmt.Errorf("min_profit_usdt must be between 0.001 and 1.0, got %f", c.MinProfitUSDT)
	}

	// Validate symbol overrides
	for symbol, override := range c.Symbols {
		if override.SpreadPct != nil {
			if *override.SpreadPct < 0.001 || *override.SpreadPct > 0.01 {
				return fmt.Errorf("symbol %s: spread_pct must be between 0.001 and 0.01, got %f", symbol, *override.SpreadPct)
			}
		}
		if override.TimeoutSeconds != nil {
			if *override.TimeoutSeconds < 10 || *override.TimeoutSeconds > 300 {
				return fmt.Errorf("symbol %s: timeout_seconds must be between 10 and 300, got %d", symbol, *override.TimeoutSeconds)
			}
		}
	}

	return nil
}

// GetSymbolConfig returns the configuration for a specific symbol, applying overrides if present
func (c *MicroProfitConfig) GetSymbolConfig(symbol string) *MicroProfitConfig {
	config := &MicroProfitConfig{
		Enabled:        c.Enabled,
		SpreadPct:      c.SpreadPct,
		TimeoutSeconds: c.TimeoutSeconds,
		MinProfitUSDT:  c.MinProfitUSDT,
	}

	if override, exists := c.Symbols[symbol]; exists {
		if override.Enabled != nil {
			config.Enabled = *override.Enabled == 1.0
		}
		if override.SpreadPct != nil {
			config.SpreadPct = *override.SpreadPct
		}
		if override.TimeoutSeconds != nil {
			config.TimeoutSeconds = *override.TimeoutSeconds
		}
	}

	return config
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(path string) (*MicroProfitConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return default config
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := DefaultConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

// ConfigWatcher watches for configuration file changes and triggers reloads
type ConfigWatcher struct {
	path     string
	config   *MicroProfitConfig
	logger   *zap.Logger
	watcher  *fsnotify.Watcher
	mu       sync.RWMutex
	callback func(*MicroProfitConfig) error
	stopCh   chan struct{}
}

// NewConfigWatcher creates a new configuration watcher
func NewConfigWatcher(path string, logger *zap.Logger, callback func(*MicroProfitConfig) error) (*ConfigWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	config, err := LoadFromFile(path)
	if err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to load initial config: %w", err)
	}

	cw := &ConfigWatcher{
		path:     path,
		config:   config,
		logger:   logger,
		watcher:  watcher,
		callback: callback,
		stopCh:   make(chan struct{}),
	}

	// Watch the directory containing the config file
	dir := filepath.Dir(path)
	if err := cw.watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch directory: %w", err)
	}

	logger.Info("Config watcher initialized",
		zap.String("path", path),
		zap.String("directory", dir))

	return cw, nil
}

// Start begins watching for configuration changes
func (cw *ConfigWatcher) Start() {
	go cw.watch()
	cw.logger.Info("Config watcher started")
}

// Stop stops watching for configuration changes
func (cw *ConfigWatcher) Stop() {
	close(cw.stopCh)
	cw.watcher.Close()
	cw.logger.Info("Config watcher stopped")
}

// watch monitors the configuration file for changes
func (cw *ConfigWatcher) watch() {
	for {
		select {
		case <-cw.stopCh:
			return
		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}

			// Only process write events for the config file
			if event.Op&fsnotify.Write == fsnotify.Write {
				if filepath.Clean(event.Name) == filepath.Clean(cw.path) {
					cw.logger.Info("Config file changed, reloading",
						zap.String("path", event.Name))
					cw.reload()
				}
			}

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			cw.logger.Error("Config watcher error",
				zap.Error(err))
		}
	}
}

// reload reloads the configuration from file
func (cw *ConfigWatcher) reload() {
	config, err := LoadFromFile(cw.path)
	if err != nil {
		cw.logger.Error("Failed to reload config",
			zap.Error(err))
		return
	}

	cw.mu.Lock()
	cw.config = config
	cw.mu.Unlock()

	cw.logger.Info("Config reloaded successfully",
		zap.Bool("enabled", config.Enabled),
		zap.Float64("spread_pct", config.SpreadPct),
		zap.Int("timeout_seconds", config.TimeoutSeconds))

	// Call callback if provided
	if cw.callback != nil {
		if err := cw.callback(config); err != nil {
			cw.logger.Error("Config reload callback failed",
				zap.Error(err))
		}
	}
}

// GetConfig returns the current configuration
func (cw *ConfigWatcher) GetConfig() *MicroProfitConfig {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.config
}
