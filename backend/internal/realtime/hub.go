package realtime

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"aster-bot/internal/client"

	"go.uber.org/zap"
)

type StateBlockReason string

const (
	BlockReasonMissingMarketData  StateBlockReason = "MISSING_MARKET_DATA"
	BlockReasonMissingAccountData StateBlockReason = "MISSING_ACCOUNT_DATA"
	BlockReasonStaleMarketData    StateBlockReason = "STALE_MARKET_DATA"
	BlockReasonStaleAccountData   StateBlockReason = "STALE_ACCOUNT_DATA"
)

type SymbolRuntimeSnapshot struct {
	Symbol             string
	CurrentPrice       float64
	BestBid            float64
	BestAsk            float64
	SpreadBps          float64
	SlippageEstBps     float64
	Volume24h          float64
	VolumePerHour      float64
	PositionSize       float64
	PositionNotional   float64
	InventoryNotional  float64
	UnrealizedPnL      float64
	RealizedPnL        float64
	NetPnLAfterFees    float64
	MakerFillRatio     float64
	FundingImpact      float64
	PositionAgeSec     float64
	InventoryStuckTime float64
	Side               string
	PendingOrders      int
	LastMarketEventAt  time.Time
	LastAccountEventAt time.Time
	LastOrderEventAt   time.Time
	BlockReason        StateBlockReason

	// Watchdog and metrics
	StateStuckCount   int
	RestFallbackCount uint64
}

type HubMetrics struct {
	LastMarketEventAt  time.Time
	LastAccountEventAt time.Time
	LastOrderEventAt   time.Time
	RestFallbackCount  uint64
	WSResyncCount      uint64
	StateStuckCount    uint64
}

type MarketStateProvider interface {
	GetKlines(ctx context.Context, symbol, interval string, limit int) ([]client.KlineMessage, error)
	GetTickerData(symbol string) (bestBid, bestAsk, volume24h float64, err error)
	GetLastPrice(symbol string) (float64, bool)
	EnsureKlineSubscription(symbol, interval string) error
}

type AccountStateProvider interface {
	GetPositions() map[string]client.Position
	GetOpenOrders(symbol string) []client.Order
	GetBalance() client.Balance
}

type RuntimeSnapshotProvider interface {
	GetSymbolSnapshot(ctx context.Context, symbol string) SymbolRuntimeSnapshot
	GetMetrics() HubMetrics
}

type ExecutionEventSink interface {
	IncrementWSResyncCount()
	IncrementStateStuckCount(symbol string)
}

type TradeRecord struct {
	Symbol    string
	Size      float64
	Price     float64
	Fee       float64
	IsMaker   bool
	Timestamp time.Time
}

type Hub struct {
	wsClient     *client.WebSocketClient
	marketClient *client.MarketClient
	logger       *zap.Logger

	mu               sync.Mutex
	bootstrappedKeys map[string]bool
	restFallbacks    uint64
	wsResyncCount    uint64
	stateStuckCount  uint64

	// Metrics tracking
	tradeHistory map[string][]TradeRecord // symbol -> recent trades
}

func NewHub(wsClient *client.WebSocketClient, marketClient *client.MarketClient, logger *zap.Logger) *Hub {
	return &Hub{
		wsClient:         wsClient,
		marketClient:     marketClient,
		logger:           logger.With(zap.String("component", "runtime_hub")),
		bootstrappedKeys: make(map[string]bool),
		tradeHistory:     make(map[string][]TradeRecord),
	}
}

func (h *Hub) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]client.KlineMessage, error) {
	if h.wsClient == nil {
		return nil, fmt.Errorf("runtime hub missing websocket client")
	}

	history := h.wsClient.GetRecentKlines(symbol, interval, limit)
	if len(history) >= limit || h.marketClient == nil {
		return history, nil
	}

	key := strings.ToUpper(symbol) + ":" + interval

	h.mu.Lock()
	needsBootstrap := !h.bootstrappedKeys[key]
	if needsBootstrap {
		h.bootstrappedKeys[key] = true
		h.restFallbacks++
	}
	h.mu.Unlock()

	if needsBootstrap {
		klines, err := h.marketClient.Klines(ctx, symbol, interval, limit)
		if err != nil {
			return history, err
		}

		bootstrap := make([]client.KlineMessage, 0, len(klines))
		for _, k := range klines {
			bootstrap = append(bootstrap, client.KlineMessage{
				Symbol:    strings.ToUpper(symbol),
				Interval:  interval,
				Open:      k.Open,
				High:      k.High,
				Low:       k.Low,
				Close:     k.Close,
				Volume:    k.Volume,
				IsClosed:  true,
				StartTime: k.OpenTime,
				EndTime:   k.CloseTime,
			})
		}
		h.wsClient.BootstrapKlines(symbol, interval, bootstrap)
		h.logger.Info("Bootstrapped kline cache from REST",
			zap.String("symbol", symbol),
			zap.String("interval", interval),
			zap.Int("count", len(bootstrap)))
	}

	return h.wsClient.GetRecentKlines(symbol, interval, limit), nil
}

func (h *Hub) GetTickerData(symbol string) (bestBid, bestAsk, volume24h float64, err error) {
	if h.wsClient == nil {
		return 0, 0, 0, fmt.Errorf("runtime hub missing websocket client")
	}
	return h.wsClient.GetTickerData(symbol)
}

func (h *Hub) GetLastPrice(symbol string) (float64, bool) {
	if h.wsClient == nil {
		return 0, false
	}
	return h.wsClient.GetLastPrice(symbol)
}

func (h *Hub) EnsureKlineSubscription(symbol, interval string) error {
	if h.wsClient == nil || !h.wsClient.IsRunning() {
		return nil
	}
	return h.wsClient.SubscribeToKlines([]string{symbol}, interval)
}

