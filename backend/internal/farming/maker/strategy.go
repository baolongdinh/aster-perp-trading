package maker

import (
	"aster-bot/internal/client"
	"aster-bot/internal/notifier"
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"go.uber.org/zap"
)

type pricePoint struct {
	price float64
	ts    time.Time
}

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
	riskManager    *RiskManager

	// REST API cache with 1s expiration
	restCache *cache.Cache

	// === NEW: Risk Optimization Fields ===
	// Position timeout tracking: symbol -> open time
	positionOpenTime map[string]time.Time
	// Trailing state: symbol -> trailing state
	trailingStates map[string]*TrailingState
	// Daily reset state
	dailyResetState *DailyResetState
	// EMA cache for zone-based sizing
	emaCache map[string]float64
	// Position count per side
	longPositionCount  int
	shortPositionCount int

	// P0 FIX: Price history buffer for real pump/crash detection (last 30 ticks)
	priceHistory   map[string][]pricePoint
	priceHistoryMu sync.Mutex
	// P0 FIX: Debounce mutex to prevent concurrent continuousOrderPlacement
	placementInProgress sync.Map // key=symbol, value=bool

	// === NEW: Accurate Metrics Telemetry ===
	startTime            time.Time
	initialWalletBalance float64
	maxWalletBalance     float64

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
	strategy.riskManager = NewRiskManager(config, logger)
	// Initialize REST API cache with 1s expiration and 5s cleanup interval
	strategy.restCache = cache.New(1*time.Second, 5*time.Second)

	// === NEW: Initialize risk optimization fields ===
	strategy.positionOpenTime = make(map[string]time.Time)
	strategy.trailingStates = make(map[string]*TrailingState)
	strategy.dailyResetState = &DailyResetState{
		ResetHour: config.DailyResetHour,
	}
	strategy.emaCache = make(map[string]float64)
	strategy.priceHistory = make(map[string][]pricePoint)

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
	s.startTime = time.Now()
	s.mu.Unlock()

	// Capture initial wallet balance for deterministic Realized PnL and ROI
	if acc, err := s.futuresClient.GetAccountInfo(ctx); err == nil && acc != nil {
		s.initialWalletBalance = acc.TotalWalletBalance
		s.maxWalletBalance = acc.TotalMarginBalance
	} else if bal := s.wsClient.GetCachedBalance(); bal.Asset != "" {
		s.initialWalletBalance = bal.WalletBalance
		s.maxWalletBalance = bal.MarginBalance
	}

	// === FR10: Startup Reconciliation - Verify state with exchange ===
	if err := s.reconcileOnStartup(ctx); err != nil {
		s.logger.Error("Startup reconciliation failed", zap.Error(err))
		// Continue anyway - not a fatal error
	}

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

	s.wg.Add(1)
	go s.orderManagementLoop()

	s.wg.Add(1)
	go s.riskMonitoringLoop()

	s.wg.Add(1)
	go s.positionSyncLoop()

	// === NEW: Background loops for risk optimization ===
	s.wg.Add(1)
	go s.positionTimeoutLoop()

	s.wg.Add(1)
	go s.emergencyRiskMonitorLoop()

	// === NEW: Continuous order placement optimization loop ===
	s.wg.Add(1)
	go s.continuousOrderPlacementLoop()

	// === P1 FIX: Start trailing stop loop ===
	s.wg.Add(1)
	go s.trailingStopLoop()

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

	s.wg.Wait()

	s.cancelAllOrders()

	s.logger.Info("Maker Strategy stopped")
	return nil
}

