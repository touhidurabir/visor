import { describe, expect, it } from 'vitest'
import { applyTheme, normalizeAppearance, resolveTheme, type ThemeRoot } from './theme'

describe('normalizeAppearance', () => {
  it('keeps the three values and maps everything else to system', () => {
    expect(normalizeAppearance('light')).toBe('light')
    expect(normalizeAppearance('dark')).toBe('dark')
    expect(normalizeAppearance('system')).toBe('system')
    expect(normalizeAppearance('sepia')).toBe('system')
    expect(normalizeAppearance(undefined)).toBe('system')
    expect(normalizeAppearance(null)).toBe('system')
  })
})

describe('resolveTheme', () => {
  it('lets an explicit choice win over the OS', () => {
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })
  it('follows the OS for system', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
  })
})

describe('applyTheme', () => {
  const root = (): ThemeRoot => ({ dataset: {}, style: { colorScheme: '' } })
  it('sets data-theme and color-scheme', () => {
    const r = root()
    expect(applyTheme(r, 'dark', false)).toBe('dark')
    expect(r.dataset.theme).toBe('dark')
    expect(r.style.colorScheme).toBe('dark')
  })
  it('re-resolves when the OS changes under system', () => {
    const r = root()
    applyTheme(r, 'system', false)
    expect(r.dataset.theme).toBe('light')
    applyTheme(r, 'system', true)
    expect(r.dataset.theme).toBe('dark')
    expect(r.style.colorScheme).toBe('dark')
  })
})
