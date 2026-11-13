<script setup lang="ts">
// Settings (spec §5.8). The connection is saved explicitly with "Save and
// reconnect", because it rebuilds the connection, and retention with
// "Apply", because lowering it deletes history (R27: it asks first).
// Everything else saves as it changes and applies straight away; Go clamps
// out-of-range values and the page shows what it kept.
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { NotifyService, SettingsService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { ServerService } from '../lib/api'
import type { ConnectionInput, SettingsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Settings } from '../../bindings/github.com/touhidurabir/visor/internal/settings'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import ThemeSwitch from '../components/ThemeSwitch.vue'
import { loginHint, loginLocked, useLaunchAtLogin } from '../lib/login-item'
import { describeError } from '../lib/faults'
import { useOverview } from '../lib/overview'
import { createSaveQueue } from '../lib/save-queue'
import {
  LIMITS,
  type Limit,
  clampNote,
  currentSettings,
  loadSettings,
  minutesToNs,
  notifyPermission,
  nsToMinutes,
  nsToSeconds,
  retentionChange,
  retentionConfirm,
  secondsToNs,
  typedNumber,
} from '../lib/settings'
import { appearance } from '../lib/theme-sync'
import { showToast } from '../lib/toast'
import { isMac, platform, secretStore, trayIcon } from '../lib/platform'

const { overview } = useOverview()
const view = ref<SettingsView | null>(null)
const loadError = ref('')
const warning = ref('')

// Connection draft: edited freely, saved with "Save and reconnect".
const conn = reactive({ configPath: '', username: '', password: '' })
// A saved password shows as a status line; Change… swaps in the box.
const changingPassword = ref(false)
const passwordBox = ref<HTMLInputElement | null>(null)
const connBusy = ref(false)
const connError = ref('')

// Number fields keep the typed text until saved; notes explain clamping.
type FieldKey = 'poll' | 'restarts' | 'window' | 'retention'
const fields = reactive<Record<FieldKey, string>>({ poll: '', restarts: '', window: '', retention: '' })
const notes = reactive<Record<FieldKey, string>>({ poll: '', restarts: '', window: '', retention: '' })
// Fields typed in since their last change event: a save's response never
// overwrites them.
const editing = reactive(new Set<FieldKey>())
// What was typed into each field saved in the current run, for the notes.
let typed: Partial<Record<FieldKey, number | null>> = {}
type NotifyKey = 'enabled' | 'fatal' | 'unexpectedExit' | 'crashLoop' | 'unreachable'
// The switches show this local copy, so a click shows at once while its
// save waits its turn.
const notify = reactive<Record<NotifyKey, boolean>>({ enabled: true, fatal: true, unexpectedExit: true, crashLoop: true, unreachable: true })
const saving = ref(false)
const saveError = ref('')

const s = computed<Settings | null>(() => view.value?.Settings ?? null)
const envSet = computed(() => !!view.value?.EnvConfigPath)
const connDirty = computed(() => {
  const v = view.value
  if (!v) return false
  const path = envSet.value ? v.Settings.configPath : conn.configPath
  return path !== v.Settings.configPath || conn.username !== v.Settings.username || conn.password !== '' || changingPassword.value
})

/** showSaved puts saved values into the fields nobody is typing in. */
function showSaved(st: Settings) {
  const text: Record<FieldKey, string> = {
    poll: String(nsToSeconds(st.pollInterval)),
    restarts: String(st.crashLoop.restarts),
    window: String(nsToMinutes(st.crashLoop.window)),
    retention: String(st.retentionDays),
  }
  for (const k of Object.keys(text) as FieldKey[]) if (!editing.has(k)) fields[k] = text[k]
  Object.assign(notify, st.notify)
}

/** adopt takes a whole view (load, connection save, reconnect). */
function adopt(v: SettingsView, keepDraft = false) {
  view.value = v
  currentSettings.value = v
  if (!keepDraft) {
    conn.configPath = v.Settings.configPath
    conn.username = v.Settings.username
    conn.password = ''
    changingPassword.value = false
  }
  showSaved(v.Settings)
}

// The switch saves on its own (SetAppearance; Save leaves the appearance
// alone); keep this page's copy current so it shows what is in use.
watch(appearance, (a) => {
  if (view.value) view.value.Settings.appearance = a
})

const login = useLaunchAtLogin()
onMounted(() => void login.load())
/** Turn launch at login on or off; the box then shows what is really set. */
async function setLogin(e: Event) {
  const box = e.target as HTMLInputElement
  await login.set(box.checked)
  box.checked = login.view.value?.On ?? false // unchanged after a refusal, so Vue won't redraw it
}
onMounted(async () => {
  window.addEventListener('focus', checkPermission)
  void checkPermission()
  try {
    adopt(await loadSettings())
  } catch (e) {
    loadError.value = describeError(e)
  }
})
onUnmounted(() => window.removeEventListener('focus', checkPermission))

// --- Connection ---------------------------------------------------------

const status = computed(() => {
  const sv = overview.value?.Server
  if (!sv || sv.Health === 'unknown') return { tone: 'idle', text: 'Connecting…' }
  if (sv.Reachable) return { tone: 'ok', text: sv.Version ? `Connected to supervisord ${sv.Version}` : 'Connected' }
  return { tone: 'bad', text: 'Not connected' }
})
const serverError = computed(() => (overview.value?.Server.Reachable ? '' : (overview.value?.Server.Error ?? '')))

async function choose() {
  try {
    const path = await SettingsService.ChooseConfig()
    if (path) conn.configPath = path
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}

function autoDetect() {
  conn.configPath = ''
}

function cancelConnection() {
  if (view.value) adopt(view.value)
  connError.value = ''
}

async function sendConnection(input: ConnectionInput, done: string, keepDraft = false) {
  if (connBusy.value) return
  connBusy.value = true
  connError.value = ''
  try {
    const draft = { ...conn }
    const v = await SettingsService.SaveConnection(input)
    adopt(v)
    if (keepDraft && draft.configPath !== v.Settings.configPath) conn.configPath = draft.configPath
    warning.value = v.Warning
    showToast(v.Warning ? 'Saved, with a problem: see the note above' : done, v.Warning ? 'error' : 'info')
  } catch (e) {
    connError.value = describeError(e)
  } finally {
    connBusy.value = false
  }
}

function saveConnection() {
  void sendConnection(
    { ConfigPath: conn.configPath, Username: conn.username, Password: conn.password, SetPassword: conn.password !== '' },
    'Saved. Visor reconnected to supervisord.',
  )
}

/**
 * removePassword forgets the saved password and the username with it, so
 * the config's [supervisorctl] credentials apply again. It saves only that:
 * an unsaved config path stays a draft.
 */
function removePassword() {
  const saved = view.value!.Settings
  void sendConnection(
    { ConfigPath: saved.configPath, Username: '', Password: '', SetPassword: true },
    'Password and username removed. Visor uses the config’s credentials.',
    true,
  )
}

async function changePassword() {
  changingPassword.value = true
  await nextTick()
  passwordBox.value?.focus()
}

async function reconnect() {
  if (connBusy.value) return
  connBusy.value = true
  connError.value = ''
  try {
    const ov = await ServerService.Reconnect()
    adopt(await loadSettings(), true) // anything typed meanwhile stays a draft
    showToast(ov.Server.Reachable ? 'Reconnected to supervisord' : 'Reconnected, but supervisord is not answering', ov.Server.Reachable ? 'info' : 'error')
  } catch (e) {
    connError.value = describeError(e)
  } finally {
    connBusy.value = false
  }
}

// --- Everything else: saved as it changes -------------------------------

const queue = createSaveQueue<Settings, SettingsView>({
  base: () => view.value!.Settings,
  settingsOf: (v) => v.Settings,
  send: (next) => SettingsService.Save(next),
  done(v, errors) {
    saving.value = false
    const cur = view.value!
    if (v) {
      // Save never changes the connection, retention or appearance; keep
      // what the page shows for them, which their own saves may have
      // changed meanwhile.
      const merged: SettingsView = {
        ...cur,
        Settings: {
          ...v.Settings,
          configPath: cur.Settings.configPath,
          username: cur.Settings.username,
          retentionDays: cur.Settings.retentionDays,
          appearance: cur.Settings.appearance,
        },
        LoadError: v.LoadError,
        Warning: v.Warning,
      }
      view.value = merged
      currentSettings.value = merged
      warning.value = v.Warning
    }
    showSaved(view.value!.Settings) // after a failure, this puts the saved values back
    const st = view.value!.Settings
    const kept: Record<FieldKey, [number, Limit, string]> = {
      poll: [nsToSeconds(st.pollInterval), LIMITS.pollSeconds, 'second'],
      restarts: [st.crashLoop.restarts, LIMITS.restarts, 'restart'],
      window: [nsToMinutes(st.crashLoop.window), LIMITS.windowMinutes, 'minute'],
      retention: [st.retentionDays, LIMITS.retentionDays, 'day'],
    }
    for (const [k, t] of Object.entries(typed) as [FieldKey, number | null][]) notes[k] = clampNote(t, ...kept[k])
    typed = {}
    saveError.value = errors.length ? describeError(errors[errors.length - 1]) : ''
  },
})

function save(change: (next: Settings) => void) {
  if (!view.value) return
  saving.value = true
  void queue.enqueue(change)
}

/** saveField queues a number field's change; empty or junk text puts the saved value back. */
function saveField(key: FieldKey, apply: (next: Settings, n: number) => void) {
  editing.delete(key)
  const n = typedNumber(fields[key])
  if (n === null) {
    showSaved(view.value!.Settings)
    return
  }
  typed[key] = n
  save((next) => apply(next, n))
}

const savePoll = () => saveField('poll', (next, n) => (next.pollInterval = secondsToNs(n)))
const saveRestarts = () => saveField('restarts', (next, n) => (next.crashLoop.restarts = Math.round(n)))
const saveWindow = () => saveField('window', (next, n) => (next.crashLoop.window = minutesToNs(n)))

// --- Retention: a draft, applied explicitly (R27) -----------------------

const retention = computed(() => (s.value ? retentionChange(fields.retention, s.value.retentionDays) : { kind: 'same' as const }))
const retentionBusy = ref(false)
const confirmRetention = ref<ReturnType<typeof retentionConfirm> & { days: number } | null>(null)

/** applyRetention saves a higher retention at once; a lower one asks first. */
function applyRetention() {
  const r = retention.value
  if (r.kind === 'lower') confirmRetention.value = { ...retentionConfirm(r.days), days: r.days }
  else if (r.kind === 'raise') void sendRetention(r.days)
}

function cancelRetention() {
  editing.delete('retention')
  notes.retention = ''
  if (view.value) showSaved(view.value.Settings)
}

async function sendRetention(days: number) {
  confirmRetention.value = null
  if (retentionBusy.value || !view.value) return
  retentionBusy.value = true
  const typedDays = typedNumber(fields.retention)
  try {
    const v = await SettingsService.ApplyRetention(days)
    const cur = view.value!
    // Like a settings save: keep what the page shows for the connection.
    const merged: SettingsView = { ...cur, Settings: { ...cur.Settings, retentionDays: v.Settings.retentionDays }, Warning: v.Warning }
    view.value = merged
    currentSettings.value = merged
    warning.value = v.Warning
    editing.delete('retention')
    showSaved(merged.Settings)
    notes.retention = clampNote(typedDays, v.Settings.retentionDays, LIMITS.retentionDays, 'day')
    saveError.value = ''
  } catch (e) {
    saveError.value = describeError(e)
  } finally {
    retentionBusy.value = false
  }
}

function setMenuBarCount(on: boolean) {
  save((next) => (next.menuBarCount = on))
}

function setNotify(key: NotifyKey, on: boolean) {
  notify[key] = on
  save((next) => (next.notify[key] = on))
}

// Whether macOS shows Visor's notifications. Checked again when the window
// comes back to the front, e.g. from System Settings.
const permission = ref('')
const permissionView = computed(() => notifyPermission(permission.value))
const testBusy = ref(false)

async function checkPermission() {
  try {
    permission.value = await NotifyService.NotificationStatus()
  } catch {
    permission.value = 'unavailable'
  }
}

function openNotificationSettings() {
  NotifyService.OpenNotificationSettings().catch((e) => showToast(describeError(e), 'error'))
}

async function sendTest() {
  testBusy.value = true
  try {
    await NotifyService.SendTestNotification()
    showToast('Sent a test notification')
  } catch (e) {
    showToast(describeError(e), 'error')
  } finally {
    testBusy.value = false
    void checkPermission()
  }
}

const notifyTypes: { key: Exclude<NotifyKey, 'enabled'>; label: string }[] = [
  { key: 'fatal', label: 'A process can’t start (FATAL)' },
  { key: 'unexpectedExit', label: 'A process exits unexpectedly' },
  { key: 'crashLoop', label: 'A process keeps restarting (crash loop)' },
  { key: 'unreachable', label: 'supervisord stops responding' },
]
</script>


<template>
  <section class="page" aria-labelledby="settings-title">
    <header>
      <div class="title">
        <h1 id="settings-title">Settings</h1>
        <span v-if="view" :class="['badge', saving ? 'b-idle' : 'b-ok']" aria-live="polite">
          <svg v-if="!saving" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" aria-hidden="true"><path d="m5 12 5 5 9-10" /></svg>
          {{ saving ? 'Saving…' : 'All changes saved' }}
        </span>
      </div>
      <p class="intro">How Visor connects to supervisord, how often it checks, and what it tells you about. Most changes save as you make them.</p>
    </header>

    <div v-if="loadError" class="alert" role="alert">
      <strong>Can’t load the settings</strong>
      <p>{{ loadError }}</p>
    </div>
    <div v-if="view?.LoadError" class="alert" role="alert">
      <strong>Visor is using the default settings</strong>
      <p>{{ view.LoadError }}</p>
    </div>
    <div v-if="saveError" class="alert" role="alert">
      <strong>Can’t save that change</strong>
      <p>{{ saveError }}</p>
    </div>
    <div v-if="warning" class="warn" role="status">
      <strong>Saved, with a problem</strong>
      <p>{{ describeError(warning) }}</p>
    </div>

    <!-- Card order (flow change 3): General, Connection, Notifications,
         Checking and history. M7's remote servers go above General. -->
    <div v-if="view && s" class="cards">
      <!-- General -->
      <div class="card c-gen" role="group" aria-labelledby="general-title">
        <h2 id="general-title" class="card-title">General</h2>
        <div class="row">
          <div class="what">
            <label class="name" for="login">Launch at login</label>
            <span id="login-hint" class="hint">{{ login.view.value ? loginHint(login.view.value.Status) : '' }}</span>
            <span v-if="login.view.value?.Status === 'requires_approval'" class="hint-action">
              <button type="button" class="btn sm" @click="login.openLoginItems">Open Login Items</button>
            </span>
            <span v-if="login.error.value" class="note" role="alert">{{ login.error.value }}</span>
          </div>
          <div class="ctl">
            <input
              id="login"
              class="switch"
              type="checkbox"
              role="switch"
              aria-describedby="login-hint"
              :checked="login.view.value?.On ?? false"
              :disabled="!login.view.value || login.busy.value || loginLocked(login.view.value.Status)"
              @change="setLogin($event)"
            />
          </div>
        </div>
        <div class="row">
          <div class="what">
            <span class="name">Appearance</span>
            <span class="hint">System follows your {{ isMac() ? 'Mac’s' : 'desktop’s' }} light or dark setting.</span>
          </div>
          <div class="ctl"><ThemeSwitch /></div>
        </div>
        <div class="row">
          <div class="what">
            <label class="name" for="menubar-count">{{ isMac() ? 'Show the unread count in the menu bar' : 'Show the unread count on the tray icon' }}</label>
            <span class="hint">The number {{ isMac() ? 'beside' : 'on' }} Visor’s {{ trayIcon() }}. The bell and the icon’s menu show it either way.</span>
          </div>
          <div class="ctl">
            <input id="menubar-count" class="switch" type="checkbox" role="switch" :checked="s?.menuBarCount ?? true" @change="setMenuBarCount(($event.target as HTMLInputElement).checked)" />
          </div>
        </div>
      </div>

      <!-- Connection -->
      <div class="card c-conn" role="group" aria-labelledby="conn-title">
        <div class="card-head">
          <h2 id="conn-title" class="card-title">Connection</h2>
          <span :class="['badge', `b-${status.tone}`]"><span :class="['dot', `d-${status.tone}`]" aria-hidden="true" />{{ status.text }}</span>
        </div>

        <div class="row wide">
          <div class="what">
            <span class="name">Config file</span>
            <span class="hint">
              <template v-if="envSet">Set by <span class="mono">VISOR_CONFIG</span>. Unset it to choose a file here.</template>
              <template v-else-if="!conn.configPath">Auto-detect looks in the usual {{ isMac() ? 'Homebrew and system' : 'system (/etc/supervisor, /etc)' }} locations.</template>
              <template v-else>The supervisord.conf Visor reads.</template>
            </span>
          </div>
          <div class="ctl end">
            <span class="mono value" data-testid="config-path">{{
              envSet ? view.EnvConfigPath : conn.configPath || (view.Settings.configPath === '' && view.ConfigPath ? `Auto-detect: ${view.ConfigPath}` : 'Auto-detect')
            }}</span>
            <span class="btns">
              <button type="button" class="btn sm" :disabled="envSet || connBusy" @click="choose">Choose…</button>
              <button type="button" class="btn sm" :disabled="envSet || connBusy || !conn.configPath" @click="autoDetect">Use auto-detect</button>
            </span>
          </div>
        </div>

        <div class="row wide">
          <div class="what">
            <span class="name">Socket</span>
            <span class="hint">Read from the config’s <span class="mono">[unix_http_server]</span> or <span class="mono">[inet_http_server]</span> section.</span>
          </div>
          <div class="ctl end">
            <span v-if="view.Socket" class="mono value">{{ view.Socket }}</span>
            <span v-else class="hint">None found</span>
          </div>
        </div>

        <div class="row">
          <div class="what">
            <label class="name" for="conn-user">Username</label>
            <span class="hint">Leave empty to use the <span class="mono">[supervisorctl]</span> username and password in the config.</span>
          </div>
          <div class="ctl">
            <input id="conn-user" v-model.trim="conn.username" class="input field" type="text" autocomplete="off" spellcheck="false" placeholder="From the config" />
          </div>
        </div>

        <div class="row">
          <div class="what">
            <label class="name" for="conn-pass">Password</label>
            <span class="hint">
              Kept in your {{ secretStore() }}, never in a file.<template v-if="view.HasPassword"> Removing it also clears the username, so the config’s credentials apply again.</template>
            </span>
          </div>
          <div v-if="view.HasPassword && !changingPassword" class="ctl">
            <span class="pw-saved" data-testid="password-saved">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="11" width="16" height="10" rx="2" /><path d="M8 11V7a4 4 0 0 1 8 0v4" /></svg>
              {{ view.PasswordThisSession ? 'Kept until Visor quits: no keyring is running' : `Saved in your ${secretStore()}` }}
            </span>
            <button type="button" class="btn sm" :disabled="connBusy" @click="changePassword">Change…</button>
            <button type="button" class="btn sm" :disabled="connBusy" @click="removePassword">Remove password</button>
          </div>
          <div v-else class="ctl">
            <input
              id="conn-pass"
              ref="passwordBox"
              v-model="conn.password"
              class="input field"
              type="password"
              autocomplete="off"
              :placeholder="view.HasPassword ? 'New password' : 'Optional'"
            />
          </div>
        </div>

        <div v-if="connError" class="alert inset" role="alert">
          <strong>Can’t save the connection</strong>
          <p>{{ connError }}</p>
        </div>
        <p v-else-if="serverError" class="server-error" role="status">{{ describeError(serverError) }}</p>

        <div class="foot">
          <button
            type="button"
            class="btn"
            :disabled="connBusy || connDirty"
            :title="connDirty ? 'Save and reconnect to use your changes' : undefined"
            @click="reconnect"
          >
            {{ connBusy && !connDirty ? 'Reconnecting…' : 'Reconnect' }}
          </button>
          <template v-if="connDirty">
            <button type="button" class="btn" :disabled="connBusy" @click="cancelConnection">Cancel</button>
            <button type="button" class="btn primary" :disabled="connBusy" @click="saveConnection">{{ connBusy ? 'Reconnecting…' : 'Save and reconnect' }}</button>
          </template>
        </div>
      </div>

      <!-- Notifications -->
      <div class="card c-notify" role="group" aria-labelledby="notify-title">
        <h2 id="notify-title" class="card-title">Notifications</h2>
        <div class="row">
          <div class="what">
            <label class="name" for="notify-on">Send notifications</label>
            <span class="hint">Only for configs you watch. Issues are recorded either way.</span>
          </div>
          <div class="ctl">
            <input id="notify-on" class="switch" type="checkbox" role="switch" :checked="notify.enabled" @change="setNotify('enabled', ($event.target as HTMLInputElement).checked)" />
          </div>
        </div>
        <fieldset class="types" :disabled="!notify.enabled">
          <legend class="sr-only">Notify me when</legend>
          <label v-for="t in notifyTypes" :key="t.key" class="check">
            <input type="checkbox" :checked="notify[t.key]" @change="setNotify(t.key, ($event.target as HTMLInputElement).checked)" />
            {{ t.label }}
          </label>
        </fieldset>
        <div class="row">
          <div class="what">
            <span class="name">{{ isMac() ? 'macOS permission' : 'Desktop banners' }}</span>
            <span class="hint">{{ isMac() ? 'Visor asks once, the first time it runs.' : 'Shown by your desktop’s notification service.' }}</span>
          </div>
          <div class="ctl">
            <span :class="['badge', 'wrap', `b-${permissionView.tone}`]" data-testid="notify-permission"><span :class="['dot', `d-${permissionView.tone}`]" aria-hidden="true" />{{ permissionView.text }}</span>
            <button type="button" class="btn sm" :disabled="testBusy" @click="sendTest">Send a test notification</button>
          </div>
        </div>
        <div v-if="platform().systemSettings" class="row">
          <div class="what">
            <span class="name">Keep them on screen</span>
            <span class="hint">
              macOS decides how long they stay. Visor asks for Persistent (they stay until you close them) when it first runs; choose
              Temporary in System Settings → Notifications → Visor to have them slide away.
            </span>
          </div>
          <div class="ctl">
            <button type="button" class="btn sm" @click="openNotificationSettings">Open Notification Settings</button>
          </div>
        </div>
      </div>

      <!-- Checking and history (was Polling, Crash loops and Issue history) -->
      <div class="card c-check" role="group" aria-labelledby="check-title">
        <h2 id="check-title" class="card-title">Checking and history</h2>
        <div class="row">
          <div class="what">
            <label class="name" for="poll">Check supervisord every</label>
            <span class="hint">1 to 10 seconds. Shorter shows changes sooner.</span>
            <span v-if="notes.poll" class="note" role="status">{{ notes.poll }}</span>
          </div>
          <div class="ctl">
            <input id="poll" v-model="fields.poll" class="input num" @input="editing.add('poll')" type="number" inputmode="decimal" :min="LIMITS.pollSeconds.min" :max="LIMITS.pollSeconds.max" step="1" @change="savePoll" />
            <span class="unit">seconds</span>
          </div>
        </div>
        <div class="row">
          <div class="what">
            <span class="name">Open a crash-loop issue after</span>
            <span class="hint">Restarts you ask for don’t count.</span>
            <span v-if="notes.restarts" class="note" role="status">{{ notes.restarts }}</span>
            <span v-if="notes.window" class="note" role="status">{{ notes.window }}</span>
          </div>
          <div class="ctl">
            <input id="loop-restarts" v-model="fields.restarts" @input="editing.add('restarts')" aria-label="Restarts" class="input num" type="number" inputmode="numeric" :min="LIMITS.restarts.min" :max="LIMITS.restarts.max" step="1" @change="saveRestarts" />
            <span class="unit">restarts within</span>
            <input id="loop-window" v-model="fields.window" @input="editing.add('window')" aria-label="Minutes" class="input num" type="number" inputmode="numeric" :min="LIMITS.windowMinutes.min" :max="LIMITS.windowMinutes.max" step="1" @change="saveWindow" />
            <span class="unit">minutes</span>
          </div>
        </div>
        <div class="row">
          <div class="what">
            <label class="name" for="keep">Keep resolved issues and process history for</label>
            <span class="hint">At least 7 days. Open issues are always kept. A shorter time deletes older entries once you confirm.</span>
            <span v-if="notes.retention" class="note" role="status">{{ notes.retention }}</span>
          </div>
          <div class="ctl">
            <input id="keep" v-model="fields.retention" class="input num" @input="editing.add('retention')" type="number" inputmode="numeric" :min="LIMITS.retentionDays.min" :max="LIMITS.retentionDays.max" step="1" @keydown.enter="applyRetention" @keydown.esc="cancelRetention" />
            <span class="unit">days</span>
            <template v-if="retention.kind === 'raise' || retention.kind === 'lower'">
              <button type="button" class="btn sm" :disabled="retentionBusy" @click="cancelRetention">Cancel</button>
              <button type="button" class="btn sm primary" :disabled="retentionBusy" @click="applyRetention">{{ retentionBusy ? 'Applying…' : 'Apply' }}</button>
            </template>
          </div>
        </div>
      </div>
    </div>

    <ConfirmDialog
      v-if="confirmRetention"
      :title="confirmRetention.title"
      :message="confirmRetention.message"
      :confirm-label="confirmRetention.confirmLabel"
      @confirm="sendRetention(confirmRetention.days)"
      @cancel="confirmRetention = null"
    />
  </section>
</template>

<style scoped>
/* Board 12 (SettingsNew): cards whose rows have the label and help on the
   left and the control on the right, like System Settings. One column of
   cards up to 860 px; a wide window puts them in two columns (General and
   Notifications on the left, Connection and Checking on the right). */
.page { display: flex; flex-direction: column; gap: 18px; max-width: 1480px; container-type: inline-size; }
header, .page > .alert, .page > .warn { max-width: 860px; }
header { display: flex; flex-direction: column; gap: 6px; }
.title { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
h1 { margin: 0; font-family: var(--display); font-optical-sizing: auto; font-size: 30px; font-weight: 650; line-height: 1.1; letter-spacing: -0.015em; }
.intro { margin: 0; color: var(--ink2); }
.cards { display: flex; flex-direction: column; gap: 16px; max-width: 860px; }
@container (min-width: 1000px) {
  .cards { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; max-width: none; }
  .c-gen { grid-column: 1; grid-row: 1; }
  .c-conn { grid-column: 2; grid-row: 1 / span 2; }
  .c-notify { grid-column: 1; grid-row: 2 / span 2; }
  .c-check { grid-column: 2; grid-row: 3; }
}
.card { border: 1px solid var(--line); border-radius: 10px; background: var(--raised); }
.card-title { margin: 0; font-size: 15px; font-weight: 600; }
.card > .card-title { padding: 14px 18px 4px; }
.card-head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 10px; padding: 14px 18px 4px; }
.row { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; column-gap: 16px; row-gap: 8px; padding: 12px 18px; border-top: 1px solid var(--line); }
.row.wide { grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr); }
.card-title + .row, .card-head + .row { border-top: 0; }
/* A narrow window stacks each row: the control goes under its label. */
@container (max-width: 600px) {
  .row, .row.wide { grid-template-columns: minmax(0, 1fr); }
  .ctl, .ctl.end { justify-content: flex-start; align-items: flex-start; }
  .ctl.end .value { text-align: left; }
}
.what { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.name { font-weight: 600; }
.hint { color: var(--ink3); font-size: 12.5px; }
.hint-action { padding-top: 4px; }
.note { color: var(--warn); font-size: 12.5px; }
.ctl { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 8px; min-width: 0; }
.ctl.end { flex-direction: column; align-items: flex-end; gap: 6px; }
.btns { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
.mono { font-family: var(--mono); font-size: 12.5px; }
.value { text-align: right; overflow-wrap: anywhere; user-select: text; }
.pw-saved { display: inline-flex; align-items: center; gap: 6px; color: var(--ink2); }
.field { width: 260px; max-width: 100%; }
/* Number fields: narrow, right-aligned, tabular, the unit after. */
.num { width: 64px; text-align: right; font-variant-numeric: tabular-nums; }
.num::-webkit-inner-spin-button, .num::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
.unit { color: var(--ink2); }
.switch { appearance: none; position: relative; flex: none; width: 38px; height: 22px; margin: 0; border-radius: 999px; background: var(--line2); cursor: pointer; transition: background 0.15s; }
.switch::after { content: ''; position: absolute; top: 2px; left: 2px; width: 18px; height: 18px; border-radius: 50%; background: var(--on-accent); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.25); transition: transform 0.15s; }
.switch:checked { background: var(--accent); }
.switch:checked::after { transform: translateX(16px); }
.switch:disabled { opacity: 0.55; cursor: default; }
/* The four kinds sit under "Send notifications" and dim with it. */
.types { display: flex; flex-direction: column; gap: 8px; margin: 0; padding: 0 18px 12px 34px; border: 0; min-width: 0; }
.types:disabled { opacity: 0.55; }
.check { display: flex; align-items: center; gap: 8px; }
.check input { width: 15px; height: 15px; margin: 0; accent-color: var(--accent); }
.foot { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; padding: 10px 18px 14px; border-top: 1px solid var(--line); }
.badge.wrap { height: auto; min-height: 22px; padding: 2px 8px; white-space: normal; }
.alert { margin: 0; padding: 12px; border: 1px solid var(--error-card-border); border-radius: var(--radius-md); background: var(--bad-soft); color: var(--bad); }
.alert.inset { margin: 0 18px 12px; }
.alert p, .warn p { margin: 4px 0 0; user-select: text; }
.warn { margin: 0; padding: 12px; border: 1px solid var(--warn-dot); border-radius: var(--radius-md); background: var(--warn-soft); color: var(--warn); }
.server-error { margin: 0; padding: 0 18px 12px; color: var(--bad); font-size: 12.5px; user-select: text; }
.sr-only { position: absolute; left: -9999px; }
</style>
