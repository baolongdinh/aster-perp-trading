import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'dashboard',
      component: () => import('../views/Dashboard.vue')
    },
    {
      path: '/strategies',
      name: 'strategies',
      component: () => import('../views/Strategies.vue')
    },
    {
      path: '/trades',
      name: 'trades',
      component: () => import('../views/Trades.vue')
    },
    {
      path: '/config',
      name: 'config',
      component: () => import('../views/Config.vue')
    }
  ]
})

export default router
