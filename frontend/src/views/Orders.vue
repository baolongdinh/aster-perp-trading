<script setup lang="ts">
import { useOrders } from '../api/orders'
import Card from '../components/ui/Card.vue'
import Badge from '../components/ui/Badge.vue'
import PageContainer from '../components/layout/PageContainer.vue'

const { orders, totalOrders, pendingNotional, loading, cancelOrder } = useOrders()

const getStatusVariant = (status: string) => {
  switch (status) {
    case 'FILLED': return 'success'
    case 'PARTIALLY_FILLED': return 'info'
    case 'CANCELLED': return 'neutral'
    case 'REJECTED': return 'danger'
    default: return 'warning'
  }
}

const filledPercent = (order: any) => {
  if (!order.quantity) return 0
  return Math.round((order.filled_qty / order.quantity) * 100)
}
</script>

<template>
  <PageContainer title="Open Orders">
    <div class="stats-row">
      <Card>
        <div class="stat">
          <span class="label">Total Orders</span>
          <span class="value">{{ totalOrders }}</span>
        </div>
      </Card>
      <Card>
        <div class="stat">
          <span class="label">Pending Notional</span>
          <span class="value">${{ pendingNotional.toLocaleString() }}</span>
        </div>
      </Card>
    </div>

    <Card title="Orders" class="mt-6">
      <div v-if="loading" class="loading">Loading...</div>
      <div v-else-if="orders.length === 0" class="empty">No open orders</div>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>Symbol</th>
            <th>Side</th>
            <th>Type</th>
            <th>Price</th>
            <th>Quantity</th>
            <th>Status</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="order in orders" :key="order.order_id">
            <td class="symbol">{{ order.symbol }}</td>
            <td :class="['side', order.side.toLowerCase()]">{{ order.side }}</td>
            <td>{{ order.type }}</td>
            <td>${{ order.price.toFixed(2) }}</td>
            <td>
              {{ order.filled_qty }}/{{ order.quantity }}
              <div class="progress-bar">
                <div class="progress-fill" :style="{ width: filledPercent(order) + '%' }" />
              </div>
            </td>
            <td><Badge :variant="getStatusVariant(order.status)">{{ order.status }}</Badge></td>
            <td>
              <button class="cancel-btn" @click="cancelOrder(order.order_id)">Cancel</button>
            </td>
          </tr>
        </tbody>
      </table>
    </Card>
  </PageContainer>
</template>

<style scoped>
.stats-row { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--space-4); }
.stat { display: flex; justify-content: space-between; align-items: center; }
.label { color: var(--text-muted); font-size: var(--text-sm); }
.value { font-weight: 700; color: var(--text-primary); }
.mt-6 { margin-top: var(--space-6); }
.loading, .empty { text-align: center; padding: var(--space-8); color: var(--text-muted); }
.data-table { width: 100%; border-collapse: collapse; }
.data-table th { text-align: left; padding: var(--space-3); font-size: var(--text-xs); font-weight: 600; color: var(--text-muted); text-transform: uppercase; border-bottom: 1px solid var(--border); }
.data-table td { padding: var(--space-3); border-bottom: 1px solid var(--border); }
.symbol { font-weight: 600; color: var(--text-primary); }
.side.buy { color: var(--accent-success); }
.side.sell { color: var(--accent-danger); }
.progress-bar { height: 4px; background: var(--bg-elevated); border-radius: var(--radius-full); margin-top: var(--space-1); }
.progress-fill { height: 100%; background: var(--accent-primary); }
.cancel-btn { padding: var(--space-1) var(--space-3); background: var(--status-danger-bg); color: var(--accent-danger); border: none; border-radius: var(--radius-md); cursor: pointer; font-size: var(--text-xs); }
</style>
