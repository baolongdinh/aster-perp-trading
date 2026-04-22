package adaptive_grid

import (
	"aster-bot/internal/client"
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MappingStatus represents the status of the position-to-take-profit mapping
type MappingStatus int

const (
	MappingStatusActive    MappingStatus = iota // Mapping is active, take profit order pending
	MappingStatusCompleted                      // Take profit order filled, mapping complete
	MappingStatusCancelled                      // Take profit order cancelled, mapping cancelled
	MappingStatusTimeout                        // Take profit order timed out, mapping expired
)

// String returns the string representation of the mapping status
func (s MappingStatus) String() string {
	switch s {
	case MappingStatusActive:
		return "ACTIVE"
	case MappingStatusCompleted:
		return "COMPLETED"
	case MappingStatusCancelled:
		return "CANCELLED"
	case MappingStatusTimeout:
		return "TIMEOUT"
	default:
		return "UNKNOWN"
	}
}

// PositionTakeProfitMapping maps a position to its corresponding take profit order
type PositionTakeProfitMapping struct {
	PositionID        string        `json:"position_id"`
	TakeProfitOrderID string        `json:"take_profit_order_id"`
	Symbol            string        `json:"symbol"`
	Status            MappingStatus `json:"status"`
	CreatedAt         time.Time     `json:"created_at"`
	CompletedAt       *time.Time    `json:"completed_at,omitempty"`
}

// TakeProfitManager handles the lifecycle of take profit orders
type TakeProfitManager struct {
	logger *zap.Logger
	config *MicroProfitConfig

	// Tracking
	mu                 sync.RWMutex
	takeProfitOrders   map[string]*TakeProfitOrder           // orderID -> TakeProfitOrder
	positionMappings   map[string]*PositionTakeProfitMapping // positionID -> PositionTakeProfitMapping
	orderToPositionMap map[string]string                     // orderID -> positionID

	// Metrics
	totalMicroProfit     float64
	totalOrdersPlaced    int
	totalOrdersFilled    int
	totalOrdersCancelled int
	totalOrdersTimeout   int

	// Grid manager reference (for rebalancing)
	gridManager interface{}

	// FuturesClient for placing real orders on exchange
	futuresClient FuturesClientInterface

	// Ticker for timeout checks
	timeoutTicker *time.Ticker
	timeoutDone   chan bool

	// Config watcher for hot-reload
	configWatcher *ConfigWatcher
}

// NewTakeProfitManager creates a new TakeProfitManager
func NewTakeProfitManager(logger *zap.Logger, config *MicroProfitConfig) *TakeProfitManager {
	return &TakeProfitManager{
		logger:             logger,
		config:             config,
		takeProfitOrders:   make(map[string]*TakeProfitOrder),
		positionMappings:   make(map[string]*PositionTakeProfitMapping),
		orderToPositionMap: make(map[string]string),
		timeoutDone:        make(chan bool),
	}
}

// SetGridManager sets the grid manager reference for rebalancing
func (m *TakeProfitManager) SetGridManager(gridManager interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gridManager = gridManager
}

// SetFuturesClient sets the futures client for placing real orders
func (m *TakeProfitManager) SetFuturesClient(client FuturesClientInterface) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.futuresClient = client
}

