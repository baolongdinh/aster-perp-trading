package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"aster-bot/internal/activitylog"

	"go.uber.org/zap"
)

// ActivityQueryRequest represents the query parameters for activity logs.
type ActivityQueryRequest struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	EventTypes []string  `json:"event_types,omitempty"`
	Symbols    []string  `json:"symbols,omitempty"`
	Strategies []string  `json:"strategies,omitempty"`
	Severities []string  `json:"severities,omitempty"`
	FullText   string    `json:"full_text,omitempty"`
	Limit      int       `json:"limit"`
	Offset     int       `json:"offset"`
	SortOrder  string    `json:"sort_order"`
}

// MetricsResponse represents aggregated metrics for activity logs.
type MetricsResponse struct {
	TotalEvents    int64                 `json:"total_events"`
	EventsByType   map[string]int64      `json:"events_by_type"`
	EventsBySymbol map[string]int64      `json:"events_by_symbol"`
	TimeRange      activitylog.TimeRange `json:"time_range"`
}

// ExportRequest represents an export request for activity logs.
type ExportRequest struct {
	Query    activitylog.Query `json:"query"`
	Format   string            `json:"format"` // json | csv
	Compress bool              `json:"compress"`
}

// SummaryResponse represents a daily summary of activity.
type SummaryResponse struct {
	Date             string  `json:"date"`
	TotalEvents      int64   `json:"total_events"`
	OrderCount       int64   `json:"order_count"`
	OrdersFilled     int64   `json:"orders_filled"`
	OrdersFailed     int64   `json:"orders_failed"`
	RealizedPnL      float64 `json:"realized_pnl"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	ActiveStrategies int     `json:"active_strategies"`
}

// registerActivityRoutes registers activity log API routes.
func (s *Server) registerActivityRoutes() {
	s.mux.HandleFunc("GET /api/v1/activity", s.handleGetActivity)
	s.mux.HandleFunc("GET /api/v1/activity/trace/{trace_id}", s.handleGetTrace)
	s.mux.HandleFunc("GET /api/v1/activity/metrics", s.handleGetMetrics)
	s.mux.HandleFunc("POST /api/v1/activity/export", s.handleExport)
	s.mux.HandleFunc("GET /api/v1/activity/summary", s.handleGetSummary)
}

// handleGetActivity handles GET /api/v1/activity - Query activity logs.
func (s *Server) handleGetActivity(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	req := parseActivityQuery(r)

	// Build query
	query := activitylog.Query{
		TimeRange: activitylog.TimeRange{
			Start: req.Start,
			End:   req.End,
		},
		Pagination: activitylog.Pagination{
			Limit:  req.Limit,
			Offset: req.Offset,
		},
		SortOrder: activitylog.SortOrder(req.SortOrder),
		FullText:  req.FullText,
	}

	// Convert event types
	for _, et := range req.EventTypes {
		query.EventTypes = append(query.EventTypes, activitylog.EventType(et))
	}

	// Copy other filters
	query.Symbols = req.Symbols
	query.Strategies = req.Strategies
	for _, sev := range req.Severities {
		query.Severities = append(query.Severities, activitylog.Severity(sev))
	}

	// Execute query
	result, err := s.activityLog.Query(r.Context(), query)
	if err != nil {
		s.log.Error("failed to query activity logs", zap.Error(err))
		http.Error(w, fmt.Sprintf("query failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonOK(w, result)
}

// handleGetTrace handles GET /api/v1/activity/trace/{trace_id} - Get trace chain.
func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	traceID := r.PathValue("trace_id")
	if traceID == "" {
		http.Error(w, "trace_id is required", http.StatusBadRequest)
		return
	}

	// For now, return empty result
	// In full implementation, this would query storage by trace_id
	result := []activitylog.LogEntry{}

	jsonOK(w, map[string]interface{}{
		"trace_id": traceID,
		"entries":  result,
		"count":    len(result),
	})
}

// handleGetMetrics handles GET /api/v1/activity/metrics - Get aggregated metrics.
func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	// Parse time range from query params
	start, end := parseTimeRange(r)

	// For now, return mock metrics
	// In full implementation, this would aggregate from storage
	metrics := MetricsResponse{
		TotalEvents:    0,
		EventsByType:   make(map[string]int64),
		EventsBySymbol: make(map[string]int64),
		TimeRange: activitylog.TimeRange{
			Start: start,
			End:   end,
		},
	}

	jsonOK(w, metrics)
}

// handleExport handles POST /api/v1/activity/export - Export logs.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate format
	if req.Format != "json" && req.Format != "csv" {
		http.Error(w, "format must be 'json' or 'csv'", http.StatusBadRequest)
		return
	}

	// For now, return empty export
	// In full implementation, this would query storage and stream results

	if req.Format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=activity_export.csv")
		writer := csv.NewWriter(w)
		writer.Write([]string{"timestamp", "event_type", "severity", "symbol", "trace_id", "payload"})
		writer.Flush()
		return
	}

	// JSON format
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=activity_export.json")
	json.NewEncoder(w).Encode([]activitylog.LogEntry{})
}

// handleGetSummary handles GET /api/v1/activity/summary - Get daily summary.
func (s *Server) handleGetSummary(w http.ResponseWriter, r *http.Request) {
	// Parse date parameter (default to today)
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}

	// For now, return mock summary
	// In full implementation, this would aggregate from storage
	summary := SummaryResponse{
		Date:             dateStr,
		TotalEvents:      0,
		OrderCount:       0,
		OrdersFilled:     0,
		OrdersFailed:     0,
		RealizedPnL:      0,
		UnrealizedPnL:    0,
		ActiveStrategies: 0,
	}

	jsonOK(w, summary)
}

// parseActivityQuery parses query parameters from the request.
func parseActivityQuery(r *http.Request) ActivityQueryRequest {
	req := ActivityQueryRequest{
		Start:     time.Now().Add(-24 * time.Hour),
		End:       time.Now(),
		Limit:     100,
		Offset:    0,
		SortOrder: "DESC",
	}

	// Parse time range
	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			req.Start = t
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			req.End = t
		}
	}

	// Parse pagination
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 1000 {
			req.Limit = n
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			req.Offset = n
		}
	}

	// Parse arrays
	req.EventTypes = r.URL.Query()["event_types"]
	req.Symbols = r.URL.Query()["symbols"]
	req.Strategies = r.URL.Query()["strategies"]
	req.Severities = r.URL.Query()["severities"]

	// Parse other params
	req.FullText = r.URL.Query().Get("q")
	if sort := r.URL.Query().Get("sort"); sort == "ASC" || sort == "DESC" {
		req.SortOrder = sort
	}

	return req
}

// parseTimeRange parses time range from query parameters.
func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	return start, end
}
