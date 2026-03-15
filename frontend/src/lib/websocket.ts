import { ref, onMounted, onUnmounted } from 'vue'
import type { WsMessage } from '../types'

const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws'

export function useWebSocket(onMessage: (msg: WsMessage) => void) {
  const socket = ref<WebSocket | null>(null)
  const isConnected = ref(false)
  const error = ref<string | null>(null)
  let reconnectTimer: any = null

  function connect() {
    console.log('Connecting to WebSocket:', WS_URL)
    socket.value = new WebSocket(WS_URL)

    socket.value.onopen = () => {
      console.log('WebSocket Connected')
      isConnected.value = true
      error.value = null
      if (reconnectTimer) clearTimeout(reconnectTimer)
    }

    socket.value.onmessage = (event) => {
      try {
        const msg: WsMessage = JSON.parse(event.data)
        onMessage(msg)
      } catch (err) {
        console.error('Failed to parse WS message:', err)
      }
    }

    socket.value.onclose = () => {
      console.log('WebSocket Disconnected')
      isConnected.value = false
      // Attempt reconnect after 5 seconds
      reconnectTimer = setTimeout(connect, 5000)
    }

    socket.value.onerror = (err) => {
      console.error('WebSocket Error:', err)
      error.value = 'Connection error'
      socket.value?.close()
    }
  }

  onMounted(() => {
    connect()
  })

  onUnmounted(() => {
    if (socket.value) {
      socket.value.onclose = null // Prevent reconnect loop
      socket.value.close()
    }
    if (reconnectTimer) clearTimeout(reconnectTimer)
  })

  return { isConnected, error }
}
