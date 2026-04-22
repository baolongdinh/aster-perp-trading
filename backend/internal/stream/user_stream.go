package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// User data event types.
const (
	EventAccountUpdate    = "ACCOUNT_UPDATE"
	EventOrderTradeUpdate = "ORDER_TRADE_UPDATE"
	EventAccountConfig    = "ACCOUNT_CONFIG_UPDATE"
	EventMarginCall       = "MARGIN_CALL"
	EventListenKeyExpired = "listenKeyExpired"
)

// WsOrderUpdate is the ORDER_TRADE_UPDATE event.
type WsOrderUpdate struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Order     struct {
		Symbol        string  `json:"s"`
		ClientOrderID string  `json:"c"`
		Side          string  `json:"S"`
		OrderType     string  `json:"o"`
		TimeInForce   string  `json:"f"`
		OrigQty       float64 `json:"q,string"`
		Price         float64 `json:"p,string"`
		AvgPrice      float64 `json:"ap,string"`
		StopPrice     float64 `json:"sp,string"`
		ExecType      string  `json:"x"` // NEW, PARTIAL_FILL, FILL, CANCELED, EXPIRED, etc.
		OrderStatus   string  `json:"X"`
		OrderID       int64   `json:"i"`
		FilledQty     float64 `json:"l,string"`
		CumFilledQty  float64 `json:"z,string"`
		Commission    float64 `json:"n,string"`
		CommAsset     string  `json:"N"`
		TradeID       int64   `json:"t"`
		RealizedPnL   float64 `json:"rp,string"`
		IsMaker       bool    `json:"m"`
		PositionSide  string  `json:"ps"`
		IsReduceOnly  bool    `json:"R"`
	} `json:"o"`
}

// WsAccountUpdate is the ACCOUNT_UPDATE event.
type WsAccountUpdate struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Update    struct {
		EventReason string `json:"m"`
		Balances    []struct {
			Asset              string  `json:"a"`
			WalletBalance      float64 `json:"wb,string"`
			CrossWalletBalance float64 `json:"cw,string"`
			BalanceChange      float64 `json:"bc,string"`
		} `json:"B"`
		Positions []struct {
			Symbol             string  `json:"s"`
			PositionAmt        float64 `json:"pa,string"`
			EntryPrice         float64 `json:"ep,string"`
			BreakevenPrice     float64 `json:"bep,string"`
			AccumulatedRealPnL float64 `json:"cr,string"`
			UnrealizedPnL      float64 `json:"up,string"`
			MarginType         string  `json:"mt"`
			IsolatedMargin     float64 `json:"iw,string"`
			PositionSide       string  `json:"ps"`
		} `json:"P"`
	} `json:"a"`
}

// UserStreamHandlers receives user data events.
type UserStreamHandlers struct {
	OnOrderUpdate   func(WsOrderUpdate)
	OnAccountUpdate func(WsAccountUpdate)
	OnMarginCall    func(json.RawMessage)
}

// UserStream manages the user data WebSocket stream with listenKey keepalive.
type UserStream struct {
	wsBase       string
	getListenKey func(ctx context.Context) (string, error)
	keepalive    func(ctx context.Context) error
	handlers     UserStreamHandlers
	log          *zap.Logger
	connected    atomic.Uint32
}

// NewUserStream creates a user data stream.
// getListenKey and keepalive should call FuturesClient methods.
func NewUserStream(
	wsBase string,
	getListenKey func(ctx context.Context) (string, error),
	keepalive func(ctx context.Context) error,
	handlers UserStreamHandlers,
	log *zap.Logger,
) *UserStream {
	return &UserStream{
		wsBase:       strings.TrimRight(wsBase, "/"),
		getListenKey: getListenKey,
		keepalive:    keepalive,
		handlers:     handlers,
		log:          log,
	}
}

