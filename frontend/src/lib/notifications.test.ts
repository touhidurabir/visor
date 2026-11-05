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

const { useNotifications, resetNotifications, noticeTime, noticeRoute, severityWord, severityLevel, noticeIcon, groupByDay } = await import('./notifications')
const { KIND_ICONS } = await import('./issue-status')

const n = (over: Partial<NotificationView> = {}): NotificationView => ({
  ID: 'n1', IssueID: 'i1', Kind: 'unexpected_exit', Process: 'flaky:flaky', Config: '/c/flaky.ini', ConfigName: 'flaky.ini',
  Title: 'flaky exited unexpectedly', Detail: 'Exited with status 2', AtMs: 0, Count: 1, Read: false, Shown: false, Severity: 'warning', ...over,
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

describe('severityWord', () => {
  it('names each level, and anything else as info', () => {
    expect([severityWord('error'), severityWord('warning'), severityWord('info'), severityWord('')]).toEqual(['Error', 'Warning', 'Info', 'Info'])
  })
})

describe('severityLevel', () => {
  it('keeps error and warning, and reads anything else as info', () => {
    expect(['error', 'warning', 'info', '', 'odd'].map(severityLevel)).toEqual(['error', 'warning', 'info', 'info', 'info'])
  })
})

describe('noticeIcon', () => {
  it('shows the kind: exit, can\'t start, crash loop, supervisord down', () => {
    expect(noticeIcon(n({ Kind: 'unexpected_exit' }))).toBe(KIND_ICONS.exit)
    expect(noticeIcon(n({ Kind: 'fatal', Severity: 'error' }))).toBe(KIND_ICONS.fatal)
    expect(noticeIcon(n({ Kind: 'crash_loop', Severity: 'error' }))).toBe(KIND_ICONS.loop)
    expect(noticeIcon(n({ Kind: 'unreachable', Severity: 'error' }))).toBe(KIND_ICONS.down)
  })
  it('shows good news as a check, not as the kind it ends', () => {
    expect(noticeIcon(n({ Kind: 'unreachable', Severity: 'info' }))).not.toBe(KIND_ICONS.down)
  })
})

describe('groupByDay', () => {
  const at = (d: number, h: number, m = 0, s = 0, ms = 0) => new Date(2026, 10, d, h, m, s, ms).getTime()
  const names = (now: number, ...times: number[]) =>
    groupByDay(times.map((AtMs, k) => n({ ID: `n${k}`, AtMs })), now).map((g) => [g.name, g.items.map((x) => x.ID)])

  it('splits at local midnight, keeping the order', () => {
    const now = at(4, 9, 30)
    expect(names(now, at(4, 9), at(4, 0, 0, 0, 0), at(3, 23, 59, 59, 999), at(1, 12))).toEqual([
      ['Today', ['n0', 'n1']],
      ['Earlier', ['n2', 'n3']],
    ])
  })
  it('puts everything from yesterday under Earlier just after midnight', () => {
    expect(names(at(4, 0, 0, 1), at(3, 23, 59), at(3, 8))).toEqual([['Earlier', ['n0', 'n1']]])
  })
  it('keeps late-evening items under Today until midnight', () => {
    expect(names(at(4, 23, 59, 59), at(4, 0, 1), at(4, 23, 50))).toEqual([['Today', ['n0', 'n1']]])
  })
  it('counts a time after now as today, and leaves out empty groups', () => {
    expect(names(at(4, 10), at(4, 11))).toEqual([['Today', ['n0']]])
    expect(names(at(4, 10))).toEqual([])
  })
})
