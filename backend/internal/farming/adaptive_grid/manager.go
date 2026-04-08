package adaptive_grid

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/farming/adaptive_config"
	"aster-bot/internal/farming/market_regime"

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
	gridManager    GridManagerInterface
	configManager  ConfigManagerInterface
	regimeDetector *market_regime.RegimeDetector
	futuresClient  FuturesClientInterface
	logger         *zap.Logger
	mu             sync.RWMutex

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

	// NEW: RiskMonitor for dynamic sizing and exposure
	riskMonitor *RiskMonitor

	// NEW: RangeDetector for breakout detection
	rangeDetectors map[string]*RangeDetector // symbol -> range detector

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

	// NEW: ATRCalculator for volatility calculation
	atrCalc *ATRCalculator

	// NEW: RSICalculator for RSI calculation
	rsiCalc *RSICalculator

	// NEW: OptimizationConfig from YAML files
	optConfig *config.OptimizationConfig

	// NEW: Consecutive loss tracking for cooldown
	consecutiveLosses map[string]int       // symbol -> consecutive loss count
	lastLossTime      map[string]time.Time // symbol -> last loss timestamp
	cooldownActive    map[string]bool      // symbol -> cooldown status
	totalLossesToday  int                  // total losses today

	// NEW: Partial close tracking for TP levels
	partialCloseConfig *PartialCloseConfig
	positionSlices     map[string]*PositionSlice // symbol -> position slice tracking

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
		TrailingStopPct:          0.01,                   // Default 1%
		TrailingStopDistancePct:  0.005,                  // Default 0.5%
		LiquidationBufferPct:     0.35,                   // Default 35% - more buffer to prevent liquidation
		PositionCheckInterval:    200 * time.Millisecond, // Fast risk checks
		TrendingThreshold:        0.7,                    // Default 70%

		// Take-profit from config
		TakeProfitRRatio: cfg.TakeProfitRRatio,
		MinTakeProfitPct: cfg.MinTakeProfitPct,
		MaxTakeProfitPct: cfg.MaxTakeProfitPct,

		// Directional bias from config
		UseDirectionalBias: true, // Default

		// Consecutive loss tracking - GIẢM cho volume farming
		MaxConsecutiveLosses: 5,                // Tăng lên 5 losses (thay vì 3)
		CooldownDuration:     30 * time.Second, // GIẢM xuống 30s (thay vì 5 phút)

		// Grid spread - default for volume farming
		BaseGridSpreadPct: 0.0015, // Default 0.15%
	}
}

