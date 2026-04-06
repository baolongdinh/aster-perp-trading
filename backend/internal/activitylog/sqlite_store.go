package activitylog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Storage interface using SQLite.
type SQLiteStore struct {
	db   *sql.DB
	path string
}

// NewSQLiteStore creates a new SQLite storage instance.
// The database is created with WAL mode enabled for better concurrency.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// Set connection pool limits
	db.SetMaxOpenConns(1) // SQLite handles concurrency better with single writer
	db.SetMaxIdleConns(1)

	store := &SQLiteStore{
		db:   db,
		path: path,
	}

	if err := store.createSchema(); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return store, nil
}

// createSchema initializes the database schema.
func (s *SQLiteStore) createSchema() error {
	schema := `
CREATE TABLE IF NOT EXISTS activity_logs (
    id TEXT PRIMARY KEY,
    trace_id TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '1.0',
    
    -- Context fields
    symbol TEXT,
    strategy_id TEXT,
    strategy_name TEXT,
    bot_instance TEXT,
    
    -- Payload as JSON
    payload JSON NOT NULL,
    
    -- Metadata
    source_file TEXT,
    source_line INTEGER,
    latency_ms REAL
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_timestamp ON activity_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_trace_id ON activity_logs(trace_id);
CREATE INDEX IF NOT EXISTS idx_event_type ON activity_logs(event_type);
CREATE INDEX IF NOT EXISTS idx_symbol ON activity_logs(symbol);
CREATE INDEX IF NOT EXISTS idx_strategy_id ON activity_logs(strategy_id);
CREATE INDEX IF NOT EXISTS idx_severity ON activity_logs(severity);

-- Composite indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_symbol_time ON activity_logs(symbol, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_strategy_time ON activity_logs(strategy_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_event_time ON activity_logs(event_type, timestamp DESC);
`
	_, err := s.db.Exec(schema)
	return err
}

// Insert writes a single log entry.
func (s *SQLiteStore) Insert(ctx context.Context, entry LogEntry) error {
	return s.InsertBatch(ctx, []LogEntry{entry})
}

// InsertBatch writes multiple log entries in a single transaction.
func (s *SQLiteStore) InsertBatch(ctx context.Context, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO activity_logs (
			id, trace_id, timestamp, event_type, severity, version,
			symbol, strategy_id, strategy_name, bot_instance,
			payload, source_file, source_line, latency_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		payloadJSON, err := json.Marshal(entry.Payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}

		_, err = stmt.ExecContext(ctx,
			entry.ID.String(),
			entry.TraceID,
			entry.Timestamp.UnixMilli(),
			string(entry.EventType),
			string(entry.Severity),
			entry.Version,
			entry.Context.Symbol,
			entry.Context.StrategyID,
			entry.Context.StrategyName,
			entry.Context.BotInstance,
			payloadJSON,
			entry.Metadata.SourceFile,
			entry.Metadata.SourceLine,
			entry.Metadata.LatencyMs,
		)
		if err != nil {
			return fmt.Errorf("failed to insert entry: %w", err)
		}
	}

	return tx.Commit()
}

