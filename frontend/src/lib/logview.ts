// What the Logs tab shows, as pure functions over visor:log chunks: whole
// numbered lines, the current partial line, notes for truncation and
// skipped output, and search highlighting.
import type { LogChunkView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

export type Stream = 'stdout' | 'stderr'

export interface LogLine {
  /** Line number since the view started (or since the log was cleared); 0 for notes. */
  n: number
  text: string
  note?: boolean
}

export interface LogState {
  lines: LogLine[]
  /** Text after the last newline, still being written. */
  partial: string
  next: number
}

/** How many lines a log view keeps; older ones scroll away. */
export const MAX_LINES = 5000

export const TRUNCATED_NOTE = 'The log was cleared or rotated.'
export const SKIPPED_NOTE = 'Some output was written faster than Visor reads it and is not shown.'

export function emptyLog(): LogState {
  return { lines: [], partial: '', next: 1 }
}

/** applyChunk returns the state after one chunk; it never mutates s. */
export function applyChunk(s: LogState, c: Pick<LogChunkView, 'Text' | 'Truncated' | 'Skipped'>, maxLines = MAX_LINES): LogState {
  let lines = s.lines
  let partial = s.partial
  let next = s.next
  if (c.Truncated) {
    lines = [{ n: 0, text: TRUNCATED_NOTE, note: true }]
    partial = ''
    next = 1
  } else {
    lines = lines.slice()
  }
  if (c.Skipped) {
    if (partial !== '') lines.push({ n: next++, text: partial })
    partial = ''
    lines.push({ n: 0, text: SKIPPED_NOTE, note: true })
  }
  const parts = (partial + c.Text).split('\n')
  partial = parts.pop() ?? ''
  for (const p of parts) lines.push({ n: next++, text: p.endsWith('\r') ? p.slice(0, -1) : p })
  if (lines.length > maxLines) lines = lines.slice(lines.length - maxLines)
  return { lines, partial, next }
}

/** shownLines is every line to render, with the partial line last. */
export function shownLines(s: LogState): LogLine[] {
  return s.partial === '' ? s.lines : [...s.lines, { n: s.next, text: s.partial }]
}

export interface Segment {
  text: string
  match: boolean
}

/** highlight splits text around case-insensitive, literal matches of query. */
export function highlight(text: string, query: string): Segment[] {
  if (!query) return [{ text, match: false }]
  const hay = text.toLowerCase()
  const needle = query.toLowerCase()
  const out: Segment[] = []
  let from = 0
  for (let i = hay.indexOf(needle); i !== -1; i = hay.indexOf(needle, from)) {
    if (i > from) out.push({ text: text.slice(from, i), match: false })
    out.push({ text: text.slice(i, i + needle.length), match: true })
    from = i + needle.length
  }
  if (from < text.length || out.length === 0) out.push({ text: text.slice(from), match: false })
  return out
}

/** countMatches counts matches of query in the log lines (not notes). */
export function countMatches(lines: LogLine[], query: string): number {
  if (!query) return 0
  let n = 0
  for (const l of lines) if (!l.note) n += highlight(l.text, query).filter((s) => s.match).length
  return n
}

/**
 * noLogMessage recognises LogService's "no log file" errors (Go wraps
 * logs.ErrNoLog) and returns what to show instead of an error; null for
 * any other error.
 */
export function noLogMessage(e: unknown): string | null {
  const text = e instanceof Error ? e.message : String(e)
  if (text.includes('stderr is sent to the stdout log')) return 'stderr is sent to the stdout log'
  if (text.includes('this stream has no log file')) return 'This stream has no log file'
  return null
}
