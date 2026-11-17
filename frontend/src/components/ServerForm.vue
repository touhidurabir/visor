<script setup lang="ts">
// Add or edit a server (Remote servers): name, tag, colour, host, port,
// user, password, SSH key, Use sudo with its own password, Read only, and
// under Advanced the config path and a jump host. Test connects with the
// form as it is (nothing saved) and shows the capability report; Save stores
// it; Connect saves it and opens its tab (Reconnect, when it is open);
// Connect without saving opens a tab and keeps nothing. A server that can't
// connect opens no tab: the form stays, with the report saying why.
// Secrets go to the Keychain or keyring: an empty field keeps what is saved.
import { computed, nextTick, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { RemoteService, type ReportView, type SavedServerView, type SecretInput } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Server } from '../../bindings/github.com/touhidurabir/visor/internal/settings/models'
import { errorMessage } from '../lib/overview'
import { secretStore } from '../lib/platform'
import { closeServer, COLORS, connectOnce, hostKeyPrompt, openServer, TAGS, updateTab, type OpenResult } from '../lib/servers'
import { serverTabs } from '../lib/tabs'
import { showToast } from '../lib/toast'
import DialogShell from './DialogShell.vue'
import MuteChoices from './MuteChoices.vue'
import ReportRows from './ReportRows.vue'

const props = defineProps<{ editing: SavedServerView | null; report?: ReportView }>()
const emit = defineEmits<{ close: []; saved: [server: Server] }>()
const router = useRouter()

const e = props.editing
const form = reactive<Server>({
  id: e?.Server.id ?? '',
  name: e?.Server.name ?? '',
  tag: e?.Server.tag ?? 'development',
  color: e?.Server.color ?? COLORS[1].hex,
  host: e?.Server.host ?? '',
  port: e?.Server.port ?? 22,
  user: e?.Server.user ?? '',
  keyPath: e?.Server.keyPath ?? '',
  jump: e?.Server.jump ?? '',
  useSudo: e?.Server.useSudo ?? false,
  sudoPassword: e?.Server.sudoPassword ?? 'same',
  configPath: e?.Server.configPath ?? '',
  readOnly: e?.Server.readOnly ?? false,
  mutes: e?.Server.mutes ?? { processes: false, programs: false, files: false, daemon: false },
})
// Typed secrets; untouched ones keep what is saved.
const secrets = reactive({ password: '', keyPassphrase: '', sudo: '' })
const touched = reactive({ password: false, keyPassphrase: false, sudo: false })
const saved = { password: !!e?.HasPassword, keyPassphrase: !!e?.HasKeyPassphrase, sudo: !!e?.HasSudoPassword }

const report = ref<ReportView | null>(props.report ?? null)
// Where the report came from: a Test, or a Connect that failed.
const reportFrom = ref<'test' | 'connect'>(props.report ? 'connect' : 'test')
const testing = ref(false)
const saving = ref(false)
const error = ref('')
const testedIn = ref(0)
const result = ref<HTMLElement>()

const title = computed(() => (!e ? 'Add a server' : e.Unsaved ? `Save ${e.Server.name}` : `Edit ${e.Server.name}`))
const passed = computed(() => !!report.value && !report.value.Blocked)
const isOpen = computed(() => !!form.id && serverTabs.value.some((t) => t.id === form.id))
const heading = computed(() => {
  if (!report.value) return ''
  if (report.value.HostKey) return 'Waiting for you to trust the host key'
  if (reportFrom.value === 'connect') return 'Visor couldn’t connect'
  return passed.value ? 'Test passed' : 'Test found a problem'
})

async function showResult() {
  await nextTick()
  result.value?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
}

function secret(kind: keyof typeof secrets): SecretInput {
  if (!touched[kind] && saved[kind]) return { Keep: true, Value: '' }
  return { Keep: false, Value: secrets[kind] }
}

function input() {
  return { Server: { ...form, port: Number(form.port) || 22 }, Password: secret('password'), KeyPassphrase: secret('keyPassphrase'), SudoPassword: secret('sudo') }
}

function placeholder(kind: keyof typeof secrets, empty: string): string {
  return saved[kind] && !touched[kind] ? `Saved in your ${secretStore()}` : empty
}

async function test() {
  testing.value = true
  error.value = ''
  const t0 = performance.now()
  try {
    const r = await RemoteService.Test(input())
    testedIn.value = (performance.now() - t0) / 1000
    if (r.HostKey?.Status === 'unknown') {
      hostKeyPrompt.value = { view: r.HostKey, server: form.name || form.host, then: () => void test() }
    }
    report.value = r
    reportFrom.value = 'test'
    void showResult()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    testing.value = false
  }
}

/**
 * save stores the server. With connect (Connect, Reconnect), or when its
 * open tab can't connect, it then (re)opens the tab; one that can't connect
 * keeps the form open with the report. Saved either way.
 */
