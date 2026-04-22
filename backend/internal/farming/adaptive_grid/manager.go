package adaptive_grid

import (
	"context"
	"fmt"
	"math"
	"runtime/debug"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_config"
	"aster-bot/internal/farming/market_regime"
	"aster-bot/internal/realtime"

	"go.uber.org/zap"
)

// GridManagerInterface defines the interface needed from GridManager
type GridManagerInterface interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetActivePositions(symbol string) ([]interface{}, error)
	CancelAllOrders(ctx context.Context, symbol string) error
	ClearGrid(ctx context.Context, symbol string) error
	RebuildGrid(ctx context.Context, symbol string) error
	SetOrderSize(size float64)
	SetGridSpread(spread float64)
	SetMaxOrdersPerSide(max int)
	SetPositionTimeout(minutes int)
}

// FuturesClientInterface defines methods needed from futures client for position tracking
type FuturesClientInterface interface {
	GetPositions(ctx context.Context) ([]client.Position, error)
	GetAccountInfo(ctx context.Context) (*client.AccountInfo, error)
	PlaceOrder(ctx context.Context, req client.PlaceOrderRequest) (*client.Order, error)
}

// ConfigManagerInterface defines methods needed from config manager
type ConfigManagerInterface interface {
	LoadConfig() error
	IsEnabled() bool
	GetRegimeConfig(regime string) (*config.RegimeConfig, bool)
	GetLastReload() time.Time
}

// SymbolPosition tracks position state for a symbol
type SymbolPosition struct {
	PositionAmt      float64 // Positive = Long, Negative = Short
	EntryPrice       float64
	MarkPrice        float64
	UnrealizedPnL    float64
	LiquidationPrice float64
	Leverage         float64
	NotionalValue    float64 // |PositionAmt| * MarkPrice
	LastUpdated      time.Time
}

// AdaptiveGridManager extends GridManager with regime-aware functionality and risk management
type AdaptiveGridManager struct {
	gridManager      GridManagerInterface
	configManager    ConfigManagerInterface
	regimeDetector   *market_regime.RegimeDetector
	futuresClient    FuturesClientInterface
	positionProvider PositionProvider // NEW: WebSocket position provider
	marketProvider   realtime.MarketStateProvider
	apiBaseURL       string // API base URL for historical data fetch
	logger           *zap.Logger
	mu               sync.RWMutex

	// Adaptive state
	currentRegime      map[string]market_regime.MarketRegime
	lastRegimeChange   map[string]time.Time
	transitionCooldown map[string]time.Time

	// Risk management - Circuit Breaker
	circuitBreakers   map[string]*CircuitBreaker
	trendingStrength  map[string]float64 // 0-1, đo lường độ mạnh của xu hướng
	tradingPaused     map[string]bool    // true = tạm dừng trading
	maxPositionSize   map[string]float64 // Giới hạn position size (USDT)
	unhedgedExposure  map[string]float64 // Vị thế chưa hedged
	trailingStopPrice map[string]float64 // Giá trailing stop

	// Position tracking from exchange
	positions          map[string]*SymbolPosition // symbol -> position
	positionStopLoss   map[string]float64         // symbol -> stop loss price
	positionTakeProfit map[string]float64         // symbol -> take profit price
	maxUnrealizedLoss  map[string]float64         // symbol -> max allowed unrealized loss (USDT)

	// Risk parameters
	riskConfig *RiskConfig

	// NEW: Dynamic timeout config for EXIT_ALL state
	dynamicTimeoutConfig *config.DynamicTimeoutConfig

	// NEW: RiskMonitor for dynamic sizing and exposure
	riskMonitor *RiskMonitor

	// NEW: RangeDetector for breakout detection
	rangeDetectors map[string]*RangeDetector // symbol -> range detector

	// NEW: Track ENTER_GRID transition time to avoid immediate breakout
	enterGridTime map[string]time.Time // symbol -> time when entered ENTER_GRID state

	// NEW: VolumeScaler for pyramid/tapered sizing
	volumeScalers map[string]*VolumeScaler // symbol -> volume scaler

	// NEW: TimeFilter for time-based trading rules
	timeFilter *TimeFilter

	// NEW: SpreadProtection for monitoring orderbook spread
	spreadProtection *SpreadProtection

	// NEW: DynamicSpreadCalculator for ATR-based spreads
	dynamicSpreadCalc *DynamicSpreadCalculator

	// NEW: InventoryManager for tracking inventory skew
	inventoryMgr *InventoryManager

	// NEW: ClusterManager for cluster stop-loss
	clusterMgr *ClusterManager

	// NEW: TrendDetector for RSI-based trend detection
	trendDetector *TrendDetector

	// NEW: FundingRateMonitor for funding rate tracking
	fundingMonitor *FundingRateMonitor

	// NEW: WebSocket client for real-time data
	wsClient *client.WebSocketClient

	// NEW: ExitExecutor for fast exit on breakouts
	exitExecutor interface{} // Use interface to avoid circular dependency

	// NEW: ModeManager for trading mode evaluation (DEPRECATED - being replaced by CircuitBreaker)
	modeManager interface{} // Use interface to avoid circular dependency

	// NEW: Agentic CircuitBreaker for unified trading decisions (replaces ModeManager)
	circuitBreaker interface{} // Use interface to avoid circular dependency

	// NEW: ATRCalculator for volatility calculation
	atrCalc *ATRCalculator

	// NEW: RSICalculator for RSI calculation
	rsiCalc *RSICalculator

	// NEW: Global trading pause state (for VPIN toxic flow)
	tradingPausedGlobal bool   // true = global trading paused
	pauseReason         string // reason for global pause (e.g., "toxic_vpin")

	// NEW: OptimizationConfig from YAML files
	optConfig *config.OptimizationConfig

	// NEW: MicroGridCalculator for high-frequency micro grid trading
	microGridCalc *MicroGridCalculator

	// Trading lifecycle state machine for runtime execution governance
	stateMachine *GridStateMachine

	// NEW: RealTimeOptimizer for real-time parameter optimization
	realtimeOptimizer *RealTimeOptimizer

	// NEW: LearningEngine for adaptive threshold learning
	learningEngine *LearningEngine

	// NEW: FluidFlowEngine for continuous flow behavior (fluid like water)
	fluidFlowEngine *FluidFlowEngine

	// NEW: Consecutive loss tracking for cooldown
	consecutiveLosses map[string]int       // symbol -> consecutive loss count
	lastLossTime      map[string]time.Time // symbol -> last loss timestamp
	cooldownActive    map[string]bool      // symbol -> cooldown status
	totalLossesToday  int                  // total losses today

	// NEW: Kline counters for periodic learning engine adaptation
	klineCounters map[string]int // symbol -> kline counter

	// NEW: Partial close tracking for TP levels
	partialCloseConfig *PartialCloseConfig

	// NEW: ConditionBlocker for conditional blocking (replaces state-based blocking)
	conditionBlocker *ConditionBlocker
	positionSlices   map[string]*PositionSlice // symbol -> position slice tracking

	// NEW: Position reduction tracking for gradual size reduction
	positionReductionTime map[string]time.Time // symbol -> last reduction timestamp

	// NEW: VPIN monitor for toxic flow detection
	vpinMonitor interface {
		IsToxic() bool
		TriggerPause()
		Resume()
		UpdateVolume(buyVol, sellVol float64)
	}

	// Control channels
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// PartialCloseConfig holds configuration for partial take-profit strategy
type PartialCloseConfig struct {
	Enabled          bool    `yaml:"enabled"`
	TP1_ClosePct     float64 `yaml:"tp1_close_pct"`      // e.g., 0.30 = 30%
	TP1_ProfitPct    float64 `yaml:"tp1_profit_pct"`     // e.g., 0.005 = 0.5%
	TP2_ClosePct     float64 `yaml:"tp2_close_pct"`      // e.g., 0.40 = 40%
	TP2_ProfitPct    float64 `yaml:"tp2_profit_pct"`     // e.g., 0.01 = 1.0%
	TP3_ClosePct     float64 `yaml:"tp3_close_pct"`      // e.g., 0.30 = 30%
	TP3_ProfitPct    float64 `yaml:"tp3_profit_pct"`     // e.g., 0.015 = 1.5%
	TrailingAfterTP2 bool    `yaml:"trailing_after_tp2"` // Enable trailing after TP2
	TrailingDistance float64 `yaml:"trailing_distance"`  // Trailing distance %
}

// DefaultPartialCloseConfig returns default partial close configuration
func DefaultPartialCloseConfig() *PartialCloseConfig {
	return &PartialCloseConfig{
		Enabled:          true,
		TP1_ClosePct:     0.30,  // Close 30% at TP1
		TP1_ProfitPct:    0.005, // 0.5% profit
		TP2_ClosePct:     0.40,  // Close 40% at TP2
		TP2_ProfitPct:    0.01,  // 1.0% profit
		TP3_ClosePct:     0.30,  // Close 30% at TP3
		TP3_ProfitPct:    0.015, // 1.5% profit
		TrailingAfterTP2: true,
		TrailingDistance: 0.005, // 0.5% trailing distance
	}
}

// TPLevel represents a single take-profit level
type TPLevel struct {
	TargetPct   float64 // Profit % to trigger
	ClosePct    float64 // % of position to close
	IsHit       bool    // Whether this TP was hit
	ExecutedQty float64 // Actual quantity closed
}

// PositionSlice tracks position divided into slices for partial close
type PositionSlice struct {
	Symbol         string
	OriginalSize   float64   // Original position size
	RemainingSize  float64   // Remaining after partial closes
	ClosedPct      float64   // Total % closed so far
	EntryPrice     float64   // Entry price
	Side           string    // "LONG" or "SHORT"
	TPLevels       []TPLevel // TP1, TP2, TP3
	TrailingActive bool      // Trailing stop activated
	TrailingPrice  float64   // Current trailing stop price
	CreatedAt      time.Time
}

// RiskConfig holds risk management parameters
type RiskConfig struct {
	MaxPositionUSDT          float64       // Max position size per symbol (USDT)
	MaxUnhedgedExposureUSDT  float64       // Max unhedged exposure (USDT)
	MaxUnrealizedLossUSDT    float64       // Max unrealized loss before emergency close per position
	PerPositionLossLimitUSDT float64       // Max unrealized loss per position (1 USDT default)
	TotalNetLossLimitUSDT    float64       // Max total net unrealized loss before close all (5 USDT default)
	StopLossPct              float64       // Stop loss percentage from entry (e.g., 0.02 = 2%)
	TrailingStopPct          float64       // Trailing stop activation (e.g., 0.01 = 1% profit)
	TrailingStopDistancePct  float64       // Trailing stop distance (e.g., 0.005 = 0.5%)
	LiquidationBufferPct     float64       // Close before liquidation (e.g., 0.2 = 20% away)
	PositionCheckInterval    time.Duration // How often to check positions
	TrendingThreshold        float64       // Trending strength to pause trading

	// Take-profit fields
	TakeProfitRRatio float64 // Target R:R ratio (e.g., 1.5 = 1.5:1)
	MinTakeProfitPct float64 // Minimum TP as % (e.g., 0.01 = 1%)
	MaxTakeProfitPct float64 // Maximum TP as % (e.g., 0.05 = 5%)

	// Directional bias field
	UseDirectionalBias bool // Only trade with trend (no counter-trend)

	// NEW: Consecutive loss tracking - GIẢM cho volume farming
	MaxConsecutiveLosses int           // Max consecutive losses before cooldown (default 5)
	CooldownDuration     time.Duration // Cooldown duration after max losses (default 30s)

	// NEW: Dynamic spread adjustment
	BaseGridSpreadPct float64 // Base grid spread percentage (e.g., 0.0015 = 0.15%)

	// NEW: Conditional transitions config
	ConditionalTransitions *config.ConditionalTransitionsConfig
}

// ConvertRiskConfig converts config.RiskConfig to adaptive_grid.RiskConfig
func ConvertRiskConfig(cfg config.RiskConfig) *RiskConfig {
	return &RiskConfig{
		MaxPositionUSDT:          cfg.MaxPositionUSDTPerSymbol,
		MaxUnhedgedExposureUSDT:  cfg.MaxTotalPositionsUSDT,
		MaxUnrealizedLossUSDT:    3.0, // Default, can be overridden
		PerPositionLossLimitUSDT: 1.0, // Default, can be overridden
		TotalNetLossLimitUSDT:    5.0, // Default, can be overridden
		StopLossPct:              cfg.PerTradeStopLossPct,
		TrailingStopPct:          0.01,             // Default 1%
		TrailingStopDistancePct:  0.005,            // Default 0.5%
		LiquidationBufferPct:     0.35,             // Default 35% - more buffer to prevent liquidation
		PositionCheckInterval:    10 * time.Second, // Conservative to avoid API rate limits
		TrendingThreshold:        0.7,              // Default 70%

		// Take-profit from config
		TakeProfitRRatio: cfg.TakeProfitRRatio,
		MinTakeProfitPct: cfg.MinTakeProfitPct,
		MaxTakeProfitPct: cfg.MaxTakeProfitPct,

		// Directional bias from config
		UseDirectionalBias: true, // Default

		// Consecutive loss tracking - GIẢM cho volume farming
		MaxConsecutiveLosses: 3,                // Strict exit after 3 consecutive losses
		CooldownDuration:     30 * time.Second, // GIẢM xuống 30s (thay vì 5 phút)

		// Grid spread - default for volume farming
		BaseGridSpreadPct: 0.0015, // Default 0.15%

		// Conditional transitions from config
		ConditionalTransitions: cfg.ConditionalTransitions,
	}
}

// DefaultRiskConfig returns default risk configuration
func DefaultRiskConfig() *RiskConfig {
	return &RiskConfig{
		MaxPositionUSDT:          300.0,            // Max 300 USDT per symbol
		MaxUnhedgedExposureUSDT:  200.0,            // Max 200 USDT unhedged
		MaxUnrealizedLossUSDT:    3.0,              // Close if unrealized loss > 3 USDT per position
		PerPositionLossLimitUSDT: 1.0,              // Close position if unrealized loss > 1 USDT
		TotalNetLossLimitUSDT:    5.0,              // Close ALL if total net unrealized loss > 5 USDT
		StopLossPct:              0.01,             // 1% stop loss
		TrailingStopPct:          0.01,             // Activate at 1% profit
		TrailingStopDistancePct:  0.005,            // 0.5% trailing distance
		LiquidationBufferPct:     0.35,             // Close at 35% away from liquidation - more safety margin
		PositionCheckInterval:    10 * time.Second, // Conservative to avoid API rate limits to prevent liquidation
		TrendingThreshold:        0.7,              // Pause if trending > 70%

		// Take-profit defaults
		TakeProfitRRatio: 1.5,  // 1.5:1 R:R
		MinTakeProfitPct: 0.01, // Minimum 1% TP
		MaxTakeProfitPct: 0.05, // Maximum 5% TP

		// Directional bias default
		UseDirectionalBias: true, // Only trade with trend by default

		// Consecutive loss tracking - GIẢM cho volume farming
		MaxConsecutiveLosses: 3,                // Strict exit after 3 consecutive losses
		CooldownDuration:     30 * time.Second, // GIẢM xuống 30s (thay vì 5 phút)
	}
}

// MultiLayerLiquidationConfig holds 4-tier liquidation protection settings
type MultiLayerLiquidationConfig struct {
	Enabled          bool    `yaml:"enabled" mapstructure:"enabled"`                       // Enable multi-layer protection
	Layer1WarnPct    float64 `yaml:"layer1_warn_pct" mapstructure:"layer1_warn_pct"`       // Warn at 50% distance to liq
	Layer2ReducePct  float64 `yaml:"layer2_reduce_pct" mapstructure:"layer2_reduce_pct"`   // Reduce 50% at 30% distance
	Layer3ClosePct   float64 `yaml:"layer3_close_pct" mapstructure:"layer3_close_pct"`     // Close 100% at 15% distance
	Layer4HedgePct   float64 `yaml:"layer4_hedge_pct" mapstructure:"layer4_hedge_pct"`     // Hedge + close at 10% distance
	ReducePositionBy float64 `yaml:"reduce_position_by" mapstructure:"reduce_position_by"` // Reduce by 50% (0.5)
}

// DefaultMultiLayerLiquidationConfig returns default 4-tier protection
func DefaultMultiLayerLiquidationConfig() *MultiLayerLiquidationConfig {
	return &MultiLayerLiquidationConfig{
		Enabled:          true, // Enabled by default for 4-tier protection
		Layer1WarnPct:    0.50, // 50% distance - warning only
		Layer2ReducePct:  0.30, // 30% distance - reduce position
		Layer3ClosePct:   0.15, // 15% distance - close all
		Layer4HedgePct:   0.10, // 10% distance - emergency hedge + close
		ReducePositionBy: 0.50, // Reduce by 50%
	}
}

// LiquidationTier represents a single protection tier
type LiquidationTier struct {
	Name         string    // Tier name (Warn, Reduce, Close, Hedge)
	DistancePct  float64   // Distance to liquidation (e.g., 0.50 = 50%)
	Action       string    // Action to take (WARN, REDUCE, CLOSE, HEDGE)
	ReductionPct float64   // For REDUCE action: reduce by this %
	IsActive     bool      // Whether this tier is currently active
	TriggeredAt  time.Time // When this tier was triggered
}

// CircuitBreaker tự động dừng trading khi trending quá mạnh
type CircuitBreaker struct {
	maxTrendingStrength float64       // Ngưỡng trending để dừng (0.7 = 70%)
	cooldownDuration    time.Duration // Thời gian nghỉ sau khi dừng
	lastTriggered       time.Time     // Lần cuối trigger
	triggerCount        int           // Số lần trigger trong ngày
}

// NewCircuitBreaker tạo circuit breaker mới
func NewCircuitBreaker(maxStrength float64, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxTrendingStrength: maxStrength,
		cooldownDuration:    cooldown,
		triggerCount:        0,
	}
}

// ShouldBreak kiểm tra có nên dừng trading không
func (cb *CircuitBreaker) ShouldBreak(trendingStrength float64) bool {
	if trendingStrength > cb.maxTrendingStrength {
		cb.lastTriggered = time.Now()
		cb.triggerCount++
		return true
	}
	return false
}

// IsInCooldown kiểm tra có đang trong thời gian nghỉ không
func (cb *CircuitBreaker) IsInCooldown() bool {
	return time.Since(cb.lastTriggered) < cb.cooldownDuration
}

// NewAdaptiveGridManager creates a new adaptive grid manager
func NewAdaptiveGridManager(
	baseGrid GridManagerInterface,
	configManager *adaptive_config.AdaptiveConfigManager,
	regimeDetector *market_regime.RegimeDetector,
	futuresClient FuturesClientInterface,
	positionProvider PositionProvider, // NEW: WebSocket position provider
	marketProvider realtime.MarketStateProvider,
	apiBaseURL string, // API base URL for historical data fetch
	logger *zap.Logger,
) *AdaptiveGridManager {
	return &AdaptiveGridManager{
		gridManager:           baseGrid,
		configManager:         configManager,
		regimeDetector:        regimeDetector,
		futuresClient:         futuresClient,
		positionProvider:      positionProvider, // NEW: WebSocket position provider
		marketProvider:        marketProvider,
		apiBaseURL:            apiBaseURL, // API base URL
		logger:                logger,
		currentRegime:         make(map[string]market_regime.MarketRegime),
		lastRegimeChange:      make(map[string]time.Time),
		transitionCooldown:    make(map[string]time.Time),
		circuitBreakers:       make(map[string]*CircuitBreaker),
		trendingStrength:      make(map[string]float64),
		tradingPaused:         make(map[string]bool),
		maxPositionSize:       make(map[string]float64),
		unhedgedExposure:      make(map[string]float64),
		trailingStopPrice:     make(map[string]float64),
		positions:             make(map[string]*SymbolPosition),
		positionStopLoss:      make(map[string]float64),
		positionTakeProfit:    make(map[string]float64),
		maxUnrealizedLoss:     make(map[string]float64),
		riskConfig:            DefaultRiskConfig(),
		rangeDetectors:        make(map[string]*RangeDetector), // NEW: Range detectors
		enterGridTime:         make(map[string]time.Time),      // NEW: ENTER_GRID transition time
		volumeScalers:         make(map[string]*VolumeScaler),  // NEW: Volume scalers
		timeFilter:            nil,                             // NEW: Time filter (init later)
		consecutiveLosses:     make(map[string]int),            // NEW: Track consecutive losses
		lastLossTime:          make(map[string]time.Time),      // NEW: Track last loss time
		cooldownActive:        make(map[string]bool),           // NEW: Track cooldown state
		positionReductionTime: make(map[string]time.Time),      // NEW: Track last position reduction time
		klineCounters:         make(map[string]int),            // NEW: Track kline counters for learning engine
		microGridCalc:         NewMicroGridCalculator(nil),     // NEW: Micro grid calculator (disabled by default)
		stopCh:                make(chan struct{}),
		mu:                    sync.RWMutex{},
	}
}

// SetRiskConfig updates risk configuration
func (a *AdaptiveGridManager) SetRiskConfig(config *RiskConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.riskConfig = config
	a.logger.Info("Risk config updated",
		zap.Float64("max_position", config.MaxPositionUSDT),
		zap.Float64("max_unhedged", config.MaxUnhedgedExposureUSDT),
		zap.Float64("max_unrealized_loss", config.MaxUnrealizedLossUSDT),
		zap.Float64("stop_loss_pct", config.StopLossPct),
	)
}

// SetStateMachine wires the shared runtime trading lifecycle state machine.
func (a *AdaptiveGridManager) SetStateMachine(sm *GridStateMachine) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stateMachine = sm
}

// GetStateMachine returns the state machine
func (a *AdaptiveGridManager) GetStateMachine() *GridStateMachine {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stateMachine
}

// SetRealTimeOptimizer sets the real-time optimizer
func (a *AdaptiveGridManager) SetRealTimeOptimizer(optimizer *RealTimeOptimizer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.realtimeOptimizer = optimizer
	a.logger.Info("RealTimeOptimizer set on AdaptiveGridManager")
}

// GetRealTimeOptimizer returns the real-time optimizer
func (a *AdaptiveGridManager) GetRealTimeOptimizer() *RealTimeOptimizer {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.realtimeOptimizer
}

// SetLearningEngine sets the learning engine
func (a *AdaptiveGridManager) SetLearningEngine(engine *LearningEngine) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.learningEngine = engine
	a.logger.Info("LearningEngine set on AdaptiveGridManager")
}

// GetLearningEngine returns the learning engine
func (a *AdaptiveGridManager) GetLearningEngine() *LearningEngine {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.learningEngine
}

// SetFluidFlowEngine sets the fluid flow engine for continuous flow behavior
func (a *AdaptiveGridManager) SetFluidFlowEngine(engine *FluidFlowEngine) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fluidFlowEngine = engine
	a.logger.Info("FluidFlowEngine set on AdaptiveGridManager")
}

// GetFluidFlowEngine returns the fluid flow engine
func (a *AdaptiveGridManager) GetFluidFlowEngine() *FluidFlowEngine {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.fluidFlowEngine
}

// SetVPINMonitor sets the VPIN monitor for toxic flow detection
func (a *AdaptiveGridManager) SetVPINMonitor(vpinMonitor interface {
	IsToxic() bool
	TriggerPause()
	Resume()
	UpdateVolume(buyVol, sellVol float64)
}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.vpinMonitor = vpinMonitor
	a.logger.Info("VPINMonitor set on AdaptiveGridManager")
}

// GetVPINMonitor returns the VPIN monitor
func (a *AdaptiveGridManager) GetVPINMonitor() interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.vpinMonitor
}

// GetRiskMonitor returns the risk monitor
func (a *AdaptiveGridManager) GetRiskMonitor() *RiskMonitor {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.riskMonitor
}

// GetRiskConfig returns the risk configuration
func (a *AdaptiveGridManager) GetRiskConfig() *RiskConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.riskConfig
}

// SetDynamicTimeoutConfig sets the dynamic timeout configuration
func (a *AdaptiveGridManager) SetDynamicTimeoutConfig(config *config.DynamicTimeoutConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dynamicTimeoutConfig = config
	a.logger.Info("Dynamic timeout config set on AdaptiveGridManager")
}

// BuildTransitionConditions builds a conditions map for conditional transitions
// This aggregates current market and position conditions for transition evaluation
func (a *AdaptiveGridManager) BuildTransitionConditions(symbol string) map[string]float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	conditions := make(map[string]float64)

	// Position conditions
	if position, exists := a.positions[symbol]; exists && position != nil {
		positionPct := position.NotionalValue / a.riskConfig.MaxPositionUSDT
		conditions["position_pct"] = positionPct
		conditions["pnl"] = position.UnrealizedPnL
		conditions["notional"] = position.NotionalValue
	}

	// Volatility conditions
	if detector, exists := a.rangeDetectors[symbol]; exists {
		avgADX := detector.averageADXLocked()
		// Normalize ADX to 0-1 range (ADX 0-100)
		conditions["volatility"] = avgADX / 100.0
		conditions["adx"] = avgADX

		// BB width for volatility context
		if detector.currentRange != nil {
			conditions["bb_width"] = detector.currentRange.WidthPct
		}
	}

	// Regime conditions (convert to numeric)
	regime := a.GetCurrentRegime(symbol)
	switch regime {
	case market_regime.RegimeRanging:
		conditions["regime"] = 0.0
	case market_regime.RegimeTrending:
		conditions["regime"] = 1.0
	case market_regime.RegimeVolatile:
		conditions["regime"] = 2.0
	default:
		conditions["regime"] = 0.5
	}

	return conditions
}

// TryConditionalTransition attempts a conditional transition if enabled
// Falls back to regular transition if conditional transitions disabled
func (a *AdaptiveGridManager) TryConditionalTransition(symbol string, event GridEvent) bool {
	// Check if conditional transitions are enabled
	stateMachine := a.stateMachine
	if stateMachine == nil {
		return false
	}

	// Check if conditional transitions are enabled and config is available
	enabled := false
	confidenceThreshold := 0.6 // default

	if a.riskConfig != nil && a.riskConfig.ConditionalTransitions != nil {
		enabled = a.riskConfig.ConditionalTransitions.Enabled
		confidenceThreshold = a.riskConfig.ConditionalTransitions.ConfidenceThreshold
	}

	if enabled {
		// Build conditions
		conditions := a.BuildTransitionConditions(symbol)
		// Try conditional transition
		return stateMachine.TransitionWithConfidence(symbol, event, conditions, confidenceThreshold)
	}

	// Fall back to regular transition
	return stateMachine.Transition(symbol, event)
}

// GetTradingMode returns the current trading mode for a symbol from CircuitBreaker
func (a *AdaptiveGridManager) GetTradingMode(symbol string) string {
	// This will be called by the circuit breaker
	// For now, return a default mode
	// TODO: Integrate with CircuitBreaker to get actual mode
	return "FULL"
}

// GetModeSizeMultiplier returns the size multiplier for a trading mode
func (a *AdaptiveGridManager) GetModeSizeMultiplier(mode string) float64 {
	switch mode {
	case "FULL":
		return 1.0
	case "REDUCED":
		return 0.5
	case "MICRO":
		return 0.1
	case "PAUSED":
		return 0.0
	default:
		return 1.0
	}
}

