package maker

import (
	"context"
	"time"

	"aster-bot/internal/client"
)

type OrderSide string
type OrderType string
type OrderStatus string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"

	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"

	OrderStatusNew       OrderStatus = "NEW"
	OrderStatusPartially OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled    OrderStatus = "FILLED"
	OrderStatusCanceled  OrderStatus = "CANCELED"
	OrderStatusExpired   OrderStatus = "EXPIRED"
	OrderStatusNewCancel OrderStatus = "NEW"
)

type Order struct {
	OrderID       int64       `json:"orderId"`
	Symbol        string      `json:"symbol"`
	Side          OrderSide   `json:"side"`
	Type          OrderType   `json:"type"`
	Price         float64     `json:"price"`
	OrigQty       float64     `json:"origQty"`
	ExecutedQty   float64     `json:"executedQty"`
	Status        OrderStatus `json:"status"`
	TimeInForce   string      `json:"timeInForce"`
	UpdateTime    int64       `json:"updateTime"`
	ClientOrderID string      `json:"clientOrderId"`
	// Local tracking for smart cancellation
	PlacedTime time.Time `json:"-"`
	AgeSeconds int64     `json:"-"`
}

type PositionState struct {
	Symbol        string
	Amount        float64
	EntryPrice    float64
	MarkPrice     float64
	UnrealizedPNL float64
	UpdatedAt     time.Time
}

type InventoryState struct {
	LongExposure  float64
	ShortExposure float64
	NetExposure   float64
	MaxExposure   float64
}

type BestPrice struct {
	Symbol    string
	BidPrice  float64
	BidQty    float64
	AskPrice  float64
	AskQty    float64
	Timestamp time.Time
}

type LimitOrderRequest struct {
	Symbol      string
	Side        OrderSide
	Price       float64
	Quantity    float64
	TimeInForce string
}

type MakerStrategy interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	PlaceOrders(symbol string) error
	CancelOrders(symbol string) error
	ReplaceOrder(oldOrderID int64, symbol string) error
	GetSpread(symbol string) float64
	GetPosition(symbol string) *PositionState
	GetInventoryState() InventoryState
	GetConfig() *Config
}

type OrderManager interface {
	PlaceLimitOrder(ctx context.Context, req LimitOrderRequest) (*Order, error)
	CancelOrder(ctx context.Context, symbol string, orderID int64) error
	GetOpenOrders(symbol string) ([]Order, error)
	BatchCancel(symbol string, orderIDs []int64) error
}

type InventoryManager interface {
	UpdatePosition(symbol string, amount float64, entryPrice, markPrice float64)
	GetPosition(symbol string) *PositionState
	GetNetExposure() float64
	ShouldRebalance(symbol string) bool
	CalculateTargetSpread(symbol string, baseSpreadBps float64) (buySpread, sellSpread float64)
}

type RiskGuard interface {
	Check(ctx context.Context) (shouldStop bool, reason string)
	Name() string
}

type FuturesClientInterface interface {
	PlaceOrder(ctx context.Context, req client.PlaceOrderRequest) (*client.Order, error)
	CancelOrder(ctx context.Context, req client.CancelOrderRequest) (*client.Order, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]client.Order, error)
	GetPositions(ctx context.Context) ([]client.Position, error)
	GetAccountInfo(ctx context.Context) (*client.AccountInfo, error)
}

type WebSocketClientInterface interface {
	SubscribeToTicker(symbols []string) error
	GetTickerChannel() <-chan map[string]interface{}
	GetTickerData(symbol string) (bestBid, bestAsk, volume24h float64, err error)
	IsRunning() bool
	GetCachedPositions() map[string]client.Position
	GetCachedBalance() client.Balance
}

// === NEW: Extended Types for Risk Optimization ===

type ZoneType string

const (
	ZoneAboveEMA  ZoneType = "above_ema"
	ZoneNormalDip ZoneType = "normal_dip"
	ZoneStrongDip ZoneType = "strong_dip"
	ZoneHardDip   ZoneType = "hard_dip"
)

type TrailingState struct {
	PositionID    string
	PeakProfitPct float64
	ActivationPct float64
	CallbackPct   float64
	IsActive      bool
}

type GridActiveZone struct {
	MinPrice    float64
	MaxPrice    float64
	GridSpacing float64
	LevelCount  int
	Levels      []float64
}

type DailyResetState struct {
	LastResetDate   time.Time
	ResetHour       int
	PositionsClosed int
	TotalVolume     float64
	TotalProfit     float64
}
