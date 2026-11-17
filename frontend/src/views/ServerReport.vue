<script setup lang="ts">
// A server's report, laid out as Doctor is: the lens with one segment per
// check, a headline, and the checks with each gap's fix (spec 2.3). From the
// sidebar's Server report, and when an open tab's connection fails. Check
// again re-runs it; while the tab is blocked Visor re-checks every 30
// seconds.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RemoteService, type ReportView, type SavedServerView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import Lens from '../components/Lens.vue'
import ReportRows from '../components/ReportRows.vue'
import { reportLens, reportLook } from '../lib/doctor'
import { errorMessage, useOverview } from '../lib/overview'
import { reports, serverForm, tagOf } from '../lib/servers'
import { currentTab } from '../lib/tabs'

const id = currentTab.value
const { overview } = useOverview()
const report = ref<ReportView | null>(reports.value[id] ?? null)
const checking = ref(false)
const failed = ref('')
const saved = ref<SavedServerView | null>(null)
const runs = ref(0)

const host = computed(() => overview.value?.Host)
const user = computed(() => saved.value?.Server.user || 'this user')
const blocked = computed(() => !!report.value?.Blocked)
const rows = computed(() => report.value?.Rows ?? [])
const lens = computed(() => reportLens(rows.value))
const segments = computed(() => rows.value.map((r) => reportLook(r.Status).segment))

async function check() {
  checking.value = true
  failed.value = ''
  try {
    report.value = await RemoteService.Recheck(id)
    reports.value = { ...reports.value, [id]: report.value }
    runs.value++
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
    <div class="lens-col">
      <Lens :key="runs" :segments="segments" :big="lens.big" :small="lens.small" :pulse="lens.big === '✓'" />
    </div>
    <div class="text-col" :aria-busy="checking">
      <header>
        <div class="title">
          <h1 id="report-h" class="display">{{ blocked ? 'Visor can’t manage supervisord here yet' : 'Server report' }}</h1>
          <span v-if="host?.Tag" :class="['badge', tagOf(host.Tag).cls]">{{ tagOf(host.Tag).label }}</span>
        </div>
        <p class="lede">
          <template v-if="blocked">Fix the first row that needs attention, then check again. </template>
          What <span class="mono">{{ user }}</span> can do on {{ host?.Name ?? 'this server' }}. Visor runs these checks with every Test, and when a server can’t connect.
        </p>
      </header>
      <div class="bar">
        <button class="btn" type="button" @click="edit">Edit server</button>
        <button :class="['btn', { primary: blocked }]" type="button" :disabled="checking" @click="check">{{ checking ? 'Checking…' : 'Check again' }}</button>
      </div>
      <p v-if="failed" class="err" role="alert">{{ failed }}</p>
      <section v-if="report" class="card checks" aria-label="Checks"><ReportRows :rows="rows" /></section>
      <p v-else-if="checking" class="muted">Checking…</p>
      <p class="meta">Visor never runs sudo or changes the server’s files to fix these: the commands are for you or the server’s admin.</p>
    </div>
  </section>
</template>

<style scoped>
.page { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 40px; }
.lens-col { flex: 0 1 260px; display: flex; justify-content: center; --lens-size: 260px; }
.text-col { flex: 1 1 420px; max-width: 640px; display: flex; flex-direction: column; gap: 18px; }
header { display: flex; flex-direction: column; gap: 8px; }
.title { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.display { margin: 0; font-family: var(--display); font-optical-sizing: auto; font-size: 34px; font-weight: 650; line-height: 1.05; letter-spacing: -0.015em; }
.lede { margin: 0; font-size: 15px; line-height: 1.5; color: var(--ink2); }
.bar { display: flex; gap: 8px; }
.checks { padding: 0; }
.err { color: var(--bad); }
</style>