// CalculateExitPercentage calculates the graduated exit percentage based on conditions
// Returns percentage (0-1) of position to exit
func (a *AdaptiveGridManager) CalculateExitPercentage(symbol string, volatility, timeInPosition float64, regime string) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Get position PnL
	position, exists := a.positions[symbol]
	if !exists {
		return 1.0 // Default to full exit if no position
	}

	pnlRatio := position.UnrealizedPnL / position.NotionalValue

	// Use config thresholds if available, otherwise use defaults
	smallLossThreshold := -0.02   // 2% loss
	mediumLossThreshold := -0.05  // 5% loss
	largeLossThreshold := -0.10   // 10% loss
	extremeLossThreshold := -0.15 // 15% loss

	smallLossExit := 0.25
	mediumLossExit := 0.50
	largeLossExit := 0.75
	extremeLossExit := 1.0

	if a.riskConfig != nil && a.riskConfig.ConditionalTransitions != nil {
		// For now, use hardcoded defaults
		// TODO: Wire graduated exit config from RiskConfig
	}

	// Calculate exit percentage based on loss
	if pnlRatio < extremeLossThreshold {
		// Extreme loss: 100% exit
		a.logger.Info("Extreme loss detected, full exit",
			zap.String("symbol", symbol),
			zap.Float64("pnl_ratio", pnlRatio),
			zap.Float64("exit_percentage", extremeLossExit))
		return extremeLossExit
	} else if pnlRatio < largeLossThreshold {
		// Large loss: 75% exit
		a.logger.Info("Large loss detected, partial exit",
			zap.String("symbol", symbol),
			zap.Float64("pnl_ratio", pnlRatio),
			zap.Float64("exit_percentage", largeLossExit))
		return largeLossExit
	} else if pnlRatio < mediumLossThreshold {
		// Medium loss: 50% exit
		a.logger.Info("Medium loss detected, partial exit",
			zap.String("symbol", symbol),
			zap.Float64("pnl_ratio", pnlRatio),
			zap.Float64("exit_percentage", mediumLossExit))
		return mediumLossExit
	} else if pnlRatio < smallLossThreshold {
		// Small loss: 25% exit
		a.logger.Info("Small loss detected, partial exit",
			zap.String("symbol", symbol),
			zap.Float64("pnl_ratio", pnlRatio),
			zap.Float64("exit_percentage", smallLossExit))
		return smallLossExit
	}

	// Profit or neutral: no forced exit
	return 0.0
}

// GetRangeDetector returns the range detector for a specific symbol
func (a *AdaptiveGridManager) GetRangeDetector(symbol string) *RangeDetector {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rangeDetectors[symbol]
}

// callRealTimeOptimizer calls the real-time optimizer to update parameters
func (a *AdaptiveGridManager) callRealTimeOptimizer(symbol string, high, low, close, bid, ask float64) {
	if a.realtimeOptimizer == nil {
		return
	}

	// Get market conditions
	detector := a.rangeDetectors[symbol]
	if detector == nil {
		return
	}

	// Calculate volatility from ATR
	volatility := 0.0
	if a.atrCalc != nil {
		volatility = a.atrCalc.GetATR() / close
	}

	// Get regime (convert to string manually)
	regime := "RANGING" // Default
	currentRegime := a.GetCurrentRegime(symbol)
	if currentRegime == market_regime.RegimeTrending {
		regime = "TRENDING"
	} else if currentRegime == market_regime.RegimeVolatile {
		regime = "VOLATILE"
	}

	// Get skew from inventory (default to 0 for now)
	skew := 0.0
	// TODO: Get actual skew from inventory manager when available

	// Get funding rate
	funding := 0.0
	if a.fundingMonitor != nil {
		fundingInfo := a.fundingMonitor.GetFundingRate(symbol)
		if fundingInfo != nil {
			funding = fundingInfo.Rate
		}
	}

	// Get depth (estimate from range)
	depth := 0.5 // Default
	if detector.currentRange != nil {
		depth = 0.5 // TODO: Calculate actual depth from range
	}

	// Get risk (estimate from position)
	risk := 0.5 // Default
	if position, exists := a.positions[symbol]; exists {
		risk = position.NotionalValue / a.riskConfig.MaxPositionUSDT
	}

	// Get equity
	equity := 1000.0 // Default

	// Get opportunity
	opportunity := 0.5 // Default

	// Get liquidity
	liquidity := 0.5 // Default

	// Get drawdown
	drawdown := 0.0
	// TODO: Get actual drawdown from risk monitor when available

	// Get consecutive losses
	losses := 0
	if l, exists := a.consecutiveLosses[symbol]; exists {
		losses = l
	}

	currentTime := time.Now()

	// Optimize parameters
	newSpread := a.realtimeOptimizer.OptimizeSpread(volatility, skew, funding, currentTime)
	newOrderCount := a.realtimeOptimizer.OptimizeOrderCount(depth, risk, regime, currentTime)
	newSize := a.realtimeOptimizer.OptimizeSize(equity, risk, opportunity, liquidity, currentTime)
	newMode := a.realtimeOptimizer.OptimizeMode(risk, volatility, drawdown, losses, currentTime)

	// Log parameter changes - SAMPLING LOGGING to prevent log flooding
	// Only log when parameters change significantly (>5%)
	currentSpread, currentOrderCount, currentSize, currentMode := a.realtimeOptimizer.GetCurrentParameters()

	spreadChange := math.Abs(newSpread-currentSpread) / currentSpread
	sizeChange := math.Abs(newSize-currentSize) / currentSize

	shouldLog := false
	if newMode != currentMode {
		shouldLog = true
	} else if spreadChange > 0.05 || sizeChange > 0.05 {
		shouldLog = true
	} else if newOrderCount != currentOrderCount {
		shouldLog = true
	}

	if shouldLog {
		a.logger.Info("Real-time optimizer updated parameters",
			zap.String("symbol", symbol),
			zap.Float64("old_spread", currentSpread),
			zap.Float64("new_spread", newSpread),
			zap.Int("old_order_count", currentOrderCount),
			zap.Int("new_order_count", newOrderCount),
			zap.Float64("old_size", currentSize),
			zap.Float64("new_size", newSize),
			zap.String("old_mode", currentMode),
			zap.String("new_mode", newMode))
	}
}

// SetExitExecutor wires the exit executor for fast exit on breakouts
func (a *AdaptiveGridManager) SetExitExecutor(executor interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.exitExecutor = executor
}

// GetActiveSymbols returns all symbols with active range detectors
func (a *AdaptiveGridManager) GetActiveSymbols() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	symbols := make([]string, 0, len(a.rangeDetectors))
	for symbol := range a.rangeDetectors {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// SetModeManager sets the mode manager reference trading mode evaluation
func (a *AdaptiveGridManager) SetModeManager(manager interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modeManager = manager
}

// SetCircuitBreaker sets the agentic circuit breaker for unified trading decisions
func (a *AdaptiveGridManager) SetCircuitBreaker(cb interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.circuitBreaker = cb
}

// SetOptimizationConfig sets the optimization config from YAML files
func (a *AdaptiveGridManager) SetOptimizationConfig(optConfig *config.OptimizationConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.optConfig = optConfig
	if optConfig != nil {
		a.logger.Info("Optimization config set",
			zap.Bool("dynamic_grid", optConfig.DynamicGrid != nil),
			zap.Bool("inventory_skew", optConfig.InventorySkew != nil),
			zap.Bool("cluster_stoploss", optConfig.ClusterStopLoss != nil),
			zap.Bool("trend_detection", optConfig.TrendDetection != nil),
			zap.Bool("safeguards", optConfig.Safeguards != nil),
			zap.Bool("time_filter", optConfig.TimeFilter != nil),
			zap.Bool("micro_grid", optConfig.MicroGrid != nil && optConfig.MicroGrid.Enabled),
			zap.Bool("dynamic_leverage", optConfig.DynamicLeverage != nil && optConfig.DynamicLeverage.Enabled),
		)

		// Initialize micro grid if enabled
		if optConfig.MicroGrid != nil && optConfig.MicroGrid.Enabled {
			a.microGridCalc = NewMicroGridCalculator(&MicroGridConfig{
				Enabled:          optConfig.MicroGrid.Enabled,
				SpreadPct:        optConfig.MicroGrid.SpreadPct,
				OrdersPerSide:    optConfig.MicroGrid.OrdersPerSide,
				OrderSizeUSDT:    optConfig.MicroGrid.OrderSizeUSDT,
				MinProfitPerFill: optConfig.MicroGrid.MinProfitPerFill,
			})
			a.logger.Info("Micro grid mode enabled",
				zap.Float64("spread_pct", optConfig.MicroGrid.SpreadPct),
				zap.Int("orders_per_side", optConfig.MicroGrid.OrdersPerSide),
				zap.Float64("order_size_usdt", optConfig.MicroGrid.OrderSizeUSDT))
		}

		// Initialize micro partial close if enabled
		if optConfig.MicroPartialClose != nil && optConfig.MicroPartialClose.Enabled {
			mp := optConfig.MicroPartialClose
			// Convert 4 micro TP levels to 3 partial close levels (using first 3)
			if len(mp.TPLevels) >= 3 {
				a.partialCloseConfig = &PartialCloseConfig{
					Enabled:          mp.Enabled,
					TP1_ClosePct:     mp.TPLevels[0].ClosePct,
					TP1_ProfitPct:    mp.TPLevels[0].TargetPct,
					TP2_ClosePct:     mp.TPLevels[1].ClosePct,
					TP2_ProfitPct:    mp.TPLevels[1].TargetPct,
					TP3_ClosePct:     mp.TPLevels[2].ClosePct,
					TP3_ProfitPct:    mp.TPLevels[2].TargetPct,
					TrailingAfterTP2: mp.TrailingAfterTP3, // Map TP3 trailing to TP2
					TrailingDistance: mp.TrailingDistance,
				}
				a.logger.Info("Micro partial close enabled",
					zap.Float64("tp1_target", mp.TPLevels[0].TargetPct),
					zap.Float64("tp2_target", mp.TPLevels[1].TargetPct),
					zap.Float64("tp3_target", mp.TPLevels[2].TargetPct),
					zap.Bool("trailing_after_tp3", mp.TrailingAfterTP3))
			}
		}
	}
}

// SetMicroGridMode enables or disables micro grid mode
func (a *AdaptiveGridManager) SetMicroGridMode(enabled bool, config *MicroGridConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if enabled && config != nil {
		a.microGridCalc = NewMicroGridCalculator(config)
		a.logger.Info("Micro grid mode enabled",
			zap.Float64("spread_pct", config.SpreadPct),
			zap.Int("orders_per_side", config.OrdersPerSide),
			zap.Float64("order_size_usdt", config.OrderSizeUSDT))
	} else {
		a.microGridCalc = NewMicroGridCalculator(nil)
		a.logger.Info("Micro grid mode disabled")
	}
}

// IsMicroGridEnabled returns whether micro grid mode is active
func (a *AdaptiveGridManager) IsMicroGridEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.microGridCalc != nil && a.microGridCalc.IsEnabled()
}

// GetMicroGridPrices returns buy and sell prices for micro grid
func (a *AdaptiveGridManager) GetMicroGridPrices(currentPrice float64) (buyPrices []float64, sellPrices []float64) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.microGridCalc == nil || !a.microGridCalc.IsEnabled() {
		return nil, nil
	}

	return a.microGridCalc.CalculateGridPrices(currentPrice)
}

// GetMicroGridOrderSize returns the order size for micro grid
func (a *AdaptiveGridManager) GetMicroGridOrderSize(price float64) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.microGridCalc == nil || !a.microGridCalc.IsEnabled() {
		return 0
	}

	return a.microGridCalc.CalculateOrderSize(price)
}

// Initialize sets up the adaptive grid manager
func (a *AdaptiveGridManager) Initialize(ctx context.Context) error {
	a.logger.Info("Initializing adaptive grid manager")

	// Load adaptive configuration
	if err := a.configManager.LoadConfig(); err != nil {
		return err
	}

	// Check if adaptive config is enabled
	if !a.configManager.IsEnabled() {
		a.logger.Warn("Adaptive configuration is disabled, using base grid manager")
		return nil
	}

	// Log if optimization config from YAML is available
	if a.optConfig != nil {
		a.logger.Info("Using optimization config from YAML files",
			zap.Bool("dynamic_grid", a.optConfig.DynamicGrid != nil),
			zap.Bool("inventory_skew", a.optConfig.InventorySkew != nil),
			zap.Bool("cluster_stoploss", a.optConfig.ClusterStopLoss != nil),
			zap.Bool("trend_detection", a.optConfig.TrendDetection != nil),
			zap.Bool("safeguards", a.optConfig.Safeguards != nil),
			zap.Bool("time_filter", a.optConfig.TimeFilter != nil))
	} else {
		a.logger.Warn("No optimization config from YAML, using hardcoded defaults")
	}

	// NEW: Initialize RiskMonitor for dynamic sizing
	enhancedConfig := DefaultEnhancedRiskConfig()
	a.riskMonitor = NewRiskMonitor(a.futuresClient, enhancedConfig, a.logger)
	a.riskMonitor.Start(ctx)
	a.logger.Info("RiskMonitor started with dynamic sizing",
		zap.Float64("base_notional", enhancedConfig.BaseOrderNotional),
		zap.Float64("max_exposure_pct", enhancedConfig.MaxTotalExposurePct),
	)

	// NEW: Initialize TimeFilter for time-based trading rules
	var timeFilterConfig *TradingHoursConfig
	if a.optConfig != nil && a.optConfig.TimeFilter != nil {
		timeFilterConfig = ConvertTimeFilterConfig(a.optConfig.TimeFilter)
		a.logger.Info("TimeFilter using loaded config",
			zap.String("mode", a.optConfig.TimeFilter.Mode),
			zap.Int("slots", len(a.optConfig.TimeFilter.Slots)))
	} else {
		timeFilterConfig = DefaultTradingHoursConfig()
		a.logger.Info("TimeFilter using default config")
	}
	timeFilter, err := NewTimeFilter(timeFilterConfig, a.logger)
	if err != nil {
		a.logger.Warn("Failed to initialize TimeFilter, using default", zap.Error(err))
	} else {
		a.timeFilter = timeFilter
		a.logger.Info("TimeFilter initialized",
			zap.String("mode", string(timeFilterConfig.Mode)),
			zap.String("timezone", timeFilterConfig.Timezone))
	}

	// NEW: Initialize SpreadProtection for orderbook spread monitoring
	a.spreadProtection = NewSpreadProtection(a.logger)
	a.logger.Info("SpreadProtection initialized")

	// NEW: Initialize DynamicSpreadCalculator for ATR-based spreads
	var dynamicSpreadConfig *DynamicSpreadConfig
	if a.optConfig != nil && a.optConfig.DynamicGrid != nil {
		dynamicSpreadConfig = ConvertDynamicGridConfig(a.optConfig.DynamicGrid)
		a.logger.Info("DynamicSpreadCalculator using loaded config",
			zap.Float64("base_spread_pct", a.optConfig.DynamicGrid.BaseSpreadPct),
			zap.Float64("low_mult", a.optConfig.DynamicGrid.SpreadMultipliers.Low))
	} else {
		dynamicSpreadConfig = DefaultDynamicSpreadConfig()
		a.logger.Info("DynamicSpreadCalculator using default config")
	}
	a.dynamicSpreadCalc = NewDynamicSpreadCalculator(dynamicSpreadConfig, a.logger)
	a.logger.Info("DynamicSpreadCalculator initialized",
		zap.Float64("base_spread_pct", dynamicSpreadConfig.BaseSpreadPct),
		zap.Float64("low_threshold", dynamicSpreadConfig.LowThreshold))

	// NEW: Initialize InventoryManager for inventory skew tracking
	var inventoryConfig *InventoryConfig
	if a.optConfig != nil && a.optConfig.InventorySkew != nil {
		inventoryConfig = ConvertInventoryConfig(a.optConfig.InventorySkew)
		a.logger.Info("InventoryManager using loaded config",
			zap.Float64("max_inventory_pct", a.optConfig.InventorySkew.MaxInventoryPct))
	} else {
		inventoryConfig = DefaultInventoryConfig()
		a.logger.Info("InventoryManager using default config")
	}
	a.inventoryMgr = NewInventoryManager(inventoryConfig, a.logger)
	a.logger.Info("InventoryManager initialized",
		zap.Float64("max_inventory_pct", inventoryConfig.MaxInventoryPct))

	// NEW: Initialize ClusterManager for cluster stop-loss
	var clusterConfig *ClusterStopLossConfig
	if a.optConfig != nil && a.optConfig.ClusterStopLoss != nil {
		clusterConfig = ConvertClusterStopLossConfig(a.optConfig.ClusterStopLoss)
		a.logger.Info("ClusterManager using loaded config",
			zap.Float64("monitor_hours", a.optConfig.ClusterStopLoss.TimeThresholds.Monitor))
	} else {
		clusterConfig = DefaultClusterStopLossConfig()
		a.logger.Info("ClusterManager using default config")
	}
	a.clusterMgr = NewClusterManager(clusterConfig, a.logger)
	a.logger.Info("ClusterManager initialized",
		zap.Float64("monitor_hours", clusterConfig.MonitorHours),
		zap.Float64("emergency_hours", clusterConfig.EmergencyHours))

	// NEW: Initialize TrendDetector for trend detection
	var trendConfig *TrendDetectionConfig
	if a.optConfig != nil && a.optConfig.TrendDetection != nil {
		trendConfig = ConvertTrendDetectionConfig(a.optConfig.TrendDetection)
		a.logger.Info("TrendDetector using loaded config",
			zap.Int("rsi_period", a.optConfig.TrendDetection.RSI.Period),
			zap.String("timeframe", a.optConfig.TrendDetection.RSI.Timeframe))
	} else {
		trendConfig = DefaultTrendDetectionConfig()
		a.logger.Info("TrendDetector using default config")
	}
	a.trendDetector = NewTrendDetector(trendConfig, a.logger)
	a.logger.Info("TrendDetector initialized",
		zap.Int("rsi_period", trendConfig.RSIPeriod))

	// NEW: Initialize FundingRateMonitor (pass nil client for now)
	fundingConfig := DefaultFundingProtectionConfig()
	a.fundingMonitor = NewFundingRateMonitor(fundingConfig, nil, a.logger)
	a.logger.Info("FundingRateMonitor initialized",
		zap.Float64("high_threshold", fundingConfig.HighThreshold))

	// NEW: Initialize ATRCalculator with default period (or from YAML config if available)
	atrPeriod := 14
	if a.optConfig != nil && a.optConfig.DynamicGrid != nil && a.optConfig.DynamicGrid.ATRPeriod > 0 {
		atrPeriod = a.optConfig.DynamicGrid.ATRPeriod
		a.logger.Info("Using ATR period from YAML config", zap.Int("period", atrPeriod))
	}
	a.atrCalc = NewATRCalculator(atrPeriod)
	a.logger.Info("ATRCalculator initialized", zap.Int("period", atrPeriod))

	// NEW: Initialize RSICalculator with default period (or from YAML config if available)
	rsiPeriod := 14
	if a.optConfig != nil && a.optConfig.TrendDetection != nil && a.optConfig.TrendDetection.RSI.Period > 0 {
		rsiPeriod = a.optConfig.TrendDetection.RSI.Period
		a.logger.Info("Using RSI period from YAML config", zap.Int("period", rsiPeriod))
	}
	a.rsiCalc = NewRSICalculator(rsiPeriod)
	a.logger.Info("RSICalculator initialized", zap.Int("period", rsiPeriod))

	// Start position monitoring goroutine
	a.wg.Add(1)
	go a.positionMonitor(ctx)

	// NEW: Start real-time exit signal monitoring goroutine
	// Monitors ADX/BB every tick for immediate exit conditions
	a.wg.Add(1)
	go a.realtimeExitMonitor(ctx)

	// NEW: Start time slot monitoring goroutine
	a.wg.Add(1)
	go a.slotMonitor(ctx)

	// NEW: Start cleanup worker to ensure clean state for non-trading symbols
	a.wg.Add(1)
	go a.cleanupWorker(ctx)

	a.logger.Info("Adaptive grid manager initialized successfully",
		zap.Bool("enabled", a.configManager.IsEnabled()),
		zap.Time("last_reload", a.configManager.GetLastReload()))

	return nil
}

// slotMonitor monitors time slot changes and triggers grid transitions
func (a *AdaptiveGridManager) slotMonitor(ctx context.Context) {
	defer a.wg.Done()

	// Check every 30 seconds for slot changes
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initialize slot tracking on first run
	if a.timeFilter != nil {
		a.timeFilter.UpdateSlotTracking()
		a.logger.Info("Slot monitor initialized",
			zap.String("current_slot", a.GetCurrentSlot().Description))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			if a.timeFilter != nil {
				if changed := a.timeFilter.UpdateSlotTracking(); changed {
					// Slot changed - handle transition for all active symbols
					a.handleSlotTransitionForAll(ctx)
				}
			}
		}
	}
}

// cleanupWorker ensures clean state for symbols in non-trading states
// Prevents race conditions where orders/positions remain after state transitions
func (a *AdaptiveGridManager) cleanupWorker(ctx context.Context) {
	defer a.wg.Done()

	// Check every 10 seconds
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	a.logger.Info("Cleanup worker started",
		zap.Duration("check_interval", 10*time.Second))

	for {
		select {
		case <-ctx.Done():
			a.logger.Info("Cleanup worker stopping (context cancelled)")
			return
		case <-a.stopCh:
			a.logger.Info("Cleanup worker stopping (stop signal)")
			return
		case <-ticker.C:
			a.cleanupNonTradingSymbols(ctx)
		}
	}
}

// cleanupNonTradingSymbols cleans up orders/positions for symbols in non-trading states
func (a *AdaptiveGridManager) cleanupNonTradingSymbols(ctx context.Context) {
	a.mu.RLock()
	symbols := make([]string, 0, len(a.rangeDetectors))
	for symbol := range a.rangeDetectors {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range symbols {
		// Check state
		state := GridStateIdle
		if a.stateMachine != nil {
			state = a.stateMachine.GetState(symbol)
		}

		// Non-trading states: IDLE, EXIT_ALL, WAIT_NEW_RANGE
		// These states should NOT have any orders or positions
		shouldClean := false
		reason := ""

		switch state {
		case GridStateIdle:
			shouldClean = true
			reason = "IDLE state"
		case GridStateExitAll:
			shouldClean = true
			reason = "EXIT_ALL state"
		case GridStateWaitNewRange:
			// Don't cancel orders in WAIT_NEW_RANGE - this is a waiting state
			// Orders should be allowed to remain while waiting for range to establish
			shouldClean = false
			reason = "WAIT_NEW_RANGE state - keeping orders"
		}

		if !shouldClean {
			continue
		}

		// Check exchange for orders and positions
		if a.gridManager != nil {
			// Cancel all orders
			if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
				a.logger.Warn("Failed to cancel orders during cleanup",
					zap.String("symbol", symbol),
					zap.String("reason", reason),
					zap.Error(err))
			} else {
				a.logger.Info("Cancelled orders during cleanup",
					zap.String("symbol", symbol),
					zap.String("reason", reason))
			}
		}

		// Close all positions via WebSocket cache + futures client for execution
		// CRITICAL: Use positionProvider (WebSocket cache) instead of REST API for position data
		positions := make(map[string]client.Position)
		if a.positionProvider != nil {
			wsPositions, _ := a.positionProvider.GetCachedPositions(ctx)
			// Convert slice to map
			for _, p := range wsPositions {
				positions[p.Symbol] = p
			}
		} else {
			a.logger.Warn("Position provider unavailable during cleanup; skipping position read",
				zap.String("symbol", symbol),
				zap.String("reason", reason))
		}

		if pos, ok := positions[symbol]; ok && pos.PositionAmt != 0 {
			// Close position
			side := "SELL"
			if pos.PositionAmt < 0 {
				side = "BUY"
			}
			if a.futuresClient != nil {
				_, err := a.futuresClient.PlaceOrder(ctx, client.PlaceOrderRequest{
					Symbol:   symbol,
					Side:     side,
					Type:     "MARKET",
					Quantity: fmt.Sprintf("%f", math.Abs(pos.PositionAmt)),
				})
				if err != nil {
					a.logger.Warn("Failed to close position during cleanup",
						zap.String("symbol", symbol),
						zap.String("reason", reason),
						zap.Float64("position_amt", pos.PositionAmt),
						zap.Error(err))
				} else {
					a.logger.Info("Closed position during cleanup",
						zap.String("symbol", symbol),
						zap.String("reason", reason),
						zap.Float64("position_amt", pos.PositionAmt))
				}
			}
		}

		// CRITICAL: Transition from EXIT_ALL to WAIT_NEW_RANGE after cleanup
		// This fixes the stuck EXIT_ALL state issue
		if state == GridStateExitAll {
			a.markExitCompleted(symbol)
		}
	}
}

// realtimeExitMonitor monitors ADX/BB every tick for immediate exit signals
// This is a dedicated goroutine separate from WebSocket processing to ensure
// real-time exit detection with ~100ms tick monitoring
func (a *AdaptiveGridManager) realtimeExitMonitor(ctx context.Context) {
	defer a.wg.Done()

	// Check every 5s for real-time exit conditions (reduced from 100ms to avoid API spam)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	a.logger.Info("Real-time exit monitor started",
		zap.Duration("check_interval", 5*time.Second))

	for {
		select {
		case <-ctx.Done():
			a.logger.Info("Real-time exit monitor stopping (context cancelled)")
			return
		case <-a.stopCh:
			a.logger.Info("Real-time exit monitor stopping (stop signal)")
			return
		case <-ticker.C:
			a.checkRealtimeExitConditions(ctx)
		}
	}
}

