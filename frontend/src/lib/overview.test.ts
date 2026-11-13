import { beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick } from 'vue'
import type { ExplainLineView, ExplainView, OverviewView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

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
  ServerAPI: { Overview: () => overviewCall() },
}))

const { opensOnProblems, useOverview } = await import('./overview')

function view(health: string, running: number): OverviewView {
  return {
    Host: { ID: 'local', Name: 'This Mac', Remote: false, Tag: '', Color: '', ReadOnly: false, Sudo: false, Via: '', Address: '', Conn: '', ConnError: '' },
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

describe('opensOnProblems', () => {
  const line = (Level: string): ExplainLineView => ({ Line: 1, Text: 'x=1', Key: 'x', Kind: 'key', Meaning: '', Level, Problems: null, Suggest: '', Add: null, Paths: null })
  const explain = (o: Partial<ExplainView> = {}): ExplainView => ({
    Summary: '', Errors: 0, SaveErrors: 0, Suggestions: 0, Settings: 1, Paths: 0, ParseError: null, FileProblems: null,
    Sections: [{ Name: 'program:a', Program: 'a', Summary: '', Failure: '', Lines: [line('')] }],
    ...o,
  })

  it('opens on "All lines" for a file with no problems', () => {
    expect(opensOnProblems(explain())).toBe(false)
  })
  it('opens on "Only problems" for an error or a suggestion', () => {
    expect(opensOnProblems(explain({ Errors: 1 }))).toBe(true)
    expect(opensOnProblems(explain({ Suggestions: 2 }))).toBe(true)
  })
  it('opens on "Only problems" for a problem that is not on a line', () => {
    expect(opensOnProblems(explain({ FileProblems: [{ Level: 'warning', Live: false, Line: 0, Message: 'm', Fix: '', Edit: null }] }))).toBe(true)
  })
  it('opens on "Only problems" when a line is flagged, whatever the counts say', () => {
    expect(opensOnProblems(explain({ Sections: [{ Name: 'program:a', Program: 'a', Summary: '', Failure: '', Lines: [line(''), line('suggestion')] }] }))).toBe(true)
  })
  it('opens on "All lines" before the explanation loads, or when the file cannot be read', () => {
    expect(opensOnProblems(null)).toBe(false)
    expect(opensOnProblems(explain({ Errors: 1, ParseError: { Level: 'error', Live: false, Line: 3, Message: 'bad', Fix: '', Edit: null } }))).toBe(false)
  })
})
