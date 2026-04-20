package farming

import (
	"context"
	"fmt"
	"math"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aster-bot/internal/client"
	"aster-bot/internal/config"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// SymbolSelector selects and manages trading symbols.
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

// SymbolMetrics captures derived selection metrics used by tests and ranking.
type SymbolMetrics struct {
	Volume24h     float64
	CurrentSpread float64
}

// NewSymbolSelector creates a new symbol selector.
func NewSymbolSelector(futuresClient *client.FuturesClient, logger *logrus.Entry, volumeConfig *config.VolumeFarmConfig) *SymbolSelector {
	zapLogger, _ := zap.NewDevelopment()
	normalized := normalizeVolumeConfig(volumeConfig)
	wsURL := buildTickerStreamURL(normalized.Exchange.FuturesWSBase, normalized.TickerStream)
	if wsURL == "" {
		logger.Error("Failed to build WebSocket URL - using default")
		wsURL = "wss://fstream.asterdex.com/ws/!ticker@arr"
	}
	wsClient := client.NewWebSocketClient(wsURL, zapLogger)

	return &SymbolSelector{
		futuresClient: futuresClient,
		wsClient:      wsClient,
		logger:        logger,
		activeSymbols: make([]*SymbolData, 0),
		stopCh:        make(chan struct{}),
		config:        normalized,
	}
}

// Start starts the symbol selector.
func (s *SymbolSelector) Start(ctx context.Context) error {
	s.isRunningMu.Lock()
	if s.isRunning {
		s.isRunningMu.Unlock()
		return fmt.Errorf("symbol selector is already running")
	}
	s.isRunning = true
	s.isRunningMu.Unlock()

	s.logger.Info("Starting Symbol Selector with WebSocket")

	s.logger.WithField("supported_quotes", s.config.Symbols.QuoteCurrencies).Info("Supported quote currencies")

	// Only connect if not already connected (e.g., when using shared WebSocket client)
	if !s.wsClient.IsRunning() {
		if err := s.wsClient.Connect(ctx); err != nil {
			s.logger.WithError(err).Error("Failed to connect WebSocket")
			return fmt.Errorf("failed to connect WebSocket: %w", err)
		}

		// Subscribe to all ticker array stream
		if err := s.wsClient.SubscribeToTicker([]string{"!ticker@arr"}); err != nil {
			s.logger.WithError(err).Warn("Failed to subscribe to ticker stream")
		}
	}

	s.logger.Info("WebSocket connected for real-time ticker data")

	s.wg.Add(1)
	go s.websocketProcessor(ctx)

	s.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("Periodic update goroutine panic recovered",
					zap.Any("panic", r))
			}
		}()
		defer s.wg.Done()
		s.periodicUpdate(ctx)
	}()

	s.logger.Info("Symbol Selector started successfully")
	return nil
}

