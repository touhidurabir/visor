import { describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ ConfigService: { Folder: vi.fn() } }))

const { readOnlyReason } = await import('./folder')

describe('readOnlyReason', () => {
  it('is empty while configs can be written', () => {
    expect(readOnlyReason(null)).toBe('')
    expect(readOnlyReason({ Dir: '/home/ana/conf.d', Writable: true, Missing: false, Owner: '', Remote: false })).toBe('')
    expect(readOnlyReason({ Dir: '', Writable: false, Missing: false, Owner: '', Remote: false })).toBe('')
  })
  it('names the owner of a folder root keeps', () => {
    expect(readOnlyReason({ Dir: '/etc/supervisor/conf.d', Writable: false, Missing: false, Owner: 'root', Remote: false })).toBe('Read-only: /etc/supervisor/conf.d belongs to root')
    expect(readOnlyReason({ Dir: '/srv/c', Writable: false, Missing: false, Owner: '', Remote: false })).toBe('Read-only: /srv/c isn’t writable for you')
    expect(readOnlyReason({ Dir: '/srv/c', Writable: false, Missing: true, Owner: '', Remote: false })).toBe('Read-only: /srv/c doesn’t exist yet')
  })
})
