<script setup lang="ts">
// Activity (board "Activity"): what changed on this tab's supervisord, by
// Visor or by anyone else (supervisorctl, a deploy script, another person),
// by day, newest first, with filters. Changes made outside Visor notify
// you; each kind can be muted (Settings, or the server's Notifications).
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { byDay, byLabel, clock, KIND_GROUPS, matches, useActivity, type ActivityFilter } from '../lib/activity'
import { configRoute } from '../lib/links'
import { useOverview } from '../lib/overview'
import { currentTab, LOCAL } from '../lib/tabs'

const router = useRouter()
const { overview } = useOverview()
const { events, error, loaded } = useActivity()
const filter = reactive<ActivityFilter>({ who: 'all', kind: '', process: '' })
const now = ref(Date.now())
const tick = setInterval(() => (now.value = Date.now()), 60_000)
onBeforeUnmount(() => clearInterval(tick))

const remote = currentTab.value !== LOCAL
const name = computed(() => overview.value?.Host?.Name || 'this computer')
const processes = computed(() => [...new Set(events.value.map((e) => e.Process).filter(Boolean))].sort())
const days = computed(() => byDay(events.value.filter((e) => matches(e, filter)), now.value))
const WHO = [
  { id: 'all', label: 'All' },
  { id: 'outside', label: 'Outside Visor' },
  { id: 'visor', label: 'By Visor' },
] as const

const ICON: Record<string, string> = {
  processes: 'M4 12a8 8 0 1 0 2.3-5.6M4 4v4h4',
  programs: 'M12 5v14M5 12h14',
  files: 'M6 3h8l4 4v14H6zM14 3v4h4',
  daemon: 'M12 3v9M5.6 6.6a8 8 0 1 0 12.8 0',
}
const TONE: Record<string, string> = { processes: 'b-accent', programs: 'b-ok', files: 'b-warn', daemon: 'b-bad' }

function open(e: { Config: string; Process: string }) {
  if (e.Config) void router.push(configRoute(e.Config, e.Process ? { process: e.Process } : {}))
}
</script>

<template>
  <section class="page" aria-labelledby="act-h">
    <header>
      <h1 id="act-h">Activity</h1>
      <p class="muted">
        What changed on {{ name }}, by Visor or by anyone else (supervisorctl, a deploy script, another person). Changes made outside Visor notify you; mute a kind in
        {{ remote ? `Remote servers → ${name} → Edit` : 'Settings → Changes made outside Visor' }}.
      </p>
    </header>
    <div class="filters">
      <div class="seg" role="radiogroup" aria-label="Who">
        <button v-for="w in WHO" :key="w.id" type="button" role="radio" :aria-checked="filter.who === w.id" :class="{ on: filter.who === w.id }" @click="filter.who = w.id">{{ w.label }}</button>
      </div>
      <select v-model="filter.kind" class="input" aria-label="Kind">
        <option v-for="k in KIND_GROUPS" :key="k.id" :value="k.id">{{ k.label }}</option>
      </select>
      <select v-model="filter.process" class="input" aria-label="Process">
        <option value="">All processes</option>
        <option v-for="p in processes" :key="p" :value="p">{{ p }}</option>
      </select>
    </div>
    <p v-if="error" class="err" role="alert">{{ error }}</p>
    <p v-else-if="loaded && !days.length" class="muted empty">Nothing yet. Starts, stops, restarts, config changes and supervisord restarts show here as they happen.</p>
    <section v-for="d in days" :key="d.label" :aria-label="d.label" class="day">
      <h2 class="th">{{ d.label }}</h2>
      <div class="card list">
        <div v-for="e in d.events" :key="e.ID" class="ev" :data-kind="e.Kind" :data-by-visor="e.ByVisor">
          <span class="meta time">{{ clock(e.AtMs) }}</span>
          <span :class="['ic', TONE[e.Group]]" aria-hidden="true">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="ICON[e.Group]" /></svg>
          </span>
          <span class="what"><strong>{{ e.Text }}</strong><span v-if="e.FileName && e.Process" class="meta">{{ e.FileName }}</span></span>
          <span :class="['badge', e.ByVisor ? 'b-idle' : 'b-accent']" :data-via="e.Via || undefined">{{ byLabel(e) }}</span>
          <span class="link-cell"><button v-if="e.Config" type="button" class="link" @click="open(e)">Open config</button></span>
        </div>
      </div>
    </section>
    <p class="meta">Kept as long as process history (Settings → Checking and history). supervisord doesn’t say who made a change, so Visor shows “outside Visor”.</p>
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 16px; max-width: 900px; }
header { display: flex; flex-direction: column; gap: 6px; }
h1 { margin: 0; }
header p { margin: 0; }
.filters { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.filters .input { width: auto; }
.day { display: flex; flex-direction: column; gap: 6px; }
.th { margin: 0; font-size: 12px; font-weight: 600; color: var(--ink3); }
.list { padding: 0; }
.ev { display: grid; grid-template-columns: 56px 28px minmax(0, 1fr) auto auto; align-items: center; column-gap: 10px; padding: 10px 16px; border-top: 1px solid var(--line); }
.ev:first-child { border-top: 0; }
.ic { width: 26px; height: 26px; border-radius: 50%; display: flex; align-items: center; justify-content: center; }
.what { display: flex; flex-direction: column; min-width: 0; }
.what strong { font-weight: 600; font-family: var(--mono); font-size: 13px; overflow-wrap: anywhere; }
.link { border: 0; background: none; padding: 0; color: var(--accent-text); font: inherit; font-size: 12.5px; cursor: pointer; }
.err { color: var(--bad); }
.empty { margin: 8px 0; }
</style>
