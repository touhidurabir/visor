import { describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ ConfigService: { Folder: vi.fn() } }))

const { readOnlyReason } = await import('./folder')

describe('readOnlyReason', () => {
  it('is empty while configs can be written', () => {
    expect(readOnlyReason(null)).toBe('')
    expect(readOnlyReason({ Dir: '/home/ana/conf.d', Writable: true, Missing: false, Owner: '', Remote: false, User: '', ServerReadOnly: false })).toBe('')
    expect(readOnlyReason({ Dir: '', Writable: false, Missing: false, Owner: '', Remote: false, User: '', ServerReadOnly: false })).toBe('')
  })
  it('names the owner of a folder root keeps', () => {
    expect(readOnlyReason({ Dir: '/etc/supervisor/conf.d', Writable: false, Missing: false, Owner: 'root', Remote: false, User: '', ServerReadOnly: false })).toBe('Read-only: /etc/supervisor/conf.d belongs to root')
    expect(readOnlyReason({ Dir: '/srv/c', Writable: false, Missing: false, Owner: '', Remote: false, User: '', ServerReadOnly: false })).toBe('Read-only: /srv/c isn’t writable for you')
    expect(readOnlyReason({ Dir: '/srv/c', Writable: false, Missing: true, Owner: '', Remote: false, User: '', ServerReadOnly: false })).toBe('Read-only: /srv/c doesn’t exist yet')
  })
  it('on a server, names the SSH user, or says the server is set to Read only', () => {
    const srv = { Dir: '/etc/supervisor/conf.d', Writable: false, Missing: false, Owner: 'root', Remote: true, User: 'deploy', ServerReadOnly: false }
    expect(readOnlyReason(srv)).toBe('Read-only: deploy can’t write /etc/supervisor/conf.d on the server')
    expect(readOnlyReason({ ...srv, Missing: true })).toBe('Read-only: /etc/supervisor/conf.d doesn’t exist on the server yet')
    expect(readOnlyReason({ ...srv, ServerReadOnly: true })).toBe('Read only: this server is set to Read only')
    expect(readOnlyReason({ ...srv, Writable: true })).toBe('')
  })
})
