import { describe, expect, it, vi } from 'vitest'

vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({
  ServerAPI: { Start: vi.fn((server: string, name: string) => Promise.resolve(`${server}:${name}`)) },
  ConfigAPI: {},
  IssueAPI: {},
  LogAPI: { Save: vi.fn(() => Promise.resolve('/tmp/web.log')) },
  InboxAPI: {},
}))

const { LogService, ServerService, forTab } = await import('./api')
const { productionAsk } = await import('./production')
const { serverTabs } = await import('./tabs')
const { setCurrentTab } = await import('./tabs')

describe('api', () => {
  it('puts the tab on screen first in every call', async () => {
    setCurrentTab('local')
    expect(await ServerService.Start('web')).toBe('local:web')
    setCurrentTab('s0123456789')
    expect(await ServerService.Start('web')).toBe('s0123456789:web')
    setCurrentTab('')
    expect(await ServerService.Start('web')).toBe('local:web')
  })
  it('forTab keeps its tab whatever is on screen', async () => {
    setCurrentTab('local')
    expect(await forTab('sabc').ServerService.Start('x')).toBe('sabc:x')
  })
  it('saving a copy of a log never asks, even on production (Save is guarded on configs only)', async () => {
    serverTabs.value = [{ id: 'sprod', name: 'prod', tag: 'production', color: '', readOnly: false }]
    setCurrentTab('sprod')
    expect(await LogService.Save('web', 'stdout')).toBe('/tmp/web.log')
    expect(productionAsk.value).toBeNull()
  })
})
