<script setup lang="ts">
// The MCP server page (M9): lets AI tools (Claude Code, Codex, Cursor) read
// and manage supervisord through Visor. Turn it on, copy a command, choose
// what AI tools may do on each machine, and see their recent calls.
import { Events } from '@wailsio/runtime'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { VisorMcp, VisorMcpCall } from '../../bindings/events'
import * as Mcp from '../../bindings/github.com/touhidurabir/visor/internal/app/mcpservice'
import type { McpView } from '../../bindings/github.com/touhidurabir/visor/internal/app/models'
import type { Call } from '../../bindings/github.com/touhidurabir/visor/internal/mcp/models'
import CommandBlock from '../components/CommandBlock.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { copyText } from '../lib/clipboard'
import { connectSnippets, maskToken } from '../lib/mcp-connect'
import { errorMessage } from '../lib/overview'
import { showToast } from '../lib/toast'

const view = ref<McpView>()
const loadError = ref('')
const busy = ref(false)
const showToken = ref(false)
const portText = ref('')
const tab = ref<'claude' | 'codex' | 'json'>('claude')
const confirm = ref<'' | 'regenerate' | 'claude'>('')
const claudeOut = ref('')

const LEVELS = [
  { value: 'off', label: 'Off' },
  { value: 'read', label: 'Read' },
  { value: 'control', label: 'Control' },
  { value: 'configs', label: 'Change configs' },
]
const levelLabel = (l: string) => LEVELS.find((x) => x.value === l)?.label ?? l
const GROUPS = [
  { level: 'read', title: 'Read' },
  { level: 'control', title: 'Control' },
  { level: 'configs', title: 'Change configs' },
]

const status = computed(() => {
  switch (view.value?.Status) {
    case 'running':
      return { tone: 'ok', text: 'Running', note: '' }
    case 'port_in_use':
      return { tone: 'warn', text: 'Port in use', note: view.value.Error }
    case 'failed':
      return { tone: 'bad', text: 'Not running', note: view.value.Error }
  }
  return { tone: 'idle', text: 'Off', note: 'Turn it on to let AI tools connect.' }
})
const token = computed(() => view.value?.Token ?? '')
const snippets = computed(() => connectSnippets(view.value?.URL ?? '', token.value))
const shown = (text: string) => (showToken.value ? text : maskToken(text, token.value))
const tools = computed(() => view.value?.Tools ?? [])
const calls = computed(() => view.value?.Calls ?? [])

function set(v: McpView) {
  view.value = v
  portText.value = String(v.Port)
}

async function run(fn: () => Promise<McpView>) {
  busy.value = true
  try {
    set(await fn())
  } catch (e) {
    showToast(errorMessage(e), 'error')
    set(await Mcp.Get())
  } finally {
    busy.value = false
  }
}

const setEnabled = (on: boolean) => run(() => Mcp.SetEnabled(on))
const setAllowEnv = (on: boolean) => run(() => Mcp.SetAllowEnv(on))
const setAccess = (id: string, level: string, ask: boolean) => run(() => Mcp.SetAccess(id, level, ask))

function savePort() {
  const port = Number(portText.value)
  if (!view.value || port === view.value.Port) return
  if (!Number.isInteger(port) || port < 1024 || port > 65535) {
    showToast('Choose a port from 1024 to 65535.', 'error')
    portText.value = String(view.value.Port)
    return
  }
  void run(() => Mcp.SetPort(port))
}

async function copyToken() {
  try {
    await copyText(token.value)
    showToast('Token copied', 'info')
  } catch (e) {
    showToast(errorMessage(e), 'error')
  }
}

async function regenerate() {
  confirm.value = ''
  await run(() => Mcp.RegenerateToken())
  showToast('New token made: AI tools need the new command or config.', 'info')
}

