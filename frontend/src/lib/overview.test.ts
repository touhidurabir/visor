import { beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick } from 'vue'
import type { OverviewView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

// The Wails event source and the generated bindings, mocked: tests drive
// the overview by calling the captured event handler.
const handlers = new Map<string, (ev: { data: unknown }) => void>()
const unsubscribe = vi.fn()
const overviewCall = vi.fn<() => Promise<OverviewView>>()

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (ev: { data: unknown }) => void) => {
      handlers.set(name, cb)
      return () => {
        handlers.delete(name)
        unsubscribe()
      }
    },
  },
}))
vi.mock('../../bindings/events', () => ({ VisorOverview: 'visor:overview' }))
vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({
  ServerService: { Overview: () => overviewCall() },
}))

const { useOverview } = await import('./overview')

function view(health: string, running: number): OverviewView {
  return {
    Server: { Name: 'Local', Version: '', UptimeMs: 0, ConfigPath: '', Reachable: true, Error: '', Health: health },
    Stats: { Configs: 0, Processes: running, Running: running, Failing: 0, Stopped: 0, OpenIssues: 0 },
    Configs: [],
  }
}

function emit(data: OverviewView) {
  const cb = handlers.get('visor:overview')
  if (!cb) throw new Error('no visor:overview subscriber')
  cb({ data })
}

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('useOverview', () => {
  beforeEach(() => {
    handlers.clear()
    unsubscribe.mockClear()
    overviewCall.mockReset()
  })

  it('loads the initial overview, then follows visor:overview events', async () => {
    overviewCall.mockResolvedValue(view('ok', 1))
    const scope = effectScope()
    const { overview, error } = scope.run(() => useOverview())!
    await flush()
    expect(overview.value?.Stats.Running).toBe(1)
    expect(error.value).toBeNull()

    emit(view('error', 2))
    await nextTick()
    expect(overview.value?.Server.Health).toBe('error')
    expect(overview.value?.Stats.Running).toBe(2)

    scope.stop()
    expect(unsubscribe).toHaveBeenCalledOnce()
    expect(handlers.has('visor:overview')).toBe(false)
  })

  it('keeps an event that arrives before the initial call returns', async () => {
    let resolve!: (v: OverviewView) => void
    overviewCall.mockReturnValue(new Promise((r) => (resolve = r)))
    const scope = effectScope()
    const { overview } = scope.run(() => useOverview())!
    emit(view('warning', 3))
    resolve(view('ok', 1)) // older than the event
    await flush()
    expect(overview.value?.Server.Health).toBe('warning')
    scope.stop()
  })

  it('reports a failed initial call, and clears it on the next event', async () => {
    overviewCall.mockRejectedValue(new Error('boom'))
    const scope = effectScope()
    const { overview, error } = scope.run(() => useOverview())!
    await flush()
    expect(overview.value).toBeNull()
    expect(error.value).toBe('boom')

    emit(view('ok', 1))
    expect(error.value).toBeNull()
    expect(overview.value?.Stats.Running).toBe(1)
    scope.stop()
  })
})
