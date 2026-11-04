// Add new → "Copy an existing config": what the copy renamed, and the
// settings Go flagged as still naming the original (CopyDoubt), shown as
// warnings above the editor and in its gutter while they still apply.
import type { CopyDoubt, Rename } from '../../bindings/github.com/touhidurabir/visor/internal/config'

export interface Mark {
  line: number
  level: 'error' | 'warning'
}

/** "mailer-worker → mailer-worker-copy, group mailer → mailer-copy". */
export function renamesNote(renames: Rename[]): string {
  return renames.map((r) => `${r.Kind === 'group' ? 'group ' : ''}${r.From} → ${r.To}`).join(', ')
}

// %(program_name)s and the like name the copy itself, as Go's expansionRe.
const EXPANSION = /%%|%\(\w+\)[0-9]*[sd]/g
const NAME_CHAR = /[A-Za-z0-9_-]/

/** name appears in v as a whole name (no name character right before or after). */
function mentions(v: string, name: string): boolean {
  if (!name) return false
  for (let i = v.indexOf(name); i >= 0; i = v.indexOf(name, i + 1)) {
    const before = v[i - 1]
    const after = v[i + name.length]
    if ((before === undefined || !NAME_CHAR.test(before)) && (after === undefined || !NAME_CHAR.test(after))) return true
  }
  return false
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** Cuts at the first ';' that follows whitespace, as Go's stripInlineComment. */
function stripInlineComment(s: string): string {
  for (let i = 0; i < s.length; i++) if (s[i] === ';' && (i === 0 || s[i - 1] === ' ' || s[i - 1] === '\t')) return s.slice(0, i)
  return s
}

/**
 * The value of key on line at (0-based), as Go's ParseINI reads it: without
 * an inline comment, with its indented continuation lines, which go on past
 * blank and comment lines. null when that line doesn't set key.
 */
function valueAt(lines: string[], at: number, key: string): string | null {
  const line = lines[at]?.replace(/\r$/, '')
  if (line === undefined) return null
  // A setting starts its line; an indented line continues the one above.
  const m = new RegExp(`^${escape(key)}[ \\t]*[=:]`, 'i').exec(line)
  if (!m) return null
  let value = stripInlineComment(line.slice(m[0].length)).trim()
  for (let i = at + 1; i < lines.length; i++) {
    const l = lines[i].replace(/\r$/, '')
    const t = l.trim()
    if (t === '' || t[0] === ';' || t[0] === '#') continue
    if (!/^[ \t]/.test(l)) break
    const part = stripInlineComment(l).trim()
    if (part) value += '\n' + part
  }
  return value
}

/** Log files: Go flags a fixed path the copy would share with the original. */
const LOG_KEYS = new Set(['stdout_logfile', 'stderr_logfile'])

export interface Draft {
  fileName: string
  content: string
}

/**
 * The doubts that still hold for the draft as it is now (now), against the
 * copy as Go made it (copy). A doubt about a setting goes away once the
 * person changes it so it no longer names the original, or, for a log file
 * the two would share, changes its value at all; one about a section once
 * its header goes; one about the file name (line 0) once the file is
 * renamed. Go's line numbers are the copy's, so a doubt whose line moved
 * away is dropped too.
 */
export function liveDoubts(doubts: CopyDoubt[], now: Draft, copy: Draft): CopyDoubt[] {
  const lines = now.content.split('\n')
  const copyLines = copy.content.split('\n')
  return doubts.filter((d) => {
    if (!d.Line) return now.fileName === copy.fileName
    const at = d.Line - 1
    const line = lines[at]
    if (line === undefined) return false
    if (!d.Key) return line.trim().startsWith('[') && line.includes(d.Section)
    const value = valueAt(lines, at, d.Key)
    if (value === null) return false
    if (LOG_KEYS.has(d.Key)) return value === valueAt(copyLines, at, d.Key)
    return mentions(value.replace(EXPANSION, ' '), d.Name)
  })
}

/** The editor's marks for the doubts: a warning on each line. */
export function doubtMarks(doubts: CopyDoubt[]): Mark[] {
  return [...new Set(doubts.map((d) => d.Line))].filter((l) => l > 0).map((line) => ({ line, level: 'warning' as const }))
}

/** Joins mark lists, keeping the worst mark per line. */
export function mergeMarks(...lists: Mark[][]): Mark[] {
  const worst = new Map<number, Mark['level']>()
  for (const list of lists) for (const m of list) if (worst.get(m.line) !== 'error') worst.set(m.line, m.level)
  return [...worst].sort((a, b) => a[0] - b[0]).map(([line, level]) => ({ line, level }))
}
