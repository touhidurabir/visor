// The menu-bar popover's words and layout. Flow 1 of the polish batch: the
// failing processes come first, in a "Needs attention" group, and the rest
// follow under their config file. "Failing" is the Go side's classification
// (lib/health.ts isFailing), the same one the dashboard sorts by. Each row
// offers only the actions that apply to its state.
import type { ConfigView, ProcessView, ServerView, StatsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { connectionBadge, type ConnectionProblem } from './connection'
import { formatDuration } from './format'
import { isFailing, processLabel } from './health'
import { stateLabel, stateLook, type StateTone } from './state'

export const NEEDS_ATTENTION = 'Needs attention'

export interface TrayGroup {
  key: string
  /** "Needs attention", or the config's file name. */
  name: string
  /** The config file; null for Needs attention. */
  config: ConfigView | null
  processes: ProcessView[]
  /** How many of this config's processes are listed under Needs attention instead. */
  moved: number
}

/**
 * Needs attention (only when something fails) with every failing process
 * in config order, then each config that has processes, without its
 * failing ones. A config whose processes all fail keeps its header (and
 * so its Watch switch) with no rows.
 */
export function trayGroups(configs: ConfigView[]): TrayGroup[] {
  const listed = configs.filter((c) => (c.Processes ?? []).length > 0)
  const failing = listed.flatMap((c) => (c.Processes ?? []).filter(isFailing))
  const groups = listed.map((c): TrayGroup => {
    const procs = c.Processes ?? []
    const rest = procs.filter((p) => !isFailing(p))
    return { key: `config:${c.Path || c.FileName}`, name: c.FileName, config: c, processes: rest, moved: procs.length - rest.length }
  })
  if (!failing.length) return groups
  return [{ key: 'attention', name: NEEDS_ATTENTION, config: null, processes: failing, moved: 0 }, ...groups]
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

export interface RowLook {
  /** The dot's tone; a failing process is always red. */
  tone: StateTone
  /** "up 4s", "failing 3m", "stopped 5d", or the state when there is no time. */
  time: string
  /** The class of the time's word. */
  timeClass: string
  failing: boolean
}

/** How a process row reads: its dot, and its time with the word that says what it means. */
export function rowLook(p: ProcessView): RowLook {
  const failing = isFailing(p)
  const tone = failing ? 'bad' : stateLook(p.State, p.Status).tone
  // Go's Since already says "up 4s", "failing 3m" or "stopped 5d".
  const time = p.Since || (failing ? 'failing' : stateLabel(p.State).toLowerCase())
  const timeClass = tone === 'bad' ? 't-bad' : tone === 'warn' ? 't-warn' : 'meta'
  return { tone, time, timeClass, failing }
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
