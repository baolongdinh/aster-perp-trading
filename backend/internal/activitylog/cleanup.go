package activitylog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// CleanupJob manages periodic cleanup of old activity log entries.
type CleanupJob struct {
	storage     Storage
	fileStore   *FileStore
	config      Config
	log         *zap.Logger
	ticker      *time.Ticker
	stopCh      chan struct{}
	wg          sync.WaitGroup
	running     bool
	mu          sync.RWMutex
}

// NewCleanupJob creates a new cleanup job.
func NewCleanupJob(storage Storage, fileStore *FileStore, config Config, log *zap.Logger) *CleanupJob {
	return &CleanupJob{
		storage:   storage,
		fileStore: fileStore,
		config:    config,
		log:       log,
		stopCh:    make(chan struct{}),
	}
}

// Start starts the cleanup job with periodic runs.
func (c *CleanupJob) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("cleanup job already running")
	}

	c.running = true
	c.ticker = time.NewTicker(1 * time.Hour) // Run every hour

	c.wg.Add(1)
	go c.run(ctx)

	c.log.Info("Cleanup job started",
		zap.Int("retention_days", c.config.RetentionDays),
	)
	return nil
}

// Stop stops the cleanup job.
func (c *CleanupJob) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return
	}

	c.running = false
	c.ticker.Stop()
	close(c.stopCh)

	c.wg.Wait()
	c.log.Info("Cleanup job stopped")
}

// run is the main cleanup loop.
func (c *CleanupJob) run(ctx context.Context) {
	defer c.wg.Done()

	// Run initial cleanup on start
	c.doCleanup(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-c.ticker.C:
			c.doCleanup(ctx)
		}
	}
}

// doCleanup performs the actual cleanup operation.
func (c *CleanupJob) doCleanup(ctx context.Context) {
	retention := time.Duration(c.config.RetentionDays) * 24 * time.Hour
	cutoff := time.Now().Add(-retention)

	c.log.Info("Starting cleanup",
		zap.Time("cutoff", cutoff),
		zap.Int("retention_days", c.config.RetentionDays),
	)

	// Cleanup SQLite storage
	sqliteStart := time.Now()
	if c.storage != nil {
		sqliteCtx, sqliteCancel := context.WithTimeout(ctx, 5*time.Minute)
		defer sqliteCancel()

		if err := c.storage.Cleanup(sqliteCtx, cutoff); err != nil {
			c.log.Error("SQLite cleanup failed",
				zap.Error(err),
				zap.Duration("duration", time.Since(sqliteStart)),
			)
		} else {
			c.log.Info("SQLite cleanup completed",
				zap.Duration("duration", time.Since(sqliteStart)),
			)
		}
	}

	// Cleanup file storage
	fileStart := time.Now()
	if c.fileStore != nil {
		if err := c.fileStore.Cleanup(retention); err != nil {
			c.log.Error("File cleanup failed",
				zap.Error(err),
				zap.Duration("duration", time.Since(fileStart)),
			)
		} else {
			c.log.Info("File cleanup completed",
				zap.Duration("duration", time.Since(fileStart)),
			)
		}
	}

	c.log.Info("Cleanup cycle completed")
}

// RunOnce runs cleanup once immediately.
func (c *CleanupJob) RunOnce(ctx context.Context) error {
	retention := time.Duration(c.config.RetentionDays) * 24 * time.Hour
	cutoff := time.Now().Add(-retention)

	// Cleanup SQLite
	if c.storage != nil {
		sqliteCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		if err := c.storage.Cleanup(sqliteCtx, cutoff); err != nil {
			return fmt.Errorf("sqlite cleanup failed: %w", err)
		}
	}

	// Cleanup file storage
	if c.fileStore != nil {
		if err := c.fileStore.Cleanup(retention); err != nil {
			return fmt.Errorf("file cleanup failed: %w", err)
		}
	}

	return nil
}

// IsRunning returns whether the cleanup job is running.
func (c *CleanupJob) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
