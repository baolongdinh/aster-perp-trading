package momentum

import (
	"fmt"
	"sync"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/indicators"
	"aster-bot/internal/strategy/regime"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type VolumeSpikeConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Symbols         []string `yaml:"symbols"`
	VolumeMaPeriod  int      `yaml:"volume_ma_period"`
	SpikeMultiplier float64  `yaml:"spike_multiplier"` // Volume must be > MA * Multiplier
	OrderSizeUSDT   float64  `yaml:"order_size_usdt"`
	Timeframe         string   `yaml:"timeframe"`
}

type VolumeSpikeStrategy struct {
	cfg         VolumeSpikeConfig
	log         *zap.Logger
	enabled     bool
	mu          sync.RWMutex
	lastSig     map[string]strategy.Side
	classifiers map[string]*regime.Classifier
}

func NewVolumeSpike(cfg VolumeSpikeConfig, log *zap.Logger) *VolumeSpikeStrategy {
	return &VolumeSpikeStrategy{
		cfg:         cfg,
		log:         log,
		enabled:     cfg.Enabled,
		lastSig:     make(map[string]strategy.Side),
		classifiers: make(map[string]*regime.Classifier),
	}
}

func (s *VolumeSpikeStrategy) RequiredIntervals() []string {
	tf := s.cfg.Timeframe
	if tf == "" {
		tf = "5m"
	}
	return []string{tf}
}

func (s *VolumeSpikeStrategy) Name() string      { return "volume_spike" }
func (s *VolumeSpikeStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *VolumeSpikeStrategy) IsEnabled() bool   { return s.enabled }
func (s *VolumeSpikeStrategy) SetEnabled(v bool) { s.enabled = v }

func (s *VolumeSpikeStrategy) SetClassifier(symbol string, c *regime.Classifier) {
	s.mu.Lock()
	s.classifiers[symbol] = c
	s.mu.Unlock()
}

func (s *VolumeSpikeStrategy) State(symbol string) string {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil {
		return "warming up (no classifier)"
	}

	// Volume spike doesn't define TF, it usually works on 5m
	vols := cf.GetVolumes("5m")
	if len(vols) < s.cfg.VolumeMaPeriod {
		return fmt.Sprintf("warming up (%d/%d)", len(vols), s.cfg.VolumeMaPeriod)
	}
	avgVol := indicators.SMA(vols, s.cfg.VolumeMaPeriod)
	lastVol := vols[len(vols)-1]
	wait := fmt.Sprintf("Wait for Vol > %.2f (%.1fx spike)", avgVol*s.cfg.SpikeMultiplier, s.cfg.SpikeMultiplier)
	return fmt.Sprintf("AvgVol:%.2f LastVol:%.2f (%.1fx) | %s", avgVol, lastVol, lastVol/avgVol, wait)
}

func (s *VolumeSpikeStrategy) OnKline(k stream.WsKline) {} // Classifier handles history

func (s *VolumeSpikeStrategy) OnMarkPrice(_ stream.WsMarkPrice)        {}
func (s *VolumeSpikeStrategy) OnOrderUpdate(_ stream.WsOrderUpdate)     {}
func (s *VolumeSpikeStrategy) OnAccountUpdate(_ stream.WsAccountUpdate) {}

func (s *VolumeSpikeStrategy) Signals(symbol string, pos *client.Position) []*strategy.Signal {
	s.mu.RLock()
	cf, ok := s.classifiers[symbol]
	s.mu.RUnlock()

	if !ok || cf == nil {
		return nil
	}

	// Using 5m as default timeframe for volume spikes
	tf := "5m"
	vols := cf.GetVolumes(tf)
	closes := cf.GetCloses(tf)
	highs := cf.GetHighs(tf)
	lows := cf.GetLows(tf)

	if len(vols) < s.cfg.VolumeMaPeriod+1 {
		return nil
	}

	volMA := indicators.SMA(vols[:len(vols)-1], s.cfg.VolumeMaPeriod)
	currentVol := vols[len(vols)-1]

	isSpike := currentVol > (volMA * s.cfg.SpikeMultiplier)

	currentHigh := highs[len(highs)-1]
	currentLow := lows[len(lows)-1]
	currentClose := closes[len(closes)-1]
	candleSize := currentHigh - currentLow
	atr := cf.GetATR(tf, 14)

	var side strategy.Side
	if isSpike && currentClose > currentLow+(candleSize*0.7) {
		side = strategy.SideBuy
	}
	if isSpike && currentClose < currentLow+(candleSize*0.3) {
		side = strategy.SideSell
	}

	// Exit Logic
	if pos != nil && pos.PositionAmt != 0 {
		if pos.PositionAmt > 0 && side == strategy.SideSell {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: "Counter Volume Spike",
			}}
		}
		if pos.PositionAmt < 0 && side == strategy.SideBuy {
			return []*strategy.Signal{{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Reason: "Counter Volume Spike",
			}}
		}
		return nil
	}

	// Entry Logic
	if isSpike && side != "" && s.lastSig[symbol] != side {
		var sl, tp float64
		if atr > 0 {
			if side == strategy.SideBuy {
				sl = currentClose - (atr * 2.0)
				tp = currentClose + (atr * 4.0)
			} else {
				sl = currentClose + (atr * 2.0)
				tp = currentClose - (atr * 4.0)
			}
		}

		s.mu.Lock()
		s.lastSig[symbol] = side
		s.mu.Unlock()

		return []*strategy.Signal{{
			Type:         strategy.SignalEnter,
			Symbol:       symbol,
			Side:         side,
			Quantity:     fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			StopLoss:     sl,
			TakeProfit:   tp,
			Reason:       fmt.Sprintf("Volume Spike %.1fx", currentVol/volMA),
			StrategyName: s.Name(),
		}}
	}

	return nil
}
