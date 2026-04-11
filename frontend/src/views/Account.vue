<script setup lang="ts">
import { useAccount } from '../api/account'
import Card from '../components/ui/Card.vue'
import StatValue from '../components/ui/StatValue.vue'
import PageContainer from '../components/layout/PageContainer.vue'

const { account } = useAccount()
</script>

<template>
  <PageContainer title="Account">
    <div class="stats-grid">
      <Card>
        <StatValue label="Balance" :value="account?.balance || 0" prefix="$" format="currency" />
      </Card>
      <Card>
        <StatValue label="Equity" :value="account?.equity || 0" prefix="$" format="currency" />
      </Card>
      <Card>
        <StatValue label="Margin Used" :value="account?.margin_used || 0" prefix="$" format="currency" />
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: (account?.margin_ratio || 0) + '%' }" />
        </div>
        <span class="ratio">{{ (account?.margin_ratio || 0).toFixed(2) }}%</span>
      </Card>
      <Card>
        <StatValue label="Realized P&L (Today)" :value="account?.realized_pnl_today || 0" prefix="$" format="currency" />
      </Card>
    </div>

    <Card title="Allocation" class="mt-6">
      <div class="allocation-bars">
        <div class="allocation-item">
          <span class="label">Available</span>
          <div class="bar-container">
            <div class="bar" :style="{ width: '60%', background: 'var(--accent-success)' }" />
          </div>
          <span class="value">${{ (account?.balance || 0).toLocaleString() }}</span>
        </div>
        <div class="allocation-item">
          <span class="label">In Positions</span>
          <div class="bar-container">
            <div class="bar" :style="{ width: '30%', background: 'var(--accent-primary)' }" />
          </div>
          <span class="value">${{ (account?.margin_used || 0).toLocaleString() }}</span>
        </div>
        <div class="allocation-item">
          <span class="label">Pending Orders</span>
          <div class="bar-container">
            <div class="bar" :style="{ width: '10%', background: 'var(--accent-warning)' }" />
          </div>
          <span class="value">${{ (account?.margin_used || 0 * 0.1).toLocaleString() }}</span>
        </div>
      </div>
    </Card>
  </PageContainer>
</template>

<style scoped>
.stats-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--space-4); }
@media (max-width: 1024px) { .stats-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 640px) { .stats-grid { grid-template-columns: 1fr; } }
.progress-bar { height: 4px; background: var(--bg-elevated); border-radius: var(--radius-full); margin-top: var(--space-2); overflow: hidden; }
.progress-fill { height: 100%; background: var(--accent-primary); transition: width 0.3s ease; }
.ratio { font-size: var(--text-xs); color: var(--text-muted); margin-top: var(--space-1); display: block; }
.mt-6 { margin-top: var(--space-6); }
.allocation-bars { display: flex; flex-direction: column; gap: var(--space-4); }
.allocation-item { display: grid; grid-template-columns: 100px 1fr 100px; align-items: center; gap: var(--space-4); }
.label { font-size: var(--text-sm); color: var(--text-secondary); }
.bar-container { height: 8px; background: var(--bg-elevated); border-radius: var(--radius-full); overflow: hidden; }
.bar { height: 100%; border-radius: var(--radius-full); }
.value { font-size: var(--text-sm); color: var(--text-primary); text-align: right; font-family: var(--font-mono); }
</style>
