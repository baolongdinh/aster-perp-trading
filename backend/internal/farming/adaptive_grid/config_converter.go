package adaptive_grid

import (
"time"

"aster-bot/internal/config"
)

// ConvertTimeFilterConfig converts internal config to adaptive_grid TimeFilter config
func ConvertTimeFilterConfig(cfg *config.TradingHoursConfig) *TradingHoursConfig {
	if cfg == nil {
		return DefaultTradingHoursConfig()
	}

	slots := make([]TimeSlotConfig, len(cfg.Slots))
	for i, slot := range cfg.Slots {
		slots[i] = TimeSlotConfig{
			Window: TimeWindow{
				Start:    slot.Window.Start,
				End:      slot.Window.End,
				Timezone: slot.Window.Timezone,
			},
			Enabled:          slot.Enabled,
			SizeMultiplier:   slot.SizeMultiplier,
			MaxExposurePct:   slot.MaxExposurePct,
			SpreadMultiplier: slot.SpreadMultiplier,
			Description:      slot.Description,
		}
	}

	return &TradingHoursConfig{
		Mode:                  TimeFilterMode(cfg.Mode),
		Timezone:              cfg.Timezone,
		Slots:                 slots,
		DefaultSizeMultiplier: 1.0,
		DefaultMaxExposurePct: 0.3,
	}
}

// ConvertTrendDetectionConfig converts internal config to adaptive_grid TrendDetector config
func ConvertTrendDetectionConfig(cfg *config.TrendDetectionConfig) *TrendDetectionConfig {
	if cfg == nil {
		return DefaultTrendDetectionConfig()
	}

	return &TrendDetectionConfig{
		RSIPeriod:       cfg.RSI.Period,
		UpdateInterval:  parseDuration(cfg.RSI.UpdateInterval),
		PersistenceTime: parseDuration(cfg.Persistence.RequiredDuration),
		Thresholds: RSIThresholds{
			StrongOverbought: cfg.Thresholds.StrongOverbought,
			MildOverbought:   cfg.Thresholds.MildOverbought,
			NeutralHigh:      cfg.Thresholds.NeutralHigh,
			NeutralLow:       cfg.Thresholds.NeutralLow,
			MildOversold:     cfg.Thresholds.MildOversold,
			StrongOversold:   cfg.Thresholds.StrongOversold,
		},
	}
}

// parseDuration helper to parse duration strings
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// ConvertDynamicGridConfig converts internal config to adaptive_grid DynamicSpreadConfig
func ConvertDynamicGridConfig(cfg *config.DynamicGridConfig) *DynamicSpreadConfig {
	if cfg == nil {
		return DefaultDynamicSpreadConfig()
	}

	lowThreshold := 0.3
	normalThreshold := 0.8
	highThreshold := 1.5
	if cfg.ATRThresholds.Low > 0 {
		lowThreshold = cfg.ATRThresholds.Low
	}
	if cfg.ATRThresholds.Normal > 0 {
		normalThreshold = cfg.ATRThresholds.Normal
	}
	if cfg.ATRThresholds.High > 0 {
		highThreshold = cfg.ATRThresholds.High
	}

	lowMult := cfg.SpreadMultipliers.Low
	normalMult := cfg.SpreadMultipliers.Normal
	highMult := cfg.SpreadMultipliers.High
	extremeMult := cfg.SpreadMultipliers.Extreme

	if lowMult == 0 {
		lowMult = 0.6
	}
	if normalMult == 0 {
		normalMult = 1.0
	}
	if highMult == 0 {
		highMult = 1.5
	}
	if extremeMult == 0 {
		extremeMult = 2.0
	}

	atrPeriod := cfg.ATRPeriod
	if atrPeriod == 0 {
		atrPeriod = 7
	}

	return &DynamicSpreadConfig{
		BaseSpreadPct:     cfg.BaseSpreadPct,
		LowThreshold:      lowThreshold,
		NormalThreshold:   normalThreshold,
		HighThreshold:     highThreshold,
		LowMultiplier:     lowMult,
		NormalMultiplier:  normalMult,
		HighMultiplier:    highMult,
		ExtremeMultiplier: extremeMult,
		ATRPeriod:         atrPeriod,
	}
}

// ConvertInventoryConfig converts internal config to adaptive_grid InventoryConfig
func ConvertInventoryConfig(cfg *config.InventorySkewConfig) *InventoryConfig {
	if cfg == nil {
		return DefaultInventoryConfig()
	}

	maxInvPct := cfg.MaxInventoryPct
	if maxInvPct == 0 {
		maxInvPct = 0.30
	}

	return &InventoryConfig{
		MaxInventoryPct: maxInvPct,
	}
}

// ConvertClusterStopLossConfig converts internal config to adaptive_grid ClusterStopLossConfig
func ConvertClusterStopLossConfig(cfg *config.ClusterStopLossConfig) *ClusterStopLossConfig {
	if cfg == nil {
		return DefaultClusterStopLossConfig()
	}

	monitorHours := cfg.TimeThresholds.Monitor
	emergencyHours := cfg.TimeThresholds.Emergency
	if monitorHours == 0 {
		monitorHours = 24
	}
	if emergencyHours == 0 {
		emergencyHours = 48
	}

	monitorDD := cfg.DrawdownThresholds.Monitor
	emergencyDD := cfg.DrawdownThresholds.Emergency
	if monitorDD == 0 {
		monitorDD = 0.02
	}
	if emergencyDD == 0 {
		emergencyDD = 0.05
	}

	return &ClusterStopLossConfig{
		MonitorHours:      monitorHours,
		EmergencyHours:    emergencyHours,
		MonitorDrawdown:   monitorDD,
		EmergencyDrawdown: emergencyDD,
	}
}