// checkRealtimeExitConditions checks all symbols for immediate exit conditions
// Called by realtimeExitMonitor every tick (100ms)
func (a *AdaptiveGridManager) checkRealtimeExitConditions(ctx context.Context) {
	a.mu.RLock()
	symbols := make([]string, 0, len(a.rangeDetectors))
	for symbol := range a.rangeDetectors {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range symbols {
		// Skip if trading not active
		if a.IsTradingPaused(symbol) {
			continue
		}

		// Get range detector
		a.mu.RLock()
		detector, exists := a.rangeDetectors[symbol]
		a.mu.RUnlock()

		if !exists {
			continue
		}

		// Condition 1: ADX > 25 indicates trending market - exit all
		avgADX := detector.averageADXLocked()
		if avgADX > 25.0 {
			a.logger.Error("REALTIME EXIT: ADX spike detected - closing all",
				zap.String("symbol", symbol),
				zap.Float64("adx", avgADX))
			a.handleTrendExit(ctx, symbol, "adx_spike")
			continue
		}

		// Condition 2: BB expansion > 1.5x average - exit all
		currentRange := detector.GetCurrentRange()
		if currentRange != nil && currentRange.WidthPct > 0 {
			// Check if width is expanding significantly (would need historical avg)
			// For now, check if width exceeds 1.5% which is considered wide
			if currentRange.WidthPct > 0.015 { // 1.5% width threshold
				a.logger.Error("REALTIME EXIT: BB expansion detected - closing all",
					zap.String("symbol", symbol),
					zap.Float64("bb_width_pct", currentRange.WidthPct*100))
				a.handleTrendExit(ctx, symbol, "bb_expansion")
				continue
			}
		}

		// Condition 3: Price outside BB for 2+ consecutive checks (simplified)
		// The range detector's handleBreakout is called via UpdatePriceForRange
		// This realtime monitor provides additional safety layer
	}
}

// handleTrendExit handles immediate exit on trend detection
func (a *AdaptiveGridManager) handleTrendExit(ctx context.Context, symbol string, reason string) {
	// Idempotent check - prevent duplicate exits
	if a.IsTradingPaused(symbol) {
		a.logger.Debug("Trend exit already handled, skipping duplicate",
			zap.String("symbol", symbol),
			zap.String("reason", reason))
		return
	}

	// Perform immediate exit actions
	a.logger.Error("Executing trend exit",
		zap.String("symbol", symbol),
		zap.String("reason", reason))

	// Cancel all orders
	if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
		a.logger.Error("Failed to cancel orders on trend exit",
			zap.String("symbol", symbol),
			zap.Error(err))
	}

	// Get position and close if exists
	a.mu.RLock()
	position, hasPosition := a.positions[symbol]
	a.mu.RUnlock()

	if hasPosition && position != nil && position.PositionAmt != 0 {
		a.emergencyClosePosition(ctx, symbol, position.PositionAmt)
	}

	// Clear grid
	if err := a.gridManager.ClearGrid(ctx, symbol); err != nil {
		a.logger.Error("Failed to clear grid on trend exit",
			zap.String("symbol", symbol),
			zap.Error(err))
	}

	// Pause trading and force recalculation
	a.pauseTrading(symbol)

	// Force range recalculation
	a.mu.RLock()
	detector, exists := a.rangeDetectors[symbol]
	a.mu.RUnlock()

	if exists {
		detector.ForceRecalculate()
	}
}

// handleSlotTransitionForAll handles time slot transition for all active symbols
func (a *AdaptiveGridManager) handleSlotTransitionForAll(ctx context.Context) {
	a.mu.RLock()
	symbols := make([]string, 0, len(a.positions))
	for symbol := range a.positions {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	// Get current slot info
	currentSlot := a.GetCurrentSlot()
	if currentSlot == nil {
		a.logger.Info("No active time slot - skipping transition for all symbols")
		return
	}

	for _, symbol := range symbols {
		if err := a.handleSlotTransition(ctx, symbol); err != nil {
			a.logger.Error("Failed to handle slot transition",
				zap.String("symbol", symbol),
				zap.Error(err))
		}
	}
}

// handleSlotTransition handles time slot change for a single symbol
func (a *AdaptiveGridManager) handleSlotTransition(ctx context.Context, symbol string) error {
	// Check cooldown (2 minutes between transitions)
	if a.IsInSlotTransitionCooldown(symbol) {
		a.logger.Debug("Slot transition cooldown active, skipping",
			zap.String("symbol", symbol))
		return nil
	}

	currentSlot := a.GetCurrentSlot()

	if currentSlot == nil {
		a.logger.Info("Exited all time slots - cancelling orders",
			zap.String("symbol", symbol))

		// Cancel all orders and pause trading
		if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
			a.logger.Error("Failed to cancel orders on slot exit",
				zap.String("symbol", symbol),
				zap.Error(err))
		}

		a.pauseTrading(symbol)
		return nil
	}

	if !currentSlot.Enabled {
		a.logger.Info("Entered disabled time slot - cancelling orders",
			zap.String("symbol", symbol),
			zap.String("slot", currentSlot.Description))

		// Cancel all orders and pause trading
		if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
			a.logger.Error("Failed to cancel orders on disabled slot",
				zap.String("symbol", symbol),
				zap.Error(err))
		}

		a.pauseTrading(symbol)
		return nil
	}

	// Enabled slot - rebuild grid with new parameters
	a.logger.Info("Entered enabled time slot - rebuilding grid",
		zap.String("symbol", symbol),
		zap.String("slot", currentSlot.Description),
		zap.Float64("size_multiplier", currentSlot.SizeMultiplier),
		zap.Float64("spread_multiplier", currentSlot.SpreadMultiplier))

	// Resume trading if it was paused
	a.resumeTrading(symbol)

	// Cancel existing orders
	if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
		a.logger.Error("Failed to cancel orders during slot transition",
			zap.String("symbol", symbol),
			zap.Error(err))
		// Continue anyway
	}

	// Clear grid
	if err := a.gridManager.ClearGrid(ctx, symbol); err != nil {
		a.logger.Error("Failed to clear grid during slot transition",
			zap.String("symbol", symbol),
			zap.Error(err))
		// Continue anyway
	}

	// Rebuild grid with new slot parameters
	if err := a.gridManager.RebuildGrid(ctx, symbol); err != nil {
		return fmt.Errorf("failed to rebuild grid for slot transition: %w", err)
	}

	a.logger.Info("Slot transition completed successfully",
		zap.String("symbol", symbol),
		zap.String("slot", currentSlot.Description))

	return nil
}

// positionMonitor monitors positions and triggers risk controls
func (a *AdaptiveGridManager) positionMonitor(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.riskConfig.PositionCheckInterval)
	defer ticker.Stop()

	// Additional ticker for checking resume conditions (every 5 seconds)
	resumeCheckTicker := time.NewTicker(5 * time.Second)
	defer resumeCheckTicker.Stop()

	// NEW: Auto-recovery ticker (every 30 seconds)
	autoRecoveryTicker := time.NewTicker(30 * time.Second)
	defer autoRecoveryTicker.Stop()

	// IMMEDIATE: Run initial blocking checks status on startup
	a.logBlockingChecksStatus()

	// NEW: Funding rate check ticker (every 5 minutes)
	fundingCheckTicker := time.NewTicker(5 * time.Minute)
	defer fundingCheckTicker.Stop()

	// NEW: Partial close check ticker (every 2 seconds)
	partialCloseTicker := time.NewTicker(2 * time.Second)
	defer partialCloseTicker.Stop()

	// NEW: Cluster stop loss check ticker (every 30 seconds)
	// This handles both cluster stop loss and time-based stop loss internally
	clusterStopLossTicker := time.NewTicker(30 * time.Second)
	defer clusterStopLossTicker.Stop()

	// DEBUG: Blocking checks status ticker (every 30 seconds)
	debugCheckTicker := time.NewTicker(30 * time.Second)
	defer debugCheckTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.checkAndManageRisk(ctx)
		case <-resumeCheckTicker.C:
			// Check if any paused symbols can resume trading
			a.CheckAndResumeAll()
		case <-autoRecoveryTicker.C:
			// Run auto-recovery to unblock stuck symbols
			a.AutoRecovery()
		case <-debugCheckTicker.C:
			// DEBUG: Log all blocking checks status
			a.logBlockingChecksStatus()
		case <-fundingCheckTicker.C:
			// Run periodic checks including auto-recovery, funding bias, and blocking status
			a.RunPeriodicChecks()
		case <-partialCloseTicker.C:
			// NEW: Check partial take-profit levels for all symbols
			a.checkPartialTakeProfits(ctx)
		case <-clusterStopLossTicker.C:
			// NEW: Check cluster stop loss for all symbols (includes time-based stop loss internally)
			a.checkClusterStopLoss(ctx)
		}
	}
}

