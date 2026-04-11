package agentic

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// StatusServer provides HTTP endpoints for monitoring the agentic engine
type StatusServer struct {
	engine *AgenticEngine
	logger *zap.Logger
	port   int
}

// NewStatusServer creates a new status server
func NewStatusServer(engine *AgenticEngine, port int, logger *zap.Logger) *StatusServer {
	return &StatusServer{
		engine: engine,
		logger: logger.With(zap.String("component", "status_server")),
		port:   port,
	}
}

// Start starts the status server
func (s *StatusServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/scores", s.handleScores)
	mux.HandleFunc("/whitelist", s.handleWhitelist)
	mux.HandleFunc("/circuit", s.handleCircuitBreaker)

	s.logger.Info("Starting status server", zap.Int("port", s.port))
	go func() {
		if err := http.ListenAndServe(":8082", mux); err != nil {
			s.logger.Error("Status server error", zap.Error(err))
		}
	}()
	return nil
}

// handleHealth returns health check status
func (s *StatusServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"running":   s.engine.IsRunning(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleStatus returns detailed engine status
func (s *StatusServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"running":          s.engine.IsRunning(),
		"last_detection":   s.engine.GetLastDetection(),
		"detection_count":  s.engine.GetDetectionCount(),
		"circuit_breaker":  s.engine.GetCircuitBreakerStatus(),
		"timestamp":        time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleScores returns current symbol scores
func (s *StatusServer) handleScores(w http.ResponseWriter, r *http.Request) {
	scores := s.engine.GetCurrentScores()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scores)
}

// handleWhitelist returns current whitelist
func (s *StatusServer) handleWhitelist(w http.ResponseWriter, r *http.Request) {
	whitelist := s.engine.GetWhitelistManager().GetCurrentWhitelist()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"whitelist": whitelist,
		"count":     len(whitelist),
	})
}

// handleCircuitBreaker returns circuit breaker status
func (s *StatusServer) handleCircuitBreaker(w http.ResponseWriter, r *http.Request) {
	status := s.engine.GetCircuitBreakerStatus()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