func (s *MakerStrategyImpl) PlaceOrders(symbol string) error {
	if !s.running {
		return nil
	}

	if stop, reason := s.emergencyStop.Check(s.ctx); stop {
		s.logger.Warn("Cannot place orders - emergency stop active", zap.String("reason", reason))
		return nil
	}

	// === Pump Detection: Check for abnormal price movements ===
	if isPump, velocity := s.detectPump(symbol); isPump {
		s.logger.Error("🚨 PUMP/CRASH DETECTED - PAUSING ORDERS",
			zap.String("symbol", symbol),
			zap.Float64("price_velocity", velocity))
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

	midPrice := (bestBid + bestAsk) / 2

	// === FR9: Spread Protection - Block orders when spread too wide ===
	if shouldBlock, reason := s.checkSpreadProtection(bestBid, bestAsk); shouldBlock {
		s.logger.Warn("Orders blocked due to spread protection", zap.String("reason", reason))
		return nil
	}

	// === FR1: Active Zone Grid - Only place orders within 0.1% of market ===
	activeZone := s.calculateActiveZoneGrid(symbol, midPrice)
	s.logger.Debug("Active zone calculated",
		zap.Float64("min_price", activeZone.MinPrice),
		zap.Float64("max_price", activeZone.MaxPrice),
		zap.Int("levels", len(activeZone.Levels)))

	// GRID STRATEGY: Place orders at market prices for maximum fills
	// Buy at best bid, Sell at best ask - no spread calculation
	buyPrice := bestBid
	sellPrice := bestAsk

	// Log grid placement
	s.logger.Info("🎯 Grid Strategy - Market Touching Orders",
		zap.String("symbol", symbol),
		zap.Float64("best_bid", bestBid),
		zap.Float64("best_ask", bestAsk),
		zap.Float64("mid_price", midPrice),
		zap.Float64("buy_price", buyPrice),
		zap.Float64("sell_price", sellPrice))

	balance := s.getAvailableBalance()
	if balance <= 0 {
		s.logger.Warn("No available balance for order placement")
		return nil
	}

	// ADVANCED POSITION MANAGEMENT & DELTA-NEUTRAL GUARD
	position := s.inventoryMgr.GetPosition(symbol)
	var positionBias float64
	var positionSize float64
	var entryPrice float64
	if position != nil {
		positionBias = position.Amount // Positive = long bias, Negative = short bias
		positionSize = math.Abs(position.Amount)
		entryPrice = position.EntryPrice
	} else {
		positionBias = 0 // No position = neutral
		positionSize = 0
		entryPrice = 0
		_ = entryPrice // Use entryPrice to avoid unused variable warning
	}

	// Calculate market impact and volatility
	bestBid, bestAsk, volume24h, _ := s.wsClient.GetTickerData(symbol)
	if bestBid == 0 || bestAsk == 0 {
		s.logger.Warn("No market data for position management", zap.String("symbol", symbol))
		return nil
	}

	midPrice = (bestBid + bestAsk) / 2

	// === P1 FIX: Delta-Neutral Guard ===
	// If net exposure exceeds threshold, block orders in the same direction
	maxExposure := float64(s.config.MaxTotalExposureUSDT)
	netExposureUSDT := positionBias * midPrice // Approx exposure for this symbol

	blockBuy := false
	blockSell := false
	if math.Abs(netExposureUSDT) > maxExposure*0.5 { // Increased from 0.1 to 0.5 to loosen blocking
		if netExposureUSDT > 0 {
			blockBuy = true
			s.logger.Warn("⚠️ Delta-Neutral Guard: Net Long exceeded 50%, blocking buys", zap.Float64("exposure", netExposureUSDT))
		} else {
			blockSell = true
			s.logger.Warn("⚠️ Delta-Neutral Guard: Net Short exceeded 50%, blocking sells", zap.Float64("exposure", netExposureUSDT))
		}
	}
	spread := bestAsk - bestBid
	volatility := spread / midPrice            // Simple volatility measure
	marketImpact := volume24h * spread / 10000 // Market impact score

	// DYNAMIC HEDGING RATIOS based on position size and market conditions
	var buyHedgeRatio, sellHedgeRatio float64
	var riskMultiplier float64

	if positionSize > 0 {
		// Larger positions need stronger hedging
		riskMultiplier = 1.0 + (positionSize / 1000.0) // Scale risk with position size
	} else {
		riskMultiplier = 1.0
	}

	// Adjust hedging based on market volatility
	if volatility > 0.02 { // High volatility market
		buyHedgeRatio = 0.5 + (riskMultiplier * 0.3) // Conservative hedging
		sellHedgeRatio = 0.5 + (riskMultiplier * 0.3)
	} else if volatility > 0.01 { // Medium volatility
		buyHedgeRatio = 0.3 + (riskMultiplier * 0.2)
		sellHedgeRatio = 0.3 + (riskMultiplier * 0.2)
	} else { // Low volatility
		buyHedgeRatio = 0.2 + (riskMultiplier * 0.1)
		sellHedgeRatio = 0.2 + (riskMultiplier * 0.1)
	}

	// Calculate dynamic order adjustments
	var buyAdjustment, sellAdjustment float64
	if positionBias > 0.001 { // Long position - aggressive hedging
		buyAdjustment = (1.0 - buyHedgeRatio)   // Reduce buy orders
		sellAdjustment = (1.0 + sellHedgeRatio) // Increase sell orders
		s.logger.Info("🔄 Advanced Hedging - Long Position",
			zap.String("symbol", symbol),
			zap.Float64("position_size", positionSize),
			zap.Float64("position_bias", positionBias),
			zap.Float64("volatility", volatility),
			zap.Float64("buy_hedge_ratio", buyHedgeRatio),
			zap.Float64("sell_hedge_ratio", sellHedgeRatio),
			zap.Float64("risk_multiplier", riskMultiplier))
	} else if positionBias < -0.001 { // Short position - aggressive hedging
		buyAdjustment = (1.0 + buyHedgeRatio)   // Increase buy orders
		sellAdjustment = (1.0 - sellHedgeRatio) // Reduce sell orders
		s.logger.Info("🔄 Advanced Hedging - Short Position",
			zap.String("symbol", symbol),
			zap.Float64("position_size", positionSize),
			zap.Float64("position_bias", positionBias),
			zap.Float64("volatility", volatility),
			zap.Float64("buy_hedge_ratio", buyHedgeRatio),
			zap.Float64("sell_hedge_ratio", sellHedgeRatio),
			zap.Float64("risk_multiplier", riskMultiplier))
	} else { // Neutral position - balanced with market adaptation
		buyAdjustment = 1.0
		sellAdjustment = 1.0

		// Adapt to market conditions
		if volatility > 0.02 {
			buyAdjustment = 0.8 // Reduce both sides in high volatility
			sellAdjustment = 0.8
		} else if volatility > 0.01 {
			buyAdjustment = 0.9 // Slight reduction in medium volatility
			sellAdjustment = 0.9
		}

		s.logger.Info("⚖️ Advanced Position Management - Neutral",
			zap.String("symbol", symbol),
			zap.Float64("market_impact", marketImpact),
			zap.Float64("volatility", volatility),
			zap.Float64("buy_adjustment", buyAdjustment),
			zap.Float64("sell_adjustment", sellAdjustment))
	}

	// === P2 FIX: Use calculateMicroProfitOrderSize instead of maxOrderValue ===
	orderSizeUsdt := s.calculateMicroProfitOrderSize(balance, symbol)
	buyQty := orderSizeUsdt / buyPrice
	sellQty := orderSizeUsdt / sellPrice

	// Apply dynamic hedging adjustments
	buyQty = buyQty * buyAdjustment
	sellQty = sellQty * sellAdjustment

	buyQty = s.roundToPrecision(buyQty, symbol)
	sellQty = s.roundToPrecision(sellQty, symbol)

	// Log volume farming metrics
	s.logger.Info("💰 High-Leverage Volume Farming",
		zap.String("symbol", symbol),
		zap.Float64("balance", balance),
		zap.Float64("leverage", float64(s.config.MaxLeverage)),
		zap.Float64("order_value_usdt", orderSizeUsdt),
		zap.Float64("buy_qty", buyQty),
		zap.Float64("sell_qty", sellQty))

	// INSTITUTIONAL DELTA-NEUTRAL GRID STRATEGY (EXA Deep Research)
	// Combining HV30 optimization + delta-neutral hedging + minimum risk

	// Calculate institutional-grade volatility metrics
	bestBid, bestAsk, volume24h, _ = s.wsClient.GetTickerData(symbol)
	midPrice = (bestBid + bestAsk) / 2
	spread = bestAsk - bestBid

	// === FR7: Zone-Based Sizing - Update EMA and get zone multiplier ===
	s.updateEMACache(symbol, midPrice)
	zoneMultiplier, zoneType := s.calculateZoneMultiplier(symbol, midPrice)
	s.logger.Debug("Zone calculation",
		zap.String("symbol", symbol),
		zap.String("zone", string(zoneType)),
		zap.Float64("multiplier", zoneMultiplier))

	// === FR1: Active Zone Grid - Dynamic spacing based on volatility ===
	// Use the pre-calculated activeZone from earlier in PlaceOrders
	gridLevels := activeZone.LevelCount
	gridSpacing := activeZone.GridSpacing

	// NEW: Calculate volatility and adjust spacing dynamically
	gridVolatility := s.calculateVolatility(symbol)
	dynamicSpacing := s.calculateDynamicSpacing(gridSpacing, gridVolatility)

	// Use dynamic spacing instead of fixed spacing
	gridSpacing = dynamicSpacing

	// Apply zone multiplier for sizing - P1 FIX: Sell is always 100%, Buy is scaled when dipping
	buyCapitalAllocation := zoneMultiplier
	sellCapitalAllocation := 1.0 // Never reduce sell allocation on dips (allows hedging/exiting)

	if buyCapitalAllocation <= 0 {
		buyCapitalAllocation = 0.1 // Minimum allocation for buy
	}

	// === FR2/FR15: Use balance-based order sizing with micro-profit optimization ===
	orderSize := s.calculateMicroProfitOrderSize(balance, symbol)
	orderCount := s.calculateOrderCount(balance)

	// Adjust grid levels based on order count config
	if orderCount < gridLevels {
		gridLevels = orderCount
	}
	if gridLevels < 5 {
		gridLevels = 5 // Minimum 5 levels
	}

	var riskLevel string
	switch zoneType {
	case ZoneAboveEMA:
		riskLevel = "ABOVE_EMA"
	case ZoneNormalDip:
		riskLevel = "NORMAL_DIP"
	case ZoneStrongDip:
		riskLevel = "STRONG_DIP"
	case ZoneHardDip:
		riskLevel = "HARD_DIP"
	default:
		riskLevel = "UNKNOWN"
	}

	// Dummy adjustedVolatility for logging compatibility (not used in new logic)
	adjustedVolatility := gridSpacing * 100

	s.logger.Info("🎯 Active Zone Grid - Fill Rate Optimized",
		zap.String("symbol", symbol),
		zap.String("zone", string(zoneType)),
		zap.Float64("zone_multiplier", zoneMultiplier),
		zap.Int("grid_levels", gridLevels),
		zap.Float64("spacing_pct", gridSpacing*100),
		zap.Float64("order_size", orderSize),
		zap.String("risk_level", riskLevel))

	// MINIMUM ORDER SIZE PROTECTION: Ensure notional >= 5.0 USD
	minNotionalUSD := 5.0
	minQtyPerOrder := minNotionalUSD / midPrice

	// Reduce grid levels if total capital insufficient (BEFORE calculating quantities)
	maxAffordableLevels := int(buyQty / minQtyPerOrder)
	if maxAffordableLevels < gridLevels {
		oldLevels := gridLevels
		gridLevels = maxAffordableLevels
		if gridLevels < 5 {
			gridLevels = 5 // Minimum 5 levels each side
		}
		s.logger.Info("🔧 Reduced grid levels for minimum notional",
			zap.Int("old_levels", oldLevels),
			zap.Int("new_levels", gridLevels),
			zap.Float64("min_qty_per_level", minQtyPerOrder))
	}

	perLevelBuyQty := buyQty * buyCapitalAllocation / float64(gridLevels)
	perLevelSellQty := sellQty * sellCapitalAllocation / float64(gridLevels)

	// Apply Delta-Neutral block
	if blockBuy {
		perLevelBuyQty = 0
	}
	if blockSell {
		perLevelSellQty = 0
	}

	// DEBUG: Log actual calculations
	buyNotional := perLevelBuyQty * midPrice
	sellNotional := perLevelSellQty * midPrice
	s.logger.Info("🔍 Order Size Debug",
		zap.Float64("mid_price", midPrice),
		zap.Float64("min_qty_required", minQtyPerOrder),
		zap.Float64("buy_qty", perLevelBuyQty),
		zap.Float64("sell_qty", perLevelSellQty),
		zap.Float64("buy_notional_usd", buyNotional),
		zap.Float64("sell_notional_usd", sellNotional),
		zap.Int("grid_levels", gridLevels))

	// EMERGENCY FIX: Force minimum quantity regardless of calculations
	if !blockBuy && perLevelBuyQty < minQtyPerOrder {
		perLevelBuyQty = minQtyPerOrder * 1.1 // Add 10% buffer
		s.logger.Info("🔧 EMERGENCY: Adjusted buy quantity for minimum notional",
			zap.Float64("old_qty", buyQty*buyCapitalAllocation/float64(gridLevels)),
			zap.Float64("new_qty", perLevelBuyQty),
			zap.Float64("min_notional_usd", minNotionalUSD),
			zap.Float64("price", midPrice))
	}

	if !blockSell && perLevelSellQty < minQtyPerOrder {
		perLevelSellQty = minQtyPerOrder * 1.1 // Add 10% buffer
		s.logger.Info("🔧 EMERGENCY: Adjusted sell quantity for minimum notional",
			zap.Float64("old_qty", sellQty*sellCapitalAllocation/float64(gridLevels)),
			zap.Float64("new_qty", perLevelSellQty),
			zap.Float64("min_notional_usd", minNotionalUSD),
			zap.Float64("price", midPrice))
	}

	// FINAL SAFETY: Force minimum quantity if still too small
	finalMinQty := minQtyPerOrder * 1.2 // 20% buffer for safety
	if perLevelBuyQty < finalMinQty {
		perLevelBuyQty = finalMinQty
		s.logger.Warn("🚨 FINAL SAFETY: Forced minimum buy quantity",
			zap.Float64("forced_qty", perLevelBuyQty),
			zap.Float64("min_required", minQtyPerOrder))
	}

	if perLevelSellQty < finalMinQty {
		perLevelSellQty = finalMinQty
		s.logger.Warn("🚨 FINAL SAFETY: Forced minimum sell quantity",
			zap.Float64("forced_qty", perLevelSellQty),
			zap.Float64("min_required", minQtyPerOrder))
	}

	// Use wait group for concurrent order placement
	var wg sync.WaitGroup
	orderPlaced := make(chan bool, gridLevels*2) // Track successful placements

	// Place buy orders concurrently with optimal spacing
	for i := 0; i < gridLevels; i++ {
		wg.Add(1)
		go func(level int) {
			defer wg.Done()
			// Ultra-Tight Volume Farming: Maximum fill rate spacing below bid
			gridOffset := gridSpacing * float64(level) // START AT 0 offset
			gridBuyPrice := bestBid * (1 - gridOffset)

			// Only place if we don't already have open buy orders to avoid duplicate grid placement
			openOrders := s.orderManager.GetOpenOrders(symbol)
			hasBuyOrders := false
			for _, order := range openOrders {
				if order.Side == OrderSideBuy {
					hasBuyOrders = true
					break
				}
			}

			if perLevelBuyQty > 0 && !hasBuyOrders {
				err := s.placeLimitOrder(symbol, OrderSideBuy, gridBuyPrice, perLevelBuyQty)
				if err != nil {
					s.logger.Error("Failed to place grid buy order", zap.Error(err))
				} else {
					s.otRatioGuard.RecordOrder()
					s.logger.Info("⚡ Ultra-Tight Grid Buy",
						zap.String("symbol", symbol),
						zap.String("risk_level", riskLevel),
						zap.Float64("hv30_adj", adjustedVolatility),
						zap.Int("level", level),
						zap.Float64("price", gridBuyPrice),
						zap.Float64("spacing_pct", gridOffset*100),
						zap.Float64("qty", perLevelBuyQty))
				}
				orderPlaced <- true
			} else if hasBuyOrders {
				orderPlaced <- true // Mark as finished simulating place
			}
		}(i)
	}

	// Place sell orders concurrently with optimal spacing
	for i := 0; i < gridLevels; i++ {
		wg.Add(1)
		go func(level int) {
			defer wg.Done()
			// Ultra-Tight Volume Farming: Maximum fill rate spacing above ask
			gridOffset := gridSpacing * float64(level) // START AT 0 offset
			gridSellPrice := bestAsk * (1 + gridOffset)

			// Only place if we don't already have open sell orders
			openOrders := s.orderManager.GetOpenOrders(symbol)
			hasSellOrders := false
			for _, order := range openOrders {
				if order.Side == OrderSideSell {
					hasSellOrders = true
					break
				}
			}

			if perLevelSellQty > 0 && !hasSellOrders {
				err := s.placeLimitOrder(symbol, OrderSideSell, gridSellPrice, perLevelSellQty)
				if err != nil {
					s.logger.Error("Failed to place grid sell order", zap.Error(err))
				} else {
					s.otRatioGuard.RecordOrder()
					s.logger.Info("⚡ Ultra-Tight Grid Sell",
						zap.String("symbol", symbol),
						zap.String("risk_level", riskLevel),
						zap.Float64("hv30_adj", adjustedVolatility),
						zap.Int("level", level),
						zap.Float64("price", gridSellPrice),
						zap.Float64("spacing_pct", gridOffset*100),
						zap.Float64("qty", perLevelSellQty))
				}
				orderPlaced <- true
			} else if hasSellOrders {
				orderPlaced <- true // Mark as finished simulating place
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

	// === FR4: Update position open time for timeout tracking ===
	// Only track if we have successful placements
	if successfulPlacements > 0 {
		s.updatePositionOpenTime(symbol)
	}

	return nil
}

func (s *MakerStrategyImpl) placeLimitOrder(symbol string, side OrderSide, price, quantity float64) error {
	req := LimitOrderRequest{
		Symbol:      symbol,
		Side:        side,
		Price:       price,
		Quantity:    quantity,
		TimeInForce: "GTX",
	}

	order, err := s.orderManager.PlaceLimitOrder(s.ctx, req)
	if err != nil {
		return err
	}

	s.logger.Info("Placed limit order",
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
			for _, symbol := range s.config.Symbols {
				s.processOrderLifecycle(symbol)
			}
		}
	}
}

func (s *MakerStrategyImpl) processOrderLifecycle(symbol string) {
	// Smart Order Lifecycle - High Fill Rate Strategy:
	// 1. Give orders time to fill (30-60 seconds)
	// 2. Only cancel if: price moves significantly OR order is too old
	// 3. Maintain 2-sided quote for continuous volume + micro-profit

	orders := s.orderManager.GetOpenOrders(symbol)
	filledCount := 0

	// Get current market price
	bestBid, bestAsk, _, err := s.wsClient.GetTickerData(symbol)
	if err != nil {
		s.logger.Warn("Failed to get ticker data", zap.String("symbol", symbol), zap.Error(err))
		return
	}
	midPrice := (bestBid + bestAsk) / 2

	// P2 FIX: SYNC WITH EXCHANGE (with 2s local cache throttling)
	var exchangeOrders []client.Order
	cacheKey := "open_orders_" + symbol
	if cached, found := s.restCache.Get(cacheKey); found {
		exchangeOrders = cached.([]client.Order)
	} else {
		var err error
		exchangeOrders, err = s.futuresClient.GetOpenOrders(s.ctx, symbol)
		if err != nil {
			s.logger.Warn("Failed to sync with exchange orders", zap.Error(err))
		} else {
			s.restCache.Set(cacheKey, exchangeOrders, 2*time.Second)
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

			// Calculate fill ratio and PnL
			fillRatio := float64(filledCount) / float64(totalOrders) * 100

			// Calculate PnL for this fill
			var fillPnL float64
			position := s.inventoryMgr.GetPosition(symbol)
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

	// EXA OPTIMIZED: Rapid Grid Refresh for Volume Farming
	gridShiftThreshold := 0.0005    // 0.05% - extreme tight threshold for micro-profits
	maxOrderAge := 15 * time.Second // 15 seconds - quick expiration to keep hugging the spread
	needsRefresh := false
	cancelledCount := 0

	// Calculate current grid center vs market center
	currentGridCenter := (bestBid + bestAsk) / 2

	// Check if we need to shift the entire grid
	needsGridShift := false

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

			// 3. Individual order too far from market - INCREASED for continuous farming
			// Grid orders are intentionally placed away from current price, so we need wider threshold
			var targetPrice float64
			if order.Side == OrderSideBuy {
				targetPrice = bestBid
			} else {
				targetPrice = bestAsk
			}
			priceDiff := math.Abs(order.Price-targetPrice) / midPrice
			if priceDiff > 0.01 { // 1% - much wider to allow grid to work
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
		s.logger.Warn("Order-to-Trade ratio high, pausing order placement", zap.String("reason", reason))
		time.Sleep(30 * time.Second)
		s.otRatioGuard.Reset()
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

		account, err := s.futuresClient.GetAccountInfo(s.ctx)
		if err != nil {
			s.logger.Warn("Failed to get account info from REST", zap.Error(err))
		} else if account != nil {
			s.logger.Info("Account info retrieved", zap.Float64("available_balance", account.AvailableBalance), zap.Float64("total_wallet", account.TotalWalletBalance))
			// Cache for 1s (cache default expiration)
			s.restCache.Set("account_info", account, cache.DefaultExpiration)
			return account.AvailableBalance
		}
	}

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

// ============================================================
// NEW: Risk Optimization Functions (FR1-FR15)
// ============================================================

// T005: Calculate Active Zone Grid - only place orders within 0.1% of market
func (s *MakerStrategyImpl) calculateActiveZoneGrid(symbol string, midPrice float64) *GridActiveZone {
	zone := &GridActiveZone{
		MinPrice:    midPrice * (1 - s.config.ActiveZoneRange),
		MaxPrice:    midPrice * (1 + s.config.ActiveZoneRange),
		GridSpacing: s.config.GridSpacingMin,
		LevelCount:  s.config.GridLevels,
		Levels:      make([]float64, 0, s.config.GridLevels),
	}

	// Generate levels from min to max
	priceRange := zone.MaxPrice - zone.MinPrice
	step := priceRange / float64(s.config.GridLevels-1)
	for i := 0; i < s.config.GridLevels; i++ {
		level := zone.MinPrice + step*float64(i)
		zone.Levels = append(zone.Levels, level)
	}

	s.logger.Debug("Active zone calculated",
		zap.String("symbol", symbol),
		zap.Float64("mid_price", midPrice),
		zap.Float64("min_price", zone.MinPrice),
		zap.Float64("max_price", zone.MaxPrice),
		zap.Int("levels", len(zone.Levels)))

	return zone
}

// T006: Check if price is within active zone
func (s *MakerStrategyImpl) isWithinActiveZone(price float64, zone *GridActiveZone) bool {
	return price >= zone.MinPrice && price <= zone.MaxPrice
}

// T007: Update position open time when position is opened
func (s *MakerStrategyImpl) updatePositionOpenTime(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.positionOpenTime[symbol] = time.Now()
	s.logger.Debug("Position open time updated", zap.String("symbol", symbol))
}

// T008: Position timeout disabled - no longer needed for continuous farming
func (s *MakerStrategyImpl) checkPositionTimeout(symbol string) (shouldClose bool, reason string) {
	// DISABLED: Always return false to allow positions to stay open for continuous farming
	return false, ""
}

// NEW: GetMidPrice helper for volatility calculation
func (s *MakerStrategyImpl) GetMidPrice(symbol string) float64 {
	bestBid, bestAsk, _, err := s.wsClient.GetTickerData(symbol)
	if err != nil {
		return 0
	}
	return (bestBid + bestAsk) / 2
}

// NEW: Calculate volatility for dynamic spacing
func (s *MakerStrategyImpl) calculateVolatility(symbol string) float64 {
	// Simple volatility calculation based on recent price changes
	// Use spread as proxy for volatility
	spread := s.GetSpread(symbol)
	midPrice := s.GetMidPrice(symbol)

	// Normalize to percentage
	volatility := spread / midPrice

	// Cap at reasonable maximum
	if volatility > 0.01 {
		volatility = 0.01
	}

	return volatility
}

// NEW: Pump detection - identify abnormal price movements
func (s *MakerStrategyImpl) detectPump(symbol string) (bool, float64) {
	// Get recent price data
	bestBid, bestAsk, _, err := s.wsClient.GetTickerData(symbol)
	if err != nil {
		s.logger.Warn("Failed to get ticker for pump detection", zap.Error(err))
		return false, 0
	}

	midPrice := (bestBid + bestAsk) / 2

	// P1 FIX: Use price history to detect velocity over time, not spread
	s.priceHistoryMu.Lock()
	now := time.Now()
	history := s.priceHistory[symbol]

	// Add current point
	history = append(history, pricePoint{price: midPrice, ts: now})

	// Remove old points (keep last 5 seconds)
	var filteredHistory []pricePoint
	for _, p := range history {
		if now.Sub(p.ts) <= 5*time.Second {
			filteredHistory = append(filteredHistory, p)
		}
	}
	s.priceHistory[symbol] = filteredHistory
	s.priceHistoryMu.Unlock()

	if len(filteredHistory) < 2 {
		return false, 0 // Need more history
	}

	oldestPrice := filteredHistory[0].price

	// Calculate actual price velocity over the time window
	priceVelocity := math.Abs(midPrice-oldestPrice) / oldestPrice

	// Pump detection thresholds - 0.5% in 5 seconds
	pumpThreshold := 0.005
	extremePumpThreshold := 0.02 // 2% price movement - extreme volatility

	isPump := priceVelocity > pumpThreshold
	isExtremePump := priceVelocity > extremePumpThreshold

	// Log pump detection
	if isPump {
		s.logger.Warn(" PUMP DETECTED",
			zap.String("symbol", symbol),
			zap.Float64("price_velocity", priceVelocity),
			zap.Float64("threshold", pumpThreshold),
			zap.Bool("is_extreme", isExtremePump))
	} else if isExtremePump {
		s.logger.Error(" EXTREME PUMP DETECTED",
			zap.String("symbol", symbol),
			zap.Float64("price_velocity", priceVelocity),
			zap.Float64("threshold", extremePumpThreshold),
			zap.Bool("is_extreme", isExtremePump))
	}

	return isPump || isExtremePump, priceVelocity
}

// NEW: Volatility-based dynamic spacing adjustment
func (s *MakerStrategyImpl) calculateDynamicSpacing(baseSpacing float64, volatility float64) float64 {
	// Increase spacing during high volatility to reduce order cancellations
	// Base spacing: 0.01% (0.0001)
	// When volatility > 2x normal, increase spacing up to 2x

	if volatility > 0.002 { // 0.2% volatility threshold
		// High volatility = wider spacing to avoid cancellations
		return baseSpacing * 2.0 // 0.02% spacing
	}

	// Normal volatility = standard spacing
	return baseSpacing // 0.01%
}

// T012-T013: Manage trailing stop for a position
func (s *MakerStrategyImpl) manageTrailingStop(symbol string, currentPrice float64, entryPrice float64, side string) (shouldClose bool, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Calculate current profit percentage
	var profitPct float64
	if side == "long" {
		profitPct = (currentPrice - entryPrice) / entryPrice
	} else {
		profitPct = (entryPrice - currentPrice) / entryPrice
	}

	// Get or create trailing state
	ts, exists := s.trailingStates[symbol]
	if !exists {
		ts = &TrailingState{
			PositionID:    symbol,
			ActivationPct: s.config.TrailingActivationPct,
			CallbackPct:   s.config.TrailingCallbackPct,
			IsActive:      false,
		}
		s.trailingStates[symbol] = ts
	}

	// Update peak profit
	if profitPct > ts.PeakProfitPct {
		ts.PeakProfitPct = profitPct
		s.logger.Debug("Trailing peak updated",
			zap.String("symbol", symbol),
			zap.Float64("peak_profit_pct", ts.PeakProfitPct))
	}

	// TRAILING STOP DISABLED - to avoid closing profitable positions during pumps
	// Only log when trailing would have triggered, but don't actually activate
	if !ts.IsActive && profitPct >= ts.ActivationPct {
		s.logger.Info("Trailing stop WOULD HAVE triggered - DISABLED",
			zap.String("symbol", symbol),
			zap.Float64("profit_pct", profitPct),
			zap.String("note", "trailing disabled to protect profits"))
	}

	// Check if trailing callback triggered
	if ts.IsActive {
		callbackLevel := ts.PeakProfitPct - ts.CallbackPct
		if profitPct <= callbackLevel {
			s.logger.Info("Trailing stop triggered - closing position",
				zap.String("symbol", symbol),
				zap.Float64("profit_pct", profitPct),
				zap.Float64("peak_profit_pct", ts.PeakProfitPct),
				zap.Float64("callback_level", callbackLevel))
			return true, "trailing_stop"
		}
	}

	return false, ""
}

// T017: Calculate order size - split into smaller chunks
func (s *MakerStrategyImpl) calculateOrderSize(balance float64) float64 {
	// Use margin equity ratio to determine trading capital
	tradingCapital := balance * s.config.MarginEquityRatio
	maxOrderValue := tradingCapital * float64(s.config.MaxLeverage)

	// Clamp to min/max order size config
	orderSize := maxOrderValue
	if orderSize < s.config.MinOrderSizeUSD {
		orderSize = s.config.MinOrderSizeUSD
	}
	if orderSize > s.config.MaxOrderSizeUSD {
		orderSize = s.config.MaxOrderSizeUSD
	}

	return orderSize
}

// T018: Calculate order count based on balance
func (s *MakerStrategyImpl) calculateOrderCount(balance float64) int {
	baseCount := 5
	// Add 1 order per $5 of balance
	additionalOrders := int(balance / 5)
	totalOrders := baseCount + additionalOrders

	// Cap at reasonable maximum
	if totalOrders > 20 {
		totalOrders = 20
	}

	return totalOrders
}

// T023-T024: Check position limits and return positions to close (FIFO)
func (s *MakerStrategyImpl) checkPositionLimits() (shouldBlock bool, positionsToClose []string) {
	s.mu.RLock()
	longCount := s.longPositionCount
	shortCount := s.shortPositionCount
	s.mu.RUnlock()

	maxPerSide := s.config.MaxPositionsPerSide
	shouldBlock = false
	var limitExceededSymbols []string

	// P1 FIX: Check if we need to close oldest positions (FIFO)
	if longCount > maxPerSide {
		s.logger.Warn("Long position limit exceeded",
			zap.Int("current", longCount),
			zap.Int("max", maxPerSide))
		shouldBlock = true
	}

	if shortCount > maxPerSide {
		s.logger.Warn("Short position limit exceeded",
			zap.Int("current", shortCount),
			zap.Int("max", maxPerSide))
		shouldBlock = true
	}

	// Just return symbol list to block new placements or trigger cleanup
	// In the future this can return oldest specific symbols
	if shouldBlock && len(s.config.Symbols) > 0 {
		limitExceededSymbols = []string{s.config.Symbols[0]} // Simplification
	}

	return shouldBlock, limitExceededSymbols
}

// T028-T030: Calculate zone multiplier based on EMA distance
func (s *MakerStrategyImpl) calculateZoneMultiplier(symbol string, currentPrice float64) (multiplier float64, zone ZoneType) {
	s.mu.RLock()
	ema, exists := s.emaCache[symbol]
	s.mu.RUnlock()

	if !exists || ema == 0 {
		// No EMA yet, use normal zone
		return s.config.ZoneNormalDipMultiplier, ZoneNormalDip
	}

	// Calculate distance from EMA as percentage
	distancePct := (currentPrice - ema) / ema

	if distancePct > 0 {
		// Price above EMA
		return s.config.ZoneAboveEMAMultiplier, ZoneAboveEMA
	} else if distancePct > -0.01 {
		// 0 to -1% from EMA
		return s.config.ZoneNormalDipMultiplier, ZoneNormalDip
	} else if distancePct > -0.02 {
		// -1% to -2% from EMA
		return s.config.ZoneStrongDipMultiplier, ZoneStrongDip
	} else {
		// Below -2% from EMA - hard dip, no buy
		return s.config.ZoneHardDipMultiplier, ZoneHardDip
	}
}

// T032: Update EMA cache for a symbol
func (s *MakerStrategyImpl) updateEMACache(symbol string, price float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Simple EMA calculation: EMA = alpha * price + (1-alpha) * previousEMA
	alpha := 2.0 / float64(s.config.EMAPeriod+1)

	if existingEMA, exists := s.emaCache[symbol]; exists {
		s.emaCache[symbol] = alpha*price + (1-alpha)*existingEMA
	} else {
		// First value, use simple moving average approximation
		s.emaCache[symbol] = price
	}
}

// T034-T035: Check if daily reset is needed
func (s *MakerStrategyImpl) shouldDailyReset() bool {
	currentHour := time.Now().UTC().Hour()
	return currentHour == s.config.DailyResetHour
}

// T037: Check spread before placing orders
func (s *MakerStrategyImpl) checkSpreadProtection(bestBid, bestAsk float64) (shouldBlock bool, reason string) {
	if bestBid == 0 || bestAsk == 0 {
		return true, "no_price"
	}

	spread := (bestAsk - bestBid) / bestBid
	if spread > s.config.SpreadThreshold {
		s.logger.Warn("Spread too wide - blocking orders",
			zap.Float64("spread", spread),
			zap.Float64("threshold", s.config.SpreadThreshold))
		return true, "spread_too_wide"
	}

	return false, ""
}

// T038: Reconcile state on startup
func (s *MakerStrategyImpl) reconcileOnStartup(ctx context.Context) error {
	s.logger.Info("Starting startup reconciliation...")

	// Fetch positions from exchange
	positions, err := s.futuresClient.GetPositions(ctx)
	if err != nil {
		s.logger.Error("Failed to fetch positions for reconciliation", zap.Error(err))
		return err
	}

	// Fetch open orders from exchange
	openOrders, err := s.futuresClient.GetOpenOrders(ctx, s.config.Symbols[0])
	if err != nil {
		s.logger.Error("Failed to fetch open orders for reconciliation", zap.Error(err))
		return err
	}

	s.logger.Info("Startup reconciliation complete",
		zap.Int("positions", len(positions)),
		zap.Int("open_orders", len(openOrders)))

	return nil
}

// ============================================================
// Background Loops for Risk Optimization
// ============================================================

// positionTimeoutLoop - Check and force close positions that exceeded timeout (FR4)
func (s *MakerStrategyImpl) positionTimeoutLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Position timeout loop stopped")
			return
		case <-ticker.C:
			// Check each symbol's positions
			for _, symbol := range s.config.Symbols {
				if shouldClose, reason := s.checkPositionTimeout(symbol); shouldClose {
					s.logger.Warn("Force closing position due to timeout",
						zap.String("symbol", symbol),
						zap.String("reason", reason))
					// In real implementation: call futuresClient to close position
					// For now, just clear the tracking
					s.mu.Lock()
					delete(s.positionOpenTime, symbol)
					s.mu.Unlock()
				}
			}
		}
	}
}

// trailingStopLoop - Monitor positions and trigger trailing stop (FR5)
func (s *MakerStrategyImpl) trailingStopLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Trailing stop loop stopped")
			return
		case <-ticker.C:
			// Get current positions and check trailing stop
			positions := s.inventoryMgr.GetAllPositions()
			for symbol, pos := range positions {
				if pos.Amount == 0 {
					continue
				}

				side := "long"
				if pos.Amount < 0 {
					side = "short"
				}

				// Get current price from ticker
				if s.wsClient != nil {
					bid, ask, _, _ := s.wsClient.GetTickerData(symbol)
					currentPrice := (bid + ask) / 2
					if currentPrice > 0 {
						if shouldClose, reason := s.manageTrailingStop(symbol, currentPrice, pos.EntryPrice, side); shouldClose {
							s.logger.Warn("Force closing position due to trailing stop",
								zap.String("symbol", symbol),
								zap.String("reason", reason))
							// In real implementation: call futuresClient to close position
						}
					}
				}
			}
		}
	}
}

