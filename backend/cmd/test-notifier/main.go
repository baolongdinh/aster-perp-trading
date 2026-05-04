package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"aster-bot/internal/notifier"

	"github.com/joho/godotenv"
)

// mockProvider implements notifier.MetricsProvider for testing
type mockProvider struct{}

func (m *mockProvider) GetCurrentMetrics() notifier.GridMetrics {
	return notifier.GridMetrics{
		Symbol:          "BTCUSDT",
		CurrentPrice:    99850.0000,
		RealizedPnL:     2.3400,
		UnrealizedPnL:   0.8700,
		FeesPaid:        1.2300,
		NetPnL:          1.1100,
		Volume30m:       15420.50,
		FilledOrders30m: 87,
		PendingOrders:   18,
		GridMinPrice:    95000.0,
		GridMaxPrice:    105000.0,
		ActiveGrids:     18,
		TotalGrids:      20,
		LastOrderTime:   time.Now().Add(-18 * time.Second), // 0.3 minutes ago
		InitialCapital:  1000.0,
		CurrentCapital:  1001.11,
		ROI:             0.11,
		DrawdownPct:     0.05,
		Uptime:          2*time.Hour + 30*time.Minute,
	}
}

func main() {
	fmt.Println("Loading .env...")
	if err := godotenv.Load("../../.env"); err != nil {
		log.Println("Note: .env file not found or couldn't load:", err)
	}

	// Read directly just in case it runs from backend root
	if os.Getenv("TELEGRAM_BOT_TOKEN") == "" {
		_ = godotenv.Load(".env")
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")

	if botToken == "" || chatID == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID is missing")
	}

	cfg := notifier.Config{
		BotToken:    botToken,
		ChatID:      chatID,
		AlertConfig: notifier.DefaultAlertConfig,
	}

	noti := notifier.NewNotifier(cfg, &mockProvider{})

	fmt.Printf("Sending Test Startup Message to ChatID: %s\n", chatID)
	noti.SendStartup(context.Background(), "BTCUSDT", 95000.0, 105000.0, 20)

	time.Sleep(1 * time.Second)

	fmt.Println("Fetching formatted Periodic metrics & sending...")
	msg, err := notifier.FormatPeriodicReport((&mockProvider{}).GetCurrentMetrics())
	if err != nil {
		log.Fatal("Format failed:", err)
	}

	// Use internal client to just fire the msg
	client := notifier.NewTelegramClient(botToken, chatID)
	err = client.SendMessage(context.Background(), msg)
	if err != nil {
		log.Fatal("Failed to send message: ", err)
	}

	fmt.Println("Successfully sent Telegram notification! Please check your phone/app.")
}
