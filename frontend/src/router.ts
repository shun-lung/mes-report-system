import { createRouter, createWebHistory } from 'vue-router'
import { useSession } from './stores/session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('./views/Login.vue'), meta: { public: true } },
    {
      path: '/',
      component: () => import('./components/AppLayout.vue'),
      children: [
        { path: '', component: () => import('./views/Dashboard.vue'), meta: { title: '生產 8HR 看板', perm: 'report:view' } },
        { path: 'orders', component: () => import('./views/Orders.vue'), meta: { title: '訂單與製令', perm: 'wo:view' } },
        { path: 'inventory', component: () => import('./views/Inventory.vue'), meta: { title: '庫存與歷程', perm: 'inventory:view' } },
        { path: 'alerts', component: () => import('./views/Alerts.vue'), meta: { title: '缺料預警', perm: '' } },
        { path: 'access', component: () => import('./views/Access.vue'), meta: { title: '權限管理', perm: 'role:view' } },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  const session = useSession()
  if (to.meta.public) return true
  if (!session.token) return '/login'
  if (!session.user) {
    try {
      await session.loadMe()
    } catch {
      return '/login'
    }
  }
  return true
})