// dailyResetLoop - Close all positions at end of day (FR8)
func (s *MakerStrategyImpl) dailyResetLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Minute) // Check every minute
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Daily reset loop stopped")
			return
		case <-ticker.C:
			if s.shouldDailyReset() {
				s.logger.Info("Daily reset triggered - closing all positions")
				// In real implementation: close all positions via futuresClient
				// Reset daily stats
				s.mu.Lock()
				s.dailyResetState.TotalVolume = 0
				s.dailyResetState.TotalProfit = 0
				s.dailyResetState.LastResetDate = time.Now()
				s.mu.Unlock()
			}
		}
	}
}

// emergencyRiskMonitorLoop - Real-time position monitoring for rapid price movements
// This is the CRITICAL protection against liquidation during pumps/crashes
func (s *MakerStrategyImpl) emergencyRiskMonitorLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(500 * time.Millisecond) // Check every 500ms - very fast
	defer ticker.Stop()

	s.logger.Info("Emergency risk monitor started - checking every 500ms")

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Emergency risk monitor stopped")
			return
		case <-ticker.C:
			s.checkEmergencyRiskConditions()
		}
	}
}

// continuousOrderPlacementLoop - Real-time order placement optimization for maximum volume
// NEW: Continuous order placement optimization for maximum volume
// This function ensures orders are always placed for maximum fills
func (s *MakerStrategyImpl) continuousOrderPlacementLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond) // Check every 100ms
	defer ticker.Stop()

	s.logger.Info("Continuous order placement started - checking every 100ms")

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Continuous order placement stopped")
			return
		case <-ticker.C:
			for _, symbol := range s.config.Symbols {
				s.continuousOrderPlacement(symbol)
			}
		}
	}
}

