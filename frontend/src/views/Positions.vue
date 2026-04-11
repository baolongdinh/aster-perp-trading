<script setup lang="ts">
import { computed } from 'vue'
import { usePositions } from '../api/positions'
import Card from '../components/ui/Card.vue'
import PnLDisplay from '../components/ui/PnLDisplay.vue'
import PageContainer from '../components/layout/PageContainer.vue'
import { TrendingUp, TrendingDown } from 'lucide-vue-next'

const { positions, loading } = usePositions()

const totalUnrealized = computed(() => {
  return positions.value.reduce((sum, p) => sum + (p.unrealized_pnl || 0), 0)
})

const longCount = computed(() => positions.value.filter(p => p.side === 'LONG').length)
const shortCount = computed(() => positions.value.filter(p => p.side === 'SHORT').length)
</script>

<template>
  <PageContainer title="Positions">
    <!-- Summary Cards -->
    <div class="stats-row">
      <Card>
        <div class="summary-item">
          <span class="label">Total Positions</span>
          <span class="value">{{ positions.length }}</span>
        </div>
      </Card>
      <Card>
        <div class="summary-item">
          <TrendingUp class="icon long" />
          <span class="label">Long</span>
          <span class="value long">{{ longCount }}</span>
        </div>
      </Card>
      <Card>
        <div class="summary-item">
          <TrendingDown class="icon short" />
          <span class="label">Short</span>
          <span class="value short">{{ shortCount }}</span>
        </div>
      </Card>
      <Card>
        <div class="summary-item">
          <span class="label">Total Unrealized</span>
          <PnLDisplay :value="totalUnrealized" />
        </div>
      </Card>
    </div>

    <!-- Positions Table -->
    <Card title="Open Positions" class="mt-6">
      <div v-if="loading" class="loading">Loading...</div>
      <div v-else-if="positions.length === 0" class="empty">No open positions</div>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>Symbol</th>
            <th>Side</th>
            <th>Size</th>
            <th>Entry Price</th>
            <th>Mark Price</th>
            <th>Leverage</th>
            <th>Unrealized P&L</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="pos in positions" :key="pos.symbol + pos.side">
            <td class="symbol">{{ pos.symbol }}</td>
            <td :class="['side', pos.side.toLowerCase()]">{{ pos.side }}</td>
            <td>{{ pos.size.toFixed(4) }}</td>
            <td>${{ pos.entry_price.toFixed(2) }}</td>
            <td>${{ (pos.mark_price || pos.entry_price).toFixed(2) }}</td>
            <td>{{ pos.leverage || '-' }}x</td>
            <td><PnLDisplay :value="pos.unrealized_pnl || 0" /></td>
          </tr>
        </tbody>
      </table>
    </Card>
  </PageContainer>
</template>

<style scoped>
.stats-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-4);
}

@media (max-width: 1024px) {
  .stats-row { grid-template-columns: repeat(2, 1fr); }
}

@media (max-width: 640px) {
  .stats-row { grid-template-columns: 1fr; }
}

.summary-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.label {
  color: var(--text-muted);
  font-size: var(--text-sm);
}

.value {
  font-weight: 700;
  color: var(--text-primary);
  margin-left: auto;
}

.value.long { color: var(--accent-success); }
.value.short { color: var(--accent-danger); }

.icon { width: 16px; height: 16px; }
.icon.long { color: var(--accent-success); }
.icon.short { color: var(--accent-danger); }

.mt-6 { margin-top: var(--space-6); }

.loading, .empty {
  text-align: center;
  padding: var(--space-8);
  color: var(--text-muted);
}

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
.side.long { color: var(--accent-success); }
.side.short { color: var(--accent-danger); }
</style>
