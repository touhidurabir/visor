import { describe, expect, it } from 'vitest'
import { chartPaths, fmtBytes, fmtCpu, sparkPath, usageLabel, usageText } from './usage'

describe('fmtCpu', () => {
  it('formats a share of one core', () => {
    expect(fmtCpu(-1)).toBe('–')
    expect(fmtCpu(0)).toBe('0 %')
    expect(fmtCpu(0.02)).toBe('0 %')
    expect(fmtCpu(4.14)).toBe('4.1 %')
    expect(fmtCpu(97.44)).toBe('97.4 %')
    expect(fmtCpu(120.3)).toBe('120 %')
  })
})

describe('fmtBytes', () => {
  it('uses 1024 steps', () => {
    expect(fmtBytes(-1)).toBe('–')
    expect(fmtBytes(0)).toBe('0 KB')
    expect(fmtBytes(980 * 1024)).toBe('980 KB')
    expect(fmtBytes(182 * 1024 * 1024)).toBe('182 MB')
    expect(fmtBytes(5.25 * 1024 * 1024)).toBe('5.3 MB')
    expect(fmtBytes(1.2 * 1024 ** 3)).toBe('1.2 GB')
  })
})

describe('usageLabel', () => {
  it('shows … for CPU before the second sample', () => {
    expect(usageLabel({ CPU: -1, MemBytes: 38 * 1024 * 1024 })).toEqual({ cpu: '…', mem: '38 MB', known: true })
    expect(usageLabel({ CPU: -1, MemBytes: -1 })).toEqual({ cpu: '–', mem: '–', known: false })
    expect(usageLabel({ CPU: 4.1, MemBytes: 1024 * 1024 })).toEqual({ cpu: '4.1 %', mem: '1.0 MB', known: true })
  })
})

describe('sparkPath', () => {
  it('scales to the box, peak at the top', () => {
    expect(sparkPath([], 48, 14)).toBe('')
    expect(sparkPath([5], 48, 14)).toBe('')
    expect(sparkPath([0, 10], 48, 14)).toBe('0,13 48,1')
  })
  it('keeps a flat zero line on the floor', () => {
    expect(sparkPath([0, 0, 0], 48, 14)).toBe('0,13 24,13 48,13')
  })
})

describe('chartPaths', () => {
  const at = (s: number) => 1_000_000 + s * 1000
  it('splits at unknown points and gaps', () => {
    const pts = [
      { AtMs: at(0), v: 0 },
      { AtMs: at(10), v: 50 },
      { AtMs: at(20), v: -1 },
      { AtMs: at(30), v: 100 },
      { AtMs: at(40), v: 100 },
      { AtMs: at(100), v: 0 },
    ]
    expect(chartPaths(pts, at(0), at(100), 100, 100, 10)).toEqual(['0,10 10,5', '30,0 40,0', '100,10'])
  })
  it('drops points before the window', () => {
    expect(chartPaths([{ AtMs: at(-50), v: 1 }, { AtMs: at(0), v: 1 }], at(0), at(10), 1, 10, 10)).toEqual(['0,0'])
  })
})

describe('usageText', () => {
  it('names the children and the unknowns', () => {
    expect(usageText({ CPU: 4.1, MemBytes: 1024 * 1024, Procs: 3, PID: 5 })).toBe('CPU 4.1 %, memory 1.0 MB, with 2 children')
    expect(usageText({ CPU: -1, MemBytes: 1024 * 1024, Procs: 2, PID: 5 })).toBe('CPU measuring, memory 1.0 MB, with 1 child')
    expect(usageText({ CPU: -1, MemBytes: -1, Procs: 0, PID: 0 })).toBe('Not running')
    expect(usageText({ CPU: -1, MemBytes: -1, Procs: 0, PID: 9 })).toMatch(/can’t measure/)
  })
})
