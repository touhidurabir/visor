// What the Logs tab shows, as pure functions over visor:log chunks: whole
// numbered lines, the current partial line, notes for truncation and
// skipped output, and search highlighting.
import type { LogChunkView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

export type Stream = 'stdout' | 'stderr'

export interface LogLine {
  /** Unique within a view and increasing, never reset (a stable render key). */
  id: number
  /** Line number since the view started (or since the log was cleared); 0 for notes. */
  n: number
  text: string
  note?: boolean
  /** The entry's level, when the line says it (see levelOf) or continues an entry that does. */
  level?: Level
  /** The line continues the entry above (a stack trace line), taking its level. */
  more?: boolean
}

export interface LogState {
  lines: LogLine[]
  /** Text after the last newline, still being written. */
  partial: string
  next: number
  /** The id the next line gets; unlike next, a cleared log doesn't reset it. */
  seq: number
}

/** How many lines a log view keeps; older ones scroll away. */
export const MAX_LINES = 5000

export const TRUNCATED_NOTE = 'The log was cleared or rotated.'
export const SKIPPED_NOTE = 'Some output was written faster than Visor reads it and is not shown.'

export function emptyLog(): LogState {
  return { lines: [], partial: '', next: 1, seq: 1 }
}

/** applyChunk returns the state after one chunk; it never mutates s. */
export function applyChunk(s: LogState, c: Pick<LogChunkView, 'Text' | 'Truncated' | 'Skipped'>, maxLines = MAX_LINES): LogState {
  let lines = s.lines
  let partial = s.partial
  let next = s.next
  let seq = s.seq
  if (c.Truncated) {
    lines = [{ id: seq++, n: 0, text: TRUNCATED_NOTE, note: true }]
    partial = ''
    next = 1
  } else {
    lines = lines.slice()
  }
  if (c.Skipped) {
    if (partial !== '') lines.push({ id: seq++, n: next++, text: partial })
    partial = ''
    lines.push({ id: seq++, n: 0, text: SKIPPED_NOTE, note: true })
  }
  const parts = (partial + c.Text).split('\n')
  partial = parts.pop() ?? ''
  for (const p of parts) lines.push(leveled({ id: seq++, n: next++, text: p.endsWith('\r') ? p.slice(0, -1) : p }, lines[lines.length - 1]))
  if (lines.length > maxLines) lines = lines.slice(lines.length - maxLines)
  return { lines, partial, next, seq }
}

/** shownLines is every line to render, with the partial line last (it has the id it keeps once it ends). */
export function shownLines(s: LogState): LogLine[] {
  return s.partial === '' ? s.lines : [...s.lines, leveled({ id: s.seq, n: s.next, text: s.partial }, s.lines[s.lines.length - 1])]
}

// --- Levels ---------------------------------------------------------------
//
// Logs come in every shape, so a line gets a level only when it says so in a
// form that is hard to mistake: a level key (JSON, logfmt), a Monolog
// "channel.LEVEL:", a leading Rails/klog letter, a queue worker's status, an
// exception or traceback start, an upper-case level word near the start, or
// a bracketed one ([error], <warn>). Anything else stays unmarked, so a log
// with no levels looks as it always did.

export type Level = 'error' | 'warn' | 'info' | 'debug'
export const LEVELS: Level[] = ['error', 'warn', 'info', 'debug']

const WORDS: Record<string, Level> = {
  emergency: 'error', emerg: 'error', alert: 'error', critical: 'error', crit: 'error', fatal: 'error', panic: 'error',
  error: 'error', err: 'error', severe: 'error', fail: 'error', failed: 'error',
  warning: 'warn', warn: 'warn',
  notice: 'info', info: 'info', information: 'info', done: 'info', processed: 'info',
  debug: 'debug', trace: 'debug', verbose: 'debug', running: 'debug', processing: 'debug',
}
const LETTERS: Record<string, Level> = { F: 'error', E: 'error', W: 'warn', I: 'info', D: 'debug' }

const JSON_LEVEL = /"(?:level|level_name|levelname|severity|lvl|loglevel|log\.level)"\s*:\s*"?([A-Za-z]+|\d+)/i
const KV_LEVEL = /(?:^|\s)(?:level|lvl|severity)=["']?([A-Za-z]+)/i
const MONOLOG = /^\[[^\]]{8,40}\]\s+[\w.-]+\.([A-Z]+):/
const RAILS = /^([DIWEF]), \[/
const KLOG = /^([IWEF])\d{4} \d\d:/
const QUEUE_STATUS = /\s(DONE|FAIL|FAILED|RUNNING)\s*$/
const QUEUE_OLD = /\]\s*(Processing|Processed|Failed):\s/
const THROWN = /^(?:Traceback \(most recent call last\)|panic: |PHP (?:Fatal|Parse) error|Uncaught |(?:[\w$]+[.\\])*[\w$]*(?:Error|Exception)(?::|\s+in\s|$))/
const UPPER = /\b(EMERGENCY|EMERG|ALERT|CRITICAL|CRIT|FATAL|PANIC|ERROR|ERR|SEVERE|WARNING|WARN|NOTICE|INFO|DEBUG|TRACE)\b/
const BRACKETED = /[[<(](emergency|alert|critical|crit|fatal|error|err|warning|warn|notice|info|debug|trace)[\]>)]/i
const CONTINUES = /^(?:\s|#\d+ |\[stacktrace\]|Stack trace:|Caused by|Next |\.\.\. \d+ more|"?\}|\))/

/** pino and bunyan count 10 to 60; Monolog 100 to 600. */
function levelOfNumber(n: number): Level {
  if (n >= 100) return n >= 400 ? 'error' : n >= 300 ? 'warn' : n >= 200 ? 'info' : 'debug'
  return n >= 50 ? 'error' : n >= 40 ? 'warn' : n >= 30 ? 'info' : 'debug'
}

/** levelOf returns the level a log line states, or undefined when it states none. */
export function levelOf(text: string): Level | undefined {
  const head = text.length > 240 ? text.slice(0, 240) : text
  let m: RegExpExecArray | null
  if (head.trimStart().startsWith('{') && (m = JSON_LEVEL.exec(text.length > 4000 ? text.slice(0, 4000) : text))) {
    return /^\d+$/.test(m[1]) ? levelOfNumber(Number(m[1])) : WORDS[m[1].toLowerCase()]
  }
  if ((m = KV_LEVEL.exec(head)) && WORDS[m[1].toLowerCase()]) return WORDS[m[1].toLowerCase()]
  if ((m = MONOLOG.exec(head)) && WORDS[m[1].toLowerCase()]) return WORDS[m[1].toLowerCase()]
  if ((m = RAILS.exec(head) ?? KLOG.exec(head))) return LETTERS[m[1]]
  if ((m = QUEUE_STATUS.exec(text) ?? QUEUE_OLD.exec(head))) return WORDS[m[1].toLowerCase()]
  if (THROWN.test(head)) return 'error'
  if ((m = UPPER.exec(head.slice(0, 120)))) return WORDS[m[1].toLowerCase()]
  if ((m = BRACKETED.exec(head))) return WORDS[m[1].toLowerCase()]
  return undefined
}

/** leveled sets a line's level: its own, or else the one of the entry it continues (prev is the line above). */
function leveled(l: LogLine, prev: LogLine | undefined): LogLine {
  const level = levelOf(l.text)
  if (level) {
    l.level = level
  } else if (prev?.level && CONTINUES.test(l.text)) {
    l.level = prev.level
    l.more = true
  }
  return l
}

/** levelCounts counts the entries of each level (continuation lines don't count). */
export function levelCounts(lines: LogLine[]): Record<Level, number> {
  const c: Record<Level, number> = { error: 0, warn: 0, info: 0, debug: 0 }
  for (const l of lines) if (l.level && !l.more) c[l.level]++
  return c
}

/** onlyLevels keeps the lines of the given levels, and every note; no levels keeps everything. */
export function onlyLevels(lines: LogLine[], levels: ReadonlySet<Level>): LogLine[] {
  if (levels.size === 0) return lines
  return lines.filter((l) => l.note || (l.level !== undefined && levels.has(l.level)))
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

/** How many matches a log view highlights, newest first: a <mark> each, so tens of thousands froze the view. */
export const MAX_MARKS = 1000

function countIn(text: string, needle: string): number {
  const hay = text.toLowerCase()
  let n = 0
  for (let i = hay.indexOf(needle); i !== -1; i = hay.indexOf(needle, i + needle.length)) n++
  return n
}

/** countMatches counts matches of query in the log lines (not notes). */
export function countMatches(lines: LogLine[], query: string): number {
  if (!query) return 0
  const needle = query.toLowerCase()
  let n = 0
  for (const l of lines) if (!l.note) n += countIn(l.text, needle)
  return n
}

/** markedLines returns the ids of the newest lines to highlight, whose matches together stay within max. */
export function markedLines(lines: LogLine[], query: string, max = MAX_MARKS): Set<number> {
  const ids = new Set<number>()
  if (!query) return ids
  const needle = query.toLowerCase()
  let n = 0
  for (let i = lines.length - 1; i >= 0; i--) {
    const l = lines[i]
    if (l.note) continue
    const c = countIn(l.text, needle)
    if (c === 0) continue
    if (n + c > max) break
    n += c
    ids.add(l.id)
  }
  return ids
}

/** How many matching lines a whole-file search shows, newest first. */
export const WHOLE_LIMIT = 500

/** sizeText words a byte count for search progress: "820 KB", "26.9 MB". */
export function sizeText(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024)).toLocaleString()} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
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
