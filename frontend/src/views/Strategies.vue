<script setup lang="ts">
import { ref } from 'vue'
import StrategyCard from '../components/StrategyCard.vue'
import { Plus, PauseCircle, Settings2, Zap, AlertCircle } from 'lucide-vue-next'

const strategies = ref([

  {
    name: 'EMA Crossover',
    enabled: true,
    winRate: '68.4%',
    profitFactor: '1.42',
    lastSignal: 'LONG BTC @ $64,210',
    symbols: ['BTCUSDT', 'ETHUSDT']
  },
  {
    name: 'Funding Rate Arb',
    enabled: false,
    winRate: '82.1%',
    profitFactor: '1.25',
    lastSignal: 'N/A',
    symbols: ['BTCUSDT', 'SOLUSDT', 'BNBUSDT']
  },
  {
    name: 'Grid Trading',
    enabled: true,
    winRate: '54.2%',
    profitFactor: '1.18',
    lastSignal: 'BUY @ $62,100',
    symbols: ['BTCUSDT']
  }
])

const pauseAll = () => {
  strategies.value.forEach(s => s.enabled = false)
}

const saveStrategy = (name: string, data: any) => {
  console.log('Saving strategy:', name, data)
  // Logic to call API
}
</script>

<template>
  <div class="p-8 space-y-8 max-w-[1600px] mx-auto">
    <!-- Header -->
    <div class="flex justify-between items-center">
      <div>
        <h1 class="text-3xl font-bold text-white tracking-tight flex items-center">
          Strategy Management
        </h1>
        <p class="text-sm text-gray-500 mt-2 italic font-mono uppercase tracking-tighter">Active Algorithms: 2 / 3</p>
      </div>

      <div class="flex space-x-3">
        <button 
          @click="pauseAll"
          class="px-4 py-2 bg-[#f84960]/10 hover:bg-[#f84960]/20 text-[#f84960] font-bold rounded-lg transition-all border border-[#f84960]/20 flex items-center"
        >
          <PauseCircle class="w-4 h-4 mr-2" />
          Pause All
        </button>
        <button class="px-4 py-2 bg-[#40baf7] hover:bg-[#40baf7]/80 text-gray-900 font-bold rounded-lg transition-all shadow-lg shadow-[#40baf7]/20 flex items-center">
          <Plus class="w-4 h-4 mr-2" />
          New Strategy
        </button>
      </div>
    </div>

    <!-- Stats Bar -->
    <div class="grid grid-cols-1 md:grid-cols-4 gap-6">
      <div v-for="i in 4" :key="i" class="bg-[#151a1e] border border-[#2b3139] rounded-xl p-4 flex items-center space-x-4">
        <div class="w-10 h-10 rounded-lg bg-gray-900 flex items-center justify-center text-gray-500">
          <AlertCircle v-if="i === 1" class="w-5 h-5" />
          <Zap v-if="i === 2" class="w-5 h-5 text-[#40baf7]" />
          <ShieldAlert v-if="i === 3" class="w-5 h-5 text-[#f84960]" />
          <Settings2 v-if="i === 4" class="w-5 h-5" />
        </div>

        <div>
          <div class="text-[10px] text-gray-500 uppercase tracking-widest font-bold">{{ ['Total signals', 'Active Leverge', 'Risk Alerts', 'Engine Load'][i-1] }}</div>
          <div class="text-sm font-mono font-bold">{{ ['1,242', '8.5x', '0', '4%'][i-1] }}</div>
        </div>
      </div>
    </div>

    <!-- Strategy Grid -->
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-8">
      <StrategyCard 
        v-for="st in strategies" 
        :key="st.name"
        v-bind="st"
        @update:enabled="st.enabled = $event"
        @save="saveStrategy(st.name, $event)"
      />
    </div>

    <!-- System Logs / Help -->
    <div class="bg-[#151a1e]/50 border border-dashed border-gray-800 rounded-xl p-8 text-center">
      <ShieldAlert class="w-10 h-10 text-gray-700 mx-auto mb-4" />
      <h4 class="text-gray-400 font-bold text-sm uppercase tracking-widest mb-2">Security Note</h4>
      <p class="text-gray-600 text-[10px] leading-relaxed max-w-lg mx-auto italic font-mono uppercase tracking-tighter">
        Strategy parameters are hot-reloaded into the Go engine. All changes are validated against the global risk management layer before execution. Signature verification (EIP-712) ensures integrity.
      </p>
    </div>
  </div>
</template>