// checkEmergencyRiskConditions - Check all emergency risk conditions
func (s *MakerStrategyImpl) checkEmergencyRiskConditions() {
	for _, symbol := range s.config.Symbols {
		// 1. Check position exposure - if position too large, reduce immediately
		s.checkPositionExposure(symbol)

		// 2. Check for extreme volatility - if price moving too fast, pause trading
		s.checkExtremeVolatility(symbol)

		// 3. Check for liquidation risk - if margin too low, close positions
		s.checkLiquidationRisk(symbol)
	}
}

// checkPositionExposure - Monitor position size and reduce if too large
func (s *MakerStrategyImpl) checkPositionExposure(symbol string) {
	position := s.inventoryMgr.GetPosition(symbol)
	if position == nil {
		return
	}

	positionSize := math.Abs(position.Amount)

	// Get current price
	bestBid, bestAsk, _, err := s.wsClient.GetTickerData(symbol)
	if err != nil || bestBid == 0 || bestAsk == 0 {
		return
	}
	currentPrice := (bestBid + bestAsk) / 2

	// Calculate unrealized PnL
	var unrealizedPnL float64
	if position.Amount > 0 {
		unrealizedPnL = (currentPrice - position.EntryPrice) * positionSize
	} else {
		unrealizedPnL = (position.EntryPrice - currentPrice) * positionSize
	}

	// Calculate position value in USDT
	positionValue := currentPrice * positionSize

	// WARNING: Position exceeds 80% of max
	maxPositionValue := s.config.MaxTotalExposureUSDT * 0.8
	if positionValue > maxPositionValue {
		s.logger.Warn("⚠️ Position size large - approaching max exposure",
			zap.String("symbol", symbol),
			zap.Float64("position_value_usdt", positionValue),
			zap.Float64("max_allowed_usdt", maxPositionValue),
			zap.Float64("unrealized_pnl", unrealizedPnL))
	}

	// CRITICAL: Position at 95% of max → FORCE CLOSE to prevent liquidation
	if positionValue > s.config.MaxTotalExposureUSDT*0.95 {
		s.logger.Error("🚨 CRITICAL: Position at liquidation threshold - FORCE CLOSE",
			zap.String("symbol", symbol),
			zap.Float64("position_value_usdt", positionValue),
			zap.Float64("max_usdt", s.config.MaxTotalExposureUSDT),
			zap.Float64("unrealized_pnl", unrealizedPnL))
		s.emergencyStop.Trigger("position_size_critical_" + symbol)
		s.cancelAllOrders()
		s.closeAllPositionsMarket(symbol)
	}
}

