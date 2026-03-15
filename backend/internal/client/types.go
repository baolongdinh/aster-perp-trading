// Package client - shared types for Aster Futures API responses.
package client

// Kline represents one OHLCV candlestick bar.
type Kline struct {
	OpenTime            int64   `json:"openTime"`
	Open                float64 `json:"open"`
	High                float64 `json:"high"`
	Low                 float64 `json:"low"`
	Close               float64 `json:"close"`
	Volume              float64 `json:"volume"`
	CloseTime           int64   `json:"closeTime"`
	QuoteVolume         float64 `json:"quoteVolume"`
	TradeCount          int     `json:"tradeCount"`
	TakerBuyBaseVolume  float64 `json:"takerBuyBaseVolume"`
	TakerBuyQuoteVolume float64 `json:"takerBuyQuoteVolume"`
}

// MarkPrice is the mark/index price snapshot.
type MarkPrice struct {
	Symbol               string  `json:"symbol"`
	MarkPrice            float64 `json:"markPrice,string"`
	IndexPrice           float64 `json:"indexPrice,string"`
	LastFundingRate      float64 `json:"lastFundingRate,string"`
	NextFundingTime      int64   `json:"nextFundingTime"`
}

// BookTicker is the best bid/ask.
type BookTicker struct {
	Symbol   string  `json:"symbol"`
	BidPrice float64 `json:"bidPrice,string"`
	BidQty   float64 `json:"bidQty,string"`
	AskPrice float64 `json:"askPrice,string"`
	AskQty   float64 `json:"askQty,string"`
}

// FundingRate is a historic funding rate entry.
type FundingRate struct {
	Symbol      string  `json:"symbol"`
	FundingRate float64 `json:"fundingRate,string"`
	FundingTime int64   `json:"fundingTime"`
}

// Balance entry per asset.
type Balance struct {
	Asset            string  `json:"asset"`
	WalletBalance    float64 `json:"walletBalance,string"`
	UnrealizedProfit float64 `json:"unrealizedProfit,string"`
	MarginBalance    float64 `json:"marginBalance,string"`
	AvailableBalance float64 `json:"availableBalance,string"`
}

// Position represents an open futures position.
type Position struct {
	Symbol           string  `json:"symbol"`
	PositionAmt      float64 `json:"positionAmt,string"`
	EntryPrice       float64 `json:"entryPrice,string"`
	MarkPrice        float64 `json:"markPrice,string"`
	UnrealizedProfit float64 `json:"unrealizedProfit,string"`
	Liquidation      float64 `json:"liquidationPrice,string"`
	Leverage         float64 `json:"leverage,string"`
	MarginType       string  `json:"marginType"`
	PositionSide     string  `json:"positionSide"` // BOTH | LONG | SHORT
}

// Order represents a placed order.
type Order struct {
	OrderID        int64   `json:"orderId"`
	ClientOrderID  string  `json:"clientOrderId"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	PositionSide   string  `json:"positionSide"`
	Type           string  `json:"type"`
	Status         string  `json:"status"`
	Price          float64 `json:"price,string"`
	OrigQty        float64 `json:"origQty,string"`
	ExecutedQty    float64 `json:"executedQty,string"`
	AvgPrice       float64 `json:"avgPrice,string"`
	TimeInForce    string  `json:"timeInForce"`
	ReduceOnly     bool    `json:"reduceOnly"`
	ClosePosition  bool    `json:"closePosition"`
	StopPrice      float64 `json:"stopPrice,string"`
	UpdateTime     int64   `json:"updateTime"`
}

// PlaceOrderRequest holds parameters for placing a new order.
type PlaceOrderRequest struct {
	Symbol           string
	Side             string  // BUY | SELL
	PositionSide     string  // BOTH | LONG | SHORT (hedge mode)
	Type             string  // LIMIT | MARKET | STOP | TAKE_PROFIT | STOP_MARKET | TAKE_PROFIT_MARKET | TRAILING_STOP_MARKET
	TimeInForce      string  // GTC | IOC | FOK | GTX
	Quantity         string
	Price            string
	StopPrice        string
	ReduceOnly       bool
	ClosePosition    bool
	CallbackRate     string
	ActivationPrice  string
	WorkingType      string // MARK_PRICE | CONTRACT_PRICE
	ClientOrderID    string
}

// CancelOrderRequest holds parameters for canceling an order.
type CancelOrderRequest struct {
	Symbol          string
	OrderID         int64
	ClientOrderID   string
}

// SetLeverageRequest sets leverage for a symbol.
type SetLeverageRequest struct {
	Symbol   string
	Leverage int
}

// AccountInfo is a partial account snapshot.
type AccountInfo struct {
	TotalWalletBalance     float64     `json:"totalWalletBalance,string"`
	TotalUnrealizedProfit  float64     `json:"totalUnrealizedProfit,string"`
	TotalMarginBalance     float64     `json:"totalMarginBalance,string"`
	AvailableBalance       float64     `json:"availableBalance,string"`
	Positions              []Position  `json:"positions"`
}