// PlaceTakeProfitOrder places a take profit order for a filled grid order
// Returns the orderID of the placed take profit order
func (m *TakeProfitManager) PlaceTakeProfitOrder(ctx context.Context, symbol, side string, fillPrice, size float64, parentOrderID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if feature is enabled
	if !m.config.Enabled {
		m.logger.Debug("Micro profit feature disabled, skipping take profit order placement",
			zap.String("symbol", symbol))
		return "", nil
	}

	// Get symbol-specific config
	symbolConfig := m.config.GetSymbolConfig(symbol)
	if !symbolConfig.Enabled {
		m.logger.Debug("Micro profit feature disabled for symbol",
			zap.String("symbol", symbol))
		return "", nil
	}

	// Calculate take profit price
	var tpPrice float64
	if side == "BUY" {
		// BUY order filled, place SELL take profit at higher price
		tpPrice = fillPrice * (1 + symbolConfig.SpreadPct)
	} else if side == "SELL" {
		// SELL order filled, place BUY take profit at lower price
		tpPrice = fillPrice * (1 - symbolConfig.SpreadPct)
	} else {
		return "", fmt.Errorf("invalid side: %s", side)
	}

	// Calculate profit to check if it meets minimum threshold
	estimatedProfit := fillPrice * symbolConfig.SpreadPct * size
	if estimatedProfit < symbolConfig.MinProfitUSDT {
		m.logger.Debug("Estimated profit below minimum threshold, skipping take profit",
			zap.String("symbol", symbol),
			zap.Float64("estimated_profit", estimatedProfit),
			zap.Float64("min_profit", symbolConfig.MinProfitUSDT))
		return "", nil
	}

	// Create take profit order
	orderID := fmt.Sprintf("TP-%s-%d", symbol, time.Now().UnixNano())
	tpSide := "SELL"
	if side == "SELL" {
		tpSide = "BUY"
	}

	timeoutAt := time.Now().Add(time.Duration(symbolConfig.TimeoutSeconds) * time.Second)
	tpOrder := &TakeProfitOrder{
		OrderID:       orderID,
		Symbol:        symbol,
		Side:          tpSide,
		Price:         tpPrice,
		Size:          size,
		ParentOrderID: parentOrderID,
		Status:        TakeProfitStatusPending,
		CreatedAt:     time.Now(),
		TimeoutAt:     &timeoutAt,
	}

	// CRITICAL: Actually place the take profit order on the exchange
	if m.futuresClient != nil {
		// Build order request for exchange using correct type
		orderReq := client.PlaceOrderRequest{
			Symbol:        symbol,
			Side:          tpSide,
			PositionSide:  "BOTH",
			Type:          "LIMIT",
			TimeInForce:   "GTC",
			Quantity:      fmt.Sprintf("%.6f", size),
			Price:         fmt.Sprintf("%.8f", tpPrice),
			ReduceOnly:    true, // IMPORTANT: Reduce only to close position
			ClientOrderID: orderID,
		}

		m.logger.Info("Placing take profit order on exchange",
			zap.String("order_id", orderID),
			zap.String("symbol", symbol),
			zap.String("side", tpSide),
			zap.Float64("price", tpPrice),
			zap.Float64("size", size))

		resp, err := m.futuresClient.PlaceOrder(ctx, orderReq)
		if err != nil {
			m.logger.Error("Failed to place take profit order on exchange",
				zap.String("order_id", orderID),
				zap.String("symbol", symbol),
				zap.Error(err))
			return "", fmt.Errorf("failed to place take profit order: %w", err)
		}

		m.logger.Info("Take profit order placed successfully on exchange",
			zap.String("order_id", orderID),
			zap.String("symbol", symbol),
			zap.Any("response", resp))
	} else {
		m.logger.Error("FuturesClient not set, cannot place take profit order on exchange",
			zap.String("order_id", orderID),
			zap.String("symbol", symbol))
		return "", fmt.Errorf("futuresClient not set, cannot place order")
	}

	// Store take profit order locally for tracking
	m.takeProfitOrders[orderID] = tpOrder

	// Create position mapping
	positionID := fmt.Sprintf("%s-%d", symbol, time.Now().UnixNano())
	mapping := &PositionTakeProfitMapping{
		PositionID:        positionID,
		TakeProfitOrderID: orderID,
		Symbol:            symbol,
		Status:            MappingStatusActive,
		CreatedAt:         time.Now(),
	}
	m.positionMappings[positionID] = mapping
	m.orderToPositionMap[orderID] = positionID

	m.totalOrdersPlaced++

	m.logger.Info("Take profit order tracked locally",
		zap.String("order_id", orderID),
		zap.String("symbol", symbol),
		zap.String("side", tpSide),
		zap.Float64("price", tpPrice),
		zap.Float64("size", size),
		zap.String("parent_order_id", parentOrderID),
		zap.Time("timeout_at", timeoutAt))

	return orderID, nil
}

// HandleTakeProfitFill handles a take profit order fill event
func (m *TakeProfitManager) HandleTakeProfitFill(orderID string, fillPrice float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tpOrder, exists := m.takeProfitOrders[orderID]
	if !exists {
		return fmt.Errorf("take profit order not found: %s", orderID)
	}

	if tpOrder.Status != TakeProfitStatusPending {
		return fmt.Errorf("take profit order not in pending status: %s (status: %s)", orderID, tpOrder.Status)
	}

	// Mark order as filled
	tpOrder.MarkFilled(fillPrice)

	// Calculate profit
	profit, err := tpOrder.CalculateProfit(fillPrice)
	if err != nil {
		m.logger.Error("Failed to calculate profit",
			zap.String("order_id", orderID),
			zap.Error(err))
		return err
	}

	// Update mapping
	positionID, exists := m.orderToPositionMap[orderID]
	if exists {
		mapping, exists := m.positionMappings[positionID]
		if exists {
			mapping.Status = MappingStatusCompleted
			completedAt := time.Now()
			mapping.CompletedAt = &completedAt
		}
	}

	// Update metrics
	m.totalMicroProfit += profit
	m.totalOrdersFilled++

	m.logger.Info("Take profit order filled",
		zap.String("order_id", orderID),
		zap.String("symbol", tpOrder.Symbol),
		zap.Float64("fill_price", fillPrice),
		zap.Float64("profit", profit))

	// Trigger grid rebalance
	// TODO: Implement rebalance trigger

	return nil
}

