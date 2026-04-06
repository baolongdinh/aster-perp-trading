<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { format } from 'date-fns'

// Types matching backend activity log
interface LogEntry {
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

// State
const entries = ref<LogEntry[]>([])
const isConnected = ref(false)
const selectedSeverity = ref<string>('')
const selectedEventType = ref<string>('')
const searchQuery = ref('')
const ws = ref<WebSocket | null>(null)
const showDetails = ref<LogEntry | null>(null)

// Event type options
const eventTypes = [
  { value: '', label: 'All Types' },
  { value: 'ORDER_PLACED', label: 'Order Placed' },
  { value: 'ORDER_FILLED', label: 'Order Filled' },
  { value: 'ORDER_CANCELLED', label: 'Order Cancelled' },
  { value: 'ORDER_REJECTED', label: 'Order Rejected' },
  { value: 'GRID_CREATED', label: 'Grid Created' },
  { value: 'POSITION_OPENED', label: 'Position Opened' },
  { value: 'POSITION_CLOSED', label: 'Position Closed' },
  { value: 'RISK_TRIGGERED', label: 'Risk Triggered' },
  { value: 'BOT_STARTED', label: 'Bot Started' },
  { value: 'BOT_STOPPED', label: 'Bot Stopped' },
]

// Filtered entries
const filteredEntries = computed(() => {
  let result = entries.value
  
  if (selectedSeverity.value) {
    result = result.filter(e => e.severity === selectedSeverity.value)
  }
  
  if (selectedEventType.value) {
    result = result.filter(e => e.event_type === selectedEventType.value)
  }
  
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    result = result.filter(e => 
      e.event_type.toLowerCase().includes(q) ||
      e.context.symbol?.toLowerCase().includes(q) ||
      e.context.strategy_id?.toLowerCase().includes(q) ||
      JSON.stringify(e.payload).toLowerCase().includes(q)
    )
  }
  
  return result
})

// Stats
const stats = computed(() => ({
  total: entries.value.length,
  info: entries.value.filter(e => e.severity === 'INFO').length,
  warn: entries.value.filter(e => e.severity === 'WARN').length,
  error: entries.value.filter(e => e.severity === 'ERROR' || e.severity === 'CRITICAL').length,
}))

// Severity colors
const severityColors: Record<string, string> = {
  INFO: 'bg-blue-500',
  WARN: 'bg-yellow-500',
  ERROR: 'bg-red-500',
  CRITICAL: 'bg-purple-500',
}

// Format timestamp
function formatTime(ts: string) {
  return format(new Date(ts), 'HH:mm:ss.SSS')
}

// Connect WebSocket
function connect() {
  const apiHost = import.meta.env.VITE_API_URL?.replace(/^http/, 'ws') || 'ws://localhost:8080'
  const wsUrl = `${apiHost}/ws`
  ws.value = new WebSocket(wsUrl)
  
  ws.value.onopen = () => {
    isConnected.value = true
    // Subscribe to activity stream
    ws.value?.send(JSON.stringify({
      type: 'subscribe',
      filter: {
        event_types: [],
        min_severity: 'INFO'
      }
    }))
  }
  
  ws.value.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      if (msg.type === 'activity' || msg.type === 'alert') {
        entries.value.unshift(msg.payload)
        // Keep only last 1000 entries
        if (entries.value.length > 1000) {
          entries.value = entries.value.slice(0, 1000)
        }
      } else if (msg.type === 'activity_batch') {
        const batch = msg.payload as LogEntry[]
        entries.value.unshift(...batch)
        if (entries.value.length > 1000) {
          entries.value = entries.value.slice(0, 1000)
        }
      }
    } catch (e) {
      console.error('Failed to parse WebSocket message:', e)
    }
  }
  
  ws.value.onclose = () => {
    isConnected.value = false
    // Reconnect after 5 seconds
    setTimeout(connect, 5000)
  }
  
  ws.value.onerror = (err) => {
    console.error('WebSocket error:', err)
    isConnected.value = false
  }
}

// Disconnect
function disconnect() {
  ws.value?.close()
  ws.value = null
  isConnected.value = false
}

// Clear logs
function clearLogs() {
  entries.value = []
}

