package notifier

import (
	"time"
)

// MetricsProvider defines the interface required for any trading bot
// to supply real-time metrics to the Telegram Notifier.
type MetricsProvider interface {
	GetCurrentMetrics() GridMetrics
}

// GridMetrics represents the current real-time state of the trading bot.
type GridMetrics struct {
	Symbol        string
	CurrentPrice  float64
	RealizedPnL   float64
	UnrealizedPnL float64
	FeesPaid      float64
	NetPnL        float64

	Volume30m       float64
	FilledOrders30m int
	PendingOrders   int

	GridMinPrice  float64
	GridMaxPrice  float64
	ActiveGrids   int
	TotalGrids    int
	LastOrderTime time.Time

	InitialCapital float64
	CurrentCapital float64
	ROI            float64

	DrawdownPct float64
	Uptime      time.Duration
}