async function save(connect: boolean) {
  saving.value = true
  error.value = ''
  try {
    const s = await RemoteService.Save(input())
    form.id = s.id
    updateTab(s)
    emit('saved', s)
    if (connect || (isOpen.value && e?.Conn && e.Conn !== 'connected')) {
      if (isOpen.value) await closeServer(router, s.id)
      done(await openServer(router, s, () => void save(true)))
    } else {
      showToast(`${s.name} saved`)
      emit('close')
    }
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}

/** connectWithoutSaving opens a tab for the form's server and keeps nothing. */
async function connectWithoutSaving() {
  saving.value = true
  error.value = ''
  try {
    done(await connectOnce(router, input(), () => void connectWithoutSaving()))
  } finally {
    saving.value = false
  }
}

function done(r: OpenResult) {
  if (r.ok) return emit('close')
  if (r.error) error.value = r.error
  if (r.report) {
    report.value = r.report
    reportFrom.value = 'connect'
    void showResult()
  }
}

async function chooseKey() {
  try {
    const p = await RemoteService.ChooseKey()
    if (p) form.keyPath = p
  } catch (err) {
    error.value = errorMessage(err)
  }
}

</script>

<template>
  <DialogShell :title="title" wide @close="emit('close')">
    <p class="muted intro">Visor connects over SSH and talks to supervisord there. Nothing is installed on the server.</p>
    <form class="form" aria-label="Server" @submit.prevent="save(true)">
      <div class="row">
        <label class="lab" for="sf-name">Name</label>
        <input id="sf-name" v-model="form.name" class="input" autocomplete="off" data-autofocus placeholder="My server" />
      </div>
      <div class="row">
        <label class="lab" for="sf-tag">Tag</label>
        <div class="stack">
          <select id="sf-tag" v-model="form.tag" class="input narrow">
            <option v-for="t in TAGS" :key="t.id" :value="t.id">{{ t.label }}</option>
          </select>
          <span class="meta">Production asks before stop, restart and other actions that interrupt processes.</span>
        </div>
      </div>
      <div class="row">
        <span id="sf-col" class="lab">Colour</span>
        <div class="swatches" role="radiogroup" aria-labelledby="sf-col">
          <button
            v-for="c in COLORS"
            :key="c.hex"
            type="button"
            :class="['sw', { on: form.color === c.hex }]"
            role="radio"
            :aria-checked="form.color === c.hex"
            :aria-label="c.name"
            :style="{ background: c.hex }"
            @click="form.color = c.hex"
          />
        </div>
      </div>

      <div class="gap" />

      <div class="row">
        <label class="lab" for="sf-host">Host</label>
        <div class="inline">
          <input id="sf-host" v-model="form.host" class="input grow" autocomplete="off" spellcheck="false" placeholder="203.0.113.24" />
          <label for="sf-port" class="small-lab">Port</label>
          <input id="sf-port" v-model.number="form.port" class="input port" inputmode="numeric" />
        </div>
      </div>
      <div class="row"><span /><span class="help">An IP, a host name, or a Host alias from <span class="mono">~/.ssh/config</span> (its user, port, key and ProxyJump are used).</span></div>
      <div class="row">
        <label class="lab" for="sf-user">User</label>
        <input id="sf-user" v-model="form.user" class="input" autocomplete="off" spellcheck="false" placeholder="username" />
      </div>
      <div class="row">
        <label class="lab" for="sf-pass">Password</label>
        <div class="inline">
          <input
            id="sf-pass"
            v-model="secrets.password"
            class="input grow"
            type="password"
            autocomplete="off"
            :placeholder="placeholder('password', 'Optional when a key works')"
            @input="touched.password = true"
          />
          <span class="meta">Kept in your {{ secretStore() }}</span>
        </div>
      </div>
      <div class="row top">
        <label class="lab" for="sf-key">SSH key</label>
        <div class="stack">
          <div class="inline">
            <input id="sf-key" v-model="form.keyPath" class="input grow mono" spellcheck="false" placeholder="~/.ssh/id_ed25519" />
            <button class="btn sm" type="button" @click="chooseKey">Choose…</button>
            <button v-if="form.keyPath" class="btn quiet icon sm" type="button" aria-label="Clear the key" @click="form.keyPath = ''">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg>
            </button>
          </div>
          <input
            v-model="secrets.keyPassphrase"
            class="input"
            type="password"
            aria-label="Key passphrase"
            autocomplete="off"
            :placeholder="placeholder('keyPassphrase', `Passphrase, if the key has one (${secretStore()})`)"
            @input="touched.keyPassphrase = true"
          />
          <span class="help">Leave the key empty to use <span class="mono">~/.ssh/config</span> and your SSH agent, 1Password’s included.</span>
        </div>
      </div>

      <div class="gap" />

      <div class="row top">
        <span class="lab">Use sudo</span>
        <div class="stack">
          <label class="switch-row">
            <input v-model="form.useSudo" type="checkbox" role="switch" aria-label="Use sudo" />
            <span class="muted">For a supervisord that only root can reach. Visor asks sudo once per connection.</span>
          </label>
          <fieldset v-if="form.useSudo" class="sudo">
            <legend class="meta">sudo password</legend>
            <label><input v-model="form.sudoPassword" type="radio" value="same" /> Same as the SSH password</label>
            <label class="inline">
              <input v-model="form.sudoPassword" type="radio" value="own" /> A different password
              <input
                v-if="form.sudoPassword === 'own'"
                v-model="secrets.sudo"
                class="input grow"
                type="password"
                aria-label="sudo password"
                autocomplete="off"
                :placeholder="placeholder('sudo', 'sudo password')"
                @input="touched.sudo = true"
              />
            </label>
            <label><input v-model="form.sudoPassword" type="radio" value="none" /> No password (NOPASSWD)</label>
          </fieldset>
        </div>
      </div>
      <div class="row">
        <span class="lab">Read only</span>
        <label class="switch-row">
          <input v-model="form.readOnly" type="checkbox" role="switch" aria-label="Read only" />
          <span class="muted">Watch only: no start, stop, restart or other changes from Visor.</span>
        </label>
      </div>
      <details class="adv">
        <summary class="meta">Advanced: config path, jump host, notifications</summary>
        <div class="row">
          <label class="lab" for="sf-conf">Config path</label>
          <input id="sf-conf" v-model="form.configPath" class="input mono" spellcheck="false" placeholder="Found on its own (/etc/supervisor/supervisord.conf, …)" />
        </div>
        <div class="row">
          <label class="lab" for="sf-jump">Jump host</label>
          <input id="sf-jump" v-model="form.jump" class="input mono" spellcheck="false" placeholder="user@bastion:22 (when not set in ~/.ssh/config)" />
        </div>
        <div class="row top">
          <span class="lab">Notifications</span>
          <MuteChoices :mutes="form.mutes" @change="(k, m) => (form.mutes = { ...form.mutes, [k]: m })" />
        </div>
      </details>
    </form>

    <section v-if="report" ref="result" class="test" aria-labelledby="sf-test" :data-passed="passed" role="status">
      <div class="test-head">
        <span :class="['dot', passed ? 'd-ok' : 'd-bad']" aria-hidden="true" />
        <h3 id="sf-test">{{ heading }}</h3>
        <span v-if="reportFrom === 'test'" class="meta">just now · {{ testedIn.toFixed(1) }} s</span>
      </div>
      <ReportRows :rows="report.Rows ?? []" />
    </section>
    <p v-if="error" class="err" role="alert">{{ error }}</p>

    <template #actions>
      <button class="btn quiet" type="button" @click="emit('close')">Cancel</button>
      <span class="spacer" />
      <button class="btn" type="button" :disabled="saving" @click="save(false)">Save</button>
      <button class="btn" type="button" :disabled="testing" @click="test">{{ testing ? 'Testing…' : 'Test' }}</button>
      <button v-if="!e" class="btn" type="button" :disabled="saving" title="Open a tab for this server, and keep nothing once it closes" @click="connectWithoutSaving">Connect without saving</button>
      <button class="btn primary" type="button" :disabled="saving" @click="save(true)">{{ saving ? 'Connecting…' : isOpen ? 'Reconnect' : 'Connect' }}</button>
    </template>
  </DialogShell>
</template>

<style scoped>
.intro { margin: 0 0 12px; }
.form { display: flex; flex-direction: column; }
.row { display: grid; grid-template-columns: 130px minmax(0, 1fr); align-items: center; column-gap: 16px; padding: 6px 0; }
.row.top { align-items: start; }
.row.top .lab { padding-top: 7px; }
.lab { font-weight: 600; text-align: right; }
.small-lab { font-weight: 600; }
.stack { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.inline { display: flex; align-items: center; gap: 8px; min-width: 0; }
.grow { flex: 1; min-width: 0; }
.narrow { max-width: 220px; }
.port { width: 72px; }
.help { color: var(--ink3); font-size: 12px; }
.gap { height: 10px; border-bottom: 1px solid var(--line); margin-bottom: 10px; }
.swatches { display: flex; gap: 8px; }
.sw { width: 26px; height: 26px; border-radius: 7px; border: 2px solid transparent; padding: 0; cursor: pointer; }
.sw.on { border-color: var(--ink); }
.switch-row { display: flex; align-items: center; gap: 10px; }
.sudo { display: flex; flex-direction: column; gap: 6px; margin: 0; padding: 8px 12px; border: 1px solid var(--line); border-radius: 8px; }
.adv { margin-top: 8px; }
.adv summary { cursor: pointer; padding: 4px 0; }
.test { margin-top: 14px; padding: 12px 0 0; border: 1px solid var(--line); border-radius: 10px; scroll-margin: 12px; }
.test-head { display: flex; align-items: center; gap: 8px; margin: 0 16px 6px; }
.test-head h3 { margin: 0; font-size: 14px; }
.test-head .meta { margin-left: auto; }
.err { color: var(--bad); margin: 10px 0 0; user-select: text; }
.spacer { flex: 1; }
</style>
