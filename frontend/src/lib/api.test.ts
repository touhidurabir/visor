import { describe, expect, it, vi } from 'vitest'

vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({
  ServerAPI: { Start: vi.fn((server: string, name: string) => Promise.resolve(`${server}:${name}`)) },
  ConfigAPI: {},
  IssueAPI: {},
  LogAPI: {},
  InboxAPI: {},
}))

const { ServerService, forTab } = await import('./api')
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
})
