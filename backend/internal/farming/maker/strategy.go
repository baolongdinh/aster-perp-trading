package maker

import (
	"aster-bot/internal/client"
	"context"
	"math"
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

	maxOrderValue := balance * float64(s.config.MaxLeverage) * 1 // 100% of leveraged balance for larger orders
	buyQty := maxOrderValue / buyPrice
	sellQty := maxOrderValue / sellPrice

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

	// DYNAMIC GRID: Place orders very close to market for instant fills
	// Use micro-spreads to ensure orders get filled quickly

	gridLevels := 12 // 24 orders total (12 buy + 12 sell) for maximum volume
	perLevelBuyQty := buyQty / float64(gridLevels)
	perLevelSellQty := sellQty / float64(gridLevels)

	// Calculate micro-spreads for each level (super tight = instant fills)
	buySpread, sellSpread := s.spreadCalc.CalculateDynamicSpread(symbol, 0.5) // Use 0.5 bps base for ultra-tight

	// Buy grid: ultra tight below bid (market touching)
	for i := 0; i < gridLevels; i++ {
		// Super tight: 0.01%, 0.02%, 0.03%... below bid
		gridOffset := 0.0001 * float64(i+1)
		gridBuyPrice := bestBid * (1 - gridOffset)

		if perLevelBuyQty > 0 {
			err := s.placeLimitOrder(symbol, OrderSideBuy, gridBuyPrice, perLevelBuyQty)
			if err != nil {
				s.logger.Error("Failed to place grid buy order", zap.Error(err))
			} else {
				s.otRatioGuard.RecordOrder()
				s.logger.Info("⚡ Dynamic Grid Buy",
					zap.String("symbol", symbol),
					zap.Int("level", i+1),
					zap.Float64("price", gridBuyPrice),
					zap.Float64("spread_bps", buySpread),
					zap.Float64("qty", perLevelBuyQty))
			}
		}
	}

	// Sell grid: super tight above ask (market touching)
	for i := 0; i < gridLevels; i++ {
		// Super tight: 0.01%, 0.02%, 0.03%... above ask
		gridOffset := 0.0001 * float64(i+1)
		gridSellPrice := bestAsk * (1 + gridOffset)

		if perLevelSellQty > 0 {
			err := s.placeLimitOrder(symbol, OrderSideSell, gridSellPrice, perLevelSellQty)
			if err != nil {
				s.logger.Error("Failed to place grid sell order", zap.Error(err))
			} else {
				s.otRatioGuard.RecordOrder()
				s.logger.Info("⚡ Dynamic Grid Sell",
					zap.String("symbol", symbol),
					zap.Int("level", i+1),
					zap.Float64("price", gridSellPrice),
					zap.Float64("spread_bps", sellSpread),
					zap.Float64("qty", perLevelSellQty))
			}
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

	// Slower cycle - give orders time to fill
	ticker := time.NewTicker(10 * time.Second)
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

	// Check for filled orders first (revenue event!)
	for _, order := range orders {
		if order.Status == OrderStatusFilled {
			filledCount++
			s.orderManager.RemoveOrder(symbol, order.OrderID)
			s.otRatioGuard.RecordFill()
			s.logger.Info("🎯 ORDER FILLED - Micro Profit Earned!",
				zap.String("symbol", symbol),
				zap.Int64("order_id", order.OrderID),
				zap.String("side", string(order.Side)),
				zap.Float64("price", order.Price),
				zap.Float64("qty", order.OrigQty))
		} else if order.Status == OrderStatusCanceled || order.Status == OrderStatusExpired {
			s.orderManager.RemoveOrder(symbol, order.OrderID)
		}
	}

	// Dynamic Grid rebalancing - shift grid when price moves slightly
	gridShiftThreshold := 0.00015   // 0.015% - very sensitive for market touching
	maxOrderAge := 60 * time.Second // Give orders 60 seconds to fill
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
				err := s.orderManager.CancelOrder(s.ctx, symbol, order.OrderID)
				if err != nil {
					s.logger.Warn("Failed to cancel order", zap.Error(err))
				} else {
					cancelledCount++
					needsRefresh = true
					s.logger.Info("🔄 Order cancelled",
						zap.String("symbol", symbol),
						zap.String("reason", reason),
						zap.Int64("order_id", order.OrderID),
						zap.Float64("age_sec", float64(order.AgeSeconds)),
						zap.Float64("price_diff_pct", priceDiff*100))
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
