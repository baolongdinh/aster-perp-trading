import { useAutoRefresh } from '../composables/useAutoRefresh'
import { apiClient } from './client'
import type { DailySummary } from './types'

export function useSummary() {
  const fetchSummary = async (): Promise<DailySummary> => {
    const response = await apiClient.get('/api/v1/summary')
    return response.data
  }

  const { data: summary, loading, error, refresh } = useAutoRefresh<DailySummary>(fetchSummary, 10000)

  return { summary, loading, error, refresh }
}
