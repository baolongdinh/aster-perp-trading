package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// GridAdapter connects agent decisions to the existing grid manager
type GridAdapter struct {
	config        *AgentConfig
	factorEngine  *FactorEngine
	positionSizer *PositionSizer
}

// NewGridAdapter creates a new grid adapter
func NewGridAdapter(config *AgentConfig) *GridAdapter {
	return &GridAdapter{
		config:        config,
		factorEngine:  NewFactorEngine(&config.Agent.Factors),
		positionSizer: NewPositionSizer(&config.Agent.PositionSizing),
	}
}

// AdaptDecision converts agent decision to grid parameters
func (ga *GridAdapter) AdaptDecision(
	ctx context.Context,
	decision TradingDecision,
	baseParams GridParams,
) (GridParams, error) {

	// Convert IndicatorSnapshot to IndicatorValues
	values := &IndicatorValues{
		ADX:           decision.RegimeSnapshot.Indicators.ADX,
		BBWidth:       decision.RegimeSnapshot.Indicators.BBWidth,
		ATR14:         decision.RegimeSnapshot.Indicators.ATR14,
		VolumeMA20:    decision.RegimeSnapshot.Indicators.VolumeMA20,
		CurrentVolume: decision.RegimeSnapshot.Indicators.CurrentVolume,
		EMA9:          decision.RegimeSnapshot.Indicators.EMA9,
		EMA21:         decision.RegimeSnapshot.Indicators.EMA21,
		EMA50:         decision.RegimeSnapshot.Indicators.EMA50,
		EMA200:        decision.RegimeSnapshot.Indicators.EMA200,
	}

	// Apply score-based sizing
	finalSize := ga.positionSizer.CalculateSize(
		baseParams.PositionSize,
		decision.FinalScore,
		values,
	)

	// Calculate grid spacing based on volatility
	gridSpacing := ga.positionSizer.CalculateGridSpacing(values)

	// Calculate stop loss distance based on ATR
	stopLossDistance := ga.calculateStopLoss(&decision.RegimeSnapshot.Indicators)

	adaptedParams := GridParams{
		GridSpacing:      gridSpacing,
		PositionSize:     finalSize,
		StopLossDistance: stopLossDistance,
	}

	return adaptedParams, nil
}

// calculateStopLoss computes stop loss distance based on ATR
func (ga *GridAdapter) calculateStopLoss(indicators *IndicatorSnapshot) float64 {
	// Use 2x ATR as default stop loss
	atr := indicators.ATR14

	// Adjust based on regime
	switch {
	case atr < 0.5:
		return 1.5 * atr // Tight stop for low volatility
	case atr < 1.5:
		return 2.0 * atr // Normal stop
	default:
		return 3.0 * atr // Wide stop for high volatility
	}
}

// ShouldDeploy determines if grid should be deployed based on decision
func (ga *GridAdapter) ShouldDeploy(decision TradingDecision) (bool, string) {
	scoring := ga.config.Agent.Factors.Scoring

	switch {
	case decision.FinalScore >= scoring.DeployFullThreshold:
		return true, "Deploying grid with full size"
	case decision.FinalScore >= scoring.DeployReducedThreshold:
		return true, "Deploying grid with reduced size"
	case decision.FinalScore >= scoring.WaitThreshold:
		return false, "Waiting for better conditions"
	default:
		return false, "Conditions unfavorable - not deploying"
	}
}

// DecisionHandler processes trading decisions and applies them
type DecisionHandler struct {
	adapter      *GridAdapter
	logger       *DefaultDecisionLogger
	patternStore *PatternStore
	currentPair  string // Current trading pair (BTCUSD1, ETHUSD1, SOLUSD1)
}

// NewDecisionHandler creates a new decision handler
func NewDecisionHandler(
	adapter *GridAdapter,
	logger *DefaultDecisionLogger,
	patterns *PatternStore,
	pair string,
) *DecisionHandler {
	return &DecisionHandler{
		adapter:      adapter,
		logger:       logger,
		patternStore: patterns,
		currentPair:  pair,
	}
}

