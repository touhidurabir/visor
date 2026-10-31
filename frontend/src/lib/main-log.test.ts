import { describe, expect, it } from 'vitest'
import { DEFAULT_GROUPS, footerNote, groupsParam, levelClass, levelsFor, logTime, parseGroups } from './main-log'

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
