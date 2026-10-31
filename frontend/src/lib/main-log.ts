// Pure helpers for the supervisord log tab: level chips, URL state and
// the wording of the footer.
export type LevelGroup = 'errors' | 'warnings' | 'info' | 'debug'

export const LEVEL_GROUPS: { key: LevelGroup; label: string; levels: string[] }[] = [
  { key: 'errors', label: 'Errors', levels: ['CRIT', 'ERRO'] },
  { key: 'warnings', label: 'Warnings', levels: ['WARN'] },
  { key: 'info', label: 'Info', levels: ['INFO'] },
  { key: 'debug', label: 'Debug', levels: ['DEBG', 'TRAC', 'BLAT'] },
]

export const DEFAULT_GROUPS: LevelGroup[] = ['errors', 'warnings', 'info']

/** levelsFor lists the supervisord levels behind the chosen chips, in chip order. */
export function levelsFor(groups: readonly LevelGroup[]): string[] {
  return LEVEL_GROUPS.filter((g) => groups.includes(g.key)).flatMap((g) => g.levels)
}

/** parseGroups reads "errors,info" from the URL; junk or nothing gives the default. */
export function parseGroups(raw: unknown): LevelGroup[] {
  if (typeof raw !== 'string' || raw === '') return [...DEFAULT_GROUPS]
  const keys = new Set(raw.split(','))
  const picked = LEVEL_GROUPS.filter((g) => keys.has(g.key)).map((g) => g.key)
  return picked.length ? picked : [...DEFAULT_GROUPS]
}

export const groupsParam = (groups: readonly LevelGroup[]) => LEVEL_GROUPS.filter((g) => groups.includes(g.key)).map((g) => g.key).join(',')

/** levelClass names the colour of a level pill. */
export function levelClass(level: string): 'err' | 'warn' | 'info' | 'debug' {
  if (level === 'CRIT' || level === 'ERRO') return 'err'
  if (level === 'WARN') return 'warn'
  if (level === 'INFO') return 'info'
  return 'debug'
}

/** logTime is "Nov 1, 14:36:32" in local time. */
export function logTime(ms: number): string {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  const month = d.toLocaleString('en-US', { month: 'short' })
  return `${month} ${d.getDate()}, ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

/** footerNote: "Showing 3 of 10 matching lines", or all of them. */
export function footerNote(shown: number, matched: number): string {
  const lines = (n: number) => `${n} ${n === 1 ? 'line' : 'lines'}`
  return shown < matched ? `Showing ${shown} of ${matched} matching lines` : `Showing ${lines(matched)}`
}
