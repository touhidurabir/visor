// vue-router in hash mode: the Go side opens the main window at #/ and the
// menu-bar popover at #/tray. Routes with meta.bare render without AppShell.
import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'
import AddForm from '../views/AddForm.vue'
import AddImport from '../views/AddImport.vue'
import Activity from '../views/Activity.vue'
import AddRaw from '../views/AddRaw.vue'
import ConfigDetail from '../views/ConfigDetail.vue'
import Dashboard from '../views/Dashboard.vue'
import Doctor from '../views/Doctor.vue'
import Issues from '../views/Issues.vue'
import RemoteServers from '../views/RemoteServers.vue'
import ServerReport from '../views/ServerReport.vue'
import Settings from '../views/Settings.vue'
import Tray from '../views/Tray.vue'
import { tabGuard } from './tabs'

export const routes: RouteRecordRaw[] = [
  { path: '/', name: 'dashboard', component: Dashboard },
  { path: '/config/:path(.*)', name: 'config', component: ConfigDetail, meta: { title: 'Config' } },
  { path: '/issues', name: 'issues', component: Issues, meta: { title: 'Issues' } },
  { path: '/issues/log', name: 'issues-log', component: Issues, meta: { title: 'Issues' } },
  { path: '/activity', name: 'activity', component: Activity, meta: { title: 'Activity' } },
  { path: '/new', name: 'new', component: AddRaw, meta: { title: 'Add new' } },
  { path: '/new/form', name: 'new-form', component: AddForm, meta: { title: 'Add new' } },
  { path: '/new/import', name: 'new-import', component: AddImport, meta: { title: 'Add new' } },
  { path: '/settings', name: 'settings', component: Settings, meta: { title: 'Settings' } },
  { path: '/doctor', name: 'doctor', component: Doctor, meta: { title: 'Doctor' } },
  { path: '/servers', name: 'servers', component: RemoteServers, meta: { title: 'Remote servers' } },
  { path: '/tray', name: 'tray', component: Tray, meta: { bare: true } },
  // A server's tab: the same pages under /s/<server id> (lib/tabs.ts).
  { path: '/s/:server', name: 'server-dashboard', component: Dashboard },
  { path: '/s/:server/config/:path(.*)', name: 'server-config', component: ConfigDetail, meta: { title: 'Config' } },
  { path: '/s/:server/issues', name: 'server-issues', component: Issues, meta: { title: 'Issues' } },
  { path: '/s/:server/issues/log', name: 'server-issues-log', component: Issues, meta: { title: 'Issues' } },
  { path: '/s/:server/activity', name: 'server-activity', component: Activity, meta: { title: 'Activity' } },
  { path: '/s/:server/report', name: 'server-report', component: ServerReport, meta: { title: 'Server report' } },
  { path: '/s/:server/new', name: 'server-new', component: AddRaw, meta: { title: 'Add new' } },
  { path: '/s/:server/new/form', name: 'server-new-form', component: AddForm, meta: { title: 'Add new' } },
  { path: '/s/:server/new/import', name: 'server-new-import', component: AddImport, meta: { title: 'Add new' } },
  { path: '/:rest(.*)*', redirect: '/' },
]

export function createAppRouter(history = createWebHashHistory()) {
  const router = createRouter({ history, routes })
  router.beforeEach(tabGuard)
  return router
}
