import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { NotificationView, NotificationsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

const handlers = new Map<string, (ev: { data: unknown }) => void>()
const list = vi.fn<() => Promise<NotificationsView>>()
const markRead = vi.fn<(id: string) => Promise<void>>()
const markAll = vi.fn<() => Promise<void>>()

vi.mock('@wailsio/runtime', () => ({
  Events: { On: (name: string, cb: (ev: { data: unknown }) => void) => (handlers.set(name, cb), () => handlers.delete(name)) },
}))
vi.mock('../../bindings/events', () => ({ VisorNotifications: 'visor:notifications' }))
vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({
  InboxService: { Notifications: () => list(), MarkRead: (id: string) => markRead(id), MarkAllRead: () => markAll() },
}))

const { useNotifications, resetNotifications, noticeTime, noticeRoute } = await import('./notifications')

const n = (over: Partial<NotificationView> = {}): NotificationView => ({
  ID: 'n1', IssueID: 'i1', Kind: 'unexpected_exit', Process: 'flaky:flaky', Config: '/c/flaky.ini', ConfigName: 'flaky.ini',
  Title: 'flaky exited unexpectedly', Detail: 'Exited with status 2', AtMs: 0, Count: 1, Read: false, Shown: false, ...over,
})
const flush = () => new Promise((r) => setTimeout(r, 0))

describe('useNotifications', () => {
  beforeEach(() => {
    resetNotifications()
    handlers.clear()
    list.mockReset()
    markRead.mockReset().mockResolvedValue()
    markAll.mockReset().mockResolvedValue()
  })

  it('loads once, counts unread and follows events', async () => {
    list.mockResolvedValue({ Unread: 1, Items: [n(), n({ ID: 'n2', Read: true })] })
    const a = useNotifications()
    const b = useNotifications()
    await flush()
    expect(list).toHaveBeenCalledTimes(1)
    expect(a.unread.value).toBe(1)
    handlers.get('visor:notifications')!({ data: { Unread: 2, Items: [n({ ID: 'n3' }), n()] } })
    expect(b.items.value.map((x) => x.ID)).toEqual(['n3', 'n1'])
    expect(b.unread.value).toBe(2)
  })

  it('marks read at once and tells Go', async () => {
    list.mockResolvedValue({ Unread: 2, Items: [n(), n({ ID: 'n2' })] })
    const s = useNotifications()
    await flush()
    await s.markRead('n1')
    expect(markRead).toHaveBeenCalledWith('n1')
    expect(s.unread.value).toBe(1)
    await s.markRead('n1') // already read: no second call
    expect(markRead).toHaveBeenCalledTimes(1)
    await s.markAllRead()
    expect(markAll).toHaveBeenCalled()
    expect(s.unread.value).toBe(0)
  })
})

describe('noticeTime', () => {
  const now = new Date(2026, 10, 1, 21, 0).getTime()
  it('is relative while recent', () => {
    expect(noticeTime(now - 20_000, now)).toBe('just now')
    expect(noticeTime(now - 2 * 60_000, now)).toBe('2 min ago')
    expect(noticeTime(now - 3 * 3_600_000, now)).toBe('3 h ago')
  })
  it('gives the day and time when older', () => {
    expect(noticeTime(new Date(2026, 9, 31, 21, 40).getTime(), now)).toBe('Yesterday 21:40')
  })
})

describe('noticeRoute', () => {
  it('opens the config on that process', () => {
    expect(noticeRoute(n())).toEqual({ name: 'config', params: { path: '/c/flaky.ini' }, query: { process: 'flaky:flaky' } })
  })
  it('opens Issues for supervisord itself', () => {
    expect(noticeRoute(n({ Config: '', Process: '' }))).toBe('/issues')
  })
})
