<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronRight, Activity } from 'lucide-vue-next'

interface NavItem {
  name: string
  icon: any
  path: string
}

interface Props {
  items: NavItem[]
  defaultOpen?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  defaultOpen: true
})

const emit = defineEmits<{
  'update:open': [value: boolean]
}>()

const route = useRoute()
const router = useRouter()

const isOpen = ref(props.defaultOpen)

const sidebarWidth = computed(() => isOpen.value ? 'w-64' : 'w-20')

const toggleSidebar = () => {
  isOpen.value = !isOpen.value
  emit('update:open', isOpen.value)
}

const navigate = (path: string) => {
  router.push(path)
}

const isActive = (path: string) => route.path === path
</script>

<template>
  <aside 
    :class="[
      'sidebar',
      sidebarWidth
    ]"
  >
    <!-- Logo Area -->
    <div class="sidebar-header">
      <Activity class="logo-icon" />
      <span v-if="isOpen" class="logo-text">
        ASTER<span class="logo-accent">BOT</span>
      </span>
    </div>

    <!-- Navigation -->
    <nav class="sidebar-nav">
      <button
        v-for="item in props.items"
        :key="item.path"
        @click="navigate(item.path)"
        :class="[
          'nav-item',
          isActive(item.path) ? 'nav-item-active' : 'nav-item-inactive'
        ]"
      >
        <component :is="item.icon" class="nav-icon" />
        <span v-if="isOpen" class="nav-label">{{ item.name }}</span>
        
        <!-- Active Indicator -->
        <div 
          v-if="isActive(item.path)" 
          class="active-indicator"
        />

        <!-- Tooltip (Minified Mode) -->
        <div 
          v-if="!isOpen"
          class="nav-tooltip"
        >
          {{ item.name }}
        </div>
      </button>
    </nav>

    <!-- Footer / Collapse Toggle -->
    <div class="sidebar-footer">
      <button 
        @click="toggleSidebar"
        class="toggle-btn"
      >
        <ChevronRight :class="['toggle-icon', isOpen ? 'rotate-180' : '']" />
      </button>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  background-color: var(--bg-secondary);
  border-right: 1px solid var(--border);
  transition: width 0.3s ease;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.sidebar-header {
  height: 64px;
  display: flex;
  align-items: center;
  padding: 0 var(--space-6);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.logo-icon {
  width: 32px;
  height: 32px;
  color: var(--accent-primary);
  flex-shrink: 0;
}

.logo-text {
  margin-left: var(--space-3);
  font-size: var(--text-xl);
  font-weight: 700;
  font-style: italic;
  letter-spacing: -0.025em;
  color: var(--text-primary);
}

.logo-accent {
  color: var(--accent-primary);
  font-style: normal;
  font-weight: 400;
}

.sidebar-nav {
  flex: 1;
  padding: var(--space-6) var(--space-3);
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  overflow-y: auto;
}

.nav-item {
  position: relative;
  display: flex;
  align-items: center;
  padding: var(--space-3);
  border-radius: var(--radius-lg);
  transition: all var(--transition-fast);
  border: none;
  background: transparent;
  cursor: pointer;
  width: 100%;
  text-align: left;
}

.nav-icon {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
}

.nav-label {
  margin-left: var(--space-3);
  font-weight: 500;
  white-space: nowrap;
}

.nav-item-active {
  background-color: var(--status-info-bg);
  color: var(--accent-primary);
}

.nav-item-inactive {
  color: var(--text-secondary);
}

.nav-item-inactive:hover {
  background-color: var(--bg-elevated);
  color: var(--text-primary);
}

.active-indicator {
  position: absolute;
  left: 0;
  width: 3px;
  height: 24px;
  background-color: var(--accent-primary);
  border-radius: 0 4px 4px 0;
}

.nav-tooltip {
  position: absolute;
  left: calc(100% + var(--space-4));
  padding: var(--space-1) var(--space-2);
  background-color: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: var(--text-xs);
  border-radius: var(--radius-md);
  white-space: nowrap;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.2s;
  border: 1px solid var(--border);
  z-index: 50;
}

.nav-item:hover .nav-tooltip {
  opacity: 1;
}

.sidebar-footer {
  padding: var(--space-4);
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}

.toggle-btn {
  width: 100%;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-lg);
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: all var(--transition-fast);
}

.toggle-btn:hover {
  background-color: var(--bg-elevated);
  color: var(--text-primary);
}

.toggle-icon {
  width: 20px;
  height: 20px;
  transition: transform 0.3s ease;
}

.rotate-180 {
  transform: rotate(180deg);
}
</style>
