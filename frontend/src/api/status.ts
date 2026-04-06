import { ref, onMounted, onUnmounted } from 'vue'
import { apiClient } from './client'
import type { BotStatus } from './types'

export function useStatus() {
  const status = ref<BotStatus | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  const fetchStatus = async () => {
    loading.value = true
    error.value = null
    try {
      const response = await apiClient.get('/api/v1/status')
      status.value = response.data
    } catch (err: any) {
      error.value = err.message || 'Failed to fetch status'
    } finally {
      loading.value = false
    }
  }

  // Auto-refresh every 3 seconds
  let interval: number | undefined
  onMounted(() => {
    fetchStatus()
    interval = setInterval(fetchStatus, 3000)
  })
  onUnmounted(() => clearInterval(interval))

  return { status, loading, error, refresh: fetchStatus }
}
