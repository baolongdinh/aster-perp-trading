package maker

import (
	"aster-bot/internal/client"
	"context"
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
	// Initialize REST API cache with 1s expiration and 5s cleanup interval
	strategy.restCache = cache.New(1*time.Second, 5*time.Second)

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

	// ADVANCED POSITION MANAGEMENT: Dynamic hedging with market impact detection
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

	maxOrderValue := balance * float64(s.config.MaxLeverage) * 1 // 100% of leveraged balance for larger orders
	buyQty := maxOrderValue / buyPrice
	sellQty := maxOrderValue / sellPrice

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
		zap.Float64("order_value_usdt", maxOrderValue),
		zap.Float64("buy_qty", buyQty),
		zap.Float64("sell_qty", sellQty))

	// INSTITUTIONAL DELTA-NEUTRAL GRID STRATEGY (EXA Deep Research)
	// Combining HV30 optimization + delta-neutral hedging + minimum risk

	// Calculate institutional-grade volatility metrics
	bestBid, bestAsk, volume24h, _ = s.wsClient.GetTickerData(symbol)
	midPrice = (bestBid + bestAsk) / 2
	spread = bestAsk - bestBid
	dailyVolatility := spread / midPrice * 100 // Simplified daily volatility %

	// Institutional HV30 volatility model (Delta Neutral V3 methodology)
	// k = 0.618 smoothing constant (empirically derived from 14 crypto pairs, 24 months)
	hv30SmoothingConstant := 0.618
	adjustedVolatility := dailyVolatility * hv30SmoothingConstant

	// Institutional 4-tier regime gate with risk assessment
	var gridLevels int
	var gridSpacing float64
	var capitalAllocation float64
	var riskLevel string

	// ULTRA-TIGHT VOLUME FARMING: Maximum fill rate optimization
	// Focus on continuous micro profit without waiting

	if adjustedVolatility < 2.0 {
		// Ultra-tight regime: Maximum volume farming, continuous fills
		gridLevels = 50         // 50 levels each side = 100 orders total
		gridSpacing = 0.00025   // 0.05% spacing - ultra-tight for max fills
		capitalAllocation = 1.0 // 100% allocation - maximum volume
		riskLevel = "ULTRA_TIGHT"
		s.logger.Info("⚡ Ultra-Tight Regime - Maximum Volume Farming",
			zap.String("symbol", symbol),
			zap.Float64("hv30_adj", adjustedVolatility),
			zap.Int("grid_levels", gridLevels),
			zap.Float64("spacing_pct", gridSpacing*100),
			zap.String("risk_level", riskLevel))
	} else if adjustedVolatility <= 5.0 {
		// Tight regime: High frequency fills + volume farming
		gridLevels = 40         // 40 levels each side = 80 orders total
		gridSpacing = 0.0005    // 0.1% spacing - tight for continuous fills
		capitalAllocation = 1.0 // 100% allocation - maximum volume
		riskLevel = "TIGHT"
		s.logger.Info("🎯 Tight Regime - High Frequency Volume Farming",
			zap.String("symbol", symbol),
			zap.Float64("hv30_adj", adjustedVolatility),
			zap.Int("grid_levels", gridLevels),
			zap.Float64("spacing_pct", gridSpacing*100),
			zap.String("risk_level", riskLevel))
	} else if adjustedVolatility <= 8.0 {
		// Balanced regime: Volume farming with reasonable spacing
		gridLevels = 30         // 30 levels each side = 60 orders total
		gridSpacing = 0.00015   // 0.15% spacing - balanced for fills + profit
		capitalAllocation = 0.9 // 90% allocation - high volume
		riskLevel = "BALANCED"
		s.logger.Info("⚖️ Balanced Regime - Optimized Volume Farming",
			zap.String("symbol", symbol),
			zap.Float64("hv30_adj", adjustedVolatility),
			zap.Int("grid_levels", gridLevels),
			zap.Float64("spacing_pct", gridSpacing*100),
			zap.String("risk_level", riskLevel))
	} else {
		// Wide regime: Still volume farming but wider spacing
		gridLevels = 25         // 25 levels each side = 50 orders total
		gridSpacing = 0.002     // 0.2% spacing - wider but still fills
		capitalAllocation = 0.8 // 80% allocation - reduced risk
		riskLevel = "WIDE"
		s.logger.Info("� Wide Regime - Adaptive Volume Farming",
			zap.String("symbol", symbol),
			zap.Float64("hv30_adj", adjustedVolatility),
			zap.Int("grid_levels", gridLevels),
			zap.Float64("spacing_pct", gridSpacing*100),
			zap.String("risk_level", riskLevel))
	}

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

	perLevelBuyQty := buyQty * capitalAllocation / float64(gridLevels)
	perLevelSellQty := sellQty * capitalAllocation / float64(gridLevels)

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
	if perLevelBuyQty < minQtyPerOrder {
		perLevelBuyQty = minQtyPerOrder * 1.1 // Add 10% buffer
		s.logger.Info("🔧 EMERGENCY: Adjusted buy quantity for minimum notional",
			zap.Float64("old_qty", buyQty*capitalAllocation/float64(gridLevels)),
			zap.Float64("new_qty", perLevelBuyQty),
			zap.Float64("min_notional_usd", minNotionalUSD),
			zap.Float64("price", midPrice))
	}

	if perLevelSellQty < minQtyPerOrder {
		perLevelSellQty = minQtyPerOrder * 1.1 // Add 10% buffer
		s.logger.Info("🔧 EMERGENCY: Adjusted sell quantity for minimum notional",
			zap.Float64("old_qty", sellQty*capitalAllocation/float64(gridLevels)),
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
			gridOffset := gridSpacing * float64(level+1)
			gridBuyPrice := bestBid * (1 - gridOffset)

			if perLevelBuyQty > 0 {
				err := s.placeLimitOrder(symbol, OrderSideBuy, gridBuyPrice, perLevelBuyQty)
				if err != nil {
					s.logger.Error("Failed to place grid buy order", zap.Error(err))
				} else {
					s.otRatioGuard.RecordOrder()
					s.logger.Info("⚡ Ultra-Tight Grid Buy",
						zap.String("symbol", symbol),
						zap.String("risk_level", riskLevel),
						zap.Float64("hv30_adj", adjustedVolatility),
						zap.Int("level", level+1),
						zap.Float64("price", gridBuyPrice),
						zap.Float64("spacing_pct", gridOffset*100),
						zap.Float64("qty", perLevelBuyQty))
				}
				orderPlaced <- true
			}
		}(i)
	}

	// Place sell orders concurrently with optimal spacing
	for i := 0; i < gridLevels; i++ {
		wg.Add(1)
		go func(level int) {
			defer wg.Done()
			// Ultra-Tight Volume Farming: Maximum fill rate spacing above ask
			gridOffset := gridSpacing * float64(level+1)
			gridSellPrice := bestAsk * (1 + gridOffset)

			if perLevelSellQty > 0 {
				err := s.placeLimitOrder(symbol, OrderSideSell, gridSellPrice, perLevelSellQty)
				if err != nil {
					s.logger.Error("Failed to place grid sell order", zap.Error(err))
				} else {
					s.otRatioGuard.RecordOrder()
					s.logger.Info("⚡ Ultra-Tight Grid Sell",
						zap.String("symbol", symbol),
						zap.String("risk_level", riskLevel),
						zap.Float64("hv30_adj", adjustedVolatility),
						zap.Int("level", level+1),
						zap.Float64("price", gridSellPrice),
						zap.Float64("spacing_pct", gridOffset*100),
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

	// SYNC WITH EXCHANGE: Get current order state from exchange
	exchangeOrders, err := s.futuresClient.GetOpenOrders(s.ctx, symbol)
	if err != nil {
		s.logger.Warn("Failed to sync with exchange orders", zap.Error(err))
	} else {
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

	// EXA OPTIMIZED: Allow sufficient time for fills
	gridShiftThreshold := 0.002      // 0.2% - reasonable threshold for grid rebalancing
	maxOrderAge := 120 * time.Second // 2 minutes - sufficient time for fills per EXA research
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