// ProcessDecision handles a new trading decision
func (dh *DecisionHandler) ProcessDecision(
	ctx context.Context,
	decision TradingDecision,
	baseParams GridParams,
) (*GridParams, error) {

	// Log the decision
	if err := dh.logger.Log(decision); err != nil {
		// Log error but continue
	}

	// Check if patterns should impact the decision
	if dh.patternStore != nil && dh.patternStore.IsActive(dh.currentPair) {
		matches := dh.patternStore.FindMatches(dh.currentPair, decision.RegimeSnapshot.Indicators, 5)
		impact := dh.patternStore.CalculatePatternImpact(dh.currentPair, matches)

		// Adjust score
		decision.FinalScore += impact
		decision.PatternMatches = matches
		decision.PatternImpact = impact
	}

	// Determine if we should deploy
	shouldDeploy, rationale := dh.adapter.ShouldDeploy(decision)
	decision.Rationale = rationale
	decision.Executed = shouldDeploy

	if !shouldDeploy {
		return nil, fmt.Errorf("deployment rejected: %s", rationale)
	}

	// Adapt parameters
	adaptedParams, err := dh.adapter.AdaptDecision(ctx, decision, baseParams)
	if err != nil {
		return nil, fmt.Errorf("failed to adapt decision: %w", err)
	}

	return &adaptedParams, nil
}

// RecordOutcome records trade outcome for pattern learning
func (dh *DecisionHandler) RecordOutcome(
	decisionID string,
	snapshot RegimeSnapshot,
	params GridParams,
	outcome TradeOutcome,
) {
	if dh.patternStore == nil {
		return
	}

	// Add pattern for learning
	dh.patternStore.AddPattern(dh.currentPair, snapshot, params, outcome)

	// Save patterns periodically
	if dh.patternStore.GetPatternCount(dh.currentPair)%10 == 0 {
		dh.patternStore.SavePair(dh.currentPair)
	}
}

// CompleteAgent represents the fully assembled agent
type CompleteAgent struct {
	Config        *AgentConfig
	Detector      *Detector
	Breakers      CircuitBreakerManager
	FactorEngine  *FactorEngine
	PositionSizer *PositionSizer
	Logger        *DefaultDecisionLogger
	PatternStore  *PatternStore
	Handler       *DecisionHandler
	Adapter       *GridAdapter
	AlertManager  *AlertManager

	isRunning bool
	stopCh    chan struct{}
}

// NewCompleteAgent creates a fully configured agent
func NewCompleteAgent(config *AgentConfig) (*CompleteAgent, error) {
	// Initialize components
	detector := NewDetector(&config.Agent.RegimeDetection)
	breakers := NewCircuitBreakerManager(&config.Agent.CircuitBreakers)
	logger := NewDecisionLogger(&config.Agent.Logging)

	var patternStore *PatternStore
	var err error
	if config.Agent.Patterns.Enabled {
		patternStore, err = NewPatternStore(&config.Agent.Patterns)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize pattern store: %w", err)
		}
	}

	adapter := NewGridAdapter(config)
	// Default pair for single-pair mode; multi-pair requires separate handlers
	handler := NewDecisionHandler(adapter, logger, patternStore, "BTCUSD1")

	// Initialize alert manager with Telegram notifier from env vars
	notifier := NewTelegramNotifier(config.Agent.Alerting.RateLimitWindow)
	alertManager := NewAlertManager(config.Agent.Alerting, notifier)

	return &CompleteAgent{
		Config:        config,
		Detector:      detector,
		Breakers:      breakers,
		FactorEngine:  adapter.factorEngine,
		PositionSizer: adapter.positionSizer,
		Logger:        logger,
		PatternStore:  patternStore,
		Handler:       handler,
		Adapter:       adapter,
		AlertManager:  alertManager,
		stopCh:        make(chan struct{}),
	}, nil
}

