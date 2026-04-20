package farming

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"aster-bot/internal/client"

	"go.uber.org/zap"
)

// ExitExecutor handles fast exit sequences for breakouts/trends
type ExitExecutor struct {
	futuresClient *client.FuturesClient
	wsClient      *client.WebSocketClient
	timeout       time.Duration
	logger        *zap.Logger
}

// ExitSequence tracks the exit operation timing and results
type ExitSequence struct {
	Symbol          string
	TriggeredAt     time.Time
	OrdersCancelled int
	PositionsClosed int
	CompletedAt     time.Time
	Duration        time.Duration
	Error           error
	Steps           []ExitStep
}

// ExitStep tracks individual step timing
type ExitStep struct {
	Name      string
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
	Error     error
}

// NewExitExecutor creates a new exit executor
func NewExitExecutor(
	futuresClient *client.FuturesClient,
	wsClient *client.WebSocketClient,
	timeout time.Duration,
	logger *zap.Logger,
) *ExitExecutor {
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	return &ExitExecutor{
		futuresClient: futuresClient,
		wsClient:      wsClient,
		timeout:       timeout,
		logger:        logger.With(zap.String("component", "exit_executor")),
	}
}

// ExecuteFastExit performs a fast exit sequence for a symbol
// Timeline: T+0 detect, T+100ms cancel, T+800ms close, T+5s verify
func (e *ExitExecutor) ExecuteFastExit(ctx context.Context, symbol string) *ExitSequence {
	sequence := &ExitSequence{
		Symbol:      symbol,
		TriggeredAt: time.Now(),
		Steps:       make([]ExitStep, 0),
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	e.logger.Warn("Executing fast exit sequence",
		zap.String("symbol", symbol),
		zap.Duration("timeout", e.timeout))

	// Step 1: Cancel all pending orders (T+0 to T+100ms)
	cancelStep := e.executeStep("cancel_orders", func() error {
		return e.cancelAllOrders(ctx, symbol)
	})
	sequence.Steps = append(sequence.Steps, cancelStep)
	if cancelStep.Error != nil {
		e.logger.Error("Failed to cancel orders", zap.Error(cancelStep.Error))
	}
	sequence.OrdersCancelled = 1 // Track that we attempted cancellation

	// Check context after cancellation
	if ctx.Err() != nil {
		sequence.Error = ctx.Err()
		sequence.CompletedAt = time.Now()
		sequence.Duration = time.Since(sequence.TriggeredAt)
		return sequence
	}

	// Step 2: Get positions from WebSocket cache (T+100ms to T+500ms)
	positionsStep := e.executeStep("get_positions", func() error {
		return nil // Positions retrieved during close step
	})
	sequence.Steps = append(sequence.Steps, positionsStep)

	// Step 3: Close open positions with market orders (T+500ms to T+3s)
	closeStep := e.executeStep("close_positions", func() error {
		closed, err := e.closePositionsMarket(ctx, symbol)
		sequence.PositionsClosed = closed
		return err
	})
	sequence.Steps = append(sequence.Steps, closeStep)
	if closeStep.Error != nil {
		e.logger.Error("Failed to close positions", zap.Error(closeStep.Error))
	}

	// Check context
	if ctx.Err() != nil {
		sequence.Error = ctx.Err()
	}

	// Step 4: Verify all positions closed via WebSocket (T+3s to T+5s)
	verifyStep := e.executeStep("verify_closure", func() error {
		return e.verifyPositionsClosed(ctx, symbol)
	})
	sequence.Steps = append(sequence.Steps, verifyStep)
	if verifyStep.Error != nil {
		e.logger.Error("Failed to verify position closure", zap.Error(verifyStep.Error))
		if sequence.Error == nil {
			sequence.Error = verifyStep.Error
		}
	}

	// Complete sequence
	sequence.CompletedAt = time.Now()
	sequence.Duration = time.Since(sequence.TriggeredAt)

	e.logger.Info("Fast exit sequence completed",
		zap.String("symbol", symbol),
		zap.Duration("total_duration", sequence.Duration),
		zap.Int("orders_cancelled", sequence.OrdersCancelled),
		zap.Int("positions_closed", sequence.PositionsClosed),
		zap.Error(sequence.Error))

	return sequence
}

// executeStep executes a single step and tracks timing
func (e *ExitExecutor) executeStep(name string, fn func() error) ExitStep {
	step := ExitStep{
		Name:      name,
		StartTime: time.Now(),
	}

	step.Error = fn()

	step.EndTime = time.Now()
	step.Duration = step.EndTime.Sub(step.StartTime)

	return step
}

// cancelAllOrders cancels all pending orders for a symbol
func (e *ExitExecutor) cancelAllOrders(ctx context.Context, symbol string) error {
	// TODO: Replace with WebSocket cache when available
	// For now, use REST API to get open orders
	orders, err := e.futuresClient.GetOpenOrders(ctx, symbol)
	if err != nil {
		e.logger.Warn("Failed to get open orders", zap.Error(err))
		return nil
	}

	if len(orders) == 0 {
		e.logger.Debug("No orders to cancel", zap.String("symbol", symbol))
		return nil
	}

	e.logger.Info("Cancelling orders",
		zap.String("symbol", symbol),
		zap.Int("order_count", len(orders)))

	// Cancel orders concurrently with limited parallelism
	var wg sync.WaitGroup
	var cancelErrors []error
	var mu sync.Mutex

	maxConcurrent := 5
	semaphore := make(chan struct{}, maxConcurrent)

	for _, order := range orders {
		// Skip filled or cancelled orders
		if order.Status == "FILLED" || order.Status == "CANCELLED" {
			continue
		}

		wg.Add(1)
		semaphore <- struct{}{}

		go func(orderID int64) {
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("Order cancellation goroutine panic recovered",
						zap.String("symbol", symbol),
						zap.Int64("order_id", orderID),
						zap.Any("panic", r))
					mu.Lock()
					cancelErrors = append(cancelErrors, fmt.Errorf("panic: %v", r))
					mu.Unlock()
				}
			}()
			defer wg.Done()
			defer func() { <-semaphore }()

			req := client.CancelOrderRequest{
				Symbol:  symbol,
				OrderID: orderID,
			}
			_, err := e.futuresClient.CancelOrder(ctx, req)
			if err != nil {
				mu.Lock()
				cancelErrors = append(cancelErrors, err)
				mu.Unlock()
				e.logger.Warn("Failed to cancel order",
					zap.String("symbol", symbol),
					zap.Int64("order_id", orderID),
					zap.Error(err))
			}
		}(order.OrderID)
	}

	wg.Wait()

	if len(cancelErrors) > 0 {
		return fmt.Errorf("failed to cancel %d orders: %v", len(cancelErrors), cancelErrors)
	}

	return nil
}

