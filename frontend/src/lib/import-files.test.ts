import { describe, expect, it } from 'vitest'
import type { ExplainView, ImportResultView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { addLabel, programsOf, resultLine, targetName } from './import-files'


describe('targetName', () => {
  it('keeps a name the include setting loads', () => {
    expect(targetName('horizon.ini', '.ini')).toBe('horizon.ini')
    expect(targetName('Horizon.INI', '.ini')).toBe('Horizon.ini')
  })
  it('swaps a config extension for the include setting’s', () => {
    expect(targetName('scheduler.conf', '.ini')).toBe('scheduler.ini')
    expect(targetName('worker.txt', '.conf')).toBe('worker.conf')
  })
  it('keeps every name for an include setting with no extension (files = *)', () => {
    expect(targetName('worker.conf', '')).toBe('worker.conf')
  })
  it('adds the extension to a name without a config one', () => {
    expect(targetName('worker', '.ini')).toBe('worker.ini')
    expect(targetName('worker.conf.example', '.ini')).toBe('worker.conf.example.ini')
  })
})

describe('programsOf', () => {
  it('lists the program sections only', () => {
    const v = { Sections: [{ Program: 'a' }, { Program: '' }, { Program: 'b' }] } as unknown as ExplainView
    expect(programsOf(v)).toEqual(['a', 'b'])
    expect(programsOf(null)).toEqual([])
  })
})

describe('addLabel', () => {
  it('counts configs', () => {
    expect(addLabel(1)).toBe('Add 1 config')
    expect(addLabel(3)).toBe('Add 3 configs')
  })
})

describe('resultLine', () => {
  const r = (o: Partial<ImportResultView>) => ({ FileName: 'a.ini', Path: '/x/a.ini', Result: { Added: ['a'], Changed: [], Removed: [], Errors: [] }, Error: '', Started: false, StartError: '', ...o }) as ImportResultView
  it('says what supervisord did, and that it started', () => {
    expect(resultLine(r({}))).toEqual({ ok: true, text: 'supervisord added a.' })
    expect(resultLine(r({ Started: true }))).toEqual({ ok: true, text: 'supervisord added a and started it.' })
  })
  it('reports refusals, apply problems and failed starts', () => {
    expect(resultLine(r({ Error: 'refused' }))).toEqual({ ok: false, text: 'refused' })
    expect(resultLine(r({ Error: 'applying it failed', Written: true }))).toEqual({ ok: false, text: 'Saved, but: applying it failed' })
    expect(resultLine(r({ Result: { Added: [], Changed: [], Removed: [], Errors: ['boom'] } as never }))).toEqual({ ok: false, text: 'Saved, but supervisord reported: boom' })
    expect(resultLine(r({ StartError: 'no such file' })).text).toBe('supervisord added a, but starting failed: no such file')
  })
})
