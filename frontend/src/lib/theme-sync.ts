// Keeps the page's theme in step with the saved appearance and the OS:
// applies the cached choice at once, follows OS changes live while the
// choice is 'system', follows the visor:settings event (so a change in the
// main window reaches the menu-bar popover), and saves changes.
import { ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { SettingsService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorSettings } from '../../bindings/events'
import { APPEARANCE_CACHE_KEY, applyTheme, normalizeAppearance, type Appearance } from './theme'
import { currentSettings } from './settings'

/** The appearance in use, shared by every control. */
export const appearance = ref<Appearance>('system')

// Saves in flight: an event that arrives meanwhile is older than the click.
let pending = 0
let started = false

const query = () => (typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null)

function render() {
  applyTheme(document.documentElement, appearance.value, query()?.matches ?? false)
}

function cache(a: Appearance) {
  try {
    localStorage.setItem(APPEARANCE_CACHE_KEY, a)
  } catch {
    // Private mode or blocked storage: the early script falls back to the OS.
  }
}

/** show makes a the appearance in use, in memory and on screen. */
function show(a: Appearance) {
  appearance.value = a
  cache(a)
  render()
  const cur = currentSettings.value
  if (cur) cur.Settings.appearance = a
}

/** startThemeSync is called once, before the app mounts. */
export function startThemeSync() {
  if (started) return
  started = true
  try {
    appearance.value = normalizeAppearance(localStorage.getItem(APPEARANCE_CACHE_KEY))
  } catch {
    // keep 'system'
  }
  render()
  query()?.addEventListener('change', render)
  Events.On(VisorSettings, (ev) => {
    if (pending === 0) show(normalizeAppearance(ev.data?.Settings?.appearance))
  })
  void SettingsService.Get()
    .then((v) => {
      if (pending === 0) show(normalizeAppearance(v.Settings.appearance))
    })
    .catch(() => {
      // The page reports a settings load failure itself.
    })
}

/**
 * setAppearance shows the choice at once and saves it. If the save fails the
 * previous choice comes back and the error is thrown for the caller to show.
 */
export async function setAppearance(next: Appearance): Promise<void> {
  const before = appearance.value
  if (next === before) return
  pending++
  show(next)
  try {
    const cur = await SettingsService.Get()
    await SettingsService.Save({ ...cur.Settings, appearance: next })
  } catch (e) {
    show(before)
    throw e
  } finally {
    pending--
  }
}
