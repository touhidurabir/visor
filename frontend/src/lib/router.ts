// vue-router in hash mode: the Go side opens the main window at #/ and the
// menu-bar popover at #/tray. Routes with meta.bare render without AppShell.
import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'
import AddForm from '../views/AddForm.vue'
import AddRaw from '../views/AddRaw.vue'
import ConfigDetail from '../views/ConfigDetail.vue'
import Dashboard from '../views/Dashboard.vue'
import Issues from '../views/Issues.vue'
import Placeholder from '../views/Placeholder.vue'
import Settings from '../views/Settings.vue'
import Tray from '../views/Tray.vue'

export const routes: RouteRecordRaw[] = [
  { path: '/', name: 'dashboard', component: Dashboard },
  { path: '/config/:path(.*)', name: 'config', component: ConfigDetail, meta: { title: 'Config' } },
  { path: '/issues', name: 'issues', component: Issues, meta: { title: 'Issues' } },
  { path: '/issues/log', name: 'issues-log', component: Issues, meta: { title: 'Issues' } },
  { path: '/new', name: 'new', component: AddRaw, meta: { title: 'Add new' } },
  { path: '/new/form', name: 'new-form', component: AddForm, meta: { title: 'Add new' } },
  { path: '/settings', name: 'settings', component: Settings, meta: { title: 'Settings' } },
  { path: '/doctor', name: 'doctor', component: Placeholder, meta: { title: 'Doctor' } },
  { path: '/tray', name: 'tray', component: Tray, meta: { bare: true } },
  { path: '/:rest(.*)*', redirect: '/' },
]

export function createAppRouter(history = createWebHashHistory()) {
  return createRouter({ history, routes })
}
