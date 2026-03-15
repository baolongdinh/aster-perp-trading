// Package strategy defines the Strategy interface and common signal types.
package strategy

import (
	"aster-bot/internal/client"
	"aster-bot/internal/stream"
)

// Side represents trade direction.
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// SignalType describes what action to take.
type SignalType string

const (
	SignalEnter SignalType = "ENTER"
	SignalExit  SignalType = "EXIT"
	SignalNone  SignalType = "NONE"
)

// Signal is the output of a strategy decision.
type Signal struct {
	Type         SignalType
	Symbol       string
	Side         Side
	PositionSide string  // BOTH | LONG | SHORT
	Quantity     string  // USDT notional or coin qty (strategy fills this)
	Price        string  // empty = MARKET order
	StopLoss     float64 // stop-loss price (0 = no SL)
	TakeProfit   float64 // take-profit price (0 = no TP)
	Reason       string  // log label
	StrategyName string  // which sub-strategy fired this
}

// Strategy is implemented by every trading strategy.
type Strategy interface {
	// Name returns the unique strategy name.
	Name() string

	// OnKline is called for each closed/live kline event.
	OnKline(k stream.WsKline)

	// OnMarkPrice is called for each mark price update.
	OnMarkPrice(mp stream.WsMarkPrice)

	// OnOrderUpdate is called when an order status changes.
	OnOrderUpdate(u stream.WsOrderUpdate)

	// OnAccountUpdate is called when balances/positions change.
	OnAccountUpdate(u stream.WsAccountUpdate)

	// Signal returns the current signal (may return SignalNone).
	Signal(symbol string, currentPos *client.Position) *Signal

	// Symbols returns which symbols this strategy watches.
	Symbols() []string

	// State returns a human-readable string of the strategy's current internal state.
	State(symbol string) string

	// IsEnabled returns whether this strategy is active.
	IsEnabled() bool

	// SetEnabled sets the enabled state.
	SetEnabled(bool)
}
