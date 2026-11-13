<script setup lang="ts">
// One tab: colour dot, name, the tag chip, its state (failing count or why
// it can't reach supervisord) and, for a server, × to close it. Its health
// comes from that tab's own overview events.
import { computed } from 'vue'
import { connectionBadge, connectionProblem } from '../lib/connection'
import { useOverview } from '../lib/overview'
import { tagOf } from '../lib/servers'
import { LOCAL } from '../lib/tabs'

const props = defineProps<{ id: string; name: string; tag?: string; color?: string; active: boolean; title?: string }>()
const emit = defineEmits<{ select: []; close: [] }>()

const { overview } = useOverview(() => props.id)
const remote = computed(() => props.id !== LOCAL)
const chip = computed(() => (remote.value && props.tag ? tagOf(props.tag) : null))
const state = computed(() => {
  const ov = overview.value
  if (!ov) return remote.value ? { text: 'Connecting…', cls: 't-idle' } : null
  const bad = connectionBadge(connectionProblem(ov.Server))
  if (bad) return { text: remote.value ? 'No access' : bad, cls: 't-warn' }
  if (ov.Stats.Failing > 0) return { text: `${ov.Stats.Failing} failing`, cls: 't-bad' }
  return null
})
const dot = computed(() => {
  if (remote.value) return { background: props.color || 'var(--idle-dot)' }
  return {}
})
const health = computed(() => ({ ok: 'd-ok', warning: 'd-warn', error: 'd-bad' })[overview.value?.Server.Health ?? ''] ?? 'd-idle')
</script>

<template>
  <div
    :class="['tab', { on: active }]"
    role="tab"
    :aria-selected="active"
    :tabindex="active ? 0 : -1"
    :title="title"
    :data-tab="id"
    @click="emit('select')"
    @keydown.enter.prevent="emit('select')"
    @keydown.space.prevent="emit('select')"
    @auxclick.middle="remote && emit('close')"
  >
    <span v-if="remote" class="dot" :style="dot" aria-hidden="true" />
    <span v-else :class="['dot', health]" aria-hidden="true" />
    <span class="nm">{{ name }}</span>
    <span v-if="chip" :class="['chip', chip.cls]">{{ chip.chip }}</span>
    <span v-if="state" :class="['state', state.cls]">{{ state.text }}</span>
    <button v-if="remote" class="x" type="button" :aria-label="`Close ${name} and disconnect`" tabindex="-1" @click.stop="emit('close')">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg>
    </button>
    <span v-else class="pad" />
  </div>
</template>

<style scoped>
.tab {
  display: flex;
  align-items: center;
  gap: 7px;
  height: 36px;
  min-width: 0;
  max-width: 250px;
  box-sizing: border-box;
  padding: 0 6px 0 12px;
  border: 1px solid transparent;
  border-bottom: 0;
  border-radius: 8px 8px 0 0;
  color: var(--ink2);
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  --wails-draggable: no-drag;
}
.tab:hover { background: var(--hover); }
.tab.on { background: var(--bg); border-color: var(--line); color: var(--ink); margin-bottom: -1px; height: 37px; }
.tab:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.dot { width: 9px; height: 9px; flex: none; }
.nm { overflow: hidden; text-overflow: ellipsis; }
.chip { display: inline-flex; align-items: center; height: 18px; padding: 0 6px; border-radius: 5px; font-size: 11px; font-weight: 700; letter-spacing: .02em; flex: none; }
.state { font-size: 12px; font-weight: 600; flex: none; }
.x { display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px; flex: none; border: 0; border-radius: 5px; padding: 0; background: transparent; color: var(--ink3); cursor: pointer; }
.x:hover { background: var(--line); color: var(--ink); }
.pad { width: 6px; flex: none; }
</style>
