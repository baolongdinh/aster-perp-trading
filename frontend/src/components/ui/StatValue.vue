<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  label: string
  value: number | string
  prefix?: string
  suffix?: string
  change?: number
  format?: 'currency' | 'number' | 'percent' | 'compact'
  decimals?: number
}

const props = withDefaults(defineProps<Props>(), {
  format: 'number',
  decimals: 2
})

const formatValue = (val: number | string): string => {
  if (typeof val === 'string') return val
  
  switch (props.format) {
    case 'currency':
      return new Intl.NumberFormat('en-US', {
        style: 'decimal',
        minimumFractionDigits: props.decimals,
        maximumFractionDigits: props.decimals
      }).format(val)
    case 'percent':
      return `${val.toFixed(props.decimals)}%`
    case 'compact':
      return new Intl.NumberFormat('en-US', {
        notation: 'compact',
        maximumFractionDigits: 1
      }).format(val)
    default:
      return new Intl.NumberFormat('en-US', {
        minimumFractionDigits: props.decimals,
        maximumFractionDigits: props.decimals
      }).format(val)
  }
}

const formattedValue = computed(() => formatValue(props.value))

const formattedChange = computed(() => {
  if (props.change === undefined) return null
  const sign = props.change >= 0 ? '+' : ''
  return `${sign}${props.change.toFixed(2)}%`
})

const changeClass = computed(() => {
  if (props.change === undefined) return ''
  return props.change >= 0 ? 'change-positive' : 'change-negative'
})
</script>

<template>
  <div class="stat-value">
    <div class="stat-label">{{ props.label }}</div>
    <div class="stat-amount">
      <span v-if="props.prefix" class="stat-prefix">{{ props.prefix }}</span>
      <span class="stat-number">{{ formattedValue }}</span>
      <span v-if="props.suffix" class="stat-suffix">{{ props.suffix }}</span>
    </div>
    <div v-if="formattedChange" class="stat-change" :class="changeClass">
      {{ formattedChange }}
    </div>
  </div>
</template>

<style scoped>
.stat-value {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.stat-label {
  font-size: var(--text-sm);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.stat-amount {
  display: flex;
  align-items: baseline;
  gap: var(--space-1);
}

.stat-prefix,
.stat-suffix {
  font-size: var(--text-lg);
  color: var(--text-secondary);
}

.stat-number {
  font-size: var(--text-2xl);
  font-weight: 700;
  color: var(--text-primary);
  font-family: var(--font-mono);
}

.stat-change {
  font-size: var(--text-sm);
  font-weight: 500;
}

.change-positive {
  color: var(--accent-success);
}

.change-negative {
  color: var(--accent-danger);
}
</style>
