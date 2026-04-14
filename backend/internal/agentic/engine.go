package agentic

import (
	"context"

	"fmt"

	"sync"

	"time"

	"aster-bot/internal/client"

	"aster-bot/internal/config"

	"go.uber.org/zap"
)

// AgenticEngine is the decision layer that controls the Volume Farm execution

type AgenticEngine struct {
	config *config.AgenticConfig

	marketClient *client.MarketClient

	vfController VFWhitelistController

	logger *zap.Logger

	// Components

	detectors map[string]*RegimeDetector

	scorer *OpportunityScorer

	whitelistManager *WhitelistManager

	positionSizer *PositionSizer

	circuitBreaker *CircuitBreaker

	// NEW: Telegram notifier for alerts

	alertManager AlertManager

	// State

	currentScores map[string]SymbolScore

	mu sync.RWMutex

	isRunning bool

	stopCh chan struct{}

	wg sync.WaitGroup

	lastDetection time.Time

	detectionCount int
}

// NewAgenticEngine creates a new agentic decision engine

func NewAgenticEngine(

	cfg *config.AgenticConfig,

	httpClient *client.HTTPClient,

	vfController VFWhitelistController,

	logger *zap.Logger,

) (*AgenticEngine, error) {

	if cfg == nil {

		return nil, fmt.Errorf("agentic config is nil")

	}

	marketClient := client.NewMarketClient(httpClient)

	engine := &AgenticEngine{

		config: cfg,

		marketClient: marketClient,

		vfController: vfController,

		logger: logger.With(zap.String("component", "agentic_engine")),

		detectors: make(map[string]*RegimeDetector),

		currentScores: make(map[string]SymbolScore),

		stopCh: make(chan struct{}),

		alertManager: NewTelegramAlertManager(logger),
	}

	// Initialize scorer

	engine.scorer = NewOpportunityScorer(cfg.Scoring)

	// Initialize whitelist manager

	engine.whitelistManager = NewWhitelistManager(cfg.WhitelistManagement, vfController, logger)

	// Initialize position sizer

	engine.positionSizer = NewPositionSizer(cfg.PositionSizing)

	// Initialize circuit breaker

	engine.circuitBreaker = NewCircuitBreaker(cfg.CircuitBreakers, logger)

	// Initialize detectors for each symbol in universe

	for _, symbol := range cfg.Universe.Symbols {

		detector := NewRegimeDetector(symbol, cfg.RegimeDetection, marketClient, logger)

		engine.detectors[symbol] = detector

	}

	updateInterval, _ := time.ParseDuration(cfg.RegimeDetection.UpdateInterval)

	if updateInterval <= 0 {

		updateInterval = 30 * time.Second

	}

	logger.Info("AgenticEngine created",

		zap.Int("symbols", len(cfg.Universe.Symbols)),

		zap.Duration("update_interval", updateInterval),
	)

	return engine, nil

}

// Start starts the agentic engine

func (ae *AgenticEngine) Start(ctx context.Context) error {

	ae.mu.Lock()

	if ae.isRunning {

		ae.mu.Unlock()

		return fmt.Errorf("agentic engine is already running")

	}

	ae.isRunning = true

	ae.mu.Unlock()

	ae.logger.Info("Starting AgenticEngine")

	// Initial detection run

	if err := ae.runDetectionCycle(ctx); err != nil {

		ae.logger.Warn("Initial detection cycle failed", zap.Error(err))

	}

	// Send startup notification

	if ae.alertManager != nil && ae.alertManager.IsEnabled() {

		for symbol := range ae.detectors {

			if err := ae.alertManager.SendStartup(symbol); err != nil {

				ae.logger.Warn("Failed to send startup notification", zap.Error(err))

			}

			break // Only notify for first symbol

		}

	}

	// Start detection loop

	ae.wg.Add(1)

	go ae.detectionLoop(ctx)

	ae.logger.Info("AgenticEngine started successfully")

	return nil

}

// Stop stops the agentic engine

func (ae *AgenticEngine) Stop() error {

	ae.mu.Lock()

	if !ae.isRunning {

		ae.mu.Unlock()

		return nil

	}

	ae.isRunning = false

	ae.mu.Unlock()

	ae.logger.Info("Stopping AgenticEngine")

	// Signal stop

	close(ae.stopCh)

	// Wait for goroutines to finish with timeout

	done := make(chan struct{})

	go func() {

		ae.wg.Wait()

		close(done)

	}()

	select {

	case <-done:

		ae.logger.Info("AgenticEngine stopped gracefully")

	case <-time.After(10 * time.Second):

		ae.logger.Warn("AgenticEngine stop timeout")

	}

	return nil

}

// detectionLoop runs the periodic detection cycle

func (ae *AgenticEngine) detectionLoop(ctx context.Context) {

	defer ae.wg.Done()

	// Parse interval from config string

	interval, err := time.ParseDuration(ae.config.RegimeDetection.UpdateInterval)

	if err != nil || interval <= 0 {

		interval = 30 * time.Second

	}

	ticker := time.NewTicker(interval)

	defer ticker.Stop()

	ae.logger.Info("Detection loop started", zap.Duration("interval", interval))

	for {

		select {

		case <-ctx.Done():

			ae.logger.Info("Detection loop stopped (context done)")

			return

		case <-ae.stopCh:

			ae.logger.Info("Detection loop stopped (stop signal)")

			return

		case <-ticker.C:

			if err := ae.runDetectionCycle(ctx); err != nil {

				ae.logger.Error("Detection cycle failed", zap.Error(err))

			}

		}

	}

}

