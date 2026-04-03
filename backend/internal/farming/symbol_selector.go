package farming

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// SymbolSelector selects and manages trading symbols
type SymbolSelector struct {
	logger        *logrus.Entry
	futuresClient *client.FuturesClient
	wsClient      *client.WebSocketClient
	activeSymbols []*SymbolData
	mu            sync.RWMutex
	isRunning     bool
	isRunningMu   sync.RWMutex
	stopCh        chan struct{}
	wg            sync.WaitGroup
	config        *config.VolumeFarmConfig
}

// NewSymbolSelector creates a new symbol selector
func NewSymbolSelector(futuresClient *client.FuturesClient, logger *logrus.Entry, volumeConfig *config.VolumeFarmConfig) *SymbolSelector {
	// Create WebSocket client for real-time data
	zapLogger, _ := zap.NewDevelopment()
	wsClient := client.NewWebSocketClient("wss://fstream.asterdex.com/ws/!ticker@arr", zapLogger)

	return &SymbolSelector{
		futuresClient: futuresClient,
		wsClient:      wsClient,
		logger:        logger,
		activeSymbols: make([]*SymbolData, 0),
		stopCh:        make(chan struct{}),
		config:        volumeConfig,
	}
}

// Start starts the symbol selector
func (s *SymbolSelector) Start(ctx context.Context) error {
	s.isRunningMu.Lock()
	if s.isRunning {
		s.isRunningMu.Unlock()
		return fmt.Errorf("symbol selector is already running")
	}
	s.isRunning = true
	s.isRunningMu.Unlock()

	s.logger.Info("🔍 Starting Symbol Selector with WebSocket")

	// Connect to WebSocket for all ticker data
	if err := s.wsClient.Connect(ctx); err != nil {
		s.logger.WithError(err).Error("Failed to connect WebSocket")
		return fmt.Errorf("failed to connect WebSocket: %w", err)
	}

	s.logger.Info("✅ WebSocket connected for real-time ticker data")

	// Start WebSocket message processor
	s.wg.Add(1)
	go s.websocketProcessor(ctx)

	// Start periodic updates
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.periodicUpdate(ctx)
	}()

	s.logger.Info("✅ Symbol Selector started successfully")
	return nil
}

// websocketProcessor processes real-time WebSocket ticker data
func (s *SymbolSelector) websocketProcessor(ctx context.Context) {
	defer s.wg.Done()

	tickerCh := s.wsClient.GetTickerChannel()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case msg := <-tickerCh:
			s.processWebSocketTicker(msg)
		}
	}
}

// processWebSocketTicker processes real-time ticker data from WebSocket
func (s *SymbolSelector) processWebSocketTicker(msg map[string]interface{}) {
	s.logger.WithField("stream", msg["stream"]).Debug("Received WebSocket ticker message")

	// Extract data from Aster API WebSocket message format
	data, ok := msg["data"].([]interface{})
	if !ok {
		s.logger.WithField("msg", msg).Debug("WebSocket message missing data field or wrong format")
		return
	}

	s.logger.WithField("ticker_count", len(data)).Debug("Processing ticker array")

	s.mu.Lock()
	defer s.mu.Unlock()

	// Process all tickers in the array
	for _, item := range data {
		ticker, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// Extract required fields
		symbol, ok := ticker["s"].(string)
		if !ok {
			continue
		}

		// For ticker stream, volume is 'v' and count is 'c' (number of trades)
		volumeStr, ok := ticker["v"].(string)
		if !ok {
			continue
		}

		countFloat, ok := ticker["c"].(float64)
		if !ok {
			// Try as string first
			countStr, ok := ticker["c"].(string)
			if !ok {
				continue
			}
			var err error
			countFloat, err = strconv.ParseFloat(countStr, 64)
			if err != nil {
				continue
			}
		}

		// Convert strings to numbers
		volume, err := strconv.ParseFloat(volumeStr, 64)
		if err != nil {
			continue
		}

		count := int(countFloat)

		// Check volume threshold from config
		if volume >= s.config.MinVolume24h {
			symbolData := &SymbolData{
				Symbol:     symbol,
				BaseAsset:  s.extractBaseAsset(symbol),
				QuoteAsset: s.extractQuoteCurrency(symbol),
				Status:     "TRADING",
				Volume24h:  volume,
				Count24h:   count,
			}

			// Update or add symbol
			found := false
			for i, existing := range s.activeSymbols {
				if existing.Symbol == symbol {
					s.activeSymbols[i] = symbolData
					found = true
					break
				}
			}

			if !found {
				s.activeSymbols = append(s.activeSymbols, symbolData)
			}
		}
	}

	s.logger.WithField("symbols_count", len(s.activeSymbols)).Debug("Updated symbols from WebSocket")
}

