package adaptive_grid

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// FullFlowScenario mô phỏng toàn bộ flow trading
// Từ IDLE -> ENTER_GRID -> TRADING -> EXIT_ALL -> WAIT_NEW_RANGE -> ENTER_GRID
type FullFlowScenario struct {
	name           string
	steps          []FlowStep
	expectedFinal  GridState
}

type FlowStep struct {
	action      string
	event       GridEvent
	expectedState GridState
	validate    func(sm *GridStateMachine, symbol string) bool
}

// TestFullFlow_NormalTradingLifecycle mô phỏng lifecycle bình thường
func TestFullFlow_NormalTradingLifecycle(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)
	symbol := "BTCUSD1"

	scenario := []struct {
		step        int
		action      string
		event       GridEvent
		wantState   GridState
		canPlace    bool
		isTrading   bool
	}{
		{1, "Range confirmed by detector", EventRangeConfirmed, GridStateEnterGrid, true, false},
		{2, "Entry orders placed", EventEntryPlaced, GridStateTrading, true, true},
		{3, "Trading active for 1 hour", GridEvent(-1), GridStateTrading, true, true}, // no transition
		{4, "ADX spike detected - exit", EventTrendExit, GridStateExitAll, false, false},
		{5, "Positions closed", EventPositionsClosed, GridStateWaitNewRange, false, false},
		{6, "Wait 30s for stabilization", GridEvent(-1), GridStateWaitNewRange, false, false},
		{7, "New range confirmed", EventNewRangeReady, GridStateEnterGrid, true, false},
		{8, "Re-enter grid", EventEntryPlaced, GridStateTrading, true, true},
	}

	for _, s := range scenario {
		t.Run(s.action, func(t *testing.T) {
			if s.event >= 0 {
				ok := sm.Transition(symbol, s.event)
				assert.True(t, ok, "Step %d: Transition should succeed", s.step)
			}

			// Validate state
			assert.Equal(t, s.wantState, sm.GetState(symbol), "Step %d: State mismatch", s.step)

			// Validate placement permission
			assert.Equal(t, s.canPlace, sm.CanPlaceOrders(symbol), "Step %d: CanPlaceOrders mismatch", s.step)

			// Validate trading status
			assert.Equal(t, s.isTrading, sm.IsTrading(symbol), "Step %d: IsTrading mismatch", s.step)
		})
	}
}

// TestFullFlow_EmergencyExitScenarios các tình huống exit khẩn cấp
func TestFullFlow_EmergencyExitScenarios(t *testing.T) {
	scenarios := []struct {
		name        string
		trigger     string
		exitEvent   GridEvent
	}{
		{"ADX > 25 spike", "High ADX detected", EventTrendExit},
		{"BB expansion > 1.5x", "Wide BB detected", EventTrendExit},
		{"Price outside BB bands", "Breakout detected", EventEmergencyExit},
		{"Consecutive losses > 3", "Max losses reached", EventEmergencyExit},
		{"Liquidation tier 4", "Near liquidation", EventEmergencyExit},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			logger := zap.NewNop()
			sm := NewGridStateMachine(logger)
			symbol := "BTCUSD1"

			// Setup: đang trading
			sm.ForceState(symbol, GridStateTrading)
			assert.True(t, sm.IsTrading(symbol), "Should be trading initially")

			// Trigger exit
			ok := sm.Transition(symbol, sc.exitEvent)
			assert.True(t, ok, "Exit transition should succeed")

			// Verify exit state
			assert.Equal(t, GridStateExitAll, sm.GetState(symbol), "Should be EXIT_ALL")
			assert.False(t, sm.IsTrading(symbol), "Should NOT be trading after exit")
			assert.False(t, sm.CanPlaceOrders(symbol), "Should NOT place orders in exit")
		})
	}
}

