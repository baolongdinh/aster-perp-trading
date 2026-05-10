package maker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

type AlertType string

const (
	AlertLiquidation    AlertType = "liquidation"
	AlertEmergencyStop  AlertType = "emergency_stop"
	AlertPositionBias   AlertType = "position_bias"
	AlertDailyLoss      AlertType = "daily_loss"
	AlertHighDrawdown   AlertType = "high_drawdown"
	AlertCircuitBreaker AlertType = "circuit_breaker"
	AlertStartup        AlertType = "startup"
	AlertShutdown       AlertType = "shutdown"
)

type AlertSeverity string

const (
	SeverityCritical AlertSeverity = "critical"
	SeverityWarning  AlertSeverity = "warning"
	SeverityInfo     AlertSeverity = "info"
)

type Alert struct {
	Type      AlertType
	Severity  AlertSeverity
	Title     string
	Message   string
	Timestamp time.Time
	Data      map[string]interface{}
}

type Notifier interface {
	Send(alert Alert) error
	IsEnabled() bool
}

type AlertManager struct {
	notifier    Notifier
	enabled     bool
	rateLimiter *RateLimiter
	logger      *zap.Logger
}

type RateLimiter struct {
	mu       sync.RWMutex
	lastSent map[AlertType]time.Time
	window   time.Duration
}

func NewRateLimiter(window time.Duration) *RateLimiter {
	if window == 0 {
		window = 5 * time.Minute
	}
	return &RateLimiter{
		lastSent: make(map[AlertType]time.Time),
		window:   window,
	}
}

func (r *RateLimiter) CanSend(alertType AlertType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	last, exists := r.lastSent[alertType]
	if !exists {
		return true
	}
	return time.Since(last) >= r.window
}

func (r *RateLimiter) RecordSent(alertType AlertType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastSent[alertType] = time.Now()
}

func NewAlertManager(logger *zap.Logger, rateLimitWindow time.Duration) *AlertManager {
	notifier := NewTelegramNotifier(logger)
	enabled := notifier.IsEnabled()

	if enabled {
		logger.Info("Telegram alerts enabled for Maker Strategy")
	} else {
		logger.Info("Telegram alerts disabled - set TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID")
	}

	return &AlertManager{
		notifier:    notifier,
		enabled:     enabled,
		rateLimiter: NewRateLimiter(rateLimitWindow),
		logger:      logger,
	}
}

func (am *AlertManager) IsEnabled() bool {
	return am.enabled && am.notifier != nil
}

func (am *AlertManager) Send(alert Alert) error {
	if !am.IsEnabled() {
		return nil
	}

	if !am.rateLimiter.CanSend(alert.Type) {
		am.logger.Debug("Alert rate limited",
			zap.String("type", string(alert.Type)),
			zap.String("title", alert.Title))
		return nil
	}

	err := am.notifier.Send(alert)
	if err != nil {
		am.logger.Error("Failed to send alert",
			zap.String("type", string(alert.Type)),
			zap.Error(err))
		return err
	}

	am.rateLimiter.RecordSent(alert.Type)
	return nil
}

func (am *AlertManager) NotifyLiquidation(symbol string, position, threshold float64) error {
	alert := Alert{
		Type:      AlertLiquidation,
		Severity:  SeverityCritical,
		Title:     "🚨 LIQUIDATION RISK",
		Message:   fmt.Sprintf("Position %.4f %s approaching liquidation threshold (%.1f%%)", position, symbol, threshold*100),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"Symbol":    symbol,
			"Position":  fmt.Sprintf("%.4f", position),
			"Threshold": fmt.Sprintf("%.1f%%", threshold*100),
		},
	}
	return am.Send(alert)
}

func (am *AlertManager) NotifyEmergencyStop(reason string, details map[string]interface{}) error {
	alert := Alert{
		Type:      AlertEmergencyStop,
		Severity:  SeverityCritical,
		Title:     "🚨 EMERGENCY STOP TRIGGERED",
		Message:   fmt.Sprintf("Bot stopped due to: %s", reason),
		Timestamp: time.Now(),
		Data:      details,
	}
	return am.Send(alert)
}

