import { useAutoRefresh } from '../composables/useAutoRefresh'
import { apiClient } from './client'
import type { Account } from './types'

export function useAccount() {
  const fetchAccount = async (): Promise<Account> => {
    const response = await apiClient.get('/api/v1/account')
    return response.data
  }

  const { data: account, loading, error, refresh } = useAutoRefresh<Account>(fetchAccount, 5000)

  return { account, loading, error, refresh }
}
