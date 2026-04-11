package handlers

import (
	"encoding/json"
	"net/http"
)

// GridConfig represents a grid configuration
type GridConfig struct {
	Symbol        string  `json:"symbol"`
	Status        string  `json:"status"`
	Levels        int     `json:"levels"`
	SpreadPct     float64 `json:"spread_pct"`
	PositionSize  float64 `json:"position_size"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	Volume24h     float64 `json:"volume_24h"`
}

// FarmingResponse represents volume farming metrics
type FarmingResponse struct {
	ActiveGrids         int          `json:"active_grids"`
	TotalGrids          int          `json:"total_grids"`
	Volume24h           float64      `json:"volume_24h"`
	Volume7d            float64      `json:"volume_7d"`
	EstimatedFunding24h float64      `json:"estimated_funding_24h"`
	GridConfigs         []GridConfig `json:"grid_configs"`
}

// HandleFarming returns volume farming metrics
// TODO: Wire with farming manager when available
func HandleFarming() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Placeholder - return empty data until farming manager is wired
		response := FarmingResponse{
			ActiveGrids:         0,
			TotalGrids:          0,
			Volume24h:           0,
			Volume7d:            0,
			EstimatedFunding24h: 0,
			GridConfigs:         []GridConfig{},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