func (am *AlertManager) NotifyPositionBias(symbol string, biasPct, threshold float64) error {
	alert := Alert{
		Type:      AlertPositionBias,
		Severity:  SeverityWarning,
		Title:     "⚠️ POSITION BIAS EXCEEDED",
		Message:   fmt.Sprintf("Position bias %.1f%% exceeded threshold %.1f%% for %s", biasPct*100, threshold*100, symbol),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"Symbol":    symbol,
			"Bias":      fmt.Sprintf("%.1f%%", biasPct*100),
			"Threshold": fmt.Sprintf("%.1f%%", threshold*100),
		},
	}
	return am.Send(alert)
}

func (am *AlertManager) NotifyDailyLoss(currentLoss, limit float64) error {
	alert := Alert{
		Type:      AlertDailyLoss,
		Severity:  SeverityWarning,
		Title:     "⚠️ DAILY LOSS LIMIT",
		Message:   fmt.Sprintf("Daily loss %.2f%% approaching limit %.1f%%", currentLoss*100, limit*100),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"Current Loss": fmt.Sprintf("%.2f%%", currentLoss*100),
			"Limit":        fmt.Sprintf("%.1f%%", limit*100),
			"Remaining":    fmt.Sprintf("%.2f%%", (limit-currentLoss)*100),
		},
	}
	return am.Send(alert)
}

func (am *AlertManager) NotifyHighDrawdown(drawdown, limit float64) error {
	alert := Alert{
		Type:      AlertHighDrawdown,
		Severity:  SeverityWarning,
		Title:     "⚠️ HIGH DRAWDOWN",
		Message:   fmt.Sprintf("Drawdown %.2f%% approaching limit %.1f%%", drawdown*100, limit*100),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"Drawdown": fmt.Sprintf("%.2f%%", drawdown*100),
			"Limit":    fmt.Sprintf("%.1f%%", limit*100),
		},
	}
	return am.Send(alert)
}

func (am *AlertManager) NotifyStartup(symbol, mode string) error {
	alert := Alert{
		Type:      AlertStartup,
		Severity:  SeverityInfo,
		Title:     "✅ Maker Bot Started",
		Message:   fmt.Sprintf("Bot started in %s mode for %s", mode, symbol),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"Symbol": symbol,
			"Mode":   mode,
		},
	}
	return am.Send(alert)
}

func (am *AlertManager) NotifyShutdown() error {
	alert := Alert{
		Type:      AlertShutdown,
		Severity:  SeverityInfo,
		Title:     "🛑 Maker Bot Stopped",
		Message:   "Bot stopped gracefully",
		Timestamp: time.Now(),
	}
	return am.Send(alert)
}

type TelegramNotifier struct {
	botToken string
	chatID   string
	enabled  bool
	client   *http.Client
	logger   *zap.Logger
	mu       sync.RWMutex
}

func NewTelegramNotifier(logger *zap.Logger) *TelegramNotifier {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	enabled := botToken != "" && chatID != ""

	logger.Debug("Telegram config loaded",
		zap.Bool("bot_token_set", botToken != ""),
		zap.String("chat_id", chatID),
		zap.Bool("enabled", enabled))

	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		enabled:  enabled,
		client:   &http.Client{Timeout: 10 * time.Second},
		logger:   logger,
	}
}

func (t *TelegramNotifier) IsEnabled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.enabled && t.botToken != "" && t.chatID != ""
}

func (t *TelegramNotifier) Send(alert Alert) error {
	if !t.IsEnabled() {
		return nil
	}

	message := t.formatMessage(alert)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)
	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	t.logger.Debug("Sending Telegram message",
		zap.String("chat_id", t.chatID),
		zap.String("message", message[:min(len(message), 50)]+"..."))

	resp, err := t.client.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.logger.Error("Telegram API error",
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(body)))
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (t *TelegramNotifier) formatMessage(alert Alert) string {
	var emoji string
	switch alert.Severity {
	case SeverityCritical:
		emoji = "🚨"
	case SeverityWarning:
		emoji = "⚠️"
	case SeverityInfo:
		emoji = "ℹ️"
	default:
		emoji = "📢"
	}

	message := fmt.Sprintf("%s <b>%s</b>\n\n%s\n\n🕐 %s",
		emoji,
		alert.Title,
		alert.Message,
		alert.Timestamp.Format("2006-01-02 15:04:05"),
	)

	if len(alert.Data) > 0 {
		message += "\n\n📊 <b>Details:</b>"
		for key, value := range alert.Data {
			message += fmt.Sprintf("\n• %s: %v", key, value)
		}
	}

	return message
}
