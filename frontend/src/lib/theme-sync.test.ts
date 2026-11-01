import { beforeEach, describe, expect, it, vi } from 'vitest'

// The Wails event source, the bindings and the DOM bits theme-sync touches,
// faked: each SetAppearance call waits until the test settles it.
const handlers = new Map<string, (ev: { data: unknown }) => void>()
type Call = { appearance: string; resolve: () => void; reject: (e: unknown) => void }
const calls: Call[] = []
const view = (appearance: string) => ({ Settings: { appearance } })

vi.mock('@wailsio/runtime', () => ({
  Events: { On: (name: string, cb: (ev: { data: unknown }) => void) => handlers.set(name, cb) },
}))
vi.mock('../../bindings/events', () => ({ VisorSettings: 'visor:settings' }))
vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({
  SettingsService: {
    Get: () => Promise.resolve(view('system')),
    SetAppearance: (appearance: string) =>
      new Promise((resolve, reject) => calls.push({ appearance, resolve: () => resolve(view(appearance)), reject })),
  },
}))

// Vue's DOM runtime reads the real document when it loads: load it first.
await import('vue')
const root = { dataset: {} as Record<string, string | undefined>, style: { colorScheme: '' } }
vi.stubGlobal('document', { documentElement: root })
vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => {} })

const { appearance, setAppearance, startThemeSync } = await import('./theme-sync')

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('theme sync', () => {
  beforeEach(async () => {
    startThemeSync()
    await flush()
    calls.length = 0
  })

  it('saves one choice at a time and the last one wins', async () => {
    const first = setAppearance('light')
    const second = setAppearance('dark')
    expect(appearance.value).toBe('dark')
    expect(calls.map((c) => c.appearance)).toEqual(['light'])

    // An older event that lands meanwhile does not move the switch.
    handlers.get('visor:settings')!({ data: view('system') })
    expect(appearance.value).toBe('dark')

    calls[0].resolve()
    await flush()
    expect(calls.map((c) => c.appearance)).toEqual(['light', 'dark'])
    calls[1].resolve()
    await Promise.all([first, second])
    expect(appearance.value).toBe('dark')
    expect(root.dataset.theme).toBe('dark')
  })

  it('puts the stored choice back when the save fails', async () => {
    const p = setAppearance('light')
    expect(appearance.value).toBe('light')
    calls[0].reject(new Error('disk full'))
    await expect(p).rejects.toThrow('disk full')
    expect(appearance.value).toBe('dark') // what the previous test stored
  })
})
