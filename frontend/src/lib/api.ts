import axios from 'axios'
import type { Position, Strategy, Trade, BotStatus } from '../types'

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api'

const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 5000,
})

export const asterApi = {
  getStatus: () => api.get<BotStatus>('/status'),
  getPositions: () => api.get<Position[]>('/positions'),
  getStrategies: () => api.get<Strategy[]>('/strategies'),
  toggleStrategy: (id: string, enabled: boolean) => api.post(`/strategies/${id}/toggle`, { enabled }),
  getTrades: (params?: any) => api.get<Trade[]>('/trades', { params }),
  updateConfig: (config: any) => api.post('/config', config),
}

export default asterApi
