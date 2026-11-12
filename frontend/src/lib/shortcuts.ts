// Visor's keyboard shortcuts, in one table, so each hint and its handler
// come from the same place. macOS shows them in the menu bar
// (internal/app/appmenu.go) with ⌘; Linux has no menu bar, so the app shell
// carries them out with Ctrl.
import { isMac } from './platform'

export const SHORTCUTS = {
  settings: ',',
  newConfig: 'n',
  dashboard: '1',
  issues: '2',
  find: 'f',
  reload: 'r',
  closeWindow: 'w',
  quit: 'q',
} as const

export type ShortcutId = keyof typeof SHORTCUTS

const shown = (key: string) => key.toUpperCase()

/** As menus show it: "⌘N" on macOS, "Ctrl+N" on Linux. */
export function shortcutHint(id: ShortcutId): string {
  return modKeyHint(SHORTCUTS[id])
}

/** For aria-keyshortcuts: "Meta+N" or "Control+N". */
export function shortcutAria(id: ShortcutId): string {
  return `${isMac() ? 'Meta' : 'Control'}+${shown(SHORTCUTS[id])}`
}

/** The event holds this platform's modifier (⌘ or Ctrl) and nothing else. */
export function modOnly(e: KeyboardEvent): boolean {
  if (e.altKey || e.shiftKey) return false
  return isMac() ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey
}

/** Which shortcut the key press is, if any. */
export function shortcutOf(e: KeyboardEvent): ShortcutId | null {
  if (!modOnly(e)) return null
  const key = e.key.toLowerCase()
  for (const [id, k] of Object.entries(SHORTCUTS)) {
    if (k === key) return id as ShortcutId
  }
  return null
}

/** A key with this platform's modifier, for keys the menu bar doesn't hold (Copy): "⌘C" or "Ctrl+C". */
export function modKeyHint(key: string): string {
  return isMac() ? `⌘${shown(key)}` : `Ctrl+${shown(key)}`
}
