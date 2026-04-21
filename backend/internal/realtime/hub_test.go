package realtime

import (
	"context"
	"testing"
	"time"

	"aster-bot/internal/client"

	"go.uber.org/zap"
)

func TestHubBuildsRuntimeSnapshotFromWebSocketState(t *testing.T) {
	ws := client.NewWebSocketClient("wss://example.test/ws", zap.NewNop())
	ws.UpdatePositionCache(client.Position{
		Symbol:           "BTCUSDT",
		PositionAmt:      2,
		MarkPrice:        100,
		UnrealizedProfit: 5,
		PositionSide:     "LONG",
	})
	ws.UpdateOrderCache(client.Order{OrderID: 1, Symbol: "BTCUSDT", Status: "NEW"})
	ws.BootstrapKlines("BTCUSDT", "1m", []client.KlineMessage{
		{Symbol: "BTCUSDT", Interval: "1m", Close: 100, EndTime: 1000},
	})
	ws.UpsertTickerSnapshot(client.TickerSnapshot{
		Symbol:    "BTCUSDT",
		LastPrice: 100,
		BidPrice:  99.5,
		AskPrice:  100.5,
		Volume24h: 2500000,
		EventTime: time.Now().UnixMilli(),
	})

	hub := NewHub(ws, nil, zap.NewNop())
	snapshot := hub.GetSymbolSnapshot(context.Background(), "BTCUSDT")

	if snapshot.CurrentPrice != 100 {
		t.Fatalf("expected current price 100, got %v", snapshot.CurrentPrice)
	}
	if snapshot.PositionSize != 2 {
		t.Fatalf("expected position size 2, got %v", snapshot.PositionSize)
	}
	if snapshot.PendingOrders != 1 {
		t.Fatalf("expected 1 pending order, got %d", snapshot.PendingOrders)
	}
	if snapshot.BlockReason != "" {
		t.Fatalf("expected healthy snapshot, got block reason %q", snapshot.BlockReason)
	}
}
