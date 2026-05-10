package maker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MultiProcessManager manages multiple trading processes for different symbols
type MultiProcessManager struct {
	logger            *zap.Logger
	configPath        string
	dryRun           bool
	adaptiveConfig     *AdaptiveConfigManager
	
	// Process tracking
	processes        map[string]*TradingProcess
	processStatus     map[string]ProcessStatus
	mu               sync.RWMutex
	
	// Configuration
	maxConcurrentProcesses int
	restartDelay         time.Duration
	healthCheckInterval   time.Duration
}

// TradingProcess represents a single symbol trading process
type TradingProcess struct {
	Symbol        string    `json:"symbol"`
	ProcessID     int       `json:"process_id"`
	Command       *exec.Cmd `json:"-"`
	Status        ProcessStatus `json:"status"`
	StartTime     time.Time   `json:"start_time"`
	LastHeartbeat time.Time   `json:"last_heartbeat"`
	RestartCount  int        `json:"restart_count"`
	Config        *AdaptiveConfig `json:"config"`
	
	// Channels for communication
	StopChan      chan struct{} `json:"-"`
	StatusChan    chan ProcessStatus `json:"-"`
	ErrorChan      chan error      `json:"-"`
}

// ProcessStatus represents the current state of a trading process
type ProcessStatus string

const (
	StatusStarting   ProcessStatus = "starting"
	StatusRunning   ProcessStatus = "running"
	StatusStopping  ProcessStatus = "stopping"
	StatusStopped   ProcessStatus = "stopped"
	StatusError     ProcessStatus = "error"
	StatusRestarting ProcessStatus = "restarting"
)

// ProcessInfo contains information about a running process
type ProcessInfo struct {
	Symbol       string        `json:"symbol"`
	Status       ProcessStatus  `json:"status"`
	PID          int           `json:"pid"`
	StartTime     time.Time     `json:"start_time"`
	RestartCount  int           `json:"restart_count"`
	Config       *AdaptiveConfig `json:"config"`
	LastUpdate   time.Time     `json:"last_update"`
}

func NewMultiProcessManager(
	configPath string,
	dryRun bool,
	logger *zap.Logger,
) *MultiProcessManager {
	return &MultiProcessManager{
		logger:                logger,
		configPath:            configPath,
		dryRun:               dryRun,
		processes:             make(map[string]*TradingProcess),
		processStatus:          make(map[string]ProcessStatus),
		maxConcurrentProcesses:   10,
		restartDelay:           10 * time.Second,
		healthCheckInterval:     30 * time.Second,
	}
}

// StartMultipleProcesses launches trading processes for multiple symbols
func (m *MultiProcessManager) StartMultipleProcesses(ctx context.Context, symbols []string) error {
	m.logger.Info("Starting multi-process trading system",
		zap.Strings("symbols", symbols),
		zap.Int("max_processes", m.maxConcurrentProcesses))
	
	// Initialize adaptive config manager
	adaptiveConfig := NewAdaptiveConfigManager(nil, nil, m.logger)
	if err := adaptiveConfig.Start(ctx); err != nil {
		return fmt.Errorf("failed to start adaptive config manager: %w", err)
	}
	m.adaptiveConfig = adaptiveConfig
	
	// Start processes for each symbol
	for _, symbol := range symbols {
		if err := m.startSymbolProcess(ctx, symbol); err != nil {
			m.logger.Error("Failed to start process for symbol",
				zap.String("symbol", symbol),
				zap.Error(err))
			continue
		}
	}
	
	// Start health monitoring
	go m.healthMonitoringLoop(ctx)
	
	// Start status reporting
	go m.statusReportingLoop(ctx)
	
	m.logger.Info("Multi-process trading system started successfully")
	return nil
}

// startSymbolProcess launches a single trading process for a symbol
func (m *MultiProcessManager) startSymbolProcess(ctx context.Context, symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Check if already running
	if process, exists := m.processes[symbol]; exists && process.Status == StatusRunning {
		return fmt.Errorf("process for symbol %s already running", symbol)
	}
	
	// Get optimal configuration for symbol
	config, err := m.adaptiveConfig.GetOptimalConfig(symbol)
	if err != nil {
		return fmt.Errorf("failed to get optimal config for %s: %w", symbol, err)
	}
	
	// Build command arguments
	args := m.buildProcessArgs(symbol, config)
	
	// Create command
	cmd := exec.CommandContext(ctx, "go", args...)
	
	// Create process structure
	process := &TradingProcess{
		Symbol:        symbol,
		ProcessID:     len(m.processes) + 1,
		Command:       cmd,
		Status:        StatusStarting,
		StartTime:     time.Now(),
		LastHeartbeat: time.Now(),
		RestartCount:  0,
		Config:        config,
		StopChan:      make(chan struct{}),
		StatusChan:    make(chan ProcessStatus, 1),
		ErrorChan:      make(chan error, 1),
	}
	
	// Store process
	m.processes[symbol] = process
	m.processStatus[symbol] = StatusStarting
	
	// Start process in goroutine
	go m.runProcess(ctx, process)
	
	m.logger.Info("Started trading process",
		zap.String("symbol", symbol),
		zap.Int("process_id", process.ProcessID),
		zap.Float64("spread_bps", config.DefaultSpreadBps),
		zap.Float64("grid_spacing_bps", config.MicroGridSpacingBps))
	
	return nil
}

