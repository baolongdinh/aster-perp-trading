package handlers

import (
	"encoding/json"
	"net/http"

	"aster-bot/internal/risk"
)

// AccountResponse represents account/wallet information
type AccountResponse struct {
	Balance          float64 `json:"balance"`
	Equity           float64 `json:"equity"`
	MarginUsed       float64 `json:"margin_used"`
	MarginRatio      float64 `json:"margin_ratio"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	RealizedPnLToday float64 `json:"realized_pnl_today"`
}

// HandleAccount returns account/wallet information
func HandleAccount(riskMgr *risk.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get data from risk manager
		availableBalance := riskMgr.GetAvailableBalance()
		startingEquity := riskMgr.GetDailyStartingEquity()
		dailyPnL := riskMgr.DailyPnL()

		// Calculate derived values
		// Note: In a real implementation, these would come from exchange account info
		// For now, we estimate based on available data
		equity := startingEquity + dailyPnL
		if equity == 0 {
			equity = availableBalance // Fallback
		}

		marginUsed := 0.0 // Would come from exchange
		marginRatio := 0.0
		if equity > 0 {
			marginRatio = (marginUsed / equity) * 100
		}

		response := AccountResponse{
			Balance:          availableBalance,
			Equity:           equity,
			MarginUsed:       marginUsed,
			MarginRatio:      marginRatio,
			UnrealizedPnL:    0, // Would need position data
			RealizedPnLToday: dailyPnL,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
