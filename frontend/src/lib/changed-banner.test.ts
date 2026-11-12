import { describe, expect, it } from 'vitest'
import { bannerKey, dismissBanner, isBannerDismissed } from './changed-banner'

describe('changed banner', () => {
  const f = (Path: string) => ({ Path })
  it('has nothing to say without files', () => {
    expect(bannerKey([], [], [])).toBe('')
    expect(isBannerDismissed('')).toBe(false)
  })
  it('stays hidden while the same files are listed, in any order, and comes back when they change', () => {
    const k = bannerKey([f('/b'), f('/a')], [f('/c')], [])
    dismissBanner(k)
    expect(isBannerDismissed(bannerKey([f('/a'), f('/b')], [f('/c')], []))).toBe(true)
    expect(isBannerDismissed(bannerKey([f('/a'), f('/b')], [], []))).toBe(false)
    expect(isBannerDismissed(bannerKey([f('/a'), f('/b')], [], [f('/c')]))).toBe(false)
  })
})
