import { createRouter, createWebHistory } from 'vue-router'
import { useSession } from './stores/session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('./views/Login.vue'), meta: { public: true } },
    { path: '/station', component: () => import('./views/Station.vue') },
    { path: '/print/cards/:id', component: () => import('./views/PrintCards.vue') },
    {
      path: '/',
      component: () => import('./components/AppLayout.vue'),
      children: [
        { path: '', component: () => import('./views/Dashboard.vue'), meta: { title: '生產 8HR 看板', perm: 'report:view' } },
        { path: 'orders', component: () => import('./views/Orders.vue'), meta: { title: '訂單與製令', perm: 'wo:view' } },
        { path: 'products', component: () => import('./views/master/Products.vue'), meta: { title: '產品資料', perm: 'product:view', group: '基本資料' } },
        { path: 'stations', component: () => import('./views/master/Stations.vue'), meta: { title: '站點與線別', perm: 'station:view', group: '基本資料' } },
        { path: 'defects', component: () => import('./views/master/DefectCodes.vue'), meta: { title: '不良代碼', perm: 'station:view', group: '基本資料' } },
        { path: 'warehouse', component: () => import('./views/Warehouse.vue'), meta: { title: '倉儲作業', perm: 'inventory:view', group: '倉儲' } },
        { path: 'inventory', component: () => import('./views/Inventory.vue'), meta: { title: '庫存與歷程', perm: 'inventory:view', group: '倉儲' } },
        { path: 'alerts', component: () => import('./views/Alerts.vue'), meta: { title: '缺料預警', perm: '', group: '倉儲' } },
        { path: 'access', component: () => import('./views/Access.vue'), meta: { title: '權限管理', perm: 'role:view', group: '系統' } },
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
