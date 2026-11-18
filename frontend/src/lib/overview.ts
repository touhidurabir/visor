// The live overview: one initial ServerService.Overview() call, then every
// visor:overview event the Go side pushes after each poll and action. The
// frontend never polls supervisord itself.
import { getCurrentScope, onScopeDispose, ref, watch, type Ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { type ExplainView, type OverviewView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { ServerAPI } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorOverview } from '../../bindings/events'
import { isProblem } from './explain'
import { currentTab, LOCAL } from './tabs'

export function errorMessage(e: unknown): string {
  if (e instanceof Error && e.name === 'CancelledError') return '' // the person said no
  return e instanceof Error ? e.message : String(e)
}

/**
 * useOverview subscribes for the lifetime of the calling component (or
 * effect scope). `error` is set only when no overview could be loaded at
 * all; `reload` retries the initial call.
 */
export function useOverview(tab?: () => string): {
  overview: Ref<OverviewView | null>
  error: Ref<string | null>
  reload: () => Promise<void>
} {
  // Events come for every open tab; this keeps the one on screen (or the
  // one tab() names, for the popover's sections).
  const which = tab ?? (() => currentTab.value)
  const overview = ref<OverviewView | null>(null)
  const error = ref<string | null>(null)
  let fromEvent = false // an event is newer than any in-flight Overview() call

  const off = Events.On(VisorOverview, (ev) => {
    if ((ev.data?.Host?.ID || LOCAL) !== which()) return
    overview.value = ev.data
    error.value = null
    fromEvent = true
  })
  if (getCurrentScope()) onScopeDispose(off)

  async function reload() {
    fromEvent = false
    const id = which()
    try {
      const ov = await ServerAPI.Overview(id)
      if (id !== which()) return
      if (!fromEvent) overview.value = ov
      error.value = null
    } catch (e) {
      if (!fromEvent && id === which()) error.value = errorMessage(e)
    }
  }
  void reload()
  if (!tab && getCurrentScope()) {
    watch(currentTab, () => {
      overview.value = null
      error.value = null
      void reload()
    })
  }

  return { overview, error, reload }
}

/**
 * opensOnProblems is where a config's Overview starts its explanation
 * (flow 2, approved 2026-11-04): on "Only problems" when the file has an
 * error, a warning or a suggestion, so what needs you is first; on "All
 * lines" when it has none, or before it has loaded, or when it can't be
 * read (that error shows instead of the lines). The user can still switch.
 * With one card per program, "Only problems" leaves out the cards with no
 * problem line, but never a failing one (buildCards in explain-cards).
 */
/** placeName names the tab's place: a server's own name, else this computer's profile ("Local"). */
export function placeName(o: OverviewView | null | undefined): string {
  return (o?.Host?.Remote ? o.Host.Name : o?.Server.Name) || 'Local'
}

export function opensOnProblems(view: ExplainView | null): boolean {
  if (!view || view.ParseError) return false
  if (view.Errors + view.Suggestions > 0 || (view.FileProblems?.length ?? 0) > 0) return true
  return (view.Sections ?? []).some((s) => (s.Lines ?? []).some(isProblem))
}
