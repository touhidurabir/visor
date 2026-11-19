import { describe, expect, it } from 'vitest'
import { applyChunk, countMatches, emptyLog, highlight, levelCounts, levelOf, markedLines, noLogMessage, onlyLevels, shownLines, type Level } from './logview'

const chunk = (Text: string, extra: Partial<{ Truncated: boolean; Skipped: boolean }> = {}) => ({
  ID: 'log-1',
  Text,
  Truncated: false,
  Skipped: false,
  ...extra,
})
const texts = (s: ReturnType<typeof emptyLog>) => shownLines(s).map((l) => (l.note ? `[${l.text}]` : `${l.n}:${l.text}`))

describe('applyChunk', () => {
  it('splits whole lines and keeps a partial last line until it ends', () => {
    let s = applyChunk(emptyLog(), chunk('one\ntw'))
    expect(texts(s)).toEqual(['1:one', '2:tw'])
    s = applyChunk(s, chunk('o\nthree\n'))
    expect(texts(s)).toEqual(['1:one', '2:two', '3:three'])
  })
  it('drops carriage returns at line ends', () => {
    expect(texts(applyChunk(emptyLog(), chunk('a\r\nb\r\n')))).toEqual(['1:a', '2:b'])
  })
  it('keeps the newest lines and their numbers when over the cap', () => {
    const s = applyChunk(emptyLog(), chunk('1\n2\n3\n4\n5\n'), 3)
    expect(texts(s)).toEqual(['3:3', '4:4', '5:5'])
  })
  it('starts over after truncation, with a note', () => {
    let s = applyChunk(emptyLog(), chunk('old\nlines\n'))
    s = applyChunk(s, chunk('new\n', { Truncated: true }))
    expect(texts(s)).toEqual(['[The log was cleared or rotated.]', '1:new'])
  })
  it('marks skipped output and ends the partial line before it', () => {
    let s = applyChunk(emptyLog(), chunk('a\nhal'))
    s = applyChunk(s, chunk('later\n', { Skipped: true }))
    expect(texts(s)).toEqual(['1:a', '2:hal', '[Some output was written faster than Visor reads it and is not shown.]', '3:later'])
  })
})

describe('line ids (M6)', () => {
  const ids = (s: ReturnType<typeof emptyLog>) => shownLines(s).map((l) => l.id)
  it('keep each line’s id when older lines scroll away', () => {
    let s = applyChunk(emptyLog(), chunk('1\n2\n3\n'), 3)
    const before = ids(s)
    s = applyChunk(s, chunk('4\n'), 3)
    expect(ids(s).slice(0, 2)).toEqual(before.slice(1))
    expect(new Set(ids(s)).size).toBe(3)
  })
  it('never reuse an id, even after the log is cleared', () => {
    let s = applyChunk(emptyLog(), chunk('a\nb\n'))
    const seen = new Set(ids(s))
    s = applyChunk(s, chunk('c\n', { Truncated: true }))
    for (const id of ids(s)) expect(seen.has(id)).toBe(false)
    expect(texts(s)).toEqual(['[The log was cleared or rotated.]', '1:c'])
  })
  it('give a partial line the id it keeps once it ends', () => {
    let s = applyChunk(emptyLog(), chunk('one\ntw'))
    const partial = ids(s)[1]
    s = applyChunk(s, chunk('o\n'))
    expect(ids(s)[1]).toBe(partial)
  })
  it('are increasing', () => {
    let s = applyChunk(emptyLog(), chunk('a\nhal'))
    s = applyChunk(s, chunk('later\n', { Skipped: true }))
    const got = ids(s)
    expect(got).toEqual([...got].sort((x, y) => x - y))
    expect(new Set(got).size).toBe(got.length)
  })
})

describe('highlight', () => {
  it('splits text around case-insensitive matches', () => {
    expect(highlight('Error: an error', 'error')).toEqual([
      { text: 'Error', match: true },
      { text: ': an ', match: false },
      { text: 'error', match: true },
    ])
  })
  it('returns the text whole without a query or a match', () => {
    expect(highlight('plain', '')).toEqual([{ text: 'plain', match: false }])
    expect(highlight('plain', 'zzz')).toEqual([{ text: 'plain', match: false }])
  })
  it('treats the query literally', () => {
    expect(highlight('a.b axb', '.')).toEqual([
      { text: 'a', match: false },
      { text: '.', match: true },
      { text: 'b axb', match: false },
    ])
  })
  it('counts matches across lines', () => {
    const s = applyChunk(emptyLog(), chunk('boom\nok\nBOOM boom\n'))
    expect(countMatches(shownLines(s), 'boom')).toBe(3)
    expect(countMatches(shownLines(s), '')).toBe(0)
  })
})

describe('noLogMessage', () => {
  it('names a redirected stderr', () => {
    expect(noLogMessage(new Error('stderr is sent to the stdout log: this stream has no log file'))).toBe(
      'stderr is sent to the stdout log',
    )
  })
  it('names a stream without a log file', () => {
    expect(noLogMessage(new Error('this stream has no log file'))).toBe('This stream has no log file')
    expect(noLogMessage('cannot follow the stdout log of x: this stream has no log file')).toBe('This stream has no log file')
  })
  it('is null for other errors', () => {
    expect(noLogMessage(new Error('supervisord: unreachable'))).toBeNull()
  })
})

