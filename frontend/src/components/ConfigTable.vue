<script setup lang="ts">
// Configs and their processes: collapsible config rows with the Watch toggle
// and action menus. The table only reports what was chosen; Dashboard wires
// each menu id to a service call.
import { reactive } from 'vue'
import { RouterLink } from 'vue-router'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { sinceLabel } from '../lib/format'
import { comingSoon, configMenu, processMenu } from '../lib/menus'
import ActionMenu from './ActionMenu.vue'
import StateBadge from './StateBadge.vue'

defineProps<{ configs: ConfigView[]; nowMs: number }>()
const emit = defineEmits<{
  process: [p: ProcessView, c: ConfigView, id: string]
  config: [c: ConfigView, id: string]
  watch: [c: ConfigView, on: boolean]
}>()

const collapsed = reactive<Record<string, boolean>>({})
const key = (c: ConfigView) => c.Path || c.FileName

function procName(p: ProcessView) {
  return p.Group === p.Name ? p.Name : p.FullName
}
</script>

<template>
  <div class="frame">
    <div class="grid">
      <div class="row head label">
        <span>Name</span><span>State</span><span>For</span><span>PID</span><span>Details</span><span>Notify</span><span />
      </div>
      <p v-if="!configs.length" class="empty">No configs or processes match.</p>
      <div v-for="c in configs" :key="key(c)" class="cfg">
        <div class="row cfg-row">
          <div class="name">
            <button
              type="button"
              class="icon small"
              :aria-label="(collapsed[key(c)] ? 'Show ' : 'Hide ') + c.FileName"
              :aria-expanded="!collapsed[key(c)]"
              @click="collapsed[key(c)] = !collapsed[key(c)]"
            >
              <svg :class="{ open: !collapsed[key(c)] }" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg>
            </button>
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" class="file" aria-hidden="true"><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z" /><path d="M14 3v6h6" /></svg>
            <RouterLink v-if="c.Path" class="file-link" :to="`/config/${c.Path}`">{{ c.FileName }}</RouterLink>
            <span v-else class="file-link">{{ c.FileName }}</span>
            <span v-if="c.Changed" class="chip">changed</span>
          </div>
          <div class="summary">{{ c.Summary }}</div>
          <div v-if="c.Path">
            <button
              type="button"
              :class="['watch', { on: c.Watched }]"
              :aria-pressed="c.Watched"
              :title="c.Watched ? 'Visor notifies you about problems in this file' : 'Visor stays quiet about this file'"
              @click="emit('watch', c, !c.Watched)"
            >
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.9 1.9 0 0 0 3.4 0" /></svg>
              {{ c.Watched ? 'Watching' : 'Watch' }}
            </button>
          </div>
          <div v-else />
          <ActionMenu
            :items="comingSoon(configMenu(c), ['reveal', 'open-log', 'apply', 'remove'])"
            :label="`Actions for ${c.FileName}`"
            heading="Whole config file"
            @select="(id) => emit('config', c, id)"
          />
        </div>
        <template v-if="!collapsed[key(c)]">
          <div v-for="p in c.Processes ?? []" :key="p.FullName" class="row proc">
            <component
              :is="c.Path ? RouterLink : 'span'"
              :to="{ path: `/config/${c.Path}`, query: { process: p.FullName } }"
              class="pname" :title="p.FullName"
            >{{ procName(p) }}</component>
            <span><StateBadge :state="p.State" :status="p.Status" /></span>
            <span class="sub">{{ sinceLabel(p, nowMs) }}</span>
            <span class="mono sub">{{ p.PID > 0 ? p.PID : '—' }}</span>
            <span class="detail" :title="p.Detail">{{ p.Detail }}</span>
            <span />
            <ActionMenu
              :items="processMenu(p)"
              :label="`Actions for ${p.FullName}`"
              heading="This process"
              @select="(id) => emit('process', p, c, id)"
            />
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.frame { border: 1px solid var(--line); border-radius: 10px; overflow-x: auto; }
.grid { min-width: 1000px; }
.row {
  display: grid;
  grid-template-columns: minmax(280px, 2.4fr) 110px 140px 60px minmax(150px, 1.6fr) 110px 40px;
  align-items: center;
  gap: 12px;
  padding: 0 12px;
  min-height: 40px;
}
.row:hover:not(.head) { background: var(--bg-subtle); }
.head { min-height: 34px; background: var(--bg-subtle); }
.label { font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); }
.cfg { border-top: 1px solid var(--line); }
.cfg-row { background: var(--bg-subtle); }
.cfg-row .summary { grid-column: span 4; color: var(--text-muted); }
.name { display: flex; align-items: center; gap: 6px; min-width: 0; }
.file { color: var(--text-muted); flex: none; }
.file-link { font-weight: 600; color: var(--text); text-decoration: none; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
a.file-link:hover { text-decoration: underline; }
.chip { padding: 2px 8px; border-radius: 999px; font-size: 12px; font-weight: 600; background: var(--warning-bg); color: var(--warning-fg); }
.icon { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--stopped-fg); cursor: pointer; }
.icon.small { width: 24px; height: 24px; }
.icon:hover { background: var(--bg-hover); }
.icon svg { transition: transform 0.12s; }
.icon svg.open { transform: rotate(90deg); }
.watch {
  display: inline-flex; align-items: center; gap: 5px; min-height: 28px; padding: 0 9px;
  border-radius: 999px; border: 1px solid var(--line-strong); background: var(--bg);
  font: inherit; font-size: 12px; color: var(--stopped-fg); cursor: pointer;
}
.watch.on { background: var(--bg-active); border-color: var(--accent-text); color: var(--accent-text); }
.proc { border-top: 1px solid var(--line); }
.pname { padding-left: 52px; font-family: var(--font-mono); font-size: 12px; color: var(--text); text-decoration: none; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
a.pname:hover { text-decoration: underline; }
.sub { color: var(--stopped-fg); }
.mono { font-family: var(--font-mono); font-size: 12px; }
.detail { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.empty { margin: 0; padding: 16px 12px; color: var(--text-muted); }
</style>
