// Labels for the Issues page and dashboard list, as pure functions.
import type { IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { formatDuration } from './format'

/** Stroke icons (24-unit viewBox) for each kind of issue, as the canvas draws them. */
export const KIND_ICONS = {
  exit: 'M3 12h12M11 6l6 6-6 6M21 4v16',
  fatal: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 8v5M12 16v.01',
  loop: 'M20 12a8 8 0 1 1-2.3-5.7M20 4v5h-5',
  down: 'M12 3v8M6.3 6.3a8 8 0 1 0 11.4 0',
  other: 'M12 8v5M12 16v.01',
} as const

const KINDS: Record<string, { label: string; cls: string; icon: string }> = {
  fatal: { label: "Can't start", cls: 't-bad', icon: KIND_ICONS.fatal },
  unexpected_exit: { label: 'Exited', cls: 't-warn', icon: KIND_ICONS.exit },
  crash_loop: { label: 'Crash loop', cls: 't-warn', icon: KIND_ICONS.loop },
  unreachable: { label: 'Unreachable', cls: 't-bad', icon: KIND_ICONS.down },
}

/**
 * issueKind is how a kind shows: an icon (an SVG path) plus a sentence-case
 * word, and the t-* colour class for both.
 */
export function issueKind(kind: string): { label: string; cls: string; icon: string } {
  if (KINDS[kind]) return KINDS[kind]
  const word = kind.replace(/_/g, ' ')
  return { label: word.charAt(0).toUpperCase() + word.slice(1), cls: 't-idle', icon: KIND_ICONS.other }
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