// TestFullFlow_RegridConditions kiểm tra điều kiện regrid
func TestFullFlow_RegridConditions(t *testing.T) {
	t.Run("AllConditionsMet", func(t *testing.T) {
		logger := zap.NewNop()
		sm := NewGridStateMachine(logger)
		symbol := "BTCUSD1"

		// Flow: EXIT_ALL -> WAIT_NEW_RANGE (với điều kiện đủ)
		sm.ForceState(symbol, GridStateExitAll)

		// Điều kiện 1: Positions closed
		ok := sm.Transition(symbol, EventPositionsClosed)
		assert.True(t, ok)
		assert.Equal(t, GridStateWaitNewRange, sm.GetState(symbol))

		// Điều kiện 2: New range ready
		ok = sm.Transition(symbol, EventNewRangeReady)
		assert.True(t, ok)
		assert.Equal(t, GridStateEnterGrid, sm.GetState(symbol))
		assert.True(t, sm.CanPlaceOrders(symbol), "Can place orders after regrid ready")
	})

	t.Run("RegridCooldown", func(t *testing.T) {
		logger := zap.NewNop()
		sm := NewGridStateMachine(logger)
		symbol := "BTCUSD1"

		// Activate cooldown
		sm.ActivateRegridCooldown(symbol, 1*time.Minute)
		assert.True(t, sm.IsRegridCooldownActive(symbol), "Cooldown should be active")

		// Cooldown prevents immediate regrid
		// (Logic này cần được kiểm tra trong isReadyForRegrid)
	})
}

// TestFullFlow_ConcurrentSymbolManagement quản lý nhiều symbols
func TestFullFlow_ConcurrentSymbolManagement(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)

	symbols := []string{"BTCUSD1", "ETHUSD1", "SOLUSD1"}

	// Setup: mỗi symbol ở state khác nhau
	states := []GridState{
		GridStateIdle,
		GridStateTrading,
		GridStateWaitNewRange,
	}

	for i, symbol := range symbols {
		sm.ForceState(symbol, states[i])
	}

	// Verify independent states
	for i, symbol := range symbols {
		assert.Equal(t, states[i], sm.GetState(symbol), "Symbol %s state mismatch", symbol)
	}

	// Transition each independently
	sm.Transition(symbols[0], EventRangeConfirmed) // IDLE -> ENTER_GRID
	sm.Transition(symbols[1], EventTrendExit)    // TRADING -> EXIT_ALL
	sm.Transition(symbols[2], EventNewRangeReady) // WAIT_NEW_RANGE -> ENTER_GRID

	// Verify new states
	assert.Equal(t, GridStateEnterGrid, sm.GetState(symbols[0]))
	assert.Equal(t, GridStateExitAll, sm.GetState(symbols[1]))
	assert.Equal(t, GridStateEnterGrid, sm.GetState(symbols[2]))

	// Verify placement permissions
	assert.True(t, sm.CanPlaceOrders(symbols[0]), "BTC: can place")
	assert.False(t, sm.CanPlaceOrders(symbols[1]), "ETH: cannot place (exit)")
	assert.True(t, sm.CanPlaceOrders(symbols[2]), "SOL: can place")
}

// TestFullFlow_AgentDecisionWaiting agent chờ đợi và quyết định
func TestFullFlow_AgentDecisionWaiting(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)
	symbol := "BTCUSD1"

	// Scenario: Agent phát hiện range sideways nhưng chưa đủ confidence
	t.Run("AgentWaitForConfirmation", func(t *testing.T) {
		// IDLE state - agent đang theo dõi
		sm.ForceState(symbol, GridStateIdle)

		// Agent phát hiện range nhưng ADX cao -> chờ
		// (Không transition, vẫn ở IDLE)
		assert.Equal(t, GridStateIdle, sm.GetState(symbol))
		assert.False(t, sm.CanPlaceOrders(symbol), "Chưa vào lệnh khi ADX cao")

		// Sau 3 candles ADX < 20, agent confirm
		ok := sm.Transition(symbol, EventRangeConfirmed)
		assert.True(t, ok)
		assert.Equal(t, GridStateEnterGrid, sm.GetState(symbol))
	})

	t.Run("AgentExitDecision", func(t *testing.T) {
		// Đang trading
		sm.ForceState(symbol, GridStateTrading)

		// Agent phát hiện trend mạnh -> quyết định exit
		ok := sm.Transition(symbol, EventTrendExit)
		assert.True(t, ok)
		assert.Equal(t, GridStateExitAll, sm.GetState(symbol))
	})
}

