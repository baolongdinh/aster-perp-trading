package main

import (
	"aster-bot/internal/auth"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	// Optional: use custom API credentials from command line
	userWalletFlag := flag.String("userWallet", "", "User wallet address (V3 auth)")
	apiSignerFlag := flag.String("apiSigner", "", "API signer address (V3 auth)")
	apiSignerKeyFlag := flag.String("apiSignerKey", "", "API signer private key (V3 auth)")
	listenKey := flag.String("listenKey", "", "Asterdex listen key (optional - will auto-generate if not provided)")
	wsBaseFlag := flag.String("wsBase", "", "WebSocket base URL (optional - will load from config if not provided)")
	restBaseFlag := flag.String("restBase", "", "REST API base URL (optional - will load from config if not provided)")
	flag.Parse()

	// Load V3 credentials from environment or command line
	userWallet := os.Getenv("ASTER_USER_WALLET")
	apiSigner := os.Getenv("ASTER_API_SIGNER")
	apiSignerKey := os.Getenv("ASTER_API_SIGNER_KEY")

	if *userWalletFlag != "" {
		userWallet = *userWalletFlag
	}
	if *apiSignerFlag != "" {
		apiSigner = *apiSignerFlag
	}
	if *apiSignerKeyFlag != "" {
		apiSignerKey = *apiSignerKeyFlag
	}

	if userWallet == "" || apiSigner == "" || apiSignerKey == "" {
		log.Fatal("V3 credentials required: ASTER_USER_WALLET, ASTER_API_SIGNER, ASTER_API_SIGNER_KEY")
		log.Println("Usage:")
		log.Println("  export ASTER_USER_WALLET=0x...")
		log.Println("  export ASTER_API_SIGNER=0x...")
		log.Println("  export ASTER_API_SIGNER_KEY=...")
		log.Println("  ./debug_userstream")
		log.Println("Or:")
		log.Println("  ./debug_userstream -userWallet=0x... -apiSigner=0x... -apiSignerKey=...")
	}

	// Load endpoints from config or use defaults
	restBase := *restBaseFlag
	wsBase := *wsBaseFlag
	if restBase == "" {
		restBase = "https://fapi.asterdex.com"
	}
	if wsBase == "" {
		wsBase = "wss://fstream.asterdex.com"
	}

	log.Println("=== Asterdex UserStream Debug Test (V3 Auth) ===")
	log.Printf("REST Base: %s", restBase)
	log.Printf("WebSocket Base: %s", wsBase)
	log.Printf("User Wallet: %s", userWallet)
	log.Printf("API Signer: %s", apiSigner)

	// Get listenKey (either from flag or auto-generate)
	if *listenKey == "" {
		log.Println("\n[Step 1] Getting listenKey from Asterdex API (V3 Auth)...")
		var err error
		*listenKey, err = createListenKeyV3(restBase, userWallet, apiSigner, apiSignerKey)
		if err != nil {
			log.Fatalf("Failed to create listenKey: %v", err)
		}
		log.Printf("✓ ListenKey obtained: %s", *listenKey)
	} else {
		log.Printf("\nUsing provided listenKey: %s", *listenKey)
	}

	// Connect WebSocket
	log.Println("\nConnecting to WebSocket...")
	wsURL := fmt.Sprintf("%s/ws/%s", wsBase, *listenKey)
	log.Printf("WebSocket URL: %s", wsURL)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()
	log.Println("✓ WebSocket connected successfully")

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	// Listen for messages
	log.Println("\nListening for messages (press Ctrl+C to stop)...")
	log.Println("Waiting for account/order updates from exchange...")

	messageCount := 0
	accountUpdateCount := 0
	orderUpdateCount := 0
	unknownCount := 0

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()

loop:
	for {
		select {
		case <-interrupt:
			log.Println("\n⚠️ Interrupted by user")
			break loop
		case <-heartbeat.C:
			log.Printf("💓 Heartbeat: Total messages=%d, Account=%d, Order=%d, Unknown=%d",
				messageCount, accountUpdateCount, orderUpdateCount, unknownCount)
		default:
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("❌ Read error: %v", err)
				break loop
			}

			// Reset deadline on each message
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))

			messageCount++
			log.Printf("\n📨 Message #%d received (size: %d bytes)", messageCount, len(msg))

			// Parse message type
			var peek struct {
				EventType string `json:"e"`
			}
			if err := json.Unmarshal(msg, &peek); err != nil {
				log.Printf("  ❌ Failed to parse event type: %v", err)
				log.Printf("  Raw: %s", string(msg[:min(200, len(msg))]))
				unknownCount++
				continue
			}

			log.Printf("  Event type: %s", peek.EventType)

			// Parse full message based on type
			switch peek.EventType {
			case "ACCOUNT_UPDATE":
				accountUpdateCount++
				var msgData map[string]interface{}
				if err := json.Unmarshal(msg, &msgData); err != nil {
					log.Printf("  ❌ Failed to unmarshal ACCOUNT_UPDATE: %v", err)
					continue
				}

				log.Printf("  ✅ ACCOUNT_UPDATE parsed successfully")

				// Print account data
				if account, ok := msgData["a"].(map[string]interface{}); ok {
					log.Printf("  Event reason: %v", account["m"])

					// Balances
					if balances, ok := account["B"].([]interface{}); ok {
						log.Printf("  Balances: %d", len(balances))
						for i, bal := range balances {
							b := bal.(map[string]interface{})
							log.Printf("    [%d] Asset: %s, Wallet: %v, Cross: %v, Change: %v",
								i, b["a"], b["wb"], b["cw"], b["bc"])
						}
					}

					// Positions
					if positions, ok := account["P"].([]interface{}); ok {
						log.Printf("  Positions: %d", len(positions))
						for i, pos := range positions {
							p := pos.(map[string]interface{})
							log.Printf("    [%d] Symbol: %s, Amt: %v, Entry: %v, PnL: %v, Side: %v",
								i, p["s"], p["pa"], p["ep"], p["up"], p["ps"])
						}
					}
				}

			case "ORDER_TRADE_UPDATE":
				orderUpdateCount++
				var msgData map[string]interface{}
				if err := json.Unmarshal(msg, &msgData); err != nil {
					log.Printf("  ❌ Failed to unmarshal ORDER_TRADE_UPDATE: %v", err)
					continue
				}

				log.Printf("  ✅ ORDER_TRADE_UPDATE parsed successfully")

				// Print order data
				if order, ok := msgData["o"].(map[string]interface{}); ok {
					log.Printf("  Symbol: %v", order["s"])
					log.Printf("  Side: %v", order["S"])
					log.Printf("  Type: %v", order["o"])
					log.Printf("  Status: %v", order["X"])
					log.Printf("  OrderID: %v", order["i"])
					log.Printf("  Quantity: %v", order["q"])
					log.Printf("  Price: %v", order["p"])
					log.Printf("  Filled: %v", order["z"])
				}

			case "listenKeyExpired":
				log.Printf("  ⚠️ Listen key expired!")
				log.Println("  Please get a new listenKey and restart this test")
				break loop

			default:
				unknownCount++
				log.Printf("  ❓ Unknown event type")
				log.Printf("  Raw: %s", string(msg[:min(200, len(msg))]))
			}
		}
	}

	// Summary
	log.Println("\n=== Test Summary ===")
	log.Printf("Total messages: %d", messageCount)
	log.Printf("Account updates: %d", accountUpdateCount)
	log.Printf("Order updates: %d", orderUpdateCount)
	log.Printf("Unknown messages: %d", unknownCount)

	if messageCount == 0 {
		log.Println("\n❌ TEST FAILED - No messages received")
		log.Println("\nPossible issues:")
		log.Println("  1. No active positions/orders on exchange")
		log.Println("  2. ListenKey expired or invalid")
		log.Println("  3. WebSocket endpoint is incorrect")
		log.Println("  4. Network/firewall blocking connection")
		log.Println("  5. Asterdex not sending updates for this account")
		os.Exit(1)
	} else if accountUpdateCount == 0 && orderUpdateCount == 0 {
		log.Println("\n⚠️  TEST WARNING - Messages received but no account/order updates")
		log.Println("  This might be normal if you have no active positions/orders")
		log.Println("  Try placing an order or opening a position on the exchange")
	} else {
		log.Println("\n✅ TEST PASSED - UserStream is working!")
	}
}

