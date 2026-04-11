<script setup lang="ts">
type ButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost'
type ButtonSize = 'sm' | 'md' | 'lg'

interface Props {
  variant?: ButtonVariant
  size?: ButtonSize
  disabled?: boolean
  loading?: boolean
  type?: 'button' | 'submit' | 'reset'
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'primary',
  size: 'md',
  type: 'button'
})

const emit = defineEmits<{
  click: [event: MouseEvent]
}>()

const variantClasses: Record<ButtonVariant, string> = {
  primary: 'btn-primary',
  secondary: 'btn-secondary',
  danger: 'btn-danger',
  ghost: 'btn-ghost'
}

const sizeClasses: Record<ButtonSize, string> = {
  sm: 'btn-sm',
  md: 'btn-md',
  lg: 'btn-lg'
}

const handleClick = (e: MouseEvent) => {
  if (!props.disabled && !props.loading) {
    emit('click', e)
  }
}
</script>

<template>
  <button
    :type="props.type"
    class="btn"
    :class="[variantClasses[props.variant], sizeClasses[props.size]]"
    :disabled="props.disabled || props.loading"
    @click="handleClick"
  >
    <span v-if="loading" class="btn-spinner" />
    <span v-else-if="$slots.icon" class="btn-icon">
      <slot name="icon" />
    </span>
    <span v-if="$slots.default" class="btn-text">
      <slot />
    </span>
  </button>
</template>

<style scoped>
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  font-weight: 500;
  border: none;
  border-radius: var(--radius-md);
  cursor: pointer;
  transition: all var(--transition-fast);
  font-family: inherit;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Sizes */
.btn-sm {
  padding: var(--space-1) var(--space-3);
  font-size: var(--text-xs);
}

.btn-md {
  padding: var(--space-2) var(--space-4);
  font-size: var(--text-sm);
}

.btn-lg {
  padding: var(--space-3) var(--space-5);
  font-size: var(--text-base);
}

/* Variants */
.btn-primary {
  background-color: var(--accent-primary);
  color: var(--bg-primary);
}

.btn-primary:hover:not(:disabled) {
  background-color: var(--accent-primary-hover);
  box-shadow: var(--shadow-glow);
}

.btn-secondary {
  background-color: var(--bg-elevated);
  color: var(--text-primary);
  border: 1px solid var(--border);
}

.btn-secondary:hover:not(:disabled) {
  background-color: var(--border-light);
}

.btn-danger {
  background-color: var(--accent-danger);
  color: white;
}

.btn-danger:hover:not(:disabled) {
  background-color: var(--accent-danger-hover);
}

.btn-ghost {
  background-color: transparent;
  color: var(--text-secondary);
}

.btn-ghost:hover:not(:disabled) {
  background-color: var(--bg-elevated);
  color: var(--text-primary);
}

/* Spinner */
.btn-spinner {
  width: 16px;
  height: 16px;
  border: 2px solid currentColor;
  border-right-color: transparent;
  border-radius: 50%;
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.btn-icon {
  display: flex;
  align-items: center;
}

.btn-icon:only-child {
  margin: 0;
}
</style>
