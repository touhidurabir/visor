import { describe, expect, it } from 'vitest'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { connectionProblem } from './connection'
import { NEEDS_ATTENTION, downLine, footerReason, rowLook, rowName, trayActions, trayGroups, trayHeader } from './tray'

const p = (Name: string, Status: string, State = 'RUNNING', Since = '') => ({ FullName: `${Name}:${Name}`, Name, Group: Name, Status, State, Since }) as ProcessView
const cfg = (FileName: string, ...Processes: ProcessView[]) => ({ FileName, Path: `/c/${FileName}`, Processes }) as ConfigView
const shape = (configs: ConfigView[]) => trayGroups(configs).map((g) => [g.name, g.processes.map((x) => x.Name), g.moved])

describe('rowName', () => {
  it('tells two failing processes of the same name apart under Needs attention', () => {
    const inGroup = (Group: string, Name: string) => ({ FullName: `${Group}:${Name}`, Name, Group, Status: 'failing', State: 'FATAL', Since: '' }) as ProcessView
    const groups = trayGroups([cfg('shop.ini', inGroup('shop', 'worker')), cfg('mail.ini', inGroup('mail', 'worker'))])
    expect(groups[0].name).toBe(NEEDS_ATTENTION)
    expect(groups[0].processes.map((x) => rowName(groups[0], x))).toEqual(['shop:worker', 'mail:worker'])
  })
  it('is the plain name when the group is the program, and under a config', () => {
    const groups = trayGroups([cfg('a.ini', p('flaky', 'failing', 'EXITED'), p('ok', 'running'))])
    expect(rowName(groups[0], groups[0].processes[0])).toBe('flaky')
    const under = { config: cfg('m.ini') }
    expect(rowName(under, { FullName: 'mail:worker', Name: 'worker', Group: 'mail' } as ProcessView)).toBe('worker')
  })
})

describe('trayGroups', () => {
  it('puts the failing processes first, in Needs attention, and the rest by config', () => {
    const configs = [
      cfg('mailer.ini', p('worker', 'running'), p('digest', 'failing', 'FATAL')),
      cfg('notes.ini', p('indexer', 'stopped', 'EXITED')),
      cfg('reports.ini', p('export', 'stopped', 'STOPPED'), p('sync', 'failing', 'BACKOFF')),
    ]
    expect(shape(configs)).toEqual([
      [NEEDS_ATTENTION, ['digest', 'sync'], 0],
      ['mailer.ini', ['worker'], 1],
      ['notes.ini', ['indexer'], 0],
      ['reports.ini', ['export'], 1],
    ])
    expect(trayGroups(configs)[0].config).toBeNull()
    expect(trayGroups(configs)[1].config?.FileName).toBe('mailer.ini')
  })

  it('has no Needs attention group when nothing fails', () => {
    expect(shape([cfg('a.ini', p('a', 'running')), cfg('b.ini', p('b', 'other', 'STARTING'))])).toEqual([
      ['a.ini', ['a'], 0],
      ['b.ini', ['b'], 0],
    ])
  })

  it('keeps a config whose processes all fail, with no rows, so its Watch switch stays', () => {
    expect(shape([cfg('flaky.ini', p('flaky', 'failing', 'EXITED'))])).toEqual([
      [NEEDS_ATTENTION, ['flaky'], 0],
      ['flaky.ini', [], 1],
    ])
  })

  it('leaves out configs supervisord runs nothing from', () => {
    expect(shape([cfg('empty.ini'), { FileName: 'null.ini', Path: '', Processes: null } as ConfigView, cfg('a.ini', p('a', 'running'))])).toEqual([['a.ini', ['a'], 0]])
  })

  it('uses the Go status, not the state name: an expected exit is not failing', () => {
    expect(shape([cfg('n.ini', p('done', 'stopped', 'EXITED'))])).toEqual([['n.ini', ['done'], 0]])
  })
})

