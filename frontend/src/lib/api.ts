// The per-tab services, bound to the tab on screen. The Go side takes the
// tab's id as every method's first argument (ServerAPI, ConfigAPI, …); these
// objects add it, so views call ServerService.Start(name) as before.
import { ConfigAPI, InboxAPI, IssueAPI, LogAPI, ServerAPI } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { guard, GUARDED } from './production'
import { currentTab } from './tabs'

type Rest<F> = F extends (server: string, ...args: infer A) => infer R ? (...args: A) => R : never
export type Bound<T> = { [K in keyof T]: Rest<T[K]> }

const NONE = new Set<string>()

/**
 * bind returns api with the tab's id (from tab()) put first in every call;
 * the calls named in guarded ask first on production servers.
 */
export function bind<T extends object>(api: T, tab: () => string = () => currentTab.value, guarded: Set<string> = GUARDED): Bound<T> {
  const out: Record<string, unknown> = {}
  for (const [name, fn] of Object.entries(api)) {
    if (typeof fn !== 'function') continue
    const call = fn as (...a: unknown[]) => unknown
    out[name] = guarded.has(name)
      ? // Production servers ask first (lib/production.ts).
        async (...args: unknown[]) => {
          const id = tab()
          await guard(id, name, args)
          return call(id, ...args)
        }
      : (...args: unknown[]) => call(tab(), ...args)
  }
  return out as Bound<T>
}

// Only ServerService and ConfigService have calls production servers guard
// (LogService.Save only saves a copy of a log).
export const ServerService = bind(ServerAPI)
export const ConfigService = bind(ConfigAPI)
export const IssueService = bind(IssueAPI, undefined, NONE)
export const LogService = bind(LogAPI, undefined, NONE)
export const InboxService = bind(InboxAPI, undefined, NONE)

/**
 * useApi is the services for the tab on screen when a view sets up, kept for
 * its whole life: its clean-up (a log follow it stops) still reaches its own
 * tab after another tab comes on screen.
 */
export function useApi() {
  return forTab(currentTab.value)
}

/** forTab is the services for one tab whatever is on screen (the popover's sections). */
export function forTab(id: string) {
  const tab = () => id
  return {
    ServerService: bind(ServerAPI, tab),
    ConfigService: bind(ConfigAPI, tab),
    IssueService: bind(IssueAPI, tab, NONE),
    LogService: bind(LogAPI, tab, NONE),
    InboxService: bind(InboxAPI, tab, NONE),
  }
}
