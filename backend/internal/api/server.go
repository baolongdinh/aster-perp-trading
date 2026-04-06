// Package api provides the internal HTTP REST + WebSocket API for the frontend dashboard.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"aster-bot/internal/activitylog"
	"aster-bot/internal/engine"
	"aster-bot/internal/risk"

	"go.uber.org/zap"
)

// Server is the HTTP API server.
type Server struct {
	engine      *engine.Engine
	risk        *risk.Manager
	activityLog *activitylog.ActivityLogger
	hub         *WsHub
	log         *zap.Logger
	mux         *http.ServeMux
}

// NewServer creates a new API server.
func NewServer(e *engine.Engine, r *risk.Manager, al *activitylog.ActivityLogger, log *zap.Logger) *Server {
	s := &Server{
		engine:      e,
		risk:        r,
		activityLog: al,
		hub:         NewWsHub(log),
		log:         log,
		mux:         http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// Handler returns the HTTP handler (for use with http.ListenAndServe).
func (s *Server) Handler() http.Handler {
	return corsMiddleware(s.mux)
}

// Hub returns the WS hub (for pushing events from engine).
func (s *Server) Hub() *WsHub { return s.hub }

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/v1/positions", s.handlePositions)
	s.mux.HandleFunc("GET /api/v1/orders", s.handleOrders)
	s.mux.HandleFunc("GET /api/v1/strategies", s.handleStrategies)
	s.mux.HandleFunc("POST /api/v1/strategies/{name}/enable", s.handleEnableStrategy)
	s.mux.HandleFunc("POST /api/v1/strategies/{name}/disable", s.handleDisableStrategy)
	s.mux.HandleFunc("GET /api/v1/metrics", s.handleMetrics)
	s.registerActivityRoutes()
	s.mux.HandleFunc("GET /ws", s.hub.ServeWS)
}

// handleStatus returns bot/engine status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]interface{}{
		"running":     s.engine.IsRunning(),
		"paused":      s.risk.IsPaused(),
		"daily_pnl":   s.risk.DailyPnL(),
		"open_pos":    s.risk.OpenPositions(),
		"server_time": time.Now().UnixMilli(),
	})
}

// handlePositions returns current open positions.
func (s *Server) handlePositions(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, s.engine.Positions())
}

// handleOrders returns open orders from the order manager.
func (s *Server) handleOrders(w http.ResponseWriter, r *http.Request) {
	// TODO: wire order manager reference here
	jsonOK(w, []interface{}{})
}

// handleStrategies returns strategy list + status.
func (s *Server) handleStrategies(w http.ResponseWriter, r *http.Request) {
	type stratInfo struct {
		Name    string   `json:"name"`
		Enabled bool     `json:"enabled"`
		Symbols []string `json:"symbols"`
	}
	var out []stratInfo
	for _, st := range s.engine.Strategies() {
		out = append(out, stratInfo{
			Name:    st.Name(),
			Enabled: st.IsEnabled(),
			Symbols: st.Symbols(),
		})
	}
	jsonOK(w, out)
}

// handleEnableStrategy enables a strategy by name.
func (s *Server) handleEnableStrategy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	for _, st := range s.engine.Strategies() {
		if st.Name() == name {
			st.SetEnabled(true)
			s.log.Info("strategy enabled via API", zap.String("name", name))
			jsonOK(w, map[string]string{"status": "enabled", "name": name})
			return
		}
	}
	http.Error(w, fmt.Sprintf("strategy %q not found", name), http.StatusNotFound)
}

// handleDisableStrategy disables a strategy by name.
func (s *Server) handleDisableStrategy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	for _, st := range s.engine.Strategies() {
		if st.Name() == name {
			st.SetEnabled(false)
			s.log.Info("strategy disabled via API", zap.String("name", name))
			jsonOK(w, map[string]string{"status": "disabled", "name": name})
			return
		}
	}
	http.Error(w, fmt.Sprintf("strategy %q not found", name), http.StatusNotFound)
}

// handleMetrics returns P&L metrics.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]interface{}{
		"daily_pnl":      s.risk.DailyPnL(),
		"open_positions": s.risk.OpenPositions(),
		"is_paused":      s.risk.IsPaused(),
	})
}

// --- Helpers ---

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
