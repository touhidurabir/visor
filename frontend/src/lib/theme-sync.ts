// Keeps the page's theme in step with the saved appearance and the OS:
// applies the cached choice at once, follows OS changes live while the
// choice is 'system', follows the visor:settings event (so a change in the
// main window reaches the menu-bar popover), and saves changes one at a
// time through SetAppearance, which changes nothing else.
import { ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { SettingsService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { SettingsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorSettings } from '../../bindings/events'
import { createSaveQueue } from './save-queue'
import { APPEARANCE_CACHE_KEY, applyTheme, normalizeAppearance, type Appearance } from './theme'
import { currentSettings } from './settings'

/** The appearance in use, shared by every control. */
export const appearance = ref<Appearance>('system')

let started = false
// The appearance Visor last confirmed saved.
let saved: Appearance = 'system'
// Why the last run of saves failed, for the first caller waiting on it.
let failure: unknown = null

// One save in flight; choices made meanwhile fold into the next, so quick
// arrow presses save once more at most and the last choice wins. While it
// runs, visor:settings events are older than the choice on screen.
const queue = createSaveQueue<{ appearance: Appearance }, SettingsView>({
  base: () => ({ appearance: saved }),
  settingsOf: (v) => ({ appearance: normalizeAppearance(v.Settings.appearance) }),
  send: (next) => SettingsService.SetAppearance(next.appearance),
  done(v, errors) {
    if (v) saved = normalizeAppearance(v.Settings.appearance)
    failure = errors.length ? errors[errors.length - 1] : null
    show(saved) // what is on screen is what is stored
  },
})

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
  saved = appearance.value
  render()
  query()?.addEventListener('change', render)
  Events.On(VisorSettings, (ev) => adopt(ev.data?.Settings?.appearance))
  void SettingsService.Get()
    .then((v) => adopt(v.Settings.appearance))
    .catch(() => {
      // The page reports a settings load failure itself.
    })
}

/** adopt shows a saved appearance from Go, unless a save of ours is still running. */
function adopt(raw: unknown) {
  if (queue.busy()) return
  saved = normalizeAppearance(raw)
  show(saved)
}

/**
 * setAppearance shows the choice at once and saves it. If the save fails the
 * stored choice comes back and the error is thrown for the caller to show.
 */
export async function setAppearance(next: Appearance): Promise<void> {
  if (next === appearance.value) return
  show(next)
  await queue.enqueue((s) => {
    s.appearance = next
  })
  const e = failure
  failure = null
  if (e) throw e
}
