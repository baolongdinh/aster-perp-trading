import { useAutoRefresh } from '../composables/useAutoRefresh'
import { apiClient } from './client'
import type { OrdersResponse } from './types'

export function useOrders() {
  const fetchOrders = async (): Promise<OrdersResponse> => {
    const response = await apiClient.get('/api/v1/orders')
    return response.data
  }

  const cancelOrder = async (orderId: string): Promise<boolean> => {
    try {
      await apiClient.post(`/api/v1/orders/${orderId}/cancel`)
      return true
    } catch (err) {
      console.error('Failed to cancel order:', err)
      return false
    }
  }

  const { data: ordersResponse, loading, error, refresh } = useAutoRefresh<OrdersResponse>(fetchOrders, 3000)

  return {
    orders: computed(() => ordersResponse.value?.orders || []),
    totalOrders: computed(() => ordersResponse.value?.total_orders || 0),
    pendingNotional: computed(() => ordersResponse.value?.pending_notional || 0),
    loading,
    error,
    refresh,
    cancelOrder
  }
}

import { computed } from 'vue'
