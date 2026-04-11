package handlers

import (
	"encoding/json"
	"net/http"
)

// Order represents an open order
type Order struct {
	OrderID   string  `json:"order_id"`
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Type      string  `json:"type"`
	Price     float64 `json:"price"`
	Quantity  float64 `json:"quantity"`
	FilledQty float64 `json:"filled_qty"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
}

// OrdersResponse represents open orders
type OrdersResponse struct {
	Orders          []Order `json:"orders"`
	TotalOrders     int     `json:"total_orders"`
	PendingNotional float64 `json:"pending_notional"`
}

// HandleOrders returns open orders
// TODO: Wire with order manager when available
func HandleOrders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Placeholder - return empty list until order manager is wired
		response := OrdersResponse{
			Orders:          []Order{},
			TotalOrders:     0,
			PendingNotional: 0,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
