package maker

import (
	"go.uber.org/zap"
)

type RiskLevel struct {
	Level    int
	Name     string
	Trigger  string
	Action   string
	IsActive bool
}

type RiskManager struct {
	config     *Config
	logger     *zap.Logger
	levels     map[int]*RiskLevel
	imRate     float64
	positionPct float64
	dailyPnL   float64
	emergencyTriggered bool
}

func NewRiskManager(config *Config, logger *zap.Logger) *RiskManager {
	return &RiskManager{
		config: config,
		logger: logger,
		levels: map[int]*RiskLevel{
			1: {Level: 1, Name: "IM Rate High", Trigger: "IM >= 90%", Action: "Block workflow", IsActive: false},
			2: {Level: 2, Name: "High IM + Profit", Trigger: "IM >= 80% && profit > 0", Action: "Close position", IsActive: false},
			3: {Level: 3, Name: "Trailing Stop", Trigger: "Profit >= 0.1%", Action: "Activate trailing", IsActive: false},
			4: {Level: 4, Name: "Position Limit", Trigger: "Position > 40% max", Action: "Block new orders", IsActive: false},
			5: {Level: 5, Name: "Position Timeout", Trigger: "Position > 60s", Action: "Force close", IsActive: false},
			6: {Level: 6, Name: "Both Profitable", Trigger: "Long && Short profit > 0", Action: "Close both", IsActive: false},
			7: {Level: 7, Name: "TP Safety", Trigger: "Insufficient margin for TP", Action: "Pause TP", IsActive: false},
			8: {Level: 8, Name: "TP Pre-Check", Trigger: "Margin < required for protective", Action: "Block orders", IsActive: false},
			9: {Level: 9, Name: "Rate Limit", Trigger: "> N regrid in 48h", Action: "Block regrid", IsActive: false},
			10: {Level: 10, Name: "Emergency Stop", Trigger: "Multiple triggers", Action: "Full stop", IsActive: false},
		},
	}
}

func (rm *RiskManager) CheckAllLevels(ctx *RiskCheckContext) (actions []RiskAction) {
	// Reset all levels
	for k := range rm.levels {
		rm.levels[k].IsActive = false
	}

	// Level 1: IM Rate >= 90% → Block workflow
	if ctx.IMRate >= 0.9 {
		rm.levels[1].IsActive = true
		actions = append(actions, RiskAction{
			Level:   1,
			Type:    ActionBlockWorkflow,
			Reason:  "IM rate >= 90%",
		})
	}

	// Level 2: IM >= 80% && profit > 0 → Close position
	if ctx.IMRate >= 0.8 && ctx.UnrealizedProfit > 0 {
		rm.levels[2].IsActive = true
		actions = append(actions, RiskAction{
			Level:   2,
			Type:    ActionClosePosition,
			Reason:  "High IM + profit, securing gains",
		})
	}

	// Level 3: Trailing stop handled separately in strategy

	// Level 4: Position > 40% max → Block new orders
	if ctx.PositionPct > 0.4 {
		rm.levels[4].IsActive = true
		actions = append(actions, RiskAction{
			Level:   4,
			Type:    ActionBlockNewOrders,
			Reason:  "Position > 40% max limit",
		})
	}

	// Level 5: Position timeout handled separately in strategy

	// Level 6: Both sides profitable → Close both
	if ctx.LongProfit > 0 && ctx.ShortProfit > 0 {
		rm.levels[6].IsActive = true
		actions = append(actions, RiskAction{
			Level:   6,
			Type:    ActionCloseBoth,
			Reason:  "Both sides profitable",
		})
	}

	// Level 10: Emergency stop - multiple critical triggers
	criticalCount := 0
	if rm.levels[1].IsActive {
		criticalCount++
	}
	if rm.levels[4].IsActive {
		criticalCount++
	}
	if criticalCount >= 2 {
		rm.levels[10].IsActive = true
		rm.emergencyTriggered = true
		actions = append(actions, RiskAction{
			Level:   10,
			Type:    ActionEmergencyStop,
			Reason:  "Multiple critical risk triggers",
		})
	}

	return actions
}

func (rm *RiskManager) IsEmergencyTriggered() bool {
	return rm.emergencyTriggered
}

func (rm *RiskManager) ResetEmergency() {
	rm.emergencyTriggered = false
	rm.logger.Info("Emergency stop reset - manual intervention complete")
}

func (rm *RiskManager) GetActiveLevels() []int {
	var active []int
	for k, v := range rm.levels {
		if v.IsActive {
			active = append(active, k)
		}
	}
	return active
}

type RiskActionType string

const (
	ActionBlockWorkflow   RiskActionType = "BLOCK_WORKFLOW"
	ActionClosePosition   RiskActionType = "CLOSE_POSITION"
	ActionBlockNewOrders  RiskActionType = "BLOCK_NEW_ORDERS"
	ActionCloseBoth       RiskActionType = "CLOSE_BOTH"
	ActionPauseTP         RiskActionType = "PAUSE_TP"
	ActionEmergencyStop   RiskActionType = "EMERGENCY_STOP"
)

type RiskAction struct {
	Level int
	Type  RiskActionType
	Reason string
}

type RiskCheckContext struct {
	IMRate           float64
	PositionPct      float64
	UnrealizedProfit float64
	LongProfit       float64
	ShortProfit      float64
}
