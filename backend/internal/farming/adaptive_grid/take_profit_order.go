package adaptive_grid

import (
	"fmt"
	"time"
)

// TakeProfitStatus represents the status of a take profit order
type TakeProfitStatus int

const (
	TakeProfitStatusPending   TakeProfitStatus = iota // Order placed, waiting for fill
	TakeProfitStatusFilled                            // Order filled successfully
	TakeProfitStatusCancelled                          // Order cancelled manually
	TakeProfitStatusTimeout                            // Order expired without fill
)

// String returns the string representation of the status
func (s TakeProfitStatus) String() string {
	switch s {
	case TakeProfitStatusPending:
		return "PENDING"
	case TakeProfitStatusFilled:
		return "FILLED"
	case TakeProfitStatusCancelled:
		return "CANCELLED"
	case TakeProfitStatusTimeout:
		return "TIMEOUT"
	default:
		return "UNKNOWN"
	}
}

// TakeProfitOrder represents a take profit order placed for a specific position
type TakeProfitOrder struct {
	OrderID       string            `json:"order_id"`
	Symbol        string            `json:"symbol"`
	Side          string            `json:"side"`          // "BUY" or "SELL"
	Price         float64           `json:"price"`         // Take profit price
	Size          float64           `json:"size"`          // Order size (quantity)
	ParentOrderID string            `json:"parent_order_id"` // ID of the filled grid order
	Status        TakeProfitStatus `json:"status"`
	CreatedAt     time.Time         `json:"created_at"`
	FilledAt      *time.Time        `json:"filled_at,omitempty"`
	TimeoutAt     *time.Time        `json:"timeout_at,omitempty"`
}

// CalculateProfit calculates the profit amount from this take profit order
// Returns profit in USDT
func (t *TakeProfitOrder) CalculateProfit(fillPrice float64) (float64, error) {
	if t.Status != TakeProfitStatusFilled {
		return 0, fmt.Errorf("cannot calculate profit for non-filled order (status: %s)", t.Status)
	}

	// Profit calculation depends on side
	if t.Side == "BUY" {
		// BUY take profit: we sold at higher price, profit = (fillPrice - entryPrice) * size
		// For take profit, entryPrice is the parent order price
		return (fillPrice - t.Price) * t.Size, nil
	} else if t.Side == "SELL" {
		// SELL take profit: we bought at lower price, profit = (entryPrice - fillPrice) * size
		return (t.Price - fillPrice) * t.Size, nil
	}

	return 0, fmt.Errorf("invalid side: %s", t.Side)
}

// IsExpired checks if the take profit order has expired based on timeout
func (t *TakeProfitOrder) IsExpired() bool {
	if t.TimeoutAt == nil {
		return false
	}
	return time.Now().After(*t.TimeoutAt)
}

// IsPending checks if the take profit order is still pending
func (t *TakeProfitOrder) IsPending() bool {
	return t.Status == TakeProfitStatusPending
}

// IsFilled checks if the take profit order has been filled
func (t *TakeProfitOrder) IsFilled() bool {
	return t.Status == TakeProfitStatusFilled
}

// MarkFilled marks the order as filled
func (t *TakeProfitOrder) MarkFilled(fillPrice float64) {
	t.Status = TakeProfitStatusFilled
	now := time.Now()
	t.FilledAt = &now
}

// MarkCancelled marks the order as cancelled
func (t *TakeProfitOrder) MarkCancelled() {
	t.Status = TakeProfitStatusCancelled
}

// MarkTimeout marks the order as timed out
func (t *TakeProfitOrder) MarkTimeout() {
	t.Status = TakeProfitStatusTimeout
}
