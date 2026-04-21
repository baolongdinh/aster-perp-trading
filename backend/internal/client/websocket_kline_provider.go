package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// KlineProvider provides kline data via WebSocket streaming
// Replaces REST API kline fetching to avoid rate limits
type KlineProvider struct {
	wsClient       *WebSocketClient
	logger         *zap.Logger
	
	// Kline storage per symbol
	klines         map[string][]KlineMessage // symbol -> klines
	klinesMu       sync.RWMutex
	
	// Warm-up tracking
	warmupComplete map[string]bool
	warmupMu       sync.RWMutex
	
	// Minimum klines needed for warm-up
	minWarmupKlines int
	
	// Stop channel
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewKlineProvider creates a new WebSocket-based kline provider
func NewKlineProvider(wsClient *WebSocketClient, logger *zap.Logger) *KlineProvider {
	return &KlineProvider{
		wsClient:        wsClient,
		logger:          logger.With(zap.String("component", "kline_provider")),
		klines:          make(map[string][]KlineMessage),
		warmupComplete:  make(map[string]bool),
		minWarmupKlines: 100, // Default: need at least 100 klines for indicators
		stopCh:          make(chan struct{}),
	}
}

// SetMinWarmupKlines sets the minimum number of klines needed for warm-up
func (kp *KlineProvider) SetMinWarmupKlines(min int) {
	kp.minWarmupKlines = min
}

// Start begins the kline collection process
func (kp *KlineProvider) Start(ctx context.Context) {
	kp.wg.Add(1)
	go kp.klineCollector(ctx)
	kp.logger.Info("Kline provider started",
		zap.Int("min_warmup_klines", kp.minWarmupKlines))
}

// Stop stops the kline provider
func (kp *KlineProvider) Stop() {
	close(kp.stopCh)
	kp.wg.Wait()
	kp.logger.Info("Kline provider stopped")
}

// klineCollector collects klines from WebSocket channel
func (kp *KlineProvider) klineCollector(ctx context.Context) {
	defer kp.wg.Done()

	klineCh := kp.wsClient.GetKlineChannel()
	
	for {
		select {
		case <-ctx.Done():
			kp.logger.Info("Kline collector stopping (context cancelled)")
			return
		case <-kp.stopCh:
			kp.logger.Info("Kline collector stopping (stop signal)")
			return
		case kline, ok := <-klineCh:
			if !ok {
				kp.logger.Warn("Kline channel closed")
				return
			}
			kp.processKline(kline)
		}
	}
}

// processKline processes and stores a kline message
func (kp *KlineProvider) processKline(kline KlineMessage) {
	symbol := kline.Symbol

	kp.klinesMu.Lock()
	defer kp.klinesMu.Unlock()

	// Initialize slice if needed
	if _, exists := kp.klines[symbol]; !exists {
		kp.klines[symbol] = make([]KlineMessage, 0, kp.minWarmupKlines*2)
		kp.logger.Info("Started collecting klines for symbol",
			zap.String("symbol", symbol))
	}

	// Check if we already have this kline (by start time)
	klines := kp.klines[symbol]
	for i, existing := range klines {
		if existing.StartTime == kline.StartTime {
			// Update existing kline (more complete data)
			klines[i] = kline
			return
		}
	}

	// Add new kline
	klines = append(klines, kline)
	
	// Keep only the most recent klines (sliding window)
	maxKlines := kp.minWarmupKlines * 2
	if len(klines) > maxKlines {
		klines = klines[len(klines)-maxKlines:]
	}
	
	kp.klines[symbol] = klines

	// Check if warm-up is complete
	kp.checkWarmupComplete(symbol, len(klines))
}

// checkWarmupComplete checks and marks if warm-up is complete for a symbol
func (kp *KlineProvider) checkWarmupComplete(symbol string, count int) {
	kp.warmupMu.Lock()
	defer kp.warmupMu.Unlock()

	if count >= kp.minWarmupKlines && !kp.warmupComplete[symbol] {
		kp.warmupComplete[symbol] = true
		kp.logger.Info("Kline warm-up complete for symbol",
			zap.String("symbol", symbol),
			zap.Int("klines_collected", count))
	}
}

// IsWarmupComplete checks if warm-up is complete for a symbol
func (kp *KlineProvider) IsWarmupComplete(symbol string) bool {
	kp.warmupMu.RLock()
	defer kp.warmupMu.RUnlock()
	return kp.warmupComplete[symbol]
}

// GetKlines returns collected klines for a symbol
func (kp *KlineProvider) GetKlines(symbol string) []KlineMessage {
	kp.klinesMu.RLock()
	defer kp.klinesMu.RUnlock()
	
	klines, exists := kp.klines[symbol]
	if !exists {
		return nil
	}
	
	// Return a copy
	result := make([]KlineMessage, len(klines))
	copy(result, klines)
	return result
}

// GetKlinesCount returns the number of collected klines for a symbol
func (kp *KlineProvider) GetKlinesCount(symbol string) int {
	kp.klinesMu.RLock()
	defer kp.klinesMu.RUnlock()
	return len(kp.klines[symbol])
}

// WaitForWarmup blocks until warm-up is complete for a symbol or timeout
func (kp *KlineProvider) WaitForWarmup(ctx context.Context, symbol string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for kline warm-up for %s", symbol)
		case <-ticker.C:
			if kp.IsWarmupComplete(symbol) {
				return nil
			}
		}
	}
}

// GetOHLCV returns OHLCV data for a symbol (for indicator calculations)
func (kp *KlineProvider) GetOHLCV(symbol string) (opens, highs, lows, closes, volumes []float64) {
	kp.klinesMu.RLock()
	defer kp.klinesMu.RUnlock()

	klines := kp.klines[symbol]
	if len(klines) == 0 {
		return nil, nil, nil, nil, nil
	}

	count := len(klines)
	opens = make([]float64, count)
	highs = make([]float64, count)
	lows = make([]float64, count)
	closes = make([]float64, count)
	volumes = make([]float64, count)

	for i, k := range klines {
		opens[i] = k.Open
		highs[i] = k.High
		lows[i] = k.Low
		closes[i] = k.Close
		volumes[i] = k.Volume
	}

	return opens, highs, lows, closes, volumes
}

// ClearKlines clears klines for a symbol (useful when switching symbols)
func (kp *KlineProvider) ClearKlines(symbol string) {
	kp.klinesMu.Lock()
	delete(kp.klines, symbol)
	kp.klinesMu.Unlock()

	kp.warmupMu.Lock()
	delete(kp.warmupComplete, symbol)
	kp.warmupMu.Unlock()

	kp.logger.Info("Cleared klines for symbol", zap.String("symbol", symbol))
}

// GetAllSymbols returns all symbols with collected klines
func (kp *KlineProvider) GetAllSymbols() []string {
	kp.klinesMu.RLock()
	defer kp.klinesMu.RUnlock()

	symbols := make([]string, 0, len(kp.klines))
	for symbol := range kp.klines {
		symbols = append(symbols, symbol)
	}
	return symbols
}
