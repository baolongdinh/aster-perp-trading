package maker

import (
	"aster-bot/internal/client"
	"aster-bot/internal/farming/volume_optimization"
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"go.uber.org/zap"
)

type MakerStrategyImpl struct {
	config         *Config
	logger         *zap.Logger
	futuresClient  FuturesClientInterface
	wsClient       WebSocketClientInterface
	orderManager   *OrderManagerImpl
	inventoryMgr   *InventoryManagerImpl
	spreadCalc     *SpreadCalculator
	liqGuard       *LiquidationGuard
	maxPosGuard    *MaxPositionGuard
	dailyLossGuard *DailyLossGuard
	otRatioGuard   *OrderToTradeGuard
	emergencyStop  *EmergencyStop

	// REST API cache with 1s expiration
	restCache *cache.Cache

	// Flow direction tracker for toxic flow detection
	flowTracker *FlowDirectionTracker

	// Momentum guard for detecting rapid price movements
	momentumGuard *MomentumGuard

	// Volume Optimization Modules
	pennyJumpMgr      *volume_optimization.PennyJumpManager
	inventoryHedgeMgr *volume_optimization.InventoryHedgeManager
	smartCancelMgr    *volume_optimization.SmartCancellationManager
	tickSizeMgr       *volume_optimization.TickSizeManager

	// Order Book Imbalance Detection (NEW)
	obiDetector *volume_optimization.OrderBookImbalanceDetector

	// Market Microstructure Analysis (NEW)
	microstructureAnalyzer *volume_optimization.MarketMicrostructureAnalyzer

	// NEW: Adaptive Leverage System
	leverageAdapter *LeverageAdapter

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewMakerStrategy(
	futuresClient FuturesClientInterface,
	wsClient WebSocketClientInterface,
	config *Config,
	logger *zap.Logger,
) *MakerStrategyImpl {
	if config == nil {
		config = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	strategy := &MakerStrategyImpl{
		config:        config,
		logger:        logger,
		futuresClient: futuresClient,
		wsClient:      wsClient,
		stopCh:        make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}

	strategy.orderManager = NewOrderManager(futuresClient, logger)
	strategy.inventoryMgr = NewInventoryManager(config, logger)
	strategy.spreadCalc = NewSpreadCalculator(config, strategy.inventoryMgr)
	strategy.liqGuard = NewLiquidationGuard(config, logger)
	strategy.maxPosGuard = NewMaxPositionGuard(config, logger)
	strategy.dailyLossGuard = NewDailyLossGuard(config, logger)
	strategy.otRatioGuard = NewOrderToTradeGuard(config, logger)
	strategy.emergencyStop = NewEmergencyStop(logger)
	strategy.flowTracker = NewFlowDirectionTracker(config, logger)
	strategy.momentumGuard = NewMomentumGuard(config, logger)
	// Initialize REST API cache with 1s expiration and 5s cleanup interval
	strategy.restCache = cache.New(1*time.Second, 5*time.Second)

	// Initialize Volume Optimization Modules
	strategy.tickSizeMgr = volume_optimization.NewTickSizeManager(logger)
	if err := strategy.tickSizeMgr.RefreshTickSizes(); err != nil {
		logger.Warn("Failed to refresh tick sizes", zap.Error(err))
	}

	if config.PennyJumpingEnabled {
		strategy.pennyJumpMgr = volume_optimization.NewPennyJumpManager(config.PennyJumpingConfig, logger)
		strategy.pennyJumpMgr.SetTickSizeManager(strategy.tickSizeMgr)
		logger.Info("PennyJumpManager initialized")
	}

	if config.InventoryHedgingEnabled {
		strategy.inventoryHedgeMgr = volume_optimization.NewInventoryHedgeManager(config.InventoryHedgingConfig, logger)
		strategy.inventoryHedgeMgr.SetTickSizeManager(strategy.tickSizeMgr)
		logger.Info("InventoryHedgeManager initialized")
	}

	if config.SmartCancellationEnabled {
		strategy.smartCancelMgr = volume_optimization.NewSmartCancellationManager(config.SmartCancellationConfig, logger)
		strategy.smartCancelMgr.SetCallbacks(
			func(symbol string, oldSpread, newSpread float64, changePct float64) {
				logger.Info("Spread change detected",
					zap.String("symbol", symbol),
					zap.Float64("old_spread", oldSpread),
					zap.Float64("new_spread", newSpread),
					zap.Float64("change_pct", changePct))
			},
			func(ctx context.Context, symbol string) error {
				return strategy.CancelOrders(symbol)
			},
			func(ctx context.Context, symbol string) error {
				return strategy.PlaceOrders(symbol)
			},
		)
		logger.Info("SmartCancellationManager initialized")
	}

	// Initialize Order Book Imbalance Detector (NEW)
	if config.OBIDetectionEnabled {
		strategy.obiDetector = volume_optimization.NewOrderBookImbalanceDetector(
			config.OBIWindowSize,
			config.OBIThreshold,
			logger,
		)
		logger.Info("📊 OrderBookImbalanceDetector initialized",
			zap.Int("window_size", config.OBIWindowSize),
			zap.Float64("threshold", config.OBIThreshold))

		// Wire OBI detector to spread calculator
		strategy.spreadCalc.SetOBIDetector(strategy.obiDetector)
	}

	// Initialize Market Microstructure Analyzer (NEW)
	if config.MicrostructureAnalysisEnabled {
		if strategy.obiDetector != nil {
			// Create VPIN monitor for microstructure analysis
			vpin := volume_optimization.NewVPINMonitor(volume_optimization.VPINConfig{
				WindowSize:        10,
				BucketSize:        100,
				VPINThreshold:     0.6,
				SustainedBreaches: 3,
				AutoResumeDelay:   30 * time.Second,
			}, logger)

			strategy.microstructureAnalyzer = volume_optimization.NewMarketMicrostructureAnalyzer(
				vpin,
				strategy.obiDetector,
				logger,
			)
			strategy.microstructureAnalyzer.SetAggressivenessLevel(config.AggressivenessLevel)
			logger.Info("🧠 MarketMicrostructureAnalyzer initialized",
				zap.Int("aggressiveness_level", config.AggressivenessLevel))

			// Wire all components to spread calculator
			strategy.spreadCalc.SetVPINMonitor(vpin)
			strategy.spreadCalc.SetMicrostructureAnalyzer(strategy.microstructureAnalyzer)
		}
	}

	// Initialize Adaptive Leverage System (NEW)
	strategy.leverageAdapter = NewLeverageAdapter(config.MaxLeverage, logger)
	logger.Info("🎚️ AdaptiveLeverageAdapter initialized",
		zap.Int("base_max_leverage", config.MaxLeverage))

	return strategy
}

func (s *MakerStrategyImpl) Start(ctx context.Context) error {
	s.logger.Info("Starting Maker Strategy",
		zap.Strings("symbols", s.config.Symbols),
		zap.Int("max_leverage", s.config.MaxLeverage),
		zap.Float64("spread_bps", s.config.DefaultSpreadBps))

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	// Subscribe to individual symbol tickers
	// Note: If using aggregate stream like !ticker@arr, data comes automatically
	if s.wsClient != nil {
		err := s.wsClient.SubscribeToTicker(s.config.Symbols)
		if err != nil {
			s.logger.Warn("Failed to subscribe to ticker (may be using aggregate stream)", zap.Error(err))
		} else {
			s.logger.Info("Subscribed to ticker for symbols", zap.Strings("symbols", s.config.Symbols))
		}
	}

	// Initialize DailyLossGuard with starting balance
	if s.wsClient != nil {
		time.Sleep(2 * time.Second) // Wait for WebSocket to receive balance
		balance := s.wsClient.GetCachedBalance()
		if balance.AvailableBalance > 0 {
			s.dailyLossGuard.SetStartingBalance(balance.AvailableBalance)
			s.logger.Info("DailyLossGuard initialized with starting balance",
				zap.Float64("balance", balance.AvailableBalance))
		}
	}

	// Start Volume Optimization Modules
	if s.pennyJumpMgr != nil {
		s.logger.Info("PennyJumpManager is enabled and ready")
	}
	if s.inventoryHedgeMgr != nil {
		s.inventoryHedgeMgr.Start(s.ctx)
	}
	if s.smartCancelMgr != nil {
		s.smartCancelMgr.Start(s.ctx)
	}
	if s.tickSizeMgr != nil {
		go s.tickSizeMgr.StartPeriodicRefresh(s.ctx, 5*time.Minute)
	}

	s.wg.Add(1)
	go s.orderManagementLoop()

	s.wg.Add(1)
	go s.riskMonitoringLoop()

	s.wg.Add(1)
	go s.positionSyncLoop()

	s.logger.Info("Maker Strategy started successfully")
	return nil
}

func (s *MakerStrategyImpl) Stop(ctx context.Context) error {
	s.logger.Info("Stopping Maker Strategy")

	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopCh)
	s.cancel()

	// Stop Volume Optimization Modules
	if s.inventoryHedgeMgr != nil {
		s.inventoryHedgeMgr.Stop()
	}
	if s.smartCancelMgr != nil {
		s.smartCancelMgr.Stop()
	}

	s.wg.Wait()

	s.cancelAllOrders()

	s.logger.Info("Maker Strategy stopped")
	return nil
}

func (s *MakerStrategyImpl) PlaceOrders(symbol string) error {
	s.logger.Debug("🚀 PlaceOrders called",
		zap.String("symbol", symbol),
		zap.Bool("running", s.running))

	if !s.running {
		s.logger.Debug("❌ PlaceOrders returning - not running")
		return nil
	}

	if stop, reason := s.emergencyStop.Check(s.ctx); stop {
		s.logger.Warn("Cannot place orders - emergency stop active", zap.String("reason", reason))
		return nil
	}

	var bestBid, bestAsk float64

	if s.wsClient != nil && s.wsClient.IsRunning() {
		bid, ask, _, err := s.wsClient.GetTickerData(symbol)
		if err != nil {
			s.logger.Warn("Failed to get ticker data", zap.String("symbol", symbol), zap.Error(err))
			return nil
		}
		bestBid = bid
		bestAsk = ask
	}

	if bestBid == 0 || bestAsk == 0 {
		s.logger.Warn("No best price available for symbol", zap.String("symbol", symbol))
		return nil
	}

	// Update optimization managers with best prices
	if s.pennyJumpMgr != nil {
		s.pennyJumpMgr.UpdateBestPrices(symbol, bestBid, bestAsk)
	}
	if s.smartCancelMgr != nil {
		s.smartCancelMgr.UpdateSpread(symbol, bestBid, bestAsk)
	}

	// ============================================================
	// NEW: Update OBI detector with orderbook data
	// ============================================================
	s.logger.Debug("🔍 Starting OBI detector operations")
	if s.obiDetector != nil {
		s.logger.Debug("📊 Updating OBI detector with orderbook data")
		// For now, simulate orderbook depth - in production, get real orderbook
		// TODO: Replace with real orderbook depth from WebSocket/REST
		bidQty := bestBid * 1000 // Simulated bid quantity
		askQty := bestAsk * 1000 // Simulated ask quantity
		midPrice := (bestBid + bestAsk) / 2

		s.logger.Debug("📊 Calling UpdateOrderBook")
		s.obiDetector.UpdateOrderBook(bidQty, askQty, midPrice)
		s.logger.Debug("✅ UpdateOrderBook completed")

		s.logger.Debug("📊 Calling GetSignal")
		// Log OBI signal for monitoring
		obiSignal := s.obiDetector.GetSignal()
		s.logger.Debug("✅ GetSignal completed")
		s.logger.Debug("📊 OBI Signal",
			zap.String("symbol", symbol),
			zap.Float64("obi_score", obiSignal.OBIScore),
			zap.String("bias_direction", obiSignal.BiasDirection),
			zap.String("strength", obiSignal.Strength),
			zap.String("recommended_action", obiSignal.RecommendedAction),
			zap.Float64("confidence", obiSignal.Confidence))
	}
	s.logger.Debug("✅ OBI detector operations completed")
	// Update inventory skew
	if s.inventoryHedgeMgr != nil {
		pos := s.inventoryMgr.GetPosition(symbol)
		if pos != nil {
			if pos.Amount > 0 {
				s.inventoryHedgeMgr.UpdateInventorySkew(symbol, math.Abs(pos.Amount), 0)
			} else {
				s.inventoryHedgeMgr.UpdateInventorySkew(symbol, 0, math.Abs(pos.Amount))
			}
		}
	}

	midPrice := (bestBid + bestAsk) / 2
	spread := bestAsk - bestBid

	s.logger.Info("🎯 Grid Strategy - Market Touching Orders",
		zap.String("symbol", symbol),
		zap.Float64("best_bid", bestBid),
		zap.Float64("best_ask", bestAsk),
		zap.Float64("mid_price", midPrice),
		zap.Float64("spread", spread))

	// ============================================================
	// DYNAMIC BALANCE-BASED SIZING - Compounding with current balance
	// ============================================================
	balance := s.getAvailableBalance()
	s.logger.Debug("💰 Balance check",
		zap.String("symbol", symbol),
		zap.Float64("available_balance", balance),
		zap.Bool("dynamic_sizing", s.config.UseDynamicSizing))

	if balance <= 0 {
		s.logger.Warn("❌ No available balance for order placement", zap.Float64("balance", balance))
		return nil
	}

	s.logger.Info("💰 Current Balance",
		zap.String("symbol", symbol),
		zap.Float64("available_balance", balance),
		zap.Bool("dynamic_sizing", s.config.UseDynamicSizing))

	// ============================================================
	// NEW: ADAPTIVE LEVERAGE-BASED SIZING
	// ============================================================
	var baseNotional float64
	var effectiveLeverage float64

	if s.config.UseDynamicSizing {
		// Get adaptive leverage based on market conditions
		if s.leverageAdapter != nil {
			// Update leverage adapter with current price
			s.leverageAdapter.UpdatePrice(symbol, midPrice)

			// Get adaptive leverage multiplier
			leverageMultiplier := s.leverageAdapter.GetLeverageMultiplier(symbol)
			effectiveLeverage = float64(s.config.MaxLeverage) * leverageMultiplier

			s.logger.Info("🎚️ Adaptive Leverage Applied",
				zap.String("symbol", symbol),
				zap.Float64("base_leverage", float64(s.config.MaxLeverage)),
				zap.Float64("leverage_multiplier", leverageMultiplier),
				zap.Float64("effective_leverage", effectiveLeverage))
		} else {
			// Fallback to static leverage
			effectiveLeverage = float64(s.config.MaxLeverage)
		}

		// Dynamic: Scale with balance, adaptive leverage applied
		baseNotional = balance * effectiveLeverage * (s.config.BaseNotionalUSD / 100.0)
		// Clamp to min/max
		if baseNotional < s.config.MinNotionalUSD {
			baseNotional = s.config.MinNotionalUSD
		}
		if baseNotional > s.config.MaxNotionalUSD*effectiveLeverage {
			baseNotional = s.config.MaxNotionalUSD * effectiveLeverage
		}
	} else {
		// Legacy: Static leverage calculation
		baseNotional = balance * float64(s.config.MaxLeverage)
		effectiveLeverage = float64(s.config.MaxLeverage)
	}

	buyQty := baseNotional / bestBid
	sellQty := baseNotional / bestAsk

	// ============================================================
	// POSITION BIAS PROTECTION - Reduce orders when skewed
	// ============================================================
	position := s.inventoryMgr.GetPosition(symbol)
	var positionBias float64
	if position != nil {
		positionBias = position.Amount
	}

	maxPositionQty := s.config.MaxPositionUSDT / midPrice
	positionBiasPct := 0.0
	if maxPositionQty > 0 {
		positionBiasPct = math.Abs(positionBias) / maxPositionQty
	}

	buyAdjustment := 1.0
	sellAdjustment := 1.0

	if math.Abs(positionBiasPct) > s.config.PositionBiasThreshold {
		s.logger.Warn(" Position Bias Exceeded - Taking aggressive action",
			zap.String("symbol", symbol),
			zap.Float64("position_bias_pct", positionBiasPct),
			zap.Float64("threshold", s.config.PositionBiasThreshold),
			zap.Float64("current_position", positionBias))

		// Cancel ALL existing orders to stop accumulating position
		existingOrders := s.orderManager.GetOpenOrders(symbol)
		for _, order := range existingOrders {
			if order.Status == OrderStatusNew || order.Status == OrderStatusPartially {
				s.orderManager.CancelOrder(s.ctx, symbol, order.OrderID)
				s.orderManager.RemoveOrder(symbol, order.OrderID)
				s.logger.Info(" Cancelled order due to position bias",
					zap.String("symbol", symbol),
					zap.Int64("order_id", order.OrderID),
					zap.String("side", string(order.Side)))
			}
		}

		// Apply aggressive reduction - reduce BOTH sides to minimize new exposure
		// Only allow minimal orders to slowly unwind position
		if positionBias > 0 {
			// Long biased - significantly reduce buy, moderate sell to unwind
			buyAdjustment = s.config.PositionBiasReducePct * 0.3 // More aggressive reduction
			sellAdjustment = 1.5                                 // Allow more sells to unwind
			s.logger.Warn(" Long Bias - Aggressive reduction",
				zap.Float64("buy_adjustment", buyAdjustment),
				zap.Float64("sell_adjustment", sellAdjustment))
		} else if positionBias < 0 {
			// Short biased - moderate buy to unwind, significantly reduce sell
			buyAdjustment = 1.5                                   // Allow more buys to unwind
			sellAdjustment = s.config.PositionBiasReducePct * 0.3 // More aggressive reduction
			s.logger.Warn(" Short Bias - Aggressive reduction",
				zap.Float64("buy_adjustment", buyAdjustment),
				zap.Float64("sell_adjustment", sellAdjustment))
		}

		// Check if we should emergency stop due to extreme bias
		if math.Abs(positionBiasPct) > 0.8 { // 80% of max position
			s.logger.Error(" Extreme Position Bias - Triggering emergency stop",
				zap.Float64("position_bias_pct", positionBiasPct))
			s.emergencyStop.Trigger("extreme_position_bias")
			s.cancelAllOrders()
			return nil
		}
	}

	// ============================================================
	// MICROSTRUCTURE-AWARE ORDER SIZING
	// ============================================================
	microSizeMultiplier := 1.0
	if s.microstructureAnalyzer != nil {
		signal := s.microstructureAnalyzer.AnalyzeMarket()
		microSizeMultiplier = signal.OrderSizeMultiplier

		s.logger.Info("🧠 Microstructure Signal Applied",
			zap.String("symbol", symbol),
			zap.String("trading_mode", signal.TradingMode),
			zap.Float64("composite_score", signal.CompositeScore),
			zap.Float64("order_size_mult", signal.OrderSizeMultiplier),
			zap.Float64("spread_mult", signal.SpreadMultiplier),
			zap.String("rationale", signal.Rationale))

		// Apply microstructure-based size adjustment
		buyQty *= microSizeMultiplier
		sellQty *= microSizeMultiplier
	}

	// ============================================================
	// LEGACY: TOXIC FLOW PROTECTION (kept for compatibility)
	// ============================================================
	if s.config.ToxicFlowDetection && s.flowTracker != nil {
		if s.flowTracker.IsToxicFlow() {
			toxicReduction := s.flowTracker.GetReductionFactor()
			buyAdjustment *= toxicReduction
			sellAdjustment *= toxicReduction
			s.logger.Warn("⚠️ Legacy Toxic Flow Detected - Reducing all orders",
				zap.String("symbol", symbol),
				zap.Float64("toxic_reduction", toxicReduction),
				zap.Float64("buy_ratio", s.flowTracker.GetBuyRatio()))
		}
	}

	// Apply adjustments
	buyQty *= buyAdjustment
	sellQty *= sellAdjustment

	// ============================================================
	// NEW: OBI-AWARE GRID SPACING ADJUSTMENT
	// ============================================================
	obiGridSpacingMultiplier := 1.0
	if s.obiDetector != nil && s.config.OBISpreadAdjustment {
		obiSignal := s.obiDetector.GetSignal()

		// Adjust grid spacing based on OBI - tighter when imbalanced to attract counter-orders
		switch obiSignal.RecommendedAction {
		case "TIGHTEN_SELL":
			// More buyers needed - tighten sell grid more than buy
			obiGridSpacingMultiplier = 0.8 // 20% tighter on sell side
		case "TIGHTEN_BUY":
			// More sellers needed - tighten buy grid more than sell
			obiGridSpacingMultiplier = 0.8 // 20% tighter on buy side
		case "MODERATE_SELL":
			obiGridSpacingMultiplier = 0.9 // 10% tighter
		case "MODERATE_BUY":
			obiGridSpacingMultiplier = 0.9 // 10% tighter
		default:
			obiGridSpacingMultiplier = 1.0 // No adjustment
		}

		s.logger.Info("🎯 OBI Grid Spacing Adjustment",
			zap.String("symbol", symbol),
			zap.String("action", obiSignal.RecommendedAction),
			zap.Float64("spacing_multiplier", obiGridSpacingMultiplier))
	}

	// ============================================================
	// MICRO PROFIT OPTIMIZATION - Ultra-tight grid for max fills
	// ============================================================
	var gridLevels int
	var gridSpacing float64
	var capitalAllocation float64
	var riskLevel string

	if s.config.MicroProfitMode {
		// Micro profit mode: Use configured ultra-tight parameters with OBI adjustment
		gridLevels = s.config.MicroGridLevels
		gridSpacing = (s.config.MicroGridSpacingBps / 10000.0) * obiGridSpacingMultiplier // Apply OBI adjustment
		capitalAllocation = 2.0                                                           // Increased to ensure minimum quantity meets tick size
		riskLevel = "MICRO_PROFIT"
	} else {
		// Legacy volatility-based regime
		dailyVolatility := spread / midPrice * 100
		hv30SmoothingConstant := 0.618
		adjustedVolatility := dailyVolatility * hv30SmoothingConstant

		if adjustedVolatility < 2.0 {
			gridLevels = 50
			gridSpacing = 0.00025
			capitalAllocation = 1.0
			riskLevel = "ULTRA_TIGHT"
		} else if adjustedVolatility <= 5.0 {
			gridLevels = 40
			gridSpacing = 0.0005
			capitalAllocation = 1.0
			riskLevel = "TIGHT"
		} else if adjustedVolatility <= 8.0 {
			gridLevels = 30
			gridSpacing = 0.0015
			capitalAllocation = 0.9
			riskLevel = "BALANCED"
		} else {
			gridLevels = 25
			gridSpacing = 0.002
			capitalAllocation = 0.8
			riskLevel = "WIDE"
		}
	}

	// Calculate minimum quantity per order
	minNotionalUSD := s.config.MicroMinNotionalUSD
	if minNotionalUSD <= 0 {
		minNotionalUSD = 5.0
	}
	minQtyPerOrder := minNotionalUSD / midPrice

	// Adjust grid levels if capital is insufficient
	maxAffordableLevels := int(buyQty / minQtyPerOrder)
	if maxAffordableLevels < gridLevels {
		oldLevels := gridLevels
		gridLevels = maxAffordableLevels
		if gridLevels < 5 {
			gridLevels = 5
		}
		s.logger.Info("🔧 Reduced grid levels for capital",
			zap.Int("old_levels", oldLevels),
			zap.Int("new_levels", gridLevels),
			zap.Float64("min_qty_per_level", minQtyPerOrder))
	}

	perLevelBuyQty := buyQty * capitalAllocation / float64(gridLevels)
	perLevelSellQty := sellQty * capitalAllocation / float64(gridLevels)

	// Ensure minimum quantity per order
	finalMinQty := minQtyPerOrder * 1.2
	if perLevelBuyQty < finalMinQty {
		perLevelBuyQty = finalMinQty
	}
	if perLevelSellQty < finalMinQty {
		perLevelSellQty = finalMinQty
	}

	// Round to precision using TickSizeManager
	if s.tickSizeMgr != nil {
		tickSize := s.tickSizeMgr.GetTickSize(symbol)
		perLevelBuyQty = math.Round(perLevelBuyQty/tickSize) * tickSize
		perLevelSellQty = math.Round(perLevelSellQty/tickSize) * tickSize
	} else {
		perLevelBuyQty = s.roundToPrecision(perLevelBuyQty, symbol)
		perLevelSellQty = s.roundToPrecision(perLevelSellQty, symbol)
	}

	s.logger.Info("� Micro Profit Grid Config",
		zap.String("symbol", symbol),
		zap.String("risk_level", riskLevel),
		zap.Int("grid_levels", gridLevels),
		zap.Float64("spacing_bps", s.config.MicroGridSpacingBps),
		zap.Float64("per_level_buy_qty", perLevelBuyQty),
		zap.Float64("per_level_sell_qty", perLevelSellQty),
		zap.Float64("total_buy_notional", perLevelBuyQty*bestBid*float64(gridLevels)),
		zap.Float64("total_sell_notional", perLevelSellQty*bestAsk*float64(gridLevels)))

	// ============================================================
	// PLACE GRID ORDERS
	// ============================================================
	var wg sync.WaitGroup
	orderPlaced := make(chan bool, gridLevels*2)

	// Place buy orders
	for i := 0; i < gridLevels; i++ {
		wg.Add(1)
		go func(level int) {
			defer wg.Done()
			gridOffset := gridSpacing * float64(level+1)
			gridBuyPrice := bestBid * (1 - gridOffset)

			// Apply penny jumping if enabled
			if s.pennyJumpMgr != nil {
				gridBuyPrice = s.pennyJumpMgr.GetPennyJumpedPrice(symbol, "BUY", gridBuyPrice)
			}

			// Round price to tick size
			if s.tickSizeMgr != nil {
				gridBuyPrice = s.tickSizeMgr.RoundToTickForSymbol(symbol, gridBuyPrice)
			}

			if perLevelBuyQty > 0 {
				err := s.placeLimitOrder(symbol, OrderSideBuy, gridBuyPrice, perLevelBuyQty)
				if err != nil {
					s.logger.Error("Failed to place grid buy order", zap.Error(err))
				} else {
					s.otRatioGuard.RecordOrder()
					s.flowTracker.RecordBuy(perLevelBuyQty * gridBuyPrice)
					s.logger.Info("📗 Grid Buy",
						zap.String("symbol", symbol),
						zap.Int("level", level+1),
						zap.Float64("price", gridBuyPrice),
						zap.Float64("qty", perLevelBuyQty))
				}
				orderPlaced <- true
			}
		}(i)
	}

	// Place sell orders
	for i := 0; i < gridLevels; i++ {
		wg.Add(1)
		go func(level int) {
			defer wg.Done()
			gridOffset := gridSpacing * float64(level+1)
			gridSellPrice := bestAsk * (1 + gridOffset)

			// Apply penny jumping if enabled
			if s.pennyJumpMgr != nil {
				gridSellPrice = s.pennyJumpMgr.GetPennyJumpedPrice(symbol, "SELL", gridSellPrice)
			}

			// Round price to tick size
			if s.tickSizeMgr != nil {
				gridSellPrice = s.tickSizeMgr.RoundToTickForSymbol(symbol, gridSellPrice)
			}

			if perLevelSellQty > 0 {
				err := s.placeLimitOrder(symbol, OrderSideSell, gridSellPrice, perLevelSellQty)
				if err != nil {
					s.logger.Error("Failed to place grid sell order", zap.Error(err))
				} else {
					s.otRatioGuard.RecordOrder()
					s.flowTracker.RecordSell(perLevelSellQty * gridSellPrice)
					s.logger.Info("📕 Grid Sell",
						zap.String("symbol", symbol),
						zap.Int("level", level+1),
						zap.Float64("price", gridSellPrice),
						zap.Float64("qty", perLevelSellQty))
				}
				orderPlaced <- true
			}
		}(i)
	}

	// Wait for all orders to be placed
	go func() {
		wg.Wait()
		close(orderPlaced)
	}()

	// Count successful placements
	successfulPlacements := 0
	for success := range orderPlaced {
		if success {
			successfulPlacements++
		}
	}

	s.logger.Info("📊 Grid Complete",
		zap.String("symbol", symbol),
		zap.Int("buy_levels", gridLevels),
		zap.Int("sell_levels", gridLevels),
		zap.Float64("total_buy_qty", buyQty),
		zap.Float64("total_sell_qty", sellQty))

	return nil
}

func (s *MakerStrategyImpl) placeLimitOrder(symbol string, side OrderSide, price, quantity float64) error {
	s.logger.Debug("🎯 placeLimitOrder called",
		zap.String("symbol", symbol),
		zap.String("side", string(side)),
		zap.Float64("price", price),
		zap.Float64("quantity", quantity))

	req := LimitOrderRequest{
		Symbol:      symbol,
		Side:        side,
		Price:       price,
		Quantity:    quantity,
		TimeInForce: "GTX",
	}

	s.logger.Debug("📝 Calling orderManager.PlaceLimitOrder")
	order, err := s.orderManager.PlaceLimitOrder(s.ctx, req)
	if err != nil {
		s.logger.Error("❌ placeLimitOrder failed",
			zap.String("symbol", symbol),
			zap.String("side", string(side)),
			zap.Error(err))
		return err
	}

	s.logger.Info("✅ Placed limit order",
		zap.String("symbol", symbol),
		zap.String("side", string(side)),
		zap.Float64("price", price),
		zap.Float64("quantity", quantity),
		zap.Int64("order_id", order.OrderID))

	return nil
}

func (s *MakerStrategyImpl) CancelOrders(symbol string) error {
	orders := s.orderManager.GetOpenOrders(symbol)
	for _, order := range orders {
		err := s.orderManager.CancelOrder(s.ctx, symbol, order.OrderID)
		if err != nil {
			s.logger.Warn("Failed to cancel order",
				zap.Int64("order_id", order.OrderID),
				zap.Error(err))
		}
	}
	return nil
}

func (s *MakerStrategyImpl) ReplaceOrder(oldOrderID int64, symbol string) error {
	s.orderManager.RemoveOrder(symbol, oldOrderID)
	return s.PlaceOrders(symbol)
}

func (s *MakerStrategyImpl) GetSpread(symbol string) float64 {
	buySp, sellSp := s.spreadCalc.GetSpreadForSymbol(symbol)
	return (buySp + sellSp) / 2
}

func (s *MakerStrategyImpl) GetPosition(symbol string) *PositionState {
	return s.inventoryMgr.GetPosition(symbol)
}

func (s *MakerStrategyImpl) GetInventoryState() InventoryState {
	return s.inventoryMgr.GetInventoryState()
}

func (s *MakerStrategyImpl) GetConfig() *Config {
	return s.config
}

func (s *MakerStrategyImpl) orderManagementLoop() {
	defer s.wg.Done()

	// Fast cycle - maximum trading frequency
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.logger.Debug("🔄 Order management cycle started")
			for _, symbol := range s.config.Symbols {
				s.logger.Debug("🔄 Processing order lifecycle", zap.String("symbol", symbol))
				s.processOrderLifecycle(symbol)
			}
			s.logger.Debug("✅ Order management cycle completed")
		}
	}
}

