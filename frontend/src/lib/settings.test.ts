import { describe, expect, it, vi } from 'vitest'

vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({ SettingsService: { Get: vi.fn() } }))

import { LIMITS, clampNote, minutesToNs, notifyPermission, nsToMinutes, nsToSeconds, retentionChange, retentionConfirm, secondsToNs, typedNumber } from './settings'

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

describe('retentionChange (R27)', () => {
  it('is the same when the typed days match the saved ones', () => {
    expect(retentionChange('90', 90)).toEqual({ kind: 'same' })
    expect(retentionChange(' 90 ', 90)).toEqual({ kind: 'same' })
  })
  it('raises or lowers to whole days', () => {
    expect(retentionChange('120', 90)).toEqual({ kind: 'raise', days: 120 })
    expect(retentionChange('30', 90)).toEqual({ kind: 'lower', days: 30 })
    expect(retentionChange(29.6, 90)).toEqual({ kind: 'lower', days: 30 })
  })
  it('clamps like Go does, so the confirmation names the days that apply', () => {
    expect(retentionChange('1', 90)).toEqual({ kind: 'lower', days: 7 })
    expect(retentionChange('99999', 90)).toEqual({ kind: 'raise', days: 3650 })
    expect(retentionChange('3', 7)).toEqual({ kind: 'same' })
  })
  it('is invalid for empty or junk text', () => {
    expect(retentionChange('', 90)).toEqual({ kind: 'invalid' })
    expect(retentionChange('abc', 90)).toEqual({ kind: 'invalid' })
  })
  it('asks before deleting, naming the days', () => {
    expect(retentionConfirm(30)).toEqual({
      title: 'Delete issues and history older than 30 days?',
      message: 'This can’t be undone.',
      confirmLabel: 'Delete older issues',
    })
  })
})

describe('notifyPermission', () => {
  it('words each permission', () => {
    expect(notifyPermission('allowed')).toEqual({ text: 'Allowed', tone: 'ok' })
    expect(notifyPermission('denied')).toEqual({ text: 'Not allowed. Allow Visor in System Settings → Notifications.', tone: 'bad' })
    expect(notifyPermission('unavailable')).toEqual({ text: 'Not available in this build', tone: 'idle' })
  })
  it('says it is checking before the answer comes', () => {
    expect(notifyPermission('')).toEqual({ text: 'Checking…', tone: 'idle' })
    expect(notifyPermission('something new')).toEqual({ text: 'Checking…', tone: 'idle' })
  })
})
