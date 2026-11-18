// Saved servers and their tabs on the frontend: opening a tab (with the host
// key prompt when Visor hasn't seen the server), closing one, and the shared
// state of the server form and the host-key dialog, which AppShell shows.
import { ref } from 'vue'
import { RemoteService, type HostKeyView, type OpenView, type ReportView, type SavedServerView, type ServerInput } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Server } from '../../bindings/github.com/touhidurabir/visor/internal/settings/models'
import { errorMessage } from './overview'
import { forgetTab, LOCAL, pathFor, serverTabs, setCurrentTab, currentTab, tabsLoaded, type TabInfo } from './tabs'
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

/**
 * The server form: null when closed; server is the one being edited, and
 * report why it couldn't connect (a server opened from the + menu or the
 * Remote servers page opens no tab then, but its form with the report).
 */
export const serverForm = ref<{ server: SavedServerView | null; report?: ReportView } | null>(null)

/** The latest report per server, for the report page (from Open or a Test). */
export const reports = ref<Record<string, ReportView>>({})

/** Picks the open tabs up from Go (a window that reloads keeps them). */
export async function refreshTabs() {
  try {
    serverTabs.value = ((await RemoteService.Tabs()) ?? []).map(tabInfo)
  } catch {
    serverTabs.value = []
  }
  tabsLoaded.value = true
  if (currentTab.value !== LOCAL && !serverTabs.value.some((t) => t.id === currentTab.value)) setCurrentTab(LOCAL)
}

/** switchTab shows a tab on the page it was last on. */
export function switchTab(router: Router, id: string) {
  setCurrentTab(id)
  void router.push(pathFor(id))
}

/** What opening a server came to: its tab, or why there is none. */
export type OpenResult = { ok: true } | { ok: false; report?: ReportView; error?: string }

/**
 * openServer opens (or shows) a saved server's tab. An unknown host key asks
 * first, then runs retry; a server Visor can't reach opens no tab and comes
 * back with its report.
 */
export async function openServer(router: Router, s: Server, retry: () => void = () => void openServer(router, s)): Promise<OpenResult> {
  return shown(router, () => RemoteService.Open(s.id), s.name, retry)
}

/** connectOnce connects to the form's server without saving it (RemoteService.ConnectOnce). */
export function connectOnce(router: Router, input: ServerInput, retry: () => void): Promise<OpenResult> {
  return shown(router, () => RemoteService.ConnectOnce(input), input.Server.name || input.Server.host, retry)
}

/** The saved servers being opened right now, so a second click waits for the first. */
export const opening = ref<string[]>([])

/** openSaved opens a saved server; one that can't connect opens its form with the report. */
export async function openSaved(router: Router, v: SavedServerView): Promise<void> {
  const id = v.Server.id
  if (opening.value.includes(id)) return
  opening.value = [...opening.value, id]
  try {
    const r = await openServer(router, v.Server, () => void openSaved(router, v))
    if (!r.ok && r.report) serverForm.value = { server: v, report: r.report }
    else if (!r.ok && r.error) showToast(r.error, 'error')
  } finally {
    opening.value = opening.value.filter((o) => o !== id)
  }
}

async function shown(router: Router, open: () => Promise<OpenView>, name: string, retry: () => void): Promise<OpenResult> {
  let res: OpenView
  try {
    res = await open()
  } catch (e) {
    return { ok: false, error: errorMessage(e) }
  }
  if (res.HostKey?.Status === 'unknown') {
    hostKeyPrompt.value = { view: res.HostKey, server: name, then: retry }
    return { ok: false }
  }
  const host = res.Overview?.Host
  if (!host) return { ok: false, report: res.Report ?? undefined }
  if (!serverTabs.value.some((t) => t.id === host.ID)) {
    serverTabs.value = [...serverTabs.value, { id: host.ID, name: host.Name, tag: host.Tag, color: host.Color || COLORS[0].hex, readOnly: host.ReadOnly }]
  }
  setCurrentTab(host.ID)
  void router.push(pathFor(host.ID))
  return { ok: true }
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
