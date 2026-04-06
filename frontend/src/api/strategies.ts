import { ref, onMounted } from 'vue'
import { apiClient } from './client'
import type { Strategy } from './types'

export function useStrategies() {
  const strategies = ref<Strategy[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  const fetchStrategies = async () => {
    loading.value = true
    error.value = null
    try {
      const response = await apiClient.get('/api/v1/strategies')
      strategies.value = response.data || []
    } catch (err: any) {
      error.value = err.message || 'Failed to fetch strategies'
    } finally {
      loading.value = false
    }
  }

  const enableStrategy = async (name: string) => {
    try {
      await apiClient.post(`/api/v1/strategies/${name}/enable`)
      await fetchStrategies()
      return true
    } catch (err: any) {
      error.value = err.message || `Failed to enable ${name}`
      return false
    }
  }

  const disableStrategy = async (name: string) => {
    try {
      await apiClient.post(`/api/v1/strategies/${name}/disable`)
      await fetchStrategies()
      return true
    } catch (err: any) {
      error.value = err.message || `Failed to disable ${name}`
      return false
    }
  }

  onMounted(fetchStrategies)

  return { strategies, loading, error, refresh: fetchStrategies, enableStrategy, disableStrategy }
}