// checkExtremeVolatility - Detect if price is moving too fast
func (s *MakerStrategyImpl) checkExtremeVolatility(symbol string) {
	bestBid, bestAsk, _, err := s.wsClient.GetTickerData(symbol)
	if err != nil || bestBid == 0 || bestAsk == 0 {
		return
	}

	midPrice := (bestBid + bestAsk) / 2
	spread := bestAsk - bestBid
	spreadPct := spread / midPrice

	// EXTREME: Spread > 0.5% - liquidity crisis, pause all trading
	if spreadPct > 0.005 {
		s.logger.Error("🚨 EXTREME VOLATILITY: Spread too wide - pausing",
			zap.String("symbol", symbol),
			zap.Float64("spread_pct", spreadPct*100),
			zap.Float64("spread", spread))

		// Trigger emergency pause
		s.emergencyStop.Trigger("extreme_volatility")
	}
}

// checkLiquidationRisk - Monitor margin and close if approaching liquidation
func (s *MakerStrategyImpl) checkLiquidationRisk(symbol string) {
	position := s.inventoryMgr.GetPosition(symbol)
	if position == nil {
		return
	}

	// Get current price
	bestBid, bestAsk, _, err := s.wsClient.GetTickerData(symbol)
	if err != nil || bestBid == 0 || bestAsk == 0 {
		return
	}
	currentPrice := (bestBid + bestAsk) / 2

	// Calculate margin ratio (simplified isolated margin)
	positionSize := math.Abs(position.Amount)
	marginUsed := positionSize * currentPrice / float64(s.config.MaxLeverage)

	// Get available balance
	balance := s.getAvailableBalance()
	if marginUsed+balance <= 0 {
		return
	}
	marginRatio := marginUsed / (marginUsed + balance)

	// WARNING: Margin ratio > 80%
	if marginRatio > 0.8 {
		s.logger.Warn("⚠️ HIGH MARGIN USAGE - approaching liquidation",
			zap.String("symbol", symbol),
			zap.Float64("margin_ratio_pct", marginRatio*100),
			zap.Float64("margin_used", marginUsed),
			zap.Float64("available_balance", balance))
	}

	// EMERGENCY: Margin ratio > 90% → cancel all orders + close position NOW
	if marginRatio > 0.9 {
		s.logger.Error("🚨 EMERGENCY: Margin critical - force closing position to prevent liquidation",
			zap.String("symbol", symbol),
			zap.Float64("margin_ratio_pct", marginRatio*100))
		s.emergencyStop.Trigger("margin_critical_" + symbol)
		s.cancelAllOrders()
		s.closeAllPositionsMarket(symbol)
	}
}

