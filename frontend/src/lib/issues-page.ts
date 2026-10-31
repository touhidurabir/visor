// Pure helpers for the Issues page: filter options, date ranges, selection
// math and the wording of its confirmations.
import type { IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

export const KIND_OPTIONS = [
  { value: '', label: 'All kinds' },
  { value: 'fatal', label: "Can't start" },
  { value: 'unexpected_exit', label: 'Exited unexpectedly' },
  { value: 'crash_loop', label: 'Crash loop' },
  { value: 'unreachable', label: 'supervisord unreachable' },
]

export type DatePreset = 'any' | '24h' | '7d' | '30d' | 'custom'

export const DATE_OPTIONS: { value: DatePreset; label: string }[] = [
  { value: 'any', label: 'Any time' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: 'custom', label: 'Custom range…' },
]

/** A range of OpenedMs, [fromMs, toMs); 0 means unbounded. */
export interface MsRange {
  fromMs: number
  toMs: number
}

const HOUR = 3_600_000
const WINDOWS: Record<string, number> = { '24h': 24 * HOUR, '7d': 7 * 24 * HOUR, '30d': 30 * 24 * HOUR }

/** localDayStart is midnight at the start of a "YYYY-MM-DD" day (plus days), in local time; 0 when empty or invalid. */
function localDayStart(day: string, plusDays = 0): number {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day)
  if (!m) return 0
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]) + plusDays).getTime()
}

/**
 * dateRange turns the Date select into a range. A preset counts back from
 * nowMs. A custom range runs from the start of the first day to the end of
 * the last day (inclusive); a missing day leaves that side open, and
 * swapped days are put in order.
 */
export function dateRange(preset: DatePreset, nowMs: number, fromDay = '', toDay = ''): MsRange {
  if (preset === 'any') return { fromMs: 0, toMs: 0 }
  if (preset !== 'custom') return { fromMs: nowMs - WINDOWS[preset], toMs: 0 }
  let a = fromDay
  let b = toDay
  if (a && b && a > b) [a, b] = [b, a]
  return { fromMs: localDayStart(a), toMs: localDayStart(b, 1) }
}

/** The ids a bulk action would touch: open ones to resolve, closed ones to delete. */
export interface Selection {
  resolve: string[]
  remove: string[]
}

export function selectionActions(rows: IssueView[], selected: ReadonlySet<string>): Selection {
  const out: Selection = { resolve: [], remove: [] }
  for (const r of rows) {
    if (!selected.has(r.ID)) continue
    ;(r.Status === 'open' ? out.resolve : out.remove).push(r.ID)
  }
  return out
}

/** allSelected: every loaded row is selected (false for no rows). */
export function allSelected(rows: IssueView[], selected: ReadonlySet<string>): boolean {
  return rows.length > 0 && rows.every((r) => selected.has(r.ID))
}

/** toggleAll selects every loaded row, or clears the selection when all are selected. */
export function toggleAll(rows: IssueView[], selected: ReadonlySet<string>): Set<string> {
  return allSelected(rows, selected) ? new Set() : new Set(rows.map((r) => r.ID))
}

/** keepLoaded drops selected ids whose rows are no longer shown. */
export function keepLoaded(rows: IssueView[], selected: ReadonlySet<string>): Set<string> {
  const have = new Set(rows.map((r) => r.ID))
  return new Set([...selected].filter((id) => have.has(id)))
}

export const issues = (n: number) => `${n} ${n === 1 ? 'issue' : 'issues'}`

export interface Ask {
  title: string
  message: string
  confirmLabel: string
}

/** The dialog shows title, then message: together they read as the planned copy. */
export function resolveAsk(n: number): Ask {
  return {
    title: `Resolve ${issues(n)}?`,
    message: `${n === 1 ? 'It moves' : 'They move'} to Resolved. Visor won't reopen ${n === 1 ? 'it' : 'them'} for the same failure.`,
    confirmLabel: `Resolve ${issues(n)}`,
  }
}

export function deleteAsk(n: number, single = false): Ask {
  if (single) return { title: 'Delete this issue?', message: "This can't be undone.", confirmLabel: 'Delete issue' }
  return {
    title: `Delete ${n} resolved ${n === 1 ? 'issue' : 'issues'}?`,
    message: "This can't be undone.",
    confirmLabel: `Delete ${issues(n)}`,
  }
}
