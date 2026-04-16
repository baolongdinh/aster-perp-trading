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

	// NEW: Adaptive State Management Components (Phase 2-10)
	scoreEngine         *ScoreCalculationEngine
	decisionEngine      *DecisionEngine
	idleHandler         *IdleStateHandler
	waitRangeHandler    *WaitRangeStateHandler
	enterGridHandler    *EnterGridStateHandler
	tradingGridHandler  *TradingGridStateHandler
	trendingHandler     *TrendingStateHandler
	accumulationHandler *AccumulationStateHandler
	defensiveHandler    *DefensiveStateHandler
	overSizeHandler     *OverSizeStateHandler
	recoveryHandler     *RecoveryStateHandler
	eventPublisher      *EventPublisher

	// NEW: Hybrid integration - Event-driven communication with VF
	stateEventBus *StateEventBus
	vfBridge      *AgenticVFBridge

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
	engine.circuitBreaker.Start() // Start evaluation worker

	// NEW: Initialize adaptive state management components (Phase 2-10)
	if cfg.ScoreEngine.Enabled {
		engine.scoreEngine = NewScoreCalculationEngine(&cfg.ScoreEngine, logger)
		engine.decisionEngine = NewDecisionEngine(nil, engine.scoreEngine, logger)
		engine.idleHandler = NewIdleStateHandler(engine.scoreEngine, logger)
		engine.waitRangeHandler = NewWaitRangeStateHandler(engine.scoreEngine, logger)
		engine.enterGridHandler = NewEnterGridStateHandler(engine.scoreEngine, logger)
		engine.tradingGridHandler = NewTradingGridStateHandler(engine.scoreEngine, logger)
		engine.trendingHandler = NewTrendingStateHandler(engine.scoreEngine, logger)
		engine.accumulationHandler = NewAccumulationStateHandler(engine.scoreEngine, logger)
		engine.defensiveHandler = NewDefensiveStateHandler(engine.scoreEngine, logger)
		engine.overSizeHandler = NewOverSizeStateHandler(engine.scoreEngine, logger)
		engine.recoveryHandler = NewRecoveryStateHandler(engine.scoreEngine, logger)
		engine.eventPublisher = NewEventPublisher(logger)

		// Wire event publisher to decision engine
		eventCh := make(chan StateTransition, 100)
		engine.eventPublisher.Subscribe(eventCh)
		engine.decisionEngine.SubscribeToTransitions(eventCh)

		// NEW: Initialize hybrid event-driven integration (Option 3)
		engine.stateEventBus = NewStateEventBus(logger)
		engine.vfBridge = NewAgenticVFBridge(engine.stateEventBus, logger)

		// VF handler will be subscribed externally when VFEngine is available
		logger.Info("Hybrid state-execution integration initialized",
			zap.Bool("state_event_bus", engine.stateEventBus != nil),
			zap.Bool("vf_bridge", engine.vfBridge != nil),
		)

		logger.Info("Adaptive state management initialized",
			zap.Bool("score_engine", cfg.ScoreEngine.Enabled),
			zap.Bool("idle_handler", engine.idleHandler != nil),
			zap.Bool("wait_range_handler", engine.waitRangeHandler != nil),
			zap.Bool("enter_grid_handler", engine.enterGridHandler != nil),
			zap.Bool("trading_grid_handler", engine.tradingGridHandler != nil),
			zap.Bool("trending_handler", engine.trendingHandler != nil),
			zap.Bool("accumulation_handler", engine.accumulationHandler != nil),
			zap.Bool("defensive_handler", engine.defensiveHandler != nil),
			zap.Bool("over_size_handler", engine.overSizeHandler != nil),
			zap.Bool("recovery_handler", engine.recoveryHandler != nil),
		)
	}

	// Set callback to trigger emergency exit when circuit breaker trips for a symbol
	if vfController != nil {
		engine.circuitBreaker.SetOnTripCallback(func(symbol, reason string) {
			engine.logger.Error("Circuit breaker tripped for symbol, triggering emergency exit",
				zap.String("symbol", symbol),
				zap.String("reason", reason))
			if err := vfController.TriggerEmergencyExit(reason); err != nil {
				engine.logger.Error("Failed to trigger emergency exit",
					zap.String("symbol", symbol),
					zap.String("reason", reason),
					zap.Error(err))
			}
		})

		// Set callback to trigger force placement when circuit breaker resets for a symbol
		engine.circuitBreaker.SetOnResetCallback(func(symbol string) {
			engine.logger.Info("Circuit breaker reset for symbol, triggering force placement",
				zap.String("symbol", symbol))
			if err := vfController.TriggerForcePlacement(); err != nil {
				engine.logger.Error("Failed to trigger force placement",
					zap.String("symbol", symbol),
					zap.Error(err))
			}
		})
	}

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

	// Stop circuit breaker evaluation worker
	ae.circuitBreaker.Stop()

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

	// NEW: 2.5 Run adaptive state management (Phase 2+3)
	if ae.idleHandler != nil {
		ae.runStateManagement(ctx, detectionResults)
	}

	// 2.5 Update circuit breaker with market conditions for dynamic reset
	// For now, use default values - TODO: wire with actual detector data
	for symbol := range detectionResults {
		// Update with placeholder data - will be improved later
		ae.circuitBreaker.UpdateMarketConditions(symbol, 0.01, 0.0, 0.0, 0.0)
	}

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

	status := ae.circuitBreaker.GetStatus()
	result := make(map[string]interface{})
	for k, v := range status {
		result[k] = v
	}
	return result

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

