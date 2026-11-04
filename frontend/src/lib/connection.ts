// What the dashboard says when Visor has no connection to supervisord: a
// plain title and one line, mapped from the overview's raw error (the Go
// side's text, which stays under "Show details"). The kinds follow Doctor's
// situations (internal/doctor): a socket that isn't open to this user is
// "denied", a turned-down password "auth", any other unreachable socket
// "down"; an error before Visor could even poll (no config, no socket in it,
// a saved password it couldn't read) is "setup".
import type { ServerView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Segment } from './health'

export type ConnectionKind = 'down' | 'denied' | 'auth' | 'password' | 'setup'

export interface ConnectionProblem {
  kind: ConnectionKind
  title: string
  /** One plain line on what it means; `mono` pieces are a socket or address. */
  line: Segment[]
  /** The socket or address that didn't answer, when the error names it. */
  address: string
  /** Why an action is unavailable, for its tooltip. */
  reason: string
  /** Visor keeps polling, so the problem can clear by itself. */
  retrying: boolean
  /** The raw error, for "Show details". */
  raw: string
}

type Server = Pick<ServerView, 'Reachable' | 'Health' | 'Error'>

/**
 * No answer from supervisord, once the first poll (or the failed setup) has
 * said so: when the sidebar and the popover show a connection badge.
 */
export function isDown(s: Server | null | undefined): boolean {
  return !!s && s.Health !== 'unknown' && !s.Reachable
}

/** "dial unix /path/s.sock: …" or "dial tcp 127.0.0.1:9001: …" → the address. */
function addressOf(raw: string): string {
  return /dial (?:unix|tcp) (\S+?): /.exec(raw)?.[1] ?? ''
}

const OUTCOME = 'so Visor can’t show or control processes. Your config files are fine.'

/** The banner's words for a down connection, or null when connected (or not yet known). */
export function connectionProblem(s: Server | null | undefined): ConnectionProblem | null {
  if (!s || !isDown(s)) return null
  const raw = s.Error ?? ''
  const address = addressOf(raw)
  const polling = /supervisord: (unreachable|unauthorized)/.test(raw)
  const base = { address, raw, retrying: polling }
  if (/supervisord: unauthorized/.test(raw)) {
    return {
      ...base,
      kind: 'auth',
      title: 'supervisord wants a password',
      line: [{ text: `supervisord is running, but it turned down the username and password Visor used, ${OUTCOME}` }],
      reason: 'supervisord turned down Visor’s password',
    }
  }
  if (polling && /permission denied/i.test(raw)) {
    return {
      ...base,
      kind: 'denied',
      title: 'supervisord won’t let Visor in',
      line: [{ text: `supervisord’s socket isn’t open to your user, ${OUTCOME}` }],
      reason: 'supervisord won’t let Visor in',
    }
  }
  if (polling) {
    return {
      ...base,
      kind: 'down',
      title: 'supervisord isn’t running',
      line: address
        ? [{ text: 'Nothing answers at ' }, { text: address, mono: true }, { text: `, ${OUTCOME}` }]
        : [{ text: `Nothing answers on its socket, ${OUTCOME}` }],
      reason: 'supervisord isn’t running',
    }
  }
  if (/cannot read the supervisord password/.test(raw)) {
    return {
      ...base,
      kind: 'password',
      title: 'Visor can’t read the saved password',
      line: [{ text: 'Allow Visor to use the saved password, or enter it again in Settings. Until then, Visor can’t show or control processes.' }],
      reason: 'Visor can’t read the saved password',
    }
  }
  return {
    ...base,
    kind: 'setup',
    title: 'Visor can’t connect to supervisord',
    line: [{ text: `Something in the setup stops Visor from connecting, ${OUTCOME} Doctor says what to fix.` }],
    reason: 'Visor isn’t connected to supervisord',
  }
}

/**
 * The short badge beside the server's name (sidebar, popover header): "Not
 * running" only when supervisord isn't running, "Not connected" for any
 * other problem (a password, a socket closed to this user, the setup), and
 * null while connected.
 */
export function connectionBadge(problem: Pick<ConnectionProblem, 'kind'> | null | undefined): string | null {
  if (!problem) return null
  return problem.kind === 'down' ? 'Not running' : 'Not connected'
}
