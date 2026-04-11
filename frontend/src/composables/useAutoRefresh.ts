import { ref, onMounted, onUnmounted } from 'vue'

export function useAutoRefresh<T>(
  fetchFn: () => Promise<T>,
  interval: number = 3000
) {
  const data = ref<T | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const isPaused = ref(false)
  
  let intervalId: number | null = null

  const fetch = async () => {
    if (isPaused.value) return
    
    loading.value = true
    error.value = null
    
    try {
      const result = await fetchFn()
      data.value = result
    } catch (err: any) {
      error.value = err.message || 'Failed to fetch data'
      console.error('AutoRefresh error:', err)
    } finally {
      loading.value = false
    }
  }

  const start = () => {
    if (intervalId) return
    fetch()
    intervalId = window.setInterval(fetch, interval)
  }

  const stop = () => {
    if (intervalId) {
      clearInterval(intervalId)
      intervalId = null
    }
  }

  const pause = () => {
    isPaused.value = true
  }

  const resume = () => {
    isPaused.value = false
    fetch()
  }

  const refresh = () => {
    return fetch()
  }

  onMounted(start)
  onUnmounted(stop)

  return {
    data,
    loading,
    error,
    isPaused,
    refresh,
    start,
    stop,
    pause,
    resume
  }
}