// Run connects user data stream and maintains listenKey. Reconnects on expiry/failure.
func (us *UserStream) Run(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			us.log.Error("UserStream goroutine panic recovered",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()

	delay := reconnectDelay
	for {
		if ctx.Err() != nil {
			return
		}
		err := us.connect(ctx)
		if err != nil {
			us.log.Error("user stream error", zap.Error(err), zap.Duration("retry_in", delay))
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
		delay = reconnectDelay
	}
}

func (us *UserStream) connect(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			us.log.Error("UserStream connect panic recovered",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()

	listenKey, err := us.getListenKey(ctx)
	if err != nil {
		return fmt.Errorf("user stream: get listenKey: %w", err)
	}
	us.log.Info("listenKey obtained successfully",
		zap.String("listen_key", listenKey))
	u := fmt.Sprintf("%s/ws/%s", us.wsBase, listenKey)
	us.log.Info("connecting user data stream",
		zap.String("ws_url", u),
		zap.String("listen_key", listenKey))

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		us.log.Error("failed to connect user data stream",
			zap.String("ws_url", u),
			zap.Error(err))
		return err
	}
	us.log.Info("user data stream connected successfully")
	us.connected.Store(1)
	defer conn.Close()
	defer us.connected.Store(0)

	// Keepalive goroutine: PUT listenKey every 30 min (server expires at 60 min)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				us.log.Error("UserStream keepalive panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())))
			}
		}()

		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := us.keepalive(ctx); err != nil {
					us.log.Error("listenKey keepalive failed", zap.Error(err))
				} else {
					us.log.Debug("listenKey keepalive OK")
				}
			}
		}
	}()

	conn.SetReadDeadline(time.Now().Add(70 * time.Minute))

	messageCount := 0
	lastMessageTime := time.Now()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				sinceLastMsg := time.Since(lastMessageTime).Seconds()
				us.log.Info("UserStream heartbeat",
					zap.Int("total_messages", messageCount),
					zap.Float64("seconds_since_last_message", sinceLastMsg))
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		if ctx.Err() != nil {
			return nil
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("user stream read: %w", err)
		}
		// Reset deadline on any message
		conn.SetReadDeadline(time.Now().Add(70 * time.Minute))
		messageCount++
		lastMessageTime = time.Now()
		if reconnect := us.dispatch(msg); reconnect {
			return fmt.Errorf("listenKey expired")
		}
	}
}

func (us *UserStream) dispatch(raw []byte) bool {
	var peek struct {
		EventType string `json:"e"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		us.log.Debug("Failed to unmarshal user stream message", zap.Error(err))
		return false
	}

	// Debug log all received event types
	us.log.Debug("User stream message received", zap.String("event_type", peek.EventType))

	switch peek.EventType {
	case EventOrderTradeUpdate:
		var ev WsOrderUpdate
		if json.Unmarshal(raw, &ev) == nil && us.handlers.OnOrderUpdate != nil {
			us.log.Debug("Order update dispatched")
			us.handlers.OnOrderUpdate(ev)
		}
	case EventAccountUpdate:
		var ev WsAccountUpdate
		if json.Unmarshal(raw, &ev) == nil && us.handlers.OnAccountUpdate != nil {
			us.log.Info("Account update dispatched",
				zap.Int("positions", len(ev.Update.Positions)),
				zap.Int("balances", len(ev.Update.Balances)))
			us.handlers.OnAccountUpdate(ev)
		} else {
			us.log.Warn("Failed to unmarshal account update or handler nil",
				zap.String("raw", string(raw)))
		}
	case EventMarginCall:
		if us.handlers.OnMarginCall != nil {
			us.log.Warn("Margin call received")
			us.handlers.OnMarginCall(raw)
		}
	case EventListenKeyExpired:
		us.log.Warn("listenKey expired, reconnecting user stream")
		return true
	default:
		us.log.Debug("Unknown user stream event type", zap.String("event_type", peek.EventType))
	}
	return false
}

// IsConnected reports whether the user data stream currently has an active websocket connection.
func (us *UserStream) IsConnected() bool {
	return us != nil && us.connected.Load() == 1
}
