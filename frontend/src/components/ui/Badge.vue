<script setup lang="ts">
import { computed } from 'vue'

type BadgeVariant = 'success' | 'warning' | 'danger' | 'info' | 'neutral'
type BadgeSize = 'sm' | 'md'

interface Props {
  variant?: BadgeVariant
  size?: BadgeSize
  dot?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'neutral',
  size: 'md',
  dot: false
})

const variantClasses: Record<BadgeVariant, string> = {
  success: 'badge-success',
  warning: 'badge-warning',
  danger: 'badge-danger',
  info: 'badge-info',
  neutral: 'badge-neutral'
}

const dotColors: Record<BadgeVariant, string> = {
  success: 'var(--accent-success)',
  warning: 'var(--accent-warning)',
  danger: 'var(--accent-danger)',
  info: 'var(--accent-primary)',
  neutral: 'var(--text-muted)'
}

// Use computed to avoid TS6133
const currentVariant = computed(() => variantClasses[props.variant])
const currentDotColor = computed(() => dotColors[props.variant])
</script>

<template>
  <span 
    class="badge"
    :class="[currentVariant, props.size]"
  >
    <span 
      v-if="props.dot" 
      class="badge-dot"
      :style="{ backgroundColor: currentDotColor }"
    />
    <slot />
  </span>
</template>

<style scoped>
.badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  font-weight: 500;
  border-radius: var(--radius-md);
  font-size: var(--text-xs);
}

.badge.sm {
  padding: var(--space-1) var(--space-2);
}

.badge.md {
  padding: var(--space-1) var(--space-3);
  font-size: var(--text-sm);
}

.badge-success {
  background-color: var(--status-success-bg);
  color: var(--accent-success);
}

.badge-warning {
  background-color: var(--status-warning-bg);
  color: var(--accent-warning);
}

.badge-danger {
  background-color: var(--status-danger-bg);
  color: var(--accent-danger);
}

.badge-info {
  background-color: var(--status-info-bg);
  color: var(--accent-primary);
}

.badge-neutral {
  background-color: var(--bg-elevated);
  color: var(--text-secondary);
}

.badge-dot {
  width: 6px;
  height: 6px;
  border-radius: var(--radius-full);
}
</style>
