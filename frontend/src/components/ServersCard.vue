<script setup lang="ts">
// Settings → Servers: the saved servers (name, tag, who Visor connects as,
// options, open or not) with Connect/Show, Edit and Delete, Add a server,
// and how many may be connected at once (this computer doesn't count).
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { RemoteService, type SavedServerView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { errorMessage } from '../lib/overview'
import { platform, secretStore } from '../lib/platform'
import { closeServer, openServer, serverForm, switchTab, tagOf } from '../lib/servers'
import { serverTabs } from '../lib/tabs'
import { showToast } from '../lib/toast'
import ConfirmDialog from './ConfirmDialog.vue'

const props = defineProps<{ maxServers: number }>()
const emit = defineEmits<{ limit: [n: number] }>()
const router = useRouter()

const list = ref<SavedServerView[]>([])
const failed = ref('')
const deleting = ref<SavedServerView | null>(null)
const limit = ref(String(props.maxServers))
watch(() => props.maxServers, (n) => (limit.value = String(n)))

async function load() {
  try {
    list.value = (await RemoteService.List()) ?? []
    failed.value = ''
  } catch (e) {
    failed.value = errorMessage(e)
  }
}
onMounted(load)
// The form saves elsewhere; reload when it closes, and when tabs open or close.
watch(serverForm, (f) => (f ? undefined : void load()))
watch(() => serverTabs.value.length, () => void load())

function open(s: SavedServerView) {
  if (serverTabs.value.some((t) => t.id === s.Server.id)) return switchTab(router, s.Server.id)
  void openServer(router, s.Server)
}

async function remove() {
  const s = deleting.value
  deleting.value = null
  if (!s) return
  try {
    if (serverTabs.value.some((t) => t.id === s.Server.id)) await closeServer(router, s.Server.id)
    await RemoteService.Delete(s.Server.id)
    showToast(`${s.Server.name} deleted`)
  } catch (e) {
    showToast(errorMessage(e), 'error')
  }
  void load()
}

function setLimit() {
  const n = Math.round(Number(limit.value))
  if (!Number.isFinite(n) || n < 1) {
    limit.value = String(props.maxServers)
    return
  }
  const clamped = Math.min(10, Math.max(1, n))
  limit.value = String(clamped)
  if (clamped !== props.maxServers) emit('limit', clamped)
}

const addr = (s: SavedServerView) => `${s.Server.user ? s.Server.user + '@' : ''}${s.Server.host}${s.Server.port !== 22 ? ':' + s.Server.port : ''}`
const options = (s: SavedServerView) => [s.Server.useSudo ? 'Use sudo' : '', s.Server.readOnly ? 'Read only' : '', s.Server.keyPath ? 'Key' : ''].filter(Boolean)
const isOpen = (s: SavedServerView) => serverTabs.value.some((t) => t.id === s.Server.id)
</script>

<template>
  <section class="card servers" aria-labelledby="servers-title">
    <div class="head">
      <h2 id="servers-title" class="card-title">Servers</h2>
      <label class="limit muted">
        Connected at once, at most
        <input v-model="limit" class="input num" inputmode="numeric" aria-label="Servers connected at once" @change="setLimit" @keydown.enter="setLimit" />
        <span class="meta">({{ platform().local }} doesn’t count)</span>
      </label>
      <button class="btn sm" type="button" @click="serverForm = { server: null }">Add a server…</button>
    </div>
    <p v-if="failed" class="err">{{ failed }}</p>
    <p v-else-if="!list.length" class="empty muted">No servers yet. Add one to manage its supervisord over SSH, in a tab beside {{ platform().local }}.</p>
    <template v-else>
      <div class="srv th-row" aria-hidden="true"><span class="th">Name</span><span class="th">Connects as</span><span class="th">Options</span><span class="th end">Status</span></div>
      <div v-for="s in list" :key="s.Server.id" class="srv" :data-server="s.Server.name">
        <span class="nm">
          <span class="dot" :style="{ background: s.Server.color || 'var(--idle-dot)' }" aria-hidden="true" />
          <strong>{{ s.Server.name }}</strong>
          <span :class="['chip', tagOf(s.Server.tag).cls]">{{ tagOf(s.Server.tag).chip }}</span>
        </span>
        <span class="mono addr">{{ addr(s) }}</span>
        <span class="opts"><span v-for="o in options(s)" :key="o" class="badge b-idle">{{ o }}</span></span>
        <span class="act">
          <span :class="isOpen(s) ? 't-ok' : 't-idle'">{{ isOpen(s) ? 'Open' : 'Not open' }}</span>
          <button class="btn sm" type="button" @click="open(s)">{{ isOpen(s) ? 'Show' : 'Connect' }}</button>
          <button class="btn sm" type="button" :aria-label="`Edit ${s.Server.name}`" @click="serverForm = { server: s }">Edit</button>
          <button class="btn sm quiet danger" type="button" :aria-label="`Delete ${s.Server.name}`" @click="deleting = s">Delete</button>
        </span>
      </div>
    </template>
    <p class="meta foot">Passwords, key passphrases and sudo passwords are kept in your {{ secretStore() }}. Deleting a server also deletes them and its history.</p>
    <ConfirmDialog
      v-if="deleting"
      :title="`Delete ${deleting.Server.name}?`"
      :message="`Its tab closes, and its saved passwords and its Issues history on this computer are deleted. Nothing changes on the server.`"
      confirm-label="Delete"
      @confirm="remove"
      @cancel="deleting = null"
    />
  </section>
</template>

<style scoped>
.servers { margin-bottom: 16px; max-width: 860px; }
@container (min-width: 1000px) { .servers { max-width: none; } }
.head { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 16px; padding: 14px 18px 8px; }
.head .card-title { margin: 0; }
.limit { display: inline-flex; align-items: center; gap: 6px; margin-left: auto; }
.num { width: 48px; text-align: center; }
.srv { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(0, 1.4fr) minmax(0, 1fr) auto; align-items: center; column-gap: 16px; padding: 12px 18px; border-top: 1px solid var(--line); }
.th-row { padding-top: 8px; padding-bottom: 8px; }
.th { font-size: 12px; font-weight: 600; color: var(--ink3); }
.end { text-align: right; }
.nm { display: flex; align-items: center; gap: 8px; min-width: 0; }
.nm strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.chip { display: inline-flex; align-items: center; height: 18px; padding: 0 6px; border-radius: 5px; font-size: 11px; font-weight: 700; letter-spacing: .02em; flex: none; }
.addr { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12.5px; }
.opts { display: flex; flex-wrap: wrap; gap: 4px; }
.act { display: flex; align-items: center; gap: 6px; justify-content: flex-end; }
.act > span { font-size: 12.5px; font-weight: 600; margin-right: 4px; }
.empty, .err, .foot { margin: 0; padding: 10px 18px 14px; }
.err { color: var(--bad); }
.foot { border-top: 1px solid var(--line); }
@container (max-width: 700px) {
  .srv { grid-template-columns: minmax(0, 1fr); row-gap: 6px; }
  .th-row { display: none; }
  .act { justify-content: flex-start; }
}
</style>