// checkAndManageRisk checks positions and applies risk controls
func (a *AdaptiveGridManager) checkAndManageRisk(ctx context.Context) {
	if a.futuresClient == nil {
		a.logger.Error("RISK CHECK: futuresClient is nil - cannot check positions")
		return
	}

	// Fetch positions from WebSocket cache only in steady-state
	var positions []client.Position
	var err error
	if a.positionProvider != nil {
		positions, err = a.positionProvider.GetCachedPositions(ctx)
	} else {
		a.logger.Warn("RISK CHECK: positionProvider is nil - skipping steady-state risk pass")
		return
	}
	if err != nil {
		a.logger.Warn("RISK CHECK: Failed to fetch positions", zap.Error(err))
		return
	}

	a.logger.Debug("RISK CHECK: Fetched positions",
		zap.Int("count", len(positions)),
		zap.Float64("per_position_limit", a.riskConfig.PerPositionLossLimitUSDT),
		zap.Float64("max_unrealized_loss", a.riskConfig.MaxUnrealizedLossUSDT),
		zap.Float64("total_net_loss_limit", a.riskConfig.TotalNetLossLimitUSDT))

	// Calculate total net unrealized PnL across all positions
	totalNetUnrealizedPnL := 0.0
	activePositions := make([]client.Position, 0)

	for _, pos := range positions {
		if pos.PositionAmt == 0 {
			a.logger.Debug("RISK CHECK: Skipping empty position", zap.String("symbol", pos.Symbol))
			continue // Skip empty positions
		}
		activePositions = append(activePositions, pos)
		totalNetUnrealizedPnL += pos.UnrealizedProfit
		a.logger.Debug("RISK CHECK: Active position",
			zap.String("symbol", pos.Symbol),
			zap.Float64("amt", pos.PositionAmt),
			zap.Float64("unrealized_pnl", pos.UnrealizedProfit))
	}

	a.logger.Info("RISK CHECK: Summary",
		zap.Int("active_positions", len(activePositions)),
		zap.Float64("total_net_unrealized_pnl", totalNetUnrealizedPnL))

	// CRITICAL: Check total net unrealized loss first
	// If total net loss > 5 USDT, close ALL positions and ALL orders immediately
	if totalNetUnrealizedPnL < -a.riskConfig.TotalNetLossLimitUSDT {
		a.logger.Error("CRITICAL: TOTAL NET UNREALIZED LOSS EXCEEDED - EMERGENCY CLOSE ALL",
			zap.Float64("total_net_unrealized_pnl", totalNetUnrealizedPnL),
			zap.Float64("limit", -a.riskConfig.TotalNetLossLimitUSDT),
			zap.Int("active_positions", len(activePositions)))

		// Close all positions and orders for all symbols
		a.emergencyCloseAll(ctx, activePositions)
		return
	}

	// Build map of active symbols
	activeSymbols := make(map[string]bool)
	for _, pos := range activePositions {
		activeSymbols[pos.Symbol] = true
	}

	// CRITICAL: Check for liquidated positions (tracked but no longer active)
	a.mu.RLock()
	trackedSymbols := make([]string, 0, len(a.positions))
	for symbol := range a.positions {
		trackedSymbols = append(trackedSymbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range trackedSymbols {
		if !activeSymbols[symbol] {
			// Position was liquidated or force-closed by exchange
			a.logger.Error("POSITION LIQUIDATED OR FORCE-CLOSED BY EXCHANGE - Rebuilding grid",
				zap.String("symbol", symbol))

			// Clear tracking
			a.mu.Lock()
			delete(a.positions, symbol)
			delete(a.positionStopLoss, symbol)
			delete(a.trailingStopPrice, symbol)
			delete(a.positionTakeProfit, symbol)
			a.mu.Unlock()

			// Clear and rebuild grid to start fresh
			if a.gridManager != nil {
				if err := a.gridManager.ClearGrid(ctx, symbol); err != nil {
					a.logger.Error("Failed to clear grid after liquidation", zap.String("symbol", symbol), zap.Error(err))
				}
				time.Sleep(500 * time.Millisecond)
				if err := a.gridManager.RebuildGrid(ctx, symbol); err != nil {
					a.logger.Error("Failed to rebuild grid after liquidation", zap.String("symbol", symbol), zap.Error(err))
				}
			}

			// Resume trading
			a.resumeTrading(symbol)
			a.RecordTradeResult(symbol, false) // Count as loss
		}
	}

	// Process each position individually
	for _, pos := range activePositions {
		symbol := pos.Symbol

		// Update position tracking
		a.UpdatePositionTracking(symbol, &pos)

		// Check risk limits for this position
		a.evaluateRiskAndAct(ctx, symbol, &pos)
	}
}

// checkPartialTakeProfits checks partial take-profit levels for all active symbols
func (a *AdaptiveGridManager) checkPartialTakeProfits(ctx context.Context) {
	a.mu.RLock()
	symbols := make([]string, 0, len(a.positions))
	for symbol := range a.positions {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range symbols {
		a.mu.RLock()
		markPrice := a.positions[symbol].MarkPrice
		a.mu.RUnlock()

		if markPrice > 0 {
			closed, err := a.CheckPartialTakeProfits(ctx, symbol, markPrice)
			if err != nil {
				a.logger.Error("Failed to check partial take profits",
					zap.String("symbol", symbol),
					zap.Error(err))
			}
			if closed {
				a.logger.Info("Partial take profit executed",
					zap.String("symbol", symbol),
					zap.Float64("price", markPrice))
			}
		}
	}
}

// checkClusterStopLoss checks cluster stop loss for all active symbols
func (a *AdaptiveGridManager) checkClusterStopLoss(ctx context.Context) {
	a.mu.RLock()
	symbols := make([]string, 0, len(a.positions))
	for symbol := range a.positions {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range symbols {
		a.mu.RLock()
		markPrice := a.positions[symbol].MarkPrice
		a.mu.RUnlock()

		if markPrice > 0 {
			clusters, err := a.CheckClusterStopLoss(symbol, markPrice)
			if err != nil {
				a.logger.Error("Failed to check cluster stop loss",
					zap.String("symbol", symbol),
					zap.Error(err))
			}
			if len(clusters) > 0 {
				a.logger.Info("Cluster stop loss triggered",
					zap.String("symbol", symbol),
					zap.Int("clusters", len(clusters)))
				// Trigger exit for symbol
				a.ExitAll(ctx, symbol, EventEmergencyExit, "Cluster stop loss triggered")
			}
		}
	}
}

// UpdatePositionTracking updates internal position state (exported for cross-package calls)
func (a *AdaptiveGridManager) UpdatePositionTracking(symbol string, pos *client.Position) {
	a.mu.Lock()
	defer a.mu.Unlock()

	notional := math.Abs(pos.PositionAmt) * pos.MarkPrice

	a.positions[symbol] = &SymbolPosition{
		PositionAmt:      pos.PositionAmt,
		EntryPrice:       pos.EntryPrice,
		MarkPrice:        pos.MarkPrice,
		UnrealizedPnL:    pos.UnrealizedProfit,
		LiquidationPrice: pos.Liquidation,
		Leverage:         pos.Leverage,
		NotionalValue:    notional,
		LastUpdated:      time.Now(),
	}

	// Initialize stop loss if not set
	if _, exists := a.positionStopLoss[symbol]; !exists {
		a.setInitialStopLoss(symbol, pos.EntryPrice, pos.PositionAmt)
	}

	// Initialize take profit if not set
	if _, exists := a.positionTakeProfit[symbol]; !exists {
		slPrice := a.positionStopLoss[symbol]
		a.setInitialTakeProfit(symbol, pos.EntryPrice, slPrice, pos.PositionAmt)
	}

	// Update trailing stop if in profit
	a.updateTrailingStop(symbol, pos.MarkPrice, pos.PositionAmt)
}

// setInitialStopLoss sets the initial stop loss price
func (a *AdaptiveGridManager) setInitialStopLoss(symbol string, entryPrice, positionAmt float64) {
	stopLossPct := a.riskConfig.StopLossPct

	if positionAmt > 0 { // Long position
		a.positionStopLoss[symbol] = entryPrice * (1 - stopLossPct)
	} else { // Short position
		a.positionStopLoss[symbol] = entryPrice * (1 + stopLossPct)
	}

	a.logger.Info("Stop loss set",
		zap.String("symbol", symbol),
		zap.Float64("entry", entryPrice),
		zap.Float64("stop_loss", a.positionStopLoss[symbol]),
		zap.Float64("pct", stopLossPct*100))
}

// updateTrailingStop updates trailing stop if in profit
func (a *AdaptiveGridManager) updateTrailingStop(symbol string, markPrice, positionAmt float64) {
	entryPrice := a.positions[symbol].EntryPrice
	trailingActivation := a.riskConfig.TrailingStopPct
	trailingDistance := a.riskConfig.TrailingStopDistancePct

	var profitPct float64
	var newStopPrice float64

	if positionAmt > 0 { // Long
		profitPct = (markPrice - entryPrice) / entryPrice
		if profitPct > trailingActivation {
			newStopPrice = markPrice * (1 - trailingDistance)
			if currentStop, exists := a.trailingStopPrice[symbol]; !exists || newStopPrice > currentStop {
				a.trailingStopPrice[symbol] = newStopPrice
				a.logger.Info("Trailing stop updated (Long)",
					zap.String("symbol", symbol),
					zap.Float64("mark_price", markPrice),
					zap.Float64("new_stop", newStopPrice),
					zap.Float64("profit_pct", profitPct*100))
			}
		}
	} else { // Short
		profitPct = (entryPrice - markPrice) / entryPrice
		if profitPct > trailingActivation {
			newStopPrice = markPrice * (1 + trailingDistance)
			if currentStop, exists := a.trailingStopPrice[symbol]; !exists || newStopPrice < currentStop {
				a.trailingStopPrice[symbol] = newStopPrice
				a.logger.Info("Trailing stop updated (Short)",
					zap.String("symbol", symbol),
					zap.Float64("mark_price", markPrice),
					zap.Float64("new_stop", newStopPrice),
					zap.Float64("profit_pct", profitPct*100))
			}
		}
	}
}

// evaluateRiskAndAct evaluates risk and takes action if needed
func (a *AdaptiveGridManager) evaluateRiskAndAct(ctx context.Context, symbol string, pos *client.Position) {
	a.mu.RLock()
	position := a.positions[symbol]
	stopLoss := a.positionStopLoss[symbol]
	trailingStop := a.trailingStopPrice[symbol]
	takeProfit := a.positionTakeProfit[symbol]
	a.mu.RUnlock()

	if position == nil {
		a.logger.Warn("RISK CHECK: Position tracking nil", zap.String("symbol", symbol))
		return
	}

	markPrice := position.MarkPrice
	notional := position.NotionalValue
	unrealizedPnL := position.UnrealizedPnL
	liquidationPrice := position.LiquidationPrice
	entryPrice := position.EntryPrice

	a.logger.Debug("RISK CHECK: Evaluating position",
		zap.String("symbol", symbol),
		zap.Float64("unrealized_pnl", unrealizedPnL),
		zap.Float64("per_position_limit", -a.riskConfig.PerPositionLossLimitUSDT),
		zap.Float64("max_unrealized_loss", -a.riskConfig.MaxUnrealizedLossUSDT))

	// 1. Check stop loss
	if a.isStopLossHit(symbol, markPrice, pos.PositionAmt, stopLoss, trailingStop) {
		a.logger.Warn("STOP LOSS TRIGGERED - Closing position",
			zap.String("symbol", symbol),
			zap.Float64("mark_price", markPrice),
			zap.Float64("unrealized_pnl", unrealizedPnL))
		a.emergencyClosePosition(ctx, symbol, pos.PositionAmt)
		return
	}

	// 1.5. Check take profit
	if a.isTakeProfitHit(symbol, markPrice, pos.PositionAmt, takeProfit) {
		a.logger.Info("TAKE PROFIT HIT - Closing position with profit",
			zap.String("symbol", symbol),
			zap.Float64("mark_price", markPrice),
			zap.Float64("entry_price", entryPrice),
			zap.Float64("take_profit", takeProfit),
			zap.Float64("unrealized_pnl", unrealizedPnL),
			zap.Float64("profit_pct", (markPrice-entryPrice)/entryPrice*100))
		a.closePositionWithProfit(ctx, symbol, pos.PositionAmt)
		return
	}

	// 2. Check liquidation proximity
	if a.isNearLiquidation(symbol, markPrice, liquidationPrice, pos.PositionAmt) {
		a.logger.Warn("NEAR LIQUIDATION - Emergency closing",
			zap.String("symbol", symbol),
			zap.Float64("mark_price", markPrice),
			zap.Float64("liquidation_price", liquidationPrice),
			zap.Float64("distance_pct", math.Abs(markPrice-liquidationPrice)/markPrice*100))
		a.emergencyClosePosition(ctx, symbol, pos.PositionAmt)
		return
	}

	// 3. Check max unrealized loss per position (3 USDT default)
	if unrealizedPnL < -a.riskConfig.MaxUnrealizedLossUSDT {
		a.logger.Warn("MAX UNREALIZED LOSS EXCEEDED - Closing position",
			zap.String("symbol", symbol),
			zap.Float64("unrealized_pnl", unrealizedPnL),
			zap.Float64("max_loss", a.riskConfig.MaxUnrealizedLossUSDT))
		a.emergencyClosePosition(ctx, symbol, pos.PositionAmt)
		return
	}

	// NEW: 3.5. Check per-position loss limit (1 USDT)
	// Close position immediately if unrealized loss > 1 USDT
	if unrealizedPnL < -a.riskConfig.PerPositionLossLimitUSDT {
		a.logger.Error("PER-POSITION LOSS LIMIT EXCEEDED - Closing position immediately",
			zap.String("symbol", symbol),
			zap.Float64("unrealized_pnl", unrealizedPnL),
			zap.Float64("limit", a.riskConfig.PerPositionLossLimitUSDT))
		a.emergencyClosePosition(ctx, symbol, pos.PositionAmt)
		return
	}

	// 4. Check max position size - Use gradual reduction instead of pause
	hardCap := a.riskConfig.MaxPositionUSDT * 1.2
	if notional > a.riskConfig.MaxPositionUSDT {
		if notional >= hardCap {
			// CRITICAL: Hard cap exceeded - emergency close
			a.logger.Error("HARD POSITION CAP EXCEEDED - Emergency closing position",
				zap.String("symbol", symbol),
				zap.Float64("notional", notional),
				zap.Float64("max_allowed", a.riskConfig.MaxPositionUSDT),
				zap.Float64("hard_cap", hardCap))
			a.emergencyClosePosition(ctx, symbol, pos.PositionAmt)
			return
		}

		// Position exceeded max but below hard cap - reduce gradually
		// Check if we recently reduced position (avoid repeated reductions)
		lastReductionTime, exists := a.positionReductionTime[symbol]
		if !exists || time.Since(lastReductionTime) > 30*time.Second {
			a.logger.Warn("MAX POSITION SIZE EXCEEDED - Reducing position by 50% gradually",
				zap.String("symbol", symbol),
				zap.Float64("notional", notional),
				zap.Float64("max_allowed", a.riskConfig.MaxPositionUSDT),
				zap.Float64("hard_cap", hardCap))

			// Reduce position by 50%
			a.reducePositionByPct(ctx, symbol, pos.PositionAmt, 0.5)

			// Track last reduction time
			a.mu.Lock()
			a.positionReductionTime[symbol] = time.Now()
			a.mu.Unlock()
		} else {
			a.logger.Debug("Position reduction recently executed, waiting for cooldown",
				zap.String("symbol", symbol),
				zap.Duration("time_since_last_reduction", time.Since(lastReductionTime)))
		}
		return
	}

	// 4.1. Clear reduction time when position size decreased below max
	if notional <= a.riskConfig.MaxPositionUSDT {
		a.mu.Lock()
		delete(a.positionReductionTime, symbol)
		a.mu.Unlock()
	}

	// 5. Check multi-layer liquidation protection (4-tier system)
	// This provides graduated responses as position approaches liquidation
	if liquidationPrice > 0 {
		a.checkMultiLayerLiquidation(ctx, symbol, markPrice, liquidationPrice, pos.PositionAmt)
	}

	a.logger.Debug("RISK CHECK: Position passed all risk checks",
		zap.String("symbol", symbol),
		zap.Float64("unrealized_pnl", unrealizedPnL))

	// Log position summary periodically for dashboard visibility
	if notional > 0 {
		a.logger.Info("Position Summary",
			zap.String("symbol", symbol),
			zap.Float64("notional", notional),
			zap.Float64("max_allowed", a.riskConfig.MaxPositionUSDT),
			zap.Float64("utilization_pct", (notional/a.riskConfig.MaxPositionUSDT)*100),
			zap.Float64("unrealized_pnl", unrealizedPnL),
			zap.Float64("entry_price", entryPrice),
			zap.Float64("mark_price", markPrice))
	}
}

// isStopLossHit checks if stop loss is hit
func (a *AdaptiveGridManager) isStopLossHit(symbol string, markPrice, positionAmt, stopLoss, trailingStop float64) bool {
	if positionAmt == 0 {
		return false
	}

	// Check fixed stop loss
	if positionAmt > 0 && markPrice <= stopLoss { // Long
		return true
	}
	if positionAmt < 0 && markPrice >= stopLoss { // Short
		return true
	}

	// Check trailing stop
	if trailingStop > 0 {
		if positionAmt > 0 && markPrice <= trailingStop { // Long trailing
			return true
		}
		if positionAmt < 0 && markPrice >= trailingStop { // Short trailing
			return true
		}
	}

	return false
}

// AutoRecovery attempts to unblock symbols that are stuck in various states
func (a *AdaptiveGridManager) AutoRecovery() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.logger.Info("=== AUTO-RECOVERY START ===")

	for symbol := range a.rangeDetectors {
		a.logger.Info("Checking symbol for auto-recovery", zap.String("symbol", symbol))

		// 1. Check RangeDetector state
		if detector, exists := a.rangeDetectors[symbol]; exists {
			rangeInfo := detector.GetRangeInfo()
			state := rangeInfo["state"]
			if state == "Unknown" || state == "Initializing" {
				a.logger.Warn("Auto-recovery: Force initializing RangeDetector stuck in Unknown/Initializing",
					zap.String("symbol", symbol),
					zap.Any("state", state))
				config := DefaultRangeConfig()
				config.StabilizationPeriod = 30 * time.Second // Shorter for auto-recovery
				a.InitializeRangeDetector(symbol, config)
			}
		}

		// 2. Check State Machine
		if a.stateMachine != nil {
			state := a.stateMachine.GetState(symbol)
			stateTime := a.stateMachine.GetStateTime(symbol)
			timeInState := time.Since(stateTime)

			switch state {
			case GridStateExitAll:
				// Check if position is still open from cache (fast check without API call)
				position, hasPosition := a.positions[symbol]
				positionZero := !hasPosition || position == nil || position.PositionAmt == 0

				if !positionZero {
					a.logger.Warn("Auto-recovery: EXIT_ALL with open position detected, closing position",
						zap.String("symbol", symbol),
						zap.Float64("position_amt", position.PositionAmt),
						zap.Duration("time_in_state", timeInState))
					// Unlock to allow emergencyClosePosition to acquire lock if needed
					a.mu.Unlock()
					a.emergencyClosePosition(context.Background(), symbol, position.PositionAmt)
					a.mu.Lock()
				} else {
					a.logger.Info("Auto-recovery: EXIT_ALL position already closed, transitioning",
						zap.String("symbol", symbol),
						zap.Duration("time_in_state", timeInState))
				}

				// Transition to WAIT_NEW_RANGE (idempotent if already called)
				if timeInState > 30*time.Second { // Give 30s for cleanup to complete
					a.markExitCompleted(symbol)
				}

			case GridStateWaitNewRange:
				// Fluid like water: Detect market condition and adapt strategy
				// Trending market -> TRENDING state (breakout trading)
				// Sideways market -> ENTER_GRID state (grid trading)
				if detector, exists := a.rangeDetectors[symbol]; exists {
					rangeInfo := detector.GetRangeInfo()
					adx, _ := rangeInfo["adx"].(float64)
					sidewaysADXMax, _ := rangeInfo["sideways_adx_max"].(float64)

					// Check if we have enough price data
					dataPoints, _ := rangeInfo["data_points"].(int)
					if dataPoints >= 20 {
						// Detect market condition
						if adx > sidewaysADXMax {
							// Trending market - switch to TRENDING strategy
							a.logger.Info("Auto-recovery: Trending market detected, transitioning to TRENDING",
								zap.String("symbol", symbol),
								zap.Duration("time_in_state", timeInState),
								zap.Float64("adx", adx),
								zap.Float64("sideways_threshold", sidewaysADXMax),
								zap.String("philosophy", "soft like water - adapt to trend"))
							if a.stateMachine.CanTransition(symbol, EventTrendDetected) {
								a.stateMachine.Transition(symbol, EventTrendDetected)
							}
						} else {
							// Sideways market - switch to GRID strategy
							a.logger.Info("Auto-recovery: Sideways market detected, transitioning to ENTER_GRID",
								zap.String("symbol", symbol),
								zap.Duration("time_in_state", timeInState),
								zap.Float64("adx", adx),
								zap.Float64("sideways_threshold", sidewaysADXMax),
								zap.String("philosophy", "soft like water - adapt to range"))
							if a.stateMachine.CanTransition(symbol, EventRangeConfirmed) {
								a.stateMachine.Transition(symbol, EventRangeConfirmed)
							}
						}
					} else {
						a.logger.Info("Auto-recovery: WAIT_NEW_RANGE waiting for more data points",
							zap.String("symbol", symbol),
							zap.Duration("time_in_state", timeInState),
							zap.Int("data_points", dataPoints))
					}
				} else if timeInState > 2*time.Minute {
					// Stuck too long without detector, force to IDLE
					a.logger.Warn("Auto-recovery: Force transition from WAIT_NEW_RANGE to IDLE (no detector)",
						zap.String("symbol", symbol),
						zap.Duration("time_in_state", timeInState))
					a.stateMachine.ForceState(symbol, GridStateIdle)
				}

			case GridStateIdle:
				// Fluid like water: Detect market condition and adapt strategy
				if detector, exists := a.rangeDetectors[symbol]; exists {
					rangeInfo := detector.GetRangeInfo()
					dataPoints, _ := rangeInfo["data_points"].(int)

					if dataPoints >= 20 && timeInState > 30*time.Second {
						adx, _ := rangeInfo["adx"].(float64)
						sidewaysADXMax, _ := rangeInfo["sideways_adx_max"].(float64)

						if adx > sidewaysADXMax {
							// Trending market - switch to TRENDING strategy
							a.logger.Info("Auto-recovery: Trending market detected in IDLE, transitioning to TRENDING",
								zap.String("symbol", symbol),
								zap.Duration("time_in_state", timeInState),
								zap.Float64("adx", adx),
								zap.Float64("sideways_threshold", sidewaysADXMax),
								zap.String("philosophy", "soft like water - adapt to trend"))
							if a.stateMachine.CanTransition(symbol, EventTrendDetected) {
								a.stateMachine.Transition(symbol, EventTrendDetected)
							}
						} else {
							// Sideways market - switch to GRID strategy
							a.logger.Info("Auto-recovery: Sideways market detected in IDLE, transitioning to ENTER_GRID",
								zap.String("symbol", symbol),
								zap.Duration("time_in_state", timeInState),
								zap.Float64("adx", adx),
								zap.Float64("sideways_threshold", sidewaysADXMax),
								zap.String("philosophy", "soft like water - adapt to range"))
							if a.stateMachine.CanTransition(symbol, EventRangeConfirmed) {
								a.stateMachine.Transition(symbol, EventRangeConfirmed)
							}
						}
					} else {
						a.logger.Info("Auto-recovery: IDLE waiting for more data points",
							zap.String("symbol", symbol),
							zap.Duration("time_in_state", timeInState),
							zap.Int("data_points", dataPoints))
					}
				}

			case GridStateEnterGrid:
				// NEW: Force transition to TRADING if stuck > 2 minutes
				// This provides additional safety net for stuck ENTER_GRID state
				if timeInState > 2*time.Minute {
					a.logger.Warn("Auto-recovery: Force ENTER_GRID -> TRADING (stuck too long)",
						zap.String("symbol", symbol),
						zap.Duration("time_in_state", timeInState))
					if a.stateMachine.CanTransition(symbol, EventEntryPlaced) {
						a.stateMachine.Transition(symbol, EventEntryPlaced)
					}
				}
			}
		}

		// 3. Check tradingPaused
		if a.tradingPaused[symbol] {
			state := a.stateMachine.GetState(symbol)
			if state == GridStateIdle || state == GridStateWaitNewRange {
				a.logger.Warn("Auto-recovery: Auto-resuming paused symbol in IDLE/WAIT_NEW_RANGE",
					zap.String("symbol", symbol),
					zap.String("state", state.String()))
				a.resumeTrading(symbol)
			}
		}
	}

	a.logger.Info("=== AUTO-RECOVERY END ===")
}

// logBlockingChecksStatus logs the status of all blocking checks for debugging
func (a *AdaptiveGridManager) logBlockingChecksStatus() {
	a.mu.RLock()
	defer a.mu.RUnlock()

	a.logger.Info("=== BLOCKING CHECKS STATUS ===")

	// 0. RangeDetector status (BEFORE other checks)
	a.logger.Info("RangeDetector Status")
	for symbol, detector := range a.rangeDetectors {
		rangeInfo := detector.GetRangeInfo()
		state := rangeInfo["state"]
		a.logger.Info("RangeDetector Details",
			zap.String("symbol", symbol),
			zap.Any("state", state),
			zap.Bool("should_trade", detector.ShouldTrade()),
			zap.Any("has_range", rangeInfo["has_range"]),
			zap.Any("range_lower", rangeInfo["range_lower"]),
			zap.Any("range_upper", rangeInfo["range_upper"]),
			zap.Any("adx_filter_enabled", rangeInfo["adx_filter_enabled"]),
			zap.Any("is_sideways", rangeInfo["is_sideways"]),
			zap.Any("data_points", rangeInfo["data_points"]))

		// Force initialize if stuck in Unknown state for too long
		if state == "Unknown" || state == "Initializing" {
			a.logger.Warn("RangeDetector stuck in Unknown/Initializing state, forcing initialization IMMEDIATELY",
				zap.String("symbol", symbol),
				zap.Any("current_state", state))
			// Force re-initialization with default config
			config := DefaultRangeConfig()
			config.StabilizationPeriod = 10 * time.Second // Ultra-short for immediate recovery
			a.InitializeRangeDetector(symbol, config)
		}
	}

	// 1. CircuitBreaker status
	if a.circuitBreaker != nil {
		if cb, ok := a.circuitBreaker.(interface {
			GetTrippedSymbols() []string
		}); ok {
			trippedSymbols := cb.GetTrippedSymbols()
			a.logger.Info("CircuitBreaker Status",
				zap.Strings("tripped_symbols", trippedSymbols),
				zap.Int("count", len(trippedSymbols)))
		}
	}

	// 2. RiskMonitor exposure
	if a.riskMonitor != nil {
		totalExposure, maxExposure, utilization := a.riskMonitor.GetExposureStats()
		a.logger.Info("RiskMonitor Exposure",
			zap.Float64("total_exposure", totalExposure),
			zap.Float64("max_exposure", maxExposure),
			zap.Float64("utilization_pct", utilization*100))
	}

	// 3. Position limits
	for symbol, position := range a.positions {
		hardCap := a.riskConfig.MaxPositionUSDT * 1.2
		a.logger.Info("Position Status",
			zap.String("symbol", symbol),
			zap.Float64("notional", position.NotionalValue),
			zap.Float64("max_allowed", a.riskConfig.MaxPositionUSDT),
			zap.Float64("hard_cap", hardCap),
			zap.Bool("exceeded_hard_cap", position.NotionalValue >= hardCap))
	}

	// 4. State machine status
	if a.stateMachine != nil {
		for symbol := range a.rangeDetectors {
			state := a.stateMachine.GetState(symbol)
			canPlace := a.stateMachine.CanPlaceOrders(symbol)
			stateTime := a.stateMachine.GetStateTime(symbol)
			timeInState := time.Since(stateTime)

			a.logger.Info("State Machine Status",
				zap.String("symbol", symbol),
				zap.String("state", state.String()),
				zap.Bool("can_place_orders", canPlace),
				zap.Duration("time_in_state", timeInState))

			if !canPlace {
				a.logger.Warn("State Machine Blocking Orders for symbol", zap.String("symbol", symbol), zap.String("reason", state.String()))

				// Force transition if stuck in EXIT_ALL (immediate - no wait)
				if state == GridStateExitAll {
					a.logger.Warn("Symbol in EXIT_ALL state, forcing transition to WAIT_NEW_RANGE IMMEDIATELY",
						zap.String("symbol", symbol),
						zap.Duration("time_in_state", timeInState))
					a.markExitCompleted(symbol)
				}

				// Force transition from IDLE if range is ready (immediate - no wait)
				if state == GridStateIdle {
					if detector, exists := a.rangeDetectors[symbol]; exists && detector.ShouldTrade() {
						a.logger.Warn("Symbol in IDLE state with ready range, forcing transition to ENTER_GRID IMMEDIATELY",
							zap.String("symbol", symbol),
							zap.Duration("time_in_state", timeInState))
						a.stateMachine.Transition(symbol, EventRangeConfirmed)
					}
				}

				// Force transition if stuck in WAIT_NEW_RANGE for more than 2 minutes (reduced from 5min)
				if state == GridStateWaitNewRange && timeInState > 2*time.Minute {
					a.logger.Warn("Symbol stuck in WAIT_NEW_RANGE for too long, forcing transition to IDLE",
						zap.String("symbol", symbol),
						zap.Duration("time_in_state", timeInState))
					a.stateMachine.ForceState(symbol, GridStateIdle)
				}
			}
		}
	}

	// 5. Trading paused status
	pausedSymbols := make([]string, 0)
	for symbol, paused := range a.tradingPaused {
		if paused {
			pausedSymbols = append(pausedSymbols, symbol)
			// Check if symbol is stuck in paused state
			state := a.stateMachine.GetState(symbol)
			if state == GridStateIdle || state == GridStateWaitNewRange {
				// If paused but in IDLE or WAIT_NEW_RANGE, auto-resume
				a.logger.Warn("Auto-resuming trading for paused symbol in IDLE/WAIT_NEW_RANGE",
					zap.String("symbol", symbol),
					zap.String("state", state.String()))
				a.resumeTrading(symbol)
			}
		}
	}
	if len(pausedSymbols) > 0 {
		a.logger.Warn("Trading PAUSED for symbols", zap.Strings("symbols", pausedSymbols))
	} else {
		a.logger.Info("Trading NOT paused for any symbol")
	}

	// 6. Cooldown status
	cooldownSymbols := make([]string, 0)
	for symbol, active := range a.cooldownActive {
		if active {
			cooldownSymbols = append(cooldownSymbols, symbol)
		}
	}
	a.logger.Info("Cooldown Status",
		zap.Strings("cooldown_symbols", cooldownSymbols),
		zap.Int("count", len(cooldownSymbols)))

	// Note: Balance and GridManager active orders are logged separately in other components
	a.logger.Info("=== END BLOCKING CHECKS STATUS ===")
}

// isTakeProfitHit checks if take profit is hit
func (a *AdaptiveGridManager) isTakeProfitHit(symbol string, markPrice, positionAmt, takeProfit float64) bool {
	if positionAmt == 0 || takeProfit <= 0 {
		return false
	}

	if positionAmt > 0 && markPrice >= takeProfit { // Long
		return true
	}
	if positionAmt < 0 && markPrice <= takeProfit { // Short
		return true
	}

	return false
}

// closePositionWithProfit closes position at take profit
func (a *AdaptiveGridManager) closePositionWithProfit(ctx context.Context, symbol string, positionAmt float64) {
	if positionAmt == 0 || a.futuresClient == nil {
		return
	}

	side := "SELL"
	if positionAmt < 0 {
		side = "BUY" // Close short
	}

	qty := fmt.Sprintf("%.6f", math.Abs(positionAmt))

	orderReq := client.PlaceOrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          "MARKET",
		Quantity:      qty,
		ReduceOnly:    true,
		ClosePosition: true,
	}

	a.logger.Info("TAKE PROFIT CLOSE - Placing market order",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("qty", qty))

	order, err := a.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		a.logger.Error("Failed to close position at take profit", zap.Error(err))
		return
	}

	a.logger.Info("Position closed at take profit successfully",
		zap.String("symbol", symbol),
		zap.Int64("order_id", order.OrderID))

	// Clear position tracking
	a.mu.Lock()
	delete(a.positions, symbol)
	delete(a.positionStopLoss, symbol)
	delete(a.trailingStopPrice, symbol)
	delete(a.positionTakeProfit, symbol)
	a.mu.Unlock()

	// Record win for consecutive loss tracking
	a.RecordTradeResult(symbol, true)
}

// CalculateTakeProfitPrice calculates TP price based on R:R ratio
func (a *AdaptiveGridManager) CalculateTakeProfitPrice(
	symbol string,
	entryPrice float64,
	stopLossPrice float64,
	positionAmt float64,
) float64 {
	if entryPrice <= 0 || stopLossPrice <= 0 || positionAmt == 0 {
		return 0
	}

	// Calculate risk distance
	riskDistance := math.Abs(entryPrice - stopLossPrice)

	// Get target R:R from config
	targetRR := a.riskConfig.TakeProfitRRatio
	if targetRR <= 0 {
		targetRR = 1.5 // Default 1.5:1
	}

	// Calculate reward distance
	rewardDistance := riskDistance * targetRR

	// Apply inventory adjustment (reduce TP when skewed)
	if a.inventoryMgr != nil {
		skewRatio := a.inventoryMgr.CalculateSkewRatio(symbol)
		action := a.inventoryMgr.GetSkewAction(skewRatio)

		// Adjust TP based on skew action
		switch action {
		case SkewActionReduceSkewSide:
			rewardDistance = rewardDistance * 0.85 // Reduce 15%
		case SkewActionPauseSkewSide:
			rewardDistance = rewardDistance * 0.70 // Reduce 30%
		case SkewActionEmergencySkew:
			rewardDistance = rewardDistance * 0.50 // Reduce 50%
		}
	}

	// Apply min/max limits
	minTPPct := a.riskConfig.MinTakeProfitPct
	maxTPPct := a.riskConfig.MaxTakeProfitPct

	minTPDistance := entryPrice * minTPPct
	maxTPDistance := entryPrice * maxTPPct

	if rewardDistance < minTPDistance {
		rewardDistance = minTPDistance
	}
	if rewardDistance > maxTPDistance && maxTPPct > 0 {
		rewardDistance = maxTPDistance
	}

	// Calculate TP price
	if positionAmt > 0 { // Long
		return entryPrice + rewardDistance
	}
	return entryPrice - rewardDistance // Short
}

// setInitialTakeProfit sets the initial take profit price for a position
func (a *AdaptiveGridManager) setInitialTakeProfit(symbol string, entryPrice, stopLossPrice, positionAmt float64) {
	tpPrice := a.CalculateTakeProfitPrice(symbol, entryPrice, stopLossPrice, positionAmt)
	if tpPrice > 0 {
		a.positionTakeProfit[symbol] = tpPrice
		a.logger.Info("Take profit set",
			zap.String("symbol", symbol),
			zap.Float64("entry", entryPrice),
			zap.Float64("stop_loss", stopLossPrice),
			zap.Float64("take_profit", tpPrice),
			zap.Float64("rr", a.riskConfig.TakeProfitRRatio))
	}
}

// CalculateLiquidationBuffer calculates dynamic liquidation buffer based on leverage
// Higher leverage requires larger buffer to prevent liquidation
func (a *AdaptiveGridManager) CalculateLiquidationBuffer(leverage float64) float64 {
	if leverage >= 100 {
		return 0.50 // 50% buffer for 100x leverage
	} else if leverage >= 50 {
		return 0.35 // 35% for 50x leverage
	} else if leverage >= 20 {
		return 0.25 // 25% for 20x leverage
	}
	return 0.20 // 20% for lower leverage
}

// isNearLiquidation checks if position is near liquidation
func (a *AdaptiveGridManager) isNearLiquidation(symbol string, markPrice, liquidationPrice, positionAmt float64) bool {
	if liquidationPrice == 0 || positionAmt == 0 {
		return false
	}

	distance := math.Abs(markPrice - liquidationPrice)
	distancePct := distance / markPrice

	// Use dynamic buffer if position info available, otherwise use config default
	bufferPct := a.riskConfig.LiquidationBufferPct
	leverage := 0.0
	if symbol != "" {
		a.mu.RLock()
		pos := a.positions[symbol]
		a.mu.RUnlock()
		if pos != nil && pos.Leverage > 0 {
			leverage = pos.Leverage
			bufferPct = a.CalculateLiquidationBuffer(pos.Leverage)
		}
	}

	// Calculate effective buffer: bufferPct is percentage of liquidation distance
	// For 100x leverage: liquidation ~1% away, buffer 50% -> effective = 0.5%
	// For 20x leverage: liquidation ~5% away, buffer 25% -> effective = 1.25%
	var effectiveBufferPct float64
	if leverage > 0 {
		liqDistancePct := 1.0 / leverage // Approximate liquidation distance from entry
		effectiveBufferPct = liqDistancePct * bufferPct
	} else {
		// Fallback: assume 100x leverage if unknown
		effectiveBufferPct = 0.01 * bufferPct
	}

	a.logger.Debug("Liquidation check",
		zap.String("symbol", symbol),
		zap.Float64("distance_pct", distancePct),
		zap.Float64("buffer_pct", bufferPct),
		zap.Float64("effective_buffer_pct", effectiveBufferPct),
		zap.Float64("leverage", leverage))

	return distancePct < effectiveBufferPct
}

// emergencyClosePosition closes position immediately with market order
func (a *AdaptiveGridManager) emergencyClosePosition(ctx context.Context, symbol string, positionAmt float64) {
	if positionAmt == 0 || a.futuresClient == nil {
		return
	}

	side := "SELL"
	if positionAmt < 0 {
		side = "BUY" // Close short
	}

	qty := fmt.Sprintf("%.6f", math.Abs(positionAmt))

	// Try with ReduceOnly first
	orderReq := client.PlaceOrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          "MARKET",
		Quantity:      qty,
		ReduceOnly:    true,
		ClosePosition: true,
	}

	a.logger.Warn("EMERGENCY CLOSE - Placing market order (with ReduceOnly)",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("qty", qty))

	order, err := a.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		a.logger.Warn("Emergency close with ReduceOnly failed, retrying without ReduceOnly",
			zap.String("symbol", symbol),
			zap.Error(err))

		// Retry without ReduceOnly
		orderReq.ReduceOnly = false
		order, err = a.futuresClient.PlaceOrder(ctx, orderReq)
		if err != nil {
			a.logger.Error("Failed to emergency close position even without ReduceOnly - symbol may be stuck",
				zap.String("symbol", symbol),
				zap.Error(err))
			// Don't return - allow the symbol to potentially resume trading despite the failure
			// This prevents getting stuck forever in WAIT_NEW_RANGE
		}
	}

	if err == nil {
		a.logger.Info("Position closed successfully",
			zap.String("symbol", symbol),
			zap.Int64("order_id", order.OrderID))

		// Clear position tracking
		a.mu.Lock()
		delete(a.positions, symbol)
		delete(a.positionStopLoss, symbol)
		delete(a.trailingStopPrice, symbol)
		delete(a.positionTakeProfit, symbol)
		a.mu.Unlock()
	} else {
		a.logger.Error("Emergency close failed - position remains open, symbol may resume with existing position",
			zap.String("symbol", symbol),
			zap.Error(err))
	}

	// Clear grid and transition to WAIT_NEW_RANGE. Re-entry must go through regrid readiness.
	if a.gridManager != nil {
		a.logger.Info("Clearing grid after emergency close",
			zap.String("symbol", symbol))

		if err := a.gridManager.ClearGrid(ctx, symbol); err != nil {
			a.logger.Error("Failed to clear grid after emergency close",
				zap.String("symbol", symbol),
				zap.Error(err))
		}
	}

	a.markExitCompleted(symbol)
	a.logger.Info("Trading moved to WAIT_NEW_RANGE after emergency close",
		zap.String("symbol", symbol))
}

// emergencyCloseAll closes ALL positions and cancels ALL orders across all symbols
// This is triggered when total net unrealized loss exceeds the limit (5 USDT)
func (a *AdaptiveGridManager) emergencyCloseAll(ctx context.Context, positions []client.Position) {
	a.logger.Error("EMERGENCY CLOSE ALL - Closing all positions and cancelling all orders!")

	// 1. Cancel ALL orders for ALL symbols
	// We need to get unique symbols from all positions
	symbolsMap := make(map[string]bool)
	for _, pos := range positions {
		symbolsMap[pos.Symbol] = true
	}

	// Also include any symbols that might have orders but no positions
	a.mu.RLock()
	for symbol := range a.positions {
		symbolsMap[symbol] = true
	}
	a.mu.RUnlock()

	// Cancel all orders for each symbol
	for symbol := range symbolsMap {
		a.logger.Warn("Cancelling all orders for symbol", zap.String("symbol", symbol))
		if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
			a.logger.Error("Failed to cancel orders for symbol", zap.String("symbol", symbol), zap.Error(err))
		}
	}

	// 2. Close ALL positions
	for _, pos := range positions {
		if pos.PositionAmt == 0 {
			continue
		}

		symbol := pos.Symbol
		side := "SELL"
		if pos.PositionAmt < 0 {
			side = "BUY" // Close short
		}

		qty := fmt.Sprintf("%.6f", math.Abs(pos.PositionAmt))

		orderReq := client.PlaceOrderRequest{
			Symbol:        symbol,
			Side:          side,
			Type:          "MARKET",
			Quantity:      qty,
			ReduceOnly:    true,
			ClosePosition: true,
		}

		a.logger.Error("EMERGENCY CLOSE - Closing position",
			zap.String("symbol", symbol),
			zap.String("side", side),
			zap.String("qty", qty),
			zap.Float64("unrealized_pnl", pos.UnrealizedProfit))

		order, err := a.futuresClient.PlaceOrder(ctx, orderReq)
		if err != nil {
			a.logger.Error("Failed to close position", zap.String("symbol", symbol), zap.Error(err))
			continue
		}

		a.logger.Info("Position closed successfully",
			zap.String("symbol", symbol),
			zap.Int64("order_id", order.OrderID))
	}

	// 3. Clear ALL position tracking
	a.mu.Lock()
	a.positions = make(map[string]*SymbolPosition)
	a.positionStopLoss = make(map[string]float64)
	a.trailingStopPrice = make(map[string]float64)
	a.mu.Unlock()

	// 4. Pause trading for ALL symbols
	for symbol := range symbolsMap {
		a.pauseTrading(symbol)
	}

	// 5. Initialize range detectors for all symbols to wait for stabilization
	for symbol := range symbolsMap {
		// Initialize or reset range detector to wait for new stable range
		config := DefaultRangeConfig()
		config.StabilizationPeriod = 1 * time.Minute // Wait 1 minute for stabilization (reduced from 5min)
		a.InitializeRangeDetector(symbol, config)
		a.logger.Info("Range detector initialized - waiting for price stabilization",
			zap.String("symbol", symbol),
			zap.Duration("stabilization_period", config.StabilizationPeriod))
	}

	a.logger.Error("EMERGENCY CLOSE ALL COMPLETE - All positions closed, trading paused, waiting for stabilization")
}

// pauseTrading pauses trading for a symbol with auto-resume timer
func (a *AdaptiveGridManager) pauseTrading(symbol string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tradingPaused[symbol] = true

	// Set auto-resume timer (default 1 minute - reduced from 5min)
	stabilizationPeriod := 1 * time.Minute

	a.logger.Info("Trading paused with auto-resume timer",
		zap.String("symbol", symbol),
		zap.Duration("auto_resume_after", stabilizationPeriod))

	// Schedule auto-resume with full logic
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Error("Auto-resume goroutine panic recovered",
					zap.String("symbol", symbol),
					zap.Any("panic", r))
			}
		}()

		time.Sleep(stabilizationPeriod)
		a.mu.Lock()
		if paused, exists := a.tradingPaused[symbol]; exists && paused {
			delete(a.tradingPaused, symbol)
			a.mu.Unlock()
			a.logger.Info("Auto-resuming trading after stabilization period",
				zap.String("symbol", symbol))

			// NEW: Trigger full resume logic including state transitions and grid rebuild
			if a.TryResumeTrading(symbol) {
				a.logger.Info("Auto-resume with state transition successful",
					zap.String("symbol", symbol))
			} else {
				a.logger.Warn("Auto-resume failed, will retry via CheckAndResumeAll",
					zap.String("symbol", symbol))
			}
		} else {
			a.mu.Unlock()
		}
	}()
}

// resumeTrading resumes trading for a symbol
func (a *AdaptiveGridManager) resumeTrading(symbol string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.tradingPaused, symbol)
}

