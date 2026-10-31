<script setup lang="ts">
// Settings (spec §5.8). The connection is saved explicitly with "Save and
// reconnect", because it rebuilds the connection. Everything else saves as
// it changes and applies straight away; Go clamps out-of-range values and
// the page shows what it kept.
import { computed, onMounted, reactive, ref } from 'vue'
import { ServerService, SettingsService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { SettingsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Settings } from '../../bindings/github.com/touhidurabir/visor/internal/settings'
import { describeError } from '../lib/faults'
import { useOverview } from '../lib/overview'
import {
  LIMITS,
  clampNote,
  currentSettings,
  loadSettings,
  minutesToNs,
  nsToMinutes,
  nsToSeconds,
  secondsToNs,
  typedNumber,
} from '../lib/settings'
import { showToast } from '../lib/toast'

const { overview } = useOverview()
const view = ref<SettingsView | null>(null)
const loadError = ref('')

// Connection draft: edited freely, saved with "Save and reconnect".
const conn = reactive({ configPath: '', username: '', password: '' })
const connBusy = ref(false)
const connError = ref('')

// Number fields keep the typed text until saved; notes explain clamping.
const fields = reactive({ poll: '', restarts: '', window: '', retention: '' })
const notes = reactive({ poll: '', restarts: '', window: '', retention: '' })
const saving = ref(false)
const saveError = ref('')

const s = computed<Settings | null>(() => view.value?.Settings ?? null)
const envSet = computed(() => !!view.value?.EnvConfigPath)
const connDirty = computed(() => {
  const v = view.value
  if (!v) return false
  const path = envSet.value ? v.Settings.configPath : conn.configPath
  return path !== v.Settings.configPath || conn.username !== v.Settings.username || conn.password !== ''
})

function adopt(v: SettingsView) {
  view.value = v
  currentSettings.value = v
  conn.configPath = v.Settings.configPath
  conn.username = v.Settings.username
  conn.password = ''
  fields.poll = String(nsToSeconds(v.Settings.pollInterval))
  fields.restarts = String(v.Settings.crashLoop.restarts)
  fields.window = String(nsToMinutes(v.Settings.crashLoop.window))
  fields.retention = String(v.Settings.retentionDays)
}

onMounted(async () => {
  try {
    adopt(await loadSettings())
  } catch (e) {
    loadError.value = describeError(e)
  }
})

// --- Connection ---------------------------------------------------------

const status = computed(() => {
  const sv = overview.value?.Server
  if (!sv || sv.Health === 'unknown') return { cls: 'stopped', text: 'Connecting…' }
  if (sv.Reachable) return { cls: 'running', text: sv.Version ? `Connected to supervisord ${sv.Version}` : 'Connected' }
  return { cls: 'failing', text: 'Not connected' }
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

async function saveConnection(opts: { forgetPassword?: boolean } = {}) {
  if (connBusy.value) return
  connBusy.value = true
  connError.value = ''
  try {
    const setPassword = opts.forgetPassword || conn.password !== ''
    const v = await SettingsService.SaveConnection({
      ConfigPath: conn.configPath,
      Username: conn.username,
      Password: opts.forgetPassword ? '' : conn.password,
      SetPassword: setPassword,
    })
    adopt(v)
    showToast(opts.forgetPassword ? 'Password removed from your Keychain' : 'Saved. Visor reconnected to supervisord.')
  } catch (e) {
    connError.value = describeError(e)
  } finally {
    connBusy.value = false
  }
}

async function reconnect() {
  if (connBusy.value) return
  connBusy.value = true
  connError.value = ''
  try {
    const ov = await ServerService.Reconnect()
    adopt(await loadSettings())
    showToast(ov.Server.Reachable ? 'Reconnected to supervisord' : 'Reconnected, but supervisord is not answering', ov.Server.Reachable ? 'info' : 'error')
  } catch (e) {
    connError.value = describeError(e)
  } finally {
    connBusy.value = false
  }
}

// --- Everything else: saved as it changes -------------------------------

async function save(change: (next: Settings) => void, after?: (v: SettingsView) => void) {
  if (!view.value) return
  const next: Settings = JSON.parse(JSON.stringify(view.value.Settings))
  change(next)
  saving.value = true
  saveError.value = ''
  try {
    const v = await SettingsService.Save(next)
    const keep = { ...conn }
    adopt(v)
    Object.assign(conn, keep) // an unsaved connection draft survives
    after?.(v)
  } catch (e) {
    saveError.value = describeError(e)
  } finally {
    saving.value = false
  }
}

function savePoll() {
  const typed = typedNumber(fields.poll)
  if (typed === null) return void (fields.poll = String(nsToSeconds(view.value!.Settings.pollInterval)))
  void save(
    (n) => (n.pollInterval = secondsToNs(typed)),
    (v) => (notes.poll = clampNote(typed, nsToSeconds(v.Settings.pollInterval), LIMITS.pollSeconds, 'second')),
  )
}

function saveCrashLoop() {
  const restarts = typedNumber(fields.restarts)
  const minutes = typedNumber(fields.window)
  const cur = view.value!.Settings.crashLoop
  void save(
    (n) => {
      n.crashLoop.restarts = restarts === null ? cur.restarts : Math.round(restarts)
      n.crashLoop.window = minutes === null ? cur.window : minutesToNs(minutes)
    },
    (v) => {
      notes.restarts = clampNote(restarts, v.Settings.crashLoop.restarts, LIMITS.restarts, 'restart')
      notes.window = clampNote(minutes, nsToMinutes(v.Settings.crashLoop.window), LIMITS.windowMinutes, 'minute')
    },
  )
}

function saveRetention() {
  const typed = typedNumber(fields.retention)
  if (typed === null) return void (fields.retention = String(view.value!.Settings.retentionDays))
  void save(
    (n) => (n.retentionDays = Math.round(typed)),
    (v) => (notes.retention = clampNote(typed, v.Settings.retentionDays, LIMITS.retentionDays, 'day')),
  )
}

type NotifyKey = 'enabled' | 'fatal' | 'unexpectedExit' | 'crashLoop' | 'unreachable'
function setNotify(key: NotifyKey, on: boolean) {
  void save((n) => (n.notify[key] = on))
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
      <div>
        <h1 id="settings-title">Settings</h1>
        <p class="intro">How Visor connects to supervisord, how often it checks, and what it tells you about. Changes save as you make them.</p>
      </div>
      <span class="saved" aria-live="polite">{{ saving ? 'Saving…' : view ? 'All changes saved' : '' }}</span>
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

    <template v-if="view && s">
      <!-- Connection -->
      <div class="card" role="group" aria-labelledby="conn-title">
        <div class="card-head">
          <h2 id="conn-title">Connection</h2>
          <span :class="['badge', status.cls]"><span class="dot" aria-hidden="true" />{{ status.text }}</span>
        </div>

        <div class="row stack">
          <div class="what">
            <label class="name" for="conf-path">Config file</label>
            <p class="hint">
              <template v-if="envSet">Set by VISOR_CONFIG. Unset it to choose a file here.</template>
              <template v-else-if="!conn.configPath">Auto-detect looks in the usual Homebrew and system locations.</template>
              <template v-else>The supervisord.conf Visor reads.</template>
            </p>
          </div>
          <div class="ctl wide">
            <input
              id="conf-path"
              class="mono"
              type="text"
              readonly
              :disabled="envSet"
              :value="envSet ? view.EnvConfigPath : conn.configPath || (view.Settings.configPath === '' && view.ConfigPath ? `Auto-detect: ${view.ConfigPath}` : 'Auto-detect')"
            />
            <div class="btns">
              <span v-if="envSet" class="badge stopped">Set by VISOR_CONFIG</span>
              <button type="button" class="btn" :disabled="envSet || connBusy" @click="choose">Choose…</button>
              <button type="button" class="btn" :disabled="envSet || connBusy || !conn.configPath" @click="autoDetect">Use auto-detect</button>
            </div>
          </div>
        </div>

        <div class="row">
          <div class="what">
            <span class="name">Socket</span>
            <p class="hint">Read from the config’s [unix_http_server] or [inet_http_server] section.</p>
          </div>
          <div class="ctl wide">
            <span v-if="view.Socket" class="mono value">{{ view.Socket }}</span>
            <span v-else class="hint">None found</span>
          </div>
        </div>

        <div class="row">
          <div class="what">
            <label class="name" for="conn-user">Username</label>
            <p class="hint">Leave empty to use the [supervisorctl] username and password in the config.</p>
          </div>
          <div class="ctl">
            <input id="conn-user" v-model.trim="conn.username" type="text" autocomplete="off" spellcheck="false" placeholder="From the config" />
          </div>
        </div>

        <div class="row">
          <div class="what">
            <label class="name" for="conn-pass">Password</label>
            <p class="hint">Saved in your Keychain, never in a file.</p>
          </div>
          <div class="ctl">
            <input
              id="conn-pass"
              v-model="conn.password"
              type="password"
              autocomplete="off"
              :placeholder="view.HasPassword ? 'Saved in your Keychain' : 'Not set'"
            />
            <button v-if="view.HasPassword" type="button" class="btn" :disabled="connBusy" @click="saveConnection({ forgetPassword: true })">Remove</button>
          </div>
        </div>

        <div v-if="connError" class="alert" role="alert">
          <strong>Can’t save the connection</strong>
          <p>{{ connError }}</p>
        </div>
        <p v-else-if="serverError" class="server-error" role="status">{{ describeError(serverError) }}</p>

        <div class="foot">
          <template v-if="connDirty">
            <button type="button" class="btn" :disabled="connBusy" @click="cancelConnection">Cancel</button>
            <button type="button" class="btn primary" :disabled="connBusy" @click="saveConnection()">{{ connBusy ? 'Reconnecting…' : 'Save and reconnect' }}</button>
          </template>
          <button v-else type="button" class="btn" :disabled="connBusy" @click="reconnect">{{ connBusy ? 'Reconnecting…' : 'Reconnect' }}</button>
        </div>
      </div>

      <!-- Polling -->
      <div class="card" role="group" aria-labelledby="poll-title">
        <h2 id="poll-title">Polling</h2>
        <div class="row">
          <div class="what">
            <label class="name" for="poll">Check supervisord every</label>
            <p class="hint">1 to 10 seconds. Shorter shows changes sooner.</p>
            <p v-if="notes.poll" class="note" role="status">{{ notes.poll }}</p>
          </div>
          <div class="ctl">
            <input id="poll" v-model="fields.poll" class="num" type="number" inputmode="decimal" :min="LIMITS.pollSeconds.min" :max="LIMITS.pollSeconds.max" step="1" @change="savePoll" />
            <span class="unit">seconds</span>
          </div>
        </div>
      </div>

      <!-- Notifications -->
      <div class="card" role="group" aria-labelledby="notify-title">
        <h2 id="notify-title">Notifications</h2>
        <div class="row">
          <div class="what">
            <label class="name" for="notify-on">Send notifications</label>
            <p class="hint">Only for configs you watch. Issues are recorded either way.</p>
          </div>
          <div class="ctl">
            <input id="notify-on" class="switch" type="checkbox" role="switch" :checked="s.notify.enabled" @change="setNotify('enabled', ($event.target as HTMLInputElement).checked)" />
          </div>
        </div>
        <fieldset class="types" :disabled="!s.notify.enabled">
          <legend class="sr-only">Notify me when</legend>
          <label v-for="t in notifyTypes" :key="t.key" class="check">
            <input type="checkbox" :checked="s.notify[t.key]" @change="setNotify(t.key, ($event.target as HTMLInputElement).checked)" />
            {{ t.label }}
          </label>
        </fieldset>
      </div>

      <!-- Crash loops -->
      <div class="card" role="group" aria-labelledby="loop-title">
        <h2 id="loop-title">Crash loops</h2>
        <div class="row">
          <div class="what">
            <span class="name">Open a crash-loop issue after</span>
            <p class="hint">Restarts you ask for don’t count.</p>
            <p v-if="notes.restarts" class="note" role="status">{{ notes.restarts }}</p>
            <p v-if="notes.window" class="note" role="status">{{ notes.window }}</p>
          </div>
          <div class="ctl">
            <input id="loop-restarts" v-model="fields.restarts" aria-label="Restarts" class="num" type="number" inputmode="numeric" :min="LIMITS.restarts.min" :max="LIMITS.restarts.max" step="1" @change="saveCrashLoop" />
            <span class="unit">restarts within</span>
            <input id="loop-window" v-model="fields.window" aria-label="Minutes" class="num" type="number" inputmode="numeric" :min="LIMITS.windowMinutes.min" :max="LIMITS.windowMinutes.max" step="1" @change="saveCrashLoop" />
            <span class="unit">minutes</span>
          </div>
        </div>
      </div>

      <!-- Issue history -->
      <div class="card" role="group" aria-labelledby="keep-title">
        <h2 id="keep-title">Issue history</h2>
        <div class="row">
          <div class="what">
            <label class="name" for="keep">Keep resolved issues and process history for</label>
            <p class="hint">At least 7 days. Open issues are always kept; a shorter time clears older entries straight away.</p>
            <p v-if="notes.retention" class="note" role="status">{{ notes.retention }}</p>
          </div>
          <div class="ctl">
            <input id="keep" v-model="fields.retention" class="num" type="number" inputmode="numeric" :min="LIMITS.retentionDays.min" :max="LIMITS.retentionDays.max" step="1" @change="saveRetention" />
            <span class="unit">days</span>
          </div>
        </div>
      </div>

      <!-- Startup and appearance -->
      <div class="card" role="group" aria-labelledby="general-title">
        <h2 id="general-title">General</h2>
        <div class="row">
          <div class="what">
            <label class="name" for="login">Launch at login</label>
            <p class="hint">Coming in this release.</p>
          </div>
          <div class="ctl">
            <input id="login" class="switch" type="checkbox" role="switch" disabled />
          </div>
        </div>
        <div class="row">
          <div class="what">
            <span class="name">Appearance</span>
            <p class="hint">Follows your Mac’s light/dark setting.</p>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 16px; max-width: 760px; }
header { display: flex; flex-wrap: wrap; align-items: flex-end; justify-content: space-between; gap: 12px; }
h1 { margin: 0; font-size: 22px; font-weight: 650; }
h2 { margin: 0; font-size: 14px; font-weight: 650; }
.intro { margin: 4px 0 0; max-width: 600px; color: var(--text-muted); }
.saved { color: var(--text-muted); font-size: 12px; }
.card { border: 1px solid var(--line); border-radius: 10px; padding: 16px; display: flex; flex-direction: column; gap: 10px; }
.card-head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 8px; }
.row { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 16px; padding-top: 10px; border-top: 1px solid var(--stopped-bg); }
.card > h2 + .row, .card-head + .row { border-top: 0; padding-top: 2px; }
.what { flex: 1 1 260px; min-width: 0; }
.name { font-weight: 600; }
.hint { margin: 2px 0 0; color: var(--text-muted); font-size: 12px; }
.note { margin: 4px 0 0; color: var(--warning-fg); font-size: 12px; }
.ctl { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.ctl.wide { flex: 1 1 320px; justify-content: flex-end; }
.stack { flex-direction: column; align-items: stretch; }
.stack .what { flex: none; }
.stack .ctl.wide { flex: none; justify-content: flex-start; }
.ctl.wide input { flex: 1 1 260px; min-width: 0; }
.btns { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.mono { font-family: var(--font-mono); font-size: 12px; }
.value { overflow-wrap: anywhere; user-select: text; }
.unit { color: var(--text-muted); }
input[type='text'], input[type='password'], input[type='number'] { min-height: 32px; box-sizing: border-box; padding: 0 10px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; }
input[type='text'], input[type='password'] { width: 220px; }
input.mono { font-family: var(--font-mono); font-size: 12px; }
input[readonly] { background: var(--bg-subtle); }
input:disabled { color: var(--text-muted); }
input.num { width: 72px; text-align: right; }
.switch { appearance: none; position: relative; width: 38px; height: 22px; margin: 0; border-radius: 999px; background: var(--line-strong); cursor: pointer; transition: background 0.15s; }
.switch::after { content: ''; position: absolute; top: 2px; left: 2px; width: 18px; height: 18px; border-radius: 50%; background: #fff; box-shadow: 0 1px 2px rgba(0, 0, 0, 0.25); transition: transform 0.15s; }
.switch:checked { background: var(--accent); }
.switch:checked::after { transform: translateX(16px); }
.switch:disabled { opacity: 0.5; cursor: default; }
.types { display: flex; flex-direction: column; gap: 8px; margin: 0; padding: 4px 0 0 2px; border: 0; }
.types:disabled { color: var(--text-faint); }
.check { display: flex; align-items: center; gap: 8px; }
.check input { width: 15px; height: 15px; margin: 0; accent-color: var(--accent); }
.foot { display: flex; justify-content: flex-end; gap: 8px; padding-top: 10px; border-top: 1px solid var(--stopped-bg); }
.btn { display: inline-flex; align-items: center; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; white-space: nowrap; cursor: pointer; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.btn.primary { border-color: var(--accent); background: var(--accent); color: var(--on-accent); }
.btn.primary:hover:not(:disabled) { background: var(--accent-hover); }
.badge { display: inline-flex; align-items: center; gap: 6px; padding: 2px 8px; border-radius: 999px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.running { background: var(--running-bg); color: var(--running-fg); }
.running .dot { background: var(--running-dot); }
.failing { background: var(--failing-bg); color: var(--failing-fg); }
.failing .dot { background: var(--failing-dot); }
.stopped { background: var(--stopped-bg); color: var(--stopped-fg); }
.stopped .dot { background: var(--stopped-dot); }
.alert { margin: 0; padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); }
.alert p { margin: 4px 0 0; user-select: text; }
.server-error { margin: 0; color: var(--failing-fg); font-size: 12px; user-select: text; }
.sr-only { position: absolute; left: -9999px; }
</style>