// discoverSymbols initializes with empty symbols - data will come from WebSocket
func (s *SymbolSelector) discoverSymbols(ctx context.Context) error {
	s.logger.Info("Initializing symbol discovery - will populate from WebSocket data")

	// Start with empty list - WebSocket will populate symbols as they appear
	s.mu.Lock()
	s.activeSymbols = make([]*SymbolData, 0)
	s.mu.Unlock()

	return nil
}

// extractBaseAsset extracts base asset from symbol (e.g., BTC from BTCUSDT)
func (s *SymbolSelector) extractBaseAsset(symbol string) string {
	// Use configured quote currencies
	for _, quote := range s.config.SupportedQuoteCurrencies {
		if len(symbol) > len(quote) && symbol[len(symbol)-len(quote):] == quote {
			return symbol[:len(symbol)-len(quote)]
		}
	}
	return symbol // Fallback
}

// extractQuoteCurrency extracts quote currency from symbol (e.g., USDT from BTCUSDT)
func (s *SymbolSelector) extractQuoteCurrency(symbol string) string {
	// Use configured quote currencies
	for _, quote := range s.config.SupportedQuoteCurrencies {
		if len(symbol) > len(quote) && symbol[len(symbol)-len(quote):] == quote {
			return quote
		}
	}
	return "" // Fallback
}

// periodicUpdate runs periodic symbol updates using WebSocket data
func (s *SymbolSelector) periodicUpdate(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			// Log current symbol count - data comes from WebSocket
			s.mu.RLock()
			count := len(s.activeSymbols)
			s.mu.RUnlock()
			s.logger.WithField("symbols_count", count).Debug("Periodic symbol update - using WebSocket data")
		}
	}
}

// GetActiveSymbols returns the list of active symbols
func (s *SymbolSelector) GetActiveSymbols() []*SymbolData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions
	symbols := make([]*SymbolData, len(s.activeSymbols))
	copy(symbols, s.activeSymbols)
	return symbols
}

// GetActiveSymbolCount returns the number of active symbols
func (s *SymbolSelector) GetActiveSymbolCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.activeSymbols)
}

// Stop stops the symbol selector
func (s *SymbolSelector) Stop(ctx context.Context) error {
	s.isRunningMu.Lock()
	if !s.isRunning {
		s.isRunningMu.Unlock()
		return nil
	}
	s.isRunning = false
	s.isRunningMu.Unlock()

	s.logger.Info("🛑 Stopping Symbol Selector")

	close(s.stopCh)

	// Close WebSocket connection
	if s.wsClient != nil {
		if err := s.wsClient.Close(); err != nil {
			s.logger.WithError(err).Error("Error closing WebSocket connection")
		}
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("✅ Symbol Selector stopped gracefully")
		return nil
	case <-ctx.Done():
		s.logger.Warn("⚠️  Symbol Selector stop timeout")
		return ctx.Err()
	}
}

// IsRunning returns whether the symbol selector is running
func (s *SymbolSelector) IsRunning() bool {
	s.isRunningMu.RLock()
	defer s.isRunningMu.RUnlock()
	return s.isRunning
}
