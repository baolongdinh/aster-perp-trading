<script setup lang="ts">
interface Props {
  botStatus?: 'online' | 'offline' | 'paused'
  apiEndpoint?: string
  equity?: number
  userInitial?: string
}

const props = withDefaults(defineProps<Props>(), {
  botStatus: 'online',
  apiEndpoint: 'fapi.asterdex.com',
  equity: 0,
  userInitial: 'A'
})

const statusConfig = {
  online: { color: 'var(--accent-success)', label: 'BOT ONLINE', pulse: true },
  offline: { color: 'var(--accent-danger)', label: 'BOT OFFLINE', pulse: false },
  paused: { color: 'var(--accent-warning)', label: 'BOT PAUSED', pulse: false }
}

const currentStatus = statusConfig[props.botStatus]

const formattedEquity = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2
}).format(props.equity)
</script>

<template>
  <header class="header">
    <div class="header-left">
      <div class="status-indicator">
        <span 
          class="status-dot"
          :class="{ 'animate-pulse': currentStatus.pulse }"
          :style="{ backgroundColor: currentStatus.color }"
        />
        <span class="status-label" :style="{ color: currentStatus.color }">
          {{ currentStatus.label }}
        </span>
      </div>
      <div class="divider" />
      <span class="api-endpoint">API: {{ props.apiEndpoint }}</span>
    </div>
    
    <div class="header-right">
      <div class="equity-display">
        <div class="equity-label">Account Equity</div>
        <div class="equity-value">{{ formattedEquity }}</div>
      </div>
      <div class="avatar">
        {{ props.userInitial }}
      </div>
    </div>
  </header>
</template>

<style scoped>
.header {
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--space-8);
  border-bottom: 1px solid var(--border);
  background-color: rgba(21, 26, 30, 0.5);
  backdrop-filter: blur(12px);
  position: sticky;
  top: 0;
  z-index: 10;
  flex-shrink: 0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: var(--space-4);
}

.status-indicator {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.animate-pulse {
  animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}

@keyframes pulse {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.5;
  }
}

.status-label {
  font-size: var(--text-xs);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}

.divider {
  width: 1px;
  height: 16px;
  background-color: var(--border-light);
}

.api-endpoint {
  font-size: var(--text-sm);
  color: var(--text-muted);
  font-family: var(--font-mono);
  letter-spacing: -0.025em;
}

.header-right {
  display: flex;
  align-items: center;
  gap: var(--space-6);
}

.equity-display {
  text-align: right;
}

.equity-label {
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  line-height: 1;
  margin-bottom: var(--space-1);
}

.equity-value {
  font-size: var(--text-sm);
  font-weight: 700;
  color: var(--text-primary);
  font-family: var(--font-mono);
}

.avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background-color: rgba(64, 186, 247, 0.2);
  border: 1px solid rgba(64, 186, 247, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--accent-primary);
  font-weight: 700;
  box-shadow: 0 0 20px rgba(64, 186, 247, 0.1);
}
</style>
