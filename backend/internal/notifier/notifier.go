package notifier

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Config holds the configuration for the overall Notifier service.
type Config struct {
	BotToken    string
	ChatID      string
	AlertConfig AlertConfig
	ReportEvery time.Duration
	CheckEvery  time.Duration
}

// Notifier encapsulates the background notification service.
type Notifier struct {
	config   Config
	provider MetricsProvider
	client   *TelegramClient

	stopCh    chan struct{}
	wg        sync.WaitGroup
	isRunning bool

	lastReportTime time.Time
	lastPriceCache float64
}

// NewNotifier creates an instance of the Notifier service.
func NewNotifier(cfg Config, provider MetricsProvider) *Notifier {
	// Fallbacks
	if cfg.ReportEvery == 0 {
		cfg.ReportEvery = 30 * time.Minute
	}
	if cfg.CheckEvery == 0 {
		cfg.CheckEvery = 1 * time.Minute
	}

	return &Notifier{
		config:   cfg,
		provider: provider,
		client:   NewTelegramClient(cfg.BotToken, cfg.ChatID),
		stopCh:   make(chan struct{}),
	}
}

// SendStartup pushes an immediate message indicating the bot holds started.
func (n *Notifier) SendStartup(ctx context.Context, symbol string, minPrice, maxPrice float64, totalGrids int) {
	msg := FormatStartup(symbol, minPrice, maxPrice, totalGrids)
	if err := n.client.SendMessage(ctx, msg); err != nil {
		log.Printf("[Notifier] Failed to send startup message: %v\n", err)
	}
}

// SendError pushes critical error alerts.
func (n *Notifier) SendError(ctx context.Context, botErr error) {
	if botErr == nil {
		return
	}
	msg := FormatError(botErr)
	_ = n.client.SendMessage(ctx, msg)
}

// SendShutdown pushes bot shutdown alerts.
func (n *Notifier) SendShutdown(ctx context.Context) {
	msg := FormatAlert("Shutdown", "Bot đang được tắt một cách có chủ đích (Graceful Shutdown) hoặc bị ngắt tiến trình.")
	_ = n.client.SendMessage(ctx, msg)
}

// Start begins the background check routines for periodic reports and emergency alerts.
func (n *Notifier) Start(ctx context.Context) {
	if n.client.botToken == "" || n.client.chatID == "" {
		log.Println("[Notifier] Token or Chat ID disabled, shutting down internal notifier loops.")
		return
	}
	if n.provider == nil {
		log.Println("[Notifier] MetricsProvider is nil.")
		return
	}

	n.isRunning = true
	n.wg.Add(1)
	defer n.wg.Done()

	ticker := time.NewTicker(n.config.CheckEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.evaluateMetrics(ctx)
		}
	}
}

// evaluateMetrics runs routine checks against rulesets.
func (n *Notifier) evaluateMetrics(ctx context.Context) {
	metrics := n.provider.GetCurrentMetrics()

	// 1. Check Periodic Report
	if time.Since(n.lastReportTime) >= n.config.ReportEvery {
		msg, err := FormatPeriodicReport(metrics)
		if err == nil {
			_ = n.client.SendMessage(ctx, msg)
			n.lastReportTime = time.Now()
		} else {
			log.Printf("[Notifier] Failed to format periodic report: %v\n", err)
		}
	}

	// 2. Alert Checks

	// Breakout
	if HasGridBreakout(metrics) {
		msg := FormatAlert("Breakout", fmt.Sprintf("Giá %f đã thoát khỏi khung Grid (%.f -> %.f). Lệnh đang pending có thể bị treo hoặc dính rủi ro.", metrics.CurrentPrice, metrics.GridMinPrice, metrics.GridMaxPrice))
		_ = n.client.SendMessage(ctx, msg)
		// Rate limit warnings could be added here to avoid spamming breakout alerts
	}

	// High Drawdown
	if IsDrawdownCritical(metrics, n.config.AlertConfig) {
		msg := FormatAlert("Drawdown", fmt.Sprintf("Drawdown đã đạt %.2f%%, vượt ngưỡng an toàn (%.2f%%).", metrics.DrawdownPct, n.config.AlertConfig.DrawdownThresholdPct))
		_ = n.client.SendMessage(ctx, msg)
	}

	// No Orders
	if IsNoOrdersCritical(metrics, n.config.AlertConfig) {
		msg := FormatAlert("NoOrders", fmt.Sprintf("Không có lệnh nào được thực thi trong %.0f phút qua. Hãy kiểm tra lại API hoặc spread.", n.config.AlertConfig.NoOrderMinutes))
		_ = n.client.SendMessage(ctx, msg)
	}

	// High Volatility
	if n.lastPriceCache > 0 && HasHighVolatility(n.lastPriceCache, metrics.CurrentPrice, n.config.AlertConfig) {
		msg := FormatAlert("Volatility", fmt.Sprintf("Biến động giá mạnh trực tiếp: Giá hiện tại $%.2f.", metrics.CurrentPrice))
		_ = n.client.SendMessage(ctx, msg)
	}

	n.lastPriceCache = metrics.CurrentPrice
}

// Stop cleanly terminates the notifying routine.
func (n *Notifier) Stop() {
	if n.isRunning {
		close(n.stopCh)
		n.wg.Wait()
		n.isRunning = false
	}
}
