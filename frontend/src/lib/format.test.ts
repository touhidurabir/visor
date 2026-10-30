import { describe, expect, it } from 'vitest'
import { formatDuration, shortSince } from './format'

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
