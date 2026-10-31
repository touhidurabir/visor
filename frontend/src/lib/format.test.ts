import { describe, expect, it } from 'vitest'
import { duration, formatDuration, historyNote, historyTime, shortSince, sinceLabel } from './format'
import type { ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

const s = 1000
const m = 60 * s
const h = 60 * m
const d = 24 * h

describe('formatDuration', () => {
  // Same units as the Go side's "up 6d 3h" labels.
  it.each([
    [0, '0s'],
    [59 * s, '59s'],
    [41 * m + 5 * s, '41m'],
    [3 * h + 2 * m, '3h 2m'],
    [3 * h, '3h'],
    [6 * d + 3 * h + 10 * m, '6d 3h'],
    [5 * d, '5d'],
    [-5, '0s'],
  ])('%d ms → %s', (ms, want) => {
    expect(formatDuration(ms)).toBe(want)
  })
})

describe('shortSince', () => {
  it.each([
    ['up 6d 3h', '6d 3h'],
    ['failing 2h 12m', 'failing 2h 12m'],
    ['stopped 5d', 'stopped 5d'],
    ['', ''],
  ])('%s → %s', (since, want) => {
    expect(shortSince(since)).toBe(want)
  })
})

describe('duration', () => {
  it.each([
    [0, 'just now'],
    [59 * s, 'just now'],
    [60 * s, '1m'],
    [41 * m + 5 * s, '41m'],
    [3 * h + 2 * m, '3h 2m'],
    [3 * h, '3h'],
    [6 * d + 3 * h + 10 * m, '6d 3h'],
    [5 * d, '5d'],
  ])('%d ms → %s', (ms, want) => {
    expect(duration(ms)).toBe(want)
  })
})

describe('sinceLabel', () => {
  const base = { StartMs: 0, StopMs: 0, State: 'RUNNING', Status: 'running', Since: '' } as unknown as ProcessView
  const now = 10 * d
  it('running: up + time since start', () => {
    expect(sinceLabel({ ...base, StartMs: now - (41 * m + 5 * s) }, now)).toBe('up 41m')
  })
  it('running for under a minute: up just now', () => {
    expect(sinceLabel({ ...base, StartMs: now - 5 * s }, now)).toBe('up just now')
  })
  it('failing: failing + time since stop', () => {
    expect(sinceLabel({ ...base, State: 'FATAL', Status: 'failing', StopMs: now - (2 * h + 12 * m) }, now)).toBe('failing 2h 12m')
  })
  it('stopped: stopped + time since stop', () => {
    expect(sinceLabel({ ...base, State: 'STOPPED', Status: 'stopped', StopMs: now - 5 * d }, now)).toBe('stopped 5d')
  })
  it('no timestamps: falls back to the label from the Go side', () => {
    expect(sinceLabel({ ...base, State: 'STOPPED', Status: 'stopped', Since: 'stopped 3d' }, now)).toBe('stopped 3d')
  })
})

describe('historyTime', () => {
  // Local time, like the clock people read.
  const now = new Date(2026, 9, 30, 10, 0).getTime() // Fri 30 Oct 2026
  it.each([
    [new Date(2026, 9, 30, 7, 48), 'Today 07:48'],
    [new Date(2026, 9, 29, 23, 5), 'Yesterday 23:05'],
    [new Date(2026, 9, 26, 14, 2), 'Mon 14:02'],
    [new Date(2026, 9, 3, 9, 30), 'Oct 3 09:30'],
    [new Date(2025, 11, 31, 18, 0), 'Dec 31 2025 18:00'],
  ])('%s → %s', (at, want) => {
    expect(historyTime(at.getTime(), now)).toBe(want)
  })
})

describe('historyNote', () => {
  it('uses the recorded note, else the state it came from', () => {
    expect(historyNote({ AtMs: 1, Process: 'a:a', From: 'RUNNING', To: 'RUNNING', Note: 'Restarted by you' })).toBe('Restarted by you')
    expect(historyNote({ AtMs: 1, Process: 'a:a', From: 'STOPPED', To: 'STARTING', Note: 'by you' })).toBe('By you')
    expect(historyNote({ AtMs: 1, Process: 'a:a', From: 'STARTING', To: 'RUNNING', Note: '' })).toBe('From STARTING')
    expect(historyNote({ AtMs: 1, Process: 'a:a', From: '', To: 'RUNNING', Note: '' })).toBe('First seen')
  })
})