// DefaultRiskConfig returns default risk configuration
func DefaultRiskConfig() *RiskConfig {
	return &RiskConfig{
		MaxPositionUSDT:          300.0,                  // Max 300 USDT per symbol
		MaxUnhedgedExposureUSDT:  200.0,                  // Max 200 USDT unhedged
		MaxUnrealizedLossUSDT:    3.0,                    // Close if unrealized loss > 3 USDT per position
		PerPositionLossLimitUSDT: 1.0,                    // Close position if unrealized loss > 1 USDT
		TotalNetLossLimitUSDT:    5.0,                    // Close ALL if total net unrealized loss > 5 USDT
		StopLossPct:              0.01,                   // 1% stop loss
		TrailingStopPct:          0.01,                   // Activate at 1% profit
		TrailingStopDistancePct:  0.005,                  // 0.5% trailing distance
		LiquidationBufferPct:     0.35,                   // Close at 35% away from liquidation - more safety margin
		PositionCheckInterval:    200 * time.Millisecond, // Fast risk checks to prevent liquidation
		TrendingThreshold:        0.7,                    // Pause if trending > 70%

		// Take-profit defaults
		TakeProfitRRatio: 1.5,  // 1.5:1 R:R
		MinTakeProfitPct: 0.01, // Minimum 1% TP
		MaxTakeProfitPct: 0.05, // Maximum 5% TP

		// Directional bias default
		UseDirectionalBias: true, // Only trade with trend by default

		// Consecutive loss tracking - GIẢM cho volume farming
		MaxConsecutiveLosses: 5,                // Tăng lên 5 losses (thay vì 3)
		CooldownDuration:     30 * time.Second, // GIẢM xuống 30s (thay vì 5 phút)
	}
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
	logger *zap.Logger,
) *AdaptiveGridManager {
	return &AdaptiveGridManager{
		gridManager:        baseGrid,
		configManager:      configManager,
		regimeDetector:     regimeDetector,
		futuresClient:      futuresClient,
		logger:             logger,
		currentRegime:      make(map[string]market_regime.MarketRegime),
		lastRegimeChange:   make(map[string]time.Time),
		transitionCooldown: make(map[string]time.Time),
		circuitBreakers:    make(map[string]*CircuitBreaker),
		trendingStrength:   make(map[string]float64),
		tradingPaused:      make(map[string]bool),
		maxPositionSize:    make(map[string]float64),
		unhedgedExposure:   make(map[string]float64),
		trailingStopPrice:  make(map[string]float64),
		positions:          make(map[string]*SymbolPosition),
		positionStopLoss:   make(map[string]float64),
		positionTakeProfit: make(map[string]float64),
		maxUnrealizedLoss:  make(map[string]float64),
		riskConfig:         DefaultRiskConfig(),
		rangeDetectors:     make(map[string]*RangeDetector), // NEW: Range detectors
		volumeScalers:      make(map[string]*VolumeScaler),  // NEW: Volume scalers
		timeFilter:         nil,                             // NEW: Time filter (init later)
		consecutiveLosses:  make(map[string]int),            // NEW: Track consecutive losses
		lastLossTime:       make(map[string]time.Time),      // NEW: Track last loss time
		cooldownActive:     make(map[string]bool),           // NEW: Track cooldown state
		stopCh:             make(chan struct{}),
		mu:                 sync.RWMutex{},
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
		)
	}
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

	// NEW: Start time slot monitoring goroutine
	a.wg.Add(1)
	go a.slotMonitor(ctx)

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

	// Additional ticker for checking resume conditions (less frequent)
	resumeCheckTicker := time.NewTicker(10 * time.Second)
	defer resumeCheckTicker.Stop()

	// NEW: Funding rate check ticker (every 5 minutes)
	fundingCheckTicker := time.NewTicker(5 * time.Minute)
	defer fundingCheckTicker.Stop()

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
		case <-fundingCheckTicker.C:
			// NEW: Check and apply funding rate bias
			a.CheckFundingAndApplyBias()
		}
	}
}

// checkAndManageRisk checks positions and applies risk controls
func (a *AdaptiveGridManager) checkAndManageRisk(ctx context.Context) {
	if a.futuresClient == nil {
		a.logger.Error("RISK CHECK: futuresClient is nil - cannot check positions")
		return
	}

	// Fetch positions from exchange
	positions, err := a.futuresClient.GetPositions(ctx)
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
		a.updatePositionTracking(symbol, &pos)

		// Check risk limits for this position
		a.evaluateRiskAndAct(ctx, symbol, &pos)
	}
}

