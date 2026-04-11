package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultDecisionLogger implements DecisionLogger interface
type DefaultDecisionLogger struct {
	basePath     string
	retentionDays int
	mu           sync.Mutex
}

// NewDecisionLogger creates a new decision logger
func NewDecisionLogger(config *LoggingConfig) *DefaultDecisionLogger {
	return &DefaultDecisionLogger{
		basePath:      "data/logs/decisions",
		retentionDays: config.RetentionDays,
	}
}

// Log writes a decision to the log file
func (dl *DefaultDecisionLogger) Log(decision TradingDecision) error {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	// Ensure directory exists
	today := time.Now().Format("2006-01-02")
	dirPath := filepath.Join(dl.basePath, today)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file
	logFile := filepath.Join(dirPath, "decisions.jsonl")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// Marshal decision
	data, err := json.Marshal(decision)
	if err != nil {
		return fmt.Errorf("failed to marshal decision: %w", err)
	}

	// Write to file
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write decision: %w", err)
	}

	return nil
}

// Query retrieves decisions based on filters
func (dl *DefaultDecisionLogger) Query(filters LogFilters) ([]TradingDecision, error) {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	var decisions []TradingDecision

	// Determine date range
	startDate := time.Now().AddDate(0, 0, -dl.retentionDays)
	endDate := time.Now()

	if filters.StartTime != nil {
		startDate = *filters.StartTime
	}
	if filters.EndTime != nil {
		endDate = *filters.EndTime
	}

	// Iterate through date directories
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		dirPath := filepath.Join(dl.basePath, d.Format("2006-01-02"))
		logFile := filepath.Join(dirPath, "decisions.jsonl")

		// Read file if exists
		data, err := os.ReadFile(logFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read log file: %w", err)
		}

		// Parse lines
		lines := splitLines(string(data))
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}

			var decision TradingDecision
			if err := json.Unmarshal([]byte(line), &decision); err != nil {
				continue // Skip invalid lines
			}

			// Apply filters
			if matchesFilters(decision, filters) {
				decisions = append(decisions, decision)
			}

			// Check limit
			if filters.Limit > 0 && len(decisions) >= filters.Limit {
				return decisions, nil
			}
		}
	}

	return decisions, nil
}

// matchesFilters checks if a decision matches the query filters
func matchesFilters(decision TradingDecision, filters LogFilters) bool {
	if filters.Regime != nil && decision.RegimeSnapshot.Regime != *filters.Regime {
		return false
	}

	if filters.DecisionType != nil && decision.DecisionType != *filters.DecisionType {
		return false
	}

	return true
}

// splitLines splits a string into lines
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// CleanupOldLogs removes logs older than retention period
func (dl *DefaultDecisionLogger) CleanupOldLogs() error {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	cutoffDate := time.Now().AddDate(0, 0, -dl.retentionDays)

	entries, err := os.ReadDir(dl.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Parse directory name as date
		date, err := time.Parse("2006-01-02", entry.Name())
		if err != nil {
			continue // Skip non-date directories
		}

		if date.Before(cutoffDate) {
			dirPath := filepath.Join(dl.basePath, entry.Name())
			if err := os.RemoveAll(dirPath); err != nil {
				return fmt.Errorf("failed to remove old log directory: %w", err)
			}
		}
	}

	return nil
}
