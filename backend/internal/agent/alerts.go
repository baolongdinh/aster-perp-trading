package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// AlertType represents different types of alerts
type AlertType string

const (
	AlertRegimeChange   AlertType = "regime_change"
	AlertCircuitBreaker AlertType = "circuit_breaker"
	AlertHighDrawdown   AlertType = "high_drawdown"
	AlertInfo           AlertType = "info"
)

// Alert represents a single alert message
type Alert struct {
	Type      AlertType
	Title     string
	Message   string
	Timestamp time.Time
	Severity  string // critical, warning, info
	Data      map[string]interface{}
}

// Notifier is the interface for sending alerts
type Notifier interface {
	Send(alert Alert) error
	IsEnabled() bool
}

// AlertManager coordinates alert generation and sending
type AlertManager struct {
	notifier    Notifier
	config      AlertingConfig
	lastRegime  RegimeType
	initialized bool
}

// NewAlertManager creates a new alert manager
func NewAlertManager(config AlertingConfig, notifier Notifier) *AlertManager {
	return &AlertManager{
		notifier:    notifier,
		config:      config,
		lastRegime:  "", // Empty means not initialized
		initialized: config.Enabled,
	}
}

// IsEnabled returns if alerting is enabled
func (am *AlertManager) IsEnabled() bool {
	return am.initialized && am.notifier != nil && am.notifier.IsEnabled()
}

// NotifyRegimeChange sends alert when regime changes
func (am *AlertManager) NotifyRegimeChange(newRegime RegimeSnapshot) error {
	if !am.IsEnabled() || !am.config.OnRegimeChange {
		return nil
	}

	// Skip if this is the first detection
	if am.lastRegime == "" {
		am.lastRegime = newRegime.Regime
		return nil
	}

	// Check if regime actually changed
	if am.lastRegime == newRegime.Regime {
		return nil
	}

	alert := Alert{
		Type:      AlertRegimeChange,
		Title:     "Chuyển Đổi Chế Độ Thị Trường",
		Severity:  "warning",
		Message:   fmt.Sprintf("Thị trường chuyển từ *%s* sang *%s*", am.lastRegime, newRegime.Regime),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"Regime cũ":  am.lastRegime,
			"Regime mới": newRegime.Regime,
			"Confidence": fmt.Sprintf("%.1f%%", newRegime.Confidence),
			"ADX":        fmt.Sprintf("%.2f", newRegime.Indicators.ADX),
			"BB Width":   fmt.Sprintf("%.2f%%", newRegime.Indicators.BBWidth*100),
			"ATR":        fmt.Sprintf("%.2f", newRegime.Indicators.ATR14),
		},
	}

	// Update last regime
	am.lastRegime = newRegime.Regime

	return am.notifier.Send(alert)
}

// NotifyCircuitBreaker sends alert when circuit breaker triggers
func (am *AlertManager) NotifyCircuitBreaker(breakerName string, action string, details map[string]interface{}) error {
	if !am.IsEnabled() || !am.config.OnCircuitBreaker {
		return nil
	}

	alert := Alert{
		Type:      AlertCircuitBreaker,
		Title:     "🚨 Circuit Breaker Kích Hoạt",
		Severity:  "critical",
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("*Cầu chì %s* đã kích hoạt\n\nHành động: %s", breakerName, action),
		Data:      details,
	}

	return am.notifier.Send(alert)
}

// NotifyHighDrawdown sends early warning when drawdown approaches limit
func (am *AlertManager) NotifyHighDrawdown(currentDrawdown, limit float64) error {
	if !am.IsEnabled() || !am.config.OnHighDrawdown {
		return nil
	}

	alert := Alert{
		Type:      AlertHighDrawdown,
		Title:     "⚠️ Cảnh Báo Drawdown Cao",
		Severity:  "warning",
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Drawdown hiện tại *%.2f%%* đang tiến gần ngưỡng cầu chì (%.1f%%)", currentDrawdown*100, limit*100),
		Data: map[string]interface{}{
			"Drawdown hiện tại": fmt.Sprintf("%.2f%%", currentDrawdown*100),
			"Ngưỡng cầu chì":    fmt.Sprintf("%.1f%%", limit*100),
			"Còn cách":          fmt.Sprintf("%.2f%%", (limit-currentDrawdown)*100),
		},
	}

	return am.notifier.Send(alert)
}

