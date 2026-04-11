import { useAutoRefresh } from '../composables/useAutoRefresh'
import { apiClient } from './client'
import type { RiskMetrics } from './types'

export function useRisk() {
  const fetchRisk = async (): Promise<RiskMetrics> => {
    const response = await apiClient.get('/api/v1/risk')
    return response.data
  }

  const { data: risk, loading, error, refresh } = useAutoRefresh<RiskMetrics>(fetchRisk, 3000)

  return { risk, loading, error, refresh }
}
