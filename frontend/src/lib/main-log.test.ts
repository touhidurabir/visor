import { describe, expect, it } from 'vitest'
import { DEFAULT_GROUPS, appendLines, footerNote, groupsParam, levelClass, levelsFor, logTime, parseGroups } from './main-log'

describe('level chips', () => {
  it('maps chips to supervisord levels', () => {
    expect(levelsFor(['errors'])).toEqual(['CRIT', 'ERRO'])
    expect(levelsFor(['debug', 'warnings'])).toEqual(['WARN', 'DEBG', 'TRAC', 'BLAT'])
    expect(levelsFor(DEFAULT_GROUPS)).toEqual(['CRIT', 'ERRO', 'WARN', 'INFO'])
    expect(levelsFor([])).toEqual([])
  })
  it('round-trips through the URL and falls back to the default', () => {
    expect(parseGroups(groupsParam(['info', 'errors']))).toEqual(['errors', 'info'])
    expect(parseGroups(undefined)).toEqual(DEFAULT_GROUPS)
    expect(parseGroups('nope')).toEqual(DEFAULT_GROUPS)
    expect(parseGroups(['errors'])).toEqual(DEFAULT_GROUPS)
  })
  it('colours levels', () => {
    expect(['CRIT', 'ERRO', 'WARN', 'INFO', 'DEBG', 'TRAC', 'BLAT'].map(levelClass)).toEqual(['err', 'err', 'warn', 'info', 'debug', 'debug', 'debug'])
  })
})

describe('wording', () => {
  it('formats the time in local time', () => {
    expect(logTime(new Date(2026, 10, 1, 14, 6, 9).getTime())).toBe('Nov 1, 14:06:09')
  })
  it('words the footer', () => {
    expect(footerNote(3, 10)).toBe('Showing 3 of 10 matching lines')
    expect(footerNote(1, 1)).toBe('Showing 1 line')
    expect(footerNote(0, 0)).toBe('Showing 0 lines')
  })
})

describe('appendLines', () => {
  const line = (n: number) => ({ AtMs: n, Level: 'INFO', Text: `line ${n}` })
  it('adds the new lines after the shown ones and counts them', () => {
    const got = appendLines({ lines: [line(1), line(2)], matched: 5 }, { Lines: [line(3)], Matched: 1 }, 10)
    expect(got.lines.map((l) => l.AtMs)).toEqual([1, 2, 3])
    expect(got.matched).toBe(6)
  })
  it('keeps only the newest limit lines', () => {
    const got = appendLines({ lines: [line(1), line(2), line(3)], matched: 3 }, { Lines: [line(4), line(5)], Matched: 2 }, 3)
    expect(got.lines.map((l) => l.AtMs)).toEqual([3, 4, 5])
    expect(got.matched).toBe(5)
  })
  it('treats no lines as nothing new', () => {
    const before = { lines: [line(1)], matched: 1 }
    expect(appendLines(before, { Lines: null, Matched: 0 }, 10)).toEqual(before)
  })
})
