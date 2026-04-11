import { useAutoRefresh } from '../composables/useAutoRefresh'
import { apiClient } from './client'
import type { FarmingMetrics } from './types'

export function useFarming() {
  const fetchFarming = async (): Promise<FarmingMetrics> => {
    const response = await apiClient.get('/api/v1/farming')
    return response.data
  }

  const { data: farming, loading, error, refresh } = useAutoRefresh<FarmingMetrics>(fetchFarming, 5000)

  return { farming, loading, error, refresh }
}