async function addToClaude() {
  confirm.value = ''
  busy.value = true
  try {
    const out = await Mcp.AddToClaudeCode()
    claudeOut.value = out || 'Added to Claude Code.'
    showToast('Visor is in Claude Code. Start a new session to use it.', 'info')
  } catch (e) {
    showToast(errorMessage(e), 'error')
  } finally {
    busy.value = false
  }
}

function time(at: string) {
  return new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
function resultText(c: Call) {
  if (c.result === 'ok') return 'OK'
  const what = c.result === 'refused' ? 'Refused' : 'Error'
  return c.detail ? `${what}: ${c.detail}` : what
}

let offs: (() => void)[] = []
onMounted(async () => {
  offs = [
    Events.On(VisorMcp, (ev: { data: McpView }) => set(ev.data)),
    Events.On(VisorMcpCall, (ev: { data: Call }) => {
      if (view.value) view.value.Calls = [ev.data, ...(view.value.Calls ?? [])].slice(0, 100)
    }),
  ]
  try {
    set(await Mcp.Get())
  } catch (e) {
    loadError.value = errorMessage(e)
  }
})
onUnmounted(() => offs.forEach((off) => off()))
</script>

<template>
  <section class="page" aria-labelledby="mcp-title">
    <header>
      <div class="title">
        <h1 id="mcp-title">MCP server</h1>
        <span v-if="view" :class="['badge', `b-${status.tone}`]" data-testid="mcp-status"><span :class="['dot', `d-${status.tone}`]" aria-hidden="true" />{{ status.text }}</span>
      </div>
      <p class="intro">Lets Claude Code, Codex and other AI tools read and manage supervisord through Visor, on this computer and on the servers you allow.</p>
      <p v-if="status.note" :class="status.tone === 'idle' ? 'meta' : 'note'" role="status">{{ status.note }}</p>
    </header>

    <div v-if="loadError" class="alert" role="alert">
      <strong>Can’t load the MCP server</strong>
      <p>{{ loadError }}</p>
    </div>

    <div v-if="view" class="cards">
      <div class="card" role="group" aria-labelledby="cfg-title">
        <h2 id="cfg-title" class="card-title">Configuration</h2>
        <div class="row">
          <div class="what">
            <label class="name" for="mcp-enabled">Enable MCP server</label>
            <span class="hint">Off by default. Visor listens on 127.0.0.1 only, never on your network.</span>
          </div>
          <div class="ctl"><input id="mcp-enabled" class="switch" type="checkbox" role="switch" :checked="view.Enabled" :disabled="busy" @change="setEnabled(($event.target as HTMLInputElement).checked)" /></div>
        </div>
        <div class="row">
          <div class="what">
            <label class="name" for="mcp-port">Port</label>
            <span class="hint mono">{{ view.URL }}</span>
          </div>
          <div class="ctl"><input id="mcp-port" v-model="portText" class="input num-port mono" inputmode="numeric" :disabled="busy" @change="savePort" @keydown.enter="savePort" /></div>
        </div>
        <div class="row token-row">
          <div class="what">
            <label class="name" for="mcp-token">Access token</label>
            <span class="hint">AI tools send it with every request. A new one disconnects them until you add it again.</span>
          </div>
          <div class="ctl-col">
            <div class="ctl">
              <input id="mcp-token" class="input mono token" :type="showToken ? 'text' : 'password'" :value="token" readonly />
              <button type="button" class="btn sm" :aria-pressed="showToken" @click="showToken = !showToken">{{ showToken ? 'Hide' : 'Show' }}</button>
              <button type="button" class="btn sm" @click="copyToken">Copy</button>
              <button type="button" class="btn sm" :disabled="busy" aria-describedby="mcp-regen-hint" @click="confirm = 'regenerate'">Regenerate…</button>
            </div>
            <span id="mcp-regen-hint" class="hint regen" data-testid="mcp-regen-hint">
              After a new token, {{ view.HasClaude ? 'click Add to Claude Code again and copy' : 'copy' }} the new command or config below into your other AI tools.
            </span>
          </div>
        </div>
      </div>

      <div class="card" role="group" aria-labelledby="con-title">
        <div class="card-head">
          <h2 id="con-title" class="card-title">Connecting</h2>
          <button v-if="view.HasClaude" type="button" class="btn sm primary" :disabled="busy" @click="confirm = 'claude'">Add to Claude Code</button>
        </div>
        <p class="meta pad">Each command has your token filled in. Run it once.</p>
        <div class="tabs pad" role="tablist" aria-label="AI tool">
          <button type="button" role="tab" :aria-selected="tab === 'claude'" :class="{ on: tab === 'claude' }" @click="tab = 'claude'">Claude Code</button>
          <button type="button" role="tab" :aria-selected="tab === 'codex'" :class="{ on: tab === 'codex' }" @click="tab = 'codex'">Codex</button>
          <button type="button" role="tab" :aria-selected="tab === 'json'" :class="{ on: tab === 'json' }" @click="tab = 'json'">JSON (Cursor, VS Code, others)</button>
        </div>
        <div class="snips" role="tabpanel" data-testid="mcp-snippets">
          <template v-if="tab === 'claude'">
            <CommandBlock :text="snippets.claude" :shown="shown(snippets.claude)" />
            <p v-if="claudeOut" class="meta" role="status">{{ claudeOut }}</p>
          </template>
          <template v-else-if="tab === 'codex'">
            <p class="meta">Codex reads the token from a variable:</p>
            <CommandBlock :text="snippets.codexEnv" :shown="shown(snippets.codexEnv)" />
            <CommandBlock :text="snippets.codexAdd" />
            <p class="meta">Or add these lines to <span class="mono">~/.codex/config.toml</span>, with the token in them:</p>
            <CommandBlock :text="snippets.codexToml" :shown="shown(snippets.codexToml)" />
          </template>
          <CommandBlock v-else :text="snippets.json" :shown="shown(snippets.json)" />
        </div>
      </div>

      <div class="card" role="group" aria-labelledby="acc-title">
        <h2 id="acc-title" class="card-title">Access</h2>
        <p class="meta pad">What AI tools may do on each machine. Servers are off until you choose. Changes show in Activity as made via the AI tool.</p>
        <div class="scroll">
          <table class="tbl" data-testid="mcp-access">
            <thead><tr><th scope="col">Machine</th><th scope="col">Access</th><th scope="col">Ask in Visor before changes</th><th scope="col"><span class="sr-only">Notes</span></th></tr></thead>
            <tbody>
              <tr v-for="m in view.Machines ?? []" :key="m.ID">
                <td><strong>{{ m.Name }}</strong> <span v-if="m.Tag" :class="['badge', 'tag', m.Tag === 'production' ? 'b-bad' : 'b-idle']">{{ m.Tag }}</span><span v-if="m.ReadOnly" class="badge tag b-idle">Read only</span></td>
                <td>
                  <select class="input sel" :aria-label="`Access for ${m.Name}`" :value="m.Level" :disabled="busy" @change="setAccess(m.ID, ($event.target as HTMLSelectElement).value, m.Ask)">
                    <option v-for="l in LEVELS" :key="l.value" :value="l.value" :disabled="m.MaxLevel === 'read' && (l.value === 'control' || l.value === 'configs')">{{ l.label }}</option>
                  </select>
                </td>
                <td>
                  <input v-if="!m.ReadOnly" class="switch" type="checkbox" role="switch" :aria-label="`Ask before changes on ${m.Name}`" :checked="m.Ask" :disabled="busy || m.AskLocked" @change="setAccess(m.ID, m.Level, ($event.target as HTMLInputElement).checked)" />
                  <span v-else class="meta">n/a</span>
                </td>
                <td class="meta">{{ m.Level === 'off' && !m.Local ? 'Invisible to AI tools' : m.AskLocked ? 'Production always asks' : m.ReadOnly ? 'Read only server: Read at most' : '' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card" role="group" aria-labelledby="priv-title">
        <h2 id="priv-title" class="card-title">Privacy</h2>
        <p class="muted pad">Logs and configs an AI tool reads are sent to your AI provider. Visor can’t filter secrets out of logs.</p>
        <div class="row">
          <div class="what">
            <label class="name" for="mcp-env">Let AI read environment values</label>
            <span class="hint">Off: values in <span class="mono">environment=</span> show as «hidden», and an AI tool can’t save a file that still holds them.</span>
          </div>
          <div class="ctl"><input id="mcp-env" class="switch" type="checkbox" role="switch" :checked="view.AllowEnv" :disabled="busy" @change="setAllowEnv(($event.target as HTMLInputElement).checked)" /></div>
        </div>
      </div>

      <div class="card wide" role="group" aria-labelledby="tools-title">
        <h2 id="tools-title" class="card-title">Available tools</h2>
        <p class="meta pad">{{ tools.length }} tools. Each takes an optional server; leaving it out means this computer.</p>
        <div class="scroll">
          <table class="tbl" data-testid="mcp-tools">
            <thead><tr><th scope="col" class="w-tool">Tool</th><th scope="col">What it does</th><th scope="col" class="w-need">Needs</th></tr></thead>
            <tbody v-for="g in GROUPS" :key="g.level">
              <tr class="grp"><th colspan="3" scope="colgroup">{{ g.title }}</th></tr>
              <tr v-for="t in tools.filter((x) => x.level === g.level)" :key="t.name">
                <td class="mono">{{ t.name }}</td>
                <td>{{ t.description }}</td>
                <td><span :class="['lvl', `l-${t.level}`]">{{ levelLabel(t.level) }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="meta foot">Never through MCP: supervisord.conf, Stop all and Restart all, restarting supervisord, clearing logs, passwords, settings and servers.</p>
      </div>

      <div class="card wide" role="group" aria-labelledby="calls-title">
        <div class="card-head">
          <h2 id="calls-title" class="card-title">Recent calls</h2>
          <span class="meta">The last 100, since Visor started</span>
        </div>
        <p v-if="!calls.length" class="meta pad empty">No calls yet.</p>
        <div v-else class="scroll">
          <table class="tbl" data-testid="mcp-calls">
            <thead><tr><th scope="col">Time</th><th scope="col">Client</th><th scope="col">Tool</th><th scope="col">Machine</th><th scope="col">Result</th></tr></thead>
            <tbody>
              <tr v-for="(c, i) in calls" :key="`${c.at}-${i}`">
                <td class="meta">{{ time(c.at) }}</td>
                <td>{{ c.client }}</td>
                <td class="mono">{{ c.tool }}</td>
                <td>{{ c.machine }}</td>
                <td :class="c.result === 'ok' ? 't-ok' : 't-bad'">{{ resultText(c) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

    </div>

    <ConfirmDialog
      v-if="confirm === 'regenerate'"
      title="Make a new token?"
      message="AI tools using the current token are disconnected until you add the new command or config to them."
      confirm-label="Make a new token"
      @confirm="regenerate"
      @cancel="confirm = ''"
    />
    <ConfirmDialog
      v-if="confirm === 'claude'"
      title="Add Visor to Claude Code?"
      :message="`Visor runs this for your user, replacing an older Visor entry: ${maskToken(snippets.claude, token)}`"
      confirm-label="Add to Claude Code"
      :danger="false"
      @confirm="addToClaude"
      @cancel="confirm = ''"
    />
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; container-type: inline-size; }
header, .alert { max-width: 980px; }
.cards { display: grid; grid-template-columns: minmax(0, 1fr); gap: 18px; max-width: 980px; }
/* Wide windows: settings in pairs, the two tables across the whole width. */
@container (min-width: 1200px) {
  .cards { grid-template-columns: repeat(2, minmax(0, 1fr)); max-width: 1680px; }
  .wide { grid-column: 1 / -1; }
  .row.token-row { grid-template-columns: minmax(0, 1fr); }
  .token-row .ctl-col { align-items: flex-start; }
  .token-row .ctl { justify-content: flex-start; }
  .token-row .regen { text-align: left; }
}
.title { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
h1 { margin: 0; }
.intro { margin: 0; color: var(--ink2); }
.note { margin: 0; color: var(--warn); font-size: 13px; }
.meta { color: var(--ink3); font-size: 12.5px; margin: 0; }
.muted { color: var(--ink2); margin: 0; }
.card { border: 1px solid var(--line); border-radius: 10px; background: var(--raised); overflow: hidden; }
.card-title { margin: 0; font-size: 15px; font-weight: 600; }
.card > .card-title { padding: 14px 18px 4px; }
.card-head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 10px; padding: 14px 18px 4px; }
.pad { padding: 4px 18px 10px; }
.empty { padding-bottom: 16px; }
.row { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; column-gap: 16px; row-gap: 8px; padding: 12px 18px; border-top: 1px solid var(--line); }
.card-title + .row { border-top: 0; }
.what { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.name { font-weight: 600; }
.hint { color: var(--ink3); font-size: 12.5px; }
.ctl-col { display: flex; flex-direction: column; align-items: flex-end; gap: 6px; min-width: 0; }
.regen { max-width: 440px; text-align: right; }
.ctl { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 8px; min-width: 0; }
.mono { font-family: var(--mono); font-size: 12.5px; }
.num-port { width: 90px; }
.token { width: 240px; max-width: 100%; }
.snips { display: flex; flex-direction: column; gap: 8px; padding: 12px 18px 16px; }
.tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); margin: 0 18px; padding: 0; }
.tabs button { height: 34px; padding: 0 12px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--ink2); font: inherit; font-size: 13.5px; font-weight: 500; cursor: pointer; margin-bottom: -1px; }
.tabs button.on { color: var(--ink); border-bottom-color: var(--accent); }
.scroll { overflow-x: auto; }
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; }
.tbl th { text-align: left; font-size: 12px; font-weight: 600; color: var(--ink3); padding: 8px 18px; border-bottom: 1px solid var(--line); }
.tbl td { padding: 9px 18px; border-top: 1px solid var(--line); vertical-align: middle; }
.tbl tbody tr:first-child td { border-top: 0; }
.grp th { background: var(--sunken); padding: 6px 18px; border-top: 1px solid var(--line); }
.w-tool { width: 200px; }
.w-need { width: 130px; }
.sel { height: 28px; }
.tag { height: 18px; font-size: 11px; margin-left: 4px; }
.lvl { display: inline-flex; align-items: center; height: 20px; padding: 0 7px; border-radius: 999px; font-size: 11.5px; font-weight: 600; white-space: nowrap; }
.l-read { background: var(--idle-soft); color: var(--idle); }
.l-control { background: var(--accent-soft); color: var(--accent-text); }
.l-configs { background: var(--warn-soft); color: var(--warn); }
.t-ok { color: var(--ok); }
.t-bad { color: var(--bad); }
.foot { padding: 10px 18px 14px; border-top: 1px solid var(--line); }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
.switch { appearance: none; position: relative; flex: none; width: 38px; height: 22px; margin: 0; border-radius: 999px; background: var(--line2); cursor: pointer; transition: background 0.15s; }
.switch::after { content: ''; position: absolute; top: 2px; left: 2px; width: 18px; height: 18px; border-radius: 50%; background: var(--on-accent); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.25); transition: transform 0.15s; }
.switch:checked { background: var(--accent); }
.switch:checked::after { transform: translateX(16px); }
.switch:disabled { opacity: 0.55; cursor: default; }
.switch:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
@media (max-width: 640px) {
  .row { grid-template-columns: minmax(0, 1fr); }
  .ctl { justify-content: flex-start; }
  .ctl-col { align-items: flex-start; }
  .regen { text-align: left; }
}
</style>