// Export logs
function exportLogs() {
  const data = JSON.stringify(entries.value, null, 2)
  const blob = new Blob([data], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `activity-logs-${format(new Date(), 'yyyy-MM-dd-HHmmss')}.json`
  a.click()
  URL.revokeObjectURL(url)
}

// Fetch historical logs
async function fetchHistorical() {
  try {
    const apiUrl = import.meta.env.VITE_API_URL || 'http://localhost:8080'
    const response = await fetch(`${apiUrl}/api/v1/activity?limit=100`)
    const data = await response.json()
    if (data.entries) {
      entries.value = [...data.entries, ...entries.value]
    }
  } catch (e) {
    console.error('Failed to fetch historical logs:', e)
  }
}

onMounted(() => {
  connect()
  fetchHistorical()
})

onUnmounted(() => {
  disconnect()
})
</script>

<template>
  <div class="activity-observer">
    <!-- Header -->
    <header class="header">
      <div class="header-left">
        <h1>Bot Activity Observer</h1>
        <span class="connection-status" :class="{ connected: isConnected }">
          {{ isConnected ? '● Live' : '● Disconnected' }}
        </span>
      </div>
      <div class="header-right">
        <button @click="clearLogs" class="btn btn-secondary">Clear</button>
        <button @click="exportLogs" class="btn btn-primary">Export</button>
      </div>
    </header>

    <!-- Stats Cards -->
    <div class="stats-grid">
      <div class="stat-card">
        <span class="stat-value">{{ stats.total }}</span>
        <span class="stat-label">Total Events</span>
      </div>
      <div class="stat-card info">
        <span class="stat-value">{{ stats.info }}</span>
        <span class="stat-label">Info</span>
      </div>
      <div class="stat-card warn">
        <span class="stat-value">{{ stats.warn }}</span>
        <span class="stat-label">Warnings</span>
      </div>
      <div class="stat-card error">
        <span class="stat-value">{{ stats.error }}</span>
        <span class="stat-label">Errors</span>
      </div>
    </div>

    <!-- Filters -->
    <div class="filters">
      <input
        v-model="searchQuery"
        type="text"
        placeholder="Search events..."
        class="search-input"
      />
      <select v-model="selectedEventType" class="filter-select">
        <option v-for="type in eventTypes" :key="type.value" :value="type.value">
          {{ type.label }}
        </option>
      </select>
      <select v-model="selectedSeverity" class="filter-select">
        <option value="">All Severities</option>
        <option value="INFO">Info</option>
        <option value="WARN">Warning</option>
        <option value="ERROR">Error</option>
        <option value="CRITICAL">Critical</option>
      </select>
    </div>

    <!-- Activity Log Table -->
    <div class="log-container">
      <table class="log-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Severity</th>
            <th>Event Type</th>
            <th>Symbol</th>
            <th>Strategy</th>
            <th>Details</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="entry in filteredEntries"
            :key="entry.id"
            @click="showDetails = entry"
            :class="{ 
              'severity-error': entry.severity === 'ERROR' || entry.severity === 'CRITICAL',
              'severity-warn': entry.severity === 'WARN'
            }"
          >
            <td class="time-cell" :title="entry.timestamp">
              {{ formatTime(entry.timestamp) }}
            </td>
            <td>
              <span class="severity-badge" :class="severityColors[entry.severity]">
                {{ entry.severity }}
              </span>
            </td>
            <td class="event-type">{{ entry.event_type }}</td>
            <td>{{ entry.context.symbol || '-' }}</td>
            <td>{{ entry.context.strategy_name || entry.context.strategy_id || '-' }}</td>
            <td class="details-cell">
              <span v-if="entry.payload.order_id">Order: {{ entry.payload.order_id.slice(-8) }}</span>
              <span v-else-if="entry.payload.filled_price">@ {{ entry.payload.filled_price }}</span>
              <span v-else-if="entry.payload.price">@ {{ entry.payload.price }}</span>
              <span v-else>View</span>
            </td>
          </tr>
        </tbody>
      </table>
      
      <div v-if="filteredEntries.length === 0" class="empty-state">
        No activity logs to display
      </div>
    </div>

    <!-- Detail Modal -->
    <div v-if="showDetails" class="modal-overlay" @click="showDetails = null">
      <div class="modal-content" @click.stop>
        <div class="modal-header">
          <h3>Event Details</h3>
          <button @click="showDetails = null" class="btn-close">×</button>
        </div>
        <div class="modal-body">
          <div class="detail-row">
            <label>ID:</label>
            <code>{{ showDetails.id }}</code>
          </div>
          <div class="detail-row">
            <label>Timestamp:</label>
            <span>{{ showDetails.timestamp }}</span>
          </div>
          <div class="detail-row">
            <label>Event Type:</label>
            <span>{{ showDetails.event_type }}</span>
          </div>
          <div class="detail-row">
            <label>Severity:</label>
            <span :class="severityColors[showDetails.severity]" class="severity-badge">
              {{ showDetails.severity }}
            </span>
          </div>
          <div v-if="showDetails.trace_id" class="detail-row">
            <label>Trace ID:</label>
            <code>{{ showDetails.trace_id }}</code>
          </div>
          <div v-if="showDetails.context.symbol" class="detail-row">
            <label>Symbol:</label>
            <span>{{ showDetails.context.symbol }}</span>
          </div>
          <div class="detail-section">
            <label>Payload:</label>
            <pre class="payload-json">{{ JSON.stringify(showDetails.payload, null, 2) }}</pre>
          </div>
          <div v-if="showDetails.metadata" class="detail-section">
            <label>Metadata:</label>
            <pre class="payload-json">{{ JSON.stringify(showDetails.metadata, null, 2) }}</pre>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.activity-observer {
  padding: 20px;
  max-width: 1400px;
  margin: 0 auto;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
}