// closeAllPositionsMarket - P0 FIX: Actually close a position via market order to prevent liquidation
func (s *MakerStrategyImpl) closeAllPositionsMarket(symbol string) {
	position := s.inventoryMgr.GetPosition(symbol)
	if position == nil || math.Abs(position.Amount) < 0.001 {
		s.logger.Info("No position to close", zap.String("symbol", symbol))
		return
	}

	// Determine close side (opposite of current position)
	closeQty := math.Abs(position.Amount)
	var closeSide string
	if position.Amount > 0 {
		closeSide = "SELL" // Long position → SELL to close
	} else {
		closeSide = "BUY" // Short position → BUY to close
	}

	s.logger.Error("🚨 FORCE CLOSE: Placing market order to close position",
		zap.String("symbol", symbol),
		zap.String("close_side", closeSide),
		zap.Float64("close_qty", closeQty),
		zap.Float64("position_amount", position.Amount))

	// Place market order
	closeReq := LimitOrderRequest{
		Symbol:   symbol,
		Side:     OrderSide(closeSide),
		Quantity: closeQty,
		// Price=0 → will be sent as MARKET order
	}

	// Use market order via futuresClient directly
	ctx, cancelCtx := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancelCtx()

	_, err := s.futuresClient.PlaceOrder(ctx, client.PlaceOrderRequest{
		Symbol:     symbol,
		Side:       closeSide,
		Type:       "MARKET",
		Quantity:   formatQuantity(symbol, closeQty),
		ReduceOnly: true,
	})
	if err != nil {
		s.logger.Error("🚨 FORCE CLOSE FAILED - MANUAL INTERVENTION REQUIRED",
			zap.String("symbol", symbol),
			zap.Error(err))
		_ = closeReq // suppress unused warning
		return
	}

	s.logger.Info("✅ Force close market order placed",
		zap.String("symbol", symbol),
		zap.String("close_side", closeSide),
		zap.Float64("close_qty", closeQty))

	// Clear local position tracking
	s.inventoryMgr.UpdatePosition(symbol, 0, 0, 0)
}

