import { describe, expect, it } from 'vitest'
import { routeOf } from './router'

describe('routeOf', () => {
  it.each([
    ['', 'main'],
    ['#', 'main'],
    ['#/', 'main'],
    ['#/tray', 'tray'],
    ['#/settings', 'settings'],
    ['#/settings/', 'settings'],
    ['#/nowhere', 'main'],
  ])('%s → %s', (hash, want) => {
    expect(routeOf(hash)).toBe(want)
  })
})
