import { describe, expect, it } from 'vitest'
import type { ExplainView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Check } from '../../bindings/github.com/touhidurabir/visor/internal/validate'
import { checkItems, fixItems, formReason, saveReason, thingsToCheck } from './fix-list'
import { fieldLabel } from './form-fields'

const p = (Level: string, Message: string, extra: object = {}) => ({ Level, Message, Line: 0, Live: false, Fix: '', Edit: null, ...extra })

/** A view with problems on lines (line → problems), plus file problems. */
function view(lines: Record<number, ReturnType<typeof p>[]>, file: ReturnType<typeof p>[] = [], saveErrors = 0): ExplainView {
  return {
    SaveErrors: saveErrors,
    FileProblems: file,
    Sections: [
      {
        Name: 'program:a',
        Program: 'a',
        Lines: Object.entries(lines).map(([n, ps]) => ({ Line: Number(n), Kind: 'key', Level: ps[0]?.Level ?? '', Problems: ps })),
      },
    ],
  } as unknown as ExplainView
}

describe('fixItems', () => {
  it('lists errors first, then warnings, each by line', () => {
    const v = view({ 3: [p('warning', '/srv/acme doesn’t exist yet.')], 15: [p('error', 'The log folder /var/log/acme doesn’t exist.')], 7: [p('error', 'Bad number.')] })
    expect(fixItems(v)).toEqual([
      { line: 7, level: 'error', text: 'Bad number.' },
      { line: 15, level: 'error', text: 'The log folder /var/log/acme doesn’t exist.' },
      { line: 3, level: 'warning', text: '/srv/acme doesn’t exist yet.' },
    ])
  })
  it('leaves out what supervisord says about the running program', () => {
    expect(fixItems(view({ 2: [p('error', 'It exited.', { Live: true })] }))).toEqual([])
  })
  it('keeps file problems, with line 0 when they are about the whole file', () => {
    expect(fixItems(view({}, [p('error', 'Wrong extension.')]))).toEqual([{ line: 0, level: 'error', text: 'Wrong extension.' }])
  })
  it('has only the parse error when the text is not a config', () => {
    expect(fixItems({ ParseError: p('error', 'No section.', { Line: 4 }) } as unknown as ExplainView)).toEqual([{ line: 4, level: 'error', text: 'No section.' }])
    expect(fixItems(null)).toEqual([])
  })
})

describe('saveReason', () => {
  it('names the lines that block saving', () => {
    expect(saveReason(view({ 22: [p('error', 'x')] }))).toBe('Fix line 22 to save')
    expect(saveReason(view({ 3: [p('error', 'x'), p('error', 'y')], 15: [p('error', 'z')] }))).toBe('Fix lines 3 and 15 to save')
    expect(saveReason(view({ 3: [p('error', 'x')], 7: [p('error', 'y')], 15: [p('error', 'z')] }))).toBe('Fix lines 3, 7 and 15 to save')
    expect(saveReason(view({ 1: [p('error', 'a')], 3: [p('error', 'x')], 7: [p('error', 'y')], 15: [p('error', 'z')] }))).toBe('Fix 4 lines to save')
  })
  it('counts problems when one is about the whole file, and ignores warnings', () => {
    expect(saveReason(view({ 3: [p('error', 'x')] }, [p('error', 'Wrong extension.')]))).toBe('Fix 2 problems to save')
    expect(saveReason(view({ 3: [p('warning', 'x')] }))).toBe('')
    expect(saveReason(view({}, [], 1))).toBe('Fix 1 problem to save')
  })
})

describe('the form’s things to check', () => {
  const c = (Level: string, Key: string, Message: string, Program = 'a', Line = 0) => ({ Level, Key, Message, Program, Line, Fix: '', Edit: null }) as unknown as Check
  const checks = [
    c('ok', 'command', 'php found'),
    c('warning', 'directory', '/srv/acme doesn’t exist yet'),
    c('error', 'stdout_logfile', 'The log folder doesn’t exist'),
    c('error', '', 'acme already exists in other.ini'),
    c('error', 'enviroment', 'Unknown setting', 'a', 9),
    c('error', '', 'Wrong extension', ''),
  ]
  it('puts each under its field, errors first, without the ok ones', () => {
    expect(checkItems(checks, fieldLabel)).toEqual([
      { key: 'stdout_logfile', label: 'Output log', level: 'error', text: 'The log folder doesn’t exist' },
      { key: 'name', label: 'Program name', level: 'error', text: 'acme already exists in other.ini' },
      { key: '', label: 'Line 9', level: 'error', text: 'Unknown setting' },
      { key: '', label: 'The file', level: 'error', text: 'Wrong extension' },
      { key: 'directory', label: 'Working folder', level: 'warning', text: '/srv/acme doesn’t exist yet' },
    ])
    expect(thingsToCheck(1)).toBe('1 thing to check')
    expect(thingsToCheck(5)).toBe('5 things to check')
  })
  it('says which fields to fix', () => {
    const items = checkItems(checks, fieldLabel)
    expect(formReason(items.slice(0, 1))).toBe('Fix “Output log” to save')
    expect(formReason(items.slice(0, 2))).toBe('Fix “Output log” and “Program name” to save')
    expect(formReason(items)).toBe('Fix 4 settings to save')
    expect(formReason(items.slice(4))).toBe('')
  })
})
