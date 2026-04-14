package agentic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AlertManager interface for sending notifications
type AlertManager interface {
	SendStartup(symbol string) error
	SendFill(symbol, side string, size, price float64, pnl float64) error
	SendError(err error) error
	IsEnabled() bool
}

// TelegramAlertManager sends alerts to Telegram
type TelegramAlertManager struct {
	botToken string
	chatID   string
	enabled  bool
	client   *http.Client
	logger   *zap.Logger
	mu       sync.RWMutex
}

// NewTelegramAlertManager creates a new Telegram alert manager from env vars
func NewTelegramAlertManager(logger *zap.Logger) *TelegramAlertManager {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	enabled := botToken != "" && chatID != ""

	if enabled {
		logger.Info("Telegram alerts enabled",
			zap.String("chat_id", chatID))
	} else {
		logger.Warn("Telegram alerts disabled - set TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID")
	}

	return &TelegramAlertManager{
		botToken: botToken,
		chatID:   chatID,
		enabled:  enabled,
		client:   &http.Client{Timeout: 10 * time.Second},
		logger:   logger,
	}
}

// IsEnabled returns whether Telegram is configured
func (t *TelegramAlertManager) IsEnabled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.enabled && t.botToken != "" && t.chatID != ""
}

// sendMessage sends a message to Telegram
func (t *TelegramAlertManager) sendMessage(message string) error {
	if !t.IsEnabled() {
		return nil
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)
	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := t.client.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

// SendStartup sends startup notification
func (t *TelegramAlertManager) SendStartup(symbol string) error {
	if !t.IsEnabled() {
		return nil
	}

	message := fmt.Sprintf(
		"🚀 <b>Agentic Bot Started</b>\n\n"+
		"Symbol: <code>%s</code>\n"+
		"Time: %s\n"+
		"Status: ✅ Running",
		symbol,
		time.Now().Format("2006-01-02 15:04:05"),
	)

	return t.sendMessage(message)
}

// SendFill sends order fill notification
func (t *TelegramAlertManager) SendFill(symbol, side string, size, price float64, pnl float64) error {
	if !t.IsEnabled() {
		return nil
	}

	emoji := "🟢"
	if side == "SELL" {
		emoji = "🔴"
	}

	pnlText := ""
	if pnl != 0 {
		pnlEmoji := "🟢"
		if pnl < 0 {
			pnlEmoji = "🔴"
		}
		pnlText = fmt.Sprintf("P&L: %s %.4f\n", pnlEmoji, pnl)
	}

	message := fmt.Sprintf(
		"%s <b>Order Filled</b>\n\n"+
		"Symbol: <code>%s</code>\n"+
		"Side: %s\n"+
		"Size: %.4f\n"+
		"Price: %.2f\n"+
		"%s"+
		"Time: %s",
		emoji,
		symbol,
		side,
		size,
		price,
		pnlText,
		time.Now().Format("15:04:05"),
	)

	return t.sendMessage(message)
}

// SendError sends error notification
func (t *TelegramAlertManager) SendError(err error) error {
	if !t.IsEnabled() || err == nil {
		return nil
	}

	message := fmt.Sprintf(
		"⚠️ <b>Error Alert</b>\n\n"+
		"<code>%s</code>\n\n"+
		"Time: %s",
		err.Error(),
		time.Now().Format("15:04:05"),
	)

	return t.sendMessage(message)
}
