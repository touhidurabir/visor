import { describe, expect, it, vi } from 'vitest'

const status = vi.fn()
const setLogin = vi.fn()
vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({
  LoginItemService: { LaunchAtLogin: () => status(), SetLaunchAtLogin: (on: boolean) => setLogin(on), OpenLoginItemsSettings: () => Promise.resolve() },
}))

let onLoginItem: ((ev: { data: unknown }) => void) | undefined
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Events: {
    On: (name: string, cb: (ev: { data: unknown }) => void) => {
      if (name === 'visor:login-item') onLoginItem = cb
      return () => {}
    },
  },
}))

const { loginHint, loginLocked, useLaunchAtLogin } = await import('./login-item')

describe('launch at login wording', () => {
  it('says what each status means', () => {
    expect(loginHint('enabled')).toBe('Visor opens in the menu bar when you log in.')
    expect(loginHint('disabled')).toBe('Open Visor in the menu bar when you log in.')
    expect(loginHint('requires_approval')).toBe('macOS needs your OK: turn Visor on in System Settings → General → Login Items.')
    expect(loginHint('not_moved')).toBe('Move Visor to Applications first. It’s running from the disk image or Downloads, which macOS doesn’t keep.')
    expect(loginHint('unsupported')).toBe('Works in the installed Visor.app, not in this build.')
  })
  it('locks the switch only where it can’t work', () => {
    expect(loginLocked('unsupported')).toBe(true)
    expect(loginLocked('not_moved')).toBe(true)
    expect(loginLocked('requires_approval')).toBe(false)
    expect(loginLocked('disabled')).toBe(false)
  })
})

describe('useLaunchAtLogin', () => {
  it('turns it on and shows the new status', async () => {
    status.mockResolvedValueOnce({ Status: 'disabled', On: false })
    setLogin.mockResolvedValueOnce({ Status: 'enabled', On: true })
    const l = useLaunchAtLogin()
    await l.load()
    await l.set(true)
    expect(setLogin).toHaveBeenCalledWith(true)
    expect(l.view.value?.On).toBe(true)
    expect(l.busy.value).toBe(false)
  })
  it('says why when macOS or the app’s location refuses, and shows the real status', async () => {
    setLogin.mockRejectedValueOnce(new Error('move Visor to Applications first'))
    status.mockResolvedValueOnce({ Status: 'not_moved', On: false })
    const l = useLaunchAtLogin()
    await l.set(true)
    expect(l.error.value).toContain('move Visor to Applications first')
    expect(l.view.value?.Status).toBe('not_moved')
  })
  it('follows a change made from the tray menu', () => {
    const l = useLaunchAtLogin()
    onLoginItem?.({ data: { Status: 'enabled', On: true } })
    expect(l.view.value).toEqual({ Status: 'enabled', On: true })
  })
})