func createListenKeyV3(restBase, userWallet, apiSigner, apiSignerKey string) (string, error) {
	// Create V3 signer
	signer, err := auth.NewV3Signer(userWallet, apiSigner, apiSignerKey, 5000)
	if err != nil {
		return "", fmt.Errorf("failed to create V3 signer: %w", err)
	}

	// Sync time with server
	log.Println("Syncing time with server...")
	offset, err := syncServerTime(restBase)
	if err != nil {
		log.Printf("Warning: failed to sync server time: %v", err)
	} else {
		signer.SetTimeOffset(offset)
		log.Printf("Server time synced, offset: %dms", offset)
	}

	// Sign the request
	params := make(map[string]string)
	signedParams, err := signer.SignRequest(params)
	if err != nil {
		return "", fmt.Errorf("failed to sign request: %w", err)
	}

	// Build form-encoded body (sorted keys, URL-encoded)
	keys := make([]string, 0, len(signedParams))
	for k := range signedParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var bodyParts []string
	for _, k := range keys {
		bodyParts = append(bodyParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(signedParams[k])))
	}
	body := strings.Join(bodyParts, "&")

	// Create request
	req, err := http.NewRequest("POST", restBase+"/fapi/v3/listenKey", strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned error: %d - %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result struct {
		ListenKey string `json:"listenKey"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.ListenKey == "" {
		return "", fmt.Errorf("listenKey not found in response: %s", string(respBody))
	}

	return result.ListenKey, nil
}

func syncServerTime(restBase string) (int64, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(restBase + "/fapi/v3/time")
	if err != nil {
		return 0, fmt.Errorf("failed to get server time: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	var timeResp struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(body, &timeResp); err != nil {
		return 0, fmt.Errorf("failed to parse time response: %w", err)
	}

	localTime := time.Now().UnixMilli()
	offset := timeResp.ServerTime - localTime
	return offset, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
