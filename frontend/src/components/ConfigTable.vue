<script setup lang="ts">
// Configs and their processes: collapsible config rows with the Watch toggle
// and action menus. The table only reports what was chosen; Dashboard wires
// each menu id to a service call. A failing row is tinted and shows its
// reason in full under the name; focusProcess brings one into view. With
// `offline` (the reason supervisord can't be used), the menus keep only
// their file actions, and `note` is a quiet line under the rows.
import { nextTick, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { sinceLabel } from '../lib/format'
import { isFailing, reasonClass, summaryParts } from '../lib/health'
import { configMenu, forHost, processMenu, withoutSupervisord, type HostRules, type MenuItem } from '../lib/menus'
import { configRoute } from '../lib/links'
import { sparkPath, usageLabel, usageMark, usageText } from '../lib/usage'
import ActionMenu from './ActionMenu.vue'
import StateText from './StateText.vue'
import { readOnlyReason, useFolder } from '../lib/folder'

// host: the tab's machine (a server's menus differ: forHost).
const props = defineProps<{ configs: ConfigView[]; nowMs: number; offline?: string; note?: string; host?: HostRules }>()
const emit = defineEmits<{
  process: [p: ProcessView, c: ConfigView, id: string]
  config: [c: ConfigView, id: string]
  watch: [c: ConfigView, on: boolean]
}>()

const NEEDS_FILE = ['open-config', 'describe', 'reveal', 'download', 'open-log', 'load', 'apply', 'remove']
const online = (items: MenuItem[]) => forHost(props.offline ? withoutSupervisord(items, props.offline) : items, props.host)
// Removing moves the file out of the config folder, so a read-only folder
// turns it off, with the reason as its tooltip.
const folder = useFolder()
const fileMenu = (c: ConfigView) =>
  online(configMenu(c)).map((i) => {
    if (!c.Path && NEEDS_FILE.includes(i.id)) return { ...i, disabled: true, title: 'This process has no config file in the include folder' }
    const ro = readOnlyReason(folder.value)
    return i.id === 'remove' && ro ? { ...i, disabled: true, title: ro } : i
  })
/** supervisord runs nothing from the file yet: its summary says "not loaded", so no "changed" badge too. */
const unloadedFile = (c: ConfigView) => (c.NotLoaded ?? []).length > 0 && !(c.Processes ?? []).length
const collapsed = reactive<Record<string, boolean>>({})
const key = (c: ConfigView) => c.Path || c.FileName
const root = ref<HTMLElement>()

/** How many of the file's processes share p's group, for the short supervisorctl name. */
const groupSize = (c: ConfigView, p: ProcessView) => (c.Processes ?? []).filter((q) => q.Group === p.Group).length

/** Expands the process's config, scrolls its row into view and focuses it. */
async function focusProcess(fullName: string) {
  const c = props.configs.find((x) => (x.Processes ?? []).some((p) => p.FullName === fullName))
  if (c) collapsed[key(c)] = false
  await nextTick()
  const row = root.value?.querySelector<HTMLElement>(`[data-process="${CSS.escape(fullName)}"]`)
  if (!row) return
  row.scrollIntoView({ block: 'center' })
  row.focus({ preventScroll: true })
}

defineExpose({ focusProcess })
</script>

<template>
  <div ref="root" class="frame">
    <div class="grid">
      <div class="row head">
        <span class="th">Name</span><span class="th">State</span><span class="th num" title="CPU (100 % is one core) and memory, with the children each process starts">Usage</span><span class="th">For</span><span class="th num">PID</span><span class="th num">Watch</span>
      </div>
      <p v-if="!configs.length" class="empty">No configs or processes match.</p>
      <div v-for="c in configs" :key="key(c)" class="cfg">
        <div class="row cfg-row">
          <div class="name">
            <button
              type="button"
              class="btn quiet icon sm chev"
              :aria-label="(collapsed[key(c)] ? 'Show ' : 'Hide ') + c.FileName"
              :aria-expanded="!collapsed[key(c)]"
              @click="collapsed[key(c)] = !collapsed[key(c)]"
            >
              <svg :class="{ open: !collapsed[key(c)] }" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg>
            </button>
            <RouterLink v-if="c.Path" class="file-link" :to="configRoute(c.Path)">{{ c.FileName }}</RouterLink>
            <span v-else class="file-link">{{ c.FileName }}</span>
            <span v-if="c.Changed && !unloadedFile(c)" class="badge b-warn">changed</span>
            <span class="summary meta"
              ><span v-for="(s, i) in summaryParts(c.Summary)" :key="i" :class="{ 't-bad': s.failing, strong: s.failing }">{{ s.text }}</span></span
            >
          </div>
          <div class="acts">
            <button
              v-if="c.Path && (c.NotLoaded ?? []).length && !offline"
              type="button"
              class="btn sm load"
              :title="`supervisord hasn’t loaded ${c.FileName} yet. Load adds its programs (supervisorctl update).`"
              :aria-label="`Load ${c.FileName} into supervisord`"
              @click="emit('config', c, 'load')"
            >
              Load
            </button>
            <button
              v-if="c.Path"
              type="button"
              :class="['btn quiet icon sm watch', { on: c.Watched }]"
              :aria-pressed="c.Watched"
              :aria-label="`${c.Watched ? 'Watching' : 'Watch'} ${c.FileName}`"
              :title="c.Watched ? 'Watching: Visor notifies you about problems in this file' : 'Watch: Visor stays quiet about this file until you watch it'"
              @click="emit('watch', c, !c.Watched)"
            >
              <svg width="15" height="15" viewBox="0 0 24 24" :fill="c.Watched ? 'currentColor' : 'none'" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.9 1.9 0 0 0 3.4 0" fill="none" /></svg>
            </button>
            <ActionMenu
              :items="fileMenu(c)"
              :label="`Actions for ${c.FileName}`"
              heading="Whole config file"
              small
              @select="(id) => emit('config', c, id)"
            />
          </div>
        </div>
        <template v-if="!collapsed[key(c)]">
          <div
            v-for="p in c.Processes ?? []"
            :key="p.FullName"
            :class="['row proc', { failing: isFailing(p) }]"
            :data-process="p.FullName"
            :tabindex="isFailing(p) ? -1 : undefined"
          >
            <div class="pcell">
              <component
                :is="c.Path ? RouterLink : 'span'"
                :to="configRoute(c.Path, { process: p.FullName })"
                class="pname" :title="p.FullName"
              ><span v-if="p.Group !== p.Name" class="grp">{{ p.Group }}:</span>{{ p.Name }}</component>
              <span v-if="p.Detail" :class="['why', reasonClass(p)]">{{ p.Detail }}</span>
            </div>
            <span><StateText :state="p.State" :status="p.Status" /></span>
            <span class="use" :data-usage="p.FullName" :title="usageText(p)">
              <span class="sr-only">{{ usageText(p) }}</span>
              <svg v-if="usageLabel(p).known && (p.Spark ?? []).length > 1" class="spark" width="48" height="14" viewBox="0 0 48 14" aria-hidden="true"><polyline :points="sparkPath(p.Spark ?? [], 48, 14)" /></svg>
              <span v-if="usageLabel(p).known" class="figs" aria-hidden="true"
                ><span class="fig">{{ usageLabel(p).cpu }}</span><span class="sep">·</span><span class="fig">{{ usageLabel(p).mem }}</span></span
              >
              <span v-else class="fig" aria-hidden="true">{{ usageMark(p) }}</span>
            </span>
            <span class="meta">{{ sinceLabel(p, nowMs) }}</span>
            <span class="mono pid num">{{ p.PID > 0 ? p.PID : '–' }}</span>
            <div class="acts">
              <ActionMenu
                :items="online(processMenu(p, groupSize(c, p)))"
                :label="`Actions for ${p.FullName}`"
                heading="This process"
                small
                @select="(id) => emit('process', p, c, id)"
              />
            </div>
          </div>
          <div v-for="n in c.NotLoaded ?? []" :key="'not-loaded:' + n.Program" class="row proc unloaded" :data-not-loaded="n.Program">
            <div class="pcell">
              <span class="pname" :title="`${n.Program} isn’t loaded: supervisord doesn’t know it yet`">{{ n.Program }}</span>
            </div>
            <span class="state"><span class="dot d-idle" aria-hidden="true" /><span class="t-idle">Not loaded</span></span>
            <span class="use"><span class="fig">–</span></span>
            <span class="meta">–</span>
            <span class="mono pid num">–</span>
            <div class="acts" />
          </div>
        </template>
      </div>
    </div>
    <p v-if="note" class="note">{{ note }}</p>
  </div>
</template>

<style scoped>
.frame { border: 1px solid var(--line); border-radius: 10px; background: var(--raised); overflow-x: auto; container-type: inline-size; }
/* Name gets at least 180 px; the rest are fixed (+ 5 gaps and the padding),
   so the table fits the default 1000 px window. */
.grid { min-width: 742px; }
.row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 120px 118px 96px 64px 72px;
  align-items: center;
  column-gap: 12px;
  padding: 0 16px;
}
.head { height: 34px; background: var(--sunken); }
.th { font-size: 12px; font-weight: 600; color: var(--ink3); }
.num { text-align: right; }
.cfg { border-top: 1px solid var(--line); }
.cfg-row { min-height: 44px; }
.cfg-row .name { grid-column: 1 / 6; }
.cfg-row:hover, .proc:hover { background: var(--sunken); }
.name { display: flex; align-items: center; gap: 8px; min-width: 0; }
.chev { flex: none; margin-left: -6px; color: var(--ink3); }
.chev svg { transition: transform 0.12s; }
.chev svg.open { transform: rotate(90deg); }
.file-link { flex: none; font-weight: 600; color: var(--ink); text-decoration: none; white-space: nowrap; }
a.file-link:hover { text-decoration: underline; }
.summary { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.strong { font-weight: 600; }
.acts { display: flex; justify-content: flex-end; align-items: center; gap: 2px; }
.watch { color: var(--ink3); }
.watch.on, .watch.on:hover:not(:disabled) { color: var(--accent-text); }
.proc { min-height: 38px; padding-top: 6px; padding-bottom: 6px; border-top: 1px solid var(--line); }
.proc.failing, .proc.failing:hover { background: var(--bad-soft); }
.proc:focus { outline: 2px solid var(--focus); outline-offset: -2px; }
/* The name lines up with the file name, past the chevron. */
.pcell { display: flex; flex-direction: column; gap: 1px; min-width: 0; padding-left: 28px; }
.pname { font-family: var(--mono); font-size: 12.5px; color: var(--ink); text-decoration: none; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
a.pname:hover { text-decoration: underline; }
.grp { color: var(--ink3); }
/* The reason, in full: it wraps instead of being cut off. */
.why { font-size: 12.5px; line-height: 1.4; overflow-wrap: anywhere; user-select: text; }
.meta { color: var(--ink3); font-size: 12.5px; }
.mono { font-family: var(--mono); font-size: 12.5px; }
.pid { color: var(--ink2); font-variant-numeric: tabular-nums; }
/* A program supervisord doesn't know yet: grey, and Load on its file's row. */
.unloaded .pname { color: var(--ink2); }
.state { display: inline-flex; align-items: center; gap: 6px; }
.load { margin-right: 4px; }
.empty { margin: 0; padding: 16px; color: var(--ink2); }
.note { margin: 0; padding: 10px 16px; border-top: 1px solid var(--line); background: var(--sunken); color: var(--ink3); font-size: 12.5px; }
/* Usage: CPU · memory, right-aligned; the 10-minute sparkline on wide tables. */
.use { position: relative; display: flex; align-items: center; justify-content: flex-end; gap: 5px; font-family: var(--mono); font-size: 12px; color: var(--ink2); font-variant-numeric: tabular-nums; white-space: nowrap; }
.figs { display: inline-flex; gap: 5px; }
.sep { color: var(--ink3); }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
.spark { display: none; flex: none; }
.spark polyline { fill: none; stroke: var(--accent); stroke-width: 1.3; stroke-linejoin: round; }
@container (min-width: 900px) {
  .row { grid-template-columns: minmax(200px, 1fr) 120px 172px 110px 80px 72px; }
  .spark { display: block; }
}
</style>
