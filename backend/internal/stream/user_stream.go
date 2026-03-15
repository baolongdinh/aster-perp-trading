package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// User data event types.
const (
	EventAccountUpdate      = "ACCOUNT_UPDATE"
	EventOrderTradeUpdate   = "ORDER_TRADE_UPDATE"
	EventAccountConfig      = "ACCOUNT_CONFIG_UPDATE"
	EventMarginCall         = "MARGIN_CALL"
	EventListenKeyExpired   = "listenKeyExpired"
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
		TradeID       int64   `json:"t"`
		RealizedPnL   float64 `json:"rp,string"`
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
			Symbol              string  `json:"s"`
			PositionAmt         float64 `json:"pa,string"`
			EntryPrice          float64 `json:"ep,string"`
			BreakevenPrice      float64 `json:"bep,string"`
			AccumulatedRealPnL  float64 `json:"cr,string"`
			UnrealizedPnL       float64 `json:"up,string"`
			MarginType          string  `json:"mt"`
			IsolatedMargin      float64 `json:"iw,string"`
			PositionSide        string  `json:"ps"`
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
	wsBase   string
	getListenKey func(ctx context.Context) (string, error)
	keepalive    func(ctx context.Context) error
	handlers UserStreamHandlers
	log      *zap.Logger
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
	listenKey, err := us.getListenKey(ctx)
	if err != nil {
		return fmt.Errorf("user stream: get listenKey: %w", err)
	}
	u := fmt.Sprintf("%s/ws/%s", us.wsBase, listenKey)
	us.log.Info("connecting user data stream")

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return err
	}
	us.log.Info("user data stream connected successfully")
	defer conn.Close()

	// Keepalive goroutine: PUT listenKey every 30 min (server expires at 60 min)
	go func() {
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
		us.dispatch(msg)
	}
}

func (us *UserStream) dispatch(raw []byte) {
	var peek struct {
		EventType string `json:"e"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return
	}
	switch peek.EventType {
	case EventOrderTradeUpdate:
		var ev WsOrderUpdate
		if json.Unmarshal(raw, &ev) == nil && us.handlers.OnOrderUpdate != nil {
			us.handlers.OnOrderUpdate(ev)
		}
	case EventAccountUpdate:
		var ev WsAccountUpdate
		if json.Unmarshal(raw, &ev) == nil && us.handlers.OnAccountUpdate != nil {
			us.handlers.OnAccountUpdate(ev)
		}
	case EventMarginCall:
		if us.handlers.OnMarginCall != nil {
			us.handlers.OnMarginCall(raw)
		}
	case EventListenKeyExpired:
		us.log.Warn("listenKey expired, reconnecting user stream")
		// Returning error causes reconnect loop to get a new listenKey
		// We can't directly return from here but the connection will die shortly
	}
}
