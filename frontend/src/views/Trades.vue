<script setup lang="ts">
import { ref } from 'vue'
import { 
  Search, 
  Filter, 
  Download, 
  ChevronLeft, 
  ChevronRight,
  Calendar
} from 'lucide-vue-next'


const trades = ref([
  {
    time: '2026-03-15 14:22:45',
    symbol: 'BTC/USDT',
    side: 'LONG',
    strategy: 'EMA Crossover',
    price: '$64,210.50',
    quantity: '0.45 BTC',
    realizedPnl: '+$120.40',
    isPositive: true,
    status: 'Filled'
  },
  {
    time: '2026-03-15 13:10:12',
    symbol: 'ETH/USDT',
    side: 'SHORT',
    strategy: 'Funding Arb',
    price: '$3,450.20',
    quantity: '2.5 ETH',
    realizedPnl: '+$85.20',
    isPositive: true,
    status: 'Filled'
  },
  {
    time: '2026-03-15 11:45:30',
    symbol: 'SOL/USDT',
    side: 'LONG',
    strategy: 'Grid Trading',
    price: '$145.20',
    quantity: '50 SOL',
    realizedPnl: '-$45.10',
    isPositive: false,
    status: 'Filled'
  },
  {
    time: '2026-03-15 09:22:15',
    symbol: 'BTC/USDT',
    side: 'SHORT',
    strategy: 'EMA Crossover',
    price: '$63,980.10',
    quantity: '0.20 BTC',
    realizedPnl: '+$42.30',
    isPositive: true,
    status: 'Filled'
  },
  {
    time: '2026-03-15 08:15:00',
    symbol: 'ETH/USDT',
    side: 'BUY',
    strategy: 'Manual',
    price: '$3,420.00',
    quantity: '1.0 ETH',
    realizedPnl: 'N/A',
    isPositive: true,
    status: 'Cancelled'
  }
])
</script>

