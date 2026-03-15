package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	"aster-bot/internal/strategy/meanrev"
	"aster-bot/internal/strategy/momentum"
	"aster-bot/internal/strategy/structure"
	"aster-bot/internal/strategy/trend"

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
	log := buildLogger(cfg.Log.Level, cfg.Log.File)
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
	httpClient := client.NewHTTPClient(cfg.Exchange.FuturesRESTBase, signer, log, cfg.Exchange.RequestsPerSecond)
	marketClient := client.NewMarketClient(httpClient)
	futuresClient := client.NewFuturesClient(httpClient, cfg.Bot.DryRun, log)

	// --- Precision Manager ---
	prec := client.NewPrecisionManager()

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

	// --- Exchange Info & Precision ---
	info, err := marketClient.ExchangeInfo(ctx)
	if err != nil {
		log.Fatal("failed to get exchange info", zap.Error(err))
	}
	if err := prec.UpdateFromExchangeInfo(info); err != nil {
		log.Error("failed to parse precision info", zap.Error(err))
	}

	log.Info("exchange reachable, time synced",
		zap.Int64("server_time", serverTime),
		zap.Int64("offset_ms", offset),
	)

	// --- Risk manager ---
	riskMgr := risk.NewManager(cfg.Risk, log)
	riskMgr.LoadState()

	// --- Order manager ---
	orderMgr := ordermanager.NewManager(futuresClient, prec, log)

	// --- Build strategies from config ---
	strategies := buildStrategies(cfg, riskMgr, log)

	if len(strategies) == 0 {
		log.Warn("no strategies enabled — bot will run but not trade")
	}

	// --- Engine ---
	eng := engine.New(cfg, futuresClient, marketClient, riskMgr, orderMgr, prec, strategies, log)

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