// GetStateEventBus returns the state event bus for hybrid integration
func (ae *AgenticEngine) GetStateEventBus() *StateEventBus {
	return ae.stateEventBus
}

// runStateManagement executes adaptive state management for all symbols
// This is the core integration point for state-based trading
func (ae *AgenticEngine) runStateManagement(ctx context.Context, detections map[string]RegimeSnapshot) {
	for symbol, regime := range detections {
		// Only process if we have a decision engine and idle handler
		if ae.decisionEngine == nil || ae.idleHandler == nil {
			continue
		}

		// Get current state for this symbol
		currentState, ok := ae.decisionEngine.GetSymbolState(symbol)
		if !ok {
			// New symbol, start in IDLE
			currentState = &SymbolTradingState{
				Symbol:      symbol,
				CurrentMode: TradingModeIdle,
				ModeScores:  make(map[TradingMode]*TradingModeScore),
			}
		}

		// Execute state-specific logic
		switch currentState.CurrentMode {
		case TradingModeIdle:
			// Handle IDLE state
			transition, err := ae.idleHandler.HandleState(ctx, symbol, regime)
			if err != nil {
				ae.logger.Error("IDLE state handler failed",
					zap.String("symbol", symbol),
					zap.Error(err),
				)
				continue
			}

			if transition != nil {
				// Execute the transition through decision engine
				_, err := ae.decisionEngine.EvaluateAndDecide(symbol, &ScoreInputs{
					Symbol:         symbol,
					RegimeSnapshot: regime,
				})
				if err != nil {
					ae.logger.Error("State transition failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
				} else {
					ae.logger.Info("State transition executed",
						zap.String("symbol", symbol),
						zap.String("from", string(transition.FromState)),
						zap.String("to", string(transition.ToState)),
						zap.Float64("score", transition.Score),
					)

					// NEW: Request execution via VF Bridge (Hybrid Integration)
					if ae.vfBridge != nil {
						if err := ae.vfBridge.RequestStateTransition(ctx, symbol,
							transition.FromState, transition.ToState,
							transition.Trigger, transition.Score, regime); err != nil {
							ae.logger.Error("Failed to request VF execution",
								zap.String("symbol", symbol),
								zap.Error(err),
							)
						}
					}
				}
			}

		case TradingModeWaitNewRange:
			// Handle WAIT_NEW_RANGE state
			if ae.waitRangeHandler != nil {
				// Get current price (would need price feed, using placeholder)
				currentPrice := 0.0 // TODO: Wire with actual price feed

				transition, err := ae.waitRangeHandler.HandleState(ctx, symbol, regime, currentPrice)
				if err != nil {
					ae.logger.Error("WAIT_NEW_RANGE state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					_, err := ae.decisionEngine.EvaluateAndDecide(symbol, &ScoreInputs{
						Symbol:         symbol,
						RegimeSnapshot: regime,
					})
					if err != nil {
						ae.logger.Error("State transition failed",
							zap.String("symbol", symbol),
							zap.Error(err),
						)
					}
				}
			}

		case TradingModeGrid:
			// Handle active GRID trading state (Phase 6)
			if ae.tradingGridHandler != nil {
				// TODO: Get actual price, position size, and blended signals
				currentPrice := 0.0
				positionSize := 0.0
				signals := &SignalBundle{OverallStrength: 0.5}

				transition, err := ae.tradingGridHandler.HandleState(
					ctx, symbol, regime, currentPrice, positionSize, signals,
				)
				if err != nil {
					ae.logger.Error("TRADING_GRID state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					_, err := ae.decisionEngine.EvaluateAndDecide(symbol, &ScoreInputs{
						Symbol:         symbol,
						RegimeSnapshot: regime,
					})
					if err != nil {
						ae.logger.Error("State transition failed",
							zap.String("symbol", symbol),
							zap.Error(err),
						)
					}
				}
			}

		case TradingModeTrending:
			// Handle TRENDING state (Phase 7)
			if ae.trendingHandler != nil {
				// TODO: Get actual price, breakout level, and FVG zones
				currentPrice := 0.0
				breakoutLevel := 0.0
				fvgZones := []FVGZone{}

				transition, err := ae.trendingHandler.HandleState(
					ctx, symbol, regime, currentPrice, breakoutLevel, fvgZones,
				)
				if err != nil {
					ae.logger.Error("TRENDING state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					_, err := ae.decisionEngine.EvaluateAndDecide(symbol, &ScoreInputs{
						Symbol:         symbol,
						RegimeSnapshot: regime,
					})
					if err != nil {
						ae.logger.Error("State transition failed",
							zap.String("symbol", symbol),
							zap.Error(err),
						)
					}
				}
			}

		case TradingModeAccumulation:
			// Handle ACCUMULATION state (Phase 8)
			if ae.accumulationHandler != nil {
				// TODO: Get actual price and volume
				currentPrice := 0.0
				volume24h := regime.Volume24h

				transition, err := ae.accumulationHandler.HandleState(
					ctx, symbol, regime, currentPrice, volume24h,
				)
				if err != nil {
					ae.logger.Error("ACCUMULATION state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					_, err := ae.decisionEngine.EvaluateAndDecide(symbol, &ScoreInputs{
						Symbol:         symbol,
						RegimeSnapshot: regime,
					})
					if err != nil {
						ae.logger.Error("State transition failed",
							zap.String("symbol", symbol),
							zap.Error(err),
						)
					}
				}
			}

		case TradingModeDefensive:
			// Handle DEFENSIVE state (Phase 9)
			if ae.defensiveHandler != nil {
				// TODO: Get actual price, position size, and unrealized PnL
				currentPrice := 0.0
				positionSize := 0.0
				unrealizedPnL := 0.0

				transition, err := ae.defensiveHandler.HandleState(
					ctx, symbol, regime, currentPrice, positionSize, unrealizedPnL,
				)
				if err != nil {
					ae.logger.Error("DEFENSIVE state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					_, err := ae.decisionEngine.EvaluateAndDecide(symbol, &ScoreInputs{
						Symbol:         symbol,
						RegimeSnapshot: regime,
					})
					if err != nil {
						ae.logger.Error("State transition failed",
							zap.String("symbol", symbol),
							zap.Error(err),
						)
					}
				}
			}

		case TradingModeOverSize:
			// Handle OVER_SIZE state (Phase 10)
			if ae.overSizeHandler != nil {
				// TODO: Get actual price and position size
				currentPrice := 0.0
				positionSize := 0.0

				transition, err := ae.overSizeHandler.HandleState(
					ctx, symbol, regime, currentPrice, positionSize,
				)
				if err != nil {
					ae.logger.Error("OVER_SIZE state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					_, err := ae.decisionEngine.EvaluateAndDecide(symbol, &ScoreInputs{
						Symbol:         symbol,
						RegimeSnapshot: regime,
					})
					if err != nil {
						ae.logger.Error("State transition failed",
							zap.String("symbol", symbol),
							zap.Error(err),
						)
					}
				}
			}

		case TradingModeRecovery:
			// Handle RECOVERY state (Phase 10)
			if ae.recoveryHandler != nil {
				// TODO: Get actual exit PnL and reason
				exitPnL := 0.0
				exitReason := ""
				consecutiveLosses := 0

				transition, err := ae.recoveryHandler.HandleState(
					ctx, symbol, regime, exitPnL, exitReason, consecutiveLosses,
				)
				if err != nil {
					ae.logger.Error("RECOVERY state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					_, err := ae.decisionEngine.EvaluateAndDecide(symbol, &ScoreInputs{
						Symbol:         symbol,
						RegimeSnapshot: regime,
					})
					if err != nil {
						ae.logger.Error("State transition failed",
							zap.String("symbol", symbol),
							zap.Error(err),
						)
					}
				}
			}

		default:
			ae.logger.Debug("State not handled yet",
				zap.String("symbol", symbol),
				zap.String("state", string(currentState.CurrentMode)),
			)
		}
	}
}