// TryResumeTrading attempts to resume trading for a symbol if conditions are met
// This is called after emergency close to wait for price stabilization
func (a *AdaptiveGridManager) TryResumeTrading(symbol string) bool {
	a.mu.RLock()
	isPaused := a.tradingPaused[symbol]
	detector, hasDetector := a.rangeDetectors[symbol]
	stateMachine := a.stateMachine
	a.mu.RUnlock()

	// If not paused, nothing to do
	if !isPaused {
		return true
	}

	// If no range detector, we can't check stabilization
	if !hasDetector {
		a.logger.Warn("No range detector found - refusing to resume trading without range confirmation",
			zap.String("symbol", symbol))
		return false
	}

	// Require full regrid readiness while waiting for a new range.
	// However, add a timeout to prevent getting stuck forever if emergency close failed
	if stateMachine != nil && stateMachine.GetState(symbol) == GridStateWaitNewRange {
		// Check how long we've been in WAIT_NEW_RANGE
		stateTime := stateMachine.GetStateTime(symbol)
		waitDuration := time.Since(stateTime)
		maxWaitTime := 2 * time.Minute // Force resume after 2 minutes (reduced from 10min)

		if waitDuration > maxWaitTime {
			a.logger.Warn("WAIT_NEW_RANGE timeout - forcing resume to prevent stuck state",
				zap.String("symbol", symbol),
				zap.Duration("waited", waitDuration),
				zap.Duration("max_wait", maxWaitTime))
			// Force state to IDLE to allow reentry
			stateMachine.ForceState(symbol, GridStateIdle)
		} else if !a.isReadyForRegrid(symbol) {
			a.logger.Debug("Waiting for regrid conditions",
				zap.String("symbol", symbol),
				zap.String("state", stateMachine.GetState(symbol).String()),
				zap.Duration("waited", waitDuration),
				zap.Duration("remaining", maxWaitTime-waitDuration))
			return false
		}

		if stateMachine.CanTransition(symbol, EventNewRangeReady) {
			stateMachine.Transition(symbol, EventNewRangeReady)
		}
		// Removed ClearRegridCooldown - cooldown disabled
	}

	// Check if range is active and trading is allowed
	// For WAIT_NEW_RANGE state, we already checked isReadyForRegrid above
	// For other states, just check if range detector allows trading
	currentState := GridStateIdle
	if stateMachine != nil {
		currentState = stateMachine.GetState(symbol)
	}

	canResume := false
	if currentState == GridStateWaitNewRange {
		// Already checked isReadyForRegrid above, just need range to be active
		canResume = detector.ShouldTrade()
	} else {
		// For other states, just check if range allows trading (don't require zero position)
		canResume = detector.ShouldTrade()
	}

	if canResume {
		currentRegime := a.GetCurrentRegime(symbol)
		a.logger.Info("Resuming trading - Range is active",
			zap.String("symbol", symbol),
			zap.String("range_state", detector.GetStateString()),
			zap.String("current_state", currentState.String()),
			zap.String("regime", string(currentRegime)))
		a.resumeTrading(symbol)
		if a.gridManager != nil {
			if err := a.gridManager.RebuildGrid(context.Background(), symbol); err != nil {
				a.logger.Warn("Failed to rebuild grid after range reactivation",
					zap.String("symbol", symbol),
					zap.Error(err))
			}
		}
		return true
	}

	// Still waiting for stabilization
	a.logger.Debug("Waiting for price stabilization before resuming",
		zap.String("symbol", symbol),
		zap.String("range_state", detector.GetStateString()))
	return false
}

// CheckAndResumeAll attempts to resume trading for all paused symbols
// Returns map of symbol -> resumed (true/false)
func (a *AdaptiveGridManager) CheckAndResumeAll() map[string]bool {
	a.mu.RLock()
	pausedSymbols := make([]string, 0, len(a.tradingPaused))
	for symbol := range a.tradingPaused {
		pausedSymbols = append(pausedSymbols, symbol)
	}
	a.mu.RUnlock()

	results := make(map[string]bool)
	for _, symbol := range pausedSymbols {
		results[symbol] = a.TryResumeTrading(symbol)
	}
	return results
}

// IsTradingPaused checks if trading is paused for a symbol
func (a *AdaptiveGridManager) IsTradingPaused(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tradingPaused[symbol]
}

// GetPosition returns current position for a symbol
func (a *AdaptiveGridManager) GetPosition(symbol string) *SymbolPosition {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.positions[symbol]
}

// GetAllPositions returns all tracked positions
func (a *AdaptiveGridManager) GetAllPositions() map[string]*SymbolPosition {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]*SymbolPosition)
	for k, v := range a.positions {
		result[k] = v
	}
	return result
}

// GetCurrentRegime returns the current regime for a symbol
func (a *AdaptiveGridManager) GetCurrentRegime(symbol string) market_regime.MarketRegime {
	a.mu.RLock()
	defer a.mu.RUnlock()

	regime, exists := a.currentRegime[symbol]
	if !exists {
		regime = market_regime.RegimeUnknown
	}

	return regime
}

// OnRegimeChange handles regime change notifications
func (a *AdaptiveGridManager) OnRegimeChange(symbol string, oldRegime, newRegime market_regime.MarketRegime) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.currentRegime[symbol] = newRegime
	a.lastRegimeChange[symbol] = time.Now()

	a.logger.Info("Regime change detected",
		zap.String("symbol", symbol),
		zap.String("from", string(oldRegime)),
		zap.String("to", string(newRegime)),
		zap.Time("timestamp", a.lastRegimeChange[symbol]))

	// CRITICAL: Automatically trigger HandleRegimeTransition for parameter switching
	// Run in goroutine to avoid blocking the price update loop
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Error("Regime transition goroutine panic recovered",
					zap.String("symbol", symbol),
					zap.String("from", string(oldRegime)),
					zap.String("to", string(newRegime)),
					zap.Any("panic", r))
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := a.HandleRegimeTransition(ctx, symbol, oldRegime, newRegime); err != nil {
			a.logger.Error("Failed to handle regime transition",
				zap.String("symbol", symbol),
				zap.String("from", string(oldRegime)),
				zap.String("to", string(newRegime)),
				zap.Error(err))
		} else {
			a.logger.Info("Regime transition completed successfully",
				zap.String("symbol", symbol),
				zap.String("regime", string(newRegime)))
		}
	}()
}

// IsInTransitionCooldown checks if symbol is in transition cooldown
func (a *AdaptiveGridManager) IsInTransitionCooldown(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	cooldown, exists := a.transitionCooldown[symbol]
	if !exists {
		return false
	}

	return time.Since(cooldown) < 30*time.Second
}

// IsInSlotTransitionCooldown checks if symbol is in slot transition cooldown (2 minutes)
func (a *AdaptiveGridManager) IsInSlotTransitionCooldown(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	cooldown, exists := a.transitionCooldown[symbol]
	if !exists {
		return false
	}

	return time.Since(cooldown) < 2*time.Minute
}

// SetTransitionCooldown sets the transition cooldown for a symbol
func (a *AdaptiveGridManager) SetTransitionCooldown(symbol string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.transitionCooldown[symbol] = time.Now()
}

// HandleRegimeTransition handles regime change with smooth transition
func (a *AdaptiveGridManager) HandleRegimeTransition(
	ctx context.Context,
	symbol string,
	oldRegime, newRegime market_regime.MarketRegime,
) error {
	a.logger.Info("Starting regime transition",
		zap.String("symbol", symbol),
		zap.String("from", string(oldRegime)),
		zap.String("to", string(newRegime)))

	// Check if we have position before clearing
	position := a.GetPosition(symbol)
	if position != nil && math.Abs(position.PositionAmt) > 0 {
		// In trending regime, consider closing position
		if newRegime == market_regime.RegimeTrending {
			a.logger.Warn("Transitioning to trending regime with open position - considering close",
				zap.String("symbol", symbol),
				zap.Float64("position_amt", position.PositionAmt),
				zap.Float64("unrealized_pnl", position.UnrealizedPnL))
			// Note: We don't auto-close here, let risk monitor handle it
		}
	}

	// Set transition cooldown
	a.SetTransitionCooldown(symbol)

	// Phase 1: Cancel existing orders
	if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
		return err
	}

	// Phase 2: Wait for transition period
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
		a.logger.Info("Transition cooldown completed", zap.String("symbol", symbol))
	case <-ctx.Done():
		return ctx.Err()
	}

	// Phase 3: Apply new regime parameters
	if err := a.applyNewRegimeParameters(ctx, symbol, newRegime); err != nil {
		return err
	}

	// Phase 4: Rebuild grid with new parameters (only if not paused)
	if !a.IsTradingPaused(symbol) {
		if err := a.gridManager.RebuildGrid(ctx, symbol); err != nil {
			return err
		}
	}

	a.logger.Info("Regime transition completed successfully",
		zap.String("symbol", symbol),
		zap.String("final_regime", string(newRegime)))

	return nil
}

// applyNewRegimeParameters applies new regime-specific parameters
func (a *AdaptiveGridManager) applyNewRegimeParameters(
	ctx context.Context,
	symbol string,
	newRegime market_regime.MarketRegime,
) error {
	a.logger.Info("Applying new regime parameters",
		zap.String("symbol", symbol),
		zap.String("regime", string(newRegime)))

	// Get regime configuration
	regimeConfig, exists := a.configManager.GetRegimeConfig(string(newRegime))
	if !exists {
		return fmt.Errorf("no configuration found for regime: %s", newRegime)
	}

	// Adjust risk parameters based on regime
	a.mu.Lock()
	defer a.mu.Unlock()

	switch newRegime {
	case market_regime.RegimeTrending:
		// In trending: tighten stop loss, reduce position size
		a.riskConfig.StopLossPct = 0.01      // 1% stop
		a.riskConfig.MaxPositionUSDT = 500.0 // Reduce max position
		a.tradingPaused[symbol] = true       // Pause until trend stabilizes
		a.logger.Warn("TRENDING regime: Trading paused, tight stops enabled")

	case market_regime.RegimeRanging:
		// In ranging: normal parameters, resume trading
		a.riskConfig.StopLossPct = 0.015      // 1.5% stop
		a.riskConfig.MaxPositionUSDT = 1000.0 // Normal max position
		delete(a.tradingPaused, symbol)
		a.logger.Info("RANGING regime: Trading resumed with normal parameters")

	case market_regime.RegimeVolatile:
		// In volatile: wider stops, smaller positions
		a.riskConfig.StopLossPct = 0.02      // 2% stop
		a.riskConfig.MaxPositionUSDT = 300.0 // Smaller positions
		a.logger.Warn("VOLATILE regime: Reduced position size, wider stops")
	}

	// Apply parameters to grid manager
	a.gridManager.SetOrderSize(regimeConfig.OrderSizeUSDT)
	a.gridManager.SetGridSpread(regimeConfig.GridSpreadPct)
	a.gridManager.SetMaxOrdersPerSide(regimeConfig.MaxOrdersPerSide)
	a.gridManager.SetPositionTimeout(regimeConfig.PositionTimeoutMinutes)

	a.logger.Info("New regime parameters applied successfully",
		zap.String("symbol", symbol),
		zap.String("regime", string(newRegime)))

	return nil
}

// Stop stops the adaptive grid manager
func (a *AdaptiveGridManager) Stop(ctx context.Context) error {
	a.logger.Info("Stopping adaptive grid manager")

	// Safely close stopCh
	select {
	case <-a.stopCh:
		// already closed
	default:
		close(a.stopCh)
	}

	// NEW: Stop RiskMonitor
	if a.riskMonitor != nil {
		a.riskMonitor.Stop()
		a.logger.Info("RiskMonitor stopped")
	}

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Error("AdaptiveGridManager WaitGroup goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		a.logger.Info("Adaptive grid manager stopped gracefully")
		return nil
	case <-ctx.Done():
		a.logger.Warn("Adaptive grid manager stop timeout")
		return ctx.Err()
	}
}

// CanPlaceOrder checks if new orders can be placed for a symbol
// Simplified: CircuitBreaker is the single source of truth for trading decisions
func (a *AdaptiveGridManager) CanPlaceOrder(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	a.logger.Info("=== CanPlaceOrder START ===", zap.String("symbol", symbol))

	// Check 1: Manual pause
	if a.tradingPaused[symbol] {
		a.logger.Warn("CanPlaceOrder BLOCKED: trading paused",
			zap.String("symbol", symbol))
		return false
	}
	a.logger.Debug("Check 1 PASS: trading not paused", zap.String("symbol", symbol))

	// Check 2: Position limit (1.2x hard cap)
	if position, exists := a.positions[symbol]; exists {
		if position.NotionalValue >= a.riskConfig.MaxPositionUSDT*1.2 {
			a.logger.Error("CanPlaceOrder BLOCKED: hard position cap exceeded",
				zap.String("symbol", symbol),
				zap.Float64("notional", position.NotionalValue),
				zap.Float64("max_allowed", a.riskConfig.MaxPositionUSDT*1.2))
			return false
		}
		a.logger.Debug("Check 2 PASS: position within hard cap",
			zap.String("symbol", symbol),
			zap.Float64("notional", position.NotionalValue))
	} else {
		a.logger.Debug("Check 2 PASS: no position", zap.String("symbol", symbol))
	}

	// Check 3: Grid State - REMOVED state-based blocking
	// Replaced with ConditionBlocker for graduated blocking (see GetOrderSizeMultiplier)
	// State transitions still tracked for logging and other purposes
	if a.stateMachine != nil {
		currentState := a.stateMachine.GetState(symbol)
		a.logger.Debug("Check 3: State-based blocking removed, using ConditionBlocker for graduated blocking",
			zap.String("symbol", symbol),
			zap.String("state", currentState.String()))
	}

	// Check 3.5: ConditionBlocker - graduated blocking based on conditions
	if a.conditionBlocker != nil {
		blockingFactor := a.GetOrderSizeMultiplier(symbol)
		a.logger.Info("Check 3.5: ConditionBlocker decision",
			zap.String("symbol", symbol),
			zap.Float64("blocking_factor", blockingFactor))

		// If blocking factor is 0, fully block trading
		if blockingFactor <= 0.01 {
			a.logger.Warn("CanPlaceOrder BLOCKED: ConditionBlocker full block",
				zap.String("symbol", symbol),
				zap.Float64("blocking_factor", blockingFactor))
			return false
		}

		// If blocking factor is very low (MICRO mode threshold), still allow but log
		if blockingFactor < 0.1 {
			a.logger.Info("CanPlaceOrder: MICRO mode allowed by ConditionBlocker",
				zap.String("symbol", symbol),
				zap.Float64("blocking_factor", blockingFactor))
		}
	}

	// Check 4: CircuitBreaker (single source of truth)
	if a.circuitBreaker != nil {
		if cb, ok := a.circuitBreaker.(interface {
			GetSymbolDecision(symbol string) (canTrade bool, tradingMode string)
		}); ok {
			canTrade, mode := cb.GetSymbolDecision(symbol)
			modeMultiplier := a.GetModeSizeMultiplier(mode)

			a.logger.Info("Check 4: CircuitBreaker decision",
				zap.String("symbol", symbol),
				zap.Bool("can_trade", canTrade),
				zap.String("mode", mode),
				zap.Float64("mode_multiplier", modeMultiplier))

			// MICRO mode always allows trading (never blocks)
			if mode == "MICRO" {
				a.logger.Info("MICRO mode: always allows trading",
					zap.String("symbol", symbol),
					zap.Float64("size_multiplier", modeMultiplier))
				return true
			}

			// PAUSED mode blocks all trading
			if mode == "PAUSED" {
				a.logger.Warn("CanPlaceOrder BLOCKED: PAUSED mode",
					zap.String("symbol", symbol),
					zap.String("mode", mode))
				return false
			}

			// Respect CircuitBreaker's canTrade decision for other modes
			if !canTrade {
				a.logger.Warn("CanPlaceOrder BLOCKED: CircuitBreaker",
					zap.String("symbol", symbol),
					zap.String("mode", mode))
				return false
			}
		} else {
			a.logger.Warn("CircuitBreaker exists but GetSymbolDecision not available",
				zap.String("symbol", symbol))
		}
	} else {
		a.logger.Warn("CircuitBreaker is nil",
			zap.String("symbol", symbol))
	}
	a.logger.Debug("Check 4 PASS: CircuitBreaker allows trading", zap.String("symbol", symbol))

	// Check 5: TimeFilter - DISABLED for volume farming (trade 24/7)
	// if a.timeFilter != nil {
	// 	canTrade := a.timeFilter.CanTrade()
	// 	a.logger.Info("Check 5: TimeFilter",
	// 		zap.String("symbol", symbol),
	// 		zap.Bool("can_trade", canTrade))
	// 	if !canTrade {
	// 		a.logger.Warn("CanPlaceOrder BLOCKED: outside trading hours",
	// 			zap.String("symbol", symbol))
	// 		return false
	// 	}
	// } else {
	// 	a.logger.Debug("Check 5 SKIP: TimeFilter is nil", zap.String("symbol", symbol))
	// }
	a.logger.Debug("Check 5 SKIP: TimeFilter disabled for volume farming", zap.String("symbol", symbol))

	// Check 6: FundingRate
	if a.fundingMonitor != nil {
		biasSide, _, shouldSkip := a.fundingMonitor.GetFundingBias(symbol)
		a.logger.Info("Check 6: FundingRate",
			zap.String("symbol", symbol),
			zap.String("bias_side", biasSide),
			zap.Bool("should_skip", shouldSkip))
		if shouldSkip {
			a.logger.Warn("CanPlaceOrder BLOCKED: extreme funding rate",
				zap.String("symbol", symbol),
				zap.String("bias_side", biasSide))
			return false
		}
	} else {
		a.logger.Debug("Check 5 SKIP: FundingMonitor is nil", zap.String("symbol", symbol))
	}

	// Check 7: VPIN Toxic Flow Detection
	if a.vpinMonitor != nil {
		// Type assertion to access VPINMonitor methods
		vpinMonitor, ok := a.vpinMonitor.(interface {
			IsToxic() bool
			IsPaused() bool
			TriggerPause()
			Resume()
			GetPauseStartTime() time.Time
			GetAutoResumeDelay() time.Duration
			GetVPIN() float64
		})

		if ok {
			// Check if currently paused due to toxic flow
			if a.tradingPausedGlobal {
				// Check auto-resume condition
				if vpinMonitor.IsPaused() {
					pauseDuration := time.Since(vpinMonitor.GetPauseStartTime())
					autoResumeDelay := vpinMonitor.GetAutoResumeDelay()

					if pauseDuration > autoResumeDelay {
						// Auto-resume trading
						vpinMonitor.Resume()
						a.mu.Lock()
						a.tradingPausedGlobal = false
						a.pauseReason = ""
						a.mu.Unlock()
						a.logger.Info("AUTO-RESUME: Trading resumed after toxic flow pause",
							zap.String("symbol", symbol),
							zap.Duration("pause_duration", pauseDuration),
							zap.Duration("auto_resume_delay", autoResumeDelay))
					} else {
						a.logger.Warn("CanPlaceOrder BLOCKED: Trading paused due to toxic flow",
							zap.String("symbol", symbol),
							zap.String("pause_reason", a.pauseReason),
							zap.Duration("pause_duration", pauseDuration),
							zap.Duration("remaining_delay", autoResumeDelay-pauseDuration))
						return false
					}
				}
			} else {
				// Check for toxic flow condition
				if vpinMonitor.IsToxic() {
					vpinMonitor.TriggerPause()
					a.mu.Lock()
					a.tradingPausedGlobal = true
					a.pauseReason = "toxic_vpin"
					a.mu.Unlock()
					a.logger.Warn("TRADING PAUSED: Toxic flow detected by VPIN",
						zap.String("symbol", symbol),
						zap.Float64("vpin_value", vpinMonitor.GetVPIN()),
						zap.String("pause_reason", a.pauseReason),
						zap.Duration("auto_resume_delay", vpinMonitor.GetAutoResumeDelay()))
					return false
				}
				a.logger.Debug("Check 7 PASS: No toxic flow detected", zap.String("symbol", symbol))
			}
		} else {
			// Fallback if type assertion fails
			a.logger.Warn("VPIN monitor type assertion failed, skipping VPIN check", zap.String("symbol", symbol))
		}
	} else {
		a.logger.Debug("Check 7 SKIP: VPIN monitor is nil", zap.String("symbol", symbol))
	}

	a.logger.Info("=== CanPlaceOrder ALL CHECKS PASSED ===", zap.String("symbol", symbol))
	return true
}

// GetOrderSizeMultiplier calculates the order size multiplier using ConditionBlocker
// Returns a value between 0 and 1, where 1 = full size, 0.1 = MICRO mode
func (a *AdaptiveGridManager) GetOrderSizeMultiplier(symbol string) float64 {
	if a.conditionBlocker == nil {
		// If ConditionBlocker not initialized, return 1.0 (full size)
		return 1.0
	}

	// Get market data from RangeDetector
	detector := a.rangeDetectors[symbol]
	if detector == nil {
		return 1.0
	}

	// Calculate position size score
	positionSizeScore := 0.0
	if position, exists := a.positions[symbol]; exists {
		positionSizeScore = a.conditionBlocker.NormalizePositionSize(
			position.NotionalValue,
			a.riskConfig.MaxPositionUSDT,
		)
	}

	// Calculate volatility score from ATR and volatility
	atrPct := 0.0
	volatilityPct := 0.0
	if detector.currentRange != nil {
		atr := detector.currentRange.ATR
		currentPrice := detector.currentRange.MidPrice // Use MidPrice as current price
		if currentPrice > 0 {
			atrPct = atr / currentPrice
		}
		volatilityPct = detector.currentRange.Volatility
	}
	volatilityScore := a.conditionBlocker.NormalizeVolatility(atrPct, volatilityPct)

	// Calculate risk score from PnL and drawdown
	riskScore := 0.0
	if position, exists := a.positions[symbol]; exists {
		drawdown := 0.0
		// RiskMonitor may not have GetDrawdown method, use default
		drawdown = 0.0
		riskScore = a.conditionBlocker.NormalizeRisk(position.UnrealizedPnL, drawdown)
	}

	// Calculate trend score from volatility (use as proxy for ADX)
	trendScore := 0.0
	if detector.currentRange != nil {
		// Use volatility as proxy for trend strength
		trendScore = detector.currentRange.Volatility
		if trendScore > 1 {
			trendScore = 1
		}
	}

	// Calculate skew score from inventory (default to 0 if not available)
	skewScore := 0.0
	// InventoryManager may not have GetSkew method, use default
	skewScore = 0.0

	// Calculate blocking factor
	blockingFactor := a.conditionBlocker.CalculateBlockingFactor(
		positionSizeScore,
		volatilityScore,
		riskScore,
		trendScore,
		skewScore,
	)

	// Get size multiplier from ConditionBlocker
	sizeMultiplier := a.conditionBlocker.GetSizeMultiplier(blockingFactor)

	// NEW: Apply FluidFlowEngine size multiplier for continuous flow behavior
	if a.fluidFlowEngine != nil {
		// Calculate flow parameters
		flowParams := a.fluidFlowEngine.CalculateFlow(
			symbol,
			positionSizeScore,
			volatilityScore,
			riskScore,
			trendScore,
			skewScore,
			0.5, // liquidity (default)
		)

		// Apply fluid flow size multiplier
		sizeMultiplier *= flowParams.SizeMultiplier

		a.logger.Debug("FluidFlowEngine applied",
			zap.String("symbol", symbol),
			zap.Float64("flow_intensity", flowParams.Intensity),
			zap.Float64("flow_direction", flowParams.Direction),
			zap.Float64("flow_velocity", flowParams.Velocity),
			zap.Float64("size_multiplier", flowParams.SizeMultiplier),
			zap.Float64("spread_multiplier", flowParams.SpreadMultiplier),
			zap.Float64("final_size_multiplier", sizeMultiplier))
	}

	a.logger.Debug("Order size multiplier calculated",
		zap.String("symbol", symbol),
		zap.Float64("position_size_score", positionSizeScore),
		zap.Float64("volatility_score", volatilityScore),
		zap.Float64("risk_score", riskScore),
		zap.Float64("trend_score", trendScore),
		zap.Float64("skew_score", skewScore),
		zap.Float64("blocking_factor", blockingFactor),
		zap.Float64("size_multiplier", sizeMultiplier))

	return sizeMultiplier
}

// SetConditionBlocker sets the ConditionBlocker instance
func (a *AdaptiveGridManager) SetConditionBlocker(cb *ConditionBlocker) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conditionBlocker = cb
	a.logger.Info("ConditionBlocker set")
}

// ApplyFundingBias applies funding rate bias to inventory manager
// Called periodically to update bias based on current funding rates
func (a *AdaptiveGridManager) ApplyFundingBias(symbol string) {
	if a.fundingMonitor == nil || a.inventoryMgr == nil {
		return
	}

	biasSide, strength, shouldSkip := a.fundingMonitor.GetFundingBias(symbol)

	if shouldSkip {
		a.logger.Info("ApplyFundingBias: extreme funding - consider pausing",
			zap.String("symbol", symbol),
			zap.String("side", biasSide))
		return
	}

	if biasSide != "" {
		// Apply bias to inventory manager
		a.inventoryMgr.SetBias(symbol, biasSide, strength)
		a.logger.Info("Funding bias applied",
			zap.String("symbol", symbol),
			zap.String("favored_side", biasSide),
			zap.Float64("strength", strength))
	} else {
		// Clear bias if funding normal
		a.inventoryMgr.ClearFundingBias(symbol)
	}
}

// CheckFundingAndApplyBias checks funding for all symbols and applies bias
func (a *AdaptiveGridManager) CheckFundingAndApplyBias() {
	if a.fundingMonitor == nil {
		return
	}

	a.mu.RLock()
	symbols := make([]string, 0, len(a.rangeDetectors))
	for symbol := range a.rangeDetectors {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range symbols {
		a.ApplyFundingBias(symbol)
	}
}

// RunPeriodicChecks runs all periodic checks including auto-recovery
func (a *AdaptiveGridManager) RunPeriodicChecks() {
	// Run auto-recovery every 30 seconds to unblock stuck symbols
	a.AutoRecovery()

	// Check funding bias
	a.CheckFundingAndApplyBias()

	// Log blocking status for debugging
	a.logBlockingChecksStatus()
}

// CalculateDynamicSpread calculates dynamic grid spread for a symbol based on ATR
// Returns the adjusted spread percentage
func (a *AdaptiveGridManager) CalculateDynamicSpread(symbol string) float64 {
	if a.dynamicSpreadCalc == nil {
		// Fallback to base spread from config
		return a.riskConfig.BaseGridSpreadPct
	}

	spread := a.dynamicSpreadCalc.GetDynamicSpread()
	a.logger.Debug("Dynamic spread calculated",
		zap.String("symbol", symbol),
		zap.Float64("spread_pct", spread),
		zap.String("volatility", a.dynamicSpreadCalc.GetVolatilityLevel().String()))

	return spread
}

// UpdatePriceDataForSpread updates price data for dynamic spread calculation
func (a *AdaptiveGridManager) UpdatePriceDataForSpread(symbol string, high, low, close float64) {
	if a.dynamicSpreadCalc == nil {
		return
	}

	a.dynamicSpreadCalc.UpdateATR(high, low, close)
}

// GetCurrentSlot returns the current time slot configuration
func (a *AdaptiveGridManager) GetCurrentSlot() *TimeSlotConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return nil
	}

	return a.timeFilter.GetCurrentSlot()
}

// CanTrade checks if trading is allowed based on time filter - DISABLED for volume farming (trade 24/7)
func (a *AdaptiveGridManager) CanTrade() bool {
	// DISABLED: Always return true for volume farming
	return true
}

// InitializeTimeFilter creates or reinitializes the time filter with config
func (a *AdaptiveGridManager) InitializeTimeFilter(config *TradingHoursConfig) error {
	filter, err := NewTimeFilter(config, a.logger)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.timeFilter = filter

	a.logger.Info("TimeFilter initialized/reloaded",
		zap.String("mode", string(config.Mode)),
		zap.Int("slots", len(config.Slots)))
	return nil
}

// GetTimeFilterStatus returns current time filter status
func (a *AdaptiveGridManager) GetTimeFilterStatus() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return map[string]interface{}{"error": "time filter not initialized"}
	}

	return a.timeFilter.GetCurrentStatus()
}

// GetTimeBasedSizeMultiplier returns size multiplier for current time
func (a *AdaptiveGridManager) GetTimeBasedSizeMultiplier() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return 1.0 // Default
	}

	return a.timeFilter.GetSizeMultiplier()
}

