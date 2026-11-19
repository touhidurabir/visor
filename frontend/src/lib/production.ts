// Production servers ask first (spec decision "Tag"): Stop, Restart, Signal,
// Clear log, Stop all, Restart all, and restarting or shutting down
// supervisord, wherever they are started from. lib/api.ts runs every such
// call through guard(); a dialog that already asked (a menu's or a bulk
// confirm) calls withConsent() so the person isn't asked twice.
import { ref } from 'vue'
import { currentTab, serverTabs } from './tabs'

/** CancelledError: the person said no. Views treat it as nothing done, quietly. */
export class CancelledError extends Error {
  constructor() {
    super('')
    this.name = 'CancelledError'
  }
}

export const isCancelled = (e: unknown) => e instanceof CancelledError

export interface ProductionAsk {
  title: string
  message: string
  label: string
  server: string
  answer: (yes: boolean) => void
}

/** The question on screen; AppShell (and the popover) show it. */
export const productionAsk = ref<ProductionAsk | null>(null)

let consent: string | null = null
const lasting = new Map<string, number>()

/**
 * Runs fn with the person's go-ahead already given (their dialog asked) for
 * tab (the one on screen by default). When fn returns a promise, the
 * go-ahead lasts until it settles, so one answer covers every call of that
 * action (Stop all, group by group), on that tab only.
 */
export function withConsent<T>(fn: () => T, tab: string = currentTab.value): T {
  consent = tab
  let out: T
  try {
    out = fn()
  } finally {
    consent = null
  }
  if (out instanceof Promise) {
    lasting.set(tab, (lasting.get(tab) ?? 0) + 1)
    void out
      .finally(() => {
        const n = (lasting.get(tab) ?? 1) - 1
        if (n > 0) lasting.set(tab, n)
        else lasting.delete(tab)
      })
      .catch(() => {})
  }
  return out
}

const base = (p: string) => p.split('/').pop() || p

/** What each guarded call asks: [title, message, button]. */
const QUESTIONS: Record<string, (args: unknown[]) => [string, string, string]> = {
  Stop: ([n]) => [`Stop ${n}`, 'It stops and stays stopped until you start it again.', 'Stop'],
  Restart: ([n]) => [`Restart ${n}`, 'It stops, then starts again. Work in progress is interrupted.', 'Restart'],
  Signal: ([n, sig]) => [`Send ${sig} to ${n}`, `supervisord sends ${sig} to the process.`, 'Send'],
  ClearLogs: ([n]) => [`Clear the logs of ${n}`, 'supervisord empties both its stdout and stderr log files. This can’t be undone.', 'Clear logs'],
  StopAll: () => ['Stop all processes', 'Every process stops and stays stopped until you start it again.', 'Stop all'],
  RestartAll: () => ['Restart all processes', 'Every process stops and starts again. Work in progress is interrupted.', 'Restart all'],
  StopGroup: ([g]) => [`Stop ${g}:*`, 'Every process of the group stops.', 'Stop'],
  RestartGroup: ([g]) => [`Restart ${g}:*`, 'Every process of the group stops and starts again.', 'Restart'],
  StopFile: ([p]) => [`Stop all in ${base(String(p))}`, 'Every process of the file stops.', 'Stop all'],
  RestartFile: ([p]) => [`Restart all in ${base(String(p))}`, 'Every process of the file stops and starts again.', 'Restart all'],
  RestartDaemon: () => ['Restart supervisord', 'Every process stops and starts again from the config files.', 'Restart supervisord'],
  ShutdownDaemon: () => ['Shut down supervisord', 'Every process stops, and supervisord with them.', 'Shut down'],
}

export const GUARDED = new Set(Object.keys(QUESTIONS))

/** isProduction: the tab is a server tagged production. */
export function isProduction(tab: string): boolean {
  return serverTabs.value.some((t) => t.id === tab && t.tag === 'production')
}

/**
 * guard asks before a guarded call on a production tab; it resolves when the
 * call may go ahead and rejects with CancelledError when the person says no.
 */
export async function guard(tab: string, method: string, args: unknown[]): Promise<void> {
  const ok = consent === tab || (lasting.get(tab) ?? 0) > 0 // read now, before any wait
  if (ok || !GUARDED.has(method) || !isProduction(tab)) return
  const server = serverTabs.value.find((t) => t.id === tab)?.name ?? 'this server'
  const [what, message, label] = QUESTIONS[method](args)
  const yes = await new Promise<boolean>((answer) => {
    productionAsk.value?.answer(false)
    productionAsk.value = { title: `${what} on ${server}?`, message, label, server, answer }
  })
  productionAsk.value = null
  if (!yes) throw new CancelledError()
}
