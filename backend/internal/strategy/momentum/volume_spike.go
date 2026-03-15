package momentum

import (
	"fmt"

	"aster-bot/internal/client"
	"aster-bot/internal/strategy"
	"aster-bot/internal/strategy/indicators"
	"aster-bot/internal/stream"

	"go.uber.org/zap"
)

type VolumeSpikeConfig struct {
	Enabled         bool
	Symbols         []string
	VolumeMaPeriod  int
	SpikeMultiplier float64 // Volume must be > MA * Multiplier
	OrderSizeUSDT   float64
}

type VolumeSpikeStrategy struct {
	cfg     VolumeSpikeConfig
	log     *zap.Logger
	volumes map[string][]float64
	closes  map[string][]float64
	highs   map[string][]float64
	lows    map[string][]float64
	lastSig map[string]strategy.Side
}

func NewVolumeSpike(cfg VolumeSpikeConfig, log *zap.Logger) *VolumeSpikeStrategy {
	return &VolumeSpikeStrategy{
		cfg:     cfg,
		log:     log,
		volumes: make(map[string][]float64),
		closes:  make(map[string][]float64),
		highs:   make(map[string][]float64),
		lows:    make(map[string][]float64),
		lastSig: make(map[string]strategy.Side),
	}
}

func (s *VolumeSpikeStrategy) Name() string      { return "volume_spike" }
func (s *VolumeSpikeStrategy) Symbols() []string { return s.cfg.Symbols }
func (s *VolumeSpikeStrategy) IsEnabled() bool   { return s.cfg.Enabled }
func (s *VolumeSpikeStrategy) SetEnabled(v bool) { s.cfg.Enabled = v }

func (s *VolumeSpikeStrategy) State(symbol string) string {
	volList, ok := s.volumes[symbol]
	if !ok || len(volList) < s.cfg.VolumeMaPeriod {
		return fmt.Sprintf("warming up (%d/%d)", len(volList), s.cfg.VolumeMaPeriod)
	}
	avgVol := indicators.SMA(volList, s.cfg.VolumeMaPeriod)
	lastVol := volList[len(volList)-1]
	wait := fmt.Sprintf("Wait for Vol > %.2f (%.1fx spike)", avgVol*s.cfg.SpikeMultiplier, s.cfg.SpikeMultiplier)
	return fmt.Sprintf("AvgVol:%.2f LastVol:%.2f (%.1fx) | %s", avgVol, lastVol, lastVol/avgVol, wait)
}

func (s *VolumeSpikeStrategy) OnKline(k stream.WsKline) {
	if !k.Kline.IsClosed {
		return
	}
	sym := k.Symbol
	s.volumes[sym] = append(s.volumes[sym], k.Kline.Volume)
	s.closes[sym] = append(s.closes[sym], k.Kline.Close)
	s.highs[sym] = append(s.highs[sym], k.Kline.High)
	s.lows[sym] = append(s.lows[sym], k.Kline.Low)

	if len(s.volumes[sym]) > 100 {
		s.volumes[sym] = s.volumes[sym][1:]
		s.closes[sym] = s.closes[sym][1:]
		s.highs[sym] = s.highs[sym][1:]
		s.lows[sym] = s.lows[sym][1:]
	}
}

func (s *VolumeSpikeStrategy) OnMarkPrice(mp stream.WsMarkPrice)        {}
func (s *VolumeSpikeStrategy) OnOrderUpdate(u stream.WsOrderUpdate)     {}
func (s *VolumeSpikeStrategy) OnAccountUpdate(u stream.WsAccountUpdate) {}

func (s *VolumeSpikeStrategy) Signal(symbol string, pos *client.Position) *strategy.Signal {
	vols := s.volumes[symbol]
	if len(vols) < s.cfg.VolumeMaPeriod+1 {
		return &strategy.Signal{Type: strategy.SignalNone}
	}

	volMA := indicators.SMA(vols[:len(vols)-1], s.cfg.VolumeMaPeriod)
	currentVol := vols[len(vols)-1]

	isSpike := currentVol > (volMA * s.cfg.SpikeMultiplier)

	currentHigh := s.highs[symbol][len(vols)-1]
	currentLow := s.lows[symbol][len(vols)-1]
	currentClose := s.closes[symbol][len(vols)-1]
	candleSize := currentHigh - currentLow

	var side strategy.Side
	// Bullish spike: closed in the top 30% of the candle
	if isSpike && currentClose > currentLow+(candleSize*0.7) {
		side = strategy.SideBuy
	}
	// Bearish spike: closed in the bottom 30% of the candle
	if isSpike && currentClose < currentLow+(candleSize*0.3) {
		side = strategy.SideSell
	}

	// Exit Logic
	if pos != nil && pos.PositionAmt != 0 {
		// Example naive exit: close trade if a massive opposite spike occurs
		if pos.PositionAmt > 0 && side == strategy.SideSell {
			s.lastSig[symbol] = strategy.SideSell
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Side:   strategy.SideSell,
				Reason: "Counter Volume Spike",
			}
		}
		if pos.PositionAmt < 0 && side == strategy.SideBuy {
			s.lastSig[symbol] = strategy.SideBuy
			return &strategy.Signal{
				Type:   strategy.SignalExit,
				Symbol: symbol,
				Side:   strategy.SideBuy,
				Reason: "Counter Volume Spike",
			}
		}
		return &strategy.Signal{Type: strategy.SignalNone} // Don't enter while holding
	}

	// Entry Logic
	if isSpike && side != "" && s.lastSig[symbol] != side {
		s.lastSig[symbol] = side
		return &strategy.Signal{
			Type:     strategy.SignalEnter,
			Symbol:   symbol,
			Side:     side,
			Quantity: fmt.Sprintf("%.4f", s.cfg.OrderSizeUSDT),
			Reason:   fmt.Sprintf("Volume Spike %.1fx", currentVol/volMA),
		}
	}

	return &strategy.Signal{Type: strategy.SignalNone}
}
