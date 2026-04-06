<script setup lang="ts">
import { useStrategies } from '../api/strategies'
import { Zap, Power } from 'lucide-vue-next'

const { strategies, loading, enableStrategy, disableStrategy } = useStrategies()

const toggleStrategy = async (strategy: { name: string; enabled: boolean }) => {
  if (strategy.enabled) {
    await disableStrategy(strategy.name)
  } else {
    await enableStrategy(strategy.name)
  }
}
</script>

<template>
  <div class="strategies">
    <header class="strategies-header">
      <h1>Strategies</h1>
      <span v-if="loading" class="loading-text">Loading...</span>
    </header>

    <div v-if="strategies.length === 0 && !loading" class="empty">
      No strategies configured
    </div>

    <div class="strategy-list">
      <div 
        v-for="strategy in strategies" 
        :key="strategy.name"
        class="strategy-card"
        :class="{ enabled: strategy.enabled }"
      >
        <div class="strategy-info">
          <div class="strategy-icon">
            <Zap />
          </div>
          <div class="strategy-details">
            <h3>{{ strategy.name }}</h3>
            <p class="symbols">{{ strategy.symbols?.join(', ') || 'No symbols' }}</p>
          </div>
        </div>

        <button 
          @click="toggleStrategy(strategy)"
          class="toggle-btn"
          :class="{ active: strategy.enabled }"
          :disabled="loading"
        >
          <Power class="btn-icon" />
          <span>{{ strategy.enabled ? 'ON' : 'OFF' }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.strategies {
  padding: 24px;
  max-width: 800px;
  margin: 0 auto;
}

.strategies-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.strategies-header h1 {
  font-size: 24px;
  font-weight: 600;
  color: #1a1a1a;
  margin: 0;
}

.loading-text {
  font-size: 14px;
  color: #6b7280;
}

.empty {
  text-align: center;
  padding: 40px;
  color: #6b7280;
  background: #f9fafb;
  border-radius: 12px;
}

.strategy-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.strategy-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  background: white;
  border-radius: 12px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  border-left: 4px solid #6b7280;
  transition: all 0.2s;
}

.strategy-card.enabled {
  border-left-color: #22c55e;
}

.strategy-info {
  display: flex;
  align-items: center;
  gap: 16px;
}

.strategy-icon {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f3f4f6;
  border-radius: 10px;
  color: #6b7280;
}

.strategy-icon svg {
  width: 20px;
  height: 20px;
}

.strategy-details h3 {
  font-size: 16px;
  font-weight: 600;
  color: #1a1a1a;
  margin: 0 0 4px 0;
}

.symbols {
  font-size: 13px;
  color: #6b7280;
  margin: 0;
}

.toggle-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border-radius: 8px;
  border: none;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
  background: #f3f4f6;
  color: #6b7280;
}

.toggle-btn:hover:not(:disabled) {
  background: #e5e7eb;
}

.toggle-btn.active {
  background: #dcfce7;
  color: #16a34a;
}

.toggle-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-icon {
  width: 16px;
  height: 16px;
}
</style>
