import { describe, expect, it } from 'vitest'
import type { Check } from '../../bindings/github.com/touhidurabir/visor/internal/validate'
import { fieldNotes, fileChecks, FORM_SECTIONS, nameProblem } from './form-fields'
import { firstProgramName } from './draft'

const c = (Level: 'ok' | 'warning' | 'error', Key: string, Message: string, Program = 'p', Line = 0) => ({ Level, Key, Message, Program, Line }) as Check

describe('fieldNotes', () => {
  it('puts each check on its field and the worst one wins', () => {
    const n = fieldNotes([c('ok', 'directory', 'ok dir'), c('error', 'directory', 'bad dir'), c('ok', 'command', 'found')])
    expect(n.directory).toEqual({ level: 'error', text: 'bad dir' })
    expect(n.command.level).toBe('ok')
  })

  it('treats a program-level check as about the name', () => {
    expect(fieldNotes([c('error', '', '[program:p] already exists in a.ini')]).name.level).toBe('error')
  })

  it('shows the process_num error on the copy name too', () => {
    const n = fieldNotes([c('error', 'numprocs', 'numprocs=3 needs %(process_num) in process_name')])
    expect(n.numprocs.level).toBe('error')
    expect(n.process_name.level).toBe('error')
  })
  it('shows the process_num error on both fields when Go puts it on process_name', () => {
    const n = fieldNotes([c('error', 'process_name', 'numprocs=3 needs %(process_num) in process_name')])
    expect(n.numprocs?.level).toBe('error')
    expect(n.process_name?.level).toBe('error')
  })

  it('leaves file-level checks out', () => {
    const file = c('error', '', 'x.conf does not match the include setting', '')
    expect(fieldNotes([file])).toEqual({})
    expect(fileChecks([file, c('ok', 'command', 'found')])).toEqual([file])
  })
})

describe('form', () => {
  it('has a field for every setting Go renders, once', () => {
    const keys = FORM_SECTIONS.flatMap((s) => s.fields.map((f) => f.key))
    expect(new Set(keys).size).toBe(keys.length)
    expect(keys.filter((k) => k !== 'name')).toHaveLength(18)
  })
  it('checks the program name', () => {
    expect(nameProblem('')).toBe('Required')
    expect(nameProblem('a b')).toMatch(/letters/)
    expect(nameProblem('acme-task_1.x')).toBeNull()
    expect(firstProgramName('[program:x]')).toBe('x')
  })
})
