package health

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// WorkerFunc is a function that runs a worker
type WorkerFunc func(ctx context.Context) error

// HeartbeatFunc is a function that reports heartbeat
type HeartbeatFunc func()

// WrappedWorker wraps a worker with automatic heartbeat reporting
type WrappedWorker struct {
	name           string
	workerFunc     WorkerFunc
	monitor        *Monitor
	heartbeatFunc  HeartbeatFunc
	interval       time.Duration
	logger         *zap.Logger
}

// NewWrappedWorker creates a new wrapped worker with automatic heartbeat
func NewWrappedWorker(name string, workerFunc WorkerFunc, monitor *Monitor, heartbeatInterval time.Duration, logger *zap.Logger) *WrappedWorker {
	return &WrappedWorker{
		name:          name,
		workerFunc:    workerFunc,
		monitor:       monitor,
		interval:      heartbeatInterval,
		logger:        logger.With(zap.String("worker", name)),
		heartbeatFunc: func() {
			monitor.UpdateHeartbeat(name)
		},
	}
}

// Run runs the wrapped worker with automatic heartbeat reporting
func (w *WrappedWorker) Run(ctx context.Context) error {
	// Start heartbeat ticker
	heartbeatTicker := time.NewTicker(w.interval)
	defer heartbeatTicker.Stop()

	// Run heartbeat in background
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Heartbeat goroutine panic recovered",
					zap.Any("panic", r))
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeatTicker.C:
				w.heartbeatFunc()
			case <-done:
				return
			}
		}
	}()

	// Run the actual worker
	err := w.workerFunc(ctx)

	// Stop heartbeat
	close(done)

	return err
}

// WithManualHeartbeat creates a worker that must manually call heartbeat
func WithManualHeartbeat(name string, workerFunc WorkerFunc, monitor *Monitor, logger *zap.Logger) WorkerFunc {
	return func(ctx context.Context) error {
		// Initial heartbeat
		monitor.UpdateHeartbeat(name)

		// Run worker
		err := workerFunc(ctx)

		return err
	}
}

// StartWorkerWithRetry starts a worker with automatic retry on failure
func StartWorkerWithRetry(
	name string,
	workerFunc WorkerFunc,
	monitor *Monitor,
	maxRetries int,
	retryDelay time.Duration,
	logger *zap.Logger,
) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Worker goroutine panic recovered",
					zap.String("worker", name),
					zap.Any("panic", r))
			}
		}()

		retryCount := 0
		for {
			if retryCount > maxRetries {
				logger.Error("Worker exceeded max retries, giving up",
					zap.String("worker", name),
					zap.Int("max_retries", maxRetries))
				return
			}

			logger.Info("Starting worker",
				zap.String("worker", name),
				zap.Int("attempt", retryCount+1))

			err := workerFunc(context.Background())
			if err != nil {
				logger.Error("Worker failed, will retry",
					zap.String("worker", name),
					zap.Int("attempt", retryCount+1),
					zap.Error(err),
					zap.Duration("retry_in", retryDelay))

				retryCount++
				time.Sleep(retryDelay)
				continue
			}

			logger.Info("Worker completed successfully",
				zap.String("worker", name))
			return
		}
	}()
}
