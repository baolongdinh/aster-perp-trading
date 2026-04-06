package activitylog

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"go.uber.org/zap"
)

// Broadcaster defines the interface for broadcasting events to subscribers.
type Broadcaster interface {
	BroadcastActivity(entry LogEntry)
}

// ActivityLogger is the main structured activity logging interface.
// It provides async, non-blocking logging with automatic batching and persistence.
type ActivityLogger struct {
	config      Config
	buffer      *RingBuffer
	storage     Storage
	fileStore   *FileStore
	broadcaster Broadcaster
	log         *zap.Logger

	// Background processing
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	done   chan struct{}

	// ULID entropy source
	entropy *ulid.MonotonicEntropy
}

// New creates a new ActivityLogger with the given configuration.
func New(config Config, broadcaster Broadcaster, log *zap.Logger) (*ActivityLogger, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize SQLite storage
	storage, err := NewSQLiteStore(config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	// Initialize file store
	fileStore, err := NewFileStore(config.FilePath, config.MaxFileSize)
	if err != nil {
		storage.Close()
		return nil, fmt.Errorf("failed to create file store: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	logger := &ActivityLogger{
		config:      config,
		buffer:      NewRingBuffer(config.BufferSize),
		storage:     storage,
		fileStore:   fileStore,
		broadcaster: broadcaster,
		log:         log,
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
		entropy:     ulid.Monotonic(nil, 0),
	}

	// Start background workers
	for i := 0; i < config.Workers; i++ {
		logger.wg.Add(1)
		go logger.processLoop()
	}

	// Start periodic cleanup
	logger.wg.Add(1)
	go logger.cleanupLoop()

	return logger, nil
}

// Log records a single activity event.
// This method is non-blocking and returns immediately.
func (l *ActivityLogger) Log(ctx context.Context, eventType EventType, severity Severity, context EntryContext, payload interface{}) error {
	return l.LogWithTrace(ctx, "", eventType, severity, context, payload)
}

// LogWithTrace records an event with an explicit trace ID.
func (l *ActivityLogger) LogWithTrace(ctx context.Context, traceID string, eventType EventType, severity Severity, context EntryContext, payload interface{}) error {
	// Filter checks
	if !l.config.ShouldLogEvent(eventType) {
		return nil
	}
	if !l.config.ShouldLogSeverity(severity) {
		return nil
	}

	start := time.Now()

	// Generate ULID
	id, err := ulid.New(ulid.Timestamp(time.Now()), l.entropy)
	if err != nil {
		return fmt.Errorf("failed to generate ULID: %w", err)
	}

	// Get caller info
	_, file, line, _ := runtime.Caller(2)

	// Convert payload to map
	var payloadMap map[string]interface{}
	if payload != nil {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		if err := json.Unmarshal(payloadBytes, &payloadMap); err != nil {
			return fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	}

	// Set bot instance if not provided
	if context.BotInstance == "" {
		context.BotInstance = l.config.BotInstance
	}

	entry := LogEntry{
		ID:        id,
		TraceID:   traceID,
		Timestamp: time.Now(),
		EventType: eventType,
		Severity:  severity,
		Version:   "1.0",
		Context:   context,
		Payload:   payloadMap,
		Metadata: EntryMetadata{
			SourceFile: file,
			SourceLine: line,
			LatencyMs:  0, // Will be set after processing
		},
	}

	// Calculate latency after entry creation
	entry.Metadata.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0

	// Write to ring buffer (non-blocking)
	if !l.buffer.Write(entry) {
		// Buffer full, entry dropped
		if l.log != nil {
			l.log.Warn("activity log buffer full, entry dropped",
				zap.String("event_type", string(eventType)),
				zap.Uint64("dropped_total", l.buffer.Dropped()),
			)
		}
	}

	return nil
}

// StartTrace generates a new trace ID with the given prefix.
func (l *ActivityLogger) StartTrace(prefix string) string {
	timestamp := time.Now().Unix()
	return fmt.Sprintf("%s_%d", prefix, timestamp)
}

// processLoop is the background worker that processes entries from the buffer.
func (l *ActivityLogger) processLoop() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]LogEntry, 0, l.config.BatchSize)

	for {
		select {
		case <-l.ctx.Done():
			// Flush remaining entries
			l.flushBatch(batch)
			return

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				l.flushBatch(batch)
				batch = batch[:0]
			}

		default:
			// Try to read from buffer
			entry, ok := l.buffer.TryRead()
			if !ok {
				// Buffer empty, brief pause
				time.Sleep(time.Millisecond)
				continue
			}

			batch = append(batch, entry)

			// Flush if batch is full
			if len(batch) >= l.config.BatchSize {
				l.flushBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// flushBatch writes a batch of entries to all sinks.
func (l *ActivityLogger) flushBatch(batch []LogEntry) {
	if len(batch) == 0 {
		return
	}

	// Broadcast to WebSocket (non-blocking)
	if l.broadcaster != nil {
		for _, entry := range batch {
			l.broadcaster.BroadcastActivity(entry)
		}
	}

	// Write to SQLite (blocking but with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := l.storage.InsertBatch(ctx, batch); err != nil {
		if l.log != nil {
			l.log.Error("failed to insert batch to storage",
				zap.Error(err),
				zap.Int("batch_size", len(batch)),
			)
		}
	}

	// Write to file store
	if err := l.fileStore.WriteBatch(batch); err != nil {
		if l.log != nil {
			l.log.Error("failed to write batch to file store",
				zap.Error(err),
				zap.Int("batch_size", len(batch)),
			)
		}
	}
}

// cleanupLoop periodically cleans up old entries.
func (l *ActivityLogger) cleanupLoop() {
	defer l.wg.Done()

	ticker := time.NewTicker(time.Hour) // Run cleanup every hour
	defer ticker.Stop()

	retention := time.Duration(l.config.RetentionDays) * 24 * time.Hour

	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-retention)

			// Cleanup SQLite
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := l.storage.Cleanup(ctx, cutoff); err != nil {
				if l.log != nil {
					l.log.Error("failed to cleanup storage", zap.Error(err))
				}
			}
			cancel()

			// Cleanup file store
			if err := l.fileStore.Cleanup(retention); err != nil {
				if l.log != nil {
					l.log.Error("failed to cleanup file store", zap.Error(err))
				}
			}
		}
	}
}

