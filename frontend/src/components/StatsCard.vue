<script setup lang="ts">
interface Props {
  title: string
  value: string
  change?: string
  isPositive?: boolean
  loading?: boolean
}

defineProps<Props>()
</script>

<template>
  <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl p-5 hover:border-[#40baf7]/30 transition-all group backdrop-blur-sm relative overflow-hidden">
    <!-- Glow Effect -->
    <div 
      class="absolute -top-10 -right-10 w-24 h-24 blur-3xl rounded-full opacity-10 group-hover:opacity-20 transition-opacity"
      :class="isPositive ? 'bg-[#0ecb81]' : 'bg-[#f84960]'"
    ></div>

    <div class="flex justify-between items-start mb-2">
      <h3 class="text-[10px] uppercase tracking-widest text-gray-500 font-bold">{{ title }}</h3>
      <slot name="icon"></slot>
    </div>

    <div class="flex items-baseline space-x-2">
      <div v-if="loading" class="h-8 w-24 bg-gray-800 animate-pulse rounded"></div>
      <span v-else class="text-2xl font-mono font-bold text-white tracking-tight">{{ value }}</span>
      
      <span 
        v-if="change" 
        :class="[
          'text-xs font-bold px-1.5 py-0.5 rounded',
          isPositive ? 'bg-[#0ecb81]/10 text-[#0ecb81]' : 'bg-[#f84960]/10 text-[#f84960]'
        ]"
      >
        {{ change }}
      </span>
    </div>
  </div>
</template>