func (s *MakerStrategyImpl) processOrderLifecycle(symbol string) {
	s.logger.Debug("🔍 processOrderLifecycle started", zap.String("symbol", symbol))
	// Smart Order Lifecycle - High Fill Rate Strategy:
	// 1. Give orders time to fill (30-60 seconds)
	// 2. Only cancel if: price moves significantly OR order is too old
	// 3. Maintain 2-sided quote for continuous volume + micro-profit

	orders := s.orderManager.GetOpenOrders(symbol)
	s.logger.Debug("📋 Retrieved open orders", zap.String("symbol", symbol), zap.Int("count", len(orders)))
	filledCount := 0

	// Get current market price
	s.logger.Debug("💰 Getting ticker data", zap.String("symbol", symbol))
	bestBid, bestAsk, _, err := s.wsClient.GetTickerData(symbol)
	if err != nil {
		s.logger.Warn("Failed to get ticker data", zap.String("symbol", symbol), zap.Error(err))
		return
	}
	midPrice := (bestBid + bestAsk) / 2
	s.logger.Debug("💰 Got ticker data", zap.String("symbol", symbol), zap.Float64("bid", bestBid), zap.Float64("ask", bestAsk))

	// SYNC WITH EXCHANGE: Get current order state from exchange (with 1s cache)
	cacheKey := fmt.Sprintf("open_orders_%s", symbol)
	var exchangeOrders []client.Order

	if cached, found := s.restCache.Get(cacheKey); found {
		if orders, ok := cached.([]client.Order); ok {
			s.logger.Debug("Using cached open orders", zap.String("symbol", symbol))
			exchangeOrders = orders
		}
	}

	if exchangeOrders == nil {
		s.logger.Debug("📡 Calling REST API to get open orders", zap.String("symbol", symbol))
		var err error
		exchangeOrders, err = s.futuresClient.GetOpenOrders(s.ctx, symbol)
		s.logger.Debug("📡 REST API call completed",
			zap.String("symbol", symbol),
			zap.Error(err),
			zap.Int("order_count", len(exchangeOrders)))
		if err != nil {
			s.logger.Warn("Failed to sync with exchange orders", zap.Error(err))
		} else {
			// Cache for 1s
			s.restCache.Set(cacheKey, exchangeOrders, cache.DefaultExpiration)
			s.logger.Debug("📋 Cached open orders", zap.String("symbol", symbol), zap.Int("count", len(exchangeOrders)))
		}
	}

	if exchangeOrders != nil {
		// Clean up local orders that don't exist on exchange
		var localOrdersToRemove []int64
		for _, localOrder := range orders {
			existsOnExchange := false
			for _, exchangeOrder := range exchangeOrders {
				// Compare OrderID directly - both have same type
				if exchangeOrder.OrderID == localOrder.OrderID {
					existsOnExchange = true
					break
				}
			}
			if !existsOnExchange {
				localOrdersToRemove = append(localOrdersToRemove, localOrder.OrderID)
			}
		}

		// Remove stale local orders
		for _, orderID := range localOrdersToRemove {
			s.orderManager.RemoveOrder(symbol, orderID)
			s.logger.Info("🧹 Cleaned up stale local order",
				zap.String("symbol", symbol),
				zap.Int64("order_id", orderID))
		}

		// Update orders list after cleanup
		orders = s.orderManager.GetOpenOrders(symbol)
	}

	// Check for filled orders first (revenue event!)
	totalOrders := 0
	for _, order := range orders {
		if order.Status == OrderStatusNew || order.Status == OrderStatusPartially {
			totalOrders++
		}
	}

	for _, order := range orders {
		if order.Status == OrderStatusFilled {
			filledCount++
			s.orderManager.RemoveOrder(symbol, order.OrderID)
			s.otRatioGuard.RecordFill()

			// ============================================================
			// REAL-TIME POSITION UPDATE - Update immediately after fill
			// ============================================================
			position := s.inventoryMgr.GetPosition(symbol)
			var currentAmount float64
			if position != nil {
				currentAmount = position.Amount
			}

			// Update position based on fill side
			var newAmount float64
			if order.Side == OrderSideBuy {
				newAmount = currentAmount + order.OrigQty
			} else {
				newAmount = currentAmount - order.OrigQty
			}

			// Update all position-dependent components
			s.inventoryMgr.UpdatePosition(symbol, newAmount, order.Price, midPrice)
			s.liqGuard.UpdatePosition(symbol, newAmount, order.Price, midPrice)
			s.maxPosGuard.UpdateExposure(symbol, newAmount, midPrice)

			s.logger.Info("📊 Position Updated After Fill",
				zap.String("symbol", symbol),
				zap.String("side", string(order.Side)),
				zap.Float64("fill_qty", order.OrigQty),
				zap.Float64("old_position", currentAmount),
				zap.Float64("new_position", newAmount))

			// ============================================================
			// CHECK RISK CONDITIONS AFTER EVERY FILL
			// ============================================================
			if stop, reason := s.liqGuard.Check(s.ctx); stop {
				s.logger.Error("🚨 Liquidation risk after fill - triggering emergency stop",
					zap.String("reason", reason),
					zap.Float64("position", newAmount))
				s.emergencyStop.Trigger(reason)
				s.cancelAllOrders()
				return
			}

			// Calculate fill ratio and PnL
			fillRatio := float64(filledCount) / float64(totalOrders) * 100

			// Calculate PnL for this fill
			var fillPnL float64
			if position != nil {
				if order.Side == OrderSideBuy {
					fillPnL = (midPrice - order.Price) * order.OrigQty // Buy order profit
				} else {
					fillPnL = (order.Price - midPrice) * order.OrigQty // Sell order profit
				}
			}

			s.logger.Info("🎯 ORDER FILLED - Micro Profit Earned!",
				zap.String("symbol", symbol),
				zap.Int64("order_id", order.OrderID),
				zap.String("side", string(order.Side)),
				zap.Float64("price", order.Price),
				zap.Float64("qty", order.OrigQty),
				zap.Float64("fill_ratio_pct", fillRatio),
				zap.Float64("fill_pnl", fillPnL))
		} else if order.Status == OrderStatusCanceled || order.Status == OrderStatusExpired {
			s.orderManager.RemoveOrder(symbol, order.OrderID)
		}
	}

	// EXA OPTIMIZED: Allow sufficient time for fills
	// DYNAMIC GRID SHIFT THRESHOLD - Scale with grid configuration
	// For micro profit mode: 50 levels × 0.001% = 5% total range, use 50% = 2.5%
	gridSpacing := s.config.MicroGridSpacingBps / 10000.0
	gridLevels := s.config.MicroGridLevels
	dynamicGridShiftThreshold := gridSpacing * float64(gridLevels) * 0.5
	if dynamicGridShiftThreshold < 0.01 {
		dynamicGridShiftThreshold = 0.01 // Minimum 1%
	}

	gridShiftThreshold := dynamicGridShiftThreshold
	maxOrderAge := 120 * time.Second // 2 minutes - sufficient time for fills per EXA research
	needsRefresh := false
	cancelledCount := 0
	needsGridShift := false // Declare early for momentum check

	// ============================================================
	// MOMENTUM CHECK - Cancel all orders if high momentum detected
	// ============================================================
	if s.momentumGuard != nil {
		if momentumDetected, reason := s.momentumGuard.Check(midPrice); momentumDetected {
			s.logger.Warn("⚡ High Momentum - Cancelling all orders to prevent adverse selection",
				zap.String("symbol", symbol),
				zap.String("reason", reason),
				zap.Float64("mid_price", midPrice))
			needsGridShift = true
			// Force cancel all orders
			for _, order := range orders {
				if order.Status == OrderStatusNew || order.Status == OrderStatusPartially {
					s.orderManager.CancelOrder(s.ctx, symbol, order.OrderID)
					s.orderManager.RemoveOrder(symbol, order.OrderID)
					cancelledCount++
				}
			}
			// Don't place new orders immediately - wait for momentum to subside
			s.logger.Info("⏸️ Pausing order placement due to high momentum")
			return
		}
	}

	// Calculate current grid center vs market center
	currentGridCenter := (bestBid + bestAsk) / 2

	// Check if we need to shift the entire grid (update if not already set by momentum)

	if len(orders) > 0 {
		// Find average price of existing grid orders
		var totalOrderPrice float64
		var orderCount int
		for _, order := range orders {
			if order.Status == OrderStatusNew || order.Status == OrderStatusPartially {
				totalOrderPrice += order.Price
				orderCount++
			}
		}

		if orderCount > 0 {
			avgGridPrice := totalOrderPrice / float64(orderCount)
			gridDrift := math.Abs(avgGridPrice-currentGridCenter) / currentGridCenter

			if gridDrift > gridShiftThreshold {
				needsGridShift = true
				s.logger.Info("🔄 Grid Shift Needed",
					zap.String("symbol", symbol),
					zap.Float64("grid_center", currentGridCenter),
					zap.Float64("avg_grid_price", avgGridPrice),
					zap.Float64("grid_drift_pct", gridDrift*100),
					zap.Float64("threshold_pct", gridShiftThreshold*100))
			}
		}
	}

	// Individual order cancellation logic
	for _, order := range orders {
		if order.Status == OrderStatusNew || order.Status == OrderStatusPartially {
			// Update order age
			order.AgeSeconds = int64(time.Since(order.PlacedTime).Seconds())

			// Cancel conditions:
			shouldCancel := false
			reason := ""

			// 1. Grid shift needed - cancel ALL orders
			if needsGridShift {
				shouldCancel = true
				reason = "grid shift"
			}

			// 2. Order too old
			if time.Since(order.PlacedTime) > maxOrderAge {
				shouldCancel = true
				reason = "too old"
			}

			// 3. Individual order too far from market
			var targetPrice float64
			if order.Side == OrderSideBuy {
				targetPrice = bestBid
			} else {
				targetPrice = bestAsk
			}
			priceDiff := math.Abs(order.Price-targetPrice) / midPrice
			if priceDiff > 0.001 { // 0.1%
				shouldCancel = true
				if reason == "" {
					reason = "price drift"
				}
			}

			if shouldCancel {
				// Validate order state before canceling
				orders := s.orderManager.GetOpenOrders(symbol)
				orderExists := false
				for _, existingOrder := range orders {
					if existingOrder.OrderID == order.OrderID {
						orderExists = true
						break
					}
				}

				if orderExists {
					// Retry logic for unknown order errors
					maxRetries := 3
					for retry := 0; retry < maxRetries; retry++ {
						err := s.orderManager.CancelOrder(s.ctx, symbol, order.OrderID)
						if err == nil {
							s.orderManager.RemoveOrder(symbol, order.OrderID)
							cancelledCount++
							s.logger.Info("🔄 Order cancelled",
								zap.String("symbol", symbol),
								zap.String("reason", reason),
								zap.Int64("order_id", order.OrderID),
								zap.Int64("age_sec", int64(time.Since(order.PlacedTime).Seconds())),
								zap.Float64("price_diff_pct", priceDiff*100),
								zap.Int("retry", retry+1))
							break
						} else if strings.Contains(err.Error(), "Unknown order sent") && retry < maxRetries-1 {
							// Wait a bit before retry for unknown order
							time.Sleep(100 * time.Millisecond)
							s.logger.Warn("Retrying cancel - unknown order",
								zap.String("symbol", symbol),
								zap.Int64("order_id", order.OrderID),
								zap.Int("retry", retry+1),
								zap.Error(err))
						} else {
							s.logger.Warn("Failed to cancel order after retries",
								zap.String("symbol", symbol),
								zap.Int64("order_id", order.OrderID),
								zap.Int("retry", retry+1),
								zap.Error(err))
							break
						}
					}
				} else {
					s.logger.Warn("Order not found in local tracking - skipping cancel",
						zap.String("symbol", symbol),
						zap.Int64("order_id", order.OrderID))
				}
			}
		}
	}

	// Count active orders after cleanup
	activeBuy := false
	activeSell := false
	for _, order := range s.orderManager.GetOpenOrders(symbol) {
		if order.Status == OrderStatusNew || order.Status == OrderStatusPartially {
			if order.Side == OrderSideBuy {
				activeBuy = true
			} else {
				activeSell = true
			}
		}
	}

	// Place orders if needed
	placeOrders := false
	placeReason := ""

	if filledCount > 0 {
		placeOrders = true
		placeReason = "fill replacement"
	} else if needsRefresh || needsGridShift {
		placeOrders = true
		if needsGridShift {
			placeReason = "grid shift"
		} else {
			placeReason = "stale replacement"
		}
	} else if !activeBuy || !activeSell {
		placeOrders = true
		placeReason = "missing side"
	}

	if placeOrders {
		s.logger.Info("📈 Placing fresh orders",
			zap.String("symbol", symbol),
			zap.String("reason", placeReason),
			zap.Bool("has_buy", activeBuy),
			zap.Bool("has_sell", activeSell),
			zap.Int("filled_count", filledCount),
			zap.Int("cancelled_count", cancelledCount))
		s.PlaceOrders(symbol)
	} else {
		// Log that we're keeping orders alive for fills
		s.logger.Debug("⏳ Keeping orders alive for fills",
			zap.String("symbol", symbol),
			zap.Bool("has_buy", activeBuy),
			zap.Bool("has_sell", activeSell))
	}
}

