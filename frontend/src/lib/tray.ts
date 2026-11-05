// The menu-bar popover's words and layout. Flow 1 of the polish batch: the
// failing processes come first, in a "Needs attention" group, and the rest
// follow under their config file. "Failing" is the Go side's classification
// (lib/health.ts isFailing), the same one the dashboard sorts by. Each row
// offers only the actions that apply to its state. Several copies of one
// program (numprocs > 1) are one row in their config, "mailer-worker ×2",
// that unfolds into the copies; its buttons act on every copy.
import type { ConfigView, ProcessView, ServerView, StatsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { connectionBadge, type ConnectionProblem } from './connection'
import { copiesLine, rollUp } from './explain-cards'
import { formatDuration } from './format'
import { isFailing, processLabel } from './health'
import { stateLabel, stateLook, type StateTone } from './state'

export const NEEDS_ATTENTION = 'Needs attention'

/**
 * One row of the popover: a process, or every copy of a program that runs
 * several (numprocs > 1), folded into one row.
 */
export interface TrayItem {
  /** Stable while the process (or program) stays: its full name, or "copies:<config>:<program>". */
  key: string
  /** What the row shows and its buttons say: the process name, or the program's for copies. */
  name: string
  /** The process, or every copy in config order. */
  procs: ProcessView[]
  /** Copies folded into one row ("name ×N"). */
  multi: boolean
}

export interface TrayGroup {
  key: string
  /** "Needs attention", or the config's file name. */
  name: string
  /** The config file; null for Needs attention. */
  config: ConfigView | null
  items: TrayItem[]
  /** How many of this config's rows are listed under Needs attention instead. */
  moved: number
}

/**
 * The rows of one config: each process on its own, except a program with
 * several copies, which is one row where its first copy is. A failing
 * process on its own row moves to Needs attention (so is left out here);
 * copies stay together, failing ones too, and a failing copy is also
 * listed under Needs attention.
 */
function configItems(c: ConfigView): { items: TrayItem[]; moved: number } {
  const procs = c.Processes ?? []
  const copies = new Map<string, ProcessView[]>()
  for (const p of procs) if (p.Program) copies.set(p.Program, [...(copies.get(p.Program) ?? []), p])
  const items: TrayItem[] = []
  const done = new Set<string>()
  let moved = 0
  for (const p of procs) {
    const many = p.Program ? copies.get(p.Program)! : [p]
    if (many.length > 1) {
      if (done.has(p.Program)) continue
      done.add(p.Program)
      items.push({ key: `copies:${c.Path || c.FileName}:${p.Program}`, name: p.Program, procs: many, multi: true })
    } else if (isFailing(p)) {
      moved++
    } else {
      items.push({ key: p.FullName, name: p.Name, procs: [p], multi: false })
    }
  }
  return { items, moved }
}

/**
 * Needs attention (only when something fails) with every failing process
 * in config order, then each config that has processes, without its
 * failing ones (copies stay folded in their row). A config whose rows all
 * moved keeps its header (and so its Watch switch) with no rows.
 */
export function trayGroups(configs: ConfigView[]): TrayGroup[] {
  const listed = configs.filter((c) => (c.Processes ?? []).length > 0)
  const failing = listed.flatMap((c) => (c.Processes ?? []).filter(isFailing))
  const groups = listed.map((c): TrayGroup => ({ key: `config:${c.Path || c.FileName}`, name: c.FileName, config: c, ...configItems(c) }))
  if (!failing.length) return groups
  const attention: TrayGroup = { key: 'attention', name: NEEDS_ATTENTION, config: null, items: [], moved: 0 }
  attention.items = failing.map((p) => ({ key: p.FullName, name: rowName(attention, p), procs: [p], multi: false }))
  return [attention, ...groups]
}

/**
 * The name a row shows (and its buttons say). Under its config, the process
 * name; in Needs attention, where several configs meet, "group:name" when
 * they differ (processLabel), so two failing workers can be told apart.
 */
export function rowName(group: Pick<TrayGroup, 'config'>, p: ProcessView): string {
  return group.config ? p.Name : processLabel(p)
}

export type TrayVerb = 'start' | 'stop' | 'restart'

export const VERB_LABEL: Record<TrayVerb, string> = { start: 'Start', stop: 'Stop', restart: 'Restart' }

// supervisord's RUNNING_STATES: Start would only say "already started".
const UP = new Set(['RUNNING', 'STARTING', 'BACKOFF'])
const DOWN = new Set(['STOPPED', 'EXITED', 'FATAL'])

/**
 * The buttons a row shows: running (or starting, or retrying) gets Stop and
 * Restart; stopped or failed gets Start; stopping (or unknown) gets none.
 */
export function trayActions(state: string): TrayVerb[] {
  if (UP.has(state)) return ['stop', 'restart']
  if (DOWN.has(state)) return ['start']
  return []
}

/**
 * The buttons of a row of copies, as for one process: Stop and Restart
 * while any copy runs (or starts, or retries), else Start when any is
 * stopped or failed, else none.
 */
export function itemActions(item: Pick<TrayItem, 'procs'>): TrayVerb[] {
  if (item.procs.some((p) => UP.has(p.State))) return ['stop', 'restart']
  if (item.procs.some((p) => DOWN.has(p.State))) return ['start']
  return []
}

/**
 * The processes a row's button acts on. One process: itself. Copies: every
 * copy the verb applies to, so Stop leaves stopped copies alone, Start
 * running ones, and Restart (stop if running, then start) brings every
 * copy up that isn't on its way down already.
 */
export function actionTargets(verb: TrayVerb, item: Pick<TrayItem, 'procs' | 'multi'>): ProcessView[] {
  if (!item.multi) return item.procs
  if (verb === 'restart') return item.procs.filter((p) => UP.has(p.State) || DOWN.has(p.State))
  return item.procs.filter((p) => trayActions(p.State).includes(verb))
}

/** The fold button's name: "Show the 2 copies of mailer-worker", or Hide when open. */
export function foldLabel(item: Pick<TrayItem, 'name' | 'procs'>, open: boolean): string {
  return `${open ? 'Hide' : 'Show'} the ${item.procs.length} copies of ${item.name}`
}

export interface RowLook {
  /** The dot's tone; a failing process is always red. */
  tone: StateTone
  /** The second line: "Running · up 4s", "Fatal · failing 3m", "2 of 2 running". */
  line: string
  /** The class of the second line (t-bad is semibold too). */
  lineClass: string
  /** A failing process: its row is tinted. A row of copies never is; its line turns red. */
  failing: boolean
}

const lineClass = (tone: StateTone) => (tone === 'bad' ? 't-bad' : tone === 'warn' ? 't-warn' : 'meta')

/**
 * A process's state and time: "Running · up 4s", "Fatal · failing 3m",
 * "Stopped · 4m" (Go's "stopped 4m" without saying stopped twice), or the
 * state alone when there is no time.
 */
export function stateLine(p: Pick<ProcessView, 'State' | 'Since'>): string {
  const label = stateLabel(p.State)
  if (!p.Since) return label
  return `${label} · ${p.Since.replace(/^stopped /, '')}`
}

/** How a process row reads: its dot, and its state and time. */
export function rowLook(p: ProcessView): RowLook {
  const failing = isFailing(p)
  const tone = failing ? 'bad' : stateLook(p.State, p.Status).tone
  return { tone, line: stateLine(p), lineClass: lineClass(tone), failing }
}

/**
 * How a row reads: one process as rowLook; copies with rollUp's state for
 * the dot (as the program's card on the config page) and copiesLine's
 * counts, red when any copy fails.
 */
export function itemLook(item: Pick<TrayItem, 'procs' | 'multi'>): RowLook {
  if (!item.multi) return rowLook(item.procs[0])
  const r = rollUp(item.procs)
  const tone = r.status === 'failing' ? 'bad' : stateLook(r.state, r.status).tone
  return { tone, line: copiesLine(item.procs), lineClass: lineClass(tone), failing: false }
}

export interface TrayHeader {
  /** The dot beside the server's name. */
  tone: StateTone
  /** "2 failing", "Not running"; null when nothing needs saying. */
  badge: string | null
  /** "8 running · 2 stopped · up 1h 12m", or what's wrong with the connection. */
  meta: string
}

const HEALTH_TONE: Record<string, StateTone> = { ok: 'ok', warning: 'warn', error: 'bad' }

/** The popover header's dot, badge and line. */
export function trayHeader(
  server: Pick<ServerView, 'Health' | 'UptimeMs'> | null | undefined,
  stats: Pick<StatsView, 'Running' | 'Stopped' | 'Failing'> | null | undefined,
  problem: ConnectionProblem | null,
): TrayHeader {
  if (problem) {
    const meta = problem.kind === 'down' ? 'Can’t reach supervisord' : problem.reason
    return { tone: 'bad', badge: connectionBadge(problem), meta }
  }
  const tone = HEALTH_TONE[server?.Health ?? ''] ?? 'idle'
  if (!stats) return { tone, badge: null, meta: '' }
  const parts = [`${stats.Running} running`, `${stats.Stopped} stopped`]
  if (server?.UptimeMs) parts.push(`up ${formatDuration(server.UptimeMs)}`)
  return { tone, badge: stats.Failing > 0 ? `${stats.Failing} failing` : null, meta: parts.join(' · ') }
}

/**
 * The down card's line under connectionProblem's title. For supervisord
 * not running, the short version (the socket is under Show details); any
 * other problem keeps the dashboard's line, which says what went wrong.
 */
export function downLine(problem: ConnectionProblem): string {
  if (problem.kind === 'down') return 'Visor can’t show or control processes until it’s back. Doctor walks you through starting it.'
  return problem.line.map((s) => s.text).join('')
}

/** Why the footer's buttons are off, under them. */
export function footerReason(problem: ConnectionProblem): string {
  return problem.kind === 'down' ? 'These need supervisord running.' : 'These need a connection to supervisord.'
}