// NotifyInfo sends informational alert
func (am *AlertManager) NotifyInfo(title, message string, data map[string]interface{}) error {
	if !am.IsEnabled() {
		return nil
	}

	alert := Alert{
		Type:      AlertInfo,
		Title:     title,
		Severity:  "info",
		Timestamp: time.Now(),
		Message:   message,
		Data:      data,
	}

	return am.notifier.Send(alert)
}

// NotifyStartup sends startup notification
func (am *AlertManager) NotifyStartup(symbol string, testMode bool) error {
	mode := "LIVE"
	if testMode {
		mode = "TEST"
	}

	return am.NotifyInfo(
		"✅ Agentic Trading Bot Khởi Động",
		fmt.Sprintf("Bot đã khởi động ở chế độ *%s* cho cặp *%s*", mode, symbol),
		map[string]interface{}{
			"Symbol": symbol,
			"Mode":   mode,
		},
	)
}

// NotifyShutdown sends shutdown notification
func (am *AlertManager) NotifyShutdown() error {
	return am.NotifyInfo(
		"🛑 Bot Dừng Hoạt Động",
		"Agentic Trading Bot đã dừng hoạt động an toàn.",
		nil,
	)
}

// TestNotification sends a test alert to verify Telegram configuration
func (am *AlertManager) TestNotification() error {
	if !am.IsEnabled() {
		return fmt.Errorf("alerting is not enabled")
	}

	alert := Alert{
		Type:      AlertInfo,
		Title:     "🧪 Test Alert",
		Severity:  "info",
		Timestamp: time.Now(),
		Message:   "Đây là tin nhắn kiểm tra từ Agentic Trading Bot.\n\nNếu bạn nhận được tin nhắn này, cấu hình Telegram đã đúng!",
		Data: map[string]interface{}{
			"Status":    "✅ Working",
			"Bot Token": "TELEGRAM_BOT_TOKEN",
			"Chat ID":   "TELEGRAM_CHAT_ID",
		},
	}

	return am.notifier.Send(alert)
}

// TelegramNotifier sends alerts to Telegram
type TelegramNotifier struct {
	botToken string
	chatID   string
	enabled  bool
	client   *http.Client
	mu       sync.RWMutex
	lastSent map[AlertType]time.Time // Rate limiting per alert type
	window   time.Duration           // Rate limit window
}

// NewTelegramNotifier creates a new Telegram notifier from env vars
func NewTelegramNotifier(window time.Duration) *TelegramNotifier {
	if window == 0 {
		window = 5 * time.Minute
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	enabled := os.Getenv("TELEGRAM_ENABLED") != "false" // Default enabled if token exists

	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		enabled:  enabled && botToken != "" && chatID != "",
		client:   &http.Client{Timeout: 10 * time.Second},
		lastSent: make(map[AlertType]time.Time),
		window:   window,
	}
}

// IsEnabled returns whether the notifier is enabled
func (t *TelegramNotifier) IsEnabled() bool {
	return t.enabled && t.botToken != "" && t.chatID != ""
}

// canSend checks rate limiting
func (t *TelegramNotifier) canSend(alertType AlertType) bool {
	t.mu.RLock()
	last, exists := t.lastSent[alertType]
	t.mu.RUnlock()

	if !exists {
		return true
	}
	return time.Since(last) >= t.window
}

// recordSent records the time an alert was sent
func (t *TelegramNotifier) recordSent(alertType AlertType) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastSent[alertType] = time.Now()
}

// Send sends an alert to Telegram with rate limiting
func (t *TelegramNotifier) Send(alert Alert) error {
	if !t.IsEnabled() {
		return nil
	}

	if !t.canSend(alert.Type) {
		return fmt.Errorf("rate limited: %s alert sent recently", alert.Type)
	}

	message := t.formatMessage(alert)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)
	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := t.client.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	t.recordSent(alert.Type)
	return nil
}

// formatMessage formats alert for Telegram with emoji
func (t *TelegramNotifier) formatMessage(alert Alert) string {
	var emoji string
	switch alert.Severity {
	case "critical":
		emoji = "🚨"
	case "warning":
		emoji = "⚠️"
	case "success":
		emoji = "✅"
	default:
		emoji = "ℹ️"
	}

	message := fmt.Sprintf("%s *%s*\n\n%s\n\n🕐 *Thời gian:* %s",
		emoji,
		alert.Title,
		alert.Message,
		alert.Timestamp.Format("2006-01-02 15:04:05"),
	)

	if len(alert.Data) > 0 {
		message += "\n\n📊 *Chi tiết:*"
		for key, value := range alert.Data {
			message += fmt.Sprintf("\n• %s: %v", key, value)
		}
	}

	return message
}