// Query retrieves log entries matching the query criteria.
func (s *SQLiteStore) Query(ctx context.Context, q Query) (QueryResult, error) {
	var whereClauses []string
	var args []interface{}

	// Build WHERE clauses
	if !q.TimeRange.Start.IsZero() {
		whereClauses = append(whereClauses, "timestamp >= ?")
		args = append(args, q.TimeRange.Start.UnixMilli())
	}
	if !q.TimeRange.End.IsZero() {
		whereClauses = append(whereClauses, "timestamp <= ?")
		args = append(args, q.TimeRange.End.UnixMilli())
	}
	if len(q.EventTypes) > 0 {
		placeholders := make([]string, len(q.EventTypes))
		for i, et := range q.EventTypes {
			placeholders[i] = "?"
			args = append(args, string(et))
		}
		whereClauses = append(whereClauses, fmt.Sprintf("event_type IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(q.Symbols) > 0 {
		placeholders := make([]string, len(q.Symbols))
		for i, sym := range q.Symbols {
			placeholders[i] = "?"
			args = append(args, sym)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("symbol IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(q.Strategies) > 0 {
		placeholders := make([]string, len(q.Strategies))
		for i, strat := range q.Strategies {
			placeholders[i] = "?"
			args = append(args, strat)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("strategy_id IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(q.Severities) > 0 {
		placeholders := make([]string, len(q.Severities))
		for i, sev := range q.Severities {
			placeholders[i] = "?"
			args = append(args, string(sev))
		}
		whereClauses = append(whereClauses, fmt.Sprintf("severity IN (%s)", strings.Join(placeholders, ",")))
	}
	if q.TraceID != "" {
		whereClauses = append(whereClauses, "trace_id = ?")
		args = append(args, q.TraceID)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Get total count
	countSQL := "SELECT COUNT(*) FROM activity_logs " + whereSQL
	var total int64
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return QueryResult{}, fmt.Errorf("failed to count entries: %w", err)
	}

	// Build main query
	sortOrder := "DESC"
	if q.SortOrder == SortAsc {
		sortOrder = "ASC"
	}

	limit := q.Pagination.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	querySQL := fmt.Sprintf(`
		SELECT id, trace_id, timestamp, event_type, severity, version,
			symbol, strategy_id, strategy_name, bot_instance,
			payload, source_file, source_line, latency_ms
		FROM activity_logs
		%s
		ORDER BY timestamp %s
		LIMIT ? OFFSET ?
	`, whereSQL, sortOrder)

	queryArgs := append(args, limit, q.Pagination.Offset)

	rows, err := s.db.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("failed to query entries: %w", err)
	}
	defer rows.Close()

	entries, err := s.scanRows(rows)
	if err != nil {
		return QueryResult{}, err
	}

	return QueryResult{
		Entries: entries,
		Total:   total,
		HasMore: int64(q.Pagination.Offset+len(entries)) < total,
	}, nil
}

// GetByTraceID retrieves all entries with the given trace ID.
func (s *SQLiteStore) GetByTraceID(ctx context.Context, traceID string) ([]LogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, trace_id, timestamp, event_type, severity, version,
			symbol, strategy_id, strategy_name, bot_instance,
			payload, source_file, source_line, latency_ms
		FROM activity_logs
		WHERE trace_id = ?
		ORDER BY timestamp ASC
	`, traceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query by trace_id: %w", err)
	}
	defer rows.Close()

	return s.scanRows(rows)
}

// GetByID retrieves a single entry by its ID.
func (s *SQLiteStore) GetByID(ctx context.Context, id string) (*LogEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, trace_id, timestamp, event_type, severity, version,
			symbol, strategy_id, strategy_name, bot_instance,
			payload, source_file, source_line, latency_ms
		FROM activity_logs
		WHERE id = ?
	`, id)

	entry, err := s.scanRow(row)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// Count returns the number of entries matching the query.
func (s *SQLiteStore) Count(ctx context.Context, q Query) (int64, error) {
	var whereClauses []string
	var args []interface{}

	if !q.TimeRange.Start.IsZero() {
		whereClauses = append(whereClauses, "timestamp >= ?")
		args = append(args, q.TimeRange.Start.UnixMilli())
	}
	if !q.TimeRange.End.IsZero() {
		whereClauses = append(whereClauses, "timestamp <= ?")
		args = append(args, q.TimeRange.End.UnixMilli())
	}
	if len(q.EventTypes) > 0 {
		placeholders := make([]string, len(q.EventTypes))
		for i, et := range q.EventTypes {
			placeholders[i] = "?"
			args = append(args, string(et))
		}
		whereClauses = append(whereClauses, fmt.Sprintf("event_type IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(q.Symbols) > 0 {
		placeholders := make([]string, len(q.Symbols))
		for i, sym := range q.Symbols {
			placeholders[i] = "?"
			args = append(args, sym)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("symbol IN (%s)", strings.Join(placeholders, ",")))
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	var count int64
	sql := "SELECT COUNT(*) FROM activity_logs " + whereSQL
	if err := s.db.QueryRowContext(ctx, sql, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count: %w", err)
	}
	return count, nil
}

// Aggregate performs aggregation queries.
func (s *SQLiteStore) Aggregate(ctx context.Context, q Query, agg Aggregation) (AggregationResult, error) {
	// For now, implement basic count by group
	if agg.Type != AggCount {
		return AggregationResult{}, fmt.Errorf("aggregation type %s not yet implemented", agg.Type)
	}

	groupByFields := strings.Join(agg.GroupBy, ", ")
	sql := fmt.Sprintf(`
		SELECT %s, COUNT(*) as count
		FROM activity_logs
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY %s
	`, groupByFields, groupByFields)

	rows, err := s.db.QueryContext(ctx, sql,
		q.TimeRange.Start.UnixMilli(),
		q.TimeRange.End.UnixMilli(),
	)
	if err != nil {
		return AggregationResult{}, fmt.Errorf("failed to aggregate: %w", err)
	}
	defer rows.Close()

	var results []AggregationGroup
	for rows.Next() {
		var count int64
		groupKey := make(map[string]interface{})

		// Scan based on group by fields
		dest := make([]interface{}, len(agg.GroupBy)+1)
		for i := range agg.GroupBy {
			var val interface{}
			dest[i] = &val
		}
		dest[len(agg.GroupBy)] = &count

		if err := rows.Scan(dest...); err != nil {
			return AggregationResult{}, fmt.Errorf("failed to scan aggregate row: %w", err)
		}

		for i, field := range agg.GroupBy {
			groupKey[field] = *(dest[i].(*interface{}))
		}

		results = append(results, AggregationGroup{
			GroupKey: groupKey,
			Value:    float64(count),
			Count:    count,
		})
	}

	return AggregationResult{Results: results}, nil
}

// Cleanup removes entries older than the given time.
func (s *SQLiteStore) Cleanup(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM activity_logs WHERE timestamp < ?",
		before.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup old entries: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// scanRows scans multiple rows into LogEntry slices.
func (s *SQLiteStore) scanRows(rows *sql.Rows) ([]LogEntry, error) {
	var entries []LogEntry
	for rows.Next() {
		entry, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}
	return entries, rows.Err()
}

// RowScanner interface to handle both *sql.Rows and *sql.Row
type RowScanner interface {
	Scan(dest ...interface{}) error
}

// scanRow scans a single row into a LogEntry.
func (s *SQLiteStore) scanRow(scanner RowScanner) (*LogEntry, error) {
	var idStr string
	var timestampMs int64
	var eventTypeStr, severityStr string
	var payloadJSON []byte

	entry := &LogEntry{
		Payload: make(map[string]interface{}),
	}

	err := scanner.Scan(
		&idStr,
		&entry.TraceID,
		&timestampMs,
		&eventTypeStr,
		&severityStr,
		&entry.Version,
		&entry.Context.Symbol,
		&entry.Context.StrategyID,
		&entry.Context.StrategyName,
		&entry.Context.BotInstance,
		&payloadJSON,
		&entry.Metadata.SourceFile,
		&entry.Metadata.SourceLine,
		&entry.Metadata.LatencyMs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}

	// Parse ULID
	if err := entry.ID.UnmarshalText([]byte(idStr)); err != nil {
		return nil, fmt.Errorf("failed to parse ULID: %w", err)
	}

	entry.Timestamp = time.UnixMilli(timestampMs)
	entry.EventType = EventType(eventTypeStr)
	entry.Severity = Severity(severityStr)

	if len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &entry.Payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	}

	return entry, nil
}
