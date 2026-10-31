// The live overview: one initial ServerService.Overview() call, then every
// visor:overview event the Go side pushes after each poll and action. The
// frontend never polls supervisord itself.
import { getCurrentScope, onScopeDispose, ref, type Ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { ServerService, type OverviewView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorOverview } from '../../bindings/events'

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

/**
 * useOverview subscribes for the lifetime of the calling component (or
 * effect scope). `error` is set only when no overview could be loaded at
 * all; `reload` retries the initial call.
 */
export function useOverview(): {
  overview: Ref<OverviewView | null>
  error: Ref<string | null>
  reload: () => Promise<void>
} {
  const overview = ref<OverviewView | null>(null)
  const error = ref<string | null>(null)
  let fromEvent = false // an event is newer than any in-flight Overview() call

  const off = Events.On(VisorOverview, (ev) => {
    overview.value = ev.data
    error.value = null
    fromEvent = true
  })
  if (getCurrentScope()) onScopeDispose(off)

  async function reload() {
    fromEvent = false
    try {
      const ov = await ServerService.Overview()
      if (!fromEvent) overview.value = ov
      error.value = null
    } catch (e) {
      if (!fromEvent) error.value = errorMessage(e)
    }
  }
  void reload()

  return { overview, error, reload }
}
