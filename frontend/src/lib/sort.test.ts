import { describe, expect, it } from 'vitest'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { failingFirst, sortFailingFirst } from './sort'

const p = (FullName: string, Status: string, State = 'RUNNING') => ({ FullName, Name: FullName, Group: FullName, Status, State }) as ProcessView
const names = (ps: ProcessView[] | null | undefined) => (ps ?? []).map((x) => x.FullName)

describe('failingFirst', () => {
  it('moves failing processes to the top', () => {
    const out = failingFirst([p('export', 'stopped', 'STOPPED'), p('sync', 'failing', 'FATAL')])
    expect(names(out)).toEqual(['sync', 'export'])
  })

  it('keeps the healthy rows, and the failing ones, in their original order', () => {
    const procs = [
      p('a', 'running'),
      p('b', 'failing', 'BACKOFF'),
      p('c', 'stopped', 'STOPPED'),
      p('d', 'other', 'STARTING'),
      p('e', 'failing', 'EXITED'),
      p('f', 'running'),
    ]
    expect(names(failingFirst(procs))).toEqual(['b', 'e', 'a', 'c', 'd', 'f'])
  })

  it('uses the Go status, not the state name', () => {
    // An EXITED with an expected status is stopped, not failing.
    const out = failingFirst([p('a', 'running'), p('done', 'stopped', 'EXITED')])
    expect(names(out)).toEqual(['a', 'done'])
  })

  it('does not change its input', () => {
    const procs = [p('a', 'running'), p('b', 'failing', 'FATAL')]
    failingFirst(procs)
    expect(names(procs)).toEqual(['a', 'b'])
  })
})

describe('sortFailingFirst', () => {
  it('sorts within each config and keeps the configs in order', () => {
    const configs = [
      { FileName: 'reports.ini', Processes: [p('export', 'stopped', 'STOPPED'), p('sync', 'failing', 'FATAL')] },
      { FileName: 'shop.ini', Processes: [p('q0', 'running'), p('q1', 'running')] },
    ] as ConfigView[]
    const out = sortFailingFirst(configs)
    expect(out.map((c) => c.FileName)).toEqual(['reports.ini', 'shop.ini'])
    expect(names(out[0].Processes)).toEqual(['sync', 'export'])
    // A config with nothing failing is passed through as it is.
    expect(out[1]).toBe(configs[1])
  })

  it('handles a config with no processes', () => {
    const empty = { FileName: 'e.ini', Processes: null } as ConfigView
    expect(sortFailingFirst([empty])).toEqual([empty])
  })
})
