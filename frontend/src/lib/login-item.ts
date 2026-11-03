// Launch at login (LoginItemService): what each status means for the user,
// and the switch's state for a view.
import { Events } from '@wailsio/runtime'
import { getCurrentScope, onScopeDispose, ref } from 'vue'
import { LoginItemService, type LoginItemView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorLoginItem } from '../../bindings/events'
import { describeError } from './faults'

const hints: Record<string, string> = {
  enabled: 'Visor opens in the menu bar when you log in.',
  disabled: 'Open Visor in the menu bar when you log in.',
  requires_approval: 'macOS needs your OK: turn Visor on in System Settings → General → Login Items.',
  not_moved: 'Move Visor to Applications first. It’s running from the disk image or Downloads, which macOS doesn’t keep.',
  unsupported: 'Works in the installed Visor.app, not in this build.',
}

/** The line under the switch. */
export function loginHint(status: string): string {
  return hints[status] ?? hints.disabled
}

/** Whether the switch can't be used until something else changes. */
export function loginLocked(status: string): boolean {
  return status === 'unsupported' || status === 'not_moved'
}

/**
 * The launch-at-login switch's state for a view (Settings, Doctor): load
 * the status, turn it on or off, open Login Items when macOS wants an OK.
 */
export function useLaunchAtLogin() {
  const view = ref<LoginItemView | null>(null)
  const busy = ref(false)
  const error = ref<string | null>(null)
  // Changed from the tray menu while this view is open.
  const off = Events.On(VisorLoginItem, (ev) => {
    view.value = ev.data
    error.value = null
  })
  if (getCurrentScope()) onScopeDispose(off)

  async function load() {
    try {
      view.value = await LoginItemService.LaunchAtLogin()
    } catch (e) {
      error.value = describeError(e)
    }
  }
  async function set(on: boolean) {
    busy.value = true
    error.value = null
    try {
      view.value = await LoginItemService.SetLaunchAtLogin(on)
    } catch (e) {
      error.value = describeError(e)
      await load()
    } finally {
      busy.value = false
    }
  }
  function openLoginItems() {
    LoginItemService.OpenLoginItemsSettings().catch((e) => (error.value = describeError(e)))
  }
  return { view, busy, error, load, set, openLoginItems }
}
