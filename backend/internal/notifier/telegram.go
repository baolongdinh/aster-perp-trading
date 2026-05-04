package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TelegramClient handles direct communication with the Telegram Bot API
type TelegramClient struct {
	botToken string
	chatID   string
	client   *http.Client
}

// NewTelegramClient creates a new HTTP client wrapper for Telegram
func NewTelegramClient(botToken, chatID string) *TelegramClient {
	return &TelegramClient{
		botToken: botToken,
		chatID:   chatID,
		client: &http.Client{
			Timeout: 10 * time.Second, // Reasonable timeout for Telegram API
		},
	}
}

// sendMessagePayload is the JSON structure for Telegram sendMessage endpoint
type sendMessagePayload struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// SendMessage sends an HTML or plain text message to the configured channel/user.
// Using HTML parse mode by default for formatting flexibility.
func (t *TelegramClient) SendMessage(ctx context.Context, message string) error {
	if t.botToken == "" || t.chatID == "" {
		return fmt.Errorf("telegram bot token or chat ID is empty")
	}

	payload := sendMessagePayload{
		ChatID:    t.chatID,
		Text:      message,
		ParseMode: "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode telegram payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		if desc, ok := errResp["description"].(string); ok {
			return fmt.Errorf("telegram API error (%s): %s", resp.Status, desc)
		}
		return fmt.Errorf("telegram API returned status: %s", resp.Status)
	}

	return nil
}
