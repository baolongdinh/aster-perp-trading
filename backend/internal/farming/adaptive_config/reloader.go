package adaptive_config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

// ConfigReloader handles hot-reload of adaptive configuration
type ConfigReloader struct {
	configPath  string
	logger      *zap.Logger
	lastModTime time.Time
}

// NewConfigReloader creates a new config reloader
func NewConfigReloader(configPath string, logger *zap.Logger) *ConfigReloader {
	return &ConfigReloader{
		configPath: configPath,
		logger:     logger,
	}
}

// StartWatching starts file system watcher for config changes
func (r *ConfigReloader) StartWatching(ctx context.Context, reloadCallback func() error) error {
	// Get absolute path
	absPath, err := filepath.Abs(r.configPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s", absPath)
	}

	// Get directory for watching
	watchDir := filepath.Dir(absPath)

	r.logger.Info("Starting config file watcher",
		zap.String("config_path", absPath),
		zap.String("watch_dir", watchDir))

	// TODO: Implement file system watcher
	// This would use fsnotify or similar library for cross-platform file watching
	r.logger.Info("Config hot-reload not implemented yet - manual reload required")

	return nil
}

// TriggerReload manually triggers configuration reload
func (r *ConfigReloader) TriggerReload() error {
	r.logger.Info("Manual config reload triggered")

	// Get file modification time
	info, err := os.Stat(r.configPath)
	if err != nil {
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	// Check if file was modified
	if info.ModTime().After(r.lastModTime) {
		r.lastModTime = info.ModTime()
		r.logger.Info("Config file modified, reload recommended")
		return fmt.Errorf("config file was modified, please reload configuration")
	}

	r.logger.Info("Config file unchanged")
	return nil
}
