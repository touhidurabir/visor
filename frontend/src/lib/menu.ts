// Choices from Visor's menu bar that the page carries out (visor:menu):
// "find", "reload" (check supervisord's config for changes) and
// "restart-all". The page that is showing handles them; Reload Config and
// Restart All go to the dashboard first when nothing on screen can.
import { Events } from '@wailsio/runtime'
import { VisorMenu } from '../../bindings/events'

type Router = { push: (to: string) => unknown }

const handlers = new Map<string, () => void>()
const toDashboard = new Set(['reload', 'restart-all'])
let pending: string | null = null
let off: (() => void) | null = null

/** Starts listening for menu choices (once, from the app shell). */
export function followMenu(router: Router) {
  off?.()
  off = Events.On(VisorMenu, (ev: { data: unknown }) => {
    const name = String(ev.data)
    const run = handlers.get(name)
    if (run) return run()
    if (toDashboard.has(name)) {
      pending = name
      void router.push('/')
    }
  })
}

/** Handles name while the calling page is showing; returns the undo. */
export function onMenu(name: string, run: () => void): () => void {
  handlers.set(name, run)
  return () => {
    if (handlers.get(name) === run) handlers.delete(name)
  }
}

/** Whether name was chosen before this page showed (then forgets it). */
export function takePendingMenu(name: string): boolean {
  if (pending !== name) return false
  pending = null
  return true
}

/** For tests. */
export function resetMenu() {
  off?.()
  off = null
  handlers.clear()
  pending = null
}