// CheckTimeouts checks for expired take profit orders and handles them
func (m *TakeProfitManager) CheckTimeouts(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for orderID, tpOrder := range m.takeProfitOrders {
		if tpOrder.Status == TakeProfitStatusPending && tpOrder.IsExpired() {
			m.logger.Info("Take profit order timeout detected",
				zap.String("order_id", orderID),
				zap.String("symbol", tpOrder.Symbol))

			// Mark order as timed out
			tpOrder.MarkTimeout()
			m.totalOrdersTimeout++

			// Update mapping
			positionID, exists := m.orderToPositionMap[orderID]
			if exists {
				mapping, exists := m.positionMappings[positionID]
				if exists {
					mapping.Status = MappingStatusTimeout
					completedAt := time.Now()
					mapping.CompletedAt = &completedAt
				}
			}

			// CRITICAL: Immediately close position by market order to prevent liquidation
			// This is "eat fast, close fast" - if take profit doesn't fill in time, close immediately
			if m.futuresClient != nil {
				go func(symbol, side string, size float64, orderID string) {
					// Determine close side (opposite of position side)
					closeSide := "SELL"
					if side == "SELL" {
						closeSide = "BUY"
					}

					m.logger.Info("EMERGENCY: Closing position by market order due to timeout",
						zap.String("symbol", symbol),
						zap.String("order_id", orderID),
						zap.String("close_side", closeSide),
						zap.Float64("size", size))

					// Place market order to close position immediately
					closeReq := client.PlaceOrderRequest{
						Symbol:       symbol,
						Side:         closeSide,
						PositionSide: "BOTH",
						Type:         "MARKET",
						Quantity:     fmt.Sprintf("%.6f", size),
						ReduceOnly:   true, // IMPORTANT: Reduce only to close position
					}

					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()

					resp, err := m.futuresClient.PlaceOrder(ctx, closeReq)
					if err != nil {
						m.logger.Error("FAILED to close position on timeout - RISK OF LIQUIDATION",
							zap.String("symbol", symbol),
							zap.String("order_id", orderID),
							zap.Error(err))
					} else {
						m.logger.Info("Position closed successfully by market order on timeout",
							zap.String("symbol", symbol),
							zap.String("order_id", orderID),
							zap.Any("response", resp))
					}
				}(tpOrder.Symbol, tpOrder.Side, tpOrder.Size, orderID)
			}
		}
	}
}

// StartTimeoutChecker starts the periodic timeout checker
func (m *TakeProfitManager) StartTimeoutChecker(ctx context.Context) {
	m.mu.Lock()
	if m.timeoutTicker != nil {
		m.mu.Unlock()
		return
	}
	m.timeoutTicker = time.NewTicker(5 * time.Second)
	m.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("Timeout checker goroutine panic recovered",
					zap.Any("panic", r))
			}
		}()
		for {
			select {
			case <-ctx.Done():
				m.StopTimeoutChecker()
				return
			case <-m.timeoutDone:
				return
			case <-m.timeoutTicker.C:
				m.CheckTimeouts(ctx)
			}
		}
	}()

	m.logger.Info("Take profit timeout checker started")
}

// StopTimeoutChecker stops the periodic timeout checker
func (m *TakeProfitManager) StopTimeoutChecker() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.timeoutTicker != nil {
		m.timeoutTicker.Stop()
		m.timeoutTicker = nil
		close(m.timeoutDone)
		m.timeoutDone = make(chan bool)
		m.logger.Info("Take profit timeout checker stopped")
	}
}

// GetMicroProfitMetrics returns the current micro profit metrics
func (m *TakeProfitManager) GetMicroProfitMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	successRate := 0.0
	if m.totalOrdersPlaced > 0 {
		successRate = float64(m.totalOrdersFilled) / float64(m.totalOrdersPlaced) * 100
	}

	timeoutRate := 0.0
	if m.totalOrdersPlaced > 0 {
		timeoutRate = float64(m.totalOrdersTimeout) / float64(m.totalOrdersPlaced) * 100
	}

	return map[string]interface{}{
		"total_micro_profit":     m.totalMicroProfit,
		"total_orders_placed":    m.totalOrdersPlaced,
		"total_orders_filled":    m.totalOrdersFilled,
		"total_orders_cancelled": m.totalOrdersCancelled,
		"total_orders_timeout":   m.totalOrdersTimeout,
		"success_rate":           successRate,
		"timeout_rate":           timeoutRate,
		"active_orders":          len(m.takeProfitOrders),
	}
}

// Shutdown gracefully shuts down the manager
func (m *TakeProfitManager) Shutdown(ctx context.Context) {
	m.StopTimeoutChecker()
	if m.configWatcher != nil {
		m.configWatcher.Stop()
	}
	m.logger.Info("Take profit manager shutdown complete")
}

// UpdateConfig updates the configuration (called by config watcher)
func (m *TakeProfitManager) UpdateConfig(config *MicroProfitConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = config
	m.logger.Info("Take profit manager config updated",
		zap.Bool("enabled", config.Enabled),
		zap.Float64("spread_pct", config.SpreadPct),
		zap.Int("timeout_seconds", config.TimeoutSeconds))

	return nil
}

// StartConfigWatcher starts the configuration watcher for hot-reload
func (m *TakeProfitManager) StartConfigWatcher(configPath string) error {
	watcher, err := NewConfigWatcher(configPath, m.logger, func(config *MicroProfitConfig) error {
		return m.UpdateConfig(config)
	})
	if err != nil {
		return err
	}

	m.configWatcher = watcher
	watcher.Start()
	m.logger.Info("Config watcher started for take profit manager",
		zap.String("config_path", configPath))

	return nil
}
