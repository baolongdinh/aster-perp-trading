package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aster-bot/internal/api"
	"aster-bot/internal/auth"
	"aster-bot/internal/client"
	"aster-bot/internal/config"
	"aster-bot/internal/engine"
	"aster-bot/internal/ordermanager"
	"aster-bot/internal/risk"
	"aster-bot/internal/strategy"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// --- Config ---
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: load config: %v\n", err)
		os.Exit(1)
	}

	// --- Logger ---
	log := buildLogger(cfg.Log.Level)
	defer log.Sync()

	if cfg.Bot.DryRun {
		log.Warn("⚠️  DRY-RUN MODE — no real orders will be sent")
	}

	// --- Auth signer (V1 HMAC-SHA256) ---
	signer, err := auth.NewSigner(
		cfg.Exchange.APIKey,
		cfg.Exchange.APISecret,
		cfg.Exchange.RecvWindow,
	)
	if err != nil {
		log.Fatal("failed to create signer", zap.Error(err))
	}
	// Zero out api secret from config struct as early as possible
	cfg.Exchange.APISecret = ""

	// --- HTTP client ---
	httpClient := client.NewHTTPClient(cfg.Exchange.FuturesRESTBase, signer, log)
	marketClient := client.NewMarketClient(httpClient)
	futuresClient := client.NewFuturesClient(httpClient, cfg.Bot.DryRun, log)

	// --- Server Time Sync ---
	ctx := context.Background()

	// We make auth-less request first to get server time
	localTimeBefore := time.Now().UnixMilli()
	serverTime, err := marketClient.ServerTime(ctx)
	if err != nil {
		log.Fatal("failed to get server time", zap.Error(err))
	}
	localTimeAfter := time.Now().UnixMilli()

	// Estimated current local time (halfway through the round trip)
	localTimeEstimated := localTimeBefore + (localTimeAfter-localTimeBefore)/2

	// Calculate the difference: server time - local time
	offset := serverTime - localTimeEstimated
	signer.SetTimeOffset(offset)

	log.Info("exchange reachable, time synced",
		zap.Int64("server_time", serverTime),
		zap.Int64("offset_ms", offset),
	)

	// --- Risk manager ---
	riskMgr := risk.NewManager(cfg.Risk, log)

	// --- Order manager ---
	orderMgr := ordermanager.NewManager(futuresClient, log)

	// --- Build strategies from config ---
	strategies := buildStrategies(cfg, log)
	if len(strategies) == 0 {
		log.Warn("no strategies enabled — bot will run but not trade")
	}

	// --- Engine ---
	eng := engine.New(cfg, futuresClient, marketClient, riskMgr, orderMgr, strategies, log)

	// --- API server ---
	apiServer := api.NewServer(eng, riskMgr, log)

	// --- Start engine ---
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := eng.Start(runCtx); err != nil {
		log.Fatal("engine start failed", zap.Error(err))
	}

	// --- Start HTTP server ---
	addr := fmt.Sprintf("%s:%d", cfg.API.Host, cfg.API.Port)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      apiServer.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	go func() {
		log.Info("API server listening", zap.String("addr", addr))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("API server error", zap.Error(err))
		}
	}()

	// --- Graceful shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Info("shutdown signal received", zap.String("signal", sig.String()))

	cancel()
	eng.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	httpSrv.Shutdown(shutdownCtx)

	log.Info("bot shutdown complete")
}

// buildStrategies constructs strategy instances from config.
func buildStrategies(cfg *config.Config, log *zap.Logger) []strategy.Strategy {
	var out []strategy.Strategy
	for _, sc := range cfg.Strategies {
		switch sc.Name {
		case "ema_cross":
			emaCfg := strategy.EMACrossConfig{
				Enabled:       sc.Enabled,
				Symbols:       sc.Symbols,
				FastPeriod:    intParam(sc.Params, "fast_period", 9),
				SlowPeriod:    intParam(sc.Params, "slow_period", 21),
				Leverage:      intParam(sc.Params, "leverage", 5),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 50),
				Timeframe:     stringParam(sc.Params, "timeframe", "5m"),
			}
			out = append(out, strategy.NewEMACross(emaCfg, log))
			log.Info("strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		default:
			log.Warn("unknown strategy in config", zap.String("name", sc.Name))
		}
	}
	return out
}

func buildLogger(level string) *zap.Logger {
	lvl := zapcore.InfoLevel
	lvl.UnmarshalText([]byte(level))

	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(lvl),
		Development: false,
		Encoding:    "console",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "T",
			LevelKey:       "L",
			NameKey:        "N",
			CallerKey:      "C",
			MessageKey:     "M",
			StacktraceKey:  "S",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.CapitalColorLevelEncoder,
			EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05"),
			EncodeDuration: zapcore.StringDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	logger, _ := cfg.Build()
	return logger
}

// --- Config param helpers ---

func intParam(params map[string]interface{}, key string, def int) int {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	}
	return def
}

func floatParam(params map[string]interface{}, key string, def float64) float64 {
	v, ok := params[key]
	if !ok {
		return def
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return def
}

func stringParam(params map[string]interface{}, key string, def string) string {
	v, ok := params[key]
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}
