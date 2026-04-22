package agentic

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"aster-bot/internal/config"
	"aster-bot/internal/health"
	"aster-bot/internal/realtime"

	"go.uber.org/zap"
)

// AgenticEngine is the decision layer that controls the Volume Farm execution

type AgenticEngine struct {
	config *config.AgenticConfig

	marketData      realtime.MarketStateProvider
	runtimeProvider realtime.RuntimeSnapshotProvider

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

	// Watchdog
	stateTimeouts map[TradingMode]time.Duration

	// NEW: Health monitoring
	healthMonitor *health.Monitor

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

	marketData realtime.MarketStateProvider,

	runtimeProvider realtime.RuntimeSnapshotProvider,

	vfController VFWhitelistController,

	logger *zap.Logger,

) (*AgenticEngine, error) {

	if cfg == nil {

		return nil, fmt.Errorf("agentic config is nil")

	}

	engine := &AgenticEngine{

		config: cfg,

		marketData: marketData,

		runtimeProvider: runtimeProvider,

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

	// NEW: State Watchdog Timeouts (Phase 3)
	engine.stateTimeouts = map[TradingMode]time.Duration{
		TradingModeEnterGrid:    2 * time.Minute,
		TradingModeGrid:         24 * time.Hour,
		TradingModeTrending:     4 * time.Hour,
		TradingModeAccumulation: 8 * time.Hour,
		TradingModeRecovery:     6 * time.Hour,
		TradingModeOverSize:     1 * time.Hour,
		TradingModeDefensive:    30 * time.Minute,
	}

	// NEW: Initialize adaptive state management components (Phase 2-10)
	if cfg.ScoreEngine.Enabled {
		engine.scoreEngine = NewScoreCalculationEngine(&cfg.ScoreEngine, logger)
		engine.decisionEngine = NewDecisionEngine(&cfg.DecisionEngine, &cfg.AgenticV2, engine.scoreEngine, logger)
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
		engine.vfBridge = NewAgenticVFBridge(engine.stateEventBus, engine.decisionEngine, logger)

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

		detector := NewRegimeDetector(symbol, cfg.RegimeDetection, marketData, logger)

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

	// Initialize health monitor
	engine.healthMonitor = health.NewMonitor(logger)
	logger.Info("Health monitor initialized for AgenticEngine")

	return engine, nil

}

// registerWorkersForHealthMonitoring registers critical agentic workers for health monitoring
func (ae *AgenticEngine) registerWorkersForHealthMonitoring(ctx context.Context) {
	// Register detection loop worker
	ae.healthMonitor.RegisterWorker(health.WorkerConfig{
		Name:                "agentic_detection_loop",
		HeartbeatInterval:   30 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		MaxErrorCount:       5,
		AutoRestart:         true,
	}, func(ctx context.Context) error {
		ae.detectionLoop(ctx)
		return nil
	}, func() error {
		close(ae.stopCh)
		return nil
	})

	ae.logger.Info("Agentic workers registered for health monitoring")
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

	// Start health monitor
	if err := ae.healthMonitor.Start(); err != nil {
		ae.logger.Error("Failed to start health monitor", zap.Error(err))
	} else {
		ae.logger.Info("Health monitor started")
	}

	// Register workers for health monitoring
	ae.registerWorkersForHealthMonitoring(ctx)

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

	go func() {
		defer func() {
			if r := recover(); r != nil {
				ae.logger.Error("Detection loop goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		ae.detectionLoop(ctx)
	}()

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

	// Stop health monitor
	if ae.healthMonitor != nil {
		if err := ae.healthMonitor.Stop(); err != nil {
			ae.logger.Error("Failed to stop health monitor", zap.Error(err))
		} else {
			ae.logger.Info("Health monitor stopped")
		}
	}

	// Stop circuit breaker evaluation worker
	ae.circuitBreaker.Stop()

	// Signal stop

	close(ae.stopCh)

	// Wait for goroutines to finish with timeout

	done := make(chan struct{})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				ae.logger.Error("AgenticEngine WaitGroup goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()
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

	defer func() {
		if r := recover(); r != nil {
			ae.logger.Error("Detection loop panic recovered",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()
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

			// Report heartbeat
			if ae.healthMonitor != nil {
				ae.healthMonitor.UpdateHeartbeat("agentic_detection_loop")
			}

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
	for symbol, regime := range detectionResults {
		volatility := regime.ATR14
		trend := float64(regime.ADX) / 100.0

		var spread, volume float64
		snap := ae.runtimeProvider.GetSymbolSnapshot(ctx, symbol)
		if snap.BestAsk > 0 {
			spread = (snap.BestAsk - snap.BestBid) / snap.BestAsk
		}
		volume = snap.Volume24h

		ae.circuitBreaker.UpdateMarketConditions(symbol, volatility, trend, spread, volume)
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
			defer func() {
				if r := recover(); r != nil {
					ae.logger.Error("Regime detector goroutine panic recovered",
						zap.String("symbol", sym),
						zap.Any("panic", r),
						zap.String("stack", string(debug.Stack())))
				}
			}()
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

// GetHealthStatus returns the health status of all monitored workers
func (ae *AgenticEngine) GetHealthStatus() map[string]health.WorkerHealth {
	if ae.healthMonitor == nil {
		return nil
	}
	return ae.healthMonitor.GetAllWorkerHealth()
}

func (ae *AgenticEngine) getRuntimeSnapshot(ctx context.Context, symbol string) realtime.SymbolRuntimeSnapshot {
	if ae.runtimeProvider == nil {
		return realtime.SymbolRuntimeSnapshot{Symbol: symbol, BlockReason: realtime.BlockReasonMissingMarketData}
	}
	return ae.runtimeProvider.GetSymbolSnapshot(ctx, symbol)
}

func (ae *AgenticEngine) buildSignalBundle(snapshot realtime.SymbolRuntimeSnapshot, regime RegimeSnapshot) *SignalBundle {
	overall := 0.5
	if regime.Confidence > 0 {
		overall = regime.Confidence
	}
	if snapshot.BlockReason != "" {
		overall = 0.2
	}
	meanReversion := 0.2
	breakout := 0.2
	liquidity := max(0.1, 1.0-min(1.0, snapshot.SpreadBps/25.0))
	fvg := 0.2
	if regime.Regime == RegimeSideways || regime.Regime == RegimeRecovery {
		meanReversion = min(1.0, regime.Confidence+0.2)
		fvg = min(1.0, meanReversion*0.85)
	} else if regime.Regime == RegimeTrending {
		breakout = min(1.0, regime.Confidence+0.15)
		fvg = min(1.0, breakout*0.75)
	}
	return &SignalBundle{
		FVGSignal:       fvg,
		LiquiditySignal: liquidity,
		BreakoutSignal:  breakout,
		MeanReversion:   meanReversion,
		OverallStrength: overall,
	}
}

func (ae *AgenticEngine) buildMarketStateVector(snapshot realtime.SymbolRuntimeSnapshot, regime RegimeSnapshot) MarketStateVector {
	liquidityQuality := 1.0
	if snapshot.SpreadBps > 0 {
		liquidityQuality = 1.0 - min(1.0, snapshot.SpreadBps/20.0)
	}

	rangeQuality := 0.25
	if regime.Regime == RegimeSideways {
		rangeQuality = min(1.0, regime.Confidence+0.2)
	} else if regime.Regime == RegimeRecovery {
		rangeQuality = min(1.0, regime.Confidence+0.1)
	}

	trendStrength := min(1.0, regime.ADX/45.0)
	if regime.Regime != RegimeTrending {
		trendStrength *= 0.6
	}

	volatilityState := min(1.0, regime.ATR14*100)
	breakoutPersistence := min(1.0, trendStrength*(0.6+regime.Confidence*0.4))

	// Phase 1: Enhanced Vector
	trendPersistence := 0.0
	if regime.Regime == RegimeTrending {
		trendPersistence = regime.Confidence
	}

	spreadStress := snapshot.SpreadBps / 20.0 // Normalized to 20bps

	return MarketStateVector{
		Regime:              regime.Regime,
		TrendStrength:       trendStrength,
		RangeQuality:        rangeQuality,
		VolatilityState:     volatilityState,
		LiquidityQuality:    liquidityQuality,
		BreakoutPersistence: breakoutPersistence,
		TrendPersistence:    trendPersistence,
		OrderflowImbalance:  0.5, // Placeholder for Phase 2
		SpreadStress:        spreadStress,
	}
}

func (ae *AgenticEngine) buildExecutionContext(snapshot realtime.SymbolRuntimeSnapshot, regime RegimeSnapshot) ExecutionContext {
	return ExecutionContext{
		CurrentPrice:      snapshot.CurrentPrice,
		BestBid:           snapshot.BestBid,
		BestAsk:           snapshot.BestAsk,
		SpreadBps:         snapshot.SpreadBps,
		SlippageEstBps:    snapshot.SlippageEstBps,
		FundingImpactBps:  snapshot.FundingImpact,
		PositionSize:      snapshot.PositionSize,
		InventoryNotional: snapshot.InventoryNotional,
		PositionAgeSec:    snapshot.PositionAgeSec,
		PendingOrders:     snapshot.PendingOrders,
		RealizedPnL:       snapshot.RealizedPnL,
		UnrealizedPnL:     snapshot.UnrealizedPnL,
		MakerFillRatio:    snapshot.MakerFillRatio,
		Regime:            regime.Regime,
	}
}

func (ae *AgenticEngine) defaultLifecyclePolicy(vector MarketStateVector, snapshot realtime.SymbolRuntimeSnapshot) TradeLifecyclePolicy {
	targetAgeSec := int64(480)
	feeBudget := 8.0
	minRangeQuality := 0.55
	volumeWeight := 0.8
	profitWeight := 0.45
	riskWeight := 0.7

	if ae.config.AgenticV2.PositionAgeTargetSec > 0 {
		targetAgeSec = ae.config.AgenticV2.PositionAgeTargetSec
	}
	if ae.config.AgenticV2.FeeGuardrails.MaxFeeBudgetBps > 0 {
		feeBudget = ae.config.AgenticV2.FeeGuardrails.MaxFeeBudgetBps
	}
	if ae.config.AgenticV2.FeeGuardrails.MinRangeQuality > 0 {
		minRangeQuality = ae.config.AgenticV2.FeeGuardrails.MinRangeQuality
	}
	if ae.config.AgenticV2.ObjectiveWeights.VolumeWeight > 0 {
		volumeWeight = ae.config.AgenticV2.ObjectiveWeights.VolumeWeight
	}
	if ae.config.AgenticV2.ObjectiveWeights.ProfitWeight > 0 {
		profitWeight = ae.config.AgenticV2.ObjectiveWeights.ProfitWeight
	}
	if ae.config.AgenticV2.ObjectiveWeights.RiskWeight > 0 {
		riskWeight = ae.config.AgenticV2.ObjectiveWeights.RiskWeight
	}

	tpTarget := 12.0
	if ae.config.AgenticV2.FeeGuardrails.DefaultTPBps > 0 {
		tpTarget = ae.config.AgenticV2.FeeGuardrails.DefaultTPBps
	}
	hardSL := 28.0
	if ae.config.AgenticV2.FeeGuardrails.DefaultHardSLBps > 0 {
		hardSL = ae.config.AgenticV2.FeeGuardrails.DefaultHardSLBps
	}

	inventorySkew := 0.0
	if snapshot.InventoryNotional > 0 && snapshot.CurrentPrice > 0 {
		inventorySkew = min(1.0, snapshot.InventoryNotional/(snapshot.CurrentPrice*0.1))
	}

	return TradeLifecyclePolicy{
		TPBands: []TPBand{
			{TargetBps: tpTarget, CloseRatio: 0.5, MakerOnly: true},
			{TargetBps: tpTarget * 1.5, CloseRatio: 0.3, MakerOnly: true},
			{TargetBps: tpTarget * 2.0, CloseRatio: 0.2, MakerOnly: true},
		},
		SLPolicy: SLPolicy{
			SoftATRMultiplier: 1.6,
			HardLossBps:       hardSL,
			TimeStopSec:       targetAgeSec,
		},
		RegridPolicy: RegridPolicy{
			AllowImmediate:  vector.RangeQuality >= minRangeQuality,
			MinRangeQuality: minRangeQuality,
			FlattenFirst:    true,
		},
		MakerOnly:         true,
		MaxPositionAgeSec: targetAgeSec,
		FeeBudgetBps:      feeBudget,
		InventorySkew:     inventorySkew,
		Objective: ModeObjective{
			VolumeWeight: volumeWeight,
			ProfitWeight: profitWeight,
			RiskWeight:   riskWeight,
		},
	}
}

func (ae *AgenticEngine) commitHandlerTransition(
	ctx context.Context,
	symbol string,
	regime RegimeSnapshot,
	snapshot realtime.SymbolRuntimeSnapshot,
	transition *StateTransition,
) {
	if transition == nil || ae.decisionEngine == nil {
		return
	}

	// Phase 0: Guardrails - Hard Kill Switch
	if ae.config.AgenticV2.Enabled && ae.config.AgenticV2.HardKillSwitch {
		ae.logger.Warn("Hard kill switch active, rejecting transition", zap.String("symbol", symbol))
		return
	}

	// Phase 0: Guardrails - Per-symbol Loss Cap
	if ae.config.AgenticV2.Enabled && ae.config.AgenticV2.PerSymbolLossCapUSDT > 0 {
		if snapshot.RealizedPnL < -ae.config.AgenticV2.PerSymbolLossCapUSDT {
			ae.logger.Error("Per-symbol loss cap reached, forcing IDLE",
				zap.String("symbol", symbol),
				zap.Float64("realized_pnl", snapshot.RealizedPnL),
				zap.Float64("cap", ae.config.AgenticV2.PerSymbolLossCapUSDT))
			transition.ToState = TradingModeIdle
			transition.Trigger = "loss_cap_reached"
		}
	}

	vector := ae.buildMarketStateVector(snapshot, regime)
	executionContext := ae.buildExecutionContext(snapshot, regime)
	policy := ae.defaultLifecyclePolicy(vector, snapshot)
	intent := TransitionIntent{
		Symbol:           symbol,
		FromState:        transition.FromState,
		ToState:          transition.ToState,
		Trigger:          transition.Trigger,
		Score:            transition.Score,
		MarketState:      vector,
		ExecutionContext: executionContext,
		LifecyclePolicy:  policy,
		Timestamp:        transition.Timestamp,
	}

	committed, err := ae.decisionEngine.CommitTransition(intent)
	if err != nil {
		ae.logger.Error("State transition commit failed",
			zap.String("symbol", symbol),
			zap.Error(err),
		)
		return
	}
	if committed == nil {
		return
	}

	ae.recordExitContext(symbol, committed, snapshot)

	// Phase 0: Shadow Mode
	if ae.config.AgenticV2.Enabled && ae.config.AgenticV2.ShadowMode {
		ae.logger.Info("SHADOW MODE: Transition committed but execution skipped",
			zap.String("symbol", symbol),
			zap.String("from", string(committed.FromState)),
			zap.String("to", string(committed.ToState)))
		return
	}

	if ae.vfBridge != nil {
		if err := ae.vfBridge.RequestStateTransition(ctx, symbol, intent); err != nil {
			ae.logger.Error("Failed to request VF execution",
				zap.String("symbol", symbol),
				zap.Error(err),
			)
		}
	}
}

func (ae *AgenticEngine) recordExitContext(symbol string, transition *StateTransition, snapshot realtime.SymbolRuntimeSnapshot) {
	if transition == nil || ae.decisionEngine == nil {
		return
	}

	switch transition.ToState {
	case TradingModeDefensive, TradingModeRecovery, TradingModeIdle:
		reason := transition.Trigger
		if reason == "" {
			reason = string(snapshot.BlockReason)
		}
		ae.decisionEngine.RecordExitContext(symbol, snapshot.UnrealizedPnL, reason)
	}
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

		snapshot := ae.getRuntimeSnapshot(ctx, symbol)

		// US4: Watchdog Check - Check if state is stuck
		if currentStateObj := ae.decisionEngine.GetSymbolTradingState(symbol); currentStateObj != nil {
			timeout, exists := ae.stateTimeouts[currentStateObj.CurrentMode]
			if exists && !currentStateObj.StateEnteredAt.IsZero() {
				timeInState := time.Since(currentStateObj.StateEnteredAt)
				if timeInState > timeout {
					ae.logger.Warn("State watchdog timeout triggered",
						zap.String("symbol", symbol),
						zap.String("mode", string(currentStateObj.CurrentMode)),
						zap.Duration("duration", timeInState),
						zap.Duration("limit", timeout),
					)

					ae.decisionEngine.IncrementStateStuckCount(symbol)
					if sink, ok := ae.runtimeProvider.(realtime.ExecutionEventSink); ok {
						sink.IncrementStateStuckCount(symbol)
					}

					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, &StateTransition{
						FromState: currentStateObj.CurrentMode,
						ToState:   TradingModeIdle,
						Trigger:   "watchdog_timeout",
						Score:     1.0,
						Timestamp: time.Now(),
					})
					continue // Skip normal processing for this cycle
				}
			}

			if ae.config.AgenticV2.AckTimeoutSec > 0 &&
				ae.decisionEngine.HasPendingExecutionAck(symbol, time.Duration(ae.config.AgenticV2.AckTimeoutSec)*time.Second) {
				ae.logger.Warn("Execution acknowledgement timed out",
					zap.String("symbol", symbol),
					zap.String("mode", string(currentStateObj.CurrentMode)),
				)
			}
		}

		if snapshot.BlockReason != "" && currentState.CurrentMode != TradingModeIdle && currentState.CurrentMode != TradingModeRecovery {
			ae.logger.Warn("Runtime snapshot degraded for state management",
				zap.String("symbol", symbol),
				zap.String("state", string(currentState.CurrentMode)),
				zap.String("block_reason", string(snapshot.BlockReason)),
			)
			// Note: we continue here to let handlers decide if they can handle partial data or if they should transition
		}

		// Execute state-specific logic
		switch currentState.CurrentMode {
		case TradingModeIdle:
			// Handle IDLE state
			transition, err := ae.idleHandler.HandleState(ctx, symbol, regime, snapshot)
			if err != nil {
				ae.logger.Error("IDLE state handler failed",
					zap.String("symbol", symbol),
					zap.Error(err),
				)
				continue
			}

			if transition != nil {
				ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
			}

		case TradingModeWaitNewRange:
			// Handle WAIT_NEW_RANGE state
			if ae.waitRangeHandler != nil {
				transition, err := ae.waitRangeHandler.HandleState(ctx, symbol, regime, snapshot)
				if err != nil {
					ae.logger.Error("WAIT_NEW_RANGE state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
				}
			}

		case TradingModeEnterGrid:
			if ae.enterGridHandler != nil {
				signals := ae.buildSignalBundle(snapshot, regime)
				var rangeBoundaries *RangeBoundaries
				if ae.waitRangeHandler != nil {
					rangeBoundaries, _ = ae.waitRangeHandler.GetRangeBoundaries(symbol)
				}

				transition, err := ae.enterGridHandler.HandleState(
					ctx, symbol, regime, snapshot, rangeBoundaries, signals,
				)
				if err != nil {
					ae.logger.Error("ENTER_GRID state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}
				if transition != nil {
					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
				}
			}

		case TradingModeGrid:
			// Handle active GRID trading state (Phase 6)
			if ae.tradingGridHandler != nil {
				signals := ae.buildSignalBundle(snapshot, regime)

				transition, err := ae.tradingGridHandler.HandleState(
					ctx, symbol, regime, snapshot, signals,
				)
				if err != nil {
					ae.logger.Error("TRADING_GRID state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
				}
			}

		case TradingModeTrending:
			// Handle TRENDING state (Phase 7)
			if ae.trendingHandler != nil {
				currentPrice := snapshot.CurrentPrice
				breakoutLevel := snapshot.BestAsk
				if breakoutLevel <= 0 {
					breakoutLevel = snapshot.BestBid
				}
				if breakoutLevel <= 0 {
					breakoutLevel = currentPrice
				}
				vector := ae.buildMarketStateVector(snapshot, regime)

				transition, err := ae.trendingHandler.HandleState(
					ctx, symbol, regime, snapshot, vector, breakoutLevel,
				)
				if err != nil {
					ae.logger.Error("TRENDING state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
				}
			}

		case TradingModeAccumulation:
			// Handle ACCUMULATION state (Phase 8)
			if ae.accumulationHandler != nil {
				transition, err := ae.accumulationHandler.HandleState(
					ctx, symbol, regime, snapshot,
				)
				if err != nil {
					ae.logger.Error("ACCUMULATION state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
				}
			}

		case TradingModeDefensive:
			// Handle DEFENSIVE state (Phase 9)
			if ae.defensiveHandler != nil {
				transition, err := ae.defensiveHandler.HandleState(
					ctx, symbol, regime, snapshot,
				)
				if err != nil {
					ae.logger.Error("DEFENSIVE state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
				}
			}

		case TradingModeOverSize:
			// Handle OVER_SIZE state (Phase 10)
			if ae.overSizeHandler != nil {
				transition, err := ae.overSizeHandler.HandleState(
					ctx, symbol, regime, snapshot,
				)
				if err != nil {
					ae.logger.Error("OVER_SIZE state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
				}
			}

		case TradingModeRecovery:
			// Handle RECOVERY state (Phase 10)
			if ae.recoveryHandler != nil {
				exitPnL := snapshot.UnrealizedPnL
				exitReason := string(snapshot.BlockReason)

				// T012: Lấy consecutiveLosses từ state thật
				consecutiveLosses := 0
				if currentState := ae.decisionEngine.GetSymbolTradingState(symbol); currentState != nil {
					consecutiveLosses = currentState.ConsecutiveLosses
					if currentState.LastExitReason != "" {
						exitReason = currentState.LastExitReason
					}
					if currentState.LastExitPnL != 0 {
						exitPnL = currentState.LastExitPnL
					}
				}

				transition, err := ae.recoveryHandler.HandleState(
					ctx, symbol, regime, snapshot, exitPnL, exitReason, consecutiveLosses,
				)
				if err != nil {
					ae.logger.Error("RECOVERY state handler failed",
						zap.String("symbol", symbol),
						zap.Error(err),
					)
					continue
				}

				if transition != nil {
					ae.commitHandlerTransition(ctx, symbol, regime, snapshot, transition)
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
