package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// WorkerStatus represents the health status of a worker
type WorkerStatus string

const (
	WorkerStatusHealthy   WorkerStatus = "healthy"
	WorkerStatusUnhealthy WorkerStatus = "unhealthy"
	WorkerStatusDead      WorkerStatus = "dead"
	WorkerStatusUnknown   WorkerStatus = "unknown"
)

// WorkerHealth represents the health information of a worker
type WorkerHealth struct {
	Name         string
	Status       WorkerStatus
	LastSeen     time.Time
	LastError    string
	ErrorCount   int
	RestartCount int
}

// WorkerConfig holds configuration for a worker
type WorkerConfig struct {
	Name              string
	HeartbeatInterval time.Duration
	HealthCheckInterval time.Duration
	MaxErrorCount     int
	AutoRestart       bool
}

// Worker represents a monitored worker with restart capability
type Worker struct {
	config        WorkerConfig
	startFunc     func(ctx context.Context) error
	stopFunc      func() error
	health        WorkerHealth
	mu            sync.RWMutex
	logger        *zap.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
}

// Monitor tracks health of all workers and can restart them
type Monitor struct {
	workers       map[string]*Worker
	mu            sync.RWMutex
	logger        *zap.Logger
	healthCheckTicker *time.Ticker
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

// NewMonitor creates a new health monitor
func NewMonitor(logger *zap.Logger) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Monitor{
		workers: make(map[string]*Worker),
		logger:  logger.With(zap.String("component", "health_monitor")),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// RegisterWorker registers a worker for monitoring
func (m *Monitor) RegisterWorker(config WorkerConfig, startFunc func(ctx context.Context) error, stopFunc func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.workers[config.Name]; exists {
		return fmt.Errorf("worker %s already registered", config.Name)
	}

	workerCtx, workerCancel := context.WithCancel(m.ctx)

	worker := &Worker{
		config: config,
		startFunc: startFunc,
		stopFunc: stopFunc,
		health: WorkerHealth{
			Name:         config.Name,
			Status:       WorkerStatusUnknown,
			LastSeen:     time.Now(),
		},
		logger:  m.logger.With(zap.String("worker", config.Name)),
		ctx:     workerCtx,
		cancel:  workerCancel,
		isRunning: false,
	}

	m.workers[config.Name] = worker
	m.logger.Info("Worker registered for health monitoring",
		zap.String("worker", config.Name),
		zap.Bool("auto_restart", config.AutoRestart))

	return nil
}

// Start starts the health monitor
func (m *Monitor) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.healthCheckTicker = time.NewTicker(30 * time.Second)
	m.wg.Add(1)

	go m.healthCheckLoop()
	go m.autoRestartLoop()

	m.logger.Info("Health monitor started")
	return nil
}

// Stop stops the health monitor
func (m *Monitor) Stop() error {
	m.cancel()

	if m.healthCheckTicker != nil {
		m.healthCheckTicker.Stop()
	}

	m.wg.Wait()

	// Stop all workers
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, worker := range m.workers {
		if worker.isRunning && worker.stopFunc != nil {
			m.logger.Info("Stopping worker", zap.String("worker", name))
			if err := worker.stopFunc(); err != nil {
				m.logger.Error("Error stopping worker",
					zap.String("worker", name),
					zap.Error(err))
			}
		}
	}

	m.logger.Info("Health monitor stopped")
	return nil
}

// StartWorker starts a specific worker
func (m *Monitor) StartWorker(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	worker, exists := m.workers[name]
	if !exists {
		return fmt.Errorf("worker %s not found", name)
	}

	if worker.isRunning {
		return fmt.Errorf("worker %s is already running", name)
	}

	worker.isRunning = true
	worker.health.Status = WorkerStatusHealthy
	worker.health.LastSeen = time.Now()

	go m.runWorker(worker)

	m.logger.Info("Worker started", zap.String("worker", name))
	return nil
}

// StopWorker stops a specific worker
func (m *Monitor) StopWorker(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	worker, exists := m.workers[name]
	if !exists {
		return fmt.Errorf("worker %s not found", name)
	}

	if !worker.isRunning {
		return fmt.Errorf("worker %s is not running", name)
	}

	worker.cancel()
	worker.isRunning = false
	worker.health.Status = WorkerStatusUnknown

	m.logger.Info("Worker stopped", zap.String("worker", name))
	return nil
}

// runWorker runs a worker with panic recovery
func (m *Monitor) runWorker(worker *Worker) {
	defer func() {
		if r := recover(); r != nil {
			worker.logger.Error("Worker panic recovered",
				zap.Any("panic", r),
				zap.String("stack", fmt.Sprintf("%+v", r)))
			m.reportWorkerError(worker, fmt.Sprintf("panic: %v", r))
		}
	}()

	err := worker.startFunc(worker.ctx)
	if err != nil {
		worker.logger.Error("Worker stopped with error", zap.Error(err))
		m.reportWorkerError(worker, err.Error())
	}

	worker.mu.Lock()
	worker.isRunning = false
	worker.health.Status = WorkerStatusDead
	worker.mu.Unlock()
}

// healthCheckLoop periodically checks worker health
func (m *Monitor) healthCheckLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.healthCheckTicker.C:
			m.checkAllWorkers()
		}
	}
}

