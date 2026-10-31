import { describe, expect, it } from 'vitest'
import type { IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { allSelected, dateRange, deleteAsk, issueFiltersQuery, keepLoaded, parseIssueFilters, resolveAsk, selectionActions, toggleAll, type IssueFilters } from './issues-page'

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
