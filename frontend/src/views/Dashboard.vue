<script setup lang="ts">
import { computed } from 'vue'
import { useStatus } from '../api/status'
import { usePositions } from '../api/positions'
import { 
  Activity,
  TrendingUp,
  TrendingDown,
  Wallet,
  List
} from 'lucide-vue-next'

const { status } = useStatus()
const { positions, loading: positionsLoading } = usePositions()

const isRunning = computed(() => status.value?.running ?? false)
const isPaused = computed(() => status.value?.paused ?? false)
const dailyPnl = computed(() => status.value?.daily_pnl ?? 0)

const totalUnrealizedPnl = computed(() => {
  return positions.value.reduce((sum, pos) => sum + (pos.unrealized_pnl || 0), 0)
})

const formatPnl = (pnl: number) => {
  const sign = pnl >= 0 ? '+' : ''
  return `${sign}$${pnl.toFixed(2)}`
}

const formatPrice = (price: number) => {
  return price.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 4 })
}
</script>

<template>
  <div class="dashboard">
    <header class="dashboard-header">
      <h1>Bot Dashboard</h1>
      <div class="status-badge" :class="{ running: isRunning, paused: isPaused }">
        <Activity class="status-icon" />
        <span>{{ isRunning ? (isPaused ? 'Paused' : 'Running') : 'Stopped' }}</span>
      </div>
    </header>

    <div class="stats-grid">
      <div class="stat-card" :class="{ positive: dailyPnl >= 0, negative: dailyPnl < 0 }">
        <div class="stat-icon">
          <TrendingUp v-if="dailyPnl >= 0" />
          <TrendingDown v-else />
        </div>
        <div class="stat-content">
          <span class="stat-label">Daily P&L</span>
          <span class="stat-value">{{ formatPnl(dailyPnl) }}</span>
        </div>
      </div>

      <div class="stat-card">
        <div class="stat-icon"><Wallet /></div>
        <div class="stat-content">
          <span class="stat-label">Unrealized P&L</span>
          <span class="stat-value" :class="{ positive: totalUnrealizedPnl >= 0, negative: totalUnrealizedPnl < 0 }">
            {{ formatPnl(totalUnrealizedPnl) }}
          </span>
        </div>
      </div>

      <div class="stat-card">
        <div class="stat-icon"><List /></div>
        <div class="stat-content">
          <span class="stat-label">Open Positions</span>
          <span class="stat-value">{{ positions.length }}</span>
        </div>
      </div>
    </div>

    <div class="positions-section">
      <h2>Open Positions</h2>
      <div v-if="positionsLoading" class="loading">Loading...</div>
      <div v-else-if="positions.length === 0" class="empty">No positions</div>
      <table v-else class="positions-table">
        <thead>
          <tr>
            <th>Symbol</th>
            <th>Side</th>
            <th>Size</th>
            <th>Entry</th>
            <th>P&L</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="pos in positions" :key="pos.symbol + pos.side">
            <td class="symbol">{{ pos.symbol }}</td>
            <td class="side" :class="pos.side.toLowerCase()">{{ pos.side }}</td>
            <td>{{ pos.size.toFixed(4) }}</td>
            <td>${{ formatPrice(pos.entry_price) }}</td>
            <td class="pnl" :class="{ positive: (pos.unrealized_pnl || 0) >= 0, negative: (pos.unrealized_pnl || 0) < 0 }">
              {{ formatPnl(pos.unrealized_pnl || 0) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.dashboard { padding: 24px; max-width: 1200px; margin: 0 auto; }
.dashboard-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.dashboard-header h1 { font-size: 24px; font-weight: 600; color: #1a1a1a; margin: 0; }
.status-badge { display: flex; align-items: center; gap: 8px; padding: 8px 16px; border-radius: 8px; background: #f3f4f6; color: #6b7280; font-size: 14px; font-weight: 500; }
.status-badge.running { background: #dcfce7; color: #16a34a; }
.status-badge.paused { background: #fef3c7; color: #d97706; }
.stats-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin-bottom: 32px; }
.stat-card { display: flex; align-items: center; gap: 16px; padding: 20px; background: white; border-radius: 12px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); border-left: 4px solid #6b7280; }
.stat-card.positive { border-left-color: #22c55e; }
.stat-card.negative { border-left-color: #ef4444; }
.stat-icon { width: 40px; height: 40px; display: flex; align-items: center; justify-content: center; background: #f3f4f6; border-radius: 10px; color: #6b7280; }
.stat-label { font-size: 13px; color: #6b7280; margin-bottom: 4px; }
.stat-value { font-size: 24px; font-weight: 700; color: #1a1a1a; }
.stat-value.positive { color: #22c55e; }
.stat-value.negative { color: #ef4444; }
.positions-section { background: white; border-radius: 12px; padding: 24px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.positions-section h2 { font-size: 18px; font-weight: 600; margin: 0 0 20px 0; }
.loading, .empty { text-align: center; padding: 40px; color: #6b7280; }
.positions-table { width: 100%; border-collapse: collapse; }
.positions-table th { text-align: left; padding: 12px; font-size: 12px; font-weight: 600; color: #6b7280; text-transform: uppercase; border-bottom: 2px solid #e5e7eb; }
.positions-table td { padding: 16px 12px; border-bottom: 1px solid #f3f4f6; font-size: 14px; color: #4b5563; }
.symbol { font-weight: 600; color: #1a1a1a; }
.side.long { color: #22c55e; }
.side.short { color: #ef4444; }
.pnl.positive { color: #22c55e; font-weight: 500; }
.pnl.negative { color: #ef4444; font-weight: 500; }
@media (max-width: 768px) { .stats-grid { grid-template-columns: 1fr; } }
</style>
