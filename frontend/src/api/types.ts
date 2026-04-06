// API Response Types matching backend structures

export interface BotStatus {
  running: boolean
  paused: boolean
  daily_pnl: number
  open_pos: number
  server_time: number
}

export interface Position {
  symbol: string
  side: 'LONG' | 'SHORT'
  size: number
  entry_price: number
  mark_price?: number
  unrealized_pnl?: number
  leverage?: number
}

export interface Strategy {
  name: string
  enabled: boolean
  symbols: string[]
}

export interface Order {
  order_id: string
  symbol: string
  side: 'BUY' | 'SELL'
  type: string
  price: number
  quantity: number
  status: string
}

export interface Metrics {
  daily_pnl: number
  open_positions: number
  is_paused: boolean
}

export interface ActivityEntry {
  id: string
  trace_id?: string
  timestamp: string
  event_type: string
  severity: 'INFO' | 'WARN' | 'ERROR' | 'CRITICAL'
  context: {
    symbol?: string
    strategy_id?: string
    order_id?: string
    strategy_name?: string
    position_id?: string
  }
  payload: Record<string, any>
  metadata?: {
    source_file?: string
    source_line?: number
    latency_ms?: number
  }
}

export interface ActivityQueryResponse {
  entries: ActivityEntry[]
  total: number
  has_more: boolean
}