// closePositionsMarket closes open positions with market orders
func (e *ExitExecutor) closePositionsMarket(ctx context.Context, symbol string) (int, error) {
	// TODO: Replace with WebSocket cache when available
	// For now, use REST API to get positions
	positions, err := e.futuresClient.GetPositions(ctx)
	if err != nil {
		e.logger.Warn("Failed to get positions", zap.Error(err))
		return 0, err
	}

	// Find position for symbol
	var position *client.Position
	for i := range positions {
		if positions[i].Symbol == symbol {
			position = &positions[i]
			break
		}
	}

	if position == nil || position.PositionAmt == 0 {
		e.logger.Debug("No position to close", zap.String("symbol", symbol))
		return 0, nil
	}

	e.logger.Info("Closing position",
		zap.String("symbol", symbol),
		zap.Float64("size", position.PositionAmt),
		zap.String("side", position.PositionSide))

	// Determine close side (opposite of position side)
	closeSide := "SELL"
	if position.PositionSide == "SELL" {
		closeSide = "BUY"
	}

	// Place market order to close position
	orderReq := client.PlaceOrderRequest{
		Symbol:   symbol,
		Side:     closeSide,
		Type:     "MARKET",
		Quantity: fmt.Sprintf("%f", math.Abs(position.PositionAmt)),
	}

	_, err = e.futuresClient.PlaceOrder(ctx, orderReq)
	if err != nil {
		return 0, fmt.Errorf("failed to place close order: %w", err)
	}

	return 1, nil
}

// verifyPositionsClosed verifies all positions are closed
func (e *ExitExecutor) verifyPositionsClosed(ctx context.Context, symbol string) error {
	// Check position via WebSocket cache
	positions := e.wsClient.GetCachedPositions()

	position, exists := positions[symbol]
	if !exists || position.PositionAmt == 0 {
		return nil // Position closed
	}

	// Position still exists - this is an error
	return fmt.Errorf("position still open after exit: symbol=%s, size=%f", symbol, position.PositionAmt)
}

// ExecuteEmergencyExit performs emergency exit for all symbols
func (e *ExitExecutor) ExecuteEmergencyExit(ctx context.Context, symbols []string) map[string]*ExitSequence {
	results := make(map[string]*ExitSequence)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, symbol := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("Emergency exit goroutine panic recovered",
						zap.String("symbol", sym),
						zap.Any("panic", r))
					mu.Lock()
					results[sym] = &ExitSequence{Error: fmt.Errorf("panic: %v", r)}
					mu.Unlock()
				}
			}()
			defer wg.Done()
			seq := e.ExecuteFastExit(ctx, sym)
			mu.Lock()
			results[sym] = seq
			mu.Unlock()
		}(symbol)
	}

	wg.Wait()
	return results
}