<template>
  <div class="p-8 space-y-8 max-w-[1600px] mx-auto">
    <!-- Header -->
    <div class="flex flex-col md:flex-row md:items-center justify-between gap-6">
      <div>
        <h1 class="text-3xl font-bold text-white tracking-tight">Trade History</h1>
        <div class="flex items-center mt-2 text-sm text-gray-500 space-x-4">
          <span class="flex items-center"><Calendar class="w-4 h-4 mr-1.5" /> Filtered: Last 30 Days</span>
        </div>
      </div>

      <div class="flex items-center space-x-3">
        <div class="relative group">
          <Search class="absolute left-3 top-2.5 w-4 h-4 text-gray-500 group-focus-within:text-[#40baf7] transition-colors" />
          <input 
            type="text" 
            placeholder="Search symbol or strategy..."
            class="bg-[#151a1e] border border-[#2b3139] rounded-lg pl-10 pr-4 py-2 text-sm text-white focus:outline-none focus:border-[#40baf7]/50 w-64 transition-all"
          >
        </div>
        <button class="p-2 bg-gray-800 hover:bg-gray-700 text-gray-400 rounded-lg transition-colors border border-gray-700">
          <Filter class="w-5 h-5" />
        </button>
        <button class="px-4 py-2 bg-[#40baf7] hover:bg-[#40baf7]/80 text-gray-900 font-bold rounded-lg transition-all flex items-center shadow-lg shadow-[#40baf7]/20 uppercase text-xs tracking-widest">
          <Download class="w-4 h-4 mr-2" />
          Export CSV
        </button>
      </div>
    </div>

    <!-- Quick Stats Summary -->
    <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
      <div v-for="i in 3" :key="i" class="bg-[#151a1e] border border-[#2b3139] p-5 rounded-xl backdrop-blur-sm border-l-4" :class="[i === 1 ? 'border-l-gray-700' : i === 2 ? 'border-l-[#0ecb81]' : 'border-l-[#40baf7]']">
        <div class="text-[10px] text-gray-500 uppercase tracking-widest font-bold mb-1">{{ ['Total Executions', 'Net Realized P&L', 'Success Rate'][i-1] }}</div>
        <div class="text-2xl font-mono font-bold" :class="i === 2 ? 'text-[#0ecb81]' : 'text-white'">
          {{ ['142', '+$1,420.50', '62.4%'][i-1] }}
        </div>
      </div>
    </div>

    <!-- Table Container -->
    <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl overflow-hidden backdrop-blur-sm">
      <div class="overflow-x-auto">
        <table class="w-full text-left border-collapse">
          <thead>
            <tr class="bg-gray-900/50 text-[10px] uppercase tracking-widest text-gray-500 font-bold">
              <th class="px-6 py-4 border-b border-[#2b3139]">Execution Time</th>
              <th class="px-6 py-4 border-b border-[#2b3139]">Asset</th>
              <th class="px-6 py-4 border-b border-[#2b3139]">Side</th>
              <th class="px-6 py-4 border-b border-[#2b3139]">Strategy</th>
              <th class="px-6 py-4 border-b border-[#2b3139]">Price</th>
              <th class="px-6 py-4 border-b border-[#2b3139]">Quantity</th>
              <th class="px-6 py-4 border-b border-[#2b3139] text-right">Realized P&L</th>
              <th class="px-6 py-4 border-b border-[#2b3139] text-center">Status</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[#2b3139]">
            <tr v-for="trade in trades" :key="trade.time" class="hover:bg-gray-800/30 transition-colors group cursor-default">
              <td class="px-6 py-4 text-xs font-mono text-gray-400">{{ trade.time }}</td>
              <td class="px-6 py-4 font-bold text-white tracking-tight">{{ trade.symbol }}</td>
              <td class="px-6 py-4 text-xs">
                <span :class="[
                  'px-2 py-0.5 rounded font-bold tracking-widest',
                  trade.side === 'LONG' ? 'bg-[#0ecb81]/10 text-[#0ecb81]' : 'bg-[#f84960]/10 text-[#f84960]'
                ]">
                  {{ trade.side }}
                </span>
              </td>
              <td class="px-6 py-4 text-xs text-gray-400 italic">{{ trade.strategy }}</td>
              <td class="px-6 py-4 font-mono text-sm text-gray-300">{{ trade.price }}</td>
              <td class="px-6 py-4 font-mono text-sm text-gray-400">{{ trade.quantity }}</td>
              <td class="px-6 py-4 text-right">
                <div :class="['font-mono font-bold text-sm', trade.isPositive ? 'text-[#0ecb81]' : 'text-[#f84960]']">
                  {{ trade.realizedPnl }}
                </div>
              </td>
              <td class="px-6 py-4 text-center">
                <span :class="[
                  'text-[10px] font-bold uppercase tracking-widest',
                  trade.status === 'Filled' ? 'text-gray-400' : 'text-gray-600 line-through'
                ]">
                  {{ trade.status }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Pagination -->
      <div class="px-6 py-4 border-t border-[#2b3139] flex items-center justify-between bg-gray-900/20">
        <div class="text-xs text-gray-600 font-mono italic">
          Showing 1 to 5 of 142 historical trades
        </div>
        <div class="flex items-center space-x-2">
          <button class="p-1.5 hover:bg-gray-800 rounded transition-colors text-gray-600"><ChevronLeft class="w-4 h-4" /></button>
          <div class="flex items-center space-x-1 px-4 text-xs font-mono">
            <span class="text-white font-bold bg-[#40baf7]/10 px-2 py-0.5 rounded border border-[#40baf7]/30">1</span>
            <button class="hover:bg-gray-800 px-2 py-0.5 rounded text-gray-500">2</button>
            <button class="hover:bg-gray-800 px-2 py-0.5 rounded text-gray-500">3</button>
            <span class="text-gray-700">...</span>
            <button class="hover:bg-gray-800 px-2 py-0.5 rounded text-gray-500">29</button>
          </div>
          <button class="p-1.5 hover:bg-gray-800 rounded transition-colors text-gray-500"><ChevronRight class="w-4 h-4" /></button>
        </div>
      </div>
    </div>
  </div>
</template>