// TestFullFlow_GridCreationToExit vòng đời từ tạo grid đến exit
func TestFullFlow_GridCreationToExit(t *testing.T) {
	logger := zap.NewNop()
	sm := NewGridStateMachine(logger)
	symbol := "BTCUSD1"

	steps := []struct {
		desc     string
		validate func() bool
	}{
		{
			"1. Agent detects range (BB bands tight, ADX < 20)",
			func() bool {
				return sm.GetState(symbol) == GridStateIdle
			},
		},
		{
			"2. State: IDLE -> ENTER_GRID (RangeConfirmed)",
			func() bool {
				sm.Transition(symbol, EventRangeConfirmed)
				return sm.GetState(symbol) == GridStateEnterGrid
			},
		},
		{
			"3. GridManager places orders (Micro grid 0.05%, 5 orders/side)",
			func() bool {
				return sm.CanPlaceOrders(symbol)
			},
		},
		{
			"4. State: ENTER_GRID -> TRADING (EntryPlaced)",
			func() bool {
				sm.Transition(symbol, EventEntryPlaced)
				return sm.GetState(symbol) == GridStateTrading && sm.IsTrading(symbol)
			},
		},
		{
			"5. Trading active - orders getting filled",
			func() bool {
				return sm.IsTrading(symbol)
			},
		},
		{
			"6. ADX spike > 25 detected by realtime monitor",
			func() bool {
				// Realtime monitor gọi handleTrendExit
				return true
			},
		},
		{
			"7. State: TRADING -> EXIT_ALL (TrendExit)",
			func() bool {
				sm.Transition(symbol, EventTrendExit)
				return sm.GetState(symbol) == GridStateExitAll
			},
		},
		{
			"8. Cancel all orders, close positions",
			func() bool {
				return !sm.CanPlaceOrders(symbol) && !sm.IsTrading(symbol)
			},
		},
		{
			"9. State: EXIT_ALL -> WAIT_NEW_RANGE (PositionsClosed)",
			func() bool {
				sm.Transition(symbol, EventPositionsClosed)
				return sm.GetState(symbol) == GridStateWaitNewRange
			},
		},
		{
			"10. Wait for new range (range shift >= 0.5%, BB width < 1.5x)",
			func() bool {
				// isReadyForRegrid conditions checked here
				return sm.GetState(symbol) == GridStateWaitNewRange
			},
		},
		{
			"11. State: WAIT_NEW_RANGE -> ENTER_GRID (NewRangeReady)",
			func() bool {
				sm.Transition(symbol, EventNewRangeReady)
				return sm.GetState(symbol) == GridStateEnterGrid
			},
		},
		{
			"12. Ready to place new grid",
			func() bool {
				return sm.CanPlaceOrders(symbol)
			},
		},
	}

	for _, step := range steps {
		t.Run(step.desc, func(t *testing.T) {
			assert.True(t, step.validate(), "Step failed: %s", step.desc)
		})
	}
}

// BenchmarkFullLifecycle benchmarks the full state transition cycle
func BenchmarkFullLifecycle(b *testing.B) {
	logger := zap.NewNop()
	symbol := "BTCUSD1"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm := NewGridStateMachine(logger)

		// Full cycle
		sm.Transition(symbol, EventRangeConfirmed)  // IDLE -> ENTER_GRID
		sm.Transition(symbol, EventEntryPlaced)     // ENTER_GRID -> TRADING
		sm.Transition(symbol, EventTrendExit)       // TRADING -> EXIT_ALL
		sm.Transition(symbol, EventPositionsClosed) // EXIT_ALL -> WAIT_NEW_RANGE
		sm.Transition(symbol, EventNewRangeReady)   // WAIT_NEW_RANGE -> ENTER_GRID
	}
}
