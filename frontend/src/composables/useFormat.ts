export function useFormat() {
  /**
   * Format a number as currency (USD)
   */
  const formatCurrency = (value: number, decimals = 2): string => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals
    }).format(value)
  }

  /**
   * Format a number with specified decimals
   */
  const formatNumber = (value: number, decimals = 2, compact = false): string => {
    if (compact && Math.abs(value) >= 1000) {
      return new Intl.NumberFormat('en-US', {
        notation: 'compact',
        maximumFractionDigits: 1
      }).format(value)
    }
    
    return new Intl.NumberFormat('en-US', {
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals
    }).format(value)
  }

  /**
   * Format as percentage
   */
  const formatPercent = (value: number, decimals = 2): string => {
    return `${value.toFixed(decimals)}%`
  }

  /**
   * Format price with appropriate decimals based on magnitude
   */
  const formatPrice = (value: number, symbol?: string): string => {
    let decimals = 2
    if (Math.abs(value) < 1) decimals = 6
    else if (Math.abs(value) < 10) decimals = 4
    else if (Math.abs(value) < 1000) decimals = 2
    else decimals = 0
    
    const formatted = formatNumber(value, decimals)
    return symbol ? `${symbol} ${formatted}` : formatted
  }

  /**
   * Format PnL with sign and color class
   */
  const formatPnL = (value: number, showSign = true): { text: string; class: string } => {
    const sign = showSign ? (value >= 0 ? '+' : '-') : ''
    const text = `${sign}$${Math.abs(value).toFixed(2)}`
    const cls = value > 0 ? 'positive' : value < 0 ? 'negative' : 'neutral'
    return { text, class: cls }
  }

  /**
   * Format date
   */
  const formatDate = (date: Date | string | number, format: 'short' | 'long' | 'time' = 'short'): string => {
    const d = new Date(date)
    
    switch (format) {
      case 'time':
        return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
      case 'long':
        return d.toLocaleDateString('en-US', { year: 'numeric', month: 'long', day: 'numeric' })
      default:
        return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    }
  }

  return {
    formatCurrency,
    formatNumber,
    formatPercent,
    formatPrice,
    formatPnL,
    formatDate
  }
}
