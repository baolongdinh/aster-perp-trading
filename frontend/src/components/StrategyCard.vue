<script setup lang="ts">
import { ref } from 'vue'
import { 
  Settings2, 
  ChevronDown, 
  Save,
  RotateCcw
} from 'lucide-vue-next'


interface Props {
  name: string
  enabled: boolean
  winRate: string
  profitFactor: string
  lastSignal: string
  symbols: string[]
}

defineProps<Props>()

const emit = defineEmits(['update:enabled', 'save'])

const isExpanded = ref(false)
const localLeverage = ref(5)
const localOrderSize = ref(100)

const toggleExpand = () => isExpanded.value = !isExpanded.value
</script>

<template>
  <div 
    :class="[
      'bg-[#151a1e] border rounded-xl overflow-hidden transition-all duration-300 backdrop-blur-sm',
      enabled ? 'border-[#40baf7]/40 shadow-lg shadow-[#40baf7]/5' : 'border-[#2b3139]'
    ]"
  >
    <div class="p-6">
      <div class="flex justify-between items-start mb-6">
        <div>
          <h3 class="text-xl font-bold text-white tracking-tight italic">{{ name }}</h3>
          <p class="text-[10px] text-gray-500 uppercase tracking-widest mt-1">Status: {{ enabled ? 'Active' : 'Paused' }}</p>
        </div>
        
        <label class="relative inline-flex items-center cursor-pointer">
          <input 
            type="checkbox" 
            :checked="enabled" 
            @change="$emit('update:enabled', !enabled)"
            class="sr-only peer"
          >
          <div class="w-11 h-6 bg-gray-700 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-[#40baf7]"></div>
        </label>
      </div>

      <!-- Quick Metrics -->
      <div class="grid grid-cols-3 gap-4 mb-6">
        <div class="text-center p-3 bg-gray-900/50 rounded-lg border border-[#2b3139]">
          <div class="text-[10px] text-gray-500 uppercase tracking-tighter mb-1">Win Rate</div>
          <div class="text-sm font-mono font-bold text-white">{{ winRate }}</div>
        </div>
        <div class="text-center p-3 bg-gray-900/50 rounded-lg border border-[#2b3139]">
          <div class="text-[10px] text-gray-500 uppercase tracking-tighter mb-1">Profit Factor</div>
          <div class="text-sm font-mono font-bold text-white">{{ profitFactor }}</div>
        </div>
        <div class="text-center p-3 bg-gray-900/50 rounded-lg border border-[#2b3139]">
          <div class="text-[10px] text-gray-500 uppercase tracking-tighter mb-1">Last Signal</div>
          <div class="text-[10px] font-mono font-bold text-[#0ecb81] truncate px-1">{{ lastSignal }}</div>
        </div>
      </div>

      <!-- Symbols & Expanding Toggle -->
      <div class="flex justify-between items-center">
        <div class="flex gap-1 flex-wrap max-w-[70%]">
          <span 
            v-for="symbol in symbols" 
            :key="symbol"
            class="text-[9px] font-bold px-2 py-0.5 bg-gray-800 text-gray-400 rounded border border-gray-700 uppercase"
          >
            {{ symbol }}
          </span>
        </div>
        
        <button 
          @click="toggleExpand"
          class="text-xs text-gray-500 hover:text-white flex items-center transition-colors"
        >
          <Settings2 class="w-4 h-4 mr-1.5" />
          {{ isExpanded ? 'Hide Params' : 'Edit Params' }}
          <ChevronDown :class="['w-3 h-3 ml-1 transition-transform', isExpanded ? 'rotate-180' : '']" />
        </button>
      </div>
    </div>

    <!-- Parameter Editor (Expanded) -->
    <transition
      enter-active-class="transition duration-300 ease-out"
      enter-from-class="transform -translate-y-2 opacity-0"
      enter-to-class="transform translate-y-0 opacity-100"
      leave-active-class="transition duration-200 ease-in"
      leave-from-class="transform translate-y-0 opacity-100"
      leave-to-class="transform -translate-y-2 opacity-0"
    >
      <div v-if="isExpanded" class="bg-gray-900/50 border-t border-[#2b3139] p-6 space-y-6 shadow-inner">
        <div class="space-y-4">
          <div>
            <div class="flex justify-between mb-2">
              <label class="text-[10px] uppercase tracking-widest text-gray-400 font-bold">Leverage Mode</label>
              <span class="text-[10px] font-mono text-[#40baf7]">{{ localLeverage }}x</span>
            </div>
            <input 
              v-model="localLeverage" 
              type="range" min="1" max="25" 
              class="w-full h-1.5 bg-gray-700 rounded-lg appearance-none cursor-pointer accent-[#40baf7]"
            >
          </div>

          <div>
            <label class="text-[10px] uppercase tracking-widest text-gray-400 font-bold block mb-2">Order Size (USDT)</label>
            <div class="relative">
              <input 
                v-model="localOrderSize"
                type="number"
                class="w-full bg-[#0b0e11] border border-[#2b3139] rounded px-3 py-2 text-sm font-mono text-white focus:outline-none focus:border-[#40baf7]/50"
              >
              <span class="absolute right-3 top-2 text-[10px] text-gray-600 font-bold">USDT</span>
            </div>
          </div>
        </div>

        <div class="flex gap-2">
          <button 
            @click="$emit('save', { leverage: localLeverage, size: localOrderSize })"
            class="flex-1 bg-[#40baf7]/10 hover:bg-[#40baf7]/20 text-[#40baf7] py-2 rounded text-xs font-bold transition-colors flex items-center justify-center border border-[#40baf7]/20"
          >
            <Save class="w-3.5 h-3.5 mr-2" />
            Save Changes
          </button>
          <button class="p-2 bg-gray-800 hover:bg-gray-700 text-gray-400 rounded transition-colors flex items-center justify-center">
            <RotateCcw class="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </transition>
  </div>
</template>