// websocketProcessor processes real-time WebSocket ticker data.
func (s *SymbolSelector) websocketProcessor(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("WebSocket processor goroutine panic recovered",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()
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

// processWebSocketTicker processes real-time ticker data from WebSocket.
func (s *SymbolSelector) processWebSocketTicker(msg map[string]interface{}) {
	data, ok := msg["data"].([]interface{})
	if !ok {
		s.logger.WithField("msg", msg).Debug("WebSocket message missing data field or wrong format")
		return
	}

	s.logger.WithField("data_count", len(data)).Debug("Received WebSocket ticker data")

	// // Log first few symbols for debugging
	// if len(data) > 0 {
	// 	sampleSymbols := make([]string, 0, 5)
	// 	for i, item := range data {
	// 		if i >= 5 {
	// 			break
	// 		}
	// 		if ticker, ok := item.(map[string]interface{}); ok {
	// 			if symbol, ok := ticker["s"].(string); ok {
	// 				sampleSymbols = append(sampleSymbols, symbol)
	// 			}
	// 		}
	// 	}
	// 	s.logger.WithField("sample_symbols", sampleSymbols).Debug("Sample symbols from WS data")
	// }

	candidates := make([]*SymbolData, 0, len(data))
	for _, item := range data {
		ticker, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		symbolRaw, ok := ticker["s"].(string)
		if !ok {
			continue
		}
		// Normalize symbol to uppercase for consistent comparison
		symbol := strings.ToUpper(symbolRaw)

		// Check blacklist first
		if s.isSymbolBlacklisted(symbol) {
			s.logger.WithFields(logrus.Fields{
				"symbol": symbol,
			}).Debug("Filtered out symbol - blacklisted")
			continue
		}

		quoteCurrency := s.extractQuoteCurrency(symbol)
		if !s.isQuoteCurrencySupported(quoteCurrency) {
			s.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"quote_currency": quoteCurrency,
			}).Debug("Filtered out symbol - unsupported quote currency")
			continue
		}

		quoteVolume, ok := parseTickerFloat(ticker["q"])
		if !ok || quoteVolume < s.minQuoteVolume() {
			s.logger.WithFields(logrus.Fields{
				"symbol":         symbol,
				"quote_currency": quoteCurrency,
				"volume_24h":     quoteVolume,
				"min_required":   s.minQuoteVolume(),
			}).Debug("Filtered out symbol - insufficient volume")
			continue
		}

		tradeCount, _ := parseTickerInt(ticker["n"])
		s.logger.WithFields(logrus.Fields{
			"symbol":         symbol,
			"quote_currency": quoteCurrency,
			"volume_24h":     quoteVolume,
		}).Debug("Symbol passed quote currency filter")
		candidates = append(candidates, &SymbolData{
			Symbol:     symbol,
			BaseAsset:  s.extractBaseAsset(symbol),
			QuoteAsset: quoteCurrency,
			Status:     "TRADING",
			Volume24h:  quoteVolume,
			Count24h:   tradeCount,
		})
	}

	s.logger.WithField("candidates_count", len(candidates)).Debug("Filtered symbol candidates")

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].QuoteAsset == candidates[j].QuoteAsset {
			return candidates[i].Volume24h > candidates[j].Volume24h
		}
		return candidates[i].QuoteAsset < candidates[j].QuoteAsset
	})

	selected := s.selectTopSymbols(candidates)

	// Add whitelist symbols if not already selected
	if len(s.config.Symbols.Whitelist) > 0 {
		whitelistSymbols := make(map[string]bool)
		for _, symbol := range s.config.Symbols.Whitelist {
			whitelistSymbols[symbol] = true
		}

		for _, candidate := range candidates {
			if whitelistSymbols[candidate.Symbol] {
				// Already selected
				delete(whitelistSymbols, candidate.Symbol)
			}
		}

		// 	// Add remaining whitelist symbols with dummy data
		// 	for symbol := range whitelistSymbols {
		// 		quote := s.extractQuoteCurrency(symbol)
		// 		if quote != "" && s.isQuoteCurrencySupported(quote) {
		// 			selected = append(selected, &SymbolData{
		// 				Symbol:     symbol,
		// 				BaseAsset:  s.extractBaseAsset(symbol),
		// 				QuoteAsset: quote,
		// 				Status:     "TRADING",
		// 				Volume24h:  1000000, // Dummy volume to pass min check
		// 				Count24h:   1000,
		// 			})
		// 			s.logger.WithField("symbol", symbol).Debug("Added whitelist symbol")
		// 		}
		// 	}
		// }
	}

	s.mu.Lock()
	s.activeSymbols = selected
	s.mu.Unlock()

	s.logger.WithField("symbols_count", len(selected)).Debug("Updated symbols from WebSocket")
}

func (s *SymbolSelector) selectTopSymbols(candidates []*SymbolData) []*SymbolData {
	maxPerQuote := s.config.Symbols.MaxSymbolsPerQuote
	if maxPerQuote <= 0 {
		maxPerQuote = 3
	}

	selected := make([]*SymbolData, 0, maxPerQuote*maxInt(len(s.config.Symbols.QuoteCurrencies), 1))
	perQuoteCount := make(map[string]int)
	seen := make(map[string]struct{})

	for _, candidate := range candidates {
		if _, ok := seen[candidate.Symbol]; ok {
			continue
		}
		if perQuoteCount[candidate.QuoteAsset] >= maxPerQuote {
			continue
		}

		selected = append(selected, candidate)
		perQuoteCount[candidate.QuoteAsset]++
		seen[candidate.Symbol] = struct{}{}
	}

	return selected
}

func (s *SymbolSelector) minQuoteVolume() float64 {
	if s.config.Symbols.MinVolume24h > 0 {
		return s.config.Symbols.MinVolume24h
	}
	if s.config.MinVolume24h > 0 {
		return s.config.MinVolume24h
	}
	// For volume farming, use a very low default threshold
	return 1000 // Instead of 1,000,000
}

