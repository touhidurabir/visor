import { describe, expect, it } from 'vitest'
import type { ExplainView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { addLines, applyEdit, explainMarks, fileSize, fixToSave } from './explain'

const text = '[program:a]\ncommand=x\nnumprocs=3\nprocess_name=%(program_name)s\n'

describe('applyEdit', () => {
  it('replaces a line', () => {
    expect(applyEdit(text, { Line: 4, Insert: false, Rename: '', Text: 'process_name=%(program_name)s_%(process_num)02d' })).toBe(
      '[program:a]\ncommand=x\nnumprocs=3\nprocess_name=%(program_name)s_%(process_num)02d\n',
    )
  })
  it('inserts after a line', () => {
    expect(applyEdit(text, { Line: 3, Insert: true, Rename: '', Text: 'autostart=true' })).toBe(
      '[program:a]\ncommand=x\nnumprocs=3\nautostart=true\nprocess_name=%(program_name)s\n',
    )
  })
  it('renames only the key, keeping the value, comment and continuation lines', () => {
    const typo = '[program:a]\nautostrat = true ; start it\nenviroment=A="1",\n  B="2"\n'
    const once = applyEdit(typo, { Line: 2, Insert: false, Text: 'autostart=true', Rename: 'autostart' })
    expect(applyEdit(once, { Line: 3, Insert: false, Text: 'environment=A="1", …', Rename: 'environment' })).toBe(
      '[program:a]\nautostart = true ; start it\nenvironment=A="1",\n  B="2"\n',
    )
  })
  it('leaves a rename alone when the line has no key', () => {
    expect(applyEdit(text, { Line: 1, Insert: false, Text: 'x=1', Rename: 'x' })).toBe(text)
  })
  it('leaves the text alone for a line that is not there', () => {
    expect(applyEdit(text, { Line: 40, Insert: false, Rename: '', Text: 'x' })).toBe(text)
  })
})

describe('addLines', () => {
  it('adds lines after a line, in order', () => {
    expect(addLines('[program:a]\ncommand=x', 2, ['stdout_logfile=/l/a.log', 'stderr_logfile=/l/a.err.log'])).toBe(
      '[program:a]\ncommand=x\nstdout_logfile=/l/a.log\nstderr_logfile=/l/a.err.log',
    )
  })
})

describe('explainMarks', () => {
  it('marks problem lines, not unset rows', () => {
    const v = {
      Sections: [
        {
          Name: 'program:a',
          Program: 'a',
          Lines: [
            { Line: 1, Kind: 'section', Level: '' },
            { Line: 2, Kind: 'key', Level: 'error' },
            { Line: 3, Kind: 'key', Level: 'warning' },
            { Line: 3, Kind: 'unset', Level: 'suggestion' },
          ],
        },
      ],
      FileProblems: [{ Line: 0, Level: 'error' }],
    } as unknown as ExplainView
    expect(explainMarks(v)).toEqual([
      { line: 2, level: 'error' },
      { line: 3, level: 'warning' },
    ])
  })
  it('marks the parse error line', () => {
    expect(explainMarks({ ParseError: { Line: 4, Level: 'error' } } as unknown as ExplainView)).toEqual([{ line: 4, level: 'error' }])
  })
})

describe('fileSize and fixToSave', () => {
  it('reads like a person would say it', () => {
    expect(fileSize(-1)).toBe('')
    expect(fileSize(1)).toBe('1 byte')
    expect(fileSize(512)).toBe('512 bytes')
    expect(fileSize(2_516_582)).toBe('2.4 MB')
    expect(fileSize(50 * 1024 * 1024)).toBe('50 MB')
    expect(fileSize(1024 * 1024 - 1)).toBe('1.0 MB')
    expect(fileSize(9.97 * 1024)).toBe('10 KB')
    expect(fixToSave(1)).toBe('Fix 1 line to save')
    expect(fixToSave(3)).toBe('Fix 3 lines to save')
    expect(fixToSave(1, false)).toBe('Fix 1 problem to save')
  })
})
