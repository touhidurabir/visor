// Light / dark theme. The choice is Settings.appearance ('system' follows
// macOS). Everything here is pure or takes the DOM as an argument, so it
// runs under vitest without a browser; theme-sync.ts wires it to the page.
export type Appearance = 'system' | 'light' | 'dark'
export type Theme = 'light' | 'dark'

export const APPEARANCES: readonly Appearance[] = ['system', 'light', 'dark']

/** The localStorage key index.html's early script reads to avoid a flash. */
export const APPEARANCE_CACHE_KEY = 'visor.appearance'

/** normalizeAppearance maps anything unknown to 'system', as Go's Clamp does. */
export function normalizeAppearance(v: unknown): Appearance {
  return v === 'light' || v === 'dark' ? v : 'system'
}

/** resolveTheme is the theme to show: the choice, or the OS's for 'system'. */
export function resolveTheme(appearance: Appearance, systemDark: boolean): Theme {
  if (appearance === 'light') return 'light'
  if (appearance === 'dark') return 'dark'
  return systemDark ? 'dark' : 'light'
}

/** The part of document.documentElement that applyTheme writes. */
export interface ThemeRoot {
  dataset: Record<string, string | undefined>
  style: { colorScheme: string }
}

/** applyTheme sets data-theme (which tokens.css keys on) and color-scheme (native controls, scrollbars). */
export function applyTheme(root: ThemeRoot, appearance: Appearance, systemDark: boolean): Theme {
  const theme = resolveTheme(appearance, systemDark)
  root.dataset.theme = theme
  root.style.colorScheme = theme
  return theme
}