/* Header */
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  padding-bottom: 15px;
  border-bottom: 1px solid #e0e0e0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 15px;
}

.header h1 {
  margin: 0;
  font-size: 24px;
  font-weight: 600;
  color: #1a1a1a;
}

.connection-status {
  font-size: 12px;
  color: #999;
  font-weight: 500;
}

.connection-status.connected {
  color: #22c55e;
}

.header-right {
  display: flex;
  gap: 10px;
}

/* Buttons */
.btn {
  padding: 8px 16px;
  border-radius: 6px;
  border: none;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-primary {
  background: #3b82f6;
  color: white;
}

.btn-primary:hover {
  background: #2563eb;
}

.btn-secondary {
  background: #f3f4f6;
  color: #374151;
}

.btn-secondary:hover {
  background: #e5e7eb;
}

/* Stats Grid */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 15px;
  margin-bottom: 20px;
}

.stat-card {
  background: #f9fafb;
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  border-left: 4px solid #6b7280;
}

.stat-card.info {
  border-left-color: #3b82f6;
}

.stat-card.warn {
  border-left-color: #f59e0b;
}

.stat-card.error {
  border-left-color: #ef4444;
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
  color: #1a1a1a;
}

.stat-label {
  font-size: 13px;
  color: #6b7280;
  margin-top: 4px;
}

/* Filters */
.filters {
  display: flex;
  gap: 12px;
  margin-bottom: 15px;
}

.search-input {
  flex: 1;
  padding: 10px 14px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 14px;
}

.filter-select {
  padding: 10px 14px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 14px;
  background: white;
  min-width: 150px;
}

/* Log Table */
.log-container {
  background: white;
  border-radius: 8px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  overflow: hidden;
}

.log-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.log-table th {
  background: #f9fafb;
  padding: 12px 16px;
  text-align: left;
  font-weight: 600;
  color: #374151;
  border-bottom: 1px solid #e5e7eb;
  white-space: nowrap;
}

.log-table td {
  padding: 10px 16px;
  border-bottom: 1px solid #f3f4f6;
  color: #4b5563;
}

.log-table tbody tr {
  cursor: pointer;
  transition: background 0.15s;
}

.log-table tbody tr:hover {
  background: #f9fafb;
}

.log-table tbody tr.severity-error {
  background: #fef2f2;
}

.log-table tbody tr.severity-warn {
  background: #fffbeb;
}

.time-cell {
  font-family: 'Monaco', 'Menlo', monospace;
  font-size: 12px;
  color: #6b7280;
  white-space: nowrap;
}

.event-type {
  font-weight: 500;
  color: #1a1a1a;
}

.details-cell {
  font-size: 12px;
  color: #6b7280;
}

/* Severity Badge */
.severity-badge {
  display: inline-block;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  color: white;
}

.bg-blue-500 { background: #3b82f6; }
.bg-yellow-500 { background: #f59e0b; }
.bg-red-500 { background: #ef4444; }
.bg-purple-500 { background: #8b5cf6; }

/* Empty State */
.empty-state {
  padding: 40px;
  text-align: center;
  color: #9ca3af;
}

/* Modal */
.modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  padding: 20px;
}

.modal-content {
  background: white;
  border-radius: 12px;
  width: 100%;
  max-width: 600px;
  max-height: 80vh;
  overflow: auto;
  box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.1);
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid #e5e7eb;
}

.modal-header h3 {
  margin: 0;
  font-size: 18px;
}

.btn-close {
  background: none;
  border: none;
  font-size: 24px;
  cursor: pointer;
  color: #6b7280;
  padding: 0;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
}

.btn-close:hover {
  background: #f3f4f6;
}

.modal-body {
  padding: 20px;
}

.detail-row {
  display: flex;
  padding: 8px 0;
  border-bottom: 1px solid #f3f4f6;
}

.detail-row label {
  width: 100px;
  font-weight: 500;
  color: #6b7280;
  font-size: 13px;
}

.detail-row code {
  background: #f3f4f6;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 12px;
  font-family: 'Monaco', 'Menlo', monospace;
}

.detail-section {
  margin-top: 16px;
}

.detail-section label {
  display: block;
  font-weight: 500;
  color: #6b7280;
  font-size: 13px;
  margin-bottom: 8px;
}

.payload-json {
  background: #1f2937;
  color: #e5e7eb;
  padding: 12px;
  border-radius: 6px;
  font-size: 12px;
  font-family: 'Monaco', 'Menlo', monospace;
  overflow-x: auto;
  max-height: 200px;
  overflow-y: auto;
}
</style>
