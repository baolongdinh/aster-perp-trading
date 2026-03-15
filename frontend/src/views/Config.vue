<script setup lang="ts">
import { ref } from 'vue'
import { 
  ShieldCheck, 
  Key, 
  Settings2, 
  Bell, 
  Info,
  Save,
  Trash2,
  ExternalLink,
  ShieldAlert
} from 'lucide-vue-next'

const config = ref({
  walletAddress: '0x1A2b...C3d4',
  signerAddress: '0x5E6f...G7h8',
  maxPositions: 10,
  stopLoss: 5.0,
  dailyDrawdown: 3.5,
  minEquity: 1000,
  webhookUrl: 'https://discord.com/api/webhooks/...',
  notifyFills: true,
  notifyLiquidations: true,
  notifyErrors: true
})

const isSaving = ref(false)

const saveConfig = () => {
  isSaving.value = true
  setTimeout(() => {
    isSaving.value = false
    alert('Configuration saved successfully.')
  }, 1000)
}
</script>

<template>
  <div class="p-8 space-y-8 max-w-[1200px] mx-auto pb-24">
    <!-- Header -->
    <div class="flex justify-between items-center">
      <div>
        <h1 class="text-3xl font-bold text-white tracking-tight">Global Configuration</h1>
        <p class="text-sm text-gray-500 mt-2 font-mono uppercase tracking-tighter">System Version: v2.4.1-alpha</p>
      </div>

      <button 
        @click="saveConfig"
        class="px-6 py-2.5 bg-[#40baf7] hover:bg-[#40baf7]/80 text-gray-900 font-bold rounded-lg transition-all flex items-center shadow-lg shadow-[#40baf7]/20 sticky top-4 z-20"
      >
        <Save class="w-4 h-4 mr-2" />
        {{ isSaving ? 'Saving...' : 'Save All Changes' }}
      </button>
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-8">
      <!-- API & Connectivity -->
      <div class="space-y-6">
        <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl p-6 backdrop-blur-sm">
          <div class="flex items-center space-x-3 mb-6">
            <ShieldCheck class="w-5 h-5 text-[#40baf7]" />
            <h3 class="text-sm font-bold text-white uppercase tracking-widest">Connectivity</h3>
          </div>
          
          <div class="space-y-4">
            <div>
              <label class="text-[10px] uppercase tracking-widest text-gray-500 font-bold block mb-2">Aster User (Wallet)</label>
              <div class="flex space-x-2">
                <input 
                  type="text" v-model="config.walletAddress" 
                  class="flex-1 bg-[#0b0e11] border border-[#2b3139] rounded px-3 py-2 text-xs font-mono text-gray-400 focus:outline-none focus:border-[#40baf7]/30"
                >
                <button class="px-3 bg-gray-800 text-gray-400 rounded hover:bg-gray-700 transition-colors"><ExternalLink class="w-3.5 h-3.5" /></button>
              </div>
            </div>

            <div>
              <label class="text-[10px] uppercase tracking-widest text-gray-500 font-bold block mb-2">Signer Address</label>
              <input 
                type="text" v-model="config.signerAddress" 
                class="w-full bg-[#0b0e11] border border-[#2b3139] rounded px-3 py-2 text-xs font-mono text-gray-400 focus:outline-none focus:border-[#40baf7]/30"
              >
            </div>

            <div class="pt-4">
              <button class="w-full py-2 bg-gray-800 hover:bg-gray-700 text-xs font-bold text-white rounded transition-colors flex items-center justify-center border border-[#2b3139]">
                <Key class="w-3.5 h-3.5 mr-2" />
                Update Secure Signer Key
              </button>
            </div>
          </div>
        </div>

        <!-- Global Risk -->
        <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl p-6 backdrop-blur-sm">
          <div class="flex items-center space-x-3 mb-6">
            <Settings2 class="w-5 h-5 text-[#40baf7]" />
            <h3 class="text-sm font-bold text-white uppercase tracking-widest">Risk Management</h3>
          </div>
          
          <div class="space-y-6">
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label class="text-[10px] uppercase tracking-widest text-gray-500 font-bold block mb-2">Max Positions</label>
                <input 
                  type="number" v-model="config.maxPositions" 
                  class="w-full bg-[#0b0e11] border border-[#2b3139] rounded px-3 py-2 text-sm font-mono text-white focus:outline-none focus:border-[#40baf7]/30"
                >
              </div>
              <div>
                <label class="text-[10px] uppercase tracking-widest text-gray-500 font-bold block mb-2">Daily Drawdown %</label>
                <input 
                  type="number" v-model="config.dailyDrawdown" step="0.1"
                  class="w-full bg-[#0b0e11] border border-[#2b3139] rounded px-3 py-2 text-sm font-mono text-white focus:outline-none focus:border-[#40baf7]/30"
                >
              </div>
            </div>

            <div>
              <div class="flex justify-between mb-2">
                <label class="text-[10px] uppercase tracking-widest text-gray-500 font-bold">Global Stop Loss</label>
                <span class="text-xs font-mono text-[#f84960] font-bold">{{ config.stopLoss }}%</span>
              </div>
              <input 
                type="range" min="0" max="20" step="0.5" v-model="config.stopLoss"
                class="w-full h-1.5 bg-gray-700 rounded-lg appearance-none cursor-pointer accent-[#f84960]"
              >
            </div>
          </div>
        </div>
      </div>

      <!-- Notifications & System -->
      <div class="space-y-6">
        <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl p-6 backdrop-blur-sm">
          <div class="flex items-center space-x-3 mb-6">
            <Bell class="w-5 h-5 text-[#40baf7]" />
            <h3 class="text-sm font-bold text-white uppercase tracking-widest">Notifications</h3>
          </div>
          
          <div class="space-y-4">
            <div>
              <label class="text-[10px] uppercase tracking-widest text-gray-500 font-bold block mb-2">Webhook URL (Discord/Telegram)</label>
              <input 
                type="password" v-model="config.webhookUrl" 
                class="w-full bg-[#0b0e11] border border-[#2b3139] rounded px-3 py-2 text-xs font-mono text-gray-400 focus:outline-none focus:border-[#40baf7]/30"
              >
            </div>

            <div class="space-y-3 pt-2">
              <div v-for="type in ['Fills', 'Liquidations', 'System Errors']" :key="type" class="flex items-center justify-between">
                <span class="text-xs text-gray-300 font-medium">{{ type }}</span>
                <label class="relative inline-flex items-center cursor-pointer">
                  <input type="checkbox" checked class="sr-only peer">
                  <div class="w-9 h-5 bg-gray-700 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-[#0ecb81]"></div>
                </label>
              </div>
            </div>
          </div>
        </div>

        <div class="bg-[#151a1e] border border-[#2b3139] rounded-xl p-6 backdrop-blur-sm">
          <div class="flex items-center space-x-3 mb-6">
            <Info class="w-5 h-5 text-gray-500" />
            <h3 class="text-sm font-bold text-white uppercase tracking-widest">System Info</h3>
          </div>
          
          <div class="space-y-2 text-[10px] font-mono">
            <div class="flex justify-between border-b border-[#2b3139] py-2">
              <span class="text-gray-500 uppercase">Uptime</span>
              <span class="text-gray-300">14d 06h 22m 11s</span>
            </div>
            <div class="flex justify-between border-b border-[#2b3139] py-2">
              <span class="text-gray-500 uppercase">Environment</span>
              <span class="text-gray-300">Production (Go v1.22)</span>
            </div>
            <div class="flex justify-between py-2 text-[#0ecb81]">
              <span class="uppercase">Database Sync</span>
              <span class="font-bold">HEALTHY</span>
            </div>
          </div>
        </div>

        <!-- Danger Zone -->
        <div class="border border-[#f84960]/30 bg-[#f84960]/5 rounded-xl p-6">
          <div class="flex items-center space-x-3 mb-4">
            <ShieldAlert class="w-5 h-5 text-[#f84960]" />
            <h3 class="text-sm font-bold text-[#f84960] uppercase tracking-widest">Danger Zone</h3>
          </div>
          <p class="text-[10px] text-[#f84960]/70 mb-4 italic uppercase tracking-tighter">
            Actions in this area are irreversible. Handle with extreme caution.
          </p>
          <button class="w-full py-2 bg-[#f84960]/10 hover:bg-[#f84960]/20 text-[#f84960] rounded text-[10px] font-bold uppercase tracking-widest transition-colors border border-[#f84960]/20 flex items-center justify-center">
            <Trash2 class="w-3.5 h-3.5 mr-2" />
            Factory Reset Bot Data
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
