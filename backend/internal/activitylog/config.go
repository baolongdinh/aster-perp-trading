package activitylog

import (
	"time"
)

// Config holds configuration for the ActivityLogger.
type Config struct {
	// Buffer settings
	BufferSize    int           // Ring buffer capacity (default: 1000)
	FlushInterval time.Duration // Background flush interval (default: 100ms)

	// Storage settings
	DBPath        string // SQLite database path (default: "./data/activity.db")
	FilePath      string // JSON file path (default: "./data/activity/current")
	MaxFileSize   int64  // Max file size before rotation (default: 100MB)
	RetentionDays int    // Days to retain logs (default: 90)

	// Performance settings
	BatchSize int // SQLite batch insert size (default: 100)
	Workers   int // Background worker count (default: 2)

	// Instance identification
	BotInstance string // Bot instance identifier (default: "default")

	// Filtering
	ExcludeEvents []string // Event types to exclude from logging
	MinSeverity   Severity // Minimum severity to log (default: INFO)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		BufferSize:    1000,
		FlushInterval: 100 * time.Millisecond,
		DBPath:        "./data/activity.db",
		FilePath:      "./data/activity/current",
		MaxFileSize:   100 * 1024 * 1024, // 100MB
		RetentionDays: 90,
		BatchSize:     100,
		Workers:       2,
		BotInstance:   "default",
		MinSeverity:   SeverityInfo,
	}
}

// Validate checks if the configuration is valid and applies defaults.
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		c.BufferSize = 1000
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = 100 * time.Millisecond
	}
	if c.DBPath == "" {
		c.DBPath = "./data/activity.db"
	}
	if c.FilePath == "" {
		c.FilePath = "./data/activity/current"
	}
	if c.MaxFileSize <= 0 {
		c.MaxFileSize = 100 * 1024 * 1024
	}
	if c.RetentionDays <= 0 {
		c.RetentionDays = 90
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.Workers <= 0 {
		c.Workers = 2
	}
	if c.BotInstance == "" {
		c.BotInstance = "default"
	}
	if c.MinSeverity == "" {
		c.MinSeverity = SeverityInfo
	}
	return nil
}

// ShouldLogEvent checks if an event type should be logged based on exclude list.
func (c *Config) ShouldLogEvent(eventType EventType) bool {
	for _, excluded := range c.ExcludeEvents {
		if string(eventType) == excluded {
			return false
		}
	}
	return true
}

// ShouldLogSeverity checks if a severity level should be logged based on min severity.
func (c *Config) ShouldLogSeverity(severity Severity) bool {
	severityOrder := map[Severity]int{
		SeverityInfo:     0,
		SeverityWarn:     1,
		SeverityError:    2,
		SeverityCritical: 3,
	}
	return severityOrder[severity] >= severityOrder[c.MinSeverity]
}