func (h *Hub) GetPositions() map[string]client.Position {
	if h.wsClient == nil {
		return nil
	}
	return h.wsClient.GetCachedPositions()
}

func (h *Hub) GetOpenOrders(symbol string) []client.Order {
	if h.wsClient == nil {
		return nil
	}
	return h.wsClient.GetCachedOrders(symbol)
}

func (h *Hub) GetBalance() client.Balance {
	if h.wsClient == nil {
		return client.Balance{}
	}
	return h.wsClient.GetCachedBalance()
}

func (h *Hub) RecordTrade(symbol string, size, price, fee float64, isMaker bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	record := TradeRecord{
		Symbol:    symbol,
		Size:      size,
		Price:     price,
		Fee:       fee,
		IsMaker:   isMaker,
		Timestamp: time.Now(),
	}

	h.tradeHistory[symbol] = append(h.tradeHistory[symbol], record)

	// Keep only last 24 hours of trades
	cutoff := time.Now().Add(-24 * time.Hour)
	var filtered []TradeRecord
	for _, r := range h.tradeHistory[symbol] {
		if r.Timestamp.After(cutoff) {
			filtered = append(filtered, r)
		}
	}
	h.tradeHistory[symbol] = filtered
}

func (h *Hub) GetSymbolSnapshot(ctx context.Context, symbol string) SymbolRuntimeSnapshot {
	_ = ctx

	snapshot := SymbolRuntimeSnapshot{
		Symbol: symbol,
	}

	if h.wsClient == nil {
		snapshot.BlockReason = BlockReasonMissingMarketData
		return snapshot
	}

	if price, ok := h.wsClient.GetLastPrice(symbol); ok {
		snapshot.CurrentPrice = price
	}
	if bid, ask, volume, err := h.wsClient.GetTickerData(symbol); err == nil {
		snapshot.BestBid = bid
		snapshot.BestAsk = ask
		snapshot.Volume24h = volume
		if ask > 0 && bid > 0 && ask >= bid {
			snapshot.SpreadBps = ((ask - bid) / ask) * 10000
			// Use spread as a conservative slippage proxy until the execution layer
			// exposes a deeper market impact estimator.
			snapshot.SlippageEstBps = snapshot.SpreadBps
		}
	}

	positions := h.wsClient.GetCachedPositions()
	if pos, ok := positions[strings.ToUpper(symbol)]; ok {
		snapshot.PositionSize = math.Abs(pos.PositionAmt)
		snapshot.PositionNotional = math.Abs(pos.PositionAmt * pos.MarkPrice)
		snapshot.InventoryNotional = snapshot.PositionNotional
		snapshot.UnrealizedPnL = pos.UnrealizedProfit
		if pos.PositionAmt > 0 {
			snapshot.Side = "LONG"
		} else if pos.PositionAmt < 0 {
			snapshot.Side = "SHORT"
		}
	}

	snapshot.PendingOrders = len(h.wsClient.GetCachedOrders(symbol))

	// Calculate metrics from trade history
	h.mu.Lock()
	history := h.tradeHistory[symbol]
	h.mu.Unlock()

	var volHour, totalFee, makerCount, totalTrades float64
	hourAgo := time.Now().Add(-1 * time.Hour)
	for _, r := range history {
		totalTrades++
		if r.IsMaker {
			makerCount++
		}
		totalFee += r.Fee
		if r.Timestamp.After(hourAgo) {
			volHour += r.Size * r.Price
		}
	}

	snapshot.VolumePerHour = volHour
	if totalTrades > 0 {
		snapshot.MakerFillRatio = makerCount / totalTrades
	} else {
		snapshot.MakerFillRatio = 1.0 // Default to 1.0 if no trades yet
	}
	snapshot.NetPnLAfterFees = snapshot.RealizedPnL - totalFee

	snapshot.LastMarketEventAt, snapshot.LastAccountEventAt, snapshot.LastOrderEventAt = h.wsClient.GetLastEventTimes()

	if !snapshot.LastAccountEventAt.IsZero() && snapshot.PositionSize > 0 {
		snapshot.PositionAgeSec = time.Since(snapshot.LastAccountEventAt).Seconds()
		// For bot farm, inventory stuck time is roughly position age if not rotating
		snapshot.InventoryStuckTime = snapshot.PositionAgeSec
	}

	switch {
	case snapshot.CurrentPrice <= 0:
		snapshot.BlockReason = BlockReasonMissingMarketData
	case h.wsClient.IsCacheStale("market"):
		snapshot.BlockReason = BlockReasonStaleMarketData
	case h.wsClient.IsCacheStale("positions") && snapshot.PositionSize > 0:
		snapshot.BlockReason = BlockReasonStaleAccountData
	case snapshot.PositionSize > 0 && snapshot.LastAccountEventAt.IsZero():
		snapshot.BlockReason = BlockReasonMissingAccountData
	}

	return snapshot
}

func (h *Hub) GetMetrics() HubMetrics {
	market, account, order := time.Time{}, time.Time{}, time.Time{}
	if h.wsClient != nil {
		market, account, order = h.wsClient.GetLastEventTimes()
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return HubMetrics{
		LastMarketEventAt:  market,
		LastAccountEventAt: account,
		LastOrderEventAt:   order,
		RestFallbackCount:  h.restFallbacks,
		WSResyncCount:      h.wsResyncCount,
		StateStuckCount:    h.stateStuckCount,
	}
}

func (h *Hub) IncrementWSResyncCount() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.wsResyncCount++
}

func (h *Hub) IncrementStateStuckCount(symbol string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stateStuckCount++
	h.logger.Warn("Runtime state stuck metric incremented", zap.String("symbol", symbol), zap.Uint64("total", h.stateStuckCount))
}
