import { describe, expect, it } from 'vitest'
import type { IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { allSelected, dateRange, deleteAsk, issueFiltersQuery, keepLoaded, parseIssueFilters, resolveAsk, rowNotes, rowWhere, selectionActions, showsStatus, summaryLine, summaryParts, toggleAll, type IssueFilters } from './issues-page'

const row = (ID: string, Status = 'open') =>
  ({ ID, Kind: 'fatal', Process: 'g:w', Config: '', Message: '', OpenedMs: 1, ClosedMs: 0, Status, Notified: false, Note: '' }) as IssueView

describe('dateRange', () => {
  const now = Date.UTC(2026, 10, 1, 12)
  it('is unbounded for any time', () => {
    expect(dateRange('any', now)).toEqual({ fromMs: 0, toMs: 0 })
  })
  it('counts presets back from now', () => {
    expect(dateRange('24h', now)).toEqual({ fromMs: now - 24 * 3_600_000, toMs: 0 })
    expect(dateRange('7d', now).fromMs).toBe(now - 7 * 86_400_000)
    expect(dateRange('30d', now).fromMs).toBe(now - 30 * 86_400_000)
  })
  it('includes the whole end day for a custom range', () => {
    const r = dateRange('custom', now, '2026-10-01', '2026-10-03')
    expect(r.fromMs).toBe(new Date(2026, 9, 1).getTime())
    expect(r.toMs).toBe(new Date(2026, 9, 4).getTime())
  })
  it('rolls the end day over a month', () => {
    expect(dateRange('custom', now, '', '2026-10-31').toMs).toBe(new Date(2026, 10, 1).getTime())
  })
  it('leaves a missing side open and ignores junk', () => {
    expect(dateRange('custom', now, '2026-10-01', '')).toEqual({ fromMs: new Date(2026, 9, 1).getTime(), toMs: 0 })
    expect(dateRange('custom', now, 'nope', '')).toEqual({ fromMs: 0, toMs: 0 })
  })
  it('puts swapped days in order', () => {
    expect(dateRange('custom', now, '2026-10-03', '2026-10-01')).toEqual(dateRange('custom', now, '2026-10-01', '2026-10-03'))
  })
})

describe('selection', () => {
  const rows = [row('a'), row('b', 'resolved_user'), row('c', 'dismissed'), row('d')]
  it('splits the selection into resolvable and deletable ids', () => {
    expect(selectionActions(rows, new Set(['a', 'b', 'c']))).toEqual({ resolve: ['a'], remove: ['b', 'c'] })
    expect(selectionActions(rows, new Set())).toEqual({ resolve: [], remove: [] })
    expect(selectionActions(rows, new Set(['gone']))).toEqual({ resolve: [], remove: [] })
  })
  it('selects all loaded rows, then clears', () => {
    const all = toggleAll(rows, new Set(['a']))
    expect([...all]).toEqual(['a', 'b', 'c', 'd'])
    expect(allSelected(rows, all)).toBe(true)
    expect(toggleAll(rows, all).size).toBe(0)
    expect(allSelected([], new Set())).toBe(false)
  })
  it('drops ids that are no longer shown', () => {
    expect([...keepLoaded([row('a')], new Set(['a', 'z']))]).toEqual(['a'])
  })
})

describe('confirm wording', () => {
  it('resolve', () => {
    expect(resolveAsk(3)).toEqual({
      title: 'Resolve 3 issues?',
      message: "They move to Resolved. Visor won't reopen them for the same failure.",
      confirmLabel: 'Resolve 3 issues',
    })
    expect(resolveAsk(1).confirmLabel).toBe('Resolve 1 issue')
  })
  it('delete', () => {
    expect(deleteAsk(2).title).toBe('Delete 2 resolved issues?')
    expect(deleteAsk(2).message).toBe("This can't be undone.")
    expect(deleteAsk(2).confirmLabel).toBe('Delete 2 issues')
    expect(deleteAsk(1, true).title).toBe('Delete this issue?')
  })
})

describe('issue filters in the URL', () => {
  const defaults: IssueFilters = { status: 'open', kind: '', date: 'any', from: '', to: '', q: '', notified: false }
  it('reads nothing as the defaults and writes the defaults as nothing', () => {
    expect(parseIssueFilters({})).toEqual(defaults)
    expect(issueFiltersQuery(parseIssueFilters({}))).toEqual({})
  })
  it('round-trips every filter', () => {
    const f = { status: 'resolved', kind: 'fatal', date: 'custom', from: '2026-10-01', to: '2026-10-31', q: 'mailer', notified: true } as const
    const q = issueFiltersQuery(f)
    expect(q).toEqual({ s: 'resolved', k: 'fatal', d: 'custom', from: '2026-10-01', to: '2026-10-31', q: 'mailer', n: '1' })
    expect(parseIssueFilters(q)).toEqual(f)
  })
  it('drops junk and days outside a custom range', () => {
    expect(parseIssueFilters({ s: 'nope', k: 'boom', d: 'week', n: 'yes', q: ['a'] })).toEqual(defaults)
    expect(issueFiltersQuery({ ...defaults, date: '7d', from: '2026-10-01' })).toEqual({ d: '7d' })
    expect(parseIssueFilters({ d: 'custom', from: 'soon' })).toEqual({ ...defaults, date: 'custom' })
  })
})

describe('summary line', () => {
  const stats = { Total: 3, Open: 3, Resolved: 0, Last24h: 3, Last7d: 4, Last30d: 9 }
  it('reads open, resolved, then the last 24 hours, 7 and 30 days', () => {
    expect(summaryLine(stats)).toBe('3 open · 0 resolved · 3 in the last 24 hours · 4 in 7 days · 9 in 30 days')
  })
  it('keeps each number apart from its words', () => {
    expect(summaryParts(stats).slice(0, 3)).toEqual([
      { n: 3, label: 'open' },
      { n: 0, label: 'resolved' },
      { n: 3, label: 'in the last 24 hours' },
    ])
  })
})

describe('issue rows', () => {
  const issue = (over: Partial<IssueView> = {}) => ({ ...row('i1'), Config: '/etc/conf.d/flaky.ini', Process: 'flaky', ...over }) as IssueView
  it('shows the Status column only on All', () => {
    expect([showsStatus('open'), showsStatus('resolved'), showsStatus('all')]).toEqual([false, false, true])
  })
  it('names the config, or says why there is none', () => {
    expect(rowWhere(issue())).toBe('flaky.ini')
    expect(rowWhere(issue({ Config: '' }))).toBe('config removed')
    expect(rowWhere(issue({ Config: '', Process: '' }))).toBe('socket')
  })
  it('adds nothing when the Status column shows', () => {
    expect(rowNotes(issue({ Notified: true }), false, true)).toEqual([])
  })
  it('keeps what the hidden Status column would add', () => {
    expect(rowNotes(issue(), true, false)).toEqual([])
    expect(rowNotes(issue(), false, false)).toEqual(['not watched'])
    expect(rowNotes(issue({ Notified: true }), true, false)).toEqual(['notified'])
    expect(rowNotes(issue({ Status: 'resolved_auto' }), false, false)).toEqual(['Resolved by itself'])
    expect(rowNotes(issue({ Status: 'resolved_user', Notified: true }), true, false)).toEqual(['Marked resolved by you', 'notified'])
  })
})
