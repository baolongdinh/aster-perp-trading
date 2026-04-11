<script setup lang="ts">
interface Props {
  title?: string
  maxWidth?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
}

const props = withDefaults(defineProps<Props>(), {
  maxWidth: 'xl'
})

const maxWidthClasses = {
  sm: 'max-w-3xl',
  md: 'max-w-5xl',
  lg: 'max-w-6xl',
  xl: 'max-w-7xl',
  full: 'max-w-none'
}
</script>

<template>
  <div class="page-container" :class="maxWidthClasses[props.maxWidth]">
    <div v-if="props.title || $slots.header" class="page-header">
      <slot name="header">
        <h1 class="page-title">{{ props.title }}</h1>
      </slot>
      <div v-if="$slots.actions" class="page-actions">
        <slot name="actions" />
      </div>
    </div>
    <div class="page-content">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.page-container {
  width: 100%;
  margin: 0 auto;
  padding: var(--space-6);
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--space-6);
  padding-bottom: var(--space-4);
  border-bottom: 1px solid var(--border);
}

.page-title {
  font-size: var(--text-2xl);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.page-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.page-content {
  color: var(--text-secondary);
}
</style>
