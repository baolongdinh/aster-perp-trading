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
      path: '/activity',
      name: 'activity',
      component: () => import('../views/Activity.vue')
    }
  ]
})

export default router
