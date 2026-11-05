// The dashboard's health strip: one sentence about the server's processes
// ("2 processes are failing", "All 12 processes are running"), a line of
// detail, and small counts. "Failing" is the Go side's classification
// (ProcessView.Status "failing": FATAL, BACKOFF, or EXITED with a status the
// config doesn't expect), the same one StatsView.Failing counts.
import type { ConfigView, ProcessView, StatsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

export type HealthTone = 'ok' | 'bad' | 'warn' | 'idle'

/** A piece of the detail line; `mono` pieces are process names. */
export interface Segment {
  text: string
  mono?: boolean
}

export interface Count {
  label: string
  /** A number, or NO_COUNT while supervisord isn't connected. */
  value: number | string
}

/** What a count shows while Visor has no connection: not a zero, which would read as fact. */
export const NO_COUNT = '—'

export interface Health {
  tone: HealthTone
  sentence: string
  detail: Segment[]
  counts: Count[]
  failing: number
}

/** How many failing processes the detail line names before "and N more". */
export const NAMED = 3

export const isFailing = (p: ProcessView) => p.Status === 'failing'

/**
 * The colour of a process's reason line (ProcessView.Detail) on the
 * dashboard: red when it is failing, and red on a running process too,
 * whose only reason line is an open crash loop (Go's sinceAndDetail), an
 * error like everywhere else Visor shows one; grey otherwise.
 */
export function reasonClass(p: Pick<ProcessView, 'Status'>): string {
  return p.Status === 'failing' || p.Status === 'running' ? 't-bad' : 'meta'
}

/** A process's name as the dashboard shows it: "flaky", or "group:name" in a group. */
export function processLabel(p: ProcessView): string {
  return p.Group === p.Name ? p.Name : p.FullName
}

const plural = (n: number, one: string, many: string) => (n === 1 ? one : many)

type Totals = Pick<StatsView, 'Configs' | 'Processes' | 'Running' | 'Failing' | 'Stopped'>

export function healthSentence(s: Totals): string {
  const { Processes: n, Running: running, Failing: failing, Stopped: stopped } = s
  if (failing > 0) return failing === 1 ? '1 process is failing' : `${failing} processes are failing`
  if (n === 0) return 'No processes are loaded'
  if (running === n) return n === 1 ? '1 process is running' : `All ${n} processes are running`
  if (stopped === n) return n === 1 ? '1 process is stopped' : `All ${n} processes are stopped`
  return `${running} of ${n} processes are running`
}

function tone(s: Totals): HealthTone {
  if (s.Failing > 0) return 'bad'
  if (s.Processes === 0 || s.Stopped === s.Processes) return 'idle'
  // Starting or stopping right now.
  if (s.Running + s.Stopped < s.Processes) return 'warn'
  return 'ok'
}

/** "a, b and c need attention.", naming at most NAMED of them. */
function failingDetail(names: string[]): Segment[] {
  if (!names.length) return []
  const shown = names.slice(0, NAMED)
  const more = names.length - shown.length
  const out: Segment[] = []
  shown.forEach((name, i) => {
    if (i > 0) out.push({ text: i === shown.length - 1 && !more ? ' and ' : ', ' })
    out.push({ text: name, mono: true })
  })
  if (more) out.push({ text: ` and ${more} more` })
  out.push({ text: names.length === 1 ? ' needs attention.' : ' need attention.' })
  return out
}

function quietDetail(s: Totals): Segment[] {
  const { Processes: n, Running: running, Stopped: stopped } = s
  if (n === 0) return [{ text: s.Configs ? 'supervisord runs none of your config files yet.' : 'Add a program to get started.' }]
  if (running === n) return [{ text: 'Nothing needs your attention.' }]
  if (stopped === n) return [{ text: 'None of them are failing.' }]
  const parts: string[] = []
  if (stopped) parts.push(`${stopped} stopped`)
  const moving = n - running - stopped
  if (moving) parts.push(`${moving} starting or stopping`)
  return [{ text: `${parts.join(' and ')}; none are failing.` }]
}

export function health(stats: Totals, configs: ConfigView[]): Health {
  const names = configs.flatMap((c) => (c.Processes ?? []).filter(isFailing).map(processLabel))
  return {
    tone: tone(stats),
    sentence: healthSentence(stats),
    detail: stats.Failing > 0 ? failingDetail(names) : quietDetail(stats),
    counts: [
      { label: 'running', value: stats.Running },
      { label: 'stopped', value: stats.Stopped },
      { label: plural(stats.Processes, 'process', 'processes'), value: stats.Processes },
      { label: plural(stats.Configs, 'config', 'configs'), value: stats.Configs },
    ],
    failing: stats.Failing,
  }
}

/**
 * The strip while supervisord isn't connected: no verdict and no process
 * counts (Visor doesn't know them), only the configs it read from disk.
 */
export function disconnectedHealth(stats: Pick<StatsView, 'Configs'>): Health {
  return {
    tone: 'idle',
    sentence: 'Not connected',
    detail: [{ text: 'Counts come back with supervisord.' }],
    counts: [
      { label: 'running', value: NO_COUNT },
      { label: 'stopped', value: NO_COUNT },
      { label: 'processes', value: NO_COUNT },
      { label: plural(stats.Configs, 'config', 'configs'), value: stats.Configs },
    ],
    failing: 0,
  }
}

/** The strip's link to the Issues page: "3 open issues", "No open issues". */
export function openIssuesLabel(n: number): string {
  if (!n) return 'No open issues'
  return n === 1 ? '1 open issue' : `${n} open issues`
}

/** The first failing process in the order the table lists them, or undefined. */
export function firstFailing(configs: ConfigView[]): ProcessView | undefined {
  for (const c of configs) {
    const p = (c.Processes ?? []).find(isFailing)
    if (p) return p
  }
  return undefined
}

/**
 * A config row's summary ("2 programs · 2 running · 1 failing", from Go)
 * in pieces, with the failing count marked so only it is coloured.
 */
export function summaryParts(summary: string): Array<{ text: string; failing: boolean }> {
  return summary.split(' · ').map((text, i) => ({ text: (i ? ' · ' : '') + text, failing: /^\d+ failing$/.test(text) }))
}
