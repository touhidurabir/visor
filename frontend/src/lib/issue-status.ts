// Labels for the Issues page and dashboard list, as pure functions.
import type { IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { formatDuration } from './format'

const KINDS: Record<string, { label: string; cls: string }> = {
  fatal: { label: 'FATAL', cls: 'failing' },
  unexpected_exit: { label: 'EXITED', cls: 'failing' },
  crash_loop: { label: 'CRASH LOOP', cls: 'warning' },
  unreachable: { label: 'UNREACHABLE', cls: 'failing' },
}

/** issueKind is the badge text and colour class (failing, warning, stopped). */
export function issueKind(kind: string): { label: string; cls: string } {
  return KINDS[kind] ?? { label: kind.toUpperCase(), cls: 'stopped' }
}

/**
 * statusLabel says where an issue stands. `watched` is the Watch flag of the
 * issue's config (true when unknown); it only matters while the issue is open.
 */
export function statusLabel(i: IssueView, watched = true): string {
  switch (i.Status) {
    case 'open':
      if (!watched) return 'Open · not watched'
      return i.Notified ? 'Open · notified' : 'Open'
    case 'resolved_auto':
      return 'Resolved by itself'
    case 'resolved_user':
      return 'Marked resolved by you'
    case 'dismissed':
      return 'Dismissed'
    default:
      return i.Status
  }
}

/** lasted is how long the issue ran: closed - opened, or "… so far" while open. */
export function lasted(i: IssueView, nowMs: number): string {
  if (i.Status === 'open' || i.ClosedMs <= 0) return `${formatDuration(nowMs - i.OpenedMs)} so far`
  return formatDuration(i.ClosedMs - i.OpenedMs)
}

/** Days finished issues are kept; Task 19 replaces this with the setting. */
export const ISSUE_RETENTION_DAYS = 90
