<script setup lang="ts">
// A server's report: what its SSH user can and can't do there, one check per
// row, each gap with its fix (spec 2.3). Shown when a tab can't reach
// supervisord, and from the sidebar's Server report. Check again re-runs it;
// while the tab is blocked Visor re-checks every 30 seconds.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RemoteService, type ReportView, type SavedServerView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import CommandBlock from '../components/CommandBlock.vue'
import { errorMessage, useOverview } from '../lib/overview'
import { reports, serverForm, tagOf } from '../lib/servers'
import { currentTab } from '../lib/tabs'

const id = currentTab.value
const { overview } = useOverview()
const report = ref<ReportView | null>(reports.value[id] ?? null)
const checking = ref(false)
const failed = ref('')
const saved = ref<SavedServerView | null>(null)

const host = computed(() => overview.value?.Host)
const user = computed(() => saved.value?.Server.user || 'this user')
const blocked = computed(() => !!report.value?.Blocked)
const dot = (s: string) => ({ ok: 'd-ok', error: 'd-bad', warning: 'd-warn' })[s] ?? 'd-idle'

async function check() {
  checking.value = true
  failed.value = ''
  try {
    report.value = await RemoteService.Recheck(id)
    reports.value = { ...reports.value, [id]: report.value }
  } catch (e) {
    failed.value = errorMessage(e)
  } finally {
    checking.value = false
  }
}

async function edit() {
  const list = (await RemoteService.List()) ?? []
  serverForm.value = { server: list.find((s) => s.Server.id === id) ?? null }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  const list = await RemoteService.List().catch(() => null)
  saved.value = list?.find((s) => s.Server.id === id) ?? null
  if (!report.value) await check()
  // Re-check a blocked tab on its own, except after a refused password or
  // key: each try would count against the server's lockout.
  timer = setInterval(() => {
    if (blocked.value && !report.value?.Credentials && !checking.value) void check()
  }, 30_000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <section class="page" aria-labelledby="report-h">
    <header>
      <div class="title">
        <h1 id="report-h">Server report</h1>
        <span v-if="host?.Tag" :class="['badge', tagOf(host.Tag).cls]">{{ tagOf(host.Tag).label }}</span>
      </div>
      <p class="muted">What <span class="mono">{{ user }}</span> can do on {{ host?.Name ?? 'this server' }}. Visor runs these checks with every Test and when a tab can’t connect.</p>
    </header>

    <div v-if="blocked" class="alert" role="alert">
      <span><strong>Visor can’t manage supervisord here yet.</strong> Fix the first red row below, then check again.</span>
      <button class="btn" type="button" @click="edit">Edit server</button>
      <button class="btn" type="button" :disabled="checking" @click="check">{{ checking ? 'Checking…' : 'Check again' }}</button>
    </div>
    <div v-else class="bar">
      <button class="btn" type="button" @click="edit">Edit server</button>
      <button class="btn" type="button" :disabled="checking" @click="check">{{ checking ? 'Checking…' : 'Check again' }}</button>
    </div>
    <p v-if="failed" class="err">{{ failed }}</p>

    <section v-if="report" class="card checks" aria-label="Checks">
      <div v-for="r in report.Rows ?? []" :key="r.ID" class="ck" :data-row="r.ID" :data-status="r.Status">
        <span :class="['dot', dot(r.Status)]" aria-hidden="true" />
        <div class="what">
          <span :class="['t', { 't-bad': r.Status === 'error', 't-warn': r.Status === 'warning' }]">{{ r.Title }}</span>
          <span class="meta wrap">{{ r.Detail }}</span>
          <ol v-if="r.Fix?.length" class="fix">
            <li v-for="(f, i) in r.Fix" :key="i">
              <p>{{ f.Text }}</p>
              <CommandBlock v-if="f.Command" :text="f.Command" />
            </li>
          </ol>
        </div>
      </div>
    </section>
    <p v-else-if="checking" class="muted">Checking…</p>
    <p class="meta">Visor never runs sudo or changes the server’s files to fix these: the commands are for you or the server’s admin.</p>
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; max-width: 860px; }
header { display: flex; flex-direction: column; gap: 6px; }
.title { display: flex; align-items: center; gap: 10px; }
h1 { margin: 0; }
.alert { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 12px 14px; border: 1px solid var(--warn-dot); border-radius: 10px; background: var(--warn-soft); color: var(--warn); }
.alert span { flex: 1 1 320px; }
.bar { display: flex; gap: 8px; }
.checks { padding: 0; }
.ck { display: grid; grid-template-columns: 22px minmax(0, 1fr); gap: 10px; padding: 14px 18px; border-top: 1px solid var(--line); }
.ck:first-child { border-top: 0; }
.ck .dot { margin-top: 6px; }
.what { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.t { font-weight: 600; }
.wrap { overflow-wrap: anywhere; user-select: text; }
.fix { margin: 6px 0 0; padding-left: 18px; display: flex; flex-direction: column; gap: 8px; }
.fix p { margin: 0 0 6px; color: var(--ink2); }
.err { color: var(--bad); }
</style>
