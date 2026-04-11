<script setup lang="ts">
import { computed } from 'vue'
import { useRisk } from '../api/risk'
import Card from '../components/ui/Card.vue'
import StatValue from '../components/ui/StatValue.vue'
import PageContainer from '../components/layout/PageContainer.vue'
import { Shield, AlertTriangle } from 'lucide-vue-next'

const { risk } = useRisk()

const pnlProgress = computed(() => {
  if (!risk.value) return 0
  const limit = risk.value.daily_pnl > 0 ? 1000 : -risk.value.daily_pnl // Example limit
  return Math.min(Math.abs(risk.value.daily_pnl) / Math.abs(limit) * 100, 100)
})

const positionProgress = computed(() => {
  if (!risk.value || risk.value.max_open_positions === 0) return 0
  return (risk.value.open_positions / risk.value.max_open_positions) * 100
})

const notionalProgress = computed(() => {
  if (!risk.value || risk.value.max_total_notional === 0) return 0
  return (risk.value.total_notional / risk.value.max_total_notional) * 100
})
</script>

<template>
  <PageContainer title="Risk Metrics">
    <!-- Status Banner -->
    <div v-if="risk?.is_paused" class="risk-banner paused">
      <AlertTriangle class="w-5 h-5" />
      <span>Bot is PAUSED: {{ risk.pause_reason }}</span>
    </div>
    <div v-else class="risk-banner active">
      <Shield class="w-5 h-5" />
      <span>Bot is active - All risk checks passing</span>
    </div>

    <!-- Risk Metrics Grid -->
    <div class="stats-grid">
      <Card>
        <StatValue label="Daily P&L" :value="risk?.daily_pnl || 0" prefix="$" :decimals="2" />
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: pnlProgress + '%', background: (risk?.daily_pnl || 0) >= 0 ? 'var(--accent-success)' : 'var(--accent-danger)' }" />
        </div>
      </Card>

      <Card>
        <StatValue label="Positions" :value="risk?.open_positions || 0" suffix="" />
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: positionProgress + '%' }" />
        </div>
        <span class="limit-text">{{ risk?.open_positions || 0 }} / {{ risk?.max_open_positions || 0 }}</span>
      </Card>

      <Card>
        <StatValue label="Notional Exposure" :value="risk?.total_notional || 0" prefix="$" format="compact" />
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: notionalProgress + '%' }" />
        </div>
        <span class="limit-text">${{ (risk?.total_notional || 0).toLocaleString() }} / ${{ (risk?.max_total_notional || 0).toLocaleString() }}</span>
      </Card>

      <Card>
        <StatValue label="Available Balance" :value="risk?.available_balance || 0" prefix="$" format="currency" />
        <StatValue label="Pending Margin" :value="risk?.pending_margin || 0" prefix="$" format="currency" class="mt-2" />
      </Card>
    </div>

    <!-- Symbol Breakdown -->
    <Card v-if="risk?.positions_by_symbol && Object.keys(risk.positions_by_symbol).length > 0" title="Exposure by Symbol" class="mt-6">
      <table class="data-table">
        <thead>
          <tr>
            <th>Symbol</th>
            <th>Positions</th>
            <th>Notional</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(data, symbol) in risk.positions_by_symbol" :key="symbol">
            <td class="symbol">{{ symbol }}</td>
            <td>{{ data.count }}</td>
            <td>${{ data.notional.toLocaleString() }}</td>
          </tr>
        </tbody>
      </table>
    </Card>
  </PageContainer>
</template>

<style scoped>
.risk-banner {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4);
  border-radius: var(--radius-lg);
  margin-bottom: var(--space-6);
}

.risk-banner.paused {
  background: var(--status-danger-bg);
  color: var(--accent-danger);
}

.risk-banner.active {
  background: var(--status-success-bg);
  color: var(--accent-success);
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-4);
}

@media (max-width: 1024px) { .stats-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 640px) { .stats-grid { grid-template-columns: 1fr; } }

.progress-bar {
  height: 4px;
  background: var(--bg-elevated);
  border-radius: var(--radius-full);
  margin-top: var(--space-3);
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  background: var(--accent-primary);
  transition: width 0.3s ease;
}

.limit-text {
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin-top: var(--space-1);
  display: block;
}

.mt-2 { margin-top: var(--space-2); }
.mt-6 { margin-top: var(--space-6); }

.data-table {
  width: 100%;
  border-collapse: collapse;
}

.data-table th {
  text-align: left;
  padding: var(--space-3);
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  border-bottom: 1px solid var(--border);
}

.data-table td {
  padding: var(--space-3);
  border-bottom: 1px solid var(--border);
}

.symbol { font-weight: 600; color: var(--text-primary); }
</style>
