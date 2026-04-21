package client

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestWebSocketClientCacheAliasesAndAggregateOrders(t *testing.T) {
	ws := NewWebSocketClient("wss://example.test/ws", zap.NewNop())
	now := time.Now()

	ws.lastOrderUpdate = now
	ws.lastPosUpdate = now
	ws.lastBalUpdate = now
	ws.lastMarketUpdate = now

	if ws.IsCacheStale("order") {
		t.Fatalf("expected singular order alias to be fresh")
	}
	if ws.IsCacheStale("position") {
		t.Fatalf("expected singular position alias to be fresh")
	}
	if ws.IsCacheStale("balances") {
		t.Fatalf("expected balances alias to be fresh")
	}
	if ws.IsCacheStale("ticker") {
		t.Fatalf("expected ticker alias to be fresh")
	}

	ws.UpdateOrderCache(Order{OrderID: 1, Symbol: "BTCUSDT", Status: "NEW"})
	ws.UpdateOrderCache(Order{OrderID: 2, Symbol: "ETHUSDT", Status: "NEW"})

	orders := ws.GetCachedOrders("")
	if len(orders) != 2 {
		t.Fatalf("expected aggregated cached orders, got %d", len(orders))
	}
}

func TestWebSocketClientBootstrapKlines(t *testing.T) {
	ws := NewWebSocketClient("wss://example.test/ws", zap.NewNop())
	history := []KlineMessage{
		{Symbol: "BTCUSDT", Interval: "1m", Close: 100, EndTime: 1000},
		{Symbol: "BTCUSDT", Interval: "1m", Close: 101, EndTime: 2000},
		{Symbol: "BTCUSDT", Interval: "1m", Close: 102, EndTime: 3000},
	}

	ws.BootstrapKlines("BTCUSDT", "1m", history)

	klines := ws.GetRecentKlines("BTCUSDT", "1m", 2)
	if len(klines) != 2 {
		t.Fatalf("expected 2 klines, got %d", len(klines))
	}
	if klines[0].Close != 101 || klines[1].Close != 102 {
		t.Fatalf("unexpected kline window: %+v", klines)
	}
}