// GetTimeBasedSpreadMultiplier returns spread multiplier for current time
func (a *AdaptiveGridManager) GetTimeBasedSpreadMultiplier() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return 1.0 // Default
	}

	return a.timeFilter.GetSpreadMultiplier()
}

// IsHighVolatilityTime returns true if currently in high volatility period
func (a *AdaptiveGridManager) IsHighVolatilityTime() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return false
	}

	return a.timeFilter.IsHighVolatilityPeriod()
}

// GetCurrentTimeSlot returns current time slot info
func (a *AdaptiveGridManager) GetCurrentTimeSlot() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return map[string]interface{}{"error": "time filter not initialized"}
	}

	return a.timeFilter.GetCurrentStatus()
}

// GetOrderSize returns dynamically calculated order size with time-based adjustment
func (a *AdaptiveGridManager) GetOrderSize(symbol string, currentPrice float64, isLong bool) (float64, error) {
	// NEW: Check if micro grid mode is enabled
	if a.IsMicroGridEnabled() {
		microGridSize := a.GetMicroGridOrderSize(currentPrice)
		if microGridSize > 0 {
			a.logger.Info("Using micro grid order size",
				zap.String("symbol", symbol),
				zap.Float64("micro_grid_size", microGridSize))
			return microGridSize, nil
		}
	}

	if a.riskMonitor == nil {
		return 0, fmt.Errorf("risk monitor not initialized")
	}

	regime := a.GetCurrentRegime(symbol)
	size, err := a.riskMonitor.GetOrderSize(symbol, currentPrice, isLong, regime)
	if err != nil {
		return 0, err
	}

	// Apply time-based size multiplier
	sizeMultiplier := a.GetTimeBasedSizeMultiplier()
	if sizeMultiplier > 0 && sizeMultiplier != 1.0 {
		adjustedSize := size * sizeMultiplier
		a.logger.Info("Order size adjusted by time filter",
			zap.String("symbol", symbol),
			zap.Float64("base_size", size),
			zap.Float64("multiplier", sizeMultiplier),
			zap.Float64("adjusted_size", adjustedSize))
		size = adjustedSize
	}

	// NEW: Apply dynamic leverage adjustment
	optimalLeverage := a.GetOptimalLeverage()
	defaultLeverage := 50.0 // Default leverage for reference
	if optimalLeverage != defaultLeverage {
		// Adjust size inversely to leverage change
		// Higher leverage = smaller size, Lower leverage = larger size
		leverageRatio := defaultLeverage / optimalLeverage
		adjustedSize := size * leverageRatio
		a.logger.Info("Order size adjusted by dynamic leverage",
			zap.String("symbol", symbol),
			zap.Float64("base_size", size),
			zap.Float64("optimal_leverage", optimalLeverage),
			zap.Float64("default_leverage", defaultLeverage),
			zap.Float64("leverage_ratio", leverageRatio),
			zap.Float64("adjusted_size", adjustedSize))
		size = adjustedSize
	}

	return size, nil
}

// AddPriceData adds price data for ATR calculation
func (a *AdaptiveGridManager) AddPriceData(high, low, close float64) {
	if a.riskMonitor != nil {
		a.riskMonitor.AddPriceData(high, low, close)
	}

	// NEW: Feed price data to ATR calculator
	if a.atrCalc != nil {
		a.atrCalc.AddPrice(high, low, close)
	}
}

// UpdatePriceData feeds price data to all calculators (called from WebSocket)
func (a *AdaptiveGridManager) UpdatePriceData(symbol string, high, low, close, bid, ask float64) {
	a.logger.Warn(">>> UpdatePriceData START <<<")

	// Call real-time optimizer if enabled
	a.callRealTimeOptimizer(symbol, high, low, close, bid, ask)

	// Feed ATR calculator
	if a.atrCalc != nil {
		a.atrCalc.AddPrice(high, low, close)
	}
	a.logger.Warn(">>> ATR DONE <<<")

	// Feed RSI calculator
	if a.rsiCalc != nil {
		a.rsiCalc.AddPrice(close)
	}
	a.logger.Warn(">>> RSI DONE <<<")

	// Feed TrendDetector
	if a.trendDetector != nil {
		a.trendDetector.UpdatePrice(close, 0)
		a.logger.Warn(">>> TREND DETECTOR UPDATE DONE <<<")
	}
	a.logger.Warn(">>> TREND DETECTOR DONE <<<")

	// Feed SpreadProtection
	if a.spreadProtection != nil && bid > 0 && ask > 0 {
		a.spreadProtection.UpdateOrderbook(bid, ask)
	}
	a.logger.Warn(">>> SPREAD PROTECTION DONE <<<")

	// Feed DynamicSpreadCalculator
	if a.dynamicSpreadCalc != nil {
		a.dynamicSpreadCalc.UpdateATR(high, low, close)
	}
	a.logger.Warn(">>> DYNAMIC SPREAD DONE <<<")

	// CRITICAL: Feed RangeDetector for breakout detection
	// This enables the "con rắn săn mồi" - patience mechanism + breakout protection
	a.UpdatePriceForRange(symbol, high, low, close)
	a.logger.Warn(">>> RANGE UPDATE DONE <<<")

	// CRITICAL: Feed RegimeDetector for regime detection (ranging/trending/volatile)
	// This enables automatic regime-based parameter switching
	if a.regimeDetector != nil {
		oldRegime := a.GetCurrentRegime(symbol)
		newRegime := a.regimeDetector.DetectRegime(symbol, close)
		if oldRegime != newRegime {
			a.logger.Info("Regime change detected",
				zap.String("symbol", symbol),
				zap.String("from", string(oldRegime)),
				zap.String("to", string(newRegime)))
			a.OnRegimeChange(symbol, oldRegime, newRegime)
		}
	}
	a.logger.Warn(">>> REGIME DETECTION DONE <<<")

	// NEW: Check breakeven exit on price updates
	clusters, err := a.CheckClusterStopLoss(symbol, close)
	if err != nil {
		a.logger.Error("Failed to check breakeven exit",
			zap.String("symbol", symbol),
			zap.Error(err))
	}
	if len(clusters) > 0 {
		a.logger.Info("Breakeven exit triggered",
			zap.String("symbol", symbol),
			zap.Int("clusters", len(clusters)))
		// Trigger exit for symbol
		a.ExitAll(context.Background(), symbol, EventEmergencyExit, "Breakeven exit triggered")
	}

	// NEW: Periodically adapt thresholds using LearningEngine (every 100 klines ~ 100 seconds)
	// This enables continuous learning and adaptation
	if a.learningEngine != nil {
		// Use a simple counter to trigger adaptation periodically
		a.mu.Lock()
		if _, exists := a.klineCounters[symbol]; !exists {
			a.klineCounters[symbol] = 0
		}
		a.klineCounters[symbol]++
		counter := a.klineCounters[symbol]
		a.mu.Unlock()

		// Adapt thresholds every 100 klines
		if counter%100 == 0 {
			// Get recent performance for this symbol
			avgProfit, avgRisk, _, _, count := a.learningEngine.GetPerformance("TRADING", "GRID")
			recentPerformance := avgProfit / avgRisk
			if avgRisk == 0 {
				recentPerformance = 0
			}

			// Adapt each dimension
			positionThreshold := a.learningEngine.AdaptThreshold(symbol, "position", recentPerformance)
			volatilityThreshold := a.learningEngine.AdaptThreshold(symbol, "volatility", recentPerformance)
			riskThreshold := a.learningEngine.AdaptThreshold(symbol, "risk", recentPerformance)
			trendThreshold := a.learningEngine.AdaptThreshold(symbol, "trend", recentPerformance)

			a.logger.Info("LearningEngine adapted thresholds",
				zap.String("symbol", symbol),
				zap.Int("sample_count", count),
				zap.Float64("recent_performance", recentPerformance),
				zap.Float64("position_threshold", positionThreshold),
				zap.Float64("volatility_threshold", volatilityThreshold),
				zap.Float64("risk_threshold", riskThreshold),
				zap.Float64("trend_threshold", trendThreshold))
		}
	}
}

// RecordTradeResult records trade result for loss tracking with cooldown
func (a *AdaptiveGridManager) RecordTradeResult(symbol string, isWin bool) {
	var (
		maxLossesReached bool
		cooldownDuration time.Duration
	)

	a.mu.Lock()

	// Also update risk monitor if available
	if a.riskMonitor != nil {
		// Estimate PnL: win = +1 USDT, loss = -1 USDT (for Kelly calculation)
		pnl := -1.0
		if isWin {
			pnl = 1.0
		}
		a.riskMonitor.RecordTradeResult(symbol, pnl)
	}

	// NEW: Record performance for LearningEngine adaptive learning
	if a.learningEngine != nil {
		// Estimate PnL and risk for learning
		pnl := -1.0
		if isWin {
			pnl = 1.0
		}
		risk := 0.5     // Default risk
		volume := 1.0   // Default volume
		drawdown := 0.0 // Default drawdown

		// Get actual position data if available
		if position, exists := a.positions[symbol]; exists {
			pnl = position.UnrealizedPnL
			risk = position.NotionalValue / a.riskConfig.MaxPositionUSDT
			if risk > 1 {
				risk = 1
			}
		}

		// Record performance with current market condition
		condition := "TRADING"
		if a.stateMachine != nil {
			condition = a.stateMachine.GetState(symbol).String()
		}
		strategy := "GRID"

		a.learningEngine.RecordPerformance(condition, strategy, pnl, risk, volume, drawdown)
		a.logger.Info("LearningEngine recorded performance",
			zap.String("symbol", symbol),
			zap.String("condition", condition),
			zap.String("strategy", strategy),
			zap.Float64("pnl", pnl),
			zap.Float64("risk", risk))
	}

	// Get RiskConfig values (fallback to defaults if not set)
	maxConsecutiveLosses := a.riskConfig.MaxConsecutiveLosses
	if maxConsecutiveLosses <= 0 {
		maxConsecutiveLosses = 3 // Default
	}
	cooldownDuration = a.riskConfig.CooldownDuration
	if cooldownDuration <= 0 {
		cooldownDuration = 5 * time.Minute // Default
	}

	if isWin {
		// Reset consecutive losses on win
		if a.consecutiveLosses[symbol] > 0 {
			a.logger.Info("Win recorded - resetting consecutive losses",
				zap.String("symbol", symbol),
				zap.Int("previous_streak", a.consecutiveLosses[symbol]))
		}
		a.consecutiveLosses[symbol] = 0
		a.cooldownActive[symbol] = false
		if a.stateMachine != nil {
			a.stateMachine.ResetConsecutiveLosses(symbol)
		}
	} else {
		// Increment consecutive losses
		a.consecutiveLosses[symbol]++
		a.lastLossTime[symbol] = time.Now()
		a.totalLossesToday++

		a.logger.Warn("Loss recorded",
			zap.String("symbol", symbol),
			zap.Int("consecutive_losses", a.consecutiveLosses[symbol]),
			zap.Int("total_losses_today", a.totalLossesToday))

		// DISABLED: Check if max consecutive losses reached - don't activate cooldown for volume farming
		// if a.consecutiveLosses[symbol] >= maxConsecutiveLosses {
		// 	a.cooldownActive[symbol] = true
		// 	if a.stateMachine != nil {
		// 		a.stateMachine.RecordConsecutiveLoss(symbol)
		// 	}
		// 	maxLossesReached = true
		// 	a.logger.Error("MAX CONSECUTIVE LOSSES REACHED - Entering cooldown",
		// 		zap.String("symbol", symbol),
		// 		zap.Int("losses", a.consecutiveLosses[symbol]),
		// 		zap.Duration("cooldown", cooldownDuration))
		// }
	}

	a.mu.Unlock()

	if maxLossesReached {
		a.ExitAll(context.Background(), symbol, EventEmergencyExit, "consecutive_loss_limit")
	}
}

// IsInCooldown checks if symbol is in cooldown period
func (a *AdaptiveGridManager) IsInCooldown(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.cooldownActive[symbol] {
		return false
	}

	cooldownDuration := a.riskConfig.CooldownDuration
	if cooldownDuration <= 0 {
		cooldownDuration = 5 * time.Minute // Default
	}
	lastLoss := a.lastLossTime[symbol]
	elapsed := time.Since(lastLoss)

	if elapsed >= cooldownDuration {
		// Cooldown expired - reset
		a.mu.RUnlock()
		a.mu.Lock()
		a.cooldownActive[symbol] = false
		a.consecutiveLosses[symbol] = 0
		a.mu.Unlock()
		a.mu.RLock()

		a.logger.Info("Cooldown expired - resuming trading",
			zap.String("symbol", symbol),
			zap.Duration("elapsed", elapsed))
		return false
	}

	return true
}

// GetExposureStats returns current exposure statistics
func (a *AdaptiveGridManager) GetExposureStats() (totalExposure, maxExposure, utilization float64) {
	if a.riskMonitor == nil {
		return 0, 0, 0
	}
	return a.riskMonitor.GetExposureStats()
}

// InitializeRangeDetectorWithADX initializes range detector with optional ADX filter
func (a *AdaptiveGridManager) InitializeRangeDetectorWithADX(symbol string, config *RangeConfig, enableADX bool, maxADX float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if config == nil {
		config = DefaultRangeConfig()
	}

	detector := NewRangeDetector(config, a.logger)
	detector.SetADXFilter(enableADX, maxADX)

	a.rangeDetectors[symbol] = detector
	a.logger.Info("Range detector initialized with ADX filter",
		zap.String("symbol", symbol),
		zap.Bool("adx_enabled", enableADX),
		zap.Float64("max_adx", maxADX))
}

// InitializeRangeDetector initializes a range detector for a symbol
// Automatically applies FastRange and ADXFilter config from optConfig if available
func (a *AdaptiveGridManager) InitializeRangeDetector(symbol string, config *RangeConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Use default config if not provided
	if config == nil {
		config = DefaultRangeConfig()
	}

	// Create detector
	detector := NewRangeDetector(config, a.logger)

	// Store detector
	a.rangeDetectors[symbol] = detector
	a.logger.Info("RangeDetector initialized for symbol", zap.String("symbol", symbol))

	// CRITICAL: Force state to WAIT_NEW_RANGE on first initialization
	// This treats the bot as "just exited" and ready for reentry, consistent with the trading loop
	if a.stateMachine != nil {
		currentState := a.stateMachine.GetState(symbol)
		if currentState == GridStateIdle {
			a.stateMachine.ForceState(symbol, GridStateWaitNewRange)
			a.logger.Info("State forced to WAIT_NEW_RANGE on initialization (treating as just exited)",
				zap.String("symbol", symbol),
				zap.String("previous_state", currentState.String()))
		}
	}

	a.logger.Info("About to fetch historical klines", zap.String("symbol", symbol))
	a.fetchHistoricalKlinesAndFeedDetector(symbol, detector, 50)
	a.logger.Info("Historical klines fetch completed", zap.String("symbol", symbol))
}

// fetchHistoricalKlinesAndFeedDetector fetches historical kline data and feeds to detector
func (a *AdaptiveGridManager) fetchHistoricalKlinesAndFeedDetector(symbol string, detector *RangeDetector, periods int) {
	a.logger.Info("Fetching historical klines for immediate range establishment",
		zap.String("symbol", symbol),
		zap.Int("periods", periods))

	if a.marketProvider == nil {
		a.logger.Warn("Market provider unavailable for historical klines bootstrap",
			zap.String("symbol", symbol))
		return
	}

	if err := a.marketProvider.EnsureKlineSubscription(symbol, "1m"); err != nil {
		a.logger.Debug("Failed to ensure kline subscription for range detector bootstrap",
			zap.String("symbol", symbol),
			zap.Error(err))
	}

	klines, err := a.marketProvider.GetKlines(context.Background(), symbol, "1m", periods)
	if err != nil {
		a.logger.Error("Failed to fetch historical klines",
			zap.String("symbol", symbol),
			zap.Error(err))
		return
	}

	a.logger.Info("Historical klines fetched successfully",
		zap.String("symbol", symbol),
		zap.Int("count", len(klines)))

	// Feed klines to detector
	for _, kline := range klines {
		detector.AddPrice(kline.High, kline.Low, kline.Close)
	}

	a.logger.Info("Historical klines fed to detector",
		zap.String("symbol", symbol),
		zap.Int("klines_fed", len(klines)))
}

// UpdatePriceForRange updates price data for range detection.
func (a *AdaptiveGridManager) UpdatePriceForRange(symbol string, high, low, closePrice float64) {
	a.mu.RLock()
	detector, exists := a.rangeDetectors[symbol]
	stateMachine := a.stateMachine
	a.mu.RUnlock()
	if !exists {
		return
	}

	a.logger.Warn("UpdatePriceForRange: feeding to detector",
		zap.String("symbol", symbol),
		zap.Float64("high", high),
		zap.Float64("low", low),
		zap.Float64("close", closePrice))

	detector.AddPrice(high, low, closePrice)

	// NEW: Update continuous state every kline
	if stateMachine != nil {
		currentCS := stateMachine.GetContinuousState(symbol)
		config := &ContinuousStateConfig{SmoothingAlpha: 0.3} // Default smoothing

		// Get market data for continuous state calculation
		a.mu.RLock()
		position, hasPosition := a.positions[symbol]
		maxPosition := a.riskConfig.MaxPositionUSDT
		currentRange := detector.GetCurrentRange()
		a.mu.RUnlock()

		// Calculate dimensions
		positionNotional := 0.0
		if hasPosition && position != nil {
			positionNotional = position.NotionalValue
		}

		atrPct := 0.0
		bbWidthPct := 0.0
		if currentRange != nil {
			// Calculate ATR as percentage of price
			if currentRange.MidPrice > 0 {
				atrPct = currentRange.ATR / currentRange.MidPrice
			}
			bbWidthPct = currentRange.WidthPct
		}

		// For now, use placeholder values for PnL, drawdown, ADX, inventory
		// TODO: Integrate with actual PnL tracking, ADX from detector, inventory from InventoryManager
		pnl := 0.0
		drawdown := 0.0
		adx := detector.GetCurrentADX()
		inventory := 0.0

		currentCS.UpdateContinuousState(
			positionNotional, maxPosition,
			atrPct, bbWidthPct,
			pnl, drawdown,
			adx, inventory,
			config, a.logger,
		)

		stateMachine.UpdateContinuousState(symbol, currentCS)

		// Log continuous state
		posSize, vol, risk, trend, skew := currentCS.GetSmoothedState()
		a.logger.Debug("Continuous state updated",
			zap.String("symbol", symbol),
			zap.Float64("position_size", posSize),
			zap.Float64("volatility", vol),
			zap.Float64("risk", risk),
			zap.Float64("trend", trend),
			zap.Float64("skew", skew))
	}

	if currentRange := detector.GetCurrentRange(); currentRange != nil {
		a.UpdateDynamicLeverageMetrics(currentRange.ATR, detector.GetCurrentADX(), currentRange.WidthPct)
	}

	if stateMachine != nil {
		currentState := stateMachine.GetState(symbol)
		a.logger.Warn("State machine transition check",
			zap.String("symbol", symbol),
			zap.String("current_state", currentState.String()))

		// REMOVED detector.ShouldTrade() check to allow auto-reentry after breakout
		// CircuitBreaker is the single source of truth for trading decisions
		switch currentState {
		case GridStateIdle:
			readyForRegrid := a.isReadyForRegrid(symbol)
			canTransition := stateMachine.CanTransition(symbol, EventRangeConfirmed)
			a.logger.Warn("IDLE -> ENTER_GRID transition check",
				zap.String("symbol", symbol),
				zap.Bool("ready_for_regrid", readyForRegrid),
				zap.Bool("can_transition", canTransition))
			if readyForRegrid && canTransition {
				ok := stateMachine.Transition(symbol, EventRangeConfirmed)
				a.logger.Warn("IDLE -> ENTER_GRID transition result",
					zap.String("symbol", symbol),
					zap.Bool("success", ok))
				if ok {
					// Record ENTER_GRID transition time for breakout cooldown
					a.enterGridTime[symbol] = time.Now()
					a.logger.Warn("ENTER_GRID transition time recorded",
						zap.String("symbol", symbol),
						zap.Time("transition_time", a.enterGridTime[symbol]))
				}
			}
		case GridStateWaitNewRange:
			readyForRegrid := a.isReadyForRegrid(symbol)
			canTransition := stateMachine.CanTransition(symbol, EventNewRangeReady)
			a.logger.Warn("WAIT_NEW_RANGE -> ENTER_GRID transition check",
				zap.String("symbol", symbol),
				zap.Bool("ready_for_regrid", readyForRegrid),
				zap.Bool("can_transition", canTransition))
			if readyForRegrid && canTransition {
				ok := stateMachine.Transition(symbol, EventNewRangeReady)
				// Removed ClearRegridCooldown - cooldown disabled
				a.logger.Warn("WAIT_NEW_RANGE -> ENTER_GRID transition result",
					zap.String("symbol", symbol),
					zap.Bool("success", ok))
				if ok {
					// Record ENTER_GRID transition time for breakout cooldown
					a.enterGridTime[symbol] = time.Now()
					a.logger.Warn("ENTER_GRID transition time recorded",
						zap.String("symbol", symbol),
						zap.Time("transition_time", a.enterGridTime[symbol]))
				}
			}
		case GridStateEnterGrid:
			// No special handling - transition happens via order placement in grid_manager
			// Root cause fix: canPlaceForSymbol bypasses gates when stuck > 30s
		case GridStateExitAll:
			// FAIL-SAFE TIMEOUT: Force transition after 15s regardless of position status
			// This ensures bot never gets stuck in EXIT_ALL state
			stateTime := stateMachine.GetStateTime(symbol)
			timeInState := time.Since(stateTime)
			timeout := 15 * time.Second

			// Check if positions are zero to auto-transition to WAIT_NEW_RANGE
			position, hasPosition := a.positions[symbol]
			positionZero := !hasPosition || position == nil || math.Abs(position.PositionAmt) == 0

			if positionZero {
				canTransition := stateMachine.CanTransition(symbol, EventPositionsClosed)
				a.logger.Info("EXIT_ALL -> WAIT_NEW_RANGE transition check",
					zap.String("symbol", symbol),
					zap.Bool("position_zero", positionZero),
					zap.Bool("can_transition", canTransition))
				if canTransition {
					ok := stateMachine.Transition(symbol, EventPositionsClosed)
					a.logger.Info("EXIT_ALL -> WAIT_NEW_RANGE transition result",
						zap.String("symbol", symbol),
						zap.Bool("success", ok))
				}
			} else if timeInState > timeout {
				// FAIL-SAFE: Force transition even if position not zero
				// This prevents bot from getting stuck indefinitely
				a.logger.Error("EXIT_ALL FAIL-SAFE: Timeout exceeded, forcing transition to WAIT_NEW_RANGE",
					zap.String("symbol", symbol),
					zap.Duration("time_in_state", timeInState),
					zap.Duration("timeout", timeout),
					zap.Float64("position_amt", position.PositionAmt),
					zap.String("reason", "Prevent indefinite blocking"))

				canTransition := stateMachine.CanTransition(symbol, EventPositionsClosed)
				if canTransition {
					ok := stateMachine.Transition(symbol, EventPositionsClosed)
					a.logger.Info("EXIT_ALL FAIL-SAFE: Forced transition result",
						zap.String("symbol", symbol),
						zap.Bool("success", ok))
					if ok {
						// Update position cache to zero to match state
						a.UpdatePositionTracking(symbol, &client.Position{
							Symbol:      symbol,
							PositionAmt: 0,
							EntryPrice:  0,
							MarkPrice:   0,
						})
					}
				}
			} else {
				// Check if stuck in EXIT_ALL for too long (> 10s) with positions
				if timeInState > 10*time.Second {
					a.logger.Warn("EXIT_ALL approaching timeout, forcing close",
						zap.String("symbol", symbol),
						zap.Duration("time_in_state", timeInState),
						zap.Duration("timeout", timeout),
						zap.Float64("position_amt", position.PositionAmt))
					// Force close position
					a.emergencyClosePosition(context.Background(), symbol, position.PositionAmt)
				} else {
					a.logger.Debug("EXIT_ALL: positions not zero yet, waiting",
						zap.String("symbol", symbol),
						zap.Float64("position_amt", position.PositionAmt),
						zap.Duration("time_in_state", timeInState))
				}
			}
		case GridStateOverSize:
			// Auto-transition to TRADING when position size normalizes
			position, hasPosition := a.positions[symbol]
			if hasPosition && position != nil {
				// Use hardcoded thresholds: 80% to enter, 60% to exit
				recoveryLevel := a.riskConfig.MaxPositionUSDT * 0.6
				currentNotional := position.NotionalValue

				if currentNotional <= recoveryLevel {
					canTransition := stateMachine.CanTransition(symbol, EventSizeNormalized)
					a.logger.Info("OVER_SIZE -> TRADING transition check",
						zap.String("symbol", symbol),
						zap.Float64("current_notional", currentNotional),
						zap.Float64("recovery_level", recoveryLevel),
						zap.Bool("can_transition", canTransition))
					if canTransition {
						ok := stateMachine.Transition(symbol, EventSizeNormalized)
						a.logger.Info("OVER_SIZE -> TRADING transition result",
							zap.String("symbol", symbol),
							zap.Bool("success", ok))
					}
				}
			}
		case GridStateDefensive:
			// Auto-transition to TRADING when volatility normalizes
			// Check if BB width and ADX have normalized
			if detector != nil {
				currentRange := detector.GetCurrentRange()
				if currentRange != nil {
					// Volatility normalized if BB width < 5% and ADX < 50
					volatilityNormalized := currentRange.WidthPct < 0.05
					adxNormalized := detector.GetCurrentADX() < 50.0

					if volatilityNormalized && adxNormalized {
						canTransition := stateMachine.CanTransition(symbol, EventVolatilityNormalized)
						a.logger.Info("DEFENSIVE -> TRADING transition check",
							zap.String("symbol", symbol),
							zap.Float64("bb_width", currentRange.WidthPct),
							zap.Float64("bb_threshold", 0.05),
							zap.Float64("adx", detector.GetCurrentADX()),
							zap.Bool("volatility_normalized", volatilityNormalized),
							zap.Bool("adx_normalized", adxNormalized),
							zap.Bool("can_transition", canTransition))
						if canTransition {
							ok := stateMachine.Transition(symbol, EventVolatilityNormalized)
							a.logger.Info("DEFENSIVE -> TRADING transition result",
								zap.String("symbol", symbol),
								zap.Bool("success", ok))
						}
					}
				}
			}
		case GridStateRecovery:
			// Auto-transition to TRADING when recovery conditions met
			position, hasPosition := a.positions[symbol]
			if hasPosition && position != nil {
				// Recovery complete if PnL >= 0 (break even) and stable for 30 minutes
				pnlRecovery := position.UnrealizedPnL >= 0
				stateTime := stateMachine.GetStateTime(symbol)
				stableDuration := time.Since(stateTime)
				stableEnough := stableDuration >= 30*time.Minute

				if pnlRecovery && stableEnough {
					canTransition := stateMachine.CanTransition(symbol, EventRecoveryComplete)
					a.logger.Info("RECOVERY -> TRADING transition check",
						zap.String("symbol", symbol),
						zap.Float64("unrealized_pnl", position.UnrealizedPnL),
						zap.Float64("recovery_threshold", 0),
						zap.Duration("stable_duration", stableDuration),
						zap.Int("min_stable_minutes", 30),
						zap.Bool("pnl_recovery", pnlRecovery),
						zap.Bool("stable_enough", stableEnough),
						zap.Bool("can_transition", canTransition))
					if canTransition {
						ok := stateMachine.Transition(symbol, EventRecoveryComplete)
						a.logger.Info("RECOVERY -> TRADING transition result",
							zap.String("symbol", symbol),
							zap.Bool("success", ok))
					}
				}
			}
		case GridStateExitHalf:
			// Auto-transition to TRADING when recovery conditions met
			position, hasPosition := a.positions[symbol]
			if hasPosition && position != nil {
				// Recovery if PnL >= 0 (break even)
				pnlRecovery := position.UnrealizedPnL >= 0

				if pnlRecovery {
					canTransition := stateMachine.CanTransition(symbol, EventRecovery)
					a.logger.Info("EXIT_HALF -> TRADING transition check",
						zap.String("symbol", symbol),
						zap.Float64("unrealized_pnl", position.UnrealizedPnL),
						zap.Float64("recovery_threshold", 0),
						zap.Bool("pnl_recovery", pnlRecovery),
						zap.Bool("can_transition", canTransition))
					if canTransition {
						ok := stateMachine.Transition(symbol, EventRecovery)
						a.logger.Info("EXIT_HALF -> TRADING transition result",
							zap.String("symbol", symbol),
							zap.Bool("success", ok))
					}
				}
			}
		}
	}

	// Check breakout with cooldown after ENTER_GRID transition
	// Avoid immediate breakout detection right after entering ENTER_GRID state
	if detector.IsBreakout() {
		enterTime, hasEnterTime := a.enterGridTime[symbol]
		timeSinceEnter := time.Since(enterTime)
		cooldown := 120 * time.Second // 2 minute cooldown after ENTER_GRID (tăng từ 30s để tránh false breakout)

		if hasEnterTime && timeSinceEnter < cooldown {
			a.logger.Info("Breakout detected but in cooldown period, skipping",
				zap.String("symbol", symbol),
				zap.Duration("time_since_enter_grid", timeSinceEnter),
				zap.Duration("cooldown", cooldown))
			return
		}

		// NEW: ADX filter - chỉ breakout khi có trend mạnh (ADX > 25)
		// Tránh false breakout trong sideways market
		currentADX := detector.GetCurrentADX()
		adxThreshold := 25.0 // ADX > 25 = trend mạnh (tương ứng với DefaultRangeConfig)
		if currentADX < adxThreshold && currentADX > 0 {
			a.logger.Info("Breakout detected but ADX too low (sideways market), skipping",
				zap.String("symbol", symbol),
				zap.Float64("current_adx", currentADX),
				zap.Float64("adx_threshold", adxThreshold),
				zap.Duration("time_since_enter_grid", timeSinceEnter))
			return
		}

		a.logger.Info("Breakout detected with strong trend, triggering exit",
			zap.String("symbol", symbol),
			zap.Float64("adx", currentADX),
			zap.Float64("adx_threshold", adxThreshold),
			zap.Duration("time_since_enter_grid", timeSinceEnter))
		a.handleBreakout(context.Background(), symbol, closePrice)
		return
	}

	// Strong trend is now governed by ADX/range-state, not RSI-first.
	if detector.ShouldExitForTrend() {
		// NEW: ADX filter - chỉ exit cho strong trend khi ADX > 25 (trend mạnh)
		currentADX := detector.GetCurrentADX()
		adxThreshold := 25.0
		if currentADX < adxThreshold && currentADX > 0 {
			a.logger.Info("Strong trend detected but ADX too low (sideways market), skipping exit",
				zap.String("symbol", symbol),
				zap.Float64("current_adx", currentADX),
				zap.Float64("adx_threshold", adxThreshold))
			return
		}

		state := TrendStateNeutral
		if a.trendDetector != nil {
			state = a.trendDetector.GetTrendState()
		}
		a.handleStrongTrend(context.Background(), symbol, closePrice, state)
	}
}

