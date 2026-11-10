import { afterEach, describe, expect, it, vi } from 'vitest'

const platformCall = vi.fn()
vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({ SystemService: { Platform: () => platformCall() } }))

const { LINUX, MAC, isMac, loadPlatform, openFolderLabel, platform, revealLabel, secretStore, setPlatform, showFileLabel, trayIcon, trayName } = await import('./platform')

afterEach(() => setPlatform(MAC))

describe('platform', () => {
  it('starts with the macOS words and takes what Go says', async () => {
    expect(isMac()).toBe(true)
    platformCall.mockResolvedValueOnce(LINUX)
    await loadPlatform()
    expect(platform()).toEqual(LINUX)
    expect(isMac()).toBe(false)
  })

  it('keeps the words it has when Go cannot answer', async () => {
    platformCall.mockRejectedValueOnce(new Error('no binding'))
    await loadPlatform()
    expect(platform()).toEqual(MAC)
  })

  it('words the file manager, the secret store and the tray', () => {
    expect([revealLabel(), openFolderLabel(), showFileLabel(), secretStore(), trayName(), trayIcon()]).toEqual([
      'Reveal in Finder', 'Open in Finder', 'Show in Finder', 'Keychain', 'menu bar', 'menu-bar icon',
    ])
    setPlatform(LINUX)
    expect([revealLabel(), openFolderLabel(), showFileLabel(), secretStore(), trayName(), trayIcon()]).toEqual([
      'Show in file manager', 'Open in file manager', 'Show in file manager', 'keyring', 'tray', 'tray icon',
    ])
  })
})
