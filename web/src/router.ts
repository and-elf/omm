import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

// Hash-based history keeps all navigation client-side, so the meshd binary only
// ever serves index.html and static assets — no server-side route conflicts
// with the REST API mounted at the same origin.
const routes: RouteRecordRaw[] = [
  { path: '/setup', name: 'setup', component: () => import('@/views/SetupView.vue'), meta: { chrome: false } },
  { path: '/', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
  { path: '/homes', name: 'homes', component: () => import('@/views/HomesView.vue') },
  { path: '/homes/:id', name: 'home-detail', component: () => import('@/views/HomeDetailView.vue') },
  { path: '/enroll', name: 'enroll', component: () => import('@/views/EnrollView.vue') },
  { path: '/nodes', name: 'nodes', component: () => import('@/views/NodesView.vue') },
  { path: '/nodes/:id', name: 'node-detail', component: () => import('@/views/NodeDetailView.vue') },
  { path: '/topology', name: 'topology', component: () => import('@/views/TopologyView.vue') },
  { path: '/settings', name: 'settings', component: () => import('@/views/SettingsView.vue') },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
