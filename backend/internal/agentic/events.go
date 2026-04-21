package agentic

import "time"

// OrderPlacementEvent represents an order placement request from Agentic
type OrderPlacementEvent struct {
	Symbol    string
	OrderType string // "grid", "trend", "accumulation", "defensive"
	Side      string // "BUY", "SELL"
	Price     float64
	Size      float64
	Reason    string
	Timestamp time.Time
	RequestID string
}

// OrderCancellationEvent represents an order cancellation request from Agentic
type OrderCancellationEvent struct {
	Symbol    string
	OrderID   string
	Reason    string
	Timestamp time.Time
	RequestID string
}

// OrderExecutionResult represents the result of an order execution
type OrderExecutionResult struct {
	Symbol    string
	OrderID   string
	Success   bool
	Error     string
	Timestamp time.Time
}
