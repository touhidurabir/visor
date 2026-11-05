import { describe, expect, it } from 'vitest'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { connectionProblem } from './connection'
import { NEEDS_ATTENTION, actionTargets, downLine, foldLabel, footerReason, itemActions, itemLook, rowLook, rowName, stateLine, trayActions, trayGroups, trayHeader } from './tray'

const p = (Name: string, Status: string, State = 'RUNNING', Since = '') => ({ FullName: `${Name}:${Name}`, Name, Group: Name, Status, State, Since }) as ProcessView
const cfg = (FileName: string, ...Processes: ProcessView[]) => ({ FileName, Path: `/c/${FileName}`, Processes }) as ConfigView
const shape = (configs: ConfigView[]) => trayGroups(configs).map((g) => [g.name, g.items.map((x) => x.name), g.moved])

describe('rowName', () => {
  it('tells two failing processes of the same name apart under Needs attention', () => {
    const inGroup = (Group: string, Name: string) => ({ FullName: `${Group}:${Name}`, Name, Group, Status: 'failing', State: 'FATAL', Since: '' }) as ProcessView
    const groups = trayGroups([cfg('shop.ini', inGroup('shop', 'worker')), cfg('mail.ini', inGroup('mail', 'worker'))])
    expect(groups[0].name).toBe(NEEDS_ATTENTION)
    expect(groups[0].items.map((x) => x.name)).toEqual(['shop:worker', 'mail:worker'])
    expect(groups[0].items.map((x) => rowName(groups[0], x.procs[0]))).toEqual(['shop:worker', 'mail:worker'])
  })
  it('is the plain name when the group is the program, and under a config', () => {
    const groups = trayGroups([cfg('a.ini', p('flaky', 'failing', 'EXITED'), p('ok', 'running'))])
    expect(rowName(groups[0], groups[0].items[0].procs[0])).toBe('flaky')
    expect(groups[0].items[0].name).toBe('flaky')
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

describe('stateLine and rowLook', () => {
  it('says the state, then Go’s time', () => {
    expect(rowLook(p('a', 'running', 'RUNNING', 'up 4s'))).toEqual({ tone: 'ok', line: 'Running · up 4s', lineClass: 'meta', failing: false })
    expect(rowLook(p('b', 'failing', 'FATAL', 'failing 3m'))).toEqual({ tone: 'bad', line: 'Fatal · failing 3m', lineClass: 't-bad', failing: true })
    expect(rowLook(p('c', 'failing', 'BACKOFF', 'failing 40s')).line).toBe('Backoff · failing 40s')
  })

  it('does not say stopped twice', () => {
    expect(stateLine({ State: 'STOPPED', Since: 'stopped 4m' })).toBe('Stopped · 4m')
    expect(stateLine({ State: 'EXITED', Since: 'stopped 5d' })).toBe('Exited · 5d')
  })

  it('is the state alone when there is no time', () => {
    expect(rowLook(p('a', 'stopped', 'EXITED')).line).toBe('Exited')
    expect(rowLook(p('b', 'other', 'STARTING'))).toEqual({ tone: 'warn', line: 'Starting', lineClass: 't-warn', failing: false })
    expect(rowLook(p('c', 'failing', 'FATAL')).line).toBe('Fatal')
  })

  it('makes a retrying process red, as it is failing', () => {
    expect(rowLook(p('a', 'failing', 'BACKOFF', 'failing 1m'))).toMatchObject({ tone: 'bad', lineClass: 't-bad', failing: true })
  })
})

// A copy of a numprocs program: Program is the [program:x] name.
const copy = (Program: string, n: number, Status: string, State = 'RUNNING', Since = '') =>
  ({ FullName: `${Program}:${Program}_0${n}`, Name: `${Program}_0${n}`, Group: Program, Program, Status, State, Since }) as ProcessView

describe('copies', () => {
  const worker = [copy('mailer-worker', 0, 'running'), copy('mailer-worker', 1, 'running')]
  const queue = [copy('shop-queue', 0, 'running'), copy('shop-queue', 1, 'running'), copy('shop-queue', 2, 'failing', 'BACKOFF', 'failing 40s'), copy('shop-queue', 3, 'running')]
  const digest = { ...p('mailer-digest', 'failing', 'FATAL', 'failing 3m'), Program: 'mailer-digest' } as ProcessView

  it('folds a program’s copies into one row where the first copy is', () => {
    const one = { ...p('shop-scheduler', 'running'), Program: 'shop-scheduler' } as ProcessView
    const groups = trayGroups([cfg('mailer.ini', ...worker, digest), cfg('shop-api.ini', queue[0], queue[1], one, queue[2], queue[3])])
    expect(groups.map((g) => [g.name, g.items.map((i) => [i.name, i.multi, i.procs.length]), g.moved])).toEqual([
      [NEEDS_ATTENTION, [['mailer-digest', false, 1], ['shop-queue:shop-queue_02', false, 1]], 0],
      ['mailer.ini', [['mailer-worker', true, 2]], 1],
      ['shop-api.ini', [['shop-queue', true, 4], ['shop-scheduler', false, 1]], 0],
    ])
  })

  it('keeps a failing copy in its row and also lists it under Needs attention', () => {
    const [attention, shop] = trayGroups([cfg('shop-api.ini', ...queue)])
    // Named group:name there, as any process whose group differs.
    expect(attention.items.map((i) => i.name)).toEqual(['shop-queue:shop-queue_02'])
    expect(shop.items[0].procs.map((x) => x.Name)).toContain('shop-queue_02')
    expect(shop.moved).toBe(0)
  })

  it('a program with one process is a plain row', () => {
    const solo = { ...p('flaky', 'running'), Program: 'flaky' } as ProcessView
    expect(trayGroups([cfg('flaky.ini', solo)])[0].items).toEqual([{ key: 'flaky:flaky', name: 'flaky', procs: [solo], multi: false }])
  })

  it('rolls the copies up into counts, red when one fails', () => {
    const multi = (procs: ProcessView[]) => ({ procs, multi: true })
    expect(itemLook(multi(worker))).toEqual({ tone: 'ok', line: '2 of 2 running', lineClass: 'meta', failing: false })
    expect(itemLook(multi(queue))).toEqual({ tone: 'bad', line: '3 of 4 running · 1 failing', lineClass: 't-bad', failing: false })
    expect(itemLook(multi([copy('w', 0, 'stopped', 'STOPPED'), copy('w', 1, 'stopped', 'STOPPED')]))).toMatchObject({ tone: 'idle', line: '0 of 2 running' })
    // One process reads as itself.
    expect(itemLook({ procs: [digest], multi: false }).line).toBe('Fatal · failing 3m')
  })

  it('offers what applies to the copies, and acts on every copy it applies to', () => {
    const mixed = { procs: [copy('w', 0, 'running'), copy('w', 1, 'stopped', 'STOPPED'), copy('w', 2, 'other', 'STOPPING')], multi: true }
    expect(itemActions({ procs: worker })).toEqual(['stop', 'restart'])
    expect(itemActions({ procs: queue })).toEqual(['stop', 'restart'])
    expect(itemActions(mixed)).toEqual(['stop', 'restart'])
    expect(itemActions({ procs: [copy('w', 0, 'stopped', 'STOPPED'), copy('w', 1, 'failing', 'FATAL')] })).toEqual(['start'])
    expect(itemActions({ procs: [copy('w', 0, 'other', 'STOPPING')] })).toEqual([])

    const names = (xs: ProcessView[]) => xs.map((x) => x.Name)
    expect(names(actionTargets('restart', { procs: worker, multi: true }))).toEqual(['mailer-worker_00', 'mailer-worker_01'])
    expect(names(actionTargets('stop', mixed))).toEqual(['w_00'])
    expect(names(actionTargets('start', mixed))).toEqual(['w_01'])
    // Restart brings the stopped copy up too, and leaves the stopping one be.
    expect(names(actionTargets('restart', mixed))).toEqual(['w_00', 'w_01'])
    // One process: always itself.
    expect(actionTargets('start', { procs: [digest], multi: false })).toEqual([digest])
  })

  it('names the fold button by what it does', () => {
    expect(foldLabel({ name: 'mailer-worker', procs: worker }, false)).toBe('Show the 2 copies of mailer-worker')
    expect(foldLabel({ name: 'mailer-worker', procs: worker }, true)).toBe('Hide the 2 copies of mailer-worker')
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
