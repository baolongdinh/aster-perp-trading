package handlers

import (
	"encoding/json"
	"net/http"

	"aster-bot/internal/risk"
)

// PositionBySymbol represents position info per symbol
type PositionBySymbol struct {
	Count    int     `json:"count"`
	Notional float64 `json:"notional"`
}

// RiskResponse represents risk metrics
type RiskResponse struct {
	DailyPnL            float64                     `json:"daily_pnl"`
	DailyPnLPct         float64                     `json:"daily_pnl_pct"`
	DailyStartingEquity float64                     `json:"daily_starting_equity"`
	AvailableBalance    float64                     `json:"available_balance"`
	PendingMargin       float64                     `json:"pending_margin"`
	TotalNotional       float64                     `json:"total_notional"`
	MaxTotalNotional    float64                     `json:"max_total_notional"`
	IsPaused            bool                        `json:"is_paused"`
	PauseReason         string                      `json:"pause_reason,omitempty"`
	OpenPositions       int                         `json:"open_positions"`
	MaxOpenPositions    int                         `json:"max_open_positions"`
	PositionsBySymbol   map[string]PositionBySymbol `json:"positions_by_symbol"`
}

// HandleRisk returns risk metrics and limits
func HandleRisk(riskMgr *risk.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := riskMgr.GetConfig()

		// Get data from risk manager
		dailyPnL := riskMgr.DailyPnL()
		startingEquity := riskMgr.GetDailyStartingEquity()
		availableBalance := riskMgr.GetAvailableBalance()
		pendingMargin := riskMgr.GetPendingMargin()
		isPaused := riskMgr.IsPaused()
		openPositions := riskMgr.OpenPositions()

		// Get symbol notionals
		symNotional, totalNotional := riskMgr.GetSymNotional()

		// Calculate daily PnL percentage
		dailyPnLPct := 0.0
		if startingEquity > 0 {
			dailyPnLPct = (dailyPnL / startingEquity) * 100
		}

		// Build positions by symbol
		positionsBySymbol := make(map[string]PositionBySymbol)
		for sym, notional := range symNotional {
			positionsBySymbol[sym] = PositionBySymbol{
				Count:    1, // Simplified - actual count would need more tracking
				Notional: notional,
			}
		}

		// Determine pause reason
		pauseReason := ""
		if isPaused {
			if dailyPnL <= -cfg.DailyLossLimitUSDT {
				pauseReason = "Daily loss limit reached"
			} else if startingEquity > 0 {
				drawdown := (dailyPnL / startingEquity) * 100
				if drawdown <= -cfg.DailyDrawdownPct {
					pauseReason = "Daily drawdown limit reached"
				}
			}
		}

		response := RiskResponse{
			DailyPnL:            dailyPnL,
			DailyPnLPct:         dailyPnLPct,
			DailyStartingEquity: startingEquity,
			AvailableBalance:    availableBalance,
			PendingMargin:       pendingMargin,
			TotalNotional:       totalNotional,
			MaxTotalNotional:    cfg.MaxTotalPositionsUSDT,
			IsPaused:            isPaused,
			PauseReason:         pauseReason,
			OpenPositions:       openPositions,
			MaxOpenPositions:    cfg.MaxOpenPositions,
			PositionsBySymbol:   positionsBySymbol,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
