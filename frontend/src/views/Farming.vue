<script setup lang="ts">
import { useFarming } from '../api/farming'
import Card from '../components/ui/Card.vue'
import StatValue from '../components/ui/StatValue.vue'
import PageContainer from '../components/layout/PageContainer.vue'

const { farming, loading } = useFarming()
</script>

<template>
  <PageContainer title="Volume Farming">
    <div class="stats-grid">
      <Card>
        <StatValue label="Active Grids" :value="farming?.active_grids || 0" />
      </Card>
      <Card>
        <StatValue label="24h Volume" :value="farming?.volume_24h || 0" prefix="$" format="compact" />
      </Card>
      <Card>
        <StatValue label="7d Volume" :value="farming?.volume_7d || 0" prefix="$" format="compact" />
      </Card>
      <Card>
        <StatValue label="Est. Funding 24h" :value="farming?.estimated_funding_24h || 0" prefix="$" :decimals="2" />
      </Card>
    </div>

    <Card title="Grid Configurations" class="mt-6">
      <div v-if="loading" class="loading">Loading...</div>
      <div v-else-if="!farming?.grid_configs?.length" class="empty">No active grids</div>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>Symbol</th>
            <th>Status</th>
            <th>Levels</th>
            <th>Spread %</th>
            <th>Position Size</th>
            <th>Unrealized P&L</th>
            <th>Volume 24h</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="grid in farming.grid_configs" :key="grid.symbol">
            <td class="symbol">{{ grid.symbol }}</td>
            <td><span :class="['status', grid.status]">{{ grid.status }}</span></td>
            <td>{{ grid.levels }}</td>
            <td>{{ grid.spread_pct }}%</td>
            <td>${{ grid.position_size }}</td>
            <td :class="grid.unrealized_pnl >= 0 ? 'positive' : 'negative'">${{ grid.unrealized_pnl.toFixed(2) }}</td>
            <td>${{ grid.volume_24h.toLocaleString() }}</td>
          </tr>
        </tbody>
      </table>
    </Card>
  </PageContainer>
</template>

<style scoped>
.stats-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--space-4); }
@media (max-width: 1024px) { .stats-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 640px) { .stats-grid { grid-template-columns: 1fr; } }
.mt-6 { margin-top: var(--space-6); }
.loading, .empty { text-align: center; padding: var(--space-8); color: var(--text-muted); }
.data-table { width: 100%; border-collapse: collapse; }
.data-table th { text-align: left; padding: var(--space-3); font-size: var(--text-xs); font-weight: 600; color: var(--text-muted); text-transform: uppercase; border-bottom: 1px solid var(--border); }
.data-table td { padding: var(--space-3); border-bottom: 1px solid var(--border); }
.symbol { font-weight: 600; color: var(--text-primary); }
.status { padding: var(--space-1) var(--space-2); border-radius: var(--radius-md); font-size: var(--text-xs); text-transform: uppercase; }
.status.active { background: var(--status-success-bg); color: var(--accent-success); }
.status.inactive { background: var(--bg-elevated); color: var(--text-muted); }
.positive { color: var(--accent-success); }
.negative { color: var(--accent-danger); }
</style>