describe('markedLines', () => {
  const s = applyChunk(emptyLog(), { Text: 'aa\nb\na\nAAA\n', Truncated: false, Skipped: false })
  const ids = (q: string, max?: number) => [...markedLines(s.lines, q, max)].map((id) => s.lines.find((l) => l.id === id)!.text)

  it('highlights the newest lines whose matches stay within the cap', () => {
    expect(ids('a')).toEqual(['AAA', 'a', 'aa'])
    expect(ids('a', 4)).toEqual(['AAA', 'a']) // "aa" would make 6
    expect(ids('a', 2)).toEqual([]) // the newest line alone is over the cap
    expect(ids('')).toEqual([])
  })

  it('counts overlapping text once per match', () => {
    expect(countMatches(s.lines, 'aa')).toBe(2) // "aa" and the first two of "AAA"
  })
})

describe('levelOf', () => {
  const cases: [string, Level | undefined][] = [
    // Laravel / Monolog
    ['[2026-11-19 10:00:01] production.ERROR: SQLSTATE[HY000] Connection refused {"exception":"[object] ..."}', 'error'],
    ['[2026-11-19 10:00:01] local.WARNING: Slow query', 'warn'],
    ['[2026-11-19T10:00:01.123456+00:00] app.INFO: User logged in', 'info'],
    ['[2026-11-19 10:00:01] production.DEBUG: cache hit', 'debug'],
    ['[2026-11-19 10:00:01] production.CRITICAL: down', 'error'],
    // Horizon / queue:work, new and old
    ['  2026-11-17 15:32:00 Modules\\Machinery\\Listeners\\HandleMaintenanceCompletion ...... 81.02ms FAIL', 'error'],
    ['  2026-11-17 15:32:00 App\\Jobs\\SendMail ............................. 12.10ms DONE', 'info'],
    ['  2026-11-17 15:32:00 App\\Jobs\\SendMail ............................. RUNNING', 'debug'],
    ['[2026-11-17 15:32:00][abc123] Failed:     App\\Jobs\\SendMail', 'error'],
    ['[2026-11-17 15:32:00][abc123] Processed:  App\\Jobs\\SendMail', 'info'],
    // JSON lines: pino, Monolog JSON, structured
    ['{"level":50,"time":1700000000,"msg":"boom"}', 'error'],
    ['{"level":30,"msg":"listening"}', 'info'],
    ['{"message":"x","level":400,"level_name":"ERROR"}', 'error'],
    ['{"ts":"2026-11-19T10:00:00Z","severity":"warning","msg":"retry"}', 'warn'],
    // logfmt / Go slog
    ['time=2026-11-19T10:00:00Z level=ERROR msg="dial failed"', 'error'],
    ['ts=1 lvl=warn msg=slow', 'warn'],
    // Python, Rails, klog, nginx, Celery
    ['ERROR:root:something broke', 'error'],
    ['2026-11-19 10:00:00,123 - app - WARNING - disk at 90%', 'warn'],
    ['E, [2026-11-19T10:00:00.000 #1234] ERROR -- : boom', 'error'],
    ['W1119 10:00:00.000000   1 reflector.go:1] watch closed', 'warn'],
    ['2026/11/19 10:00:00 [error] 12#12: *1 connect() failed', 'error'],
    ['[2026-11-19 10:00:00,000: INFO/MainProcess] Connected', 'info'],
    // Thrown, no level word
    ['Traceback (most recent call last):', 'error'],
    ['TypeError: Cannot read properties of undefined', 'error'],
    ['Illuminate\\Database\\QueryException: SQLSTATE', 'error'],
    ['panic: runtime error: index out of range', 'error'],
    // No level: plain output stays unmarked
    ['Server started on port 8000', undefined],
    ['no error found in 12 files', undefined],
    ['processing 4 items', undefined],
    ['{"msg":"no level here"}', undefined],
    ['', undefined],
  ]
  it.each(cases)('%s', (text, want) => {
    expect(levelOf(text)).toBe(want)
  })
})

describe('levels in a log', () => {
  const log = [
    '[2026-11-19 10:00:00] production.ERROR: boom {"exception":"x"}',
    '[stacktrace]',
    '#0 /app/vendor/Foo.php(12): bar()',
    '#1 {main}',
    '"} ',
    '[2026-11-19 10:00:01] production.INFO: fine',
    'plain line',
    '    indented, but the line above it has no level',
    'Exception in thread "main" java.lang.IllegalStateException',
    '\tat com.example.Main.main(Main.java:5)',
  ].join('\n') + '\n'
  const s = applyChunk(emptyLog(), { Text: log, Truncated: false, Skipped: false })

  it('gives stack trace lines the level of their entry', () => {
    expect(s.lines.map((l) => [l.level, !!l.more])).toEqual([
      ['error', false], ['error', true], ['error', true], ['error', true], ['error', true],
      ['info', false],
      [undefined, false], [undefined, false],
      ['error', false], ['error', true],
    ])
  })

  it('counts entries, not lines, and filters by level', () => {
    expect(levelCounts(s.lines)).toEqual({ error: 2, warn: 0, info: 1, debug: 0 })
    expect(onlyLevels(s.lines, new Set(['info'] as Level[])).map((l) => l.text)).toEqual(['[2026-11-19 10:00:01] production.INFO: fine'])
    expect(onlyLevels(s.lines, new Set())).toBe(s.lines)
  })

  it('marks the partial line too', () => {
    const p = applyChunk(emptyLog(), { Text: 'ok\nlevel=error msg="half', Truncated: false, Skipped: false })
    expect(shownLines(p).at(-1)?.level).toBe('error')
  })
})
