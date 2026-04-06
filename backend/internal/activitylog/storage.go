package activitylog

import (
	"context"
	"time"
)

// Storage defines the persistence interface for activity logs.
// Implementations must be safe for concurrent use.
type Storage interface {
	// Insert writes a single log entry to storage.
	Insert(ctx context.Context, entry LogEntry) error

	// InsertBatch writes multiple log entries efficiently.
	InsertBatch(ctx context.Context, entries []LogEntry) error

	// Query retrieves log entries matching the query criteria.
	Query(ctx context.Context, q Query) (QueryResult, error)

	// GetByTraceID retrieves all entries with the given trace ID, ordered by timestamp.
	GetByTraceID(ctx context.Context, traceID string) ([]LogEntry, error)

	// GetByID retrieves a single entry by its ID.
	GetByID(ctx context.Context, id string) (*LogEntry, error)

	// Count returns the number of entries matching the query.
	Count(ctx context.Context, q Query) (int64, error)

	// Aggregate performs aggregation queries (count, sum, avg, min, max).
	Aggregate(ctx context.Context, q Query, agg Aggregation) (AggregationResult, error)

	// Cleanup removes entries older than the given time.
	Cleanup(ctx context.Context, before time.Time) error

	// Close releases any resources held by the storage.
	Close() error
}