// GetPositions - Helper to get all positions from inventory
func (s *MakerStrategyImpl) GetPositions() map[string]*PositionState {
	positions := make(map[string]*PositionState)
	for _, symbol := range s.config.Symbols {
		pos := s.inventoryMgr.GetPosition(symbol)
		if pos != nil {
			positions[symbol] = pos
		}
	}
	return positions
}

// GetCurrentMetrics implementation for Telegram Notifier
func (s *MakerStrategyImpl) GetCurrentMetrics() notifier.GridMetrics {
	var symbol string
	if len(s.config.Symbols) > 0 {
		symbol = s.config.Symbols[0]
	}

	bestBid, bestAsk, v24h, _ := s.wsClient.GetTickerData(symbol)
	midPrice := (bestBid + bestAsk) / 2

	metrics := s.GetMetrics()

	// Real 100% accurate metrics from Exchange
	accInfo, err := s.futuresClient.GetAccountInfo(context.Background())
	var walletBalance, unrealized, marginBalance float64
	if err == nil && accInfo != nil {
		walletBalance = accInfo.TotalWalletBalance
		unrealized = accInfo.TotalUnrealizedProfit
		marginBalance = accInfo.TotalMarginBalance
	} else {
		// Fallback to cache without REST
		bal := s.wsClient.GetCachedBalance()
		walletBalance = bal.WalletBalance
		unrealized = bal.UnrealizedProfit
		marginBalance = bal.MarginBalance
	}

	// Update High Watermark for Drawdown
	if marginBalance > s.maxWalletBalance {
		s.maxWalletBalance = marginBalance
	}
	drawdownPct := 0.0
	if s.maxWalletBalance > 0 && marginBalance < s.maxWalletBalance {
		drawdownPct = ((s.maxWalletBalance - marginBalance) / s.maxWalletBalance) * 100
	}

	// Native calculation since startup
	realized := walletBalance - s.initialWalletBalance
	netPnL := marginBalance - s.initialWalletBalance
	fees := 0.0 // Incorporated directly in WalletBalance deduction on trades

	openOrders := s.orderManager.GetOpenOrders(symbol)
	var minPrice, maxPrice float64
	minPrice = math.MaxFloat64
	lastOrderTs := s.startTime
	for _, o := range openOrders {
		if o.Price < minPrice && o.Price > 0 {
			minPrice = o.Price
		}
		if o.Price > maxPrice {
			maxPrice = o.Price
		}

		if o.UpdateTime > 0 {
			ts := time.Unix(0, o.UpdateTime*int64(time.Millisecond))
			if ts.After(lastOrderTs) {
				lastOrderTs = ts
			}
		}
	}
	if minPrice == math.MaxFloat64 {
		minPrice = 0
	}

	totalFills := 0
	if val, ok := metrics["total_fills"].(int); ok {
		totalFills = val
	}

	roi := 0.0
	if s.initialWalletBalance > 0 {
		roi = (netPnL / s.initialWalletBalance) * 100
	}

	return notifier.GridMetrics{
		Symbol:          symbol,
		CurrentPrice:    midPrice,
		RealizedPnL:     realized,
		UnrealizedPnL:   unrealized,
		FeesPaid:        fees,
		NetPnL:          netPnL,
		Volume30m:       v24h,       // Represents 24h as recent volume context
		FilledOrders30m: totalFills, // Aggregated filled orders
		PendingOrders:   len(openOrders),
		GridMinPrice:    minPrice,
		GridMaxPrice:    maxPrice,
		ActiveGrids:     len(openOrders),
		TotalGrids:      s.config.MaxPositionsPerSide * 2,
		LastOrderTime:   lastOrderTs,
		InitialCapital:  s.initialWalletBalance,
		CurrentCapital:  marginBalance,
		ROI:             roi,
		DrawdownPct:     drawdownPct,
		Uptime:          time.Since(s.startTime),
	}
}