// discoverSymbols initializes with empty symbols - data will come from WebSocket.
func (s *SymbolSelector) discoverSymbols(ctx context.Context) error {
	s.logger.Info("Initializing symbol discovery - will populate from WebSocket data")

	s.mu.Lock()
	s.activeSymbols = make([]*SymbolData, 0)
	s.mu.Unlock()

	return nil
}

// extractBaseAsset extracts base asset from symbol.
func (s *SymbolSelector) extractBaseAsset(symbol string) string {
	for _, quote := range supportedQuotes(s.config) {
		if len(symbol) > len(quote) && symbol[len(symbol)-len(quote):] == quote {
			return symbol[:len(symbol)-len(quote)]
		}
	}
	return symbol
}

// extractQuoteCurrency extracts quote currency from symbol.
func (s *SymbolSelector) extractQuoteCurrency(symbol string) string {
	for _, quote := range supportedQuotes(s.config) {
		if len(symbol) > len(quote) && symbol[len(symbol)-len(quote):] == quote {
			return quote
		}
	}
	return ""
}

// isQuoteCurrencySupported checks if the quote currency is in the supported list.
func (s *SymbolSelector) isQuoteCurrencySupported(quoteCurrency string) bool {
	if quoteCurrency == "" {
		return false
	}
	for _, supported := range s.config.Symbols.QuoteCurrencies {
		if quoteCurrency == supported {
			return true
		}
	}
	return false
}

// isSymbolBlacklisted checks if the symbol is in the blacklist.
func (s *SymbolSelector) isSymbolBlacklisted(symbol string) bool {
	for _, blacklisted := range s.config.Symbols.Blacklist {
		if symbol == blacklisted {
			return true
		}
	}
	return false
}

func (s *SymbolSelector) meetsBasicCriteria(symbol string, volume24h int, count int) bool {
	if volume24h <= 0 || count <= 0 {
		return false
	}

	minVolume := int(s.minQuoteVolume())
	if minVolume <= 0 {
		minVolume = 1_000_000
	}

	if volume24h < minVolume {
		return false
	}

	quote := s.extractQuoteCurrency(symbol)
	return s.isQuoteCurrencySupported(quote)
}

func (s *SymbolSelector) calculateSpread(ticker client.Ticker) float64 {
	if ticker.WeightedAvgPrice == 0 {
		return 0
	}
	return ((ticker.HighPrice - ticker.LowPrice) / ticker.WeightedAvgPrice) * 100
}

func (s *SymbolSelector) calculateLiquidityScore(metrics *SymbolMetrics) float64 {
	if metrics == nil || metrics.Volume24h <= 0 {
		return 0
	}

	volumeWeight := 0.7
	if s.config != nil && s.config.Symbols.VolumeWeighting > 0 {
		volumeWeight = s.config.Symbols.VolumeWeighting
	}

	volumeScore := math.Min(metrics.Volume24h/10_000_000, 1)
	return math.Max(volumeScore*volumeWeight, 0)
}

// periodicUpdate runs periodic symbol updates using WebSocket data.
func (s *SymbolSelector) periodicUpdate(ctx context.Context) {
	interval := time.Duration(s.config.SymbolRefreshIntervalSec) * time.Second
	if interval <= 0 {
		interval = 2 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.RLock()
			count := len(s.activeSymbols)
			s.mu.RUnlock()
			s.logger.WithField("symbols_count", count).Debug("Periodic symbol update - using WebSocket data")
		}
	}
}

// GetActiveSymbols returns the list of active symbols.
func (s *SymbolSelector) GetActiveSymbols() []*SymbolData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	symbols := make([]*SymbolData, len(s.activeSymbols))
	copy(symbols, s.activeSymbols)
	return symbols
}

// GetActiveSymbolCount returns the number of active symbols.
func (s *SymbolSelector) GetActiveSymbolCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.activeSymbols)
}

