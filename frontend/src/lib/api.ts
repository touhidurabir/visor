// The per-tab services, bound to the tab on screen. The Go side takes the
// tab's id as every method's first argument (ServerAPI, ConfigAPI, …); these
// objects add it, so views call ServerService.Start(name) as before.
import { ConfigAPI, InboxAPI, IssueAPI, LogAPI, ServerAPI } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { currentTab } from './tabs'

type Rest<F> = F extends (server: string, ...args: infer A) => infer R ? (...args: A) => R : never
export type Bound<T> = { [K in keyof T]: Rest<T[K]> }

/** bind returns api with the tab's id (from tab()) put first in every call. */
export function bind<T extends object>(api: T, tab: () => string = () => currentTab.value): Bound<T> {
  const out: Record<string, unknown> = {}
  for (const [name, fn] of Object.entries(api)) {
    if (typeof fn === 'function') {
      out[name] = (...args: unknown[]) => (fn as (...a: unknown[]) => unknown)(tab(), ...args)
    }
  }
  return out as Bound<T>
}

export const ServerService = bind(ServerAPI)
export const ConfigService = bind(ConfigAPI)
export const IssueService = bind(IssueAPI)
export const LogService = bind(LogAPI)
export const InboxService = bind(InboxAPI)

/** forTab is the services for one tab whatever is on screen (the popover's sections). */
export function forTab(id: string) {
  const tab = () => id
  return {
    ServerService: bind(ServerAPI, tab),
    ConfigService: bind(ConfigAPI, tab),
    IssueService: bind(IssueAPI, tab),
    LogService: bind(LogAPI, tab),
    InboxService: bind(InboxAPI, tab),
  }
}