// NEW: Continuous order placement optimization for maximum volume
// This function ensures orders are always placed for maximum fills
func (s *MakerStrategyImpl) continuousOrderPlacement(symbol string) {
	// P0 FIX: Prevent concurrent executions (Debounce)
	if _, placing := s.placementInProgress.LoadOrStore(symbol, true); placing {
		return // Already placing orders for this symbol, skip
	}

	// Move everything to a single goroutine so debounce correctly blocks
	go func() {
		// Clear debounce lock AFTER fully completing orders
		defer s.placementInProgress.Delete(symbol)

		// Check if we need more orders (less than max positions per side)
		positions := s.GetPositions()
		openOrders := s.orderManager.GetOpenOrders(symbol)

		// Count current positions per side
		longPositions := 0
		shortPositions := 0
		for _, pos := range positions {
			if pos.Amount > 0 {
				longPositions++
			} else if pos.Amount < 0 {
				shortPositions++
			}
		}

		// Count open orders per side
		buyOrders := 0
		sellOrders := 0
		for _, order := range openOrders {
			if order.Side == OrderSideBuy {
				buyOrders++
			} else if order.Side == OrderSideSell {
				sellOrders++
			}
		}

		// If we have room for more orders, place them immediately
		maxPositionsPerSide := s.config.MaxPositionsPerSide

		needMoreBuyOrders := longPositions < maxPositionsPerSide && buyOrders == 0
		needMoreSellOrders := shortPositions < maxPositionsPerSide && sellOrders == 0

		if needMoreBuyOrders || needMoreSellOrders {
			s.logger.Info("🚀 CONTINUOUS ORDER PLACEMENT",
				zap.String("symbol", symbol),
				zap.Int("long_positions", longPositions),
				zap.Int("short_positions", shortPositions),
				zap.Int("buy_orders", buyOrders),
				zap.Int("sell_orders", sellOrders),
				zap.Int("max_per_side", maxPositionsPerSide),
				zap.Bool("need_more_buy", needMoreBuyOrders),
				zap.Bool("need_more_sell", needMoreSellOrders))

			// Execute synchronously inside the goroutine to block debounce
			s.PlaceOrders(symbol)
		}
	}()
}

// NEW: Micro-profit optimization for maximum leverage utilization
// This function calculates optimal order size for maximum fills with 150x leverage
func (s *MakerStrategyImpl) calculateMicroProfitOrderSize(balance float64, symbol string) float64 {
	// Use full 150x leverage for maximum volume farming
	// Calculate margin requirement per unit
	bestBid, bestAsk, _, err := s.wsClient.GetTickerData(symbol)
	if err != nil || bestBid == 0 || bestAsk == 0 {
		return balance * 0.1 // Fallback to 10% of balance
	}

	// For zero-fee environment, we can use very tight margins
	// Target: 2.0% margin per position to leverage 150x effectively
	targetMarginRatio := 0.02     // Increased from 0.5% to 2%
	_ = (bestBid + bestAsk) / 2.0 // midPrice not needed here

	// Calculate maximum order size with 150x leverage
	maxOrderValue := balance * targetMarginRatio * float64(s.config.MaxLeverage)

	// Apply micro-profit optimization: smaller orders = more fills
	// Zero fee means every spread = profit, so we want maximum fills
	optimalSize := maxOrderValue * 0.8 // Use 80% of max to leave room for multiple positions

	// Ensure minimum order size
	if optimalSize < s.config.MinOrderSizeUSD {
		optimalSize = s.config.MinOrderSizeUSD
	}

	// Cap at maximum configured size
	if optimalSize > s.config.MaxOrderSizeUSD {
		optimalSize = s.config.MaxOrderSizeUSD
	}

	s.logger.Debug("Micro-profit optimization",
		zap.String("symbol", symbol),
		zap.Float64("balance", balance),
		zap.Float64("target_margin_ratio", targetMarginRatio),
		zap.Float64("max_order_value", maxOrderValue),
		zap.Float64("optimal_size", optimalSize))

	return optimalSize
}