// Shutdown gracefully stops the logger and flushes pending entries.
func (l *ActivityLogger) Shutdown(ctx context.Context) error {
	// Signal shutdown
	l.cancel()

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Workers finished
	case <-ctx.Done():
		// Timeout
	}

	// Flush any remaining entries in buffer
	remaining := l.buffer.Drain()
	if len(remaining) > 0 {
		l.flushBatch(remaining)
	}

	// Close storage
	if err := l.storage.Close(); err != nil {
		return fmt.Errorf("failed to close storage: %w", err)
	}

	if err := l.fileStore.Close(); err != nil {
		return fmt.Errorf("failed to close file store: %w", err)
	}

	return nil
}

// GetStats returns current logger statistics.
func (l *ActivityLogger) GetStats() LoggerStats {
	return LoggerStats{
		BufferSize:   l.buffer.Capacity(),
		BufferUsed:   l.buffer.Size(),
		DroppedCount: l.buffer.Dropped(),
		IsHealthy:    l.buffer.Size() < l.buffer.Capacity()*90/100,
	}
}

// Query queries activity logs from storage.
func (l *ActivityLogger) Query(ctx context.Context, query Query) (QueryResult, error) {
	return l.storage.Query(ctx, query)
}

// LoggerStats holds statistics about the logger.
type LoggerStats struct {
	BufferSize   uint64 `json:"buffer_size"`
	BufferUsed   uint64 `json:"buffer_used"`
	DroppedCount uint64 `json:"dropped_count"`
	IsHealthy    bool   `json:"is_healthy"`
}
