import { describe, expect, it, vi } from 'vitest'

let finish: (v: unknown) => void = () => {}
const open = vi.fn(() => new Promise((r) => (finish = r)))
vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({
  RemoteService: { Open: () => open() },
}))

const { openSaved, opening } = await import('./servers')

describe('openSaved', () => {
  it('ignores a second Connect while the first is still connecting', async () => {
    const router = { push: () => undefined }
    const v = { Server: { id: 's1', name: 'One' } } as never
    const first = openSaved(router, v)
    await openSaved(router, v)
    expect(open).toHaveBeenCalledTimes(1)
    expect(opening.value).toEqual(['s1'])
    finish({ Report: null })
    await first
    expect(opening.value).toEqual([])
  })
})
