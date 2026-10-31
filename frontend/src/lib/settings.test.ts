import { describe, expect, it, vi } from 'vitest'

vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({ SettingsService: { Get: vi.fn() } }))

import { LIMITS, clampNote, minutesToNs, nsToMinutes, nsToSeconds, secondsToNs, typedNumber } from './settings'

describe('unit conversions', () => {
  it('turns nanoseconds into seconds and back', () => {
    expect(nsToSeconds(2_000_000_000)).toBe(2)
    expect(secondsToNs(2)).toBe(2_000_000_000)
    expect(secondsToNs(1.5)).toBe(1_500_000_000)
    expect(nsToSeconds(secondsToNs(7))).toBe(7)
  })
  it('turns nanoseconds into minutes and back', () => {
    expect(nsToMinutes(600_000_000_000)).toBe(10)
    expect(minutesToNs(10)).toBe(600_000_000_000)
    expect(nsToMinutes(minutesToNs(1440))).toBe(1440)
  })
  it('rounds odd durations to one decimal', () => {
    expect(nsToSeconds(1_234_000_000)).toBe(1.2)
    expect(nsToMinutes(90_000_000_000)).toBe(1.5)
  })
})

describe('typedNumber', () => {
  it('reads numbers from text', () => {
    expect(typedNumber(' 12 ')).toBe(12)
    expect(typedNumber('2.5')).toBe(2.5)
    expect(typedNumber(4)).toBe(4)
  })
  it('is null for empty or non-numeric entries', () => {
    expect(typedNumber('')).toBeNull()
    expect(typedNumber('  ')).toBeNull()
    expect(typedNumber('abc')).toBeNull()
    expect(typedNumber(Number.NaN)).toBeNull()
  })
})

describe('clampNote', () => {
  it('is empty when the value was kept', () => {
    expect(clampNote(5, 5, LIMITS.pollSeconds, 'second')).toBe('')
    expect(clampNote(null, 5, LIMITS.pollSeconds, 'second')).toBe('')
  })
  it('names the shortest allowed value', () => {
    expect(clampNote(0, 1, LIMITS.pollSeconds, 'second')).toBe('Set to 1 second, the shortest allowed.')
    expect(clampNote(1, 7, LIMITS.retentionDays, 'day')).toBe('Set to 7 days, the shortest allowed.')
  })
  it('names the longest allowed value', () => {
    expect(clampNote(60, 10, LIMITS.pollSeconds, 'second')).toBe('Set to 10 seconds, the longest allowed.')
  })
  it('reports other changes plainly', () => {
    expect(clampNote(2.25, 2.2, LIMITS.pollSeconds, 'second')).toBe('Set to 2.2 seconds.')
  })
})