// autoRestartLoop automatically restarts dead workers
func (m *Monitor) autoRestartLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkAndRestartDeadWorkers()
		}
	}
}

// checkAllWorkers checks health of all registered workers
func (m *Monitor) checkAllWorkers() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, worker := range m.workers {
		m.checkWorkerHealth(worker)

		if worker.health.Status == WorkerStatusUnhealthy || worker.health.Status == WorkerStatusDead {
			m.logger.Warn("Worker health check failed",
				zap.String("worker", name),
				zap.String("status", string(worker.health.Status)),
				zap.Time("last_seen", worker.health.LastSeen),
				zap.Duration("time_since_last_seen", time.Since(worker.health.LastSeen)),
				zap.Int("error_count", worker.health.ErrorCount))
		}
	}
}

// checkWorkerHealth checks the health of a single worker
func (m *Monitor) checkWorkerHealth(worker *Worker) {
	worker.mu.Lock()
	defer worker.mu.Unlock()

	// If worker is not running, mark as dead
	if !worker.isRunning {
		worker.health.Status = WorkerStatusDead
		return
	}

	// Check if worker has sent heartbeat recently
	timeSinceLastSeen := time.Since(worker.health.LastSeen)
	maxSilence := worker.config.HeartbeatInterval * 3 // Allow 3x interval

	if timeSinceLastSeen > maxSilence {
		worker.health.Status = WorkerStatusUnhealthy
		worker.health.LastError = fmt.Sprintf("no heartbeat for %v", timeSinceLastSeen)
		worker.health.ErrorCount++
	} else {
		worker.health.Status = WorkerStatusHealthy
		worker.health.LastError = ""
	}
}

// checkAndRestartDeadWorkers automatically restarts dead workers
func (m *Monitor) checkAndRestartDeadWorkers() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, worker := range m.workers {
		if worker.config.AutoRestart {
			worker.mu.Lock()
			shouldRestart := !worker.isRunning && worker.health.Status == WorkerStatusDead
			worker.mu.Unlock()

			if shouldRestart {
				m.logger.Warn("Auto-restarting dead worker",
					zap.String("worker", name),
					zap.Int("restart_count", worker.health.RestartCount+1))

				if err := m.StartWorker(name); err != nil {
					m.logger.Error("Failed to auto-restart worker",
						zap.String("worker", name),
						zap.Error(err))
				} else {
					worker.mu.Lock()
					worker.health.RestartCount++
					worker.mu.Unlock()
				}
			}
		}
	}
}

// reportWorkerError reports an error for a worker
func (m *Monitor) reportWorkerError(worker *Worker, error string) {
	worker.mu.Lock()
	defer worker.mu.Unlock()

	worker.health.LastError = error
	worker.health.ErrorCount++
	worker.health.LastSeen = time.Now()
}

// UpdateHeartbeat updates the heartbeat timestamp for a worker
func (m *Monitor) UpdateHeartbeat(workerName string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	worker, exists := m.workers[workerName]
	if !exists {
		m.logger.Warn("Attempted to update heartbeat for unknown worker",
			zap.String("worker", workerName))
		return
	}

	worker.mu.Lock()
	worker.health.LastSeen = time.Now()
	worker.health.Status = WorkerStatusHealthy
	worker.health.LastError = ""
	worker.mu.Unlock()
}

// GetWorkerHealth returns the health status of a worker
func (m *Monitor) GetWorkerHealth(workerName string) (WorkerHealth, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	worker, exists := m.workers[workerName]
	if !exists {
		return WorkerHealth{}, fmt.Errorf("worker %s not found", workerName)
	}

	worker.mu.RLock()
	defer worker.mu.RUnlock()

	return worker.health, nil
}

// GetAllWorkerHealth returns health status of all workers
func (m *Monitor) GetAllWorkerHealth() map[string]WorkerHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]WorkerHealth)
	for name, worker := range m.workers {
		worker.mu.RLock()
		result[name] = worker.health
		worker.mu.RUnlock()
	}

	return result
}