// Start begins the agent operation
func (ca *CompleteAgent) Start(ctx context.Context, symbol string, testMode bool) error {
	if ca.isRunning {
		return nil
	}

	ca.isRunning = true

	// Send startup notification
	if ca.AlertManager != nil {
		ca.AlertManager.NotifyStartup(symbol, testMode)
	}

	// Start regime detection loop
	go ca.regimeDetectionLoop(ctx, symbol)

	// Start circuit breaker monitoring
	go ca.circuitBreakerLoop(ctx)

	return nil
}

// Stop gracefully shuts down the agent
func (ca *CompleteAgent) Stop() {
	if !ca.isRunning {
		return
	}

	ca.isRunning = false
	close(ca.stopCh)

	// Send shutdown notification
	if ca.AlertManager != nil {
		ca.AlertManager.NotifyShutdown()
	}

	// Save patterns for all active pairs before shutdown
	if ca.PatternStore != nil {
		for _, pair := range ca.PatternStore.GetActivePairs() {
			ca.PatternStore.SavePair(pair)
		}
	}
}

// regimeDetectionLoop runs regime detection at configured intervals
func (ca *CompleteAgent) regimeDetectionLoop(ctx context.Context, symbol string) {
	ticker := time.NewTicker(ca.Config.Agent.RegimeDetection.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ca.stopCh:
			return
		case <-ticker.C:
			// Detect regime and send alert if changed
			regime, _ := ca.Detector.Detect()
			if ca.AlertManager != nil {
				ca.AlertManager.NotifyRegimeChange(regime)
			}
		}
	}
}

// circuitBreakerLoop monitors for circuit breaker conditions
func (ca *CompleteAgent) circuitBreakerLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ca.stopCh:
			return
		case <-ticker.C:
			if event, triggered := ca.Breakers.Check(ctx); triggered {
				ca.handleCircuitBreaker(event)
			}
		}
	}
}

// handleCircuitBreaker handles triggered circuit breakers
func (ca *CompleteAgent) handleCircuitBreaker(event *CircuitBreakerEvent) {
	// Log the event
	decision := TradingDecision{
		ID:           uuid.New(),
		Timestamp:    time.Now(),
		DecisionType: DecisionClose,
		Rationale:    event.ActionTaken,
	}
	ca.Logger.Log(decision)

	// Handle based on breaker type
	switch event.BreakerType {
	case BreakerVolatility, BreakerDrawdown:
		// Emergency close - would call grid manager
	case BreakerLiquidity:
		// Pause new orders
	case BreakerLosses:
		// Reduce position size
	case BreakerConnection:
		// Pause and alert
	}
}

// GetCurrentRegime returns the current market regime
func (ca *CompleteAgent) GetCurrentRegime() RegimeSnapshot {
	return ca.Detector.GetCurrent()
}

// MakeDecision creates a new trading decision based on current conditions
func (ca *CompleteAgent) MakeDecision(
	candles []Candle,
	baseParams GridParams,
) (*TradingDecision, *GridParams, error) {

	// Update detector with latest candles
	ca.Detector.UpdateCandles(candles)

	// Get current regime
	regime, err := ca.Detector.Detect()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to detect regime: %w", err)
	}

	// Calculate indicators
	calculator := NewIndicatorCalculator()
	values := calculator.CalculateAll(candles)
	if values == nil {
		return nil, nil, fmt.Errorf("insufficient data for indicators")
	}

	// Calculate score and factors
	score, factors := ca.FactorEngine.CalculateScore(values, regime.Regime)

	// Create decision
	decision := TradingDecision{
		ID:             uuid.New(),
		Timestamp:      time.Now(),
		RegimeSnapshot: regime,
		FinalScore:     score,
		Factors:        factors,
		BaseSize:       baseParams.PositionSize,
		GridSpacing:    baseParams.GridSpacing,
	}

	// Process through handler
	adaptedParams, err := ca.Handler.ProcessDecision(context.Background(), decision, baseParams)
	if err != nil {
		return &decision, nil, err
	}

	return &decision, adaptedParams, nil
}
