<script setup lang="ts">
import { TrendingUp, TrendingDown } from 'lucide-vue-next'

interface Position {
  symbol: string
  side: 'LONG' | 'SHORT'
  leverage: number
  entryPrice: number
  markPrice: number
  unrealizedPnl: number
  unrealizedPnlPct: number
}

defineProps<{
  positions: Position[]
  loading?: boolean
}>()
</script>

<template>
  <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl overflow-hidden backdrop-blur-sm">
    <div class="px-6 py-4 border-b border-[#2b3139] flex justify-between items-center">
      <h3 class="text-sm font-bold text-white tracking-wider flex items-center uppercase">
        <span class="w-1.5 h-4 bg-[#40baf7] rounded-full mr-3"></span>
        Open Positions
      </h3>
      <div class="text-[10px] text-gray-500 uppercase tracking-widest">Live Updates</div>
    </div>

    <div class="overflow-x-auto">
      <table class="w-full text-left border-collapse">
        <thead>
          <tr class="bg-gray-900/50 text-[10px] uppercase tracking-widest text-gray-500 font-bold">
            <th class="px-6 py-3 border-b border-[#2b3139]">Symbol</th>
            <th class="px-6 py-3 border-b border-[#2b3139]">Side</th>
            <th class="px-6 py-3 border-b border-[#2b3139]">Lev.</th>
            <th class="px-6 py-3 border-b border-[#2b3139]">Entry</th>
            <th class="px-6 py-3 border-b border-[#2b3139]">Mark</th>
            <th class="px-6 py-3 border-b border-[#2b3139] text-right">Unr. P&L</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[#2b3139]">
          <tr 
            v-for="pos in positions" 
            :key="pos.symbol"
            class="hover:bg-gray-800/30 transition-colors group"
          >
            <td class="px-6 py-4 font-bold text-white">{{ pos.symbol }}</td>
            <td class="px-6 py-4">
              <span 
                :class="[
                  'text-[10px] font-bold px-2 py-0.5 rounded-full ring-1 ring-inset',
                  pos.side === 'LONG' 
                    ? 'bg-[#0ecb81]/10 text-[#0ecb81] ring-[#0ecb81]/30' 
                    : 'bg-[#f84960]/10 text-[#f84960] ring-[#f84960]/30'
                ]"
              >
                {{ pos.side }}
              </span>
            </td>
            <td class="px-6 py-4 text-gray-400 font-mono text-sm">{{ pos.leverage }}x</td>
            <td class="px-6 py-4 text-gray-400 font-mono text-sm">${{ pos.entryPrice.toLocaleString() }}</td>
            <td class="px-6 py-4 text-gray-400 font-mono text-sm">${{ pos.markPrice.toLocaleString() }}</td>
            <td class="px-6 py-4 text-right font-mono">
              <div :class="['font-bold', pos.unrealizedPnl >= 0 ? 'text-[#0ecb81]' : 'text-[#f84960]']">
                {{ pos.unrealizedPnl >= 0 ? '+' : '' }}{{ pos.unrealizedPnl.toFixed(2) }}
              </div>
              <div 
                :class="[
                  'text-[10px] flex items-center justify-end',
                  pos.unrealizedPnl >= 0 ? 'text-[#0ecb81]/70' : 'text-[#f84960]/70'
                ]"
              >
                <TrendingUp v-if="pos.unrealizedPnl >= 0" class="w-3 h-3 mr-1" />
                <TrendingDown v-else class="w-3 h-3 mr-1" />
                {{ pos.unrealizedPnlPct.toFixed(2) }}%
              </div>
            </td>
          </tr>
          <tr v-if="positions.length === 0 && !loading">
            <td colspan="6" class="px-6 py-12 text-center text-gray-500 italic text-sm">
              No open positions. Monitoring markets...
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