// updatePositionTracking updates internal position state
func (a *AdaptiveGridManager) updatePositionTracking(symbol string, pos *client.Position) {
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

	// 4. Check max position size
	if notional > a.riskConfig.MaxPositionUSDT {
		a.logger.Warn("MAX POSITION SIZE EXCEEDED - Pausing new orders",
			zap.String("symbol", symbol),
			zap.Float64("notional", notional),
			zap.Float64("max_allowed", a.riskConfig.MaxPositionUSDT))
		a.pauseTrading(symbol)
		return
	}

	a.logger.Debug("RISK CHECK: Position passed all risk checks",
		zap.String("symbol", symbol),
		zap.Float64("unrealized_pnl", unrealizedPnL))
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

	orderReq := client.PlaceOrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          "MARKET",
		Quantity:      qty,
		ReduceOnly:    true,
		ClosePosition: true,
	}

	a.logger.Warn("EMERGENCY CLOSE - Placing market order",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("qty", qty))

	order, err := a.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		a.logger.Error("Failed to emergency close position", zap.Error(err))
		return
	}

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

	// CRITICAL: Clear grid and rebuild to start fresh after loss
	// This ensures we don't carry over stale grid state that could lead to another loss
	if a.gridManager != nil {
		a.logger.Info("Clearing grid after emergency close",
			zap.String("symbol", symbol))

		if err := a.gridManager.ClearGrid(ctx, symbol); err != nil {
			a.logger.Error("Failed to clear grid after emergency close",
				zap.String("symbol", symbol),
				zap.Error(err))
		}

		// Small delay to ensure position is fully closed on exchange
		time.Sleep(500 * time.Millisecond)

		a.logger.Info("Rebuilding grid after emergency close",
			zap.String("symbol", symbol))

		if err := a.gridManager.RebuildGrid(ctx, symbol); err != nil {
			a.logger.Error("Failed to rebuild grid after emergency close",
				zap.String("symbol", symbol),
				zap.Error(err))
		}
	}

	// Resume trading immediately with fresh grid
	// The per-position loss limit ensures next position won't exceed 1u
	a.resumeTrading(symbol)
	a.logger.Info("Trading resumed with fresh grid after emergency close",
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
		config.StabilizationPeriod = 5 * time.Minute // Wait 5 minutes for stabilization
		a.InitializeRangeDetector(symbol, config)
		a.logger.Info("Range detector initialized - waiting for price stabilization",
			zap.String("symbol", symbol),
			zap.Duration("stabilization_period", config.StabilizationPeriod))
	}

	a.logger.Error("EMERGENCY CLOSE ALL COMPLETE - All positions closed, trading paused, waiting for stabilization")
}