// runDetectionCycle performs one full detection and whitelist update

func (ae *AgenticEngine) runDetectionCycle(ctx context.Context) error {

	start := time.Now()

	ae.logger.Debug("Starting detection cycle")

	// 1. Detect regime for all symbols (parallel)

	detectionResults := ae.detectAllSymbols(ctx)

	// 2. Calculate scores

	scores := ae.calculateScores(detectionResults)

	// 3. Check circuit breaker

	if tripped, reason := ae.circuitBreaker.Check(scores); tripped {

		ae.logger.Warn("Circuit breaker is active, skipping whitelist update",

			zap.String("reason", reason),
		)

		// Still store scores for monitoring

		ae.mu.Lock()

		ae.currentScores = scores

		ae.lastDetection = time.Now()

		ae.detectionCount++

		ae.mu.Unlock()

		return nil

	}

	// 4. Update whitelist (only if enabled)

	if ae.config.WhitelistManagement.Enabled {

		if err := ae.whitelistManager.UpdateWhitelist(ctx, scores); err != nil {

			return fmt.Errorf("failed to update whitelist: %w", err)

		}

	} else {

		ae.logger.Debug("Whitelist management disabled, using VF whitelist")

	}

	// 5. Store scores

	ae.mu.Lock()

	ae.currentScores = scores

	ae.lastDetection = time.Now()

	ae.detectionCount++

	ae.mu.Unlock()

	duration := time.Since(start)

	ae.logger.Info("Detection cycle completed",

		zap.Int("symbols_checked", len(detectionResults)),

		zap.Int("scores_calculated", len(scores)),

		zap.Duration("duration", duration),
	)

	return nil

}

// detectAllSymbols detects regime for all symbols in parallel

func (ae *AgenticEngine) detectAllSymbols(ctx context.Context) map[string]RegimeSnapshot {

	results := make(map[string]RegimeSnapshot)

	var mu sync.Mutex

	var wg sync.WaitGroup

	// Limit concurrent detections

	semaphore := make(chan struct{}, 5)

	for symbol, detector := range ae.detectors {

		wg.Add(1)

		go func(sym string, det *RegimeDetector) {

			defer wg.Done()

			semaphore <- struct{}{}

			defer func() { <-semaphore }()

			// Detect with timeout

			detectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

			defer cancel()

			regime, err := det.Detect(detectCtx)

			if err != nil {

				ae.logger.Warn("Failed to detect regime",

					zap.String("symbol", sym),

					zap.Error(err),
				)

				return

			}

			mu.Lock()

			results[sym] = regime

			mu.Unlock()

		}(symbol, detector)

	}

	wg.Wait()

	return results

}

// calculateScores calculates opportunity scores based on detection results

func (ae *AgenticEngine) calculateScores(detections map[string]RegimeSnapshot) map[string]SymbolScore {

	scores := make(map[string]SymbolScore)

	for symbol, regime := range detections {

		// Get indicator values from detector

		detector := ae.detectors[symbol]

		var values *IndicatorValues

		if detector != nil {

			// Create values from regime snapshot

			values = &IndicatorValues{

				ADX: regime.ADX,

				ATR14: regime.ATR14,

				BBWidth: regime.BBWidth,

				Volume24h: regime.Volume24h,
			}

		}

		// Calculate score

		score := ae.scorer.CalculateScore(regime, values)

		recommendation := ae.scorer.CalculateRecommendation(score)

		factors := ae.scorer.GetFactorBreakdown(regime, values)

		scores[symbol] = SymbolScore{

			Symbol: symbol,

			Score: score,

			Regime: regime.Regime,

			Confidence: regime.Confidence,

			Factors: factors,

			LastUpdated: time.Now(),

			Recommendation: recommendation,

			RawADX: regime.ADX,

			RawATR14: regime.ATR14,

			RawBBWidth: regime.BBWidth,
		}

	}

	return scores

}

// GetCurrentScores returns the current symbol scores

func (ae *AgenticEngine) GetCurrentScores() map[string]SymbolScore {

	ae.mu.RLock()

	defer ae.mu.RUnlock()

	scores := make(map[string]SymbolScore, len(ae.currentScores))

	for k, v := range ae.currentScores {

		scores[k] = v

	}

	return scores

}

// GetWhitelistManager returns the whitelist manager

func (ae *AgenticEngine) GetWhitelistManager() *WhitelistManager {

	return ae.whitelistManager

}

// GetPositionSizer returns the position sizer

func (ae *AgenticEngine) GetPositionSizer() *PositionSizer {

	return ae.positionSizer

}

// GetCircuitBreaker returns the circuit breaker

func (ae *AgenticEngine) GetCircuitBreaker() *CircuitBreaker {

	return ae.circuitBreaker

}

// GetCircuitBreakerStatus returns the circuit breaker status

func (ae *AgenticEngine) GetCircuitBreakerStatus() map[string]interface{} {

	return ae.circuitBreaker.GetStatus()

}

// IsRunning returns whether the engine is running

func (ae *AgenticEngine) IsRunning() bool {

	ae.mu.RLock()

	defer ae.mu.RUnlock()

	return ae.isRunning

}

// GetLastDetection returns the time of last detection

func (ae *AgenticEngine) GetLastDetection() time.Time {

	ae.mu.RLock()

	defer ae.mu.RUnlock()

	return ae.lastDetection

}

// GetDetectionCount returns the number of detection cycles

func (ae *AgenticEngine) GetDetectionCount() int {

	ae.mu.RLock()

	defer ae.mu.RUnlock()

	return ae.detectionCount

}
