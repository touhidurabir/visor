import { describe, expect, it } from 'vitest'
import { createDraft, defaultFileName, firstProgramName, type Codec } from './draft'

// A stand-in for the Go codec: same format, FormKeys order not needed here.
const codec: Codec = {
  async render(name, values) {
    return `[program:${name}]\n` + Object.entries(values).filter(([, v]) => v).map(([k, v]) => `${k}=${v}\n`).join('')
  },
  async parse(content) {
    const m = /^\[program:(.+)\]$/m.exec(content)
    if (!m) throw new Error('there is no [program:name] section')
    const values: Record<string, string> = {}
    for (const line of content.split('\n').slice(1)) {
      const i = line.indexOf('=')
      if (i > 0) values[line.slice(0, i)] = line.slice(i + 1)
    }
    return { Name: m[1], Values: values }
  },
}

const tpl = { ID: 'script', Label: 'Any script', Values: { name: 'acme-task', command: 'run.sh', directory: '/srv' } }

describe('draft', () => {
  it('starts from a template and names the file after the program', async () => {
    const d = createDraft(codec)
    d.setExt('.conf')
    await d.applyTemplate(tpl)
    expect(d.state.name).toBe('acme-task')
    expect(d.state.values).toEqual({ command: 'run.sh', directory: '/srv' })
    expect(d.state.content).toBe('[program:acme-task]\ncommand=run.sh\ndirectory=/srv\n')
    expect(d.state.fileName).toBe('acme-task.conf')
  })

  it('form -> raw -> form keeps the values', async () => {
    const d = createDraft(codec)
    await d.applyTemplate(tpl)
    await d.setField('command', 'sleep 60')
    await d.setField('name', 'napper')
    const text = d.state.content // what "Write it" shows
    expect(text).toContain('command=sleep 60')
    d.setContent(text + 'stopwaitsecs=5\n') // a hand edit
    expect(await d.toForm()).toBeNull()
    expect(d.state.name).toBe('napper')
    expect(d.state.values).toEqual({ command: 'sleep 60', directory: '/srv', stopwaitsecs: '5' })
    await d.setField('directory', '/tmp')
    expect(d.state.content).toBe('[program:napper]\ncommand=sleep 60\ndirectory=/tmp\nstopwaitsecs=5\n')
  })

  it('raw edits keep the text exactly and follow the program name', () => {
    const d = createDraft(codec)
    d.setContent('; mine\n[program:hand]\ncommand=x\n')
    expect(d.state.content).toBe('; mine\n[program:hand]\ncommand=x\n')
    expect(d.state.fileName).toBe('hand.ini')
  })

  it('stops following the program name once a file name is typed', async () => {
    const d = createDraft(codec)
    await d.applyTemplate(tpl)
    d.setFileName('mine.ini')
    await d.setField('name', 'other')
    expect(d.state.fileName).toBe('mine.ini')
    d.setFileName('')
    expect(d.state.fileName).toBe('other.ini')
  })

  it('keeps the form when the text cannot be read as a program', async () => {
    const d = createDraft(codec)
    await d.applyTemplate(tpl)
    d.setContent('nothing here')
    expect(await d.toForm()).toMatch(/no \[program:name\]/)
    expect(d.state.name).toBe('acme-task')
    expect(d.state.content).toBe('nothing here')
  })

  it('does not let a slow render overwrite a hand edit', async () => {
    let release: () => void = () => {}
    const slow: Codec = { ...codec, render: (n, v) => new Promise((r) => (release = () => r(`rendered ${n} ${Object.keys(v)}`))) }
    const d = createDraft(slow)
    const p = d.setField('command', 'x')
    d.setContent('typed since')
    release()
    await p
    expect(d.state.content).toBe('typed since')
  })

  it('reset forgets everything', async () => {
    const d = createDraft(codec)
    await d.applyTemplate(tpl)
    d.reset()
    expect(d.state.content).toBe('')
    expect(d.state.values).toEqual({})
  })
})

describe('names', () => {
  it('finds the first program', () => {
    expect(firstProgramName('[supervisord]\n[program:a b]\n[program:c]')).toBe('a b')
    expect(firstProgramName('nope')).toBe('')
  })
  it('defaults the file name', () => {
    expect(defaultFileName('web', '.ini')).toBe('web.ini')
    expect(defaultFileName('  ', '.conf')).toBe('new.conf')
  })
})
