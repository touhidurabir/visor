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

/**
 * The doubts that still hold for the text as it is now: the line still has
 * that setting (or section) and it still names the original. A doubt goes
 * away once the person changes that value; Go's line numbers are the copy's,
 * so a doubt whose line moved away is dropped too.
 */
export function liveDoubts(doubts: CopyDoubt[], content: string): CopyDoubt[] {
  const lines = content.split('\n')
  return doubts.filter((d) => {
    const at = d.Line - 1
    const line = lines[at]
    if (line === undefined) return false
    if (!d.Key) return line.trim().startsWith('[') && line.includes(d.Section)
    const m = new RegExp(`^\\s*${escape(d.Key)}\\s*[=:]`).exec(line)
    if (!m) return false
    // The value, with its indented continuation lines.
    let value = line.slice(m[0].length)
    for (let i = at + 1; i < lines.length && /^[ \t]/.test(lines[i]) && lines[i].trim(); i++) value += '\n' + lines[i]
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