// runProcess executes and monitors a trading process
func (m *MultiProcessManager) runProcess(ctx context.Context, process *TradingProcess) {
	// Update status
	m.updateProcessStatus(process.Symbol, StatusStarting)
	
	// Start the command
	if err := process.Command.Start(); err != nil {
		m.updateProcessStatus(process.Symbol, StatusError)
		process.ErrorChan <- fmt.Errorf("failed to start process: %w", err)
		return
	}
	
	// Update status to running
	m.updateProcessStatus(process.Symbol, StatusRunning)
	process.StatusChan <- StatusRunning
	
	// Monitor process
	done := make(chan error, 1)
	go func() {
		done <- process.Command.Wait()
	}()
	
	// Heartbeat loop
	heartbeatTicker := time.NewTicker(10 * time.Second)
	defer heartbeatTicker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			m.stopProcess(process)
			return
			
		case <-process.StopChan:
			m.stopProcess(process)
			return
			
		case err := <-done:
			if err != nil {
				m.handleProcessError(process, err)
			} else {
				m.updateProcessStatus(process.Symbol, StatusStopped)
			}
			return
			
		case <-heartbeatTicker.C:
			process.LastHeartbeat = time.Now()
		}
	}
}

// stopProcess gracefully stops a trading process
func (m *MultiProcessManager) stopProcess(process *TradingProcess) {
	m.updateProcessStatus(process.Symbol, StatusStopping)
	
	if process.Command.Process != nil {
		// Send SIGTERM for graceful shutdown
		process.Command.Process.Signal(os.Interrupt)
		
		// Wait for graceful shutdown or force kill
		done := make(chan error, 1)
		go func() {
			done <- process.Command.Wait()
		}()
		
		select {
		case <-done:
			m.logger.Info("Process stopped gracefully",
				zap.String("symbol", process.Symbol))
		case <-time.After(30 * time.Second):
			// Force kill if doesn't stop gracefully
			process.Command.Process.Kill()
			m.logger.Warn("Process force killed",
				zap.String("symbol", process.Symbol))
		}
	}
	
	m.updateProcessStatus(process.Symbol, StatusStopped)
}

// handleProcessError manages process failures and restarts
func (m *MultiProcessManager) handleProcessError(process *TradingProcess, err error) {
	m.logger.Error("Trading process error",
		zap.String("symbol", process.Symbol),
		zap.Int("restart_count", process.RestartCount),
		zap.Error(err))
	
	m.updateProcessStatus(process.Symbol, StatusError)
	process.ErrorChan <- err
	
	// Check if should restart
	if process.RestartCount < 3 { // Max 3 restarts
		m.updateProcessStatus(process.Symbol, StatusRestarting)
		process.StatusChan <- StatusRestarting
		
		// Wait before restart
		time.Sleep(m.restartDelay)
		
		process.RestartCount++
		
		// Restart with new optimal config
		config, configErr := m.adaptiveConfig.GetOptimalConfig(process.Symbol)
		if configErr != nil {
			m.logger.Error("Failed to get config for restart",
				zap.String("symbol", process.Symbol),
				zap.Error(configErr))
			return
		}
		
		process.Config = config
		
		// Create new command
		args := m.buildProcessArgs(process.Symbol, config)
		process.Command = exec.Command("go", args...)
		
		// Restart process
		go m.runProcess(context.Background(), process)
		
		m.logger.Info("Restarted trading process",
			zap.String("symbol", process.Symbol),
			zap.Int("restart_attempt", process.RestartCount))
	} else {
		m.logger.Error("Max restart attempts reached, giving up",
			zap.String("symbol", process.Symbol))
		m.updateProcessStatus(process.Symbol, StatusStopped)
	}
}

// buildProcessArgs constructs command line arguments for a trading process
func (m *MultiProcessManager) buildProcessArgs(symbol string, config *AdaptiveConfig) []string {
	args := []string{
		"run",
		"./cmd/volume-farm-maker/main.go",
		fmt.Sprintf("-symbol=%s", symbol),
		fmt.Sprintf("-config=%s", m.configPath),
		fmt.Sprintf("-spread=%.2f", config.DefaultSpreadBps),
		fmt.Sprintf("-leverage=%d", 50), // Use default leverage
		fmt.Sprintf("-dynamic-sizing=%t", true),
		fmt.Sprintf("-base-notional=%.2f", config.BaseNotionalUSD),
		fmt.Sprintf("-micro-profit=%t", true),
		fmt.Sprintf("-micro-spacing=%.3f", config.MicroGridSpacingBps),
		fmt.Sprintf("-micro-levels=%d", config.MicroGridLevels),
		fmt.Sprintf("-toxic-flow=%t", true),
		fmt.Sprintf("-bias-threshold=%.3f", config.PositionBiasThreshold),
		fmt.Sprintf("-momentum=%t", true),
		fmt.Sprintf("-momentum-threshold=%.3f", config.MomentumThresholdPct),
	}
	
	if m.dryRun {
		args = append(args, "-dry-run=true")
	}
	
	return args
}

