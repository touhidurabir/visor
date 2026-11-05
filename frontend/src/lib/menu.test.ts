import { beforeEach, describe, expect, it, vi } from 'vitest'

const handlers = new Map<string, (ev: { data: unknown }) => void>()
vi.mock('@wailsio/runtime', () => ({
  Events: { On: (name: string, cb: (ev: { data: unknown }) => void) => (handlers.set(name, cb), () => handlers.delete(name)) },
}))
vi.mock('../../bindings/events', () => ({ VisorMenu: 'visor:menu' }))

const { followMenu, onMenu, takePendingMenu, resetMenu } = await import('./menu')

const send = (name: string) => handlers.get('visor:menu')?.({ data: name })

describe('menu bar choices', () => {
  const push = vi.fn()
  beforeEach(() => {
    resetMenu()
    push.mockReset()
    followMenu({ push })
  })

  it('runs the handler of the page that is showing', () => {
    const find = vi.fn()
    const off = onMenu('find', find)
    send('find')
    expect(find).toHaveBeenCalledOnce()
    off()
    send('find')
    expect(find).toHaveBeenCalledOnce()
    expect(push).not.toHaveBeenCalled()
  })

  it('takes Reload Config and Restart All to the dashboard when no page handles them', () => {
    send('reload')
    expect(push).toHaveBeenCalledWith('/')
    expect(takePendingMenu('restart-all')).toBe(false)
    expect(takePendingMenu('reload')).toBe(true)
    expect(takePendingMenu('reload')).toBe(false)
  })

  it('takes the popover’s Open Doctor to Doctor', () => {
    send('doctor')
    expect(push).toHaveBeenCalledWith('/doctor')
    expect(takePendingMenu('doctor')).toBe(false)
  })

  it('ignores Find where there is nothing to search', () => {
    send('find')
    expect(push).not.toHaveBeenCalled()
    expect(takePendingMenu('find')).toBe(false)
  })
})
