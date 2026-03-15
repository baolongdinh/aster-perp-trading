export interface Position {
  symbol: string
  side: 'LONG' | 'SHORT'
  leverage: number
  entryPrice: number
  markPrice: number
  unrealizedPnl: number
  unrealizedPnlPct: number
}

export interface Strategy {
  name: string
  enabled: boolean
  winRate: number
  profitFactor: number
  lastSignal?: string
  symbols: string[]
  params: Record<string, any>
}

export interface Trade {
  id: string
  time: string
  symbol: string
  side: string
  strategy: string
  price: number
  quantity: number
  realizedPnl: number
  status: string
}

export interface BotStatus {
  online: boolean
  accountEquity: number
  dailyPnl: number
  drawdownPct: number
  uptime: string
  version: string
}

export type WsMessageType = 'POSITION_UPDATE' | 'STRATEGY_UPDATE' | 'TRADE_FILL' | 'LOG_EVENT' | 'STATUS_UPDATE'

export interface WsMessage {
  type: WsMessageType
  data: any
}