// handleStrongTrend handles strong trend detection - STRICT: đóng lệnh như breakout
func (a *AdaptiveGridManager) handleStrongTrend(ctx context.Context, symbol string, currentPrice float64, state TrendState) {
	trendScore := 0
	if a.trendDetector != nil {
		trendScore = a.trendDetector.GetTrendScore()
	}
	a.logger.Error("STRONG TREND DETECTED - Closing ALL orders and positions for this symbol",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
		zap.String("trend_state", state.String()),
		zap.Int("trend_score", trendScore))
	a.ExitAll(ctx, symbol, EventTrendExit, "strong_trend")
}

// handleBreakout handles breakout detection - STRICT risk management
// Khi breakout: đóng TẤT CẢ lệnh và position CỦA SYMBOL ĐÓ, sau đó chờ BB tạo range mới
func (a *AdaptiveGridManager) handleBreakout(ctx context.Context, symbol string, currentPrice float64) {
	a.logger.Error("BREAKOUT DETECTED - Closing ALL orders and positions for this symbol",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice))

	// NEW: Use ExitExecutor for fast exit if available
	if a.exitExecutor != nil {
		// Type assertion to call ExecuteFastExit
		if executor, ok := a.exitExecutor.(interface {
			ExecuteFastExit(ctx context.Context, symbol string) interface{}
		}); ok {
			_ = executor.ExecuteFastExit(ctx, symbol)
			a.logger.Info("Fast exit executed via ExitExecutor",
				zap.String("symbol", symbol))
			return
		}
	}

	// Fallback to standard ExitAll
	a.ExitAll(ctx, symbol, EventEmergencyExit, "breakout")
}

// GetRangeState returns current range state for a symbol
func (a *AdaptiveGridManager) GetRangeState(symbol string) RangeState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return RangeStateUnknown
	}
	return detector.GetState()
}

func (a *AdaptiveGridManager) markExitAll(symbol string, event GridEvent) {
	a.mu.RLock()
	stateMachine := a.stateMachine
	a.mu.RUnlock()
	if stateMachine == nil {
		return
	}

	switch stateMachine.GetState(symbol) {
	case GridStateTrading:
		if stateMachine.CanTransition(symbol, event) {
			stateMachine.Transition(symbol, event)
		}
	case GridStateEnterGrid, GridStateIdle:
		stateMachine.ForceState(symbol, GridStateExitAll)
	}
}

func (a *AdaptiveGridManager) markExitCompleted(symbol string) {
	a.mu.RLock()
	stateMachine := a.stateMachine
	a.mu.RUnlock()
	if stateMachine == nil {
		return
	}

	if stateMachine.GetState(symbol) == GridStateExitAll && stateMachine.CanTransition(symbol, EventPositionsClosed) {
		stateMachine.Transition(symbol, EventPositionsClosed)
		// Removed regrid cooldown - only rely on market conditions
		return
	}
	stateMachine.ForceState(symbol, GridStateWaitNewRange)
	// Removed regrid cooldown - only rely on market conditions
}

// ExitAll is the single idempotent runtime path for flattening a symbol and moving to WAIT_NEW_RANGE.
func (a *AdaptiveGridManager) ExitAll(ctx context.Context, symbol string, event GridEvent, reason string) {
	a.logger.Warn("Executing EXIT_ALL",
		zap.String("symbol", symbol),
		zap.String("reason", reason),
		zap.String("event", event.String()))

	// CRITICAL: Pause trading IMMEDIATELY to prevent grid manager from placing orders during exit
	a.pauseTrading(symbol)

	a.markExitAll(symbol, event)

	if a.gridManager != nil {
		if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
			a.logger.Error("Failed to cancel orders during EXIT_ALL",
				zap.String("symbol", symbol),
				zap.String("reason", reason),
				zap.Error(err))
		}
	}

	var positionAmt float64
	if a.positionProvider != nil {
		positions, err := a.positionProvider.GetCachedPositions(ctx)
		if err != nil {
			a.logger.Error("Failed to read cached positions during EXIT_ALL",
				zap.String("symbol", symbol),
				zap.Error(err))
		} else {
			for _, pos := range positions {
				if pos.Symbol == symbol && pos.PositionAmt != 0 {
					positionAmt = pos.PositionAmt
					a.logger.Warn("Found cached position during EXIT_ALL",
						zap.String("symbol", symbol),
						zap.Float64("position_amt", positionAmt))
					break
				}
			}
		}
	}

	if positionAmt == 0 {
		a.mu.RLock()
		position, hasPosition := a.positions[symbol]
		a.mu.RUnlock()
		if hasPosition && position != nil && position.PositionAmt != 0 {
			positionAmt = position.PositionAmt
			a.logger.Warn("Found position in cache during EXIT_ALL",
				zap.String("symbol", symbol),
				zap.Float64("position_amt", positionAmt))
		}
	}

	if positionAmt != 0 {
		a.logger.Warn("Closing position during EXIT_ALL",
			zap.String("symbol", symbol),
			zap.String("reason", reason),
			zap.Float64("position_amt", positionAmt))
		a.emergencyClosePosition(ctx, symbol, positionAmt)
	} else {
		if a.gridManager != nil {
			if err := a.gridManager.ClearGrid(ctx, symbol); err != nil {
				a.logger.Error("Failed to clear grid during EXIT_ALL",
					zap.String("symbol", symbol),
					zap.String("reason", reason),
					zap.Error(err))
			}
		}
		a.markExitCompleted(symbol)
	}

	a.mu.RLock()
	detector, hasDetector := a.rangeDetectors[symbol]
	a.mu.RUnlock()

	if hasDetector {
		detector.ForceRecalculate()
		a.logger.Info("Range detector reset after EXIT_ALL",
			zap.String("symbol", symbol),
			zap.String("reason", reason))
	}
}

// GetRangeInfo returns range information for a symbol
func (a *AdaptiveGridManager) GetRangeInfo(symbol string) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return map[string]interface{}{"error": "no range detector"}
	}
	return detector.GetRangeInfo()
}

// ShouldTradeInRange returns true if trading is allowed in current range
func (a *AdaptiveGridManager) ShouldTradeInRange(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return true // Default allow if no detector
	}

	// Only trade when range is active
	return detector.ShouldTrade()
}

// GetRangeBasedGridParams returns grid parameters based on current range
func (a *AdaptiveGridManager) GetRangeBasedGridParams(symbol string) (spreadPct float64, levels int, valid bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return 0, 0, false
	}
	return detector.GetGridParameters()
}

// GetBBRangeBands returns the Bollinger Bands upper and lower bounds for grid placement
// This allows grid to be placed exactly at BB bands for optimal range trading
func (a *AdaptiveGridManager) GetBBRangeBands(symbol string) (upper float64, lower float64, mid float64, valid bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return 0, 0, 0, false
	}

	rangeData := detector.GetCurrentRange()
	if rangeData == nil {
		return 0, 0, 0, false
	}

	// Only return valid if range is active
	if detector.GetState() != RangeStateActive {
		return 0, 0, 0, false
	}

	return rangeData.UpperBound, rangeData.LowerBound, rangeData.MidPrice, true
}

// InitializeVolumeScaler creates a volume scaler for a symbol
func (a *AdaptiveGridManager) InitializeVolumeScaler(symbol string, config *VolumeScalingConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if config == nil {
		config = DefaultVolumeScalingConfig()
	}

	a.volumeScalers[symbol] = NewVolumeScaler(config, a.logger)
	a.logger.Info("VolumeScaler initialized for symbol",
		zap.String("symbol", symbol),
		zap.Float64("center_notional", config.CenterNotional),
		zap.Float64("edge_notional", config.EdgeNotional),
		zap.String("curve", config.CurveType))
}

// UpdateVolumeScalerRange updates range for volume scaler
func (a *AdaptiveGridManager) UpdateVolumeScalerRange(symbol string, rangeData *RangeData) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	scaler, exists := a.volumeScalers[symbol]
	if !exists {
		// Auto-initialize
		a.mu.RUnlock()
		a.InitializeVolumeScaler(symbol, nil)
		a.mu.RLock()
		scaler = a.volumeScalers[symbol]
	}

	scaler.UpdateRange(rangeData)
}

// GetTaperedOrderSize returns order size with pyramid/tapered scaling
func (a *AdaptiveGridManager) GetTaperedOrderSize(symbol string, price float64, isBuy bool) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	scaler, exists := a.volumeScalers[symbol]
	if !exists {
		// Fallback to base notional
		return 100.0
	}

	return scaler.CalculateOrderSize(price, isBuy)
}

// GetGridSizesWithTapering returns all grid level sizes with tapering
func (a *AdaptiveGridManager) GetGridSizesWithTapering(symbol string, currentPrice float64, numLevels int, spreadPct float64) []GridLevelSize {
	a.mu.RLock()
	defer a.mu.RUnlock()

	scaler, exists := a.volumeScalers[symbol]
	if !exists {
		return nil
	}

	return scaler.CalculateAllGridLevels(currentPrice, numLevels, spreadPct)
}

// GetVolumeScalerInfo returns volume scaler info for a symbol
func (a *AdaptiveGridManager) GetVolumeScalerInfo(symbol string, price float64) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	scaler, exists := a.volumeScalers[symbol]
	if !exists {
		return map[string]interface{}{"error": "no volume scaler"}
	}

	info := scaler.GetZoneInfo(price)
	info["tapered_size"] = scaler.CalculateOrderSize(price, true)
	return info
}

// GetDynamicSpread returns the calculated dynamic spread percentage with time-based adjustment
func (a *AdaptiveGridManager) GetDynamicSpread() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	baseSpread := 0.0015 // Default 0.15% spread for volume farming
	if a.optConfig != nil && a.optConfig.DynamicGrid != nil && a.optConfig.DynamicGrid.BaseSpreadPct > 0 {
		baseSpread = a.optConfig.DynamicGrid.BaseSpreadPct
	} else if a.dynamicSpreadCalc != nil {
		baseSpread = a.dynamicSpreadCalc.GetDynamicSpread()
	} else if a.riskConfig != nil && a.riskConfig.BaseGridSpreadPct > 0 {
		baseSpread = a.riskConfig.BaseGridSpreadPct
	}

	// Apply time-based spread multiplier
	spreadMultiplier := a.GetTimeBasedSpreadMultiplier()
	if spreadMultiplier > 0 && spreadMultiplier != 1.0 {
		adjustedSpread := baseSpread * spreadMultiplier
		a.logger.Debug("Spread adjusted by time filter",
			zap.Float64("base_spread", baseSpread),
			zap.Float64("multiplier", spreadMultiplier),
			zap.Float64("adjusted_spread", adjustedSpread))
		return adjustedSpread
	}

	return baseSpread
}

// GetDynamicSpreadMultiplier returns current spread multiplier based on volatility
func (a *AdaptiveGridManager) GetDynamicSpreadMultiplier() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.dynamicSpreadCalc == nil {
		return 1.0 // Default multiplier
	}
	return a.dynamicSpreadCalc.GetMultiplier()
}

// GetInventoryAdjustedSize returns order size adjusted for inventory skew
func (a *AdaptiveGridManager) GetInventoryAdjustedSize(symbol string, side string, baseSize float64) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.inventoryMgr == nil {
		return baseSize
	}
	return a.inventoryMgr.GetAdjustedOrderSize(symbol, side, baseSize)
}

// ShouldPauseInventorySide returns true if side should be paused due to inventory skew
func (a *AdaptiveGridManager) ShouldPauseInventorySide(symbol string, side string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.inventoryMgr == nil {
		return false
	}
	return a.inventoryMgr.ShouldPauseSide(symbol, side)
}

// GetInventoryStatus returns current inventory status for a symbol
func (a *AdaptiveGridManager) GetInventoryStatus(symbol string) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.inventoryMgr == nil {
		return map[string]interface{}{"error": "inventory manager not initialized"}
	}
	return a.inventoryMgr.GetStatus(symbol)
}

// TrackInventoryPosition tracks a new position for inventory management
func (a *AdaptiveGridManager) TrackInventoryPosition(symbol, side string, size, entryPrice float64, gridLevel int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.inventoryMgr != nil {
		a.inventoryMgr.TrackPosition(symbol, side, size, entryPrice, gridLevel)
	}
}

// CloseInventoryPosition removes a position from inventory tracking
func (a *AdaptiveGridManager) CloseInventoryPosition(symbol string, gridLevel int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.inventoryMgr == nil {
		return fmt.Errorf("inventory manager not initialized")
	}
	return a.inventoryMgr.ClosePosition(symbol, gridLevel)
}

// UpdateInventoryPositionPrice updates position prices for PnL calculation
func (a *AdaptiveGridManager) UpdateInventoryPositionPrice(symbol string, currentPrice float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.inventoryMgr != nil {
		a.inventoryMgr.UpdatePositionPrice(symbol, currentPrice)
	}
}

// CheckClusterStopLoss checks and handles cluster stop-loss conditions
func (a *AdaptiveGridManager) CheckClusterStopLoss(symbol string, currentPrice float64) ([]Cluster, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.clusterMgr == nil {
		return nil, fmt.Errorf("cluster manager not initialized")
	}

	// Check time-based stop-loss
	updatedClusters := a.clusterMgr.CheckTimeBasedStopLoss(symbol, currentPrice)

	// Check breakeven exits
	exitClusters := a.clusterMgr.CheckBreakevenExit(symbol, currentPrice)

	// Combine results
	allClusters := append(updatedClusters, exitClusters...)
	return allClusters, nil
}

// GetClusterHeatMap returns cluster heat map for a symbol
func (a *AdaptiveGridManager) GetClusterHeatMap(symbol string) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.clusterMgr == nil {
		return map[string]interface{}{"error": "cluster manager not initialized"}
	}
	return a.clusterMgr.GenerateClusterHeatMap(symbol)
}

// TrackClusterEntry tracks a new cluster entry for stop-loss tracking
func (a *AdaptiveGridManager) TrackClusterEntry(symbol string, level int, side string, positions []PositionInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.clusterMgr != nil {
		a.clusterMgr.TrackClusterEntry(symbol, level, side, positions)
	}
}

// =============================================================================
// PARTIAL CLOSE STRATEGY (T024-T028)
// =============================================================================

// InitializePartialClose initializes position slices for partial TP strategy
func (a *AdaptiveGridManager) InitializePartialClose(symbol string, positionAmt, entryPrice float64) {
	if a.partialCloseConfig == nil || !a.partialCloseConfig.Enabled {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.positionSlices == nil {
		a.positionSlices = make(map[string]*PositionSlice)
	}

	side := "LONG"
	if positionAmt < 0 {
		side = "SHORT"
	}

	slice := &PositionSlice{
		Symbol:        symbol,
		OriginalSize:  math.Abs(positionAmt),
		RemainingSize: math.Abs(positionAmt),
		ClosedPct:     0,
		EntryPrice:    entryPrice,
		Side:          side,
		TPLevels: []TPLevel{
			{TargetPct: a.partialCloseConfig.TP1_ProfitPct, ClosePct: a.partialCloseConfig.TP1_ClosePct},
			{TargetPct: a.partialCloseConfig.TP2_ProfitPct, ClosePct: a.partialCloseConfig.TP2_ClosePct},
			{TargetPct: a.partialCloseConfig.TP3_ProfitPct, ClosePct: a.partialCloseConfig.TP3_ClosePct},
		},
		TrailingActive: false,
		CreatedAt:      time.Now(),
	}

	a.positionSlices[symbol] = slice
	a.logger.Info("Partial close initialized",
		zap.String("symbol", symbol),
		zap.Float64("size", slice.OriginalSize),
		zap.Float64("entry", entryPrice),
		zap.String("side", side))
}

// CheckPartialTakeProfits checks and executes partial take profits
func (a *AdaptiveGridManager) CheckPartialTakeProfits(ctx context.Context, symbol string, currentPrice float64) (closed bool, err error) {
	if a.partialCloseConfig == nil || !a.partialCloseConfig.Enabled {
		return false, nil
	}

	a.mu.Lock()
	slice, exists := a.positionSlices[symbol]
	a.mu.Unlock()

	if !exists || slice == nil {
		return false, nil
	}

	// Calculate current profit %
	var profitPct float64
	if slice.Side == "LONG" {
		profitPct = (currentPrice - slice.EntryPrice) / slice.EntryPrice
	} else {
		profitPct = (slice.EntryPrice - currentPrice) / slice.EntryPrice
	}

	// Check each TP level
	for i := range slice.TPLevels {
		level := &slice.TPLevels[i]
		if level.IsHit {
			continue
		}

		if profitPct >= level.TargetPct {
			// Execute partial close
			qty := slice.RemainingSize * level.ClosePct
			err := a.closePositionPartial(ctx, symbol, qty, i+1)
			if err != nil {
				a.logger.Error("Failed partial close",
					zap.String("symbol", symbol),
					zap.Int("tp_level", i+1),
					zap.Error(err))
				return false, err
			}

			a.mu.Lock()
			level.IsHit = true
			level.ExecutedQty = qty
			slice.RemainingSize -= qty
			slice.ClosedPct += level.ClosePct

			// Activate trailing stop after TP2 if enabled
			if i == 1 && a.partialCloseConfig.TrailingAfterTP2 {
				slice.TrailingActive = true
				if slice.Side == "LONG" {
					slice.TrailingPrice = currentPrice * (1 - a.partialCloseConfig.TrailingDistance)
				} else {
					slice.TrailingPrice = currentPrice * (1 + a.partialCloseConfig.TrailingDistance)
				}
			}
			a.mu.Unlock()

			a.logger.Info("Partial TP executed",
				zap.String("symbol", symbol),
				zap.Int("tp_level", i+1),
				zap.Float64("profit_pct", profitPct*100),
				zap.Float64("qty", qty),
				zap.Float64("remaining", slice.RemainingSize))

			return true, nil
		}
	}

	// Check trailing stop if active
	if slice.TrailingActive && slice.RemainingSize > 0 {
		hitTrailing := false
		if slice.Side == "LONG" && currentPrice <= slice.TrailingPrice {
			hitTrailing = true
		} else if slice.Side == "SHORT" && currentPrice >= slice.TrailingPrice {
			hitTrailing = true
		}

		if hitTrailing {
			err := a.closePositionPartial(ctx, symbol, slice.RemainingSize, 0)
			if err != nil {
				return false, err
			}

			a.mu.Lock()
			slice.RemainingSize = 0
			a.mu.Unlock()

			a.logger.Info("Trailing stop executed",
				zap.String("symbol", symbol),
				zap.Float64("price", currentPrice))

			return true, nil
		}

		// Update trailing price if moving in favorable direction
		a.mu.Lock()
		if slice.Side == "LONG" && currentPrice > slice.TrailingPrice {
			newTrailing := currentPrice * (1 - a.partialCloseConfig.TrailingDistance)
			if newTrailing > slice.TrailingPrice {
				slice.TrailingPrice = newTrailing
			}
		} else if slice.Side == "SHORT" && currentPrice < slice.TrailingPrice {
			newTrailing := currentPrice * (1 + a.partialCloseConfig.TrailingDistance)
			if newTrailing < slice.TrailingPrice {
				slice.TrailingPrice = newTrailing
			}
		}
		a.mu.Unlock()
	}

	return false, nil
}

// closePositionPartial closes a partial position
func (a *AdaptiveGridManager) closePositionPartial(ctx context.Context, symbol string, qty float64, tpLevel int) error {
	if a.futuresClient == nil || qty <= 0 {
		return nil
	}

	slice := a.positionSlices[symbol]
	if slice == nil {
		return fmt.Errorf("no position slice found for %s", symbol)
	}

	side := "SELL"
	if slice.Side == "SHORT" {
		side = "BUY"
	}

	qtyStr := fmt.Sprintf("%.6f", qty)

	orderReq := client.PlaceOrderRequest{
		Symbol:     symbol,
		Side:       side,
		Type:       "MARKET",
		Quantity:   qtyStr,
		ReduceOnly: true,
	}

	levelStr := "trailing"
	if tpLevel > 0 {
		levelStr = fmt.Sprintf("TP%d", tpLevel)
	}

	a.logger.Info("Partial close order",
		zap.String("symbol", symbol),
		zap.String("level", levelStr),
		zap.String("side", side),
		zap.String("qty", qtyStr))

	_, err := a.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		return err
	}

	return nil
}

// SetPartialCloseConfig sets the partial close configuration
func (a *AdaptiveGridManager) SetPartialCloseConfig(config *PartialCloseConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.partialCloseConfig = config
}

// GetPartialCloseStatus returns current partial close status
func (a *AdaptiveGridManager) GetPartialCloseStatus(symbol string) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.partialCloseConfig == nil || !a.partialCloseConfig.Enabled {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	slice, exists := a.positionSlices[symbol]
	if !exists || slice == nil {
		return map[string]interface{}{
			"enabled": true,
			"active":  false,
		}
	}

	return map[string]interface{}{
		"enabled":         true,
		"active":          true,
		"original_size":   slice.OriginalSize,
		"remaining_size":  slice.RemainingSize,
		"closed_pct":      slice.ClosedPct,
		"entry_price":     slice.EntryPrice,
		"side":            slice.Side,
		"tp_levels_hit":   []bool{slice.TPLevels[0].IsHit, slice.TPLevels[1].IsHit, slice.TPLevels[2].IsHit},
		"trailing_active": slice.TrailingActive,
		"trailing_price":  slice.TrailingPrice,
	}
}

// =============================================================================
// MULTI-LAYER LIQUIDATION PROTECTION (T040-T046)
// =============================================================================

// MultiLayerLiquidationStatus tracks current liquidation tier status
type MultiLayerLiquidationStatus struct {
	CurrentTier   int       `json:"current_tier"`
	DistanceToLiq float64   `json:"distance_to_liq_pct"`
	LastCheckTime time.Time `json:"last_check_time"`
	ActionsTaken  []string  `json:"actions_taken"`
}

// multiLayerStatus tracks per-symbol liquidation tier status
var multiLayerStatus = make(map[string]*MultiLayerLiquidationStatus)

