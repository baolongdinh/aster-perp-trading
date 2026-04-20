// Package stream provides WebSocket client for Aster Finance futures streams.
// Handles market data streams: aggTrade, kline, markPrice, bookTicker, depth.
// Per aster-api-websocket-v3 skill: stream names are lowercase.
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	pingInterval      = 3 * time.Minute // send ping before 5min server ping window
	pongTimeout       = 15 * time.Second
	reconnectDelay    = 2 * time.Second
	maxReconnectDelay = 60 * time.Second
)

// WsKline is a WebSocket kline event.
type WsKline struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	Kline     struct {
		StartTime   int64   `json:"t"`
		CloseTime   int64   `json:"T"`
		Interval    string  `json:"i"`
		Open        float64 `json:"o,string"`
		High        float64 `json:"h,string"`
		Low         float64 `json:"l,string"`
		Close       float64 `json:"c,string"`
		Volume      float64 `json:"v,string"`
		IsClosed    bool    `json:"x"`
		QuoteVolume float64 `json:"q,string"`
		TradeCount  int     `json:"n"`
	} `json:"k"`
}

// WsMarkPrice is a WebSocket mark price event.
type WsMarkPrice struct {
	EventType       string  `json:"e"`
	EventTime       int64   `json:"E"`
	Symbol          string  `json:"s"`
	MarkPrice       float64 `json:"p,string"`
	IndexPrice      float64 `json:"i,string"`
	FundingRate     float64 `json:"r,string"`
	NextFundingTime int64   `json:"T"`
}

// WsAggTrade is a WebSocket aggregate trade event.
type WsAggTrade struct {
	EventType    string  `json:"e"`
	EventTime    int64   `json:"E"`
	Symbol       string  `json:"s"`
	AggTradeID   int64   `json:"a"`
	Price        float64 `json:"p,string"`
	Quantity     float64 `json:"q,string"`
	IsBuyerMaker bool    `json:"m"`
}

// WsBookTicker is a WebSocket best bid/ask event.
type WsBookTicker struct {
	Symbol   string  `json:"s"`
	BidPrice float64 `json:"b,string"`
	BidQty   float64 `json:"B,string"`
	AskPrice float64 `json:"a,string"`
	AskQty   float64 `json:"A,string"`
}

// RawMessage is an untyped WS message for routing.
type RawMessage struct {
	EventType string          `json:"e"`
	Stream    string          `json:"stream"`
	Data      json.RawMessage `json:"data"`
	Raw       json.RawMessage // original
}

// Handlers for market event types.
type MarketHandlers struct {
	OnKline      func(WsKline)
	OnMarkPrice  func(WsMarkPrice)
	OnAggTrade   func(WsAggTrade)
	OnBookTicker func(WsBookTicker)
}

// MarketStream manages a WebSocket connection to market data streams.
type MarketStream struct {
	wsBase   string
	symbols  []string
	streams  []string // e.g. "btcusdt@kline_1m", "btcusdt@markPrice@1s"
	handlers MarketHandlers
	log      *zap.Logger

	mu   sync.Mutex
	conn *websocket.Conn
}

// NewMarketStream creates a new market stream manager.
// streams is a list of stream names, e.g. []string{"btcusdt@kline_1m", "btcusdt@markPrice@1s"}.
func NewMarketStream(wsBase string, streams []string, handlers MarketHandlers, log *zap.Logger) *MarketStream {
	return &MarketStream{
		wsBase:   strings.TrimRight(wsBase, "/"),
		streams:  streams,
		handlers: handlers,
		log:      log,
	}
}

// BuildStreams returns default stream names for given symbols and timeframe.
func BuildStreams(symbols []string, klineIntervals []string) []string {
	var s []string
	for _, sym := range symbols {
		lower := strings.ToLower(sym)
		for _, interval := range klineIntervals {
			s = append(s, fmt.Sprintf("%s@kline_%s", lower, interval))
		}
		s = append(s,
			fmt.Sprintf("%s@markPrice@1s", lower),
			fmt.Sprintf("%s@aggTrade", lower),
			fmt.Sprintf("%s@bookTicker", lower),
		)
	}
	return s
}

// Run connects and reads messages until ctx is cancelled. Reconnects on failure.
func (ms *MarketStream) Run(ctx context.Context) {
	delay := reconnectDelay
	for {
		if ctx.Err() != nil {
			return
		}
		err := ms.connect(ctx)
		if err != nil {
			ms.log.Error("market stream connect error", zap.Error(err), zap.Duration("retry_in", delay))
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if delay < maxReconnectDelay {
				delay *= 2
			}
			continue
		}
		delay = reconnectDelay // reset on success
	}
}

func (ms *MarketStream) connect(ctx context.Context) error {
	// Build combined stream URL
	streamParam := strings.Join(ms.streams, "/")
	u := fmt.Sprintf("%s/stream?streams=%s", ms.wsBase, streamParam)

	ms.log.Info("connecting market stream", zap.String("url", u))
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return err
	}
	ms.log.Info("market stream connected successfully")

	ms.mu.Lock()
	ms.conn = conn
	ms.mu.Unlock()
	defer func() {
		ms.mu.Lock()
		ms.conn = nil
		ms.mu.Unlock()
		conn.Close()
	}()

	// Ping goroutine
	pingDone := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ms.log.Error("MarketStream ping goroutine panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
			close(pingDone)
		}()
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongTimeout + pingInterval))
		return nil
	})
	conn.SetReadDeadline(time.Now().Add(pongTimeout + pingInterval))

	for {
		if ctx.Err() != nil {
			return nil
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		ms.dispatch(msg)
	}
}

func (ms *MarketStream) dispatch(raw []byte) {
	// Combined stream wrapper: {"stream":"..." ,"data":{...}}
	var wrapper struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}
	payload := raw
	if json.Unmarshal(raw, &wrapper) == nil && wrapper.Stream != "" {
		payload = wrapper.Data
	}

	// Peek event type
	var peek struct {
		EventType string `json:"e"`
	}
	json.Unmarshal(payload, &peek)

	switch peek.EventType {
	case "kline":
		var k WsKline
		if json.Unmarshal(payload, &k) == nil && ms.handlers.OnKline != nil {
			ms.handlers.OnKline(k)
		}
	case "markPriceUpdate":
		var mp WsMarkPrice
		if json.Unmarshal(payload, &mp) == nil && ms.handlers.OnMarkPrice != nil {
			ms.handlers.OnMarkPrice(mp)
		}
	case "aggTrade":
		var at WsAggTrade
		if json.Unmarshal(payload, &at) == nil && ms.handlers.OnAggTrade != nil {
			ms.handlers.OnAggTrade(at)
		}
	default:
		// bookTicker has no "e" field
		var bt WsBookTicker
		if json.Unmarshal(payload, &bt) == nil && bt.Symbol != "" && ms.handlers.OnBookTicker != nil {
			ms.handlers.OnBookTicker(bt)
		}
	}
}
