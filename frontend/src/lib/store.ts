import { reactive, readonly } from 'vue'
import type { Position, Strategy, Trade, BotStatus, WsMessage } from '../types'
import { asterApi } from './api'
import { useWebSocket } from './websocket'

interface State {
  status: BotStatus | null
  positions: Position[]
  strategies: Strategy[]
  recentTrades: Trade[]
  loading: boolean
  error: string | null
}

const state = reactive<State>({
  status: {
    online: true,
    accountEquity: 50245.82,
    dailyPnl: 1250.20,
    drawdownPct: 0.45,
    uptime: '14d 6h',
    version: '2.4.1'
  },
  positions: [],
  strategies: [],
  recentTrades: [],
  loading: false,
  error: null
})

export function useAsterStore() {
  async function fetchAll() {
    state.loading = true
    try {
      const [status, positions, strategies, trades] = await Promise.all([
        asterApi.getStatus(),
        asterApi.getPositions(),
        asterApi.getStrategies(),
        asterApi.getTrades({ limit: 5 })
      ])
      
      state.status = status.data
      state.positions = positions.data
      state.strategies = strategies.data
      state.recentTrades = trades.data
    } catch (err: any) {
      state.error = err.message || 'Failed to fetch data'
    } finally {
      state.loading = false
    }
  }

  function handleWsMessage(msg: WsMessage) {
    switch (msg.type) {
      case 'STATUS_UPDATE':
        state.status = msg.data
        break
      case 'POSITION_UPDATE':
        state.positions = msg.data // Simple replace for now
        break
      case 'TRADE_FILL':
        state.recentTrades.unshift(msg.data)
        if (state.recentTrades.length > 50) state.recentTrades.pop()
        break
      case 'STRATEGY_UPDATE':
        const idx = state.strategies.findIndex(s => s.name === msg.data.name)
        if (idx !== -1) state.strategies[idx] = msg.data
        break
    }
  }

  const { isConnected } = useWebSocket(handleWsMessage)

  return {
    state: readonly(state),
    isConnected,
    fetchAll
  }
}
