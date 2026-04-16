package farming

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"aster-bot/internal/agentic"
	"aster-bot/internal/auth"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/dashboard"
	"aster-bot/internal/farming/adaptive_config"
	"aster-bot/internal/farming/adaptive_grid"
	"aster-bot/internal/farming/market_regime"
	farmsync "aster-bot/internal/farming/sync"
	"aster-bot/internal/farming/tradingmode"
	"aster-bot/internal/farming/volume_optimization"
	"aster-bot/internal/risk"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// MarkPrice represents a mark price update.
type MarkPrice struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
}

// OrderUpdate represents an order update event.
type OrderUpdate struct {
	Symbol      string  `json:"symbol"`
	OrderID     string  `json:"order_id"`
	ClientID    string  `json:"client_id"`
	Side        string  `json:"side"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Price       float64 `json:"price"`
	Quantity    float64 `json:"quantity"`
	ExecutedQty float64 `json:"executed_qty"`
	Fee         float64 `json:"fee"`
	Timestamp   int64   `json:"timestamp"`
}

// VolumeFarmEngine orchestrates volume farming operations.
type VolumeFarmEngine struct {
	config              *config.Config
	volumeConfig        *config.VolumeFarmConfig
	logger              *zap.Logger
	isRunning           bool
	isRunningMu         sync.RWMutex
	futuresClient       *client.FuturesClient
	riskManager         *risk.Manager
	symbolSelector      *SymbolSelector
	gridManager         *GridManager
	adaptiveGridManager *adaptive_grid.AdaptiveGridManager
	regimeDetector      *market_regime.RegimeDetector
	configManager       *adaptive_config.AdaptiveConfigManager
	pointsTracker       *PointsTracker
	userStream          *stream.UserStream

	// NEW: Continuous Volume Farming components
	modeManager     *tradingmode.ModeManager
	circuitBreaker  *agentic.CircuitBreaker // Unified trading decision brain (single source of truth)
	exitExecutor    *ExitExecutor
	syncManager     *farmsync.SyncManager
	wsClient        *client.WebSocketClient
	metricsStreamer *dashboard.MetricsStreamer // Real-time metrics streaming

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewVolumeFarmEngine creates a new volume farming engine.
func NewVolumeFarmEngine(cfg *config.Config, logger *zap.Logger) (*VolumeFarmEngine, error) {
	logger.Info("=== NewVolumeFarmEngine called ===")

	engine := &VolumeFarmEngine{
		config: cfg,
		logger: logger,
		stopCh: make(chan struct{}),
	}

	var httpClient *client.HTTPClient

	if cfg.Exchange.UserWallet != "" && cfg.Exchange.APISigner != "" && cfg.Exchange.APISignerKey != "" {
		v3Signer, err := auth.NewV3Signer(
			cfg.Exchange.UserWallet,
			cfg.Exchange.APISigner,
			cfg.Exchange.APISignerKey,
			int64(cfg.Exchange.RecvWindow),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create V3 signer: %w", err)
		}

		httpClient = client.NewHTTPClientV3(cfg.Exchange.FuturesRESTBase, v3Signer, logger, cfg.Exchange.RequestsPerSecond)
		engine.logger.Info("Using V3 authentication (API Wallet/Agent model)")

		// Sync server time offset for V3 signer to avoid timestamp drift errors (-1021)
		marketClient := client.NewMarketClient(httpClient)
		localTimeBefore := time.Now().UnixMilli()
		serverTime, err := marketClient.ServerTime(context.Background())
		if err != nil {
			engine.logger.Warn("Failed to sync server time for V3 signer", zap.Error(err))
		} else {
			localTimeAfter := time.Now().UnixMilli()
			localTimeEstimated := localTimeBefore + (localTimeAfter-localTimeBefore)/2
			offset := serverTime - localTimeEstimated
			v3Signer.SetTimeOffset(offset)
			engine.logger.Info("V3 server time synced", zap.Int64("offset_ms", offset), zap.Int64("server_time", serverTime))
		}
	} else if cfg.Exchange.APIKey != "" && cfg.Exchange.APISecret != "" {
		v1Signer, err := auth.NewSigner(cfg.Exchange.APIKey, cfg.Exchange.APISecret, cfg.Exchange.RecvWindow)
		if err != nil {
			return nil, fmt.Errorf("failed to create V1 signer: %w", err)
		}

		httpClient = client.NewHTTPClient(cfg.Exchange.FuturesRESTBase, v1Signer, logger, cfg.Exchange.RequestsPerSecond)
		engine.logger.Info("Using V1 authentication (API Key model - deprecated)")

		// Sync server time offset for V1 signer to avoid timestamp drift errors (-1021)
		marketClient := client.NewMarketClient(httpClient)
		localTimeBefore := time.Now().UnixMilli()
		serverTime, err := marketClient.ServerTime(context.Background())
		if err != nil {
			engine.logger.Warn("Failed to sync server time for V1 signer", zap.Error(err))
		} else {
			localTimeAfter := time.Now().UnixMilli()
			localTimeEstimated := localTimeBefore + (localTimeAfter-localTimeBefore)/2
			offset := serverTime - localTimeEstimated
			v1Signer.SetTimeOffset(offset)
			engine.logger.Info("V1 server time synced", zap.Int64("offset_ms", offset), zap.Int64("server_time", serverTime))
		}
	} else {
		return nil, fmt.Errorf("no valid authentication credentials found - please configure either V3 or V1")
	}

	volumeConfig := engine.extractVolumeFarmConfig(cfg)
	engine.volumeConfig = volumeConfig

	// DEBUG: Check Risk config structure
	logger.Info("=== Volume Config Extracted ===",
		zap.Bool("market_eval_is_nil", volumeConfig.Risk.MarketConditionEvaluator == nil),
		zap.Float64("max_position_usdt", volumeConfig.Risk.MaxPositionUSDTPerSymbol),
		zap.Bool("pnl_risk_control_is_nil", volumeConfig.Risk.PnLRiskControl == nil),
		zap.Bool("over_size_is_nil", volumeConfig.Risk.OverSize == nil),
		zap.Bool("defensive_state_is_nil", volumeConfig.Risk.DefensiveState == nil),
		zap.Bool("recovery_state_is_nil", volumeConfig.Risk.RecoveryState == nil))

	if volumeConfig.Risk.MarketConditionEvaluator != nil {
		logger.Info("=== MarketConditionEvaluator Config Found ===",
			zap.Bool("enabled", volumeConfig.Risk.MarketConditionEvaluator.Enabled),
			zap.Int("interval", volumeConfig.Risk.MarketConditionEvaluator.EvaluationIntervalSec),
			zap.Float64("confidence", volumeConfig.Risk.MarketConditionEvaluator.MinConfidenceThreshold))
	} else {
		logger.Warn("=== MarketConditionEvaluator Config NOT Found ===")
	}

	engine.futuresClient = client.NewFuturesClient(httpClient, volumeConfig.Bot.DryRun, logger, cfg.Exchange.RequestsPerSecond)
	engine.riskManager = risk.NewManager(volumeConfig.Risk, logger, engine.futuresClient)

	// NEW: Initialize and inject CorrelationTracker for correlation-based risk checks
	corrThreshold := volumeConfig.Risk.CorrelationThreshold
	if corrThreshold <= 0 {
		corrThreshold = 0.8 // Default 80% correlation threshold
	}
	corrTracker := regime.NewCorrelationTracker(nil, corrThreshold)
	engine.riskManager.SetCorrelationTracker(corrTracker)
	logger.Info("CorrelationTracker initialized",
		zap.Float64("threshold", corrThreshold))

	logrusEntry := logrus.NewEntry(logrus.StandardLogger()).WithField("component", "volume_farm")

	// Create shared WebSocket client for both SymbolSelector and GridManager
	zapLogger, _ := zap.NewDevelopment()
	wsURL := buildTickerStreamURL(volumeConfig.Exchange.FuturesWSBase, volumeConfig.TickerStream)
	if wsURL == "" {
		wsURL = "wss://fstream.asterdex.com/ws/!ticker@arr"
	}
	sharedWSClient := client.NewWebSocketClient(wsURL, zapLogger)

	engine.symbolSelector = NewSymbolSelector(engine.futuresClient, logrusEntry.WithField("component", "symbol_selector"), volumeConfig)
	engine.symbolSelector.SetWebSocketClient(sharedWSClient)

	gridLogger := logrusEntry.WithField("component", "grid_manager")
	engine.gridManager = NewGridManager(engine.futuresClient, gridLogger, volumeConfig)
	engine.gridManager.SetWebSocketClient(sharedWSClient)
	engine.gridManager.ApplyConfig(volumeConfig)

	pointsLogger := logrusEntry.WithField("component", "points_tracker")
	engine.pointsTracker = NewPointsTracker(volumeConfig, pointsLogger)

	// Initialize adaptive configuration
	configManager := adaptive_config.NewAdaptiveConfigManager("config/adaptive_config.yaml", logger)
	engine.configManager = configManager

	// Initialize regime detector
	regimeDetector := market_regime.NewRegimeDetector(logger, nil)
	engine.regimeDetector = regimeDetector

	// Initialize adaptive grid manager
	adaptiveGridManager := adaptive_grid.NewAdaptiveGridManager(
		engine.gridManager,
		configManager,
		regimeDetector,
		engine.futuresClient,
		engine.gridManager,                    // GridManager implements PositionProvider interface
		volumeConfig.Exchange.FuturesRESTBase, // API base URL for historical data fetch
		logger,
	)
	engine.adaptiveGridManager = adaptiveGridManager

	stateMachine := adaptive_grid.NewGridStateMachine(logger)
	engine.gridManager.SetStateMachine(stateMachine)
	engine.adaptiveGridManager.SetStateMachine(stateMachine)

	// CRITICAL: Set risk config from volume config (not hardcode)
	adaptiveGridManager.SetRiskConfig(adaptive_grid.ConvertRiskConfig(volumeConfig.Risk))
	logger.Info("AdaptiveGridManager risk config set from volume config",
		zap.Float64("max_position_usdt", volumeConfig.Risk.MaxPositionUSDTPerSymbol),
		zap.Float64("daily_loss_limit", volumeConfig.Risk.DailyLossLimitUSDT),
		zap.Float64("per_trade_sl_pct", volumeConfig.Risk.PerTradeStopLossPct),
	)

	// Set PnL risk control config to grid manager
	if volumeConfig.Risk.PnLRiskControl != nil {
		engine.gridManager.SetPnLRiskConfig(volumeConfig.Risk.PnLRiskControl)
		logger.Info("GridManager PnL risk control config set",
			zap.Bool("enabled", volumeConfig.Risk.PnLRiskControl.Enabled),
			zap.Float64("partial_loss_usdt", volumeConfig.Risk.PnLRiskControl.PartialLossUSDT),
			zap.Float64("full_loss_usdt", volumeConfig.Risk.PnLRiskControl.FullLossUSDT))
	}

	// Initialize and wire Market Condition Evaluator
	logger.Info("Checking Market Condition Evaluator config",
		zap.Bool("is_nil", volumeConfig.Risk.MarketConditionEvaluator == nil))

	if volumeConfig.Risk.MarketConditionEvaluator != nil && volumeConfig.Risk.MarketConditionEvaluator.Enabled {
		logger.Info("Initializing Market Condition Evaluator",
			zap.Bool("enabled", volumeConfig.Risk.MarketConditionEvaluator.Enabled),
			zap.Int("evaluation_interval_sec", volumeConfig.Risk.MarketConditionEvaluator.EvaluationIntervalSec))

		marketEval := adaptive_grid.NewMarketConditionEvaluator(volumeConfig.Risk.MarketConditionEvaluator, logger)
		// Wire data sources
		marketEval.SetAdaptiveGridManager(adaptiveGridManager)
		marketEval.SetWebSocketClient(sharedWSClient)

		// Initialize AdaptiveThresholdManager and wire to MarketConditionEvaluator
		if volumeConfig.Risk.AdaptiveThresholds != nil && volumeConfig.Risk.AdaptiveThresholds.Enabled {
			logger.Info("Initializing AdaptiveThresholdManager",
				zap.Bool("enabled", volumeConfig.Risk.AdaptiveThresholds.Enabled))

			atConfig := &adaptive_grid.AdaptiveThresholdConfig{
				BasePositionThreshold:   volumeConfig.Risk.AdaptiveThresholds.BasePositionThreshold,
				BaseVolatilityThreshold: volumeConfig.Risk.AdaptiveThresholds.BaseVolatilityThreshold,
				BaseRiskThreshold:       volumeConfig.Risk.AdaptiveThresholds.BaseRiskThreshold,
				BaseTrendThreshold:      volumeConfig.Risk.AdaptiveThresholds.BaseTrendThreshold,
				AdaptationRate:          volumeConfig.Risk.AdaptiveThresholds.AdaptationRate,
				MinThreshold:            volumeConfig.Risk.AdaptiveThresholds.MinThreshold,
				MaxThreshold:            volumeConfig.Risk.AdaptiveThresholds.MaxThreshold,
				EnableLearning:          volumeConfig.Risk.AdaptiveThresholds.EnableLearning,
				LearningRate:            volumeConfig.Risk.AdaptiveThresholds.LearningRate,
			}
			atm := adaptive_grid.NewAdaptiveThresholdManager(logger, atConfig)
			marketEval.SetAdaptiveThresholdManager(atm)
			logger.Info("AdaptiveThresholdManager initialized and set on MarketConditionEvaluator")
		} else {
			logger.Warn("AdaptiveThresholdManager NOT initialized",
				zap.Bool("config_is_nil", volumeConfig.Risk.AdaptiveThresholds == nil),
				zap.Bool("enabled", volumeConfig.Risk.AdaptiveThresholds != nil && volumeConfig.Risk.AdaptiveThresholds.Enabled))
		}

		// Set to grid manager
		engine.gridManager.SetMarketConditionEvaluator(marketEval)
		logger.Info("Market Condition Evaluator initialized and wired",
			zap.Bool("enabled", volumeConfig.Risk.MarketConditionEvaluator.Enabled),
			zap.Int("evaluation_interval_sec", volumeConfig.Risk.MarketConditionEvaluator.EvaluationIntervalSec),
			zap.Float64("min_confidence_threshold", volumeConfig.Risk.MarketConditionEvaluator.MinConfidenceThreshold))
	} else {
		logger.Warn("Market Condition Evaluator NOT initialized",
			zap.Bool("config_is_nil", volumeConfig.Risk.MarketConditionEvaluator == nil),
			zap.Bool("enabled", volumeConfig.Risk.MarketConditionEvaluator != nil && volumeConfig.Risk.MarketConditionEvaluator.Enabled))
	}

	// Set adaptive state configurations
	if volumeConfig.Risk.OverSize != nil {
		engine.gridManager.SetOverSizeConfig(volumeConfig.Risk.OverSize)
		logger.Info("OVER_SIZE state config set",
			zap.Float64("threshold_pct", volumeConfig.Risk.OverSize.ThresholdPct),
			zap.Float64("recovery_pct", volumeConfig.Risk.OverSize.RecoveryPct))
	}
	if volumeConfig.Risk.DefensiveState != nil {
		engine.gridManager.SetDefensiveConfig(volumeConfig.Risk.DefensiveState)
		logger.Info("DEFENSIVE state config set",
			zap.Float64("atr_multiplier_threshold", volumeConfig.Risk.DefensiveState.ATRMultiplierThreshold),
			zap.Float64("bb_width_threshold", volumeConfig.Risk.DefensiveState.BBWidthThreshold))
	}
	if volumeConfig.Risk.RecoveryState != nil {
		engine.gridManager.SetRecoveryConfig(volumeConfig.Risk.RecoveryState)
		logger.Info("RECOVERY state config set",
			zap.Float64("recovery_threshold_usdt", volumeConfig.Risk.RecoveryState.RecoveryThresholdUSDT),
			zap.Float64("size_multiplier", volumeConfig.Risk.RecoveryState.SizeMultiplier))
	}

	// Initialize ConditionBlocker for conditional blocking
	if volumeConfig.Risk.ConditionBlocker != nil && volumeConfig.Risk.ConditionBlocker.Enabled {
		logger.Info("Initializing ConditionBlocker",
			zap.Bool("enabled", volumeConfig.Risk.ConditionBlocker.Enabled))

		cb := adaptive_grid.NewConditionBlocker(logger)
		// Set config from YAML
		cbConfig := &adaptive_grid.ConditionBlockerConfig{
			PositionSizeWeight: volumeConfig.Risk.ConditionBlocker.PositionSizeWeight,
			VolatilityWeight:   volumeConfig.Risk.ConditionBlocker.VolatilityWeight,
			RiskWeight:         volumeConfig.Risk.ConditionBlocker.RiskWeight,
			TrendWeight:        volumeConfig.Risk.ConditionBlocker.TrendWeight,
			SkewWeight:         volumeConfig.Risk.ConditionBlocker.SkewWeight,
			BlockingThreshold:  volumeConfig.Risk.ConditionBlocker.BlockingThreshold,
			MicroModeMin:       volumeConfig.Risk.ConditionBlocker.MicroModeMin,
		}
		cb.SetConfig(cbConfig)
		adaptiveGridManager.SetConditionBlocker(cb)
		logger.Info("ConditionBlocker initialized and set on AdaptiveGridManager")
	} else {
		logger.Warn("ConditionBlocker NOT initialized",
			zap.Bool("config_is_nil", volumeConfig.Risk.ConditionBlocker == nil),
			zap.Bool("enabled", volumeConfig.Risk.ConditionBlocker != nil && volumeConfig.Risk.ConditionBlocker.Enabled))
	}

	// NEW: Initialize Volume Optimization Components
	if volumeConfig.VolumeOptimization != nil && volumeConfig.VolumeOptimization.Enabled {
		logger.Info("=== Initializing Volume Optimization Components ===",
			zap.Bool("enabled", volumeConfig.VolumeOptimization.Enabled))

		// Initialize TickSizeManager
		if volumeConfig.VolumeOptimization.OrderPriority.TickSizeAwareness.Enabled {
			logger.Info("Initializing TickSizeManager")
			tickSizeMgr := volume_optimization.NewTickSizeManager(logger)

			// Set tick sizes from config
			for symbol, tickSize := range volumeConfig.VolumeOptimization.OrderPriority.TickSizeAwareness.TickSizes {
				tickSizeMgr.SetTickSize(symbol, tickSize)
			}

			// Start periodic refresh (every 5 minutes)
			ctx := context.Background()
			go tickSizeMgr.StartPeriodicRefresh(ctx, 5*time.Minute)

			logger.Info("TickSizeManager initialized and started",
				zap.Int("tick_sizes_count", len(volumeConfig.VolumeOptimization.OrderPriority.TickSizeAwareness.TickSizes)))
		}

		// Initialize VPINMonitor
		if volumeConfig.VolumeOptimization.ToxicFlow.Enabled {
			logger.Info("Initializing VPINMonitor")
			vpinConfig := volume_optimization.VPINConfig{
				WindowSize:        volumeConfig.VolumeOptimization.ToxicFlow.WindowSize,
				BucketSize:        volumeConfig.VolumeOptimization.ToxicFlow.BucketSize,
				VPINThreshold:     volumeConfig.VolumeOptimization.ToxicFlow.VPINThreshold,
				SustainedBreaches: volumeConfig.VolumeOptimization.ToxicFlow.SustainedBreaches,
				AutoResumeDelay:   volumeConfig.VolumeOptimization.ToxicFlow.AutoResumeDelay,
			}
			vpinMonitor := volume_optimization.NewVPINMonitor(vpinConfig, logger)

			// Wire to adaptiveGridManager
			adaptiveGridManager.SetVPINMonitor(vpinMonitor)

			logger.Info("VPINMonitor initialized and wired to AdaptiveGridManager",
				zap.Int("window_size", vpinConfig.WindowSize),
				zap.Float64("bucket_size", vpinConfig.BucketSize),
				zap.Float64("vpin_threshold", vpinConfig.VPINThreshold))
		}

		// Initialize PostOnlyHandler
		if volumeConfig.VolumeOptimization.MakerTaker.PostOnlyEnabled {
			logger.Info("Initializing PostOnlyHandler")
			postOnlyConfig := volume_optimization.PostOnlyConfig{
				Enabled:    volumeConfig.VolumeOptimization.MakerTaker.PostOnlyEnabled,
				Fallback:   volumeConfig.VolumeOptimization.MakerTaker.PostOnlyFallback,
				MaxRetries: 3,                      // Default
				RetryDelay: 100 * time.Millisecond, // Default
			}
			_ = volume_optimization.NewPostOnlyHandler(postOnlyConfig, logger)

			logger.Info("PostOnlyHandler initialized",
				zap.Bool("enabled", postOnlyConfig.Enabled),
				zap.Bool("fallback", postOnlyConfig.Fallback))
		}
	} else {
		logger.Warn("Volume Optimization Components NOT initialized",
			zap.Bool("config_is_nil", volumeConfig.VolumeOptimization == nil),
			zap.Bool("enabled", volumeConfig.VolumeOptimization != nil && volumeConfig.VolumeOptimization.Enabled))
	}

	// NEW: Initialize FluidFlowEngine for continuous flow behavior (always init, independent of config)
	logger.Info("Initializing FluidFlowEngine for continuous flow behavior")
	fluidFlowEngine := adaptive_grid.NewFluidFlowEngine(logger)
	adaptiveGridManager.SetFluidFlowEngine(fluidFlowEngine)
	logger.Info("FluidFlowEngine initialized and wired (US13: Fluid Flow Behavior)")

	// Connect adaptive manager as risk checker for grid manager
	engine.gridManager.SetRiskChecker(adaptiveGridManager)

	// NEW: Set dynamic size calculator callback on GridManager
	if volumeConfig.Risk.DynamicSizing != nil && volumeConfig.Risk.DynamicSizing.Enabled {
		logger.Info("Setting dynamic size calculator callback on GridManager",
			zap.Bool("enabled", volumeConfig.Risk.DynamicSizing.Enabled))

		// Create dynamic size calculator function that uses RiskMonitor's smart sizing
		engine.gridManager.SetDynamicSizeCalculator(func(symbol string, baseSize float64, currentPrice float64) float64 {
			// Use AdaptiveGridManager's RiskMonitor GetSmartOrderSize with baseSize as input
			if adaptiveGridManager != nil {
				// Access riskMonitor via adaptiveGridManager
				riskMonitor := adaptiveGridManager.GetRiskMonitor()
				if riskMonitor != nil {
					return riskMonitor.GetSmartOrderSize(symbol, baseSize)
				}
			}
			return baseSize // Fallback to base size if RiskMonitor not available
		})
		logger.Info("Dynamic size calculator callback set on GridManager")
	} else {
		logger.Warn("Dynamic size calculator NOT set",
			zap.Bool("config_is_nil", volumeConfig.Risk.DynamicSizing == nil),
			zap.Bool("enabled", volumeConfig.Risk.DynamicSizing != nil && volumeConfig.Risk.DynamicSizing.Enabled))
	}

	// NEW: Set dynamic timeout config on AdaptiveGridManager
	if volumeConfig.Risk.DynamicTimeout != nil && volumeConfig.Risk.DynamicTimeout.Enabled {
		logger.Info("Setting dynamic timeout config on AdaptiveGridManager",
			zap.Bool("enabled", volumeConfig.Risk.DynamicTimeout.Enabled))
		adaptiveGridManager.SetDynamicTimeoutConfig(volumeConfig.Risk.DynamicTimeout)
	} else {
		logger.Warn("Dynamic timeout config NOT set",
			zap.Bool("config_is_nil", volumeConfig.Risk.DynamicTimeout == nil),
			zap.Bool("enabled", volumeConfig.Risk.DynamicTimeout != nil && volumeConfig.Risk.DynamicTimeout.Enabled))
	}

	// NEW: Set conditional transitions config on state machine
	if volumeConfig.Risk.ConditionalTransitions != nil && volumeConfig.Risk.ConditionalTransitions.Enabled {
		logger.Info("Setting conditional transitions config on state machine",
			zap.Bool("enabled", volumeConfig.Risk.ConditionalTransitions.Enabled))
		stateMachine := adaptiveGridManager.GetStateMachine()
		if stateMachine != nil {
			stateMachine.SetConditionalTransitionsConfig(volumeConfig.Risk.ConditionalTransitions)
		}
	} else {
		logger.Warn("Conditional transitions config NOT set",
			zap.Bool("config_is_nil", volumeConfig.Risk.ConditionalTransitions == nil),
			zap.Bool("enabled", volumeConfig.Risk.ConditionalTransitions != nil && volumeConfig.Risk.ConditionalTransitions.Enabled))
	}

	// NEW: Set graduated modes config on circuit breaker
	if volumeConfig.Risk.GraduatedModes != nil && volumeConfig.Risk.GraduatedModes.Enabled {
		logger.Info("Setting graduated modes config on circuit breaker",
			zap.Bool("enabled", volumeConfig.Risk.GraduatedModes.Enabled))
		if engine.circuitBreaker != nil {
			engine.circuitBreaker.SetGraduatedModesConfig(volumeConfig.Risk.GraduatedModes)
		}
	} else {
		logger.Warn("Graduated modes config NOT set",
			zap.Bool("config_is_nil", volumeConfig.Risk.GraduatedModes == nil),
			zap.Bool("enabled", volumeConfig.Risk.GraduatedModes != nil && volumeConfig.Risk.GraduatedModes.Enabled))
	}

	// NEW: Connect adaptive manager reference for optimization features
	engine.gridManager.SetAdaptiveManager(adaptiveGridManager)

	// NEW: Initialize Continuous Volume Farming components
	// ModeManager - evaluates and switches trading modes based on market conditions
	modeConfig := volumeConfig.TradingModes
	if modeConfig == nil {
		modeConfig = &config.TradingModesConfig{
			MicroMode:        config.MicroModeConfig{Enabled: true},
			StandardMode:     config.StandardModeConfig{Enabled: true},
			TrendAdaptedMode: config.TrendAdaptedModeConfig{Enabled: true},
		}
	}
	engine.modeManager = tradingmode.NewModeManager(modeConfig, logger)
	logger.Info("ModeManager initialized",
		zap.Bool("micro_enabled", modeConfig.MicroMode.Enabled),
		zap.Bool("standard_enabled", modeConfig.StandardMode.Enabled),
		zap.Bool("trend_enabled", modeConfig.TrendAdaptedMode.Enabled))

	// NEW: Initialize CircuitBreaker - unified trading decision brain
	cbConfig := cfg.Agentic.CircuitBreakers
	engine.circuitBreaker = agentic.NewCircuitBreaker(cbConfig, logger)
	logger.Info("CircuitBreaker initialized as unified trading brain")

	// CRITICAL: Start CircuitBreaker evaluation loop
	engine.circuitBreaker.Start()
	logger.Info("CircuitBreaker evaluation loop started")

	// NEW: Wire CircuitBreaker into AdaptiveGridManager for unified decisions
	adaptiveGridManager.SetCircuitBreaker(engine.circuitBreaker)
	logger.Info("CircuitBreaker wired into AdaptiveGridManager")

	// NEW: Initialize MetricsStreamer for real-time dashboard
	engine.metricsStreamer = dashboard.NewMetricsStreamer(logger)
	logger.Info("MetricsStreamer initialized for dashboard WebSocket")

	// Wire metricsStreamer to GridManager for broadcasting metrics
	engine.gridManager.SetMetricsStreamer(engine.metricsStreamer)
	logger.Info("MetricsStreamer wired to GridManager")

	// NEW: Wire CircuitBreaker callbacks for automatic actions
	engine.circuitBreaker.SetOnTripCallback(func(symbol, reason string) {
		logger.Warn("CircuitBreaker TRIPPED - Triggering emergency exit",
			zap.String("symbol", symbol),
			zap.String("reason", reason))
		// Trigger emergency exit for this symbol via state machine
		adaptiveGridManager.ExitAll(context.Background(), symbol, adaptive_grid.EventEmergencyExit, reason)
	})

	engine.circuitBreaker.SetOnResetCallback(func(symbol string) {
		logger.Info("CircuitBreaker RESET - Rebuilding grid",
			zap.String("symbol", symbol))
		// Rebuild grid when circuit breaker resets
		if engine.gridManager != nil {
			if err := engine.gridManager.RebuildGrid(context.Background(), symbol); err != nil {
				logger.Warn("Failed to rebuild grid after circuit breaker reset",
					zap.String("symbol", symbol),
					zap.Error(err))
			}
		}
	})

	engine.circuitBreaker.SetOnModeChangeCallback(func(symbol string, oldMode, newMode string) {
		logger.Info("Trading mode changed",
			zap.String("symbol", symbol),
			zap.String("old_mode", oldMode),
			zap.String("new_mode", newMode))
		// Could trigger grid rebuild or parameter adjustment based on mode change
	})
	logger.Info("CircuitBreaker callbacks wired")

	// NEW: Initialize RealTimeOptimizer for parameter optimization
	realtimeOptimizer := adaptive_grid.NewRealTimeOptimizer(logger)
	adaptiveGridManager.SetRealTimeOptimizer(realtimeOptimizer)
	logger.Info("RealTimeOptimizer initialized and set on AdaptiveGridManager")

	// NEW: Initialize LearningEngine for adaptive threshold learning
	learningEngine := adaptive_grid.NewLearningEngine(logger)
	adaptiveGridManager.SetLearningEngine(learningEngine)
	logger.Info("LearningEngine initialized and set on AdaptiveGridManager")

	// NEW: Initialize AdaptiveGridGeometry for adaptive spread/order count/spacing
	gridGeometry := adaptive_grid.NewAdaptiveGridGeometry(logger)
	engine.gridManager.SetGridGeometry(gridGeometry)
	logger.Info("AdaptiveGridGeometry initialized and set on GridManager")

	// NEW: Load and set partial close configuration
	if volumeConfig.PartialClose != nil && volumeConfig.PartialClose.Enabled {
		partialCloseConfig := &adaptive_grid.PartialCloseConfig{
			Enabled:          volumeConfig.PartialClose.Enabled,
			TP1_ClosePct:     volumeConfig.PartialClose.TP1.ClosePct,
			TP1_ProfitPct:    volumeConfig.PartialClose.TP1.ProfitPct,
			TP2_ClosePct:     volumeConfig.PartialClose.TP2.ClosePct,
			TP2_ProfitPct:    volumeConfig.PartialClose.TP2.ProfitPct,
			TP3_ClosePct:     volumeConfig.PartialClose.TP3.ClosePct,
			TP3_ProfitPct:    volumeConfig.PartialClose.TP3.ProfitPct,
			TrailingAfterTP2: volumeConfig.PartialClose.TrailingAfterTP2,
			TrailingDistance: volumeConfig.PartialClose.TrailingDistance,
		}
		adaptiveGridManager.SetPartialCloseConfig(partialCloseConfig)
		logger.Info("Partial close configuration loaded and set",
			zap.Bool("enabled", partialCloseConfig.Enabled),
			zap.Float64("tp1_close_pct", partialCloseConfig.TP1_ClosePct),
			zap.Float64("tp1_profit_pct", partialCloseConfig.TP1_ProfitPct),
			zap.Float64("tp2_close_pct", partialCloseConfig.TP2_ClosePct),
			zap.Float64("tp2_profit_pct", partialCloseConfig.TP2_ProfitPct),
			zap.Float64("tp3_close_pct", partialCloseConfig.TP3_ClosePct),
			zap.Float64("tp3_profit_pct", partialCloseConfig.TP3_ProfitPct))
	} else {
		logger.Info("Partial close configuration not enabled or not provided")
	}

	// ExitExecutor - handles fast exit on breakouts
	engine.exitExecutor = NewExitExecutor(engine.futuresClient, sharedWSClient, 5*time.Second, logger)
	logger.Info("ExitExecutor initialized", zap.Duration("timeout", 5*time.Second))

	// NEW: Wire ExitExecutor into AdaptiveGridManager for breakout handling
	adaptiveGridManager.SetExitExecutor(engine.exitExecutor)
	logger.Info("ExitExecutor wired into AdaptiveGridManager")

	// NEW: Wire onOrderPlaced callback to update SyncManager
	engine.gridManager.SetOnOrderPlacedCallback(func(symbol string, order client.Order) {
		if engine.syncManager != nil {
			engine.syncManager.UpdateOrder(symbol, order)
		}
	})

	// NEW: Wire ModeManager into AdaptiveGridManager for trading mode evaluation
	adaptiveGridManager.SetModeManager(engine.modeManager)
	logger.Info("ModeManager wired into AdaptiveGridManager")

	// NEW: Register COOLDOWN callback to trigger exit sequence
	engine.modeManager.SetOnCooldownCallback(func(reason string) {
		logger.Warn("COOLDOWN mode triggered - initiating emergency exit for all symbols",
			zap.String("reason", reason))

		// Get all active symbols from adaptive grid manager
		symbols := adaptiveGridManager.GetActiveSymbols()

		// Trigger exit sequence for each symbol
		for _, symbol := range symbols {
			logger.Info("Triggering exit sequence for symbol due to COOLDOWN",
				zap.String("symbol", symbol),
				zap.String("reason", reason))

			// Use ExitExecutor to cancel orders and close positions
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			sequence := engine.exitExecutor.ExecuteFastExit(ctx, symbol)
			if sequence.Error != nil {
				logger.Error("Failed to execute fast exit during COOLDOWN",
					zap.String("symbol", symbol),
					zap.Error(sequence.Error))
			} else {
				logger.Info("Fast exit completed during COOLDOWN",
					zap.String("symbol", symbol),
					zap.Int("orders_cancelled", sequence.OrdersCancelled),
					zap.Int("positions_closed", sequence.PositionsClosed))
			}
		}

		// Schedule force grid placement after cooldown expires (10s)
		go func() {
			time.Sleep(10 * time.Second)
			logger.Info("COOLDOWN expired - forcing grid placement for all symbols")
			symbols := adaptiveGridManager.GetActiveSymbols()
			for _, symbol := range symbols {
				logger.Info("Force triggering grid placement after COOLDOWN",
					zap.String("symbol", symbol))
				engine.gridManager.enqueuePlacement(symbol)
			}
		}()
	})
	logger.Info("COOLDOWN callback registered to trigger exit sequence and force placement after expiry")

	// SyncManager - coordinates order/position/balance sync workers
	engine.syncManager = farmsync.NewSyncManager(sharedWSClient, logger)
	engine.syncManager.Initialize()

	// Wire up order missing callback to trigger GridManager handleOrderFill
	engine.syncManager.SetOnOrderMissingCallback(func(symbol string, orderID int64) {
		logger.Info("Sync worker detected missing order, triggering fill handler",
			zap.String("symbol", symbol),
			zap.Int64("order_id", orderID))

		// Trigger GridManager's handleOrderFill to process the fill and rebalance
		if engine.gridManager != nil {
			orderIDStr := strconv.FormatInt(orderID, 10)
			engine.gridManager.handleOrderFill(orderIDStr, symbol)
		}
	})

	logger.Info("SyncManager initialized with all workers")

	// Store WebSocket client reference
	engine.wsClient = sharedWSClient

	return engine, nil
}

// Start starts the volume farming engine.
func (e *VolumeFarmEngine) Start(ctx context.Context) error {
	e.isRunningMu.Lock()
	if e.isRunning {
		e.isRunningMu.Unlock()
		return fmt.Errorf("volume farming engine is already running")
	}
	e.isRunning = true
	e.isRunningMu.Unlock()

	e.logger.Info("Starting Volume Farming Engine")

	// Start WebSocket server for dashboard
	go e.startWebSocketServer()

	// Bridge context cancellation to stopCh
	// This ensures that when context is cancelled (e.g., from signal), stopCh is also closed
	go func() {
		select {
		case <-ctx.Done():
			select {
			case <-e.stopCh:
				// already closed
			default:
				close(e.stopCh)
			}
		case <-e.stopCh:
			// already closed, nothing to do
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.symbolSelector.Start(ctx); err != nil {
			e.logger.Error("Symbol selector error", zap.Error(err))
		}
	}()

	// CRITICAL: Initial symbol sync MUST complete before starting grid manager
	// to ensure activeGrids is populated before rebalancer worker starts
	e.logger.Info("Initial symbol sync before starting grid manager...")
	e.syncGridSymbols()
	e.logger.Info("Initial symbol sync complete - starting grid manager")

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.syncGridSymbols()
			}
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.gridManager.Start(ctx); err != nil {
			e.logger.Error("Grid manager error", zap.Error(err))
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.logger.Info("Adaptive grid manager goroutine STARTED")
		e.logger.Info("=== ADAPTIVE GRID MANAGER INITIALIZATION START ===")

		// CRITICAL: Load optimization config BEFORE Initialize
		// Priority 1: Use unified config if available (from agentic-vf-config.yaml)
		// Priority 2: Load from separate files in config/ directory
		var optConfig *config.OptimizationConfig

		if e.config.Optimization != nil {
			// Use optimization config from unified config file
			optConfig = e.config.Optimization
			e.logger.Info("Using optimization config from unified config file",
				zap.Bool("micro_grid_enabled", optConfig.MicroGrid != nil && optConfig.MicroGrid.Enabled),
				zap.Bool("fast_range_enabled", optConfig.FastRange != nil && optConfig.FastRange.Enabled),
				zap.Bool("adx_filter_enabled", optConfig.ADXFilter != nil && optConfig.ADXFilter.Enabled),
				zap.Bool("dynamic_leverage_enabled", optConfig.DynamicLeverage != nil && optConfig.DynamicLeverage.Enabled),
				zap.Bool("multi_layer_liq_enabled", optConfig.MultiLayerLiq != nil && optConfig.MultiLayerLiq.Enabled))
		} else {
			// Fall back to loading from separate files
			configDir := "config" // Default config directory
			var err error
			optConfig, err = config.LoadOptimizationConfig(configDir)
			if err != nil {
				e.logger.Warn("Failed to load optimization config from files, using defaults",
					zap.Error(err),
					zap.String("config_dir", configDir))
			} else {
				e.logger.Info("Optimization config loaded from separate files",
					zap.Bool("time_filter_enabled", optConfig.TimeFilter != nil && optConfig.TimeFilter.Enabled),
					zap.Int("time_slots", len(optConfig.TimeFilter.Slots)))
			}
		}

		// Pass config to adaptive grid manager BEFORE Initialize
		if optConfig != nil {
			e.adaptiveGridManager.SetOptimizationConfig(optConfig)
		}

		e.adaptiveGridManager.InitializeDynamicLeverage(nil)

		// Initialize adaptive grid manager
		e.logger.Info("Calling adaptiveGridManager.Initialize()...")
		if err := e.adaptiveGridManager.Initialize(ctx); err != nil {
			e.logger.Error("Adaptive grid manager initialization error", zap.Error(err))
			return
		}
		e.logger.Info("Adaptive grid manager initialized - position monitor ACTIVE")

		// Start regime monitoring
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.monitorRegimeChanges(ctx)
			}
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.pointsTracker.Start(ctx); err != nil {
			e.logger.Error("Points tracker error", zap.Error(err))
		}
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.monitorRisk(ctx)
	}()

	// NEW: Initialize and start UserStream for real-time order updates
	e.logger.Info("About to call initUserStream")
	e.initUserStream(ctx)
	e.logger.Info("initUserStream call completed")

	// NEW: Start SyncManager for order/position/balance sync
	if e.syncManager != nil {
		e.syncManager.Start()
		e.logger.Info("SyncManager started")
	}

	e.logger.Info("Volume Farming Engine started successfully")
	return nil
}

// initUserStream initializes and starts the user data stream WebSocket
func (e *VolumeFarmEngine) initUserStream(ctx context.Context) {
	e.logger.Info("=== initUserStream CALLED ===")

	wsBase := e.config.Exchange.FuturesWSBase
	if wsBase == "" {
		wsBase = "wss://fstream.asterdex.com"
	}

	e.logger.Info("Initializing UserStream",
		zap.String("ws_base", wsBase),
		zap.Bool("futures_client_nil", e.futuresClient == nil))

	if e.futuresClient == nil {
		e.logger.Error("FuturesClient is nil, cannot initialize UserStream")
		return
	}

	userStream := stream.NewUserStream(
		wsBase,
		e.futuresClient.StartListenKey,
		e.futuresClient.KeepaliveListenKey,
		stream.UserStreamHandlers{
			OnOrderUpdate: func(u stream.WsOrderUpdate) {
				// Convert WsOrderUpdate to local OrderUpdate
				orderUpdate := &OrderUpdate{
					Symbol:    u.Order.Symbol,
					OrderID:   strconv.FormatInt(u.Order.OrderID, 10),
					ClientID:  u.Order.ClientOrderID,
					Side:      u.Order.Side,
					Type:      u.Order.OrderType,
					Price:     u.Order.Price,
					Quantity:  u.Order.FilledQty,
					Status:    u.Order.OrderStatus,
					Fee:       0, // Fee not available in WsOrderUpdate
					Timestamp: u.EventTime,
				}
				// Process all order status updates for proper sync
				e.gridManager.OnOrderUpdate(orderUpdate)
			},
			OnAccountUpdate: func(u stream.WsAccountUpdate) {
				// Update WebSocket cache with positions and balances
				e.logger.Info("Account update received via WebSocket",
					zap.Int("positions", len(u.Update.Positions)),
					zap.Int("balances", len(u.Update.Balances)))

				// Update GridManager's position cache directly to keep it in sync
				if e.gridManager != nil {
					e.gridManager.OnAccountUpdate(u)
				}

				// Also update wsClient cache for other consumers
				for _, pos := range u.Update.Positions {
					position := client.Position{
						Symbol:           pos.Symbol,
						PositionAmt:      pos.PositionAmt,
						EntryPrice:       pos.EntryPrice,
						MarkPrice:        pos.EntryPrice, // Use EntryPrice as fallback
						UnrealizedProfit: pos.UnrealizedPnL,
						PositionSide:     pos.PositionSide,
					}
					e.wsClient.UpdatePositionCache(position)
				}

				// Update balance cache
				for _, bal := range u.Update.Balances {
					if bal.Asset == "USDT" || bal.Asset == "USD1" {
						balance := client.Balance{
							Asset:            bal.Asset,
							AvailableBalance: bal.CrossWalletBalance, // Use CrossWalletBalance as available
							WalletBalance:    bal.WalletBalance,
							MarginBalance:    bal.WalletBalance, // Use WalletBalance as margin
						}
						e.wsClient.UpdateBalanceCache(balance)
					}
				}
			},
		},
		e.logger,
	)
	e.userStream = userStream

	// Start user stream in background
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.logger.Info("Starting UserStream for real-time order updates")
		userStream.Run(ctx)
		e.logger.Info("UserStream stopped")
	}()
}
func (e *VolumeFarmEngine) syncGridSymbols() {
	e.logger.Info("=== SYNC GRID SYMBOLS START ===")

	activeSymbols := e.symbolSelector.GetActiveSymbols()
	e.logger.Info("Active symbols from selector",
		zap.Int("count", len(activeSymbols)),
		zap.Strings("symbols", func() []string {
			syms := make([]string, len(activeSymbols))
			for i, s := range activeSymbols {
				syms[i] = s.Symbol
			}
			return syms
		}()))

	// Get whitelist from symbol selector (updated by Agentic layer)
	whitelist := e.symbolSelector.GetWhitelist()
	e.logger.Info("Whitelist from selector",
		zap.Int("count", len(whitelist)),
		zap.Strings("whitelist", whitelist))

	// FALLBACK: If agentic whitelist is empty, use config whitelist
	if len(whitelist) == 0 && len(e.volumeConfig.Symbols.Whitelist) > 0 {
		whitelist = e.volumeConfig.Symbols.Whitelist
		e.logger.Info("Using config whitelist (agentic whitelist empty)", zap.Strings("whitelist", whitelist))
	}

	// Always include whitelist symbols
	if len(whitelist) > 0 {
		whitelistSymbols := make(map[string]bool)
		for _, sym := range whitelist {
			whitelistSymbols[sym] = true
		}
		for _, s := range activeSymbols {
			delete(whitelistSymbols, s.Symbol)
		}
		// Add remaining whitelist
		for sym := range whitelistSymbols {
			quote := ""
			for _, qc := range e.volumeConfig.Symbols.QuoteCurrencies {
				if len(sym) > len(qc) && sym[len(sym)-len(qc):] == qc {
					quote = qc
					break
				}
			}
			if quote != "" {
				base := sym[:len(sym)-len(quote)]
				activeSymbols = append(activeSymbols, &SymbolData{
					Symbol:     sym,
					BaseAsset:  base,
					QuoteAsset: quote,
					Status:     "TRADING",
					Volume24h:  1000000,
					Count24h:   1000,
				})
			}
		}
		e.logger.Info("Added whitelist to active symbols", zap.Int("total_count", len(activeSymbols)))
	}

	if len(activeSymbols) == 0 && len(whitelist) > 0 {
		e.logger.Info("No symbols from selector, adding whitelist symbols only")
		// Only add whitelist symbols with USD1 quote - NO USDT
		whitelistOnly := e.createWhitelistSymbolsFromList(whitelist)
		activeSymbols = append(activeSymbols, whitelistOnly...)
	}

	e.logger.Info("Final active symbols before UpdateSymbols",
		zap.Int("count", len(activeSymbols)),
		zap.Strings("symbols", func() []string {
			syms := make([]string, len(activeSymbols))
			for i, s := range activeSymbols {
				syms[i] = s.Symbol
			}
			return syms
		}()))

	// NEW: Set correct leverage for each symbol from exchange info
	e.setLeverageForSymbols(activeSymbols)

	e.logger.Info("Updating grid manager with symbols", zap.Int("count", len(activeSymbols)), zap.Strings("symbols", func() []string {
		syms := make([]string, len(activeSymbols))
		for i, s := range activeSymbols {
			syms[i] = s.Symbol
		}
		return syms
	}()))
	e.gridManager.UpdateSymbols(activeSymbols)

	e.logger.Info("=== SYNC GRID SYMBOLS END ===")
}

// setLeverageForSymbols sets the correct leverage for each symbol based on exchange info
func (e *VolumeFarmEngine) setLeverageForSymbols(symbols []*SymbolData) {
	// CRITICAL: Ensure exchange info is loaded before checking leverage
	// This may be called before grid manager Start() completes
	if e.gridManager.precisionMgr.GetMaxLeverage("BTCUSD1") == 0 {
		e.logger.Info("Exchange info not loaded yet, fetching directly for leverage setup")
		marketClient := client.NewMarketClient(e.futuresClient.GetHTTPClient())
		exchangeInfo, err := marketClient.ExchangeInfo(context.Background())
		if err != nil {
			e.logger.Warn("Failed to fetch exchange info for leverage setup", zap.Error(err))
		} else {
			exchangeInfoBytes := []byte(exchangeInfo)
			e.gridManager.precisionMgr.UpdateFromExchangeInfo(exchangeInfoBytes)
			e.logger.Info("Exchange info loaded for leverage setup", zap.Int("bytes", len(exchangeInfoBytes)))
		}
	}

	for _, sym := range symbols {
		maxLeverage := e.gridManager.precisionMgr.GetMaxLeverage(sym.Symbol)
		if maxLeverage > 0 {
			e.logger.Info("Setting leverage for symbol",
				zap.String("symbol", sym.Symbol),
				zap.Float64("max_leverage", maxLeverage))

			// CRITICAL: Use dynamic leverage based on BB/ADX market conditions
			// Falls back to 80% of max if dynamic calculator not available
			calculatedLeverage := maxLeverage * 0.8
			if e.adaptiveGridManager != nil {
				calculatedLeverage = e.adaptiveGridManager.GetOptimalLeverage()
				e.logger.Info("Dynamic leverage calculated",
					zap.String("symbol", sym.Symbol),
					zap.Float64("calculated_leverage", calculatedLeverage),
					zap.Float64("max_leverage", maxLeverage))
			}

			// Clamp to max leverage and ensure minimum of 1
			targetLeverage := int(calculatedLeverage)
			if targetLeverage > int(maxLeverage) {
				targetLeverage = int(maxLeverage)
			}
			if targetLeverage < 1 {
				targetLeverage = 1
			}

			if err := e.futuresClient.SetLeverage(context.Background(), client.SetLeverageRequest{
				Symbol:   sym.Symbol,
				Leverage: targetLeverage,
			}); err != nil {
				e.logger.Warn("Failed to set leverage",
					zap.String("symbol", sym.Symbol),
					zap.Error(err))
			} else {
				e.logger.Info("Leverage set successfully",
					zap.String("symbol", sym.Symbol),
					zap.Int("leverage", targetLeverage))
			}
		} else {
			e.logger.Warn("Max leverage unknown for symbol, skipping leverage set",
				zap.String("symbol", sym.Symbol))
		}
	}
}

func (e *VolumeFarmEngine) createWhitelistSymbols() []*SymbolData {
	// Use whitelist from symbol selector (updated by Agentic layer)
	whitelist := e.symbolSelector.GetWhitelist()
	return e.createWhitelistSymbolsFromList(whitelist)
}

func (e *VolumeFarmEngine) createWhitelistSymbolsFromList(whitelist []string) []*SymbolData {
	symbols := make([]*SymbolData, 0, len(whitelist))
	for _, symbol := range whitelist {
		// Extract quote currency
		quote := ""
		for _, qc := range e.volumeConfig.Symbols.QuoteCurrencies {
			if len(symbol) > len(qc) && symbol[len(symbol)-len(qc):] == qc {
				quote = qc
				break
			}
		}
		if quote == "" {
			e.logger.Warn("Cannot extract quote currency for whitelist symbol", zap.String("symbol", symbol))
			continue
		}

		// Extract base asset
		base := symbol[:len(symbol)-len(quote)]

		symbols = append(symbols, &SymbolData{
			Symbol:     symbol,
			BaseAsset:  base,
			QuoteAsset: quote,
			Status:     "TRADING",
			Volume24h:  1000000, // Dummy volume
			Count24h:   1000,
		})
		e.logger.Info("Created whitelist symbol", zap.String("symbol", symbol), zap.String("base", base), zap.String("quote", quote))
	}
	return symbols
}

// Stop stops the volume farming engine.
func (e *VolumeFarmEngine) Stop(ctx context.Context) error {
	e.isRunningMu.Lock()
	if !e.isRunning {
		e.isRunningMu.Unlock()
		return nil
	}
	e.isRunning = false
	e.isRunningMu.Unlock()

	e.logger.Info("Stopping Volume Farming Engine")

	// Safely close stopCh (may already be closed by bridge goroutine)
	select {
	case <-e.stopCh:
		// already closed
	default:
		close(e.stopCh)
	}

	if e.symbolSelector != nil {
		if err := e.symbolSelector.Stop(ctx); err != nil {
			e.logger.Warn("Symbol selector stop error", zap.Error(err))
		}
	}
	if e.gridManager != nil {
		if err := e.gridManager.Stop(ctx); err != nil {
			e.logger.Warn("Grid manager stop error", zap.Error(err))
		}
	}
	if e.pointsTracker != nil {
		if err := e.pointsTracker.Stop(ctx); err != nil {
			e.logger.Warn("Points tracker stop error", zap.Error(err))
		}
	}

	// NEW: Stop SyncManager
	if e.syncManager != nil {
		e.syncManager.Stop()
		e.logger.Info("SyncManager stopped")
	}

	// NEW: Stop CircuitBreaker evaluation loop
	if e.circuitBreaker != nil {
		e.circuitBreaker.Stop()
		e.logger.Info("CircuitBreaker evaluation loop stopped")
	}

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		e.logger.Info("Volume Farming Engine stopped gracefully")
		return nil
	case <-ctx.Done():
		e.logger.Warn("Volume Farming Engine stop timeout")
		return ctx.Err()
	}
}

// IsRunning returns whether the engine is running.
func (e *VolumeFarmEngine) IsRunning() bool {
	e.isRunningMu.RLock()
	defer e.isRunningMu.RUnlock()
	return e.isRunning
}

// GetStatus returns the current status.
func (e *VolumeFarmEngine) GetStatus() *VolumeFarmStatus {
	status := &VolumeFarmStatus{
		IsRunning:     e.IsRunning(),
		ActiveSymbols: e.symbolSelector.GetActiveSymbolCount(),
		ActiveGrids:   0,
		CurrentPoints: e.pointsTracker.GetCurrentPoints(),
		CurrentVolume: e.pointsTracker.GetCurrentVolume(),
		DailyLoss:     e.riskManager.DailyPnL(),
		RiskStatus: func() string {
			if e.riskManager.IsPaused() {
				return "PAUSED"
			}
			return "ACTIVE"
		}(),
		LastUpdate: time.Now(),
	}

	// Add micro profit metrics if grid manager has take profit manager
	if e.gridManager != nil {
		status.MicroProfitMetrics = e.gridManager.GetTakeProfitMetrics()
	}

	return status
}

// monitorRegimeChanges monitors and handles regime changes for all symbols
func (e *VolumeFarmEngine) monitorRegimeChanges(ctx context.Context) {
	_ = ctx
	e.logger.Debug("Regime monitoring delegated to AdaptiveGridManager range/ADX state machine")
}

// monitorRisk monitors risk levels and takes action.
func (e *VolumeFarmEngine) monitorRisk(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			dailyPnL := e.riskManager.DailyPnL()
			isPaused := e.riskManager.IsPaused()
			availableBalance := e.riskManager.GetAvailableBalance()
			dailyStartingEquity := e.riskManager.GetDailyStartingEquity()

			e.logger.Info("Risk Monitor Status",
				zap.Float64("daily_pnl", dailyPnL),
				zap.Bool("is_paused", isPaused),
				zap.Float64("available_balance", availableBalance),
				zap.Float64("daily_starting_equity", dailyStartingEquity),
				zap.Float64("daily_loss_limit", e.volumeConfig.Risk.DailyLossLimitUSDT))

			if dailyPnL < -e.volumeConfig.Risk.DailyLossLimitUSDT {
				e.logger.Warn("Daily loss limit reached, stopping farming",
					zap.Float64("daily_loss", dailyPnL),
					zap.Float64("limit", e.volumeConfig.Risk.DailyLossLimitUSDT))
			}

			if isPaused {
				e.logger.Warn("Risk manager paused, stopping farming")
			}

			if availableBalance <= 0 {
				e.logger.Warn("Available balance is 0, cannot place new orders",
					zap.Float64("available_balance", availableBalance))
			}
		}
	}
}

// extractVolumeFarmConfig extracts volume farming config from main config.
func (e *VolumeFarmEngine) extractVolumeFarmConfig(cfg *config.Config) *config.VolumeFarmConfig {
	if cfg.VolumeFarming == nil {
		return &config.VolumeFarmConfig{
			Enabled:                  true,
			MaxDailyLossUSDT:         200,  // Higher limit for volume farming
			MaxTotalDrawdownPct:      15.0, // More tolerance for volume
			OrderSizeUSDT:            5,    // Small orders for high volume
			GridSpreadPct:            0.01, // Tight spread
			MaxOrdersPerSide:         30,   // 30 orders per side
			ReplaceImmediately:       true, // Immediate replacement
			PositionTimeoutMinutes:   10,   // Fast turnover
			SymbolRefreshIntervalSec: 30,   // Fast symbol refresh
			GridPlacementCooldownSec: 1,    // 1 second cooldown
			RateLimitCooldownSec:     3,    // Quick recovery
			SupportedQuoteCurrencies: []string{"USD1"},
			MinVolume24h:             500, // Lower threshold
			Bot:                      cfg.Bot,
			Exchange:                 cfg.Exchange,
			Risk:                     cfg.Risk,
			API:                      cfg.API,
			Symbols: config.SymbolsConfig{
				QuoteCurrencies:    []string{"USD1"},
				MaxSymbolsPerQuote: 20,  // More symbols
				MinVolume24h:       500, // Lower volume threshold
			},
			VolumeOptimization: config.DefaultVolumeOptimizationConfig(),
		}
	}

	e.logger.Info("Extracted volume farming config",
		zap.Bool("volume_farming_enabled", cfg.VolumeFarming.Enabled),
		zap.String("quote_currency_mode", cfg.VolumeFarming.Symbols.QuoteCurrencyMode),
		zap.Strings("quote_currencies", cfg.VolumeFarming.Symbols.QuoteCurrencies),
		zap.Float64("min_volume_24h", cfg.VolumeFarming.Symbols.MinVolume24h),
		zap.Strings("whitelist", cfg.VolumeFarming.Symbols.Whitelist),
	)

	volumeConfig := &config.VolumeFarmConfig{
		Enabled:                  cfg.VolumeFarming.Enabled,
		MaxDailyLossUSDT:         cfg.VolumeFarming.MaxDailyLossUSDT,
		MaxTotalDrawdownPct:      cfg.VolumeFarming.MaxTotalDrawdownPct,
		OrderSizeUSDT:            cfg.VolumeFarming.OrderSizeUSDT,
		GridSpreadPct:            cfg.VolumeFarming.GridSpreadPct,
		MaxOrdersPerSide:         cfg.VolumeFarming.MaxOrdersPerSide,
		ReplaceImmediately:       cfg.VolumeFarming.ReplaceImmediately,
		PositionTimeoutMinutes:   cfg.VolumeFarming.PositionTimeoutMinutes,
		TickerStream:             cfg.VolumeFarming.TickerStream,
		SymbolRefreshIntervalSec: cfg.VolumeFarming.SymbolRefreshIntervalSec,
		GridPlacementCooldownSec: cfg.VolumeFarming.GridPlacementCooldownSec,
		RateLimitCooldownSec:     cfg.VolumeFarming.RateLimitCooldownSec,
		SupportedQuoteCurrencies: append([]string{}, cfg.VolumeFarming.SupportedQuoteCurrencies...),
		MinVolume24h:             cfg.VolumeFarming.MinVolume24h,
		Bot:                      cfg.Bot,
		Symbols:                  cfg.VolumeFarming.Symbols,
		Exchange:                 cfg.Exchange,
		Risk:                     cfg.Risk,
		API:                      cfg.API,
		VolumeOptimization:       cfg.VolumeFarming.VolumeOptimization,
	}

	if volumeConfig.MinVolume24h <= 0 {
		volumeConfig.MinVolume24h = volumeConfig.Symbols.MinVolume24h
	}
	// Only apply defaults if still zero after using Symbols.MinVolume24h
	if volumeConfig.MinVolume24h <= 0 {
		volumeConfig.MinVolume24h = 1000 // Use config file default
	}
	if len(volumeConfig.SupportedQuoteCurrencies) == 0 {
		volumeConfig.SupportedQuoteCurrencies = append([]string{}, volumeConfig.Symbols.QuoteCurrencies...)
	}
	if len(volumeConfig.SupportedQuoteCurrencies) == 0 {
		volumeConfig.SupportedQuoteCurrencies = []string{"USD1"}
	}
	if len(volumeConfig.Symbols.QuoteCurrencies) == 0 {
		volumeConfig.Symbols.QuoteCurrencies = append([]string{}, volumeConfig.SupportedQuoteCurrencies...)
	}
	if volumeConfig.TickerStream == "" {
		volumeConfig.TickerStream = "!ticker@arr"
	}
	if volumeConfig.SymbolRefreshIntervalSec <= 0 {
		volumeConfig.SymbolRefreshIntervalSec = 60 // Use config file default (60s not 30s)
	}
	if volumeConfig.GridPlacementCooldownSec < 0 { // Allow 0 for no cooldown
		volumeConfig.GridPlacementCooldownSec = 0
	}
	if volumeConfig.RateLimitCooldownSec <= 0 {
		volumeConfig.RateLimitCooldownSec = 3
	}
	if volumeConfig.Symbols.MaxSymbolsPerQuote <= 0 {
		volumeConfig.Symbols.MaxSymbolsPerQuote = 1 // Use config file default (1 not 20)
	}
	// Ensure risk values from volume-farm-config are properly set
	if volumeConfig.Risk.MaxPositionUSDTPerSymbol <= 0 && cfg.VolumeFarming.Risk.MaxPositionUSDTPerSymbol > 0 {
		volumeConfig.Risk.MaxPositionUSDTPerSymbol = cfg.VolumeFarming.Risk.MaxPositionUSDTPerSymbol
	}
	if volumeConfig.Risk.MaxTotalPositionsUSDT <= 0 && cfg.VolumeFarming.Risk.MaxTotalPositionsUSDT > 0 {
		volumeConfig.Risk.MaxTotalPositionsUSDT = cfg.VolumeFarming.Risk.MaxTotalPositionsUSDT
	}
	if volumeConfig.Risk.DailyLossLimitUSDT <= 0 && cfg.VolumeFarming.Risk.DailyLossLimitUSDT > 0 {
		volumeConfig.Risk.DailyLossLimitUSDT = cfg.VolumeFarming.Risk.DailyLossLimitUSDT
	}
	if volumeConfig.Risk.MaxOpenPositions <= 0 && cfg.VolumeFarming.Risk.MaxOpenPositions > 0 {
		volumeConfig.Risk.MaxOpenPositions = cfg.VolumeFarming.Risk.MaxOpenPositions
	}
	if volumeConfig.Risk.PerTradeStopLossPct <= 0 && cfg.VolumeFarming.Risk.PerTradeStopLossPct > 0 {
		volumeConfig.Risk.PerTradeStopLossPct = cfg.VolumeFarming.Risk.PerTradeStopLossPct
	}
	if volumeConfig.Risk.PerTradeTakeProfitPct <= 0 && cfg.VolumeFarming.Risk.PerTradeTakeProfitPct > 0 {
		volumeConfig.Risk.PerTradeTakeProfitPct = cfg.VolumeFarming.Risk.PerTradeTakeProfitPct
	}
	// Log final applied config values
	e.logger.Info("Final volume farming config applied",
		zap.Float64("order_size_usdt", volumeConfig.OrderSizeUSDT),
		zap.Float64("grid_spread_pct", volumeConfig.GridSpreadPct),
		zap.Int("max_orders_per_side", volumeConfig.MaxOrdersPerSide),
		zap.Int("position_timeout_minutes", volumeConfig.PositionTimeoutMinutes),
		zap.Int("symbol_refresh_interval_sec", volumeConfig.SymbolRefreshIntervalSec),
		zap.Int("grid_placement_cooldown_sec", volumeConfig.GridPlacementCooldownSec),
		zap.Float64("risk_max_position_usdt", volumeConfig.Risk.MaxPositionUSDTPerSymbol),
		zap.Float64("risk_daily_loss_limit", volumeConfig.Risk.DailyLossLimitUSDT),
		zap.Int("risk_max_open_positions", volumeConfig.Risk.MaxOpenPositions),
		zap.Float64("risk_per_trade_sl_pct", volumeConfig.Risk.PerTradeStopLossPct),
	)

	return volumeConfig
}

// VolumeFarmStatus represents the current status.
type VolumeFarmStatus struct {
	IsRunning          bool                   `json:"is_running"`
	ActiveSymbols      int                    `json:"active_symbols"`
	ActiveGrids        int                    `json:"active_grids"`
	CurrentPoints      int64                  `json:"current_points"`
	CurrentVolume      float64                `json:"current_volume"`
	DailyLoss          float64                `json:"daily_loss"`
	RiskStatus         string                 `json:"risk_status"`
	LastUpdate         time.Time              `json:"last_update"`
	MicroProfitMetrics map[string]interface{} `json:"micro_profit_metrics,omitempty"`
}

// UpdateWhitelist updates the whitelist for symbol selection (called by Agentic layer)
func (e *VolumeFarmEngine) UpdateWhitelist(symbols []string) error {
	e.logger.Info("Updating whitelist from Agentic layer",
		zap.Strings("symbols", symbols))

	// Update the symbol selector whitelist
	e.symbolSelector.SetWhitelist(symbols)

	// Trigger a resync of grid symbols
	e.syncGridSymbols()

	return nil
}

// GetActivePositions returns the current active positions across all symbols
func (e *VolumeFarmEngine) GetActivePositions() ([]agentic.PositionStatus, error) {
	if e.adaptiveGridManager == nil {
		return nil, nil
	}

	tracked := e.adaptiveGridManager.GetAllPositions()
	positions := make([]agentic.PositionStatus, 0, len(tracked))
	for symbol, pos := range tracked {
		if pos == nil {
			continue
		}

		side := ""
		if pos.PositionAmt > 0 {
			side = "LONG"
		} else if pos.PositionAmt < 0 {
			side = "SHORT"
		}

		positions = append(positions, agentic.PositionStatus{
			Symbol:        symbol,
			HasPosition:   pos.PositionAmt != 0,
			Size:          math.Abs(pos.PositionAmt),
			UnrealizedPnL: pos.UnrealizedPnL,
			Side:          side,
		})
	}

	return positions, nil
}

// TriggerEmergencyExit triggers fast exit for all active symbols when circuit breaker trips
func (e *VolumeFarmEngine) TriggerEmergencyExit(reason string) error {
	e.logger.Error("EMERGENCY EXIT TRIGGERED - Closing ALL positions immediately",
		zap.String("reason", reason))

	if e.exitExecutor == nil {
		e.logger.Error("ExitExecutor not available, cannot trigger emergency exit")
		return fmt.Errorf("exitExecutor not available")
	}

	if e.adaptiveGridManager == nil {
		e.logger.Error("AdaptiveGridManager not available, cannot get active symbols")
		return fmt.Errorf("adaptiveGridManager not available")
	}

	// Get all active symbols
	tracked := e.adaptiveGridManager.GetAllPositions()
	activeSymbols := make([]string, 0)
	for symbol, pos := range tracked {
		if pos != nil && pos.PositionAmt != 0 {
			activeSymbols = append(activeSymbols, symbol)
		}
	}

	if len(activeSymbols) == 0 {
		e.logger.Info("No active positions to close")
		return nil
	}

	e.logger.Warn("Triggering emergency exit for active symbols",
		zap.Strings("symbols", activeSymbols),
		zap.Int("count", len(activeSymbols)))

	// Execute fast exit for each symbol in parallel
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make([]error, 0)

	for _, symbol := range activeSymbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			sequence := e.exitExecutor.ExecuteFastExit(ctx, sym)
			if sequence.Error != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("%s: %w", sym, sequence.Error))
				mu.Unlock()
				e.logger.Error("Failed to execute fast exit",
					zap.String("symbol", sym),
					zap.Error(sequence.Error))
			} else {
				e.logger.Info("Fast exit executed successfully",
					zap.String("symbol", sym),
					zap.Duration("duration", sequence.Duration))
			}
		}(symbol)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("emergency exit completed with %d errors: %v", len(errors), errors)
	}

	e.logger.Info("Emergency exit completed successfully",
		zap.Int("symbols_closed", len(activeSymbols)))

	return nil
}

// TriggerForcePlacement triggers force placement for all active symbols when circuit breaker resets
func (e *VolumeFarmEngine) TriggerForcePlacement() error {
	e.logger.Info("FORCE PLACEMENT TRIGGERED - Circuit breaker reset, resuming trading")

	if e.adaptiveGridManager == nil {
		e.logger.Error("AdaptiveGridManager not available, cannot trigger force placement")
		return fmt.Errorf("adaptiveGridManager not available")
	}

	if e.modeManager == nil {
		e.logger.Error("ModeManager not available, cannot trigger force placement")
		return fmt.Errorf("modeManager not available")
	}

	// Check if still in cooldown
	if e.modeManager.IsInCooldown() {
		e.logger.Warn("Still in cooldown mode, skipping force placement")
		return nil
	}

	// Get all tracked symbols from adaptive grid manager
	tracked := e.adaptiveGridManager.GetAllPositions()
	symbols := make([]string, 0)
	for symbol := range tracked {
		symbols = append(symbols, symbol)
	}

	if len(symbols) == 0 {
		e.logger.Info("No tracked symbols to force placement")
		return nil
	}

	e.logger.Info("Triggering force placement for tracked symbols",
		zap.Strings("symbols", symbols),
		zap.Int("count", len(symbols)))

	// For each symbol, enqueue placement
	for _, symbol := range symbols {
		e.logger.Info("Enqueueing force placement",
			zap.String("symbol", symbol))
		// This will trigger the placement worker to place orders
		if e.gridManager != nil {
			e.gridManager.enqueuePlacement(symbol)
		}
	}

	e.logger.Info("Force placement triggered successfully",
		zap.Int("symbols", len(symbols)))

	return nil
}

// startWebSocketServer starts the WebSocket server for dashboard
func (e *VolumeFarmEngine) startWebSocketServer() {
	e.logger.Info("=== startWebSocketServer called ===")

	if e.metricsStreamer == nil {
		e.logger.Error("metricsStreamer is nil, cannot start WebSocket server")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/metrics", e.metricsStreamer.HandleWebSocket)

	// Add a simple health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	port := ":8083"
	e.logger.Info("Starting WebSocket server for dashboard", zap.String("port", port))
	e.logger.Info("WebSocket endpoints:", zap.String("ws_metrics", "/ws/metrics"), zap.String("health", "/health"))

	// Start periodic metrics broadcast
	go e.periodicMetricsBroadcast()

	if err := http.ListenAndServe(port, mux); err != nil {
		e.logger.Error("WebSocket server failed", zap.Error(err))
	}
}

// periodicMetricsBroadcast broadcasts metrics to dashboard every second
func (e *VolumeFarmEngine) periodicMetricsBroadcast() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if e.metricsStreamer == nil {
			continue
		}

		// Get current metrics from GridManager
		totalVolume := e.gridManager.GetTotalVolume()
		activeOrders := e.gridManager.GetActiveOrderCount()

		e.metricsStreamer.BroadcastMetric("metrics_update", "", map[string]interface{}{
			"total_volume":  totalVolume,
			"active_orders": activeOrders,
			"timestamp":     time.Now().Format(time.RFC3339),
		}, time.Now())
	}
}