func (s *MakerStrategyImpl) riskMonitoringLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkRiskConditions()
		}
	}
}

func (s *MakerStrategyImpl) checkRiskConditions() {
	if stop, reason := s.liqGuard.Check(s.ctx); stop {
		s.emergencyStop.Trigger(reason)
		s.cancelAllOrders()
		return
	}

	if stop, reason := s.maxPosGuard.Check(s.ctx); stop {
		s.emergencyStop.Trigger(reason)
		s.cancelAllOrders()
		return
	}

	if stop, reason := s.dailyLossGuard.Check(s.ctx); stop {
		s.emergencyStop.Trigger(reason)
		s.cancelAllOrders()
		return
	}

	if stop, reason := s.otRatioGuard.Check(s.ctx); stop {
		s.logger.Warn("❌ Order-to-Trade ratio high, pausing order placement", zap.String("reason", reason))
		time.Sleep(30 * time.Second)
		s.otRatioGuard.Reset()
	} else {
	}
}

func (s *MakerStrategyImpl) positionSyncLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.syncPositions()
		}
	}
}

func (s *MakerStrategyImpl) syncPositions() {
	if s.futuresClient == nil {
		return
	}

	// Check cache first
	var positions []client.Position
	var err error

	if cached, found := s.restCache.Get("positions"); found {
		if pos, ok := cached.([]client.Position); ok {
			s.logger.Debug("Using cached positions")
			positions = pos
		}
	}

	// If not cached, fetch from API
	if positions == nil {
		positions, err = s.futuresClient.GetPositions(s.ctx)
		if err != nil {
			s.logger.Warn("Failed to get positions for sync", zap.Error(err))
			return
		}
		// Cache for 1s
		s.restCache.Set("positions", positions, cache.DefaultExpiration)
	}

	if err != nil {
		s.logger.Warn("Failed to get positions for sync", zap.Error(err))
		return
	}

	for _, pos := range positions {
		if pos.PositionAmt != 0 {
			s.inventoryMgr.UpdatePosition(pos.Symbol, pos.PositionAmt, pos.EntryPrice, pos.MarkPrice)
			s.liqGuard.UpdatePosition(pos.Symbol, pos.PositionAmt, pos.EntryPrice, pos.MarkPrice)
			s.maxPosGuard.UpdateExposure(pos.Symbol, pos.PositionAmt, pos.MarkPrice)
		}
	}
}

