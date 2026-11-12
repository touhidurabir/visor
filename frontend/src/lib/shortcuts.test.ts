import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({ SystemService: {} }))

const { LINUX, MAC, setPlatform } = await import('./platform')
const { modKeyHint, modOnly, shortcutAria, shortcutHint, shortcutOf } = await import('./shortcuts')

const key = (k: string, mods: Partial<KeyboardEvent> = {}) =>
  ({ key: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...mods }) as KeyboardEvent

afterEach(() => setPlatform(MAC))

describe('shortcuts on macOS', () => {
  it('shows ⌘ and takes ⌘ alone', () => {
    expect(shortcutHint('newConfig')).toBe('⌘N')
    expect(shortcutHint('settings')).toBe('⌘,')
    expect(shortcutAria('newConfig')).toBe('Meta+N')
    expect(modKeyHint('c')).toBe('⌘C')
    expect(shortcutOf(key('n', { metaKey: true }))).toBe('newConfig')
    expect(shortcutOf(key('N', { metaKey: true }))).toBe('newConfig')
    expect(shortcutOf(key('n', { ctrlKey: true }))).toBeNull()
    expect(shortcutOf(key('n', { metaKey: true, shiftKey: true }))).toBeNull()
    expect(shortcutOf(key('x', { metaKey: true }))).toBeNull()
  })
})

describe('shortcuts on Linux', () => {
  it('shows Ctrl and takes Ctrl alone', () => {
    setPlatform(LINUX)
    expect(shortcutHint('find')).toBe('Ctrl+F')
    expect(shortcutAria('settings')).toBe('Control+,')
    expect(modKeyHint('c')).toBe('Ctrl+C')
    expect(modOnly(key('f', { ctrlKey: true }))).toBe(true)
    expect(modOnly(key('f', { metaKey: true }))).toBe(false)
    expect(modOnly(key('f', { ctrlKey: true, altKey: true }))).toBe(false)
    const ids = ['1', '2', ',', 'n', 'f', 'r', 'w', 'q'].map((k) => shortcutOf(key(k, { ctrlKey: true })))
    expect(ids).toEqual(['dashboard', 'issues', 'settings', 'newConfig', 'find', 'reload', 'closeWindow', 'quit'])
    // Ctrl+C stays copy.
    expect(shortcutOf(key('c', { ctrlKey: true }))).toBeNull()
  })
})
