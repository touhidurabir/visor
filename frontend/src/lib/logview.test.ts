import { describe, expect, it } from 'vitest'
import { applyChunk, countMatches, emptyLog, highlight, markedLines, noLogMessage, shownLines } from './logview'

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
