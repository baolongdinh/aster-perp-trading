<script setup lang="ts">
import { ref } from 'vue'
import StatsCard from '../components/StatsCard.vue'
import PositionsTable from '../components/PositionsTable.vue'
import { 
  BarChart3, 
  AlertCircle,
  Clock,
  RefreshCw
} from 'lucide-vue-next'

const totalPnl = ref({ value: '+$1,250.20', change: '+2.4%', isPositive: true })
const openPositionsCount = ref({ value: '4', change: '$12,400 Notional', isPositive: true })
const dailyDrawdown = ref({ value: '0.45%', change: 'Safe', isPositive: true })

interface Position {
  symbol: string
  side: 'LONG' | 'SHORT'
  leverage: number
  entryPrice: number
  markPrice: number
  unrealizedPnl: number
  unrealizedPnlPct: number
}

const mockPositions = ref<Position[]>([
  {
    symbol: 'BTC/USDT',
    side: 'LONG',

    leverage: 10,
    entryPrice: 64210.50,
    markPrice: 65120.40,
    unrealizedPnl: 450.20,
    unrealizedPnlPct: 1.42
  },
  {
    symbol: 'ETH/USDT',
    side: 'SHORT',
    leverage: 5,
    entryPrice: 3450.20,
    markPrice: 3420.10,
    unrealizedPnl: 120.40,
    unrealizedPnlPct: 0.85
  },
  {
    symbol: 'SOL/USDT',
    side: 'LONG',
    leverage: 3,
    entryPrice: 145.20,
    markPrice: 142.10,
    unrealizedPnl: -25.40,
    unrealizedPnlPct: -2.10
  }
])

const lastUpdate = ref(new Date().toLocaleTimeString())
const isRefreshing = ref(false)

const refreshData = () => {
  isRefreshing.value = true
  setTimeout(() => {
    isRefreshing.value = false
    lastUpdate.value = new Date().toLocaleTimeString()
  }, 800)
}
</script>