// buildStrategies constructs the StrategyRouter meta-strategy.
func buildStrategies(cfg *config.Config, riskMgr *risk.Manager, log *zap.Logger) []strategy.Strategy {

	var activeSymbols []string
	var activeSubs []strategy.Strategy

	for _, sc := range cfg.Strategies {
		switch sc.Name {
		case "ema_cross":
			emaCfg := trend.EMACrossConfig{
				FastPeriod: intParam(sc.Params, "fast_period", 9),
				SlowPeriod: intParam(sc.Params, "slow_period", 21),
				Timeframe:  stringParam(sc.Params, "timeframe", "5m"),
				Symbols:    sc.Symbols,
				Enabled:    sc.Enabled,
				JoinTrend:  boolParam(sc.Params, "join_trend", false),
			}
			activeSubs = append(activeSubs, trend.NewEMACross(emaCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "rsi_divergence":
			rsiCfg := meanrev.RSIDivergenceConfig{
				RsiPeriod:     intParam(sc.Params, "rsi_period", 14),
				Overbought:    floatParam(sc.Params, "overbought", 70.0),
				Oversold:      floatParam(sc.Params, "oversold", 30.0),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Timeframe:     stringParam(sc.Params, "timeframe", "15m"),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
			}
			activeSubs = append(activeSubs, meanrev.NewRSIDivergence(rsiCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "vwap_reversion":
			vCfg := meanrev.VWAPReversionConfig{
				DevThreshold:  floatParam(sc.Params, "dev_threshold_pct", 0.5),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "15m"),
			}
			activeSubs = append(activeSubs, meanrev.NewVWAPReversion(vCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "bb_bounce":
			bbCfg := meanrev.BBBounceConfig{
				Period:        intParam(sc.Params, "period", 20),
				StdDev:        floatParam(sc.Params, "std_dev", 2.0),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "15m"),
			}
			activeSubs = append(activeSubs, meanrev.NewBBBounce(bbCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "sr_bounce":
			srCfg := meanrev.SRBounceConfig{
				Lookback:      intParam(sc.Params, "lookback", 50),
				BouncePct:     floatParam(sc.Params, "bounce_pct", 0.1),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "1h"),
			}
			activeSubs = append(activeSubs, meanrev.NewSRBounce(srCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "breakout_retest":
			brCfg := trend.BreakoutRetestConfig{
				ConsolidationPeriods: intParam(sc.Params, "consolidation_periods", 20),
				BreakoutVolumeMult:   floatParam(sc.Params, "breakout_vol_mult", 2.0),
				RetestTolerancePct:   floatParam(sc.Params, "retest_tolerance_pct", 0.1),
				OrderSizeUSDT:        floatParam(sc.Params, "order_size_usdt", 50),
				Symbols:              sc.Symbols,
				Enabled:              sc.Enabled,
				Timeframe:            stringParam(sc.Params, "timeframe", "1h"),
			}
			activeSubs = append(activeSubs, trend.NewBreakoutRetest(brCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "flag_pennant":
			fpCfg := trend.FlagPennantConfig{
				ImpulseMinPct:     floatParam(sc.Params, "impulse_min_pct", 3.0),
				ImpulseCandles:    intParam(sc.Params, "impulse_candles", 5),
				FlagMaxRetracePct: floatParam(sc.Params, "flag_max_retrace_pct", 38.2),
				FlagCandles:       intParam(sc.Params, "flag_candles", 10),
				OrderSizeUSDT:     floatParam(sc.Params, "order_size_usdt", 50),
				Symbols:           sc.Symbols,
				Enabled:           sc.Enabled,
				Timeframe:         stringParam(sc.Params, "timeframe", "1h"),
			}
			activeSubs = append(activeSubs, trend.NewFlagPennant(fpCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "trailing_sh":
			shCfg := trend.TrailingSHConfig{
				SwingPeriod:   intParam(sc.Params, "swing_period", 5),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "1h"),
			}
			activeSubs = append(activeSubs, trend.NewTrailingSH(shCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "momentum_roc":
			rocCfg := momentum.MomentumROCConfig{
				ROCPeriod:     intParam(sc.Params, "roc_period", 10),
				ROCThreshold:  floatParam(sc.Params, "roc_threshold", 1.0),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 50),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "15m"),
			}
			activeSubs = append(activeSubs, momentum.NewMomentumROC(rocCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "orb":
			orbCfg := momentum.ORBConfig{
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "15m"),
			}
			activeSubs = append(activeSubs, momentum.NewORB(orbCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "volume_spike":
			volCfg := momentum.VolumeSpikeConfig{
				VolumeMaPeriod:  intParam(sc.Params, "volume_ma_period", 20),
				SpikeMultiplier: floatParam(sc.Params, "spike_multiplier", 3.0),
				OrderSizeUSDT:   floatParam(sc.Params, "order_size_usdt", 50),
				Symbols:         sc.Symbols,
				Enabled:         sc.Enabled,
			}
			activeSubs = append(activeSubs, momentum.NewVolumeSpike(volCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "structure_bos":
			bosCfg := structure.BOSConfig{
				SwingPeriod:   intParam(sc.Params, "swing_period", 5),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "1h"),
			}
			activeSubs = append(activeSubs, structure.NewBOS(bosCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "liquidity_sweep":
			lqCfg := structure.LiquiditySweepConfig{
				Lookback:      intParam(sc.Params, "lookback", 50),
				TolerancePct:  floatParam(sc.Params, "tolerance_pct", 0.05),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "1h"),
			}
			activeSubs = append(activeSubs, structure.NewLiquiditySweep(lqCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		case "fvg_fill":
			fvgCfg := structure.FVGConfig{
				MinGapPct:     floatParam(sc.Params, "min_gap_pct", 0.1),
				OrderSizeUSDT: floatParam(sc.Params, "order_size_usdt", 100),
				Symbols:       sc.Symbols,
				Enabled:       sc.Enabled,
				Timeframe:     stringParam(sc.Params, "timeframe", "1h"),
			}
			activeSubs = append(activeSubs, structure.NewFVG(fvgCfg, log))
			if sc.Enabled {
				activeSymbols = append(activeSymbols, sc.Symbols...)
			}
			log.Info("sub-strategy loaded",
				zap.String("name", sc.Name),
				zap.Bool("enabled", sc.Enabled),
				zap.Strings("symbols", sc.Symbols),
			)
		default:
			log.Warn("unknown strategy in config", zap.String("name", sc.Name))
		}
	}

	// De-duplicate active symbols for the Router
	seen := make(map[string]bool)
	var finalSymbols []string
	for _, s := range activeSymbols {
		if !seen[s] {
			seen[s] = true
			finalSymbols = append(finalSymbols, s)
		}
	}

	routerCfg := strategy.RouterConfig{
		Enabled: len(finalSymbols) > 0,
		Symbols: finalSymbols,
	}
	router := strategy.NewRouter(routerCfg, riskMgr, log)

	for _, sub := range activeSubs {
		router.Register(sub)
	}

	log.Info("strategy router loaded", zap.Int("active_subs", len(activeSubs)), zap.Strings("symbols", finalSymbols))

	// Engine expects an array of strategies, but we only give it the Router
	return []strategy.Strategy{router}
}

func buildLogger(level, filePath string) *zap.Logger {
	lvl := zapcore.InfoLevel
	lvl.UnmarshalText([]byte(level))

	encoderConfig := zapcore.EncoderConfig{
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
	}

	// Output paths
	stdout := zapcore.Lock(os.Stdout)

	cores := []zapcore.Core{
		zapcore.NewCore(zapcore.NewConsoleEncoder(encoderConfig), stdout, lvl),
	}

	if filePath != "" {
		// Ensure directory exists
		dir := "logs"
		if i := strings.LastIndex(filePath, "/"); i != -1 {
			dir = filePath[:i]
		} else if i := strings.LastIndex(filePath, "\\"); i != -1 {
			dir = filePath[:i]
		}

		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		} else {
			file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
			} else {
				// We use console encoder for file too as requested by user's preference for readable logs
				cores = append(cores, zapcore.NewCore(zapcore.NewConsoleEncoder(encoderConfig), zapcore.AddSync(file), lvl))
			}
		}
	}

	core := zapcore.NewTee(cores...)
	return zap.New(core, zap.AddCaller())
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
		fmt.Printf("[DEBUG CONFIG] %s missing, using default: %f\n", key, def)
		return def
	}

	var result float64 = def
	var parsed bool = true

	switch t := v.(type) {
	case float64:
		result = t
	case float32:
		result = float64(t)
	case int:
		result = float64(t)
	case int32:
		result = float64(t)
	case int64:
		result = float64(t)
	case uint:
		result = float64(t)
	case uint64:
		result = float64(t)
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			result = f
		} else {
			parsed = false
		}
	default:
		// Fallback to sprintf
		if f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); err == nil {
			result = f
		} else {
			parsed = false
		}
	}

	if parsed {
		fmt.Printf("[DEBUG CONFIG] %s successfully parsed as: %f (raw type: %T, raw val: %v)\n", key, result, v, v)
		return result
	}

	fmt.Printf("[DEBUG CONFIG] %s failed to parse, using default: %f (raw type: %T, raw val: %v)\n", key, def, v, v)
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
func boolParam(params map[string]interface{}, key string, def bool) bool {
	v, ok := params[key]
	if !ok {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