// pauseTrading pauses trading for a symbol
func (a *AdaptiveGridManager) pauseTrading(symbol string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tradingPaused[symbol] = true
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
	a.mu.RUnlock()

	// If not paused, nothing to do
	if !isPaused {
		return true
	}

	// If no range detector, we can't check stabilization
	if !hasDetector {
		// Just resume without range check (fallback)
		a.logger.Info("No range detector found - resuming trading without stabilization check",
			zap.String("symbol", symbol))
		a.resumeTrading(symbol)
		return true
	}

	// Check if range is active (price is stable)
	if detector.ShouldTrade() {
		a.logger.Info("Price stabilized - Range is active, resuming trading",
			zap.String("symbol", symbol),
			zap.String("range_state", detector.GetStateString()))
		a.resumeTrading(symbol)
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
// For volume farming: luôn cho phép rebalance ngay sau khi fill để duy trì số lệnh grid
func (a *AdaptiveGridManager) CanPlaceOrder(symbol string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Check if trading is paused
	if a.tradingPaused[symbol] {
		return false
	}

	// NEW: Check consecutive loss cooldown - GIẢM thời gian cho volume farming
	if a.cooldownActive[symbol] {
		lastLoss := a.lastLossTime[symbol]
		cooldownDuration := a.riskConfig.CooldownDuration
		if cooldownDuration <= 0 {
			cooldownDuration = 30 * time.Second // GIẢM: 30s thay vì 5 phút cho volume farming
		}
		if time.Since(lastLoss) < cooldownDuration {
			a.logger.Warn("CanPlaceOrder: cooldown active but allowing rebalance for volume farming",
				zap.String("symbol", symbol),
				zap.Int("consecutive_losses", a.consecutiveLosses[symbol]),
				zap.Duration("remaining", cooldownDuration-time.Since(lastLoss)))
			// Volume farming: Vẫn cho phép nhưng log warning - không return false
		} else {
			// Cooldown expired - reset
			a.cooldownActive[symbol] = false
			a.consecutiveLosses[symbol] = 0
		}
	}

	// Check if in cooldown - GIẢM thời gian cho volume farming
	if cooldown, exists := a.transitionCooldown[symbol]; exists {
		if time.Since(cooldown) < 5*time.Second { // GIẢM từ 30s xuống 5s
			a.logger.Debug("CanPlaceOrder: transition cooldown active but allowing for volume farming",
				zap.String("symbol", symbol),
				zap.Duration("elapsed", time.Since(cooldown)))
			// Volume farming: Không return false, vẫn cho phép
		}
	}

	// Check position limit - Volume farming: vẫn cho phép rebalance để hedge
	if position, exists := a.positions[symbol]; exists {
		if position.NotionalValue >= a.riskConfig.MaxPositionUSDT {
			a.logger.Warn("CanPlaceOrder: max position reached but allowing rebalance for volume farming",
				zap.String("symbol", symbol),
				zap.Float64("notional", position.NotionalValue),
				zap.Float64("max", a.riskConfig.MaxPositionUSDT))
			// Volume farming: Vẫn cho phép đặt lệnh đối ứng để hedge và farm volume
			// Không return false
		}
	}

	// NEW: Check RiskMonitor exposure limits - Cho phép rebalance với size nhỏ hơn
	if a.riskMonitor != nil {
		_, maxExposure, utilization := a.riskMonitor.GetExposureStats()
		if utilization >= 1.0 {
			a.logger.Warn("CanPlaceOrder: max exposure reached but allowing rebalance for volume farming",
				zap.Float64("utilization", utilization*100),
				zap.Float64("max_exposure", maxExposure))
			// Volume farming: Vẫn cho phép rebalance, size sẽ được điều chỉnh tự động
			// Không return false để không block việc thay thế lệnh đã fill
		}
	}

	// CRITICAL: Check range state - STRICT: Chỉ trade khi BB range active
	if detector, exists := a.rangeDetectors[symbol]; exists {
		if !detector.ShouldTrade() {
			a.logger.Warn("CanPlaceOrder BLOCKED: BB range not active - waiting for range establishment",
				zap.String("symbol", symbol),
				zap.String("state", detector.GetStateString()))
			return false // STRICT: Không có range = không trade
		}
	}

	// NEW: Check TimeFilter - only trade during allowed hours
	if a.timeFilter != nil {
		if !a.timeFilter.CanTrade() {
			a.logger.Debug("CanPlaceOrder blocked: outside trading hours",
				zap.String("symbol", symbol))
			return false
		}
	}

	// NEW: Check SpreadProtection - log warning nhưng không block cho volume farming
	if a.spreadProtection != nil {
		if a.spreadProtection.ShouldPauseTrading() {
			a.logger.Warn("CanPlaceOrder: spread too wide but allowing for volume farming",
				zap.String("symbol", symbol),
				zap.Float64("spread_pct", a.spreadProtection.GetSpreadPct()*100))
			// Volume farming: Không block, vẫn cho phép đặt lệnh với spread rộng
		}
	}

	// NEW: Check InventoryManager - Volume farming: không pause side dù skew cao
	if a.inventoryMgr != nil {
		// Không kiểm tra ShouldPauseSide nữa - luôn cho phép cả 2 sides
		// Size sẽ được điều chỉnh trong GetInventoryAdjustedSize
		skewRatio := a.inventoryMgr.CalculateSkewRatio(symbol)
		if skewRatio > 0.8 {
			a.logger.Info("CanPlaceOrder: inventory skew detected but allowing for volume farming",
				zap.String("symbol", symbol),
				zap.Float64("skew_ratio", skewRatio))
		}
	}

	// NEW: Check TrendDetector - Volume farming: log warning nhưng không block
	if a.trendDetector != nil {
		state := a.trendDetector.GetTrendState()
		score := a.trendDetector.GetTrendScore()

		// Strong trend - log warning nhưng không block
		if score >= 4 {
			a.logger.Info("CanPlaceOrder: strong trend detected but allowing for volume farming",
				zap.String("symbol", symbol),
				zap.String("trend_state", state.String()),
				zap.Int("trend_score", score))
			// Volume farming: Không return false, vẫn cho phép đặt lệnh
			// Grid sẽ tự động điều chỉnh spread và size
		}
	}

	// NEW: Check FundingRate - skip opening if funding too high
	if a.fundingMonitor != nil {
		if biasSide, _, shouldSkip := a.fundingMonitor.GetFundingBias(symbol); shouldSkip {
			a.logger.Warn("CanPlaceOrder BLOCKED: extreme funding rate",
				zap.String("symbol", symbol),
				zap.String("bias_side", biasSide))
			return false
		}
	}

	return true
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
	symbols := make([]string, 0, len(a.positions))
	for symbol := range a.positions {
		symbols = append(symbols, symbol)
	}
	a.mu.RUnlock()

	for _, symbol := range symbols {
		a.ApplyFundingBias(symbol)
	}
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

// CanTrade returns true if trading is allowed at current time
func (a *AdaptiveGridManager) CanTrade() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.timeFilter == nil {
		return true // Default allow if no time filter
	}

	return a.timeFilter.CanTrade()
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
		return adjustedSize, nil
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
	a.mu.Lock()
	defer a.mu.Unlock()

	// Feed ATR calculator
	if a.atrCalc != nil {
		a.atrCalc.AddPrice(high, low, close)
	}

	// Feed RSI calculator
	if a.rsiCalc != nil {
		a.rsiCalc.AddPrice(close)
	}

	// Feed TrendDetector
	if a.trendDetector != nil {
		a.trendDetector.UpdatePrice(close, 0)

		// STRICT: Đóng tất cả lệnh khi strong trend detected
		if a.trendDetector.GetTrendScore() >= 6 {
			state := a.trendDetector.GetTrendState()
			a.handleStrongTrend(context.Background(), symbol, close, state)
		}
	}

	// Feed SpreadProtection
	if a.spreadProtection != nil && bid > 0 && ask > 0 {
		a.spreadProtection.UpdateOrderbook(bid, ask)
	}

	// Feed DynamicSpreadCalculator
	if a.dynamicSpreadCalc != nil {
		a.dynamicSpreadCalc.UpdateATR(high, low, close)
	}
}

// RecordTradeResult records trade result for loss tracking with cooldown
func (a *AdaptiveGridManager) RecordTradeResult(symbol string, isWin bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Also update risk monitor if available
	if a.riskMonitor != nil {
		// Estimate PnL: win = +1 USDT, loss = -1 USDT (for Kelly calculation)
		pnl := -1.0
		if isWin {
			pnl = 1.0
		}
		a.riskMonitor.RecordTradeResult(symbol, pnl)
	}

	// Get RiskConfig values (fallback to defaults if not set)
	maxConsecutiveLosses := a.riskConfig.MaxConsecutiveLosses
	if maxConsecutiveLosses <= 0 {
		maxConsecutiveLosses = 3 // Default
	}
	cooldownDuration := a.riskConfig.CooldownDuration
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
	} else {
		// Increment consecutive losses
		a.consecutiveLosses[symbol]++
		a.lastLossTime[symbol] = time.Now()
		a.totalLossesToday++

		a.logger.Warn("Loss recorded",
			zap.String("symbol", symbol),
			zap.Int("consecutive_losses", a.consecutiveLosses[symbol]),
			zap.Int("total_losses_today", a.totalLossesToday))

		// Check if max consecutive losses reached
		if a.consecutiveLosses[symbol] >= maxConsecutiveLosses {
			a.cooldownActive[symbol] = true
			a.pauseTrading(symbol)
			a.logger.Error("MAX CONSECUTIVE LOSSES REACHED - Entering cooldown",
				zap.String("symbol", symbol),
				zap.Int("losses", a.consecutiveLosses[symbol]),
				zap.Duration("cooldown", cooldownDuration))
		}
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

// InitializeRangeDetector creates a range detector for a symbol
func (a *AdaptiveGridManager) InitializeRangeDetector(symbol string, config *RangeConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if config == nil {
		config = DefaultRangeConfig()
	}

	a.rangeDetectors[symbol] = NewRangeDetector(config, a.logger)
	a.logger.Info("RangeDetector initialized for symbol",
		zap.String("symbol", symbol),
		zap.String("method", config.Method))
}

// UpdatePriceForRange updates price data for range detection
func (a *AdaptiveGridManager) UpdatePriceForRange(symbol string, high, low, close float64) {
	a.mu.RLock()
	detector, exists := a.rangeDetectors[symbol]
	a.mu.RUnlock()

	if !exists {
		// Auto-initialize if not exists
		a.InitializeRangeDetector(symbol, nil)
		detector, _ = a.rangeDetectors[symbol]
	}

	detector.AddPrice(high, low, close)

	// Check for breakout and handle
	if detector.IsBreakout() {
		a.handleBreakout(context.Background(), symbol, close)
	}
}

// handleStrongTrend handles strong trend detection - STRICT: đóng lệnh như breakout
func (a *AdaptiveGridManager) handleStrongTrend(ctx context.Context, symbol string, currentPrice float64, state TrendState) {
	a.logger.Error("STRONG TREND DETECTED - Closing ALL orders and positions!",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice),
		zap.String("trend_state", state.String()),
		zap.Int("trend_score", a.trendDetector.GetTrendScore()))

	// STRICT: Đóng tất cả lệnh và position như breakout
	// 1. Cancel ALL orders immediately
	if a.gridManager != nil {
		if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
			a.logger.Error("Failed to cancel orders on strong trend", zap.String("symbol", symbol), zap.Error(err))
		}
	}

	// 2. Close any open position
	a.mu.RLock()
	position, hasPosition := a.positions[symbol]
	a.mu.RUnlock()

	if hasPosition && position.PositionAmt != 0 {
		a.logger.Warn("Closing position on strong trend",
			zap.String("symbol", symbol),
			zap.Float64("position_amt", position.PositionAmt))
		a.emergencyClosePosition(ctx, symbol, position.PositionAmt)
	}

	// 3. Clear grid
	if a.gridManager != nil {
		if err := a.gridManager.ClearGrid(ctx, symbol); err != nil {
			a.logger.Error("Failed to clear grid on strong trend", zap.String("symbol", symbol), zap.Error(err))
		}
	}

	// 4. Pause trading and wait for BB to establish new range
	a.pauseTrading(symbol)

	// 5. Force recalculation of range
	a.mu.RLock()
	detector, exists := a.rangeDetectors[symbol]
	a.mu.RUnlock()

	if exists {
		detector.ForceRecalculate()
	}

	a.logger.Info("Trading paused after strong trend - Waiting for BB to establish new range",
		zap.String("symbol", symbol))
}

// handleBreakout handles breakout detection - STRICT risk management
// Khi breakout: đóng TẤT CẢ lệnh và position, sau đó chờ BB tạo range mới
func (a *AdaptiveGridManager) handleBreakout(ctx context.Context, symbol string, currentPrice float64) {
	a.logger.Error("BREAKOUT DETECTED - Closing ALL orders and positions immediately!",
		zap.String("symbol", symbol),
		zap.Float64("price", currentPrice))

	// 1. Cancel ALL orders immediately
	if a.gridManager != nil {
		if err := a.gridManager.CancelAllOrders(ctx, symbol); err != nil {
			a.logger.Error("Failed to cancel orders on breakout", zap.String("symbol", symbol), zap.Error(err))
		}
	}

	// 2. Close any open position
	a.mu.RLock()
	position, hasPosition := a.positions[symbol]
	a.mu.RUnlock()

	if hasPosition && position.PositionAmt != 0 {
		a.logger.Warn("Closing position on breakout",
			zap.String("symbol", symbol),
			zap.Float64("position_amt", position.PositionAmt))
		a.emergencyClosePosition(ctx, symbol, position.PositionAmt)
	}

	// 3. Clear grid
	if a.gridManager != nil {
		if err := a.gridManager.ClearGrid(ctx, symbol); err != nil {
			a.logger.Error("Failed to clear grid on breakout", zap.String("symbol", symbol), zap.Error(err))
		}
	}

	// 4. Pause trading and wait for BB to establish new range
	a.pauseTrading(symbol)

	// 5. Force recalculation of range
	a.mu.RLock()
	detector, exists := a.rangeDetectors[symbol]
	a.mu.RUnlock()

	if exists {
		detector.ForceRecalculate()
	}

	a.logger.Info("Trading paused after breakout - Waiting for BB to establish new range",
		zap.String("symbol", symbol))
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
