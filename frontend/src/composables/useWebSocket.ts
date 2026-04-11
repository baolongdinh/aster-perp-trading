import { ref, onMounted, onUnmounted } from 'vue'

export type WsMessageType = 'activity' | 'alert' | 'metrics' | 'position' | 'order' | 'account'

export interface WsMessage {
  type: WsMessageType
  payload: any
  timestamp?: string
}

export function useWebSocket(
  url: string,
  autoReconnect: boolean = true,
  reconnectInterval: number = 5000,
  maxReconnectAttempts: number = 10
) {
  const ws = ref<WebSocket | null>(null)
  const isConnected = ref(false)
  const isConnecting = ref(false)
  const lastMessage = ref<WsMessage | null>(null)
  const error = ref<string | null>(null)
  const reconnectCount = ref(0)
  
  let reconnectTimeout: number | null = null
  let messageHandlers: Map<WsMessageType, ((payload: any) => void)[]> = new Map()

  const connect = () => {
    if (isConnecting.value || isConnected.value) return
    
    isConnecting.value = true
    error.value = null
    
    try {
      ws.value = new WebSocket(url)
      
      ws.value.onopen = () => {
        isConnected.value = true
        isConnecting.value = false
        reconnectCount.value = 0
        console.log('WebSocket connected')
      }
      
      ws.value.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data) as WsMessage
          lastMessage.value = msg
          
          // Call registered handlers
          const handlers = messageHandlers.get(msg.type)
          if (handlers) {
            handlers.forEach(handler => handler(msg.payload))
          }
        } catch (e) {
          console.error('Failed to parse WebSocket message:', e)
        }
      }
      
      ws.value.onclose = () => {
        isConnected.value = false
        isConnecting.value = false
        ws.value = null
        
        if (autoReconnect && reconnectCount.value < maxReconnectAttempts) {
          reconnectCount.value++
          reconnectTimeout = window.setTimeout(connect, reconnectInterval)
        }
      }
      
      ws.value.onerror = (err) => {
        console.error('WebSocket error:', err)
        error.value = 'WebSocket connection error'
        isConnecting.value = false
      }
    } catch (err: any) {
      error.value = err.message || 'Failed to connect'
      isConnecting.value = false
    }
  }

  const disconnect = () => {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout)
      reconnectTimeout = null
    }
    
    if (ws.value) {
      ws.value.close()
      ws.value = null
    }
    
    isConnected.value = false
    isConnecting.value = false
  }

  const send = (message: any): boolean => {
    if (!ws.value || !isConnected.value) {
      console.error('WebSocket not connected')
      return false
    }
    
    try {
      ws.value.send(JSON.stringify(message))
      return true
    } catch (err) {
      console.error('Failed to send WebSocket message:', err)
      return false
    }
  }

  const subscribe = (type: WsMessageType, handler: (payload: any) => void) => {
    if (!messageHandlers.has(type)) {
      messageHandlers.set(type, [])
    }
    messageHandlers.get(type)!.push(handler)
    
    // Send subscribe message
    send({ type: 'subscribe', channel: type })
  }

  const unsubscribe = (type: WsMessageType, handler?: (payload: any) => void) => {
    if (!handler) {
      messageHandlers.delete(type)
    } else {
      const handlers = messageHandlers.get(type)
      if (handlers) {
        const index = handlers.indexOf(handler)
        if (index > -1) {
          handlers.splice(index, 1)
        }
      }
    }
    
    // Send unsubscribe message
    send({ type: 'unsubscribe', channel: type })
  }

  onMounted(() => {
    if (autoReconnect) {
      connect()
    }
  })

  onUnmounted(disconnect)

  return {
    ws,
    isConnected,
    isConnecting,
    lastMessage,
    error,
    reconnectCount,
    connect,
    disconnect,
    send,
    subscribe,
    unsubscribe
  }
}
