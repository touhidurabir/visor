import { describe, expect, it } from 'vitest'
import type { IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { KIND_ICONS, issueKind, lasted, statusLabel } from './issue-status'

const issue = (over: Partial<IssueView> = {}): IssueView => ({
  ID: 'i1', Kind: 'fatal', Process: 'web:web', Config: '/etc/web.conf', Message: 'm',
  OpenedMs: 1_000_000, ClosedMs: 0, Status: 'open', Notified: false, Note: '', ...over,
})

describe('statusLabel', () => {
  it('says whether an open issue notified you', () => {
    expect(statusLabel(issue({ Notified: true }))).toBe('Open · notified')
  })
  it('says when the config is not watched', () => {
    expect(statusLabel(issue({ Notified: true }), false)).toBe('Open · not watched')
    expect(statusLabel(issue(), false)).toBe('Open · not watched')
  })
  it('is plain Open when watched and not (yet) notified', () => {
    expect(statusLabel(issue())).toBe('Open')
  })
  it('labels closed issues by who closed them', () => {
    expect(statusLabel(issue({ Status: 'resolved_auto' }))).toBe('Resolved by itself')
    expect(statusLabel(issue({ Status: 'resolved_user' }))).toBe('Marked resolved by you')
    expect(statusLabel(issue({ Status: 'dismissed' }))).toBe('Dismissed')
  })
  it('ignores watch state once closed', () => {
    expect(statusLabel(issue({ Status: 'resolved_auto' }), false)).toBe('Resolved by itself')
  })
})

describe('lasted', () => {
  it('is closed minus opened for closed issues', () => {
    expect(lasted(issue({ Status: 'resolved_auto', ClosedMs: 1_003_000 }), 9_999_999)).toBe('3s')
    expect(lasted(issue({ Status: 'dismissed', ClosedMs: 1_000_000 + 7_920_000 }), 0)).toBe('2h 12m')
  })
  it('counts up to now for open issues', () => {
    expect(lasted(issue(), 1_000_000 + 600_000)).toBe('10m so far')
  })
})

describe('issueKind', () => {
  it('maps kinds to an icon, a sentence-case word and a colour class', () => {
    expect(issueKind('fatal')).toEqual({ label: "Can't start", cls: 't-bad', icon: KIND_ICONS.fatal })
    expect(issueKind('unexpected_exit')).toEqual({ label: 'Exited', cls: 't-warn', icon: KIND_ICONS.exit })
    expect(issueKind('crash_loop')).toEqual({ label: 'Crash loop', cls: 't-warn', icon: KIND_ICONS.loop })
    expect(issueKind('unreachable')).toEqual({ label: 'Unreachable', cls: 't-bad', icon: KIND_ICONS.down })
  })
  it('shows an unknown kind as a plain word', () => {
    expect(issueKind('weird_thing')).toEqual({ label: 'Weird thing', cls: 't-idle', icon: KIND_ICONS.other })
  })
})
