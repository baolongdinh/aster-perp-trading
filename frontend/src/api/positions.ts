import { ref, onMounted, onUnmounted } from 'vue'
import { apiClient } from './client'
import type { Position } from './types'

export function usePositions() {
  const positions = ref<Position[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  const fetchPositions = async () => {
    loading.value = true
    error.value = null
    try {
      const response = await apiClient.get('/api/v1/positions')
      positions.value = response.data || []
    } catch (err: any) {
      error.value = err.message || 'Failed to fetch positions'
    } finally {
      loading.value = false
    }
  }

  // Auto-refresh every 3 seconds
  let interval: number | undefined
  onMounted(() => {
    fetchPositions()
    interval = setInterval(fetchPositions, 3000)
  })
  onUnmounted(() => clearInterval(interval))

  return { positions, loading, error, refresh: fetchPositions }
}
