package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// =============================================================================
// WEBSOCKET CLIENT - BOTTLENECK DETECTION TESTS
// =============================================================================

// TestWebSocketClient_ConnectionBottleneck tests connection establishment speed
func TestWebSocketClient_ConnectionBottleneck(t *testing.T) {
	logger := zap.NewNop()

	// Create test WebSocket server
	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade: %v", err)
		}
		defer conn.Close()

		// Echo back any messages
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.WriteMessage(msgType, msg)
		}
	}))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Test connection time
	start := time.Now()
	client := NewWebSocketClient(wsURL, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	connectTime := time.Since(start)

	require.NoError(t, err, "WebSocket connection should succeed")
	assert.Less(t, connectTime, 2*time.Second, "Connection should establish within 2 seconds")
	t.Logf("WebSocket connection established in %v", connectTime)

	client.Close()
}

// TestWebSocketClient_MessageThroughput tests message handling capacity
func TestWebSocketClient_MessageThroughput(t *testing.T) {
	logger := zap.NewNop()
	receivedCount := 0
	var mu sync.Mutex

	// Create test server that sends ticker messages rapidly
	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade: %v", err)
		}
		defer conn.Close()

		// Wait for subscribe message
		_, _, _ = conn.ReadMessage()

		// Send 1000 ticker messages rapidly (array format like !ticker@arr)
		for i := 0; i < 1000; i++ {
			ticker := []interface{}{
				map[string]interface{}{
					"s": "BTCUSD1",
					"c": "50000.00",
					"v": "1000.00",
				},
			}
			data, _ := json.Marshal(ticker)
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewWebSocketClient(wsURL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.NoError(t, err)

	// Subscribe first
	client.SubscribeToTicker([]string{"!ticker@arr"})

	// Get ticker channel and consume messages
	tickerCh := client.GetTickerChannel()
	done := make(chan bool)
	go func() {
		timeout := time.After(3 * time.Second)
		for {
			select {
			case <-tickerCh:
				mu.Lock()
				receivedCount++
				mu.Unlock()
			case <-timeout:
				done <- true
				return
			}
		}
	}()

	<-done

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	// Should receive most messages (allowing for some buffer overflow)
	assert.Greater(t, count, 500, "Should receive at least 500 out of 1000 messages")
	t.Logf("Received %d/1000 messages (%.1f%%)", count, float64(count)/10)

	client.Close()
}

// TestWebSocketClient_BufferOverflowDetection tests buffer capacity under load
func TestWebSocketClient_BufferOverflowDetection(t *testing.T) {
	logger := zap.NewNop()
	receivedMessages := 0
	var mu sync.Mutex

	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade: %v", err)
		}
		defer conn.Close()

		// Wait for subscribe
		_, _, _ = conn.ReadMessage()

		// Send messages faster than consumer can process (array format)
		ticker := []interface{}{
			map[string]interface{}{
				"s": "BTCUSD1",
				"c": "50000.00",
			},
		}
		data, _ := json.Marshal(ticker)

		// Send at high rate for 3 seconds
		start := time.Now()
		for time.Since(start) < 3*time.Second {
			conn.WriteMessage(websocket.TextMessage, data)
			time.Sleep(time.Millisecond) // 1000 msg/sec
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewWebSocketClient(wsURL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client.Connect(ctx)
	client.SubscribeToTicker([]string{"!ticker@arr"})

	// Slow consumer to test buffer overflow
	tickerCh := client.GetTickerChannel()
	timeout := time.After(5 * time.Second)
	slowConsumer := time.NewTicker(100 * time.Millisecond)
	defer slowConsumer.Stop()

consumerLoop:
	for {
		select {
		case <-tickerCh:
			mu.Lock()
			receivedMessages++
			mu.Unlock()
		case <-slowConsumer.C:
			// Simulate slow processing
		case <-timeout:
			break consumerLoop
		}
	}

	mu.Lock()
	received := receivedMessages
	mu.Unlock()

	// With 2000 buffer and slow consumer, some messages should be dropped
	t.Logf("Buffer overflow test: received %d messages", received)

	client.Close()
}

// TestWebSocketClient_ArrayMessageProcessing tests !ticker@arr format
func TestWebSocketClient_ArrayMessageProcessing(t *testing.T) {
	logger := zap.NewNop()
	receivedMessages := make([]map[string]interface{}, 0)
	var mu sync.Mutex

	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade: %v", err)
		}
		defer conn.Close()

		// Wait for subscribe
		_, _, _ = conn.ReadMessage()

		// Send array format like !ticker@arr (array of ticker objects)
		tickers := []interface{}{
			map[string]interface{}{"s": "BTCUSD1", "c": "50000.00"},
			map[string]interface{}{"s": "ETHUSD1", "c": "3000.00"},
			map[string]interface{}{"s": "SOLUSD1", "c": "100.00"},
		}
		data, _ := json.Marshal(tickers)
		conn.WriteMessage(websocket.TextMessage, data)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewWebSocketClient(wsURL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client.Connect(ctx)
	client.SubscribeToTicker([]string{"!ticker@arr"})

	// Get ticker channel
	tickerCh := client.GetTickerChannel()
	timeout := time.After(1 * time.Second)

	for {
		select {
		case msg := <-tickerCh:
			mu.Lock()
			receivedMessages = append(receivedMessages, msg)
			mu.Unlock()
		case <-timeout:
			goto done
		}
	}
done:

	mu.Lock()
	count := len(receivedMessages)
	mu.Unlock()

	// Should receive 1 message containing array data (wrapped by processArrayMessage)
	assert.GreaterOrEqual(t, count, 1, "Should receive at least 1 message from array")

	// Verify the message contains the array data
	if count > 0 {
		msg := receivedMessages[0]
		if data, ok := msg["data"].([]interface{}); ok {
			assert.Equal(t, 3, len(data), "Should contain 3 ticker items")
		} else {
			t.Error("Message should have 'data' field containing array")
		}
	}
	t.Logf("Received %d messages from array format", count)

	client.Close()
}

// TestWebSocketClient_SubscriptionLatency measures subscription request time
func TestWebSocketClient_SubscriptionLatency(t *testing.T) {
	logger := zap.NewNop()
	subscriptionReceived := make(chan bool, 1)

	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade: %v", err)
		}
		defer conn.Close()

		// Wait for subscription message
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// Verify it's a subscribe message
		var data map[string]interface{}
		json.Unmarshal(msg, &data)
		if method, ok := data["method"]; ok && method == "SUBSCRIBE" {
			close(subscriptionReceived)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewWebSocketClient(wsURL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.NoError(t, err)

	// Measure subscription time
	start := time.Now()
	client.SubscribeToTicker([]string{"!ticker@arr"})

	select {
	case <-subscriptionReceived:
		latency := time.Since(start)
		assert.Less(t, latency, 500*time.Millisecond, "Subscription should complete within 500ms")
		t.Logf("Subscription completed in %v", latency)
	case <-time.After(2 * time.Second):
		t.Fatal("Subscription timeout")
	}

	client.Close()
}

// TestWebSocketClient_ConnectionStability tests long-running connection
func TestWebSocketClient_ConnectionStability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running stability test in short mode")
	}

	logger := zap.NewNop()
	messageCount := 0
	var mu sync.Mutex

	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade: %v", err)
		}
		defer conn.Close()

		// Wait for subscribe
		_, _, _ = conn.ReadMessage()

		// Send periodic messages for 10 seconds
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		start := time.Now()
		for time.Since(start) < 10*time.Second {
			<-ticker.C
			ticker := []interface{}{
				map[string]interface{}{
					"s": "BTCUSD1",
					"c": "50000.00",
				},
			}
			data, _ := json.Marshal(ticker)
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewWebSocketClient(wsURL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client.Connect(ctx)
	client.SubscribeToTicker([]string{"!ticker@arr"})

	// Get ticker channel
	tickerCh := client.GetTickerChannel()
	timeout := time.After(12 * time.Second)

	for {
		select {
		case <-tickerCh:
			mu.Lock()
			messageCount++
			mu.Unlock()
		case <-timeout:
			goto done
		}
	}
done:

	mu.Lock()
	count := messageCount
	mu.Unlock()

	// Should receive approximately 100 messages (one every 100ms for 10s)
	assert.InDelta(t, 100, count, 20, "Should receive ~100 messages over 10 seconds")
	t.Logf("Received %d messages in 10 seconds", count)

	client.Close()
}

// =============================================================================
// FULL FLOW INTEGRATION TEST
// =============================================================================

// TestFullFlow_WebSocketToGridPlacement tests complete flow from WS to grid placement
func TestFullFlow_WebSocketToGridPlacement(t *testing.T) {
	logger := zap.NewNop()

	// Mock server that simulates exchange WebSocket
	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade: %v", err)
		}
		defer conn.Close()

		// Wait for subscribe
		_, _, _ = conn.ReadMessage()

		// Send ticker updates every 100ms
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for i := 0; i < 50; i++ {
			<-ticker.C
			price := 50000.0 + float64(i)
			ticker := []interface{}{
				map[string]interface{}{
					"s": "BTCUSD1",
					"c": fmt.Sprintf("%.2f", price),
				},
			}
			data, _ := json.Marshal(ticker)
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewWebSocketClient(wsURL, logger)

	// Track received prices
	prices := make([]float64, 0)
	var mu sync.Mutex

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client.Connect(ctx)
	client.SubscribeToTicker([]string{"!ticker@arr"})

	// Get ticker channel and consume
	tickerCh := client.GetTickerChannel()
	timeout := time.After(8 * time.Second)

	for {
		select {
		case msg := <-tickerCh:
			// Message is already wrapped by processArrayMessage with "data" field
			if data, ok := msg["data"].([]interface{}); ok {
				for _, item := range data {
					if ticker, ok := item.(map[string]interface{}); ok {
						if priceStr, ok := ticker["c"].(string); ok {
							var price float64
							fmt.Sscanf(priceStr, "%f", &price)
							mu.Lock()
							prices = append(prices, price)
							mu.Unlock()
						}
					}
				}
			}
		case <-timeout:
			goto done
		}
	}
done:

	mu.Lock()
	receivedCount := len(prices)
	mu.Unlock()

	// Verify we received most messages
	assert.Greater(t, receivedCount, 40, "Should receive at least 40 out of 50 price updates")
	t.Logf("Full flow test: Received %d price updates", receivedCount)

	client.Close()
}
