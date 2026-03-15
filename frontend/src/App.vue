<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { 
  LayoutDashboard, 
  Zap, 
  History, 
  Settings, 
  Activity,
  ChevronRight
} from 'lucide-vue-next'


const router = useRouter()
const route = useRoute()

const isSidebarOpen = ref(true)

const navItems = [
  { name: 'Dashboard', icon: LayoutDashboard, path: '/' },
  { name: 'Strategies', icon: Zap, path: '/strategies' },
  { name: 'Trades', icon: History, path: '/trades' },
  { name: 'Config', icon: Settings, path: '/config' },
]

const navigate = (path: string) => {
  router.push(path)
}
</script>

<template>
  <div class="min-h-screen bg-[#0b0e11] text-gray-100 flex font-sans">
    <!-- Sidebar -->
    <aside 
      :class="[
        'bg-[#151a1e] border-r border-[#2b3139] transition-all duration-300 flex flex-col',
        isSidebarOpen ? 'w-64' : 'w-20'
      ]"
    >
      <!-- Logo Area -->
      <div class="h-16 flex items-center px-6 border-b border-[#2b3139]">
        <Activity class="text-[#40baf7] w-8 h-8 shrink-0" />
        <span v-if="isSidebarOpen" class="ml-3 font-bold text-xl tracking-tight text-white italic">ASTER<span class="text-[#40baf7] font-normal not-italic">BOT</span></span>
      </div>

      <!-- Navigation -->
      <nav class="flex-1 py-6 px-3 space-y-1">
        <button
          v-for="item in navItems"
          :key="item.path"
          @click="navigate(item.path)"
          :class="[
            'w-full flex items-center p-3 rounded-lg transition-colors group relative',
            route.path === item.path 
              ? 'bg-[#40baf7]/10 text-[#40baf7]' 
              : 'text-gray-400 hover:bg-gray-800 hover:text-white'
          ]"
        >
          <component :is="item.icon" class="w-6 h-6 shrink-0" />
          <span v-if="isSidebarOpen" class="ml-4 font-medium">{{ item.name }}</span>
          
          <!-- Active Indicator -->
          <div 
            v-if="route.path === item.path" 
            class="absolute left-0 w-1 h-6 bg-[#40baf7] rounded-r-full"
          ></div>

          <!-- Tooltip (Minified Mode) -->
          <div 
            v-if="!isSidebarOpen"
            class="absolute left-full ml-4 px-2 py-1 bg-gray-900 text-white text-xs rounded opacity-0 group-hover:opacity-100 pointer-events-none transition-opacity whitespace-nowrap z-50 border border-gray-700 shadow-xl"
          >
            {{ item.name }}
          </div>
        </button>
      </nav>

      <!-- Footer / Collapse Toggle -->
      <div class="p-4 border-t border-[#2b3139]">
        <button 
          @click="isSidebarOpen = !isSidebarOpen"
          class="w-full h-10 flex items-center justify-center rounded-lg hover:bg-gray-800 text-gray-400 transition-colors"
        >
          <ChevronRight :class="['w-5 h-5 transition-transform duration-300', isSidebarOpen ? 'rotate-180' : '']" />
        </button>
      </div>
    </aside>

    <!-- Main Content -->
    <main class="flex-1 flex flex-col min-w-0 overflow-hidden">
      <!-- Top Header Area (Optional, depending on view) -->
      <header class="h-16 flex items-center justify-between px-8 border-b border-[#2b3139] bg-[#151a1e]/50 backdrop-blur-md sticky top-0 z-10">
        <div class="flex items-center space-x-4">
          <div class="flex items-center space-x-2">
            <span class="w-2 h-2 rounded-full bg-[#0ecb81] animate-pulse"></span>
            <span class="text-xs font-semibold text-[#0ecb81] uppercase tracking-widest">BOT ONLINE</span>
          </div>
          <div class="h-4 w-px bg-gray-700"></div>
          <span class="text-sm text-gray-400 font-mono tracking-tighter">API: fapi.asterdex.com</span>
        </div>
        
        <div class="flex items-center space-x-6">
          <div class="text-right">
            <div class="text-[10px] text-gray-500 uppercase tracking-widest leading-none mb-1">Account Equity</div>
            <div class="text-sm font-mono font-bold text-white">$50,245.82</div>
          </div>
          <div class="w-10 h-10 rounded-full bg-[#40baf7]/20 border border-[#40baf7]/50 flex items-center justify-center text-[#40baf7] font-bold shadow-lg shadow-[#40baf7]/10">
            A
          </div>
        </div>
      </header>

      <!-- Page View -->
      <div class="flex-1 overflow-y-auto custom-scrollbar">
        <router-view v-slot="{ Component }">
          <transition 
            name="fade" 
            mode="out-in"
          >
            <component :is="Component" />
          </transition>
        </router-view>
      </div>
    </main>
  </div>
</template>

<style>
/* Custom Scrollbar */
.custom-scrollbar::-webkit-scrollbar {
  width: 6px;
}
.custom-scrollbar::-webkit-scrollbar-track {
  background: #0b0e11;
}
.custom-scrollbar::-webkit-scrollbar-thumb {
  background: #2b3139;
  border-radius: 10px;
}
.custom-scrollbar::-webkit-scrollbar-thumb:hover {
  background: #40baf7;
}

/* Page Transitions */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.fade-enter-from {
  opacity: 0;
  transform: translateY(5px);
}

.fade-leave-to {
  opacity: 0;
  transform: translateY(-5px);
}
</style>