func (s *MakerStrategyImpl) cancelAllOrders() {
	for _, symbol := range s.config.Symbols {
		orders := s.orderManager.GetOpenOrders(symbol)
		for _, order := range orders {
			s.orderManager.CancelOrder(s.ctx, symbol, order.OrderID)
		}
	}
}

func (s *MakerStrategyImpl) getAvailableBalance() float64 {
	if s.wsClient != nil && s.wsClient.IsRunning() {
		balance := s.wsClient.GetCachedBalance()
		s.logger.Debug("WebSocket balance cache", zap.Float64("available", balance.AvailableBalance))
		if balance.AvailableBalance > 0 {
			return balance.AvailableBalance
		}
	}

	if s.futuresClient != nil {
		// Check cache first
		if cached, found := s.restCache.Get("account_info"); found {
			if account, ok := cached.(*client.AccountInfo); ok {
				s.logger.Debug("Using cached account info")
				return account.AvailableBalance
			}
		}

		s.logger.Debug("📡 Getting account info via REST API")
		account, err := s.futuresClient.GetAccountInfo(s.ctx)
		if err != nil {
			s.logger.Error("❌ Failed to get account info", zap.Error(err))
		} else {
			s.logger.Debug("✅ Account info received", zap.Float64("balance", account.AvailableBalance))
			// Cache for 30 seconds
			s.restCache.Set("account_info", account, 30*time.Second)
			return account.AvailableBalance
		}
	}

	s.logger.Warn("⚠️ getAvailableBalance returning 0 (fallback)")
	s.logger.Warn("No balance available from any source")
	return 0
}

func (s *MakerStrategyImpl) roundToPrecision(qty float64, symbol string) float64 {
	return math.Round(qty*10000) / 10000
}

func (s *MakerStrategyImpl) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *MakerStrategyImpl) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"total_orders":   s.otRatioGuard.totalOrders,
		"total_fills":    s.otRatioGuard.totalFills,
		"ot_ratio":       s.otRatioGuard.GetRatio(),
		"daily_pnl":      s.dailyLossGuard.GetDailyPnL(),
		"total_exposure": s.maxPosGuard.GetTotalExposure(),
		"long_exposure":  s.inventoryMgr.GetLongExposure(),
		"short_exposure": s.inventoryMgr.GetShortExposure(),
		"unrealized_pnl": s.inventoryMgr.GetTotalUnrealizedPNL(),
	}
}