// Stop stops the symbol selector.
func (s *SymbolSelector) Stop(ctx context.Context) error {
	s.isRunningMu.Lock()
	if !s.isRunning {
		s.isRunningMu.Unlock()
		return nil
	}
	s.isRunning = false
	s.isRunningMu.Unlock()

	s.logger.Info("Stopping Symbol Selector")

	// Safely close stopCh
	select {
	case <-s.stopCh:
		// already closed
	default:
		close(s.stopCh)
	}

	if s.wsClient != nil {
		if err := s.wsClient.Close(); err != nil {
			s.logger.WithError(err).Error("Error closing WebSocket connection")
		}
	}

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("WaitGroup goroutine panic recovered during stop",
					zap.Any("panic", r))
				close(done)
			}
		}()
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("Symbol Selector stopped gracefully")
		return nil
	case <-ctx.Done():
		s.logger.Warn("Symbol Selector stop timeout")
		return ctx.Err()
	}
}

// IsRunning returns whether the symbol selector is running.
func (s *SymbolSelector) IsRunning() bool {
	s.isRunningMu.RLock()
	defer s.isRunningMu.RUnlock()
	return s.isRunning
}

// SetWebSocketClient sets an external WebSocket client to share connection
func (s *SymbolSelector) SetWebSocketClient(wsClient *client.WebSocketClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsClient = wsClient
}

// SetWhitelist updates the whitelist for symbol selection (called by Agentic layer)
func (s *SymbolSelector) SetWhitelist(symbols []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithField("whitelist", symbols).Info("Setting whitelist from Agentic layer")

	// Update the config whitelist
	s.config.Symbols.Whitelist = symbols

	// Disable auto-discover when using Agentic whitelist
	s.config.Symbols.AutoDiscover = false
}

// GetWhitelist returns the current whitelist from symbol selector
func (s *SymbolSelector) GetWhitelist() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	whitelist := make([]string, len(s.config.Symbols.Whitelist))
	copy(whitelist, s.config.Symbols.Whitelist)
	return whitelist
}

func normalizeVolumeConfig(cfg *config.VolumeFarmConfig) *config.VolumeFarmConfig {
	if cfg == nil {
		cfg = &config.VolumeFarmConfig{
			Symbols: config.SymbolsConfig{
				MinVolume24h:       1000,
				MaxSymbolsPerQuote: 1,
				QuoteCurrencies:    []string{"USD1"},
			},
			SymbolRefreshIntervalSec: 60,
			TickerStream:             "!ticker@arr",
		}
	}

	normalized := *cfg

	// Only apply defaults if values are not set (zero/empty)
	if normalized.Symbols.MinVolume24h <= 0 {
		normalized.Symbols.MinVolume24h = normalized.MinVolume24h
	}
	if normalized.Symbols.MinVolume24h <= 0 {
		normalized.Symbols.MinVolume24h = 1000 // Default only if not configured
	}
	if normalized.SymbolRefreshIntervalSec <= 0 {
		normalized.SymbolRefreshIntervalSec = 60 // Use config default (60s not 120s)
	}
	if normalized.TickerStream == "" {
		normalized.TickerStream = "!ticker@arr"
	}
	if normalized.Exchange.FuturesWSBase == "" {
		normalized.Exchange.FuturesWSBase = "wss://fstream.asterdex.com"
	}
	if len(normalized.Symbols.QuoteCurrencies) == 0 {
		normalized.Symbols.QuoteCurrencies = append([]string{}, normalized.SupportedQuoteCurrencies...)
	}
	if len(normalized.Symbols.QuoteCurrencies) == 0 {
		normalized.Symbols.QuoteCurrencies = []string{"USD1"}
	}
	if normalized.Symbols.MaxSymbolsPerQuote <= 0 {
		normalized.Symbols.MaxSymbolsPerQuote = 1 // Use config file default (1 not 3)
	}

	return &normalized
}

func parseTickerFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case string:
		out, err := strconv.ParseFloat(v, 64)
		return out, err == nil
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func parseTickerInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case string:
		out, err := strconv.Atoi(v)
		return out, err == nil
	default:
		return 0, false
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func supportedQuotes(cfg *config.VolumeFarmConfig) []string {
	if cfg != nil {
		if len(cfg.Symbols.QuoteCurrencies) > 0 {
			return cfg.Symbols.QuoteCurrencies
		}
		if len(cfg.SupportedQuoteCurrencies) > 0 {
			return cfg.SupportedQuoteCurrencies
		}
	}
	return []string{"USDT", "USD1", "BUSD", "USDC", "PERP"}
}
