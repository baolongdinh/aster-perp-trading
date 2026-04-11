<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  value: number
  size?: 'sm' | 'md' | 'lg'
  showSign?: boolean
  decimals?: number
}

const props = withDefaults(defineProps<Props>(), {
  size: 'md',
  showSign: true,
  decimals: 2
})

const sign = computed(() => {
  if (!props.showSign) return ''
  if (props.value > 0) return '+'
  if (props.value < 0) return '-'
  return ''
})

const absValue = computed(() => Math.abs(props.value).toFixed(props.decimals))

const pnlClass = computed(() => {
  if (props.value > 0) return 'positive'
  if (props.value < 0) return 'negative'
  return 'neutral'
})

const sizeClass = computed(() => `size-${props.size}`)
</script>

<template>
  <span class="pnl" :class="[pnlClass, sizeClass]">
    <span v-if="props.showSign" class="sign">{{ sign }}</span>
    <span class="currency">$</span>
    <span class="amount">{{ absValue }}</span>
  </span>
</template>

<style scoped>
.pnl {
  font-family: var(--font-mono);
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 1px;
}

/* Sizes */
.size-sm {
  font-size: var(--text-sm);
}

.size-md {
  font-size: var(--text-base);
}

.size-lg {
  font-size: var(--text-xl);
}

/* Colors */
.positive {
  color: var(--accent-success);
}

.negative {
  color: var(--accent-danger);
}

.neutral {
  color: var(--text-secondary);
}

.sign {
  margin-right: 1px;
}

.currency {
  opacity: 0.8;
}
</style>