describe('trayActions', () => {
  it('gives a running process Stop and Restart, and no Start', () => {
    expect(trayActions('RUNNING')).toEqual(['stop', 'restart'])
    expect(trayActions('STARTING')).toEqual(['stop', 'restart'])
    // supervisord is still retrying it: Start would say "already started".
    expect(trayActions('BACKOFF')).toEqual(['stop', 'restart'])
  })

  it('gives a stopped or failed process Start only', () => {
    for (const s of ['STOPPED', 'EXITED', 'FATAL']) expect(trayActions(s)).toEqual(['start'])
  })

  it('gives nothing while it is stopping, or in a state Visor does not know', () => {
    expect(trayActions('STOPPING')).toEqual([])
    expect(trayActions('UNKNOWN')).toEqual([])
  })
})

describe('rowLook', () => {
  it('shows Go’s time, which says what it means', () => {
    expect(rowLook(p('a', 'running', 'RUNNING', 'up 4s'))).toEqual({ tone: 'ok', time: 'up 4s', timeClass: 'meta', failing: false })
    expect(rowLook(p('b', 'failing', 'FATAL', 'failing 3m'))).toEqual({ tone: 'bad', time: 'failing 3m', timeClass: 't-bad', failing: true })
    expect(rowLook(p('c', 'stopped', 'STOPPED', 'stopped 5d')).time).toBe('stopped 5d')
  })

  it('falls back to the state when there is no time', () => {
    expect(rowLook(p('a', 'stopped', 'EXITED')).time).toBe('exited')
    expect(rowLook(p('b', 'other', 'STARTING'))).toEqual({ tone: 'warn', time: 'starting', timeClass: 't-warn', failing: false })
    expect(rowLook(p('c', 'failing', 'FATAL')).time).toBe('failing')
  })

  it('makes a retrying process red, as it is failing', () => {
    expect(rowLook(p('a', 'failing', 'BACKOFF', 'failing 1m'))).toMatchObject({ tone: 'bad', timeClass: 't-bad' })
  })
})

const down = connectionProblem({ Reachable: false, Health: 'error', Error: 'cannot reach supervisord: supervisord: unreachable: dial unix /tmp/s.sock: connect: no such file or directory' })!
const auth = connectionProblem({ Reachable: false, Health: 'error', Error: 'cannot reach supervisord: supervisord: unauthorized' })!

describe('trayHeader', () => {
  const stats = { Running: 8, Stopped: 2, Failing: 2 }
  it('counts and says how long supervisord has been up', () => {
    expect(trayHeader({ Health: 'error', UptimeMs: 72 * 60_000 }, stats, null)).toEqual({ tone: 'bad', badge: '2 failing', meta: '8 running · 2 stopped · up 1h 12m' })
  })

  it('has no badge when nothing fails', () => {
    expect(trayHeader({ Health: 'ok', UptimeMs: 0 }, { ...stats, Failing: 0 }, null)).toEqual({ tone: 'ok', badge: null, meta: '8 running · 2 stopped' })
  })

  it('says supervisord is not running only when that is the problem', () => {
    expect(trayHeader({ Health: 'error', UptimeMs: 0 }, stats, down)).toEqual({ tone: 'bad', badge: 'Not running', meta: 'Can’t reach supervisord' })
    expect(trayHeader({ Health: 'error', UptimeMs: 0 }, stats, auth)).toEqual({ tone: 'bad', badge: 'Not connected', meta: 'supervisord turned down Visor’s password' })
  })

  it('is quiet before the first overview', () => {
    expect(trayHeader(null, null, null)).toEqual({ tone: 'idle', badge: null, meta: '' })
  })
})

describe('down card', () => {
  it('keeps the socket out of the line when supervisord is not running', () => {
    expect(down.title).toBe('supervisord isn’t running')
    expect(downLine(down)).toBe('Visor can’t show or control processes until it’s back. Doctor walks you through starting it.')
    expect(footerReason(down)).toBe('These need supervisord running.')
  })

  it('never says "isn’t running" for a turned-down password', () => {
    expect(downLine(auth)).toContain('turned down the username and password')
    expect(downLine(auth)).not.toContain('isn’t running')
    expect(footerReason(auth)).toBe('These need a connection to supervisord.')
  })
})
