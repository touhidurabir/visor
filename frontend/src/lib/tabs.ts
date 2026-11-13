// The tabs across the top of the main window: this computer first (it can't
// be closed), then the servers open this launch, in the order opened. Every
// per-tab service call (lib/api.ts) goes to the tab on screen.
import { ref } from 'vue'
import type { RouteLocationNormalized, RouteLocationRaw } from 'vue-router'

export const LOCAL = 'local'

export const currentTab = ref<string>(LOCAL)

/** setCurrentTab switches the tab the views and api calls use. */
export function setCurrentTab(id: string) {
  currentTab.value = id || LOCAL
}

export interface TabInfo {
  id: string
  name: string
  tag: string
  color: string
  readOnly: boolean
}

/** The open server tabs, in the order opened (the local tab is not listed). */
export const serverTabs = ref<TabInfo[]>([])
/** serverTabs has been loaded from Go (refreshTabs). */
export const tabsLoaded = ref(false)

export function isServerOpen(id: string): boolean {
  return serverTabs.value.some((t) => t.id === id)
}

/** The tab's home: the dashboard. */
export function homeOf(id: string): string {
  return id === LOCAL ? '/' : `/s/${id}`
}

// Each tab remembers the page it was on, so switching back returns there.
const lastPath = new Map<string, string>()

export function rememberPath(id: string, fullPath: string) {
  lastPath.set(id, fullPath)
}

export function pathFor(id: string): string {
  return lastPath.get(id) ?? homeOf(id)
}

export function forgetTab(id: string) {
  lastPath.delete(id)
  serverTabs.value = serverTabs.value.filter((t) => t.id !== id)
}

/** The tab after (or before, with -1) the one on screen, wrapping round. */
export function neighbour(step: 1 | -1): string {
  const ids = [LOCAL, ...serverTabs.value.map((t) => t.id)]
  const i = Math.max(0, ids.indexOf(currentTab.value))
  return ids[(i + step + ids.length) % ids.length]
}

// Pages that belong to a tab, and their server-tab counterparts. Doctor and
// Add new are this computer's; on a server they become the server report
// and the dashboard (configs on servers are read only).
const SERVER_ROUTE: Record<string, string> = {
  dashboard: 'server-dashboard',
  config: 'server-config',
  issues: 'server-issues',
  'issues-log': 'server-issues-log',
  doctor: 'server-report',
  new: 'server-dashboard',
  'new-form': 'server-dashboard',
  'new-import': 'server-dashboard',
}

/**
 * tabGuard keeps navigation inside the tab on screen: a link to "/issues"
 * from a server tab goes to that server's Issues. A server route sets the
 * tab (history, a reload). Settings belongs to no tab.
 */
export function tabGuard(to: RouteLocationNormalized): RouteLocationRaw | true {
  if (to.meta.bare) return true
  const server = typeof to.params.server === 'string' ? to.params.server : ''
  if (server) {
    // A closed tab (history, a reload after it closed) goes to this computer.
    if (tabsLoaded.value && !serverTabs.value.some((t) => t.id === server)) {
      setCurrentTab(LOCAL)
      return '/'
    }
    setCurrentTab(server)
    return true
  }
  const cur = currentTab.value
  const name = String(to.name ?? '')
  if (cur !== LOCAL && SERVER_ROUTE[name]) {
    return { name: SERVER_ROUTE[name], params: { ...to.params, server: cur }, query: to.query, hash: to.hash }
  }
  return true
}

/** tabHref is a tab page's path in the tab on screen: "/issues" or "/s/<id>/issues". */
export function tabHref(path: string): string {
  const id = currentTab.value
  if (id === LOCAL) return path
  return path === '/' ? homeOf(id) : `/s/${id}${path}`
}

/** baseName is a route's name without the server prefix ("server-issues" → "issues"). */
export function baseName(name: unknown): string {
  return String(name ?? '').replace(/^server-/, '')
}
