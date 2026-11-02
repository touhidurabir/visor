// Helpers for the line-by-line explanation (ExplainView, from Go): apply a
// fix to the text, add suggested lines, and turn problems into editor marks.
import type { EditView, ExplainLineView, ExplainView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

/**
 * Applies one edit: replace line `Line` (1-based), insert after it, or
 * with `Rename` rename only that line's key (value, comment and
 * continuation lines stay as they are).
 */
export function applyEdit(content: string, e: EditView): string {
  const lines = content.split('\n')
  if (e.Rename) {
    const m = /^(\s*)[^\s=:;#[]+(?=\s*[=:])/.exec(lines[e.Line - 1] ?? '')
    if (!m) return content
    lines[e.Line - 1] = m[1] + e.Rename + lines[e.Line - 1].slice(m[0].length)
  } else if (e.Insert) {
    lines.splice(Math.min(e.Line, lines.length), 0, e.Text)
  } else if (e.Line >= 1 && e.Line <= lines.length) {
    lines[e.Line - 1] = e.Text
  } else {
    return content
  }
  return lines.join('\n')
}

/** Inserts lines after line `after` (1-based), in order. */
export function addLines(content: string, after: number, add: string[]): string {
  const lines = content.split('\n')
  lines.splice(Math.min(after, lines.length), 0, ...add)
  return lines.join('\n')
}

/** The editor's gutter marks: the worst problem on each line. */
export function explainMarks(v: ExplainView | null): { line: number; level: 'error' | 'warning' }[] {
  if (!v) return []
  if (v.ParseError) return v.ParseError.Line > 0 ? [{ line: v.ParseError.Line, level: 'error' }] : []
  const out: { line: number; level: 'error' | 'warning' }[] = []
  for (const s of v.Sections ?? [])
    for (const l of s.Lines ?? [])
      if (l.Kind !== 'unset' && (l.Level === 'error' || l.Level === 'warning')) out.push({ line: l.Line, level: l.Level })
  for (const p of v.FileProblems ?? []) if (p.Line > 0) out.push({ line: p.Line, level: p.Level === 'error' ? 'error' : 'warning' })
  return out
}

/** A line worth showing under "Only problems". */
export function isProblem(l: ExplainLineView): boolean {
  return l.Level !== ''
}

/** "2.4 MB", "512 bytes"; "" when unknown. */
export function fileSize(n: number): string {
  if (n < 0) return ''
  if (n < 1024) return `${n} ${n === 1 ? 'byte' : 'bytes'}`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

/** "Fix 1 line to save", "Fix 3 lines to save"; "problems" when some are about the whole file. */
export function fixToSave(n: number, lines = true): string {
  const what = lines ? (n === 1 ? 'line' : 'lines') : n === 1 ? 'problem' : 'problems'
  return `Fix ${n} ${what} to save`
}

/** Whether every error that blocks saving is on a line of the file. */
export function errorsOnLines(v: ExplainView | null): boolean {
  return !(v?.FileProblems ?? []).some((p) => p.Level === 'error' && !p.Line)
}
