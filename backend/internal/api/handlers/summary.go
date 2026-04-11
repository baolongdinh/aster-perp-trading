package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"aster-bot/internal/risk"
)

// TradeStats represents trade statistics
type TradeStats struct {
	Total    int     `json:"total"`
	Winning  int     `json:"winning"`
	Losing   int     `json:"losing"`
	WinRate  float64 `json:"win_rate"`
}

// OrderStats represents order statistics
type OrderStats struct {
	Placed    int `json:"placed"`
	Filled    int `json:"filled"`
	Cancelled int `json:"cancelled"`
	Rejected  int `json:"rejected"`
}

// SummaryResponse represents daily summary
type SummaryResponse struct {
	Date              string     `json:"date"`
	StartingEquity    float64    `json:"starting_equity"`
	CurrentEquity     float64    `json:"current_equity"`
	TotalPnL          float64    `json:"total_pnl"`
	TotalPnLPct       float64    `json:"total_pnl_pct"`
	Trades            TradeStats `json:"trades"`
	Orders            OrderStats `json:"orders"`
	Volume            float64    `json:"volume"`
	StrategiesActive  int        `json:"strategies_active"`
}

// HandleSummary returns daily summary
func HandleSummary(riskMgr *risk.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get date parameter (default to today)
		dateStr := r.URL.Query().Get("date")
		if dateStr == "" {
			dateStr = time.Now().Format("2006-01-02")
		}

		// Get data from risk manager
		startingEquity := riskMgr.GetDailyStartingEquity()
		dailyPnL := riskMgr.DailyPnL()

		// Calculate derived values
		currentEquity := startingEquity + dailyPnL
		totalPnLPct := 0.0
		if startingEquity > 0 {
			totalPnLPct = (dailyPnL / startingEquity) * 100
		}

		// TODO: Get actual trade/order/volume data from activity log
		response := SummaryResponse{
			Date:           dateStr,
			StartingEquity: startingEquity,
			CurrentEquity:  currentEquity,
			TotalPnL:       dailyPnL,
			TotalPnLPct:    totalPnLPct,
			Trades: TradeStats{
				Total:   0,
				Winning: 0,
				Losing:  0,
				WinRate: 0,
			},
			Orders: OrderStats{
				Placed:    0,
				Filled:    0,
				Cancelled: 0,
				Rejected:  0,
			},
			Volume:           0,
			StrategiesActive: 0, // TODO: Get from engine
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