// checkMultiLayerLiquidation checks and handles multi-tier liquidation protection
// Automatically applies config from optConfig.MultiLayerLiq if available
func (a *AdaptiveGridManager) checkMultiLayerLiquidation(ctx context.Context, symbol string, markPrice, liqPrice, positionAmt float64) {
	// Calculate distance to liquidation
	distanceToLiq := math.Abs(markPrice - liqPrice)
	distancePct := distanceToLiq / markPrice

	// Load config from optConfig if available, otherwise use default
	config := DefaultMultiLayerLiquidationConfig()
	if a.optConfig != nil && a.optConfig.MultiLayerLiq != nil {
		optML := a.optConfig.MultiLayerLiq
		config.Enabled = optML.Enabled
		config.Layer1WarnPct = optML.Layer1WarnPct
		config.Layer2ReducePct = optML.Layer2ReducePct
		config.Layer3ClosePct = optML.Layer3ClosePct
		config.Layer4HedgePct = optML.Layer4HedgePct
		config.ReducePositionBy = optML.ReducePositionBy
	}

	if !config.Enabled {
		// Fall back to original single-layer check
		if a.isNearLiquidation(symbol, markPrice, liqPrice, positionAmt) {
			a.emergencyClosePosition(ctx, symbol, positionAmt)
		}
		return
	}

	// Initialize status if not exists
	if multiLayerStatus[symbol] == nil {
		multiLayerStatus[symbol] = &MultiLayerLiquidationStatus{
			CurrentTier:   0,
			ActionsTaken:  []string{},
			LastCheckTime: time.Now(),
		}
	}
	status := multiLayerStatus[symbol]
	status.DistanceToLiq = distancePct
	status.LastCheckTime = time.Now()

	// Tier 4: 10% distance - Emergency hedge + close ALL (CRITICAL)
	if distancePct < config.Layer4HedgePct {
		if status.CurrentTier < 4 {
			status.CurrentTier = 4
			status.ActionsTaken = append(status.ActionsTaken, "TIER4_EMERGENCY_HEDGE")
			a.logger.Error("LIQUIDATION TIER 4: Emergency hedge + close ALL!",
				zap.String("symbol", symbol),
				zap.Float64("distance_pct", distancePct*100),
				zap.Float64("mark_price", markPrice),
				zap.Float64("liq_price", liqPrice))
			a.emergencyHedgeAndClose(ctx, symbol, positionAmt)
		}
		return
	}

	// Tier 3: 15% distance - Close 100% position
	if distancePct < config.Layer3ClosePct {
		if status.CurrentTier < 3 {
			status.CurrentTier = 3
			status.ActionsTaken = append(status.ActionsTaken, "TIER3_CLOSE_ALL")
			a.logger.Error("LIQUIDATION TIER 3: Closing ALL position!",
				zap.String("symbol", symbol),
				zap.Float64("distance_pct", distancePct*100),
				zap.Float64("mark_price", markPrice),
				zap.Float64("liq_price", liqPrice))
			a.emergencyClosePosition(ctx, symbol, positionAmt)
		}
		return
	}

	// Tier 2: 30% distance - Reduce 50% position
	if distancePct < config.Layer2ReducePct {
		if status.CurrentTier < 2 {
			status.CurrentTier = 2
			status.ActionsTaken = append(status.ActionsTaken, "TIER2_REDUCE_50PCT")
			a.logger.Warn("LIQUIDATION TIER 2: Reducing position by 50%",
				zap.String("symbol", symbol),
				zap.Float64("distance_pct", distancePct*100),
				zap.Float64("mark_price", markPrice),
				zap.Float64("liq_price", liqPrice))
			a.reducePositionByPct(ctx, symbol, positionAmt, config.ReducePositionBy)
		}
		return
	}

	// Tier 1: 50% distance - Warning only
	if distancePct < config.Layer1WarnPct {
		if status.CurrentTier < 1 {
			status.CurrentTier = 1
			status.ActionsTaken = append(status.ActionsTaken, "TIER1_WARNING")
			a.logger.Warn("LIQUIDATION TIER 1: Approaching liquidation - WARNING",
				zap.String("symbol", symbol),
				zap.Float64("distance_pct", distancePct*100),
				zap.Float64("mark_price", markPrice),
				zap.Float64("liq_price", liqPrice))
		}
		return
	}

	// Reset tier if moved away from liquidation
	if distancePct > config.Layer1WarnPct && status.CurrentTier > 0 {
		status.CurrentTier = 0
		status.ActionsTaken = []string{}
		a.logger.Info("Liquidation tier reset - moved away from danger zone",
			zap.String("symbol", symbol),
			zap.Float64("distance_pct", distancePct*100))
	}
}

// emergencyHedgeAndClose creates hedge position and closes all (Tier 4)
func (a *AdaptiveGridManager) emergencyHedgeAndClose(ctx context.Context, symbol string, positionAmt float64) {
	if a.futuresClient == nil {
		return
	}

	// Create hedge order (counter-position)
	side := "BUY"
	if positionAmt > 0 {
		side = "SELL"
	}

	hedgeQty := math.Abs(positionAmt) * 0.5 // Hedge with 50% of position

	a.logger.Error("EMERGENCY HEDGE: Placing counter-position",
		zap.String("symbol", symbol),
		zap.String("hedge_side", side),
		zap.Float64("hedge_qty", hedgeQty))

	hedgeReq := client.PlaceOrderRequest{
		Symbol:     symbol,
		Side:       side,
		Type:       "MARKET",
		Quantity:   fmt.Sprintf("%.6f", hedgeQty),
		ReduceOnly: false, // This opens new position as hedge
	}

	_, err := a.futuresClient.PlaceOrder(ctx, hedgeReq)
	if err != nil {
		a.logger.Error("Failed to place hedge order", zap.Error(err))
	}

	// Then close original position
	a.emergencyClosePosition(ctx, symbol, positionAmt)
}

// reducePositionByClose reduces position by specified percentage (Tier 2)
func (a *AdaptiveGridManager) reducePositionByPct(ctx context.Context, symbol string, positionAmt float64, pct float64) {
	if a.futuresClient == nil || positionAmt == 0 {
		return
	}

	reduceQty := math.Abs(positionAmt) * pct
	side := "SELL"
	if positionAmt < 0 {
		side = "BUY"
	}

	a.logger.Warn("REDUCING POSITION",
		zap.String("symbol", symbol),
		zap.Float64("original_qty", math.Abs(positionAmt)),
		zap.Float64("reduce_pct", pct*100),
		zap.Float64("reduce_qty", reduceQty))

	orderReq := client.PlaceOrderRequest{
		Symbol:     symbol,
		Side:       side,
		Type:       "MARKET",
		Quantity:   fmt.Sprintf("%.6f", reduceQty),
		ReduceOnly: true,
	}

	_, err := a.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		a.logger.Error("Failed to reduce position", zap.Error(err))
	}
}

// GetLiquidationTierStatus returns current multi-layer liquidation status
func (a *AdaptiveGridManager) GetLiquidationTierStatus(symbol string) map[string]interface{} {
	status, exists := multiLayerStatus[symbol]
	if !exists || status == nil {
		return map[string]interface{}{
			"enabled": false,
			"tier":    0,
		}
	}

	return map[string]interface{}{
		"enabled":         true,
		"current_tier":    status.CurrentTier,
		"distance_to_liq": status.DistanceToLiq * 100, // as percentage
		"last_check_time": status.LastCheckTime,
		"actions_taken":   status.ActionsTaken,
	}
}

// isReadyForRegrid checks if all conditions are met for re-gridding after exit
// For volume farming: State-aware conditions - allows regrid from any state with specific criteria
//  1. State-specific criteria (see below)
//  2. No regrid cooldown (REQUIRED)
//  3. Market conditions (ADX, BB width)
//
// State-specific criteria:
//   - IDLE/WAIT_NEW_RANGE: Standard conditions (position ≈ 0, ADX < 70, BB width < 10x)
//   - OVER_SIZE: position ≤ 60% max + ADX < 50 + BB width < 5x
//   - DEFENSIVE: BB width < 3x + ADX < 50
//   - RECOVERY: PnL ≥ 0 + stable 5 minutes
//   - EXIT_HALF: PnL ≥ 0
//   - EXIT_ALL: position = 0 OR dynamic timeout expired
//   - TRADING: Allow regrid with position check
func (a *AdaptiveGridManager) isReadyForRegrid(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Get current state
	var currentState GridState
	if a.stateMachine != nil {
		currentState = a.stateMachine.GetState(symbol)
	} else {
		currentState = GridStateIdle
	}

	// State-specific regrid criteria
	switch currentState {
	case GridStateOverSize:
		return a.canRegridFromOverSize(symbol)
	case GridStateDefensive:
		return a.canRegridFromDefensive(symbol)
	case GridStateRecovery:
		return a.canRegridFromRecovery(symbol)
	case GridStateExitHalf:
		return a.canRegridFromExitHalf(symbol)
	case GridStateExitAll:
		return a.canRegridFromExitAll(symbol)
	case GridStateTrading:
		return a.canRegridFromTrading(symbol)
	case GridStateIdle, GridStateWaitNewRange:
		// Standard regrid conditions for these states
		return a.canRegridStandard(symbol, currentState)
	default:
		a.logger.Debug("Regrid check: unknown state",
			zap.String("symbol", symbol),
			zap.String("state", currentState.String()))
		return false
	}
}

// canRegridStandard checks standard regrid conditions for IDLE/WAIT_NEW_RANGE
func (a *AdaptiveGridManager) canRegridStandard(symbol string, state GridState) bool {
	a.logger.Warn("Standard regrid check START",
		zap.String("symbol", symbol),
		zap.String("state", state.String()))

	// Must have zero or negligible position before re-entry
	if position, exists := a.positions[symbol]; exists && position != nil {
		if math.Abs(position.PositionAmt) > 0 && position.NotionalValue >= 10.0 {
			a.logger.Warn("Standard regrid check FAILED: position not zero",
				zap.String("symbol", symbol),
				zap.String("state", state.String()),
				zap.Float64("position_amt", position.PositionAmt),
				zap.Float64("notional", position.NotionalValue))
			return false
		}
		if math.Abs(position.PositionAmt) > 0 && position.NotionalValue < 10.0 {
			a.logger.Info("Standard regrid check: ignoring dust position",
				zap.String("symbol", symbol),
				zap.String("state", state.String()),
				zap.Float64("position_amt", position.PositionAmt),
				zap.Float64("notional", position.NotionalValue))
		}
	} else {
		a.logger.Info("Standard regrid check: no position found (OK)",
			zap.String("symbol", symbol),
			zap.String("state", state.String()))
	}

	result := a.checkMarketConditionsForRegrid(symbol, state)

	// NEW: If market conditions fail, check if we've been stuck long enough to force regrid anyway
	// This prevents getting stuck indefinitely when range detector has no data
	if !result && state == GridStateWaitNewRange {
		if a.stateMachine != nil {
			stateTime := a.stateMachine.GetStateTime(symbol)
			if !stateTime.IsZero() {
				timeInState := time.Since(stateTime)
				// REDUCED: 5 minutes -> 2 minutes for faster recovery
				if timeInState > 2*time.Minute {
					a.logger.Warn("Standard regrid check: forcing regrid due to timeout fallback",
						zap.String("symbol", symbol),
						zap.String("state", state.String()),
						zap.Duration("time_in_state", timeInState))
					return true
				}
				a.logger.Info("Standard regrid check: market conditions failed, waiting for timeout fallback",
					zap.String("symbol", symbol),
					zap.String("state", state.String()),
					zap.Duration("time_in_state", timeInState),
					zap.Duration("fallback_timeout", 2*time.Minute))
			} else {
				a.logger.Warn("Standard regrid check: state time is zero, skipping fallback check",
					zap.String("symbol", symbol),
					zap.String("state", state.String()))
			}
		}
	}

	a.logger.Warn("Standard regrid check RESULT",
		zap.String("symbol", symbol),
		zap.String("state", state.String()),
		zap.Bool("passed", result))
	return result
}

// canRegridFromOverSize checks if regrid is allowed from OVER_SIZE state
func (a *AdaptiveGridManager) canRegridFromOverSize(symbol string) bool {
	// OVER_SIZE: position ≤ 60% max + ADX < 50 + BB width < 5x
	maxPosition := a.riskConfig.MaxPositionUSDT
	if position, exists := a.positions[symbol]; exists && position != nil {
		positionPct := position.NotionalValue / maxPosition
		if positionPct > 0.6 {
			a.logger.Debug("OVER_SIZE regrid check: position too large",
				zap.String("symbol", symbol),
				zap.Float64("position_pct", positionPct))
			return false
		}
	}

	// Check market conditions
	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return false
	}

	// ADX < 50
	avgADX := detector.averageADXLocked()
	if avgADX >= 50.0 {
		a.logger.Debug("OVER_SIZE regrid check: ADX too high",
			zap.String("symbol", symbol),
			zap.Float64("avg_adx", avgADX))
		return false
	}

	// BB width < 5x
	currentRange := detector.currentRange
	lastAccepted := detector.lastAcceptedRange
	if currentRange != nil && lastAccepted != nil {
		widthRatio := currentRange.WidthPct / lastAccepted.WidthPct
		if widthRatio >= 5.0 {
			a.logger.Debug("OVER_SIZE regrid check: BB width not contracted enough",
				zap.String("symbol", symbol),
				zap.Float64("width_ratio", widthRatio))
			return false
		}
	}

	a.logger.Info("OVER_SIZE regrid allowed",
		zap.String("symbol", symbol))
	return true
}

// canRegridFromDefensive checks if regrid is allowed from DEFENSIVE state
func (a *AdaptiveGridManager) canRegridFromDefensive(symbol string) bool {
	// DEFENSIVE: BB width < 3x + ADX < 50
	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		return false
	}

	// ADX < 50
	avgADX := detector.averageADXLocked()
	if avgADX >= 50.0 {
		a.logger.Debug("DEFENSIVE regrid check: ADX too high",
			zap.String("symbol", symbol),
			zap.Float64("avg_adx", avgADX))
		return false
	}

	// BB width < 3x
	currentRange := detector.currentRange
	lastAccepted := detector.lastAcceptedRange
	if currentRange != nil && lastAccepted != nil {
		widthRatio := currentRange.WidthPct / lastAccepted.WidthPct
		if widthRatio >= 3.0 {
			a.logger.Debug("DEFENSIVE regrid check: BB width not contracted enough",
				zap.String("symbol", symbol),
				zap.Float64("width_ratio", widthRatio))
			return false
		}
	}

	a.logger.Info("DEFENSIVE regrid allowed",
		zap.String("symbol", symbol))
	return true
}

// canRegridFromRecovery checks if regrid is allowed from RECOVERY state
func (a *AdaptiveGridManager) canRegridFromRecovery(symbol string) bool {
	// RECOVERY: PnL ≥ 0 + stable 5 minutes
	if position, exists := a.positions[symbol]; exists && position != nil {
		if position.UnrealizedPnL < 0 {
			a.logger.Debug("RECOVERY regrid check: PnL negative",
				zap.String("symbol", symbol),
				zap.Float64("pnl", position.UnrealizedPnL))
			return false
		}
	}

	// Check stability (5 minutes in RECOVERY state)
	if a.stateMachine != nil {
		stateTime := a.stateMachine.GetStateTime(symbol)
		if time.Since(stateTime) < 5*time.Minute {
			a.logger.Debug("RECOVERY regrid check: not stable enough yet",
				zap.String("symbol", symbol),
				zap.Duration("time_in_recovery", time.Since(stateTime)))
			return false
		}
	}

	a.logger.Info("RECOVERY regrid allowed",
		zap.String("symbol", symbol))
	return true
}

// canRegridFromExitHalf checks if regrid is allowed from EXIT_HALF state
func (a *AdaptiveGridManager) canRegridFromExitHalf(symbol string) bool {
	// EXIT_HALF: PnL ≥ 0
	if position, exists := a.positions[symbol]; exists && position != nil {
		if position.UnrealizedPnL < 0 {
			a.logger.Debug("EXIT_HALF regrid check: PnL negative",
				zap.String("symbol", symbol),
				zap.Float64("pnl", position.UnrealizedPnL))
			return false
		}
	}

	a.logger.Info("EXIT_HALF regrid allowed",
		zap.String("symbol", symbol))
	return true
}

// canRegridFromExitAll checks if regrid is allowed from EXIT_ALL state
func (a *AdaptiveGridManager) canRegridFromExitAll(symbol string) bool {
	// EXIT_ALL: position = 0 OR dynamic timeout expired
	if position, exists := a.positions[symbol]; exists && position != nil {
		if math.Abs(position.PositionAmt) > 0 {
			// Position not zero - check dynamic timeout
			stateTime := a.stateMachine.GetStateTime(symbol)
			dynamicTimeout := a.calculateDynamicTimeout(symbol, position)
			if time.Since(stateTime) < dynamicTimeout {
				a.logger.Debug("EXIT_ALL regrid check: dynamic timeout not expired",
					zap.String("symbol", symbol),
					zap.Duration("elapsed", time.Since(stateTime)),
					zap.Duration("timeout", dynamicTimeout))
				return false
			}
		}
	}

	a.logger.Info("EXIT_ALL regrid allowed",
		zap.String("symbol", symbol))
	return true
}

// canRegridFromTrading checks if regrid is allowed from TRADING state
func (a *AdaptiveGridManager) canRegridFromTrading(symbol string) bool {
	// TRADING: Allow regrid with position check (must not exceed max)
	maxPosition := a.riskConfig.MaxPositionUSDT
	if position, exists := a.positions[symbol]; exists && position != nil {
		if position.NotionalValue > maxPosition {
			a.logger.Debug("TRADING regrid check: position exceeds max",
				zap.String("symbol", symbol),
				zap.Float64("position", position.NotionalValue),
				zap.Float64("max", maxPosition))
			return false
		}
	}

	// Check standard market conditions
	return a.checkMarketConditionsForRegrid(symbol, GridStateTrading)
}

// checkMarketConditionsForRegrid checks common market conditions for regrid
func (a *AdaptiveGridManager) checkMarketConditionsForRegrid(symbol string, state GridState) bool {
	a.logger.Warn("Market condition check START",
		zap.String("symbol", symbol),
		zap.String("state", state.String()))

	detector, exists := a.rangeDetectors[symbol]
	if !exists {
		a.logger.Error("Market condition check FAILED: no range detector",
			zap.String("symbol", symbol),
			zap.String("state", state.String()))
		return false
	}

	// ADX < 70 (relaxed for faster reentry after breakout)
	avgADX := detector.averageADXLocked()
	a.logger.Warn("Market condition check: ADX",
		zap.String("symbol", symbol),
		zap.String("state", state.String()),
		zap.Float64("avg_adx", avgADX),
		zap.Float64("threshold", 70.0))
	if avgADX >= 70.0 {
		a.logger.Warn("Market condition check FAILED: ADX too high",
			zap.String("symbol", symbol),
			zap.String("state", state.String()),
			zap.Float64("avg_adx", avgADX))
		return false
	}

	// BB width contraction check (relaxed to 10x)
	currentRange := detector.currentRange
	lastAccepted := detector.lastAcceptedRange
	if currentRange == nil || lastAccepted == nil {
		a.logger.Error("Market condition check FAILED: no range data",
			zap.String("symbol", symbol),
			zap.String("state", state.String()),
			zap.Bool("has_current", currentRange != nil),
			zap.Bool("has_last", lastAccepted != nil))
		return false
	}

	// Check BB width contraction (current width < 10x last accepted) - very relaxed
	widthRatio := currentRange.WidthPct / lastAccepted.WidthPct
	a.logger.Warn("Market condition check: BB width ratio",
		zap.String("symbol", symbol),
		zap.String("state", state.String()),
		zap.Float64("width_ratio", widthRatio),
		zap.Float64("threshold", 10.0),
		zap.Float64("current_width_pct", currentRange.WidthPct),
		zap.Float64("last_width_pct", lastAccepted.WidthPct))
	if widthRatio >= 10.0 {
		a.logger.Warn("Market condition check FAILED: BB width not contracted enough",
			zap.String("symbol", symbol),
			zap.String("state", state.String()),
			zap.Float64("width_ratio", widthRatio))
		return false
	}

	a.logger.Warn("Market condition check PASSED",
		zap.String("symbol", symbol),
		zap.String("state", state.String()))
	return true
}

// calculateDynamicTimeout calculates dynamic timeout for EXIT_ALL state
// Timeout varies based on:
// - PnL: profitable positions get longer timeout (up to 60s)
// - Loss positions get shorter timeout (as low as 5s)
// - Volatility: calm market = longer timeout, volatile = shorter
// - Regime: ranging = longer, trending/volatile = shorter
func (a *AdaptiveGridManager) calculateDynamicTimeout(symbol string, position *SymbolPosition) time.Duration {
	// Use config values if available, otherwise use defaults
	baseTimeoutSeconds := 15.0
	minTimeoutSeconds := 5.0
	maxTimeoutSeconds := 60.0

	if a.dynamicTimeoutConfig != nil {
		baseTimeoutSeconds = float64(a.dynamicTimeoutConfig.BaseTimeoutSeconds)
		minTimeoutSeconds = float64(a.dynamicTimeoutConfig.MinTimeoutSeconds)
		maxTimeoutSeconds = float64(a.dynamicTimeoutConfig.MaxTimeoutSeconds)
	}

	baseTimeout := time.Duration(baseTimeoutSeconds) * time.Second

	// 1. PnL-based adjustment
	if position != nil {
		pnlPct := position.UnrealizedPnL / position.NotionalValue
		if pnlPct > 0 {
			// Profitable: increase timeout
			pnlMultiplier := 10.0
			maxMultiplier := 4.0
			if a.dynamicTimeoutConfig != nil {
				pnlMultiplier = a.dynamicTimeoutConfig.PnlMultiplier
				maxMultiplier = a.dynamicTimeoutConfig.MaxPnlMultiplier
			}
			adjustedMultiplier := math.Min(1.0+pnlPct*pnlMultiplier, maxMultiplier)
			baseTimeout = time.Duration(float64(baseTimeout) * adjustedMultiplier)
		} else if pnlPct < 0 {
			// Loss: decrease timeout
			lossMultiplier := 5.0
			minMultiplier := 0.33
			if a.dynamicTimeoutConfig != nil {
				lossMultiplier = a.dynamicTimeoutConfig.LossMultiplier
				minMultiplier = a.dynamicTimeoutConfig.MinLossMultiplier
			}
			adjustedMultiplier := math.Max(1.0+pnlPct*lossMultiplier, minMultiplier)
			baseTimeout = time.Duration(float64(baseTimeout) * adjustedMultiplier)
		}
	}

	// 2. Volatility-based adjustment
	detector, exists := a.rangeDetectors[symbol]
	if exists {
		avgADX := detector.averageADXLocked()
		lowADX := 30.0
		highADX := 60.0
		lowMult := 1.2
		highMult := 0.7

		if a.dynamicTimeoutConfig != nil {
			lowADX = a.dynamicTimeoutConfig.LowVolatilityADX
			highADX = a.dynamicTimeoutConfig.HighVolatilityADX
			lowMult = a.dynamicTimeoutConfig.LowVolatilityMultiplier
			highMult = a.dynamicTimeoutConfig.HighVolatilityMultiplier
		}

		if avgADX < lowADX {
			// Low volatility: longer timeout
			baseTimeout = time.Duration(float64(baseTimeout) * lowMult)
		} else if avgADX > highADX {
			// High volatility: shorter timeout
			baseTimeout = time.Duration(float64(baseTimeout) * highMult)
		}
	}

	// 3. Regime-based adjustment
	regime := a.GetCurrentRegime(symbol)
	rangingMult := 1.1
	trendingMult := 0.8
	volatileMult := 0.6

	if a.dynamicTimeoutConfig != nil {
		rangingMult = a.dynamicTimeoutConfig.RangingMultiplier
		trendingMult = a.dynamicTimeoutConfig.TrendingMultiplier
		volatileMult = a.dynamicTimeoutConfig.VolatileMultiplier
	}

	switch regime {
	case market_regime.RegimeRanging:
		baseTimeout = time.Duration(float64(baseTimeout) * rangingMult)
	case market_regime.RegimeTrending:
		baseTimeout = time.Duration(float64(baseTimeout) * trendingMult)
	case market_regime.RegimeVolatile:
		baseTimeout = time.Duration(float64(baseTimeout) * volatileMult)
	}

	// 4. Clamp to min/max bounds
	minTimeout := time.Duration(minTimeoutSeconds) * time.Second
	maxTimeout := time.Duration(maxTimeoutSeconds) * time.Second
	if baseTimeout < minTimeout {
		baseTimeout = minTimeout
	}
	if baseTimeout > maxTimeout {
		baseTimeout = maxTimeout
	}

	a.logger.Debug("Dynamic timeout calculated",
		zap.String("symbol", symbol),
		zap.Duration("timeout", baseTimeout),
		zap.Float64("pnl", position.UnrealizedPnL),
		zap.String("regime", string(regime)))

	return baseTimeout
}

// =============================================================================
// DYNAMIC LEVERAGE INTEGRATION (T033-T039)
// =============================================================================

// dynamicLeverageCalc holds the dynamic leverage calculator
var dynamicLeverageCalc *DynamicLeverageCalculator

// InitializeDynamicLeverage initializes the dynamic leverage calculator
// Automatically applies config from optConfig.DynamicLeverage if available
func (a *AdaptiveGridManager) InitializeDynamicLeverage(config *DynamicLeverageConfig) {
	// Priority 1: Use provided config
	// Priority 2: Use optConfig.DynamicLeverage from YAML
	// Priority 3: Use default config
	if config == nil && a.optConfig != nil && a.optConfig.DynamicLeverage != nil {
		optDL := a.optConfig.DynamicLeverage
		config = &DynamicLeverageConfig{
			Enabled:               optDL.Enabled,
			BaseLeverage:          optDL.BaseLeverage,
			MinLeverage:           optDL.MinLeverage,
			MaxLeverage:           optDL.MaxLeverage,
			ATRThresholdHigh:      optDL.ATRThresholdHigh,
			ATRThresholdLow:       optDL.ATRThresholdLow,
			ADXThresholdTrending:  optDL.ADXThresholdTrending,
			BBWidthThresholdTight: optDL.BBWidthThresholdTight,
		}
		a.logger.Info("Dynamic leverage config loaded from YAML")
	}
	if config == nil {
		config = DefaultDynamicLeverageConfig()
	}

	dynamicLeverageCalc = NewDynamicLeverageCalculator(config)
	a.logger.Info("Dynamic leverage initialized",
		zap.Bool("enabled", config.Enabled),
		zap.Float64("base_leverage", config.BaseLeverage),
		zap.Float64("min_leverage", config.MinLeverage),
		zap.Float64("max_leverage", config.MaxLeverage))
}

// UpdateDynamicLeverageMetrics updates metrics for leverage calculation
func (a *AdaptiveGridManager) UpdateDynamicLeverageMetrics(atr, adx, bbWidth float64) {
	if dynamicLeverageCalc == nil {
		return
	}

	dynamicLeverageCalc.UpdateATR(atr)
	dynamicLeverageCalc.UpdateADX(adx)
	dynamicLeverageCalc.UpdateBBWidth(bbWidth)
}

// GetOptimalLeverage returns calculated optimal leverage
func (a *AdaptiveGridManager) GetOptimalLeverage() float64 {
	if dynamicLeverageCalc == nil {
		return 50.0 // Default leverage
	}
	return dynamicLeverageCalc.CalculateOptimalLeverage()
}

// GetDynamicLeverageMetrics returns current metrics for display
func (a *AdaptiveGridManager) GetDynamicLeverageMetrics() map[string]float64 {
	if dynamicLeverageCalc == nil {
		return map[string]float64{
			"calculated_leverage": 50.0,
			"enabled":             0,
		}
	}

	metrics := dynamicLeverageCalc.GetCurrentMetrics()
	metrics["enabled"] = 1
	return metrics
}
