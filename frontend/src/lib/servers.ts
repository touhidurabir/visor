// Saved servers and their tabs on the frontend: opening a tab (with the host
// key prompt when Visor hasn't seen the server), closing one, and the shared
// state of the server form and the host-key dialog, which AppShell shows.
import { ref } from 'vue'
import { RemoteService, type HostKeyView, type ReportView, type SavedServerView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Server } from '../../bindings/github.com/touhidurabir/visor/internal/settings/models'
import { errorMessage } from './overview'
import { forgetTab, homeOf, LOCAL, pathFor, serverTabs, setCurrentTab, currentTab, type TabInfo } from './tabs'
import { showToast } from './toast'

type Router = { push: (to: string) => unknown }

export const TAGS = [
  { id: 'local', label: 'Local', chip: 'LOCAL', cls: 'b-idle' },
  { id: 'development', label: 'Development', chip: 'DEV', cls: 'b-accent' },
  { id: 'testing', label: 'Testing', chip: 'TEST', cls: 'b-idle' },
  { id: 'staging', label: 'Staging', chip: 'STAGING', cls: 'b-warn' },
  { id: 'production', label: 'Production', chip: 'PRODUCTION', cls: 'b-bad' },
] as const

export const COLORS = [
  { name: 'Grey', hex: '#9AA4B2' },
  { name: 'Blue', hex: '#0A62D0' },
  { name: 'Amber', hex: '#E09B1B' },
  { name: 'Green', hex: '#22A75A' },
  { name: 'Red', hex: '#E5483A' },
  { name: 'Violet', hex: '#8B5CF6' },
] as const

export function tagOf(id: string) {
  return TAGS.find((t) => t.id === id) ?? TAGS[1]
}

export function tabInfo(s: Server): TabInfo {
  return { id: s.id, name: s.name, tag: s.tag, color: s.color || COLORS[0].hex, readOnly: s.readOnly }
}

/** The host key waiting for the user, and what to do once it is trusted. */
export const hostKeyPrompt = ref<{ view: HostKeyView; server: string; then: () => void } | null>(null)

/** The server form: null when closed; server is the one being edited. */
export const serverForm = ref<{ server: SavedServerView | null } | null>(null)

/** The latest report per server, for the report page (from Open or a Test). */
export const reports = ref<Record<string, ReportView>>({})

/** Picks the open tabs up from Go (a window that reloads keeps them). */
export async function refreshTabs() {
  try {
    serverTabs.value = ((await RemoteService.Tabs()) ?? []).map(tabInfo)
  } catch {
    serverTabs.value = []
  }
  if (currentTab.value !== LOCAL && !serverTabs.value.some((t) => t.id === currentTab.value)) setCurrentTab(LOCAL)
}

/** switchTab shows a tab on the page it was last on. */
export function switchTab(router: Router, id: string) {
  setCurrentTab(id)
  void router.push(pathFor(id))
}

/**
 * openServer opens (or shows) a saved server's tab. An unknown host key asks
 * first; a refused one says why. A connection with a gap opens on its report.
 */
export async function openServer(router: Router, s: Server): Promise<void> {
  let res
  try {
    res = await RemoteService.Open(s.id)
  } catch (e) {
    showToast(errorMessage(e), 'error')
    return
  }
  if (res.HostKey) {
    hostKeyPrompt.value = { view: res.HostKey, server: s.name, then: () => void openServer(router, s) }
    return
  }
  if (!serverTabs.value.some((t) => t.id === s.id)) serverTabs.value = [...serverTabs.value, tabInfo(s)]
  if (res.Report) reports.value = { ...reports.value, [s.id]: res.Report }
  setCurrentTab(s.id)
  void router.push(res.Report?.Blocked ? `${homeOf(s.id)}/report` : pathFor(s.id))
}

/** closeServer closes a server's tab (it stays saved) and shows the one before it. */
export async function closeServer(router: Router, id: string): Promise<void> {
  if (id === LOCAL) return
  const ids = serverTabs.value.map((t) => t.id)
  const i = ids.indexOf(id)
  try {
    await RemoteService.Close(id)
  } catch (e) {
    showToast(errorMessage(e), 'error')
  }
  forgetTab(id)
  if (currentTab.value === id) {
    const next = serverTabs.value[Math.min(i, serverTabs.value.length - 1)]?.id ?? (i > 0 ? ids[i - 1] : LOCAL)
    switchTab(router, serverTabs.value.some((t) => t.id === next) ? next : LOCAL)
  }
}

/** Updates an open tab's name, tag and colour after its server was edited. */
export function updateTab(s: Server) {
  serverTabs.value = serverTabs.value.map((t) => (t.id === s.id ? tabInfo(s) : t))
}