// healthMonitoringLoop monitors the health of all processes
func (m *MultiProcessManager) healthMonitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(m.healthCheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkProcessHealth()
		}
	}
}

// checkProcessHealth verifies all processes are responsive
func (m *MultiProcessManager) checkProcessHealth() {
	m.mu.RLock()
	processes := make([]*TradingProcess, 0, len(m.processes))
	for _, process := range m.processes {
		processes = append(processes, process)
	}
	m.mu.RUnlock()
	
	for _, process := range processes {
		// Check heartbeat
		if time.Since(process.LastHeartbeat) > 60*time.Second {
			m.logger.Warn("Process heartbeat timeout",
				zap.String("symbol", process.Symbol),
				zap.Time("last_heartbeat", process.LastHeartbeat))
			
			// Consider process dead
			if process.Status == StatusRunning {
				m.handleProcessError(process, fmt.Errorf("heartbeat timeout"))
			}
		}
		
		// Check if process is still alive
		if process.Command.Process != nil && process.Command.ProcessState != nil {
			if process.Command.ProcessState.Exited() {
				m.logger.Warn("Process died unexpectedly",
					zap.String("symbol", process.Symbol))
				m.handleProcessError(process, fmt.Errorf("process died"))
			}
		}
	}
}

// statusReportingLoop periodically reports process status
func (m *MultiProcessManager) statusReportingLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second) // Report every minute
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reportProcessStatus()
		}
	}
}

// reportProcessStatus logs current status of all processes
func (m *MultiProcessManager) reportProcessStatus() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if len(m.processes) == 0 {
		return
	}
	
	m.logger.Info("=== Multi-Process Status Report ===")
	
	for symbol, process := range m.processes {
		uptime := time.Since(process.StartTime)
		m.logger.Info("Process Status",
			zap.String("symbol", symbol),
			zap.String("status", string(process.Status)),
			zap.Duration("uptime", uptime),
			zap.Int("restart_count", process.RestartCount),
			zap.Float64("current_spread_bps", process.Config.DefaultSpreadBps),
			zap.Float64("grid_spacing_bps", process.Config.MicroGridSpacingBps),
			zap.Time("last_heartbeat", process.LastHeartbeat))
	}
	
	m.logger.Info("=== End Status Report ===")
}

// updateProcessStatus updates the status of a process
func (m *MultiProcessManager) updateProcessStatus(symbol string, status ProcessStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.processStatus[symbol] = status
	
	if process, exists := m.processes[symbol]; exists {
		process.Status = status
	}
}

// GetProcessInfo returns information about all processes
func (m *MultiProcessManager) GetProcessInfo() map[string]*ProcessInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	info := make(map[string]*ProcessInfo)
	
	for symbol, process := range m.processes {
		pid := 0
		if process.Command.Process != nil {
			pid = process.Command.Process.Pid
		}
		
		info[symbol] = &ProcessInfo{
			Symbol:      symbol,
			Status:      process.Status,
			PID:         pid,
			StartTime:    process.StartTime,
			RestartCount: process.RestartCount,
			Config:      process.Config,
			LastUpdate:  process.LastHeartbeat,
		}
	}
	
	return info
}

// StopSymbolProcess stops a specific symbol process
func (m *MultiProcessManager) StopSymbolProcess(symbol string) error {
	m.mu.RLock()
	process, exists := m.processes[symbol]
	m.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("no process found for symbol %s", symbol)
	}
	
	// Send stop signal
	close(process.StopChan)
	
	m.logger.Info("Stopping process for symbol",
		zap.String("symbol", symbol))
	
	return nil
}

// StopAllProcesses stops all trading processes
func (m *MultiProcessManager) StopAllProcesses() {
	m.mu.RLock()
	processes := make([]*TradingProcess, 0, len(m.processes))
	for _, process := range m.processes {
		processes = append(processes, process)
	}
	m.mu.RUnlock()
	
	// Stop all processes
	for _, process := range processes {
		close(process.StopChan)
	}
	
	m.logger.Info("All trading processes stopped")
}

// RestartSymbolProcess restarts a specific symbol process
func (m *MultiProcessManager) RestartSymbolProcess(symbol string) error {
	m.mu.RLock()
	process, exists := m.processes[symbol]
	m.mu.RUnlock()
	
	if !exists {
		return m.startSymbolProcess(context.Background(), symbol)
	}
	
	// Stop current process
	close(process.StopChan)
	
	// Wait for stop
	time.Sleep(2 * time.Second)
	
	// Start new process
	return m.startSymbolProcess(context.Background(), symbol)
}

// GetRunningSymbols returns list of currently running symbols
func (m *MultiProcessManager) GetRunningSymbols() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var symbols []string
	for symbol, status := range m.processStatus {
		if status == StatusRunning {
			symbols = append(symbols, symbol)
		}
	}
	
	return symbols
}