<template>
  <div class="p-8 space-y-8 max-w-[1600px] mx-auto">
    <!-- Page Header -->
    <div class="flex flex-col md:flex-row md:items-center justify-between gap-4">
      <div>
        <h1 class="text-3xl font-bold text-white flex items-center tracking-tight">
          Performance Dashboard
          <span class="ml-4 text-[10px] bg-gray-800 text-gray-400 px-2 py-1 rounded font-mono uppercase tracking-tighter">Live</span>
        </h1>
        <div class="flex items-center mt-2 text-sm text-gray-500 space-x-4">
          <div class="flex items-center">
            <Clock class="w-4 h-4 mr-1.5" />
            Last Updated: {{ lastUpdate }}
          </div>
          <button @click="refreshData" class="flex items-center hover:text-[#40baf7] transition-colors group">
            <RefreshCw :class="['w-4 h-4 mr-1.5', isRefreshing ? 'animate-spin' : 'group-hover:rotate-180 transition-transform duration-500']" />
            Refresh
          </button>
        </div>
      </div>
      
      <div class="flex items-center space-x-3">
        <button class="px-4 py-2 bg-gray-800 hover:bg-gray-700 text-sm font-semibold rounded-lg transition-colors border border-gray-700">
          History
        </button>
        <button class="px-4 py-2 bg-[#40baf7] hover:bg-[#40baf7]/80 text-gray-900 font-bold rounded-lg transition-all shadow-lg shadow-[#40baf7]/20">
          New Strategy
        </button>
      </div>
    </div>

    <!-- Stats Grid -->
    <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
      <StatsCard 
        title="Total Net P&L" 
        :value="totalPnl.value" 
        :change="totalPnl.change" 
        :is-positive="totalPnl.isPositive"
      >
        <template #icon>
          <TrendingUp v-if="totalPnl.isPositive" class="w-4 h-4 text-[#0ecb81]" />
          <TrendingUp v-else class="w-4 h-4 text-[#f84960] rotate-180" />
        </template>
      </StatsCard>

      <StatsCard 
        title="Open Positions" 
        :value="openPositionsCount.value" 
        :change="openPositionsCount.change" 
        :is-positive="openPositionsCount.isPositive"
      >
        <template #icon>
          <BarChart3 class="w-4 h-4 text-[#40baf7]" />
        </template>
      </StatsCard>

      <StatsCard 
        title="Daily Drawdown" 
        :value="dailyDrawdown.value" 
        :change="dailyDrawdown.change" 
        :is-positive="dailyDrawdown.isPositive"
      >
        <template #icon>
          <AlertCircle class="w-4 h-4 text-[#40baf7]" />
        </template>
      </StatsCard>
    </div>

    <!-- Charts & Main Table Grid -->
    <div class="grid grid-cols-1 lg:grid-cols-3 gap-8">
      <!-- PnL Chart (Placeholder for now) -->
      <div class="lg:col-span-2 space-y-8">
        <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl p-6 backdrop-blur-sm">
          <div class="flex justify-between items-center mb-6">
            <h3 class="text-sm font-bold text-white tracking-wider flex items-center uppercase">
              <span class="w-1.5 h-4 bg-[#40baf7] rounded-full mr-3"></span>
              Equity Curve (30D)
            </h3>
            <div class="flex items-center space-x-2">
              <span class="w-3 h-3 rounded-full bg-[#40baf7]/20 border border-[#40baf7]"></span>
              <span class="text-xs text-gray-400">Net Value</span>
            </div>
          </div>
          
          <div class="h-64 flex items-center justify-center border-2 border-dashed border-gray-800 rounded-lg group hover:border-[#40baf7]/20 transition-colors">
            <div class="text-center">
              <BarChart3 class="w-12 h-12 text-gray-700 mx-auto mb-3 group-hover:text-[#40baf7]/30 transition-colors" />
              <p class="text-gray-600 text-sm italic font-mono uppercase tracking-tighter">Charting engine initializing...</p>
            </div>
          </div>
        </div>

        <!-- Positions Table -->
        <PositionsTable :positions="mockPositions" />
      </div>

      <!-- Right Side Panel: Recent Fills -->
      <div class="space-y-8">
        <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl flex flex-col h-full backdrop-blur-sm">
          <div class="px-6 py-4 border-b border-[#2b3139] flex justify-between items-center">
            <h3 class="text-sm font-bold text-white tracking-wider flex items-center uppercase">
              <span class="w-1.5 h-4 bg-purple-500 rounded-full mr-3"></span>
              Recent Activity
            </h3>
            <button class="text-[10px] text-[#40baf7] uppercase tracking-widest hover:underline">View All</button>
          </div>
          
          <div class="flex-1 p-6 space-y-6">
            <div v-for="i in 5" :key="i" class="flex items-start space-x-4 group cursor-default">
              <div class="mt-1 w-2 h-2 rounded-full bg-[#0ecb81] shadow-[0_0_8px_rgba(14,203,129,0.5)]"></div>
              <div class="flex-1">
                <div class="flex justify-between">
                  <span class="text-xs font-bold text-white tracking-widest group-hover:text-[#40baf7] transition-colors uppercase">BTCUSDT Buy Fill</span>
                  <span class="text-[10px] text-gray-600 font-mono">14:22:45</span>
                </div>
                <p class="text-[10px] text-gray-500 mt-1 leading-relaxed">
                  EMA Crossover signal triggered long entry at <span class="text-white">$64,210.50</span>. Size 0.45 BTC.
                </p>
              </div>
            </div>
          </div>

          <div class="p-4 bg-gray-900/30 border-t border-[#2b3139]">
            <div class="flex items-center justify-between text-[10px] text-gray-500 font-mono uppercase">
              <span>Engine Status</span>
              <span class="text-[#0ecb81]">All systems nominal</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
