// What changed on a tab's supervisord, by Visor or outside it (Go's
// activity log): the Activity page's list and the dashboard's strip, kept
// current by visor:activity events for that tab.
import { getCurrentScope, onScopeDispose, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { IssueAPI, type ActivityEventView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorActivity } from '../../bindings/events'
import { errorMessage } from './overview'
import { currentTab, LOCAL } from './tabs'

export const KIND_GROUPS = [
  { id: '', label: 'All kinds' },
  { id: 'processes', label: 'Started, stopped, restarted' },
  { id: 'programs', label: 'Programs added or removed' },
  { id: 'files', label: 'Config files' },
  { id: 'daemon', label: 'supervisord' },
] as const

export interface ActivityFilter {
  who: 'all' | 'outside' | 'visor'
  kind: string
  process: string
}

export function matches(e: ActivityEventView, f: ActivityFilter): boolean {
  if (f.who === 'outside' && e.ByVisor) return false
  if (f.who === 'visor' && !e.ByVisor) return false
  if (f.kind && e.Group !== f.kind && e.Kind !== f.kind) return false
  if (f.process && e.Process !== f.process && e.Config !== f.process) return false
  return true
}

/** useActivity loads a tab's activity (newest first) and follows new events. */
export function useActivity(tab: string = currentTab.value, limit = 500) {
  const events = ref<ActivityEventView[]>([])
  const error = ref('')
  const loaded = ref(false)
  async function reload() {
    try {
      const v = await IssueAPI.Activity(tab, { Kind: '', Process: '', Outside: false, Limit: limit })
      events.value = v.Events ?? []
      error.value = ''
    } catch (e) {
      error.value = errorMessage(e)
    } finally {
      loaded.value = true
    }
  }
  const off = Events.On(VisorActivity, (ev: { data: { Server: string; Events: ActivityEventView[] | null } }) => {
    if ((ev.data?.Server || LOCAL) !== tab) return
    events.value = [...[...(ev.data.Events ?? [])].reverse(), ...events.value].slice(0, limit)
  })
  if (getCurrentScope()) onScopeDispose(off)
  void reload()
  return { events, error, loaded, reload }
}

export interface DayGroup {
  label: string
  events: ActivityEventView[]
}

/** byDay splits newest-first events into Today, Yesterday and dated days. */
export function byDay(events: ActivityEventView[], nowMs: number): DayGroup[] {
  const now = new Date(nowMs)
  const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const day = 24 * 60 * 60 * 1000
  const out: DayGroup[] = []
  for (const e of events) {
    let label: string
    if (e.AtMs >= midnight) label = 'Today'
    else if (e.AtMs >= midnight - day) label = 'Yesterday'
    else label = new Date(e.AtMs).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })
    const last = out[out.length - 1]
    if (last?.label === label) last.events.push(e)
    else out.push({ label, events: [e] })
  }
  return out
}

/** The clock time an event shows: "14:05". */
export function clock(atMs: number): string {
  return new Date(atMs).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}
