import { describe, expect, it } from 'vitest'
import type { ConfigView, ProcessView, StatsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { disconnectedHealth, firstFailing, health, healthSentence, NO_COUNT, openIssuesLabel, processLabel, reasonClass, summaryParts } from './health'

const stats = (s: Partial<StatsView>) => ({ Configs: 5, Processes: 12, Running: 0, Failing: 0, Stopped: 0, OpenIssues: 0, ...s }) as StatsView
const p = (FullName: string, Status: string, Group = FullName) => ({ FullName, Name: FullName.split(':').pop()!, Group, Status }) as ProcessView
const cfg = (FileName: string, procs: ProcessView[]) => ({ FileName, Processes: procs }) as ConfigView
const text = (h: ReturnType<typeof health>) => h.detail.map((s) => s.text).join('')

describe('healthSentence', () => {
  it.each([
    [{ Failing: 2, Running: 8, Stopped: 2 }, '2 processes are failing'],
    [{ Failing: 1, Running: 11 }, '1 process is failing'],
    [{ Running: 12 }, 'All 12 processes are running'],
    [{ Processes: 1, Running: 1 }, '1 process is running'],
    [{ Stopped: 12 }, 'All 12 processes are stopped'],
    [{ Processes: 1, Stopped: 1 }, '1 process is stopped'],
    [{ Running: 10, Stopped: 2 }, '10 of 12 processes are running'],
    [{ Running: 0, Stopped: 10 }, '0 of 12 processes are running'], // two are starting
    [{ Processes: 0 }, 'No processes are loaded'],
  ])('%j → %s', (s, want) => {
    expect(healthSentence(stats(s))).toBe(want)
  })

  it('counts failing before anything else, even when some are stopped', () => {
    expect(healthSentence(stats({ Failing: 3, Stopped: 9 }))).toBe('3 processes are failing')
  })
})

describe('health', () => {
  it('names the failing processes, as the table shows them', () => {
    const configs = [
      cfg('mailer.ini', [p('mailer-digest', 'failing'), p('mailer-worker:mailer-worker_00', 'running', 'mailer-worker')]),
      cfg('reports.ini', [p('reports-sync', 'failing'), p('reports-export', 'stopped')]),
    ]
    const h = health(stats({ Failing: 2, Running: 8, Stopped: 2 }), configs)
    expect(h.tone).toBe('bad')
    expect(h.failing).toBe(2)
    expect(text(h)).toBe('mailer-digest and reports-sync need attention.')
    expect(h.detail.filter((s) => s.mono).map((s) => s.text)).toEqual(['mailer-digest', 'reports-sync'])
  })

  it('names one, or three and how many more', () => {
    expect(text(health(stats({ Failing: 1 }), [cfg('a', [p('a', 'failing')])]))).toBe('a needs attention.')
    const five = ['a', 'b', 'c', 'd', 'e'].map((n) => p(n, 'failing'))
    expect(text(health(stats({ Failing: 5 }), [cfg('x', five)]))).toBe('a, b, c and 2 more need attention.')
    expect(text(health(stats({ Failing: 3 }), [cfg('x', five.slice(0, 3))]))).toBe('a, b and c need attention.')
  })

  it('reads calm when nothing fails', () => {
    expect(health(stats({ Running: 12 }), [])).toMatchObject({ tone: 'ok', failing: 0 })
    expect(text(health(stats({ Running: 12 }), []))).toBe('Nothing needs your attention.')
    expect(health(stats({ Running: 10, Stopped: 2 }), []).tone).toBe('ok')
    expect(text(health(stats({ Running: 10, Stopped: 2 }), []))).toBe('2 stopped; none are failing.')
    expect(health(stats({ Running: 9, Stopped: 1 }), []).tone).toBe('warn')
    expect(text(health(stats({ Running: 9, Stopped: 1 }), []))).toBe('1 stopped and 2 starting or stopping; none are failing.')
    expect(health(stats({ Stopped: 12 }), []).tone).toBe('idle')
    expect(text(health(stats({ Stopped: 12 }), []))).toBe('None of them are failing.')
  })

  it('says what to do when nothing is loaded', () => {
    expect(health(stats({ Processes: 0 }), []).tone).toBe('idle')
    expect(text(health(stats({ Processes: 0 }), []))).toBe('supervisord runs none of your config files yet.')
    expect(text(health(stats({ Processes: 0, Configs: 0 }), []))).toBe('Add a program to get started.')
  })

  it('has small counts with singular labels for one', () => {
    expect(health(stats({ Running: 8, Stopped: 2, Failing: 2 }), []).counts).toEqual([
      { label: 'running', value: 8 },
      { label: 'stopped', value: 2 },
      { label: 'processes', value: 12 },
      { label: 'configs', value: 5 },
    ])
    expect(health(stats({ Configs: 1, Processes: 1, Running: 1 }), []).counts.map((c) => c.label)).toEqual(['running', 'stopped', 'process', 'config'])
  })
})

describe('disconnectedHealth', () => {
  it('shows dashes, not zeros, for what only supervisord knows, and keeps the configs on disk', () => {
    const h = disconnectedHealth({ Configs: 5 })
    expect(h).toMatchObject({ tone: 'idle', sentence: 'Not connected', failing: 0 })
    expect(text(h)).toBe('Counts come back with supervisord.')
    expect(h.counts).toEqual([
      { label: 'running', value: NO_COUNT },
      { label: 'stopped', value: NO_COUNT },
      { label: 'processes', value: NO_COUNT },
      { label: 'configs', value: 5 },
    ])
    expect(NO_COUNT).toBe('—')
    expect(disconnectedHealth({ Configs: 1 }).counts[3]).toEqual({ label: 'config', value: 1 })
  })
})

describe('firstFailing', () => {
  it('is the first failing process in table order', () => {
    const configs = [cfg('a', [p('a1', 'running')]), cfg('b', [p('b1', 'stopped'), p('b2', 'failing')]), cfg('c', [p('c1', 'failing')])]
    expect(firstFailing(configs)?.FullName).toBe('b2')
    expect(firstFailing([cfg('a', [p('a1', 'running')])])).toBeUndefined()
    expect(firstFailing([{ FileName: 'e', Processes: null } as ConfigView])).toBeUndefined()
  })
})

describe('processLabel', () => {
  it('drops the group when it is the program itself', () => {
    expect(processLabel(p('flaky', 'running'))).toBe('flaky')
    expect(processLabel(p('shop-queue:shop-queue_00', 'running', 'shop-queue'))).toBe('shop-queue:shop-queue_00')
  })
})

describe('summaryParts', () => {
  it('marks only the failing count', () => {
    expect(summaryParts('2 programs · 2 running · 1 failing')).toEqual([
      { text: '2 programs', failing: false },
      { text: ' · 2 running', failing: false },
      { text: ' · 1 failing', failing: true },
    ])
    expect(summaryParts('1 program · not loaded')).toEqual([
      { text: '1 program', failing: false },
      { text: ' · not loaded', failing: false },
    ])
  })
})

describe('openIssuesLabel', () => {
  it.each([
    [0, 'No open issues'],
    [1, '1 open issue'],
    [3, '3 open issues'],
  ])('%i → %s', (n, want) => {
    expect(openIssuesLabel(n)).toBe(want)
  })
})

describe('reasonClass', () => {
  it('is red for a failing process', () => {
    expect(reasonClass({ Status: 'failing' })).toBe('t-bad')
  })
  it('is red for a running process, whose reason line is a crash loop (an error, as in the bell)', () => {
    expect(reasonClass({ Status: 'running' })).toBe('t-bad')
  })
  it('is grey otherwise', () => {
    expect(reasonClass({ Status: 'stopped' })).toBe('meta')
    expect(reasonClass({ Status: 'other' })).toBe('meta')
  })
})
